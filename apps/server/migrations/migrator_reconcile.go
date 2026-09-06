package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func reconcileAdoption(operationCtx context.Context, db *sql.DB, database string, oldPID int32, generation int64, before *version13ManifestSnapshot, history, foundation string) (resultErr error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(operationCtx), 5*time.Second)
	defer cancel()
	conn, acquireErr := db.Conn(ctx)
	if acquireErr != nil {
		return acquireErr
	}
	backend, captureErr := captureMigrationBackend(conn)
	if captureErr != nil {
		ignoreMigrationCleanupError(conn.Close())
		return captureErr
	}
	clean := false
	defer func() {
		resultErr = errors.Join(resultErr, finalizeMigrationBackend(ctx, backend, conn, clean))
	}()
	if err := verifyMigrationDriver(conn); err != nil {
		return err
	}
	var user, superuser, currentDatabase string
	var pid int32
	if err := conn.QueryRowContext(ctx, `SELECT session_user,current_setting('is_superuser'),current_database(),pg_backend_pid()`).Scan(&user, &superuser, &currentDatabase, &pid); err != nil {
		return err
	}
	if user != "aboutme" || superuser != "on" || currentDatabase != database || pid == oldPID || !localMigrationDatabasePattern.MatchString(currentDatabase) {
		return ErrMigrationHistoryCorrupt
	}
	if _, err := conn.ExecContext(ctx, `BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY`); err != nil {
		return err
	}
	transactionActive := true
	defer func() {
		if transactionActive {
			rollbackErr := rollbackStatusConnection(ctx, conn)
			resultErr = errors.Join(resultErr, rollbackErr)
			if rollbackErr == nil {
				identityErr := verifySessionIdentity(ctx, conn, pid, "aboutme", "on")
				resultErr = errors.Join(resultErr, identityErr)
				clean = identityErr == nil
			}
		}
	}()
	after, err := validateVersion13Manifest(ctx, conn, "aboutme_runtime_owner")
	if err != nil {
		return errors.Join(ErrMigrationHistoryCorrupt, err)
	}
	allowedACL := map[string]string{}
	for _, evidence := range after.objects {
		if evidence.object.kind == "table" && evidence.object.identity == "goose_db_version" {
			allowedACL[manifestObjectKey("table", "public", "goose_db_version")] = evidence.acl
		}
	}
	beforeComparable := copyManifestWithoutRows(before)
	afterComparable := copyManifestWithoutRows(after)
	if err := compareVersion13ManifestSnapshots(beforeComparable, afterComparable, "aboutme", "aboutme_runtime_owner", allowedACL); err != nil {
		return errors.Join(ErrMigrationHistoryCorrupt, err)
	}
	if err := validateProvisioning(ctx, conn); err != nil {
		return errors.Join(ErrMigrationHistoryCorrupt, err)
	}
	if err := validateAdoptedGooseACL(ctx, conn); err != nil {
		return errors.Join(ErrMigrationHistoryCorrupt, err)
	}
	var gotHistory, gotFoundation string
	if err := conn.QueryRowContext(ctx, adoptionHistoryEvidenceQuery).Scan(&gotHistory); err != nil {
		return err
	}
	if err := conn.QueryRowContext(ctx, adoptionFoundationEvidenceQuery).Scan(&gotFoundation); err != nil {
		return err
	}
	if gotHistory != history || gotFoundation != foundation {
		return ErrMigrationHistoryCorrupt
	}
	var gate, owner, writerKind, operation string
	var gotGeneration int64
	var enforcement int16
	if err := conn.QueryRowContext(ctx, `SELECT write_gate,generation,migrator_enforcement_version,migration_history_owner,last_writer_kind,last_writer_operation_id FROM public.runtime_write_state WHERE singleton`).Scan(&gate, &gotGeneration, &enforcement, &owner, &writerKind, &operation); err != nil {
		return err
	}
	if gate != "open" || enforcement != 0 || owner != "aboutme_runtime_owner" || gotGeneration < generation+1 ||
		(gotGeneration == generation+1 && (writerKind != "migrator" || operation != "migration-history-adoption")) {
		return ErrMigrationHistoryCorrupt
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		transactionActive = false
		return err
	}
	transactionActive = false
	if err := verifySessionIdentity(ctx, conn, pid, "aboutme", "on"); err != nil {
		return err
	}
	clean = true
	return nil
}

func copyManifestWithoutRows(source *version13ManifestSnapshot) *version13ManifestSnapshot {
	copySnapshot := &version13ManifestSnapshot{objects: append([]version13ObjectEvidence(nil), source.objects...), triggers: append([]string(nil), source.triggers...)}
	for i := range copySnapshot.objects {
		copySnapshot.objects[i].rowCount = 0
	}
	return copySnapshot
}

const adoptionHistoryEvidenceQuery = `SELECT COALESCE(jsonb_agg(jsonb_build_array(id,version_id,is_applied,tstamp) ORDER BY id)::text,'[]') FROM public.goose_db_version`

const adoptionFoundationEvidenceQuery = `SELECT COALESCE(jsonb_agg(jsonb_build_array(kind,identity,oid,owner,definition) ORDER BY kind,identity)::text,'[]') FROM (
 SELECT 'relation' AS kind,c.relname AS identity,c.oid,pg_get_userbyid(c.relowner) AS owner,concat_ws('|',c.relkind::text,c.relpersistence::text) AS definition FROM pg_class c WHERE c.relnamespace='public'::regnamespace AND c.relname IN ('runtime_write_state','runtime_write_state_pkey')
 UNION ALL SELECT 'function',p.proname||'('||pg_get_function_identity_arguments(p.oid)||')',p.oid,pg_get_userbyid(p.proowner),pg_get_functiondef(p.oid) FROM pg_proc p WHERE p.pronamespace='public'::regnamespace AND p.prokind='f' AND p.proname||'('||pg_get_function_identity_arguments(p.oid)||')' IN ('runtime_assert_write_finished()','runtime_validate_write_marker()','runtime_create_write_marker()','runtime_require_write_entry()','runtime_enter_write()','runtime_assert_write_entry()','runtime_assert_business_write()','runtime_finish_write()','runtime_validate_migrator_marker()','runtime_enter_migrator()','runtime_begin_migration_write(operation_id text)','runtime_exit_migrator()','runtime_read_migrator_metadata()')
 UNION ALL SELECT 'type',t.typname,t.oid,pg_get_userbyid(t.typowner),concat_ws('|',t.typtype::text,t.typrelid::text) FROM pg_type t WHERE t.typnamespace='public'::regnamespace AND t.typname='runtime_write_state'
) evidence`

func readAdoptionEvidence(ctx context.Context, q manifestQueryer) (string, string, error) {
	var history, foundation string
	if err := q.QueryRowContext(ctx, adoptionHistoryEvidenceQuery).Scan(&history); err != nil {
		return "", "", fmt.Errorf("migrations: capture adoption history: %w", err)
	}
	if err := q.QueryRowContext(ctx, adoptionFoundationEvidenceQuery).Scan(&foundation); err != nil {
		return "", "", fmt.Errorf("migrations: capture adoption foundation: %w", err)
	}
	return history, foundation, nil
}
