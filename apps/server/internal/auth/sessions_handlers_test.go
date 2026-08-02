// Package auth_test exercises task-9-brief.md Step 2's remaining
// session-management HTTP surface: POST /api/v1/auth/logout, GET+DELETE
// /api/v1/sessions (device list, logout-everywhere), and DELETE
// /api/v1/sessions/{id} (per-session revoke). These tests run against a
// live Postgres database (spec §9) and reuse every helper me_test.go (same
// package) defines -- newSessionAPITestService, issueTestSession,
// sessionRequestCookie, doJSON, decodeErrorCode -- rather than duplicating
// them.
//
// Response-shape ruling (coordinator addendum DD-C13, 2026-08-02, resolving
// an ambiguity the concurrently-derived adversarial suite surfaced): GET
// /sessions succeeds with 200 and a bodied {"data":[...]} envelope; the
// three mutating endpoints (logout, DELETE /sessions/{id}, DELETE
// /sessions) succeed with 204 No Content and NO body at all -- the
// {"data":...} envelope applies to bodied responses only.
package auth_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// sessionsListEnvelope decodes GET /sessions' success envelope.
type sessionsListEnvelope struct {
	Data []struct {
		ID         string    `json:"id"`
		CreatedAt  time.Time `json:"createdAt"`
		LastSeenAt time.Time `json:"lastSeenAt"`
		UA         *string   `json:"ua"`
		IP         *string   `json:"ip"`
		Current    bool      `json:"current"`
	} `json:"data"`
}

// assertNoBody fails the test unless resp's body is completely empty --
// DD-C13's 204-means-no-body requirement.
func assertNoBody(t *testing.T, resp *http.Response) {
	t.Helper()

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("response body = %q, want empty (204 No Content)", got)
	}
}

// rowRevokedAt queries sessions.revoked_at directly for id, so a test can
// prove a row was (or, crucially, was NOT) touched independently of
// whatever the handler's own response claims -- the same direct-table
// convention session_test.go/session_adversarial_test.go already use via
// newRowInspectorPool.
func rowRevokedAt(t *testing.T, id uuid.UUID) *time.Time {
	t.Helper()

	pool := newRowInspectorPool(t)
	var revokedAt *time.Time
	if err := pool.QueryRow(context.Background(), `SELECT revoked_at FROM sessions WHERE id = $1`, id).Scan(&revokedAt); err != nil {
		t.Fatalf("query revoked_at for %s: %v", id, err)
	}
	return revokedAt
}

// forceReauthenticatedAtStale directly SQL-updates sessionID's
// reauthenticated_at to a value older than session.go's reauthWindow
// (15m), so RequireRecentReauth rejects it -- without needing a fake
// clock wired into the Service under test (newSessionAPITestService uses
// a real *SessionManager, exactly like production).
func forceReauthenticatedAtStale(t *testing.T, sessionID uuid.UUID) {
	t.Helper()

	pool := newRowInspectorPool(t)
	stale := time.Now().Add(-20 * time.Minute)
	if _, err := pool.Exec(context.Background(), `UPDATE sessions SET reauthenticated_at = $2 WHERE id = $1`, sessionID, stale); err != nil {
		t.Fatalf("force reauthenticated_at stale: %v", err)
	}
}

// forceRotationGraceDead directly SQL-updates sessionID's
// rotation_grace_until to a past instant while leaving revoked_at NULL --
// simulating Task 7's grace-dead rotation predecessor (a row whose
// successor already exists and whose grace window has since elapsed)
// without driving the real >24h rotation algorithm end to end, which
// would require reconciling a fake clock against the real
// time.Now()-based *SessionManager newSessionAPITestService's Service
// uses internally.
func forceRotationGraceDead(t *testing.T, sessionID uuid.UUID) {
	t.Helper()

	pool := newRowInspectorPool(t)
	past := time.Now().Add(-time.Minute)
	if _, err := pool.Exec(context.Background(), `UPDATE sessions SET rotation_grace_until = $2 WHERE id = $1`, sessionID, past); err != nil {
		t.Fatalf("force rotation_grace_until into the past: %v", err)
	}
}

// forceSessionIdleExpired directly SQL-updates sessionID's last_seen_at
// to older than idleTimeout (30d, session_adversarial_test.go's mirrored
// constant), while leaving revoked_at NULL -- fix round 1, finding I1:
// ListLiveSessionsForUser/RevokeSessionForUser must both treat this
// exactly like a grace-dead predecessor, not as a still-live session.
func forceSessionIdleExpired(t *testing.T, sessionID uuid.UUID) {
	t.Helper()

	pool := newRowInspectorPool(t)
	past := time.Now().Add(-(idleTimeout + time.Hour))
	if _, err := pool.Exec(context.Background(), `UPDATE sessions SET last_seen_at = $2 WHERE id = $1`, sessionID, past); err != nil {
		t.Fatalf("force last_seen_at idle-expired: %v", err)
	}
}

