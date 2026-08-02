// Package auth_test: independent, spec-derived adversarial tests for
// Task 10 (task-10-brief.md), AC-AUTH-001 ("No automatic email merge
// across providers") and the account-linking/reauth surface it adds.
// Derived WITHOUT reading internal/auth/link.go (does not exist in this
// checkout), task-10-report.md, or Task 10's git history -- only from
// task-10-brief.md, docs/specs/aboutme-design.md §3 ("OAuth" and
// "Sessions"), and every COMMITTED package/test file at this checkout:
// handlers.go (the resolveGoogleUser/resolveGitHubUser stubs Task 10
// replaces, and the shared redirect/cookie funnel), linkedin.go (the
// interim DD-C12 link/reauth safety net Task 10 replaces), session.go
// (RequireRecentReauth/TouchReauthenticated), transaction.go
// (TransactionStore.Begin/Consume, Purpose), and the existing test
// harnesses this file reuses rather than duplicates: oidctest
// (google_adversarial_test.go/linkedin_test.go), the GitHub REST stub
// (github_test.go), and Task 9's session/reauth test seams
// (me_test.go, sessions_handlers_test.go, session_adversarial_test.go,
// csrf_test.go).
//
// Two independent halves, matching the brief's own two steps:
//
//   - Step 2's email-merge rejection matrix (TestCallback_
//     EmailMergeRejectionMatrix): all six cross-provider pairs, the
//     different-email non-merge case, and the same-provider/different-sub
//     collision case -- eight scenarios, table-driven.
//   - Step 3's link/reauth table: four named tests
//     (TestLink_RejectsWithoutRecentReauth,
//     TestLink_RejectsIdentityAlreadyClaimedByAnotherUser,
//     TestLink_IdempotentWhenAlreadyLinkedToSelf,
//     TestPurposeReauth_RefreshesReauthenticatedAt_ButDoesNotCreateIdentity).
//
// Design choice for the same-provider collision case (matrix row 8):
// exercised via Google, not GitHub, mirroring the brief's own example
// ("two different Google Workspace accounts ... reporting the same
// email"). The committed GitHub stub (newGitHubStub, github_test.go) is
// single-shot per instance -- one fixed (code, userID, emails) triple
// baked in at construction, with no setter to change it mid-test -- so
// driving GitHub through the SAME identity twice with a different sub
// isn't possible without either a second Service/stub pair or extending
// the stub, neither of which this independent suite should do (extending
// shared test infrastructure is integration-owner territory, and a
// second Service instance sharing one store.Queries is a needless
// complication when the brief's own example already names a
// same-provider case Google trivially supports -- oidctest.Provider
// issues arbitrarily many distinct-subject codes from one instance).
//
// See notes.md for the full per-test derivation record, every ADAPT
// marker, and the ambiguities this file resolved (and how).
//
// Reconciled against the landed implementation (commit 7f88dd0,
// "feat(auth): add explicit account linking and reject automatic email
// merge") after derivation completed: all four ADAPT markers confirmed
// against link.go/handlers.go and adjusted where the landed wire shape
// differed from this suite's own best-supported guess -- see notes.md's
// integration report for the exact diff and reasoning per marker. No
// assertion or scenario was weakened in reconciliation.
//
// Fix round 1 (Opus review, findings C1/DD-C16): the reauth-then-link
// same-site chain C1 found required TestLink_RejectsWithoutRecentReauth's
// own /start request to carry a same-site signal (an Origin header) it
// didn't need before -- glue only, at that one test's own call site; see
// its doc comment for the exact reasoning. No other test in this file
// drives /start via HTTP (the other three construct their transaction
// directly via TransactionStore.Begin, bypassing /start's same-site check
// entirely), so nothing else here needed touching.
package auth_test

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/auth/oidctest"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// ============================================================================
// Shared per-provider login-attempt helpers
// ============================================================================

// googleLoginAttempt drives a full, fresh GET /start -> GET /callback
// Google login round trip against handler for email, using a brand-new
// subject (uniqueSubject, google_adversarial_test.go) every call so
// repeated calls against the same handler never collide with each other.
// Returns the subject used (identities.provider_user_id) and the raw
// HTTP response, for either a success or a rejection assertion.
func googleLoginAttempt(t *testing.T, handler http.Handler, p *oidctest.Provider, email string) (subject string, resp *http.Response) {
	t.Helper()

	subject = uniqueSubject(t)
	txCookie, state, nonce := beginGoogle(t, handler)
	code := "code-matrix-google-" + uuid.NewString()
	p.RegisterCode(code, oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
	})
	resp = doCallback(t, handler, code, state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.
	return subject, resp
}

// linkedInLoginAttempt is googleLoginAttempt's LinkedIn counterpart,
// reusing linkedin_test.go's beginLinkedIn/doLinkedInCallback and
// uniqueLinkedInSubject. Always registers a verified email (design spec
// §3's LinkedIn registration rule is orthogonal to this suite's own
// concern and must never confound a collision assertion).
func linkedInLoginAttempt(t *testing.T, handler http.Handler, p *oidctest.Provider, email string) (subject string, resp *http.Response) {
	t.Helper()

	subject = uniqueLinkedInSubject(t)
	txCookie, state, nonce := beginLinkedIn(t, handler)
	code := "code-matrix-linkedin-" + uuid.NewString()
	p.RegisterCode(code, oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
	})
	resp = doLinkedInCallback(t, handler, code, state, txCookie) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.
	return subject, resp
}

// githubIdentity is a single fixed GitHub account fixture: newGitHubStub
// (github_test.go) bakes its (code, userID, emails) triple in at
// construction with no setter to change it afterward, so ONE
// githubIdentity can back exactly one login attempt's worth of GitHub
// state within a test -- see this file's own package doc comment for why
// the same-provider collision case (matrix row 8) uses Google instead of
// trying to drive GitHub through two distinct identities in one test.
type githubIdentity struct {
	stub    *gitHubStub
	code    string
	subject string // strconv.FormatInt(userID, 10) -- identities.provider_user_id's stored shape (github.go).
}

