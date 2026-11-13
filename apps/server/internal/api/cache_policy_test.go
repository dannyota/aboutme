package api_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/testutil"
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
		req.RemoteAddr = "127.0.0.1:9000"
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

func TestRouter_HealthEndpoints_BypassRateLimit(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{}, nil)

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
