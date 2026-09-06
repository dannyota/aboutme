package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/pressly/goose/v3/lock"
)

type bootstrapSessionLocker struct {
	identity       MigrationIdentity
	wrapped        lock.SessionLocker
	stayAdmin      bool
	allowAbsent    bool
	wantOwner      string
	mu             sync.Mutex
	active         *sql.Conn
	pid            int32
	handoffToken   error
	completedClean bool
	backend        *migrationBackend
}

// SessionLock establishes the fixed bootstrap identity before taking Goose's lock.
func (l *bootstrapSessionLocker) SessionLock(ctx context.Context, conn *sql.Conn) (resultErr error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active != nil {
		return errors.New("migrations: bootstrap locker already active")
	}
	l.completedClean = false
	backend, err := captureMigrationBackend(conn)
	if err != nil {
		return err
	}
	owned := true
	defer func() {
		if owned {
			resultErr = errors.Join(resultErr, backend.retire(ctx, conn))
		}
	}()
	if verifyErr := verifyMigrationDriver(conn); verifyErr != nil {
		return verifyErr
	}
	var user, superuser string
	var pid int32
	if identityErr := conn.QueryRowContext(ctx, `SELECT session_user,current_setting('is_superuser'),pg_backend_pid()`).Scan(&user, &superuser, &pid); identityErr != nil {
		return fmt.Errorf("migrations: verify bootstrap identity: %w", identityErr)
	}
	switch l.identity.kind {
	case migrationIdentityDirect:
		if user != "aboutme_migrator" || superuser != "off" || l.stayAdmin {
			return errors.New("migrations: direct bootstrap identity mismatch")
		}
	case migrationIdentityLocalAdmin:
		if user != "aboutme" || superuser != "on" {
			return errors.New("migrations: local bootstrap identity mismatch")
		}
		if !l.stayAdmin {
			if _, assumeErr := conn.ExecContext(ctx, `SET SESSION AUTHORIZATION aboutme_migrator`); assumeErr != nil {
				return fmt.Errorf("migrations: assume bootstrap migrator identity: %w", assumeErr)
			}
		}
	default:
		return errors.New("migrations: invalid bootstrap identity")
	}
	if lockErr := l.wrapped.SessionLock(ctx, conn); lockErr != nil {
		return fmt.Errorf("migrations: acquire bootstrap Goose lock: %w", lockErr)
	}
	state, err := readMigrationCatalogState(ctx, conn)
	if err == nil {
		if state.runtimeExists {
			err = errBootstrapStateTransition
		} else if (!state.historyExists && !l.allowAbsent) || (state.historyExists && state.historyOwner != l.wantOwner) {
			err = ErrMigrationHistoryCorrupt
		} else if state.historyExists {
			var head int64
			head, err = readHistoryHead(ctx, conn)
			if err == nil && head > 12 {
				err = ErrMigrationHistoryCorrupt
			} else if err == nil {
				err = validateUnprotectedHistory(ctx, conn, state.historyOwner, head)
			}
		}
	}
	if err != nil {
		cleanupErr := withRuntimeCleanupContext(ctx, func(cleanupCtx context.Context) error {
			var cleanup error
			cleanup = errors.Join(cleanup, l.wrapped.SessionUnlock(cleanupCtx, conn))
			if l.identity.kind == migrationIdentityLocalAdmin && !l.stayAdmin {
				_, resetErr := conn.ExecContext(cleanupCtx, `RESET SESSION AUTHORIZATION`)
				cleanup = errors.Join(cleanup, resetErr)
				if resetErr == nil {
					cleanup = errors.Join(cleanup, verifySessionIdentity(cleanupCtx, conn, pid, "aboutme", "on"))
				}
			}
			cleanup = errors.Join(cleanup, backend.retire(cleanupCtx, conn))
			return cleanup
		})
		owned = false
		if err == errBootstrapStateTransition && cleanupErr == nil { //nolint:errorlint // This internal sentinel is assigned directly above.
			l.handoffToken = fmt.Errorf("bootstrap foundation handoff pid %d", pid)
			l.completedClean = true
			return l.handoffToken
		}
		return errors.Join(err, cleanupErr)
	}
	l.active, l.pid, l.backend = conn, pid, backend
	owned = false
	return nil
}

// SessionUnlock releases Goose, resets local identity, and retires the backend.
func (l *bootstrapSessionLocker) SessionUnlock(ctx context.Context, conn *sql.Conn) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if conn == nil || conn != l.active {
		active := l.active
		backend := l.backend
		l.active, l.pid, l.backend = nil, 0, nil
		var retirementErr error
		if active != nil && backend != nil {
			retirementErr = backend.retire(ctx, active)
		}
		return errors.Join(errors.New("migrations: bootstrap unlock connection mismatch"), retirementErr)
	}
	pid := l.pid
	backend := l.backend
	l.active, l.pid = nil, 0
	l.backend = nil
	cleanupCtx, cancel := runtimeCleanupContext(ctx)
	defer cancel()
	var result error
	if err := l.wrapped.SessionUnlock(cleanupCtx, conn); err != nil {
		result = errors.Join(result, err)
	}
	if l.identity.kind == migrationIdentityLocalAdmin && !l.stayAdmin {
		if _, err := conn.ExecContext(cleanupCtx, `RESET SESSION AUTHORIZATION`); err != nil {
			result = errors.Join(result, err)
		} else {
			result = errors.Join(result, verifySessionIdentity(cleanupCtx, conn, pid, "aboutme", "on"))
		}
	}
	result = errors.Join(result, backend.retire(ctx, conn))
	l.completedClean = result == nil
	return result
}
