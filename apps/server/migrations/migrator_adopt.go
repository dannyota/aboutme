//nolint:govet // Sequential migration cleanup keeps each error next to its operation.
package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/pressly/goose/v3/lock"
)

// AdoptHistoryOwner transfers the fixed local version-13 manifest to runtime_owner.
func AdoptHistoryOwner(ctx context.Context, db *sql.DB, lockOpts ...lock.SessionLockerOption) (result AdoptionResult, resultErr error) {
	if db == nil {
		return result, errors.New("migrations: nil adoption database")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return result, err
	}
	backend, err := captureMigrationBackend(conn)
	if err != nil {
		ignoreMigrationCleanupError(conn.Close())
		return result, err
	}
	backendOwned := true
	defer func() {
		if backendOwned {
			resultErr = errors.Join(resultErr, finalizeMigrationBackend(ctx, backend, conn, false))
		}
	}()
	clean := false
	if err := verifyMigrationDriver(conn); err != nil {
		return result, err
	}
	var user, superuser, database string
	var pid int32
	if err := conn.QueryRowContext(ctx, `SELECT session_user,current_setting('is_superuser'),current_database(),pg_backend_pid()`).Scan(&user, &superuser, &database, &pid); err != nil {
		return result, err
	}
	if user != "aboutme" || superuser != "on" || !localMigrationDatabasePattern.MatchString(database) {
		return result, errors.New("migrations: history adoption authority or database class mismatch")
	}
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock_shared($1)`, runtimeMigrationLockID); err != nil {
		return result, fmt.Errorf("migrations: acquire adoption runtime lock: %w", err)
	}
	wrapped, err := lock.NewPostgresSessionLocker(append([]lock.SessionLockerOption{lock.WithLockID(LockID)}, lockOpts...)...)
	if err != nil {
		return result, err
	}
	if err := wrapped.SessionLock(ctx, conn); err != nil {
		return result, fmt.Errorf("migrations: acquire adoption Goose lock: %w", err)
	}
	locked := true
	if _, err := conn.ExecContext(ctx, `BEGIN`); err != nil {
		return result, err
	}
	transactionActive := true
	defer func() {
		if transactionActive {
			if rollbackErr := rollbackStatusConnection(ctx, conn); rollbackErr != nil {
				resultErr = errors.Join(resultErr, rollbackErr)
				locked = false
			}
		}
	}()
	if err := lockVersion13ManifestTables(ctx, conn); err != nil {
		return result, err
	}
	state, err := readMigrationCatalogState(ctx, conn)
	if err != nil {
		return result, err
	}
	if !state.runtimeExists || !state.historyExists {
		return result, ErrMigrationHistoryCorrupt
	}
	var gate, recordedOwner string
	var generation int64
	var enforcement int16
	if err := conn.QueryRowContext(ctx, `SELECT write_gate,generation,migrator_enforcement_version,migration_history_owner FROM public.runtime_write_state WHERE singleton`).Scan(&gate, &generation, &enforcement, &recordedOwner); err != nil {
		return result, err
	}
	if gate != "open" || generation < 1 {
		return result, ErrMigrationHistoryCorrupt
	}
	head, err := readHistoryHead(ctx, conn)
	if err != nil {
		return result, err
	}
	if state.historyOwner == "aboutme_runtime_owner" && recordedOwner == "aboutme_runtime_owner" {
		if enforcement != 0 || head != 13 {
			return result, ErrMigrationHistoryCorrupt
		}
		if err := validateUnprotectedHistory(ctx, conn, "aboutme_runtime_owner", head); err != nil {
			return result, err
		}
		if err := validateProvisioning(ctx, conn); err != nil {
			return result, err
		}
		if _, err := validateVersion13Manifest(ctx, conn, "aboutme_runtime_owner"); err != nil {
			return result, err
		}
		if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
			transactionActive = false
			retireErr := finalizeMigrationBackend(ctx, backend, conn, false)
			backendOwned = false
			return result, errors.Join(err, retireErr)
		}
		transactionActive = false
		result.outcome = AlreadyConverged
		return result, finishAdoptionLocks(ctx, conn, wrapped, pid, &locked, &clean)
	}
	if state.historyOwner != "aboutme" || recordedOwner != "aboutme" || enforcement != 0 || head != 13 {
		return result, ErrMigrationHistoryCorrupt
	}
	if err := validateUnprotectedHistory(ctx, conn, "aboutme", head); err != nil {
		return result, err
	}
	if err := validateProvisioningACLBounds(ctx, conn); err != nil {
		return result, err
	}
	before, err := validateVersion13Manifest(ctx, conn, "aboutme")
	if err != nil {
		return result, err
	}
	historyEvidence, foundationEvidence, err := readAdoptionEvidence(ctx, conn)
	if err != nil {
		return result, err
	}
	if err := installProvisioning(ctx, conn, database); err != nil {
		return result, err
	}
	if _, err := conn.ExecContext(ctx, `GRANT CREATE ON SCHEMA public TO aboutme_runtime_owner`); err != nil {
		return result, err
	}
	for _, statement := range fixedVersion13AlterStatements() {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return result, fmt.Errorf("migrations: transfer fixed manifest object: %w", err)
		}
	}
	if _, err := conn.ExecContext(ctx, `SET LOCAL ROLE aboutme_runtime_owner; REVOKE ALL ON TABLE public.goose_db_version FROM PUBLIC,aboutme_app,aboutme_maintenance,aboutme_lifecycle_command,aboutme_fencing_proof,aboutme_migrator; GRANT SELECT,INSERT ON TABLE public.goose_db_version TO aboutme_migrator; RESET ROLE`); err != nil {
		return result, err
	}
	if _, err := conn.ExecContext(ctx, `REVOKE CREATE ON SCHEMA public FROM aboutme_runtime_owner`); err != nil {
		return result, err
	}
	after, err := validateVersion13Manifest(ctx, conn, "aboutme_runtime_owner")
	if err != nil {
		return result, err
	}
	if err := validateAdoptedGooseACL(ctx, conn); err != nil {
		return result, err
	}
	allowedACL := map[string]string{}
	for _, evidence := range after.objects {
		if evidence.object.kind == "table" && evidence.object.identity == "goose_db_version" {
			allowedACL[manifestObjectKey("table", "public", "goose_db_version")] = evidence.acl
		}
	}
	if err := compareVersion13ManifestSnapshots(before, after, "aboutme", "aboutme_runtime_owner", allowedACL); err != nil {
		return result, err
	}
	var singleton bool
	if err := conn.QueryRowContext(ctx, `SELECT singleton FROM public.runtime_write_state WHERE singleton FOR UPDATE`).Scan(&singleton); err != nil || !singleton {
		return result, err
	}
	var nowAt time.Time
	if err := conn.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&nowAt); err != nil {
		return result, err
	}
	if _, err := conn.ExecContext(ctx, `UPDATE public.runtime_write_state SET generation=generation+1,last_write_at=GREATEST(last_write_at,$1),last_writer_kind='migrator',last_writer_operation_id='migration-history-adoption',updated_at=GREATEST(updated_at,$1),migration_history_owner='aboutme_runtime_owner' WHERE singleton`, nowAt); err != nil {
		return result, err
	}
	if err := validateProvisioning(ctx, conn); err != nil {
		return result, err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		transactionActive = false
		cause := fmt.Errorf("migrations: adoption commit outcome unknown: %w", err)
		retireErr := finalizeMigrationBackend(ctx, backend, conn, false)
		backendOwned = false
		locked = false
		if retireErr == nil {
			reconcileErr := reconcileAdoption(ctx, db, database, pid, generation, before, historyEvidence, foundationEvidence)
			if reconcileErr == nil {
				return AdoptionResult{outcome: ReconciledConverged, ambiguousCause: cause}, nil
			}
			return result, errors.Join(cause, reconcileErr)
		}
		return result, errors.Join(cause, retireErr)
	}
	transactionActive = false
	result.outcome = Applied
	if cleanupErr := finishAdoptionLocks(ctx, conn, wrapped, pid, &locked, &clean); cleanupErr != nil {
		retireErr := finalizeMigrationBackend(ctx, backend, conn, false)
		backendOwned = false
		locked = false
		cause := errors.Join(cleanupErr, retireErr)
		if retireErr == nil {
			reconcileErr := reconcileAdoption(ctx, db, database, pid, generation, before, historyEvidence, foundationEvidence)
			if reconcileErr == nil {
				return AdoptionResult{outcome: ReconciledConverged, ambiguousCause: cause}, nil
			}
			return AdoptionResult{}, errors.Join(cause, reconcileErr)
		}
		return AdoptionResult{}, cause
	}
	return result, nil
}
func validateAdoptedGooseACL(ctx context.Context, q manifestQueryer) error {
	var valid bool
	if err := q.QueryRowContext(ctx, `SELECT
 (SELECT count(*) FROM pg_class c CROSS JOIN LATERAL aclexplode(COALESCE(c.relacl,acldefault('r',c.relowner))) x
   WHERE c.oid='public.goose_db_version'::regclass AND x.grantee='aboutme_migrator'::regrole
     AND x.grantor='aboutme_runtime_owner'::regrole AND x.privilege_type IN ('SELECT','INSERT') AND NOT x.is_grantable)=2
 AND NOT EXISTS (SELECT 1 FROM pg_class c CROSS JOIN LATERAL aclexplode(COALESCE(c.relacl,acldefault('r',c.relowner))) x
   WHERE c.oid='public.goose_db_version'::regclass
     AND NOT (x.grantee='aboutme_runtime_owner'::regrole AND x.grantor='aboutme_runtime_owner'::regrole)
     AND NOT (x.grantee='aboutme_migrator'::regrole AND x.grantor='aboutme_runtime_owner'::regrole AND x.privilege_type IN ('SELECT','INSERT') AND NOT x.is_grantable))
 AND NOT EXISTS (SELECT 1 FROM pg_attribute a CROSS JOIN LATERAL aclexplode(a.attacl) x
   WHERE a.attrelid='public.goose_db_version'::regclass AND a.attnum>0 AND NOT a.attisdropped)`).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return ErrMigrationHistoryCorrupt
	}
	return nil
}

func lockVersion13ManifestTables(ctx context.Context, exec migrationExecer) error {
	for _, object := range fixedVersion13Manifest() {
		if object.kind != "table" || object.identity == "runtime_write_state" {
			continue
		}
		statement := fmt.Sprintf(`LOCK TABLE "public"."%s" IN ACCESS EXCLUSIVE MODE`, object.identity)
		if _, err := exec.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrations: lock fixed manifest table: %w", err)
		}
	}
	return nil
}

func finishAdoptionLocks(ctx context.Context, conn *sql.Conn, wrapped lock.SessionLocker, pid int32, locked, clean *bool) error {
	cleanupCtx, cancel := runtimeCleanupContext(ctx)
	defer cancel()
	if err := wrapped.SessionUnlock(cleanupCtx, conn); err != nil {
		return err
	}
	var released bool
	if err := conn.QueryRowContext(cleanupCtx, `SELECT pg_advisory_unlock_shared($1)`, runtimeMigrationLockID).Scan(&released); err != nil {
		return err
	}
	if !released {
		return errors.New("migrations: release adoption runtime lock")
	}
	if err := verifySessionIdentity(cleanupCtx, conn, pid, "aboutme", "on"); err != nil {
		return err
	}
	*locked, *clean = false, true
	return nil
}
