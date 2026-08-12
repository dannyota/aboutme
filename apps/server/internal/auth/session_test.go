// Package auth_test exercises SessionManager against live Postgres.
package auth_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// TestSessionCookieName_MatchesHostPrefixContract pins the literal value
// required by the session cookie contract. This matters
// beyond naming taste: a browser only honors a __Host- prefixed cookie
// when it is Secure, Path=/, and carries no Domain attribute.
func TestSessionCookieName_MatchesHostPrefixContract(t *testing.T) {
	const want = "__Host-session"
	if auth.SessionCookieName != want {
		t.Errorf("SessionCookieName = %q, want %q", auth.SessionCookieName, want)
	}
}

// TestIssueThenAuthenticate_ReturnsSameSession_NoRotation issues a session,
// authenticates with the raw token it
// returns, and confirm the round trip returns the same session with no
// rotation triggered (the session is brand new -- age 0, far under
// rotationAge).
func TestIssueThenAuthenticate_ReturnsSameSession_NoRotation(t *testing.T) {
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	sm := auth.NewSessionManager(q)
	ctx := context.Background()

	raw, issued, err := sm.Issue(ctx, userID, "Mozilla/5.0", "203.0.113.7")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if raw == "" {
		t.Fatal("Issue() raw token is empty, want a non-empty bearer token")
	}
	if issued.UserID != userID {
		t.Errorf("Issue() session.UserID = %v, want %v", issued.UserID, userID)
	}

	got, rotated, err := sm.Authenticate(ctx, raw)
	if err != nil {
		t.Fatalf("Authenticate() error = %v, want nil", err)
	}
	if got.ID != issued.ID {
		t.Errorf("Authenticate() session ID = %v, want %v (same session, age 0, no rotation due)", got.ID, issued.ID)
	}
	if rotated != "" {
		t.Errorf("Authenticate() rotatedToken = %q, want empty (no rotation due at age 0)", rotated)
	}
}

// ---- issuance ------------------------------------------------------------

// TestIssue_CreatesNewRowEachTime proves repeated issuance resists fixation by
// creating distinct tokens and rows.
func TestIssue_CreatesNewRowEachTime(t *testing.T) {
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	sm := auth.NewSessionManager(q)
	ctx := context.Background()

	raw1, sess1, err := sm.Issue(ctx, userID, "ua-1", "203.0.113.1")
	if err != nil {
		t.Fatalf("first Issue() error = %v", err)
	}
	raw2, sess2, err := sm.Issue(ctx, userID, "ua-2", "203.0.113.2")
	if err != nil {
		t.Fatalf("second Issue() error = %v", err)
	}

	if raw1 == raw2 {
		t.Error("Issue() returned the same raw token twice, want two distinct CSPRNG tokens")
	}
	if sess1.ID == sess2.ID {
		t.Error("Issue() returned the same session ID twice, want two distinct rows (fixation defense)")
	}
	if bytes.Equal(sess1.CSRFSecret, sess2.CSRFSecret) {
		t.Error("Issue() returned the same csrf_secret twice, want two distinct CSPRNG secrets")
	}

	// Both rows must independently authenticate -- the second Issue() must
	// not have invalidated or clobbered the first.
	got1, _, err := sm.Authenticate(ctx, raw1)
	if err != nil {
		t.Fatalf("Authenticate(raw1) error = %v, want nil", err)
	}
	if got1.ID != sess1.ID {
		t.Errorf("Authenticate(raw1) session ID = %v, want %v", got1.ID, sess1.ID)
	}
	got2, _, err := sm.Authenticate(ctx, raw2)
	if err != nil {
		t.Fatalf("Authenticate(raw2) error = %v, want nil", err)
	}
	if got2.ID != sess2.ID {
		t.Errorf("Authenticate(raw2) session ID = %v, want %v", got2.ID, sess2.ID)
	}
}

// TestIssue_StoresUAAndIP guards that Issue round-trips the caller-supplied
// user agent and IP onto the stored row for the device list.
func TestIssue_StoresUAAndIP(t *testing.T) {
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	sm := auth.NewSessionManager(q)
	ctx := context.Background()

	_, sess, err := sm.Issue(ctx, userID, "Mozilla/5.0 (Test)", "198.51.100.7")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if sess.UA == nil || *sess.UA != "Mozilla/5.0 (Test)" {
		t.Errorf("Issue() session.UA = %v, want %q", sess.UA, "Mozilla/5.0 (Test)")
	}
	if sess.IP == nil || sess.IP.String() != "198.51.100.7" {
		t.Errorf("Issue() session.IP = %v, want 198.51.100.7", sess.IP)
	}
}

// TestIssue_EmptyUAAndIP_StoresNil guards the other direction: an absent
// user agent or IP (both legitimate -- e.g. a proxy that strips them)
// must store as SQL NULL, not an empty string or the zero netip.Addr.
func TestIssue_EmptyUAAndIP_StoresNil(t *testing.T) {
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	sm := auth.NewSessionManager(q)
	ctx := context.Background()

	_, sess, err := sm.Issue(ctx, userID, "", "")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if sess.UA != nil {
		t.Errorf("Issue() session.UA = %v, want nil for empty input", sess.UA)
	}
	if sess.IP != nil {
		t.Errorf("Issue() session.IP = %v, want nil for empty input", sess.IP)
	}
}

