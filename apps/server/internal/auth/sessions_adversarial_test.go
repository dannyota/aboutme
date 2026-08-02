// Package auth_test: independent, spec-derived adversarial tests for
// Task 9's `/me`, logout, and session-management HTTP surface (task-9-
// brief.md, AC-AUTH-005). The scenario structure and assertions below were
// derived WITHOUT reading me.go, sessions_handlers.go, RequireSession's
// implementation, task-9-report.md, or Task 9's git history -- only
// task-9-brief.md, docs/specs/aboutme-design.md §3/§4, the binding
// DD-C11/DD-C5 rulings, and the packages committed at HEAD facfab5 (see
// notes.md for the full derivation record and per-test rationale).
//
// This file is the POST-INTEGRATION reconciliation pass, authored after
// Task 9 landed (HEAD e79195b): every one of the original 7 ADAPT markers
// has been resolved against the real, now-committed seams, and every
// helper this file used has been swapped for the implementer's own
// identical one wherever me_test.go/sessions_handlers_test.go (the
// implementer's own 25-test harness, same package) already provides it,
// per notes.md's integration report. No assertion or scenario was removed
// or weakened in reconciliation -- only the plumbing connecting them to
// the real API changed.
//
// Harness reuse (already committed by Task 9 or earlier tasks, NOT
// redefined here): newTestQueries, createTestUser (transaction_test.go);
// newRowInspectorPool, randomHandle (transaction_adversarial_test.go);
// sessionTokenHash, setSessionRowLastSeenAt (session_test.go /
// session_adversarial_test.go); idleTimeout (session_adversarial_test.go);
// testPublicOrigin, testLogger, noopPinger (handlers_test.go);
// csrfTokenFor (csrf_test.go); newSessionAPITestService, issueTestSession,
// sessionRequestCookie, doJSON, meEnvelopeBody, decodeErrorCode
// (me_test.go); sessionsListEnvelope, assertNoBody, rowRevokedAt,
// forceReauthenticatedAtStale, forceRotationGraceDead, sessionIDPath
// (sessions_handlers_test.go).
package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// ---- new helpers this file needs, with no identical existing equivalent --

// forceRotationGraceInFuture directly SQL-updates sessionID's
// rotation_grace_until to delta from now, revoked_at left untouched --
// forceRotationGraceDead's (sessions_handlers_test.go) missing "still
// within grace" complement, needed to pin DD-C5's "live" semantics in
// both directions against the SAME row.
func forceRotationGraceInFuture(t *testing.T, sessionID uuid.UUID, delta time.Duration) {
	t.Helper()
	pool := newRowInspectorPool(t)
	future := time.Now().Add(delta)
	if _, err := pool.Exec(context.Background(), `UPDATE sessions SET rotation_grace_until = $2 WHERE id = $1`, sessionID, future); err != nil {
		t.Fatalf("force rotation_grace_until into the future: %v", err)
	}
}

// listedSessionIDs decodes resp (GET /sessions' success envelope) and
// returns the set of returned session ids, for tests that only need
// membership/count rather than the full per-entry shape sessionsListEnvelope
// (sessions_handlers_test.go) already decodes elsewhere.
func listedSessionIDs(t *testing.T, resp *http.Response) map[string]bool {
	t.Helper()
	var body sessionsListEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /sessions response body: %v", err)
	}
	ids := make(map[string]bool, len(body.Data))
	for _, e := range body.Data {
		ids[e.ID] = true
	}
	return ids
}

// ============================================================================
// Step 3's required table (brief verbatim, TestMe_CSRFTokenNotInAnyHeaderOrLog
// renamed to TestMe_CSRFTokenNotInAnyHeaderOrCookie per the dispatching
// integration owner's explicit instruction).
// ============================================================================

