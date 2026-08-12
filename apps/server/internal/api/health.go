package api

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// DBPinger checks database reachability. *store.Pool satisfies this via its
// embedded pgxpool.Pool.
type DBPinger interface {
	Ping(ctx context.Context) error
}

// cachedPinger wraps a DBPinger behind a single-flight, TTL-memoized cache:
// at most one real Ping is ever in flight at a time, and its result —
// success OR failure — is reused by every caller for ttl afterward. This is
// what bounds /readyz's database cost regardless of request volume. Health
// endpoints are deliberately exempt from the viewer-keyed RateLimit. See
// router.go's healthChain comment: a false 429/400 on infra probes risks a
// restart loop. A failing ping is cached exactly like a succeeding one,
// deliberately: caching only successes would let a sustained flood against
// an already-down database retrigger a fresh connection-pool acquisition
// on every single request — a thundering herd against the very dependency
// that is already failing.
//
// New()/router.go constructs one of these once per server (wrapping the
// caller's real pinger) and passes it to Readyz in place of the raw
// pinger — Readyz itself has no caching logic of its own and does not need
// any, since cachedPinger already satisfies DBPinger.
type cachedPinger struct {
	pinger  DBPinger
	timeout time.Duration
	ttl     time.Duration
	clock   func() time.Time

	mu       sync.Mutex
	result   error
	expires  time.Time     // zero value: no cached result yet, always stale
	inflight chan struct{} // non-nil exactly while a real Ping is running
}

// newCachedPinger wraps pinger. timeout bounds each real ping it performs
// (independent of any individual caller's own context — see Ping); ttl
// bounds how long a cached result is reused (defaults to DefaultReadyTTL
// when <= 0); clock supplies the current time (defaults to time.Now when
// nil, so tests can inject a deterministic one).
func newCachedPinger(pinger DBPinger, timeout, ttl time.Duration, clock func() time.Time) *cachedPinger {
	if ttl <= 0 {
		ttl = DefaultReadyTTL
	}
	if clock == nil {
		clock = time.Now
	}
	return &cachedPinger{pinger: pinger, timeout: timeout, ttl: ttl, clock: clock}
}

// Ping implements DBPinger. A caller either reuses a still-fresh cached
// result immediately, waits for an already-in-flight real ping and then
// reuses ITS result, or — only if neither applies — becomes the one
// caller that actually performs a real ping, so however many callers
// arrive concurrently, at most one real Ping call is ever in flight.
func (c *cachedPinger) Ping(ctx context.Context) error {
	c.mu.Lock()
	if c.clock().Before(c.expires) {
		result := c.result
		c.mu.Unlock()
		return result
	}
	if done := c.inflight; done != nil {
		c.mu.Unlock()
		select {
		case <-done:
			return c.snapshot()
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	done := make(chan struct{})
	c.inflight = done
	c.mu.Unlock()

	// Deliberately NOT derived from ctx: this one real ping's result is
	// shared by every caller currently waiting on it (and every caller for
	// the next ttl), so one particular caller's own request being canceled
	// or timing out must never cut the ping short for everyone else. c.timeout
	// (Readyz's ReadyTimeout) still bounds how long it can run.
	pingCtx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	//nolint:contextcheck // intentionally independent of the caller's ctx — see the comment above
	err := c.pinger.Ping(pingCtx)

	c.mu.Lock()
	c.result = err
	c.expires = c.clock().Add(c.ttl)
	c.inflight = nil
	c.mu.Unlock()
	close(done)

	return err
}

func (c *cachedPinger) snapshot() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.result
}

// Healthz reports liveness: it always returns 200 while the process is
// running and has no dependencies to fail with, so it can never touch the
// database. A database outage must never affect this endpoint — otherwise
// an orchestrator restart-looping on a failing liveness check would take
// down a server that is otherwise healthy.
func Healthz() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteData(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// Readyz reports readiness: it pings the database with the given timeout
// and returns 200 when reachable, or 503 with the standard error envelope
// when it is not (or the ping does not complete within timeout).
func Readyz(pinger DBPinger, timeout time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		if err := pinger.Ping(ctx); err != nil {
			WriteError(w, http.StatusServiceUnavailable, "not_ready", "database is unreachable")
			return
		}
		WriteData(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}
