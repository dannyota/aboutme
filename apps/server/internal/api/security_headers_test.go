package api_test

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"danny.vn/aboutme/apps/server/internal/api"
)

func passthroughHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestSecurityHeaders_SetsBaselineHeaders(t *testing.T) {
	t.Parallel()

	handler := api.SecurityHeaders(nil)(passthroughHandler())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Security-Policy"); got != api.ContentSecurityPolicy {
		t.Errorf("Content-Security-Policy = %q, want %q", got, api.ContentSecurityPolicy)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != api.XContentTypeOptions {
		t.Errorf("X-Content-Type-Options = %q, want %q", got, api.XContentTypeOptions)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != api.ReferrerPolicy {
		t.Errorf("Referrer-Policy = %q, want %q", got, api.ReferrerPolicy)
	}
}

func TestSecurityHeaders_HSTSAbsentOnPlainHTTPLocalDev(t *testing.T) {
	t.Parallel()

	handler := api.SecurityHeaders(api.LoopbackTrustedProxies())(passthroughHandler())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://localhost/healthz", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("Strict-Transport-Security = %q, want absent on plain HTTP", got)
	}
}

func TestSecurityHeaders_HSTSPresentViaTrustedProxyHTTPS(t *testing.T) {
	t.Parallel()

	handler := api.SecurityHeaders(api.LoopbackTrustedProxies())(passthroughHandler())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Strict-Transport-Security"); got != api.StrictTransportSecurityValue {
		t.Errorf("Strict-Transport-Security = %q, want %q", got, api.StrictTransportSecurityValue)
	}
}

func TestSecurityHeaders_HSTSAbsentWhenProxyHintComesFromUntrustedPeer(t *testing.T) {
	t.Parallel()

	handler := api.SecurityHeaders(api.LoopbackTrustedProxies())(passthroughHandler())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "203.0.113.5:54321"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("Strict-Transport-Security = %q, want absent (X-Forwarded-Proto from an untrusted peer must be ignored)", got)
	}
}

func TestSecurityHeaders_HSTSPresentOnDirectTLS(t *testing.T) {
	t.Parallel()

	handler := api.SecurityHeaders(nil)(passthroughHandler())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", nil)
	req.TLS = &tls.ConnectionState{}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Strict-Transport-Security"); got != api.StrictTransportSecurityValue {
		t.Errorf("Strict-Transport-Security = %q, want %q", got, api.StrictTransportSecurityValue)
	}
}

func TestSecurityHeaders_AppliesToRejectedResponses(t *testing.T) {
	t.Parallel()

	rejecting := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.WriteError(w, http.StatusTeapot, "rejected_for_test", "rejected")
	})
	handler := api.SecurityHeaders(nil)(rejecting)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != api.ContentSecurityPolicy {
		t.Errorf("Content-Security-Policy on rejected response = %q, want %q", got, api.ContentSecurityPolicy)
	}
}

func TestRouter_New_HSTSAbsentOnPlainHTTPAndPresentViaTrustedProxy(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{TrustedProxies: api.LoopbackTrustedProxies()}, nil)

	plain := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", nil)
	plain.RemoteAddr = "203.0.113.9:1111"
	recPlain := httptest.NewRecorder()
	handler.ServeHTTP(recPlain, plain)
	if got := recPlain.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("plain HTTP via untrusted peer: Strict-Transport-Security = %q, want absent", got)
	}

	viaProxy := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", nil)
	viaProxy.RemoteAddr = "127.0.0.1:1111"
	viaProxy.Header.Set("X-Forwarded-Proto", "https")
	recProxy := httptest.NewRecorder()
	handler.ServeHTTP(recProxy, viaProxy)
	if got := recProxy.Header().Get("Strict-Transport-Security"); got == "" {
		t.Error("via trusted proxy asserting https: Strict-Transport-Security is absent, want present")
	}
}

func TestRouter_New_DefaultOptions_NeverTrustsAnyPeer(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{}, nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "127.0.0.1:1111"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("Strict-Transport-Security = %q, want absent: api.Options{} must not implicitly trust "+
			"loopback (or any peer); the caller must configure TrustedProxies explicitly", got)
	}
}
