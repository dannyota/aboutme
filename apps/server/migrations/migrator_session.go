package migrations

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pressly/goose/v3/lock"
)

const (
	runtimeMigrationLockID      = int64(3727108639518528074)
	runtimeLockerCleanupTimeout = 5 * time.Second
)

type runtimePostLockValidator func(context.Context, *sql.Conn, int32) error
type runtimeExecFunc func(context.Context, *sql.Conn, string) (sql.Result, error)

type runtimeSessionLocker struct {
	identity MigrationIdentity
	wrapped  lock.SessionLocker
	validate runtimePostLockValidator
	exec     runtimeExecFunc

	mu         sync.Mutex
	activeConn *sql.Conn
	backendPID int32
}

func newRuntimeSessionLocker(identity MigrationIdentity, wrapped lock.SessionLocker, validate runtimePostLockValidator) (*runtimeSessionLocker, error) {
	if !identity.valid() {
		return nil, errors.New("migrations: invalid migration identity")
	}
	if wrapped == nil {
		return nil, errors.New("migrations: nil Goose session locker")
	}
	if validate == nil {
		return nil, errors.New("migrations: nil post-lock validator")
	}
	return &runtimeSessionLocker{identity: identity, wrapped: wrapped, validate: validate, exec: func(ctx context.Context, conn *sql.Conn, statement string) (sql.Result, error) {
		return conn.ExecContext(ctx, statement)
	}}, nil
}

// SessionLock establishes runtime entry before the wrapped Goose lock.
func (l *runtimeSessionLocker) SessionLock(ctx context.Context, conn *sql.Conn) error {
	if conn == nil {
		return errors.New("migrations: nil pinned connection")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.activeConn != nil {
		return errors.New("migrations: runtime session locker already active")
	}

	pid, err := l.establishIdentity(ctx, conn)
	if err != nil {
		return err
	}
	if _, err = l.exec(ctx, conn, `SELECT public.runtime_enter_migrator()`); err != nil {
		primary := fmt.Errorf("migrations: enter runtime migrator session: %w", err)
		if sqlState(err) == "55000" {
			cleanupErr := withRuntimeCleanupContext(ctx, func(cleanupCtx context.Context) error {
				return l.verifySessionLocksAbsent(cleanupCtx, conn, pid)
			})
			cleanupErr = errors.Join(cleanupErr, withRuntimeCleanupContext(ctx, func(cleanupCtx context.Context) error {
				return l.resetLocalIdentity(cleanupCtx, conn, pid)
			}))
			if cleanupErr == nil {
				return primary
			}
			poisonSQLConn(conn)
			return errors.Join(primary, cleanupErr)
		}
		poisonSQLConn(conn)
		return primary
	}
	if err = l.wrapped.SessionLock(ctx, conn); err != nil {
		primary := fmt.Errorf("migrations: acquire Goose session lock: %w", err)
		cleanupErr := withRuntimeCleanupContext(ctx, func(cleanupCtx context.Context) error {
			return l.exitRuntime(cleanupCtx, conn, pid)
		})
		cleanupErr = errors.Join(cleanupErr, withRuntimeCleanupContext(ctx, func(cleanupCtx context.Context) error {
			return l.resetLocalIdentity(cleanupCtx, conn, pid)
		}))
		poisonSQLConn(conn)
		return errors.Join(primary, cleanupErr)
	}
	if err = l.verifyLocksHeld(ctx, conn, pid); err != nil {
		primary := fmt.Errorf("migrations: verify acquired migration locks: %w", err)
		cleanupErr := l.cleanUnlock(ctx, conn, pid)
		poisonSQLConn(conn)
		return errors.Join(primary, cleanupErr)
	}
	if err = l.validate(ctx, conn, pid); err != nil {
		primary := fmt.Errorf("migrations: validate locked migration state: %w", err)
		cleanupErr := l.cleanUnlock(ctx, conn, pid)
		poisonSQLConn(conn)
		return errors.Join(primary, cleanupErr)
	}
	l.activeConn = conn
	l.backendPID = pid
	return nil
}

// SessionUnlock releases Goose and runtime locks in reverse order.
func (l *runtimeSessionLocker) SessionUnlock(ctx context.Context, conn *sql.Conn) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if conn == nil || l.activeConn == nil || conn != l.activeConn {
		active := l.activeConn
		l.activeConn = nil
		l.backendPID = 0
		if active != nil {
			poisonSQLConn(active)
		}
		if conn != nil && conn != active {
			poisonSQLConn(conn)
		}
		return errors.New("migrations: runtime session unlock connection mismatch")
	}
	pid := l.backendPID
	l.activeConn = nil
	l.backendPID = 0
	if err := l.cleanUnlock(ctx, conn, pid); err != nil {
		poisonSQLConn(conn)
		return err
	}
	return nil
}

func (l *runtimeSessionLocker) cleanUnlock(ctx context.Context, conn *sql.Conn, pid int32) error {
	var result error
	if err := withRuntimeCleanupContext(ctx, func(cleanupCtx context.Context) error {
		return l.wrapped.SessionUnlock(cleanupCtx, conn)
	}); err != nil {
		result = errors.Join(result, fmt.Errorf("migrations: release Goose session lock: %w", err))
	}
	result = errors.Join(result, withRuntimeCleanupContext(ctx, func(cleanupCtx context.Context) error {
		return l.exitRuntime(cleanupCtx, conn, pid)
	}))
	result = errors.Join(result, withRuntimeCleanupContext(ctx, func(cleanupCtx context.Context) error {
		return l.resetLocalIdentity(cleanupCtx, conn, pid)
	}))
	return result
}

