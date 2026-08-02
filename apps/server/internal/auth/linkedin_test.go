// Package auth_test exercises the LinkedIn OIDC login flow end to end
// against oidctest's in-process mock provider, through the same real
// golang.org/x/oauth2 + coreos/go-oidc code paths production uses --
// task-5-brief.md Step 1. This file covers exactly that step's required
// four-case registration-email matrix (AC-AUTH-002): registration is
// rejected unless email is present AND email_verified is present and
// true. The purpose=link carve-out (linking still allowed without a
// verified email) and the standard OIDC adversarial matrix (wrong issuer/
// audience/signature/nonce/expiry, re-run against LinkedIn's own issuer)
// are a separate, independently authored suite per the phase's review
// workflow (task-5-brief.md Steps 2-3) and are not duplicated here.
package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/auth/oidctest"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// uniqueLinkedInSubject mirrors google_adversarial_test.go's
// uniqueSubject with its own "li-sub-" prefix, so a LinkedIn identity row
// this file creates can never collide with one of that file's "g-sub-"
// prefixed Google identities in their shared TEST_DATABASE_URL.
// uniqueEmail (google_adversarial_test.go) is reused unchanged: it is
// already provider-agnostic.
func uniqueLinkedInSubject(t *testing.T) string {
	t.Helper()
	return "li-sub-" + uuid.NewString()
}

// beginLinkedIn is beginGoogle's (google_adversarial_test.go) LinkedIn
// counterpart: drives GET auth.LinkedInStartPath and returns the
// __Host-oauth-tx cookie it set, the state query param, and the
// server-generated nonce query param from its redirect Location.
func beginLinkedIn(t *testing.T, handler http.Handler) (txCookie *http.Cookie, state, nonce string) {
	t.Helper()

	start := doGet(t, handler, auth.LinkedInStartPath) //nolint:bodyclose // doGet (handlers_test.go) closes the body itself before returning.
	if start.StatusCode != http.StatusFound {
		t.Fatalf("GET %s status = %d, want %d (a redirect to the provider)", auth.LinkedInStartPath, start.StatusCode, http.StatusFound)
	}

	txCookie = extractCookie(start, auth.OAuthTxCookieName)
	if txCookie == nil {
		t.Fatalf("GET %s did not set the %s cookie", auth.LinkedInStartPath, auth.OAuthTxCookieName)
	}

	loc := start.Header.Get("Location")
	state = mustQueryParam(t, loc, "state")
	nonce = mustQueryParam(t, loc, "nonce")

	return txCookie, state, nonce
}

// doLinkedInCallback is doCallback's (google_adversarial_test.go)
// LinkedIn counterpart: drives GET auth.LinkedInCallbackPath?code=...&
// state=... via the shared doGet.
func doLinkedInCallback(t *testing.T, handler http.Handler, code, state string, cookies ...*http.Cookie) *http.Response {
	t.Helper()

	path := auth.LinkedInCallbackPath + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state)
	return doGet(t, handler, path, cookies...) //nolint:bodyclose // doGet (handlers_test.go) closes the body itself before returning.
}

