// Package auth_test exercises the LinkedIn OIDC login flow end to end
// against oidctest's in-process mock provider, through the same real
// golang.org/x/oauth2 + coreos/go-oidc code paths production uses --
// task-5-brief.md Step 1. This file covers that step's required four-case
// registration-email matrix (AC-AUTH-002: registration is rejected unless
// email is present AND email_verified is present and true), plus
// integration-owner ruling DD-C12's interim purpose=link/reauth safety
// net (fix round 1): until Task 10 lands the full link algorithm,
// resolveLinkedInUser must never resolve a link/reauth transaction to any
// user OTHER than tx.LinkingUserID, and never issue a session for one.
// These transactions are constructed directly via TransactionStore.Begin
// (task-5-brief.md Step 2's own carve-out: "this test can construct the
// transaction directly ... to stay scoped to LinkedIn's rule rather than
// depending on Task 10's HTTP surface"), not through /start, since the
// HTTP surface that lets an authenticated visitor actually request a link
// transaction is Task 10's job. The purpose=link email-carve-out test
// itself (unverified email still allowed to link) and the standard OIDC
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
	"github.com/dannyota/aboutme/apps/server/internal/config"
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

// ==== DD-C12: interim purpose=link/reauth safety net (fix round 1) ====
//
// resolveLinkedInUser's purpose=link/reauth branch is INTERIM, pending
// Task 10's full link algorithm (the real three-way NewUser|
// ExistingIdentity|EmailCollision contract, its own 409 error vocabulary,
// etc.) -- these three tests pin only the narrow safety property that
// must hold regardless: a link/reauth transaction NEVER resolves to, or
// issues a session for, any user other than tx.LinkingUserID.

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
	return &http.Cookie{Name: auth.OAuthTxCookieName, Value: handle}, transaction
}

// TestResolveLinkedInUser_LinkPurpose_MissingLinkingUserID_RejectsGenerically
// is DD-C12 case (a): a purpose=link transaction with no linking user
// recorded (uuid.Nil) is malformed and can never be safely resolved to
// any account -- resolveLinkedInUser must reject it (errLinkedInLinkRejected,
// which handleLinkedInCallback maps to a generic 302 auth_failed, never
// email_not_verified and never a 500) BEFORE performing any database
// write.
//
// This state is unreachable through a real Begin-backed Transaction (the
// oauth_transactions table's own CHECK constraint,
// oauth_transactions_link_needs_user, already requires a non-null
// linking_user_id for purpose IN ('link', 'reauth') -- confirmed by
// attempting exactly this Begin call during this test's own development,
// which failed with SQLSTATE 23514 rather than ever producing a usable
// Transaction). So this test calls resolveLinkedInUser directly
// (auth.ResolveLinkedInUserForTest) rather than through the HTTP surface
// every other test in this file drives: it proves the Go-level
// defense-in-depth check exists and works on its own, independently of
// the DB constraint that (today) makes it unreachable in production.
func TestResolveLinkedInUser_LinkPurpose_MissingLinkingUserID_RejectsGenerically(t *testing.T) {
	t.Parallel()

	q := newTestQueries(t)
	cfg := config.Config{
		PublicOrigin:         testPublicOrigin,
		GoogleClientID:       oidctest.DefaultClientID,
		GoogleClientSecret:   "test-google-client-secret",
		LinkedInClientID:     oidctest.DefaultClientID,
		LinkedInClientSecret: "test-linkedin-client-secret",
	}
	// Issuer overrides are never dialed by this test (resolveLinkedInUser
	// does no OIDC discovery of its own), but NewServiceForTest's
	// parameters are positional and required; an unroutable, obviously
	// inert placeholder documents that.
	const unusedIssuer = "http://127.0.0.1:1"
	svc, err := auth.NewServiceForTest(testLogger(), cfg, q, unusedIssuer, unusedIssuer)
	if err != nil {
		t.Fatalf("NewServiceForTest() error = %v", err)
	}

	subject := uniqueLinkedInSubject(t)
	_, resolveErr := auth.ResolveLinkedInUserForTest(context.Background(), svc, auth.PurposeLink, uuid.Nil, subject, uniqueEmail(t), ptrTrue(), "")
	if resolveErr == nil {
		t.Fatal("resolveLinkedInUser() error = nil, want errLinkedInLinkRejected for a link transaction with no linking user")
	}
	if !auth.IsLinkedInLinkRejectedForTest(resolveErr) {
		t.Errorf("resolveLinkedInUser() error = %v, want the DD-C12 link-rejected sentinel", resolveErr)
	}
	assertNoLinkedInIdentity(t, q, subject)
}

