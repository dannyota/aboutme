// These adversarial tests cover email-merge rejection, link ownership,
// idempotency, recent reauthentication, and reauthentication isolation. See
// docs/design/security.md.
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

// googleLoginAttempt drives a fresh Google login and returns its subject and
// response.
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

// linkedInLoginAttempt uses a verified email so LinkedIn's registration rule
// cannot confound collision assertions.
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

// githubIdentity is a single-use account because gitHubStub fixes its code,
// user, and emails at construction.
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

// githubAttempt requires handler to be constructed with gi.stub because the
// endpoint override is immutable.
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

// assertNoIdentityForProvider proves rejection created no provider identity.
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

// ==== email-merge rejection matrix ====

// matrixCase describes one existing-account and login-attempt pairing.
type matrixCase struct {
	name             string
	existingProvider auth.Provider
	attemptProvider  auth.Provider
	// sameEmail: true for every collision row (six cross-provider pairs
	// plus the same-provider row); false only for the one explicit
	// non-merge row (different email must succeed as an ordinary NewUser).
	sameEmail bool
	// wantCollision is separate from sameEmail so the table states its
	// expected outcome rather than deriving it in the assertion.
	wantCollision bool
}

// assertEmailCollisionRejected checks the browser redirect, cookie effects,
// and no-oracle rule: it may name the attempted provider but never the provider
// already attached to the victim.
func assertEmailCollisionRejected(t *testing.T, resp *http.Response, attemptProvider, existingProvider auth.Provider) {
	t.Helper()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("email-collision attempt status = %d, want %d (a redirect, never a raw JSON 409 -- /callback is a top-level browser navigation)", resp.StatusCode, http.StatusFound)
	}
	loc := resp.Header.Get("Location")
	if got := mustQueryParam(t, loc, "error"); got != "email_already_registered" {
		t.Errorf("error param = %q, want %q", got, "email_already_registered")
	}
	assertRedirectPath(t, loc, "/login") // Every rejection targets /login, never "/".

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

	// The provider parameter names the attempted provider, which the callback
	// URL already disclosed, never the provider attached to the existing user.
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

	// Build the GitHub fixture before the Service because its endpoint override
	// is immutable. At most one role uses GitHub in each case.
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

		// ... and the victim's account row is byte-identical, not merely present.
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

	// A different email must not trigger the collision defense.
	if attemptResp.StatusCode != http.StatusFound {
		t.Fatalf("%s: attempt login status = %d, want %d (a different-email NewUser login must succeed, never be treated as a collision)", c.name, attemptResp.StatusCode, http.StatusFound)
	}
	assertRedirectPath(t, attemptResp.Header.Get("Location"), "/app/resumes") // success path, not /login.
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

// TestCallback_EmailMergeRejectionMatrix covers all six cross-provider
// directions, a different-email success, and a same-provider collision with a
// different subject. Automatic email merge would permit account takeover.
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

// ==== link and reauthentication matrix ====

// wantSettingsSessionsPath mirrors the unexported production constant. Every
// link or reauthentication callback redirects here on success or rejection.
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

