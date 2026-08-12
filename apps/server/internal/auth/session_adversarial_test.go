// These adversarial tests prove AC-AUTH-004: one concurrent rotation winner,
// a bounded predecessor grace interval, inherited absolute expiry and recent
// reauthentication, and indistinguishable invalid-session outcomes. Row-level
// queries verify the compare-and-swap and lineage state that black-box return
// values alone cannot prove. See docs/adr/0015-session-rotation-delivery.md.
package auth_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// ---- mirrored constants -------------------------------------------------
//
// These mirror auth's unexported lifecycle constants. Redeclared here, like
// transaction_adversarial_test.go's txTTL mirrors oauthTxTTL, because this
// file is black-box (package auth_test) and cannot reference an
// unexported constant of package auth directly.

const (
	idleTimeout      = 30 * 24 * time.Hour
	absoluteTimeout  = 90 * 24 * time.Hour
	rotationAge      = 24 * time.Hour
	rotationGrace    = 60 * time.Second
	lastSeenThrottle = time.Hour
	reauthWindow     = 15 * time.Minute
)

// ---- harness --------------------------------------------------------------

// newTestSessionManager builds a manager with an injected clock.
func newTestSessionManager(t *testing.T, now func() time.Time) *auth.SessionManager {
	t.Helper()
	return newTestSessionManagerWithQueries(t, newTestQueries(t), now)
}

// newTestSessionManagerWithQueries shares q with row-level assertions.
func newTestSessionManagerWithQueries(t *testing.T, q *store.Queries, now func() time.Time) *auth.SessionManager {
	t.Helper()
	return auth.NewSessionManagerForTest(q, now)
}

// ---- row-state assertion helpers ------------------------------------------
//
// The tests query rows directly when the typed store surface does not expose the
// state needed to prove concurrency or expiry behavior.

