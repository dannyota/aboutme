package api

import (
	"context"
	"net/http"
	"sync"
	"time"
)

type DBPinger interface {
	Ping(ctx context.Context) error
}

type cachedPinger struct {
	pinger  DBPinger
	timeout time.Duration
	ttl     time.Duration
	clock   func() time.Time

	mu       sync.Mutex
	result   error
	expires  time.Time
	inflight chan struct{}
}

func newCachedPinger(pinger DBPinger, timeout, ttl time.Duration, clock func() time.Time) *cachedPinger {
	if ttl <= 0 {
		ttl = DefaultReadyTTL
	}
	if clock == nil {
		clock = time.Now
	}
	return &cachedPinger{pinger: pinger, timeout: timeout, ttl: ttl, clock: clock}
}

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

	pingCtx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	//nolint:contextcheck // intentionally independent of the caller's ctx
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

func Healthz() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteData(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

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
