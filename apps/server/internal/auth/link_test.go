// Package auth_test: this package's own (not independently authored)
// coverage for link.go, added during Task 10 fix round 1 in direct
// response to Opus review's findings on the original implementation:
//
//   - C1 (Critical, DD-C16): GET /auth/{provider}/start's same-site
//     enforcement for purpose=link/reauth (sameSiteInitiated, csrf.go) --
//     the control that closes the reauth-then-link cross-site chain the
//     review traced. See link.go's own top-of-file comment for the full
//     attack narrative this control closes.
//   - I4: purpose=reauth against an UNCLAIMED identity must reject with
//     no identity row created and no reauthenticated_at bump -- otherwise
//     reauth becomes an un-gated link primitive.
//   - I5: GitHub's own purpose=link round trip had zero coverage --
//     link.go's shared resolveLinkOrReauth is provider-generic by
//     construction, but "generic by construction" is exactly the kind of
//     claim a wiring bug (the wrong Provider constant passed at a
//     GitHub-specific call site) can silently defeat without a
//     provider-specific proof.
//   - I6: 401 session_required on ?purpose=link|reauth with no session at
//     all -- the most basic rejection startPurposeAndLinkingUser produces
//     had no dedicated test.
//
// link_adversarial_test.go (independently authored, Step 2/3) already
// covers the reauth-refresh happy path, the already-claimed-by-another-
// user rejection, the self-relink idempotency case, and the stale-reauth
// /start rejection -- none of that is duplicated here.
package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/auth/oidctest"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// ============================================================================
// C1 / DD-C16: same-site enforcement on purpose=link/reauth starts
// ============================================================================

// doGetWithHeaders is doGet's (handlers_test.go) sibling for this file's
// own same-site tests, which need to set Sec-Fetch-Site/Origin directly --
// a capability neither doGet nor doJSON (me_test.go) exposes (doJSON sets
// Origin only, via its own dedicated parameter, but has no way to set
// Sec-Fetch-Site at all).
func doGetWithHeaders(t *testing.T, handler http.Handler, path string, headers map[string]string, cookies ...*http.Cookie) *http.Response {
	t.Helper()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	resp := rec.Result()
	if err := resp.Body.Close(); err != nil {
		t.Errorf("close response body: %v", err)
	}
	return resp
}

// TestStart_SameSiteEnforcement_LinkAndReauth is C1's own required table:
// every combination of Sec-Fetch-Site/Origin the finding calls out,
// exercised for BOTH purpose=link and purpose=reauth (DD-C16 gates both
// identically -- see sameSiteInitiated's own doc comment, csrf.go, for
// why reauth needs this every bit as much as link does). Every case uses
// a session that is otherwise fully valid -- and, for purpose=link,
// already within RequireRecentReauth's window -- so a rejection can only
// be attributed to the same-site check itself, never a downstream gate
// masquerading as it.
func TestStart_SameSiteEnforcement_LinkAndReauth(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		secFetchSite string // "" = header absent.
		origin       string // "" = header absent.
		wantRejected bool
	}{
		// Sec-Fetch-Site present: its value is authoritative, no fallback.
		{"Sec-Fetch-Site: cross-site", "cross-site", "", true},
		{"Sec-Fetch-Site: same-site (not same-origin)", "same-site", "", true},
		{"Sec-Fetch-Site: none", "none", "", true},
		{"Sec-Fetch-Site: same-origin", "same-origin", "", false},
		// Sec-Fetch-Site absent: falls back to originAllowed (Origin/Referer).
		{"missing Sec-Fetch-Site, foreign Origin", "", "https://evil.example", true},
		{"missing Sec-Fetch-Site, matching Origin", "", testPublicOrigin, false},
		{"missing both signals entirely (fail closed)", "", "", true},
	}

	for _, purpose := range []auth.Purpose{auth.PurposeLink, auth.PurposeReauth} {
		for _, c := range cases {
			t.Run(string(purpose)+"/"+c.name, func(t *testing.T) {
				t.Parallel()

				p := oidctest.NewProvider(t)
				handler, q := newTestService(t, withGoogleIssuer(p.URL))
				pool := newRowInspectorPool(t)

				userID := createTestUser(t, q)
				raw, _ := issueTestSession(t, q, userID) // fresh reauth (Issue sets it to now) -- satisfies purpose=link's own gate too.

				headers := map[string]string{}
				if c.secFetchSite != "" {
					headers["Sec-Fetch-Site"] = c.secFetchSite
				}
				if c.origin != "" {
					headers["Origin"] = c.origin
				}

				resp := doGetWithHeaders(t, handler, auth.GoogleStartPath+"?purpose="+string(purpose), headers, sessionRequestCookie(raw)) //nolint:bodyclose // doGetWithHeaders closes the body itself before returning.

				if c.wantRejected {
					if resp.StatusCode != http.StatusForbidden {
						t.Fatalf("status = %d, want %d (DD-C16 same-site rejection)", resp.StatusCode, http.StatusForbidden)
					}
					if got := decodeErrorCode(t, resp); got != "csrf_rejected" {
						t.Errorf("error.code = %q, want %q", got, "csrf_rejected")
					}
					if tx := extractCookie(resp, auth.OAuthTxCookieName); tx != nil && tx.MaxAge >= 0 {
						t.Error("a LIVE __Host-oauth-tx cookie was set despite the same-site rejection, want none -- no transaction may be created before the same-site gate passes")
					}
					if got := oauthTransactionCountForLinkingUser(context.Background(), t, pool, userID); got != 0 {
						t.Errorf("oauth_transactions row count for linking_user_id=%s = %d, want 0 (Begin must never run before the same-site gate passes)", userID, got)
					}
					return
				}

				if resp.StatusCode != http.StatusFound {
					t.Fatalf("status = %d, want %d (a same-site-initiated, fully-authorized request must reach the provider redirect)", resp.StatusCode, http.StatusFound)
				}
				if txc := extractCookie(resp, auth.OAuthTxCookieName); txc == nil {
					t.Error("a same-site request did not set __Host-oauth-tx, want a live transaction")
				}
			})
		}
	}
}

