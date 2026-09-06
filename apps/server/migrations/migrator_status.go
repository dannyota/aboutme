//nolint:govet // Sequential migration cleanup keeps each error next to its operation.
package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pressly/goose/v3"
)

func statusFS(ctx context.Context, db *sql.DB, fsys fs.FS, identity MigrationIdentity) (statuses []*goose.MigrationStatus, resultErr error) {
	if db == nil || !identity.valid() {
		return nil, errors.New("migrations: invalid Status arguments")
	}
	if err := validateProtectedMigrationSources(fsys); err != nil {
		return nil, err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrations: acquire status connection: %w", err)
	}
	backend, err := captureMigrationBackend(conn)
	if err != nil {
		ignoreMigrationCleanupError(conn.Close())
		return nil, err
	}
	clean := false
	defer func() {
		resultErr = errors.Join(resultErr, finalizeMigrationBackend(ctx, backend, conn, clean))
	}()
	if err := verifyMigrationDriver(conn); err != nil {
		return nil, err
	}
	state, err := readMigrationCatalogState(ctx, conn)
	if err != nil {
		return nil, err
	}
	stayAdmin := state.historyOwner == "aboutme" || (!state.historyExists && identity.kind == migrationIdentityLocalAdmin)
	pid, err := establishStatusIdentity(ctx, conn, identity, stayAdmin)
	if err != nil {
		return nil, err
	}
	if _, err := conn.ExecContext(ctx, `BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY`); err != nil {
		return nil, fmt.Errorf("migrations: begin read-only status snapshot: %w", err)
	}
	transactionActive := true
	defer func() {
		if transactionActive {
			if rollbackErr := rollbackStatusConnection(ctx, conn); rollbackErr != nil {
				resultErr = errors.Join(resultErr, rollbackErr)
			} else if resetErr := finishStatusIdentity(ctx, conn, identity, stayAdmin, pid); resetErr != nil {
				resultErr = errors.Join(resultErr, resetErr)
			} else {
				clean = true
			}
		}
	}()
	lockedState, err := readMigrationCatalogState(ctx, conn)
	if err != nil {
		return nil, err
	}
	if lockedState != state {
		return nil, ErrMigrationHistoryCorrupt
	}
	if !lockedState.historyExists {
		if lockedState.runtimeExists {
			return nil, ErrMigrationHistoryCorrupt
		}
		if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
			transactionActive = false
			return nil, err
		}
		transactionActive = false
		if err := finishStatusIdentity(ctx, conn, identity, stayAdmin, pid); err != nil {
			return nil, err
		}
		clean = true
		return nil, ErrMigrationHistoryMissing
	}
	head, err := readHistoryHead(ctx, conn)
	if err != nil {
		return nil, err
	}
	if stayAdmin {
		if err := validateUnprotectedHistory(ctx, conn, state.historyOwner, head); err != nil {
			return nil, err
		}
		if identity.kind != migrationIdentityLocalAdmin || (!state.runtimeExists && head > 12) {
			return nil, ErrMigrationHistoryCorrupt
		}
		if state.runtimeExists {
			var gate, owner string
			var generation int64
			var enforcement int16
			if err := conn.QueryRowContext(ctx, `SELECT write_gate,generation,migrator_enforcement_version,migration_history_owner FROM public.runtime_write_state WHERE singleton`).Scan(&gate, &generation, &enforcement, &owner); err != nil {
				return nil, err
			}
			if (gate != "open" && gate != "closing" && gate != "closed") || generation < 1 || enforcement != 0 || owner != "aboutme" || head != 13 {
				return nil, ErrMigrationHistoryCorrupt
			}
		}
	} else {
		if !state.runtimeExists {
			if err := validateUnprotectedHistory(ctx, conn, state.historyOwner, head); err != nil {
				return nil, err
			}
			if head > 12 || state.historyOwner != "aboutme_migrator" {
				return nil, ErrMigrationHistoryCorrupt
			}
		} else if err := validateProtectedHistory(ctx, conn, true, false); err != nil {
			return nil, err
		}
	}
	statuses, err = buildMigrationStatuses(ctx, conn, fsys)
	if err != nil {
		return nil, err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		primary := fmt.Errorf("migrations: commit read-only status snapshot: %w", err)
		transactionActive = false
		return nil, primary
	}
	transactionActive = false
	if err := finishStatusIdentity(ctx, conn, identity, stayAdmin, pid); err != nil {
		return nil, err
	}
	clean = true
	return statuses, nil
}

