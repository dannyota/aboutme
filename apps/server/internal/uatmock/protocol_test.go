package uatmock

import (
	"context"
	"io"
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

const testVerifier = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"

func TestGoogleFlowUsesRealDiscoveryExchangeAndVerification(t *testing.T) {
	baseURL, closeServer := startTestServer(t)
	defer closeServer()

	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Timeout: 5 * time.Second})
	provider, err := oidc.NewProvider(ctx, baseURL+"/google")
	if err != nil {
		t.Fatalf("discover provider: %v", err)
	}
	oauthConfig := oauth2.Config{
		ClientID:     testClientID,
		ClientSecret: testClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  testRedirectURL,
		Scopes:       []string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail},
	}

	query := validAuthorizeQuery()
	code, redirect := authorizeThroughHTTP(t, baseURL, query)
	if got := redirect.Query().Get("state"); got != query.Get("state") {
		t.Fatalf("redirect state = %q, want exact input", got)
	}
	if got := redirect.Scheme + "://" + redirect.Host + redirect.Path; got != testRedirectURL {
		t.Fatalf("redirect URL = %q, want %q", got, testRedirectURL)
	}

	token, err := oauthConfig.Exchange(ctx, code, oauth2.VerifierOption(testVerifier))
	if err != nil {
		t.Fatalf("exchange code: %v", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		t.Fatal("token response has no id_token")
	}
	idToken, err := provider.Verifier(&oidc.Config{ClientID: testClientID, Now: testConfig().Now}).Verify(ctx, rawIDToken)
	if err != nil {
		t.Fatalf("verify ID token signature, issuer, audience, and expiry: %v", err)
	}
	if idToken.Nonce != query.Get("nonce") {
		t.Fatalf("nonce = %q, want %q", idToken.Nonce, query.Get("nonce"))
	}
	var claims struct {
		Subject       string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	if claims.Subject != "uat-google-001" || claims.Email != "developer@example.invalid" || !claims.EmailVerified || claims.Name != "Development User" {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestGoogleFlowConsumesCodeOnce(t *testing.T) {
	svc := newTestService(t)
	code := authorizeThroughHandler(t, svc.Handler(), validAuthorizeQuery())
	first := exchangeThroughHandler(t, svc.Handler(), code, testVerifier)
	if first.Code != http.StatusOK {
		t.Fatalf("first = %d, body = %s", first.Code, first.Body.String())
	}
	second := exchangeThroughHandler(t, svc.Handler(), code, testVerifier)
	if second.Code != http.StatusBadRequest {
		t.Fatalf("second = %d", second.Code)
	}
	if strings.Contains(second.Body.String(), code) {
		t.Fatal("token error echoed authorization code")
	}
}

func TestTokenExchangeRequiresExactBindingAndBurnsCodeBeforePKCECheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		field string
		value string
	}{
		{name: "client", field: "client_id", value: "other-client"},
		{name: "secret", field: "client_secret", value: "wrong-secret"},
		{name: "callback", field: "redirect_uri", value: "https://localhost:20443/escape"},
		{name: "grant", field: "grant_type", value: "refresh_token"},
		{name: "verifier", field: "code_verifier", value: "wrong-verifier"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(t)
			code := authorizeThroughHandler(t, svc.Handler(), validAuthorizeQuery())
			form := validTokenForm(code, testVerifier)
			form.Set(tt.field, tt.value)
			bad := exchangeFormThroughHandler(svc.Handler(), form)
			if bad.Code != http.StatusBadRequest {
				t.Fatalf("bad exchange = %d, want %d", bad.Code, http.StatusBadRequest)
			}
			if strings.Contains(bad.Body.String(), tt.value) || strings.Contains(bad.Body.String(), code) {
				t.Fatal("error response echoed a credential or code")
			}
			if tt.field == "code_verifier" {
				replay := exchangeThroughHandler(t, svc.Handler(), code, testVerifier)
				if replay.Code != http.StatusBadRequest {
					t.Fatalf("replay after PKCE failure = %d, want %d", replay.Code, http.StatusBadRequest)
				}
			}
		})
	}
}

