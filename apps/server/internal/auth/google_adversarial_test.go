// These adversarial tests drive the real Google start and callback handlers.
// They cover OIDC verification, state and transaction binding, verified email,
// rejection cookie effects, and no-oracle behavior. See docs/design/security.md.
package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/auth/oidctest"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func ptrFalse() *bool {
	b := false
	return &b
}

// generateOtherRSAKey returns a fresh RSA-2048 key distinct from any
// oidctest.Provider's own key, for TestGoogleCallback_RejectsTamperedSignature.
func generateOtherRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating test RSA key: %v", err)
	}
	return key
}

// doGetCaptureBody retains the body for no-oracle comparisons.
func doGetCaptureBody(t *testing.T, handler http.Handler, path string, cookies ...*http.Cookie) (*http.Response, string) {
	t.Helper()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("read response body: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("close response body: %v", err)
	}
	return resp, string(body)
}

// ==== flow helpers ====

// beginGoogle returns the transaction cookie, state, and server-generated nonce
// from a real start redirect. Failure tests reuse the nonce to isolate one
// verification dimension.
func beginGoogle(t *testing.T, handler http.Handler) (txCookie *http.Cookie, state, nonce string) {
	t.Helper()

	start := doGet(t, handler, auth.GoogleStartPath) //nolint:bodyclose // doGet (handlers_test.go) closes the body itself before returning.
	if start.StatusCode != http.StatusFound {
		t.Fatalf("GET %s status = %d, want %d (a redirect to the provider)", auth.GoogleStartPath, start.StatusCode, http.StatusFound)
	}

	txCookie = extractCookie(start, auth.OAuthTxCookieName)
	if txCookie == nil {
		t.Fatalf("GET %s did not set the %s cookie", auth.GoogleStartPath, auth.OAuthTxCookieName)
	}

	loc := start.Header.Get("Location")
	state = mustQueryParam(t, loc, "state")
	nonce = mustQueryParam(t, loc, "nonce")

	return txCookie, state, nonce
}

// doCallback drives GET auth.GoogleCallbackPath?code=...&state=... (via
// the shared doGet), attaching cookies (typically just the __Host-oauth-tx
// cookie beginGoogle returned; omitted entirely for
// TestGoogleCallback_RejectsMissingTxCookie's case).
func doCallback(t *testing.T, handler http.Handler, code, state string, cookies ...*http.Cookie) *http.Response {
	t.Helper()

	path := auth.GoogleCallbackPath + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state)
	return doGet(t, handler, path, cookies...) //nolint:bodyclose // doGet (handlers_test.go) closes the body itself before returning.
}

// ==== shared assertions ====

// assertRejected checks the redirect, error code, absent session cookie, and
// cleared transaction cookie. It returns the client-facing error code.
func assertRejected(t *testing.T, resp *http.Response) (errorCode string) {
	t.Helper()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want %d (every /callback rejection is a redirect, never a raw JSON error or a 200)", resp.StatusCode, http.StatusFound)
	}

	loc := resp.Header.Get("Location")
	errorCode = mustQueryParam(t, loc, "error")
	assertRedirectPath(t, loc, "/login") // Rejections use the login error page, not the success target.

	if sc := extractCookie(resp, auth.SessionCookieName); sc != nil {
		t.Errorf("response set a %s cookie on a rejected callback (value=%q), want none -- a rejected callback must never authenticate the visitor", auth.SessionCookieName, sc.Value)
	}

	tx := extractCookie(resp, auth.OAuthTxCookieName)
	if tx == nil {
		t.Fatalf("response did not clear the %s cookie on a rejected callback (no Set-Cookie for it at all) -- DD-C4 requires ClearOAuthTxCookie on every failure path", auth.OAuthTxCookieName)
	}
	if tx.MaxAge >= 0 {
		t.Errorf("%s cookie MaxAge = %d on a rejected callback, want negative (cleared, matching auth.ClearOAuthTxCookie)", auth.OAuthTxCookieName, tx.MaxAge)
	}
	if tx.Value != "" {
		t.Errorf("%s cookie Value = %q on a rejected callback, want empty (cleared)", auth.OAuthTxCookieName, tx.Value)
	}

	return errorCode
}