// TestMe_CSRFTokenNotInAnyHeaderOrCookie is the direct regression test for
// design spec §3's CSRF row: "GET /me returns the token in its JSON body
// (never cookie/URL/log)". It proves both halves: the token IS present
// (and correct) in the body, and it is ABSENT from every response header
// value and every Set-Cookie -- not just that some header happens to be
// missing, but that the exact token string never appears anywhere outside
// the body. (me_test.go's own TestGetMe_CSRFTokenOnlyInResponseBody covers
// the same property independently -- authored blind to it.)
func TestMe_CSRFTokenNotInAnyHeaderOrCookie(t *testing.T) {
	t.Parallel()
	handler, q := newSessionAPITestService(t)
	userID := createTestUser(t, q)
	raw, sess := issueTestSession(t, q, userID)

	resp := doJSON(t, handler, http.MethodGet, auth.MePath, "", "", "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want %d", auth.MePath, resp.StatusCode, http.StatusOK)
	}

	var body meEnvelopeBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /me response body: %v", err)
	}

	wantToken := csrfTokenFor(sess)
	if body.Data.CSRFToken == "" {
		t.Fatal("data.csrfToken is empty, want the session's CSRF token")
	}
	if body.Data.CSRFToken != wantToken {
		t.Errorf("data.csrfToken = %q, want %q (base64.RawURLEncoding of the session's csrf_secret)", body.Data.CSRFToken, wantToken)
	}

	for name, values := range resp.Header {
		for _, v := range values {
			if strings.Contains(v, body.Data.CSRFToken) {
				t.Errorf("response header %s = %q contains the CSRF token -- must never leak outside the body", name, v)
			}
		}
	}
	for _, c := range resp.Cookies() {
		if strings.Contains(c.Value, body.Data.CSRFToken) {
			t.Errorf("Set-Cookie %s=%q contains the CSRF token -- must never leak into a cookie", c.Name, c.Value)
		}
	}
}

// TestLogout_ClearsSiteDataHeader pins design spec §3's Logout row exactly:
// "Delete session row + expire cookie + Clear-Site-Data header", with the
// literal directive list `"cookies", "storage"` DD-C11 pins, plus DD-C13's
// 204-no-body success shape.
func TestLogout_ClearsSiteDataHeader(t *testing.T) {
	t.Parallel()
	handler, q := newSessionAPITestService(t)
	userID := createTestUser(t, q)
	rawSelf, sess := issueTestSession(t, q, userID)

	resp := doJSON(t, handler, http.MethodPost, auth.LogoutPath, testPublicOrigin, csrfTokenFor(sess), "", sessionRequestCookie(rawSelf)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST %s status = %d, want %d (DD-C13)", auth.LogoutPath, resp.StatusCode, http.StatusNoContent)
	}
	assertNoBody(t, resp)

	const want = `"cookies", "storage"`
	if got := resp.Header.Get("Clear-Site-Data"); got != want {
		t.Errorf("Clear-Site-Data = %q, want exactly %q", got, want)
	}

	cleared := extractCookie(resp, auth.SessionCookieName)
	if cleared == nil || cleared.MaxAge >= 0 {
		t.Error("logout did not clear the __Host-session cookie (want a Set-Cookie with negative Max-Age)")
	}

	// Server-side revocation, not merely a client-side cookie clear: the
	// session's own raw token must stop authenticating.
	after := doJSON(t, handler, http.MethodGet, auth.MePath, "", "", "", sessionRequestCookie(rawSelf)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if after.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET %s with the logged-out session's raw token after logout status = %d, want %d", auth.MePath, after.StatusCode, http.StatusUnauthorized)
	}
}

