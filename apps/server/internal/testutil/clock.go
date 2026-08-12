// Package testutil provides deterministic fixtures shared by server tests.
package testutil

import (
	"sync"
	"time"
)

// Epoch is the shared timestamp for fixtures that need no specific instant.
var Epoch = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// Clock is a concurrency-safe clock that tests can advance without sleeping.
type Clock struct {
	mu  sync.Mutex
	now time.Time
}

// NewClock returns a Clock starting at start.
func NewClock(start time.Time) *Clock {
	return &Clock{now: start}
}

// NewClockAtEpoch returns a Clock starting at Epoch.
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