func (l *runtimeSessionLocker) establishIdentity(ctx context.Context, conn *sql.Conn) (int32, error) {
	var sessionUser, superuser string
	var pid int32
	if err := conn.QueryRowContext(ctx, `SELECT session_user,current_setting('is_superuser'),pg_backend_pid()`).Scan(&sessionUser, &superuser, &pid); err != nil {
		poisonSQLConn(conn)
		return 0, fmt.Errorf("migrations: verify migration identity: %w", err)
	}
	switch l.identity.kind {
	case migrationIdentityDirect:
		if sessionUser != "aboutme_migrator" || superuser != "off" {
			return 0, fmt.Errorf("migrations: direct identity session_user=%q is_superuser=%q", sessionUser, superuser)
		}
	case migrationIdentityLocalAdmin:
		if sessionUser != "aboutme" || superuser != "on" {
			return 0, fmt.Errorf("migrations: local identity session_user=%q is_superuser=%q", sessionUser, superuser)
		}
		if _, err := l.exec(ctx, conn, `SET SESSION AUTHORIZATION aboutme_migrator`); err != nil {
			poisonSQLConn(conn)
			return 0, fmt.Errorf("migrations: assume fixed migrator identity: %w", err)
		}
		if err := verifySessionIdentity(ctx, conn, pid, "aboutme_migrator", "off"); err != nil {
			poisonSQLConn(conn)
			return 0, err
		}
	default:
		return 0, errors.New("migrations: invalid migration identity")
	}
	return pid, nil
}

func (l *runtimeSessionLocker) exitRuntime(ctx context.Context, conn *sql.Conn, pid int32) error {
	if err := verifySessionIdentity(ctx, conn, pid, "aboutme_migrator", "off"); err != nil {
		return err
	}
	if _, err := l.exec(ctx, conn, `SELECT public.runtime_exit_migrator()`); err != nil {
		return fmt.Errorf("migrations: exit runtime migrator session: %w", err)
	}
	return l.verifySessionLocksAbsent(ctx, conn, pid)
}

func (l *runtimeSessionLocker) verifySessionLocksAbsent(ctx context.Context, conn *sql.Conn, pid int32) error {
	if err := verifySessionIdentity(ctx, conn, pid, "aboutme_migrator", "off"); err != nil {
		return err
	}
	var absent bool
	if err := conn.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM pg_locks WHERE pid=pg_backend_pid() AND locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND objsubid=1 AND ((classid::bigint<<32)|objid::bigint) IN ($1,$2))`, runtimeMigrationLockID, LockID).Scan(&absent); err != nil {
		return fmt.Errorf("migrations: verify session lock absence: %w", err)
	}
	if !absent {
		return errors.New("migrations: migration session lock remains held")
	}
	return nil
}

func (l *runtimeSessionLocker) verifyLocksHeld(ctx context.Context, conn *sql.Conn, pid int32) error {
	if err := verifySessionIdentity(ctx, conn, pid, "aboutme_migrator", "off"); err != nil {
		return err
	}
	var runtimeHeld, gooseHeld bool
	err := conn.QueryRowContext(ctx, `SELECT
EXISTS(SELECT 1 FROM pg_locks WHERE pid=pg_backend_pid() AND locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND objsubid=1 AND ((classid::bigint<<32)|objid::bigint)=$1 AND mode='ShareLock' AND granted),
EXISTS(SELECT 1 FROM pg_locks WHERE pid=pg_backend_pid() AND locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND objsubid=1 AND ((classid::bigint<<32)|objid::bigint)=$2 AND mode='ExclusiveLock' AND granted)`, runtimeMigrationLockID, LockID).Scan(&runtimeHeld, &gooseHeld)
	if err != nil {
		return fmt.Errorf("migrations: inspect acquired migration locks: %w", err)
	}
	if !runtimeHeld || !gooseHeld {
		return fmt.Errorf("migrations: required locks missing runtime=%t goose=%t", runtimeHeld, gooseHeld)
	}
	return nil
}

func (l *runtimeSessionLocker) resetLocalIdentity(ctx context.Context, conn *sql.Conn, pid int32) error {
	if l.identity.kind != migrationIdentityLocalAdmin {
		return verifySessionIdentity(ctx, conn, pid, "aboutme_migrator", "off")
	}
	if _, err := l.exec(ctx, conn, `RESET SESSION AUTHORIZATION`); err != nil {
		return fmt.Errorf("migrations: reset local migration identity: %w", err)
	}
	return verifySessionIdentity(ctx, conn, pid, "aboutme", "on")
}

func verifySessionIdentity(ctx context.Context, conn *sql.Conn, pid int32, wantUser, wantSuperuser string) error {
	var user, superuser string
	var gotPID int32
	if err := conn.QueryRowContext(ctx, `SELECT session_user,current_setting('is_superuser'),pg_backend_pid()`).Scan(&user, &superuser, &gotPID); err != nil {
		return fmt.Errorf("migrations: verify pinned identity: %w", err)
	}
	if gotPID != pid || user != wantUser || superuser != wantSuperuser {
		return fmt.Errorf("migrations: pinned identity mismatch pid=%d user=%q superuser=%q", gotPID, user, superuser)
	}
	return nil
}

func runtimeCleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), runtimeLockerCleanupTimeout)
}

func withRuntimeCleanupContext(ctx context.Context, cleanup func(context.Context) error) error {
	cleanupCtx, cancel := runtimeCleanupContext(ctx)
	defer cancel()
	return cleanup(cleanupCtx)
}

func poisonSQLConn(conn *sql.Conn) {
	if conn == nil {
		return
	}
	ignoreMigrationCleanupError(conn.Raw(func(any) error { return driver.ErrBadConn }))
	ignoreMigrationCleanupError(conn.Close())
}

func ignoreMigrationCleanupError(error) {}

func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}