// TestRevokeAll_WithoutRecentReauth_TouchesNothing is the row-count/spy
// half the brief calls out explicitly: a bug here could revoke everything
// and then discover it should have refused. Three of the caller's own
// sessions (including the one making this very request) must all remain
// completely untouched by a REJECTED revoke-all. (sessions_handlers_
// test.go's own TestDeleteAllSessions_StaleReauth_RejectsBeforeRevoking
// covers the same property independently, with two sessions instead of
// three -- authored blind to it.)
func TestRevokeAll_WithoutRecentReauth_TouchesNothing(t *testing.T) {
	t.Parallel()
	handler, q := newSessionAPITestService(t)
	userID := createTestUser(t, q)

	rawCaller, callerSess := issueTestSession(t, q, userID)
	rawOther1, otherSess1 := issueTestSession(t, q, userID)
	rawOther2, otherSess2 := issueTestSession(t, q, userID)

	forceReauthenticatedAtStale(t, callerSess.ID)

	resp := doJSON(t, handler, http.MethodDelete, auth.SessionsPath, testPublicOrigin, csrfTokenFor(callerSess), "", sessionRequestCookie(rawCaller)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("DELETE %s (stale reauth) status = %d, want %d", auth.SessionsPath, resp.StatusCode, http.StatusForbidden)
	}
	if got := decodeErrorCode(t, resp); got != "reauth_required" {
		t.Errorf("error.code = %q, want %q", got, "reauth_required")
	}

	for _, s := range []store.Session{callerSess, otherSess1, otherSess2} {
		if got := rowRevokedAt(t, s.ID); got != nil {
			t.Errorf("session %s revoked_at = %v after a REJECTED revoke-all, want nil (untouched)", s.ID, got)
		}
	}

	for _, raw := range []string{rawCaller, rawOther1, rawOther2} {
		r := doJSON(t, handler, http.MethodGet, auth.MePath, "", "", "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
		if r.StatusCode != http.StatusOK {
			t.Errorf("GET %s with an untouched session after a rejected revoke-all status = %d, want %d", auth.MePath, r.StatusCode, http.StatusOK)
		}
	}
}

// TestSessionsList_NeverLeaksOtherUsersSessions checks both count and ids,
// per the brief's explicit "never ... even by row-count coincidence"
// framing: userA has 2 sessions, userB has 3 (deliberately different
// counts), so an off-by-one or wrong-WHERE-clause bug that happened to
// return the right COUNT for the wrong reason would still be caught by
// the id check, and vice versa. (sessions_handlers_test.go's own
// TestListSessions_OnlyReturnsCallersOwnRows covers the same property
// independently, with equal 1-vs-1 counts -- authored blind to it; the
// unequal counts here strengthen beyond that.)
func TestSessionsList_NeverLeaksOtherUsersSessions(t *testing.T) {
	t.Parallel()
	handler, q := newSessionAPITestService(t)
	userA := createTestUser(t, q)
	userB := createTestUser(t, q)

	rawA1, sessA1 := issueTestSession(t, q, userA)
	_, sessA2 := issueTestSession(t, q, userA)
	_, sessB1 := issueTestSession(t, q, userB)
	_, sessB2 := issueTestSession(t, q, userB)
	_, sessB3 := issueTestSession(t, q, userB)

	resp := doJSON(t, handler, http.MethodGet, auth.SessionsPath, "", "", "", sessionRequestCookie(rawA1)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want %d", auth.SessionsPath, resp.StatusCode, http.StatusOK)
	}
	gotIDs := listedSessionIDs(t, resp)

	if len(gotIDs) != 2 {
		t.Errorf("GET %s for userA returned %d distinct entries, want exactly 2 (userA's own live sessions, none of userB's 3)", auth.SessionsPath, len(gotIDs))
	}
	for _, want := range []uuid.UUID{sessA1.ID, sessA2.ID} {
		if !gotIDs[want.String()] {
			t.Errorf("GET /sessions for userA missing its own session %s", want)
		}
	}
	for _, leaked := range []uuid.UUID{sessB1.ID, sessB2.ID, sessB3.ID} {
		if gotIDs[leaked.String()] {
			t.Errorf("GET /sessions for userA leaked userB's session %s", leaked)
		}
	}
}

// ============================================================================
// Strengthening (assignment item 5): DD-C5 indistinguishability, per-
// session-revoke reauth gate, grace-dead list visibility, current-session
// cookie clearing, rotation pass-through, invalid-cookie 401, and CSRF
// enforcement across all three mutating endpoints.
// ============================================================================

// TestDeleteSession_DDC5_IndistinguishableAcrossFailureModes pins DD-C5
// verbatim: another user's session id, a random id that never existed, and
// the caller's own already-revoked session id must be byte-identical --
// status AND body -- not merely "all 404". This is the one DD-C5 case
// sessions_handlers_test.go's own three separate 404 tests (another-user,
// unknown, malformed) do NOT cover: the caller's own, already-revoked
// session, and none of those three cross-check byte-identical bodies
// against each other the way this test does.
func TestDeleteSession_DDC5_IndistinguishableAcrossFailureModes(t *testing.T) {
	t.Parallel()
	handler, q := newSessionAPITestService(t)
	userA := createTestUser(t, q)
	userB := createTestUser(t, q)

	rawA, callerSess := issueTestSession(t, q, userA)
	_, otherUsersSess := issueTestSession(t, q, userB)
	_, ownAlreadyRevokedSess := issueTestSession(t, q, userA)

	sm := auth.NewSessionManager(q)
	if err := sm.Revoke(context.Background(), ownAlreadyRevokedSess.ID); err != nil {
		t.Fatalf("pre-revoke own session: %v", err)
	}

	cases := []struct {
		name   string
		target uuid.UUID
	}{
		{"another user's session id", otherUsersSess.ID},
		{"a random id that never existed", uuid.New()},
		{"the caller's own, already-revoked session", ownAlreadyRevokedSess.ID},
	}

	var refStatus int
	var refBody []byte
	for i, c := range cases {
		resp := doJSON(t, handler, http.MethodDelete, sessionIDPath(c.target), testPublicOrigin, csrfTokenFor(callerSess), "", sessionRequestCookie(rawA)) //nolint:bodyclose // doJSON closes the body itself before returning.
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("%s: read response body: %v", c.name, err)
		}
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status = %d, want %d", c.name, resp.StatusCode, http.StatusNotFound)
		}
		if i == 0 {
			refStatus, refBody = resp.StatusCode, body
			continue
		}
		if resp.StatusCode != refStatus {
			t.Errorf("%s: status = %d, want %d (must match %q -- DD-C5 no-oracle)", c.name, resp.StatusCode, refStatus, cases[0].name)
		}
		if !bytes.Equal(body, refBody) {
			t.Errorf("%s: body = %s, want byte-identical to %q's body %s (DD-C5 -- no oracle across other-user/unknown/already-revoked)", c.name, body, cases[0].name, refBody)
		}
	}
}

