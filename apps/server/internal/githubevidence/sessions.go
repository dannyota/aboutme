package githubevidence

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

type heldToken struct {
	mu      sync.Mutex
	value   string
	claimed bool
}

func newHeldToken(value string) *heldToken {
	return &heldToken{value: value}
}

func (t *heldToken) peek() (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.claimed {
		return "", false
	}
	return t.value, true
}

func (t *heldToken) claim() (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.claimed {
		return "", false
	}
	value := t.value
	t.value, t.claimed = "", true
	return value, true
}

type liveSession struct {
	token      *heldToken
	epoch      int64
	generation int64
	expiresAt  time.Time
	timer      *time.Timer
}

type issue struct {
	cancel context.CancelFunc
}

type registry struct {
	mu       sync.Mutex
	closed   bool
	closedAt time.Time
	sessions map[uuid.UUID]*liveSession
	pending  map[*heldToken]struct{}
	issuing  map[*issue]struct{}
	inflight int
	idle     []chan struct{}
	ttl      time.Duration
	revoke   func(token string)
	observe  func()
}

func newRegistry(ttl time.Duration, revoke func(token string)) *registry {
	return &registry{
		sessions: map[uuid.UUID]*liveSession{},
		pending:  map[*heldToken]struct{}{},
		issuing:  map[*issue]struct{}{},
		ttl:      ttl,
		revoke:   revoke,
	}
}

func (r *registry) beginIssue(ctx context.Context) (context.Context, func(), bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, nil, false
	}
	issueCtx, cancel := context.WithCancel(ctx)
	entry := &issue{cancel: cancel}
	r.issuing[entry] = struct{}{}
	return issueCtx, func() { r.endIssue(entry) }, true
}

func (r *registry) endIssue(entry *issue) {
	r.mu.Lock()
	delete(r.issuing, entry)
	entry.cancel()
	r.signalIdleLocked()
	r.mu.Unlock()
}

func (r *registry) hold(value string) *heldToken {
	token := newHeldToken(value)
	r.mu.Lock()
	var claimed []string
	if r.closed {
		claimed = r.claimLocked(token, claimed)
	} else {
		r.pending[token] = struct{}{}
	}
	r.mu.Unlock()
	r.spawn(claimed)
	return token
}

func (r *registry) release(token *heldToken) {
	if token == nil {
		return
	}
	r.mu.Lock()
	delete(r.pending, token)
	claimed := r.claimLocked(token, nil)
	r.mu.Unlock()
	r.spawn(claimed)
}

func (r *registry) install(account uuid.UUID, token *heldToken, epoch, generation int64, now time.Time) (*liveSession, bool) {
	r.observed()
	r.mu.Lock()
	if _, pending := r.pending[token]; r.closed || !pending {
		r.mu.Unlock()
		return nil, false
	}
	delete(r.pending, token)
	var claimed []string
	if previous := r.sessions[account]; previous != nil {
		previous.timer.Stop()
		claimed = r.claimLocked(previous.token, claimed)
	}
	session := &liveSession{token: token, epoch: epoch, generation: generation, expiresAt: now.Add(r.ttl)}
	session.timer = time.AfterFunc(r.ttl, func() { r.endSession(account, session) })
	r.sessions[account] = session
	r.mu.Unlock()
	r.spawn(claimed)
	return session, true
}

func (r *registry) current(account uuid.UUID) (*liveSession, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	session := r.sessions[account]
	return session, session != nil
}

func (r *registry) end(account uuid.UUID) bool {
	r.observed()
	r.mu.Lock()
	session := r.sessions[account]
	if session == nil {
		r.mu.Unlock()
		return false
	}
	delete(r.sessions, account)
	session.timer.Stop()
	claimed := r.claimLocked(session.token, nil)
	r.mu.Unlock()
	r.spawn(claimed)
	return true
}

func (r *registry) endSession(account uuid.UUID, session *liveSession) {
	r.observed()
	r.mu.Lock()
	if r.sessions[account] != session {
		r.mu.Unlock()
		return
	}
	delete(r.sessions, account)
	session.timer.Stop()
	claimed := r.claimLocked(session.token, nil)
	r.mu.Unlock()
	r.spawn(claimed)
}

func (r *registry) observed() {
	if r.observe != nil {
		r.observe()
	}
}

func (r *registry) close() time.Time {
	r.mu.Lock()
	if r.closed {
		closedAt := r.closedAt
		r.mu.Unlock()
		return closedAt
	}
	r.closed, r.closedAt = true, time.Now()
	for entry := range r.issuing {
		entry.cancel()
	}
	closedAt := r.closedAt
	var claimed []string
	for account, session := range r.sessions {
		session.timer.Stop()
		claimed = r.claimLocked(session.token, claimed)
		delete(r.sessions, account)
	}
	for token := range r.pending {
		claimed = r.claimLocked(token, claimed)
		delete(r.pending, token)
	}
	r.mu.Unlock()
	r.spawn(claimed)
	return closedAt
}

func (r *registry) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

func (r *registry) wait(ctx context.Context) error {
	r.mu.Lock()
	if r.inflight == 0 && len(r.issuing) == 0 {
		r.mu.Unlock()
		return nil
	}
	idle := make(chan struct{})
	r.idle = append(r.idle, idle)
	r.mu.Unlock()
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *registry) claimLocked(token *heldToken, claimed []string) []string {
	value, ok := token.claim()
	if !ok {
		return claimed
	}
	r.inflight++
	return append(claimed, value)
}

func (r *registry) signalIdleLocked() {
	if r.inflight != 0 || len(r.issuing) != 0 {
		return
	}
	for _, idle := range r.idle {
		close(idle)
	}
	r.idle = nil
}

func (r *registry) spawn(claimed []string) {
	for _, value := range claimed {
		go func() {
			r.revoke(value)
			r.mu.Lock()
			r.inflight--
			r.signalIdleLocked()
			r.mu.Unlock()
		}()
	}
}
