package uatmock

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	testLinkedInClientID     = "aboutme-local-linkedin"
	testLinkedInClientSecret = "not-a-secret-local-linkedin"
	testLinkedInRedirectURL  = "https://localhost:20443/api/v1/auth/linkedin/callback"
)

func linkedinTestConfig() Config {
	cfg := testConfig()
	cfg.LinkedInClientID = testLinkedInClientID
	cfg.LinkedInClientSecret = testLinkedInClientSecret
	return cfg
}

func newLinkedInTestService(t *testing.T) *Service {
	t.Helper()
	svc, err := New(linkedinTestConfig())
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return svc
}

func validLinkedInAuthorizeQuery() url.Values {
	return url.Values{
		"client_id":     {testLinkedInClientID},
		"redirect_uri":  {testLinkedInRedirectURL},
		"response_type": {"code"},
		"scope":         {"openid profile email"},
		"state":         {"linkedin-state"},
		"nonce":         {"linkedin-nonce"},
	}
}

func validLinkedInTokenForm(code string) url.Values {
	return url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {testLinkedInRedirectURL},
		"client_id":     {testLinkedInClientID},
		"client_secret": {testLinkedInClientSecret},
	}
}

func postLinkedInForm(handler http.Handler, path string, form url.Values, mutate func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if mutate != nil {
		mutate(req)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// linkedinAuthorize submits the account form and returns the callback query.
func linkedinAuthorize(t *testing.T, handler http.Handler, action, subject string) url.Values {
	t.Helper()
	form := validLinkedInAuthorizeQuery()
	form.Set("action", action)
	if subject != "" {
		form.Set("account", subject)
	}
	rec := postLinkedInForm(handler, linkedinAuthorizePath, form, nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("authorize = %d, body = %s", rec.Code, rec.Body.String())
	}
	redirect, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	if got := redirect.Scheme + "://" + redirect.Host + redirect.Path; got != testLinkedInRedirectURL {
		t.Fatalf("redirect URL = %q, want %q", got, testLinkedInRedirectURL)
	}
	return redirect.Query()
}

func assertOAuthError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, status, rec.Body.String())
	}
	var body oauthError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error != code {
		t.Fatalf("body = %s, want error %q", rec.Body.String(), code)
	}
}

func TestLinkedInFlowUsesDocumentedRequestsAndRealVerification(t *testing.T) {
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	baseURL := "http://" + ln.Addr().String()
	cfg := linkedinTestConfig()
	cfg.IssuerURL = baseURL + "/google"
	svc, err := New(cfg)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	server := &http.Server{Handler: svc.Handler(), ReadHeaderTimeout: time.Second}
	serverDone := make(chan error, 1)
	go func() {
		serveErr := server.Serve(ln)
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
		serverDone <- serveErr
	}()
	defer func() {
		if closeErr := server.Close(); closeErr != nil {
			t.Errorf("close server: %v", closeErr)
		}
		if serveErr := <-serverDone; serveErr != nil {
			t.Errorf("serve: %v", serveErr)
		}
	}()

	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Timeout: 5 * time.Second})
	provider, err := oidc.NewProvider(ctx, baseURL+"/linkedin")
	if err != nil {
		t.Fatalf("discover provider: %v", err)
	}
	if got := provider.Endpoint().AuthURL; got != testPublicOrigin+linkedinAuthorizePath {
		t.Fatalf("authorization endpoint = %q", got)
	}
	endpoint := provider.Endpoint()
	endpoint.AuthStyle = oauth2.AuthStyleInParams
	oauthConfig := oauth2.Config{
		ClientID:     testLinkedInClientID,
		ClientSecret: testLinkedInClientSecret,
		Endpoint:     endpoint,
		RedirectURL:  testLinkedInRedirectURL,
		Scopes:       []string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail},
	}

	callback := linkedinAuthorize(t, svc.Handler(), "allow", "lnkd-Q7x2mP4tVa")
	if callback.Get("state") != "linkedin-state" {
		t.Fatalf("state = %q, want the exact input", callback.Get("state"))
	}
	token, err := oauthConfig.Exchange(ctx, callback.Get("code"))
	if err != nil {
		t.Fatalf("exchange without PKCE and with form credentials: %v", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		t.Fatal("token response has no id_token")
	}
	idToken, err := provider.Verifier(&oidc.Config{ClientID: testLinkedInClientID, Now: cfg.Now}).Verify(ctx, rawIDToken)
	if err != nil {
		t.Fatalf("verify ID token: %v", err)
	}
	var claims struct {
		Subject       string  `json:"sub"`
		Email         string  `json:"email"`
		EmailVerified *bool   `json:"email_verified"`
		Name          string  `json:"name"`
		Nonce         *string `json:"nonce"`
	}
	if err := idToken.Claims(&claims); err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	if claims.Subject != "lnkd-Q7x2mP4tVa" || claims.Email != "li-verified@example.invalid" ||
		claims.EmailVerified == nil || !*claims.EmailVerified || claims.Name != "LinkedIn Verified" {
		t.Fatalf("claims = %+v", claims)
	}
	// LinkedIn accepts the nonce parameter but returns no nonce claim.
	if claims.Nonce != nil {
		t.Fatalf("ID token nonce claim = %q, want absent as LinkedIn returns it", *claims.Nonce)
	}
}

