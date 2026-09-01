// These adversarial tests cover LinkedIn's verified-email rule, link carve-out,
// OIDC and transaction checks, no-oracle behavior, and consent denial. They use
// the real callback handler. See docs/design/security.md.
package auth_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/auth/oidctest"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// assertLinkedInLoginAccepted checks the successful login shape: redirect to
// the public root, issue a non-empty session cookie, and clear the transaction
// cookie. Link callbacks have a different success shape and do not use it.
func assertLinkedInLoginAccepted(t *testing.T, resp *http.Response) {
	t.Helper()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want %d (redirect on a successful login)", resp.StatusCode, http.StatusFound)
	}
	if got := resp.Header.Get("Location"); got != testPublicOrigin+"/app/resumes" {
		t.Errorf("callback Location = %q, want %q (a successful callback redirects to the resume list, never /login)", got, testPublicOrigin+"/app/resumes")
	}

	sessionCookie := extractCookie(resp, auth.SessionCookieName)
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatal("callback response missing a non-empty __Host-session cookie on a successful login")
	}

	tx := extractCookie(resp, auth.OAuthTxCookieName)
	if tx == nil {
		t.Fatal("callback response missing a __Host-oauth-tx Set-Cookie header clearing it on success (ruling 1)")
	}
	if tx.MaxAge >= 0 {
		t.Errorf("__Host-oauth-tx cookie MaxAge = %d on a successful callback, want negative (cleared)", tx.MaxAge)
	}
}

// ==== registration email rule ====

// TestLinkedInCallback_RegistrationEmailRuleAdversarial covers verified,
// unverified, absent-verification, and absent-email claims. Each case uses a
// unique email, subject, and provider so rows in the shared live test database
// cannot make a rejected case appear successful.
//
// A nil EmailVerified claim is never treated as true.
func TestLinkedInCallback_RegistrationEmailRuleAdversarial(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		emailPresent  bool
		emailVerified *bool // nil = claim absent -- never treated as true
		wantCreated   bool
	}{
		{"verified email present", true, ptrTrue(), true},
		{"unverified email present", true, ptrFalse(), false},
		{"email present, verified claim absent", true, nil, false},
		{"email absent entirely", false, nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := oidctest.NewProvider(t)
			handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

			subject := uniqueLinkedInSubject(t)
			var email string
			if tc.emailPresent {
				email = uniqueEmail(t)
			}

			txCookie, state, nonce := beginLinkedIn(t, handler)
			p.RegisterCode("code-"+tc.name, oidctest.Claims{
				Subject:       subject,
				Email:         email,
				EmailVerified: tc.emailVerified,
				Nonce:         nonce, // pass every OTHER check so only the email rule is under test
			})

			resp := doLinkedInCallback(t, handler, "code-"+tc.name, state, txCookie) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.

			if !tc.wantCreated {
				errorCode := assertRejected(t, resp)
				if errorCode != "email_not_verified" {
					t.Errorf("error code = %q, want %q (design spec §3 LinkedIn row: absent email_verified is NEVER treated as true)", errorCode, "email_not_verified")
				}
				assertNoLinkedInIdentity(t, q, subject)
				if tc.emailPresent {
					assertNoUser(t, q, email)
				}
				return
			}

			assertLinkedInLoginAccepted(t, resp)

			identity, err := q.GetIdentityByProviderSubject(context.Background(), store.GetIdentityByProviderSubjectParams{
				Provider:       string(auth.ProviderLinkedIn),
				ProviderUserID: subject,
			})
			if err != nil {
				t.Fatalf("GetIdentityByProviderSubject(linkedin, %q) error = %v, want a created identity row", subject, err)
			}
			usr, err := q.GetUserByEmail(context.Background(), email)
			if err != nil {
				t.Fatalf("GetUserByEmail(%q) error = %v, want a created user row", email, err)
			}
			if identity.UserID != usr.ID {
				t.Errorf("identity.UserID = %v, want %v (the same created user)", identity.UserID, usr.ID)
			}
		})
	}
}

// ==== linking carve-out ====