// newGitHubIdentity builds a fresh, single-use GitHub account fixture
// whose one verified primary email is exactly email.
func newGitHubIdentity(t *testing.T, email string) *githubIdentity {
	t.Helper()

	userID := uniqueGitHubUserID(t)
	code := "code-matrix-github-" + uuid.NewString()
	token := "token-matrix-github-" + uuid.NewString()
	stub := newGitHubStub(t,
		withTokenResponse(code, token),
		withUser(userID, "octocat-matrix"),
		withEmails([]ghEmail{{Email: email, Primary: true, Verified: true}}),
	)
	return &githubIdentity{stub: stub, code: code, subject: strconv.FormatInt(userID, 10)}
}

// githubAttempt drives one GET /start -> GET /callback GitHub round trip
// against handler using gi's fixed identity. handler's Service must
// already be wired to gi.stub.URL via withGitHubEndpoint at construction
// time (githubEndpointOverride has no post-construction setter).
func githubAttempt(t *testing.T, handler http.Handler, gi *githubIdentity) (subject string, resp *http.Response) {
	t.Helper()

	txCookie, state := beginGitHubFlow(t, handler)
	resp = doGitHubCallback(t, handler, gi.code, state, txCookie) //nolint:bodyclose // doGitHubCallback -> doGet closes the body itself before returning.
	return gi.subject, resp
}

// loginVia dispatches to the right provider-specific login-attempt
// helper. gh is nil unless provider == auth.ProviderGitHub, in which case
// it must be the fixture already wired into handler's Service.
func loginVia(t *testing.T, handler http.Handler, p *oidctest.Provider, gh *githubIdentity, provider auth.Provider, email string) (subject string, resp *http.Response) {
	t.Helper()

	switch provider {
	case auth.ProviderGoogle:
		return googleLoginAttempt(t, handler, p, email)
	case auth.ProviderLinkedIn:
		return linkedInLoginAttempt(t, handler, p, email)
	case auth.ProviderGitHub:
		if gh == nil {
			t.Fatalf("loginVia: provider=github requested but no githubIdentity fixture was built for this role")
		}
		return githubAttempt(t, handler, gh)
	default:
		t.Fatalf("loginVia: unknown provider %v", provider)
		return "", nil
	}
}

// assertNoIdentityForProvider is assertNoIdentity's (google_adversarial_
// test.go) provider-generic counterpart -- that helper and
// assertNoLinkedInIdentity (linkedin_test.go) each hardcode one provider.
func assertNoIdentityForProvider(t *testing.T, q *store.Queries, provider auth.Provider, providerUserID string) {
	t.Helper()

	_, err := q.GetIdentityByProviderSubject(context.Background(), store.GetIdentityByProviderSubjectParams{
		Provider:       string(provider),
		ProviderUserID: providerUserID,
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("GetIdentityByProviderSubject(%s, %q) error = %v, want pgx.ErrNoRows (no identity row may be created by a rejected email-collision attempt)", provider, providerUserID, err)
	}
}

// ============================================================================
// Step 2: the email-merge rejection matrix
// ============================================================================

// matrixCase is one row of the brief's Step 2 table.
type matrixCase struct {
	name             string
	existingProvider auth.Provider
	attemptProvider  auth.Provider
	// sameEmail: true for every collision row (six cross-provider pairs
	// plus the same-provider row); false only for the one explicit
	// non-merge row (different email must succeed as an ordinary NewUser).
	sameEmail bool
	// wantCollision mirrors sameEmail exactly in every row this table
	// actually has, but is kept as its own field (rather than reusing
	// sameEmail directly in the assertion) so a future row that is
	// same-email but for some OTHER spec reason must NOT collide (none
	// exist today) doesn't have to fight the field's name.
	wantCollision bool
}

// assertEmailCollisionRejected asserts the design spec §3 email-collision
// contract on resp, a rejected attemptProvider /callback whose email
// already belongs to an account reached via existingProvider:
//
//   - 302 redirect (DD-C3/DD-C4: /callback is a top-level browser
//     navigation, never a raw JSON 409 -- the brief's own "Produces"
//     section states this explicitly for exactly this rejection: "again a
//     redirect, never a raw JSON 409").
//   - ?error=email_already_registered.
//   - no __Host-session cookie (the attacker is never authenticated as
//     the victim -- this is the core account-takeover property).
//   - __Host-oauth-tx cleared (ruling 1, applies to every /callback exit
//     path).
//   - the EXISTING provider's name never appears in the redirect --
//     design spec §3: "must not name the existing provider -- naming it
//     hands an attacker a targeted-phishing hint." The ATTEMPTING
//     provider's own name is fine (the browser already knows it: it just
//     came from that provider's own /callback URL).
//
// The `provider=` query-parameter sub-check below was ADAPT-flagged
// during derivation; reconciliation against the landed
// redirectEmailAlreadyRegistered (handlers.go) confirmed it exactly:
// "&provider=" + url.QueryEscape(string(provider)) where provider is the
// ATTEMPTING provider -- no change needed. See notes.md's integration
// report.
func assertEmailCollisionRejected(t *testing.T, resp *http.Response, attemptProvider, existingProvider auth.Provider) {
	t.Helper()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("email-collision attempt status = %d, want %d (a redirect, never a raw JSON 409 -- /callback is a top-level browser navigation)", resp.StatusCode, http.StatusFound)
	}
	loc := resp.Header.Get("Location")
	if got := mustQueryParam(t, loc, "error"); got != "email_already_registered" {
		t.Errorf("error param = %q, want %q", got, "email_already_registered")
	}
	assertRedirectPath(t, loc, "/login") // DD-C7: every rejection targets /login, never "/".

	if sc := extractCookie(resp, auth.SessionCookieName); sc != nil {
		t.Errorf("response set a %s cookie on a rejected email-collision callback (value=%q), want none -- the attacker must never be authenticated as the victim", auth.SessionCookieName, sc.Value)
	}
	tx := extractCookie(resp, auth.OAuthTxCookieName)
	if tx == nil || tx.MaxAge >= 0 {
		t.Error("__Host-oauth-tx not cleared on a rejected email-collision callback (ruling 1)")
	}

	if existingProvider != attemptProvider && strings.Contains(strings.ToLower(loc), string(existingProvider)) {
		t.Errorf("redirect Location %q names the EXISTING provider %q -- design spec §3 forbids this (it hands an attacker a targeted-phishing hint about which provider the victim's account is actually linked to)", loc, existingProvider)
	}

	// Confirmed against the landed redirectEmailAlreadyRegistered
	// (handlers.go): <p> is the ATTEMPTING provider (the one this exact
	// /callback belongs to; already known to the browser, so this does
	// not reopen the "never name the existing provider" leak checked
	// above) -- no longer an ADAPT guess.
	if got := mustQueryParam(t, loc, "provider"); got != string(attemptProvider) {
		t.Errorf("provider param = %q, want %q (the provider this /callback belongs to, per the brief's literal wire shape)", got, attemptProvider)
	}
}

