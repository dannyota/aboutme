// Tests for migrate-gen's post-processing logic: goose-header injection,
// sequential renaming, atomic publish, and extension-declaration
// cross-checking. These are pure filesystem operations, so they run
// hermetically (no Atlas CLI, no database) as part of `go test ./...`.
// The end-to-end path (invoking Atlas for real against a live database) is
// in main_e2e_test.go, gated behind TEST_DATABASE_URL and the Atlas CLI
// being on PATH.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureGooseHeader_PrependsWhenMissing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "00002_add_thing.sql")
	original := `-- Create "thing" table
CREATE TABLE "public"."thing" ("id" bigint NOT NULL);
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := ensureGooseHeader(path); err != nil {
		t.Fatalf("ensureGooseHeader() error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := "-- +goose Up\n" + original
	if string(got) != want {
		t.Errorf("content =\n%s\nwant\n%s", got, want)
	}
}

func TestEnsureGooseHeader_IdempotentWhenAlreadyPresent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "00002_add_thing.sql")
	original := "-- +goose Up\nCREATE TABLE thing (id bigint);\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := ensureGooseHeader(path); err != nil {
		t.Fatalf("ensureGooseHeader() error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != original {
		t.Errorf("content changed on a file that already had the header:\ngot  %q\nwant %q", got, original)
	}
}

// TestEnsureGooseHeader_IdempotentWithUpAndDownSections covers the shape
// Atlas actually emits with --dir-format goose (verified empirically):
// both a "-- +goose Up" and a "-- +goose Down" section, with real reverse
// DDL in the Down section. ensureGooseHeader is a defensive no-op here,
// not the header-adding path it was written for originally — see its doc
// comment.
func TestEnsureGooseHeader_IdempotentWithUpAndDownSections(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "00002_add_thing.sql")
	original := "-- +goose Up\n" +
		"-- create \"thing\" table\n" +
		"CREATE TABLE \"public\".\"thing\" (\"id\" bigint NOT NULL);\n\n" +
		"-- +goose Down\n" +
		"-- reverse: create \"thing\" table\n" +
		"DROP TABLE \"public\".\"thing\";\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := ensureGooseHeader(path); err != nil {
		t.Fatalf("ensureGooseHeader() error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != original {
		t.Errorf("content changed on a file Atlas already wrote with both Up and Down sections:\ngot  %q\nwant %q", got, original)
	}
}

func TestNextSequence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files []string
		want  int
	}{
		{name: "empty directory", files: nil, want: 1},
		{name: "single existing migration", files: []string{"00001_extensions.sql"}, want: 2},
		{
			name:  "picks the max, not the count",
			files: []string{"00001_extensions.sql", "00003_add_resumes.sql", "00002_add_users.sql"},
			want:  4,
		},
		{
			name:  "ignores non-sequential and non-sql names",
			files: []string{"00001_extensions.sql", "atlas.sum", "README.md"},
			want:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			for _, f := range tt.files {
				if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
					t.Fatalf("WriteFile(%s): %v", f, err)
				}
			}

			got, err := nextSequence(dir)
			if err != nil {
				t.Fatalf("nextSequence() error: %v", err)
			}
			if got != tt.want {
				t.Errorf("nextSequence() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestNextSequence_MissingDirectory(t *testing.T) {
	t.Parallel()

	got, err := nextSequence(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("nextSequence() error: %v", err)
	}
	if got != 1 {
		t.Errorf("nextSequence() = %d, want 1 for a missing directory", got)
	}
}

func TestRenameSequential_RenamesTimestampedFilesInOrder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	seed := filepath.Join(dir, "00001_extensions.sql")
	if err := os.WriteFile(seed, []byte("-- +goose Up\nCREATE EXTENSION citext;\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Atlas can, in principle, write more than one file per diff (rare,
	// but the diff algorithm is not obligated to produce exactly one). Two
	// timestamped files must come out numbered 00002 then 00003, in
	// filename (chronological) order, regardless of the order they're
	// passed in.
	older := filepath.Join(dir, "20260101000000_add_users.sql")
	newer := filepath.Join(dir, "20260101000001_add_sessions.sql")
	for _, f := range []string{older, newer} {
		if err := os.WriteFile(f, []byte("-- content\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", f, err)
		}
	}

	got, err := renameSequential(dir, []string{newer, older}) // deliberately out of order
	if err != nil {
		t.Fatalf("renameSequential() error: %v", err)
	}

	want := []string{
		filepath.Join(dir, "00002_add_users.sql"),
		filepath.Join(dir, "00003_add_sessions.sql"),
	}
	if len(got) != len(want) {
		t.Fatalf("renameSequential() returned %d paths, want %d: %v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("renameSequential()[%d] = %s, want %s", i, got[i], w)
		}
		if _, err := os.Stat(w); err != nil {
			t.Errorf("expected renamed file %s to exist: %v", w, err)
		}
	}
	if _, err := os.Stat(older); !os.IsNotExist(err) {
		t.Errorf("original timestamped file %s should no longer exist", older)
	}
}

func TestRenameSequential_LeavesNonTimestampedNamesAlone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	already := filepath.Join(dir, "00002_hand_written.sql")
	if err := os.WriteFile(already, []byte("-- content\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := renameSequential(dir, []string{already})
	if err != nil {
		t.Fatalf("renameSequential() error: %v", err)
	}
	if len(got) != 1 || got[0] != already {
		t.Errorf("renameSequential() = %v, want unchanged path %s", got, already)
	}
	if _, err := os.Stat(already); err != nil {
		t.Errorf("file should still exist at its original path: %v", err)
	}
}

func TestSqlFiles_MissingDirectoryIsEmptyNotError(t *testing.T) {
	t.Parallel()

	got := sqlFiles(filepath.Join(t.TempDir(), "does-not-exist"))
	if len(got) != 0 {
		t.Errorf("sqlFiles() = %v, want empty for a missing directory", got)
	}
}

func TestNewFiles_OnlyReturnsAdditions(t *testing.T) {
	t.Parallel()

	before := map[string]bool{"a.sql": true, "b.sql": true}
	after := map[string]bool{"a.sql": true, "b.sql": true, "c.sql": true}

	got := newFiles(before, after)
	if len(got) != 1 || got[0] != "c.sql" {
		t.Errorf("newFiles() = %v, want [c.sql]", got)
	}
}

// -----------------------------------------------------------------------
// copyMigrationsDir
// -----------------------------------------------------------------------

func TestCopyMigrationsDir_CopiesRegularFilesOnly(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	dst := t.TempDir()

	writeFile(t, filepath.Join(src, "00001_extensions.sql"), "-- +goose Up\nCREATE EXTENSION citext;\n")
	writeFile(t, filepath.Join(src, "atlas.sum"), "h1:abc=\n")
	if err := os.Mkdir(filepath.Join(src, "subdir"), 0o750); err != nil {
		t.Fatalf("Mkdir(subdir): %v", err)
	}

	if err := copyMigrationsDir(src, dst); err != nil {
		t.Fatalf("copyMigrationsDir() error: %v", err)
	}

	for _, name := range []string{"00001_extensions.sql", "atlas.sum"} {
		want, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			t.Fatalf("read src/%s: %v", name, err)
		}
		got, err := os.ReadFile(filepath.Join(dst, name))
		if err != nil {
			t.Fatalf("copyMigrationsDir() did not copy %s: %v", name, err)
		}
		if string(got) != string(want) {
			t.Errorf("dst/%s = %q, want %q", name, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, "subdir")); !os.IsNotExist(err) {
		t.Errorf("copyMigrationsDir() copied a subdirectory; want regular files only")
	}
}

func TestCopyMigrationsDir_MissingSourceIsNotAnError(t *testing.T) {
	t.Parallel()

	dst := t.TempDir()
	if err := copyMigrationsDir(filepath.Join(t.TempDir(), "does-not-exist"), dst); err != nil {
		t.Fatalf("copyMigrationsDir() error = %v, want nil for a not-yet-created migrations directory", err)
	}
	entries, err := os.ReadDir(dst)
	if err != nil {
		t.Fatalf("ReadDir(dst): %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("dst has %d entries, want 0", len(entries))
	}
}

// TestCopyMigrationsDir_PreservesConventionalFileMode is the regression
// test for review finding M1: copyMigrationsDir previously hardcoded
// os.WriteFile's mode to 0600, silently rewriting a pre-existing
// migration file's real permissions (0644, verified against the actual
// committed apps/server/migrations/) down to a more restrictive one that
// only the newly Atlas-generated file in the same directory wouldn't
// share — mixed modes in one directory with no code reason for the
// difference.
func TestCopyMigrationsDir_PreservesConventionalFileMode(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	dst := t.TempDir()
	writeFile(t, filepath.Join(src, "00001_extensions.sql"), "-- +goose Up\nCREATE EXTENSION citext;\n")

	if err := copyMigrationsDir(src, dst); err != nil {
		t.Fatalf("copyMigrationsDir() error: %v", err)
	}

	info, err := os.Stat(filepath.Join(dst, "00001_extensions.sql"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	const wantMode = 0o644
	if got := info.Mode().Perm(); got != wantMode {
		t.Errorf("copied file mode = %o, want %o (the conventional file mode)", got, wantMode)
	}
}

// -----------------------------------------------------------------------
// publish
// -----------------------------------------------------------------------

func TestPublish_ReplacesDirContentsWithWorkDir(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	dir := filepath.Join(parent, "migrations")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatalf("Mkdir(dir): %v", err)
	}
	writeFile(t, filepath.Join(dir, "00001_extensions.sql"), "old content\n")

	workDir, err := os.MkdirTemp(parent, "migrations.gen-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	writeFile(t, filepath.Join(workDir, "00001_extensions.sql"), "old content\n")
	writeFile(t, filepath.Join(workDir, "00002_widgets.sql"), "new content\n")
	writeFile(t, filepath.Join(workDir, "atlas.sum"), "new sum\n")

	if err = publish(dir, workDir); err != nil {
		t.Fatalf("publish() error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "00002_widgets.sql"))
	if err != nil {
		t.Fatalf("publish() did not install the new file: %v", err)
	}
	if string(got) != "new content\n" {
		t.Errorf("00002_widgets.sql = %q, want %q", got, "new content\n")
	}
	if _, err := os.ReadFile(filepath.Join(dir, "atlas.sum")); err != nil {
		t.Fatalf("publish() did not install the new atlas.sum: %v", err)
	}

	// The temporary directory and its backup must both be gone: publish
	// leaves no trace beyond the published dir itself.
	if _, err := os.Stat(workDir); !os.IsNotExist(err) {
		t.Errorf("workDir %s still exists after publish()", workDir)
	}
	if _, err := os.Stat(workDir + ".bak"); !os.IsNotExist(err) {
		t.Errorf("backup directory %s still exists after publish()", workDir+".bak")
	}
}

// TestPublish_BootstrapsWhenDirDoesNotExist is the regression test for
// review finding I1: the very first `migrate-gen` run in a project has no
// migrations/ directory yet (every other function in this file already
// tolerates that — copyMigrationsDir's own doc comment, sqlFiles,
// nextSequence returning 1, run defaulting name to "init" — see their doc
// comments). publish must too: with no pre-existing dir to move aside,
// publishing is just workDir -> dir, not an error.
func TestPublish_BootstrapsWhenDirDoesNotExist(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	dir := filepath.Join(parent, "migrations") // deliberately never created

	workDir, err := os.MkdirTemp(parent, "migrations.gen-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	writeFile(t, filepath.Join(workDir, "00001_extensions.sql"), "first migration\n")
	writeFile(t, filepath.Join(workDir, "atlas.sum"), "sum\n")

	if err = publish(dir, workDir); err != nil {
		t.Fatalf("publish() error = %v, want nil when dir does not exist yet (the first-ever generation)", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "00001_extensions.sql"))
	if err != nil {
		t.Fatalf("publish() did not install the generated file: %v", err)
	}
	if string(got) != "first migration\n" {
		t.Errorf("00001_extensions.sql = %q, want %q", got, "first migration\n")
	}

	if _, err := os.Stat(workDir); !os.IsNotExist(err) {
		t.Errorf("workDir %s still exists after publish()", workDir)
	}
	if _, err := os.Stat(workDir + ".bak"); !os.IsNotExist(err) {
		t.Errorf("backup directory %s exists after publish(), want none created when there was nothing to back up", workDir+".bak")
	}
}

// TestPublish_FailedSecondRenameRestoresOriginal simulates the narrow
// failure window between publish's two renames by removing workDir out
// from under publish right before it's called: the first rename (dir ->
// backup) still succeeds, since it never touches workDir, but the second
// (workDir -> dir) then fails because its source no longer exists. publish
// must restore dir from the backup rather than leaving it missing, and
// must still return a non-nil error either way.
func TestPublish_FailedSecondRenameRestoresOriginal(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	dir := filepath.Join(parent, "migrations")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatalf("Mkdir(dir): %v", err)
	}
	writeFile(t, filepath.Join(dir, "00001_extensions.sql"), "old content\n")

	workDir, err := os.MkdirTemp(parent, "migrations.gen-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	if err = os.RemoveAll(workDir); err != nil {
		t.Fatalf("RemoveAll(workDir): %v", err)
	}

	err = publish(dir, workDir)
	if err == nil {
		t.Fatal("publish() error = nil, want an error when the second rename fails")
	}

	restored, readErr := os.ReadFile(filepath.Join(dir, "00001_extensions.sql"))
	if readErr != nil {
		t.Fatalf("publish() left dir missing after a failed second rename: %v", readErr)
	}
	if string(restored) != "old content\n" {
		t.Errorf("restored dir content = %q, want %q", restored, "old content\n")
	}
}

// -----------------------------------------------------------------------
// checkExtensionDeclarations
// -----------------------------------------------------------------------

func TestCheckExtensionDeclarations_MatchingDeclarationsPass(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	writeFile(t, filepath.Join(migrations, "00001_extensions.sql"),
		"-- +goose Up\nCREATE EXTENSION IF NOT EXISTS citext;\n\n-- +goose Down\nDROP EXTENSION IF EXISTS citext;\n")
	schema := filepath.Join(dir, "schema.sql")
	writeFile(t, schema, "CREATE EXTENSION IF NOT EXISTS citext;\n")

	if err := checkExtensionDeclarations(migrations, schema); err != nil {
		t.Errorf("checkExtensionDeclarations() error = %v, want nil for matching declarations", err)
	}
}

// TestCheckExtensionDeclarations_IgnoresMentionsInsideComments guards a
// real false positive found while validating this check against the
// actual committed sql/schema.sql: its own doc comment prose says "...
// CREATE EXTENSION with the first CREATE TABLE it depends on", which an
// earlier version of createExtensionPattern matched against uncommented
// text and misread as a genuine "CREATE EXTENSION with" declaration
// (extension name "with"). Only statements outside "--" line comments may
// ever count.
func TestCheckExtensionDeclarations_IgnoresMentionsInsideComments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	writeFile(t, filepath.Join(migrations, "00001_extensions.sql"),
		"-- +goose Up\nCREATE EXTENSION IF NOT EXISTS citext;\n\n-- +goose Down\nDROP EXTENSION IF EXISTS citext;\n")
	schema := filepath.Join(dir, "schema.sql")
	writeFile(t, schema,
		"-- citext is enabled now, not deferred, so Phase 1 only ever mixes a\n"+
			"-- CREATE EXTENSION with the first CREATE TABLE it depends on.\n"+
			"CREATE EXTENSION IF NOT EXISTS citext;\n")

	if err := checkExtensionDeclarations(migrations, schema); err != nil {
		t.Errorf("checkExtensionDeclarations() error = %v, want nil: the comment's prose must not be read as a declaration", err)
	}
}

func TestCheckExtensionDeclarations_SchemaHasExtensionMigrationLacks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	// No hand-written migration at all.
	schema := filepath.Join(dir, "schema.sql")
	writeFile(t, schema, "CREATE EXTENSION IF NOT EXISTS pgcrypto;\n")

	err := checkExtensionDeclarations(migrations, schema)
	if err == nil {
		t.Fatal("checkExtensionDeclarations() error = nil, want an error: schema declares pgcrypto with no hand-written migration")
	}
	if !strings.Contains(err.Error(), "pgcrypto") {
		t.Errorf("checkExtensionDeclarations() error = %q, want it to mention pgcrypto", err)
	}
}

func TestCheckExtensionDeclarations_MigrationHasExtensionSchemaLacks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	writeFile(t, filepath.Join(migrations, "00001_extensions.sql"),
		"-- +goose Up\nCREATE EXTENSION IF NOT EXISTS citext;\n\n-- +goose Down\nDROP EXTENSION IF EXISTS citext;\n")
	schema := filepath.Join(dir, "schema.sql")
	writeFile(t, schema, "-- no extensions declared\n")

	err := checkExtensionDeclarations(migrations, schema)
	if err == nil {
		t.Fatal("checkExtensionDeclarations() error = nil, want an error: migration creates citext with no schema declaration")
	}
	if !strings.Contains(err.Error(), "citext") {
		t.Errorf("checkExtensionDeclarations() error = %q, want it to mention citext", err)
	}
}

// TestRun_ExtensionMismatch_FailsFastWithoutAtlasOrDatabase exercises
// run()'s top-level ordering: the extension-declaration check has no
// external dependencies, so it must reject a mismatch before run() ever
// looks for the Atlas CLI or requires DATABASE_URL — this test sets
// neither, so a false failure ordering would surface as the wrong error
// message (or a hang/atlas-not-found) instead of the extension mismatch.
func TestRun_ExtensionMismatch_FailsFastWithoutAtlasOrDatabase(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	migrationsDir := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrationsDir, 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	schemaFile := filepath.Join(dir, "schema.sql")
	writeFile(t, schemaFile, "CREATE EXTENSION IF NOT EXISTS pgcrypto;\n")

	err := run("x", false, migrationsDir, schemaFile)
	if err == nil {
		t.Fatal("run() error = nil, want an extension-declaration mismatch error")
	}
	if !strings.Contains(err.Error(), "pgcrypto") {
		t.Errorf("run() error = %q, want it to mention pgcrypto", err)
	}
}

// -----------------------------------------------------------------------
// checkUndiffableObjects
// -----------------------------------------------------------------------
//
// Shared fixtures below (enforceMaxResumesFn, resumesMax3Trigger,
// resumesMax3Down) mirror the shape Phase 2A Task 3's real spec-mandated
// 3-resumes-per-user DB trigger will take: a CREATE FUNCTION whose
// dollar-quoted body contains several internal ";"-terminated statements,
// paired with a CREATE TRIGGER that calls it. Using one realistic fixture
// throughout keeps every cross-check test exercising the same
// dollar-quote/semicolon shape the D9 cross-check exists for.

const enforceMaxResumesFn = `CREATE FUNCTION enforce_max_resumes() RETURNS trigger AS $$
BEGIN
  IF (SELECT count(*) FROM resumes WHERE user_id = NEW.user_id) >= 3 THEN
    RAISE EXCEPTION 'too many resumes';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