// TestIssue_AbsoluteExpiresAtAnchoredToIssueTime guards the absolute
// timeout's anchor point directly: absolute_expires_at must be
// created_at + absoluteTimeout (90 days), computed at Issue time -- the
// value every later rotation must then copy forward unchanged.
func TestIssue_AbsoluteExpiresAtAnchoredToIssueTime(t *testing.T) {
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	clk := testutil.NewClockAtEpoch()
	sm := auth.NewSessionManagerForTest(q, clk.Now)
	ctx := context.Background()

	_, sess, err := sm.Issue(ctx, userID, "ua", "203.0.113.9")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	want := testutil.Epoch.Add(90 * 24 * time.Hour)
	if !sess.AbsoluteExpiresAt.Equal(want) {
		t.Errorf("Issue() session.AbsoluteExpiresAt = %v, want %v (created_at + 90d)", sess.AbsoluteExpiresAt, want)
	}
}

// ---- hashing --------------------------------------------------------------

// sessionTokenHash computes the expected stored value independently of the
// production helper.
func sessionTokenHash(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

// TestIssue_TokenHashedAtRest proves the raw token resolves through its
// independently computed SHA-256 hash.
func TestIssue_TokenHashedAtRest(t *testing.T) {
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	sm := auth.NewSessionManager(q)
	ctx := context.Background()

	raw, issued, err := sm.Issue(ctx, userID, "ua", "203.0.113.10")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	row, err := q.GetSessionByTokenHash(ctx, sessionTokenHash(raw))
	if err != nil {
		t.Fatalf("GetSessionByTokenHash(sha256(raw)) error = %v, want nil", err)
	}
	if row.ID != issued.ID {
		t.Errorf("GetSessionByTokenHash(sha256(raw)) ID = %v, want %v", row.ID, issued.ID)
	}

	// A row looked up by the raw token's own bytes (as if token_hash
	// stored the token in cleartext) must not exist.
	if _, err := q.GetSessionByTokenHash(ctx, []byte(raw)); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("GetSessionByTokenHash(raw token bytes) error = %v, want pgx.ErrNoRows (token_hash is not the raw token)", err)
	}
}

// ---- basic expiry branches -------------------------------------------------

// TestAuthenticate_UnknownToken_ReturnsSessionInvalid covers Authenticate's
// not-found branch: a token shaped like a real one but never issued.
func TestAuthenticate_UnknownToken_ReturnsSessionInvalid(t *testing.T) {
	q := newTestQueries(t)
	sm := auth.NewSessionManager(q)
	ctx := context.Background()

	if _, _, err := sm.Authenticate(ctx, "never-issued-token"); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("Authenticate(unknown token) error = %v, want ErrSessionInvalid", err)
	}
}

// TestAuthenticate_RevokedSession_ReturnsSessionInvalid covers
// Authenticate's revoked branch directly. Revocation is independent of any
// rotation grace window.
func TestAuthenticate_RevokedSession_ReturnsSessionInvalid(t *testing.T) {
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	sm := auth.NewSessionManager(q)
	ctx := context.Background()

	raw, sess, err := sm.Issue(ctx, userID, "ua", "203.0.113.11")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if err := sm.Revoke(ctx, sess.ID); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}

	if _, _, err := sm.Authenticate(ctx, raw); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("Authenticate(revoked session) error = %v, want ErrSessionInvalid", err)
	}
}

// TestAuthenticate_IdleExpiredSession_ReturnsSessionInvalid covers
// Authenticate's idle-expiry branch: a fake clock advanced more than
// idleTimeout (30d) past the session's last_seen_at, with no intervening
// request to refresh it.
func TestAuthenticate_IdleExpiredSession_ReturnsSessionInvalid(t *testing.T) {
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	clk := testutil.NewClockAtEpoch()
	sm := auth.NewSessionManagerForTest(q, clk.Now)
	ctx := context.Background()

	raw, _, err := sm.Issue(ctx, userID, "ua", "203.0.113.12")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	clk.Advance(30*24*time.Hour + time.Second)

	if _, _, err := sm.Authenticate(ctx, raw); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("Authenticate(idle-expired session) error = %v, want ErrSessionInvalid", err)
	}
}

// TestAuthenticate_AbsoluteExpiredSession_ReturnsSessionInvalid writes a fresh
// last_seen_at before authentication, isolating the absolute-expiry ceiling
// from idle expiry.
func TestAuthenticate_AbsoluteExpiredSession_ReturnsSessionInvalid(t *testing.T) {
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	clk := testutil.NewClockAtEpoch()
	sm := auth.NewSessionManagerForTest(q, clk.Now)
	ctx := context.Background()
	inspector := newRowInspectorPool(t)

	raw, issued, err := sm.Issue(ctx, userID, "ua", "203.0.113.13")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	recentActivity := clk.Advance(90*24*time.Hour + time.Second)
	if _, err := inspector.Exec(ctx, `UPDATE sessions SET last_seen_at = $2 WHERE id = $1`, issued.ID, recentActivity); err != nil {
		t.Fatalf("force last_seen_at fresh: %v", err)
	}

	if _, _, err := sm.Authenticate(ctx, raw); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("Authenticate(absolute-expired session, recent last_seen_at) error = %v, want ErrSessionInvalid (absolute expiry is a hard ceiling independent of activity)", err)
	}
}

// ---- throttle ---------------------------------------------------------------

// TestAuthenticate_WithinThrottleWindow_DoesNotUpdateLastSeenAt is
// lastSeenThrottle's negative case: two Authenticate calls one minute
// apart -- well inside the one-hour throttle window -- must leave
// last_seen_at at its original value after the second call.
func TestAuthenticate_WithinThrottleWindow_DoesNotUpdateLastSeenAt(t *testing.T) {
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	clk := testutil.NewClockAtEpoch()
	sm := auth.NewSessionManagerForTest(q, clk.Now)
	ctx := context.Background()

	raw, issued, err := sm.Issue(ctx, userID, "ua", "203.0.113.14")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	clk.Advance(time.Minute)
	if _, _, err = sm.Authenticate(ctx, raw); err != nil {
		t.Fatalf("Authenticate() error = %v, want nil", err)
	}

	row, err := q.GetSessionByTokenHash(ctx, sessionTokenHash(raw))
	if err != nil {
		t.Fatalf("GetSessionByTokenHash() error = %v", err)
	}
	if !row.LastSeenAt.Equal(issued.LastSeenAt) {
		t.Errorf("last_seen_at after a call 1m later = %v, want unchanged %v (throttle window is 1h)", row.LastSeenAt, issued.LastSeenAt)
	}
}

