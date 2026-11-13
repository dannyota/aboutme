package testutil

import (
	"sync"
	"time"
)

var Epoch = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

type Clock struct {
	mu  sync.Mutex
	now time.Time
}

func NewClock(start time.Time) *Clock {
	return &Clock{now: start}
}

func NewClockAtEpoch() *Clock {
	return NewClock(Epoch)
}

func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *Clock) Advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	return c.now
}

func (c *Clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}