// forceSessionAbsoluteExpired directly SQL-updates sessionID's
// absolute_expires_at to one hour in the past, while leaving revoked_at
// and last_seen_at untouched -- fix round 1, finding I1's absolute-expiry
// counterpart to forceSessionIdleExpired above.
func forceSessionAbsoluteExpired(t *testing.T, sessionID uuid.UUID) {
	t.Helper()

	pool := newRowInspectorPool(t)
	past := time.Now().Add(-time.Hour)
	if _, err := pool.Exec(context.Background(), `UPDATE sessions SET absolute_expires_at = $2 WHERE id = $1`, sessionID, past); err != nil {
		t.Fatalf("force absolute_expires_at expired: %v", err)
	}
}

// ---- POST /api/v1/auth/logout ---------------------------------------------

// TestLogout_RevokesCurrentSessionAndClearsCookie is the happy path:
// logout revokes the caller's own session (row-level proof, not just the
// response) and clears its cookie in the same response.
func TestLogout_RevokesCurrentSessionAndClearsCookie(t *testing.T) {
	handler, q := newSessionAPITestService(t)
	userID := createTestUser(t, q)
	raw, sess := issueTestSession(t, q, userID)

	resp := doJSON(t, handler, http.MethodPost, auth.LogoutPath, testPublicOrigin, csrfTokenFor(sess), "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
	assertNoBody(t, resp)

	cleared := extractCookie(resp, auth.SessionCookieName)
	if cleared == nil || cleared.MaxAge >= 0 {
		t.Error("__Host-session not cleared by logout")
	}

	if revokedAt := rowRevokedAt(t, sess.ID); revokedAt == nil {
		t.Error("session row's revoked_at is still NULL after logout, want non-NULL")
	}

	sm := auth.NewSessionManager(q)
	if _, _, err := sm.Authenticate(context.Background(), raw); err == nil {
		t.Error("Authenticate(logged-out token) error = nil, want ErrSessionInvalid")
	}
}

// TestLogout_ResponseCarriesClearSiteDataHeader is task-9-brief.md's own
// pinned wire value, checked exactly.
func TestLogout_ResponseCarriesClearSiteDataHeader(t *testing.T) {
	handler, q := newSessionAPITestService(t)
	userID := createTestUser(t, q)
	raw, sess := issueTestSession(t, q, userID)

	resp := doJSON(t, handler, http.MethodPost, auth.LogoutPath, testPublicOrigin, csrfTokenFor(sess), "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	if got := resp.Header.Get("Clear-Site-Data"); got != `"cookies", "storage"` {
		t.Errorf("Clear-Site-Data = %q, want %q", got, `"cookies", "storage"`)
	}
}

// TestLogout_WithoutCSRFToken_Returns403AndSessionStillValid proves
// RequireCSRF gates logout: no X-CSRF-Token means the session is never
// revoked at all.
func TestLogout_WithoutCSRFToken_Returns403AndSessionStillValid(t *testing.T) {
	handler, q := newSessionAPITestService(t)
	userID := createTestUser(t, q)
	raw, sess := issueTestSession(t, q, userID)

	resp := doJSON(t, handler, http.MethodPost, auth.LogoutPath, testPublicOrigin, "", "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
	if got := decodeErrorCode(t, resp); got != "csrf_rejected" {
		t.Errorf("error.code = %q, want %q", got, "csrf_rejected")
	}

	if revokedAt := rowRevokedAt(t, sess.ID); revokedAt != nil {
		t.Error("session row's revoked_at is non-NULL after a CSRF-rejected logout, want NULL (never touched)")
	}
}

// TestLogout_WrongMethod_Returns405 proves the route enforces POST only.
func TestLogout_WrongMethod_Returns405(t *testing.T) {
	handler, _ := newSessionAPITestService(t)

	resp := doJSON(t, handler, http.MethodGet, auth.LogoutPath, "", "", "") //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET %s status = %d, want %d", auth.LogoutPath, resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

// newRotationCapableTestService is TestGetMe_RotatesSessionOnAuthenticate_SetsNewCookie's
// (me_test.go) setup, factored out for reuse here: a bare Service whose
// internal *SessionManager is swapped (SetSessionManagerForTest,
// export_test.go) for one driven by a fake, advanceable clock, so a test
// can push a session's age past rotationAge (24h) deterministically and
// drive the resulting rotation through the REAL HTTP handler chain
// instead of racing the real wall clock.
func newRotationCapableTestService(t *testing.T) (http.Handler, *store.Queries, *testutil.Clock, *auth.SessionManager) {
	t.Helper()

	q := newTestQueries(t)
	svc, err := auth.NewService(testLogger(), config.Config{PublicOrigin: testPublicOrigin}, q)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	clk := testutil.NewClockAtEpoch()
	sm := auth.NewSessionManagerForTest(q, clk.Now)
	auth.SetSessionManagerForTest(svc, sm)
	handler := api.New(testLogger(), noopPinger{}, api.Options{}, svc.RegisterRoutes)
	return handler, q, clk, sm
}

// TestLogout_RotatedRequest_RevokesPredecessorToo is fix round 1's
// finding I2 (design owner ruling DD-C14): logout kills the whole
// credential LINEAGE, not just the successor row the logout request was
// itself authenticated with. Issues a session, ages it past rotationAge
// via a fake clock, drives a request that rotates it (GET /me), then logs
// out using the SUCCESSOR's own cookie -- the OLD (predecessor) raw token
// must stop authenticating immediately, not merely remain valid for up to
// rotationGrace (60s) the way it would have before this fix (the exact
// exposure the review traced).
func TestLogout_RotatedRequest_RevokesPredecessorToo(t *testing.T) {
	handler, q, clk, sm := newRotationCapableTestService(t)

	userID := createTestUser(t, q)
	rawOld, _, err := sm.Issue(context.Background(), userID, "ua", "203.0.113.92")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	clk.Advance(25 * time.Hour) // past rotationAge (24h)

	rotateResp := doJSON(t, handler, http.MethodGet, auth.MePath, "", "", "", sessionRequestCookie(rawOld)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if rotateResp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s (rotating request) status = %d, want %d", auth.MePath, rotateResp.StatusCode, http.StatusOK)
	}
	successor := extractCookie(rotateResp, auth.SessionCookieName)
	if successor == nil {
		t.Fatal("rotating request did not carry a successor Set-Cookie -- test setup broken, not the code under test")
	}
	successorSess, err := q.GetSessionByTokenHash(context.Background(), sessionTokenHash(successor.Value))
	if err != nil {
		t.Fatalf("look up successor row by its new token: %v", err)
	}

	logoutResp := doJSON(t, handler, http.MethodPost, auth.LogoutPath, testPublicOrigin, csrfTokenFor(successorSess), "", sessionRequestCookie(successor.Value)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if logoutResp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout with the successor's own cookie status = %d, want %d", logoutResp.StatusCode, http.StatusNoContent)
	}

	// The exposure this fix closes: the OLD (predecessor) raw token must
	// no longer authenticate at all -- not merely "until its grace window
	// expires."
	after := doJSON(t, handler, http.MethodGet, auth.MePath, "", "", "", sessionRequestCookie(rawOld)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if after.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET %s with the PREDECESSOR's raw token after logout (via the successor) status = %d, want %d (DD-C14: logout must kill the whole lineage)", auth.MePath, after.StatusCode, http.StatusUnauthorized)
	}

	// For completeness: the successor's own token is of course also dead
	// (ordinary logout behavior, not this fix's own novel assertion).
	afterSuccessor := doJSON(t, handler, http.MethodGet, auth.MePath, "", "", "", sessionRequestCookie(successor.Value)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if afterSuccessor.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET %s with the successor's own raw token after its own logout status = %d, want %d", auth.MePath, afterSuccessor.StatusCode, http.StatusUnauthorized)
	}
}

// TestDeleteSession_RevokingRotatedCurrentSession_RevokesPredecessorToo
// is DD-C14's DELETE /sessions/{id} counterpart to
// TestLogout_RotatedRequest_RevokesPredecessorToo above: revoking one's
// own CURRENT session by id, after an in-flight rotation, must revoke the
// predecessor too.
func TestDeleteSession_RevokingRotatedCurrentSession_RevokesPredecessorToo(t *testing.T) {
	handler, q, clk, sm := newRotationCapableTestService(t)

	userID := createTestUser(t, q)
	rawOld, _, err := sm.Issue(context.Background(), userID, "ua", "203.0.113.93")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	clk.Advance(25 * time.Hour) // past rotationAge (24h)

	rotateResp := doJSON(t, handler, http.MethodGet, auth.MePath, "", "", "", sessionRequestCookie(rawOld)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if rotateResp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s (rotating request) status = %d, want %d", auth.MePath, rotateResp.StatusCode, http.StatusOK)
	}
	successor := extractCookie(rotateResp, auth.SessionCookieName)
	if successor == nil {
		t.Fatal("rotating request did not carry a successor Set-Cookie -- test setup broken, not the code under test")
	}
	successorSess, err := q.GetSessionByTokenHash(context.Background(), sessionTokenHash(successor.Value))
	if err != nil {
		t.Fatalf("look up successor row by its new token: %v", err)
	}

	// DELETE /sessions/{id} requires a RECENT reauthentication. Rotation
	// copies reauthenticated_at forward from the predecessor unchanged
	// (session.go's tryRotate, by design -- rotation must never itself
	// satisfy the recent-reauth gate), which by the fake clock's own
	// 25h-advanced "now" is stale -- touch it fresh (by the SAME fake
	// clock the Service under test reads) so only DD-C14's own behavior
	// is under test here, not an unrelated reauth rejection.
	pool := newRowInspectorPool(t)
	if _, err := pool.Exec(context.Background(), `UPDATE sessions SET reauthenticated_at = $2 WHERE id = $1`, successorSess.ID, clk.Now()); err != nil {
		t.Fatalf("touch successor's reauthenticated_at fresh: %v", err)
	}

	delResp := doJSON(t, handler, http.MethodDelete, sessionIDPath(successorSess.ID), testPublicOrigin, csrfTokenFor(successorSess), "", sessionRequestCookie(successor.Value)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE own current (rotated) session status = %d, want %d", delResp.StatusCode, http.StatusNoContent)
	}

	after := doJSON(t, handler, http.MethodGet, auth.MePath, "", "", "", sessionRequestCookie(rawOld)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if after.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET %s with the PREDECESSOR's raw token after revoking the successor via DELETE /sessions/{id} status = %d, want %d (DD-C14)", auth.MePath, after.StatusCode, http.StatusUnauthorized)
	}
}

// ---- GET /api/v1/sessions (device list) ------------------------------------

// TestListSessions_ReturnsCallersDevicesWithCurrentFlag issues two
// sessions for one user, authenticates with one of them, and confirms
// both appear with exactly the authenticating one flagged current: true.
func TestListSessions_ReturnsCallersDevicesWithCurrentFlag(t *testing.T) {
	handler, q := newSessionAPITestService(t)
	userID := createTestUser(t, q)
	rawCurrent, current := issueTestSession(t, q, userID)
	_, other := issueTestSession(t, q, userID)

	resp := doJSON(t, handler, http.MethodGet, auth.SessionsPath, "", "", "", sessionRequestCookie(rawCurrent)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body sessionsListEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if len(body.Data) != 2 {
		t.Fatalf("len(data) = %d, want 2", len(body.Data))
	}

	seen := map[string]bool{}
	for _, entry := range body.Data {
		seen[entry.ID] = entry.Current
		if entry.ID == current.ID.String() && !entry.Current {
			t.Errorf("entry for the authenticating session (%s) has current = false, want true", entry.ID)
		}
		if entry.ID == other.ID.String() && entry.Current {
			t.Errorf("entry for the OTHER session (%s) has current = true, want false", entry.ID)
		}
		if entry.CreatedAt.Location() != time.UTC {
			t.Errorf("entry %s createdAt location = %v, want UTC", entry.ID, entry.CreatedAt.Location())
		}
	}
	if !seen[current.ID.String()] {
		t.Errorf("device list missing the authenticating session %s", current.ID)
	}
	if _, ok := seen[other.ID.String()]; !ok {
		t.Errorf("device list missing the other live session %s", other.ID)
	}
}

// TestListSessions_ExcludesRevokedSessions proves an explicitly revoked
// session never appears in the caller's own device list.
func TestListSessions_ExcludesRevokedSessions(t *testing.T) {
	handler, q := newSessionAPITestService(t)
	userID := createTestUser(t, q)
	rawCurrent, current := issueTestSession(t, q, userID)
	_, revoked := issueTestSession(t, q, userID)

	sm := auth.NewSessionManager(q)
	if err := sm.Revoke(context.Background(), revoked.ID); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}

	resp := doJSON(t, handler, http.MethodGet, auth.SessionsPath, "", "", "", sessionRequestCookie(rawCurrent)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var body sessionsListEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	for _, entry := range body.Data {
		if entry.ID == revoked.ID.String() {
			t.Errorf("device list includes the revoked session %s, want it excluded", revoked.ID)
		}
	}
	if len(body.Data) != 1 || body.Data[0].ID != current.ID.String() {
		t.Errorf("data = %+v, want exactly the one live session %s", body.Data, current.ID)
	}
}

// TestListSessions_ExcludesGraceDeadRotationPredecessors is Task 7's
// ledger obligation, exercised directly: a session whose rotation_grace_until
// has passed but whose revoked_at is still NULL (a rotation predecessor
// whose successor already exists) must never appear in the caller's own
// device list, even though revoked_at alone wouldn't reject it.
func TestListSessions_ExcludesGraceDeadRotationPredecessors(t *testing.T) {
	handler, q := newSessionAPITestService(t)
	userID := createTestUser(t, q)
	rawCurrent, current := issueTestSession(t, q, userID)
	_, predecessor := issueTestSession(t, q, userID)
	forceRotationGraceDead(t, predecessor.ID)

	resp := doJSON(t, handler, http.MethodGet, auth.SessionsPath, "", "", "", sessionRequestCookie(rawCurrent)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var body sessionsListEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	for _, entry := range body.Data {
		if entry.ID == predecessor.ID.String() {
			t.Errorf("device list includes the grace-dead predecessor %s, want it excluded (revoked_at is NULL but rotation_grace_until is in the past)", predecessor.ID)
		}
	}
	if len(body.Data) != 1 || body.Data[0].ID != current.ID.String() {
		t.Errorf("data = %+v, want exactly the one live session %s", body.Data, current.ID)
	}
}

// TestListSessions_ExcludesIdleExpiredSessions is fix round 1's finding
// I1: the original ListLiveSessionsForUser only excluded revoked and
// grace-dead rows, silently listing an idle-expired session (last_seen_at
// older than idleTimeout, 30d) as a live device -- one of sessionDead's
// four death modes it was missing.
func TestListSessions_ExcludesIdleExpiredSessions(t *testing.T) {
	handler, q := newSessionAPITestService(t)
	userID := createTestUser(t, q)
	rawCurrent, current := issueTestSession(t, q, userID)
	_, idleExpired := issueTestSession(t, q, userID)
	forceSessionIdleExpired(t, idleExpired.ID)

	resp := doJSON(t, handler, http.MethodGet, auth.SessionsPath, "", "", "", sessionRequestCookie(rawCurrent)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var body sessionsListEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	for _, entry := range body.Data {
		if entry.ID == idleExpired.ID.String() {
			t.Errorf("device list includes the idle-expired session %s, want it excluded", idleExpired.ID)
		}
	}
	if len(body.Data) != 1 || body.Data[0].ID != current.ID.String() {
		t.Errorf("data = %+v, want exactly the one live session %s", body.Data, current.ID)
	}
}

// TestListSessions_ExcludesAbsoluteExpiredSessions is fix round 1's
// finding I1's absolute-expiry counterpart to the idle-expiry test above.
func TestListSessions_ExcludesAbsoluteExpiredSessions(t *testing.T) {
	handler, q := newSessionAPITestService(t)
	userID := createTestUser(t, q)
	rawCurrent, current := issueTestSession(t, q, userID)
	_, absoluteExpired := issueTestSession(t, q, userID)
	forceSessionAbsoluteExpired(t, absoluteExpired.ID)

	resp := doJSON(t, handler, http.MethodGet, auth.SessionsPath, "", "", "", sessionRequestCookie(rawCurrent)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var body sessionsListEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	for _, entry := range body.Data {
		if entry.ID == absoluteExpired.ID.String() {
			t.Errorf("device list includes the absolute-expired session %s, want it excluded", absoluteExpired.ID)
		}
	}
	if len(body.Data) != 1 || body.Data[0].ID != current.ID.String() {
		t.Errorf("data = %+v, want exactly the one live session %s", body.Data, current.ID)
	}
}

// TestListSessions_OnlyReturnsCallersOwnRows proves user-scoping directly:
// two users, each with their own sessions; user A's device list must
// never include user B's session id, even though both users have the
// same NUMBER of sessions (a row-count-only check would miss a scoping
// bug that swapped which rows are returned).
func TestListSessions_OnlyReturnsCallersOwnRows(t *testing.T) {
	handler, q := newSessionAPITestService(t)
	userA := createTestUser(t, q)
	userB := createTestUser(t, q)
	rawA, sessA := issueTestSession(t, q, userA)
	_, sessB := issueTestSession(t, q, userB)

	resp := doJSON(t, handler, http.MethodGet, auth.SessionsPath, "", "", "", sessionRequestCookie(rawA)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var body sessionsListEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	if len(body.Data) != 1 || body.Data[0].ID != sessA.ID.String() {
		t.Fatalf("data = %+v, want exactly userA's own session %s", body.Data, sessA.ID)
	}
	for _, entry := range body.Data {
		if entry.ID == sessB.ID.String() {
			t.Fatal("userA's device list includes userB's session id, want strict user scoping")
		}
	}
}

// TestSessionsCollection_UnsupportedMethod_Returns405 proves the shared
// /sessions dispatcher rejects any method beyond GET/DELETE.
func TestSessionsCollection_UnsupportedMethod_Returns405(t *testing.T) {
	handler, _ := newSessionAPITestService(t)

	resp := doJSON(t, handler, http.MethodPut, auth.SessionsPath, "", "", "") //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("PUT %s status = %d, want %d", auth.SessionsPath, resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

// ---- DELETE /api/v1/sessions/{id} (per-session revoke) ---------------------

func sessionIDPath(id uuid.UUID) string {
	return auth.SessionsPath + "/" + id.String()
}

// TestDeleteSession_OwnCurrentSessionID_RevokesAndClearsCookie: revoking
// the session the request is itself authenticated with clears its cookie
// in this same response (task-9-brief.md's explicit requirement).
func TestDeleteSession_OwnCurrentSessionID_RevokesAndClearsCookie(t *testing.T) {
	handler, q := newSessionAPITestService(t)
	userID := createTestUser(t, q)
	raw, sess := issueTestSession(t, q, userID)

	resp := doJSON(t, handler, http.MethodDelete, sessionIDPath(sess.ID), testPublicOrigin, csrfTokenFor(sess), "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
	assertNoBody(t, resp)

	cleared := extractCookie(resp, auth.SessionCookieName)
	if cleared == nil || cleared.MaxAge >= 0 {
		t.Error("__Host-session not cleared when revoking the current session's own id")
	}
	if revokedAt := rowRevokedAt(t, sess.ID); revokedAt == nil {
		t.Error("session row's revoked_at is still NULL, want non-NULL")
	}
}

// TestDeleteSession_OwnOtherSessionID_RevokesWithoutClearingCurrentCookie
// is the negative case: revoking a DIFFERENT session the caller owns must
// not clear the cookie of the session the request is authenticated with.
func TestDeleteSession_OwnOtherSessionID_RevokesWithoutClearingCurrentCookie(t *testing.T) {
	handler, q := newSessionAPITestService(t)
	userID := createTestUser(t, q)
	rawCurrent, current := issueTestSession(t, q, userID)
	_, other := issueTestSession(t, q, userID)

	resp := doJSON(t, handler, http.MethodDelete, sessionIDPath(other.ID), testPublicOrigin, csrfTokenFor(current), "", sessionRequestCookie(rawCurrent)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	if extractCookie(resp, auth.SessionCookieName) != nil {
		t.Error("__Host-session was cleared when revoking a DIFFERENT session, want it left alone")
	}
	if revokedAt := rowRevokedAt(t, other.ID); revokedAt == nil {
		t.Error("the targeted OTHER session's revoked_at is still NULL, want non-NULL")
	}
	if revokedAt := rowRevokedAt(t, current.ID); revokedAt != nil {
		t.Error("the CURRENT session's revoked_at is non-NULL, want it untouched")
	}
}

// TestDeleteSession_AnotherUsersSessionID_Returns404AndLeavesItLive is
// task-9-brief.md's explicit requirement: revoking another user's session
// id returns 404 (never 403 -- DD-C5's no-oracle contract), and the
// target session is never actually touched.
func TestDeleteSession_AnotherUsersSessionID_Returns404AndLeavesItLive(t *testing.T) {
	handler, q := newSessionAPITestService(t)
	userA := createTestUser(t, q)
	userB := createTestUser(t, q)
	rawA, sessA := issueTestSession(t, q, userA)
	rawB, sessB := issueTestSession(t, q, userB)

	resp := doJSON(t, handler, http.MethodDelete, sessionIDPath(sessB.ID), testPublicOrigin, csrfTokenFor(sessA), "", sessionRequestCookie(rawA)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
	if got := decodeErrorCode(t, resp); got != "not_found" {
		t.Errorf("error.code = %q, want %q", got, "not_found")
	}

	if revokedAt := rowRevokedAt(t, sessB.ID); revokedAt != nil {
		t.Error("userB's session was revoked by userA's request, want it untouched")
	}
	sm := auth.NewSessionManager(q)
	if _, _, err := sm.Authenticate(context.Background(), rawB); err != nil {
		t.Errorf("Authenticate(userB's token) after userA's rejected DELETE error = %v, want nil (must still authenticate)", err)
	}
}

// TestDeleteSession_UnknownSessionID_Returns404 covers an id that parses
// as a UUID but names no row at all -- the same 404 as "belongs to
// someone else" (no oracle).
func TestDeleteSession_UnknownSessionID_Returns404(t *testing.T) {
	handler, q := newSessionAPITestService(t)
	userID := createTestUser(t, q)
	raw, sess := issueTestSession(t, q, userID)

	resp := doJSON(t, handler, http.MethodDelete, sessionIDPath(uuid.New()), testPublicOrigin, csrfTokenFor(sess), "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
	if got := decodeErrorCode(t, resp); got != "not_found" {
		t.Errorf("error.code = %q, want %q", got, "not_found")
	}
}

// TestDeleteSession_MalformedSessionID_Returns404 covers a path segment
// that isn't even a valid UUID -- still the same uniform 404, never a
// distinct 400 that would leak which case applied.
func TestDeleteSession_MalformedSessionID_Returns404(t *testing.T) {
	handler, q := newSessionAPITestService(t)
	userID := createTestUser(t, q)
	raw, sess := issueTestSession(t, q, userID)

	resp := doJSON(t, handler, http.MethodDelete, auth.SessionsPath+"/not-a-uuid", testPublicOrigin, csrfTokenFor(sess), "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
	if got := decodeErrorCode(t, resp); got != "not_found" {
		t.Errorf("error.code = %q, want %q", got, "not_found")
	}
}

// TestDeleteSession_WithoutRecentReauth_Returns403AndLeavesSessionLive is
// DD-C11's per-session-revoke reauth gate (the spec correction that added
// recent-reauth to this endpoint, not just DELETE /sessions): a stale
// reauthenticated_at rejects the request before RevokeForUser ever runs.
func TestDeleteSession_WithoutRecentReauth_Returns403AndLeavesSessionLive(t *testing.T) {
	handler, q := newSessionAPITestService(t)
	userID := createTestUser(t, q)
	raw, sess := issueTestSession(t, q, userID)
	forceReauthenticatedAtStale(t, sess.ID)

	resp := doJSON(t, handler, http.MethodDelete, sessionIDPath(sess.ID), testPublicOrigin, csrfTokenFor(sess), "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
	if got := decodeErrorCode(t, resp); got != "reauth_required" {
		t.Errorf("error.code = %q, want %q", got, "reauth_required")
	}

	if revokedAt := rowRevokedAt(t, sess.ID); revokedAt != nil {
		t.Error("session row's revoked_at is non-NULL after a reauth-rejected DELETE, want NULL (never touched)")
	}
}

// TestDeleteSession_OwnDeadSessionID_Returns404 is fix round 1's finding
// M5 (self-consistency with I1): revoking one's own session id must 404,
// not succeed, when that session is already dead by ANY of sessionDead's
// predicates -- grace-dead, idle-expired, or absolute-expired -- exactly
// mirroring what GET /sessions already excludes from the device list. A
// caller must never be able to "revoke" a row the list itself says
// doesn't exist.
func TestDeleteSession_OwnDeadSessionID_Returns404(t *testing.T) {
	tests := []struct {
		name     string
		makeDead func(t *testing.T, id uuid.UUID)
	}{
		{"grace-dead rotation predecessor", forceRotationGraceDead},
		{"idle-expired", forceSessionIdleExpired},
		{"absolute-expired", forceSessionAbsoluteExpired},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, q := newSessionAPITestService(t)
			userID := createTestUser(t, q)
			rawCaller, caller := issueTestSession(t, q, userID)
			_, dead := issueTestSession(t, q, userID)
			tt.makeDead(t, dead.ID)

			resp := doJSON(t, handler, http.MethodDelete, sessionIDPath(dead.ID), testPublicOrigin, csrfTokenFor(caller), "", sessionRequestCookie(rawCaller)) //nolint:bodyclose // doJSON closes the body itself before returning.
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
			}
			if got := decodeErrorCode(t, resp); got != "not_found" {
				t.Errorf("error.code = %q, want %q", got, "not_found")
			}

			if revokedAt := rowRevokedAt(t, dead.ID); revokedAt != nil {
				t.Errorf("dead session's revoked_at = %v after a rejected DELETE, want nil (RevokeForUser must affect zero rows, not set revoked_at as a side effect)", revokedAt)
			}
		})
	}
}

// ---- DELETE /api/v1/sessions (logout-everywhere) ---------------------------

// TestDeleteAllSessions_RevokesEveryLiveSessionForCaller is the happy
// path: every one of the caller's sessions is revoked (row-level proof),
// the current cookie is cleared, Clear-Site-Data is set, and a different
// user's session is left untouched.
func TestDeleteAllSessions_RevokesEveryLiveSessionForCaller(t *testing.T) {
	handler, q := newSessionAPITestService(t)
	userID := createTestUser(t, q)
	rawCurrent, current := issueTestSession(t, q, userID)
	_, other := issueTestSession(t, q, userID)

	otherUser := createTestUser(t, q)
	rawUnrelated, unrelated := issueTestSession(t, q, otherUser)

	resp := doJSON(t, handler, http.MethodDelete, auth.SessionsPath, testPublicOrigin, csrfTokenFor(current), "", sessionRequestCookie(rawCurrent)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
	assertNoBody(t, resp)

	cleared := extractCookie(resp, auth.SessionCookieName)
	if cleared == nil || cleared.MaxAge >= 0 {
		t.Error("__Host-session not cleared by logout-everywhere")
	}
	if got := resp.Header.Get("Clear-Site-Data"); got != `"cookies", "storage"` {
		t.Errorf("Clear-Site-Data = %q, want %q", got, `"cookies", "storage"`)
	}

	if revokedAt := rowRevokedAt(t, current.ID); revokedAt == nil {
		t.Error("current session's revoked_at is still NULL, want non-NULL")
	}
	if revokedAt := rowRevokedAt(t, other.ID); revokedAt == nil {
		t.Error("other session's revoked_at is still NULL, want non-NULL")
	}
	if revokedAt := rowRevokedAt(t, unrelated.ID); revokedAt != nil {
		t.Error("a DIFFERENT user's session was revoked by logout-everywhere, want it untouched")
	}

	sm := auth.NewSessionManager(q)
	if _, _, err := sm.Authenticate(context.Background(), rawUnrelated); err != nil {
		t.Errorf("Authenticate(unrelated user's token) after logout-everywhere error = %v, want nil", err)
	}
}

// TestDeleteAllSessions_StaleReauth_RejectsBeforeRevoking is
// task-9-brief.md Step 2's explicitly required spy/row-count assertion: a
// stale reauthenticated_at must reject the request with 403
// reauth_required BEFORE RevokeAll ever touches a row -- proven here by
// asserting, directly against the table, that every one of the caller's
// sessions still has revoked_at NULL afterward, and that they still
// authenticate.
func TestDeleteAllSessions_StaleReauth_RejectsBeforeRevoking(t *testing.T) {
	handler, q := newSessionAPITestService(t)
	userID := createTestUser(t, q)
	rawCurrent, current := issueTestSession(t, q, userID)
	rawOther, other := issueTestSession(t, q, userID)
	forceReauthenticatedAtStale(t, current.ID)

	resp := doJSON(t, handler, http.MethodDelete, auth.SessionsPath, testPublicOrigin, csrfTokenFor(current), "", sessionRequestCookie(rawCurrent)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
	if got := decodeErrorCode(t, resp); got != "reauth_required" {
		t.Errorf("error.code = %q, want %q", got, "reauth_required")
	}

	if resp.Header.Get("Clear-Site-Data") != "" {
		t.Error("Clear-Site-Data was set on a reauth-rejected logout-everywhere, want none")
	}
	if extractCookie(resp, auth.SessionCookieName) != nil {
		t.Error("__Host-session was cleared on a reauth-rejected logout-everywhere, want it untouched")
	}

	// Row-count proof: NEITHER of the caller's two sessions was revoked --
	// a bug that revoked-then-discovered-it-should-refuse would fail this,
	// even though the response itself already looked like a clean 403.
	if revokedAt := rowRevokedAt(t, current.ID); revokedAt != nil {
		t.Error("current session's revoked_at is non-NULL after a reauth-rejected logout-everywhere, want NULL")
	}
	if revokedAt := rowRevokedAt(t, other.ID); revokedAt != nil {
		t.Error("other session's revoked_at is non-NULL after a reauth-rejected logout-everywhere, want NULL")
	}

	sm := auth.NewSessionManager(q)
	if _, _, err := sm.Authenticate(context.Background(), rawOther); err != nil {
		t.Errorf("Authenticate(other session's token) after a reauth-rejected logout-everywhere error = %v, want nil", err)
	}
}

// TestDeleteAllSessions_WithoutCSRFToken_Returns403AndTouchesNothing
// proves RequireCSRF gates logout-everywhere too, running even before the
// reauth check: with no X-CSRF-Token, nothing is revoked regardless of
// how recent reauthenticated_at is.
func TestDeleteAllSessions_WithoutCSRFToken_Returns403AndTouchesNothing(t *testing.T) {
	handler, q := newSessionAPITestService(t)
	userID := createTestUser(t, q)
	rawCurrent, current := issueTestSession(t, q, userID)

	resp := doJSON(t, handler, http.MethodDelete, auth.SessionsPath, testPublicOrigin, "", "", sessionRequestCookie(rawCurrent)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
	if got := decodeErrorCode(t, resp); got != "csrf_rejected" {
		t.Errorf("error.code = %q, want %q", got, "csrf_rejected")
	}

	if revokedAt := rowRevokedAt(t, current.ID); revokedAt != nil {
		t.Error("session row's revoked_at is non-NULL after a CSRF-rejected logout-everywhere, want NULL (never touched)")
	}
}
