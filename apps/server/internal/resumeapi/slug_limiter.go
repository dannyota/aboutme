package resumeapi

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	slugAttemptLimit       = 30
	slugAttemptWindow      = time.Hour
	defaultSlugAttemptKeys = 10_000
)

type slugAttemptLimiter interface {
	AllowChangedSlug(accountID uuid.UUID, now time.Time) bool
}

type slugAttemptBucket struct {
	attempts     [slugAttemptLimit]time.Time
	start        int
	count        int
	lastActivity time.Time
	lastNow      time.Time
}

type memorySlugAttemptLimiter struct {
	mu       sync.Mutex
	maxKeys  int
	buckets  map[uuid.UUID]*slugAttemptBucket
	overflow slugAttemptBucket
}

// newSlugAttemptLimiter creates the per-account, rolling-hour limiter. The
// optional cap exists for deterministic bounded-state tests; production uses
// the ADR 0018 maximum.
func newSlugAttemptLimiter(maxKeys ...int) *memorySlugAttemptLimiter {
	cap := defaultSlugAttemptKeys
	if len(maxKeys) == 1 && maxKeys[0] > 0 {
		cap = maxKeys[0]
	}
	return &memorySlugAttemptLimiter{maxKeys: cap, buckets: make(map[uuid.UUID]*slugAttemptBucket)}
}

func (l *memorySlugAttemptLimiter) AllowChangedSlug(accountID uuid.UUID, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.removeExpiredLocked(now)
	if bucket, ok := l.buckets[accountID]; ok {
		return bucket.allow(now)
	}
	if len(l.buckets) < l.maxKeys {
		bucket := &slugAttemptBucket{}
		l.buckets[accountID] = bucket
		return bucket.allow(now)
	}
	return l.overflow.allow(now)
}

func (l *memorySlugAttemptLimiter) trackedBucketCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

func (l *memorySlugAttemptLimiter) removeExpiredLocked(now time.Time) {
	for accountID, bucket := range l.buckets {
		bucket.prune(now)
		if bucket.count == 0 || (!bucket.lastActivity.IsZero() && now.Sub(bucket.lastActivity) >= 24*time.Hour) {
			delete(l.buckets, accountID)
		}
	}
	l.overflow.prune(now)
}

func (b *slugAttemptBucket) allow(now time.Time) bool {
	if now.Before(b.lastNow) {
		now = b.lastNow
	}
	b.prune(now)
	b.lastNow = now
	b.lastActivity = now
	if b.count < slugAttemptLimit {
		b.attempts[(b.start+b.count)%slugAttemptLimit] = now
		b.count++
		return true
	}
	// Keep the newest thirty attempts. Replacing the oldest entry makes denied
	// attempts part of the rolling window without an attacker-growing queue.
	b.attempts[b.start] = now
	b.start = (b.start + 1) % slugAttemptLimit
	return false
}

func (b *slugAttemptBucket) prune(now time.Time) {
	if now.Before(b.lastNow) {
		now = b.lastNow
	}
	cutoff := now.Add(-slugAttemptWindow)
	for b.count > 0 && !b.attempts[b.start].After(cutoff) {
		b.start = (b.start + 1) % slugAttemptLimit
		b.count--
	}
}
