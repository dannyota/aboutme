package dbroles

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// PostgreSQL advisory locks are per database, so LockID serializes catalog writes made through one database only.
const LockID int64 = 1229627785684121947

var (
	ErrDrift = errors.New("database role configuration drift")
)

type Result struct {
	Created  int
	Verified int
}

type role struct {
	name                                            string
	login, superuser, inherit, createrole, createdb bool
	replication, bypassRLS                          bool
}

var fixedRoles = []role{
	{name: "aboutme_migrator", login: true},
	{name: "aboutme_app", login: true},
}

type membership struct {
	role, member, grantor string
	admin, inherit, set   bool
}

type catalogStore interface {
	databaseAndUser(context.Context) (string, string, error)
	lock(context.Context) error
	readRoles(context.Context) (map[string]role, error)
	readMemberships(context.Context) ([]membership, error)
	createRole(context.Context, role) error
	grantDatabaseAndSchema(context.Context, string) error
}

func Ensure(ctx context.Context, db *sql.DB) (Result, error) {
	if db == nil {
		return Result{}, errors.New("dbroles: nil database")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, fmt.Errorf("dbroles: begin: %w", err)
	}
	return ensureTransaction(ctx, sqlTransaction{sqlCatalog: sqlCatalog{tx: tx}, Tx: tx})
}

type transaction interface {
	catalogStore
	Commit() error
	Rollback() error
}

func ensureTransaction(ctx context.Context, tx transaction) (Result, error) {
	committed := false
	defer func() {
		if !committed {
			rollbackBounded(tx, 5*time.Second)
		}
	}()
	result, err := ensureCatalog(ctx, tx)
	if err != nil {
		return Result{}, err
	}
	if err := tx.Commit(); err != nil {
		return Result{}, fmt.Errorf("dbroles: commit outcome unknown: %w", err)
	}
	committed = true
	return result, nil
}

func rollbackBounded(tx interface{ Rollback() error }, timeout time.Duration) {
	done := make(chan error, 1)
	go func() { done <- tx.Rollback() }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, sql.ErrTxDone) {
			slog.Warn("dbroles: rollback failed")
		}
	case <-timer.C:
	}
}

func ensureCatalog(ctx context.Context, store catalogStore) (Result, error) {
	database, currentUser, err := store.databaseAndUser(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("dbroles: identify database: %w", err)
	}
	if lockErr := store.lock(ctx); lockErr != nil {
		return Result{}, fmt.Errorf("dbroles: acquire lock: %w", lockErr)
	}

	roles, err := store.readRoles(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("dbroles: read roles: %w", err)
	}
	memberships, err := store.readMemberships(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("dbroles: read memberships: %w", err)
	}

	var result Result
	switch {
	case len(roles) != 0 && len(roles) != len(fixedRoles):
		return Result{}, fmt.Errorf("%w: partial fixed role set", ErrDrift)
	case len(roles) == len(fixedRoles):
		for _, want := range fixedRoles {
			got, ok := roles[want.name]
			if !ok || got != want {
				return Result{}, fmt.Errorf("%w: role %s", ErrDrift, want.name)
			}
		}
		if err := verifyMemberships(memberships, currentUser); err != nil {
			return Result{}, err
		}
		result.Verified = len(fixedRoles)
	default:
		if len(memberships) != 0 {
			return Result{}, fmt.Errorf("%w: membership references absent fixed role", ErrDrift)
		}
		for _, spec := range fixedRoles {
			if err := store.createRole(ctx, spec); err != nil {
				return Result{}, fmt.Errorf("dbroles: create %s: %w", spec.name, err)
			}
		}
		result.Created = len(fixedRoles)
	}

	if err := store.grantDatabaseAndSchema(ctx, database); err != nil {
		return Result{}, fmt.Errorf("dbroles: grant database and schema privileges: %w", err)
	}
	return result, nil
}

