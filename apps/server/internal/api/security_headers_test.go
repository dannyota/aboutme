package api_test

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/api"
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

// TestSecurityHeaders_HSTSAbsentOnPlainHTTPLocalDev is the specific case
// the design calls out by name: a plain-HTTP request with no trusted-proxy
// hint must never get Strict-Transport-Security, because a developer
// hitting http://localhost would have their browser remember that and be
// forced onto HTTPS for localhost from then on.
func TestSecurityHeaders_HSTSAbsentOnPlainHTTPLocalDev(t *testing.T) {
	t.Parallel()

	handler := api.SecurityHeaders(api.LoopbackTrustedProxies())(passthroughHandler())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "http://localhost/healthz", nil)
	req.RemoteAddr = "127.0.0.1:54321" // plain HTTP: no TLS, no X-Forwarded-Proto
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("Strict-Transport-Security = %q, want absent on plain HTTP", got)
	}
}

// TestSecurityHeaders_HSTSPresentViaTrustedProxyHTTPS covers the production
// path: Caddy terminates TLS and forwards to Go over plain loopback HTTP,
// asserting X-Forwarded-Proto: https. Because the peer is trusted, that
// assertion is honored and HSTS is sent.
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

// TestSecurityHeaders_HSTSAbsentWhenProxyHintComesFromUntrustedPeer proves
// the same spoofing-resistance property as rate limiting's client-IP
// handling: X-Forwarded-Proto is only honored when it arrives via a
// trusted proxy peer. A direct, untrusted client claiming https must not
// get HSTS just by setting the header itself.
func TestSecurityHeaders_HSTSAbsentWhenProxyHintComesFromUntrustedPeer(t *testing.T) {
	t.Parallel()

	handler := api.SecurityHeaders(api.LoopbackTrustedProxies())(passthroughHandler())

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "203.0.113.5:54321" // not a trusted proxy
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

// TestSecurityHeaders_AppliesToRejectedResponses proves headers land even
// when a downstream layer rejects the request — the specific requirement
// that determined where SecurityHeaders sits in router.go's middleware
// chain (outside every layer that can itself write a response).
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

// TestRouter_New_HSTSAbsentOnPlainHTTPAndPresentViaTrustedProxy is an
// end-to-end check through api.New()'s real middleware chain (not just the
// SecurityHeaders middleware in isolation), confirming router.go's wiring
// actually threads opts.TrustedProxies through to SecurityHeaders.
func TestRouter_New_HSTSAbsentOnPlainHTTPAndPresentViaTrustedProxy(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{TrustedProxies: api.LoopbackTrustedProxies()})

	plain := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", nil)
	plain.RemoteAddr = "203.0.113.9:1111" // not loopback: not the trusted Caddy hop
	recPlain := httptest.NewRecorder()
	handler.ServeHTTP(recPlain, plain)
	if got := recPlain.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("plain HTTP via untrusted peer: Strict-Transport-Security = %q, want absent", got)
	}

	viaProxy := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", nil)
	viaProxy.RemoteAddr = "127.0.0.1:1111" // the trusted Caddy hop
	viaProxy.Header.Set("X-Forwarded-Proto", "https")
	recProxy := httptest.NewRecorder()
	handler.ServeHTTP(recProxy, viaProxy)
	if got := recProxy.Header().Get("Strict-Transport-Security"); got == "" {
		t.Error("via trusted proxy asserting https: Strict-Transport-Security is absent, want present")
	}
}

// TestRouter_New_DefaultOptions_NeverTrustsAnyPeer proves api.New does not
// hardcode LoopbackTrustedProxies(), which is wrong for
// any topology (e.g. podman-compose) where Caddy doesn't reach Go over
// loopback. api.New must not assume a topology on the caller's behalf — a
// zero-value Options (no TrustedProxies configured) must trust no peer at
// all, even one connecting from loopback, matching
// RateLimiterConfig.TrustedProxies' own documented zero-value semantics.
func TestRouter_New_DefaultOptions_NeverTrustsAnyPeer(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "127.0.0.1:1111" // would be the trusted Caddy hop, IF configured
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("Strict-Transport-Security = %q, want absent: api.Options{} must not implicitly trust "+
			"loopback (or any peer) — the caller must configure TrustedProxies explicitly", got)
	}
}
