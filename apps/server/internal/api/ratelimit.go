package api

import (
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	DefaultRateLimitRequests = 300
	DefaultRateLimitWindow   = time.Minute
	DefaultRateLimitMaxKeys  = 10_000
)

type RateLimiterConfig struct {
	Requests       int
	Window         time.Duration
	TrustedProxies TrustedProxies
	Clock          func() time.Time
	MaxKeys        int
	Key            KeyFunc
	Logger         *slog.Logger
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

func RateLimit(cfg RateLimiterConfig) Middleware {
	cfg = cfg.withDefaults()
	limiter := newRateLimiter(cfg)
	warner := &trustMismatchWarner{}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			now := cfg.Clock()

			if len(cfg.TrustedProxies) > 0 && !cfg.TrustedProxies.trusts(r.RemoteAddr) {
				warner.maybeWarn(cfg.Logger, now, r.RemoteAddr)
			}

			key, ok := cfg.Key(r, cfg.TrustedProxies)
			if !ok {
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

func peerBucketKey(r *http.Request) (string, bool) {
	addr, ok := peerAddr(r.RemoteAddr)
	if !ok {
		return "", false
	}
	return "peer:" + addr.String(), true
}

const trustMismatchWarnInterval = time.Minute

type trustMismatchWarner struct {
	mu           sync.Mutex
	clock        monotonicClamp
	lastWarnedAt time.Time
}

func (w *trustMismatchWarner) maybeWarn(logger *slog.Logger, now time.Time, remoteAddr string) {
	w.mu.Lock()
	defer w.mu.Unlock()

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

func retryAfterSeconds(d time.Duration) int {
	secs := int(math.Ceil(d.Seconds()))
	if secs < 1 {
		secs = 1
	}
	return secs
}
