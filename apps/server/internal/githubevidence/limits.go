package githubevidence

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

type windowLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	events  map[uuid.UUID][]time.Time
	sweepAt time.Time
}

func newWindowLimiter(limit int, window time.Duration) *windowLimiter {
	return &windowLimiter{limit: limit, window: window, events: map[uuid.UUID][]time.Time{}}
}

func (l *windowLimiter) admit(key uuid.UUID, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-l.window)
	if !now.Before(l.sweepAt) {
		for account, events := range l.events {
			if len(events) == 0 || !events[len(events)-1].After(cutoff) {
				delete(l.events, account)
			}
		}
		l.sweepAt = now.Add(l.window)
	}
	kept := l.events[key][:0]
	for _, at := range l.events[key] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	if len(kept) >= l.limit {
		l.events[key] = kept
		return false, kept[0].Sub(cutoff)
	}
	l.events[key] = append(kept, now)
	return true, 0
}
