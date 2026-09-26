// These tests pin LinkedIn's documented confidential web flow: no PKCE, client
// credentials in the token request body, the OIDC nonce as the code-injection
// defense, and LinkedIn's cancel errors. See docs/design/linkedin-sign-in.md
// and docs/adr/0058-linkedin-sign-in-in-production.md.
package auth_test

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/auth/oidctest"
)

// linkedinDiscoverySnapshot is LinkedIn's public discovery document as served
// at https://www.linkedin.com/oauth/.well-known/openid-configuration on
// 2026-09-26.
//
//go:embed testdata/linkedin-discovery.json
var linkedinDiscoverySnapshot []byte

// linkedinTestClientSecret matches the secret newTestService configures.
const linkedinTestClientSecret = "test-linkedin-client-secret"

// TestLinkedInStart_AuthorizeURL_NonceWithoutPKCE proves the authorize URL
// carries the documented parameters and a nonce, and no PKCE challenge.
func TestLinkedInStart_AuthorizeURL_NonceWithoutPKCE(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	start := doGet(t, handler, auth.LinkedInStartPath) //nolint:bodyclose // doGet closes the body itself before returning.
	if start.StatusCode != http.StatusFound {
		t.Fatalf("GET %s status = %d, want %d", auth.LinkedInStartPath, start.StatusCode, http.StatusFound)
	}
	loc, err := url.Parse(start.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse start redirect Location: %v", err)
	}
	q := loc.Query()

	want := map[string]string{
		"response_type": "code",
		"client_id":     oidctest.DefaultClientID,
		"redirect_uri":  testPublicOrigin + auth.LinkedInCallbackPath,
		"scope":         "openid profile email",
	}
	for name, value := range want {
		if got := q.Get(name); got != value {
			t.Errorf("authorize URL %s = %q, want %q", name, got, value)
		}
	}
	for _, name := range []string{"state", "nonce"} {
		if q.Get(name) == "" {
			t.Errorf("authorize URL has no %s, want a per-transaction value", name)
		}
	}
	for _, name := range []string{"code_challenge", "code_challenge_method"} {
		if q.Has(name) {
			t.Errorf("authorize URL carries %s=%q, want none: LinkedIn's documented web flow has no PKCE", name, q.Get(name))
		}
	}
}

// TestLinkedInCallback_TokenRequest_FormCredentialsWithoutVerifier proves the
// token request sends client_id and client_secret in the form body, no
// Authorization header, and no code_verifier, in exactly one request.
func TestLinkedInCallback_TokenRequest_FormCredentialsWithoutVerifier(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	txCookie, state, nonce := beginLinkedIn(t, handler)
	p.RegisterCode("code-token-shape", oidctest.Claims{
		Subject: uniqueLinkedInSubject(t), Email: uniqueEmail(t), EmailVerified: ptrTrue(), Nonce: nonce,
	})

	resp := doLinkedInCallback(t, handler, "code-token-shape", state, txCookie) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.
	assertLinkedInLoginAccepted(t, resp)

	req, count := p.LastTokenRequest()
	if count != 1 {
		t.Errorf("token requests = %d, want exactly 1 (no retry with another client authentication style)", count)
	}
	if req.Authorization != "" {
		t.Errorf("token request Authorization header is set, want none: LinkedIn documents client credentials in the form body")
	}
	want := map[string]string{
		"grant_type":    "authorization_code",
		"code":          "code-token-shape",
		"redirect_uri":  testPublicOrigin + auth.LinkedInCallbackPath,
		"client_id":     oidctest.DefaultClientID,
		"client_secret": linkedinTestClientSecret,
	}
	for name, value := range want {
		if got := req.Form[name]; len(got) != 1 || got[0] != value {
			t.Errorf("token request form %s = %q, want exactly [%q]", name, got, value)
		}
	}
	if req.Form.Has("code_verifier") {
		t.Error("token request carries code_verifier, want none: LinkedIn's token endpoint rejects it from a confidential client")
	}
}

