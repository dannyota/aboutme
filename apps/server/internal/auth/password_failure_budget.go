package auth

import (
	"math"
	"sync"
	"time"
)

// failureSweepCooldown bounds how often the failure store scans the entries
// map for expired keys once saturated: at most once per second, matching the
// admission store's amortized-sweep behavior.
const failureSweepCooldown = time.Second

// failureBudget is one key's fixed-window failure counter. A zero windowStart
// means no window is open.
type failureBudget struct {
	count       int
	windowStart time.Time
}

// failureLimiter is the PasswordFailureLimiter implementation: a bounded,
// fixed-window failure counter. The first RecordFailure opens a window of
// fixed length; later failures increment the count but never extend the
// window. When the window closes, the count is debt-free until the next
// RecordFailure opens a fresh window. The store is capped at maxKeys active
// entries plus one shared overflow bucket; expired entries are reclaimed on
// admission when saturated, active entries are never evicted, and a clock
// rollback fails closed (see failureClock).
type failureLimiter struct {
	limit   int
	window  time.Duration
	maxKeys int

	mu          sync.Mutex
	clock       failureClock
	entries     map[[32]byte]*failureBudget
	overflow    failureBudget
	nextSweepAt time.Time
}

func newFailureLimiter(limit int, window time.Duration, maxKeys int) *failureLimiter {
	return &failureLimiter{
		limit:   limit,
		window:  window,
		maxKeys: maxKeys,
		entries: make(map[[32]byte]*failureBudget),
	}
}

// State reports the current failure state for emailKey without mutating
// anything. A key with no recorded state (or a closed window) reports
// unexhausted. If the store is saturated, an untracked key would share the
// overflow bucket on its next RecordFailure, so State reflects that bucket.
func (f *failureLimiter) State(now time.Time, emailKey [32]byte) FailureState {
	f.mu.Lock()
	defer f.mu.Unlock()
	now = f.clock.clamp(now)

	b, ok := f.entries[emailKey]
	if ok {
		return f.stateLocked(b, now)
	}
	if len(f.entries) >= f.maxKeys {
		return f.stateLocked(&f.overflow, now)
	}
	return FailureState{}
}

func (f *failureLimiter) stateLocked(b *failureBudget, now time.Time) FailureState {
	if b.count == 0 || b.windowStart.IsZero() || b.count < f.limit {
		return FailureState{}
	}
	remaining := f.window - now.Sub(b.windowStart)
	if remaining <= 0 {
		return FailureState{}
	}
	return FailureState{Exhausted: true, RetryAfterSeconds: ceilSeconds(remaining)}
}

// RecordFailure atomically increments emailKey's bucket, opening a fresh fixed
// window if none is open or the previous one has closed, and reports whether
// the window is exhausted. Exhaustion starts at failure limit (the tenth) and
// never resets until the window closes; later failures within the same window
// keep reporting exhausted with the time remaining in the original window.
func (f *failureLimiter) RecordFailure(now time.Time, emailKey [32]byte) FailureState {
	f.mu.Lock()
	defer f.mu.Unlock()
	now = f.clock.clamp(now)

	b, ok := f.entries[emailKey]
	if !ok {
		if len(f.entries) >= f.maxKeys && !now.Before(f.nextSweepAt) {
			f.evictExpiredLocked(now)
			f.nextSweepAt = now.Add(failureSweepCooldown)
		}
		if len(f.entries) < f.maxKeys {
			b = &failureBudget{}
			f.entries[emailKey] = b
		} else {
			// The store is full of active entries: share the single overflow
			// bucket rather than evicting a protected key.
			return f.recordLocked(&f.overflow, now)
		}
	}
	return f.recordLocked(b, now)
}

func (f *failureLimiter) recordLocked(b *failureBudget, now time.Time) FailureState {
	if b.windowStart.IsZero() || now.Sub(b.windowStart) >= f.window {
		b.windowStart = now
		b.count = 0
	}
	b.count++
	if b.count >= f.limit {
		remaining := f.window - now.Sub(b.windowStart)
		if remaining < 0 {
			remaining = 0
		}
		return FailureState{Exhausted: true, RetryAfterSeconds: ceilSeconds(remaining)}
	}
	return FailureState{}
}

// ClearSuccess removes only emailKey's entry. An overflow key has no private
// entry, so clearing it is a no-op: the shared overflow bucket is never
// cleared for one key's success.
func (f *failureLimiter) ClearSuccess(emailKey [32]byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.entries, emailKey)
}

// evictExpiredLocked drops every entry whose fixed window has closed (or that
// has no open window), freeing its slot for a new key. f.mu must be held by
// the caller.
func (f *failureLimiter) evictExpiredLocked(now time.Time) {
	for k, b := range f.entries {
		if b.count == 0 || b.windowStart.IsZero() || now.Sub(b.windowStart) >= f.window {
			delete(f.entries, k)
		}
	}
}

// failureClock turns a possibly-backward wall clock into a per-instance
// non-decreasing one, so a backward step cannot make an open failure window
// look expired (which would let a rollback clear an attacker's debt) or a
// closed one look fresh.
type failureClock struct {
	highWater time.Time
}

func (c *failureClock) clamp(now time.Time) time.Time {
	if now.Before(c.highWater) {
		return c.highWater
	}
	c.highWater = now
	return now
}

// ceilSeconds converts a duration to a whole-second retry count, always at
// least 1 so an exhausted caller is never told to retry immediately.
func ceilSeconds(d time.Duration) int {
	secs := int(math.Ceil(d.Seconds()))
	if secs < 1 {
		secs = 1
	}
	return secs
}
