package auth

import (
	"math"
	"sync"
	"time"
)

const failureSweepCooldown = time.Second

type failureBudget struct {
	count       int
	windowStart time.Time
}

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

func (f *failureLimiter) ClearSuccess(emailKey [32]byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.entries, emailKey)
}

func (f *failureLimiter) evictExpiredLocked(now time.Time) {
	for k, b := range f.entries {
		if b.count == 0 || b.windowStart.IsZero() || now.Sub(b.windowStart) >= f.window {
			delete(f.entries, k)
		}
	}
}

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

func ceilSeconds(d time.Duration) int {
	secs := int(math.Ceil(d.Seconds()))
	if secs < 1 {
		secs = 1
	}
	return secs
}