// TestLinkedInCallback_CancelErrors_RedirectCancelled proves each LinkedIn
// cancel error and access_denied give the canceled result after the state
// check, and never reach the token endpoint.
func TestLinkedInCallback_CancelErrors_RedirectCancelled(t *testing.T) {
	t.Parallel()

	for _, providerError := range []string{"user_cancelled_login", "user_cancelled_authorize", "access_denied"} { //nolint:misspell // LinkedIn's exact wire values.
		t.Run(providerError, func(t *testing.T) {
			t.Parallel()

			p := oidctest.NewProvider(t)
			handler, _ := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

			txCookie, state, _ := beginLinkedIn(t, handler)
			path := auth.LinkedInCallbackPath + "?error=" + url.QueryEscape(providerError) +
				"&error_description=" + url.QueryEscape("The member left the consent page") + "&state=" + url.QueryEscape(state)
			resp := doGet(t, handler, path, txCookie) //nolint:bodyclose // doGet closes the body itself before returning.

			if got := assertRejected(t, resp); got != "cancelled" { //nolint:misspell // Exact wire value uses double-L "cancelled".
				t.Errorf("error code = %q, want %q", got, "cancelled") //nolint:misspell // same wire value as above
			}
			if _, count := p.LastTokenRequest(); count != 0 {
				t.Errorf("token requests = %d, want 0 after a cancel", count)
			}
		})
	}
}

// TestLinkedInCallback_CancelError_StateCheckedFirst proves a cancel error
// with a wrong state is a generic failure, not a cancel.
func TestLinkedInCallback_CancelError_StateCheckedFirst(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	txCookie, _, _ := beginLinkedIn(t, handler)
	path := auth.LinkedInCallbackPath + "?error=user_cancelled_login&state=attacker-supplied-wrong-state" //nolint:misspell // LinkedIn's exact wire value.
	resp := doGet(t, handler, path, txCookie)                                                             //nolint:bodyclose // doGet closes the body itself before returning.

	if got := assertRejected(t, resp); got != "auth_failed" {
		t.Errorf("error code = %q, want %q (state is checked before the provider error)", got, "auth_failed")
	}
}

// TestLinkedInCallback_OtherProviderError_GenericFailure proves any other
// error value is the generic failure, even when a code is also present.
func TestLinkedInCallback_OtherProviderError_GenericFailure(t *testing.T) {
	t.Parallel()

	for _, providerError := range []string{"server_error", "invalid_scope", "unauthorized_scope_error", "user_cancelled"} { //nolint:misspell // a near-miss of LinkedIn's wire values.
		t.Run(providerError, func(t *testing.T) {
			t.Parallel()

			p := oidctest.NewProvider(t)
			handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

			subject := uniqueLinkedInSubject(t)
			email := uniqueEmail(t)
			txCookie, state, nonce := beginLinkedIn(t, handler)
			p.RegisterCode("code-with-error", oidctest.Claims{
				Subject: subject, Email: email, EmailVerified: ptrTrue(), Nonce: nonce,
			})
			path := auth.LinkedInCallbackPath + "?error=" + url.QueryEscape(providerError) +
				"&code=code-with-error&state=" + url.QueryEscape(state)
			resp := doGet(t, handler, path, txCookie) //nolint:bodyclose // doGet closes the body itself before returning.

			if got := assertRejected(t, resp); got != "auth_failed" {
				t.Errorf("error code = %q, want %q", got, "auth_failed")
			}
			if _, count := p.LastTokenRequest(); count != 0 {
				t.Errorf("token requests = %d, want 0 when the provider reports an error", count)
			}
			assertNoLinkedInIdentity(t, q, subject)
			assertNoUser(t, q, email)
		})
	}
}

