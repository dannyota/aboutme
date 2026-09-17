package api

import (
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
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