// identityCountForProviderSubject proves an idempotent link does not insert a
// duplicate row, rather than inferring that property from the HTTP response.
func identityCountForProviderSubject(ctx context.Context, t *testing.T, db rowQuerier, provider, providerUserID string) int {
	t.Helper()
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM identities WHERE provider = $1 AND provider_user_id = $2`, provider, providerUserID).Scan(&count); err != nil {
		t.Fatalf("identityCountForProviderSubject query error: %v", err)
	}
	return count
}

// beginGoogleTransaction creates a provider-bound transaction and matching
// request cookie for callback tests that do not exercise the start endpoint.
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

// TestLink_RejectsWithoutRecentReauth proves a stale session is rejected
// before TransactionStore.Begin. A stale caller must never receive a live,
// single-use transaction handle.
//
// The link start is a same-origin, CSRF-protected POST. The response is a JSON
// 403 with reauth_required, and the row assertions prove no transaction was
// persisted before the gate failed.
func TestLink_RejectsWithoutRecentReauth(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))
	pool := newRowInspectorPool(t)

	userID := createTestUser(t, q)
	raw, sess := issueTestSession(t, q, userID)
	forceReauthenticatedAtStale(t, sess.ID) // sessions_handlers_test.go: reauthenticated_at -> now-20m.

	resp := doJSON(t, handler, http.MethodPost, auth.GoogleStartPath+"?purpose=link", testPublicOrigin, csrfTokenFor(sess), "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST %s?purpose=link with a stale-reauth session status = %d, want %d", auth.GoogleStartPath, resp.StatusCode, http.StatusForbidden)
	}
	if got := decodeErrorCode(t, resp); got != "reauth_required" {
		t.Errorf("error.code = %q, want %q (DD-C11's code, shared with the session-authenticated endpoints)", got, "reauth_required")
	}
	if tx := extractCookie(resp, auth.OAuthTxCookieName); tx != nil && tx.MaxAge >= 0 {
		t.Error("a LIVE __Host-oauth-tx cookie was set despite the reauth rejection, want none -- no transaction may be created before the reauth gate passes")
	}
	if got := oauthTransactionCountForLinkingUser(context.Background(), t, pool, userID); got != 0 {
		t.Errorf("oauth_transactions row count for linking_user_id=%s = %d, want 0 (Begin must never run before the reauth gate passes)", userID, got)
	}
}

// TestLink_RejectsIdentityAlreadyClaimedByAnotherUser proves user B cannot
// claim user A's provider identity. Direct transaction setup isolates callback
// ownership checks from the start endpoint.
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
	// account, starts a link transaction naming itself as LinkingUserID, and
	// presents its own session so callback authentication reaches the ownership
	// check under test.
	attackerUserID := createTestUser(t, q)
	attackerRaw, _ := issueTestSession(t, q, attackerUserID)
	txCookie, tx := beginGoogleTransaction(t, q, auth.PurposeLink, attackerUserID)

	// Present the victim's subject while authenticated as the attacker.
	code := "code-link-hijack-" + uuid.NewString()
	p.RegisterCode(code, oidctest.Claims{
		Subject:       victimSubject,
		Email:         uniqueEmail(t), // Linking deliberately ignores this unrelated email.
		EmailVerified: ptrTrue(),
		Nonce:         tx.Nonce,
	})
	resp := doCallback(t, handler, code, tx.State, txCookie, sessionRequestCookie(attackerRaw)) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("link-hijack attempt status = %d, want %d (DD-C15: 302, never a raw JSON 409)", resp.StatusCode, http.StatusFound)
	}
	loc := resp.Header.Get("Location")
	if got := mustQueryParam(t, loc, "error"); got != "identity_already_linked" {
		t.Errorf("error param = %q, want %q", got, "identity_already_linked")
	}
	assertRedirectPath(t, loc, wantSettingsSessionsPath) // Link outcomes target settings, never "/login".

	// Rejection must never authenticate the attacker as the victim.
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

// TestLink_IdempotentWhenAlreadyLinkedToSelf proves a repeated link is a no-op,
// not a duplicate insert. Each direct callback carries the user's live session.
func TestLink_IdempotentWhenAlreadyLinkedToSelf(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))
	ctx := context.Background()
	pool := newRowInspectorPool(t)

	userID := createTestUser(t, q)
	raw, _ := issueTestSession(t, q, userID)
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
		return doCallback(t, handler, code, tx.State, txCookie, sessionRequestCookie(raw)) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.
	}

	firstResp := linkOnce(uniqueEmail(t)) //nolint:bodyclose // linkOnce -> doCallback/doGet closes the body itself before returning.
	if firstResp.StatusCode != http.StatusFound {
		t.Fatalf("first link attempt status = %d, want %d", firstResp.StatusCode, http.StatusFound)
	}
	assertRedirectPath(t, firstResp.Header.Get("Location"), wantSettingsSessionsPath) // Link success returns to settings.
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

	// The second link presents a different email to prove linking keys only on
	// provider subject and does not apply the login email rule.
	secondResp := linkOnce(uniqueEmail(t)) //nolint:bodyclose // linkOnce -> doCallback/doGet closes the body itself before returning.
	if secondResp.StatusCode != http.StatusFound {
		t.Fatalf("second (idempotent) link attempt status = %d, want %d -- must succeed as a no-op, never 500", secondResp.StatusCode, http.StatusFound)
	}
	assertRedirectPath(t, secondResp.Header.Get("Location"), wantSettingsSessionsPath) // Link success returns to settings.
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

// TestPurposeReauth_RefreshesReauthenticatedAt_ButDoesNotCreateIdentity uses an
// injected clock to prove reauthentication refreshes the matching session
// without mutating identities.
func TestPurposeReauth_RefreshesReauthenticatedAt_ButDoesNotCreateIdentity(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	svcPool := newTestPool(t)
	q := store.New(svcPool)
	pool := newRowInspectorPool(t)
	ctx := context.Background()

	cfg := config.Config{
		PublicOrigin:         testPublicOrigin,
		ProviderLoginEnabled: true,
		GoogleClientID:       oidctest.DefaultClientID,
		GoogleClientSecret:   "test-google-client-secret",
	}
	svc, err := auth.NewServiceForTest(testLogger(), cfg, svcPool, p.URL, "", "")
	if err != nil {
		t.Fatalf("NewServiceForTest() error = %v", err)
	}
	clk := testutil.NewClockAtEpoch()
	sm := auth.NewSessionManagerForTest(q, clk.Now)
	auth.SetSessionManagerForTest(svc, sm)
	handler := api.New(testLogger(), noopPinger{}, api.Options{}, nil, svc.RegisterRoutes)

	// Create the existing link through a real login against this handler.
	subject, priorResp := googleLoginAttempt(t, handler, p, uniqueEmail(t)) //nolint:bodyclose // googleLoginAttempt -> doCallback/doGet closes the body itself before returning.
	if priorResp.StatusCode != http.StatusFound {
		t.Fatalf("prior google login status = %d, want %d -- setup is broken, not the code under test", priorResp.StatusCode, http.StatusFound)
	}
	// Both success and rejection use 302, so require the session cookie too.
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

	// The reauthenticated session must belong to the linked user.
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
	assertRedirectPath(t, resp.Header.Get("Location"), wantSettingsSessionsPath) // Reauthentication returns to settings.
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
