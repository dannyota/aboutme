// Package realtime contains the bounded local invalidation hub and PostgreSQL
// notification listener used by the SSE transport.
package realtime

import (
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"sync"
	"syscall"

	"github.com/google/uuid"
)

const (
	defaultMaxConnections = 2000
	defaultMaxPerIP       = 100
	defaultMaxPerAccount  = 20
	defaultQueueDepth     = 8
)

var (
	// ErrLimited indicates that a hub admission budget is exhausted.
	ErrLimited = errors.New("realtime: connection limit reached")
	// ErrUnavailable indicates that the notification listener is not ready.
	ErrUnavailable = errors.New("realtime: listener unavailable")
)

// Config contains hub admission limits. Zero values select the documented defaults.
type Config struct {
	// MaxConnections is the task-wide stream limit.
	MaxConnections int
	// MaxPerIP is the stream limit for one canonical client IP.
	MaxPerIP int
	// MaxPerAccount is the stream limit for one account.
	MaxPerAccount int
	// QueueDepth is the per-stream metadata queue capacity.
	QueueDepth int
	// AdmitFD reports whether the process has sufficient file descriptor headroom.
	AdmitFD func() bool
}

// Scope identifies either an account-owned or public resume stream.
type Scope struct {
	AccountID uuid.UUID
	ResumeID  uuid.UUID
	IP        string
}

// Change is the metadata invalidation delivered to matching subscriptions.
type Change struct {
	AccountID uuid.UUID `json:"account_id"`
	ResumeID  uuid.UUID `json:"resume_id"`
	Revision  int64     `json:"revision"`
	Deleted   bool      `json:"deleted"`
}

// Hub coordinates bounded local realtime subscriptions.
type Hub struct {
	mu                            sync.Mutex
	max, maxIP, maxAccount, depth int
	fd                            func() bool
	available, closed             bool
	next                          uint64
	subs                          map[uint64]*Subscription
	ip                            map[string]int
	accounts                      map[uuid.UUID]int
}

// Subscription is a bounded stream of matching metadata changes.
type Subscription struct {
	// Events receives metadata changes until the subscription closes.
	Events <-chan Change
	// Done closes when the subscription is closed or evicted.
	Done   <-chan struct{}
	h      *Hub
	id     uint64
	events chan Change
	done   chan struct{}
	once   sync.Once
	scope  Scope
}

// NewHub creates an unavailable, bounded local realtime hub.
func NewHub(c Config) (*Hub, error) {
	limits := []struct {
		name string
		p    *int
		d    int
	}{{"MaxConnections", &c.MaxConnections, defaultMaxConnections}, {"MaxPerIP", &c.MaxPerIP, defaultMaxPerIP}, {"MaxPerAccount", &c.MaxPerAccount, defaultMaxPerAccount}, {"QueueDepth", &c.QueueDepth, defaultQueueDepth}}
	for _, x := range limits {
		if *x.p == 0 {
			*x.p = x.d
		}
		if *x.p < 0 || *x.p > x.d {
			return nil, fmt.Errorf("realtime: %s out of range", x.name)
		}
	}
	if c.AdmitFD == nil {
		c.AdmitFD = processFDOK
	}
	return &Hub{max: c.MaxConnections, maxIP: c.MaxPerIP, maxAccount: c.MaxPerAccount, depth: c.QueueDepth, fd: c.AdmitFD, subs: make(map[uint64]*Subscription), ip: make(map[string]int), accounts: make(map[uuid.UUID]int)}, nil
}

// SetAvailable changes listener readiness. Becoming unavailable closes streams.
func (h *Hub) SetAvailable(v bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.available = v
	if !v {
		h.closeAllLocked()
	}
}

// Subscribe admits a stream for the supplied account or resume scope.
func (h *Hub) Subscribe(scope Scope) (*Subscription, error) {
	ip, err := canonicalIP(scope.IP)
	if err != nil {
		return nil, err
	}
	if (scope.AccountID == uuid.Nil) == (scope.ResumeID == uuid.Nil) {
		return nil, errors.New("realtime: scope must contain exactly one identity")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || !h.available {
		return nil, ErrUnavailable
	}
	if !h.fd() || len(h.subs) >= h.max || h.ip[ip] >= h.maxIP || (scope.AccountID != uuid.Nil && h.accounts[scope.AccountID] >= h.maxAccount) {
		return nil, ErrLimited
	}
	h.next++
	events := make(chan Change, h.depth)
	done := make(chan struct{})
	s := &Subscription{h: h, id: h.next, events: events, done: done, Events: events, Done: done, scope: Scope{AccountID: scope.AccountID, ResumeID: scope.ResumeID, IP: ip}}
	h.subs[s.id] = s
	h.ip[ip]++
	if scope.AccountID != uuid.Nil {
		h.accounts[scope.AccountID]++
	}
	return s, nil
}

// Close idempotently closes the subscription and reclaims its admission keys.
func (s *Subscription) Close() {
	s.once.Do(func() { s.h.mu.Lock(); defer s.h.mu.Unlock(); s.h.removeLocked(s) })
}

// Publish queues a metadata change for every matching stream.
func (h *Hub) Publish(c Change) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || !h.available {
		return
	}
	for _, s := range h.subs {
		if !matches(s.scope, c) {
			continue
		}
		if s.scope.ResumeID != uuid.Nil && c.Deleted {
			h.removeLocked(s)
			continue
		}
		select {
		case s.events <- c:
		default:
			h.removeLocked(s)
		}
	}
}

func matches(s Scope, c Change) bool {
	if s.AccountID != uuid.Nil {
		return s.AccountID == c.AccountID
	}
	return s.ResumeID == c.ResumeID
}

// Close permanently stops admission and closes every subscription.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	h.available = false
	h.closeAllLocked()
}
func (h *Hub) closeAllLocked() {
	for _, s := range h.subs {
		h.removeLocked(s)
	}
}
func (h *Hub) removeLocked(s *Subscription) {
	if _, ok := h.subs[s.id]; !ok {
		return
	}
	delete(h.subs, s.id)
	h.ip[s.scope.IP]--
	if h.ip[s.scope.IP] == 0 {
		delete(h.ip, s.scope.IP)
	}
	if s.scope.AccountID != uuid.Nil {
		h.accounts[s.scope.AccountID]--
		if h.accounts[s.scope.AccountID] == 0 {
			delete(h.accounts, s.scope.AccountID)
		}
	}
	close(s.done)
	close(s.events)
}

func canonicalIP(v string) (string, error) {
	p := net.ParseIP(v)
	if p == nil || p.String() != v {
		return "", errors.New("realtime: invalid canonical IP")
	}
	return v, nil
}

func processFDOK() bool {
	if runtime.GOOS == "linux" {
		entries, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			return false
		}
		var lim syscall.Rlimit
		if syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim) != nil || lim.Cur == 0 {
			return false
		}
		return fdAdmission(uint64(len(entries)), lim.Cur)
	}
	return false
}

func fdAdmission(used, limit uint64) bool {
	if limit == 0 || used > limit {
		return false
	}
	quarter := limit / 4
	if limit%4 != 0 {
		quarter++
	}
	return used <= limit-quarter
}
