package auth_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/store"
	"danny.vn/aboutme/apps/server/internal/testutil"
)

const (
	idleTimeout      = 30 * 24 * time.Hour
	absoluteTimeout  = 90 * 24 * time.Hour
	rotationAge      = 24 * time.Hour
	rotationGrace    = 60 * time.Second
	lastSeenThrottle = time.Hour
	reauthWindow     = 15 * time.Minute
)

func newTestSessionManager(t *testing.T, now func() time.Time) *auth.SessionManager {
	t.Helper()
	return newTestSessionManagerWithQueries(t, newTestQueries(t), now)
}

func newTestSessionManagerWithQueries(t *testing.T, q *store.Queries, now func() time.Time) *auth.SessionManager {
	t.Helper()
	return auth.NewSessionManagerForTest(q, now)
}

func sessionRowCountForUser(ctx context.Context, t *testing.T, db rowQuerier, userID uuid.UUID) int {
	t.Helper()
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id = $1`, userID).Scan(&count); err != nil {
		t.Fatalf("sessionRowCountForUser query error: %v", err)
	}
	return count
}

func sessionRowTokenHash(ctx context.Context, t *testing.T, db rowQuerier, id uuid.UUID) []byte {
	t.Helper()
	var hash []byte
	if err := db.QueryRow(ctx, `SELECT token_hash FROM sessions WHERE id = $1`, id).Scan(&hash); err != nil {
		t.Fatalf("sessionRowTokenHash(%s) query error: %v", id, err)
	}
	return hash
}

func sessionRowLastSeenAt(ctx context.Context, t *testing.T, db rowQuerier, id uuid.UUID) time.Time {
	t.Helper()
	var ts time.Time
	if err := db.QueryRow(ctx, `SELECT last_seen_at FROM sessions WHERE id = $1`, id).Scan(&ts); err != nil {
		t.Fatalf("sessionRowLastSeenAt(%s) query error: %v", id, err)
	}
	return ts
}

func sessionRowAbsoluteExpiresAt(ctx context.Context, t *testing.T, db rowQuerier, id uuid.UUID) time.Time {
	t.Helper()
	var ts time.Time
	if err := db.QueryRow(ctx, `SELECT absolute_expires_at FROM sessions WHERE id = $1`, id).Scan(&ts); err != nil {
		t.Fatalf("sessionRowAbsoluteExpiresAt(%s) query error: %v", id, err)
	}
	return ts
}

func setSessionRowLastSeenAt(ctx context.Context, t *testing.T, pool *store.Pool, id uuid.UUID, ts time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `UPDATE sessions SET last_seen_at = $2 WHERE id = $1`, id, ts); err != nil {
		t.Fatalf("setSessionRowLastSeenAt(%s) exec error: %v", id, err)
	}
}

func TestAuthenticate_ConcurrentRotation_MintsExactlyOneSuccessor(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	userID := createTestUser(t, newTestQueries(t))
	m := newTestSessionManager(t, clock.Now)
	ctx := context.Background()
	pool := newRowInspectorPool(t)

	raw, origSess, err := m.Issue(ctx, userID, "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	clock.Advance(25 * time.Hour)

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

	if winnerSessID == origSess.ID {
		t.Error("the winning call's returned session id == predecessor id, want the new successor's id (rotation did not actually happen)")
	}

	if got := sessionRowCountForUser(ctx, t, pool, userID); got != 2 {
		t.Errorf("sessions row count for user = %d, want exactly 2 (predecessor + exactly one successor)", got)
	}

	sessionRowTokenHash(ctx, t, pool, origSess.ID)
	gotHash := sessionRowTokenHash(ctx, t, pool, winnerSessID)
	wantHash := sessionTokenHash(rotatedToken)
	if string(gotHash) != string(wantHash) {
		t.Errorf("successor row token_hash = %x, want sha256(rotatedToken) = %x", gotHash, wantHash)
	}
}

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

	clock.Advance(idleTimeout + 24*time.Hour)

	if _, _, err := m.Authenticate(ctx, raw); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Fatalf("Authenticate() after idle timeout error = %v, want ErrSessionInvalid", err)
	}
}

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

	clock.Advance(absoluteTimeout + 24*time.Hour)

	setSessionRowLastSeenAt(ctx, t, pool, sess.ID, clock.Now())

	if _, _, err := m.Authenticate(ctx, raw); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Fatalf("Authenticate() 91d past created_at (recent last_seen_at) error = %v, want ErrSessionInvalid", err)
	}
}

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

	clock.Advance(rotationAge + time.Hour)

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
	if got := sessionRowAbsoluteExpiresAt(ctx, t, pool, succSess.ID); !got.Equal(origSess.AbsoluteExpiresAt) {
		t.Errorf("successor row absolute_expires_at = %v, want %v", got, origSess.AbsoluteExpiresAt)
	}

	remaining := origSess.AbsoluteExpiresAt.Sub(clock.Now())
	clock.Advance(remaining + time.Hour)
	setSessionRowLastSeenAt(ctx, t, pool, succSess.ID, clock.Now())

	if _, _, err := m.Authenticate(ctx, rotatedToken); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("Authenticate(successor token) past the original absolute ceiling error = %v, want ErrSessionInvalid (lineage dies together)", err)
	}
}

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

	if _, _, err := m.Authenticate(ctx, rotatedToken); err != nil {
		t.Fatalf("Authenticate(successor token) first use error = %v", err)
	}

	clock.Advance(rotationGrace + time.Second)

	if _, _, err := m.Authenticate(ctx, raw); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("Authenticate(old token) after grace window error = %v, want ErrSessionInvalid", err)
	}
	if _, _, err := m.Authenticate(ctx, rotatedToken); err != nil {
		t.Errorf("Authenticate(successor token) after grace window error = %v, want nil (successor unaffected by predecessor's grace expiry)", err)
	}
}

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

	clock.Advance(2 * time.Hour)
	sess1, _, err := m.Authenticate(ctx, raw)
	if err != nil {
		t.Fatalf("first Authenticate() error = %v", err)
	}
	if sess1.LastSeenAt.Equal(origSess.LastSeenAt) {
		t.Fatal("last_seen_at unchanged after 2h, want a write (throttle setup did not force one -- test would be vacuous)")
	}
	lastSeenAfterFirst := sessionRowLastSeenAt(ctx, t, pool, origSess.ID)

	clock.Advance(time.Minute)
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

func TestAuthenticate_NoOracleAcrossFailureModes(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	ctx := context.Background()
	errs := make(map[string]error, 5)

	{
		q := newTestQueries(t)
		m := newTestSessionManagerWithQueries(t, q, clock.Now)
		_, _, err := m.Authenticate(ctx, randomHandle(t))
		errs["unknown"] = err
	}

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
	clock.Advance(rotationAge + time.Hour)

	_, rotatedToken, err := m.Authenticate(ctx, raw)
	if err != nil {
		t.Fatalf("Authenticate() at 25h (should rotate) error = %v", err)
	}
	if rotatedToken == "" {
		t.Fatal("Authenticate() at 25h returned no rotatedToken, want rotation to have occurred")
	}

	clock.Advance(10 * time.Second)
	if err := m.Revoke(ctx, predSess.ID); err != nil {
		t.Fatalf("Revoke(predecessor) error = %v", err)
	}

	clock.Advance(5 * time.Second)
	if _, _, err := m.Authenticate(ctx, raw); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Errorf("Authenticate(predecessor token) after revoking the predecessor mid-grace error = %v, want ErrSessionInvalid", err)
	}

	if _, _, err := m.Authenticate(ctx, rotatedToken); err != nil {
		t.Errorf("Authenticate(successor token) after revoking only the predecessor error = %v, want nil (revoke must not cascade to a sibling row)", err)
	}
}

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
	if _, _, err := m.Authenticate(ctx, raw1); err != nil {
		t.Errorf("Authenticate(raw1) after second Issue() error = %v, want nil", err)
	}
	if _, _, err := m.Authenticate(ctx, raw2); err != nil {
		t.Errorf("Authenticate(raw2) error = %v, want nil", err)
	}
}

func liveSessionCountForUser(ctx context.Context, t *testing.T, pool *store.Pool, userID uuid.UUID) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL", userID).Scan(&n); err != nil {
		t.Fatalf("count live sessions: %v", err)
	}
	return n
}

type authResult struct {
	rotated string
	err     error
}

func TestAuthenticate_StaleEpochCannotRotate(t *testing.T) {
	t.Run("advanced before the request", func(t *testing.T) {
		ctx := context.Background()
		pool := newTestPool(t)
		q := store.New(pool)
		userID := createTestUser(t, q)
		clock := testutil.NewClockAtEpoch()
		sm := auth.NewSessionManagerWithPoolForTest(pool, clock.Now)
		raw, predecessor, err := sm.Issue(ctx, userID, "ua", "1.2.3.4")
		if err != nil {
			t.Fatalf("Issue() error = %v", err)
		}
		clock.Advance(rotationAge + time.Hour)
		if _, execErr := pool.Exec(ctx, "UPDATE users SET auth_epoch = auth_epoch + 1 WHERE id = $1", userID); execErr != nil {
			t.Fatalf("advance auth epoch: %v", execErr)
		}
		if _, rotated, authErr := sm.Authenticate(ctx, raw); !errors.Is(authErr, auth.ErrSessionInvalid) || rotated != "" {
			t.Fatalf("Authenticate(stale epoch) = (%q, %v), want no token and ErrSessionInvalid", rotated, authErr)
		}
		if got := sessionRowCountForUser(ctx, t, pool, userID); got != 1 {
			t.Fatalf("session rows = %d, want 1 (no successor)", got)
		}
		stored, err := q.GetSessionByID(ctx, predecessor.ID)
		if err != nil {
			t.Fatalf("read predecessor: %v", err)
		}
		if stored.RotationGraceUntil != nil {
			t.Fatalf("stale predecessor was admitted to rotation, grace until %v", stored.RotationGraceUntil)
		}
	})

	t.Run("advanced between admission and insert", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		pool := newTestPool(t)
		q := store.New(pool)
		userID := createTestUser(t, q)
		clock := testutil.NewClockAtEpoch()
		sm := auth.NewSessionManagerWithPoolForTest(pool, clock.Now)
		raw, _, err := sm.Issue(ctx, userID, "ua", "1.2.3.4")
		if err != nil {
			t.Fatalf("Issue() error = %v", err)
		}
		clock.Advance(rotationAge + time.Hour)
		var probeErr error
		auth.SetSessionRotationProbeForTest(sm, func() {
			_, probeErr = pool.Exec(ctx, "UPDATE users SET auth_epoch = auth_epoch + 1 WHERE id = $1", userID)
		})
		_, rotated, authErr := sm.Authenticate(ctx, raw)
		if probeErr != nil {
			t.Fatalf("advance auth epoch in probe: %v", probeErr)
		}
		if !errors.Is(authErr, auth.ErrSessionInvalid) || rotated != "" {
			t.Fatalf("Authenticate(epoch advanced after admission) = (%q, %v), want no token and ErrSessionInvalid", rotated, authErr)
		}
		if got := sessionRowCountForUser(ctx, t, pool, userID); got != 1 {
			t.Fatalf("session rows = %d, want 1 (no successor past the epoch fence)", got)
		}
	})
}

func TestReplaceAfterEpochChange_RacesRotation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool := newTestPool(t)
	q := store.New(pool)
	userID := createTestUser(t, q)
	clock := testutil.NewClockAtEpoch()
	sm := auth.NewSessionManagerWithPoolForTest(pool, clock.Now)
	raw, predecessor, err := sm.Issue(ctx, userID, "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	clock.Advance(rotationAge + time.Hour)

	admitted := make(chan struct{})
	release := make(chan struct{})
	var admitOnce sync.Once
	auth.SetSessionRotationProbeForTest(sm, func() {
		admitOnce.Do(func() { close(admitted) })
		<-release
	})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	done := make(chan authResult, 1)
	go func() {
		_, rotated, authErr := sm.Authenticate(ctx, raw)
		done <- authResult{rotated: rotated, err: authErr}
	}()
	select {
	case <-admitted:
	case <-ctx.Done():
		t.Fatal("rotation admission did not happen before the deadline")
	}

	replacement, replaceErr := advanceEpochAndReplace(ctx, pool, q, sm, userID, predecessor.ID, nil)
	close(release)
	var result authResult
	select {
	case result = <-done:
	case <-ctx.Done():
		t.Fatal("Authenticate() did not finish before the deadline")
	}
	if replaceErr != nil {
		t.Fatalf("ReplaceAfterEpochChangeTx() during rotation error = %v", replaceErr)
	}
	if !errors.Is(result.err, auth.ErrSessionInvalid) || result.rotated != "" {
		t.Fatalf("Authenticate() racing replacement = (%q, %v), want no token and ErrSessionInvalid", result.rotated, result.err)
	}
	if got := sessionRowCountForUser(ctx, t, pool, userID); got != 2 {
		t.Fatalf("session rows = %d, want 2 (predecessor and replacement, no successor)", got)
	}
	if got := liveSessionCountForUser(ctx, t, pool, userID); got != 1 {
		t.Fatalf("live session rows = %d, want only the replacement", got)
	}
	if _, _, authErr := sm.Authenticate(ctx, replacement.RawToken); authErr != nil {
		t.Fatalf("Authenticate(replacement) error = %v", authErr)
	}
}

func TestReplaceAfterEpochChange_RacesIssue(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool := newTestPool(t)
	q := store.New(pool)
	userID := createTestUser(t, q)
	sm := auth.NewSessionManagerWithPoolForTest(pool, time.Now)
	oldRaw, current, err := sm.Issue(ctx, userID, "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin replacement: %v", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(context.Background()); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Errorf("rollback replacement: %v", rollbackErr)
		}
	}()
	qtx := q.WithTx(tx)
	if _, err = qtx.GetUserForUpdate(ctx, userID); err != nil {
		t.Fatalf("lock user: %v", err)
	}
	advanced, err := qtx.AdvanceUserAuthEpoch(ctx, userID)
	if err != nil {
		t.Fatalf("advance epoch: %v", err)
	}
	replacement, err := sm.ReplaceAfterEpochChangeTx(ctx, qtx, userID, current.ID, nil)
	if err != nil {
		t.Fatalf("ReplaceAfterEpochChangeTx() error = %v", err)
	}

	type issueResult struct {
		sess store.Session
		err  error
	}
	issued := make(chan issueResult, 1)
	go func() {
		_, sess, issueErr := sm.Issue(ctx, userID, "ua", "5.6.7.8")
		issued <- issueResult{sess: sess, err: issueErr}
	}()
	select {
	case r := <-issued:
		t.Fatalf("Issue() finished while the epoch change held the user lock: %+v", r)
	case <-time.After(200 * time.Millisecond):
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatalf("commit replacement: %v", err)
	}
	var r issueResult
	select {
	case r = <-issued:
	case <-ctx.Done():
		t.Fatal("Issue() did not finish before the deadline")
	}
	if r.err != nil {
		t.Fatalf("Issue() after epoch change error = %v", r.err)
	}
	if r.sess.AuthEpoch != advanced.AuthEpoch {
		t.Fatalf("queued Issue() epoch = %d, want new epoch %d", r.sess.AuthEpoch, advanced.AuthEpoch)
	}
	if _, _, authErr := sm.Authenticate(ctx, oldRaw); !errors.Is(authErr, auth.ErrSessionInvalid) {
		t.Fatalf("Authenticate(prior session) error = %v, want ErrSessionInvalid", authErr)
	}
	if _, _, authErr := sm.Authenticate(ctx, replacement.RawToken); authErr != nil {
		t.Fatalf("Authenticate(replacement) error = %v", authErr)
	}
}
