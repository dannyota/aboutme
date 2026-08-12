// Package auth_test exercises the hermetic __Host-session cookie helpers.
package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
)

// setSessionCookie parses the emitted Set-Cookie header.
func setSessionCookie(t *testing.T, fn func(http.ResponseWriter)) *http.Cookie {
	t.Helper()

	rec := httptest.NewRecorder()
	fn(rec)

	cookies := (&http.Response{Header: rec.Header()}).Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d Set-Cookie headers, want exactly 1 (raw: %q)", len(cookies), rec.Header().Values("Set-Cookie"))
	}
	return cookies[0]
}

// TestSetSessionCookie_EmitsPinnedAttributes checks each attribute from
// docs/design/security.md independently.
func TestSetSessionCookie_EmitsPinnedAttributes(t *testing.T) {
	t.Parallel()

	const token = "session-token-value-under-test"
	c := setSessionCookie(t, func(w http.ResponseWriter) { auth.SetSessionCookie(w, token) })

	if c.Name != auth.SessionCookieName {
		t.Errorf("Name = %q, want %q", c.Name, auth.SessionCookieName)
	}
	if c.Value != token {
		t.Errorf("Value = %q, want %q", c.Value, token)
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
	const wantMaxAge = 90 * 24 * 60 * 60 // 90 days, in seconds
	if c.MaxAge != wantMaxAge {
		t.Errorf("MaxAge = %d, want %d (90 days, matching session.go's absoluteTimeout)", c.MaxAge, wantMaxAge)
	}
	// __Host- is only meaningful with no Domain attribute at all.
	if c.Domain != "" {
		t.Errorf("Domain = %q, want empty (a __Host- cookie must not set Domain)", c.Domain)
	}
}

// TestClearSessionCookie_ExpiresWithMatchingAttributes ensures the deletion
// cookie matches the attributes that identify the stored cookie.
func TestClearSessionCookie_ExpiresWithMatchingAttributes(t *testing.T) {
	t.Parallel()

	c := setSessionCookie(t, auth.ClearSessionCookie)

	if c.Name != auth.SessionCookieName {
		t.Errorf("Name = %q, want %q", c.Name, auth.SessionCookieName)
	}
	if c.Value != "" {
		t.Errorf("Value = %q, want empty", c.Value)
	}
	if c.MaxAge >= 0 {
		t.Errorf("MaxAge = %d, want negative (delete now)", c.MaxAge)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want %q (must match SetSessionCookie's for the browser to actually delete it)", c.Path, "/")
	}
	if !c.Secure {
		t.Error("Secure = false, want true (must match SetSessionCookie's)")
	}
	if !c.HttpOnly {
		t.Error("HttpOnly = false, want true (must match SetSessionCookie's)")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want SameSiteLaxMode (must match SetSessionCookie's)", c.SameSite)
	}
}
