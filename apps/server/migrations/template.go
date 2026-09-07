package migrations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// Template databases are local test tooling beside ProvisionDatabase. Every
// name they touch must satisfy localMigrationDatabasePattern, and the build
// path is the same provisioned applyFS path tests use today.

const templateHarnessVersion = 1

const templateBuildTimeout = 2 * time.Minute

var templateDatabasePattern = regexp.MustCompile(`^aboutme_migrate_template_[0-9]+_[0-9a-f]{16}$`)

var disposableDatabasePattern = regexp.MustCompile(`^aboutme_migrate_(cmd_)?test_[0-9]+_[0-9]+$`)

// ErrTemplateMissing reports a clone from a template that no longer exists.
var ErrTemplateMissing = errors.New("migrations: template database is missing")

// TemplateDatabaseName names the local template for fsys applied through
// version through. A non-positive through means every source.
func TemplateDatabaseName(fsys fs.FS, through int64) (string, error) {
	sources, err := migrationSourcesFromFS(fsys)
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	if _, err = fmt.Fprintf(digest, "aboutme.migration-template.v%d\n", templateHarnessVersion); err != nil {
		return "", err
	}
	var last int64
	for _, source := range sources {
		if through > 0 && source.Version > through {
			break
		}
		data, err := fs.ReadFile(fsys, source.Path)
		if err != nil {
			return "", err
		}
		if _, err = fmt.Fprintf(digest, "%d %s %d\n", source.Version, source.Path, len(data)); err != nil {
			return "", err
		}
		digest.Write(data)
		last = source.Version
	}
	if last == 0 {
		return "", errors.New("migrations: no migration sources for template")
	}
	return fmt.Sprintf("aboutme_migrate_template_%d_%s", last, hex.EncodeToString(digest.Sum(nil))[:16]), nil
}

// throughFS hides sources above through from the Goose provider.
type throughFS struct {
	fs.FS
	through int64
}

// ReadDir lists name without the sources above through, which is the path
// fs.Glob takes for the Goose provider and migrationSourcesFromFS.
func (f throughFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(f.FS, name)
	if err != nil {
		return nil, err
	}
	kept := entries[:0]
	for _, entry := range entries {
		version, parseErr := sourceVersionFromName(entry.Name())
		if entry.IsDir() || parseErr != nil || f.through <= 0 || version <= f.through {
			kept = append(kept, entry)
		}
	}
	return kept, nil
}

// Open refuses sources above through so a hidden file cannot be read by
// name either.
func (f throughFS) Open(name string) (fs.File, error) {
	if version, err := sourceVersionFromName(name); err == nil && f.through > 0 && version > f.through {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return f.FS.Open(name)
}

func sourceVersionFromName(name string) (int64, error) {
	base := name
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	var version int64
	if _, err := fmt.Sscanf(base, "%d_", &version); err != nil || version <= 0 {
		return 0, fmt.Errorf("migrations: %q is not a versioned source", name)
	}
	return version, nil
}

func databaseDSN(adminDSN, name string) (string, error) {
	u, err := url.Parse(adminDSN)
	if err != nil {
		return "", errors.New("migrations: invalid admin connection URL")
	}
	u.Path = "/" + name
	return u.String(), nil
}

func withAdminConn(ctx context.Context, adminDSN string, fn func(*sql.Conn) error) (resultErr error) {
	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, admin.Close()) }()
	conn, err := admin.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, conn.Close()) }()
	return fn(conn)
}