// TestStart_PurposeLogin_UnaffectedBySameSiteEnforcement proves DD-C16's
// scope is exactly purpose=link/reauth: an ordinary purpose=login start
// (the default -- no ?purpose= at all) must succeed even when every
// same-site signal says the request is cross-site, since a login start
// must stay reachable from anywhere (a bookmarked or shared "sign in"
// link, another site's "continue with aboutme" button).
func TestStart_PurposeLogin_UnaffectedBySameSiteEnforcement(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL))

	resp := doGetWithHeaders(t, handler, auth.GoogleStartPath, map[string]string{ //nolint:bodyclose // doGetWithHeaders closes the body itself before returning.
		"Sec-Fetch-Site": "cross-site",
		"Origin":         "https://evil.example",
	})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("purpose=login start with cross-site signals status = %d, want %d (DD-C16 must not apply to purpose=login)", resp.StatusCode, http.StatusFound)
	}
	if txc := extractCookie(resp, auth.OAuthTxCookieName); txc == nil {
		t.Error("purpose=login start did not set __Host-oauth-tx despite succeeding")
	}
}

// ============================================================================
// I4: purpose=reauth against an UNCLAIMED identity must reject
// ============================================================================

// TestPurposeReauth_RejectsUnclaimedIdentity_NoIdentityCreatedNoReauthBump
// is fix round 1's I4: the mirror image of link_adversarial_test.go's
// TestPurposeReauth_RefreshesReauthenticatedAt_ButDoesNotCreateIdentity
// (which proves reauth against an ALREADY-linked identity refreshes
// reauthenticated_at and touches nothing in identities). This test proves
// the other half of "no auto-link via reauth" (design spec §3): reauth
// against an identity NO ONE has ever linked must reject outright --
// creating NEITHER an identities row NOR bumping reauthenticated_at.
// Without this, reauth would be an un-gated link primitive (design spec
// requires purpose=link's own recent-reauth gate; purpose=reauth has none
// of its own precisely because it is never supposed to attach anything
// new).
func TestPurposeReauth_RejectsUnclaimedIdentity_NoIdentityCreatedNoReauthBump(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))
	pool := newRowInspectorPool(t)
	ctx := context.Background()

	userID := createTestUser(t, q)
	raw, sess := issueTestSession(t, q, userID)
	before := sessionRowReauthenticatedAt(ctx, t, pool, sess.ID)

	txCookie, tx := beginGoogleTransaction(t, q, auth.PurposeReauth, userID)

	subject := uniqueSubject(t) // brand new -- never linked to ANY user.
	code := "code-reauth-unclaimed-" + uuid.NewString()
	p.RegisterCode(code, oidctest.Claims{
		Subject:       subject,
		Email:         uniqueEmail(t),
		EmailVerified: ptrTrue(),
		Nonce:         tx.Nonce,
	})

	resp := doGet(t, handler, auth.GoogleCallbackPath+"?code="+code+"&state="+tx.State, txCookie, sessionRequestCookie(raw)) //nolint:bodyclose // doGet closes the body itself before returning.

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	loc := resp.Header.Get("Location")
	if got := mustQueryParam(t, loc, "error"); got != "auth_failed" {
		t.Errorf("error param = %q, want %q (no auto-link via reauth -- the generic, no-oracle rejection, not identity_already_linked -- nobody else claims this identity either)", got, "auth_failed")
	}
	assertRedirectPath(t, loc, wantSettingsSessionsPath) // DD-C15.
	if sc := extractCookie(resp, auth.SessionCookieName); sc != nil && sc.Value != raw {
		t.Errorf("response set a NEW %s cookie (value=%q) distinct from the caller's own, want none or the same rotated value only", auth.SessionCookieName, sc.Value)
	}

	assertNoIdentityForProvider(t, q, auth.ProviderGoogle, subject)

	after := sessionRowReauthenticatedAt(ctx, t, pool, sess.ID)
	if !after.Equal(before) {
		t.Errorf("sessions.reauthenticated_at changed after a purpose=reauth attempt against an UNCLAIMED identity (before=%v after=%v), want unchanged -- reauth must never silently link", before, after)
	}
}

