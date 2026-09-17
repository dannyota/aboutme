package api

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// evictionSweepCooldown bounds how often allow scans the whole entries map
// for expired keys once the store is saturated: at most once per this
// interval of cfg.Clock's time, never once per request. Without this, a
// sustained flood of distinct new keys against a full store would pay an
// O(MaxKeys) map scan, holding l.mu, on every single request — a
// denial-of-service vector in its own right.
// One second is frequent enough to reclaim capacity for genuine key
// turnover promptly, while bounding the added scan cost to a small,
// constant amortized overhead regardless of request rate.
const evictionSweepCooldown = time.Second

// rateLimiterIdleExpiry is the hard bound on a tracked key that has made no
// request. It applies even when an unusually slow bucket has not fully
// refilled. Allowed and rejected requests both refresh the activity time.
const rateLimiterIdleExpiry = 24 * time.Hour

// rateLimiter holds the per-key token-bucket state behind RateLimit, built
// on golang.org/x/time/rate rather than a hand-rolled bucket: its
// AllowN/TokensAt family already takes an explicit time.Time instead of
// reading the wall clock itself, which is exactly the injectable-clock
// shape this package's tests need, and its accounting is re-derived from
// first principles on every call rather than accumulated incrementally, so
// a clock that briefly moves backward (an NTP step, a VM migration pause)
// re-anchors cleanly instead of latching the bucket at a stale high-water
// mark and refusing to refill again until real time catches back up past
// it — the failure mode a hand-rolled "only refill when elapsed > 0, else
// leave lastSeen untouched" implementation has. See clampedLimiter for the
// one gap x/time/rate itself does not close (an admitted request observed
// at a rolled-back time can still re-anchor the bucket backward).
//
// Memory is bounded at cfg.MaxKeys entries. Once full, admitting a new key
// first tries to reclaim space from entries that are expired — fully
// refilled, i.e. carrying no state distinguishable from a brand-new key
// (see evictExpiredLocked) — and only if none are found does the new key
// fall back to sharing overflow, a single limiter common to every key that
// arrives while the store is saturated with genuinely active entries.
// Deliberately not evicting an arbitrary active entry is the point: doing
// so would hand an attacker a fresh bucket for the price of one more
// distinct (possibly forged) key, defeating the limit under sustained key
// churn. Sharing one overflow bucket instead caps the aggregate throughput
// every such key can extract, however many distinct keys are involved, at
// exactly one key's normal budget.
//
// allow performs lookup/admission, the expiry decision, and the resulting
// limiter's first token check as ONE operation under l.mu — never
// releasing the lock between "this key now has an entry" and "that entry's
// first token is spent." Otherwise a concurrent admission could evict a
// newly inserted, still-full entry before its first token is consumed and
// defeat the shared-overflow bound.
type rateLimiter struct {
	cfg   RateLimiterConfig
	limit rate.Limit // events per second, derived once from cfg
	burst int        // == cfg.Requests

	mu          sync.Mutex
	clock       monotonicClamp // guarded by mu; prevents a rollback from skipping a due sweep
	entries     map[string]*rateLimiterEntry
	overflow    *clampedLimiter
	nextSweepAt time.Time // zero value: the first saturated request always sweeps
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

// BoundedRateLimiter is an exported, key-bounded admission store built on the
// same ADR 0018 store that RateLimit's middleware uses: at most cfg.MaxKeys
// active keys plus one shared overflow bucket, expired-key reclamation, and
// monotonic-clock fail-closed behavior. It exists so a non-HTTP caller (the
// password rate policies in internal/auth) can enforce a per-key admission
// budget by calling Admit directly instead of going through HTTP middleware.
//
// The caller supplies the wall-clock instant explicitly rather than through a
// Clock callback, so the same instance can be driven deterministically in
// tests and by a caller that already holds a validated timestamp. The store
// still clamps internally so a backward clock step cannot expire an active
// entry early.
type BoundedRateLimiter struct {
	inner *rateLimiter
}

// NewBoundedRateLimiter returns a bounded admission store for cfg. Requests,
// Window, and MaxKeys come from cfg (defaults from RateLimiterConfig apply);
// the Clock/Key/Logger fields are irrelevant to direct Admit calls.
func NewBoundedRateLimiter(cfg RateLimiterConfig) *BoundedRateLimiter {
	cfg = cfg.withDefaults()
	return &BoundedRateLimiter{inner: newRateLimiter(cfg)}
}

// Admit reports whether key may be admitted at time now and, if not, how many
// whole seconds the caller should wait before retrying (always at least 1). An
// allowed admission consumes one token; a rejected admission consumes no state
// and leaves no debt (see admitNow). The returned seconds are already
// ceiling-rounded, so a caller can set Retry-After directly.
func (b *BoundedRateLimiter) Admit(now time.Time, key string) (allowed bool, retrySeconds int) {
	allowed, wait := b.inner.allow(key, now)
	if allowed {
		return true, 0
	}
	return false, retryAfterSeconds(wait)
}

// allow reports whether the request for key is allowed at time now, and if
// not, how long the caller should wait before retrying. See the rateLimiter
// doc comment for why lookup, the expiry decision, and the first admission
// check below all happen under one continuous hold of l.mu.
func (l *rateLimiter) allow(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Clamp to a monotonic high-water mark before the expiry decision below,
	// so a backward clock step can't push now below nextSweepAt and suppress
	// a due eviction sweep for the whole rollback span.
	// Each per-key clampedLimiter still clamps independently; this only makes
	// the store-level sweep clock non-decreasing too.
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
			// The store is full of entries that are not expired (see
			// evictExpiredLocked): key shares the overflow limiter rather
			// than evicting one of them or being admitted unbounded.
			// admitNow below runs on this same shared limiter while l.mu
			// is still held, exactly like a per-key entry's own first
			// admission — no separate code path, no separate gap.
			return admitNow(l.overflow, now)
		}
	}

	// Activity, not admission, owns idle expiry. A rejected request proves the
	// key is still active and must not let it age into a fresh private bucket.
	entry.lastRequest = now
	return admitNow(entry.limiter, now)
}

