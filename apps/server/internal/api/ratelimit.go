package api

import (
	"context"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Default values used when the corresponding RateLimiterConfig field is
// zero.
const (
	// DefaultRateLimitRequests is the whole-server request budget RateLimit
	// enforces with a zero-value RateLimiterConfig. It permits normal
	// interactive use while setting a ceiling on scripted abuse. Route groups
	// layer stricter endpoint-specific limits (auth, slug claims, render) by
	// wrapping just those routes in their own
	// RateLimit(RateLimiterConfig{...}) call.
	DefaultRateLimitRequests = 300
	// DefaultRateLimitWindow is the interval DefaultRateLimitRequests
	// applies over.
	DefaultRateLimitWindow = time.Minute
	// DefaultRateLimitMaxKeys bounds the limiter's memory: at most this
	// many distinct client keys are tracked at once, no matter how many
	// unique keys — real or forged — send requests. See rateLimiter for
	// what happens once the store is at this limit.
	DefaultRateLimitMaxKeys = 10_000
)

// RateLimiterConfig configures RateLimit. The zero value uses the package
// defaults (DefaultRateLimitRequests, DefaultRateLimitWindow,
// DefaultRateLimitMaxKeys, time.Now, IPKeyFunc, and no trusted proxies).
type RateLimiterConfig struct {
	// Requests is the number of requests a single key may make per Window,
	// sustained indefinitely. Defaults to DefaultRateLimitRequests.
	Requests int
	// Window is the interval Requests applies over. Defaults to
	// DefaultRateLimitWindow.
	Window time.Duration
	// TrustedProxies controls which requests RateLimit and its default
	// Key (IPKeyFunc) treat as having arrived via a trusted reverse proxy;
	// see TrustedProxies for the spoofing risk of getting this wrong. The
	// zero value (nil) trusts no one, so every request is keyed on its raw
	// socket peer address regardless of any header it sends.
	TrustedProxies TrustedProxies
	// Clock returns the current time. Defaults to time.Now; tests inject a
	// fake clock (e.g. testutil.Clock) so limiter expiry is deterministic
	// and requires no sleeping.
	Clock func() time.Time
	// MaxKeys bounds the number of distinct client keys tracked at once.
	// Defaults to DefaultRateLimitMaxKeys.
	MaxKeys int
	// Key derives the rate-limit key for a request. Defaults to IPKeyFunc.
	// See KeyFunc, AccountKeyFunc, and CompositeKeyFunc for composing a
	// stricter route policy.
	Key KeyFunc
	// Logger receives RateLimit's own operational warnings — currently
	// just the rate-limited client-IP trust boundary mismatch warning (see
	// trustMismatchWarner) — as distinct from Logging's per-request access
	// log. Defaults to slog.Default().
	Logger *slog.Logger
}

func (c RateLimiterConfig) withDefaults() RateLimiterConfig {
	if c.Requests <= 0 {
		c.Requests = DefaultRateLimitRequests
	}
	if c.Window <= 0 {
		c.Window = DefaultRateLimitWindow
	}
	if c.Clock == nil {
		c.Clock = time.Now
	}
	if c.MaxKeys <= 0 {
		c.MaxKeys = DefaultRateLimitMaxKeys
	}
	if c.Key == nil {
		c.Key = IPKeyFunc
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}

// RateLimit returns middleware enforcing cfg's per-key request budget: a
// token bucket per client key (see KeyFunc), capacity cfg.Requests,
// refilling at cfg.Requests/cfg.Window tokens per second. A request whose
// key has exhausted its bucket gets 429 Too Many Requests through
// WriteError, with a Retry-After header giving a concrete whole-second
// wait.
//
// RateLimit is a reusable building block, not one fixed global policy:
// call it again with a stricter RateLimiterConfig around a specific route
// group (auth, slug claims, or render) to layer a tighter limit on the
// whole-server default wired in router.go.
//
// Keys come from cfg.Key, which defaults to IPKeyFunc — itself gated on
// cfg.TrustedProxies. See TrustedProxies for why getting that wrong is a
// bypass, not just an inaccuracy.
func RateLimit(cfg RateLimiterConfig) Middleware {
	cfg = cfg.withDefaults()
	limiter := newRateLimiter(cfg)
	warner := &trustMismatchWarner{}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			now := cfg.Clock()

			// Trust is configured (cfg.TrustedProxies is non-empty — the
			// required, fail-closed production state, see
			// internal/config's TRUSTED_PROXY_CIDRS), yet this request's
			// socket peer isn't in it. Either something is reaching this
			// process around the intended proxy hop, or TRUSTED_PROXY_CIDRS
			// doesn't match the real deployment topology, silently
			// degrading every such request to the untrusted-peer branch.
			// trustMismatchWarner throttles this to at most once per
			// trustMismatchWarnInterval rather than logging per request. See
			// trustMismatchWarner for why a one-shot warning is the
			// wrong shape for a condition that, once true, stays true for
			// every subsequent request until an operator fixes it.
			if len(cfg.TrustedProxies) > 0 && !cfg.TrustedProxies.trusts(r.RemoteAddr) {
				warner.maybeWarn(cfg.Logger, now, r.RemoteAddr)
			}

			key, ok := cfg.Key(r, cfg.TrustedProxies)
			if !ok {
				// cfg.Key could not determine a trusted, unambiguous key —
				// see IPKeyFunc/resolveClientIP for when this happens.
				// Reject outright rather than falling back to a default key.
				// A fallback would let a trusted proxy's
				// missing or malformed canonical header collapse every
				// viewer behind it into one shared bucket.
				//
				// Bound that fail-closed path by charging a bucket keyed on the
				// raw sending peer address before writing the 400. Its "peer:"
				// namespace is distinct from the normal "ip:" keys IPKeyFunc uses,
				// so it cannot spend any viewer's budget. A peer that keeps
				// sending a missing/malformed canonical header (e.g. a
				// compromised trusted proxy) exhausts its own budget and
				// gets 429 like everything else, instead of unlimited free
				// 400s. The point is metering, not log reduction. RateLimit sits
				// inside Logging, so a 429 is logged like a 400. This does not cut
				// log volume; it puts a ceiling on a request path that was otherwise
				// completely unmetered for any peer inside TRUSTED_PROXY_CIDRS.
				if peerKey, peerOK := peerBucketKey(r); peerOK {
					if allowed, retryAfter := limiter.allow(peerKey, now); !allowed {
						w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(retryAfter)))
						WriteError(w, http.StatusTooManyRequests, "rate_limited",
							"too many requests; retry later")
						return
					}
				}
				WriteError(w, http.StatusBadRequest, "invalid_client_ip",
					"client IP could not be determined from a trusted, unambiguous source")
				return
			}

			allowed, retryAfter := limiter.allow(key, now)
			if !allowed {
				w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(retryAfter)))
				WriteError(w, http.StatusTooManyRequests, "rate_limited",
					"too many requests; retry later")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// peerBucketKey derives a rate-limit key from r's raw socket peer address