// TestLinkedInCallback_NonceCheckedAfterExchange proves the nonce, not PKCE,
// stops an injected code: the exchange succeeds, the ID token verifies, and
// the callback still rejects a missing or foreign nonce without writing.
func TestLinkedInCallback_NonceCheckedAfterExchange(t *testing.T) {
	t.Parallel()

	for name, nonceFor := range map[string]func(real string) string{
		"foreign nonce": func(string) string { return "nonce-from-another-transaction" },
		"missing nonce": func(string) string { return "" },
		"prefix nonce":  func(real string) string { return real[:len(real)-1] },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p := oidctest.NewProvider(t)
			logger, logBuf := newCapturingLogger()
			handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL), withLogger(logger))

			subject := uniqueLinkedInSubject(t)
			email := uniqueEmail(t)
			txCookie, state, nonce := beginLinkedIn(t, handler)
			p.RegisterCode("code-injected", oidctest.Claims{
				Subject: subject, Email: email, EmailVerified: ptrTrue(), Nonce: nonceFor(nonce),
			})

			resp := doLinkedInCallback(t, handler, "code-injected", state, txCookie) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.

			if got := assertRejected(t, resp); got != "auth_failed" {
				t.Errorf("error code = %q, want %q", got, "auth_failed")
			}
			if _, count := p.LastTokenRequest(); count != 1 {
				t.Errorf("token requests = %d, want 1 (the exchange itself succeeds; the nonce check rejects)", count)
			}
			if !containsLogReason(logBuf.Bytes(), "nonce_mismatch") {
				t.Errorf("log = %q, want reason nonce_mismatch", logBuf.String())
			}
			assertNoLinkedInIdentity(t, q, subject)
			assertNoUser(t, q, email)
		})
	}
}

// TestLinkedInDiscoverySnapshot_MatchesIssuerConstant serves LinkedIn's
// committed discovery document and discovers it with the issuer constant. It
// fails if LinkedIn's issuer and the constant disagree.
func TestLinkedInDiscoverySnapshot_MatchesIssuerConstant(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(linkedinDiscoverySnapshot); err != nil {
			t.Errorf("write discovery snapshot: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	ctx := oidc.InsecureIssuerURLContext(context.Background(), auth.LinkedInIssuerForTest)
	provider, err := oidc.NewProvider(ctx, srv.URL)
	if err != nil {
		t.Fatalf("oidc.NewProvider(snapshot, issuer %q) error = %v, want the snapshot's issuer to equal the constant", auth.LinkedInIssuerForTest, err)
	}

	endpoint := provider.Endpoint()
	if endpoint.AuthURL != "https://www.linkedin.com/oauth/v2/authorization" {
		t.Errorf("authorization endpoint = %q, want LinkedIn's documented URL (the web allowlist holds it)", endpoint.AuthURL)
	}
	if endpoint.TokenURL != "https://www.linkedin.com/oauth/v2/accessToken" {
		t.Errorf("token endpoint = %q, want LinkedIn's documented URL", endpoint.TokenURL)
	}

	var meta struct {
		ScopesSupported []string `json:"scopes_supported"`
		ClaimsSupported []string `json:"claims_supported"`
	}
	if err := provider.Claims(&meta); err != nil {
		t.Fatalf("decode discovery metadata: %v", err)
	}
	for _, scope := range []string{"openid", "profile", "email"} {
		if !slices.Contains(meta.ScopesSupported, scope) {
			t.Errorf("scopes_supported = %q, want it to include %q", meta.ScopesSupported, scope)
		}
	}
	for _, claim := range []string{"sub", "email", "email_verified", "name"} {
		if !slices.Contains(meta.ClaimsSupported, claim) {
			t.Errorf("claims_supported = %q, want it to include %q", meta.ClaimsSupported, claim)
		}
	}
}

// containsLogReason reports whether any JSON log line carries reason.
func containsLogReason(logs []byte, reason string) bool {
	for _, line := range bytes.Split(logs, []byte("\n")) {
		var rec struct {
			Reason string `json:"reason"`
		}
		if json.Unmarshal(line, &rec) == nil && rec.Reason == reason {
			return true
		}
	}
	return false
}
