package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/dannyota/aboutme/apps/server/migrations"
)

var writeRunnerDatabaseCounter atomic.Uint64

var writeRunnerDatabaseName = regexp.MustCompile(`^aboutme_migrate_test_[0-9]+_[0-9]+$`)

func TestWriteTxRunnerLiveSuccessFinishesCommitsAndReusesCleanBackend(t *testing.T) {
	dsn, admin := newWriteRunnerDatabase(t)
	pool := newWriteRunnerAppPool(t, dsn, nil)
	runner := NewWriteTxRunner(pool)
	var firstPID, secondPID int32

	if err := runner.ExecWrite(context.Background(), func(q *Queries) error {
		if err := q.db.QueryRow(context.Background(), `SELECT pg_backend_pid()`).Scan(&firstPID); err != nil {
			return err
		}
		_, err := q.db.Exec(context.Background(), `INSERT INTO public.runtime_write_probe VALUES (1)`)
		return err
	}); err != nil {
		t.Fatalf("first ExecWrite: %v", err)
	}
	if err := runner.ExecWrite(context.Background(), func(q *Queries) error {
		return q.db.QueryRow(context.Background(), `SELECT pg_backend_pid()`).Scan(&secondPID)
	}); err != nil {
		t.Fatalf("second ExecWrite: %v", err)
	}
	if firstPID != secondPID {
		t.Fatalf("clean backend PID changed from %d to %d", firstPID, secondPID)
	}
	var rows, generation int64
	if err := admin.QueryRowContext(context.Background(), `SELECT count(*) FROM public.runtime_write_probe`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(context.Background(), `SELECT generation FROM public.runtime_write_state WHERE singleton`).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || generation != 2 {
		t.Fatalf("rows=%d generation=%d, want 1 and 2", rows, generation)
	}
}

func TestWriteTxRunnerLiveGateFailureRollsBackAndReusesBackend(t *testing.T) {
	dsn, admin := newWriteRunnerDatabase(t)
	pidSeen := make(chan int32, 1)
	pool := newWriteRunnerAppPool(t, dsn, func(ctx context.Context, conn *pgx.Conn) error {
		var pid int32
		if err := conn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			return err
		}
		select {
		case pidSeen <- pid:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	runner := NewWriteTxRunner(pool)
	if _, err := admin.ExecContext(context.Background(), `UPDATE public.runtime_write_state SET write_gate='closed' WHERE singleton`); err != nil {
		t.Fatal(err)
	}
	callbacks := 0
	err := runner.ExecWrite(context.Background(), func(*Queries) error { callbacks++; return nil })
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55000" || callbacks != 0 {
		t.Fatalf("error=%v callbacks=%d, want 55000 and zero callbacks", err, callbacks)
	}
	firstPID := <-pidSeen
	if _, err := admin.ExecContext(context.Background(), `UPDATE public.runtime_write_state SET write_gate='open' WHERE singleton`); err != nil {
		t.Fatal(err)
	}
	var reusedPID int32
	if err := runner.ExecWrite(context.Background(), func(q *Queries) error {
		return q.db.QueryRow(context.Background(), `SELECT pg_backend_pid()`).Scan(&reusedPID)
	}); err != nil {
		t.Fatal(err)
	}
	if reusedPID != firstPID {
		t.Fatalf("confirmed rollback replaced PID %d with %d", firstPID, reusedPID)
	}
}

func TestWriteTxRunnerLiveForcedConstraintCannotBypassFinish(t *testing.T) {
	dsn, admin := newWriteRunnerDatabase(t)
	pool := newWriteRunnerAppPool(t, dsn, nil)
	runner := NewWriteTxRunner(pool)
	var firstPID int32
	err := runner.ExecWrite(context.Background(), func(q *Queries) error {
		if err := q.db.QueryRow(context.Background(), `SELECT pg_backend_pid()`).Scan(&firstPID); err != nil {
			return err
		}
		if _, err := q.db.Exec(context.Background(), `INSERT INTO public.runtime_write_probe VALUES (10)`); err != nil {
			return err
		}
		_, err := q.db.Exec(context.Background(), `SET CONSTRAINTS ALL IMMEDIATE`)
		return err
	})
	if err == nil {
		t.Fatal("forced constraints before runner finish succeeded")
	}
	var rows int
	if err := admin.QueryRowContext(context.Background(), `SELECT count(*) FROM public.runtime_write_probe WHERE id=10`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("rolled-back rows=%d err=%v, want zero", rows, err)
	}
	var reusedPID int32
	if err := runner.ExecWrite(context.Background(), func(q *Queries) error {
		return q.db.QueryRow(context.Background(), `SELECT pg_backend_pid()`).Scan(&reusedPID)
	}); err != nil {
		t.Fatal(err)
	}
	if reusedPID != firstPID {
		t.Fatalf("confirmed callback rollback replaced PID %d with %d", firstPID, reusedPID)
	}
}

func TestWriteTxRunnerLiveAM001DestroysBackendAndUsesReplacement(t *testing.T) {
	dsn, admin := newWriteRunnerDatabase(t)
	var connections atomic.Int32
	pids := make(chan int32, 2)
	pool := newWriteRunnerAppPool(t, dsn, func(ctx context.Context, conn *pgx.Conn) error {
		var pid int32
		if err := conn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			return err
		}
		select {
		case pids <- pid:
		case <-ctx.Done():
			return ctx.Err()
		}
		if connections.Add(1) == 1 {
			if _, err := conn.Exec(ctx, `CREATE TEMP VIEW runtime_write_entry_v1 AS SELECT 1 AS wrong_shape`); err != nil {
				return err
			}
		}
		return nil
	})
	runner := NewWriteTxRunner(pool)
	callbacks := 0
	err := runner.ExecWrite(context.Background(), func(*Queries) error { callbacks++; return nil })
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "AM001" || callbacks != 0 {
		t.Fatalf("error=%v callbacks=%d, want AM001 and zero callbacks", err, callbacks)
	}
	oldPID := <-pids
	assertBackendGone(t, admin, oldPID)
	var replacementPID int32
	if err := runner.ExecWrite(context.Background(), func(q *Queries) error {
		return q.db.QueryRow(context.Background(), `SELECT pg_backend_pid()`).Scan(&replacementPID)
	}); err != nil {
		t.Fatalf("replacement write: %v", err)
	}
	if replacementPID == oldPID {
		t.Fatalf("poisoned backend PID %d was reused", oldPID)
	}
}

func TestWriteTxRunnerLiveExclusiveBarrierBlocksBeforeCallback(t *testing.T) {
	dsn, admin := newWriteRunnerDatabase(t)
	holder, err := admin.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close() //nolint:errcheck
	tx, err := holder.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(context.Background(), `SELECT pg_advisory_xact_lock($1)`, int64(3727108639518528074)); err != nil {
		t.Fatal(err)
	}
	pidSeen := make(chan int32, 1)
	pool := newWriteRunnerAppPool(t, dsn, func(ctx context.Context, conn *pgx.Conn) error {
		var pid int32
		if err := conn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			return err
		}
		select {
		case pidSeen <- pid:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	callbackStarted := make(chan struct{})
	done := make(chan error, 1)
	runnerCtx, cancelRunner := context.WithTimeout(context.Background(), 5*time.Second)
	joined := false
	defer func() {
		cancelRunner()
		ignoreWriteCleanupError(tx.Rollback())
		if !joined {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Errorf("runner did not stop during cleanup")
			}
		}
	}()
	go func() {
		done <- NewWriteTxRunner(pool).ExecWrite(runnerCtx, func(*Queries) error {
			close(callbackStarted)
			return nil
		})
	}()
	var runnerPID int32
	select {
	case runnerPID = <-pidSeen:
	case <-runnerCtx.Done():
		t.Fatalf("runner did not acquire a backend: %v", runnerCtx.Err())
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		var waiting bool
		queryCtx, queryCancel := context.WithTimeout(context.Background(), time.Second)
		err := admin.QueryRowContext(queryCtx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND classid=867785103 AND objid=2177536586 AND objsubid=1 AND mode='ShareLock' AND granted=false)`, runnerPID).Scan(&waiting)
		queryCancel()
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("runner PID %d did not wait on advisory barrier", runnerPID)
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case <-callbackStarted:
		t.Fatal("callback started while exclusive barrier was held")
	default:
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		joined = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not finish after exclusive barrier release")
	}
}

func TestWriteTxRunnerLiveUnknownFinishDestroysBackend(t *testing.T) {
	dsn, admin := newWriteRunnerDatabase(t)
	pool := newWriteRunnerAppPool(t, dsn, nil)
	runner := NewWriteTxRunner(pool)
	pidReady := make(chan int32, 1)
	terminated := make(chan struct{})
	var releaseCallback sync.Once
	callbacks := 0
	done := make(chan error, 1)
	runnerCtx, cancelRunner := context.WithTimeout(context.Background(), 5*time.Second)
	joined := false
	defer func() {
		releaseCallback.Do(func() { close(terminated) })
		cancelRunner()
		if !joined {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Errorf("runner did not stop during cleanup")
			}
		}
	}()
	go func() {
		done <- runner.ExecWrite(runnerCtx, func(q *Queries) error {
			callbacks++
			var pid int32
			if err := q.db.QueryRow(runnerCtx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			select {
			case pidReady <- pid:
			case <-runnerCtx.Done():
				return runnerCtx.Err()
			}
			select {
			case <-terminated:
				return nil
			case <-runnerCtx.Done():
				return runnerCtx.Err()
			}
		})
	}()
	var oldPID int32
	select {
	case oldPID = <-pidReady:
	case <-runnerCtx.Done():
		t.Fatalf("callback did not report its backend: %v", runnerCtx.Err())
	}
	var killed bool
	terminateCtx, terminateCancel := context.WithTimeout(context.Background(), time.Second)
	err := admin.QueryRowContext(terminateCtx, `SELECT pg_terminate_backend($1)`, oldPID).Scan(&killed)
	terminateCancel()
	if err != nil || !killed {
		t.Fatalf("terminate backend: killed=%v err=%v", killed, err)
	}
	releaseCallback.Do(func() { close(terminated) })
	select {
	case err = <-done:
		joined = true
	case <-runnerCtx.Done():
		t.Fatalf("runner did not return after termination: %v", runnerCtx.Err())
	}
	if err == nil || callbacks != 1 {
		t.Fatalf("error=%v callbacks=%d, want finish transport error and one callback", err, callbacks)
	}
	assertBackendGone(t, admin, oldPID)
	var replacementPID int32
	if err := runner.ExecWrite(context.Background(), func(q *Queries) error {
		return q.db.QueryRow(context.Background(), `SELECT pg_backend_pid()`).Scan(&replacementPID)
	}); err != nil {
		t.Fatalf("replacement write: %v", err)
	}
	if replacementPID == oldPID {
		t.Fatalf("terminated backend PID %d was reused", oldPID)
	}
}

type liveWrappedLease struct {
	*pooledWriteTxLease
	wrap func(pgx.Tx) pgx.Tx
}

func (l *liveWrappedLease) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	tx, err := l.pooledWriteTxLease.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return l.wrap(tx), nil
}

type commitResponseLostTx struct {
	pgx.Tx
	err error
}

func (tx *commitResponseLostTx) Commit(ctx context.Context) error {
	if err := tx.Tx.Commit(ctx); err != nil {
		return err
	}
	return tx.err
}

type rollbackResponseLostTx struct {
	pgx.Tx
	err error
}

func (tx *rollbackResponseLostTx) Rollback(context.Context) error { return tx.err }

func TestWriteTxRunnerLiveCommitResponseAmbiguityDestroysWithoutReplay(t *testing.T) {
	dsn, admin := newWriteRunnerDatabase(t)
	pool := newWriteRunnerAppPool(t, dsn, nil)
	wantErr := errors.New("simulated lost commit response")
	runner := liveWrappedRunner(pool, func(tx pgx.Tx) pgx.Tx { return &commitResponseLostTx{Tx: tx, err: wantErr} })
	callbacks := 0
	var oldPID int32
	err := runner.ExecWrite(context.Background(), func(q *Queries) error {
		callbacks++
		if err := q.db.QueryRow(context.Background(), `SELECT pg_backend_pid()`).Scan(&oldPID); err != nil {
			return err
		}
		_, err := q.db.Exec(context.Background(), `INSERT INTO public.runtime_write_probe VALUES (20)`)
		return err
	})
	if !errors.Is(err, wantErr) || callbacks != 1 {
		t.Fatalf("error=%v callbacks=%d, want wrapped ambiguity and one callback", err, callbacks)
	}
	assertBackendGone(t, admin, oldPID)
	var rows int
	if err := admin.QueryRowContext(context.Background(), `SELECT count(*) FROM public.runtime_write_probe WHERE id=20`).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("committed rows=%d err=%v, want one without replay", rows, err)
	}
	var replacementPID int32
	if err := NewWriteTxRunner(pool).ExecWrite(context.Background(), func(q *Queries) error {
		return q.db.QueryRow(context.Background(), `SELECT pg_backend_pid()`).Scan(&replacementPID)
	}); err != nil {
		t.Fatal(err)
	}
	if replacementPID == oldPID {
		t.Fatalf("ambiguous commit backend PID %d was reused", oldPID)
	}
}

func TestWriteTxRunnerLiveCanceledFailedCleanupDestroysAndPreservesPrimary(t *testing.T) {
	dsn, admin := newWriteRunnerDatabase(t)
	pool := newWriteRunnerAppPool(t, dsn, nil)
	rollbackErr := errors.New("simulated lost rollback response")
	runner := liveWrappedRunner(pool, func(tx pgx.Tx) pgx.Tx { return &rollbackResponseLostTx{Tx: tx, err: rollbackErr} })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wantErr := errors.New("callback failed")
	var oldPID int32
	err := runner.ExecWrite(ctx, func(q *Queries) error {
		if err := q.db.QueryRow(context.Background(), `SELECT pg_backend_pid()`).Scan(&oldPID); err != nil {
			return err
		}
		if _, err := q.db.Exec(context.Background(), `INSERT INTO public.runtime_write_probe VALUES (30)`); err != nil {
			return err
		}
		cancel()
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error=%v, want primary callback error", err)
	}
	assertBackendGone(t, admin, oldPID)
	var rows int
	if err := admin.QueryRowContext(context.Background(), `SELECT count(*) FROM public.runtime_write_probe WHERE id=30`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("discarded transaction rows=%d err=%v, want zero", rows, err)
	}
	var replacementPID int32
	if err := NewWriteTxRunner(pool).ExecWrite(context.Background(), func(q *Queries) error {
		return q.db.QueryRow(context.Background(), `SELECT pg_backend_pid()`).Scan(&replacementPID)
	}); err != nil {
		t.Fatal(err)
	}
	if replacementPID == oldPID {
		t.Fatalf("failed-cleanup backend PID %d was reused", oldPID)
	}
}

func liveWrappedRunner(pool *pgxpool.Pool, wrap func(pgx.Tx) pgx.Tx) WriteTxRunner {
	return &writeTxRunner{acquire: func(ctx context.Context) (writeTxLease, error) {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			return nil, err
		}
		return &liveWrappedLease{pooledWriteTxLease: &pooledWriteTxLease{conn: conn}, wrap: wrap}, nil
	}}
}

func newWriteRunnerDatabase(t *testing.T) (string, *sql.DB) {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		if os.Getenv("REQUIRE_TEST_DB") == "1" {
			t.Fatal("TEST_DATABASE_URL is required")
		}
		t.Skip("TEST_DATABASE_URL not set; skipping live-database integration test")
	}
	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := admin.Close(); closeErr != nil {
			t.Errorf("close admin pool: %v", closeErr)
		}
	})
	name := fmt.Sprintf("aboutme_migrate_test_%d_%d", time.Now().UnixNano(), writeRunnerDatabaseCounter.Add(1))
	if !writeRunnerDatabaseName.MatchString(name) {
		t.Fatalf("unsafe disposable database name %q", name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, createErr := admin.ExecContext(ctx, `CREATE DATABASE `+name); createErr != nil {
		t.Fatal(createErr)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, dropErr := admin.ExecContext(cleanupCtx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`); dropErr != nil {
			t.Errorf("drop disposable database %s: %v", name, dropErr)
		}
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + strings.TrimPrefix(name, "/")
	dsn := u.String()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("close disposable database pool: %v", closeErr)
		}
	})
	setupCtx, setupCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer setupCancel()
	provider, err := migrations.NewProvider(db, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(setupCtx, 13); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if _, err := db.ExecContext(setupCtx, `CREATE TABLE public.runtime_write_probe(id integer PRIMARY KEY); ALTER TABLE public.runtime_write_probe OWNER TO aboutme_runtime_owner; CREATE TRIGGER runtime_write_probe_assert BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.runtime_write_probe FOR EACH STATEMENT EXECUTE FUNCTION public.runtime_assert_business_write('probe'); GRANT SELECT,INSERT,UPDATE,DELETE ON public.runtime_write_probe TO aboutme_app`); err != nil {
		t.Fatalf("create assertion probe: %v", err)
	}
	return dsn, db
}

func newWriteRunnerAppPool(t *testing.T, dsn string, setup func(context.Context, *pgx.Conn) error) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	cfg.MinConns = 0
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		if setup != nil {
			if setupErr := setup(ctx, conn); setupErr != nil {
				return setupErr
			}
		}
		_, authErr := conn.Exec(ctx, `SET SESSION AUTHORIZATION aboutme_app`)
		return authErr
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func assertBackendGone(t *testing.T, admin *sql.DB, pid int32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		var exists bool
		queryCtx, queryCancel := context.WithTimeout(ctx, time.Second)
		err := admin.QueryRowContext(queryCtx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1)`, pid).Scan(&exists)
		queryCancel()
		if err != nil {
			t.Fatal(err)
		}
		if !exists {
			return
		}
		if ctx.Err() != nil {
			t.Fatalf("discarded backend PID %d remains in pg_stat_activity", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