// assertNoIdentity asserts no identities row exists for
// (provider=google, provider_user_id=providerUserID) -- the row a
// successful callback would have created.
func assertNoIdentity(t *testing.T, q *store.Queries, providerUserID string) {
	t.Helper()

	_, err := q.GetIdentityByProviderSubject(context.Background(), store.GetIdentityByProviderSubjectParams{
		Provider:       string(auth.ProviderGoogle),
		ProviderUserID: providerUserID,
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("GetIdentityByProviderSubject(google, %q) error = %v, want pgx.ErrNoRows (no identity row may be created on a rejected callback)", providerUserID, err)
	}
}

// assertNoUser asserts no users row exists with the given email -- the row
// a successful new-user callback would have created.
func assertNoUser(t *testing.T, q *store.Queries, email string) {
	t.Helper()

	_, err := q.GetUserByEmail(context.Background(), email)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("GetUserByEmail(%q) error = %v, want pgx.ErrNoRows (no user row may be created on a rejected callback)", email, err)
	}
}

// uniqueSubject and uniqueEmail prevent shared-database collisions.
func uniqueSubject(t *testing.T) string {
	t.Helper()
	return "g-sub-" + uuid.NewString()
}

func uniqueEmail(t *testing.T) string {
	t.Helper()
	return uuid.NewString() + "@example.com"
}

// ==== OIDC rejection matrix ====