// PostgreSQL grants a non-superuser role creator ADMIN membership with INHERIT and SET false.
func verifyMemberships(got []membership, currentUser string) error {
	for _, edge := range got {
		if edge.member == currentUser && edge.admin && !edge.inherit && !edge.set {
			continue
		}
		return fmt.Errorf("%w: unexpected fixed-role membership", ErrDrift)
	}
	return nil
}

type sqlCatalog struct{ tx *sql.Tx }

type sqlTransaction struct {
	sqlCatalog
	*sql.Tx
}

func (s sqlCatalog) databaseAndUser(ctx context.Context) (string, string, error) {
	var database, user string
	err := s.tx.QueryRowContext(ctx, `SELECT current_database(), current_user`).Scan(&database, &user)
	return database, user, err
}
func (s sqlCatalog) lock(ctx context.Context) error {
	return lockCatalog(ctx, s.tx)
}

func lockCatalog(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, LockID)
	return err
}
func (s sqlCatalog) readRoles(ctx context.Context) (map[string]role, error) {
	rows, err := s.tx.QueryContext(ctx, `
SELECT rolname, rolcanlogin, rolsuper, rolinherit, rolcreaterole, rolcreatedb,
       rolreplication, rolbypassrls
FROM pg_catalog.pg_roles
WHERE rolname IN ('aboutme_migrator','aboutme_app')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // rows.Err reports iteration and implicit-close failures.
	result := make(map[string]role)
	for rows.Next() {
		var r role
		if err := rows.Scan(&r.name, &r.login, &r.superuser, &r.inherit, &r.createrole, &r.createdb, &r.replication, &r.bypassRLS); err != nil {
			return nil, err
		}
		result[r.name] = r
	}
	return result, rows.Err()
}
func (s sqlCatalog) readMemberships(ctx context.Context) ([]membership, error) {
	rows, err := s.tx.QueryContext(ctx, `
SELECT granted.rolname, member.rolname, grantor.rolname,
       m.admin_option, m.inherit_option, m.set_option
FROM pg_catalog.pg_auth_members AS m
JOIN pg_catalog.pg_roles AS granted ON granted.oid = m.roleid
JOIN pg_catalog.pg_roles AS member ON member.oid = m.member
JOIN pg_catalog.pg_roles AS grantor ON grantor.oid = m.grantor
WHERE granted.rolname IN ('aboutme_migrator','aboutme_app')
   OR member.rolname IN ('aboutme_migrator','aboutme_app')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // rows.Err reports iteration and implicit-close failures.
	var result []membership
	for rows.Next() {
		var m membership
		if err := rows.Scan(&m.role, &m.member, &m.grantor, &m.admin, &m.inherit, &m.set); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}
func (s sqlCatalog) createRole(ctx context.Context, r role) error {
	statement, ok := createStatements[r.name]
	if !ok {
		return errors.New("dbroles: unknown fixed role")
	}
	_, err := s.tx.ExecContext(ctx, statement)
	return err
}

func (s sqlCatalog) grantDatabaseAndSchema(ctx context.Context, database string) error {
	ident := quoteIdentifier(database)
	statements := []string{
		`REVOKE ALL ON DATABASE ` + ident + ` FROM PUBLIC`,
		`GRANT CONNECT, CREATE ON DATABASE ` + ident + ` TO aboutme_migrator`,
		`GRANT CONNECT ON DATABASE ` + ident + ` TO aboutme_app`,
		`REVOKE CREATE ON SCHEMA public FROM PUBLIC`,
		`GRANT USAGE, CREATE ON SCHEMA public TO aboutme_migrator`,
		`GRANT USAGE ON SCHEMA public TO aboutme_app`,
	}
	for _, stmt := range statements {
		if _, err := s.tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

// PostgreSQL DDL has no bind parameters for identifiers, so names are interpolated as quoted identifiers.
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

var createStatements = map[string]string{
	"aboutme_migrator": `CREATE ROLE aboutme_migrator LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS NOINHERIT`,
	"aboutme_app":      `CREATE ROLE aboutme_app LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS NOINHERIT`,
}
