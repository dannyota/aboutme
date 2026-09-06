//nolint:govet // Sequential live-test setup deliberately reuses short error scopes.
package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pressly/goose/v3/lock"
)

type recordingSessionLocker struct {
	events               *[]string
	lockErr, unlockErr   error
	lockConn, unlockConn *sql.Conn
	delegate             lock.SessionLocker
	unlockContextErr     error
	unlockDeadline       time.Time
	unlockHasDeadline    bool
}

func (l *recordingSessionLocker) SessionLock(ctx context.Context, conn *sql.Conn) error {
	*l.events = append(*l.events, "goose-lock")
	l.lockConn = conn
	if l.delegate != nil {
		if err := l.delegate.SessionLock(ctx, conn); err != nil {
			return err
		}
	}
	return l.lockErr
}

func (l *recordingSessionLocker) SessionUnlock(ctx context.Context, conn *sql.Conn) error {
	*l.events = append(*l.events, "goose-unlock")
	l.unlockConn = conn
	l.unlockContextErr = ctx.Err()
	l.unlockDeadline, l.unlockHasDeadline = ctx.Deadline()
	if l.delegate != nil {
		if err := l.delegate.SessionUnlock(ctx, conn); err != nil {
			return err
		}
	}
	return l.unlockErr
}

func TestRuntimeLockerSameBackendOrderAndCleanExit(t *testing.T) {
	_, db := newSessionTestDatabase(t)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	registerSessionConnCleanup(t, conn)
	var initialPID int32
	if err := conn.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&initialPID); err != nil {
		t.Fatal(err)
	}
	gooseLocker, err := lock.NewPostgresSessionLocker(lock.WithLockID(LockID), lock.WithLockTimeout(1, 1), lock.WithUnlockTimeout(1, 1))
	if err != nil {
		t.Fatal(err)
	}
	events := []string{}
	wrapped := &recordingSessionLocker{events: &events, delegate: gooseLocker}
	validator := func(ctx context.Context, got *sql.Conn, pid int32) error {
		events = append(events, "validate")
		if got != conn || pid != initialPID {
			return errors.New("validator received wrong connection")
		}
		var user string
		var lockCount int
		if err := got.QueryRowContext(ctx, `SELECT session_user,(SELECT count(*) FROM pg_locks WHERE pid=pg_backend_pid() AND locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND ((classid::bigint<<32)|objid::bigint) IN ($1,$2) AND granted)`, runtimeMigrationLockID, LockID).Scan(&user, &lockCount); err != nil {
			return err
		}
		if user != "aboutme_migrator" || lockCount != 2 {
			return fmt.Errorf("user=%s locks=%d", user, lockCount)
		}
		return nil
	}
	locker, err := newRuntimeSessionLocker(LocalAdminMigratorIdentity(), wrapped, validator)
	if err != nil {
		t.Fatal(err)
	}
	realExec := locker.exec
	cleanupContexts := make(map[string]context.Context)
	cleanupContextErrors := make(map[string]error)
	cleanupDeadlines := make(map[string]time.Time)
	locker.exec = func(execCtx context.Context, got *sql.Conn, statement string) (sql.Result, error) {
		if statement == `SELECT public.runtime_exit_migrator()` || statement == `RESET SESSION AUTHORIZATION` {
			cleanupContexts[statement] = execCtx
			cleanupContextErrors[statement] = execCtx.Err()
			cleanupDeadlines[statement], _ = execCtx.Deadline()
		}
		return realExec(execCtx, got, statement)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := locker.SessionLock(ctx, conn); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := locker.SessionUnlock(ctx, conn); err != nil {
		t.Fatal(err)
	}
	if wrapped.unlockContextErr != nil || !wrapped.unlockHasDeadline {
		t.Fatalf("cleanup context err=%v deadline=%t", wrapped.unlockContextErr, wrapped.unlockHasDeadline)
	}
	if remaining := time.Until(wrapped.unlockDeadline); remaining <= 0 || remaining > runtimeLockerCleanupTimeout {
		t.Fatalf("cleanup deadline remaining=%v", remaining)
	}
	exitStatement := `SELECT public.runtime_exit_migrator()`
	resetStatement := `RESET SESSION AUTHORIZATION`
	if cleanupContexts[exitStatement] == nil || cleanupContexts[resetStatement] == nil || cleanupContexts[exitStatement] == cleanupContexts[resetStatement] {
		t.Fatalf("cleanup contexts exit=%p reset=%p", cleanupContexts[exitStatement], cleanupContexts[resetStatement])
	}
	for _, statement := range []string{exitStatement, resetStatement} {
		if cleanupContextErrors[statement] != nil {
			t.Fatalf("%s context err=%v", statement, cleanupContextErrors[statement])
		}
		if remaining := time.Until(cleanupDeadlines[statement]); remaining <= 0 || remaining > runtimeLockerCleanupTimeout {
			t.Fatalf("%s deadline remaining=%v", statement, remaining)
		}
	}
	assertMigrationEvents(t, events, "goose-lock", "validate", "goose-unlock")
	var user string
	var pid int32
	var locks int
	if err := conn.QueryRowContext(context.Background(), `SELECT session_user,pg_backend_pid(),(SELECT count(*) FROM pg_locks WHERE pid=pg_backend_pid() AND locktype='advisory' AND ((classid::bigint<<32)|objid::bigint) IN ($1,$2))`, runtimeMigrationLockID, LockID).Scan(&user, &pid, &locks); err != nil {
		t.Fatal(err)
	}
	if user != "aboutme" || pid != initialPID || locks != 0 {
		t.Fatalf("user=%s pid=%d locks=%d", user, pid, locks)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	reused, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	registerSessionConnCleanup(t, reused)
	var reusedPID int32
	if err := reused.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&reusedPID); err != nil {
		t.Fatal(err)
	}
	if reusedPID != initialPID {
		t.Fatalf("PID changed %d to %d", initialPID, reusedPID)
	}
}

