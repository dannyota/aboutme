package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

var (
	// ErrMigrationHistoryMissing reports a database with no migration history.
	ErrMigrationHistoryMissing = errors.New("migration history is missing")
	// ErrMigrationHistoryCorrupt reports an invalid catalog/history combination.
	ErrMigrationHistoryCorrupt = errors.New("migration history is corrupt")
	// ErrHistoryAdoptionRequired reports a valid local admin-owned version-13 history.
	ErrHistoryAdoptionRequired = errors.New("migration history owner adoption is required")
	// ErrMigrationProvisioningDrift reports grants outside the fixed contract.
	ErrMigrationProvisioningDrift = errors.New("migration database provisioning drift")
)

var errBootstrapStateTransition = errors.New("migration bootstrap state changed while waiting for Goose lock")

type migrationCatalogState struct {
	runtimeExists bool
	historyExists bool
	historyOwner  string
}

func readMigrationCatalogHint(ctx context.Context, db *sql.DB) (state migrationCatalogState, resultErr error) {
	if db == nil {
		return state, errors.New("migrations: nil catalog database")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return state, err
	}
	backend, err := captureMigrationBackend(conn)
	if err != nil {
		ignoreMigrationCleanupError(conn.Close())
		return state, err
	}
	clean := false
	defer func() {
		resultErr = errors.Join(resultErr, finalizeMigrationBackend(ctx, backend, conn, clean))
	}()
	resultErr = conn.QueryRowContext(ctx, `SELECT
 to_regclass('public.runtime_write_state') IS NOT NULL,
 to_regclass('public.goose_db_version') IS NOT NULL,
	 COALESCE((SELECT pg_get_userbyid(relowner) FROM pg_class WHERE oid=to_regclass('public.goose_db_version')),'')`, pgx.QueryExecModeSimpleProtocol).Scan(&state.runtimeExists, &state.historyExists, &state.historyOwner)
	if resultErr != nil {
		resultErr = fmt.Errorf("migrations: inspect migration catalog: %w", resultErr)
	}
	clean = resultErr == nil
	return state, resultErr
}

func readMigrationCatalogState(ctx context.Context, q manifestQueryer) (migrationCatalogState, error) {
	var state migrationCatalogState
	err := q.QueryRowContext(ctx, `SELECT
 to_regclass('public.runtime_write_state') IS NOT NULL,
 to_regclass('public.goose_db_version') IS NOT NULL,
	 COALESCE((SELECT pg_get_userbyid(relowner) FROM pg_class WHERE oid=to_regclass('public.goose_db_version')),'')`).Scan(&state.runtimeExists, &state.historyExists, &state.historyOwner)
	if err != nil {
		return state, fmt.Errorf("migrations: inspect migration catalog: %w", err)
	}
	return state, nil
}

func readHistoryHead(ctx context.Context, q manifestQueryer) (int64, error) {
	var head sql.NullInt64
	if err := q.QueryRowContext(ctx, `SELECT max(version_id) FILTER (WHERE is_applied) FROM public.goose_db_version`).Scan(&head); err != nil {
		return 0, fmt.Errorf("migrations: read migration history head: %w", err)
	}
	if !head.Valid {
		return 0, nil
	}
	return head.Int64, nil
}

func validateContiguousHistoryPrefix(ctx context.Context, q manifestQueryer, head int64) error {
	if head < 0 || head > 13 {
		return ErrMigrationHistoryCorrupt
	}
	var valid bool
	if err := q.QueryRowContext(ctx, `SELECT count(*)=$1+1 AND count(DISTINCT version_id)=$1+1
 AND min(version_id)=0 AND max(version_id)=$1 AND bool_and(is_applied)
 FROM public.goose_db_version`, head).Scan(&valid); err != nil {
		return fmt.Errorf("migrations: validate contiguous history prefix: %w", err)
	}
	if !valid {
		return ErrMigrationHistoryCorrupt
	}
	return nil
}