// EnsureTemplateDatabase builds the template for fsys through version
// through once and returns its name. Concurrent builders across processes
// serialize on an advisory lock keyed by the name.
func EnsureTemplateDatabase(ctx context.Context, adminDSN string, fsys fs.FS, through int64) (string, error) {
	name, err := TemplateDatabaseName(fsys, through)
	if err != nil {
		return "", err
	}
	err = withAdminConn(ctx, adminDSN, func(conn *sql.Conn) (resultErr error) {
		if _, lockErr := conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtext($1)::bigint)`, name); lockErr != nil {
			return fmt.Errorf("migrations: lock template build: %w", lockErr)
		}
		defer func() {
			unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			if _, unlockErr := conn.ExecContext(unlockCtx, `SELECT pg_advisory_unlock(hashtext($1)::bigint)`, name); unlockErr != nil {
				resultErr = errors.Join(resultErr, unlockErr)
			}
		}()
		var isTemplate bool
		rowErr := conn.QueryRowContext(ctx, `SELECT datistemplate FROM pg_database WHERE datname=$1`, name).Scan(&isTemplate)
		switch {
		case rowErr == nil && isTemplate:
			return nil
		case rowErr == nil:
			if _, dropErr := conn.ExecContext(ctx, `DROP DATABASE `+name+` WITH (FORCE)`); dropErr != nil {
				return fmt.Errorf("migrations: drop half-built template: %w", dropErr)
			}
		case !errors.Is(rowErr, sql.ErrNoRows):
			return rowErr
		}
		if _, createErr := conn.ExecContext(ctx, `CREATE DATABASE `+name); createErr != nil {
			return fmt.Errorf("migrations: create template: %w", createErr)
		}
		if buildErr := buildTemplate(ctx, adminDSN, name, throughFS{FS: fsys, through: through}); buildErr != nil {
			dropCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			_, dropErr := conn.ExecContext(dropCtx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
			return errors.Join(buildErr, dropErr)
		}
		if _, markErr := conn.ExecContext(ctx, `ALTER DATABASE `+name+` IS_TEMPLATE true`); markErr != nil {
			return fmt.Errorf("migrations: mark template: %w", markErr)
		}
		if _, sealErr := conn.ExecContext(ctx, `ALTER DATABASE `+name+` ALLOW_CONNECTIONS false`); sealErr != nil {
			return fmt.Errorf("migrations: seal template: %w", sealErr)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return name, nil
}

func buildTemplate(ctx context.Context, adminDSN, name string, fsys fs.FS) (resultErr error) {
	buildCtx, cancel := context.WithTimeout(ctx, templateBuildTimeout)
	defer cancel()
	dsn, err := databaseDSN(adminDSN, name)
	if err != nil {
		return err
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, db.Close()) }()
	if err := ProvisionDatabase(buildCtx, db); err != nil {
		return fmt.Errorf("migrations: provision template: %w", err)
	}
	if _, err := applyFS(buildCtx, db, fsys, LocalAdminMigratorIdentity()); err != nil {
		return fmt.Errorf("migrations: apply template sources: %w", err)
	}
	return nil
}

// CloneTemplateDatabase creates disposable database name from template and
// provisions it. name must belong to the disposable test class.
func CloneTemplateDatabase(ctx context.Context, adminDSN, template, name string) error {
	if !templateDatabasePattern.MatchString(template) || !disposableDatabasePattern.MatchString(name) {
		return errors.New("migrations: template or clone name is outside the local test classes")
	}
	return withAdminConn(ctx, adminDSN, func(conn *sql.Conn) error {
		_, err := conn.ExecContext(ctx, `CREATE DATABASE `+name+` TEMPLATE `+template)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "3D000" {
			return ErrTemplateMissing
		}
		if err != nil {
			return err
		}
		if err := provisionClone(ctx, conn, adminDSN, name); err != nil {
			dropCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			_, dropErr := conn.ExecContext(dropCtx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
			return errors.Join(err, dropErr)
		}
		return nil
	})
}

// provisionClone restores the database-level grants on a clone and then
// validates it through ProvisionDatabase. CREATE DATABASE copies the
// template's files, so schema grants arrive intact, but the clone gets a
// fresh pg_database row without the template's ACL, and ProvisionDatabase
// stops installing grants once the runtime foundation exists.
func provisionClone(ctx context.Context, admin *sql.Conn, adminDSN, name string) (resultErr error) {
	for _, statement := range []string{
		`GRANT CONNECT,TEMPORARY,CREATE ON DATABASE ` + name + ` TO aboutme_migrator`,
		`GRANT TEMPORARY ON DATABASE ` + name + ` TO aboutme_runtime_owner`,
	} {
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrations: install clone database grants: %w", err)
		}
	}
	dsn, err := databaseDSN(adminDSN, name)
	if err != nil {
		return err
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, db.Close()) }()
	if err := ProvisionDatabase(ctx, db); err != nil {
		return fmt.Errorf("migrations: provision clone: %w", err)
	}
	return nil
}

// DropStaleTemplateDatabases drops every template database whose name is not
// in keep and returns the dropped names.
func DropStaleTemplateDatabases(ctx context.Context, adminDSN string, keep []string) ([]string, error) {
	kept := make(map[string]bool, len(keep))
	for _, name := range keep {
		kept[name] = true
	}
	var dropped []string
	err := withAdminConn(ctx, adminDSN, func(conn *sql.Conn) error {
		names, err := listTemplateDatabases(ctx, conn)
		if err != nil {
			return err
		}
		for _, name := range names {
			if kept[name] || !templateDatabasePattern.MatchString(name) {
				continue
			}
			if _, err := conn.ExecContext(ctx, `ALTER DATABASE `+name+` IS_TEMPLATE false`); err != nil {
				return fmt.Errorf("migrations: unmark stale template: %w", err)
			}
			if _, err := conn.ExecContext(ctx, `DROP DATABASE `+name+` WITH (FORCE)`); err != nil {
				return fmt.Errorf("migrations: drop stale template: %w", err)
			}
			dropped = append(dropped, name)
		}
		return nil
	})
	return dropped, err
}

// listTemplateDatabases reads every template-class name before the caller
// runs statements on the same connection, which cannot happen while the
// result set is still open.
func listTemplateDatabases(ctx context.Context, conn *sql.Conn) (names []string, resultErr error) {
	rows, err := conn.QueryContext(ctx, `SELECT datname FROM pg_database WHERE datname LIKE 'aboutme_migrate_template_%' ORDER BY datname`)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	for rows.Next() {
		var name string
		if scanErr := rows.Scan(&name); scanErr != nil {
			return nil, scanErr
		}
		names = append(names, name)
	}
	return names, rows.Err()
}
