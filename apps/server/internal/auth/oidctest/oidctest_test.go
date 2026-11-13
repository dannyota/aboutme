package oidctest_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"danny.vn/aboutme/apps/server/internal/auth/oidctest"
)

func ptrTrue() *bool {
	b := true
	return &b
}

func ptrFalse() *bool {
	b := false
	return &b
}

func generateTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating test RSA key: %v", err)
	}
	return key
}

type tokenResponseBody struct {
	IDToken     string `json:"id_token"`
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

func exchangeCodeFull(t *testing.T, p *oidctest.Provider, code string) (body tokenResponseBody, status int) {
	t.Helper()

	form := url.Values{
		"grant_type": {"authorization_code"},
		"code":       {code},
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, p.URL+"/token", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("building token request for code %q: %v", code, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("exchanging code %q: %v", code, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			t.Errorf("close response body: %v", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return tokenResponseBody{}, resp.StatusCode
	}

	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding token response: %v", err)
	}
	return body, resp.StatusCode
}

func exchangeCode(t *testing.T, p *oidctest.Provider, code string) (idToken string, status int) {
	t.Helper()

	body, status := exchangeCodeFull(t, p, code)
	if status != http.StatusOK {
		return "", status
	}
	if body.AccessToken == "" {
		t.Error("token response missing access_token")
	}
	if body.TokenType == "" {
		t.Error("token response missing token_type")
	}
	return body.IDToken, status
}

func TestProvider_DiscoveryAndTokenRoundTrip(t *testing.T) {
	p := oidctest.NewProvider(t)
	p.RegisterCode("test-code", oidctest.Claims{
		Subject:       "user-1",
		Email:         "a@example.com",
		EmailVerified: ptrTrue(),
	})

	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, p.URL)
	if err != nil {
		t.Fatalf("oidc.NewProvider: %v", err)
	}

	rawIDToken, status := exchangeCode(t, p, "test-code")
	if status != http.StatusOK {
		t.Fatalf("token exchange status = %d, want %d", status, http.StatusOK)
	}
	if rawIDToken == "" {
		t.Fatal("token response missing id_token")
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: oidctest.DefaultClientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		t.Fatalf("verifier.Verify: %v", err)
	}

	if idToken.Subject != "user-1" {
		t.Errorf("Subject = %q, want %q", idToken.Subject, "user-1")
	}

	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := idToken.Claims(&claims); err != nil {
		t.Fatalf("idToken.Claims: %v", err)
	}
	if claims.Email != "a@example.com" {
		t.Errorf("Email = %q, want %q", claims.Email, "a@example.com")
	}
	if !claims.EmailVerified {
		t.Errorf("EmailVerified = false, want true")
	}
}

func TestProvider_RegisterCode_SingleUse(t *testing.T) {
	p := oidctest.NewProvider(t)
	p.RegisterCode("once-only", oidctest.Claims{Subject: "user-1"})

	_, firstStatus := exchangeCode(t, p, "once-only")
	if firstStatus != http.StatusOK {
		t.Fatalf("first exchange status = %d, want %d", firstStatus, http.StatusOK)
	}

	_, secondStatus := exchangeCode(t, p, "once-only")
	if secondStatus == http.StatusOK {
		t.Fatal("second exchange of the same code succeeded, want failure")
	}
}

func TestClaims_IssuerOverride(t *testing.T) {
	p := oidctest.NewProvider(t)
	p.RegisterCode("code", oidctest.Claims{
		Subject: "user-1",
		Issuer:  "https://evil.example",
	})

	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, p.URL)
	if err != nil {
		t.Fatalf("oidc.NewProvider: %v", err)
	}

	rawIDToken, status := exchangeCode(t, p, "code")
	if status != http.StatusOK {
		t.Fatalf("token exchange status = %d, want %d", status, http.StatusOK)
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: oidctest.DefaultClientID})
	if _, err := verifier.Verify(ctx, rawIDToken); err == nil {
		t.Fatal("Verify succeeded for a token with a mismatched issuer, want error")
	}
}