// evictExpiredLocked drops every entry whose bucket is fully refilled at
// now — holding no state a fresh key wouldn't also start with — so the
// store can admit a new key without discarding any entry that is still
// mid-window for a real, currently-active client. l.mu must be held by the
// caller.
func (l *rateLimiter) evictExpiredLocked(now time.Time) {
	for k, entry := range l.entries {
		if entry.limiter.TokensAt(now) >= float64(l.burst) ||
			now.Sub(entry.lastRequest) >= rateLimiterIdleExpiry {
			delete(l.entries, k)
		}
	}
}

// admitNow checks whether lim has a token available at time now and, if
// so, consumes it: true means the token was taken; false means it wasn't
// (and the returned duration is how long until it would be available).
//
// Built on AllowN rather than a ReserveN+CancelAt pair:
// x/time/rate's ReserveN always advances the limiter's internal
// bookkeeping (lastEvent in particular), even for a reservation that will
// be canceled immediately after, and CancelAt cannot always fully reverse
// that — if a second reservation is made before the first is canceled,
// CancelAt's restoration is partial or a no-op. Concurrent rejected
// requests could therefore leave the bucket owing a token it never
// actually spent, extending denial for the next legitimate request beyond
// one real Window. AllowN(now, 1) mutates the limiter's state ONLY on the
// admitted path (see x/time/rate's reserveN with maxFutureReserve=0: the
// state-updating branch is skipped entirely whenever it would return
// false) — there is no reservation to cancel because none is ever made on
// the denied path, so a rejected request can never leave debt for any
// interleaving, not just the ones a test happens to construct.
func admitNow(lim *clampedLimiter, now time.Time) (bool, time.Duration) {
	if lim.AllowN(now, 1) {
		return true, 0
	}
	// AllowN just declined without mutating lim (see above), so this reads
	// pure, undisturbed-by-this-call state: how many tokens are missing
	// divided by the refill rate is exactly how long until the next one is
	// due.
	limit := lim.Limit()
	if limit <= 0 {
		return false, time.Second
	}
	deficit := 1 - lim.TokensAt(now)
	if deficit <= 0 {
		// AllowN and TokensAt agree the limiter's own mutex serializes
		// these two calls against each other, so this should be
		// unreachable; kept as a defensive, always-a-full-second fallback
		// rather than a zero or negative Retry-After.
		return false, time.Second
	}
	return false, time.Duration(deficit / float64(limit) * float64(time.Second))
}

