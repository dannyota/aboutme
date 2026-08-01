// Package testutil provides deterministic test fixtures shared across
// apps/server's test suites: a fake clock and seeded ID generation now,
// with domain-specific factories (users, resumes, sessions, ...) added
// here as later phases introduce those packages. Every helper in this
// package is deterministic on purpose — no time.Now(), no unseeded rand,
// no uuid.New() — so the same test produces the same values on every run,
// on every machine, and in the UAT agent's environment too (design spec §0
// "Determinism (mandatory for agent-run tests)").
package testutil

import (
	"sync"
	"time"
)

// Epoch is the fixed instant fixtures anchor to when a test needs "a
// timestamp" but not any particular one. Using one shared constant, rather
// than each test picking its own time.Date literal, makes fixture
// timestamps easy to recognize in test failures and diffs.
var Epoch = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// Clock is a settable, injectable clock for tests: it stands in for
// time.Now so time-dependent code (e.g. api.RateLimit's token-bucket
// refill) can be driven forward deterministically with Advance, instead of
// the test sleeping and hoping the scheduler cooperates. It is safe for
// concurrent use.
type Clock struct {
	mu  sync.Mutex
	now time.Time
}

// NewClock returns a Clock starting at start.
func NewClock(start time.Time) *Clock {
	return &Clock{now: start}
}

// NewClockAtEpoch returns a Clock starting at Epoch, for tests that need a
// deterministic starting instant but don't care which one.
func NewClockAtEpoch() *Clock {
	return NewClock(Epoch)
}

// Now returns the clock's current time. It has the signature of time.Now,
// so a *Clock's Now method value can be passed anywhere a func() time.Time
// is expected — e.g. api.RateLimiterConfig.Clock.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward by d (d may be negative to move it
// back), and returns the new current time.
func (c *Clock) Advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	return c.now
}

// Set moves the clock directly to t.
func (c *Clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}