// assertNoLinkedInIdentity is assertNoIdentity's (google_adversarial_test.go)
// LinkedIn counterpart -- that helper hardcodes auth.ProviderGoogle, so it
// cannot be reused here.
func assertNoLinkedInIdentity(t *testing.T, q *store.Queries, providerUserID string) {
	t.Helper()

	_, err := q.GetIdentityByProviderSubject(context.Background(), store.GetIdentityByProviderSubjectParams{
		Provider:       string(auth.ProviderLinkedIn),
		ProviderUserID: providerUserID,
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("GetIdentityByProviderSubject(linkedin, %q) error = %v, want pgx.ErrNoRows (no identity row may be created on a rejected registration)", providerUserID, err)
	}
}

// TestLinkedInCallback_RegistrationEmailRule is task-5-brief.md Step 1's
// required four-case matrix (AC-AUTH-002): design spec §3's LinkedIn rule
// -- "registration without a verified email is rejected" -- and
// specifically its "absent email_verified is never treated as true"
// clause. Every case here is a brand-new (unique, never-seen) LinkedIn
// subject, so every case is a REGISTRATION attempt (no existing identity
// to reuse, and purpose defaults to PurposeLogin -- the purpose=link
// carve-out is Step 2's separate, independently authored test).
//
// A fresh oidctest.Provider per subtest (rather than one shared across
// the whole table) keeps each case's registered code fully isolated, and
// a fresh subject/email per subtest (uniqueLinkedInSubject/uniqueEmail)
// is this package's established convention for a shared, never-reset
// TEST_DATABASE_URL (see google_test.go's happy-path doc comment for why
// a fixed identifier would be unsafe here).
func TestLinkedInCallback_RegistrationEmailRule(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		withEmail     bool
		emailVerified *bool // nil = claim absent
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
			// Both issuer overrides point at the same in-process provider:
			// this test never drives a Google route, so Google's discovery
			// is never actually triggered -- newTestService's guard just
			// requires a non-empty value to exist.
			handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

			subject := uniqueLinkedInSubject(t)
			email := ""
			if tc.withEmail {
				email = uniqueEmail(t)
			}

			txCookie, state, nonce := beginLinkedIn(t, handler)
			p.RegisterCode("code", oidctest.Claims{
				Subject:       subject,
				Email:         email,
				EmailVerified: tc.emailVerified,
				Nonce:         nonce,
			})

			resp := doLinkedInCallback(t, handler, "code", state, txCookie) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.

			if resp.StatusCode != http.StatusFound {
				t.Fatalf("callback status = %d, want %d", resp.StatusCode, http.StatusFound)
			}

			if tc.wantCreated {
				if got := resp.Header.Get("Location"); got != testPublicOrigin+"/" {
					t.Errorf("callback Location = %q, want %q (successful registration)", got, testPublicOrigin+"/")
				}
				if extractCookie(resp, auth.SessionCookieName) == nil {
					t.Error("callback did not authenticate, want a __Host-session cookie")
				}

				usr, err := q.GetUserByEmail(context.Background(), email)
				if err != nil {
					t.Fatalf("GetUserByEmail(%q) error = %v, want a created user row", email, err)
				}
				identity, err := q.GetIdentityByProviderSubject(context.Background(), store.GetIdentityByProviderSubjectParams{
					Provider:       string(auth.ProviderLinkedIn),
					ProviderUserID: subject,
				})
				if err != nil {
					t.Fatalf("GetIdentityByProviderSubject(linkedin, %q) error = %v, want a created identity row", subject, err)
				}
				if identity.UserID != usr.ID {
					t.Errorf("identity.UserID = %v, want %v (the same created user)", identity.UserID, usr.ID)
				}
				return
			}

			loc := resp.Header.Get("Location")
			if got := mustQueryParam(t, loc, "error"); got != "email_not_verified" {
				t.Errorf("error param = %q, want %q", got, "email_not_verified")
			}
			assertRedirectPath(t, loc, "/login") // DD-C7
			if extractCookie(resp, auth.SessionCookieName) != nil {
				t.Error("a session cookie was set for a rejected registration, want none")
			}
			if email != "" {
				assertNoUser(t, q, email)
			}
			assertNoLinkedInIdentity(t, q, subject)
		})
	}
}

// TestLinkedInStart_AuthorizeURL_RedirectURIMatchesPublicOriginAndCallbackPath
// is google_test.go's identical assertion, re-run for LinkedIn: the
// authorize URL's redirect_uri must equal PublicOrigin + LinkedInCallbackPath.
func TestLinkedInStart_AuthorizeURL_RedirectURIMatchesPublicOriginAndCallbackPath(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))

	startResp := doGet(t, handler, auth.LinkedInStartPath) //nolint:bodyclose // doGet closes the body itself before returning.
	if startResp.StatusCode != http.StatusFound {
		t.Fatalf("GET %s status = %d, want %d", auth.LinkedInStartPath, startResp.StatusCode, http.StatusFound)
	}
	loc, err := url.Parse(startResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse start redirect Location: %v", err)
	}

	want := testPublicOrigin + auth.LinkedInCallbackPath
	if got := loc.Query().Get("redirect_uri"); got != want {
		t.Errorf("authorize URL redirect_uri = %q, want %q (PublicOrigin + LinkedInCallbackPath)", got, want)
	}
}
