package uatmock

import (
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	testIssuerURL    = "http://127.0.0.1:20442/google"
	testPublicOrigin = "https://localhost:20443"
	testRedirectURL  = testPublicOrigin + "/api/v1/auth/google/callback"
	testClientID     = "aboutme-local-google"
	testClientSecret = "not-a-secret-local-google"
)

func TestNewRejectsIncompleteOrInconsistentConfiguration(t *testing.T) {
	t.Parallel()

	valid := testConfig()
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "missing issuer", mutate: func(c *Config) { c.IssuerURL = "" }},
		{name: "issuer path", mutate: func(c *Config) { c.IssuerURL = "http://127.0.0.1:20442/linkedin" }},
		{name: "issuer query", mutate: func(c *Config) { c.IssuerURL += "?escape=1" }},
		{name: "public origin path", mutate: func(c *Config) { c.PublicOrigin += "/escape" }},
		{name: "callback outside origin", mutate: func(c *Config) { c.RedirectURL = "https://elsewhere.invalid/callback" }},
		{name: "wrong callback path", mutate: func(c *Config) { c.RedirectURL = testPublicOrigin + "/escape" }},
		{name: "missing client id", mutate: func(c *Config) { c.ClientID = "" }},
		{name: "missing client secret", mutate: func(c *Config) { c.ClientSecret = "" }},
		{name: "missing clock", mutate: func(c *Config) { c.Now = nil }},
		{name: "missing randomness", mutate: func(c *Config) { c.Random = nil }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.mutate(&cfg)
			if _, err := New(cfg); err == nil {
				t.Fatal("New() error = nil")
			}
		})
	}
}

func TestAuthorizationGETRendersAccessibleAccountForm(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	req := httptest.NewRequest(http.MethodGet, authorizePath+"?"+validAuthorizeQuery().Encode(), nil)
	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html;") {
		t.Fatalf("Content-Type = %q", got)
	}
	for _, want := range []string{"<form", `method="post"`, `action="` + authorizePath + `"`, "Development User", "developer@example.invalid", "Continue with Google"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestHandlerRejectsUnsupportedMethodsAndContentTypes(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	tests := []struct {
		name        string
		method      string
		path        string
		contentType string
		want        int
	}{
		{name: "discovery post", method: http.MethodPost, path: discoveryPath, want: http.StatusMethodNotAllowed},
		{name: "jwks head", method: http.MethodHead, path: jwksPath, want: http.StatusMethodNotAllowed},
		{name: "authorize put", method: http.MethodPut, path: authorizePath, want: http.StatusMethodNotAllowed},
		{name: "authorize json", method: http.MethodPost, path: authorizePath, contentType: "application/json", want: http.StatusUnsupportedMediaType},
		{name: "token get", method: http.MethodGet, path: tokenPath, want: http.StatusMethodNotAllowed},
		{name: "token multipart", method: http.MethodPost, path: tokenPath, contentType: "multipart/form-data", want: http.StatusUnsupportedMediaType},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader("{}"))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			rec := httptest.NewRecorder()
			svc.Handler().ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestAuthorizationRejectsInvalidAndOversizedFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		field  string
		value  string
		delete bool
	}{
		{name: "client", field: "client_id", value: "other-client"},
		{name: "callback", field: "redirect_uri", value: "https://localhost:20443/escape"},
		{name: "response type", field: "response_type", value: "token"},
		{name: "scope", field: "scope", value: "profile email"},
		{name: "empty state", field: "state", delete: true},
		{name: "empty nonce", field: "nonce", delete: true},
		{name: "challenge method", field: "code_challenge_method", value: "plain"},
		{name: "malformed challenge", field: "code_challenge", value: "short"},
		{name: "oversized state", field: "state", value: strings.Repeat("s", maxFieldBytes+1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(t)
			form := validAuthorizeQuery()
			form.Set("account", googleSubject)
			if tt.delete {
				form.Del(tt.field)
			} else {
				form.Set(tt.field, tt.value)
			}
			req := httptest.NewRequest(http.MethodPost, authorizePath, strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			svc.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
			if strings.Contains(rec.Body.String(), form.Get(tt.field)) && form.Get(tt.field) != "" {
				t.Fatal("error response echoed rejected field")
			}
		})
	}
}

func TestAuthorizationRejectsDuplicateAccountField(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	form := validAuthorizeQuery()
	form["account"] = []string{googleSubject, googleSubject}
	req := httptest.NewRequest(http.MethodPost, authorizePath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func testConfig() Config {
	return Config{
		IssuerURL:    testIssuerURL,
		PublicOrigin: testPublicOrigin,
		RedirectURL:  testRedirectURL,
		ClientID:     testClientID,
		ClientSecret: testClientSecret,
		Now:          func() time.Time { return time.Unix(1_800_000_000, 0).UTC() },
		Random:       rand.New(rand.NewSource(1)), //nolint:gosec // deterministic test-only randomness
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	svc, err := New(testConfig())
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return svc
}
