//nolint:govet // Sequential migration cleanup keeps each error next to its operation.
package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/pressly/goose/v3/lock"
)

var localMigrationDatabasePattern = regexp.MustCompile(`^(aboutme|aboutme_dev|aboutme_migrate_(cmd_)?test_[0-9]+_[0-9]+|aboutme_migrate_template_[0-9]+_[0-9a-f]{16})$`)

// provisioningOwner is the fixed database owner that installs the grants. It
// need not be a superuser, so an RDS master user can provision.
const provisioningOwner = "aboutme"

// ProvisionDatabase installs the fixed database-local grants as the database
// owner.
func ProvisionDatabase(ctx context.Context, db *sql.DB) error {
	return provisionDatabase(ctx, db, provisioningOwner)
}

func provisionDatabase(ctx context.Context, db *sql.DB, owner string) (resultErr error) {
	if db == nil {
		return errors.New("migrations: nil provisioning database")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	backend, err := captureMigrationBackend(conn)
	if err != nil {
		ignoreMigrationCleanupError(conn.Close())
		return err
	}
	backendOwned := true
	defer func() {
		if backendOwned {
			resultErr = errors.Join(resultErr, finalizeMigrationBackend(ctx, backend, conn, false))
		}
	}()
	if err := verifyMigrationDriver(conn); err != nil {
		return err
	}
	var user, superuser, database, databaseOwner, schemaOwner string
	var pid int32
	if err := conn.QueryRowContext(ctx, `SELECT session_user,current_setting('is_superuser'),current_database(),pg_backend_pid(),pg_get_userbyid(d.datdba),pg_get_userbyid(n.nspowner) FROM pg_database d CROSS JOIN pg_namespace n WHERE d.datname=current_database() AND n.oid='public'::regnamespace`).Scan(&user, &superuser, &database, &pid, &databaseOwner, &schemaOwner); err != nil {
		return fmt.Errorf("migrations: inspect provisioning authority: %w", err)
	}
	if user != owner || databaseOwner != owner || (schemaOwner != owner && schemaOwner != "pg_database_owner") {
		return errors.New("migrations: provisioning authority or owner mismatch")
	}
	wrapped, err := lock.NewPostgresSessionLocker(lock.WithLockID(LockID))
	if err != nil {
		return err
	}
	if err := wrapped.SessionLock(ctx, conn); err != nil {
		return fmt.Errorf("migrations: acquire provisioning Goose lock: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `BEGIN`); err != nil {
		return err
	}
	transactionActive := true
	defer func() {
		if transactionActive {
			if rollbackErr := rollbackStatusConnection(ctx, conn); rollbackErr != nil {
				resultErr = errors.Join(resultErr, rollbackErr)
			}
		}
	}()
	state, err := readMigrationCatalogState(ctx, conn)
	if err != nil {
		return err
	}
	if err := validateProvisioningACLBounds(ctx, conn); err != nil {
		return err
	}
	if !state.runtimeExists {
		if err := installProvisioning(ctx, conn, database); err != nil {
			return err
		}
	}
	if err := validateProvisioning(ctx, conn); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		transactionActive = false
		retireErr := finalizeMigrationBackend(ctx, backend, conn, false)
		backendOwned = false
		return errors.Join(fmt.Errorf("migrations: provisioning commit outcome unknown: %w", err), retireErr)
	}
	transactionActive = false
	cleanupCtx, cancel := runtimeCleanupContext(ctx)
	defer cancel()
	if err := wrapped.SessionUnlock(cleanupCtx, conn); err != nil {
		return fmt.Errorf("migrations: release provisioning Goose lock: %w", err)
	}
	if err := verifySessionIdentity(cleanupCtx, conn, pid, owner, superuser); err != nil {
		return err
	}
	return nil
}

type migrationExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func installProvisioning(ctx context.Context, exec migrationExecer, database string) error {
	quotedDatabase := `"` + strings.ReplaceAll(database, `"`, `""`) + `"`
	statements := []string{
		`GRANT CONNECT,TEMPORARY,CREATE ON DATABASE ` + quotedDatabase + ` TO aboutme_migrator`,
		`GRANT USAGE ON SCHEMA public TO aboutme_migrator`,
		`GRANT CREATE ON SCHEMA public TO aboutme_migrator WITH GRANT OPTION`,
		`GRANT TEMPORARY ON DATABASE ` + quotedDatabase + ` TO aboutme_runtime_owner`,
	}
	for _, statement := range statements {
		if _, err := exec.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrations: install fixed local provisioning grants: %w", err)
		}
	}
	return nil
}