func TestClaims_AudienceOverride(t *testing.T) {
	p := oidctest.NewProvider(t)
	p.RegisterCode("code", oidctest.Claims{
		Subject:  "user-1",
		Audience: "some-other-client-id",
	})

	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, p.URL)
	if err != nil {
		t.Fatalf("oidc.NewProvider: %v", err)
	}

	rawIDToken, status := exchangeCode(t, p, "code")
	if status != http.StatusOK {
		t.Fatalf("token exchange status = %d, want %d", status, http.StatusOK)
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: oidctest.DefaultClientID})
	if _, err := verifier.Verify(ctx, rawIDToken); err == nil {
		t.Fatal("Verify succeeded for a token with a mismatched audience, want error")
	}
}

func TestClaims_ExpiresAtOverride(t *testing.T) {
	p := oidctest.NewProvider(t)
	p.RegisterCode("code", oidctest.Claims{
		Subject:   "user-1",
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	})

	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, p.URL)
	if err != nil {
		t.Fatalf("oidc.NewProvider: %v", err)
	}

	rawIDToken, status := exchangeCode(t, p, "code")
	if status != http.StatusOK {
		t.Fatalf("token exchange status = %d, want %d", status, http.StatusOK)
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: oidctest.DefaultClientID})
	if _, err := verifier.Verify(ctx, rawIDToken); err == nil {
		t.Fatal("Verify succeeded for an expired token, want error")
	} else if !strings.Contains(err.Error(), "expired") {
		t.Errorf("Verify error = %q, want it to mention expiry", err.Error())
	}
}

func TestProvider_ExpiresIn_IndependentOfIDTokenExpiry(t *testing.T) {
	p := oidctest.NewProvider(t)
	p.RegisterCode("code", oidctest.Claims{
		Subject:   "user-1",
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	})

	body, status := exchangeCodeFull(t, p, "code")
	if status != http.StatusOK {
		t.Fatalf("token exchange status = %d, want %d", status, http.StatusOK)
	}
	if body.ExpiresIn <= 0 {
		t.Errorf("ExpiresIn = %d, want a positive value independent of the past Claims.ExpiresAt", body.ExpiresIn)
	}
	if body.IDToken == "" {
		t.Fatal("token response missing id_token")
	}

	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, p.URL)
	if err != nil {
		t.Fatalf("oidc.NewProvider: %v", err)
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: oidctest.DefaultClientID})
	if _, err := verifier.Verify(ctx, body.IDToken); err == nil {
		t.Fatal("Verify succeeded for an expired id_token despite a positive expires_in, want error")
	}
}

func TestClaims_SigningKeyOverride(t *testing.T) {
	p := oidctest.NewProvider(t)

	other := generateTestKey(t)
	p.RegisterCode("code", oidctest.Claims{
		Subject:    "user-1",
		SigningKey: other,
	})

	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, p.URL)
	if err != nil {
		t.Fatalf("oidc.NewProvider: %v", err)
	}

	rawIDToken, status := exchangeCode(t, p, "code")
	if status != http.StatusOK {
		t.Fatalf("token exchange status = %d, want %d", status, http.StatusOK)
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: oidctest.DefaultClientID})
	if _, err := verifier.Verify(ctx, rawIDToken); err == nil {
		t.Fatal("Verify succeeded for a token signed with an unregistered key, want error")
	}
}