// runMatrixCase drives one matrixCase end to end against its own,
// isolated Service/database-pool pair (newTestService's own convention --
// every test in this package opens its own connection against the same
// shared TEST_DATABASE_URL, relying on unique row identifiers rather than
// a fresh/reset database per test).
func runMatrixCase(t *testing.T, c matrixCase) {
	t.Helper()

	p := oidctest.NewProvider(t)

	existingEmail := uniqueEmail(t)
	attemptEmail := existingEmail
	if !c.sameEmail {
		attemptEmail = uniqueEmail(t)
	}

	// Any GitHub fixture this case needs must exist BEFORE the Service is
	// built: githubEndpointOverride has no post-construction setter
	// (export_test.go's NewServiceForTest). At most one of these is ever
	// non-nil for any row in this table (GitHub never plays both roles in
	// the same case -- see this file's own package doc comment for why
	// the one case that would have needed that, row 8, uses Google
	// instead).
	var githubExisting, githubAttemptIdentity *githubIdentity
	if c.existingProvider == auth.ProviderGitHub {
		githubExisting = newGitHubIdentity(t, existingEmail)
	}
	if c.attemptProvider == auth.ProviderGitHub {
		githubAttemptIdentity = newGitHubIdentity(t, attemptEmail)
	}

	opts := []testServiceOption{withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL)}
	switch {
	case githubExisting != nil:
		opts = append(opts, withGitHubEndpoint(githubExisting.stub.URL))
	case githubAttemptIdentity != nil:
		opts = append(opts, withGitHubEndpoint(githubAttemptIdentity.stub.URL))
	}
	handler, q := newTestService(t, opts...)
	ctx := context.Background()

	// ---- establish the existing account -----------------------------
	_, existingResp := loginVia(t, handler, p, githubExisting, c.existingProvider, existingEmail) //nolint:bodyclose // loginVia -> doCallback/doGet closes the body itself before returning.
	if existingResp.StatusCode != http.StatusFound {
		t.Fatalf("%s: existing-account login via %s status = %d, want %d -- setup is broken, not the code under test", c.name, c.existingProvider, existingResp.StatusCode, http.StatusFound)
	}
	if extractCookie(existingResp, auth.SessionCookieName) == nil {
		t.Fatalf("%s: existing-account login via %s did not authenticate -- setup is broken, not the code under test", c.name, c.existingProvider)
	}

	existingUser, err := q.GetUserByEmail(ctx, existingEmail)
	if err != nil {
		t.Fatalf("%s: GetUserByEmail(existing) error = %v", c.name, err)
	}
	existingIdentitiesBefore, err := q.ListIdentitiesByUserID(ctx, existingUser.ID)
	if err != nil {
		t.Fatalf("%s: ListIdentitiesByUserID(existing) error = %v", c.name, err)
	}

	// ---- the attack / non-attack attempt ------------------------------
	attemptSubject, attemptResp := loginVia(t, handler, p, githubAttemptIdentity, c.attemptProvider, attemptEmail) //nolint:bodyclose // loginVia -> doCallback/doGet closes the body itself before returning.

	if c.wantCollision {
		assertEmailCollisionRejected(t, attemptResp, c.attemptProvider, c.existingProvider)

		// "zero new rows in users/identities" -- scoped, not a global
		// table COUNT(*) (this package's established convention for a
		// shared, never-reset TEST_DATABASE_URL under parallel tests):
		// the attacker's own (provider, sub) must not exist at all ...
		assertNoIdentityForProvider(t, q, c.attemptProvider, attemptSubject)

		// ... and the victim's account row is BYTE-IDENTICAL, not just
		// "still exists" (the brief's explicit requirement: "full-row
		// comparison, not just existence").
		afterUser, getErr := q.GetUserByEmail(ctx, existingEmail)
		if getErr != nil {
			t.Fatalf("%s: GetUserByEmail(existing) after the attempt error = %v, want the row still present, unchanged", c.name, getErr)
		}
		if !reflect.DeepEqual(existingUser, afterUser) {
			t.Errorf("%s: the existing account's row changed as a side effect of a REJECTED cross-provider login attempt (account-takeover-adjacent bug):\n before: %+v\n after:  %+v", c.name, existingUser, afterUser)
		}
		afterIdentities, getErr := q.ListIdentitiesByUserID(ctx, existingUser.ID)
		if getErr != nil {
			t.Fatalf("%s: ListIdentitiesByUserID(existing) after the attempt error = %v", c.name, getErr)
		}
		if !reflect.DeepEqual(existingIdentitiesBefore, afterIdentities) {
			t.Errorf("%s: the existing account's identities changed as a side effect of a REJECTED login attempt: before=%+v after=%+v", c.name, existingIdentitiesBefore, afterIdentities)
		}
		return
	}

	// Case 7: different email -- ordinary NewUser, an independent
	// account, must NOT be accidentally blocked by the collision defense
	// above (this is the "false positive" side of the same code path).
	if attemptResp.StatusCode != http.StatusFound {
		t.Fatalf("%s: attempt login status = %d, want %d (a different-email NewUser login must succeed, never be treated as a collision)", c.name, attemptResp.StatusCode, http.StatusFound)
	}
	assertRedirectPath(t, attemptResp.Header.Get("Location"), "/") // success path, not /login.
	if extractCookie(attemptResp, auth.SessionCookieName) == nil {
		t.Fatalf("%s: attempt login did not authenticate, want a real NewUser session", c.name)
	}

	attemptUser, err := q.GetUserByEmail(ctx, attemptEmail)
	if err != nil {
		t.Fatalf("%s: GetUserByEmail(attempt) error = %v, want a NEW user row created", c.name, err)
	}
	if attemptUser.ID == existingUser.ID {
		t.Errorf("%s: the different-email attempt resolved to the SAME user as the existing account, want a distinct new account (different emails must never merge)", c.name)
	}
	attemptIdentity, err := q.GetIdentityByProviderSubject(ctx, store.GetIdentityByProviderSubjectParams{
		Provider:       string(c.attemptProvider),
		ProviderUserID: attemptSubject,
	})
	if err != nil {
		t.Fatalf("%s: GetIdentityByProviderSubject(attempt) error = %v, want a created identity row", c.name, err)
	}
	if attemptIdentity.UserID != attemptUser.ID {
		t.Errorf("%s: attempt identity.UserID = %v, want %v (the newly created user)", c.name, attemptIdentity.UserID, attemptUser.ID)
	}
}

