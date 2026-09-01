// Package auth_test exercises LinkedIn OIDC and its verified-email rule against
// an in-process provider.
package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/auth/oidctest"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// uniqueLinkedInSubject avoids collisions in the shared test database.
func uniqueLinkedInSubject(t *testing.T) string {
	t.Helper()
	return "li-sub-" + uuid.NewString()
}

// beginLinkedIn returns the transaction cookie, state, and nonce from /start.
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

// doLinkedInCallback drives the LinkedIn callback through the shared router.
func doLinkedInCallback(t *testing.T, handler http.Handler, code, state string, cookies ...*http.Cookie) *http.Response {
	t.Helper()

	path := auth.LinkedInCallbackPath + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state)
	return doGet(t, handler, path, cookies...) //nolint:bodyclose // doGet (handlers_test.go) closes the body itself before returning.
}

// assertNoLinkedInIdentity checks the LinkedIn provider key.
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

// TestLinkedInCallback_RegistrationEmailRule covers verified, unverified,
// absent-verification, and absent-email registration claims.
//
// Each case uses a fresh provider and identifiers to isolate its code and rows.
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
				if got := resp.Header.Get("Location"); got != testPublicOrigin+"/app/resumes" {
					t.Errorf("callback Location = %q, want %q (successful registration)", got, testPublicOrigin+"/app/resumes")
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
			assertRedirectPath(t, loc, "/login")
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

// TestLinkedInCallback_RejectionLogsProviderAttribute catches a wrong provider
// constant at LinkedIn's shared rejection funnel.
func TestLinkedInCallback_RejectionLogsProviderAttribute(t *testing.T) {
	t.Parallel()

	logger, logBuf := newCapturingLogger()
	// Missing transaction state rejects before either provider endpoint is used.
	handler, _ := newTestService(t, withGoogleIssuer(auth.UnroutableTestSentinel), withLinkedInIssuer(auth.UnroutableTestSentinel), withLogger(logger))

	resp := doGet(t, handler, auth.LinkedInCallbackPath+"?code=whatever&state=whatever") //nolint:bodyclose // doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}

	logged := logBuf.String()
	if !strings.Contains(logged, `"provider":"linkedin"`) {
		t.Errorf("log record = %q, want a provider attribute identifying LinkedIn as the callback that was rejected", logged)
	}
}

// ==== Constructing a link or reauthentication transaction directly ====

// beginLinkedInTransaction constructs an OAuth transaction directly via
// TransactionStore.Begin to keep provider callback tests independent of
// the start endpoint. It returns the cookie a real start would set and the
// transaction state and nonce needed by the mock provider.
func beginLinkedInTransaction(t *testing.T, q *store.Queries, purpose auth.Purpose, linkingUserID uuid.UUID) (txCookie *http.Cookie, tx auth.Transaction) {
	t.Helper()

	txStore := auth.NewTransactionStore(q)
	handle, transaction, err := txStore.Begin(context.Background(), auth.ProviderLinkedIn, purpose, linkingUserID, testPublicOrigin+auth.LinkedInCallbackPath)
	if err != nil {
		t.Fatalf("TransactionStore.Begin() error = %v", err)
	}
	return requestCookie(auth.OAuthTxCookieName, handle), transaction
}
