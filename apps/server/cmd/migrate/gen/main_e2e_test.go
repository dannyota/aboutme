// End-to-end tests for the real Atlas generation pipeline: gated behind a
// live Postgres server (TEST_DATABASE_URL, exactly like the migrations
// package's harness tests) and requiring the real Atlas CLI on PATH. This
// is the only place that actually invokes Atlas — main_test.go's hermetic
// tests cover pure post-processing logic (headers, renaming, publish,
// extension cross-checking) in isolation, per that file's package doc
// comment.
//
// TestRun_EndToEnd_GooseFormatReplayAcrossExtensionBoundary reproduces and
// guards the --dir-format bug the data-layer review flagged
// (review-datalayer.txt "Critical"): without --dir-format goose, Atlas
// replays a goose migration's Up *and* Down sections as one
// undifferentiated script when reconstructing directory history, so a
// hand-written extension migration's Down section (DROP EXTENSION) silently
// undoes its own Up section during replay. That alone doesn't fail a
// two-migration diff (schema.sql's own CREATE EXTENSION statement is
// enough for evaluating the *desired* end-state on its own), but it breaks
// the very next generation once Atlas must replay *two* prior migrations —
// the extension one and a citext-dependent one — to reconstruct current
// state: replaying the second then fails with `type "public.citext" does
// not exist`, because the (buggy) replay of the first already dropped it.
// This was verified empirically against this exact scenario before the fix
// landed; see the task report for the observed error text.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/dannyota/aboutme/apps/server/migrations"
)

// e2eTimeout bounds every end-to-end test's live-database/Atlas work.
// Generous: each test invokes the real Atlas CLI (a subprocess) twice and
// talks to a real Postgres server several times.
const e2eTimeout = 60 * time.Second

// realExtensionsMigrationPath points at the actual committed goose
// extension migration — three directories up from this package
// (cmd/migrate/gen -> cmd/migrate -> cmd -> apps/server, then into
// migrations/) — so these tests exercise the real Up/Down migration this
// tool must handle correctly, not a synthetic stand-in. Its checksum is
// never borrowed from the real committed atlas.sum; see
// seedRealExtensionsMigration's doc comment for why.
const (
	realExtensionsMigrationPath = "../../../migrations/00001_extensions.sql"
)

func requireAtlas(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("atlas"); err != nil {
		t.Skip("atlas CLI not on PATH; skipping end-to-end generator test " +
			"(see main.go's package doc comment for the pinned install command)")
	}
}

func requireTestDatabaseURL(t *testing.T) string {
	t.Helper()
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping end-to-end generator test")
	}
	return dbURL
}

var genTestDatabaseCounter atomic.Uint64