func TestTokenExchangeRejectsInvalidPKCEVerifierSyntaxAfterConsumingCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		verifier string
	}{
		{name: "short", verifier: "a"},
		{name: "overlong", verifier: strings.Repeat("a", 129)},
		{name: "illegal character", verifier: strings.Repeat("a", 42) + "!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(t)
			query := validAuthorizeQuery()
			query.Set("code_challenge", oauth2.S256ChallengeFromVerifier(tt.verifier))
			code := authorizeThroughHandler(t, svc.Handler(), query)

			response := exchangeThroughHandler(t, svc.Handler(), code, tt.verifier)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("exchange = %d, want %d", response.Code, http.StatusBadRequest)
			}
			if got := response.Body.String(); got != "{\"error\":\"invalid_grant\"}\n" {
				t.Fatalf("error body = %q, want generic invalid_grant", got)
			}

			svc.mu.Lock()
			_, codeStillStored := svc.codes[code]
			svc.mu.Unlock()
			if codeStillStored {
				t.Fatal("invalid verifier did not consume the authorization code")
			}
		})
	}
}

func TestTokenRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	req := httptest.NewRequest(http.MethodPost, tokenPath, strings.NewReader(strings.Repeat("x", maxFormBytes+1)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func startTestServer(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	baseURL := "http://" + ln.Addr().String()
	cfg := testConfig()
	cfg.IssuerURL = baseURL + "/google"
	svc, err := New(cfg)
	if err != nil {
		_ = ln.Close()
		t.Fatalf("New(): %v", err)
	}
	server := &http.Server{Handler: svc.Handler(), ReadHeaderTimeout: time.Second}
	go func() { _ = server.Serve(ln) }()
	return baseURL, func() {
		_ = server.Close()
	}
}

func validAuthorizeQuery() url.Values {
	return url.Values{
		"client_id":             {testClientID},
		"redirect_uri":          {testRedirectURL},
		"response_type":         {"code"},
		"scope":                 {"openid profile email"},
		"state":                 {"exact-state-value"},
		"nonce":                 {"exact-nonce-value"},
		"code_challenge":        {oauth2.S256ChallengeFromVerifier(testVerifier)},
		"code_challenge_method": {"S256"},
	}
}

func authorizeThroughHTTP(t *testing.T, baseURL string, form url.Values) (string, *url.URL) {
	t.Helper()
	form = cloneValues(form)
	form.Set("account", googleSubject)
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.PostForm(baseURL+authorizePath, form)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("authorize status = %d, body = %s", resp.StatusCode, body)
	}
	redirect, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	return redirect.Query().Get("code"), redirect
}

func authorizeThroughHandler(t *testing.T, handler http.Handler, form url.Values) string {
	t.Helper()
	form = cloneValues(form)
	form.Set("account", googleSubject)
	req := httptest.NewRequest(http.MethodPost, authorizePath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("authorize = %d, body = %s", rec.Code, rec.Body.String())
	}
	redirect, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	code := redirect.Query().Get("code")
	if code == "" {
		t.Fatal("redirect has no code")
	}
	return code
}

func validTokenForm(code, verifier string) url.Values {
	return url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {testRedirectURL},
		"client_id":     {testClientID},
		"client_secret": {testClientSecret},
		"code_verifier": {verifier},
	}
}

func exchangeThroughHandler(t *testing.T, handler http.Handler, code, verifier string) *httptest.ResponseRecorder {
	t.Helper()
	return exchangeFormThroughHandler(handler, validTokenForm(code, verifier))
}

func exchangeFormThroughHandler(handler http.Handler, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, tokenPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func cloneValues(values url.Values) url.Values {
	cloned := make(url.Values, len(values))
	for key, items := range values {
		cloned[key] = append([]string(nil), items...)
	}
	return cloned
}
