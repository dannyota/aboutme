package auth_test

// These tests pin the IssueTx primitive's exact session fields and rollback
// behavior, and the user-lock fence that Issue and rotation serialize on.

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// TestIssueTx_CreatesExactFreshSessionFields drives IssueTx through a locked
// user row and pins every fresh-login field, including rotated_from = NULL.
func TestIssueTx_CreatesExactFreshSessionFields(t *testing.T) {
	pool := newTestPool(t)
	q := store.New(pool)
	userID := createTestUser(t, q)
	clk := testutil.NewClockAtEpoch()
	sm := auth.NewSessionManagerWithPoolForTest(pool, clk.Now)
	ctx := context.Background()

	var issued auth.SessionIssue
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		qtx := q.WithTx(tx)
		user, lockErr := qtx.GetUserForUpdate(ctx, userID)
		if lockErr != nil {
			return lockErr
		}
		var issueErr error
		issued, issueErr = sm.IssueTx(ctx, qtx, user, "ua", "203.0.113.7")
		return issueErr
	})
	if err != nil {
		t.Fatalf("IssueTx() error = %v", err)
	}

	if issued.RawToken == "" {
		t.Fatal("IssueTx() raw token is empty")
	}
	if issued.Session.UserID != userID {
		t.Errorf("session.UserID = %v, want %v", issued.Session.UserID, userID)
	}
	if issued.Session.RotatedFrom != nil {
		t.Errorf("fresh session RotatedFrom = %v, want nil (a login is never a rotation successor)", issued.Session.RotatedFrom)
	}
	if !issued.Session.CreatedAt.Equal(clk.Now()) {
		t.Errorf("session.CreatedAt = %v, want %v (the manager's clock)", issued.Session.CreatedAt, clk.Now())
	}
	if !issued.Session.ReauthenticatedAt.Equal(clk.Now()) {
		t.Errorf("session.ReauthenticatedAt = %v, want %v (a fresh login reauthenticates now)", issued.Session.ReauthenticatedAt, clk.Now())
	}
	wantAbs := clk.Now().Add(90 * 24 * time.Hour)
	if !issued.Session.AbsoluteExpiresAt.Equal(wantAbs) {
		t.Errorf("session.AbsoluteExpiresAt = %v, want %v", issued.Session.AbsoluteExpiresAt, wantAbs)
	}
	if issued.Session.UA == nil || *issued.Session.UA != "ua" {
		t.Errorf("session.UA = %v, want %q", issued.Session.UA, "ua")
	}
	if issued.Session.IP == nil || issued.Session.IP.String() != "203.0.113.7" {
		t.Errorf("session.IP = %v, want 203.0.113.7", issued.Session.IP)
	}
	if len(issued.Session.CSRFSecret) != 32 {
		t.Errorf("session.CSRFSecret length = %d, want 32", len(issued.Session.CSRFSecret))
	}

	row, err := q.GetSessionByTokenHash(ctx, sessionTokenHash(issued.RawToken))
	if err != nil {
		t.Fatalf("GetSessionByTokenHash(sha256(raw)) error = %v, want nil", err)
	}
	if row.ID != issued.Session.ID {
		t.Errorf("stored row ID = %v, want %v", row.ID, issued.Session.ID)
	}
	if !bytes.Equal(row.TokenHash, sessionTokenHash(issued.RawToken)) {
		t.Error("stored token_hash != sha256(raw token), want hashed-at-rest storage")
	}
}

// TestIssueTx_NonexistentUser_InsertFailsAndRollsBack proves IssueTx does not
// mint a session for a user row that does not exist (the foreign key rejects
// it), and the transaction leaves no partial session row.
func TestIssueTx_NonexistentUser_InsertFailsAndRollsBack(t *testing.T) {
	pool := newTestPool(t)
	q := store.New(pool)
	sm := auth.NewSessionManagerWithPoolForTest(pool, time.Now)
	ctx := context.Background()

	nonexistent := uuid.New()
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		qtx := q.WithTx(tx)
		_, issueErr := sm.IssueTx(ctx, qtx, store.User{ID: nonexistent}, "ua", "1.2.3.4")
		return issueErr
	})
	if err == nil {
		t.Fatal("IssueTx(nonexistent user) error = nil, want a foreign-key violation")
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id = $1`, nonexistent).Scan(&count); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if count != 0 {
		t.Errorf("sessions rows for a nonexistent user = %d, want 0", count)
	}
}

// TestIssueTx_SessionRowRollsBackWhenTransactionFails proves a later failure in
// the same transaction rolls back an already-inserted session row.
func TestIssueTx_SessionRowRollsBackWhenTransactionFails(t *testing.T) {
	pool := newTestPool(t)
	q := store.New(pool)
	userID := createTestUser(t, q)
	sm := auth.NewSessionManagerWithPoolForTest(pool, time.Now)
	ctx := context.Background()

	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		qtx := q.WithTx(tx)
		user, lockErr := qtx.GetUserForUpdate(ctx, userID)
		if lockErr != nil {
			return lockErr
		}
		if _, issueErr := sm.IssueTx(ctx, qtx, user, "ua", "1.2.3.4"); issueErr != nil {
			return issueErr
		}
		return errors.New("deliberate post-issue rollback")
	})
	if err == nil {
		t.Fatal("transaction error = nil, want the deliberate rollback error")
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id = $1`, userID).Scan(&count); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if count != 0 {
		t.Errorf("sessions rows after rollback = %d, want 0 (the insert must roll back with the transaction)", count)
	}
}

// TestIssue_WithPool_LocksUser proves the Issue compatibility wrapper acquires
// the user row lock before inserting, via the deterministic lock probe.
func TestIssue_WithPool_LocksUser(t *testing.T) {
	pool := newTestPool(t)
	q := store.New(pool)
	userID := createTestUser(t, q)
	sm := auth.NewSessionManagerWithPoolForTest(pool, time.Now)
	ctx := context.Background()

	locked := make(chan struct{})
	release := make(chan struct{})
	auth.SetSessionLockProbeForTest(sm, func() {
		close(locked)
		<-release
	})

	done := make(chan error, 1)
	go func() {
		_, _, err := sm.Issue(ctx, userID, "ua", "1.2.3.4")
		done <- err
	}()

	select {
	case <-locked:
	case <-done:
		t.Fatal("Issue() returned before the lock probe fired; the wrapper did not acquire the user lock first")
	}

	// While Issue holds the user lock, a reset cannot acquire it.
	now := time.Now()
	resetErr := make(chan error, 1)
	go func() {
		resetErr <- pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			qtx := q.WithTx(tx)
			if _, err := qtx.GetUserForUpdate(ctx, userID); err != nil {
				return err
			}
			_, err := qtx.RevokeAllSessions(ctx, store.RevokeAllSessionsParams{UserID: userID, RevokedAt: &now})
			return err
		})
	}()

	select {
	case err := <-resetErr:
		t.Fatalf("reset acquired the user lock while Issue held it (err=%v); the fence is broken", err)
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if err := <-resetErr; err != nil {
		t.Fatalf("reset error = %v", err)
	}
}