// TestAuthenticate_AfterThrottleWindow_UpdatesLastSeenAt is
// lastSeenThrottle's positive case: an Authenticate call more than an
// hour after the last write must advance last_seen_at to the call's own
// time.
func TestAuthenticate_AfterThrottleWindow_UpdatesLastSeenAt(t *testing.T) {
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	clk := testutil.NewClockAtEpoch()
	sm := auth.NewSessionManagerForTest(q, clk.Now)
	ctx := context.Background()

	raw, _, err := sm.Issue(ctx, userID, "ua", "203.0.113.15")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	wantSeen := clk.Advance(time.Hour + time.Second)
	if _, _, err = sm.Authenticate(ctx, raw); err != nil {
		t.Fatalf("Authenticate() error = %v, want nil", err)
	}

	row, err := q.GetSessionByTokenHash(ctx, sessionTokenHash(raw))
	if err != nil {
		t.Fatalf("GetSessionByTokenHash() error = %v", err)
	}
	if !row.LastSeenAt.Equal(wantSeen) {
		t.Errorf("last_seen_at after a call 1h1s later = %v, want %v (throttle window elapsed)", row.LastSeenAt, wantSeen)
	}
}

// ---- revoke / revoke-all / reauth touch ------------------------------------

// TestRevoke_SetsRevokedAt is Revoke's basic happy path: the target row's
// revoked_at becomes non-NULL.
func TestRevoke_SetsRevokedAt(t *testing.T) {
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	sm := auth.NewSessionManager(q)
	ctx := context.Background()

	raw, sess, err := sm.Issue(ctx, userID, "ua", "203.0.113.16")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if err = sm.Revoke(ctx, sess.ID); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}

	row, err := q.GetSessionByTokenHash(ctx, sessionTokenHash(raw))
	if err != nil {
		t.Fatalf("GetSessionByTokenHash() error = %v", err)
	}
	if row.RevokedAt == nil {
		t.Error("after Revoke(): revoked_at is NULL, want non-NULL")
	}
}

// TestRevoke_UnknownSessionID_IsNotAnError guards Revoke's documented
// idempotent-no-op behavior for a session id that doesn't exist -- logout
// must be safe to call twice (e.g. a client that double-submits) without
// erroring the second time.
func TestRevoke_UnknownSessionID_IsNotAnError(t *testing.T) {
	q := newTestQueries(t)
	sm := auth.NewSessionManager(q)
	ctx := context.Background()

	if err := sm.Revoke(ctx, uuid.New()); err != nil {
		t.Errorf("Revoke(unknown id) error = %v, want nil (idempotent no-op)", err)
	}
}

// TestRevokeForUser_AnotherUsersSessionID_AffectsZeroRowsAndStaysAuthenticating
// is the ownership check RevokeForUser adds over Revoke: a caller naming a
// session id that belongs to a different user must affect zero rows and
// must not revoke it -- the id still authenticates afterward. This is what
// lets DELETE /sessions/{id} turn 0 into a 404 without distinguishing
// another user's session from an unknown id, while leaving it untouched.
func TestRevokeForUser_AnotherUsersSessionID_AffectsZeroRowsAndStaysAuthenticating(t *testing.T) {
	q := newTestQueries(t)
	userA := createTestUser(t, q)
	userB := createTestUser(t, q)
	sm := auth.NewSessionManager(q)
	ctx := context.Background()

	rawB, sessB, err := sm.Issue(ctx, userB, "ua", "203.0.113.22")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	n, err := sm.RevokeForUser(ctx, sessB.ID, userA)
	if err != nil {
		t.Fatalf("RevokeForUser(userB's session, as userA) error = %v, want nil", err)
	}
	if n != 0 {
		t.Errorf("RevokeForUser(userB's session, as userA) affected %d rows, want 0", n)
	}

	if _, _, err := sm.Authenticate(ctx, rawB); err != nil {
		t.Errorf("Authenticate(userB's token) after RevokeForUser(as userA) error = %v, want nil (must not have been revoked)", err)
	}
}

// TestRevokeForUser_OwnSessionID_RevokesIt proves an owned live session is
// revoked and stops authenticating.
func TestRevokeForUser_OwnSessionID_RevokesIt(t *testing.T) {
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	sm := auth.NewSessionManager(q)
	ctx := context.Background()

	raw, sess, err := sm.Issue(ctx, userID, "ua", "203.0.113.23")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	n, err := sm.RevokeForUser(ctx, sess.ID, userID)
	if err != nil {
		t.Fatalf("RevokeForUser() error = %v, want nil", err)
	}
	if n != 1 {
		t.Errorf("RevokeForUser(own session) affected %d rows, want 1", n)
	}

	if _, _, err := sm.Authenticate(ctx, raw); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("Authenticate() after RevokeForUser(own session) error = %v, want ErrSessionInvalid", err)
	}
}