// TestCallback_EmailMergeRejectionMatrix is task-10-brief.md Step 2's
// core mandate: AC-AUTH-001, "no automatic email merge across providers",
// exercised as all six cross-provider pairs (both directions), the one
// explicit different-email non-merge case, and the same-provider/
// different-sub edge case the brief adds explicitly because "the code
// path is identical to the cross-provider one and should be covered by
// the same test rather than assumed." Table-driven (8 rows); every row
// runs as its own isolated t.Run subtest against its own fresh Service.
//
// A regression here is exactly what the spec calls an account-takeover
// vector -- an attacker who can obtain any verified email from any
// provider (freely creatable, e.g. a throwaway Gmail/GitHub/LinkedIn
// account using the victim's own email as a "verified" claim from a
// DIFFERENT identity) matching a victim's registered email would
// otherwise walk straight into the victim's account. Every collision
// row's rejection is unconditional, not "eventually consistent" or
// "usually rejected" -- see assertEmailCollisionRejected's own doc
// comment for exactly which properties that means.
func TestCallback_EmailMergeRejectionMatrix(t *testing.T) {
	t.Parallel()

	cases := []matrixCase{
		{
			name:             "google existing, github attempt, same email",
			existingProvider: auth.ProviderGoogle, attemptProvider: auth.ProviderGitHub,
			sameEmail: true, wantCollision: true,
		},
		{
			name:             "google existing, linkedin attempt, same email",
			existingProvider: auth.ProviderGoogle, attemptProvider: auth.ProviderLinkedIn,
			sameEmail: true, wantCollision: true,
		},
		{
			name:             "github existing, google attempt, same email",
			existingProvider: auth.ProviderGitHub, attemptProvider: auth.ProviderGoogle,
			sameEmail: true, wantCollision: true,
		},
		{
			name:             "github existing, linkedin attempt, same email",
			existingProvider: auth.ProviderGitHub, attemptProvider: auth.ProviderLinkedIn,
			sameEmail: true, wantCollision: true,
		},
		{
			name:             "linkedin existing, google attempt, same email",
			existingProvider: auth.ProviderLinkedIn, attemptProvider: auth.ProviderGoogle,
			sameEmail: true, wantCollision: true,
		},
		{
			name:             "linkedin existing, github attempt, same email",
			existingProvider: auth.ProviderLinkedIn, attemptProvider: auth.ProviderGitHub,
			sameEmail: true, wantCollision: true,
		},
		{
			name:             "google existing, github attempt, DIFFERENT email -- must not be blocked",
			existingProvider: auth.ProviderGoogle, attemptProvider: auth.ProviderGitHub,
			sameEmail: false, wantCollision: false,
		},
		{
			name:             "google existing, google attempt with a DIFFERENT sub, same email -- same-provider collision",
			existingProvider: auth.ProviderGoogle, attemptProvider: auth.ProviderGoogle,
			sameEmail: true, wantCollision: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			runMatrixCase(t, c)
		})
	}
}

// ============================================================================
// Step 3: link + reauth adversarial table
// ============================================================================

// wantSettingsSessionsPath mirrors handlers.go's own unexported
// settingsSessionsPath constant (DD-C15): every link/reauth-purpose
// /callback outcome -- success AND rejection alike -- redirects here,
// never to "/login" or "/". Confirmed by reading the landed handlers.go
// (callbackErrorRedirectBase/callbackSuccessRedirect) during
// reconciliation; redeclared here because this suite is black-box
// (package auth_test) and cannot reference an unexported constant of
// package auth directly.
const wantSettingsSessionsPath = "/app/settings/sessions"

// oauthTransactionCountForLinkingUser scopes a row-count assertion to
// exactly the rows a link/reauth Begin for userID would have created --
// safe under this package's shared, parallel, never-reset
// TEST_DATABASE_URL, unlike a bare global COUNT(*).
func oauthTransactionCountForLinkingUser(ctx context.Context, t *testing.T, db rowQuerier, userID uuid.UUID) int {
	t.Helper()
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM oauth_transactions WHERE linking_user_id = $1`, userID).Scan(&count); err != nil {
		t.Fatalf("oauthTransactionCountForLinkingUser query error: %v", err)
	}
	return count
}

// identityCountForProviderSubject is the brief's own explicit ask for
// TestLink_IdempotentWhenAlreadyLinkedToSelf: "a unique constraint would
// 500 a naive re-insert -- assert it doesn't" needs a real row-count
// check, not merely the absence of an HTTP error.
func identityCountForProviderSubject(ctx context.Context, t *testing.T, db rowQuerier, provider, providerUserID string) int {
	t.Helper()
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM identities WHERE provider = $1 AND provider_user_id = $2`, provider, providerUserID).Scan(&count); err != nil {
		t.Fatalf("identityCountForProviderSubject query error: %v", err)
	}
	return count
}

