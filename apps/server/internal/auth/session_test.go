// Package auth_test exercises SessionManager against a live Postgres
// database (spec §9), reusing this package's existing live-DB harness
// (newTestQueries/createTestUser, both defined in transaction_test.go)
// instead of duplicating it -- the same convention
// transaction_adversarial_test.go already follows for TransactionStore.
package auth_test

import (
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
// task-7-brief.md's "Produces" section specifies. As with cookie.go's
// OAuthTxCookieName (see transaction_adversarial_test.go), this matters
// beyond naming taste: a browser only honors a __Host- prefixed cookie
// when it is Secure, Path=/, and carries no Domain attribute -- Task 9's
// cookie helpers depend on getting this exact string right.
func TestSessionCookieName_MatchesHostPrefixContract(t *testing.T) {
	const want = "__Host-session"
	if auth.SessionCookieName != want {
		t.Errorf("SessionCookieName = %q, want %q", auth.SessionCookieName, want)
	}
}

// TestIssueThenAuthenticate_ReturnsSameSession_NoRotation is task-7-brief.md
// Step 1's happy path: Issue a session, Authenticate with the raw token it
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

// TestIssue_CreatesNewRowEachTime is Issue's own doc comment's fixation
// defense, exercised directly: two Issue calls for the same user must
// produce two distinct raw tokens and two distinct session rows, not the
// same row reused or updated.
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
// user agent and IP onto the stored row -- the device-list feature (Task
// 9) reads these back verbatim.
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

// sessionTokenHash reproduces sessions.token_hash from a raw token,
// independently of session.go's own unexported hashSessionToken -- the
// same cross-check transaction_adversarial_test.go's handleHash performs
// for oauth_transactions.handle_hash.
func sessionTokenHash(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

// TestIssue_TokenHashedAtRest proves Issue's doc comment claim directly:
// looking a session up by the independently-computed SHA-256 of the raw
// token it returned finds that exact row, so token_hash is genuinely
// sha256(raw) -- not the raw token itself, not some other encoding.
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
// Authenticate's revoked branch directly (a basic, single-request version
// of the grace-independence property; the adversarial
// revoke-immediacy/no-grace-window edge case is Step 3's, authored
// independently).
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

// TestAuthenticate_AbsoluteExpiredSession_ReturnsSessionInvalid covers
// Authenticate's absolute-expiry branch specifically -- as a hard ceiling
// independent of activity, not merely as a corollary of idle expiry (which
// would also fire if last_seen_at were simply left stale for 90+ days, and
// would mask a broken/missing absolute check the same way a first version
// of this test did: see this test's own mutation-testing note below). It
// forces last_seen_at fresh via a direct SQL write -- bypassing
// SessionManager, so this isn't just "Authenticate happened to touch it"
// -- immediately before asserting, so idle expiry cannot be the reason
// Authenticate rejects the session.
//
// (Author's note: an earlier version of this test advanced the clock past
// absoluteTimeout without ever refreshing last_seen_at, so idle expiry
// -- also past its own, shorter threshold by then -- fired instead and
// masked a deliberately-broken absolute-expiry guard in a mutation check.
// This version was rewritten specifically to close that gap.)
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

// TestTouchReauthenticated_UpdatesReauthenticatedAt guards
// TouchReauthenticated's own write, independently of any caller (Task 10's
// purpose=reauth flow calls it after a real OAuth round trip).
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

// ---- rotation (smoke coverage only -- the exactly-one-successor-under-
// concurrency, grace-window-edge, and absolute-expiry-anchoring properties
// are the independently authored adversarial suite's job, not this file's;
// this is just enough to prove the algorithm isn't obviously broken before
// that suite runs against it) ------------------------------------------------

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