// sessionRowCountForUser returns how many sessions rows exist for userID
// -- the concurrent-rotation test's core row-level assertion (want
// exactly 2: predecessor + the one successor, never up to 20).
func sessionRowCountForUser(ctx context.Context, t *testing.T, db rowQuerier, userID uuid.UUID) int {
	t.Helper()
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id = $1`, userID).Scan(&count); err != nil {
		t.Fatalf("sessionRowCountForUser query error: %v", err)
	}
	return count
}

// sessionRowTokenHash reads token_hash for id, failing the test if id
// does not exist (a row this test expects to still be present has
// vanished).
func sessionRowTokenHash(ctx context.Context, t *testing.T, db rowQuerier, id uuid.UUID) []byte {
	t.Helper()
	var hash []byte
	if err := db.QueryRow(ctx, `SELECT token_hash FROM sessions WHERE id = $1`, id).Scan(&hash); err != nil {
		t.Fatalf("sessionRowTokenHash(%s) query error: %v", id, err)
	}
	return hash
}

// sessionRowLastSeenAt reads last_seen_at for id.
func sessionRowLastSeenAt(ctx context.Context, t *testing.T, db rowQuerier, id uuid.UUID) time.Time {
	t.Helper()
	var ts time.Time
	if err := db.QueryRow(ctx, `SELECT last_seen_at FROM sessions WHERE id = $1`, id).Scan(&ts); err != nil {
		t.Fatalf("sessionRowLastSeenAt(%s) query error: %v", id, err)
	}
	return ts
}

// sessionRowAbsoluteExpiresAt reads absolute_expires_at for id.
func sessionRowAbsoluteExpiresAt(ctx context.Context, t *testing.T, db rowQuerier, id uuid.UUID) time.Time {
	t.Helper()
	var ts time.Time
	if err := db.QueryRow(ctx, `SELECT absolute_expires_at FROM sessions WHERE id = $1`, id).Scan(&ts); err != nil {
		t.Fatalf("sessionRowAbsoluteExpiresAt(%s) query error: %v", id, err)
	}
	return ts
}

// setSessionRowLastSeenAt isolates absolute expiry from idle expiry without
// triggering authentication or rotation.
func setSessionRowLastSeenAt(ctx context.Context, t *testing.T, pool *store.Pool, id uuid.UUID, ts time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `UPDATE sessions SET last_seen_at = $2 WHERE id = $1`, id, ts); err != nil {
		t.Fatalf("setSessionRowLastSeenAt(%s) exec error: %v", id, err)
	}
}

// ---- concurrent rotation --------------------------------------------------

// TestAuthenticate_ConcurrentRotation_MintsExactlyOneSuccessor races 20 calls.
// Every call authenticates, but one compare-and-swap winner mints a successor.
func TestAuthenticate_ConcurrentRotation_MintsExactlyOneSuccessor(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	userID := createTestUser(t, newTestQueries(t))
	m := newTestSessionManager(t, clock.Now) // All racing calls share one manager and clock.
	ctx := context.Background()
	pool := newRowInspectorPool(t)

	raw, origSess, err := m.Issue(ctx, userID, "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	clock.Advance(25 * time.Hour) // past rotationAge

	const n = 20
	var wg sync.WaitGroup
	results := make([]struct {
		sess         store.Session
		rotatedToken string
		err          error
	}, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i].sess, results[i].rotatedToken, results[i].err = m.Authenticate(ctx, raw)
		}(i)
	}
	wg.Wait()

	for _, r := range results {
		if r.err != nil {
			t.Errorf("Authenticate() error = %v, want nil (old token still valid within grace)", r.err)
		}
	}

	gotSuccessors := 0
	var rotatedToken string
	var winnerSessID uuid.UUID
	for _, r := range results {
		if r.rotatedToken != "" {
			gotSuccessors++
			rotatedToken = r.rotatedToken
			winnerSessID = r.sess.ID
		}
	}
	if gotSuccessors != 1 {
		t.Fatalf("exactly one goroutine should have won rotation and returned a new token, got %d", gotSuccessors)
	}

	// A losing call may return the predecessor while it remains valid. The
	// durable constraints are that all calls authenticate, only one token is
	// minted, exactly two rows remain, and the winner returns the successor.
	if winnerSessID == origSess.ID {
		t.Error("the winning call's returned session id == predecessor id, want the new successor's id (rotation did not actually happen)")
	}

	// The row count proves the race created one successor, not up to 20.
	if got := sessionRowCountForUser(ctx, t, pool, userID); got != 2 {
		t.Errorf("sessions row count for user = %d, want exactly 2 (predecessor + exactly one successor)", got)
	}

	// The predecessor row must still exist (rotation never deletes it --
	// it just becomes unreachable after the grace window) ...
	sessionRowTokenHash(ctx, t, pool, origSess.ID)
	// ... and the successor row's token_hash must be exactly
	// sha256(rotatedToken): the token handed back to the caller for
	// Set-Cookie must be the one that actually authenticates the row the
	// winning call minted.
	gotHash := sessionRowTokenHash(ctx, t, pool, winnerSessID)
	wantHash := sessionTokenHash(rotatedToken)
	if string(gotHash) != string(wantHash) {
		t.Errorf("successor row token_hash = %x, want sha256(rotatedToken) = %x", gotHash, wantHash)
	}
}

// ---- expiry, activity, and revocation -------------------------------------

// TestAuthenticate_RejectsIdleExpired proves a session that
// hasn't been touched in 31 days (idleTimeout is 30d) must be rejected,
// with no intervening Authenticate calls to reset the idle clock.
func TestAuthenticate_RejectsIdleExpired(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	m := newTestSessionManagerWithQueries(t, q, clock.Now)
	ctx := context.Background()

	raw, _, err := m.Issue(ctx, userID, "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	clock.Advance(idleTimeout + 24*time.Hour) // 31d, no intervening requests

	if _, _, err := m.Authenticate(ctx, raw); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Fatalf("Authenticate() after idle timeout error = %v, want ErrSessionInvalid", err)
	}
}

// TestAuthenticate_RejectsAbsoluteExpired proves 91 days past
// created_at must reject even with a *recent* last_seen_at, proving
// absolute expiry is a hard ceiling independent of activity -- not just
// "idle expiry with a longer window." last_seen_at is patched directly
// (setSessionRowLastSeenAt) to isolate that specific claim: without the
// patch, 91 days of simulated elapsed time would also trip idle expiry
// (30d), and the test wouldn't be able to tell which check actually fired.
func TestAuthenticate_RejectsAbsoluteExpired(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	m := newTestSessionManagerWithQueries(t, q, clock.Now)
	ctx := context.Background()
	pool := newRowInspectorPool(t)

	raw, sess, err := m.Issue(ctx, userID, "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	clock.Advance(absoluteTimeout + 24*time.Hour) // 91d past created_at

	// Prove the rejection below can't be idle expiry: last_seen_at is
	// "now", well inside the 30d idle window.
	setSessionRowLastSeenAt(ctx, t, pool, sess.ID, clock.Now())

	if _, _, err := m.Authenticate(ctx, raw); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Fatalf("Authenticate() 91d past created_at (recent last_seen_at) error = %v, want ErrSessionInvalid", err)
	}
}

// TestAuthenticate_RotationDoesNotExtendAbsoluteExpiry rotates at 25 hours,
// then advances to just past the original
// absolute_expires_at -- the whole lineage (predecessor and successor
// alike) must die together, proving rotation never pushes the absolute
// ceiling out. This is the core "anchored to the original login" claim in
// the rotation algorithm's prose ("absolute expiry is anchored to the
// original login, rotation must never extend it").
func TestAuthenticate_RotationDoesNotExtendAbsoluteExpiry(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	m := newTestSessionManagerWithQueries(t, q, clock.Now)
	ctx := context.Background()
	pool := newRowInspectorPool(t)

	raw, origSess, err := m.Issue(ctx, userID, "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	clock.Advance(rotationAge + time.Hour) // 25h, past rotationAge

	succSess, rotatedToken, err := m.Authenticate(ctx, raw)
	if err != nil {
		t.Fatalf("Authenticate() at 25h (should rotate) error = %v", err)
	}
	if rotatedToken == "" {
		t.Fatal("Authenticate() at 25h returned no rotatedToken, want rotation to have occurred")
	}
	if !succSess.AbsoluteExpiresAt.Equal(origSess.AbsoluteExpiresAt) {
		t.Fatalf("successor AbsoluteExpiresAt = %v, want unchanged from predecessor %v (anchored to original login)", succSess.AbsoluteExpiresAt, origSess.AbsoluteExpiresAt)
	}
	// Cross-check against the row directly, not just the returned struct.
	if got := sessionRowAbsoluteExpiresAt(ctx, t, pool, succSess.ID); !got.Equal(origSess.AbsoluteExpiresAt) {
		t.Errorf("successor row absolute_expires_at = %v, want %v", got, origSess.AbsoluteExpiresAt)
	}

	// Advance to just past the (unchanged) absolute ceiling. Patch
	// last_seen_at on the successor to "now" first, for the same reason
	// as TestAuthenticate_RejectsAbsoluteExpired: isolate that the
	// rejection below is the absolute ceiling, not idle expiry (the
	// successor's real last_seen_at would otherwise be ~89 days stale by
	// this point).
	remaining := origSess.AbsoluteExpiresAt.Sub(clock.Now())
	clock.Advance(remaining + time.Hour)
	setSessionRowLastSeenAt(ctx, t, pool, succSess.ID, clock.Now())

	if _, _, err := m.Authenticate(ctx, rotatedToken); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("Authenticate(successor token) past the original absolute ceiling error = %v, want ErrSessionInvalid (lineage dies together)", err)
	}
}

// TestAuthenticate_OldTokenRejectedAfterGraceWindow proves that once
// rotationGrace has elapsed, the predecessor's raw token must stop
// authenticating -- the successor's token must still work (sanity:
// rejection isn't just "everything is broken").
//
// The grace interval starts on the successor's first use. An undelivered and
// therefore unused successor must not orphan the predecessor.
func TestAuthenticate_OldTokenRejectedAfterGraceWindow(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	m := newTestSessionManagerWithQueries(t, q, clock.Now)
	ctx := context.Background()

	raw, _, err := m.Issue(ctx, userID, "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	clock.Advance(rotationAge + time.Hour)

	_, rotatedToken, err := m.Authenticate(ctx, raw)
	if err != nil {
		t.Fatalf("Authenticate() at 25h (should rotate) error = %v", err)
	}
	if rotatedToken == "" {
		t.Fatal("Authenticate() at 25h returned no rotatedToken, want rotation to have occurred")
	}

	// First use of the successor starts the predecessor's grace countdown.
	if _, _, err := m.Authenticate(ctx, rotatedToken); err != nil {
		t.Fatalf("Authenticate(successor token) first use error = %v", err)
	}

	clock.Advance(rotationGrace + time.Second) // past grace

	if _, _, err := m.Authenticate(ctx, raw); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("Authenticate(old token) after grace window error = %v, want ErrSessionInvalid", err)
	}
	if _, _, err := m.Authenticate(ctx, rotatedToken); err != nil {
		t.Errorf("Authenticate(successor token) after grace window error = %v, want nil (successor unaffected by predecessor's grace expiry)", err)
	}
}

// TestAuthenticate_LastSeenAtThrottled proves two calls close
// together must not both write last_seen_at (throttle window is 1h). The
// first call is placed 2h after Issue specifically so it *does* trigger a
// write (otherwise "unchanged after the second call" would trivially hold
// even with no throttle logic at all, since neither call would ever have
// written anything) -- the second call, 1 minute after the first, must
// then observe no further write.
func TestAuthenticate_LastSeenAtThrottled(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	m := newTestSessionManagerWithQueries(t, q, clock.Now)
	ctx := context.Background()
	pool := newRowInspectorPool(t)

	raw, origSess, err := m.Issue(ctx, userID, "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	clock.Advance(2 * time.Hour) // past lastSeenThrottle (1h) -- forces a write
	sess1, _, err := m.Authenticate(ctx, raw)
	if err != nil {
		t.Fatalf("first Authenticate() error = %v", err)
	}
	if sess1.LastSeenAt.Equal(origSess.LastSeenAt) {
		t.Fatal("last_seen_at unchanged after 2h, want a write (throttle setup did not force one -- test would be vacuous)")
	}
	lastSeenAfterFirst := sessionRowLastSeenAt(ctx, t, pool, origSess.ID)

	clock.Advance(time.Minute) // well under the 1h throttle
	sess2, _, err := m.Authenticate(ctx, raw)
	if err != nil {
		t.Fatalf("second Authenticate() error = %v", err)
	}

	if !sess2.LastSeenAt.Equal(sess1.LastSeenAt) {
		t.Errorf("returned LastSeenAt after second call = %v, want unchanged %v (throttle window is 1h, only 1m elapsed)", sess2.LastSeenAt, sess1.LastSeenAt)
	}
	if got := sessionRowLastSeenAt(ctx, t, pool, origSess.ID); !got.Equal(lastSeenAfterFirst) {
		t.Errorf("row last_seen_at after second call = %v, want unchanged %v", got, lastSeenAfterFirst)
	}
}

// TestRevoke_IsImmediate_NoGraceWindow calls Revoke and then
// Authenticate immediately (no clock advance at all) must already reject.
// revoked_at and rotation_grace_until must remain independent, or a revoked session's
// token would keep working for up to rotationGrace after the user thought
// they'd logged out.
func TestRevoke_IsImmediate_NoGraceWindow(t *testing.T) {
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	m := newTestSessionManagerWithQueries(t, q, time.Now)
	ctx := context.Background()

	raw, sess, err := m.Issue(ctx, userID, "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	if err := m.Revoke(ctx, sess.ID); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}

	if _, _, err := m.Authenticate(ctx, raw); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("Authenticate() immediately after Revoke() error = %v, want ErrSessionInvalid", err)
	}
}

// ---- no-oracle, revoke-mid-grace, reauth boundary, and fixation ------------

// TestAuthenticate_NoOracleAcrossFailureModes requires both sentinel identity
// and exact error text to match across unknown, revoked, idle-expired,
// absolute-expired, and grace-expired tokens. Scenario-specific wrapping would
// otherwise expose an oracle while still passing errors.Is checks.
func TestAuthenticate_NoOracleAcrossFailureModes(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	ctx := context.Background()
	errs := make(map[string]error, 5)

	// unknown token: shaped like a real one (32 raw bytes, base64url, no
	// padding -- sessionTokenBytes matches oauth's randomTokenBytes) but
	// never issued. randomHandle (transaction_adversarial_test.go) is
	// reused rather than duplicated: identical encoding shape.
	{
		q := newTestQueries(t)
		m := newTestSessionManagerWithQueries(t, q, clock.Now)
		_, _, err := m.Authenticate(ctx, randomHandle(t))
		errs["unknown"] = err
	}

	// idle-expired
	{
		clk := testutil.NewClockAtEpoch()
		q := newTestQueries(t)
		userID := createTestUser(t, q)
		m := newTestSessionManagerWithQueries(t, q, clk.Now)
		raw, _, err := m.Issue(ctx, userID, "ua", "1.2.3.4")
		if err != nil {
			t.Fatalf("idle-expired setup: Issue() error: %v", err)
		}
		clk.Advance(idleTimeout + 24*time.Hour)
		_, _, err = m.Authenticate(ctx, raw)
		errs["idle-expired"] = err
	}

	// absolute-expired (recent last_seen_at, per TestAuthenticate_RejectsAbsoluteExpired)
	{
		clk := testutil.NewClockAtEpoch()
		q := newTestQueries(t)
		userID := createTestUser(t, q)
		m := newTestSessionManagerWithQueries(t, q, clk.Now)
		pool := newRowInspectorPool(t)
		raw, sess, err := m.Issue(ctx, userID, "ua", "1.2.3.4")
		if err != nil {
			t.Fatalf("absolute-expired setup: Issue() error: %v", err)
		}
		clk.Advance(absoluteTimeout + 24*time.Hour)
		setSessionRowLastSeenAt(ctx, t, pool, sess.ID, clk.Now())
		_, _, err = m.Authenticate(ctx, raw)
		errs["absolute-expired"] = err
	}

	// revoked
	{
		clk := testutil.NewClockAtEpoch()
		q := newTestQueries(t)
		userID := createTestUser(t, q)
		m := newTestSessionManagerWithQueries(t, q, clk.Now)
		raw, sess, err := m.Issue(ctx, userID, "ua", "1.2.3.4")
		if err != nil {
			t.Fatalf("revoked setup: Issue() error: %v", err)
		}
		if revokeErr := m.Revoke(ctx, sess.ID); revokeErr != nil {
			t.Fatalf("revoked setup: Revoke() error: %v", revokeErr)
		}
		_, _, err = m.Authenticate(ctx, raw)
		errs["revoked"] = err
	}

	// old token, past the rotation grace window
	{
		clk := testutil.NewClockAtEpoch()
		q := newTestQueries(t)
		userID := createTestUser(t, q)
		m := newTestSessionManagerWithQueries(t, q, clk.Now)
		raw, _, err := m.Issue(ctx, userID, "ua", "1.2.3.4")
		if err != nil {
			t.Fatalf("old-token-post-grace setup: Issue() error: %v", err)
		}
		clk.Advance(rotationAge + time.Hour)
		_, successorRaw, rotateErr := m.Authenticate(ctx, raw)
		if rotateErr != nil {
			t.Fatalf("old-token-post-grace setup: rotating Authenticate() error: %v", rotateErr)
		}
		// The predecessor's grace countdown starts at the successor's first use,
		// not at rotation, so the successor
		// must actually be used here or the predecessor would still be
		// alive below -- deliberately, since an unused successor means
		// its one-shot token delivery was lost.
		if _, _, useErr := m.Authenticate(ctx, successorRaw); useErr != nil {
			t.Fatalf("old-token-post-grace setup: successor first use error: %v", useErr)
		}
		clk.Advance(rotationGrace + time.Second)
		_, _, err = m.Authenticate(ctx, raw)
		errs["old-token-post-grace"] = err
	}

	for name, err := range errs {
		if !errors.Is(err, auth.ErrSessionInvalid) {
			t.Errorf("%s: error = %v, want ErrSessionInvalid", name, err)
		}
	}

	want := auth.ErrSessionInvalid.Error()
	for name, err := range errs {
		if err == nil {
			continue
		}
		if got := err.Error(); got != want {
			t.Errorf("%s: error text = %q, want %q (identical text across every failure mode -- no oracle)", name, got, want)
		}
	}
}

// TestAuthenticate_RevokedMidGrace_PredecessorCannotAuthenticate proves an
// explicit revoke overrides open grace without revoking the successor.
func TestAuthenticate_RevokedMidGrace_PredecessorCannotAuthenticate(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	m := newTestSessionManagerWithQueries(t, q, clock.Now)
	ctx := context.Background()

	raw, predSess, err := m.Issue(ctx, userID, "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	clock.Advance(rotationAge + time.Hour) // past rotationAge

	_, rotatedToken, err := m.Authenticate(ctx, raw)
	if err != nil {
		t.Fatalf("Authenticate() at 25h (should rotate) error = %v", err)
	}
	if rotatedToken == "" {
		t.Fatal("Authenticate() at 25h returned no rotatedToken, want rotation to have occurred")
	}

	// Still well inside the 60s grace window.
	clock.Advance(10 * time.Second)
	if err := m.Revoke(ctx, predSess.ID); err != nil {
		t.Fatalf("Revoke(predecessor) error = %v", err)
	}

	// Still inside grace by the clock alone (10s + a few more < 60s) --
	// only the explicit revoke should be what kills it.
	clock.Advance(5 * time.Second)
	if _, _, err := m.Authenticate(ctx, raw); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("Authenticate(predecessor token) after revoking the predecessor mid-grace error = %v, want ErrSessionInvalid", err)
	}

	// Orthogonality, the other direction: the successor is a different
	// row and must be untouched by revoking its predecessor.
	if _, _, err := m.Authenticate(ctx, rotatedToken); err != nil {
		t.Errorf("Authenticate(successor token) after revoking only the predecessor error = %v, want nil (revoke must not cascade to a sibling row)", err)
	}
}

// TestRequireRecentReauth_BoundaryAtExactly15Minutes pins the strict
// greater-than boundary without database state.
func TestRequireRecentReauth_BoundaryAtExactly15Minutes(t *testing.T) {
	base := testutil.Epoch

	tests := []struct {
		name    string
		elapsed time.Duration
		wantErr bool
	}{
		{"just under the window", reauthWindow - time.Nanosecond, false},
		{"exactly at the window (inclusive per strict >)", reauthWindow, false},
		{"one nanosecond past the window", reauthWindow + time.Nanosecond, true},
		{"well past the window", reauthWindow + time.Hour, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := store.Session{ReauthenticatedAt: base}
			now := base.Add(tt.elapsed)

			err := auth.RequireRecentReauth(sess, now)
			if tt.wantErr {
				if !errors.Is(err, auth.ErrReauthRequired) {
					t.Errorf("RequireRecentReauth() error = %v, want ErrReauthRequired", err)
				}
			} else if err != nil {
				t.Errorf("RequireRecentReauth() error = %v, want nil", err)
			}
		})
	}
}

// TestIssue_AlwaysNewRow_FixationProperty proves each login gets a distinct
// token and row, preserving fixation defense and multi-device sessions.
func TestIssue_AlwaysNewRow_FixationProperty(t *testing.T) {
	q := newTestQueries(t)
	userID := createTestUser(t, q)
	m := newTestSessionManagerWithQueries(t, q, time.Now)
	ctx := context.Background()
	pool := newRowInspectorPool(t)

	raw1, sess1, err := m.Issue(ctx, userID, "ua-1", "1.2.3.4")
	if err != nil {
		t.Fatalf("first Issue() error = %v", err)
	}
	raw2, sess2, err := m.Issue(ctx, userID, "ua-2", "5.6.7.8")
	if err != nil {
		t.Fatalf("second Issue() error = %v", err)
	}

	if raw1 == raw2 {
		t.Error("two Issue() calls returned the same raw token, want distinct tokens")
	}
	if sess1.ID == sess2.ID {
		t.Error("two Issue() calls returned the same session id, want distinct rows")
	}

	if got := sessionRowCountForUser(ctx, t, pool, userID); got != 2 {
		t.Errorf("sessions row count for user after two Issue() calls = %d, want 2", got)
	}
	// Both must independently authenticate -- confirms they're really two
	// live, distinct rows, not e.g. one row whose token_hash got
	// overwritten by the second Issue (which would make raw1 stop
	// working).
	if _, _, err := m.Authenticate(ctx, raw1); err != nil {
		t.Errorf("Authenticate(raw1) after second Issue() error = %v, want nil", err)
	}
	if _, _, err := m.Authenticate(ctx, raw2); err != nil {
		t.Errorf("Authenticate(raw2) error = %v, want nil", err)
	}
}