// beginGoogleTransaction is linkedin_test.go's landed beginLinkedInTransaction
// helper's Google counterpart (added during reconciliation): that helper
// is LinkedIn-specific (hardcodes auth.ProviderLinkedIn/
// auth.LinkedInCallbackPath), so this suite's own Step 3 tests -- which
// all drive Google, per the brief's "cheapest provider" convention --
// reuse its exact shape (TransactionStore.Begin directly, task-5-brief.md
// Step 2's carve-out) instead of repeating a bare
// &http.Cookie{Name: auth.OAuthTxCookieName, ...} literal at each of
// their three call sites.
func beginGoogleTransaction(t *testing.T, q *store.Queries, purpose auth.Purpose, linkingUserID uuid.UUID) (txCookie *http.Cookie, tx auth.Transaction) {
	t.Helper()

	txStore := auth.NewTransactionStore(q)
	handle, transaction, err := txStore.Begin(context.Background(), auth.ProviderGoogle, purpose, linkingUserID, testPublicOrigin+auth.GoogleCallbackPath)
	if err != nil {
		t.Fatalf("TransactionStore.Begin() error = %v", err)
	}
	return requestCookie(auth.OAuthTxCookieName, handle), transaction
}

// sessionRowReauthenticatedAt reads sessions.reauthenticated_at for id
// directly, mirroring session_adversarial_test.go's sessionRowLastSeenAt/
// sessionRowAbsoluteExpiresAt convention.
func sessionRowReauthenticatedAt(ctx context.Context, t *testing.T, db rowQuerier, id uuid.UUID) time.Time {
	t.Helper()
	var ts time.Time
	if err := db.QueryRow(ctx, `SELECT reauthenticated_at FROM sessions WHERE id = $1`, id).Scan(&ts); err != nil {
		t.Fatalf("sessionRowReauthenticatedAt(%s) query error: %v", id, err)
	}
	return ts
}

// TestLink_RejectsWithoutRecentReauth is the brief's Step 3 row 1: a
// session whose reauthenticated_at is stale (>15m, session.go's
// reauthWindow) attempting a purpose=link /start must be rejected, and --
// the row's own explicit "no transaction even created" clause --
// TransactionStore.Begin must never run at all: the gate has to be
// checked BEFORE Begin, not after (a stale caller must never even get a
// live, single-use transaction handle to carry forward).
//
// Confirmed against the landed startPurposeAndLinkingUser (link.go): a
// `?purpose=link` query parameter on the existing GET .../start route is
// exactly right (Purpose(r.URL.Query().Get("purpose")) compared against
// PurposeLink/PurposeReauth), and the reauth gate (RequireRecentReauth)
// runs BEFORE TransactionStore.Begin, per this test's own assertions
// below -- no change needed; this was an ADAPT guess during derivation.
// Drives Google (the brief's own "cheapest provider" convention for
// shared-logic coverage).
//
// Glue added post-landing (fix round 1, DD-C16/C1): the request below now
// carries an Origin header matching testPublicOrigin. DD-C16 added a
// same-site-initiation check (sameSiteInitiated, csrf.go) that runs
// BEFORE the reauth gate this test targets -- an unadorned request (no
// Sec-Fetch-Site, no Origin, no Referer) now fails closed at that EARLIER
// check with 403 csrf_rejected, never reaching the reauth check this test
// exists to prove. This is glue restoring the request to a same-site shape
// (a real settings-page-initiated request would send one of these
// signals), not a change to what property is being asserted.
//
// Response-shape reconciliation (fix round 2, DD-C17, owner ruling): the
// rejection itself was originally a raw JSON 403 (this test's own
// original assertions, matching the DD-C16-era wire shape). DD-C17
// corrected this: GET /start is a top-level browser navigation exactly
// like /callback, so a stale-reauth rejection now redirects (302) to
// PublicOrigin + "/app/settings/sessions?error=reauth_required" -- the
// settings page that initiated the flow already renders this code and
// prompts for step-up -- rather than showing the visitor a raw JSON
// document. Only the response-shape assertions below changed; the
// underlying scenario (a stale-reauth purpose=link /start must reject
// before Begin ever runs) is exactly as originally authored.
func TestLink_RejectsWithoutRecentReauth(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))
	pool := newRowInspectorPool(t)

	userID := createTestUser(t, q)
	raw, sess := issueTestSession(t, q, userID)
	forceReauthenticatedAtStale(t, sess.ID) // sessions_handlers_test.go: reauthenticated_at -> now-20m.

	resp := doJSON(t, handler, http.MethodGet, auth.GoogleStartPath+"?purpose=link", testPublicOrigin, "", "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("GET %s?purpose=link with a stale-reauth session status = %d, want %d (DD-C17: a redirect, not a raw JSON 403)", auth.GoogleStartPath, resp.StatusCode, http.StatusFound)
	}
	wantLocation := testPublicOrigin + wantSettingsSessionsPath + "?error=reauth_required"
	if got := resp.Header.Get("Location"); got != wantLocation {
		t.Errorf("Location = %q, want %q (DD-C17: the settings page that initiated the flow, with ?error=reauth_required)", got, wantLocation)
	}
	if tx := extractCookie(resp, auth.OAuthTxCookieName); tx != nil && tx.MaxAge >= 0 {
		t.Error("a LIVE __Host-oauth-tx cookie was set despite the reauth rejection, want none -- no transaction may be created before the reauth gate passes")
	}
	if got := oauthTransactionCountForLinkingUser(context.Background(), t, pool, userID); got != 0 {
		t.Errorf("oauth_transactions row count for linking_user_id=%s = %d, want 0 (Begin must never run before the reauth gate passes)", userID, got)
	}
}