// (never TrustedClientIPHeader, which by construction is exactly what
// failed to resolve when this is called) — see RateLimit's invalid_client_ip
// path. The "peer:" prefix keeps this in a separate namespace from
// IPKeyFunc's "ip:" keys, so metering a misbehaving proxy's malformed
// requests can never collide with, or spend down, any real viewer's own
// bucket.
func peerBucketKey(r *http.Request) (string, bool) {
	addr, ok := peerAddr(r.RemoteAddr)
	if !ok {
		return "", false
	}
	return "peer:" + addr.String(), true
}

// trustMismatchWarnInterval bounds how often trustMismatchWarner re-emits
// RateLimit's client-IP trust-boundary warning. A mismatch trips on every
// request until the configuration changes, but a one-shot warning can
// disappear from the current log window. One minute keeps the condition
// visible in any reasonable log-tailing window while
// still being far coarser than per-request.
const trustMismatchWarnInterval = time.Minute

// trustMismatchWarner throttles RateLimit's trust-boundary mismatch
// warning to trustMismatchWarnInterval instead of logging every offending
// request (a resource cost an operator didn't ask for, in exactly the
// failure mode this exists to catch) or logging only the very first one
// ever (see the const doc comment).
type trustMismatchWarner struct {
	mu           sync.Mutex
	clock        monotonicClamp // guarded by mu; see maybeWarn
	lastWarnedAt time.Time      // zero value: no warning emitted yet
}