func validateUnprotectedHistory(ctx context.Context, q manifestQueryer, owner string, head int64) error {
	if owner != "aboutme" && owner != "aboutme_migrator" && owner != "aboutme_runtime_owner" {
		return ErrMigrationHistoryCorrupt
	}
	if err := validateContiguousHistoryPrefix(ctx, q, head); err != nil {
		return err
	}
	var valid bool
	if err := q.QueryRowContext(ctx, `SELECT c.relkind='r' AND pg_get_userbyid(c.relowner)=$1
 AND (SELECT array_agg(concat_ws(':',a.attnum::text,a.attname,format_type(a.atttypid,a.atttypmod),a.attnotnull::text,a.attidentity::text,a.attgenerated::text,COALESCE(pg_get_expr(d.adbin,d.adrelid),'')) ORDER BY a.attnum) FROM pg_attribute a LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum WHERE a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped)=ARRAY['1:id:integer:true:d::','2:version_id:bigint:true:::','3:is_applied:boolean:true:::','4:tstamp:timestamp without time zone:true:::now()']
 AND (SELECT count(*) FROM pg_constraint k WHERE k.conrelid=c.oid AND k.contype='p' AND pg_get_constraintdef(k.oid,true)='PRIMARY KEY (id)')=1
 AND (SELECT count(*) FROM pg_trigger t WHERE t.tgrelid=c.oid AND NOT t.tgisinternal)=0
 AND (SELECT count(*) FROM pg_class seq JOIN pg_depend d ON d.classid='pg_class'::regclass AND d.objid=seq.oid AND d.refobjid=c.oid AND d.deptype='i' WHERE seq.relname='goose_db_version_id_seq' AND seq.relowner=c.relowner)=1
 AND (SELECT count(*) FROM pg_type t WHERE t.typrelid=c.oid AND t.typowner=c.relowner)=1
 AND ($2 OR NOT EXISTS (SELECT 1 FROM aclexplode(COALESCE(c.relacl,acldefault('r',c.relowner))) x WHERE x.grantee<>c.relowner OR x.grantor<>c.relowner))
 AND ($2 OR NOT EXISTS (SELECT 1 FROM pg_attribute a CROSS JOIN LATERAL aclexplode(a.attacl) x WHERE a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped))
 FROM pg_class c WHERE c.oid='public.goose_db_version'::regclass`, owner, owner == "aboutme_runtime_owner").Scan(&valid); err != nil {
		return fmt.Errorf("migrations: validate unprotected history shape: %w", err)
	}
	if !valid {
		return ErrMigrationHistoryCorrupt
	}
	if owner == "aboutme_runtime_owner" {
		return validateAdoptedGooseACL(ctx, q)
	}
	return nil
}

type migrationRuntimeMetadata struct {
	gate        string
	generation  int64
	enforcement int16
	owner       string
}

func readLockedRuntimeMetadata(ctx context.Context, q manifestQueryer) (migrationRuntimeMetadata, error) {
	var metadata migrationRuntimeMetadata
	if err := q.QueryRowContext(ctx, `SELECT write_gate,generation,migrator_enforcement_version,migration_history_owner FROM public.runtime_read_migrator_metadata()`).Scan(&metadata.gate, &metadata.generation, &metadata.enforcement, &metadata.owner); err != nil {
		return metadata, fmt.Errorf("migrations: read protected migration metadata: %w", err)
	}
	return metadata, nil
}

func validateProtectedHistory(ctx context.Context, q manifestQueryer, allowInert, requireOpen bool) error {
	state, err := readMigrationCatalogState(ctx, q)
	if err != nil {
		return err
	}
	if !state.runtimeExists || !state.historyExists || (state.historyOwner != "aboutme_migrator" && state.historyOwner != "aboutme_runtime_owner") {
		return ErrMigrationHistoryCorrupt
	}
	metadata, err := readLockedRuntimeMetadata(ctx, q)
	if err != nil {
		return err
	}
	head, err := readHistoryHead(ctx, q)
	if err != nil {
		return err
	}
	if metadata.gate != "open" && metadata.gate != "closing" && metadata.gate != "closed" {
		return fmt.Errorf("%w: runtime gate is not open", ErrMigrationHistoryCorrupt)
	}
	if requireOpen && metadata.gate != "open" {
		return fmt.Errorf("migrations: runtime write gate unavailable")
	}
	switch metadata.enforcement {
	case 0:
		if !allowInert || head != 13 || metadata.owner != state.historyOwner {
			return ErrMigrationHistoryCorrupt
		}
		if err := validateUnprotectedHistory(ctx, q, state.historyOwner, head); err != nil {
			return err
		}
	case 1:
		if head < 14 || state.historyOwner != "aboutme_runtime_owner" || metadata.owner != "aboutme_runtime_owner" {
			return ErrMigrationHistoryCorrupt
		}
		if err := validateHistoryEnforcement(ctx, q); err != nil {
			return err
		}
		if err := validateFixedVersion13Owners(ctx, q); err != nil {
			return err
		}
	default:
		return ErrMigrationHistoryCorrupt
	}
	return nil
}