// TestLink_RejectsIdentityAlreadyClaimedByAnotherUser is the brief's Step
// 3 row 2: user B attempts to link a (provider, sub) that user A's
// identity already owns. Per the brief's own Link algorithm step 2: "If
// it belongs to a different user, reject (409 identity_already_linked --
// this is the case that prevents hijacking someone else's already-claimed
// provider identity by linking it onto your own account)."
//
// This is the inverse account-takeover shape from the Step 2 matrix:
// there, an ATTACKER tries to get pulled INTO a victim's account via
// email; here, an attacker (as user B) tries to pull the VICTIM's already-
// claimed provider identity onto the attacker's OWN account, which would
// let the attacker's own account impersonate the victim to any relying
// party that trusts "this app says user B owns this Google/GitHub/
// LinkedIn identity" (e.g. a future feature reading provider profile data
// through that identity).
//
// The transaction is constructed directly via TransactionStore.Begin
// (task-5-brief.md Step 2's own established carve-out for exactly this
// situation -- see linkedin_test.go's package doc comment: "this test can
// construct the transaction directly ... to stay scoped to LinkedIn's
// rule rather than depending on Task 10's HTTP surface"), not through a
// real /start round trip -- this isolates the test from
// TestLink_RejectsWithoutRecentReauth's own /start-wiring ADAPT risk
// entirely, since Begin has no reauth gate of its own (transaction.go);
// the gate is /start's job, already covered by row 1.
//
// Confirmed against the landed link.go/handlers.go during reconciliation:
// DD-C15 landed exactly as this suite's own "409 vs 302" ADAPT resolution
// during derivation -- 302 ?error=identity_already_linked
// (identityAlreadyLinkedErrorCode, handlers.go), via
// redirectLinkOrReauthError -> redirectWithError, never a raw JSON 409 --
// with one correction: the redirect TARGET is settingsSessionsPath
// ("/app/settings/sessions", DD-C15), not "/login" as this suite
// originally guessed (derivation predates DD-C15's settings-page
// redirect decision entirely). Updated below.
func TestLink_RejectsIdentityAlreadyClaimedByAnotherUser(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))
	ctx := context.Background()

	// User A already owns a Google identity (an ordinary prior login).
	victimEmail := uniqueEmail(t)
	victimSubject, victimResp := googleLoginAttempt(t, handler, p, victimEmail) //nolint:bodyclose // googleLoginAttempt -> doCallback/doGet closes the body itself before returning.
	if victimResp.StatusCode != http.StatusFound || extractCookie(victimResp, auth.SessionCookieName) == nil {
		t.Fatalf("victim's own login did not succeed (status=%d) -- setup is broken, not the code under test", victimResp.StatusCode)
	}
	victim, err := q.GetUserByEmail(ctx, victimEmail)
	if err != nil {
		t.Fatalf("GetUserByEmail(victim) error = %v", err)
	}
	victimIdentityBefore, err := q.GetIdentityByProviderSubject(ctx, store.GetIdentityByProviderSubjectParams{
		Provider: string(auth.ProviderGoogle), ProviderUserID: victimSubject,
	})
	if err != nil {
		t.Fatalf("GetIdentityByProviderSubject(victim) error = %v", err)
	}

	// User B (the attacker/confused deputy) is a completely separate
	// account, and starts a link transaction naming ITSELF as
	// LinkingUserID.
	attackerUserID := createTestUser(t, q)
	txCookie, tx := beginGoogleTransaction(t, q, auth.PurposeLink, attackerUserID)

	// The callback presents the VICTIM's own Google subject -- as if the
	// attacker's browser had somehow completed a real Google consent
	// screen for the victim's Google account (e.g. a session-fixation-
	// adjacent trick, or simply the attacker also controls that Google
	// account and is trying to weld it onto a second aboutme account it
	// already claimed here) -- exercising exactly the "identity belongs
	// to a DIFFERENT user than LinkingUserID" branch.
	code := "code-link-hijack-" + uuid.NewString()
	p.RegisterCode(code, oidctest.Claims{
		Subject:       victimSubject,
		Email:         uniqueEmail(t), // link has no email check at all (brief, verbatim) -- deliberately unrelated to victimEmail.
		EmailVerified: ptrTrue(),
		Nonce:         tx.Nonce,
	})
	resp := doCallback(t, handler, code, tx.State, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("link-hijack attempt status = %d, want %d (DD-C15: 302, never a raw JSON 409)", resp.StatusCode, http.StatusFound)
	}
	loc := resp.Header.Get("Location")
	if got := mustQueryParam(t, loc, "error"); got != "identity_already_linked" {
		t.Errorf("error param = %q, want %q", got, "identity_already_linked")
	}
	assertRedirectPath(t, loc, wantSettingsSessionsPath) // DD-C15: link/reauth outcomes target the settings page, never "/login".

	// The hard invariant, independent of the exact wire shape above: the
	// attacker must never come out of this authenticated as the victim.
	if sc := extractCookie(resp, auth.SessionCookieName); sc != nil {
		t.Errorf("response set a %s cookie on a rejected link-hijack attempt (value=%q), want none", auth.SessionCookieName, sc.Value)
	}

	// "No row mutated": the victim's identity must still point at the
	// victim, byte-identical to before the attack.
	victimIdentityAfter, err := q.GetIdentityByProviderSubject(ctx, store.GetIdentityByProviderSubjectParams{
		Provider: string(auth.ProviderGoogle), ProviderUserID: victimSubject,
	})
	if err != nil {
		t.Fatalf("GetIdentityByProviderSubject(victim) after the attempt error = %v, want the row still present", err)
	}
	if !reflect.DeepEqual(victimIdentityBefore, victimIdentityAfter) {
		t.Errorf("victim's identity row changed by a rejected link-hijack attempt:\n before: %+v\n after:  %+v", victimIdentityBefore, victimIdentityAfter)
	}
	if victimIdentityAfter.UserID != victim.ID {
		t.Errorf("victim identity.UserID = %v, want %v (must never be reassigned to the attacker)", victimIdentityAfter.UserID, victim.ID)
	}
	if victimIdentityAfter.UserID == attackerUserID {
		t.Fatal("victim's identity now belongs to the attacker's user id -- account takeover via link succeeded, want rejection")
	}
}