func (w *trustMismatchWarner) maybeWarn(logger *slog.Logger, now time.Time, remoteAddr string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Clamp to a monotonic high-water mark so a backward clock step (an NTP
	// correction, a VM migration pause) cannot make now.Sub(lastWarnedAt)
	// negative and thereby silence this warning for the whole rollback span.
	// The condition this warns about is persistent, so
	// falling silent during a rollback is exactly when an operator most needs
	// to still see it.
	now = w.clock.clamp(now)
	if !w.lastWarnedAt.IsZero() && now.Sub(w.lastWarnedAt) < trustMismatchWarnInterval {
		return
	}
	w.lastWarnedAt = now

	logger.Warn(
		"security: client-IP trust boundary misconfigured: request peer is not "+
			"among the configured TrustedProxies even though trust is configured; "+
			"TRUSTED_PROXY_CIDRS likely does not match the real proxy topology "+
			"(design spec §6)",
		"remote_addr", remoteAddr,
	)
}

// retryAfterSeconds converts d to the whole-second count RateLimit sends in
// the Retry-After header, always at least 1 so a rejected caller is never
// told to retry immediately.
func retryAfterSeconds(d time.Duration) int {
	secs := int(math.Ceil(d.Seconds()))
	if secs < 1 {
		secs = 1
	}
	return secs
}

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

// KeyFunc derives the rate-limit key for a request, and whether one could
// be derived at all. RateLimiterConfig.Key defaults to IPKeyFunc; see
// IPKeyFunc, AccountKeyFunc, and CompositeKeyFunc for the building blocks a
// stricter per-route policy composes from. Every KeyFunc here returns a
// bounded value derived from a parsed IP address or opaque account ID,
// never an unbounded attacker-controlled input.
//
// The second return is false when no key could be safely derived (e.g.
// IPKeyFunc when resolveClientIP fails); RateLimit rejects the request
// outright in that case rather than treating "" as a valid, sharable key.
type KeyFunc func(r *http.Request, trusted TrustedProxies) (string, bool)

// IPKeyFunc keys solely on the request's client IP (see resolveClientIP).
// It is the default RateLimiterConfig.Key.
func IPKeyFunc(r *http.Request, trusted TrustedProxies) (string, bool) {
	addr, ok := resolveClientIP(r, trusted)
	if !ok {
		return "", false
	}
	return "ip:" + addr.String(), true
}

// AccountKeyFunc keys solely on the authenticated account ID that auth
// middleware stores in the request context via
// WithAccountID. An unauthenticated request has no account in context and
// keys on the empty account ID, so every anonymous caller would collide on
// one bucket — AccountKeyFunc must not be used alone on a route reachable
// without authentication; compose it with IPKeyFunc via CompositeKeyFunc,
// or restrict it to routes auth middleware already gates. It always
// succeeds (never rejects a request) since an absent account ID is a valid,
// deliberate anonymous key, not a malformed input.
func AccountKeyFunc(r *http.Request, _ TrustedProxies) (string, bool) {
	id, _ := AccountIDFromContext(r.Context())
	return "acct:" + id, true
}

// CompositeKeyFunc returns a KeyFunc joining every funcs' output into one
// key, so e.g. CompositeKeyFunc(AccountKeyFunc, IPKeyFunc) limits each
// (account, IP) pair independently, which neither dimension alone can
// express. A per-IP-only limit lets one account exhaust it from many IPs
// (or vice versa), and a per-account-only limit can't rate-limit
// pre-authentication attempts by
// IP at all. The composite fails (second return false) if any component
// KeyFunc does, so e.g. an unresolvable client IP still rejects the
// request rather than silently degrading to the account dimension alone.
func CompositeKeyFunc(funcs ...KeyFunc) KeyFunc {
	return func(r *http.Request, trusted TrustedProxies) (string, bool) {
		parts := make([]string, len(funcs))
		for i, f := range funcs {
			part, ok := f(r, trusted)
			if !ok {
				return "", false
			}
			parts[i] = part
		}
		return strings.Join(parts, "|"), true
	}
}

// rateLimitContextKey is an unexported type for this package's context
// keys (google.github.io/styleguide/go/decisions#contexts), so a value
// stored under it can never collide with a key defined elsewhere.
type rateLimitContextKey int

const accountIDContextKey rateLimitContextKey = 0

// WithAccountID returns a copy of ctx carrying accountID for
// AccountKeyFunc (and any CompositeKeyFunc built from it) to read back via
// AccountIDFromContext. Auth middleware calls this once it
// has authenticated the request, before RateLimit's handler runs on it.
func WithAccountID(ctx context.Context, accountID string) context.Context {
	return context.WithValue(ctx, accountIDContextKey, accountID)
}

// AccountIDFromContext returns the account ID stored by WithAccountID, and
// whether one was present — false for an unauthenticated request, or one
// that reached this code outside any auth middleware (e.g. most unit
// tests).
func AccountIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(accountIDContextKey).(string)
	return id, ok
}