// TestGoogleCallback_RejectsUnverifiedEmail proves Google registration requires
// email_verified=true and returns the distinct user-actionable code.
func TestGoogleCallback_RejectsUnverifiedEmail(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	txCookie, state, nonce := beginGoogle(t, handler)
	p.RegisterCode("code-unverified", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrFalse(),
		Nonce:         nonce, // must pass every OTHER check so only email_verified fails
	})

	resp := doCallback(t, handler, "code-unverified", state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	errorCode := assertRejected(t, resp)
	if errorCode != "email_not_verified" {
		t.Errorf("error code = %q, want %q (design spec §3: Google requires email_verified == true)", errorCode, "email_not_verified")
	}

	assertNoIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// TestGoogleCallback_RejectsWrongIssuer proves the HTTP flow preserves
// go-oidc's issuer rejection and leaves no credential or database effect.
func TestGoogleCallback_RejectsWrongIssuer(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	txCookie, state, nonce := beginGoogle(t, handler)
	p.RegisterCode("code-wrong-issuer", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
		Issuer:        "https://evil.example",
	})

	resp := doCallback(t, handler, "code-wrong-issuer", state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// TestGoogleCallback_RejectsWrongAudience proves go-oidc rejects a token
// whose "aud" doesn't match the Service's configured GoogleClientID.
func TestGoogleCallback_RejectsWrongAudience(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	txCookie, state, nonce := beginGoogle(t, handler)
	p.RegisterCode("code-wrong-audience", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
		Audience:      "some-other-client-id",
	})

	resp := doCallback(t, handler, "code-wrong-audience", state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// TestGoogleCallback_RejectsTamperedSignature proves go-oidc rejects a
// token signed with a key other than the one the provider published at
// /jwks.json -- the harness's stand-in for a forged or tampered id_token.
func TestGoogleCallback_RejectsTamperedSignature(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	txCookie, state, nonce := beginGoogle(t, handler)
	p.RegisterCode("code-tampered-signature", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
		SigningKey:    generateOtherRSAKey(t),
	})

	resp := doCallback(t, handler, "code-tampered-signature", state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// TestGoogleCallback_RejectsNonceMismatch targets the manual check go-oidc does
// not perform. The fixture discards the transaction nonce and signs a fixed,
// unrelated value.
func TestGoogleCallback_RejectsNonceMismatch(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	txCookie, state, _ := beginGoogle(t, handler) // the real nonce is deliberately discarded below
	p.RegisterCode("code-nonce-mismatch", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         "wrong-nonce-does-not-match-the-real-transaction",
	})

	resp := doCallback(t, handler, "code-nonce-mismatch", state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// TestGoogleCallback_RejectsExpiredIDToken proves go-oidc rejects a token
// whose "exp" is already in the past. oidctest's
// TestProvider_ExpiresIn_IndependentOfIDTokenExpiry
// guards that the mock's OAuth2 access-token expires_in can't
// accidentally launder this past exp at the token-exchange layer, so a
// rejection observed here can only be go-oidc's own id_token expiry check.
func TestGoogleCallback_RejectsExpiredIDToken(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	txCookie, state, nonce := beginGoogle(t, handler)
	p.RegisterCode("code-expired", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
		ExpiresAt:     time.Now().Add(-1 * time.Hour),
	})

	resp := doCallback(t, handler, "code-expired", state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// ==== transaction binding and no-oracle checks ====

// TestGoogleCallback_RejectsStateMismatch prevents an attacker-controlled code
// from being bound to the victim's valid transaction cookie.
func TestGoogleCallback_RejectsStateMismatch(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	txCookie, _, nonce := beginGoogle(t, handler) // deliberately discard the real state
	p.RegisterCode("code-state-mismatch", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
	})

	resp := doCallback(t, handler, "code-state-mismatch", "attacker-supplied-wrong-state", txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// TestGoogleCallback_RejectsMissingTxCookie: a /callback request that
// carries a plausible code and state but no __Host-oauth-tx cookie at all
// (e.g. a bookmarked/replayed callback URL, or a cookie dropped by a
// privacy setting) must be rejected the same way any other invalid
// transaction is -- never treated as "no transaction to check against, so
// skip straight to token exchange".
func TestGoogleCallback_RejectsMissingTxCookie(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	_, state, nonce := beginGoogle(t, handler) // begin a real transaction, but never attach its cookie below
	p.RegisterCode("code-missing-cookie", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
	})

	resp := doCallback(t, handler, "code-missing-cookie", state) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// TestGoogleCallback_OIDCFailures_NoOracleAcrossFailureModes requires identical
// error codes and bodies across issuer, audience, signature, nonce, and expiry
// failures.
func TestGoogleCallback_OIDCFailures_NoOracleAcrossFailureModes(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL))

	otherKey := generateOtherRSAKey(t)

	// Every case except nonce mismatch echoes the real nonce so exactly one
	// verification dimension fails.
	cases := map[string]func(nonce string) oidctest.Claims{
		"wrong issuer": func(nonce string) oidctest.Claims {
			return oidctest.Claims{
				Subject: uniqueSubject(t), Email: uniqueEmail(t),
				EmailVerified: ptrTrue(), Nonce: nonce, Issuer: "https://evil.example",
			}
		},
		"wrong audience": func(nonce string) oidctest.Claims {
			return oidctest.Claims{
				Subject: uniqueSubject(t), Email: uniqueEmail(t),
				EmailVerified: ptrTrue(), Nonce: nonce, Audience: "some-other-client-id",
			}
		},
		"tampered signature": func(nonce string) oidctest.Claims {
			return oidctest.Claims{
				Subject: uniqueSubject(t), Email: uniqueEmail(t),
				EmailVerified: ptrTrue(), Nonce: nonce, SigningKey: otherKey,
			}
		},
		"nonce mismatch": func(string) oidctest.Claims {
			return oidctest.Claims{
				Subject: uniqueSubject(t), Email: uniqueEmail(t),
				EmailVerified: ptrTrue(), Nonce: "wrong-nonce-does-not-match-the-real-transaction",
			}
		},
		"expired id token": func(nonce string) oidctest.Claims {
			return oidctest.Claims{
				Subject: uniqueSubject(t), Email: uniqueEmail(t),
				EmailVerified: ptrTrue(), Nonce: nonce, ExpiresAt: time.Now().Add(-1 * time.Hour),
			}
		},
	}

	errorCodes := make(map[string]string, len(cases))
	bodies := make(map[string]string, len(cases))
	for name, buildClaims := range cases {
		code := "code-oracle-" + name

		txCookie, state, nonce := beginGoogle(t, handler)
		p.RegisterCode(code, buildClaims(nonce))

		path := auth.GoogleCallbackPath + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state)
		resp, body := doGetCaptureBody(t, handler, path, txCookie) //nolint:bodyclose // doGetCaptureBody closes the body itself before returning.

		errorCodes[name] = assertRejected(t, resp)
		bodies[name] = body
	}

	var firstName, firstCode, firstBody string
	for name, code := range errorCodes {
		if firstName == "" {
			firstName, firstCode, firstBody = name, code, bodies[name]
			continue
		}
		if code != firstCode {
			t.Errorf("error code for %q = %q, want %q (same as %q) -- distinct OIDC failure classes must not be distinguishable to the browser", name, code, firstCode, firstName)
		}
		if bodies[name] != firstBody {
			t.Errorf("response body for %q differs from %q's -- distinct OIDC failure classes must not produce distinguishable response bodies", name, firstName)
		}
	}

	if firstCode == "email_not_verified" {
		t.Error("OIDC verification failures reused the email_not_verified error code, want a distinct generic code -- these five are cryptographic/protocol failures, not the email_verified claim check, and conflating them would misreport the failure to the visitor")
	}

	// Equal bodies alone do not prove that none contains a shared leak.
	forbidden := []string{"issuer", "audience", "signature", "nonce", "expired", "expire"}
	for name, body := range bodies {
		lower := strings.ToLower(body)
		for _, word := range forbidden {
			if strings.Contains(lower, word) {
				t.Errorf("%s: response body contains %q, want no OIDC-verification internals leaked to the browser (body=%q)", name, word, body)
			}
		}
	}
}