// ============================================================================
// I5: GitHub's own purpose=link branch had zero dedicated coverage
// ============================================================================

// TestGitHubCallback_LinkPurpose_AttachesUnclaimedIdentityToLinkingUser is
// fix round 1's I5: link.go's resolveLinkOrReauth is provider-generic by
// construction (it takes a bare auth.Provider/providerUserID pair, never
// anything GitHub/Google/LinkedIn-specific), but that is a claim about
// the SHARED function -- it says nothing about whether github.go's own
// callback actually wires the right Provider constant and providerUserID
// through to it. A copy-paste slip (e.g. ProviderGoogle hardcoded at a
// GitHub call site, mirroring the exact class of bug
// TestGitHubCallback_RejectionLogsProviderAttribute's own doc comment
// warns about for the login path) would be invisible without a
// GitHub-specific round trip. Mirrors
// TestLinkedInCallback_PurposeLink_AllowsUnverifiedEmail's (linkedin_
// adversarial_test.go) DD-C15 assertion shape for GitHub instead of
// LinkedIn.
//
// Carries a live __Host-session cookie for linkingUserID on the completing
// /callback request (fix, gate hardening: resolveLinkOrReauth's
// purpose=link arm now re-authenticates the completing request via
// authenticateLinkOrReauthSession -- link.go's own doc comment -- exactly
// like purpose=reauth already did; a link callback with no session, or one
// for a different user, is now rejected rather than resolving purely off
// tx.LinkingUserID).
func TestGitHubCallback_LinkPurpose_AttachesUnclaimedIdentityToLinkingUser(t *testing.T) {
	t.Parallel()

	gi := newGitHubIdentity(t, uniqueEmail(t)) // link has no email check at all (design spec) -- this email is never read.
	handler, q := newTestService(t, withGitHubEndpoint(gi.stub.URL))
	ctx := context.Background()

	linkingUserID := createTestUser(t, q)
	raw, _ := issueTestSession(t, q, linkingUserID)
	txStore := auth.NewTransactionStore(q)
	handle, tx, err := txStore.Begin(ctx, auth.ProviderGitHub, auth.PurposeLink, linkingUserID, testPublicOrigin+auth.GitHubCallbackPath)
	if err != nil {
		t.Fatalf("TransactionStore.Begin() error = %v", err)
	}
	txCookie := requestCookie(auth.OAuthTxCookieName, handle)

	resp := doGitHubCallback(t, handler, gi.code, tx.State, txCookie, sessionRequestCookie(raw)) //nolint:bodyclose // doGitHubCallback -> doGet closes the body itself before returning.

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d (redirect on a successful link)", resp.StatusCode, http.StatusFound)
	}
	wantLocation := testPublicOrigin + wantSettingsSessionsPath
	if got := resp.Header.Get("Location"); got != wantLocation {
		t.Errorf("callback Location = %q, want %q (DD-C15)", got, wantLocation)
	}
	if sc := extractCookie(resp, auth.SessionCookieName); sc != nil {
		t.Errorf("callback set a %s cookie (value=%q) on a link success, want none -- the caller already has one", auth.SessionCookieName, sc.Value)
	}

	identity, err := q.GetIdentityByProviderSubject(ctx, store.GetIdentityByProviderSubjectParams{
		Provider:       string(auth.ProviderGitHub),
		ProviderUserID: gi.subject,
	})
	if err != nil {
		t.Fatalf("GetIdentityByProviderSubject(github, %q) error = %v, want a created identity row", gi.subject, err)
	}
	if identity.UserID != linkingUserID {
		t.Errorf("identity.UserID = %v, want %v (the linking user -- a link flow must never create a new user)", identity.UserID, linkingUserID)
	}
}