// TestLink_IdempotentWhenAlreadyLinkedToSelf is the brief's Step 3 row 3:
// linking the SAME (provider, sub) to the SAME already-linked user a
// second time must succeed as a no-op -- never a 500 from a naive
// re-INSERT hitting identities_provider_subject_key's UNIQUE (provider,
// provider_user_id) constraint, and never a second, duplicate row.
//
// Uses TransactionStore.Begin directly for both link transactions, the
// same established carve-out TestLink_RejectsIdentityAlreadyClaimedByAnotherUser
// documents.
//
// Reconciled against the landed handlers.go/link.go: a link success
// redirects to settingsSessionsPath ("/app/settings/sessions", DD-C15),
// not the bare "/" this suite originally guessed during derivation (the
// derivation-time inference -- "reuses the login success redirect,
// unchanged by Purpose" -- turned out wrong; DD-C15 gives link/reauth
// their own success target). It ALSO sets NO session cookie at all on a
// link success -- resolveLinkOrReauth's own idempotent-no-op branch (the
// "already linked to the SAME user, purpose == PurposeLink" case) never
// calls touchReauthenticatedForCurrentSession (that is reauth-only) or
// SessionManager.Issue (that is PurposeLogin-only), so the caller simply
// keeps whichever session cookie they already had; this suite's original
// "must set a session cookie" assertion was wrong and is removed below,
// not merely retargeted.
func TestLink_IdempotentWhenAlreadyLinkedToSelf(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))
	ctx := context.Background()
	pool := newRowInspectorPool(t)

	userID := createTestUser(t, q)
	subject := uniqueSubject(t)

	linkOnce := func(email string) *http.Response {
		t.Helper()
		txCookie, tx := beginGoogleTransaction(t, q, auth.PurposeLink, userID)
		code := "code-link-idempotent-" + uuid.NewString()
		p.RegisterCode(code, oidctest.Claims{
			Subject:       subject,
			Email:         email,
			EmailVerified: ptrTrue(),
			Nonce:         tx.Nonce,
		})
		return doCallback(t, handler, code, tx.State, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.
	}

	firstResp := linkOnce(uniqueEmail(t)) //nolint:bodyclose // linkOnce -> doCallback/doGet closes the body itself before returning.
	if firstResp.StatusCode != http.StatusFound {
		t.Fatalf("first link attempt status = %d, want %d", firstResp.StatusCode, http.StatusFound)
	}
	assertRedirectPath(t, firstResp.Header.Get("Location"), wantSettingsSessionsPath) // DD-C15.
	if sc := extractCookie(firstResp, auth.SessionCookieName); sc != nil {
		t.Errorf("first link response set a %s cookie (value=%q) on a link success, want none -- the caller already has one", auth.SessionCookieName, sc.Value)
	}

	firstIdentity, err := q.GetIdentityByProviderSubject(ctx, store.GetIdentityByProviderSubjectParams{
		Provider: string(auth.ProviderGoogle), ProviderUserID: subject,
	})
	if err != nil {
		t.Fatalf("GetIdentityByProviderSubject after first link error = %v, want a created identity row", err)
	}
	if firstIdentity.UserID != userID {
		t.Fatalf("first-link identity.UserID = %v, want %v", firstIdentity.UserID, userID)
	}

	// Second link of the EXACT same (provider, sub) to the SAME user --
	// per the brief's own "no email check at all for linking", the email
	// presented this time is deliberately different, and must not matter.
	secondResp := linkOnce(uniqueEmail(t)) //nolint:bodyclose // linkOnce -> doCallback/doGet closes the body itself before returning.
	if secondResp.StatusCode != http.StatusFound {
		t.Fatalf("second (idempotent) link attempt status = %d, want %d -- must succeed as a no-op, never 500", secondResp.StatusCode, http.StatusFound)
	}
	assertRedirectPath(t, secondResp.Header.Get("Location"), wantSettingsSessionsPath) // DD-C15.
	if sc := extractCookie(secondResp, auth.SessionCookieName); sc != nil {
		t.Errorf("second (idempotent) link response set a %s cookie (value=%q), want none -- the idempotent no-op branch never issues or touches a session", auth.SessionCookieName, sc.Value)
	}

	if got := identityCountForProviderSubject(ctx, t, pool, string(auth.ProviderGoogle), subject); got != 1 {
		t.Errorf("identities row count for (google, %q) after linking the same identity twice = %d, want exactly 1 (a naive re-insert would either 500 on the UNIQUE constraint or, worse, silently duplicate)", subject, got)
	}
	secondIdentity, err := q.GetIdentityByProviderSubject(ctx, store.GetIdentityByProviderSubjectParams{
		Provider: string(auth.ProviderGoogle), ProviderUserID: subject,
	})
	if err != nil {
		t.Fatalf("GetIdentityByProviderSubject after second link error = %v", err)
	}
	if !reflect.DeepEqual(firstIdentity, secondIdentity) {
		t.Errorf("identity row changed across the idempotent re-link (want byte-identical, i.e. no delete+reinsert):\n first:  %+v\n second: %+v", firstIdentity, secondIdentity)
	}
}

