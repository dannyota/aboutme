// Package auth_test exercises the __Host-oauth-tx cookie helpers. These
// tests are hermetic (no database): SetOAuthTxCookie, ReadOAuthTxCookie,
// and ClearOAuthTxCookie only touch net/http.ResponseWriter/Request.
package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
)

// setCookie calls fn (SetOAuthTxCookie or ClearOAuthTxCookie) against a
// fresh httptest.ResponseRecorder and returns the single cookie it wrote,
// parsed from the real "Set-Cookie" response header -- not just the
// pre-serialization *http.Cookie struct -- so a bug in how the header
// text itself is rendered would be caught too, not only a bug in which
// struct fields get set.
func setCookie(t *testing.T, fn func(http.ResponseWriter)) *http.Cookie {
	t.Helper()

	rec := httptest.NewRecorder()
	fn(rec)

	cookies := (&http.Response{Header: rec.Header()}).Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d Set-Cookie headers, want exactly 1 (raw: %q)", len(cookies), rec.Header().Values("Set-Cookie"))
	}
	return cookies[0]
}

// TestSetOAuthTxCookie_EmitsPinnedAttributes pins the brief's literal
// contract: "Secure; HttpOnly; SameSite=Lax; Path=/; Max-Age=600". Every
// attribute is asserted individually (not a substring match against the
// raw header) so a regression in any one of them -- e.g. a SameSite flip,
// a dropped Secure (which would silently void the __Host- prefix's
// guarantees), or a changed Max-Age -- fails this test specifically,
// rather than being caught only if some unrelated cookie-parsing test
// happens to notice.
func TestSetOAuthTxCookie_EmitsPinnedAttributes(t *testing.T) {
	t.Parallel()

	const handle = "handle-value-under-test"
	c := setCookie(t, func(w http.ResponseWriter) { auth.SetOAuthTxCookie(w, handle) })

	if c.Name != auth.OAuthTxCookieName {
		t.Errorf("Name = %q, want %q", c.Name, auth.OAuthTxCookieName)
	}
	if c.Value != handle {
		t.Errorf("Value = %q, want %q", c.Value, handle)
	}
	if !c.Secure {
		t.Error("Secure = false, want true (required for the __Host- prefix to mean anything)")
	}
	if !c.HttpOnly {
		t.Error("HttpOnly = false, want true")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want SameSiteLaxMode", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want %q", c.Path, "/")
	}
	if c.MaxAge != 600 {
		t.Errorf("MaxAge = %d, want 600 (10 minutes, matching oauthTxTTL)", c.MaxAge)
	}
	// __Host- is only meaningful with no Domain attribute at all; the
	// browser (not this test) is what actually enforces that, but a Domain
	// value here would mean the cookie stops being __Host- compliant.
	if c.Domain != "" {
		t.Errorf("Domain = %q, want empty (a __Host- cookie must not set Domain)", c.Domain)
	}
}

// TestClearOAuthTxCookie_ExpiresWithMatchingAttributes guards the other
// half of the __Host-oauth-tx contract: a browser only overwrites/deletes
// a cookie when Path (and, since this is __Host-, the implicit no-Domain
// and Secure) attributes match exactly what was set. If
// ClearOAuthTxCookie's Path, Secure, HttpOnly, or SameSite ever drifted
// from SetOAuthTxCookie's, the clear would silently stop working and a
// consumed/dead transaction cookie would linger in the browser -- exactly
// the failure mode the brief calls out for /callback's success and
// failure paths alike.
func TestClearOAuthTxCookie_ExpiresWithMatchingAttributes(t *testing.T) {
	t.Parallel()

	c := setCookie(t, auth.ClearOAuthTxCookie)

	if c.Name != auth.OAuthTxCookieName {
		t.Errorf("Name = %q, want %q", c.Name, auth.OAuthTxCookieName)
	}
	if c.Value != "" {
		t.Errorf("Value = %q, want empty", c.Value)
	}
	if c.MaxAge >= 0 {
		t.Errorf("MaxAge = %d, want negative (delete now)", c.MaxAge)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want %q (must match SetOAuthTxCookie's for the browser to actually delete it)", c.Path, "/")
	}
	if !c.Secure {
		t.Error("Secure = false, want true (must match SetOAuthTxCookie's)")
	}
	if !c.HttpOnly {
		t.Error("HttpOnly = false, want true (must match SetOAuthTxCookie's)")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want SameSiteLaxMode (must match SetOAuthTxCookie's)", c.SameSite)
	}
}

// TestReadOAuthTxCookie_RoundTripsSetValue proves ReadOAuthTxCookie
// recovers exactly the handle a request carries under
// auth.OAuthTxCookieName.
func TestReadOAuthTxCookie_RoundTripsSetValue(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req.AddCookie(requestCookie(auth.OAuthTxCookieName, "round-trip-handle"))

	got, err := auth.ReadOAuthTxCookie(req)
	if err != nil {
		t.Fatalf("ReadOAuthTxCookie() error = %v", err)
	}
	if got != "round-trip-handle" {
		t.Errorf("ReadOAuthTxCookie() = %q, want %q", got, "round-trip-handle")
	}
}

// TestReadOAuthTxCookie_MissingReturnsSentinel guards the no-cookie path:
// callers (the /callback handler) branch on ErrOAuthTxCookieMissing
// specifically, so it must be exactly this sentinel, not merely a
// non-nil error.
func TestReadOAuthTxCookie_MissingReturnsSentinel(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)

	_, err := auth.ReadOAuthTxCookie(req)
	if !errors.Is(err, auth.ErrOAuthTxCookieMissing) {
		t.Errorf("ReadOAuthTxCookie() error = %v, want ErrOAuthTxCookieMissing", err)
	}
}