// TestRevokeAll_RevokesOnlyThatUsersSessions guards RevokeAll's user
// scoping and its int64 return count: two sessions for the target user
// (both revoked, count=2) and one session for a different user (left
// alone) -- a bug in the WHERE clause's user_id scoping would either
// under- or over-revoke.
func TestRevokeAll_RevokesOnlyThatUsersSessions(t *testing.T) {
	q := newTestQueries(t)
	userA := createTestUser(t, q)
	userB := createTestUser(t, q)
	sm := auth.NewSessionManager(q)
	ctx := context.Background()

	_, sessA1, err := sm.Issue(ctx, userA, "ua", "203.0.113.17")
	if err != nil {
		t.Fatalf("Issue() sessA1 error = %v", err)
	}
	_, sessA2, err := sm.Issue(ctx, userA, "ua", "203.0.113.18")
	if err != nil {
		t.Fatalf("Issue() sessA2 error = %v", err)
	}
	rawB, _, err := sm.Issue(ctx, userB, "ua", "203.0.113.19")
	if err != nil {
		t.Fatalf("Issue() sessB error = %v", err)
	}

	n, err := sm.RevokeAll(ctx, userA)
	if err != nil {
		t.Fatalf("RevokeAll() error = %v", err)
	}
	if n != 2 {
		t.Errorf("RevokeAll(userA) revoked %d rows, want 2", n)
	}

	inspector := newRowInspectorPool(t)
	var revokedA1, revokedA2 *time.Time
	if err := inspector.QueryRow(ctx, `SELECT revoked_at FROM sessions WHERE id = $1`, sessA1.ID).Scan(&revokedA1); err != nil {
		t.Fatalf("query sessA1.revoked_at: %v", err)
	}
	if revokedA1 == nil {
		t.Error("sessA1.revoked_at is NULL, want non-NULL")
	}
	if err := inspector.QueryRow(ctx, `SELECT revoked_at FROM sessions WHERE id = $1`, sessA2.ID).Scan(&revokedA2); err != nil {
		t.Fatalf("query sessA2.revoked_at: %v", err)
	}
	if revokedA2 == nil {
		t.Error("sessA2.revoked_at is NULL, want non-NULL")
	}

	// userB's session must be untouched -- both the row itself and the
	// fact that it still authenticates.
	if _, _, err := sm.Authenticate(ctx, rawB); err != nil {
		t.Errorf("Authenticate(userB's token) after RevokeAll(userA) error = %v, want nil (userB unaffected)", err)
	}
}

// TestTouchReauthenticated_UpdatesReauthenticatedAt guards the write used
// after a successful reauthentication round trip.
func TestTouchReauthenticated_UpdatesReauthenticatedAt(t *testing.T) {
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	clk := testutil.NewClockAtEpoch()
	sm := auth.NewSessionManagerForTest(q, clk.Now)
	ctx := context.Background()

	raw, sess, err := sm.Issue(ctx, userID, "ua", "203.0.113.20")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	wantReauth := clk.Advance(time.Hour)
	if err = sm.TouchReauthenticated(ctx, sess.ID); err != nil {
		t.Fatalf("TouchReauthenticated() error = %v", err)
	}

	row, err := q.GetSessionByTokenHash(ctx, sessionTokenHash(raw))
	if err != nil {
		t.Fatalf("GetSessionByTokenHash() error = %v", err)
	}
	if !row.ReauthenticatedAt.Equal(wantReauth) {
		t.Errorf("reauthenticated_at after TouchReauthenticated() = %v, want %v", row.ReauthenticatedAt, wantReauth)
	}
}

// ---- RequireRecentReauth (pure function, no database) ----------------------