func TestRuntimeLockerEntryFailuresReuseOnlyConfirmed55000(t *testing.T) {
	_, db := newSessionTestDatabase(t)
	db.SetMaxOpenConns(2)
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	registerSessionConnCleanup(t, conn)
	var pid int32
	if err := conn.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	wrapped := &recordingSessionLocker{events: &[]string{}}
	locker, err := newRuntimeSessionLocker(LocalAdminMigratorIdentity(), wrapped, func(context.Context, *sql.Conn, int32) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `UPDATE public.runtime_write_state SET write_gate='closed' WHERE singleton`); err != nil {
		t.Fatal(err)
	}
	err = locker.SessionLock(context.Background(), conn)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55000" {
		t.Fatalf("error=%v", err)
	}
	var reusedPID int32
	if err := conn.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&reusedPID); err != nil {
		t.Fatal(err)
	}
	if reusedPID != pid {
		t.Fatalf("PID changed")
	}
	if _, err := db.ExecContext(context.Background(), `UPDATE public.runtime_write_state SET write_gate='open' WHERE singleton`); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationIdentityLiveRejectsWrongAuthenticatedSession(t *testing.T) {
	_, db := newSessionTestDatabase(t)
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	registerSessionConnCleanup(t, conn)
	validator := func(context.Context, *sql.Conn, int32) error { return nil }
	direct, err := newRuntimeSessionLocker(DirectMigratorIdentity(), &recordingSessionLocker{events: &[]string{}}, validator)
	if err != nil {
		t.Fatal(err)
	}
	if err := direct.SessionLock(context.Background(), conn); err == nil {
		t.Fatal("direct identity accepted aboutme admin")
	}
	var pidBefore, pidAfter int32
	if err := conn.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&pidBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(context.Background(), `SET SESSION AUTHORIZATION aboutme_app`); err != nil {
		t.Fatal(err)
	}
	local, err := newRuntimeSessionLocker(LocalAdminMigratorIdentity(), &recordingSessionLocker{events: &[]string{}}, validator)
	if err != nil {
		t.Fatal(err)
	}
	if err := local.SessionLock(context.Background(), conn); err == nil {
		t.Fatal("local identity accepted foreign session")
	}
	if err := conn.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&pidAfter); err != nil {
		t.Fatal(err)
	}
	if pidAfter != pidBefore {
		t.Fatal("definite role denial replaced backend")
	}
}

func TestRuntimeLockerAM001PoisonsPhysicalBackend(t *testing.T) {
	_, db := newSessionTestDatabase(t)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	registerSessionConnCleanup(t, conn)
	var pid int32
	if err := conn.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(context.Background(), `CREATE TEMP VIEW runtime_migrator_session_v1 AS SELECT true AS wrong_shape`); err != nil {
		t.Fatal(err)
	}
	locker, err := newRuntimeSessionLocker(LocalAdminMigratorIdentity(), &recordingSessionLocker{events: &[]string{}}, func(context.Context, *sql.Conn, int32) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	err = locker.SessionLock(context.Background(), conn)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "AM001" {
		t.Fatalf("error=%v", err)
	}
	if err := conn.PingContext(context.Background()); !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("poisoned conn error=%v", err)
	}
	sessionBackendGone(t, db, pid)
	replacement, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	registerSessionConnCleanup(t, replacement)
	var replacementPID int32
	if err := replacement.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&replacementPID); err != nil {
		t.Fatal(err)
	}
	if replacementPID == pid {
		t.Fatal("poisoned PID reused")
	}
}

