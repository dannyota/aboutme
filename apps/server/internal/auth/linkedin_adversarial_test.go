package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/auth/oidctest"
	"danny.vn/aboutme/apps/server/internal/store"
)

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
		t.Fatal("callback response missing a __Host-oauth-tx Set-Cookie header clearing it on success")
	}
	if tx.MaxAge >= 0 {
		t.Errorf("__Host-oauth-tx cookie MaxAge = %d on a successful callback, want negative (cleared)", tx.MaxAge)
	}
}

func TestLinkedInCallback_RegistrationEmailRuleAdversarial(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		emailPresent     bool
		emailVerified    *bool
		emailVerifiedRaw any
		wantCreated      bool
	}{
		{"verified email present", true, ptrTrue(), nil, true},
		{"unverified email present", true, ptrFalse(), nil, false},
		{"email present, verified claim absent", true, nil, nil, false},
		{"email absent entirely", false, nil, nil, false},
		{"string true email_verified", true, nil, "true", true},
		{"string false email_verified", true, nil, "false", false},
		{"garbage string email_verified", true, nil, "yes", false},
		{"garbage number email_verified", true, nil, 1, false},
		{"explicit null email_verified", true, nil, json.RawMessage("null"), false},
		{"uppercase TRUE email_verified", true, nil, "TRUE", false},
		{"padded true email_verified", true, nil, " true", false},
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
				Subject:          subject,
				Email:            email,
				EmailVerified:    tc.emailVerified,
				EmailVerifiedRaw: tc.emailVerifiedRaw,
				Nonce:            nonce,
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

func TestLinkedInCallback_PurposeLink_AllowsUnverifiedEmail(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	existingUserID := createTestUser(t, q)
	raw, _ := issueTestSession(t, q, existingUserID)
	txCookie, tx := beginLinkedInTransaction(t, q, auth.PurposeLink, existingUserID)

	subject := uniqueLinkedInSubject(t)
	email := uniqueEmail(t)
	p.RegisterCode("code-link", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: nil,
		Nonce:         tx.Nonce,
	})

	resp := doLinkedInCallback(t, handler, "code-link", tx.State, txCookie, sessionRequestCookie(raw)) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want %d (redirect on a successful link)", resp.StatusCode, http.StatusFound)
	}
	wantLocation := testPublicOrigin + "/app/settings/sessions"
	if got := resp.Header.Get("Location"); got != wantLocation {
		t.Errorf("callback Location = %q, want %q (a link success redirects to the settings page that initiated it, never the bare origin)", got, wantLocation)
	}
	if sc := extractCookie(resp, auth.SessionCookieName); sc != nil {
		t.Errorf("callback set a %s cookie (value=%q) on a link success, want none -- the caller already has one", auth.SessionCookieName, sc.Value)
	}
	txCookieResp := extractCookie(resp, auth.OAuthTxCookieName)
	if txCookieResp == nil {
		t.Fatal("callback response missing a __Host-oauth-tx Set-Cookie header clearing it on success")
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

	assertNoUser(t, q, email)
}

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

func TestLinkedInCallback_RejectsNonceMismatch(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	subject := uniqueLinkedInSubject(t)
	email := uniqueEmail(t)
	txCookie, state, _ := beginLinkedIn(t, handler)
	p.RegisterCode("code-nonce-mismatch", oidctest.Claims{
		Subject: subject, Email: email, EmailVerified: ptrTrue(),
		Nonce: "wrong-nonce-does-not-match-the-real-transaction",
	})

	resp := doLinkedInCallback(t, handler, "code-nonce-mismatch", state, txCookie) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoLinkedInIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

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

func TestLinkedInCallback_RejectsStateMismatch(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	subject := uniqueLinkedInSubject(t)
	email := uniqueEmail(t)
	txCookie, _, nonce := beginLinkedIn(t, handler)
	p.RegisterCode("code-state-mismatch", oidctest.Claims{
		Subject: subject, Email: email, EmailVerified: ptrTrue(), Nonce: nonce,
	})

	resp := doLinkedInCallback(t, handler, "code-state-mismatch", "attacker-supplied-wrong-state", txCookie) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoLinkedInIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

func TestLinkedInCallback_RejectsMissingTxCookie(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	subject := uniqueLinkedInSubject(t)
	email := uniqueEmail(t)
	_, state, nonce := beginLinkedIn(t, handler)
	p.RegisterCode("code-missing-cookie", oidctest.Claims{
		Subject: subject, Email: email, EmailVerified: ptrTrue(), Nonce: nonce,
	})

	resp := doLinkedInCallback(t, handler, "code-missing-cookie", state) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoLinkedInIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

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
