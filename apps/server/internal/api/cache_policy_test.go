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

func jsonHandler(status int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		// A hardcoded literal written to an in-memory test ResponseWriter
		// cannot realistically fail; a non-nil error here means the test
		// double itself is broken, which should fail loudly rather than
		// be silently discarded.
		if _, err := w.Write([]byte(body)); err != nil {
			panic(err)
		}
	})
}

func TestNoStoreCache_SetsCacheControlNoStore(t *testing.T) {
	t.Parallel()

	handler := api.NoStoreCache()(passthroughHandler())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Cache-Control"); got != api.CacheControlNoStore {
		t.Errorf("Cache-Control = %q, want %q", got, api.CacheControlNoStore)
	}
}

// TestNoStoreCache_AppliesToRejectedResponses mirrors
// TestSecurityHeaders_AppliesToRejectedResponses: NoStoreCache sets the
// header before calling next, so it lands even when a downstream layer
// rejects the request.
func TestNoStoreCache_AppliesToRejectedResponses(t *testing.T) {
	t.Parallel()

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
	if got := rec.Header().Get("Cache-Control"); got != api.CacheControlNoStore {
		t.Errorf("Cache-Control on rejected response = %q, want %q", got, api.CacheControlNoStore)
	}
}

func TestPublicJSONCache_SetsCacheControlAndETag(t *testing.T) {
	t.Parallel()

	handler := api.PublicJSONCache()(jsonHandler(http.StatusOK, `{"slug":"danny"}`))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Cache-Control"); got != api.CacheControlPublicJSON {
		t.Errorf("Cache-Control = %q, want %q", got, api.CacheControlPublicJSON)
	}
	if got := rec.Header().Get("ETag"); got == "" {
		t.Error("ETag is empty, want a value")
	}
	if got := rec.Body.String(); got != `{"slug":"danny"}` {
		t.Errorf("body = %q, want the handler's original body", got)
	}
}

// TestCachePolicy_InnerGroupOverridesOuterDefault proves router.go's
// outer NoStoreCache default can be overridden. router.go wires it as the
// outermost DEFAULT on every chain (so rejections carry a cache directive),
// and its comment relies on a later route group being able to substitute
// its own policy by wrapping INSIDE the mux. That only holds because
// PublicJSONCache writes Cache-Control on the real ResponseWriter AFTER the
// outer NoStoreCache set it — this test pins that ordering so a future
// public-JSON route's policy can never be silently clobbered back to
// no-store by the default. It also protects the ETag/304 machinery that
// only PublicJSONCache provides.
func TestCachePolicy_InnerGroupOverridesOuterDefault(t *testing.T) {
	t.Parallel()

	// Mirror router.go's layering: NoStoreCache outermost (the default),
	// a group's PublicJSONCache wrapped inside it.
	handler := api.NoStoreCache()(api.PublicJSONCache()(jsonHandler(http.StatusOK, `{"slug":"danny"}`)))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/public", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Cache-Control"); got != api.CacheControlPublicJSON {
		t.Errorf("Cache-Control = %q, want %q — the inner group policy must override the outer "+
			"no-store default, not the reverse", got, api.CacheControlPublicJSON)
	}
	if got := rec.Header().Get("Cache-Control"); got == api.CacheControlNoStore {
		t.Errorf("Cache-Control = %q: the outer no-store default clobbered the inner group's policy", got)
	}
	if got := rec.Header().Get("ETag"); got == "" {
		t.Error("ETag is empty: the inner PublicJSONCache's conditional-request support was lost")
	}
}

func TestPublicJSONCache_SetsCacheControlOnNonSuccessButNoETag(t *testing.T) {
	t.Parallel()

	handler := api.PublicJSONCache()(jsonHandler(http.StatusNotFound, `{"error":{"code":"not_found"}}`))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := rec.Header().Get("Cache-Control"); got != api.CacheControlPublicJSON {
		t.Errorf("Cache-Control = %q, want %q (every response, not just 2xx)", got, api.CacheControlPublicJSON)
	}
	if got := rec.Header().Get("ETag"); got != "" {
		t.Errorf("ETag = %q, want absent on a non-2xx response", got)
	}
}