// ============================================================================
// I6: 401 session_required on ?purpose=link|reauth with no session at all
// ============================================================================

// TestStart_PurposeLinkOrReauth_NoSession_RedirectsToLoginNoErrorCode is
// fix round 1's I6, reshaped by fix round 2's DD-C17: the most basic
// rejection startPurposeAndLinkingUser produces -- no __Host-session
// cookie at all -- had no dedicated test, and (before DD-C17) returned a
// raw JSON 401 on what is a top-level browser navigation. DD-C17 requires
// a 302 to PublicOrigin + "/login" instead, with NO ?error= query param at
// all -- arriving at the login page IS the message; a distinct code would
// announce that a link/reauth attempt was in flight. Every request here is
// otherwise same-site (a matching Origin header), so a rejection can only
// be attributed to the missing session, never DD-C16's same-site gate
// running first and masking it.
func TestStart_PurposeLinkOrReauth_NoSession_RedirectsToLoginNoErrorCode(t *testing.T) {
	t.Parallel()

	for _, purpose := range []auth.Purpose{auth.PurposeLink, auth.PurposeReauth} {
		t.Run(string(purpose), func(t *testing.T) {
			t.Parallel()

			p := oidctest.NewProvider(t)
			handler, _ := newTestService(t, withGoogleIssuer(p.URL))

			resp := doGetWithHeaders(t, handler, auth.GoogleStartPath+"?purpose="+string(purpose), map[string]string{ //nolint:bodyclose // doGetWithHeaders closes the body itself before returning.
				"Origin": testPublicOrigin,
			}) // deliberately no cookies at all.

			if resp.StatusCode != http.StatusFound {
				t.Fatalf("status = %d, want %d (DD-C17: a redirect, not a raw JSON 401 -- /start is a top-level browser navigation)", resp.StatusCode, http.StatusFound)
			}
			wantLocation := testPublicOrigin + "/login"
			if got := resp.Header.Get("Location"); got != wantLocation {
				t.Errorf("Location = %q, want %q (DD-C17: no ?error= code -- arriving at the login page IS the message)", got, wantLocation)
			}
			if extractCookie(resp, auth.SessionCookieName) == nil {
				t.Error("response did not clear the __Host-session cookie on a missing/invalid-session rejection, want it cleared (same hygiene as the JSON-API rejectSession)")
			} else if sc := extractCookie(resp, auth.SessionCookieName); sc.MaxAge >= 0 || sc.Value != "" {
				t.Errorf("__Host-session cookie = %+v, want cleared (empty value, negative Max-Age)", sc)
			}
			if tx := extractCookie(resp, auth.OAuthTxCookieName); tx != nil && tx.MaxAge >= 0 {
				t.Error("a LIVE __Host-oauth-tx cookie was set despite the missing-session rejection, want none -- no transaction row may exist before this gate passes")
			}
		})
	}
}

// ============================================================================
// Gate hardening: a pending purpose=link transaction must not survive
// "log out everywhere" (link.go's authenticateLinkOrReauthSession)
// ============================================================================
//
// Phase 1 whole-branch review, BLOCKING finding: resolveLinkOrReauth's
// purpose=link arm used to resolve entirely off tx.LinkingUserID (captured
// once, server-side, at /start time), never re-reading or re-authenticating
// the completing request's own __Host-session cookie -- unlike
// purpose=reauth's arm, which always did. DELETE /api/v1/sessions
// ("log out everywhere," sessions_handlers.go's handleRevokeAllSessions)
// revokes every session row for the caller but never touches
// oauth_transactions, so a purpose=link transaction begun before that
// DELETE stayed fully completable for up to oauthTxTTL=10 minutes
// afterward -- permanently attaching a provider identity to the account
// AFTER its own recovery control had just been used, with no unlink
// endpoint in v1 to undo it. link.go's authenticateLinkOrReauthSession now
// re-authenticates the completing request for BOTH purposes, so a
// revoked/dead session cookie rejects the link exactly like it already
// rejected reauth.