// monotonicClamp turns a possibly-backward wall clock into a per-instance
// non-decreasing one: clamp(now) never returns a time earlier than the
// latest it has already returned. A backward step (an NTP correction, a VM
// migration pause, or a test's injected rollback) therefore cannot make a
// time-throttled decision behave as if less time had elapsed than really
// did — which would otherwise suppress that decision for the whole rollback
// span. Three places reuse it: clampedLimiter (so x/time/rate never
// re-anchors a bucket backward), rateLimiter.allow (so a rollback can't skip
// a due eviction sweep), and trustMismatchWarner (so a
// rollback can't silence the trust-boundary warning).
//
// It carries no lock of its own; each user guards its monotonicClamp with
// that user's existing mutex.
type monotonicClamp struct {
	highWater time.Time // guarded by the owner's mutex; never accessed elsewhere
}

// clamp returns now, or the high-water mark if now precedes it, and advances
// the high-water mark to whichever it returns.
func (m *monotonicClamp) clamp(now time.Time) time.Time {
	if now.Before(m.highWater) {
		return m.highWater
	}
	m.highWater = now
	return now
}

// clampedLimiter wraps a rate.Limiter so every call it services observes a
// non-decreasing time, even when the caller's own clock briefly moves
// backward (an NTP step, a VM migration pause, or a test's injected
// rollback). This closes a gap AllowN's non-mutating denial (see admitNow)
// does not: an ADMITTED request observed at a rolled-back time still
// mutates x/time/rate's internal lim.last to that smaller value (its
// reserveN sets lim.last = t, the caller's raw, un-clamped time, whenever
// it grants the request), which corrupts every later call's elapsed-time
// math until real time catches back up past the rollback point. AllowN alone
// does not prevent this. Clamping here, one layer above x/time/rate, means
// x/time/rate itself never
// observes a time smaller than the largest one it has already seen for
// this specific bucket.
type clampedLimiter struct {
	lim   *rate.Limiter
	clock monotonicClamp // guarded by the owning rateLimiter's mu; never accessed elsewhere
}

func newClampedLimiter(limit rate.Limit, burst int) *clampedLimiter {
	return &clampedLimiter{lim: rate.NewLimiter(limit, burst)}
}

// at returns now clamped to never move backward relative to the latest
// time this bucket has already observed.
func (c *clampedLimiter) at(now time.Time) time.Time {
	return c.clock.clamp(now)
}

// AllowN reports whether n events may happen at (clamped) time now,
// consuming them if so. See rate.Limiter.AllowN.
func (c *clampedLimiter) AllowN(now time.Time, n int) bool {
	return c.lim.AllowN(c.at(now), n)
}

// TokensAt returns the number of tokens available at (clamped) time now,
// without mutating the limiter. See rate.Limiter.TokensAt.
func (c *clampedLimiter) TokensAt(now time.Time) float64 {
	return c.lim.TokensAt(c.at(now))
}

// Limit returns the limiter's configured events-per-second rate. See
// rate.Limiter.Limit.
func (c *clampedLimiter) Limit() rate.Limit {
	return c.lim.Limit()
}