func TestRuntimeLockerExclusiveRuntimeContentionPoisonsCanceledEntry(t *testing.T) {
	_, db := newSessionTestDatabase(t)
	db.SetMaxOpenConns(2)
	holder, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	registerSessionConnCleanup(t, holder)
	if _, err := holder.ExecContext(context.Background(), `SELECT pg_advisory_lock($1)`, runtimeMigrationLockID); err != nil {
		t.Fatal(err)
	}
	registerSessionAdvisoryUnlock(t, holder, runtimeMigrationLockID)
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	registerSessionConnCleanup(t, conn)
	var pid int32
	if err := conn.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	locker, err := newRuntimeSessionLocker(LocalAdminMigratorIdentity(), &recordingSessionLocker{events: &[]string{}}, func(context.Context, *sql.Conn, int32) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err = locker.SessionLock(ctx, conn)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v", err)
	}
	if err := conn.PingContext(context.Background()); !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("pinned conn error=%v", err)
	}
	sessionBackendGone(t, db, pid)
}

func TestRuntimeLockerValidationFailurePoisonsAfterCleanUnlock(t *testing.T) {
	_, db := newSessionTestDatabase(t)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	registerSessionConnCleanup(t, conn)
	var pid int32
	if err := conn.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	gooseLocker, err := lock.NewPostgresSessionLocker(lock.WithLockID(LockID), lock.WithLockTimeout(1, 1), lock.WithUnlockTimeout(1, 1))
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Errorf("catalog contamination: %w", &pgconn.PgError{Code: "AM001"})
	locker, err := newRuntimeSessionLocker(LocalAdminMigratorIdentity(), gooseLocker, func(context.Context, *sql.Conn, int32) error { return want })
	if err != nil {
		t.Fatal(err)
	}
	err = locker.SessionLock(context.Background(), conn)
	if !errors.Is(err, want) {
		t.Fatalf("lost validation cause: %v", err)
	}
	if err := conn.PingContext(context.Background()); !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("pinned conn error=%v", err)
	}
	sessionBackendGone(t, db, pid)
}

func TestRuntimeLockerMissingGooseLockPoisonsBeforeValidator(t *testing.T) {
	_, db := newSessionTestDatabase(t)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	registerSessionConnCleanup(t, conn)
	var pid int32
	if err := conn.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	validated := false
	locker, err := newRuntimeSessionLocker(LocalAdminMigratorIdentity(), &recordingSessionLocker{events: &[]string{}}, func(context.Context, *sql.Conn, int32) error { validated = true; return nil })
	if err != nil {
		t.Fatal(err)
	}
	err = locker.SessionLock(context.Background(), conn)
	if err == nil || validated {
		t.Fatalf("error=%v validated=%t", err, validated)
	}
	if err := conn.PingContext(context.Background()); !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("pinned conn error=%v", err)
	}
	sessionBackendGone(t, db, pid)
}

func TestRuntimeLockerGooseContentionCleansRuntimeAndPoisons(t *testing.T) {
	_, db := newSessionTestDatabase(t)
	db.SetMaxOpenConns(2)
	holder, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	registerSessionConnCleanup(t, holder)
	if _, err := holder.ExecContext(context.Background(), `SELECT pg_advisory_lock($1)`, LockID); err != nil {
		t.Fatal(err)
	}
	registerSessionAdvisoryUnlock(t, holder, LockID)
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	registerSessionConnCleanup(t, conn)
	var pid int32
	if err := conn.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	gooseLocker, err := lock.NewPostgresSessionLocker(lock.WithLockID(LockID), lock.WithLockTimeout(1, 1))
	if err != nil {
		t.Fatal(err)
	}
	locker, err := newRuntimeSessionLocker(LocalAdminMigratorIdentity(), gooseLocker, func(context.Context, *sql.Conn, int32) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = locker.SessionLock(ctx, conn)
	if err == nil {
		t.Fatal("Goose contention succeeded")
	}
	if err := conn.PingContext(context.Background()); !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("pinned conn error=%v", err)
	}
	sessionBackendGone(t, db, pid)
}