// TestGoogleCallback_LinkPurpose_RejectedAfterLogoutEverywhere is the
// BLOCKING finding's own required regression: start a purpose=link
// transaction under a live session, revoke every session for that user via
// DELETE /api/v1/sessions, then complete the /callback still presenting
// the now-dead session cookie (a browser tab left open on the settings
// page would do exactly this) -- the identity must NOT be created, and no
// session may be issued.
func TestGoogleCallback_LinkPurpose_RejectedAfterLogoutEverywhere(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))
	ctx := context.Background()

	linkingUserID := createTestUser(t, q)
	raw, sess := issueTestSession(t, q, linkingUserID)

	txCookie, tx := beginGoogleTransaction(t, q, auth.PurposeLink, linkingUserID)

	// "Log out everywhere," BEFORE the link transaction is ever completed --
	// this is the exact recovery control the finding says must not be
	// bypassable by a pending link.
	revokeResp := doJSON(t, handler, http.MethodDelete, auth.SessionsPath, testPublicOrigin, csrfTokenFor(sess), "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if revokeResp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE %s status = %d, want %d (setup: logout-everywhere must itself succeed)", auth.SessionsPath, revokeResp.StatusCode, http.StatusNoContent)
	}

	subject := uniqueSubject(t)
	code := "code-link-after-logout-everywhere-" + uuid.NewString()
	p.RegisterCode(code, oidctest.Claims{
		Subject:       subject,
		Email:         uniqueEmail(t),
		EmailVerified: ptrTrue(),
		Nonce:         tx.Nonce,
	})

	// The completing request still presents the NOW-REVOKED session cookie
	// -- the realistic shape of the bug: nothing about this specific
	// request looks any different from a legitimate completion; only the
	// session row it depends on has since died.
	resp := doGet(t, handler, auth.GoogleCallbackPath+"?code="+code+"&state="+tx.State, txCookie, sessionRequestCookie(raw)) //nolint:bodyclose // doGet closes the body itself before returning.

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	loc := resp.Header.Get("Location")
	if got := mustQueryParam(t, loc, "error"); got != "auth_failed" {
		t.Errorf("error param = %q, want %q (DD-C3 generic no-oracle rejection -- the completing request's session no longer authenticates)", got, "auth_failed")
	}
	assertRedirectPath(t, loc, wantSettingsSessionsPath) // DD-C15.

	if sc := extractCookie(resp, auth.SessionCookieName); sc != nil {
		t.Errorf("response set a %s cookie (value=%q) on a rejected post-logout-everywhere link attempt, want none", auth.SessionCookieName, sc.Value)
	}

	// The hard invariant: no identity row was ever created for this
	// (provider, subject), regardless of the exact wire shape above.
	assertNoIdentityForProvider(t, q, auth.ProviderGoogle, subject)

	// And no LIVE session exists for linkingUserID at all -- the point of
	// "log out everywhere" is that it actually logs the account out
	// everywhere, including in the face of an in-flight link.
	sm := auth.NewSessionManager(q)
	if _, _, err := sm.Authenticate(ctx, raw); err == nil {
		t.Error("the revoked session token still authenticates after the rejected link attempt, want it to stay dead")
	}
}

// ============================================================================
// Gate hardening: a rotated successor cookie must survive a link/reauth
// authorization mismatch (link.go's authenticateLinkOrReauthSession)
// ============================================================================

