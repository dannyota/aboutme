package api

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const evictionSweepCooldown = time.Second

const rateLimiterIdleExpiry = 24 * time.Hour

type rateLimiter struct {
	cfg   RateLimiterConfig
	limit rate.Limit
	burst int

	mu          sync.Mutex
	clock       monotonicClamp
	entries     map[string]*rateLimiterEntry
	overflow    *clampedLimiter
	nextSweepAt time.Time
}

type rateLimiterEntry struct {
	limiter     *clampedLimiter
	lastRequest time.Time
}

func newRateLimiter(cfg RateLimiterConfig) *rateLimiter {
	limit := rate.Limit(float64(cfg.Requests) / cfg.Window.Seconds())
	return &rateLimiter{
		cfg:      cfg,
		limit:    limit,
		burst:    cfg.Requests,
		entries:  make(map[string]*rateLimiterEntry),
		overflow: newClampedLimiter(limit, cfg.Requests),
	}
}

type BoundedRateLimiter struct {
	inner *rateLimiter
}

func NewBoundedRateLimiter(cfg RateLimiterConfig) *BoundedRateLimiter {
	cfg = cfg.withDefaults()
	return &BoundedRateLimiter{inner: newRateLimiter(cfg)}
}

func (b *BoundedRateLimiter) Admit(now time.Time, key string) (allowed bool, retrySeconds int) {
	allowed, wait := b.inner.allow(key, now)
	if allowed {
		return true, 0
	}
	return false, retryAfterSeconds(wait)
}

func (b *BoundedRateLimiter) Check(now time.Time, key string) (allowed bool, retrySeconds int) {
	allowed, wait := b.inner.decide(key, now, false)
	if allowed {
		return true, 0
	}
	return false, retryAfterSeconds(wait)
}

func (l *rateLimiter) allow(key string, now time.Time) (bool, time.Duration) {
	return l.decide(key, now, true)
}

func (l *rateLimiter) decide(key string, now time.Time, consume bool) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now = l.clock.clamp(now)

	entry, ok := l.entries[key]
	if !ok {
		if len(l.entries) >= l.cfg.MaxKeys && !now.Before(l.nextSweepAt) {
			l.evictExpiredLocked(now)
			l.nextSweepAt = now.Add(evictionSweepCooldown)
		}
		if len(l.entries) < l.cfg.MaxKeys {
			entry = &rateLimiterEntry{
				limiter:     newClampedLimiter(l.limit, l.burst),
				lastRequest: now,
			}
			l.entries[key] = entry
		} else {
			return decideNow(l.overflow, now, consume)
		}
	}

	entry.lastRequest = now
	return decideNow(entry.limiter, now, consume)
}

func decideNow(lim *clampedLimiter, now time.Time, consume bool) (bool, time.Duration) {
	if consume {
		return admitNow(lim, now)
	}
	if lim.TokensAt(now) >= 1 {
		return true, 0
	}
	return false, tokenWait(lim, now)
}

func (l *rateLimiter) evictExpiredLocked(now time.Time) {
	for k, entry := range l.entries {
		if entry.limiter.TokensAt(now) >= float64(l.burst) ||
			now.Sub(entry.lastRequest) >= rateLimiterIdleExpiry {
			delete(l.entries, k)
		}
	}
}

func admitNow(lim *clampedLimiter, now time.Time) (bool, time.Duration) {
	if lim.AllowN(now, 1) {
		return true, 0
	}
	return false, tokenWait(lim, now)
}

func tokenWait(lim *clampedLimiter, now time.Time) time.Duration {
	limit := lim.Limit()
	if limit <= 0 {
		return time.Second
	}
	deficit := 1 - lim.TokensAt(now)
	if deficit <= 0 {
		return time.Second
	}
	return time.Duration(deficit / float64(limit) * float64(time.Second))
}

type monotonicClamp struct {
	highWater time.Time
}

func (m *monotonicClamp) clamp(now time.Time) time.Time {
	if now.Before(m.highWater) {
		return m.highWater
	}
	m.highWater = now
	return now
}

type clampedLimiter struct {
	lim   *rate.Limiter
	clock monotonicClamp
}

func newClampedLimiter(limit rate.Limit, burst int) *clampedLimiter {
	return &clampedLimiter{lim: rate.NewLimiter(limit, burst)}
}

func (c *clampedLimiter) at(now time.Time) time.Time {
	return c.clock.clamp(now)
}

func (c *clampedLimiter) AllowN(now time.Time, n int) bool {
	return c.lim.AllowN(c.at(now), n)
}

func (c *clampedLimiter) TokensAt(now time.Time) float64 {
	return c.lim.TokensAt(c.at(now))
}

func (c *clampedLimiter) Limit() rate.Limit {
	return c.lim.Limit()
}