`

const resumesMax3Trigger = `CREATE TRIGGER resumes_max_3 BEFORE INSERT ON resumes FOR EACH ROW EXECUTE FUNCTION enforce_max_resumes();
`

const resumesMax3Down = `DROP TRIGGER resumes_max_3 ON resumes;
DROP FUNCTION enforce_max_resumes();
`

// gooseMigrationFile assembles a goose-format migration file's content from
// its Up and Down sections, mirroring every real file under
// apps/server/migrations/.
func gooseMigrationFile(up, down string) string {
	return "-- +goose Up\n" + up + "\n-- +goose Down\n" + down
}

// -----------------------------------------------------------------------
// Step 1: the broadened keyword net (M-NEW + B4)
// -----------------------------------------------------------------------

// TestCheckUndiffableObjects_DetectsEveryUnsupportedVariant is the
// regression test for review finding I2 plus M-NEW/B4: Atlas community
// edition's differ silently drops triggers, functions, procedures, views,
// sequences, rules, and policies (in every CREATE/ALTER spelling) from
// generated migrations (verified empirically — see checkUndiffableObjects'
// doc comment), so a schema.sql declaring one must always be rejected
// loudly. A bare CREATE FUNCTION/CREATE [OR REPLACE] [CONSTRAINT] TRIGGER
// is the only shape ever eligible for the D9 cross-check (want contains
// "no matching hand-written migration" below, since no migration is ever
// seeded in this table); every other row must take the unconditional,
// no-cross-check path (want contains "no such escape hatch").
func TestCheckUndiffableObjects_DetectsEveryUnsupportedVariant(t *testing.T) {
	t.Parallel()

	const crossCheckPath = "no matching hand-written migration"
	const noCrossCheckPath = "no such escape hatch"

	tests := []struct {
		name   string
		schema string
		want   []string
	}{
		{
			name:   "trigger",
			schema: resumesMax3Trigger,
			want:   []string{"resumes_max_3", crossCheckPath},
		},
		{
			name:   "constraint trigger",
			schema: "CREATE CONSTRAINT TRIGGER resumes_max_3 AFTER INSERT ON resumes FOR EACH ROW EXECUTE FUNCTION enforce_max_resumes();\n",
			want:   []string{"resumes_max_3", crossCheckPath},
		},
		{
			name:   "function",
			schema: enforceMaxResumesFn,
			want:   []string{"enforce_max_resumes", crossCheckPath},
		},
		{
			name:   "function with OR REPLACE",
			schema: "CREATE OR REPLACE FUNCTION f1() RETURNS int AS $$ SELECT 1; $$ LANGUAGE sql;\n",
			want:   []string{"f1", crossCheckPath},
		},
		{
			name:   "view",
			schema: "CREATE VIEW v1 AS SELECT 1;\n",
			want:   []string{"v1", noCrossCheckPath},
		},
		{
			name:   "materialized view",
			schema: "CREATE MATERIALIZED VIEW v1 AS SELECT 1;\n",
			want:   []string{"v1", noCrossCheckPath},
		},
		{
			name:   "recursive view",
			schema: "CREATE RECURSIVE VIEW v1 (n) AS SELECT 1;\n",
			want:   []string{"v1", noCrossCheckPath},
		},
		{
			name:   "temp view",
			schema: "CREATE TEMP VIEW v1 AS SELECT 1;\n",
			want:   []string{"v1", noCrossCheckPath},
		},
		{
			name:   "temporary view",
			schema: "CREATE TEMPORARY VIEW v1 AS SELECT 1;\n",
			want:   []string{"v1", noCrossCheckPath},
		},
		{
			name:   "sequence",
			schema: "CREATE SEQUENCE probe_seq;\n",
			want:   []string{"probe_seq", noCrossCheckPath},
		},
		{
			name:   "unlogged sequence",
			schema: "CREATE UNLOGGED SEQUENCE probe_seq;\n",
			want:   []string{"probe_seq", noCrossCheckPath},
		},
		{
			name:   "procedure",
			schema: "CREATE PROCEDURE proc1() LANGUAGE plpgsql AS $$ BEGIN END; $$;\n",
			want:   []string{"proc1", noCrossCheckPath},
		},
		{
			name:   "rule",
			schema: "CREATE RULE r1 AS ON INSERT TO widgets DO INSTEAD NOTHING;\n",
			want:   []string{"r1", noCrossCheckPath},
		},
		{
			name:   "policy",
			schema: "CREATE POLICY p1 ON widgets USING (true);\n",
			want:   []string{"p1", noCrossCheckPath},
		},
		{
			// Regression for review finding Minor 4: a detector must never
			// make firing contingent on the name's shape. A digit-leading
			// quoted identifier ("1st_policy") previously made the whole
			// undiffableObjectPattern match fail (the optional leading '"'
			// consumed the quote, then [a-zA-Z_] couldn't match '1', and
			// backtracking to skip the quote left the bare '"' character
			// itself unmatched either way) — so the entire CREATE POLICY
			// statement was silently accepted.
			name:   "policy with digit-leading quoted name",
			schema: `CREATE POLICY "1st_policy" ON widgets USING (true);` + "\n",
			want:   []string{"1st_policy", noCrossCheckPath},
		},
		{
			name:   "alter function",
			schema: "ALTER FUNCTION enforce_max_resumes() COST 100;\n",
			want:   []string{"enforce_max_resumes", noCrossCheckPath},
		},
		{
			name:   "alter trigger",
			schema: "ALTER TRIGGER resumes_max_3 ON resumes RENAME TO resumes_max_4;\n",
			want:   []string{"resumes_max_3", noCrossCheckPath},
		},
		{
			name:   "alter table enable trigger",
			schema: "ALTER TABLE resumes ENABLE TRIGGER resumes_max_3;\n",
			want:   []string{"resumes_max_3", noCrossCheckPath},
		},
		{
			name:   "alter table disable trigger",
			schema: "ALTER TABLE resumes DISABLE TRIGGER resumes_max_3;\n",
			want:   []string{"resumes_max_3", noCrossCheckPath},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			schema := filepath.Join(dir, "schema.sql")
			writeFile(t, schema, tt.schema)
			migrationsDir := filepath.Join(dir, "migrations") // deliberately never created

			err := checkUndiffableObjects(migrationsDir, schema)
			if err == nil {
				t.Fatalf("checkUndiffableObjects() error = nil, want an error for a declared %s", tt.name)
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("checkUndiffableObjects() error = %q, want it to contain %q", err, want)
				}
			}
		})
	}
}

func TestCheckUndiffableObjects_AllowsTablesExtensionsAndIndexes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	schema := filepath.Join(dir, "schema.sql")
	writeFile(t, schema,
		"CREATE EXTENSION IF NOT EXISTS citext;\n\n"+
			"CREATE TABLE widgets (id int PRIMARY KEY, name citext);\n"+
			"CREATE INDEX idx_widgets_name ON widgets (name);\n")
	migrationsDir := filepath.Join(dir, "migrations")

	if err := checkUndiffableObjects(migrationsDir, schema); err != nil {
		t.Errorf("checkUndiffableObjects() error = %v, want nil for tables/extensions/indexes", err)
	}
}

// TestCheckUndiffableObjects_IgnoresMentionsInsideComments mirrors
// TestCheckExtensionDeclarations_IgnoresMentionsInsideComments: this exact
// class of false positive (prose in a "--" comment misread as a
// declaration) was real for the extension check, so the broadened keyword
// net must strip "--" comments the same way.
func TestCheckUndiffableObjects_IgnoresMentionsInsideComments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	schema := filepath.Join(dir, "schema.sql")
	writeFile(t, schema,
		"-- Phase 2A will add a CREATE TRIGGER for the 3-resume-per-user limit;\n"+
			"-- see docs/plans/implementation-plan.md.\n"+
			"CREATE TABLE widgets (id int PRIMARY KEY);\n")
	migrationsDir := filepath.Join(dir, "migrations")

	if err := checkUndiffableObjects(migrationsDir, schema); err != nil {
		t.Errorf("checkUndiffableObjects() error = %v, want nil: the comment's prose must not be read as a declaration", err)
	}
}

// TestCheckUndiffableObjects_IgnoresMentionsInsideBlockComments is the
// /* ... */ counterpart of TestCheckUndiffableObjects_IgnoresMentionsInsideComments:
// B3 normalization stage 1 strips block comments as well as "--" line
// comments (a new capability this task adds — the previous keyword net
// only ever stripped "--" comments), so prose inside a /* ... */ span must
// never be misread as a declaration either.
func TestCheckUndiffableObjects_IgnoresMentionsInsideBlockComments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	schema := filepath.Join(dir, "schema.sql")
	writeFile(t, schema,
		"/* Phase 2A will add a CREATE TRIGGER for the 3-resume-per-user\n"+
			"   limit; see docs/plans/implementation-plan.md. */\n"+
			"CREATE TABLE widgets (id int PRIMARY KEY);\n")
	migrationsDir := filepath.Join(dir, "migrations")

	if err := checkUndiffableObjects(migrationsDir, schema); err != nil {
		t.Errorf("checkUndiffableObjects() error = %v, want nil: the block comment's prose must not be read as a declaration", err)
	}
}

// TestRun_UndiffableObject_FailsFastWithoutAtlasOrDatabase mirrors
// TestRun_ExtensionMismatch_FailsFastWithoutAtlasOrDatabase:
// checkUndiffableObjects has no external dependencies (it never requires
// Atlas or a live database — the D9 cross-check reads only the local
// filesystem), so run() must reject a declared, unsupported object before
// ever looking for the Atlas CLI or requiring DATABASE_URL.
func TestRun_UndiffableObject_FailsFastWithoutAtlasOrDatabase(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	migrationsDir := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrationsDir, 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	schemaFile := filepath.Join(dir, "schema.sql")
	writeFile(t, schemaFile, "CREATE SEQUENCE probe_seq;\n")

	err := run("x", false, migrationsDir, schemaFile)
	if err == nil {
		t.Fatal("run() error = nil, want an undiffable-object error")
	}
	if !strings.Contains(err.Error(), "probe_seq") {
		t.Errorf("run() error = %q, want it to mention probe_seq", err)
	}
	if !strings.Contains(err.Error(), "sequence") {
		t.Errorf("run() error = %q, want it to mention the object class (sequence)", err)
	}
}

// -----------------------------------------------------------------------
// Statement extraction: the dollar-quoted-body semicolon case
// -----------------------------------------------------------------------

// TestSplitStatements_DollarQuotedBodySemicolonSurvivesIntact pins the
// statement-extraction requirement the task brief calls out explicitly: a
// naive split-on-";" truncates a function body at its FIRST internal
// semicolon (there are four here: after RAISE EXCEPTION, END IF, RETURN
// NEW, and END) instead of the real terminator after "LANGUAGE plpgsql".
func TestSplitStatements_DollarQuotedBodySemicolonSurvivesIntact(t *testing.T) {
	t.Parallel()

	sql := enforceMaxResumesFn + "\n" + resumesMax3Trigger
	got := splitStatements(sql)
	if len(got) != 2 {
		t.Fatalf("splitStatements() returned %d statement(s), want 2 (a naive split-on-';' would produce more, truncating inside the dollar-quoted body): %q", len(got), got)
	}
	if !strings.Contains(got[0], "LANGUAGE plpgsql") {
		t.Errorf("statement 0 = %q, want the full function body through LANGUAGE plpgsql", got[0])
	}
	if !strings.HasSuffix(strings.TrimSpace(got[0]), ";") {
		t.Errorf("statement 0 = %q, want it to end with the terminator after the dollar-quoted body", got[0])
	}
	if !strings.Contains(got[1], "resumes_max_3") {
		t.Errorf("statement 1 = %q, want the trigger statement", got[1])
	}
}

// TestStripSQLComments_PreservesDashDashInsideSingleQuotedLiteral is the
// regression test for review finding Minor 5: stripSQLComments previously
// ran its comment regexes over raw text as a blind pre-pass, before any
// quote-awareness, so a "--" that is really just data inside a
// single-quoted string literal (not a comment at all) got misread as one
// and stripped — corrupting the literal and leaving an unbalanced quote
// behind for every later stage to trip over.
func TestStripSQLComments_PreservesDashDashInsideSingleQuotedLiteral(t *testing.T) {
	t.Parallel()

	sql := "CREATE TABLE widgets (id int PRIMARY KEY, sep text DEFAULT '--');\n" +
		"CREATE VIEW v1 AS SELECT 1;\n"

	got := stripSQLComments(sql)
	if !strings.Contains(got, "DEFAULT '--'") {
		t.Errorf("stripSQLComments() = %q, want the literal '--' preserved intact (a quote-unaware pre-pass corrupts it)", got)
	}
	if !strings.Contains(got, "CREATE VIEW v1 AS SELECT 1;") {
		t.Errorf("stripSQLComments() = %q, want the later statement intact, not swallowed by a corrupted trailing quote", got)
	}
}

// TestSplitStatements_QuotedLiteralContainingDashDashKeepsStatementsSeparate
// exercises the exact pipeline checkUndiffableObjects and
// crossCheckFunctionsAndTriggers use (stripSQLComments then
// splitStatements): under the Minor 5 bug, the corrupted unbalanced quote
// left by a non-quote-aware stripSQLComments made scanSQLSegments treat
// the rest of the file as one dangling verbatim span, so splitStatements
// merged the CREATE TABLE and the later CREATE VIEW into a single
// "statement" — and checkUndiffableObjects's per-statement single-match
// scan then reports at most the FIRST undiffable object in that merged
// blob, silently missing a second one hidden in the same tail (e.g. a
// later VIEW or SEQUENCE, per the review finding).
func TestSplitStatements_QuotedLiteralContainingDashDashKeepsStatementsSeparate(t *testing.T) {
	t.Parallel()

	sql := "CREATE TABLE widgets (id int PRIMARY KEY, sep text DEFAULT '--');\n" +
		"CREATE VIEW v1 AS SELECT 1;\n"

	got := splitStatements(stripSQLComments(sql))
	if len(got) != 2 {
		t.Fatalf("splitStatements(stripSQLComments(sql)) returned %d statement(s), want 2 (a '--' inside a single-quoted literal must never corrupt statement boundaries): %q", len(got), got)
	}
	if !strings.Contains(got[0], "DEFAULT '--'") {
		t.Errorf("statement 0 = %q, want the literal '--' preserved intact", got[0])
	}
	if !strings.Contains(got[1], "CREATE VIEW v1") {
		t.Errorf("statement 1 = %q, want the separate CREATE VIEW statement", got[1])
	}
}

// -----------------------------------------------------------------------
// Step 2: the D9 FUNCTION/TRIGGER cross-check
// -----------------------------------------------------------------------

func TestCrossCheck_NoMigrationFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	schema := filepath.Join(dir, "schema.sql")
	writeFile(t, schema, enforceMaxResumesFn+"\n"+resumesMax3Trigger)
	migrations := filepath.Join(dir, "migrations") // deliberately never created

	err := checkUndiffableObjects(migrations, schema)
	if err == nil {
		t.Fatal("checkUndiffableObjects() error = nil, want an error: schema declares a function+trigger with no migration at all")
	}
	if !strings.Contains(err.Error(), "no matching hand-written migration") {
		t.Errorf("checkUndiffableObjects() error = %q, want it to mention the missing migration", err)
	}
}

// TestCrossCheck_MatchingMigrationPasses also covers the B2 requirement
// that a "-- +goose Down" section's DROPs (here: dropping the trigger,
// then the function — resumesMax3Down's exact shape) are never scanned:
// if they were, this would fail the "no DROP FUNCTION/TRIGGER in a
// migration" rule on every single migration that ever rolls one back.
func TestCrossCheck_MatchingMigrationPasses(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	schema := filepath.Join(dir, "schema.sql")
	writeFile(t, schema, enforceMaxResumesFn+"\n"+resumesMax3Trigger)
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	writeFile(t, filepath.Join(migrations, "00005_resumes_max_3.sql"),
		gooseMigrationFile(enforceMaxResumesFn+"\n"+resumesMax3Trigger, resumesMax3Down))

	if err := checkUndiffableObjects(migrations, schema); err != nil {
		t.Errorf("checkUndiffableObjects() error = %v, want nil: schema and migration declare the identical function+trigger", err)
	}
}

func TestCrossCheck_SchemaBodyEditedFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	schema := filepath.Join(dir, "schema.sql")
	edited := strings.Replace(enforceMaxResumesFn, ">= 3", ">= 5", 1) // one-token body edit
	writeFile(t, schema, edited+"\n"+resumesMax3Trigger)
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	writeFile(t, filepath.Join(migrations, "00005_resumes_max_3.sql"),
		gooseMigrationFile(enforceMaxResumesFn+"\n"+resumesMax3Trigger, resumesMax3Down))

	err := checkUndiffableObjects(migrations, schema)
	if err == nil {
		t.Fatal("checkUndiffableObjects() error = nil, want an error: schema.sql's function body was edited but the migration was not — a name-set comparison would miss this")
	}
	if !strings.Contains(err.Error(), "enforce_max_resumes") {
		t.Errorf("checkUndiffableObjects() error = %q, want it to mention enforce_max_resumes", err)
	}
}

func TestCrossCheck_LastOccurrenceAcrossMigrationsWins(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	// First migration creates the function+trigger with the original body.
	writeFile(t, filepath.Join(migrations, "00005_resumes_max_3.sql"),
		gooseMigrationFile(enforceMaxResumesFn+"\n"+resumesMax3Trigger, resumesMax3Down))
	// A later migration re-declares the function with a new body.
	newBody := strings.Replace(enforceMaxResumesFn, "CREATE FUNCTION", "CREATE OR REPLACE FUNCTION", 1)
	newBody = strings.Replace(newBody, ">= 3", ">= 10", 1)
	writeFile(t, filepath.Join(migrations, "00006_raise_resume_cap.sql"),
		gooseMigrationFile(newBody, "-- no-op down\n"))

	schema := filepath.Join(dir, "schema.sql")
	schemaBody := strings.Replace(enforceMaxResumesFn, ">= 3", ">= 10", 1)
	writeFile(t, schema, schemaBody+"\n"+resumesMax3Trigger)

	if err := checkUndiffableObjects(migrations, schema); err != nil {
		t.Errorf("checkUndiffableObjects() error = %v, want nil: schema matches the LAST migration's redeclaration, not the first", err)
	}
}

func TestCrossCheck_MigrationDeclaresUnknownFunctionFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	writeFile(t, filepath.Join(migrations, "00005_resumes_max_3.sql"),
		gooseMigrationFile(enforceMaxResumesFn+"\n"+resumesMax3Trigger, resumesMax3Down))

	schema := filepath.Join(dir, "schema.sql")
	writeFile(t, schema, "-- schema.sql never declares enforce_max_resumes or resumes_max_3\nCREATE TABLE resumes (id bigint PRIMARY KEY);\n")

	err := checkUndiffableObjects(migrations, schema)
	if err == nil {
		t.Fatal("checkUndiffableObjects() error = nil, want an error: migration declares a function+trigger schema.sql never mentions")
	}
	if !strings.Contains(err.Error(), "enforce_max_resumes") && !strings.Contains(err.Error(), "resumes_max_3") {
		t.Errorf("checkUndiffableObjects() error = %q, want it to mention the undeclared function or trigger", err)
	}
}

func TestCrossCheck_DropInUpSectionFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	// The Up section (mistakenly) drops the function it just created in
	// the same section, instead of only the Down section legitimately
	// doing so.
	up := enforceMaxResumesFn + "\n" + resumesMax3Trigger + "\nDROP FUNCTION enforce_max_resumes();\n"
	writeFile(t, filepath.Join(migrations, "00005_resumes_max_3.sql"), gooseMigrationFile(up, "-- no-op down\n"))

	schema := filepath.Join(dir, "schema.sql")
	writeFile(t, schema, enforceMaxResumesFn+"\n"+resumesMax3Trigger)

	err := checkUndiffableObjects(migrations, schema)
	if err == nil {
		t.Fatal("checkUndiffableObjects() error = nil, want an error: the migration's Up section drops the function it declares")
	}
	if !strings.Contains(err.Error(), "enforce_max_resumes") {
		t.Errorf("checkUndiffableObjects() error = %q, want it to mention enforce_max_resumes", err)
	}
}

// TestCrossCheck_MigrationUpSection_AlterVariantsRejected is the
// regression test for review finding Important 3: the B4 addendum's
// unconditional ALTER rejection previously ran only against schema.sql —
// matchAlterUndiffableObject was never called against a migration's own
// "-- +goose Up" section, so a hand-written migration whose Up section
// contains ALTER TABLE ... DISABLE TRIGGER (or ALTER FUNCTION/ALTER
// TRIGGER) passed the gate cleanly while the trigger it silently disables
// sits inert in the database — the exact invisibility to Atlas's differ
// the package doc comment already describes, just reached from the
// migration side instead of schema.sql.
func TestCrossCheck_MigrationUpSection_AlterVariantsRejected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		alter string
	}{
		{name: "alter function", alter: "ALTER FUNCTION enforce_max_resumes() COST 100;\n"},
		{name: "alter trigger", alter: "ALTER TRIGGER resumes_max_3 ON resumes RENAME TO resumes_max_4;\n"},
		{name: "alter table enable trigger", alter: "ALTER TABLE resumes ENABLE TRIGGER resumes_max_3;\n"},
		{name: "alter table disable trigger", alter: "ALTER TABLE resumes DISABLE TRIGGER resumes_max_3;\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			migrations := filepath.Join(dir, "migrations")
			if err := os.Mkdir(migrations, 0o750); err != nil {
				t.Fatalf("Mkdir: %v", err)
			}
			// The Up section creates the matching function+trigger (so a
			// name/body-only check would see nothing wrong) AND ALSO
			// contains the ALTER — which must be rejected regardless.
			up := enforceMaxResumesFn + "\n" + resumesMax3Trigger + "\n" + tt.alter
			writeFile(t, filepath.Join(migrations, "00005_resumes_max_3.sql"), gooseMigrationFile(up, "-- no-op down\n"))

			schema := filepath.Join(dir, "schema.sql")
			writeFile(t, schema, enforceMaxResumesFn+"\n"+resumesMax3Trigger)

			err := checkUndiffableObjects(migrations, schema)
			if err == nil {
				t.Fatalf("checkUndiffableObjects() error = nil, want an error: a migration's Up section must never contain %s, even alongside a matching CREATE", tt.name)
			}
			if !strings.Contains(err.Error(), "resumes") {
				t.Errorf("checkUndiffableObjects() error = %q, want it to mention the affected object", err)
			}
		})
	}
}

// -----------------------------------------------------------------------
// B2: only "-- +goose Up" is ever scanned
// -----------------------------------------------------------------------

// TestCrossCheck_DownSectionContentNeverExtracted is the second B2
// direction: a migration whose Up section is entirely unrelated to
// functions/triggers, but whose Down section happens to contain an
// UNCOMMENTED CREATE FUNCTION with no schema.sql counterpart, must never
// have that text picked up as a real declaration. Regression for review
// finding Important 2: an earlier version of this test put its probe
// inside "--" comments, which stripSQLComments erases regardless of
// whether Up/Down-scoping (gooseUpSection) is even applied — so the test
// passed vacuously even with Up-scoping fully removed, and was not
// actually testing B2. An uncommented probe fails loudly as an orphan
// (see TestCrossCheck_MigrationDeclaresUnknownFunctionFails) if Down is
// ever scanned by mistake.
func TestCrossCheck_DownSectionContentNeverExtracted(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	up := "CREATE TABLE widgets (id bigint PRIMARY KEY);\n"
	down := "DROP TABLE widgets;\n" +
		"CREATE FUNCTION ghost() RETURNS trigger AS $$ BEGIN RETURN NEW; END; $$ LANGUAGE plpgsql;\n"
	writeFile(t, filepath.Join(migrations, "00002_widgets.sql"), gooseMigrationFile(up, down))

	schema := filepath.Join(dir, "schema.sql")
	writeFile(t, schema, "CREATE TABLE widgets (id bigint PRIMARY KEY);\n")

	if err := checkUndiffableObjects(migrations, schema); err != nil {
		t.Errorf("checkUndiffableObjects() error = %v, want nil: the Down section's CREATE FUNCTION must never be extracted as a real declaration (if it were, \"ghost\" would fail as undeclared in schema.sql)", err)
	}
}

// -----------------------------------------------------------------------
// B3: the normalization pipeline, one negative test per stage
// -----------------------------------------------------------------------

// TestCrossCheck_CommentInjectedMidStatementStripped is stage 1 (comment
// stripping): a documentation comment injected between tokens in the
// migration's copy must be stripped before comparison.
func TestCrossCheck_CommentInjectedMidStatementStripped(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	commented := strings.Replace(enforceMaxResumesFn,
		"RETURNS trigger AS $$",
		"RETURNS trigger -- enforces the per-user resume cap\n  AS $$", 1)
	writeFile(t, filepath.Join(migrations, "00005_resumes_max_3.sql"),
		gooseMigrationFile(commented+"\n"+resumesMax3Trigger, resumesMax3Down))

	schema := filepath.Join(dir, "schema.sql")
	writeFile(t, schema, enforceMaxResumesFn+"\n"+resumesMax3Trigger)

	if err := checkUndiffableObjects(migrations, schema); err != nil {
		t.Errorf("checkUndiffableObjects() error = %v, want nil: a comment injected mid-statement must be stripped before comparison", err)
	}
}

// reformattedFnHeader reformats only the portion of enforceMaxResumesFn
// BEFORE its dollar-quoted body opens (extra spaces/newlines around
// "CREATE FUNCTION ... AS $$") — deliberately never touching a single byte
// inside the "$$ ... $$" span itself, so tests built on it can isolate
// "reformatting is forgiven outside any quoted span" from "content inside
// a quoted span always compares verbatim".
func reformattedFnHeader() string {
	return strings.Replace(enforceMaxResumesFn,
		"CREATE FUNCTION enforce_max_resumes() RETURNS trigger AS $$",
		"CREATE   FUNCTION\n  enforce_max_resumes()\n  RETURNS trigger\n  AS $$", 1)
}

// TestCrossCheck_WhitespaceReformattedOutsideQuotesStillMatches is stage 2's
// positive case: extra blank lines/indentation outside any quoted span
// (here: the function header, plus blank lines between the two
// statements) must never register as a body change.
func TestCrossCheck_WhitespaceReformattedOutsideQuotesStillMatches(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	writeFile(t, filepath.Join(migrations, "00005_resumes_max_3.sql"),
		gooseMigrationFile(reformattedFnHeader()+"\n\n\n"+resumesMax3Trigger, resumesMax3Down))

	schema := filepath.Join(dir, "schema.sql")
	writeFile(t, schema, enforceMaxResumesFn+"\n"+resumesMax3Trigger)

	if err := checkUndiffableObjects(migrations, schema); err != nil {
		t.Errorf("checkUndiffableObjects() error = %v, want nil: whitespace reformatting outside any quoted span must not register as a body change", err)
	}
}

// TestCrossCheck_BodyTokenChangeInsideDollarQuoteStillFails is stage 2's
// negative case: the schema's copy reformats whitespace OUTSIDE the
// dollar-quoted span (the same forgivable reformatting as the positive
// test above) AND changes one real token INSIDE the dollar-quoted body —
// the outside reformatting must never mask the inside diff.
func TestCrossCheck_BodyTokenChangeInsideDollarQuoteStillFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	writeFile(t, filepath.Join(migrations, "00005_resumes_max_3.sql"),
		gooseMigrationFile(enforceMaxResumesFn+"\n"+resumesMax3Trigger, resumesMax3Down))

	edited := strings.Replace(reformattedFnHeader(), ">= 3", ">= 5", 1)
	schema := filepath.Join(dir, "schema.sql")
	writeFile(t, schema, edited+"\n\n\n"+resumesMax3Trigger)

	err := checkUndiffableObjects(migrations, schema)
	if err == nil {
		t.Fatal("checkUndiffableObjects() error = nil, want an error: a real body-token change inside the dollar-quoted span must still be caught even though whitespace outside the span also differs")
	}
}

// TestCrossCheck_CaseDifferenceInBodyFails is stage 3 (case-sensitive
// compare, no folding): Postgres never case-folds literal/dollar-quoted
// body text, so a case-only difference there must still be treated as a
// real diff.
func TestCrossCheck_CaseDifferenceInBodyFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	writeFile(t, filepath.Join(migrations, "00005_resumes_max_3.sql"),
		gooseMigrationFile(enforceMaxResumesFn+"\n"+resumesMax3Trigger, resumesMax3Down))

	recased := strings.Replace(enforceMaxResumesFn, "too many resumes", "TOO MANY RESUMES", 1)
	schema := filepath.Join(dir, "schema.sql")
	writeFile(t, schema, recased+"\n"+resumesMax3Trigger)

	err := checkUndiffableObjects(migrations, schema)
	if err == nil {
		t.Fatal("checkUndiffableObjects() error = nil, want an error: a case-only difference inside the dollar-quoted body must still be caught (no case-insensitive fallback)")
	}
}

// TestCrossCheck_OrReplaceElisionPasses is stage 4: CREATE FUNCTION vs
// CREATE OR REPLACE FUNCTION for the identical body must match.
func TestCrossCheck_OrReplaceElisionPasses(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	orReplaced := strings.Replace(enforceMaxResumesFn, "CREATE FUNCTION", "CREATE OR REPLACE FUNCTION", 1)
	writeFile(t, filepath.Join(migrations, "00005_resumes_max_3.sql"),
		gooseMigrationFile(orReplaced+"\n"+resumesMax3Trigger, resumesMax3Down))

	schema := filepath.Join(dir, "schema.sql")
	writeFile(t, schema, enforceMaxResumesFn+"\n"+resumesMax3Trigger)

	if err := checkUndiffableObjects(migrations, schema); err != nil {
		t.Errorf("checkUndiffableObjects() error = %v, want nil: CREATE FUNCTION vs CREATE OR REPLACE FUNCTION for the identical body must match (OR REPLACE elision)", err)
	}
}

// TestCrossCheck_NameQualificationOrQuotingDoesNotFalsePositive is stage 5:
// a schema-qualified or double-quoted name in one location vs the bare
// name in the other, for the same object, must match — the name must be
// captured anchored at the correct token, not string-matched against the
// first identifier-shaped substring in the statement.
func TestCrossCheck_NameQualificationOrQuotingDoesNotFalsePositive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		schemaStmt string
	}{
		{
			name:       "schema-qualified name",
			schemaStmt: strings.Replace(enforceMaxResumesFn, "CREATE FUNCTION enforce_max_resumes", "CREATE FUNCTION public.enforce_max_resumes", 1),
		},
		{
			name:       "double-quoted name",
			schemaStmt: strings.Replace(enforceMaxResumesFn, "CREATE FUNCTION enforce_max_resumes", `CREATE FUNCTION "enforce_max_resumes"`, 1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			migrations := filepath.Join(dir, "migrations")
			if err := os.Mkdir(migrations, 0o750); err != nil {
				t.Fatalf("Mkdir: %v", err)
			}
			writeFile(t, filepath.Join(migrations, "00005_resumes_max_3.sql"),
				gooseMigrationFile(enforceMaxResumesFn+"\n"+resumesMax3Trigger, resumesMax3Down))

			schema := filepath.Join(dir, "schema.sql")
			writeFile(t, schema, tt.schemaStmt+"\n"+resumesMax3Trigger)

			if err := checkUndiffableObjects(migrations, schema); err != nil {
				t.Errorf("checkUndiffableObjects() error = %v, want nil: %s vs the bare name for the same object must match", err, tt.name)
			}
		})
	}
}

// -----------------------------------------------------------------------
// Composite identity: a bare name is not a TRIGGER's or FUNCTION's real
// identity in Postgres (regression for review finding Important 1)
// -----------------------------------------------------------------------
//
// setUpdatedAtFn/setUpdatedAtTrigger model a trigger function shared
// across two tables — a plausible Phase 2A/3 shape (e.g. a generic
// "set_updated_at" trigger reused on both resumes and resume_versions). A
// TRIGGER's real identity is its name PLUS its target table (Postgres
// itself allows two different tables to each have their own,
// independent, same-named trigger); a bare-name identity key collapses
// these into one, either hiding a genuinely orphaned migration statement
// behind the "last occurrence" of the same name (fail-open) or rejecting
// two legitimately distinct, correctly-declared objects as if they were
// one mismatched one (fail-shut, but wrong).

const setUpdatedAtFn = `CREATE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
`

func setUpdatedAtTrigger(table string) string {
	return "CREATE TRIGGER set_updated_at BEFORE UPDATE ON " + table +
		" FOR EACH ROW EXECUTE FUNCTION set_updated_at();\n"
}

// TestCrossCheck_SameTriggerNameOnDifferentTables_MigrationExtraTableIsOrphan
// is the fail-open case from the review finding: the migration declares
// set_updated_at on BOTH resumes and resume_versions; schema.sql declares
// it only on resume_versions. A bare-name key's "last occurrence wins"
// rule would let the migration's resumes-table trigger hide behind the
// resume_versions declaration and report clean — exactly the orphan the
// gate exists to catch.
func TestCrossCheck_SameTriggerNameOnDifferentTables_MigrationExtraTableIsOrphan(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	up := setUpdatedAtFn + "\n" + setUpdatedAtTrigger("resumes") + "\n" + setUpdatedAtTrigger("resume_versions")
	writeFile(t, filepath.Join(migrations, "00005_set_updated_at.sql"), gooseMigrationFile(up, "-- no-op down\n"))

	schema := filepath.Join(dir, "schema.sql")
	writeFile(t, schema, setUpdatedAtFn+"\n"+setUpdatedAtTrigger("resume_versions"))

	err := checkUndiffableObjects(migrations, schema)
	if err == nil {
		t.Fatal("checkUndiffableObjects() error = nil, want an error: the migration's set_updated_at trigger ON resumes has no matching schema.sql declaration — a same-named trigger on a different table must not collapse identity with it")
	}
	if !strings.Contains(err.Error(), "resumes") {
		t.Errorf("checkUndiffableObjects() error = %q, want it to mention the orphaned resumes-table trigger", err)
	}
}

// TestCrossCheck_SameTriggerNameOnDifferentTables_BothDeclaredMatch is the
// mirror, fail-shut-but-wrong case: both tables' triggers are correctly
// declared on both sides. A bare-name key would compare schema.sql's
// first-processed declaration against whichever migration statement
// happens to be "last" for that shared name, rejecting a legitimate
// schema as if the (different-table) bodies had drifted.
func TestCrossCheck_SameTriggerNameOnDifferentTables_BothDeclaredMatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	up := setUpdatedAtFn + "\n" + setUpdatedAtTrigger("resumes") + "\n" + setUpdatedAtTrigger("resume_versions")
	writeFile(t, filepath.Join(migrations, "00005_set_updated_at.sql"), gooseMigrationFile(up, "-- no-op down\n"))

	schema := filepath.Join(dir, "schema.sql")
	writeFile(t, schema, setUpdatedAtFn+"\n"+setUpdatedAtTrigger("resumes")+"\n"+setUpdatedAtTrigger("resume_versions"))

	if err := checkUndiffableObjects(migrations, schema); err != nil {
		t.Errorf("checkUndiffableObjects() error = %v, want nil: a shared trigger name across two distinct tables, both correctly declared and matching, must not be treated as a mismatch", err)
	}
}

// TestCrossCheck_OverloadedFunctionName_DifferentArgListsAreDistinctIdentities
// is the FUNCTION-side analog: two overloads of to_label sharing a name
// but differing argument lists are distinct objects in Postgres. schema.sql
// declares only the text overload, so the int overload the migration
// hand-writes is an orphan a bare-name key would hide.
func TestCrossCheck_OverloadedFunctionName_DifferentArgListsAreDistinctIdentities(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	fnInt := "CREATE FUNCTION to_label(n int) RETURNS text AS $$ SELECT n::text; $$ LANGUAGE sql;\n"
	fnText := "CREATE FUNCTION to_label(n text) RETURNS text AS $$ SELECT n; $$ LANGUAGE sql;\n"
	writeFile(t, filepath.Join(migrations, "00005_to_label.sql"),
		gooseMigrationFile(fnInt+"\n"+fnText, "-- no-op down\n"))

	schema := filepath.Join(dir, "schema.sql")
	writeFile(t, schema, fnText)

	err := checkUndiffableObjects(migrations, schema)
	if err == nil {
		t.Fatal("checkUndiffableObjects() error = nil, want an error: the int-argument overload of to_label has no matching schema.sql declaration — overloaded functions must not collapse identity")
	}
	if !strings.Contains(err.Error(), "to_label") {
		t.Errorf("checkUndiffableObjects() error = %q, want it to mention to_label", err)
	}
}

// TestCrossCheck_OverloadedFunctionName_BothDeclaredMatch mirrors the
// trigger "both declared" case for functions: both overloads correctly
// declared on both sides must pass.
func TestCrossCheck_OverloadedFunctionName_BothDeclaredMatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	migrations := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrations, 0o750); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	fnInt := "CREATE FUNCTION to_label(n int) RETURNS text AS $$ SELECT n::text; $$ LANGUAGE sql;\n"
	fnText := "CREATE FUNCTION to_label(n text) RETURNS text AS $$ SELECT n; $$ LANGUAGE sql;\n"
	writeFile(t, filepath.Join(migrations, "00005_to_label.sql"),
		gooseMigrationFile(fnInt+"\n"+fnText, "-- no-op down\n"))

	schema := filepath.Join(dir, "schema.sql")
	writeFile(t, schema, fnInt+"\n"+fnText)

	if err := checkUndiffableObjects(migrations, schema); err != nil {
		t.Errorf("checkUndiffableObjects() error = %v, want nil: two overloads of the same function name, both correctly declared and matching, must not be treated as a mismatch", err)
	}
}

// writeFile is a small t.Fatal-on-error wrapper around os.WriteFile, used
// throughout this file's fixtures to keep test bodies focused on the
// scenario rather than error plumbing.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}
