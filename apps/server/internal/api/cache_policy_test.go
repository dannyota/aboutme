package api_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

func TestNoStoreCache_SetsCacheControlNoStore(t *testing.T) {
	t.Parallel()
	const wantCacheControl = "no-store, no-transform"

	handler := api.NoStoreCache()(passthroughHandler())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Cache-Control"); got != wantCacheControl {
		t.Errorf("Cache-Control = %q, want %q", got, wantCacheControl)
	}
}

// TestNoStoreCache_AppliesToRejectedResponses mirrors
// TestSecurityHeaders_AppliesToRejectedResponses: NoStoreCache sets the
// header before calling next, so it lands even when a downstream layer
// rejects the request.
func TestNoStoreCache_AppliesToRejectedResponses(t *testing.T) {
	t.Parallel()
	const wantCacheControl = "no-store, no-transform"

	rejecting := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.WriteError(w, http.StatusTeapot, "rejected_for_test", "rejected")
	})
	handler := api.NoStoreCache()(rejecting)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
	if got := rec.Header().Get("Cache-Control"); got != wantCacheControl {
		t.Errorf("Cache-Control on rejected response = %q, want %q", got, wantCacheControl)
	}
}

// TestCachePolicy_InnerGroupOverridesOuterDefault proves router.go's
// outer NoStoreCache default does not clobber a policy set inside the mux.
// The public API sets its own Cache-Control in the handler, after the outer
// NoStoreCache ran, and that later write must be the one sent.
func TestCachePolicy_InnerGroupOverridesOuterDefault(t *testing.T) {
	t.Parallel()
	const innerPolicy = "no-cache, must-revalidate"

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", innerPolicy)
		w.WriteHeader(http.StatusOK)
	})
	handler := api.NoStoreCache()(inner)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/public", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Cache-Control"); got != innerPolicy {
		t.Errorf("Cache-Control = %q, want %q: the inner policy must override the outer no-store default", got, innerPolicy)
	}
}

// TestRouter_HealthEndpoints_AreNoStore is an end-to-end check that
// router.go actually wires NoStoreCache around the health endpoints, so an
// operational check is never served stale by an intermediary.
func TestRouter_HealthEndpoints_AreNoStore(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{}, nil)

	for _, path := range []string{"/healthz", "/readyz"} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if got := rec.Header().Get("Cache-Control"); got != api.CacheControlNoStore {
			t.Errorf("%s: Cache-Control = %q, want %q", path, got, api.CacheControlNoStore)
		}
	}
}

// TestRouter_HealthEndpoint_BodyTooLargeStillCarriesNoStore proves a
// health-route response that BodyLimit itself rejects (413) must
// still carry the documented Cache-Control: no-store policy. The body
// exceeds api.HealthBodyLimitBytes, the health chain's own dedicated cap,
// not the general Options.BodyLimitBytes, which does not apply to health
// routes.
func TestRouter_HealthEndpoint_BodyTooLargeStillCarriesNoStore(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{}, nil)

	body := bytes.Repeat([]byte("a"), int(api.HealthBodyLimitBytes)+1)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz",
		bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	if got := rec.Header().Get("Cache-Control"); got != api.CacheControlNoStore {
		t.Errorf("Cache-Control on a rejected (413) health response = %q, want %q", got, api.CacheControlNoStore)
	}
}

// TestRouter_NonHealthChain_AllRejectionsCarryNoStore proves
// NoStoreCache must be the default, outermost cache policy of the
// non-health chain, so every rejection it can produce — 404
// (unknown route), 413 (oversized body), 429 (rate limited), and 400
// (invalid_client_ip) — carries Cache-Control: no-store, exactly like the
// health chain already does. Route groups override this default from inside
// the mux when they need another policy.
func TestRouter_NonHealthChain_AllRejectionsCarryNoStore(t *testing.T) {
	t.Parallel()

	t.Run("404 unknown route", func(t *testing.T) {
		t.Parallel()

		handler := api.New(testLogger(), fakePinger{}, api.Options{}, nil)
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/does-not-exist", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
		if got := rec.Header().Get("Cache-Control"); got != api.CacheControlNoStore {
			t.Errorf("Cache-Control = %q, want %q", got, api.CacheControlNoStore)
		}
	})

	t.Run("413 body too large", func(t *testing.T) {
		t.Parallel()

		handler := api.New(testLogger(), fakePinger{}, api.Options{BodyLimitBytes: 10}, nil)
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/anything",
			bytes.NewReader(bytes.Repeat([]byte("a"), 20)))
		req.ContentLength = 20
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
		}
		if got := rec.Header().Get("Cache-Control"); got != api.CacheControlNoStore {
			t.Errorf("Cache-Control = %q, want %q", got, api.CacheControlNoStore)
		}
	})

	t.Run("429 rate limited", func(t *testing.T) {
		t.Parallel()

		clock := testutil.NewClockAtEpoch()
		handler := api.New(testLogger(), fakePinger{}, api.Options{Clock: clock.Now}, nil)
		var rec *httptest.ResponseRecorder
		for i := 0; i < api.DefaultRateLimitRequests+1; i++ {
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/anything", nil)
			rec = httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
		}
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("final status = %d, want %d", rec.Code, http.StatusTooManyRequests)
		}
		if got := rec.Header().Get("Cache-Control"); got != api.CacheControlNoStore {
			t.Errorf("Cache-Control = %q, want %q", got, api.CacheControlNoStore)
		}
	})

	t.Run("400 invalid client ip", func(t *testing.T) {
		t.Parallel()

		handler := api.New(testLogger(), fakePinger{}, api.Options{
			TrustedProxies: api.LoopbackTrustedProxies(),
		}, nil)
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/anything", nil)
		req.RemoteAddr = "127.0.0.1:9000" // trusted peer, no canonical header
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
		if got := rec.Header().Get("Cache-Control"); got != api.CacheControlNoStore {
			t.Errorf("Cache-Control = %q, want %q", got, api.CacheControlNoStore)
		}
	})
}

// TestRouter_HealthEndpoints_BypassRateLimit proves flooding
// /healthz far past a tiny configured request budget must never return
// 429 — infrastructure probes are exempt from the external-viewer limiter
// entirely, not merely given a generous quota.
func TestRouter_HealthEndpoints_BypassRateLimit(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{}, nil)

	// api.New wires RateLimit with its package-default budget
	// (api.DefaultRateLimitRequests); comfortably exceed it against a
	// single path to prove exemption, not just a generous quota.
	for i := 0; i < api.DefaultRateLimitRequests+50; i++ {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d to /healthz: status = %d, want %d (health probes must bypass RateLimit)",
				i, rec.Code, http.StatusOK)
		}
	}
}