func TestLinkedInTokenRejectsBasicCredentialsAndCodeVerifier(t *testing.T) {
	t.Parallel()

	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte(testLinkedInClientID+":"+testLinkedInClientSecret))
	tests := []struct {
		name   string
		form   func(url.Values)
		mutate func(*http.Request)
	}{
		{name: "basic credentials", mutate: func(r *http.Request) { r.Header.Set("Authorization", basic) }},
		{name: "basic credentials without form secret", form: func(f url.Values) { f.Del("client_secret") }, mutate: func(r *http.Request) { r.Header.Set("Authorization", basic) }},
		{name: "code verifier", form: func(f url.Values) { f.Set("code_verifier", testVerifier) }},
		{name: "wrong secret", form: func(f url.Values) { f.Set("client_secret", "wrong") }},
		{name: "wrong client", form: func(f url.Values) { f.Set("client_id", "wrong") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := newLinkedInTestService(t)
			code := linkedinAuthorize(t, svc.Handler(), "allow", "lnkd-Q7x2mP4tVa").Get("code")
			form := validLinkedInTokenForm(code)
			if tt.form != nil {
				tt.form(form)
			}
			assertOAuthError(t, postLinkedInForm(svc.Handler(), linkedinTokenPath, form, tt.mutate), http.StatusUnauthorized, "invalid_client")

			// The client was rejected before the code was read, so the
			// documented request still succeeds once.
			if rec := postLinkedInForm(svc.Handler(), linkedinTokenPath, validLinkedInTokenForm(code), nil); rec.Code != http.StatusOK {
				t.Fatalf("documented exchange = %d, body = %s", rec.Code, rec.Body.String())
			}
			assertOAuthError(t, postLinkedInForm(svc.Handler(), linkedinTokenPath, validLinkedInTokenForm(code), nil), http.StatusBadRequest, "invalid_grant")
		})
	}
}

func TestLinkedInAuthorizeRejectsPKCEAndRendersAccountForm(t *testing.T) {
	t.Parallel()
	svc := newLinkedInTestService(t)

	for _, name := range []string{"code_challenge", "code_challenge_method"} {
		query := validLinkedInAuthorizeQuery()
		query.Set(name, "S256")
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, linkedinAuthorizePath+"?"+query.Encode(), nil)
		rec := httptest.NewRecorder()
		svc.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("authorize with %s = %d, want %d", name, rec.Code, http.StatusBadRequest)
		}
	}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, linkedinAuthorizePath+"?"+validLinkedInAuthorizeQuery().Encode(), nil)
	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authorize = %d, body = %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{
		"<title>Local LinkedIn sign-in</title>", "Choose a local LinkedIn account",
		"LinkedIn Verified (li-verified@example.invalid)", "LinkedIn No Email (no email)",
		"LinkedIn Unverified (li-unverified@example.invalid)", "LinkedIn Collision (li-collision@example.invalid)",
		"LinkedIn Link (li-link@example.invalid)",
		`value="allow">Allow</button>`, `value="cancel_login">Cancel sign-in</button>`, `value="cancel_authorize">Cancel authorization</button>`,
	} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("authorize page missing %q", want)
		}
	}
	if strings.Contains(rec.Body.String(), "code_challenge") {
		t.Error("authorize page carries a PKCE field")
	}
}