// TestLinkedInCallback_PurposeLink_AllowsUnverifiedEmail proves the link-only
// email carve-out through the real callback. Direct transaction setup isolates
// callback behavior; success keeps the caller's current session.
func TestLinkedInCallback_PurposeLink_AllowsUnverifiedEmail(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	existingUserID := createTestUser(t, q)
	raw, _ := issueTestSession(t, q, existingUserID) // The callback authenticates the session again before attaching the identity.
	txCookie, tx := beginLinkedInTransaction(t, q, auth.PurposeLink, existingUserID)

	subject := uniqueLinkedInSubject(t)
	email := uniqueEmail(t) // present, but EmailVerified is nil -- see doc comment above
	p.RegisterCode("code-link", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: nil,
		Nonce:         tx.Nonce, // the real, server-generated nonce for this transaction
	})

	resp := doLinkedInCallback(t, handler, "code-link", tx.State, txCookie, sessionRequestCookie(raw)) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want %d (redirect on a successful link)", resp.StatusCode, http.StatusFound)
	}
	wantLocation := testPublicOrigin + "/app/settings/sessions"
	if got := resp.Header.Get("Location"); got != wantLocation {
		t.Errorf("callback Location = %q, want %q (DD-C15: a link success redirects to the settings page that initiated it, never the bare origin)", got, wantLocation)
	}
	if sc := extractCookie(resp, auth.SessionCookieName); sc != nil {
		t.Errorf("callback set a %s cookie (value=%q) on a link success, want none -- the caller already has one", auth.SessionCookieName, sc.Value)
	}
	txCookieResp := extractCookie(resp, auth.OAuthTxCookieName)
	if txCookieResp == nil {
		t.Fatal("callback response missing a __Host-oauth-tx Set-Cookie header clearing it on success (ruling 1)")
	}
	if txCookieResp.MaxAge >= 0 {
		t.Errorf("__Host-oauth-tx cookie MaxAge = %d on a successful callback, want negative (cleared)", txCookieResp.MaxAge)
	}

	identity, err := q.GetIdentityByProviderSubject(context.Background(), store.GetIdentityByProviderSubjectParams{
		Provider:       string(auth.ProviderLinkedIn),
		ProviderUserID: subject,
	})
	if err != nil {
		t.Fatalf("GetIdentityByProviderSubject(linkedin, %q) error = %v, want a created identity row linking to the existing user", subject, err)
	}
	if identity.UserID != existingUserID {
		t.Errorf("linked identity.UserID = %v, want %v (the existing user the link transaction named)", identity.UserID, existingUserID)
	}

	// The carve-out attaches to the existing account; it must not also
	// spawn a brand-new user row from the claims email.
	assertNoUser(t, q, email)
}

// ==== OIDC rejection matrix ====

