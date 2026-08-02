// Package auth_test exercises the LinkedIn OIDC login flow end to end
// against oidctest's in-process mock provider, through the same real
// golang.org/x/oauth2 + coreos/go-oidc code paths production uses --
// task-5-brief.md Step 1. This file covers that step's required four-case
// registration-email matrix (AC-AUTH-002: registration is rejected unless
// email is present AND email_verified is present and true), plus
// beginLinkedInTransaction, a helper for constructing an OAuth transaction
// directly via TransactionStore.Begin (task-5-brief.md Step 2's own
// carve-out) that linkedin_adversarial_test.go's own purpose=link carve-out
// test reuses. Historically (fix round 1 through Task 10) this file also
// carried integration-owner ruling DD-C12's interim purpose=link/reauth
// safety-net tests; those were removed once Task 10 replaced
// resolveLinkedInUser's link/reauth arms with link.go's shared algorithm
// -- see beginLinkedInTransaction's own section comment below for the
// full removal note. The purpose=link email-carve-out test itself
// (unverified email still allowed to link) and the standard OIDC
// adversarial matrix (wrong issuer/audience/signature/nonce/expiry,
// re-run against LinkedIn's own issuer) are a separate, independently
// authored suite per the phase's review workflow (task-5-brief.md Steps
// 2-3) and are not duplicated here.
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

// TestLinkedInCallback_RejectionLogsProviderAttribute mirrors
// TestGoogleCallback_RejectionLogsProviderAttribute (handlers_test.go) and
// TestGitHubCallback_RejectionLogsProviderAttribute (github_test.go) for
// LinkedIn (fix round 1, M3 -- restoring an assertion lost when this
// file's own DD-C12 tests, one of which originally carried it, were
// removed): the shared rejection funnel (redirectWithError/
// redirectAuthFailed, handlers.go) is reused by every provider Service
// registers, so its log message is deliberately provider-neutral ("auth:
// callback rejected", never "auth: linkedin callback rejected") -- the
// provider attribute passed at each call site is what still lets an
// operator filter or correlate by provider in a shared log stream, and is
// the ONLY thing that would catch a copy-paste slip (e.g.
// ProviderGoogle hardcoded at a LinkedIn call site) -- the response the
// browser sees is identical either way, so only a log assertion like this
// one discriminates a swapped constant.
func TestLinkedInCallback_RejectionLogsProviderAttribute(t *testing.T) {
	t.Parallel()

	logger, logBuf := newCapturingLogger()
	// This request never reaches a real LinkedIn (or Google) call (it's
	// rejected at the missing-tx-cookie check, the very first line of
	// handleLinkedInCallback), so any non-empty override satisfies
	// newTestService's guard -- auth.UnroutableTestSentinel documents that
	// explicitly rather than a bare, unexplained literal (mirrors
	// TestGitHubCallback_RejectionLogsProviderAttribute's identical
	// reasoning). withGoogleIssuer is required here only to satisfy
	// newTestService's own belt-and-suspenders guard (it checks
	// googleIssuer/githubEndpoint, not linkedinIssuer -- see that
	// function's doc comment) -- this test never drives a Google route.
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

// ==== Constructing a link/reauth transaction directly ====
//
// beginLinkedInTransaction (below) is reused by linkedin_adversarial_test.go
// (task-5-brief.md Step 2's purpose=link carve-out test) and, historically,
// by this file's own DD-C12 interim-safety-net tests: resolveLinkedInUser's
// purpose=link/reauth branch was DD-C12's fix-round-1 interim safety net,
// pending Task 10's full link algorithm. Task 10 replaced that function's
// link/reauth arms entirely with link.go's shared resolveLinkOrReauth
// (applied uniformly to all three providers, not a LinkedIn-only
// function), so the three DD-C12-specific tests that once lived here
// (TestResolveLinkedInUser_LinkPurpose_MissingLinkingUserID_RejectsGenerically,
// TestLinkedInCallback_LinkPurpose_IdentityBelongsToDifferentUser_RejectsNoSessionForEitherUser,
// TestLinkedInCallback_LinkPurpose_UnclaimedIdentity_AttachesToLinkingUserNoNewUser)
// were removed: they asserted DD-C12's interim contract (e.g. a
// successful link redirecting to the bare "/" and issuing a session
// cookie), which DD-C15 supersedes (a link success redirects to
// PublicOrigin+"/app/settings/sessions" and issues no session at all --
// see handlers.go's callbackSuccessRedirect). Task 10's link algorithm
// gets its own independent adversarial coverage instead
// (task-10-brief.md Step 3), authored without reading this diff.

// beginLinkedInTransaction constructs an OAuth transaction directly via
// TransactionStore.Begin -- task-5-brief.md Step 2's own carve-out ("this
// test can construct the transaction directly via TransactionStore.Begin
// to stay scoped to LinkedIn's rule rather than depending on Task 10's
// HTTP surface"): the HTTP surface that lets an authenticated visitor
// actually request a link/reauth transaction is Task 10's job, not this
// one's. Returns the __Host-oauth-tx cookie a real /start would have set
// and the Transaction itself (for its State/Nonce).
func beginLinkedInTransaction(t *testing.T, q *store.Queries, purpose auth.Purpose, linkingUserID uuid.UUID) (txCookie *http.Cookie, tx auth.Transaction) {
	t.Helper()

	txStore := auth.NewTransactionStore(q)
	handle, transaction, err := txStore.Begin(context.Background(), auth.ProviderLinkedIn, purpose, linkingUserID, testPublicOrigin+auth.LinkedInCallbackPath)
	if err != nil {
		t.Fatalf("TransactionStore.Begin() error = %v", err)
	}
	return requestCookie(auth.OAuthTxCookieName, handle), transaction
}