// TrustedProxies is the set of reverse-proxy hops, by CIDR, whose
// TrustedClientIPHeader and X-Forwarded-Proto this server honors. A
// request is only treated as having arrived via a trusted proxy when its
// immediate socket peer (r.RemoteAddr) falls inside one of these ranges —
// unlike a header, RemoteAddr comes from the kernel's view of the actual
// TCP connection and cannot be forged by the client, so this is the one
// part of the trust decision an attacker cannot spoof.
//
// This must match the real deployment topology; see
// docs/design/deployment.md. Getting it wrong fails in different directions
// depending on which way it's wrong:
//   - Too broad (or defaulted to "trust everyone") lets any direct client
//     set its own TrustedClientIPHeader and pick any key it likes,
//     bypassing the limit entirely.
//   - Too narrow (or defaulted to "trust no one" when the topology
//     actually puts a proxy in front) makes every request appear to
//     originate from that proxy's own address, collapsing every distinct
//     real client into one shared bucket — a denial of service against
//     every legitimate client behind it, not just the misconfiguration's
//     author.
//
// Neither direction is a safe default, which is why this type's zero
// value (nil, trust no one) is only correct for a deployment with no
// proxy in front of Go at all — every other topology (see
// internal/config's TRUSTED_PROXY_CIDRS) must set this explicitly.
type TrustedProxies []netip.Prefix

// LoopbackTrustedProxies returns the trusted-proxy set for this project's
// production topology: Go bound to 127.0.0.1, reached
// only by Caddy over loopback (host networking, no ALB). It is NOT
// generally correct for podman-compose dev/self-host, where Caddy reaches
// Go as a separate container over the compose network rather than
// loopback — that topology's trusted CIDR is the compose network's
// subnet, supplied via TRUSTED_PROXY_CIDRS, not this function.
func LoopbackTrustedProxies() TrustedProxies {
	return TrustedProxies{
		netip.MustParsePrefix("127.0.0.1/32"),
		netip.MustParsePrefix("::1/128"),
	}
}