// TestLinkedInCallback_RejectsWrongIssuer proves LinkedIn's verifier rejects a
// token from another issuer.
func TestLinkedInCallback_RejectsWrongIssuer(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	subject := uniqueLinkedInSubject(t)
	email := uniqueEmail(t)
	txCookie, state, nonce := beginLinkedIn(t, handler)
	p.RegisterCode("code-wrong-issuer", oidctest.Claims{
		Subject: subject, Email: email, EmailVerified: ptrTrue(),
		Nonce: nonce, Issuer: "https://evil.example",
	})

	resp := doLinkedInCallback(t, handler, "code-wrong-issuer", state, txCookie) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoLinkedInIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// TestLinkedInCallback_RejectsWrongAudience proves the configured client ID is
// enforced.
func TestLinkedInCallback_RejectsWrongAudience(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	subject := uniqueLinkedInSubject(t)
	email := uniqueEmail(t)
	txCookie, state, nonce := beginLinkedIn(t, handler)
	p.RegisterCode("code-wrong-audience", oidctest.Claims{
		Subject: subject, Email: email, EmailVerified: ptrTrue(),
		Nonce: nonce, Audience: "some-other-client-id",
	})

	resp := doLinkedInCallback(t, handler, "code-wrong-audience", state, txCookie) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoLinkedInIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// TestLinkedInCallback_RejectsTamperedSignature uses an unpublished signing key.
func TestLinkedInCallback_RejectsTamperedSignature(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	subject := uniqueLinkedInSubject(t)
	email := uniqueEmail(t)
	txCookie, state, nonce := beginLinkedIn(t, handler)
	p.RegisterCode("code-tampered-signature", oidctest.Claims{
		Subject: subject, Email: email, EmailVerified: ptrTrue(),
		Nonce: nonce, SigningKey: generateOtherRSAKey(t),
	})

	resp := doLinkedInCallback(t, handler, "code-tampered-signature", state, txCookie) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoLinkedInIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// TestLinkedInCallback_RejectsNonceMismatch targets the manual nonce check.
func TestLinkedInCallback_RejectsNonceMismatch(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	subject := uniqueLinkedInSubject(t)
	email := uniqueEmail(t)
	txCookie, state, _ := beginLinkedIn(t, handler) // the real nonce is deliberately discarded
	p.RegisterCode("code-nonce-mismatch", oidctest.Claims{
		Subject: subject, Email: email, EmailVerified: ptrTrue(),
		Nonce: "wrong-nonce-does-not-match-the-real-transaction",
	})

	resp := doLinkedInCallback(t, handler, "code-nonce-mismatch", state, txCookie) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoLinkedInIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// TestLinkedInCallback_RejectsExpiredIDToken uses a past exp claim.
func TestLinkedInCallback_RejectsExpiredIDToken(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	subject := uniqueLinkedInSubject(t)
	email := uniqueEmail(t)
	txCookie, state, nonce := beginLinkedIn(t, handler)
	p.RegisterCode("code-expired", oidctest.Claims{
		Subject: subject, Email: email, EmailVerified: ptrTrue(),
		Nonce: nonce, ExpiresAt: time.Now().Add(-1 * time.Hour),
	})

	resp := doLinkedInCallback(t, handler, "code-expired", state, txCookie) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoLinkedInIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// ==== callback transaction checks ====

// TestLinkedInCallback_RejectsStateMismatch proves state remains bound to the
// exact transaction cookie.
func TestLinkedInCallback_RejectsStateMismatch(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	subject := uniqueLinkedInSubject(t)
	email := uniqueEmail(t)
	txCookie, _, nonce := beginLinkedIn(t, handler) // deliberately discard the real state
	p.RegisterCode("code-state-mismatch", oidctest.Claims{
		Subject: subject, Email: email, EmailVerified: ptrTrue(), Nonce: nonce,
	})

	resp := doLinkedInCallback(t, handler, "code-state-mismatch", "attacker-supplied-wrong-state", txCookie) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoLinkedInIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// TestLinkedInCallback_RejectsMissingTxCookie presents plausible query values
// without the transaction handle.
func TestLinkedInCallback_RejectsMissingTxCookie(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	subject := uniqueLinkedInSubject(t)
	email := uniqueEmail(t)
	_, state, nonce := beginLinkedIn(t, handler) // begin a real transaction, never attach its cookie below
	p.RegisterCode("code-missing-cookie", oidctest.Claims{
		Subject: subject, Email: email, EmailVerified: ptrTrue(), Nonce: nonce,
	})

	resp := doLinkedInCallback(t, handler, "code-missing-cookie", state) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoLinkedInIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

// TestLinkedInCallback_ProviderAccessDenied_RedirectsCancelled proves a
// provider-declined consent response maps to the distinct canceled code.
// assertRejected also checks the rejection redirect and cookie effects.
func TestLinkedInCallback_ProviderAccessDenied_RedirectsCancelled(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	txCookie, state, _ := beginLinkedIn(t, handler)

	path := auth.LinkedInCallbackPath + "?error=access_denied&state=" + url.QueryEscape(state)
	resp := doGet(t, handler, path, txCookie) //nolint:bodyclose // doGet (handlers_test.go) closes the body itself before returning.

	errorCode := assertRejected(t, resp)
	if errorCode != "cancelled" { //nolint:misspell // Exact wire value uses double-L "cancelled".
		t.Errorf("error code = %q, want %q (provider access_denied maps to the distinct code below, not the generic auth_failed)", errorCode, "cancelled") //nolint:misspell // same wire value as above
	}
}

// TestLinkedInCallback_OIDCFailures_NoOracleAcrossFailureModes requires the
// same error code and body across all OIDC verification failures.
func TestLinkedInCallback_OIDCFailures_NoOracleAcrossFailureModes(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	otherKey := generateOtherRSAKey(t)

	cases := map[string]func(nonce string) oidctest.Claims{
		"wrong issuer": func(nonce string) oidctest.Claims {
			return oidctest.Claims{
				Subject: uniqueLinkedInSubject(t), Email: uniqueEmail(t),
				EmailVerified: ptrTrue(), Nonce: nonce, Issuer: "https://evil.example",
			}
		},
		"wrong audience": func(nonce string) oidctest.Claims {
			return oidctest.Claims{
				Subject: uniqueLinkedInSubject(t), Email: uniqueEmail(t),
				EmailVerified: ptrTrue(), Nonce: nonce, Audience: "some-other-client-id",
			}
		},
		"tampered signature": func(nonce string) oidctest.Claims {
			return oidctest.Claims{
				Subject: uniqueLinkedInSubject(t), Email: uniqueEmail(t),
				EmailVerified: ptrTrue(), Nonce: nonce, SigningKey: otherKey,
			}
		},
		"nonce mismatch": func(string) oidctest.Claims {
			return oidctest.Claims{
				Subject: uniqueLinkedInSubject(t), Email: uniqueEmail(t),
				EmailVerified: ptrTrue(), Nonce: "wrong-nonce-does-not-match-the-real-transaction",
			}
		},
		"expired id token": func(nonce string) oidctest.Claims {
			return oidctest.Claims{
				Subject: uniqueLinkedInSubject(t), Email: uniqueEmail(t),
				EmailVerified: ptrTrue(), Nonce: nonce, ExpiresAt: time.Now().Add(-1 * time.Hour),
			}
		},
	}

	errorCodes := make(map[string]string, len(cases))
	bodies := make(map[string]string, len(cases))
	for name, buildClaims := range cases {
		code := "code-li-oracle-" + name

		txCookie, state, nonce := beginLinkedIn(t, handler)
		p.RegisterCode(code, buildClaims(nonce))

		path := auth.LinkedInCallbackPath + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state)
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
		t.Error("OIDC verification failures reused the email_not_verified error code, want a distinct generic code -- these five are cryptographic/protocol failures, not the email_verified claim check")
	}

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
