package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/store"
	"danny.vn/aboutme/apps/server/internal/testutil"
)

func resetAllSessions(ctx context.Context, t *testing.T, pool *store.Pool, q *store.Queries, userID uuid.UUID) error {
	t.Helper()
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		qtx := q.WithTx(tx)
		if _, err := qtx.GetUserForUpdate(ctx, userID); err != nil {
			return err
		}
		now := time.Now()
		_, err := qtx.RevokeAllSessions(ctx, store.RevokeAllSessionsParams{UserID: userID, RevokedAt: &now})
		return err
	})
}

func TestProviderIssueResetRace(t *testing.T) {
	t.Run("issue first, reset revokes", func(t *testing.T) {
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

		issueDone := make(chan struct {
			raw string
			err error
		}, 1)
		go func() {
			raw, _, err := sm.Issue(ctx, userID, "ua", "1.2.3.4")
			issueDone <- struct {
				raw string
				err error
			}{raw, err}
		}()

		<-locked

		resetDone := make(chan error, 1)
		go func() {
			resetDone <- resetAllSessions(ctx, t, pool, q, userID)
		}()

		select {
		case err := <-resetDone:
			t.Fatalf("reset acquired the user lock while issue held it (err=%v)", err)
		case <-time.After(200 * time.Millisecond):
		}

		close(release)
		issued := <-issueDone
		if issued.err != nil {
			t.Fatalf("Issue() error = %v", issued.err)
		}
		if err := <-resetDone; err != nil {
			t.Fatalf("reset error = %v", err)
		}

		if _, _, err := sm.Authenticate(ctx, issued.raw); !errors.Is(err, auth.ErrSessionInvalid) {
			t.Errorf("issued session still authenticates after the reset, want ErrSessionInvalid (reset must revoke it)")
		}
	})

	t.Run("reset first, issue after", func(t *testing.T) {
		pool := newTestPool(t)
		q := store.New(pool)
		userID := createTestUser(t, q)
		sm := auth.NewSessionManagerWithPoolForTest(pool, time.Now)
		ctx := context.Background()

		preRaw, _, err := sm.Issue(ctx, userID, "ua", "1.2.3.4")
		if err != nil {
			t.Fatalf("pre Issue() error = %v", err)
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin reset tx: %v", err)
		}
		defer func() {
			if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
				t.Errorf("Rollback() error: %v", err)
			}
		}()
		qtx := q.WithTx(tx)
		if _, err := qtx.GetUserForUpdate(ctx, userID); err != nil {
			t.Fatalf("reset lock user: %v", err)
		}
		now := time.Now()
		if _, err := qtx.RevokeAllSessions(ctx, store.RevokeAllSessionsParams{UserID: userID, RevokedAt: &now}); err != nil {
			t.Fatalf("reset revoke all: %v", err)
		}

		issueDone := make(chan error, 1)
		go func() {
			_, _, err := sm.Issue(ctx, userID, "ua", "1.2.3.5")
			issueDone <- err
		}()

		select {
		case err := <-issueDone:
			t.Fatalf("issue acquired the user lock while reset held it (err=%v)", err)
		case <-time.After(200 * time.Millisecond):
		}

		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit reset: %v", err)
		}

		if err := <-issueDone; err != nil {
			t.Fatalf("post-reset Issue() error = %v", err)
		}

		if _, _, err := sm.Authenticate(ctx, preRaw); !errors.Is(err, auth.ErrSessionInvalid) {
			t.Errorf("pre-reset session still authenticates, want ErrSessionInvalid (reset revoked it)")
		}
	})
}

func TestSessionRotation_ResetFence(t *testing.T) {
	t.Run("rotation insert before reset is revoked", func(t *testing.T) {
		pool := newTestPool(t)
		q := store.New(pool)
		userID := createTestUser(t, q)
		clk := testutil.NewClockAtEpoch()
		sm := auth.NewSessionManagerWithPoolForTest(pool, clk.Now)
		ctx := context.Background()

		oldRaw, predecessor, err := sm.Issue(ctx, userID, "ua", "1.2.3.4")
		if err != nil {
			t.Fatalf("Issue() error = %v", err)
		}
		clk.Advance(25 * time.Hour)

		successor, newRaw, err := sm.Authenticate(ctx, oldRaw)
		if err != nil {
			t.Fatalf("Authenticate() at 25h error = %v", err)
		}
		if newRaw == "" || successor.ID == predecessor.ID {
			t.Fatalf("rotation did not mint a successor (raw=%q, id=%v)", newRaw, successor.ID)
		}

		if err := resetAllSessions(ctx, t, pool, q, userID); err != nil {
			t.Fatalf("reset error = %v", err)
		}

		if _, _, err := sm.Authenticate(ctx, oldRaw); !errors.Is(err, auth.ErrSessionInvalid) {
			t.Errorf("predecessor still authenticates after reset, want ErrSessionInvalid")
		}
		if _, _, err := sm.Authenticate(ctx, newRaw); !errors.Is(err, auth.ErrSessionInvalid) {
			t.Errorf("successor still authenticates after reset, want ErrSessionInvalid (reset revokes the whole lineage)")
		}
	})

	t.Run("reset before successor transaction mints no successor", func(t *testing.T) {
		pool := newTestPool(t)
		q := store.New(pool)
		userID := createTestUser(t, q)
		clk := testutil.NewClockAtEpoch()
		sm := auth.NewSessionManagerWithPoolForTest(pool, clk.Now)
		ctx := context.Background()

		oldRaw, predecessor, err := sm.Issue(ctx, userID, "ua", "1.2.3.4")
		if err != nil {
			t.Fatalf("Issue() error = %v", err)
		}
		clk.Advance(25 * time.Hour)

		admitted := make(chan struct{})
		release := make(chan struct{})
		auth.SetSessionRotationProbeForTest(sm, func() {
			close(admitted)
			<-release
		})

		authDone := make(chan string, 1)
		go func() {
			_, rotated, authErr := sm.Authenticate(ctx, oldRaw)
			if authErr != nil {
				authDone <- "ERR:" + authErr.Error()
				return
			}
			authDone <- rotated
		}()

		<-admitted

		if err := resetAllSessions(ctx, t, pool, q, userID); err != nil {
			t.Fatalf("reset error = %v", err)
		}
		close(release)

		if rotated := <-authDone; rotated != "" {
			t.Errorf("Authenticate() rotatedToken = %q, want empty (a reset revoked the predecessor before the successor tx, so no successor may be minted)", rotated)
		}

		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id = $1`, userID).Scan(&count); err != nil {
			t.Fatalf("count sessions: %v", err)
		}
		if count != 1 {
			t.Errorf("sessions rows = %d, want 1 (only the revoked predecessor, no successor across the reset fence)", count)
		}
		_ = predecessor
	})
}