func validateProvisioningACLBounds(ctx context.Context, q manifestQueryer) error {
	var invalid bool
	err := q.QueryRowContext(ctx, `SELECT
 EXISTS (SELECT 1 FROM pg_database d CROSS JOIN LATERAL aclexplode(COALESCE(d.datacl,acldefault('d',d.datdba))) x
         WHERE d.datname=current_database() AND ((x.grantee='aboutme_migrator'::regrole AND (x.privilege_type NOT IN ('CONNECT','TEMPORARY','CREATE') OR x.is_grantable)) OR (x.grantee='aboutme_runtime_owner'::regrole AND (x.privilege_type<>'TEMPORARY' OR x.is_grantable))))
 OR EXISTS (SELECT 1 FROM pg_namespace n CROSS JOIN LATERAL aclexplode(COALESCE(n.nspacl,acldefault('n',n.nspowner))) x
         WHERE n.oid='public'::regnamespace AND ((x.grantee='aboutme_migrator'::regrole AND (x.privilege_type NOT IN ('USAGE','CREATE') OR (x.privilege_type='USAGE' AND x.is_grantable))) OR x.grantee='aboutme_runtime_owner'::regrole OR (x.grantee=0 AND x.privilege_type='CREATE')))`).Scan(&invalid)
	if err != nil {
		return fmt.Errorf("migrations: inspect provisioning ACL bounds: %w", err)
	}
	if invalid {
		return ErrMigrationProvisioningDrift
	}
	return nil
}

func validateProvisioning(ctx context.Context, q manifestQueryer) error {
	if err := validateProvisioningACLBounds(ctx, q); err != nil {
		return err
	}
	var valid bool
	err := q.QueryRowContext(ctx, `SELECT
 has_database_privilege('aboutme_migrator',current_database(),'CONNECT')
 AND has_database_privilege('aboutme_migrator',current_database(),'TEMPORARY')
 AND has_database_privilege('aboutme_migrator',current_database(),'CREATE')
 AND has_database_privilege('aboutme_runtime_owner',current_database(),'TEMPORARY')
 AND has_schema_privilege('aboutme_migrator','public','USAGE')
 AND has_schema_privilege('aboutme_migrator','public','CREATE')
 AND EXISTS (SELECT 1 FROM aclexplode((SELECT nspacl FROM pg_namespace WHERE oid='public'::regnamespace)) x
             WHERE x.grantee='aboutme_migrator'::regrole AND x.privilege_type='CREATE' AND x.is_grantable)
 AND (SELECT count(*) FROM pg_database d CROSS JOIN LATERAL aclexplode(d.datacl) x WHERE d.datname=current_database() AND x.grantee='aboutme_migrator'::regrole AND x.privilege_type IN ('CONNECT','TEMPORARY','CREATE') AND NOT x.is_grantable)=3
 AND (SELECT count(*) FROM pg_database d CROSS JOIN LATERAL aclexplode(d.datacl) x WHERE d.datname=current_database() AND x.grantee='aboutme_runtime_owner'::regrole AND x.privilege_type='TEMPORARY' AND NOT x.is_grantable)=1
 AND (SELECT count(*) FROM pg_namespace n CROSS JOIN LATERAL aclexplode(n.nspacl) x WHERE n.oid='public'::regnamespace AND x.grantee='aboutme_migrator'::regrole AND ((x.privilege_type='USAGE' AND NOT x.is_grantable) OR (x.privilege_type='CREATE' AND x.is_grantable)))=2
 AND NOT has_schema_privilege('aboutme_runtime_owner','public','CREATE')`).Scan(&valid)
	if err != nil {
		return fmt.Errorf("migrations: validate fixed provisioning: %w", err)
	}
	if !valid {
		return ErrMigrationProvisioningDrift
	}
	return nil
}