// TestGoogleCallback_LinkOrReauth_RotatedRequest_MismatchStillCarriesSuccessorCookie
// is the phase review's companion finding to the logout-everywhere gap
// above: authenticateLinkOrReauthSession must call SetSessionCookie for a
// rotated successor token BEFORE comparing sess.UserID against
// tx.LinkingUserID, not after -- SessionManager.Authenticate may already
// have minted and durably persisted a successor row (task-7-brief.md's
// >24h rotation) by the time it returns, and discarding that write on a
// mismatch (the old ordering) would silently strand the browser on a dead
// predecessor token, killing its session on its very next request even
// though the CURRENT request was correctly rejected. Drives a genuine
// rotation deterministically via a fake clock (SetSessionManagerForTest,
// the same technique TestRequireSession_RotatedRequest_CarriesSuccessorCookie,
// sessions_adversarial_test.go, establishes), then completes a
// purpose=link /callback while signed in as a DIFFERENT user than
// tx.LinkingUserID -- a shared-browser scenario where the mismatch is a
// real, correct rejection, but the rotation write underneath it must still
// reach the browser.
func TestGoogleCallback_LinkOrReauth_RotatedRequest_MismatchStillCarriesSuccessorCookie(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	q := newTestQueries(t)
	svc, err := auth.NewServiceForTest(testLogger(), config.Config{
		PublicOrigin:       testPublicOrigin,
		GoogleClientID:     oidctest.DefaultClientID,
		GoogleClientSecret: "test-google-client-secret",
	}, q, p.URL, "", "")
	if err != nil {
		t.Fatalf("NewServiceForTest() error = %v", err)
	}
	clk := testutil.NewClockAtEpoch()
	sm := auth.NewSessionManagerForTest(q, clk.Now)
	auth.SetSessionManagerForTest(svc, sm)
	handler := api.New(testLogger(), noopPinger{}, api.Options{}, svc.RegisterRoutes)
	ctx := context.Background()

	linkingUserID := createTestUser(t, q)        // named by the transaction below.
	currentBrowserUserID := createTestUser(t, q) // who this browser is ACTUALLY signed in as.
	rawOld, _, err := sm.Issue(ctx, currentBrowserUserID, "ua", "203.0.113.44")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	clk.Advance(25 * time.Hour) // past rotationAge (24h, session.go): the NEXT Authenticate must rotate.

	txStore := auth.NewTransactionStore(q)
	handle, tx, err := txStore.Begin(ctx, auth.ProviderGoogle, auth.PurposeLink, linkingUserID, testPublicOrigin+auth.GoogleCallbackPath)
	if err != nil {
		t.Fatalf("TransactionStore.Begin() error = %v", err)
	}
	txCookie := requestCookie(auth.OAuthTxCookieName, handle)

	subject := uniqueSubject(t)
	code := "code-link-rotate-mismatch-" + uuid.NewString()
	p.RegisterCode(code, oidctest.Claims{
		Subject:       subject,
		Email:         uniqueEmail(t),
		EmailVerified: ptrTrue(),
		Nonce:         tx.Nonce,
	})

	resp := doGet(t, handler, auth.GoogleCallbackPath+"?code="+code+"&state="+tx.State, txCookie, sessionRequestCookie(rawOld)) //nolint:bodyclose // doGet closes the body itself before returning.

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	loc := resp.Header.Get("Location")
	if got := mustQueryParam(t, loc, "error"); got != "auth_failed" {
		t.Errorf("error param = %q, want %q (the completing session authenticates a DIFFERENT user than tx.LinkingUserID)", got, "auth_failed")
	}

	successor := extractCookie(resp, auth.SessionCookieName)
	if successor == nil {
		t.Fatal("a rejected link callback with a >24h-old session did not carry a successor Set-Cookie -- the rotation write already happened in the database and must not be silently discarded on a mismatch")
	}
	if successor.Value == "" || successor.Value == rawOld {
		t.Errorf("successor cookie value = %q, want a new, non-empty token distinct from the original", successor.Value)
	}
	if !successor.Secure || !successor.HttpOnly {
		t.Errorf("successor cookie Secure=%v HttpOnly=%v, want both true", successor.Secure, successor.HttpOnly)
	}

	// The successor still authenticates, and still as the SAME user the
	// predecessor did -- rotation carries the row forward, it never
	// reassigns ownership, regardless of what this one request was
	// rejected for.
	again, _, err := sm.Authenticate(ctx, successor.Value)
	if err != nil {
		t.Fatalf("Authenticate(successor) error = %v, want the browser's rotated credential to keep working", err)
	}
	if again.UserID != currentBrowserUserID {
		t.Errorf("successor session UserID = %v, want %v", again.UserID, currentBrowserUserID)
	}

	assertNoIdentityForProvider(t, q, auth.ProviderGoogle, subject)
}
