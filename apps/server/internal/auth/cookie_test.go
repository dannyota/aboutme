package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"danny.vn/aboutme/apps/server/internal/auth"
)

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
	if c.Domain != "" {
		t.Errorf("Domain = %q, want empty (a __Host- cookie must not set Domain)", c.Domain)
	}
}

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

func TestReadOAuthTxCookie_MissingReturnsSentinel(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)

	_, err := auth.ReadOAuthTxCookie(req)
	if !errors.Is(err, auth.ErrOAuthTxCookieMissing) {
		t.Errorf("ReadOAuthTxCookie() error = %v, want ErrOAuthTxCookieMissing", err)
	}
}
