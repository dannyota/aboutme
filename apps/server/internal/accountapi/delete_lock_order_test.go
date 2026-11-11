package accountapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/store"
)

type lockOrderTarget struct {
	service *Service
	pool    *store.Pool
	userID  uuid.UUID
	slug    string
	delete  func(context.Context) error
}

// TestDeleteAccount_FollowsResumeWriteLockOrder holds the user row the way a
// resume write does, lets a deletion of the same account queue behind it, and
// then takes the slug claim and public state the way a publish does. With one
// lock order the deletion waits and then succeeds. A deletion that takes slug
// claims before the user row deadlocks here, and PostgreSQL aborts one side
// with SQLSTATE 40P01.
func TestDeleteAccount_FollowsResumeWriteLockOrder(t *testing.T) {
	t.Run("self-service deletion", func(t *testing.T) {
		env := newDeletionEnvironment(t)
		resumes := resume.NewStore(env.service.pool, docmigrate.NewIdentityProjector())
		created, err := resumes.Create(env.ctx, env.user.ID, "Owned", validDeletionDocument(nil))
		if err != nil {
			t.Fatalf("Create resume: %v", err)
		}
		slug := "order-" + uuid.NewString()[:8]
		if _, err := env.service.pool.Exec(env.ctx, `UPDATE resumes SET slug=$2, live=true WHERE id=$1`, created.ID, slug); err != nil {
			t.Fatalf("publish resume: %v", err)
		}
		runLockOrderRace(t, lockOrderTarget{
			service: env.service, pool: env.service.pool, userID: env.user.ID, slug: slug,
			delete: func(ctx context.Context) error { return env.service.deleteAccount(ctx, env.session) },
		})
	})

	t.Run("operator deletion", func(t *testing.T) {
		f := newOperatorFixture(t)
		slug := f.slugs[0]
		runLockOrderRace(t, lockOrderTarget{
			service: f.service, pool: f.pool, userID: f.user.ID, slug: slug,
			delete: func(ctx context.Context) error {
				_, err := f.service.DeleteAccountBySlug(ctx, slug, f.user.ID)
				return err
			},
		})
	})
}

func runLockOrderRace(t *testing.T, target lockOrderTarget) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	deletionPIDs := make(chan int, 1)
	begin := target.service.beginTx
	target.service.beginTx = func(ctx context.Context) (pgx.Tx, error) {
		tx, err := begin(ctx)
		if err != nil {
			return nil, err
		}
		var pid int
		if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				return nil, errors.Join(err, rollbackErr)
			}
			return nil, err
		}
		select {
		case deletionPIDs <- pid:
		default:
		}
		return tx, nil
	}

	writer, err := target.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin resume write: %v", err)
	}
	defer rollbackUnlessClosed(t, writer)()
	var writerPID int
	if err := writer.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&writerPID); err != nil {
		t.Fatalf("resume write backend: %v", err)
	}
	writes := store.New(writer)
	if _, err := writes.LockUserForResumeWrite(ctx, target.userID); err != nil {
		t.Fatalf("resume write lock user: %v", err)
	}

	deleteDone := make(chan error, 1)
	deleteExited := make(chan struct{})
	go func() {
		defer close(deleteExited)
		deleteDone <- target.delete(ctx)
	}()
	defer func() {
		cancel()
		<-deleteExited
	}()
	var deletionPID int
	select {
	case deletionPID = <-deletionPIDs:
	case err := <-deleteDone:
		t.Fatalf("deletion returned before its transaction began: %v", err)
	case <-ctx.Done():
		t.Fatal("deletion transaction did not begin")
	}
	waitUntilBlockedBy(ctx, t, target.pool, deletionPID, writerPID)

	slugErr := writes.LockSlugClaim(ctx, target.slug)
	failOnDeadlock(t, "resume write slug lock", slugErr)
	if slugErr != nil {
		t.Fatalf("resume write slug lock: %v", slugErr)
	}
	_, publicErr := writes.LockPublicState(ctx)
	failOnDeadlock(t, "resume write public state lock", publicErr)
	if publicErr != nil {
		t.Fatalf("resume write public state lock: %v", publicErr)
	}
	if err := writer.Commit(ctx); err != nil {
		t.Fatalf("commit resume write: %v", err)
	}

	select {
	case err := <-deleteDone:
		failOnDeadlock(t, "deletion", err)
		if err != nil {
			t.Fatalf("deletion after resume write: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("deletion did not finish after the resume write committed")
	}
	if _, err := store.New(target.pool).GetUserByID(ctx, target.userID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("user after deletion: %v, want no rows", err)
	}
}

// waitUntilBlockedBy polls until backend waiter waits on a lock held by
// backend holder, or ctx ends.
func waitUntilBlockedBy(ctx context.Context, t *testing.T, pool *store.Pool, waiter, holder int) {
	t.Helper()
	for {
		var blocked bool
		if err := pool.QueryRow(ctx, `SELECT $2::int = ANY(pg_blocking_pids($1::int))`, waiter, holder).Scan(&blocked); err != nil {
			t.Fatalf("observe deletion wait: %v", err)
		}
		if blocked {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("deletion never waited on the resume write's user lock")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func failOnDeadlock(t *testing.T, step string, err error) {
	t.Helper()
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "40P01" {
		t.Fatalf("%s deadlocked (SQLSTATE 40P01): %v", step, err)
	}
}