func TestRequireRecentReauth(t *testing.T) {
	now := testutil.Epoch

	tests := []struct {
		name    string
		reauth  time.Time
		wantErr error
	}{
		{"just reauthenticated", now, nil},
		{"14 minutes ago, within window", now.Add(-14 * time.Minute), nil},
		{"exactly the window boundary", now.Add(-15 * time.Minute), nil},
		{"16 minutes ago, past window", now.Add(-16 * time.Minute), auth.ErrReauthRequired},
		{"90 days ago", now.Add(-90 * 24 * time.Hour), auth.ErrReauthRequired},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := store.Session{ReauthenticatedAt: tt.reauth}
			err := auth.RequireRecentReauth(sess, now)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("RequireRecentReauth() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("RequireRecentReauth() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// ---- rotation smoke coverage -----------------------------------------------

// TestAuthenticate_RotatesAfter24h_SequentialSingleRequest is a minimal,
// single-goroutine sanity check of the rotation algorithm's winning path:
// past rotationAge, one Authenticate call mints exactly one successor,
// carrying user_id, absolute_expires_at, and reauthenticated_at forward
// unchanged from the predecessor, and the predecessor's own token still
// authenticates (against itself, not the successor) until its grace
// window passes.
func TestAuthenticate_RotatesAfter24h_SequentialSingleRequest(t *testing.T) {
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	clk := testutil.NewClockAtEpoch()
	sm := auth.NewSessionManagerForTest(q, clk.Now)
	ctx := context.Background()

	oldRaw, predecessor, err := sm.Issue(ctx, userID, "ua", "203.0.113.21")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	clk.Advance(25 * time.Hour)

	successor, newRaw, err := sm.Authenticate(ctx, oldRaw)
	if err != nil {
		t.Fatalf("Authenticate() at age 25h error = %v, want nil", err)
	}
	if newRaw == "" {
		t.Fatal("Authenticate() at age 25h rotatedToken is empty, want a new token (rotation due)")
	}
	if successor.ID == predecessor.ID {
		t.Error("Authenticate() at age 25h returned the predecessor's own ID, want a new successor row")
	}
	if successor.UserID != predecessor.UserID {
		t.Errorf("successor.UserID = %v, want %v (copied from predecessor)", successor.UserID, predecessor.UserID)
	}
	if !successor.AbsoluteExpiresAt.Equal(predecessor.AbsoluteExpiresAt) {
		t.Errorf("successor.AbsoluteExpiresAt = %v, want %v (rotation must not extend absolute expiry)", successor.AbsoluteExpiresAt, predecessor.AbsoluteExpiresAt)
	}
	if !successor.ReauthenticatedAt.Equal(predecessor.ReauthenticatedAt) {
		t.Errorf("successor.ReauthenticatedAt = %v, want %v (rotation is not itself a fresh OAuth login -- must not reset the recent-reauth gate)", successor.ReauthenticatedAt, predecessor.ReauthenticatedAt)
	}
	// Rotation must mint a new CSRF secret while preserving session lineage.
	if len(successor.CSRFSecret) != 32 {
		t.Errorf("successor.CSRFSecret length = %d, want 32 (256-bit CSPRNG)", len(successor.CSRFSecret))
	}
	if bytes.Equal(successor.CSRFSecret, predecessor.CSRFSecret) {
		t.Error("successor.CSRFSecret equals predecessor.CSRFSecret, want a fresh secret minted on rotation")
	}

	inspector := newRowInspectorPool(t)
	var rowCount int
	if err = inspector.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id = $1`, userID).Scan(&rowCount); err != nil {
		t.Fatalf("count sessions for user: %v", err)
	}
	if rowCount != 2 {
		t.Errorf("sessions rows for user = %d, want 2 (predecessor + exactly one successor)", rowCount)
	}

	// The predecessor's own token is still within its grace window and
	// must still authenticate -- against itself, not the successor -- and
	// must not mint yet another successor.
	stillPredecessor, extraRaw, err := sm.Authenticate(ctx, oldRaw)
	if err != nil {
		t.Fatalf("Authenticate(old token, within grace) error = %v, want nil", err)
	}
	if stillPredecessor.ID != predecessor.ID {
		t.Errorf("Authenticate(old token, within grace) session ID = %v, want %v (the predecessor itself)", stillPredecessor.ID, predecessor.ID)
	}
	if extraRaw != "" {
		t.Errorf("Authenticate(old token, within grace) rotatedToken = %q, want empty (already rotated once; must not rotate again)", extraRaw)
	}

	// The new token authenticates as the successor.
	governingNew, rotatedAgain, err := sm.Authenticate(ctx, newRaw)
	if err != nil {
		t.Fatalf("Authenticate(new token) error = %v, want nil", err)
	}
	if governingNew.ID != successor.ID {
		t.Errorf("Authenticate(new token) session ID = %v, want %v (the successor)", governingNew.ID, successor.ID)
	}
	if rotatedAgain != "" {
		t.Errorf("Authenticate(new token) rotatedToken = %q, want empty (successor is brand new, far under rotationAge)", rotatedAgain)
	}
}

// ---- rotation delivery ------------------------------------------------------
//
// A successor's raw token is delivered on exactly ONE response and is
// never stored, so a lost response would make the successor unreachable.
// The predecessor remains usable until the successor is first used, which
// proves delivery. See docs/adr/0015-session-rotation-delivery.md.

// sessionRowRotationGraceUntil reads the rotation_grace_until column of
// one sessions row, NULL included (nil out), so a test can assert on the
// deferral marker itself rather than only on its observable effect.
func sessionRowRotationGraceUntil(ctx context.Context, t *testing.T, db rowQuerier, id uuid.UUID) *time.Time {
	t.Helper()
	var ts *time.Time
	if err := db.QueryRow(ctx, `SELECT rotation_grace_until FROM sessions WHERE id = $1`, id).Scan(&ts); err != nil {
		t.Fatalf("sessionRowRotationGraceUntil query error: %v", err)
	}
	return ts
}

// TestAuthenticate_UndeliveredSuccessor_PredecessorSurvivesPastGrace is
// the lost-response case: when the successor token never reaches the
// client, the predecessor token must keep working past rotationGrace
// because nothing has proven the successor is reachable.
func TestAuthenticate_UndeliveredSuccessor_PredecessorSurvivesPastGrace(t *testing.T) {
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	clk := testutil.NewClockAtEpoch()
	sm := auth.NewSessionManagerForTest(q, clk.Now)
	ctx := context.Background()

	oldRaw, predecessor, err := sm.Issue(ctx, userID, "ua", "203.0.113.31")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	clk.Advance(rotationAge + time.Hour)

	successor, newRaw, err := sm.Authenticate(ctx, oldRaw)
	if err != nil {
		t.Fatalf("Authenticate() at 25h error = %v", err)
	}
	if newRaw == "" {
		t.Fatal("Authenticate() at 25h returned no rotatedToken, want rotation to have occurred")
	}
	// Never present newRaw; this models a lost response.

	clk.Advance(rotationGrace + time.Second)

	sess, rotatedAgain, err := sm.Authenticate(ctx, oldRaw)
	if err != nil {
		t.Fatalf("Authenticate(predecessor token) %v after rotation error = %v, want nil (the successor was never used, so the predecessor must not have died)", rotationGrace+time.Second, err)
	}
	if sess.ID != predecessor.ID {
		t.Errorf("Authenticate(predecessor token) session ID = %v, want %v (the predecessor itself)", sess.ID, predecessor.ID)
	}
	if rotatedAgain != "" {
		t.Errorf("Authenticate(predecessor token) rotatedToken = %q, want empty (already rotated once; a second successor would multiply live credentials from one lineage)", rotatedAgain)
	}

	// Still true many hours later, anywhere inside the deferral window:
	// the 60-second countdown has not started at all, because nothing has
	// proven the successor reachable. The window is bounded; see
	// TestAuthenticate_UndeliveredSuccessor_PredecessorDiesAtBoundedWindow
	// for the far side of it. This walks to just inside that bound.
	clk.Advance(rotationAge - 2*rotationGrace)
	setSessionRowLastSeenAt(ctx, t, newRowInspectorPool(t), predecessor.ID, clk.Now())
	if _, _, err := sm.Authenticate(ctx, oldRaw); err != nil {
		t.Errorf("Authenticate(predecessor token) just inside the deferral bound after an undelivered rotation error = %v, want nil", err)
	}

	// The successor row itself remains live.
	inspector := newRowInspectorPool(t)
	if got := sessionRowRotationGraceUntil(ctx, t, inspector, successor.ID); got != nil {
		t.Errorf("successor rotation_grace_until = %v, want NULL (the successor is not itself rotating)", got)
	}
}

// TestAuthenticate_SuccessorFirstUse_StartsPredecessorGrace is the other
// delivery case: the predecessor's grace window starts when the
// successor is FIRST USED, and runs exactly rotationGrace from that
// instant -- long enough for requests already in flight with the old
// token, and no longer.
func TestAuthenticate_SuccessorFirstUse_StartsPredecessorGrace(t *testing.T) {
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	clk := testutil.NewClockAtEpoch()
	sm := auth.NewSessionManagerForTest(q, clk.Now)
	ctx := context.Background()
	inspector := newRowInspectorPool(t)

	oldRaw, predecessor, err := sm.Issue(ctx, userID, "ua", "203.0.113.32")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	clk.Advance(rotationAge + time.Hour)

	_, newRaw, err := sm.Authenticate(ctx, oldRaw)
	if err != nil {
		t.Fatalf("Authenticate() at 25h error = %v", err)
	}

	// A long gap between the rotation and the successor's first use (the
	// browser was closed, the tab was backgrounded): the predecessor is
	// still alive throughout, because its grace has not started.
	clk.Advance(2 * time.Hour)
	if _, _, err := sm.Authenticate(ctx, oldRaw); err != nil {
		t.Fatalf("Authenticate(predecessor token) 2h after rotation, successor still unused, error = %v, want nil", err)
	}

	firstUse := clk.Now()
	if _, _, err := sm.Authenticate(ctx, newRaw); err != nil {
		t.Fatalf("Authenticate(successor token) first use error = %v", err)
	}

	graceUntil := sessionRowRotationGraceUntil(ctx, t, inspector, predecessor.ID)
	if graceUntil == nil {
		t.Fatal("predecessor rotation_grace_until is NULL after the successor's first use, want a real deadline")
	}
	if want := firstUse.Add(rotationGrace); !graceUntil.Equal(want) {
		t.Errorf("predecessor rotation_grace_until = %v, want %v (first use + rotationGrace)", graceUntil, want)
	}

	// Inside the window the old token still works (requests already in
	// flight when the successor's cookie landed).
	clk.Advance(rotationGrace / 2)
	if _, _, err := sm.Authenticate(ctx, oldRaw); err != nil {
		t.Errorf("Authenticate(predecessor token) inside the started grace window error = %v, want nil", err)
	}

	// Past it, the predecessor is dead and only the successor survives.
	clk.Advance(rotationGrace)
	if _, _, err := sm.Authenticate(ctx, oldRaw); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("Authenticate(predecessor token) past the started grace window error = %v, want ErrSessionInvalid", err)
	}
	if _, _, err := sm.Authenticate(ctx, newRaw); err != nil {
		t.Errorf("Authenticate(successor token) past the predecessor's grace error = %v, want nil (the successor is unaffected)", err)
	}

	// Repeated successor use must never push the predecessor's deadline
	// back out: the window starts once, at first use, and only shrinks.
	if got := sessionRowRotationGraceUntil(ctx, t, inspector, predecessor.ID); got == nil || !got.Equal(firstUse.Add(rotationGrace)) {
		t.Errorf("predecessor rotation_grace_until after further successor use = %v, want it pinned at %v", got, firstUse.Add(rotationGrace))
	}
}

// ---- the deferral's upper bound ---------------------------------------------
//
// An unbounded delivery deferral is a security defect. RFC 9700 and OWASP's
// session guidance both rest rotation's value on the superseded credential
// ceasing to work promptly. The bound is one rotation interval. A client
// that has not used its successor within rotationAge is gone, so
// nothing past that buys availability -- it only buys an attacker time.

// TestAuthenticate_UndeliveredSuccessor_PredecessorDiesAtBoundedWindow is
// that bound, stated as the property: after a rotation whose successor is
// never used, the predecessor survives the whole deferral window and then
// dies -- it does NOT stay alive to its absolute expiry.
func TestAuthenticate_UndeliveredSuccessor_PredecessorDiesAtBoundedWindow(t *testing.T) {
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	clk := testutil.NewClockAtEpoch()
	sm := auth.NewSessionManagerForTest(q, clk.Now)
	ctx := context.Background()
	inspector := newRowInspectorPool(t)

	oldRaw, predecessor, err := sm.Issue(ctx, userID, "ua", "203.0.113.33")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	clk.Advance(rotationAge + time.Hour)
	rotatedAt := clk.Now()

	_, newRaw, err := sm.Authenticate(ctx, oldRaw)
	if err != nil {
		t.Fatalf("Authenticate() at 25h error = %v", err)
	}
	if newRaw == "" {
		t.Fatal("Authenticate() at 25h returned no rotatedToken, want rotation to have occurred")
	}
	// Never present newRaw; this models a lost response.

	parked := sessionRowRotationGraceUntil(ctx, t, inspector, predecessor.ID)
	if parked == nil {
		t.Fatal("predecessor rotation_grace_until is NULL immediately after rotation, want the deferred deadline (BeginSessionRotation's CAS depends on it being non-NULL)")
	}
	if want := rotatedAt.Add(rotationAge); !parked.Equal(want) {
		t.Errorf("predecessor rotation_grace_until = %v, want %v (rotation instant + one rotation interval)", parked, want)
	}
	if parked.After(predecessor.AbsoluteExpiresAt) {
		t.Errorf("predecessor rotation_grace_until = %v is later than its absolute_expires_at %v -- rotation must never extend a session's life", parked, predecessor.AbsoluteExpiresAt)
	}

	// The whole point of the deferral still holds inside the bound: a
	// client whose successor cookie was lost keeps working, far past the
	// ordinary rotation grace interval.
	clk.Advance(rotationAge - time.Second)
	if _, _, err := sm.Authenticate(ctx, oldRaw); err != nil {
		t.Errorf("Authenticate(predecessor token) one second before the bound error = %v, want nil (the deferral must still cover a lost response)", err)
	}

	// Past the bound the predecessor is dead.
	clk.Advance(2 * time.Second)
	if _, _, err := sm.Authenticate(ctx, oldRaw); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("Authenticate(predecessor token) past the bounded deferral window error = %v, want ErrSessionInvalid (a superseded credential must stop working promptly)", err)
	}

	// And it stays dead: a fresh last_seen_at (the one thing that could
	// plausibly revive a row) does not resurrect it.
	clk.Advance(30 * 24 * time.Hour / 2) // 15 days, the span the old unbounded park allowed.
	setSessionRowLastSeenAt(ctx, t, inspector, predecessor.ID, clk.Now())
	if _, _, err := sm.Authenticate(ctx, oldRaw); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("Authenticate(predecessor token) 15 days after an undelivered rotation error = %v, want ErrSessionInvalid (the old behavior kept this token alive to absolute expiry)", err)
	}

	// The successor is unaffected by its predecessor's death: bounding the
	// deferral must not damage the row that is supposed to take over.
	setSessionRowLastSeenAt(ctx, t, inspector, successorOf(ctx, t, q, predecessor.ID), clk.Now())
	if _, _, err := sm.Authenticate(ctx, newRaw); err != nil {
		t.Errorf("Authenticate(successor token) after the predecessor's bounded window elapsed error = %v, want nil", err)
	}
}

// successorOf returns the id of the live successor row predecessorID was
// rotated into, failing the test if there is none.
func successorOf(ctx context.Context, t *testing.T, q *store.Queries, predecessorID uuid.UUID) uuid.UUID {
	t.Helper()
	succ, err := q.FindLiveSuccessorSession(ctx, &predecessorID)
	if err != nil {
		t.Fatalf("FindLiveSuccessorSession(%s) error: %v", predecessorID, err)
	}
	return succ.ID
}

// TestAuthenticate_RotationGracePark_CappedAtAbsoluteExpiry is the other
// half of the bound: min(now+rotationAge, absolute_expires_at), never the
// bare rotationAge offset. A session with less than one rotation interval
// of life left must park at its own ceiling -- rotation is not allowed to
// hand a superseded credential even one second past the absolute expiry
// fixed at login.
func TestAuthenticate_RotationGracePark_CappedAtAbsoluteExpiry(t *testing.T) {
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	clk := testutil.NewClockAtEpoch()
	sm := auth.NewSessionManagerForTest(q, clk.Now)
	ctx := context.Background()
	inspector := newRowInspectorPool(t)

	oldRaw, predecessor, err := sm.Issue(ctx, userID, "ua", "203.0.113.34")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	// Walk to 12h before the absolute ceiling -- half a rotation interval
	// of life left. last_seen_at is patched forward rather than walked
	// there by repeated Authenticate calls, for session_adversarial_test.go's
	// stated reason: re-entering Authenticate every 24h across a 90-day
	// span would rotate repeatedly and confound the one property here.
	const remaining = 12 * time.Hour
	clk.Advance(absoluteTimeout - remaining)
	setSessionRowLastSeenAt(ctx, t, inspector, predecessor.ID, clk.Now())
	rotatedAt := clk.Now()

	_, newRaw, err := sm.Authenticate(ctx, oldRaw)
	if err != nil {
		t.Fatalf("Authenticate() 12h before absolute expiry error = %v, want nil", err)
	}
	if newRaw == "" {
		t.Fatal("Authenticate() 12h before absolute expiry returned no rotatedToken, want rotation to have occurred")
	}

	parked := sessionRowRotationGraceUntil(ctx, t, inspector, predecessor.ID)
	if parked == nil {
		t.Fatal("predecessor rotation_grace_until is NULL after rotation, want the capped deadline")
	}
	if !parked.Equal(predecessor.AbsoluteExpiresAt) {
		t.Errorf("predecessor rotation_grace_until = %v, want %v (its own absolute_expires_at -- the nearer of the two bounds)", parked, predecessor.AbsoluteExpiresAt)
	}
	if uncapped := rotatedAt.Add(rotationAge); !parked.Before(uncapped) {
		t.Errorf("predecessor rotation_grace_until = %v, want strictly before the uncapped bound %v (this session has less than one rotation interval of life left)", parked, uncapped)
	}

	// Dead at the ceiling, not one rotation interval later.
	clk.Advance(remaining + time.Second)
	setSessionRowLastSeenAt(ctx, t, inspector, predecessor.ID, clk.Now())
	if _, _, err := sm.Authenticate(ctx, oldRaw); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("Authenticate(predecessor token) past the absolute ceiling error = %v, want ErrSessionInvalid", err)
	}
}

// ---- the device list across a rotation -------------------------------------

// liveSessionIDsForUser returns the ids GET /api/v1/sessions would list
// for userID at now. It calls ListLiveSessionsForUser with exactly the
// arguments handleSessionsList builds (sessions_handlers.go: UserID,
// IdleCutoff = now - idleTimeout, Now = now), so its result is the
// device list itself, not an approximation of it.
func liveSessionIDsForUser(ctx context.Context, t *testing.T, q *store.Queries, userID uuid.UUID, now time.Time) map[uuid.UUID]bool {
	t.Helper()
	rows, err := q.ListLiveSessionsForUser(ctx, store.ListLiveSessionsForUserParams{
		UserID:     userID,
		IdleCutoff: now.Add(-idleTimeout),
		Now:        now,
	})
	if err != nil {
		t.Fatalf("ListLiveSessionsForUser error: %v", err)
	}
	ids := make(map[uuid.UUID]bool, len(rows))
	for _, row := range rows {
		ids[row.ID] = true
	}
	return ids
}

// TestSessionsDeviceList_AcrossRotation proves the predecessor remains listed
// while its grace interval is open, then disappears. One device can therefore
// produce two entries during the bounded overlap.
func TestSessionsDeviceList_AcrossRotation(t *testing.T) {
	t.Run("successor first used", func(t *testing.T) {
		q := newTestQueries(t)
		userID := createTestUser(t, q)
		clk := testutil.NewClockAtEpoch()
		sm := auth.NewSessionManagerForTest(q, clk.Now)
		ctx := context.Background()

		oldRaw, predecessor, err := sm.Issue(ctx, userID, "ua", "203.0.113.35")
		if err != nil {
			t.Fatalf("Issue() error = %v", err)
		}

		clk.Advance(rotationAge + time.Hour)
		successor, newRaw, err := sm.Authenticate(ctx, oldRaw)
		if err != nil {
			t.Fatalf("Authenticate() at 25h error = %v", err)
		}

		// Immediately after the rotation: BOTH rows are live and listed --
		// two entries for one device.
		listed := liveSessionIDsForUser(ctx, t, q, userID, clk.Now())
		if !listed[predecessor.ID] {
			t.Error("predecessor absent from the device list immediately after rotation, want present (its grace window is still open, so it is still LIVE and still authenticates)")
		}
		if !listed[successor.ID] {
			t.Error("successor absent from the device list immediately after rotation, want present")
		}
		if len(listed) != 2 {
			t.Errorf("device list size immediately after rotation = %d, want 2 (one device, two live rows -- the rotation window's known cost)", len(listed))
		}

		// The successor's first use starts the real 60s countdown; the
		// predecessor is still listed inside it.
		if _, _, err := sm.Authenticate(ctx, newRaw); err != nil {
			t.Fatalf("Authenticate(successor token) first use error = %v", err)
		}
		listed = liveSessionIDsForUser(ctx, t, q, userID, clk.Now())
		if !listed[predecessor.ID] || !listed[successor.ID] || len(listed) != 2 {
			t.Errorf("device list at the successor's first use = %v, want both rows (the started grace window has not elapsed yet)", listed)
		}

		// Past the started window the duplicate is gone: one device, one
		// entry, and it is the successor.
		clk.Advance(rotationGrace + time.Second)
		listed = liveSessionIDsForUser(ctx, t, q, userID, clk.Now())
		if listed[predecessor.ID] {
			t.Error("predecessor still listed after its started grace window elapsed, want absent")
		}
		if !listed[successor.ID] || len(listed) != 1 {
			t.Errorf("device list after the grace window = %v, want exactly the successor %s", listed, successor.ID)
		}
	})

	t.Run("successor never used", func(t *testing.T) {
		q := newTestQueries(t)
		userID := createTestUser(t, q)
		clk := testutil.NewClockAtEpoch()
		sm := auth.NewSessionManagerForTest(q, clk.Now)
		ctx := context.Background()

		oldRaw, predecessor, err := sm.Issue(ctx, userID, "ua", "203.0.113.36")
		if err != nil {
			t.Fatalf("Issue() error = %v", err)
		}

		clk.Advance(rotationAge + time.Hour)
		successor, newRaw, err := sm.Authenticate(ctx, oldRaw)
		if err != nil {
			t.Fatalf("Authenticate() at 25h error = %v", err)
		}
		if newRaw == "" {
			t.Fatal("Authenticate() at 25h returned no rotatedToken, want rotation to have occurred")
		}
		// newRaw is never presented: the lost response again.

		listed := liveSessionIDsForUser(ctx, t, q, userID, clk.Now())
		if !listed[predecessor.ID] || !listed[successor.ID] || len(listed) != 2 {
			t.Errorf("device list immediately after an undelivered rotation = %v, want both rows", listed)
		}

		// Still both just inside the bound -- the client is still using
		// the predecessor, so it must still be a revocable device.
		clk.Advance(rotationAge - time.Second)
		listed = liveSessionIDsForUser(ctx, t, q, userID, clk.Now())
		if !listed[predecessor.ID] || !listed[successor.ID] || len(listed) != 2 {
			t.Errorf("device list one second before the bound = %v, want both rows", listed)
		}

		// Past the bound the duplicate disappears without another request.
		clk.Advance(2 * time.Second)
		listed = liveSessionIDsForUser(ctx, t, q, userID, clk.Now())
		if listed[predecessor.ID] {
			t.Error("predecessor still listed past the bounded deferral window, want absent (the duplicate device entry must not outlive the bound)")
		}
		if !listed[successor.ID] || len(listed) != 1 {
			t.Errorf("device list past the bounded deferral window = %v, want exactly the successor %s", listed, successor.ID)
		}
	})
}
