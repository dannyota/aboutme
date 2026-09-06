// Package dbroles creates and verifies the fixed cluster roles used by aboutme.
package dbroles

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// LockID serializes cluster role bootstrap in the common postgres database.
const LockID int64 = -7309311299645240732

var (
	// ErrDrift reports that an existing fixed role set is not exact.
	ErrDrift = errors.New("database role configuration drift")
	// ErrWrongDatabase reports an attempt outside the common postgres database.
	ErrWrongDatabase = errors.New("database role bootstrap requires the postgres database")
)

// Result reports fixed-role outcomes without connection information.
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
	{name: "aboutme_runtime_owner"},
	{name: "aboutme_migrator", login: true},
	{name: "aboutme_app", login: true},
	{name: "aboutme_restore_verify", login: true},
	{name: "aboutme_lifecycle_command", login: true},
	{name: "aboutme_fencing_proof", login: true},
	{name: "aboutme_maintenance", login: true},
}

type membership struct {
	role, member, grantor string
	admin, inherit, set   bool
}

var migratorMembership = membership{
	role: "aboutme_runtime_owner", member: "aboutme_migrator", inherit: false, set: true, admin: false,
}

type catalogStore interface {
	databaseAndUser(context.Context) (string, string, error)
	lock(context.Context) error
	readRoles(context.Context) (map[string]role, error)
	readMemberships(context.Context) ([]membership, error)
	createRole(context.Context, role) error
	grant(context.Context, membership) error
}

// Ensure atomically creates a new fixed role set or verifies an existing one.
// It never repairs, alters, drops, or changes a password on an existing role.
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
	case <-done:
	case <-timer.C:
	}
}

func ensureCatalog(ctx context.Context, store catalogStore) (Result, error) {
	database, currentUser, err := store.databaseAndUser(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("dbroles: identify database: %w", err)
	}
	if database != "postgres" {
		return Result{}, ErrWrongDatabase
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

	if len(roles) != 0 && len(roles) != len(fixedRoles) {
		return Result{}, fmt.Errorf("%w: partial fixed role set", ErrDrift)
	}
	if len(roles) == len(fixedRoles) {
		for _, want := range fixedRoles {
			got, ok := roles[want.name]
			if !ok || got != want {
				return Result{}, fmt.Errorf("%w: role %s", ErrDrift, want.name)
			}
		}
		if err := verifyMemberships(memberships, currentUser); err != nil {
			return Result{}, err
		}
		return Result{Verified: len(fixedRoles)}, nil
	}
	if len(memberships) != 0 {
		return Result{}, fmt.Errorf("%w: membership references absent fixed role", ErrDrift)
	}

	for _, spec := range fixedRoles {
		if err := store.createRole(ctx, spec); err != nil {
			return Result{}, fmt.Errorf("dbroles: create %s: %w", spec.name, err)
		}
	}
	if err := store.grant(ctx, migratorMembership); err != nil {
		return Result{}, fmt.Errorf("dbroles: create migrator membership: %w", err)
	}
	return Result{Created: len(fixedRoles)}, nil
}

func verifyMemberships(got []membership, currentUser string) error {
	required := 0
	for _, edge := range got {
		if edge.role == migratorMembership.role && edge.member == migratorMembership.member && edge.admin == migratorMembership.admin && edge.inherit == migratorMembership.inherit && edge.set == migratorMembership.set {
			required++
			continue
		}
		// PostgreSQL 18 gives a non-superuser CREATEROLE identity this
		// administrative edge on each role it creates. SET and INHERIT are
		// false, so it conveys administration without role privileges.
		if edge.member == currentUser && edge.admin && !edge.inherit && !edge.set {
			continue
		}
		return fmt.Errorf("%w: unexpected fixed-role membership", ErrDrift)
	}
	if required != 1 {
		return fmt.Errorf("%w: migrator membership count %d", ErrDrift, required)
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
	_, err := s.tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, LockID)
	return err
}
func (s sqlCatalog) readRoles(ctx context.Context) (map[string]role, error) {
	rows, err := s.tx.QueryContext(ctx, `
SELECT rolname, rolcanlogin, rolsuper, rolinherit, rolcreaterole, rolcreatedb,
       rolreplication, rolbypassrls
FROM pg_catalog.pg_roles
WHERE rolname IN ('aboutme_runtime_owner','aboutme_migrator','aboutme_app',
 'aboutme_restore_verify','aboutme_lifecycle_command','aboutme_fencing_proof','aboutme_maintenance')`)
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
WHERE granted.rolname IN ('aboutme_runtime_owner','aboutme_migrator','aboutme_app',
 'aboutme_restore_verify','aboutme_lifecycle_command','aboutme_fencing_proof','aboutme_maintenance')
   OR member.rolname IN ('aboutme_runtime_owner','aboutme_migrator','aboutme_app',
 'aboutme_restore_verify','aboutme_lifecycle_command','aboutme_fencing_proof','aboutme_maintenance')`)
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
func (s sqlCatalog) grant(ctx context.Context, m membership) error {
	if m != migratorMembership {
		return errors.New("dbroles: unknown fixed membership")
	}
	_, err := s.tx.ExecContext(ctx, `GRANT aboutme_runtime_owner TO aboutme_migrator WITH INHERIT FALSE, SET TRUE, ADMIN FALSE`)
	return err
}

var createStatements = map[string]string{
	"aboutme_runtime_owner":     `CREATE ROLE aboutme_runtime_owner NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS NOINHERIT`,
	"aboutme_migrator":          `CREATE ROLE aboutme_migrator LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS NOINHERIT`,
	"aboutme_app":               `CREATE ROLE aboutme_app LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS NOINHERIT`,
	"aboutme_restore_verify":    `CREATE ROLE aboutme_restore_verify LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS NOINHERIT`,
	"aboutme_lifecycle_command": `CREATE ROLE aboutme_lifecycle_command LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS NOINHERIT`,
	"aboutme_fencing_proof":     `CREATE ROLE aboutme_fencing_proof LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS NOINHERIT`,
	"aboutme_maintenance":       `CREATE ROLE aboutme_maintenance LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS NOINHERIT`,
}
