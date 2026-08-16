// These adversarial HTTP tests cover session ownership, liveness, revocation,
// rotation delivery, recent reauthentication, and CSRF. See
// docs/adr/0015-session-rotation-delivery.md.
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

// ---- row and response helpers ---------------------------------------------

// forceRotationGraceInFuture keeps one unrevoked predecessor inside grace.
func forceRotationGraceInFuture(t *testing.T, sessionID uuid.UUID, delta time.Duration) {
	t.Helper()
	pool := newRowInspectorPool(t)
	future := time.Now().Add(delta)
	if _, err := pool.Exec(context.Background(), `UPDATE sessions SET rotation_grace_until = $2 WHERE id = $1`, sessionID, future); err != nil {
		t.Fatalf("force rotation_grace_until into the future: %v", err)
	}
}

// listedSessionIDs returns device-list membership without copying entry fields.
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
// Session API adversarial cases.
// ============================================================================

// TestMe_CSRFTokenNotInAnyHeaderOrCookie proves GET /me returns the exact CSRF
// token in its JSON body and nowhere in response headers or cookies.
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

// TestLogout_ClearsSiteDataHeader proves logout revokes the server-side session,
// expires its cookie, emits the exact Clear-Site-Data directives, and returns
// an empty 204 response.
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

// TestRevokeAll_WithoutRecentReauth_TouchesNothing proves the recent-reauth
// check runs before any revocation. All three sessions remain live after a
// rejected request.
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

// TestSessionsList_NeverLeaksOtherUsersSessions checks both count and IDs.
// Unequal per-user counts catch a wrong ownership filter that might otherwise
// return the expected count by coincidence.
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

// ==== ownership, liveness, rotation delivery, and CSRF ======================

// TestDeleteSession_DDC5_IndistinguishableAcrossFailureModes requires another
// user's ID, an unknown ID, and the caller's already-revoked ID to return the
// same status and byte-identical body. This prevents an ownership oracle.
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

// TestDeleteSession_WithoutRecentReauth_TouchesNothing proves per-session
// revocation checks recent reauthentication before touching the target row.
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
// proves a predecessor stays listed while its grace interval is open and
// disappears once the same interval expires. A non-null grace deadline alone
// does not make the row dead.
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

// TestDeleteSession_RevokingCurrentSession_ClearsCookie proves revoking the
// current session clears its cookie and makes the raw token fail at the HTTP
// boundary.
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

// TestRequireSession_RotatedRequest_CarriesSuccessorCookie proves a request
// that triggers rotation succeeds and carries the successor cookie in the same
// response. The response body must also contain the successor's CSRF token,
// not the predecessor's.
func TestRequireSession_RotatedRequest_CarriesSuccessorCookie(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	q := store.New(pool)
	svc, err := auth.NewService(testLogger(), config.Config{PublicOrigin: testPublicOrigin}, pool)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	clk := testutil.NewClockAtEpoch()
	sm := auth.NewSessionManagerForTest(q, clk.Now)
	auth.SetSessionManagerForTest(svc, sm)
	handler := api.New(testLogger(), noopPinger{}, api.Options{}, nil, svc.RegisterRoutes)

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
// covers two ErrSessionInvalid modes at the HTTP boundary: a well-shaped token
// that was never issued and an idle-expired session. Both return the same 401
// code and clear the session cookie.
func TestRequireSession_InvalidOrExpiredCookie_Returns401AndClearsCookie(t *testing.T) {
	t.Parallel()
	handler, q := newSessionAPITestService(t)

	t.Run("never issued token", func(t *testing.T) {
		t.Parallel()
		cookie := requestCookie(auth.SessionCookieName, randomHandle(t))
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

// TestCSRF_CrossOrigin_RejectsAllThreeMutatingEndpoints covers cross-origin and
// missing-token requests for logout, per-session revoke, and revoke-all. Every
// case returns csrf_rejected and leaves its session row untouched.
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
			// Fresh reauthentication isolates this case to the CSRF gate.
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