// TestLinkedInCallback_LinkPurpose_IdentityBelongsToDifferentUser_RejectsNoSessionForEitherUser
// is DD-C12 case (b): the account-switch bug this fix round exists to
// close. Before the fix, an unknown-to-this-function-but-actually-
// existing identity was resolved unconditionally to whichever user it
// already belonged to -- for a link flow, that is a DIFFERENT account
// than the one the visitor authenticated as, and a session would be
// issued for it: a silent account switch nobody asked for. The fix must
// reject instead, and critically must issue NO session for either party
// (not the other user's -- obviously wrong -- and not the linking user's
// either, since silently ignoring the mismatch and logging the visitor
// into their OWN account would mask a real conflict that needs Task 10's
// full collision handling, not a silent no-op).
//
// This is also this dispatch's required LinkedIn rejection carrying the
// shared funnel's "provider" log attribute (fix round 1 item 3, the
// unmet half of the original item 8 dispatch requirement): the funnel is
// reused by every provider, so swapping the constant to Google throughout
// linkedin.go would otherwise still pass the whole suite undetected. This
// test reaches a real HTTP-driven rejection (unlike case (a) above, which
// resolveLinkedInUser rejects before the DB is even touched, this one is
// reachable through a genuine Begin-backed Transaction), so it is the
// natural place to carry that assertion.
func TestLinkedInCallback_LinkPurpose_IdentityBelongsToDifferentUser_RejectsNoSessionForEitherUser(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	logger, logBuf := newCapturingLogger()
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL), withLogger(logger))
	ctx := context.Background()

	otherEmail := uniqueEmail(t)
	otherUser, err := q.CreateUser(ctx, store.CreateUserParams{Email: otherEmail, Name: "Other User"})
	if err != nil {
		t.Fatalf("CreateUser(other) error = %v", err)
	}
	subject := uniqueLinkedInSubject(t)
	if _, createErr := q.CreateIdentity(ctx, store.CreateIdentityParams{
		UserID:         otherUser.ID,
		Provider:       string(auth.ProviderLinkedIn),
		ProviderUserID: subject,
	}); createErr != nil {
		t.Fatalf("CreateIdentity(other) error = %v", createErr)
	}

	linkingUser, err := q.CreateUser(ctx, store.CreateUserParams{Email: uniqueEmail(t), Name: "Linking User"})
	if err != nil {
		t.Fatalf("CreateUser(linking) error = %v", err)
	}

	txCookie, tx := beginLinkedInTransaction(t, q, auth.PurposeLink, linkingUser.ID)
	p.RegisterCode("code", oidctest.Claims{Subject: subject, Email: otherEmail, EmailVerified: ptrTrue(), Nonce: tx.Nonce})

	resp := doLinkedInCallback(t, handler, "code", tx.State, txCookie) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	loc := resp.Header.Get("Location")
	if got := mustQueryParam(t, loc, "error"); got != "auth_failed" {
		t.Errorf("error param = %q, want %q", got, "auth_failed")
	}
	assertRedirectPath(t, loc, "/login")
	if extractCookie(resp, auth.SessionCookieName) != nil {
		t.Error("a session cookie was set despite the account-mismatch rejection -- must never authenticate as EITHER user")
	}

	// Both users' rows are untouched: the identity is still owned by
	// otherUser (never reassigned to linkingUser), and otherUser's own
	// row is unchanged.
	identity, err := q.GetIdentityByProviderSubject(ctx, store.GetIdentityByProviderSubjectParams{
		Provider:       string(auth.ProviderLinkedIn),
		ProviderUserID: subject,
	})
	if err != nil {
		t.Fatalf("GetIdentityByProviderSubject(linkedin, %q) error = %v, want the pre-existing row still present", subject, err)
	}
	if identity.UserID != otherUser.ID {
		t.Errorf("identity.UserID = %v, want %v (unchanged -- must never be reassigned to the linking user)", identity.UserID, otherUser.ID)
	}
	stillOther, err := q.GetUserByID(ctx, otherUser.ID)
	if err != nil {
		t.Fatalf("GetUserByID(other) error = %v", err)
	}
	if stillOther.Email != otherEmail {
		t.Errorf("other user's Email = %q, want %q (untouched)", stillOther.Email, otherEmail)
	}

	logged := logBuf.String()
	if !strings.Contains(logged, `"provider":"linkedin"`) {
		t.Errorf("log record = %q, want a provider attribute identifying LinkedIn (not hardcoded to google)", logged)
	}
}

// TestLinkedInCallback_LinkPurpose_UnclaimedIdentity_AttachesToLinkingUserNoNewUser
// is DD-C12 case (d): an unclaimed LinkedIn identity, linked by an
// authenticated visitor, must attach to THAT visitor's own account
// (tx.LinkingUserID) -- never create a brand-new user, which is what
// resolveLinkedInUser's registration branch would otherwise have done had
// purpose/linkingUserID not been threaded through correctly.
func TestLinkedInCallback_LinkPurpose_UnclaimedIdentity_AttachesToLinkingUserNoNewUser(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))
	ctx := context.Background()

	linkingUser, err := q.CreateUser(ctx, store.CreateUserParams{Email: uniqueEmail(t), Name: "Linking User"})
	if err != nil {
		t.Fatalf("CreateUser(linking) error = %v", err)
	}

	txCookie, tx := beginLinkedInTransaction(t, q, auth.PurposeLink, linkingUser.ID)

	subject := uniqueLinkedInSubject(t)
	// Deliberately EmailVerified: nil -- this test's point is the
	// identity/session-target contract (DD-C12 case d), not the email
	// carve-out itself (that dedicated test is the adversarial author's
	// job per task-5-brief.md Step 2), but a link flow must not depend on
	// email verification at all, so leaving it absent here is a faithful,
	// minimal setup rather than an unrelated coincidence.
	p.RegisterCode("code", oidctest.Claims{Subject: subject, Nonce: tx.Nonce})

	resp := doLinkedInCallback(t, handler, "code", tx.State, txCookie) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	if got := resp.Header.Get("Location"); got != testPublicOrigin+"/" {
		t.Errorf("callback Location = %q, want %q (successful link)", got, testPublicOrigin+"/")
	}
	if extractCookie(resp, auth.SessionCookieName) == nil {
		t.Error("callback did not authenticate the linking user")
	}

	identity, err := q.GetIdentityByProviderSubject(ctx, store.GetIdentityByProviderSubjectParams{
		Provider:       string(auth.ProviderLinkedIn),
		ProviderUserID: subject,
	})
	if err != nil {
		t.Fatalf("GetIdentityByProviderSubject(linkedin, %q) error = %v, want a created identity row", subject, err)
	}
	if identity.UserID != linkingUser.ID {
		t.Errorf("identity.UserID = %v, want %v (the linking user -- a link flow must never create a new user)", identity.UserID, linkingUser.ID)
	}
}