// TestDeleteSession_WithoutRecentReauth_TouchesNothing is the per-session
// revoke's own version of the required revoke-all row-count/spy test:
// DD-C11's binding correction added recent reauth to DELETE
// /sessions/{id} too (spec §4 wins over the plan's earlier session+CSRF-
// only row) -- this proves the ordering: reauth is checked, and rejected,
// BEFORE the target row is ever touched. (sessions_handlers_test.go's own
// TestDeleteSession_WithoutRecentReauth_Returns403AndLeavesSessionLive
// covers the same property independently -- authored blind to it.)
func TestDeleteSession_WithoutRecentReauth_TouchesNothing(t *testing.T) {
	t.Parallel()
	handler, q := newSessionAPITestService(t)
	userID := createTestUser(t, q)

	rawCaller, callerSess := issueTestSession(t, q, userID)
	rawTarget, targetSess := issueTestSession(t, q, userID)
	forceReauthenticatedAtStale(t, callerSess.ID)

	resp := doJSON(t, handler, http.MethodDelete, sessionIDPath(targetSess.ID), testPublicOrigin, csrfTokenFor(callerSess), "", sessionRequestCookie(rawCaller)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("DELETE %s (stale reauth) status = %d, want %d", sessionIDPath(targetSess.ID), resp.StatusCode, http.StatusForbidden)
	}
	if got := decodeErrorCode(t, resp); got != "reauth_required" {
		t.Errorf("error.code = %q, want %q", got, "reauth_required")
	}

	if got := rowRevokedAt(t, targetSess.ID); got != nil {
		t.Errorf("target session revoked_at = %v after a REJECTED per-session revoke, want nil (untouched)", got)
	}
	r := doJSON(t, handler, http.MethodGet, auth.MePath, "", "", "", sessionRequestCookie(rawTarget)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if r.StatusCode != http.StatusOK {
		t.Errorf("GET %s with the untouched target session after a rejected revoke status = %d, want %d", auth.MePath, r.StatusCode, http.StatusOK)
	}
}

// TestSessionsList_GraceDeadPredecessor_VisibleWithinGrace_AbsentAfterGrace
// pins DD-C5's "live" semantics both directions in one test, against the
// SAME row: while a predecessor's rotation_grace_until is still in the
// future (revoked_at NULL, grace open), it IS live and must appear in the
// device list -- design decision 1's orthogonality means rotation_grace_until
// being non-NULL is not itself a death sentence. Once rotation_grace_until
// has passed (still revoked_at NULL), the SAME row must disappear -- the
// after-grace absence sessions_handlers_test.go's own
// TestListSessions_ExcludesGraceDeadRotationPredecessors also covers
// (authored blind to it), but only in the after-grace direction; the
// within-grace-visible half here is this suite's own addition.
func TestSessionsList_GraceDeadPredecessor_VisibleWithinGrace_AbsentAfterGrace(t *testing.T) {
	t.Parallel()
	handler, q := newSessionAPITestService(t)
	userID := createTestUser(t, q)

	_, predSess := issueTestSession(t, q, userID)
	rawLive, liveSess := issueTestSession(t, q, userID)

	forceRotationGraceInFuture(t, predSess.ID, 30*time.Second)

	resp := doJSON(t, handler, http.MethodGet, auth.SessionsPath, "", "", "", sessionRequestCookie(rawLive)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s (within grace) status = %d, want %d", auth.SessionsPath, resp.StatusCode, http.StatusOK)
	}
	withinGraceIDs := listedSessionIDs(t, resp)
	if !withinGraceIDs[predSess.ID.String()] {
		t.Error("predecessor absent from the list while still within its rotation grace window, want present (still LIVE)")
	}
	if !withinGraceIDs[liveSess.ID.String()] {
		t.Error("caller's own live session missing from its own list")
	}

	forceRotationGraceDead(t, predSess.ID)

	resp2 := doJSON(t, handler, http.MethodGet, auth.SessionsPath, "", "", "", sessionRequestCookie(rawLive)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("GET %s (after grace) status = %d, want %d", auth.SessionsPath, resp2.StatusCode, http.StatusOK)
	}
	afterGraceIDs := listedSessionIDs(t, resp2)
	if afterGraceIDs[predSess.ID.String()] {
		t.Error("grace-dead predecessor (revoked_at NULL, rotation_grace_until past) present in the list, want absent (DD-C5)")
	}
	if !afterGraceIDs[liveSess.ID.String()] {
		t.Error("caller's own live session missing from its own list after the predecessor died")
	}
}

// TestDeleteSession_RevokingCurrentSession_ClearsCookie pins the brief's
// "revoking the current session also clears its cookie in the response"
// requirement directly, plus that the session actually stops authenticating
// afterward at the HTTP boundary (sessions_handlers_test.go's own
// TestDeleteSession_OwnCurrentSessionID_RevokesAndClearsCookie covers the
// cookie-clear and row-level revocation independently -- authored blind to
// it -- but stops short of the follow-up 401 check this test adds).
func TestDeleteSession_RevokingCurrentSession_ClearsCookie(t *testing.T) {
	t.Parallel()
	handler, q := newSessionAPITestService(t)
	userID := createTestUser(t, q)
	rawSelf, sess := issueTestSession(t, q, userID)

	resp := doJSON(t, handler, http.MethodDelete, sessionIDPath(sess.ID), testPublicOrigin, csrfTokenFor(sess), "", sessionRequestCookie(rawSelf)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE own current session status = %d, want %d (DD-C13)", resp.StatusCode, http.StatusNoContent)
	}
	assertNoBody(t, resp)
	cleared := extractCookie(resp, auth.SessionCookieName)
	if cleared == nil || cleared.MaxAge >= 0 {
		t.Error("revoking the CURRENT session did not clear its cookie in the same response")
	}

	after := doJSON(t, handler, http.MethodGet, auth.MePath, "", "", "", sessionRequestCookie(rawSelf)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if after.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET %s with the just-revoked current session status = %d, want %d", auth.MePath, after.StatusCode, http.StatusUnauthorized)
	}
}

// TestRequireSession_RotatedRequest_CarriesSuccessorCookie is task-9-
// brief.md's "RequireSession rotation pass-through": a request presenting
// a session past rotationAge (24h, session.go) must both succeed AND carry
// a Set-Cookie for the newly-minted successor, in the SAME response --
// and that response's own body must reflect the successor's row (its
// fresh csrf_secret per session.go's tryRotate), not the stale
// predecessor's. Uses SetSessionManagerForTest (export_test.go's Task 9
// seam) to drive rotation deterministically through the real HTTP chain,
// the same fake-clock technique me_test.go's own
// TestGetMe_RotatesSessionOnAuthenticate_SetsNewCookie establishes --
// this test adds the csrf_secret-matches-the-successor cross-check that
// one does not make.
func TestRequireSession_RotatedRequest_CarriesSuccessorCookie(t *testing.T) {
	t.Parallel()
	q := newTestQueries(t)
	svc, err := auth.NewService(testLogger(), config.Config{PublicOrigin: testPublicOrigin}, q)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	clk := testutil.NewClockAtEpoch()
	sm := auth.NewSessionManagerForTest(q, clk.Now)
	auth.SetSessionManagerForTest(svc, sm)
	handler := api.New(testLogger(), noopPinger{}, api.Options{}, svc.RegisterRoutes)

	userID := createTestUser(t, q)
	rawOld, _, err := sm.Issue(context.Background(), userID, "ua", "203.0.113.91")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	clk.Advance(25 * time.Hour) // past rotationAge (24h)

	resp := doJSON(t, handler, http.MethodGet, auth.MePath, "", "", "", sessionRequestCookie(rawOld)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s with a >24h-old session status = %d, want %d", auth.MePath, resp.StatusCode, http.StatusOK)
	}
	successor := extractCookie(resp, auth.SessionCookieName)
	if successor == nil {
		t.Fatal("a request past rotationAge did not carry a successor Set-Cookie -- RequireSession must rotate transparently")
	}
	if successor.Value == "" || successor.Value == rawOld {
		t.Errorf("successor cookie value = %q, want a new, non-empty token distinct from the original", successor.Value)
	}

	var body meEnvelopeBody
	if decodeErr := json.NewDecoder(resp.Body).Decode(&body); decodeErr != nil {
		t.Fatalf("decode response body: %v", decodeErr)
	}
	row, err := q.GetSessionByTokenHash(context.Background(), sessionTokenHash(successor.Value))
	if err != nil {
		t.Fatalf("look up successor row by its new token: %v", err)
	}
	wantToken := csrfTokenFor(row)
	if body.Data.CSRFToken != wantToken {
		t.Errorf("csrfToken in the rotated response = %q, want %q (the SUCCESSOR's own csrf_secret, not the predecessor's)", body.Data.CSRFToken, wantToken)
	}

	again := doJSON(t, handler, http.MethodGet, auth.MePath, "", "", "", sessionRequestCookie(successor.Value)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if again.StatusCode != http.StatusOK {
		t.Errorf("GET %s with the successor token status = %d, want %d", auth.MePath, again.StatusCode, http.StatusOK)
	}
}

// TestRequireSession_InvalidOrExpiredCookie_Returns401AndClearsCookie
// covers two of ErrSessionInvalid's failure modes at the HTTP boundary: a
// token shaped like a real one but never issued (me_test.go's own
// TestGetMe_InvalidSessionToken_Returns401AndClearsCookie covers this case
// independently, authored blind to it), and a real session that has gone
// idle-expired (novel coverage -- no existing test in this package drives
// a genuinely idle-expired session through the real HTTP chain). Both must
// 401, carry sessionRequiredCode, AND clear the __Host-session cookie.
func TestRequireSession_InvalidOrExpiredCookie_Returns401AndClearsCookie(t *testing.T) {
	t.Parallel()
	handler, q := newSessionAPITestService(t)

	t.Run("never issued token", func(t *testing.T) {
		t.Parallel()
		cookie := &http.Cookie{Name: auth.SessionCookieName, Value: randomHandle(t)}
		resp := doJSON(t, handler, http.MethodGet, auth.MePath, "", "", "", cookie) //nolint:bodyclose // doJSON closes the body itself before returning.
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
		}
		if got := decodeErrorCode(t, resp); got != "session_required" {
			t.Errorf("error.code = %q, want %q", got, "session_required")
		}
		cleared := extractCookie(resp, auth.SessionCookieName)
		if cleared == nil || cleared.MaxAge >= 0 {
			t.Error("response did not clear __Host-session on an invalid session cookie")
		}
	})

	t.Run("idle-expired session", func(t *testing.T) {
		t.Parallel()
		userID := createTestUser(t, q)
		raw, sess := issueTestSession(t, q, userID)

		idleExpired := time.Now().Add(-(idleTimeout + 24*time.Hour))
		setSessionRowLastSeenAt(context.Background(), t, newRowInspectorPool(t), sess.ID, idleExpired)

		resp := doJSON(t, handler, http.MethodGet, auth.MePath, "", "", "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
		}
		if got := decodeErrorCode(t, resp); got != "session_required" {
			t.Errorf("error.code = %q, want %q", got, "session_required")
		}
		cleared := extractCookie(resp, auth.SessionCookieName)
		if cleared == nil || cleared.MaxAge >= 0 {
			t.Error("response did not clear __Host-session on an idle-expired session")
		}
	})
}

// TestCSRF_CrossOrigin_RejectsAllThreeMutatingEndpoints is the brief's "one
// cross-origin case each" strengthening item, table-driven across all
// three DD-C11 mutating endpoints, plus a bonus missing-token subtest per
// endpoint: both must reject with 403 csrf_rejected (csrf.go's already-
// committed single no-oracle code) and must not touch any session row.
// Novel coverage: sessions_handlers_test.go's own suite tests a missing
// CSRF token on logout and revoke-all, but never a cross-origin Origin on
// ANY of the three endpoints, and never either failure mode at all for
// DELETE /sessions/{id}.
func TestCSRF_CrossOrigin_RejectsAllThreeMutatingEndpoints(t *testing.T) {
	t.Parallel()
	handler, q := newSessionAPITestService(t)

	type endpoint struct {
		name   string
		method string
		path   func(sess store.Session) string
	}
	endpoints := []endpoint{
		{"POST /auth/logout", http.MethodPost, func(store.Session) string { return auth.LogoutPath }},
		{"DELETE /sessions/{id}", http.MethodDelete, func(sess store.Session) string { return sessionIDPath(sess.ID) }},
		{"DELETE /sessions", http.MethodDelete, func(store.Session) string { return auth.SessionsPath }},
	}

	for _, e := range endpoints {
		t.Run(e.name+"/cross-origin", func(t *testing.T) {
			t.Parallel()
			userID := createTestUser(t, q)
			// A freshly issued session already has reauthenticated_at = now
			// (session.go's Issue), so both DELETE endpoints' recent-reauth
			// gate is satisfied without a patch -- isolating this subtest to
			// the CSRF check alone.
			raw, sess := issueTestSession(t, q, userID)

			resp := doJSON(t, handler, e.method, e.path(sess), "https://evil.example", csrfTokenFor(sess), "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("%s (wrong Origin) status = %d, want %d", e.name, resp.StatusCode, http.StatusForbidden)
			}
			if got := decodeErrorCode(t, resp); got != "csrf_rejected" {
				t.Errorf("%s (wrong Origin) error.code = %q, want %q", e.name, got, "csrf_rejected")
			}
			if got := rowRevokedAt(t, sess.ID); got != nil {
				t.Errorf("%s: session revoked_at = %v after a CSRF-rejected request, want nil (untouched)", e.name, got)
			}
		})

		t.Run(e.name+"/missing-token", func(t *testing.T) {
			t.Parallel()
			userID := createTestUser(t, q)
			raw, sess := issueTestSession(t, q, userID)

			resp := doJSON(t, handler, e.method, e.path(sess), testPublicOrigin, "", "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("%s (missing token) status = %d, want %d", e.name, resp.StatusCode, http.StatusForbidden)
			}
			if got := rowRevokedAt(t, sess.ID); got != nil {
				t.Errorf("%s (missing token): session revoked_at = %v after a CSRF-rejected request, want nil (untouched)", e.name, got)
			}
		})
	}
}