// trusts reports whether remoteAddr — an http.Request.RemoteAddr-shaped
// "host:port" string (or a bare host, as some hand-built test requests
// use) — falls inside tp.
func (tp TrustedProxies) trusts(remoteAddr string) bool {
	addr, ok := peerAddr(remoteAddr)
	if !ok {
		return false
	}
	for _, prefix := range tp {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// maxAddrLen bounds how much of a claimed address string peerAddr will
// even attempt to parse: the longest valid textual IP address (IPv6 with
// an embedded IPv4 tail, e.g.
// "ffff:ffff:ffff:ffff:ffff:ffff:255.255.255.255") is 45 bytes, so
// anything longer is rejected outright rather than handed to
// netip.ParseAddr — a cheap guard against a claimed header value crafted
// to be needlessly expensive to reject.
const maxAddrLen = 45

// peerAddr extracts and validates the IP address portion of an
// http.Request.RemoteAddr- or header-value-shaped string: "host:port",
// "[ipv6]:port", or a bare host with no port (real RemoteAddr values
// always have one; TrustedClientIPHeader values and some hand-built test
// requests don't). The returned netip.Addr is Unmap()'d, so an IPv4
// address and its IPv4-in-IPv6 form ("203.0.113.5" vs
// "::ffff:203.0.113.5") always normalize to the same value — both for
// keying the rate limiter and for matching against TrustedProxies, which
// would otherwise silently fail to recognize a v4-mapped peer as
// loopback/in-range on a dual-stack listener.
//
// The result is a fixed-size value type, never a substring of remoteAddr:
// a caller that stores addr.String() in a long-lived map (e.g.
// rateLimiter.entries) retains only the bytes of the formatted address
// itself, never remoteAddr's (or a raw header's) backing array.
func peerAddr(remoteAddr string) (netip.Addr, bool) {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	host = strings.TrimSpace(host)
	if host == "" || len(host) > maxAddrLen {
		return netip.Addr{}, false
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

// TrustedClientIPHeader is the header Caddy sets, once and only once it
// has itself verified the CloudFront origin-secret, restricted forwarded
// headers to CloudFront's origin-facing ranges, and stripped every
// client-supplied forwarding header, to the single validated viewer
// address. Caddy — not Go — is the one place that reconciles a multi-hop
// X-Forwarded-For chain (e.g. CloudFront appending the viewer address, then a
// proxy in front of it
// appending its own) into one address; Go trusts this header's value only
// when the request's socket peer is in TrustedProxies (see clientIP) and
// never parses X-Forwarded-For itself.
const TrustedClientIPHeader = "X-Real-IP"

// canonicalHeaderIP resolves the single, strictly-parsed client IP a
// trusted proxy asserted via TrustedClientIPHeader. The deployment contract
// requires exactly one bare address — never a port or list — so this does not
// reuse peerAddr's host:port-tolerant
// parsing: a proxy asserting "203.0.113.5:8080" here is already violating
// the contract and must be rejected, not silently corrected the way a real
// RemoteAddr's own trailing port is.
func canonicalHeaderIP(r *http.Request) (netip.Addr, bool) {
	values := r.Header.Values(TrustedClientIPHeader)
	if len(values) != 1 {
		// Missing entirely, or repeated (RFC 9110 §5.3 allows a sender to
		// repeat a header field): an ambiguous count must never be
		// resolved by silently picking the first or last value.
		return netip.Addr{}, false
	}

	raw := strings.TrimSpace(values[0])
	if raw == "" || len(raw) > maxAddrLen {
		return netip.Addr{}, false
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

// resolveClientIP returns r's client IP, and whether one could be
// determined at all. See TrustedClientIPHeader and TrustedProxies for the
// trust decision this depends on.
//
//   - If r arrived from a trusted proxy (see TrustedProxies), the ONLY
//     source ever consulted is TrustedClientIPHeader via canonicalHeaderIP
//     — never RemoteAddr, which at a trusted hop is the proxy's own
//     address, not the viewer's, and never X-Forwarded-For, which this
//     server does not parse at all. A trusted hop whose header is missing,
//     repeated, malformed, oversized, or port-bearing fails closed (false)
//     rather than falling back to RemoteAddr. A fallback would collapse
//     every viewer behind that proxy into one bucket keyed on the proxy's
//     own address. Forcing every viewer to share that bucket is worse than
//     rejecting the malformed request.
//   - If r did not arrive from a trusted proxy, RemoteAddr is the real
//     socket peer and is used directly via peerAddr; an unparseable
//     RemoteAddr also fails closed rather than being used as a raw,
//     unbounded string key.
func resolveClientIP(r *http.Request, trusted TrustedProxies) (netip.Addr, bool) {
	if trusted.trusts(r.RemoteAddr) {
		return canonicalHeaderIP(r)
	}
	return peerAddr(r.RemoteAddr)
}

// ClientIP returns r's resolved client IP as a bare address string (no
// port), and whether one could be determined at all — the same trust
// decision resolveClientIP/IPKeyFunc already make, exposed for a caller
// outside this package that needs the address itself rather than a
// rate-limit key derived from it. Session issuance must
// record the request's real, trust-boundary-resolved client IP — never a
// raw r.RemoteAddr, which at a trusted proxy hop is Caddy's own address,
// not the viewer's — and must never re-derive its own copy of this
// decision (see TrustedClientIPHeader/TrustedProxies for why getting it
// wrong is a spoofing bypass in one direction or a denial of service in
// the other).
func ClientIP(r *http.Request, trusted TrustedProxies) (string, bool) {
	addr, ok := resolveClientIP(r, trusted)
	if !ok {
		return "", false
	}
	return addr.String(), true
}

// requestIsHTTPS reports whether r arrived over HTTPS: either TLS
// terminated on this process directly, or r arrived via a trusted proxy
// (see TrustedProxies) asserting X-Forwarded-Proto: https. Caddy
// terminates TLS for CloudFront -> Caddy -> Go and always
// sets this header, so in production this is the path SecurityHeaders'
// HSTS decision actually takes.
func requestIsHTTPS(r *http.Request, trusted TrustedProxies) bool {
	if r.TLS != nil {
		return true
	}
	if !trusted.trusts(r.RemoteAddr) {
		return false
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