func TestClaims_EmailVerifiedNil_OmitsClaim(t *testing.T) {
	p := oidctest.NewProvider(t)
	p.RegisterCode("code", oidctest.Claims{
		Subject:       "user-1",
		Email:         "",
		EmailVerified: nil,
	})

	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, p.URL)
	if err != nil {
		t.Fatalf("oidc.NewProvider: %v", err)
	}

	rawIDToken, status := exchangeCode(t, p, "code")
	if status != http.StatusOK {
		t.Fatalf("token exchange status = %d, want %d", status, http.StatusOK)
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: oidctest.DefaultClientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		t.Fatalf("verifier.Verify: %v", err)
	}

	var raw map[string]any
	if err := idToken.Claims(&raw); err != nil {
		t.Fatalf("idToken.Claims: %v", err)
	}
	if _, ok := raw["email_verified"]; ok {
		t.Error("email_verified claim present, want it omitted when EmailVerified is nil")
	}
	if _, ok := raw["email"]; ok {
		t.Error("email claim present, want it omitted when Email is empty")
	}
}

func TestClaims_EmailVerifiedFalse_IsDistinctFromNil(t *testing.T) {
	p := oidctest.NewProvider(t)
	p.RegisterCode("code", oidctest.Claims{
		Subject:       "user-1",
		Email:         "a@example.com",
		EmailVerified: ptrFalse(),
	})

	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, p.URL)
	if err != nil {
		t.Fatalf("oidc.NewProvider: %v", err)
	}

	rawIDToken, status := exchangeCode(t, p, "code")
	if status != http.StatusOK {
		t.Fatalf("token exchange status = %d, want %d", status, http.StatusOK)
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: oidctest.DefaultClientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		t.Fatalf("verifier.Verify: %v", err)
	}

	var raw map[string]any
	if err := idToken.Claims(&raw); err != nil {
		t.Fatalf("idToken.Claims: %v", err)
	}
	v, ok := raw["email_verified"]
	if !ok {
		t.Fatal("email_verified claim absent, want present (false)")
	}
	if b, isBool := v.(bool); !isBool || b {
		t.Errorf("email_verified = %v, want false", v)
	}
}

func TestProvider_NonceRoundTrips(t *testing.T) {
	p := oidctest.NewProvider(t)
	p.RegisterCode("code", oidctest.Claims{
		Subject: "user-1",
		Nonce:   "expected-nonce",
	})

	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, p.URL)
	if err != nil {
		t.Fatalf("oidc.NewProvider: %v", err)
	}

	rawIDToken, status := exchangeCode(t, p, "code")
	if status != http.StatusOK {
		t.Fatalf("token exchange status = %d, want %d", status, http.StatusOK)
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: oidctest.DefaultClientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		t.Fatalf("verifier.Verify: %v", err)
	}
	if idToken.Nonce != "expected-nonce" {
		t.Errorf("Nonce = %q, want %q", idToken.Nonce, "expected-nonce")
	}
}

func TestProvider_UnregisteredCode_Rejected(t *testing.T) {
	p := oidctest.NewProvider(t)

	_, status := exchangeCode(t, p, "never-registered")
	if status == http.StatusOK {
		t.Fatal("exchange of an unregistered code succeeded, want failure")
	}
}

func TestProvider_ClientIDOverride(t *testing.T) {
	p := oidctest.NewProvider(t)
	p.ClientID = "custom-client-id"
	p.RegisterCode("code", oidctest.Claims{Subject: "user-1"})

	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, p.URL)
	if err != nil {
		t.Fatalf("oidc.NewProvider: %v", err)
	}

	rawIDToken, status := exchangeCode(t, p, "code")
	if status != http.StatusOK {
		t.Fatalf("token exchange status = %d, want %d", status, http.StatusOK)
	}

	overriddenVerifier := provider.Verifier(&oidc.Config{ClientID: "custom-client-id"})
	if _, err := overriddenVerifier.Verify(ctx, rawIDToken); err != nil {
		t.Errorf("Verify against the overridden ClientID failed: %v, want success", err)
	}

	defaultVerifier := provider.Verifier(&oidc.Config{ClientID: oidctest.DefaultClientID})
	if _, err := defaultVerifier.Verify(ctx, rawIDToken); err == nil {
		t.Error("Verify against DefaultClientID succeeded for a token whose audience is the overridden ClientID, want error")
	}
}