func establishStatusIdentity(ctx context.Context, conn *sql.Conn, identity MigrationIdentity, stayAdmin bool) (int32, error) {
	var user, superuser string
	var pid int32
	if err := conn.QueryRowContext(ctx, `SELECT session_user,current_setting('is_superuser'),pg_backend_pid()`).Scan(&user, &superuser, &pid); err != nil {
		return 0, err
	}
	if identity.kind == migrationIdentityDirect {
		if user != "aboutme_migrator" || superuser != "off" || stayAdmin {
			return 0, errors.New("migrations: direct status identity mismatch")
		}
		return pid, nil
	}
	if user != "aboutme" || superuser != "on" {
		return 0, errors.New("migrations: local status identity mismatch")
	}
	if !stayAdmin {
		if _, err := conn.ExecContext(ctx, `SET SESSION AUTHORIZATION aboutme_migrator`); err != nil {
			return 0, err
		}
		if err := verifySessionIdentity(ctx, conn, pid, "aboutme_migrator", "off"); err != nil {
			return 0, err
		}
	}
	return pid, nil
}

func finishStatusIdentity(ctx context.Context, conn *sql.Conn, identity MigrationIdentity, stayAdmin bool, pid int32) error {
	cleanupCtx, cancel := runtimeCleanupContext(ctx)
	defer cancel()
	if identity.kind == migrationIdentityLocalAdmin && !stayAdmin {
		if _, err := conn.ExecContext(cleanupCtx, `RESET SESSION AUTHORIZATION`); err != nil {
			return err
		}
		return verifySessionIdentity(cleanupCtx, conn, pid, "aboutme", "on")
	}
	want, superuser := "aboutme_migrator", "off"
	if stayAdmin {
		want, superuser = "aboutme", "on"
	}
	return verifySessionIdentity(cleanupCtx, conn, pid, want, superuser)
}

func buildMigrationStatuses(ctx context.Context, q manifestQueryer, fsys fs.FS) ([]*goose.MigrationStatus, error) {
	sources, err := migrationSourcesFromFS(fsys)
	if err != nil {
		return nil, err
	}
	type appliedState struct {
		applied bool
		at      time.Time
	}
	applied := map[int64]appliedState{}
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT ON (version_id) version_id,is_applied,tstamp FROM public.goose_db_version ORDER BY version_id,id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var version int64
		var state appliedState
		if err := rows.Scan(&version, &state.applied, &state.at); err != nil {
			return nil, err
		}
		applied[version] = state
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	statuses := make([]*goose.MigrationStatus, 0, len(sources))
	for _, source := range sources {
		status := &goose.MigrationStatus{Source: source, State: goose.StatePending}
		if state := applied[source.Version]; state.applied {
			status.State, status.AppliedAt = goose.StateApplied, state.at
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func migrationSourcesFromFS(fsys fs.FS) ([]*goose.Source, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("migrations: read migration sources: %w", err)
	}
	var sources []*goose.Source
	for _, entry := range entries {
		name := entry.Name()
		separator := strings.IndexByte(name, '_')
		if entry.IsDir() || separator < 1 || !strings.HasSuffix(name, ".sql") {
			continue
		}
		version, err := strconv.ParseInt(name[:separator], 10, 64)
		if err != nil || version < 1 {
			return nil, fmt.Errorf("migrations: invalid SQL migration source %q", name)
		}
		sources = append(sources, &goose.Source{Type: goose.TypeSQL, Path: name, Version: version})
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Version < sources[j].Version })
	for index := 1; index < len(sources); index++ {
		if sources[index-1].Version == sources[index].Version {
			return nil, fmt.Errorf("migrations: duplicate migration version %d", sources[index].Version)
		}
	}
	return sources, nil
}

func rollbackStatusConnection(ctx context.Context, conn *sql.Conn) error {
	cleanupCtx, cancel := runtimeCleanupContext(ctx)
	defer cancel()
	if _, err := conn.ExecContext(cleanupCtx, `ROLLBACK`); err != nil {
		return fmt.Errorf("migrations: rollback status snapshot: %w", err)
	}
	return nil
}