func TestPublicJSONCache_ETagStableForIdenticalBodyDiffersForDifferentBody(t *testing.T) {
	t.Parallel()

	req := func() *http.Request {
		return httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	}

	h1 := api.PublicJSONCache()(jsonHandler(http.StatusOK, `{"slug":"danny"}`))
	rec1 := httptest.NewRecorder()
	h1.ServeHTTP(rec1, req())

	h2 := api.PublicJSONCache()(jsonHandler(http.StatusOK, `{"slug":"danny"}`))
	rec2 := httptest.NewRecorder()
	h2.ServeHTTP(rec2, req())

	if rec1.Header().Get("ETag") != rec2.Header().Get("ETag") {
		t.Errorf("ETag for identical bodies differs: %q vs %q", rec1.Header().Get("ETag"), rec2.Header().Get("ETag"))
	}

	h3 := api.PublicJSONCache()(jsonHandler(http.StatusOK, `{"slug":"someone-else"}`))
	rec3 := httptest.NewRecorder()
	h3.ServeHTTP(rec3, req())

	if rec1.Header().Get("ETag") == rec3.Header().Get("ETag") {
		t.Errorf("ETag for different bodies is the same: %q", rec1.Header().Get("ETag"))
	}
}

// TestPublicJSONCache_HonorsIfNoneMatch_Returns304NoBody proves the mechanism
// conditional polling depends on. A client with the current ETag gets a
// bodyless 304, not a
// full re-download.
func TestPublicJSONCache_HonorsIfNoneMatch_Returns304NoBody(t *testing.T) {
	t.Parallel()

	body := `{"slug":"danny","updated":1}`

	first := api.PublicJSONCache()(jsonHandler(http.StatusOK, body))
	rec1 := httptest.NewRecorder()
	rec1Req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	first.ServeHTTP(rec1, rec1Req)
	etag := rec1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("first response has no ETag")
	}

	second := api.PublicJSONCache()(jsonHandler(http.StatusOK, body))
	rec2 := httptest.NewRecorder()
	rec2Req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	rec2Req.Header.Set("If-None-Match", etag)
	second.ServeHTTP(rec2, rec2Req)

	if rec2.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want %d", rec2.Code, http.StatusNotModified)
	}
	if got := rec2.Body.Len(); got != 0 {
		t.Errorf("304 body length = %d, want 0", got)
	}
	if got := rec2.Header().Get("Cache-Control"); got != api.CacheControlPublicJSON {
		t.Errorf("Cache-Control on 304 = %q, want %q", got, api.CacheControlPublicJSON)
	}
}

func TestPublicJSONCache_MismatchedIfNoneMatch_Returns200WithBody(t *testing.T) {
	t.Parallel()

	handler := api.PublicJSONCache()(jsonHandler(http.StatusOK, `{"slug":"danny"}`))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req.Header.Set("If-None-Match", `"stale-tag-that-will-never-match"`)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != `{"slug":"danny"}` {
		t.Errorf("body = %q, want the full body", got)
	}
}

func TestPublicJSONCache_IfNoneMatchWildcard_Returns304(t *testing.T) {
	t.Parallel()

	handler := api.PublicJSONCache()(jsonHandler(http.StatusOK, `{"slug":"danny"}`))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req.Header.Set("If-None-Match", "*")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotModified)
	}
}

func TestPublicJSONCache_PreservesHandlerHeaders(t *testing.T) {
	t.Parallel()

	handler := api.PublicJSONCache()(jsonHandler(http.StatusOK, `{}`))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want the handler's own value preserved", got)
	}
}

// TestRouter_HealthEndpoints_AreNoStore is an end-to-end check that
// router.go actually wires NoStoreCache around the health endpoints, so an
// operational check is never served stale by an intermediary.
func TestRouter_HealthEndpoints_AreNoStore(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{})

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

	handler := api.New(testLogger(), fakePinger{}, api.Options{})

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

		handler := api.New(testLogger(), fakePinger{}, api.Options{})
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

		handler := api.New(testLogger(), fakePinger{}, api.Options{BodyLimitBytes: 10})
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
		handler := api.New(testLogger(), fakePinger{}, api.Options{Clock: clock.Now})
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
		})
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

	handler := api.New(testLogger(), fakePinger{}, api.Options{})

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