// TestPurposeReauth_RefreshesReauthenticatedAt_ButDoesNotCreateIdentity is
// the brief's Step 3 row 4: a purpose=reauth round trip against an
// ALREADY-linked provider must bump sessions.reauthenticated_at (the
// whole point of a reauth transaction: refreshing RequireRecentReauth's
// window ahead of a later sensitive operation) and must touch nothing in
// identities -- reauth is emphatically not a second, silent link.
//
// Uses an injected fake clock (testutil.NewClockAtEpoch,
// auth.NewSessionManagerForTest, auth.SetSessionManagerForTest -- the
// same seam sessions_adversarial_test.go's own
// TestRequireSession_RotatedRequest_CarriesSuccessorCookie already
// establishes for driving time-dependent session behavior deterministically
// through the real HTTP chain) so "refreshes" is a real before/after
// inequality, not a same-instant no-op that would pass vacuously.
//
// Confirmed against the landed touchReauthenticatedForCurrentSession
// (link.go): the "which session does reauth touch" ADAPT guess was
// exactly right -- it re-reads and re-authenticates the __Host-session
// cookie off the CURRENT /callback request (readAndAuthenticateSession,
// the same helper RequireSession itself now calls -- factored out
// specifically for this and startPurposeAndLinkingUser's own use, per
// handlers.go's own doc comment), then verifies the re-authenticated
// session's UserID equals tx.LinkingUserID before touching anything. No
// change needed to that assumption. This is the one place in this file
// /callback is assumed to read a session cookie at all for a link/reauth
// transaction (the two Link tests above correctly do NOT attach one,
// matching resolveLinkOrReauth's own tx.LinkingUserID-only resolution for
// everything except this one reauth branch).
//
// Bug fixed during reconciliation review (unrelated to any ADAPT
// marker): the original draft issued the session for a freshly
// createTestUser'd userID, then separately drove googleLoginAttempt --
// which always creates its OWN new user for an unknown identity
// (resolveLoginIdentity's NewUser branch) -- and used THAT user as
// tx.LinkingUserID. The session and the linking user were therefore two
// different users, which touchReauthenticatedForCurrentSession's own
// `sess.UserID != linkingUserID` check would have rejected every time
// (errLinkOrReauthRejected), making the test fail for a setup reason
// having nothing to do with the property under test. Fixed by issuing
// the session for the SAME user the prior Google login actually created,
// not a separately minted one.
func TestPurposeReauth_RefreshesReauthenticatedAt_ButDoesNotCreateIdentity(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	q := newTestQueries(t)
	pool := newRowInspectorPool(t)
	ctx := context.Background()

	cfg := config.Config{
		PublicOrigin:       testPublicOrigin,
		GoogleClientID:     oidctest.DefaultClientID,
		GoogleClientSecret: "test-google-client-secret",
	}
	svc, err := auth.NewServiceForTest(testLogger(), cfg, q, p.URL, "", "")
	if err != nil {
		t.Fatalf("NewServiceForTest() error = %v", err)
	}
	clk := testutil.NewClockAtEpoch()
	sm := auth.NewSessionManagerForTest(q, clk.Now)
	auth.SetSessionManagerForTest(svc, sm)
	handler := api.New(testLogger(), noopPinger{}, api.Options{}, svc.RegisterRoutes)

	// An already-linked Google identity (ordinary prior login, real HTTP
	// round trip against the same handler/db) -- resolveLoginIdentity's
	// NewUser branch creates its own new user for this unknown identity.
	subject, priorResp := googleLoginAttempt(t, handler, p, uniqueEmail(t)) //nolint:bodyclose // googleLoginAttempt -> doCallback/doGet closes the body itself before returning.
	if priorResp.StatusCode != http.StatusFound {
		t.Fatalf("prior google login status = %d, want %d -- setup is broken, not the code under test", priorResp.StatusCode, http.StatusFound)
	}
	// A REJECTED login is ALSO a 302 (to /login?error=...), so the status
	// check above alone cannot tell success from rejection -- checked here
	// explicitly rather than only inferred from the GetIdentityByProviderSubject
	// call below finding nothing (which would report a confusing "no rows"
	// error pointing at the wrong step).
	if extractCookie(priorResp, auth.SessionCookieName) == nil {
		t.Fatalf("prior google login set no session cookie (Location=%q) -- setup is broken (a REJECTED login is also a 302; this must be an actual success)", priorResp.Header.Get("Location"))
	}
	priorIdentity, err := q.GetIdentityByProviderSubject(ctx, store.GetIdentityByProviderSubjectParams{
		Provider: string(auth.ProviderGoogle), ProviderUserID: subject,
	})
	if err != nil {
		t.Fatalf("GetIdentityByProviderSubject(prior) error = %v", err)
	}
	linkedUserID := priorIdentity.UserID
	identitiesBefore, err := q.ListIdentitiesByUserID(ctx, linkedUserID)
	if err != nil {
		t.Fatalf("ListIdentitiesByUserID(before) error = %v", err)
	}

	// The session under test belongs to the SAME user the prior login
	// just created -- see this test's own doc comment for why that
	// matters (touchReauthenticatedForCurrentSession's sess.UserID ==
	// linkingUserID check).
	raw, sess, err := sm.Issue(ctx, linkedUserID, "test-agent/1.0", "203.0.113.77")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	before := sessionRowReauthenticatedAt(ctx, t, pool, sess.ID)
	if !before.Equal(clk.Now()) {
		t.Fatalf("session reauthenticated_at at Issue = %v, want exactly the clock's start time %v -- setup assumption broken", before, clk.Now())
	}
	clk.Advance(20 * time.Minute) // simulate real elapsed time between login and this reauth round trip.

	txCookie, tx := beginGoogleTransaction(t, q, auth.PurposeReauth, linkedUserID)
	code := "code-reauth-refresh-" + uuid.NewString()
	p.RegisterCode(code, oidctest.Claims{
		Subject:       subject, // the SAME already-linked identity.
		Email:         uniqueEmail(t),
		EmailVerified: ptrTrue(),
		Nonce:         tx.Nonce,
	})
	resp := doGet(t, handler, auth.GoogleCallbackPath+"?code="+code+"&state="+tx.State, txCookie, sessionRequestCookie(raw)) //nolint:bodyclose // doGet closes the body itself before returning.

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("reauth round trip status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	assertRedirectPath(t, resp.Header.Get("Location"), wantSettingsSessionsPath) // DD-C15.
	if sc := extractCookie(resp, auth.SessionCookieName); sc != nil && sc.Value != raw {
		t.Errorf("reauth response set a NEW %s cookie (value=%q) distinct from the caller's own, want none or the SAME rotated value only", auth.SessionCookieName, sc.Value)
	}

	after := sessionRowReauthenticatedAt(ctx, t, pool, sess.ID)
	if after.Equal(before) {
		t.Error("sessions.reauthenticated_at unchanged after a purpose=reauth round trip, want it refreshed to the (advanced) current time")
	}
	if !after.Equal(clk.Now()) {
		t.Errorf("sessions.reauthenticated_at after reauth = %v, want exactly the clock's current time %v", after, clk.Now())
	}

	identitiesAfter, err := q.ListIdentitiesByUserID(ctx, linkedUserID)
	if err != nil {
		t.Fatalf("ListIdentitiesByUserID(after) error = %v", err)
	}
	if !reflect.DeepEqual(identitiesBefore, identitiesAfter) {
		t.Errorf("identities for the reauthenticated user changed by a purpose=reauth round trip (want untouched): before=%+v after=%+v", identitiesBefore, identitiesAfter)
	}
}