func exchangeViaOAuth2Config(t *testing.T, p *oidctest.Provider, code, verifier string) (*oauth2.Token, error) {
	t.Helper()

	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, p.URL)
	if err != nil {
		t.Fatalf("oidc.NewProvider: %v", err)
	}
	cfg := oauth2.Config{
		ClientID: p.ClientID,
		Endpoint: provider.Endpoint(),
	}

	var opts []oauth2.AuthCodeOption
	if verifier != "" {
		opts = append(opts, oauth2.VerifierOption(verifier))
	}
	return cfg.Exchange(ctx, code, opts...)
}

func TestProvider_PKCE_AcceptsMatchingVerifier(t *testing.T) {
	p := oidctest.NewProvider(t)

	verifier := oauth2.GenerateVerifier()
	p.RegisterCode("code", oidctest.Claims{
		Subject:       "user-1",
		CodeChallenge: oauth2.S256ChallengeFromVerifier(verifier),
	})

	token, err := exchangeViaOAuth2Config(t, p, "code", verifier)
	if err != nil {
		t.Fatalf("Exchange with the matching code_verifier failed: %v, want success", err)
	}
	if token.AccessToken == "" {
		t.Error("token response missing access_token")
	}
}

func TestProvider_PKCE_RejectsMismatchedVerifier(t *testing.T) {
	p := oidctest.NewProvider(t)

	correctVerifier := oauth2.GenerateVerifier()
	wrongVerifier := oauth2.GenerateVerifier()
	p.RegisterCode("code", oidctest.Claims{
		Subject:       "user-1",
		CodeChallenge: oauth2.S256ChallengeFromVerifier(correctVerifier),
	})

	_, err := exchangeViaOAuth2Config(t, p, "code", wrongVerifier)
	if err == nil {
		t.Fatal("Exchange with a mismatched code_verifier succeeded, want failure")
	}
	var retrieveErr *oauth2.RetrieveError
	if errors.As(err, &retrieveErr) && retrieveErr.ErrorCode != "invalid_grant" {
		t.Errorf("RetrieveError.ErrorCode = %q, want %q", retrieveErr.ErrorCode, "invalid_grant")
	}
}

func TestProvider_PKCE_RejectsMissingVerifierWhenChallengeRegistered(t *testing.T) {
	p := oidctest.NewProvider(t)

	p.RegisterCode("code", oidctest.Claims{
		Subject:       "user-1",
		CodeChallenge: oauth2.S256ChallengeFromVerifier(oauth2.GenerateVerifier()),
	})

	_, err := exchangeViaOAuth2Config(t, p, "code", "")
	if err == nil {
		t.Fatal("Exchange with no code_verifier succeeded despite a registered code_challenge, want failure")
	}
}

func TestProvider_PKCE_NotRequiredWhenChallengeUnregistered(t *testing.T) {
	p := oidctest.NewProvider(t)
	p.RegisterCode("code", oidctest.Claims{Subject: "user-1"})

	_, err := exchangeViaOAuth2Config(t, p, "code", "")
	if err != nil {
		t.Errorf("Exchange with no code_verifier and no registered CodeChallenge failed: %v, want success", err)
	}
}

// x/oauth2 fills RetrieveError.ErrorCode from a JSON error body and parses a text/plain body as form data.
func TestProvider_TokenError_IsJSONWithErrorCode(t *testing.T) {
	p := oidctest.NewProvider(t)

	_, err := exchangeViaOAuth2Config(t, p, "never-registered", "")
	if err == nil {
		t.Fatal("Exchange of an unregistered code succeeded, want failure")
	}
	var retrieveErr *oauth2.RetrieveError
	if !errors.As(err, &retrieveErr) {
		t.Fatalf("error = %v (%T), want *oauth2.RetrieveError", err, err)
	}
	if retrieveErr.ErrorCode != "invalid_grant" {
		t.Errorf("RetrieveError.ErrorCode = %q, want %q (Content-Type must be application/json for this to populate)", retrieveErr.ErrorCode, "invalid_grant")
	}
}
