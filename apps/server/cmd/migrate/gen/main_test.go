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
// checkNoUndiffableObjects
// -----------------------------------------------------------------------

// TestCheckNoUndiffableObjects_RejectsEachUnsupportedClass is the
// regression test for review finding I2: Atlas community edition's
// differ silently drops triggers, functions, views, and sequences from
// generated migrations (verified empirically — see checkNoUndiffableObjects'
// doc comment), so a schema.sql declaring one must be rejected loudly
// rather than passed through as "no schema changes" / a clean drift gate.
func TestCheckNoUndiffableObjects_RejectsEachUnsupportedClass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		schema string
		want   string // substring expected in the error: the object's name
	}{
		{
			name:   "trigger",
			schema: "CREATE TRIGGER resumes_max_3 BEFORE INSERT ON resumes FOR EACH ROW EXECUTE FUNCTION enforce_max_resumes();\n",
			want:   "resumes_max_3",
		},
		{
			name:   "function",
			schema: "CREATE FUNCTION enforce_max_resumes() RETURNS trigger AS $$ BEGIN RETURN NEW; END; $$ LANGUAGE plpgsql;\n",
			want:   "enforce_max_resumes",
		},
		{
			name:   "function with OR REPLACE",
			schema: "CREATE OR REPLACE FUNCTION f1() RETURNS int AS $$ SELECT 1; $$ LANGUAGE sql;\n",
			want:   "f1",
		},
		{
			name:   "view",
			schema: "CREATE VIEW v1 AS SELECT 1;\n",
			want:   "v1",
		},
		{
			name:   "sequence",
			schema: "CREATE SEQUENCE probe_seq;\n",
			want:   "probe_seq",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			schema := filepath.Join(dir, "schema.sql")
			writeFile(t, schema, tt.schema)

			err := checkNoUndiffableObjects(schema)
			if err == nil {
				t.Fatalf("checkNoUndiffableObjects() error = nil, want an error for a declared %s", tt.name)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("checkNoUndiffableObjects() error = %q, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestCheckNoUndiffableObjects_AllowsTablesExtensionsAndIndexes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	schema := filepath.Join(dir, "schema.sql")
	writeFile(t, schema,
		"CREATE EXTENSION IF NOT EXISTS citext;\n\n"+
			"CREATE TABLE widgets (id int PRIMARY KEY, name citext);\n"+
			"CREATE INDEX idx_widgets_name ON widgets (name);\n")

	if err := checkNoUndiffableObjects(schema); err != nil {
		t.Errorf("checkNoUndiffableObjects() error = %v, want nil for tables/extensions/indexes", err)
	}
}

// TestCheckNoUndiffableObjects_IgnoresMentionsInsideComments mirrors
// TestCheckExtensionDeclarations_IgnoresMentionsInsideComments: this
// exact class of false positive (prose in a "--" comment misread as a
// declaration) was real for the extension check, so the sequence/
// trigger/view/function check must strip comments the same way.
func TestCheckNoUndiffableObjects_IgnoresMentionsInsideComments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	schema := filepath.Join(dir, "schema.sql")
	writeFile(t, schema,
		"-- Phase 2A will add a CREATE TRIGGER for the 3-resume-per-user limit;\n"+
			"-- see docs/plans/implementation-plan.md.\n"+
			"CREATE TABLE widgets (id int PRIMARY KEY);\n")

	if err := checkNoUndiffableObjects(schema); err != nil {
		t.Errorf("checkNoUndiffableObjects() error = %v, want nil: the comment's prose must not be read as a declaration", err)
	}
}

// TestRun_UndiffableObject_FailsFastWithoutAtlasOrDatabase mirrors
// TestRun_ExtensionMismatch_FailsFastWithoutAtlasOrDatabase: like the
// extension-declaration check, checkNoUndiffableObjects has no external
// dependencies, so run() must reject a declared trigger/function/view/
// sequence before ever looking for the Atlas CLI or requiring
// DATABASE_URL.
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

// writeFile is a small t.Fatal-on-error wrapper around os.WriteFile, used
// throughout this file's fixtures to keep test bodies focused on the
// scenario rather than error plumbing.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}