func TestRuntimeLockerUnlockAmbiguityPoisonsAfterReverseCleanup(t *testing.T) {
	_, db := newSessionTestDatabase(t)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	registerSessionConnCleanup(t, conn)
	var pid int32
	if err := conn.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	gooseLocker, err := lock.NewPostgresSessionLocker(lock.WithLockID(LockID), lock.WithLockTimeout(1, 1), lock.WithUnlockTimeout(1, 1))
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("simulated lost Goose unlock response")
	wrapped := &recordingSessionLocker{events: &[]string{}, delegate: gooseLocker, unlockErr: want}
	locker, err := newRuntimeSessionLocker(LocalAdminMigratorIdentity(), wrapped, func(context.Context, *sql.Conn, int32) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := locker.SessionLock(context.Background(), conn); err != nil {
		t.Fatal(err)
	}
	err = locker.SessionUnlock(context.Background(), conn)
	if !errors.Is(err, want) {
		t.Fatalf("lost unlock cause: %v", err)
	}
	if err := conn.PingContext(context.Background()); !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("pinned conn error=%v", err)
	}
	sessionBackendGone(t, db, pid)
}

func TestRuntimeLockerExitAndResetFailuresPoison(t *testing.T) {
	tests := []struct{ name, statement string }{
		{"runtime exit", `SELECT public.runtime_exit_migrator()`},
		{"identity reset", `RESET SESSION AUTHORIZATION`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, db := newSessionTestDatabase(t)
			db.SetMaxOpenConns(1)
			db.SetMaxIdleConns(1)
			conn, err := db.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			registerSessionConnCleanup(t, conn)
			var pid int32
			if err := conn.QueryRowContext(context.Background(), `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				t.Fatal(err)
			}
			gooseLocker, err := lock.NewPostgresSessionLocker(lock.WithLockID(LockID), lock.WithLockTimeout(1, 1), lock.WithUnlockTimeout(1, 1))
			if err != nil {
				t.Fatal(err)
			}
			locker, err := newRuntimeSessionLocker(LocalAdminMigratorIdentity(), gooseLocker, func(context.Context, *sql.Conn, int32) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			if err := locker.SessionLock(context.Background(), conn); err != nil {
				t.Fatal(err)
			}
			want := errors.New("simulated cleanup response loss")
			realExec := locker.exec
			locker.exec = func(ctx context.Context, got *sql.Conn, statement string) (sql.Result, error) {
				if statement == tt.statement {
					return nil, want
				}
				return realExec(ctx, got, statement)
			}
			err = locker.SessionUnlock(context.Background(), conn)
			if !errors.Is(err, want) {
				t.Fatalf("lost cleanup cause: %v", err)
			}
			if err := conn.PingContext(context.Background()); !errors.Is(err, sql.ErrConnDone) {
				t.Fatalf("pinned conn error=%v", err)
			}
			sessionBackendGone(t, db, pid)
		})
	}
}

func TestRuntimeLockerRejectsInvalidConstructorInputs(t *testing.T) {
	valid := DirectMigratorIdentity()
	callback := func(context.Context, *sql.Conn, int32) error { return nil }
	tests := []struct {
		name     string
		identity MigrationIdentity
		wrapped  lock.SessionLocker
		validate runtimePostLockValidator
	}{
		{"zero identity", MigrationIdentity{}, &recordingSessionLocker{}, callback},
		{"nil locker", valid, nil, callback},
		{"nil validator", valid, &recordingSessionLocker{}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := newRuntimeSessionLocker(tt.identity, tt.wrapped, tt.validate); err == nil {
				t.Fatal("constructor accepted invalid input")
			}
		})
	}
	locker, err := newRuntimeSessionLocker(valid, &recordingSessionLocker{events: &[]string{}}, callback)
	if err != nil {
		t.Fatal(err)
	}
	if err := locker.SessionLock(context.Background(), nil); err == nil {
		t.Fatal("SessionLock accepted nil connection")
	}
}

func assertMigrationEvents(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events=%v want=%v", got, want)
	}
}

func registerSessionConnCleanup(t *testing.T, conn *sql.Conn) {
	t.Helper()
	t.Cleanup(func() {
		err := conn.Close()
		if err != nil && !errors.Is(err, sql.ErrConnDone) {
			t.Errorf("close pinned connection: %v", err)
		}
	})
}

func registerSessionAdvisoryUnlock(t *testing.T, conn *sql.Conn, lockID int64) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, lockID); err != nil && !errors.Is(err, sql.ErrConnDone) {
			t.Errorf("release advisory lock %d: %v", lockID, err)
		}
	})
}
