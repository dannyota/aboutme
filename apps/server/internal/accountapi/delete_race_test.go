package accountapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func TestDeleteAccountTakesCanonicalEmailLockBeforeUser(t *testing.T) {
	env := newDeletionEnvironment(t)
	holder, err := env.service.pool.Begin(env.ctx)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer rollbackUnlessClosed(t, holder)()
	if err := env.queries.WithTx(holder).LockCanonicalAccountEmail(env.ctx, env.user.Email); err != nil {
		t.Fatalf("lock canonical email: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- env.service.deleteAccount(context.Background(), env.session) }()
	assertStillBlocked(t, done, "deletion bypassed canonical-email lock")
	if _, err := holder.Exec(env.ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, env.user.ID); err != nil {
		t.Fatalf("deletion locked user before canonical email: %v", err)
	}
	if err := holder.Commit(env.ctx); err != nil {
		t.Fatalf("commit holder: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("deletion after canonical-email release: %v", err)
	}
}

func TestDeleteAccountResetRace(t *testing.T) {
	t.Run("reset wins", func(t *testing.T) {
		env := newDeletionEnvironment(t)
		now := time.Now().UTC().Truncate(time.Microsecond)
		if _, err := env.queries.UpsertPasswordCredential(env.ctx, store.UpsertPasswordCredentialParams{
			UserID: env.user.ID, EncodedHash: []byte("old"), CreatedAt: now, ChangedAt: now,
		}); err != nil {
			t.Fatalf("seed credential: %v", err)
		}
		reset, err := env.queries.CreatePasswordResetToken(env.ctx, store.CreatePasswordResetTokenParams{
			UserID: env.user.ID, TokenDigest: deletionDigest(), CreatedAt: now, ExpiresAt: now.Add(30 * time.Minute),
		})
		if err != nil {
			t.Fatalf("seed reset: %v", err)
		}
		holder, err := env.service.pool.Begin(env.ctx)
		if err != nil {
			t.Fatalf("begin reset: %v", err)
		}
		defer rollbackUnlessClosed(t, holder)()
		qtx := env.queries.WithTx(holder)
		if _, err := qtx.GetUserForUpdate(env.ctx, env.user.ID); err != nil {
			t.Fatalf("reset lock user: %v", err)
		}
		if _, err := qtx.GetPasswordCredentialForUpdate(env.ctx, env.user.ID); err != nil {
			t.Fatalf("reset lock credential: %v", err)
		}
		if _, err := qtx.GetPasswordResetTokenForUpdate(env.ctx, reset.ID); err != nil {
			t.Fatalf("reset lock token: %v", err)
		}

		done := make(chan error, 1)
		go func() { done <- env.service.deleteAccount(context.Background(), env.session) }()
		assertStillBlocked(t, done, "deletion bypassed reset user lock")
		if _, err := qtx.RevokeAllSessions(env.ctx, store.RevokeAllSessionsParams{UserID: env.user.ID, RevokedAt: &now}); err != nil {
			t.Fatalf("reset revoke sessions: %v", err)
		}
		if _, err := qtx.DeletePasswordResetToken(env.ctx, reset.ID); err != nil {
			t.Fatalf("reset consume token: %v", err)
		}
		if err := holder.Commit(env.ctx); err != nil {
			t.Fatalf("commit reset: %v", err)
		}
		if err := <-done; !errors.Is(err, auth.ErrSessionInvalid) {
			t.Fatalf("deletion after reset error = %v, want invalid session", err)
		}
		if _, err := env.queries.GetUserByID(env.ctx, env.user.ID); err != nil {
			t.Fatalf("reset winner lost account: %v", err)
		}
	})

	t.Run("delete wins", func(t *testing.T) {
		env := newDeletionEnvironment(t)
		locked, release := holdDeletionAfterUserLock(env)
		deleteDone := make(chan error, 1)
		go func() { deleteDone <- env.service.deleteAccount(context.Background(), env.session) }()
		<-locked
		resetDone := make(chan error, 1)
		go func() {
			resetDone <- pgx.BeginFunc(context.Background(), env.service.pool, func(tx pgx.Tx) error {
				_, lockErr := env.queries.WithTx(tx).GetUserForUpdate(context.Background(), env.user.ID)
				return lockErr
			})
		}()
		assertStillBlocked(t, resetDone, "reset bypassed deletion user lock")
		close(release)
		if err := <-deleteDone; err != nil {
			t.Fatalf("deletion: %v", err)
		}
		if err := <-resetDone; !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("reset after deletion error = %v, want no rows", err)
		}
	})
}

func TestDeleteAccountSessionRotationRace(t *testing.T) {
	t.Run("rotation wins", func(t *testing.T) {
		env := newDeletionEnvironment(t)
		ageDeletionSession(t, env)
		_, rotated, err := auth.NewSessionManagerWithPool(env.service.pool).Authenticate(env.ctx, env.rawSession)
		if err != nil || rotated == "" {
			t.Fatalf("rotate session = (%q, %v), want successor", rotated, err)
		}
		if err := env.service.deleteAccount(env.ctx, env.session); err != nil {
			t.Fatalf("deletion after rotation: %v", err)
		}
		var count int
		if err := env.service.pool.QueryRow(env.ctx, `SELECT count(*) FROM sessions WHERE user_id=$1`, env.user.ID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("session rows after deletion = %d, error=%v, want 0", count, err)
		}
	})

	t.Run("delete wins", func(t *testing.T) {
		env := newDeletionEnvironment(t)
		ageDeletionSession(t, env)
		locked, release := holdDeletionAfterUserLock(env)
		deleteDone := make(chan error, 1)
		go func() { deleteDone <- env.service.deleteAccount(context.Background(), env.session) }()
		<-locked
		rotateDone := make(chan error, 1)
		go func() {
			_, _, rotateErr := auth.NewSessionManagerWithPool(env.service.pool).Authenticate(context.Background(), env.rawSession)
			rotateDone <- rotateErr
		}()
		assertStillBlocked(t, rotateDone, "rotation bypassed deletion user lock")
		close(release)
		if err := <-deleteDone; err != nil {
			t.Fatalf("deletion: %v", err)
		}
		if err := <-rotateDone; err == nil {
			t.Fatal("rotation succeeded after account deletion")
		}
		var count int
		if err := env.service.pool.QueryRow(env.ctx, `SELECT count(*) FROM sessions WHERE user_id=$1`, env.user.ID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("session rows after race = %d, error=%v, want 0", count, err)
		}
	})
}

func TestDeleteAccountOAuthGrantRace(t *testing.T) {
	t.Run("grant wins then cascades", func(t *testing.T) {
		env := newDeletionEnvironment(t)
		now := time.Now().UTC().Truncate(time.Microsecond)
		client, err := env.queries.CreateOAuthClient(env.ctx, store.CreateOAuthClientParams{
			ClientName: "Grant winner", RedirectURIs: []byte(`["https://client.example/callback"]`), CreatedAt: now,
		})
		if err != nil {
			t.Fatalf("create client: %v", err)
		}
		holder, err := env.service.pool.Begin(env.ctx)
		if err != nil {
			t.Fatalf("begin grant: %v", err)
		}
		defer rollbackUnlessClosed(t, holder)()
		qtx := env.queries.WithTx(holder)
		if _, err := qtx.GetOAuthClientForUpdate(env.ctx, client.ID); err != nil {
			t.Fatalf("lock client: %v", err)
		}
		if _, err := qtx.GetUserForUpdate(env.ctx, env.user.ID); err != nil {
			t.Fatalf("lock user: %v", err)
		}
		if _, err := qtx.UpsertOAuthGrant(env.ctx, store.UpsertOAuthGrantParams{
			UserID: env.user.ID, ClientID: client.ID, Scopes: "resumes:read", CreatedAt: now,
		}); err != nil {
			t.Fatalf("create grant: %v", err)
		}
		deleteDone := make(chan error, 1)
		go func() { deleteDone <- env.service.deleteAccount(context.Background(), env.session) }()
		assertStillBlocked(t, deleteDone, "deletion bypassed grant user lock")
		if err := holder.Commit(env.ctx); err != nil {
			t.Fatalf("commit grant: %v", err)
		}
		if err := <-deleteDone; err != nil {
			t.Fatalf("deletion after grant: %v", err)
		}
		assertOAuthGrantCount(t, env, 0)
	})

	t.Run("delete wins", func(t *testing.T) {
		env := newDeletionEnvironment(t)
		now := time.Now().UTC().Truncate(time.Microsecond)
		client, err := env.queries.CreateOAuthClient(env.ctx, store.CreateOAuthClientParams{
			ClientName: "Deletion race", RedirectURIs: []byte(`["https://client.example/callback"]`), CreatedAt: now,
		})
		if err != nil {
			t.Fatalf("create client: %v", err)
		}
		locked, release := holdDeletionAfterUserLock(env)
		deleteDone := make(chan error, 1)
		go func() { deleteDone <- env.service.deleteAccount(context.Background(), env.session) }()
		<-locked
		grantDone := make(chan error, 1)
		go func() {
			grantDone <- pgx.BeginFunc(context.Background(), env.service.pool, func(tx pgx.Tx) error {
				qtx := env.queries.WithTx(tx)
				if _, lockErr := qtx.GetOAuthClientForUpdate(context.Background(), client.ID); lockErr != nil {
					return lockErr
				}
				if _, lockErr := qtx.GetUserForUpdate(context.Background(), env.user.ID); lockErr != nil {
					return lockErr
				}
				_, grantErr := qtx.UpsertOAuthGrant(context.Background(), store.UpsertOAuthGrantParams{
					UserID: env.user.ID, ClientID: client.ID, Scopes: "resumes:read", CreatedAt: now,
				})
				return grantErr
			})
		}()
		assertStillBlocked(t, grantDone, "grant bypassed deletion user lock")
		close(release)
		if err := <-deleteDone; err != nil {
			t.Fatalf("deletion: %v", err)
		}
		if err := <-grantDone; !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("grant after deletion error = %v, want no rows", err)
		}
		assertOAuthGrantCount(t, env, 0)
	})
}

func assertOAuthGrantCount(t *testing.T, env deletionEnvironment, want int) {
	t.Helper()
	var count int
	if err := env.service.pool.QueryRow(env.ctx, `SELECT count(*) FROM oauth_grants WHERE user_id=$1`, env.user.ID).Scan(&count); err != nil || count != want {
		t.Fatalf("grant rows = %d, error=%v, want %d", count, err, want)
	}
}

func ageDeletionSession(t *testing.T, env deletionEnvironment) {
	t.Helper()
	old := time.Now().UTC().Add(-25 * time.Hour)
	if _, err := env.service.pool.Exec(env.ctx, `UPDATE sessions SET created_at=$2, last_seen_at=$2 WHERE id=$1`, env.session.ID, old); err != nil {
		t.Fatalf("age session: %v", err)
	}
}

func holdDeletionAfterUserLock(env deletionEnvironment) (<-chan struct{}, chan<- struct{}) {
	locked := make(chan struct{})
	release := make(chan struct{})
	env.service.afterUserLock = func() {
		close(locked)
		<-release
	}
	return locked, release
}

func assertStillBlocked(t *testing.T, done <-chan error, failure string) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("%s: %v", failure, err)
	case <-time.After(150 * time.Millisecond):
	}
}

func rollbackUnlessClosed(t *testing.T, tx pgx.Tx) func() {
	t.Helper()
	return func() {
		if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("rollback holder: %v", err)
		}
	}
}
