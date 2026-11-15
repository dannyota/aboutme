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

type accountState struct {
	session *liveSession
	pending int
	endings uint64
}

type ticket struct {
	account uuid.UUID
	endings uint64
}

type registry struct {
	mu       sync.Mutex
	closed   bool
	accounts map[uuid.UUID]*accountState
	inflight int
	idle     []chan struct{}
	ttl      time.Duration
	now      func() time.Time
	revoke   func(token string)
}

func newRegistry(ttl time.Duration, now func() time.Time, revoke func(token string)) *registry {
	return &registry{accounts: map[uuid.UUID]*accountState{}, ttl: ttl, now: now, revoke: revoke}
}

func (r *registry) reserve(account uuid.UUID) (ticket, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ticket{}, false
	}
	state := r.accounts[account]
	if state == nil {
		state = &accountState{}
		r.accounts[account] = state
	}
	state.pending++
	return ticket{account: account, endings: state.endings}, true
}

func (r *registry) abandon(t ticket, token *heldToken) {
	r.mu.Lock()
	var claimed []string
	if state := r.accounts[t.account]; state != nil {
		state.pending--
		r.pruneLocked(t.account, state)
	}
	claimed = r.claimLocked(token, claimed)
	r.mu.Unlock()
	r.spawn(claimed)
}

func (r *registry) install(t ticket, token *heldToken, epoch, generation int64) (time.Time, bool) {
	r.mu.Lock()
	state := r.accounts[t.account]
	if state == nil || r.closed || state.endings != t.endings {
		if state != nil {
			state.pending--
			r.pruneLocked(t.account, state)
		}
		claimed := r.claimLocked(token, nil)
		r.mu.Unlock()
		r.spawn(claimed)
		return time.Time{}, false
	}
	state.pending--
	var claimed []string
	if previous := state.session; previous != nil {
		previous.timer.Stop()
		claimed = r.claimLocked(previous.token, claimed)
	}
	session := &liveSession{token: token, epoch: epoch, generation: generation, expiresAt: r.now().Add(r.ttl)}
	account := t.account
	session.timer = time.AfterFunc(r.ttl, func() { r.expire(account, session) })
	state.session = session
	r.mu.Unlock()
	r.spawn(claimed)
	return session.expiresAt, true
}

func (r *registry) current(account uuid.UUID) (*liveSession, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.accounts[account]
	if state == nil || state.session == nil {
		return nil, false
	}
	return state.session, true
}

func (r *registry) end(account uuid.UUID) bool {
	r.mu.Lock()
	state := r.accounts[account]
	if state == nil {
		r.mu.Unlock()
		return false
	}
	state.endings++
	session := state.session
	state.session = nil
	var claimed []string
	if session != nil {
		session.timer.Stop()
		claimed = r.claimLocked(session.token, claimed)
	}
	r.pruneLocked(account, state)
	r.mu.Unlock()
	r.spawn(claimed)
	return session != nil
}

func (r *registry) endSession(account uuid.UUID, session *liveSession) {
	r.mu.Lock()
	state := r.accounts[account]
	if state == nil || state.session != session {
		r.mu.Unlock()
		return
	}
	state.endings++
	state.session = nil
	session.timer.Stop()
	claimed := r.claimLocked(session.token, nil)
	r.pruneLocked(account, state)
	r.mu.Unlock()
	r.spawn(claimed)
}

func (r *registry) expire(account uuid.UUID, session *liveSession) {
	r.mu.Lock()
	state := r.accounts[account]
	if state == nil || state.session != session {
		r.mu.Unlock()
		return
	}
	state.session = nil
	claimed := r.claimLocked(session.token, nil)
	r.pruneLocked(account, state)
	r.mu.Unlock()
	r.spawn(claimed)
}

func (r *registry) discard(token *heldToken) {
	r.mu.Lock()
	claimed := r.claimLocked(token, nil)
	r.mu.Unlock()
	r.spawn(claimed)
}

func (r *registry) close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	var claimed []string
	for account, state := range r.accounts {
		state.endings++
		if session := state.session; session != nil {
			session.timer.Stop()
			claimed = r.claimLocked(session.token, claimed)
			state.session = nil
		}
		r.pruneLocked(account, state)
	}
	r.mu.Unlock()
	r.spawn(claimed)
}

func (r *registry) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

func (r *registry) wait(ctx context.Context) error {
	r.mu.Lock()
	if r.inflight == 0 {
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
	if token == nil {
		return claimed
	}
	value, ok := token.claim()
	if !ok {
		return claimed
	}
	r.inflight++
	return append(claimed, value)
}

func (r *registry) pruneLocked(account uuid.UUID, state *accountState) {
	if state.session == nil && state.pending <= 0 {
		delete(r.accounts, account)
	}
}

func (r *registry) spawn(claimed []string) {
	for _, value := range claimed {
		go func() {
			r.revoke(value)
			r.mu.Lock()
			r.inflight--
			if r.inflight == 0 {
				for _, idle := range r.idle {
					close(idle)
				}
				r.idle = nil
			}
			r.mu.Unlock()
		}()
	}
}
