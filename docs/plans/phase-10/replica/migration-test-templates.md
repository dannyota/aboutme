# Migration test template databases implementation plan

> For workers: use superpowers:executing-plans within AGENTS.md ownership. ADR
> 0024 keeps one author per task and one fresh phase review. Steps use checkbox
> syntax for tracking.

**Goal:** Cut migration-package test setup from about 1.5 seconds per test to
under 0.1 seconds by cloning hash-keyed template databases, so the whole package
passes under `-race` inside go test's default ten-minute limit.

**Architecture:** The `migrations` package gains a local-only template API
beside `ProvisionDatabase`: it builds one template database per (migration
sources, target version) pair, marks it `IS_TEMPLATE`, and clones it with
`CREATE DATABASE ... TEMPLATE`. Test helpers in the package and in
`internal/testutil` call that API and keep every test on its own disposable
database. Tests that prove provisioning, adoption, or apply mechanics keep
starting from an empty database.

**Tech stack:** PostgreSQL 18 (`CREATE DATABASE ... TEMPLATE`, `datistemplate`,
`pg_advisory_lock`), Go 1.25 `database/sql` with pgx stdlib, Goose provider;
repository pins apply.

**Spec:** This plan is its own specification. Authorities:
[migrator](../../../design/scaling/migrator.md),
[migration provisioning](../../../design/scaling/migration-provisioning.md),
[AGENTS.md resource rules](../../../../AGENTS.md#resource-rules).

## Global constraints

- One database container, `aboutme-test-db`, 512 MB cap. Run heavy jobs one at a
  time; two concurrent `go test` builds were killed for low memory on
  2026-09-07.
- Live DB targets use `-count=1`. Public fixture DSN:
  `postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable`.
- Migrations after `.uat-baseline` are immutable; this plan changes no SQL.
- Root owns `Makefile`, `AGENTS.md`, workflows, and Git. Never `git add -A`.
- Every database name a migrator path touches must match
  `localMigrationDatabasePattern` in
  `apps/server/migrations/migrator_provision.go`.
- Commit messages: Conventional Commits, no agent or AI mention.

## Ownership and acceptance

One author for Tasks 1 through 5; the integration owner reviews between tasks
and commits. Owned paths:

- `apps/server/migrations/migrator_provision.go` (one regex line);
- `apps/server/migrations/migrator_provision_test.go`;
- `apps/server/migrations/template.go` (new);
- `apps/server/migrations/template_test.go` (new);
- `apps/server/migrations/migrator_apply_helpers_test.go`;
- `apps/server/migrations/runtime_membership_schema_helpers_test.go`;
- the six `runtime_*_test.go` files that start from a fixture version;
- `apps/server/internal/testutil/db.go`;
- `apps/server/internal/store/runtime_membership_test.go`.

Root owns `Makefile` and `AGENTS.md` edits in Task 5. Definition of done: the
full migrations package passes with `-race` in one run under ten minutes, the
non-race run finishes under three minutes, every existing test still creates and
drops its own database, and no template is ever connected to by a test.

## Design

**Template name.** `aboutme_migrate_template_<version>_<hash16>` where `version`
is the last migration applied and `hash16` is the first 16 hex characters of
SHA-256 over a harness version constant and every migration source at or below
that version (version, path, length, bytes). Any SQL edit produces a new name;
stale names are dropped.

**Build.** Under `pg_advisory_lock(hashtext(name))` on an admin connection to
the base database: if `pg_database` has the name with `datistemplate` true, it
is ready; if present but not a template, it is a half-built leftover and is
dropped; otherwise `CREATE DATABASE`, `ProvisionDatabase`, `applyFS` through the
version, close every connection, then `ALTER DATABASE ... IS_TEMPLATE true` and
`ALLOW_CONNECTIONS false`. The advisory lock serializes builders across test
binaries running at once, such as `make server-test-db` starting several
packages.

**Clone.** `CREATE DATABASE <disposable> TEMPLATE <template>`, then the two
database-level grants from `installProvisioning`
(`GRANT CONNECT,TEMPORARY,CREATE ... TO aboutme_migrator` and
`GRANT TEMPORARY ... TO aboutme_runtime_owner`) issued from the admin session,
then `ProvisionDatabase` on the clone to validate. The grants must be reissued
because a database ACL lives in the `pg_database` row, which `CREATE DATABASE`
does not copy, and `ProvisionDatabase` installs grants only when the runtime
foundation is absent, so on a clone it is validation-only and would otherwise
fail with provisioning drift. Schema grants arrive with the copied files.
Disposable names keep the existing `aboutme_migrate_test_<nanos>_<counter>`
class, so every existing cleanup path still applies.

**Stale templates.** Once per process, before the first build, compute the
expected name for every version in the embedded sources and drop any
`aboutme_migrate_template_%` database outside that set. A template dropped under
a concurrent process on another worktree is rebuilt by that process on its next
clone, because clone failure with `ErrTemplateMissing` triggers one rebuild.

**Who keeps an empty database.** `migrator_*_test.go`, `harness_test.go`,
`migrations_test.go`, `status_test.go`, `testdb_test.go`, and every
`cmd/migrate` test exercise provisioning, adoption, apply, and CLI paths
themselves. They are not changed.

---

### Task 1: Template name class in the local database pattern

**Files:**

- Modify: `apps/server/migrations/migrator_provision.go:15`
- Test: `apps/server/migrations/migrator_provision_test.go`

**Interfaces:**

- Produces: `localMigrationDatabasePattern` accepting
  `aboutme_migrate_template_[0-9]+_[0-9a-f]{16}`.

- [ ] **Step 1: Write the failing test**

```go
func TestLocalMigrationDatabasePatternClasses(t *testing.T) {
    for _, test := range []struct {
        name  string
        match bool
    }{
        {"aboutme", true},
        {"aboutme_dev", true},
        {"aboutme_migrate_test_1788717868022123841_1", true},
        {"aboutme_migrate_cmd_test_1788717868022123841_1", true},
        {"aboutme_migrate_template_20_0123456789abcdef", true},
        {"aboutme_migrate_template_20_0123456789ABCDEF", false},
        {"aboutme_migrate_template_20_0123456789abcde", false},
        {"aboutme_migrate_template__0123456789abcdef", false},
        {"aboutme_prod", false},
        {"postgres", false},
        {"aboutme_migrate_template_20_0123456789abcdef; DROP DATABASE x", false},
    } {
        if got := localMigrationDatabasePattern.MatchString(test.name); got != test.match {
            t.Errorf("%q match=%t want=%t", test.name, got, test.match)
        }
    }
}
```

- [ ] **Step 2: Run it and observe the template cases fail**

Run from `apps/server`:
`go test ./migrations -run '^TestLocalMigrationDatabasePatternClasses$' -count=1`
Expected: FAIL on the lowercase template name (`match=false want=true`).

- [ ] **Step 3: Extend the pattern**

```go
var localMigrationDatabasePattern = regexp.MustCompile(`^(aboutme|aboutme_dev|aboutme_migrate_(cmd_)?test_[0-9]+_[0-9]+|aboutme_migrate_template_[0-9]+_[0-9a-f]{16})$`)
```

- [ ] **Step 4: Run the test again and the provisioning tests**

Run:
`go test ./migrations -run '^TestLocalMigrationDatabasePatternClasses$|Provision' -count=1`
with `TEST_DATABASE_URL` set. Expected: PASS.

- [ ] **Step 5: Commit**

```sh
git add -- apps/server/migrations/migrator_provision.go apps/server/migrations/migrator_provision_test.go
git commit -m "test(migrations): accept template database name class" -- apps/server/migrations/migrator_provision.go apps/server/migrations/migrator_provision_test.go
```

---

### Task 2: Template API in package migrations

**Files:**

- Create: `apps/server/migrations/template.go`
- Create: `apps/server/migrations/template_test.go`

**Interfaces:**

- Consumes: `migrationSourcesFromFS(fs.FS) ([]*goose.Source, error)`,
  `applyFS(ctx, *sql.DB, fs.FS, MigrationIdentity, ...lock.SessionLockerOption)`,
  `ProvisionDatabase(ctx, *sql.DB) error`, `LocalAdminMigratorIdentity()`,
  `Status(ctx, *sql.DB, MigrationIdentity)`, `PendingCount`.
- Produces:

```go
var ErrTemplateMissing = errors.New("migrations: template database is missing")

// TemplateDatabaseName names the local template for fsys applied through
// version through; through<=0 means every source.
func TemplateDatabaseName(fsys fs.FS, through int64) (string, error)

// EnsureTemplateDatabase builds the template once and returns its name.
func EnsureTemplateDatabase(ctx context.Context, adminDSN string, fsys fs.FS, through int64) (string, error)

// CloneTemplateDatabase creates a provisioned disposable database from template.
func CloneTemplateDatabase(ctx context.Context, adminDSN, template, name string) error

// DropStaleTemplateDatabases drops every template not named in keep.
func DropStaleTemplateDatabases(ctx context.Context, adminDSN string, keep []string) ([]string, error)
```

- [ ] **Step 1: Write the pure naming test**

```go
func TestTemplateDatabaseNameIsVersionedAndSourceBound(t *testing.T) {
    head, err := TemplateDatabaseName(FS, 0)
    if err != nil {
        t.Fatal(err)
    }
    again, err := TemplateDatabaseName(FS, 0)
    if err != nil || again != head {
        t.Fatalf("head=%s again=%s error=%v", head, again, err)
    }
    if !templateDatabasePattern.MatchString(head) || !localMigrationDatabasePattern.MatchString(head) || !strings.HasPrefix(head, "aboutme_migrate_template_20_") {
        t.Fatalf("head name %q", head)
    }
    nineteen, err := TemplateDatabaseName(FS, 19)
    if err != nil || !strings.HasPrefix(nineteen, "aboutme_migrate_template_19_") || nineteen == head {
        t.Fatalf("nineteen=%s error=%v", nineteen, err)
    }
    changed := runtimeTransitionFixtureFS(t, 19)
    file := changed["00019_runtime_replica_registration.sql"]
    file.Data = append([]byte("-- touched\n"), file.Data...)
    touched, err := TemplateDatabaseName(changed, 19)
    if err != nil || touched == nineteen || !strings.HasPrefix(touched, "aboutme_migrate_template_19_") {
        t.Fatalf("touched=%s nineteen=%s error=%v", touched, nineteen, err)
    }
    if _, err := TemplateDatabaseName(fstest.MapFS{}, 0); err == nil {
        t.Fatal("empty source set produced a template name")
    }
}
```

Update the literal `20` when the head version moves; the test names the head on
purpose so a new migration forces a deliberate edit.

- [ ] **Step 2: Run it and observe the missing symbols**

Run: `go test ./migrations -run '^TestTemplateDatabaseName' -count=1` Expected:
build failure, `undefined: TemplateDatabaseName`.

- [ ] **Step 3: Write the live build, clone, rebuild, and stale tests**

```go
func templateAdminDSN(t *testing.T) string {
    t.Helper()
    base := os.Getenv("TEST_DATABASE_URL")
    if base == "" {
        if os.Getenv("REQUIRE_TEST_DB") == "1" {
            t.Fatal("TEST_DATABASE_URL required")
        }
        t.Skip("TEST_DATABASE_URL not set")
    }
    return base
}

func templateRow(t *testing.T, admin *sql.DB, name string) (exists, isTemplate, allowConn bool) {
    t.Helper()
    err := admin.QueryRowContext(context.Background(), `SELECT datistemplate,datallowconn FROM pg_database WHERE datname=$1`, name).Scan(&isTemplate, &allowConn)
    if errors.Is(err, sql.ErrNoRows) {
        return false, false, false
    }
    if err != nil {
        t.Fatal(err)
    }
    return true, isTemplate, allowConn
}

func TestEnsureTemplateDatabaseBuildsOnceAndClonesProvisioned(t *testing.T) {
    base := templateAdminDSN(t)
    admin, err := sql.Open("pgx", base)
    if err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() { _ = admin.Close() })
    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
    defer cancel()
    fixture := runtimeTransitionFixtureFS(t, 19)
    file := fixture["00019_runtime_replica_registration.sql"]
    file.Data = append([]byte("-- template test variant\n"), file.Data...)
    name, err := EnsureTemplateDatabase(ctx, base, fixture, 19)
    if err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() {
        cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
        defer stop()
        if _, err := DropStaleTemplateDatabases(cleanup, base, nil); err != nil {
            t.Errorf("drop templates: %v", err)
        }
    })
    if exists, isTemplate, allowConn := templateRow(t, admin, name); !exists || !isTemplate || allowConn {
        t.Fatalf("template row exists=%t template=%t allowconn=%t", exists, isTemplate, allowConn)
    }
    started := time.Now()
    again, err := EnsureTemplateDatabase(ctx, base, fixture, 19)
    if err != nil || again != name || time.Since(started) > 2*time.Second {
        t.Fatalf("second ensure name=%s elapsed=%s error=%v", again, time.Since(started), err)
    }
    clone := fmt.Sprintf("aboutme_migrate_test_%d_%d", time.Now().UnixNano(), compositionDatabaseCounter.Add(1))
    if err := CloneTemplateDatabase(ctx, base, name, clone); err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() {
        cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
        defer stop()
        _, _ = admin.ExecContext(cleanup, `DROP DATABASE IF EXISTS `+clone+` WITH (FORCE)`)
    })
    u, err := url.Parse(base)
    if err != nil {
        t.Fatal(err)
    }
    u.Path = "/" + clone
    db, err := sql.Open("pgx", u.String())
    if err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() { _ = db.Close() })
    statuses, err := Status(ctx, db, LocalAdminMigratorIdentity())
    if err != nil || PendingCount(statuses) != 1 {
        t.Fatalf("clone pending=%d error=%v", PendingCount(statuses), err)
    }
    if err := ProvisionDatabase(ctx, db); err != nil {
        t.Fatalf("clone provisioning is not idempotent: %v", err)
    }
    var generation int64
    var registration bool
    if err := db.QueryRowContext(ctx, `SELECT generation,to_regprocedure('public.runtime_register_serving_replica(uuid,text,text,text,text,text,text)') IS NOT NULL FROM public.runtime_write_state WHERE singleton`).Scan(&generation, &registration); err != nil || !registration {
        t.Fatalf("clone state generation=%d registration=%t error=%v", generation, registration, err)
    }
    if _, err := applyFS(ctx, db, runtimeTransitionFixtureFS(t, 20), LocalAdminMigratorIdentity()); err != nil {
        t.Fatalf("upgrade clone: %v", err)
    }
    statuses, err = Status(ctx, db, LocalAdminMigratorIdentity())
    if err != nil || PendingCount(statuses) != 0 {
        t.Fatalf("clone after upgrade pending=%d error=%v", PendingCount(statuses), err)
    }
    if _, err := admin.ExecContext(ctx, `ALTER DATABASE `+name+` IS_TEMPLATE false`); err != nil {
        t.Fatal(err)
    }
    rebuilt, err := EnsureTemplateDatabase(ctx, base, fixture, 19)
    if err != nil || rebuilt != name {
        t.Fatalf("rebuild name=%s error=%v", rebuilt, err)
    }
    if _, isTemplate, _ := templateRow(t, admin, name); !isTemplate {
        t.Fatal("half-built template was not rebuilt")
    }
    if err := CloneTemplateDatabase(ctx, base, "aboutme_migrate_template_19_0000000000000000", clone+"_x"); !errors.Is(err, ErrTemplateMissing) {
        t.Fatalf("missing template error=%v", err)
    }
    if err := CloneTemplateDatabase(ctx, base, name, "aboutme_prod"); err == nil {
        t.Fatal("clone accepted a name outside the disposable class")
    }
    dropped, err := DropStaleTemplateDatabases(ctx, base, []string{name})
    if err != nil {
        t.Fatal(err)
    }
    for _, d := range dropped {
        if d == name {
            t.Fatalf("kept template %s was dropped", name)
        }
    }
    dropped, err = DropStaleTemplateDatabases(ctx, base, nil)
    if err != nil {
        t.Fatal(err)
    }
    if exists, _, _ := templateRow(t, admin, name); exists || len(dropped) == 0 {
        t.Fatalf("stale drop exists=%t dropped=%v", exists, dropped)
    }
}

func TestEnsureTemplateDatabaseSerializesConcurrentBuilders(t *testing.T) {
    base := templateAdminDSN(t)
    fixture := runtimeTransitionFixtureFS(t, 16)
    file := fixture["00016_runtime_public_transition_schema.sql"]
    file.Data = append([]byte("-- concurrent template variant\n"), file.Data...)
    t.Cleanup(func() {
        cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
        defer stop()
        _, _ = DropStaleTemplateDatabases(cleanup, base, nil)
    })
    names := make(chan string, 2)
    errs := make(chan error, 2)
    for i := 0; i < 2; i++ {
        go func() {
            ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
            defer cancel()
            name, err := EnsureTemplateDatabase(ctx, base, fixture, 16)
            names <- name
            errs <- err
        }()
    }
    first, second := <-names, <-names
    if err := errors.Join(<-errs, <-errs); err != nil || first != second || first == "" {
        t.Fatalf("names=%s/%s error=%v", first, second, err)
    }
}
```

The fixture variants (a comment prepended to one migration file) keep these
templates distinct from the names other tests use, so the cleanup at the end
drops only this test's templates when it passes `nil` as keep. Before Task 3
lands no other test builds templates, so `nil` is safe here; Task 3 changes this
test to pass the process's cached names.

- [ ] **Step 4: Run and observe compile failures**

Run:
`REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test ./migrations -run '^TestEnsureTemplate' -count=1`
Expected: build failure listing `EnsureTemplateDatabase`,
`CloneTemplateDatabase`, `DropStaleTemplateDatabases`, `ErrTemplateMissing`.

- [ ] **Step 5: Implement `template.go`**

```go
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
    fmt.Fprintf(digest, "aboutme.migration-template.v%d\n", templateHarnessVersion)
    var last int64
    for _, source := range sources {
        if through > 0 && source.Version > through {
            break
        }
        data, err := fs.ReadFile(fsys, source.Path)
        if err != nil {
            return "", err
        }
        fmt.Fprintf(digest, "%d %s %d\n", source.Version, source.Path, len(data))
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
        if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtext($1)::bigint)`, name); err != nil {
            return fmt.Errorf("migrations: lock template build: %w", err)
        }
        defer func() {
            unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
            defer cancel()
            if _, err := conn.ExecContext(unlockCtx, `SELECT pg_advisory_unlock(hashtext($1)::bigint)`, name); err != nil {
                resultErr = errors.Join(resultErr, err)
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
        if _, err := conn.ExecContext(ctx, `CREATE DATABASE `+name); err != nil {
            return fmt.Errorf("migrations: create template: %w", err)
        }
        if err := buildTemplate(ctx, adminDSN, name, throughFS{FS: fsys, through: through}); err != nil {
            dropCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
            defer cancel()
            _, dropErr := conn.ExecContext(dropCtx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
            return errors.Join(err, dropErr)
        }
        if _, err := conn.ExecContext(ctx, `ALTER DATABASE `+name+` IS_TEMPLATE true`); err != nil {
            return fmt.Errorf("migrations: mark template: %w", err)
        }
        if _, err := conn.ExecContext(ctx, `ALTER DATABASE `+name+` ALLOW_CONNECTIONS false`); err != nil {
            return fmt.Errorf("migrations: seal template: %w", err)
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
    err := withAdminConn(ctx, adminDSN, func(conn *sql.Conn) error {
        _, err := conn.ExecContext(ctx, `CREATE DATABASE `+name+` TEMPLATE `+template)
        var pgErr *pgconn.PgError
        if errors.As(err, &pgErr) && pgErr.Code == "3D000" {
            return ErrTemplateMissing
        }
        return err
    })
    if err != nil {
        return err
    }
    dsn, err := databaseDSN(adminDSN, name)
    if err != nil {
        return err
    }
    db, err := sql.Open("pgx", dsn)
    if err != nil {
        return err
    }
    defer func() { _ = db.Close() }()
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
        rows, err := conn.QueryContext(ctx, `SELECT datname FROM pg_database WHERE datname LIKE 'aboutme_migrate_template_%' ORDER BY datname`)
        if err != nil {
            return err
        }
        var names []string
        for rows.Next() {
            var name string
            if err := rows.Scan(&name); err != nil {
                return errors.Join(err, rows.Close())
            }
            names = append(names, name)
        }
        if err := errors.Join(rows.Err(), rows.Close()); err != nil {
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
```

`DROP DATABASE ... WITH (FORCE)` on a database that is still marked a template
is refused by PostgreSQL, which is why the stale path unmarks first. `3D000` is
`invalid_catalog_name`, the code PostgreSQL raises when the template named in
`CREATE DATABASE` does not exist.

- [ ] **Step 6: Run the template tests**

Run:
`REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test ./migrations -run '^TestTemplateDatabaseName|^TestEnsureTemplate' -count=1 -v`
Expected: PASS. If `Status` on the clone reports zero pending instead of one,
`throughFS` did not hide version 20 from the provider: check how
`migrationSourcesFromFS` enumerates files and adapt `ReadDir` or add a `Glob`
method accordingly.

- [ ] **Step 7: Lint and commit**

Run: `GOGC=50 golangci-lint run ./migrations --tests`. Expected: 0 issues. Where
the shadow check reports an inner `err`, rename it (`rowErr`, `dropErr`) rather
than restructuring.

```sh
git add -- apps/server/migrations/template.go apps/server/migrations/template_test.go
git commit -m "feat(migrations): add local template database tooling" -- apps/server/migrations/template.go apps/server/migrations/template_test.go
```

---

### Task 3: Clone templates in the migration package tests

**Files:**

- Modify: `apps/server/migrations/migrator_apply_helpers_test.go`
- Modify:
  `apps/server/migrations/runtime_membership_schema_helpers_test.go:26-37`
- Modify: `apps/server/migrations/template_test.go` (cleanup keeps cached names)
- Modify: the fixture-start tests listed in Step 4

**Interfaces:**

- Consumes: Task 2 API.
- Produces: `newMigratedTestDatabase(t *testing.T, through int64) *sql.DB`;
  `runtimeMembershipDB` returns a clone at head.

- [ ] **Step 1: Write the failing helper test**

Add to `template_test.go`:

```go
func TestNewMigratedTestDatabaseClonesAndIsolates(t *testing.T) {
    ctx := context.Background()
    first := newMigratedTestDatabase(t, 0)
    second := newMigratedTestDatabase(t, 0)
    var firstName, secondName string
    if err := first.QueryRowContext(ctx, `SELECT current_database()`).Scan(&firstName); err != nil {
        t.Fatal(err)
    }
    if err := second.QueryRowContext(ctx, `SELECT current_database()`).Scan(&secondName); err != nil {
        t.Fatal(err)
    }
    if firstName == secondName || !disposableDatabasePattern.MatchString(firstName) {
        t.Fatalf("names %s/%s", firstName, secondName)
    }
    if err := membershipWrite(t, first, replicaInsert("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "i-aaaaaaaaaaaaaaaaa", "a")...); err != nil {
        t.Fatal(err)
    }
    var leaked int
    if err := second.QueryRowContext(ctx, `SELECT count(*) FROM public.runtime_replicas`).Scan(&leaked); err != nil || leaked != 0 {
        t.Fatalf("rows leaked between clones=%d error=%v", leaked, err)
    }
    statuses, err := Status(ctx, second, LocalAdminMigratorIdentity())
    if err != nil || PendingCount(statuses) != 0 {
        t.Fatalf("pending=%d error=%v", PendingCount(statuses), err)
    }
    older := newMigratedTestDatabase(t, 19)
    statuses, err = Status(ctx, older, LocalAdminMigratorIdentity())
    if err != nil || PendingCount(statuses) != 1 {
        t.Fatalf("version 19 pending=%d error=%v", PendingCount(statuses), err)
    }
}
```

- [ ] **Step 2: Run it and observe the missing helper**

Run:
`REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test ./migrations -run '^TestNewMigratedTestDatabase' -count=1`
Expected: build failure, `undefined: newMigratedTestDatabase`.

- [ ] **Step 3: Implement the helper and rewire `runtimeMembershipDB`**

In `migrator_apply_helpers_test.go` split `newCompositionTestDatabase` into
reusable pieces and add the clone path:

```go
var (
    templateNames   sync.Map
    templateBuildMu sync.Mutex
    templateSweep   sync.Once
)

func compositionBaseDSN(t *testing.T) string {
    t.Helper()
    base := os.Getenv("TEST_DATABASE_URL")
    if base == "" {
        if os.Getenv("REQUIRE_TEST_DB") == "1" {
            t.Fatal("TEST_DATABASE_URL required")
        }
        t.Skip("TEST_DATABASE_URL not set")
    }
    return base
}

func compositionAdmin(t *testing.T, base string) *sql.DB {
    t.Helper()
    admin, err := sql.Open("pgx", base)
    if err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() {
        if err := admin.Close(); err != nil {
            t.Errorf("close composition admin: %v", err)
        }
    })
    return admin
}

func compositionDatabaseNameFor(t *testing.T) string {
    t.Helper()
    name := fmt.Sprintf("aboutme_migrate_test_%d_%d", time.Now().UnixNano(), compositionDatabaseCounter.Add(1))
    if !compositionDatabaseName.MatchString(name) {
        t.Fatalf("unsafe composition database name %q", name)
    }
    return name
}

func openCompositionDatabase(t *testing.T, admin *sql.DB, base, name string) *sql.DB {
    t.Helper()
    t.Cleanup(func() {
        cleanupCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
        defer stop()
        if _, err := admin.ExecContext(cleanupCtx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`); err != nil {
            t.Errorf("drop composition database: %v", err)
        }
    })
    u, err := url.Parse(base)
    if err != nil {
        t.Fatal(err)
    }
    u.Path = "/" + strings.TrimPrefix(name, "/")
    db, err := sql.Open("pgx", u.String())
    if err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() {
        if err := db.Close(); err != nil {
            t.Errorf("close composition database: %v", err)
        }
    })
    return db
}

// newCompositionTestDatabase keeps returning an empty database for tests
// that prove provisioning, adoption and apply mechanics.
func newCompositionTestDatabase(t *testing.T) *sql.DB {
    t.Helper()
    base := compositionBaseDSN(t)
    admin := compositionAdmin(t, base)
    name := compositionDatabaseNameFor(t)
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+name); err != nil {
        t.Fatal(err)
    }
    return openCompositionDatabase(t, admin, base, name)
}

// expectedTemplateNames lists the template name of every embedded version so
// the stale sweep never drops a template another version still needs.
func expectedTemplateNames(t *testing.T) []string {
    t.Helper()
    sources, err := migrationSourcesFromFS(FS)
    if err != nil {
        t.Fatal(err)
    }
    names := make([]string, 0, len(sources))
    for _, source := range sources {
        name, err := TemplateDatabaseName(FS, source.Version)
        if err != nil {
            t.Fatal(err)
        }
        names = append(names, name)
    }
    return names
}

func ensureTestTemplate(t *testing.T, base string, through int64) string {
    t.Helper()
    if name, ok := templateNames.Load(through); ok {
        return name.(string)
    }
    templateBuildMu.Lock()
    defer templateBuildMu.Unlock()
    if name, ok := templateNames.Load(through); ok {
        return name.(string)
    }
    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
    defer cancel()
    templateSweep.Do(func() {
        if _, err := DropStaleTemplateDatabases(ctx, base, expectedTemplateNames(t)); err != nil {
            t.Fatalf("sweep stale templates: %v", err)
        }
    })
    name, err := EnsureTemplateDatabase(ctx, base, FS, through)
    if err != nil {
        t.Fatalf("build template through %d: %v", through, err)
    }
    templateNames.Store(through, name)
    return name
}

// newMigratedTestDatabase returns a provisioned disposable clone of the
// embedded sources applied through version through (0 means head).
func newMigratedTestDatabase(t *testing.T, through int64) *sql.DB {
    t.Helper()
    base := compositionBaseDSN(t)
    admin := compositionAdmin(t, base)
    name := compositionDatabaseNameFor(t)
    ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
    defer cancel()
    template := ensureTestTemplate(t, base, through)
    err := CloneTemplateDatabase(ctx, base, template, name)
    if errors.Is(err, ErrTemplateMissing) {
        templateNames.Delete(through)
        template = ensureTestTemplate(t, base, through)
        err = CloneTemplateDatabase(ctx, base, template, name)
    }
    if err != nil {
        t.Fatalf("clone template %s: %v", template, err)
    }
    return openCompositionDatabase(t, admin, base, name)
}
```

In `runtime_membership_schema_helpers_test.go` replace the body of
`runtimeMembershipDB`:

```go
func runtimeMembershipDB(t *testing.T) (*sql.DB, context.Context) {
    t.Helper()
    return newMigratedTestDatabase(t, 0), context.Background()
}
```

In `template_test.go` change both cleanups that call
`DropStaleTemplateDatabases(cleanup, base, nil)` to pass
`expectedTemplateNames(t)` instead of `nil`, so the sweep keeps the cached
templates other tests in the same process are using. The variant fixtures in
those tests still produce names outside that list, so they are still dropped.

- [ ] **Step 4: Switch the fixture-start tests to clones**

Run
`grep -n "runtimeTransitionFixtureFS(t, 1[5-9])" apps/server/migrations/*_test.go`.
For every test whose first migrator call is
`applyFS(ctx, db, runtimeTransitionFixtureFS(t, N), LocalAdminMigratorIdentity())`
directly after `newCompositionTestDatabase(t)` and `ProvisionDatabase`, replace
those three statements with `db := newMigratedTestDatabase(t, N)` and keep every
later `applyFS` call, which is the upgrade under test. Expected sites at the
time of writing: `runtime_membership_schema_test.go` (15),
`runtime_public_transition_schema_test.go` (16),
`runtime_shared_claim_schema_test.go` (16, 17),
`runtime_shared_rate_schema_test.go` (17, 18),
`runtime_replica_registration_test.go` (18),
`runtime_shared_claim_operations_test.go` (19). Do not touch
`migrator_enforcement_test.go`, `migrator_adopt_test.go`,
`migrator_apply_test.go`, or `migrator_status_test.go`; they exercise the empty
and partial paths on purpose.

The failed-migration tests that inject `SELECT 1/0` into a fixture stay valid:
the clone at N and the modified fixture through N+1 apply only N+1.

- [ ] **Step 5: Run the helper test, the switched files, and the runtime group**

Run:

```sh
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test ./migrations -run '^TestNewMigratedTestDatabase' -count=1 -v
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test ./migrations -run '^TestRuntime' -count=1
```

Expected: PASS, and the `^TestRuntime` run finishes well under 120 seconds (it
took 502 seconds across two groups before this change).

- [ ] **Step 6: Lint and commit**

Run: `GOGC=50 golangci-lint run ./migrations --tests`. Expected: 0 issues.

```sh
git add -- apps/server/migrations/migrator_apply_helpers_test.go apps/server/migrations/runtime_membership_schema_helpers_test.go apps/server/migrations/template_test.go apps/server/migrations/runtime_membership_schema_test.go apps/server/migrations/runtime_public_transition_schema_test.go apps/server/migrations/runtime_shared_claim_schema_test.go apps/server/migrations/runtime_shared_rate_schema_test.go apps/server/migrations/runtime_replica_registration_test.go apps/server/migrations/runtime_shared_claim_operations_test.go
git commit -m "test(migrations): clone template databases for runtime tests" -- apps/server/migrations/migrator_apply_helpers_test.go apps/server/migrations/runtime_membership_schema_helpers_test.go apps/server/migrations/template_test.go apps/server/migrations/runtime_membership_schema_test.go apps/server/migrations/runtime_public_transition_schema_test.go apps/server/migrations/runtime_shared_claim_schema_test.go apps/server/migrations/runtime_shared_rate_schema_test.go apps/server/migrations/runtime_replica_registration_test.go apps/server/migrations/runtime_shared_claim_operations_test.go
```

---

### Task 4: Clone templates in the store live tests

**Files:**

- Modify: `apps/server/internal/testutil/db.go`
- Test: `apps/server/internal/testutil/db_setup_test.go`
- Modify: `apps/server/internal/store/runtime_membership_test.go:203-256`

**Interfaces:**

- Consumes: `migrations.EnsureTemplateDatabase`,
  `migrations.CloneTemplateDatabase`, `migrations.ErrTemplateMissing`,
  `migrations.FS`.
- Produces:

```go
// NewMigratedTestDatabase returns the DSN and an open pool for a disposable
// clone of every embedded migration, dropped in t.Cleanup.
func NewMigratedTestDatabase(t *testing.T) (dsn string, db *sql.DB)
```

- [ ] **Step 1: Write the failing test**

```go
func TestNewMigratedTestDatabaseIsCloneAtHead(t *testing.T) {
    RequireTestDatabaseURL(t)
    dsn, db := NewMigratedTestDatabase(t)
    if !strings.Contains(dsn, "/aboutme_migrate_test_") {
        t.Fatalf("dsn %q", dsn)
    }
    statuses, err := migrations.Status(context.Background(), db, migrations.LocalAdminMigratorIdentity())
    if err != nil || migrations.PendingCount(statuses) != 0 {
        t.Fatalf("pending=%d error=%v", migrations.PendingCount(statuses), err)
    }
    other, otherDB := NewMigratedTestDatabase(t)
    if other == dsn {
        t.Fatal("clones share a database")
    }
    if err := otherDB.PingContext(context.Background()); err != nil {
        t.Fatal(err)
    }
}
```

- [ ] **Step 2: Run it and observe the missing function**

Run:
`TEST_DATABASE_URL=... go test ./internal/testutil -run '^TestNewMigratedTestDatabase' -count=1`
Expected: build failure, `undefined: NewMigratedTestDatabase`.

- [ ] **Step 3: Implement the wrapper**

```go
var (
    migratedTemplateOnce sync.Once
    migratedTemplateName string
    migratedTemplateErr  error
    migratedCloneCounter atomic.Uint64
)

// NewMigratedTestDatabase returns the DSN and an open pool for a disposable
// clone of every embedded migration. The clone is dropped in t.Cleanup.
func NewMigratedTestDatabase(t *testing.T) (string, *sql.DB) {
    t.Helper()
    base := RequireTestDatabaseURL(t)
    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
    defer cancel()
    migratedTemplateOnce.Do(func() {
        if err := bootstrapTestDatabaseRoles(ctx, base); err != nil {
            migratedTemplateErr = err
            return
        }
        migratedTemplateName, migratedTemplateErr = migrations.EnsureTemplateDatabase(ctx, base, migrations.FS, 0)
    })
    if migratedTemplateErr != nil {
        t.Fatalf("build migrated template: %v", migratedTemplateErr)
    }
    name := fmt.Sprintf("aboutme_migrate_test_%d_%d", time.Now().UnixNano(), migratedCloneCounter.Add(1))
    if err := migrations.CloneTemplateDatabase(ctx, base, migratedTemplateName, name); err != nil {
        t.Fatalf("clone migrated template: %v", err)
    }
    admin, err := sql.Open("pgx", base)
    if err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() {
        cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
        defer stop()
        if _, err := admin.ExecContext(cleanup, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`); err != nil {
            t.Errorf("drop migrated clone: %v", err)
        }
        if err := admin.Close(); err != nil {
            t.Errorf("close admin: %v", err)
        }
    })
    u, err := url.Parse(base)
    if err != nil {
        t.Fatal(err)
    }
    u.Path = "/" + name
    db, err := sql.Open("pgx", u.String())
    if err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() {
        if err := db.Close(); err != nil {
            t.Errorf("close migrated clone: %v", err)
        }
    })
    return u.String(), db
}
```

`RequireTestDatabaseURL` already skips or fails on a missing `TEST_DATABASE_URL`
exactly like the migration helpers.

- [ ] **Step 4: Replace `registrationLiveDatabase` in the store tests**

```go
func registrationLiveDatabase(t *testing.T) (string, *sql.DB) {
    t.Helper()
    return testutil.NewMigratedTestDatabase(t)
}
```

Remove the now-unused imports (`net/url`, `os`, `regexp` if no other use) and
the `writeRunnerDatabaseName` check if it becomes unused; `go vet` reports the
exact leftovers.

- [ ] **Step 5: Run the store live tests**

Run:
`REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./internal/store -run 'Live|Runtime' -v 2>&1 | tail -20`
Expected: PASS; `TestRuntimeClaim*` and `TestRuntimeReplicaRegistrationStore*`
live tests each finish in well under a second of setup.

- [ ] **Step 6: Lint and commit**

Run: `GOGC=50 golangci-lint run ./internal/testutil ./internal/store --tests`.

```sh
git add -- apps/server/internal/testutil/db.go apps/server/internal/testutil/db_setup_test.go apps/server/internal/store/runtime_membership_test.go
git commit -m "test(store): clone the migrated template database" -- apps/server/internal/testutil/db.go apps/server/internal/testutil/db_setup_test.go apps/server/internal/store/runtime_membership_test.go
```

---

### Task 5: Makefile timeout, cleanup target, and AGENTS.md note (root)

**Files:**

- Modify: `Makefile:442-446` and the `.PHONY` list on line 6
- Modify: `AGENTS.md` Gotchas

- [ ] **Step 1: Add the timeout and the cleanup target**

Recipe lines below start with a tab, as Make requires.

<!-- markdownlint-disable MD010 -->

```make
server-migration-test: ## Run the migration harness + migrate CLI (needs test-db-up or TEST_DATABASE_URL)
	@printf '%s\n' 'server-migration-test: go test migration harness and CLI packages'
	@cd apps/server && TEST_DATABASE_URL=$${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable} \
	  go test ./migrations/... ./cmd/migrate/... -count=1 -timeout 30m -v

test-db-templates-clean: ## Drop cached migration template databases from the shared container
	@url="$${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable}"; \
	  for d in $$(psql "$$url" -Atc "SELECT datname FROM pg_database WHERE datname LIKE 'aboutme_migrate_template_%'"); do \
	    psql "$$url" -Atc "ALTER DATABASE \"$$d\" IS_TEMPLATE false; DROP DATABASE \"$$d\" WITH (FORCE)"; \
	  done
```

<!-- markdownlint-enable MD010 -->

Add `test-db-templates-clean` to the `.PHONY` line.

- [ ] **Step 2: Add one Gotchas line to AGENTS.md**

```markdown
- Migration and store live tests clone hash-keyed template databases named
  `aboutme_migrate_template_<version>_<hash>`; a SQL edit builds a new one and
  drops stale ones. `make test-db-templates-clean` removes them all.
```

- [ ] **Step 3: Verify formatting and the target**

Run:

```sh
npx prettier --check --ignore-path /dev/null AGENTS.md CLAUDE.md && npx markdownlint-cli2 AGENTS.md CLAUDE.md
make test-db-templates-clean
psql "$TEST_DATABASE_URL" -Atc "SELECT count(*) FROM pg_database WHERE datname LIKE 'aboutme_migrate_template_%'"
```

Expected: lint clean; the count prints `0`.

- [ ] **Step 4: Commit**

```sh
git add -- Makefile AGENTS.md
git commit -m "build: bound migration test time and add template cleanup" -- Makefile AGENTS.md
```

---

### Task 6: Measure, record, and review

**Files:**

- Modify: `docs/plans/phase-10/replica/migration-test-templates.md` (this file,
  evidence section)

- [ ] **Step 1: Time the package three ways, one job at a time**

```sh
cd apps/server
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -count=1 ./migrations 2>&1 | tail -1
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./migrations 2>&1 | tail -1
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./migrations -run '^TestRuntimeSharedClaimOperations' 2>&1 | tail -1
cd ../.. && make server-migration-test 2>&1 | tail -1 && make server-test-db 2>&1 | grep -c '^ok'
```

Expected: non-race under 180 seconds; race under 600 seconds; focused race still
green; `server-migration-test` ok; `server-test-db` 13 packages ok.

- [ ] **Step 2: Confirm nothing leaks**

```sh
psql "$TEST_DATABASE_URL" -Atc "SELECT datname FROM pg_database WHERE datname LIKE 'aboutme_migrate_%' ORDER BY 1"
```

Expected: only `aboutme_migrate_template_<v>_<hash>` rows for the versions the
run used (at most one per version), no `aboutme_migrate_test_` rows.

- [ ] **Step 3: Record evidence in this plan**

Append an `## Implementation evidence` section with the four timings, the
`server-test-db` count, the template list, and any deviation from the steps
above. Run `npx prettier --write --ignore-path /dev/null` on this file.

- [ ] **Step 4: Fresh review and commit**

A reviewer who authored none of Tasks 1 through 5 reads the integrated diff and
confirms by name: no template is ever connected to after sealing; every clone
name is in the disposable class; the advisory lock covers check, build, and
mark; `ProvisionDatabase` runs on every clone; and the migrator tests that prove
empty-database paths still start empty. Then:

```sh
git add -- docs/plans/phase-10/replica/migration-test-templates.md
git commit -m "docs: record migration template test evidence" -- docs/plans/phase-10/replica/migration-test-templates.md
```

## Implementation evidence

Delivered in six commits from `d7db2b6`: the plan, the name class, the template
tooling, the runtime test conversion, the rate helper and store conversion, and
the Makefile and instruction updates. Three authors wrote the slices; the
integration owner reran every check before each commit.

### Measurements

All on the shared `aboutme-test-db` container, one job at a time. The "after"
migrations figures start from a cleared template cache, so they include building
all six templates.

| Measurement                                  | Before          | After            |
| -------------------------------------------- | --------------- | ---------------- |
| `./migrations`, no race                      | 528s            | 140s             |
| `./migrations`, `-race`                      | timed out, 600s | 169s             |
| `^TestRuntime` group                         | 502s            | 187s             |
| `^TestRuntimeSharedRate` subset              | 109s            | 30s              |
| `^TestRuntimeSharedClaimOperations`, `-race` | 20.6s           | 6.5s             |
| `make server-migration-test`                 | over 535s       | 139s             |
| `make server-test-db`                        | 13 packages     | 13 packages, 35s |
| `./internal/store` `-race`                   | 9.6s            | 8.4s             |

The goal is met: one `-race` run of the whole package now finishes in 169
seconds, inside go test's ten-minute default, with headroom for migrations 21
through 25. `make server-migration-test` still carries an explicit
`-timeout 30m` so growth fails loudly rather than at an implicit limit.

### Defects found while implementing

1. **A defect in this plan.** The Design section claimed `ProvisionDatabase` on
   a clone restores its database grants. It does not: a database ACL lives in
   the `pg_database` row, which `CREATE DATABASE ... TEMPLATE` does not copy,
   and `ProvisionDatabase` installs grants only when the runtime foundation is
   absent, so on a clone it is validation-only and fails with provisioning
   drift. `CloneTemplateDatabase` now reissues the two database-level grants
   from `installProvisioning` before validating. The Design section is
   corrected.
2. **A gap in the Task 3 brief.** `runtime_shared_rate_schema_helpers_test.go`
   was not in the owned paths, so 29 rate tests kept applying every migration.
   Converted separately; it was the single largest remaining cost.
3. `throughFS` needed no `Glob` method: `fs.Glob` falls back to `fs.ReadDir` for
   a filesystem that does not implement `fs.GlobFS`, and
   `migrationSourcesFromFS` uses `fs.ReadDir` as well. Proven by a pure test.
4. Lint required renaming shadowed inner `err` variables and using the two-value
   type assertion, because `errcheck.check-type-assertions` is on.

### Fresh review

The reviewer confirmed all five named invariants with cited evidence and
returned CLEAR WITH FINDINGS: no template is connected to after sealing, clone
names stay in the disposable class, the advisory lock covers check, build and
mark as one critical section, every clone is provisioned, and the tests that
prove empty-database paths still start empty. It also compared every converted
call site against its pre-change form and found each assertion intact.

Five should-fix findings were fixed before this section was written:

1. `newMigratedTestDatabase` started the clone's 60-second deadline before a
   template build that may take three minutes, and its retry reused the
   exhausted context. The plan carried the same ordering bug. The template is
   now built first, and the retry gets a fresh budget.
2. `DropStaleTemplateDatabases` unmarked and dropped without the per-name
   advisory lock, so it could interrupt a build in another test binary. It now
   takes `pg_try_advisory_lock` per name and skips a name it cannot lock, since
   a template someone is building is not stale. Proved by a test that fails
   against the unfixed code.
3. `testutil.NewMigratedTestDatabase` cached the template name in a `sync.Once`
   that could not re-fire and had no `ErrTemplateMissing` recovery, so a swept
   template broke every later store test permanently. It now invalidates and
   rebuilds once, like the migrations helper.
4. `TestNewMigratedTestDatabaseIsCloneAtHead` never ran live, because
   `server-test-db` omitted `./internal/testutil`. The package is now in that
   target, which runs 14 packages rather than 13.
5. The instruction note claimed a migration edit drops the stale template. Only
   a `./migrations` run sweeps, so other live-DB targets accumulate templates.
   The note now says so and points at the cleanup target.

### Portability confirmed

The risk this plan flagged did not materialize. A cloned database upgrades
cleanly through the real migrator: the template test builds a clone at version
19 and applies version 20 to it through `applyFS`, and the B3 identity, history
and manifest checks accept it. Clones also pass `ProvisionDatabase` validation,
which is what proves the reissued grants are exact.

### State after the run

The container holds `aboutme`, `aboutme_dev` and at most one
`aboutme_migrate_template_<version>_<hash>` per version the run used. No
disposable test database leaked in any measured run.
`make test-db-templates-clean` drops every template.

## Risks and fallbacks

- If `CREATE DATABASE ... TEMPLATE` fails with "source database is being
  accessed by other users", a connection to the template survived sealing.
  `buildTemplate` closes its pool before the `ALTER DATABASE` statements; check
  that nothing else opened the template DSN.
- If the B3 identity checks reject a clone, the failure appears in
  `TestEnsureTemplateDatabaseBuildsOnceAndClonesProvisioned` at the `applyFS`
  upgrade step. That would be a real portability assumption in the migrator and
  must be reported, not patched around in the harness.
- If the container runs out of disk, templates are the cause: each is about 20
  MB; six versions plus variants stay under 200 MB. Run
  `make test-db-templates-clean`.
