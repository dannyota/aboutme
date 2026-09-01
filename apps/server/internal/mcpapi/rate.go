package mcpapi

import (
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
)

// RateConfig carries the frozen per-token, per-user, and per-user concurrency
// budgets from process config.
type RateConfig struct {
	TokenRequests     int
	TokenWindow       time.Duration
	UserRequests      int
	UserWindow        time.Duration
	ConcurrentPerUser int
	MaxKeys           int
	Clock             func() time.Time
}

// RatePolicies owns process-wide MCP tool-call and concurrency admission.
type RatePolicies struct {
	clock func() time.Time
	token *api.BoundedRateLimiter
	user  *api.BoundedRateLimiter
	slots *requestSlots
}

// NewRatePolicies validates and creates one MCP admission policy set.
func NewRatePolicies(cfg RateConfig) (*RatePolicies, error) {
	if cfg.TokenRequests <= 0 || cfg.TokenWindow <= 0 || cfg.UserRequests <= 0 || cfg.UserWindow <= 0 ||
		cfg.ConcurrentPerUser <= 0 || cfg.MaxKeys <= 0 || cfg.Clock == nil {
		return nil, errors.New("mcp rate policies: invalid configuration")
	}
	return &RatePolicies{
		clock: cfg.Clock,
		token: api.NewBoundedRateLimiter(api.RateLimiterConfig{
			Requests: cfg.TokenRequests,
			Window:   cfg.TokenWindow,
			MaxKeys:  cfg.MaxKeys,
		}),
		user: api.NewBoundedRateLimiter(api.RateLimiterConfig{
			Requests: cfg.UserRequests,
			Window:   cfg.UserWindow,
			MaxKeys:  cfg.MaxKeys,
		}),
		slots: newRequestSlots(cfg.ConcurrentPerUser, cfg.MaxKeys),
	}, nil
}

// AdmitTool composes per-token and per-user admission. Keys contain only
// durable row IDs; bearer material never enters either store.
func (p *RatePolicies) AdmitTool(principal Principal) (bool, int) {
	now := p.clock()
	allowed, retry := p.token.Admit(now, "token:"+principal.TokenID.String())
	if !allowed {
		return false, retry
	}
	return p.user.Admit(now, "user:"+principal.UserID.String())
}

// AcquireRequest reserves one of the user's bounded in-flight request slots.
// The returned release is idempotent.
func (p *RatePolicies) AcquireRequest(userID uuid.UUID) (release func(), allowed bool) {
	return p.slots.acquire(userID)
}

// ServeRequest applies the per-user concurrency guard around one authenticated
// MCP request. defer guarantees release on normal return, panic unwinding, and
// handlers that return after request-context cancellation.
func (p *RatePolicies) ServeRequest(principal Principal, w http.ResponseWriter, r *http.Request, next http.Handler) {
	release, allowed := p.AcquireRequest(principal.UserID)
	if !allowed {
		w.Header().Set("Retry-After", "1")
		writeMCPError(w, errRateLimited)
		return
	}
	defer release()
	next.ServeHTTP(w, r)
}

func (p *RatePolicies) writeToolRateError(w http.ResponseWriter, retry int) {
	if retry < 1 {
		retry = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(retry))
	writeMCPError(w, errRateLimited)
}

type requestSlots struct {
	limit   int
	maxKeys int

	mu       sync.Mutex
	entries  map[uuid.UUID]int
	overflow int
}

func newRequestSlots(limit, maxKeys int) *requestSlots {
	return &requestSlots{limit: limit, maxKeys: maxKeys, entries: make(map[uuid.UUID]int)}
}

func (s *requestSlots) acquire(userID uuid.UUID) (func(), bool) {
	s.mu.Lock()
	if count, ok := s.entries[userID]; ok {
		if count >= s.limit {
			s.mu.Unlock()
			return nil, false
		}
		s.entries[userID] = count + 1
		s.mu.Unlock()
		return s.releaseTracked(userID), true
	}
	// While any overflow request remains active, every untracked user stays
	// on the shared overflow counter. This prevents one overflow user from
	// migrating into a newly freed private slot and obtaining two concurrent
	// budgets at once.
	if s.overflow > 0 || len(s.entries) >= s.maxKeys {
		if s.overflow >= s.limit {
			s.mu.Unlock()
			return nil, false
		}
		s.overflow++
		s.mu.Unlock()
		return s.releaseOverflow(), true
	}
	s.entries[userID] = 1
	s.mu.Unlock()
	return s.releaseTracked(userID), true
}

func (s *requestSlots) releaseTracked(userID uuid.UUID) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if count := s.entries[userID]; count <= 1 {
				delete(s.entries, userID)
			} else {
				s.entries[userID] = count - 1
			}
		})
	}
}

func (s *requestSlots) releaseOverflow() func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.overflow > 0 {
				s.overflow--
			}
		})
	}
}