func validateFixedVersion13Owners(ctx context.Context, q manifestQueryer) error {
	for _, object := range fixedVersion13Manifest() {
		var valid bool
		switch object.kind {
		case "table", "index", "sequence":
			kind := map[string]string{"table": "r", "index": "i", "sequence": "S"}[object.kind]
			err := q.QueryRowContext(ctx, `SELECT count(*)=1 FROM pg_class c WHERE c.relnamespace='public'::regnamespace AND c.relname=$1 AND c.relkind=$2 AND pg_get_userbyid(c.relowner)='aboutme_runtime_owner'`, object.identity, kind).Scan(&valid)
			if err != nil {
				return err
			}
		case "type":
			err := q.QueryRowContext(ctx, `SELECT count(*)=1 FROM pg_type t WHERE t.typnamespace='public'::regnamespace AND t.typname=$1 AND pg_get_userbyid(t.typowner)='aboutme_runtime_owner' AND t.typrelid=to_regclass('public.'||$1)`, object.identity).Scan(&valid)
			if err != nil {
				return err
			}
		case "function":
			err := q.QueryRowContext(ctx, `SELECT count(*)=1 FROM pg_proc p WHERE p.pronamespace='public'::regnamespace AND p.proname||'('||pg_get_function_identity_arguments(p.oid)||')'=$1 AND p.prokind='f' AND pg_get_userbyid(p.proowner)='aboutme_runtime_owner'`, object.identity).Scan(&valid)
			if err != nil {
				return err
			}
		}
		if !valid {
			return fmt.Errorf("%w: fixed object owner %s", ErrMigrationHistoryCorrupt, manifestObjectKey(object.kind, object.schema, object.identity))
		}
	}
	return nil
}

func validateHistoryEnforcement(ctx context.Context, q manifestQueryer) error {
	var valid bool
	err := q.QueryRowContext(ctx, `SELECT pg_get_userbyid(c.relowner)='aboutme_runtime_owner'
 AND c.relkind='r'
 AND (SELECT array_agg(concat_ws(':',a.attnum,a.attname,format_type(a.atttypid,a.atttypmod),a.attnotnull,a.attidentity,a.attgenerated,COALESCE(pg_get_expr(d.adbin,d.adrelid),'')) ORDER BY a.attnum)
      FROM pg_attribute a LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum WHERE a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped)
     = ARRAY['1:id:integer:t:d::','2:version_id:bigint:t:::','3:is_applied:boolean:t:::','4:tstamp:timestamp without time zone:t:::now()']
 AND (SELECT count(*) FROM pg_index i WHERE i.indrelid=c.oid AND i.indisprimary AND i.indkey::text='1')=1
 AND (SELECT count(*) FROM pg_trigger t WHERE t.tgrelid=c.oid AND NOT t.tgisinternal)=3
 AND EXISTS (SELECT 1 FROM pg_trigger t WHERE t.tgrelid=c.oid AND t.tgname='goose_db_version_insert_guard' AND t.tgenabled='O' AND t.tgtype=7 AND t.tgfoid='public.runtime_assert_migration_history_write()'::regprocedure)
 AND EXISTS (SELECT 1 FROM pg_trigger t WHERE t.tgrelid=c.oid AND t.tgname='goose_db_version_update_delete_guard' AND t.tgenabled='O' AND t.tgtype=27 AND t.tgfoid='public.runtime_deny_migration_history_write()'::regprocedure)
 AND EXISTS (SELECT 1 FROM pg_trigger t WHERE t.tgrelid=c.oid AND t.tgname='goose_db_version_truncate_guard' AND t.tgenabled='O' AND t.tgtype=34 AND t.tgfoid='public.runtime_deny_migration_history_write()'::regprocedure)
 AND (SELECT count(*) FROM pg_proc p WHERE p.oid IN ('public.runtime_assert_migration_history_write()'::regprocedure,'public.runtime_deny_migration_history_write()'::regprocedure) AND p.prokind='f' AND p.prosecdef AND pg_get_userbyid(p.proowner)='aboutme_runtime_owner')=2
 AND NOT EXISTS (SELECT 1 FROM aclexplode(c.relacl) x WHERE
      (x.grantee='aboutme_migrator'::regrole AND (x.privilege_type NOT IN ('SELECT','INSERT') OR x.is_grantable))
      OR (x.grantee='aboutme_runtime_owner'::regrole AND x.is_grantable)
      OR x.grantee NOT IN ('aboutme_migrator'::regrole,'aboutme_runtime_owner'::regrole))
 AND (SELECT count(*) FROM aclexplode(c.relacl) x WHERE x.grantee='aboutme_migrator'::regrole AND x.privilege_type IN ('SELECT','INSERT') AND NOT x.is_grantable)=2
 AND NOT EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped AND a.attacl IS NOT NULL)
 FROM pg_class c WHERE c.oid='public.goose_db_version'::regclass`).Scan(&valid)
	if err != nil {
		return fmt.Errorf("migrations: validate protected history: %w", err)
	}
	if !valid {
		return ErrMigrationHistoryCorrupt
	}
	return nil
}