func TestLinkedInCancelActionsReturnLinkedInErrors(t *testing.T) {
	t.Parallel()
	svc := newLinkedInTestService(t)

	for action, want := range map[string]string{
		"cancel_login":     "user_cancelled_login",     //nolint:misspell // LinkedIn's exact wire value.
		"cancel_authorize": "user_cancelled_authorize", //nolint:misspell // LinkedIn's exact wire value.
	} {
		callback := linkedinAuthorize(t, svc.Handler(), action, "")
		if callback.Get("error") != want || callback.Get("state") != "linkedin-state" || callback.Has("code") {
			t.Errorf("%s callback = %v, want error %q with state and no code", action, callback, want)
		}
	}
	form := validLinkedInAuthorizeQuery()
	form.Set("action", "approve-everything")
	form.Set("account", "lnkd-Q7x2mP4tVa")
	if rec := postLinkedInForm(svc.Handler(), linkedinAuthorizePath, form, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown action = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestLinkedInAccountsIssueOptionalEmailClaims(t *testing.T) {
	t.Parallel()
	svc := newLinkedInTestService(t)

	tests := []struct {
		subject      string
		wantEmail    string
		wantVerified string // "" means the claim is absent
	}{
		{subject: "lnkd-Q7x2mP4tVa", wantEmail: "li-verified@example.invalid", wantVerified: "true"},
		{subject: "lnkd-N3v8cR1sKe"},
		{subject: "lnkd-U5b9hW2yLo", wantEmail: "li-unverified@example.invalid", wantVerified: "false"},
		{subject: "lnkd-C2k6jT8fMu", wantEmail: "li-collision@example.invalid", wantVerified: "true"},
		{subject: "lnkd-L4r1dZ7gNi", wantEmail: "li-link@example.invalid", wantVerified: "true"},
	}
	for _, tt := range tests {
		code := linkedinAuthorize(t, svc.Handler(), "allow", tt.subject).Get("code")
		rec := postLinkedInForm(svc.Handler(), linkedinTokenPath, validLinkedInTokenForm(code), nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s exchange = %d, body = %s", tt.subject, rec.Code, rec.Body.String())
		}
		var token tokenResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &token); err != nil {
			t.Fatalf("decode token response: %v", err)
		}
		parts := strings.Split(token.IDToken, ".")
		if len(parts) != 3 {
			t.Fatalf("%s id_token is not a compact JWT", tt.subject)
		}
		payload, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		var claims map[string]json.RawMessage
		if err := json.Unmarshal(payload, &claims); err != nil {
			t.Fatalf("decode claims: %v", err)
		}
		if got := string(claims["sub"]); got != `"`+tt.subject+`"` {
			t.Errorf("sub = %s, want %q", got, tt.subject)
		}
		if tt.wantEmail == "" {
			if _, ok := claims["email"]; ok {
				t.Errorf("%s carries an email claim, want none", tt.subject)
			}
		} else if got := string(claims["email"]); got != `"`+tt.wantEmail+`"` {
			t.Errorf("%s email = %s, want %q", tt.subject, got, tt.wantEmail)
		}
		if got, ok := claims["email_verified"]; ok != (tt.wantVerified != "") || (ok && string(got) != tt.wantVerified) {
			t.Errorf("%s email_verified = %s (present %t), want %q", tt.subject, got, ok, tt.wantVerified)
		}
	}
}

func TestLinkedInModeNeedsBothCredentials(t *testing.T) {
	t.Parallel()

	for _, mutate := range []func(*Config){
		func(c *Config) { c.LinkedInClientID = "" },
		func(c *Config) { c.LinkedInClientSecret = "" },
		func(c *Config) { c.LinkedInClientSecret = strings.Repeat("x", maxFieldBytes+1) },
	} {
		cfg := linkedinTestConfig()
		mutate(&cfg)
		if _, err := New(cfg); err == nil {
			t.Error("New() error = nil, want a LinkedIn credential rejection")
		}
	}

	svc := newTestService(t)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, linkedinDiscoveryPath, nil)
	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("LinkedIn discovery without LinkedIn credentials = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