// newGenTestDatabase creates a fresh, uniquely named database on the
// server pointed to by base and returns a connection URL for it, dropped
// in t.Cleanup. This mirrors migrations/testdb_test.go's newTestDatabase:
// each live-DB test package in this repo keeps its own small copy of this
// helper (see that file's package doc comment for the established
// convention) rather than sharing one across packages.
func newGenTestDatabase(t *testing.T, base string) string {
	t.Helper()

	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatalf("open admin connection: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := admin.Close(); closeErr != nil {
			t.Logf("close admin connection: %v", closeErr)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = admin.PingContext(ctx); err != nil {
		t.Fatalf("ping admin connection (is TEST_DATABASE_URL reachable?): %v", err)
	}

	name := fmt.Sprintf("aboutme_gen_test_%d_%d", time.Now().UnixNano(), genTestDatabaseCounter.Add(1))
	if _, err = admin.ExecContext(ctx, `CREATE DATABASE `+name); err != nil {
		t.Fatalf("create database %s: %v", name, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, dropErr := admin.ExecContext(cleanupCtx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`); dropErr != nil {
			t.Logf("cleanup: drop database %s: %v", name, dropErr)
		}
	})

	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	u.Path = "/" + name
	return u.String()
}

func openGenTestDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Logf("close database: %v", closeErr)
		}
	})
	return db
}

// copyRealFile copies the real, committed file at srcRelPath (relative to
// this test file's package directory) to dst.
func copyRealFile(t *testing.T, srcRelPath, dst string) {
	t.Helper()
	data, err := os.ReadFile(srcRelPath) //nolint:gosec // srcRelPath is always one of this file's own constants, never external input
	if err != nil {
		t.Fatalf("read %s: %v", srcRelPath, err)
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}

// seedRealExtensionsMigration copies the real, committed
// 00001_extensions.sql into migrationsDir and then hashes migrationsDir
// with Atlas to produce a fresh, self-consistent atlas.sum scoped to
// exactly that one file. It deliberately does NOT copy the real repo's own
// committed atlas.sum (realAtlasSumPath): that file's checksum list grows
// with every real product migration, so copying it wholesale next to only
// a single-file migrationsDir works only by coincidence, for exactly as
// long as the real migrations directory happens to contain exactly one
// file. These e2e tests intentionally isolate the extensions migration
// from however many other real product migrations exist (see the package
// doc comment: they reproduce the extension-replay bug in isolation), so
// their atlas.sum must be generated for what's actually on disk here, not
// borrowed from the full real directory.
func seedRealExtensionsMigration(t *testing.T, migrationsDir string) {
	t.Helper()
	copyRealFile(t, realExtensionsMigrationPath, filepath.Join(migrationsDir, "00001_extensions.sql"))
	if err := runAtlas("migrate", "hash", "--dir", "file://"+migrationsDir, "--dir-format", dirFormat); err != nil {
		t.Fatalf("atlas migrate hash (seed atlas.sum for 00001_extensions.sql): %v", err)
	}
}

// onlyNewMigrationFile returns the single *.sql filename in dir not listed
// in exclude, failing the test if there isn't exactly one.
func onlyNewMigrationFile(t *testing.T, dir string, exclude ...string) string {
	t.Helper()

	excluded := map[string]bool{}
	for _, name := range exclude {
		excluded[name] = true
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}

	var found string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") || excluded[e.Name()] {
			continue
		}
		if found != "" {
			t.Fatalf("found more than one new migration file in %s: %s and %s", dir, found, e.Name())
		}
		found = e.Name()
	}
	if found == "" {
		t.Fatalf("no new migration file found in %s", dir)
	}
	return found
}

func newTempProject(t *testing.T) (migrationsDir, schemaFile string) {
	t.Helper()

	root := t.TempDir()
	migrationsDir = filepath.Join(root, "migrations")
	schemaFile = filepath.Join(root, "sql", "schema.sql")
	if err := os.MkdirAll(migrationsDir, 0o750); err != nil {
		t.Fatalf("MkdirAll(%s): %v", migrationsDir, err)
	}
	if err := os.MkdirAll(filepath.Dir(schemaFile), 0o750); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(schemaFile), err)
	}
	return migrationsDir, schemaFile
}

const schemaWidgets = `CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE widgets (
	id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	name citext NOT NULL
);
`

const schemaWidgetsAndGadgets = schemaWidgets + `
CREATE TABLE gadgets (
	id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	label citext NOT NULL
);
`

// TestRun_EndToEnd_GooseFormatReplayAcrossExtensionBoundary is the item-1
// regression test: see the package doc comment above for the exact bug
// this reproduces and guards.
func TestRun_EndToEnd_GooseFormatReplayAcrossExtensionBoundary(t *testing.T) {
	requireAtlas(t)
	adminURL := requireTestDatabaseURL(t)

	migrationsDir, schemaFile := newTempProject(t)
	seedRealExtensionsMigration(t, migrationsDir)

	t.Setenv("DATABASE_URL", adminURL)

	// Step 1: generate migration 2, a citext-dependent table. This step
	// alone does not exercise the dir-format bug (see the package doc
	// comment) — it's here so step 2 has two prior migrations to replay.
	writeFile(t, schemaFile, schemaWidgets)
	if err := run("widgets", false, migrationsDir, schemaFile); err != nil {
		t.Fatalf("run() (step 1: widgets) error: %v", err)
	}
	step1 := onlyNewMigrationFile(t, migrationsDir, "00001_extensions.sql", "atlas.sum")
	if !strings.HasPrefix(step1, "00002_") {
		t.Fatalf("step 1 generated %q, want a 00002_-prefixed file", step1)
	}

	// Step 2: generate migration 3. Atlas must now replay migrations 1 AND
	// 2 to reconstruct "current" state before diffing against schema v2 —
	// this is what actually exercises the bug (see package doc comment).
	writeFile(t, schemaFile, schemaWidgetsAndGadgets)
	if err := run("gadgets", false, migrationsDir, schemaFile); err != nil {
		t.Fatalf("run() (step 2: gadgets) error: %v", err)
	}
	step2 := onlyNewMigrationFile(t, migrationsDir, "00001_extensions.sql", "atlas.sum", step1)
	if !strings.HasPrefix(step2, "00003_") {
		t.Fatalf("step 2 generated %q, want a 00003_-prefixed file", step2)
	}

	content, err := os.ReadFile(filepath.Join(migrationsDir, step2)) //nolint:gosec // step2 comes from onlyNewMigrationFile(migrationsDir), never external input
	if err != nil {
		t.Fatalf("read generated migration: %v", err)
	}
	sql := string(content)
	if !strings.Contains(sql, "-- +goose Up") {
		t.Errorf("generated migration missing goose Up header:\n%s", sql)
	}
	if strings.Contains(sql, "DROP EXTENSION") {
		t.Errorf("generated migration replays migration 1's rollback SQL (contains DROP EXTENSION):\n%s", sql)
	}
	if !strings.Contains(strings.ToLower(sql), "gadgets") {
		t.Errorf("generated migration does not create the gadgets table:\n%s", sql)
	}

	// Prove the full three-migration set is genuinely valid, goose-
	// replayable SQL against a fresh database — not just something Atlas
	// happened not to error on — by applying it for real through the same
	// production Provider cmd/migrate uses.
	dsn := newGenTestDatabase(t, adminURL)
	db := openGenTestDB(t, dsn)
	fsys := os.DirFS(migrationsDir)
	provider, err := migrations.NewProvider(db, fsys)
	if err != nil {
		t.Fatalf("NewProvider() error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), e2eTimeout)
	defer cancel()
	results, err := provider.Up(ctx)
	if err != nil {
		t.Fatalf("Up() error applying the generated migrations: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("Up() applied %d migrations, want 3 (extensions, widgets, gadgets)", len(results))
	}

	var citextCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_extension WHERE extname = 'citext'`).Scan(&citextCount); err != nil {
		t.Fatalf("query pg_extension: %v", err)
	}
	if citextCount != 1 {
		t.Errorf("pg_extension citext count = %d, want 1", citextCount)
	}

	var tableCount int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM information_schema.tables WHERE table_name IN ('widgets', 'gadgets')`,
	).Scan(&tableCount); err != nil {
		t.Fatalf("query information_schema.tables: %v", err)
	}
	if tableCount != 2 {
		t.Errorf("widgets+gadgets table count = %d, want 2", tableCount)
	}
}

// TestRun_AtomicPublish_MigrationsDirUntouchedUntilSuccess proves
// generation is atomic end-to-end (item 1's second requirement): the real
// migrationsDir gains the new file only after a successful run, and a
// failed run (schema.sql referencing an undeclared extension, caught by
// checkExtensionDeclarations before Atlas or the database are even
// touched) leaves migrationsDir completely unchanged — not a leftover
// temp directory, not a partial file.
func TestRun_AtomicPublish_MigrationsDirUntouchedUntilSuccess(t *testing.T) {
	requireAtlas(t)
	adminURL := requireTestDatabaseURL(t)

	migrationsDir, schemaFile := newTempProject(t)
	seedRealExtensionsMigration(t, migrationsDir)

	before, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("ReadDir(migrationsDir): %v", err)
	}
	beforeNames := make([]string, 0, len(before))
	for _, e := range before {
		beforeNames = append(beforeNames, e.Name())
	}

	t.Setenv("DATABASE_URL", adminURL)

	// schema.sql references pgcrypto, which has no matching hand-written
	// migration: checkExtensionDeclarations rejects this before Atlas or
	// DATABASE_URL are even consulted for real work.
	writeFile(t, schemaFile, "CREATE EXTENSION IF NOT EXISTS pgcrypto;\n")
	if err = run("broken", false, migrationsDir, schemaFile); err == nil {
		t.Fatal("run() error = nil, want an extension-declaration error")
	}

	after, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("ReadDir(migrationsDir) after failed run: %v", err)
	}
	afterNames := make([]string, 0, len(after))
	for _, e := range after {
		afterNames = append(afterNames, e.Name())
	}
	if len(afterNames) != len(beforeNames) {
		t.Fatalf("migrationsDir contents changed after a failed run: before=%v after=%v", beforeNames, afterNames)
	}
	for i := range beforeNames {
		if beforeNames[i] != afterNames[i] {
			t.Fatalf("migrationsDir contents changed after a failed run: before=%v after=%v", beforeNames, afterNames)
		}
	}

	// A genuinely successful run afterward still works and installs
	// exactly one new file, confirming the failed attempt above left no
	// stray temp/backup directory behind that would interfere.
	writeFile(t, schemaFile, schemaWidgets)
	if err = run("widgets", false, migrationsDir, schemaFile); err != nil {
		t.Fatalf("run() (recovery) error: %v", err)
	}
	generated := onlyNewMigrationFile(t, migrationsDir, "00001_extensions.sql", "atlas.sum")
	if !strings.HasPrefix(generated, "00002_") {
		t.Errorf("recovery run generated %q, want a 00002_-prefixed file", generated)
	}

	parent := filepath.Dir(migrationsDir)
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("ReadDir(parent): %v", err)
	}
	for _, e := range entries {
		if e.Name() != "migrations" && e.Name() != "sql" {
			t.Errorf("stray entry left in project root after generation: %s", e.Name())
		}
	}
}

// TestRun_EndToEnd_BootstrapsFirstMigrationWithNoExistingDirectory is the
// item-1 regression test for review finding I1: the very first
// `migrate-gen` run in a brand-new project has no migrations/ directory
// at all yet. Every other e2e test in this file pre-seeds migrationsDir
// via newTempProject + copyRealFile before calling run(), so none of them
// covers this path — publish's own doc comment explains why a missing
// dir needs different handling than an existing one.
//
// Deliberately has no CREATE EXTENSION: the real bootstrap workflow
// hand-authors any extension migration (Atlas can never diff CREATE
// EXTENSION at all — see checkExtensionDeclarations' doc comment) before
// ever running migrate-gen, so a schema.sql with an extension and no
// migrations directory yet is not actually the first-ever-generation
// scenario this test targets; it's the checkExtensionDeclarations
// scenario already covered by TestRun_ExtensionMismatch_FailsFastWithoutAtlasOrDatabase
// and the hermetic TestCheckExtensionDeclarations_* tests.
func TestRun_EndToEnd_BootstrapsFirstMigrationWithNoExistingDirectory(t *testing.T) {
	requireAtlas(t)
	adminURL := requireTestDatabaseURL(t)

	root := t.TempDir()
	migrationsDir := filepath.Join(root, "migrations") // deliberately never created
	schemaFile := filepath.Join(root, "sql", "schema.sql")
	if err := os.MkdirAll(filepath.Dir(schemaFile), 0o750); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(schemaFile), err)
	}
	writeFile(t, schemaFile,
		"CREATE TABLE widgets (\n\tid bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,\n\tname text NOT NULL\n);\n")

	t.Setenv("DATABASE_URL", adminURL)

	if err := run("init", false, migrationsDir, schemaFile); err != nil {
		t.Fatalf("run() error on a project with no existing migrations directory: %v", err)
	}

	generated := onlyNewMigrationFile(t, migrationsDir)
	if !strings.HasPrefix(generated, "00001_") {
		t.Errorf("bootstrap run generated %q, want a 00001_-prefixed name for the first-ever migration", generated)
	}
	if _, err := os.Stat(filepath.Join(migrationsDir, "atlas.sum")); err != nil {
		t.Errorf("atlas.sum missing after bootstrap generation: %v", err)
	}

	// Review finding M1: publish must not leave migrationsDir behind at
	// os.MkdirTemp's restrictive default (0700) — it should carry the
	// conventional directory permission (0755) instead, matching what a
	// normal `mkdir` (or a fresh git checkout) produces.
	dirInfo, err := os.Stat(migrationsDir)
	if err != nil {
		t.Fatalf("Stat(migrationsDir): %v", err)
	}
	const wantDirMode = 0o755
	if got := dirInfo.Mode().Perm(); got != wantDirMode {
		t.Errorf("migrationsDir mode = %o, want %o (the conventional directory mode, not MkdirTemp's restrictive 0700 default)",
			got, wantDirMode)
	}

	content, err := os.ReadFile(filepath.Join(migrationsDir, generated)) //nolint:gosec // generated comes from onlyNewMigrationFile(migrationsDir), never external input
	if err != nil {
		t.Fatalf("read generated migration: %v", err)
	}
	sql := string(content)
	if !strings.Contains(sql, "-- +goose Up") {
		t.Errorf("generated migration missing goose Up header:\n%s", sql)
	}
	if !strings.Contains(strings.ToLower(sql), "widgets") {
		t.Errorf("generated migration does not create the widgets table:\n%s", sql)
	}

	// No stray backup directory (publish's no-existing-dir path never
	// creates one — there was nothing to move aside) and no leftover temp
	// generation directory.
	parent := filepath.Dir(migrationsDir)
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("ReadDir(parent): %v", err)
	}
	for _, e := range entries {
		if e.Name() != "migrations" && e.Name() != "sql" {
			t.Errorf("stray entry left in project root after bootstrap generation: %s", e.Name())
		}
	}
}

// -----------------------------------------------------------------------
// -check (drift gate)
// -----------------------------------------------------------------------

// TestRun_Check_NoDriftWhenSchemaMatches proves -check exits cleanly and
// writes nothing when sql/schema.sql already matches the migration
// directory — the "replay Goose migrations and assert an Atlas
// goose-format diff against sql/schema.sql is empty" half of item 5's
// drift gate.
func TestRun_Check_NoDriftWhenSchemaMatches(t *testing.T) {
	requireAtlas(t)
	adminURL := requireTestDatabaseURL(t)

	migrationsDir, schemaFile := newTempProject(t)
	seedRealExtensionsMigration(t, migrationsDir)
	// The real 00001_extensions.sql's Up section is exactly this
	// statement — schema.sql declares the same extension and nothing
	// else, so there is nothing left for Atlas to diff.
	writeFile(t, schemaFile, "CREATE EXTENSION IF NOT EXISTS citext;\n")

	before, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("ReadDir(migrationsDir): %v", err)
	}

	t.Setenv("DATABASE_URL", adminURL)

	if err = run("", true, migrationsDir, schemaFile); err != nil {
		t.Fatalf("run(check=true) error = %v, want nil when schema.sql matches the migration directory", err)
	}

	after, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("ReadDir(migrationsDir) after check: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("-check wrote to migrationsDir: before had %d entries, after has %d", len(before), len(after))
	}
}

// TestRun_Check_DetectsDrift proves -check fails (non-zero-equivalent
// error) and still writes nothing when sql/schema.sql has a change no
// migration captures yet.
func TestRun_Check_DetectsDrift(t *testing.T) {
	requireAtlas(t)
	adminURL := requireTestDatabaseURL(t)

	migrationsDir, schemaFile := newTempProject(t)
	seedRealExtensionsMigration(t, migrationsDir)
	// schemaWidgets adds a table no migration captures yet.
	writeFile(t, schemaFile, schemaWidgets)

	before, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("ReadDir(migrationsDir): %v", err)
	}

	t.Setenv("DATABASE_URL", adminURL)

	err = run("", true, migrationsDir, schemaFile)
	if err == nil {
		t.Fatal("run(check=true) error = nil, want an error: schema.sql has an uncaptured table")
	}
	if !strings.Contains(err.Error(), "drift") {
		t.Errorf("run(check=true) error = %q, want it to mention drift", err)
	}

	after, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("ReadDir(migrationsDir) after check: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("-check wrote to migrationsDir despite detecting drift: before had %d entries, after has %d",
			len(before), len(after))
	}
}
