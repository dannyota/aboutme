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

	"github.com/dannyota/aboutme/apps/server/internal/auth/oidctest"
)

func ptrTrue() *bool {
	b := true
	return &b
}

func ptrFalse() *bool {
	b := false
	return &b
}

// generateTestKey returns a fresh RSA-2048 key distinct from any
// Provider's own key, for tests that need a signing key the Provider
// never registered.
func generateTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating test RSA key: %v", err)
	}
	return key
}

// tokenResponseBody mirrors the /token endpoint's JSON success shape.
// Shared by exchangeCode and tests that need fields beyond id_token (e.g.
// expires_in).
type tokenResponseBody struct {
	IDToken     string `json:"id_token"`
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

// exchangeCodeFull POSTs code to p's /token endpoint directly (no real
// browser redirect, no oauth2.Config — this package's self-tests exercise
// the HTTP surface and go-jose signing directly) and returns the decoded
// response body. On a non-200 status it returns the zero body.
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

// exchangeCode is exchangeCodeFull's common-case sibling: it also asserts
// the wire-protocol fields every 200 response must carry, and returns just
// the id_token most tests care about.
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

// TestProvider_DiscoveryAndTokenRoundTrip proves the harness passes real
// go-oidc discovery, exchange, signature, issuer, and audience checks.
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

// TestProvider_RegisterCode_SingleUse proves an authorization code is
// consumed on its first exchange, matching a real authorization server's
// single-use codes — the property later Google/LinkedIn callback tests
// rely on implicitly (a replayed callback must not mint a second
// session).
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

// TestClaims_IssuerOverride proves Claims.Issuer overrides the default
// (the Provider's own URL) and that go-oidc's own verification — not this
// test — rejects the resulting issuer mismatch. Callback tests use this
// mechanism to exercise wrong-issuer rejection.
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

// TestClaims_AudienceOverride proves Claims.Audience overrides the
// default (the Provider's registered client id) and that go-oidc rejects
// a token whose audience doesn't match the verifier's configured
// ClientID.
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

// TestClaims_ExpiresAtOverride proves Claims.ExpiresAt overrides the
// default (now+1h) and that go-oidc rejects an already-expired token.
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

// TestProvider_ExpiresIn_IndependentOfIDTokenExpiry proves OAuth2 access-token
// lifetime does not derive from the ID token's exp claim.
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

// TestClaims_SigningKeyOverride proves Claims.SigningKey overrides the
// default (the Provider's own key) and that go-oidc rejects a token
// signed with a key other than the one published at /jwks.json — the
// harness's stand-in for a tampered or forged signature.
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

// TestClaims_EmailVerifiedNil_OmitsClaim proves the documented "nil =
// omit the claim entirely" contract for EmailVerified (LinkedIn's
// optional-email case), and that an empty Email is likewise omitted
// rather than encoded as an empty string.
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

// TestClaims_EmailVerifiedFalse_IsDistinctFromNil proves EmailVerified
// round-trips false (and is present, unlike the nil case) — guards
// against a harness bug that would silently collapse "explicitly false"
// and "absent" into the same wire representation.
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

// TestProvider_NonceRoundTrips proves Claims.Nonce is carried onto the
// signed token unchanged. go-oidc does not validate nonce itself
// (verify.go's Verify never inspects it); this only proves the harness
// carries the value through so an application-level nonce check has
// something real to compare against.
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

// TestProvider_UnregisteredCode_Rejected proves the token endpoint
// rejects a code that was never registered, not just a previously-used
// one.
func TestProvider_UnregisteredCode_Rejected(t *testing.T) {
	p := oidctest.NewProvider(t)

	_, status := exchangeCode(t, p, "never-registered")
	if status == http.StatusOK {
		t.Fatal("exchange of an unregistered code succeeded, want failure")
	}
}

// TestProvider_ClientIDOverride proves Provider.ClientID can be changed
// from its DefaultClientID default, that Claims.Audience then defaults to
// the overridden value (not the original DefaultClientID) when signing a
// token, and that go-oidc's own audience check enforces this: a verifier
// configured with the overridden ClientID accepts the token, one still
// configured with DefaultClientID rejects it. This lets tests point a Service
// at a provider registered under a non-default client ID.
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

// exchangeViaOAuth2Config builds a real oauth2.Config from p's discovery
// endpoint (via go-oidc, exactly as production code does) and exchanges
// code through it -- proving the mock is compatible with the actual
// oauth2.Config.Exchange call path, not just a hand-rolled HTTP POST (see
// exchangeCodeFull), and, when verifier is non-empty, exercising the PKCE
// code_verifier send end to end.
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

// TestProvider_PKCE_AcceptsMatchingVerifier proves the /token endpoint
// accepts an exchange whose code_verifier hashes (S256) to the
// code_challenge registered for the code. It uses the same
// oauth2.Config.Exchange path as the production OIDC providers.
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

// TestProvider_PKCE_RejectsMismatchedVerifier proves the /token endpoint
// rejects an exchange whose code_verifier does NOT hash to the registered
// code_challenge -- this is the property that makes the PKCE send in
// google.go/linkedin.go's Exchange call actually proven, not merely
// assumed to be sent.
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

// TestProvider_PKCE_RejectsMissingVerifierWhenChallengeRegistered proves a
// code registered with a CodeChallenge cannot be exchanged with no
// code_verifier at all -- PKCE must not be silently skippable just by
// omitting it.
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

// TestProvider_PKCE_NotRequiredWhenChallengeUnregistered proves PKCE is opt-in
// per registered code.
func TestProvider_PKCE_NotRequiredWhenChallengeUnregistered(t *testing.T) {
	p := oidctest.NewProvider(t)
	p.RegisterCode("code", oidctest.Claims{Subject: "user-1"})

	_, err := exchangeViaOAuth2Config(t, p, "code", "")
	if err != nil {
		t.Errorf("Exchange with no code_verifier and no registered CodeChallenge failed: %v, want success", err)
	}
}

// TestProvider_TokenError_IsJSONWithErrorCode proves JSON errors populate
// oauth2.RetrieveError.ErrorCode; text/plain would be parsed as form data.
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
