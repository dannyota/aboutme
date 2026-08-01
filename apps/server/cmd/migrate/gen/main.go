// Command gen (invoked as `migrate-gen`) generates a new goose SQL
// migration from apps/server/sql/schema.sql using Atlas, then
// post-processes Atlas's output into aboutme's goose filename convention:
// a sequential NNNNN_name.sql name (Atlas itself always emits a
// timestamp-named file — see renameSequential). With --dir-format goose
// (see dirFormat below), Atlas already writes its own "-- +goose Up"/
// "-- +goose Down" headers, so ensureGooseHeader's own header-injection is
// now a defensive, idempotent no-op — see its doc comment.
//
// This tool is for contributors changing the schema only. Per
// docs/specs/aboutme-design.md §3 "Schema management", Atlas is never
// needed to build, test, or run the server, or to apply migrations — see
// ../main.go (the `migrate` command) for that, which depends only on
// goose and the embedded migration SQL.
//
// Requires:
//
//   - the Atlas CLI on PATH, pinned to atlasVersion (see below) — not
//     whatever "latest" resolves to on the day it's installed, since an
//     unpinned Atlas can silently change diff/hash behavior out from under
//     this tool and its CI drift gate. Community edition is sufficient:
//     this tool only uses `atlas migrate diff` and `atlas migrate hash`,
//     both community features (schema inspection/diffing and directory
//     checksums — not the paid checkpoints/testing/down-migration
//     features). Install with no sudo, to a user-writable path:
//
//     ATLAS_VERSION=v1.2.0 curl -sSf https://atlasgo.sh | sh -s -- -y -o "$HOME/.local/bin/atlas" --no-install --community
//
//   - DATABASE_URL pointing at a reachable Postgres server (e.g. the dev
//     compose stack's postgres). A throwaway "<dbname>_atlasdev" database
//     is created on that server for Atlas's required dev-url and dropped
//     when this tool exits.
//
// Extensions are NOT covered by this tool: Atlas's schema differ does not
// model CREATE EXTENSION at all (verified empirically — see the comment in
// migrations/00001_extensions.sql), so extension migrations are always
// hand-written, never generated. checkExtensionDeclarations instead
// cross-checks that every hand-written CREATE EXTENSION statement under
// migrationsDir has a matching declaration in schemaFile (and vice versa),
// so that drift is caught even though Atlas itself can't see it.
//
// Triggers, functions, views, and sequences are ALSO not covered, for the
// same reason (Atlas community edition's differ silently drops all four —
// verified empirically, see checkNoUndiffableObjects), but as of this
// writing have no hand-written-migration escape hatch analogous to
// extensions': checkNoUndiffableObjects unconditionally rejects declaring
// any of them in schemaFile until Phase 2A adds real cross-checking
// support for the specific object it introduces (the spec-mandated
// 3-resumes-per-user DB trigger).
//
// The migrations directory is goose-format (a "-- +goose Up"/"-- +goose
// Down" header pair per file, e.g. migrations/00001_extensions.sql), not
// Atlas's own default "atlas" directory format. Every Atlas invocation
// below passes --dir-format goose accordingly: without it, Atlas replays a
// goose file's Up *and* Down sections as one undifferentiated SQL script
// when reconstructing the migration directory's current state, silently
// undoing whatever the Down section rolls back (e.g. a CREATE EXTENSION
// immediately followed by its own DROP EXTENSION) before computing the
// next diff.
//
// Generation and post-processing happen entirely inside a temporary
// sibling directory, seeded with a copy of migrationsDir's current
// contents, so Atlas's diff/hash steps and this tool's own header/rename
// post-processing never touch the real migrationsDir while they run. The
// finished result is published with two renames within the same parent
// directory (see publish): migrationsDir is either left completely
// untouched (any failure before publish) or replaced by the complete new
// state in one step — never observed with a stray timestamped file or a
// stale atlas.sum from a partially-completed run.
//
// Usage:
//
//	go run ./cmd/migrate/gen [-name update]
//	go run ./cmd/migrate/gen -check   # report drift without writing anything
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	migrationsDir  = "migrations"
	schemaFile     = "sql/schema.sql"
	sequenceDigits = 5

	// dirFormat is the migration directory format every Atlas invocation
	// in this tool must agree on: aboutme's migrations are goose-format
	// (see the package doc comment), never Atlas's own default "atlas"
	// format.
	dirFormat = "goose"

	// atlasVersion is the pinned Atlas CLI release this tool and CI's
	// drift gate are verified against (see atlasInstallHint). Bump this
	// deliberately, alongside re-verifying the generation pipeline, not
	// silently by tracking "latest".
	atlasVersion = "v1.2.0"

	atlasInstallHint = `install the pinned version (no sudo, user-writable path):` + "\n" +
		`  ATLAS_VERSION=` + atlasVersion + ` curl -sSf https://atlasgo.sh | sh -s -- -y -o "$HOME/.local/bin/atlas" --no-install --community` + "\n" +
		`then add that directory to PATH. See https://atlasgo.io/docs#installation for other options`
)

// errAtlasNotFound is the terse, chain-friendly sentinel returned by run;
// main prints atlasInstallHint's longer, multi-line instructions
// separately so the error value itself stays a normal short Go error
// string.
var errAtlasNotFound = errors.New("atlas CLI not found on PATH")

// timestampedName matches Atlas's default output filename, e.g.
// "20260801060253_add_resumes.sql", capturing the part after the
// timestamp so it can be renumbered sequentially.
var timestampedName = regexp.MustCompile(`^\d{14}_(.+\.sql)$`)

// sequentialPrefix matches aboutme's goose filename convention,
// e.g. "00007_add_resumes.sql", capturing the sequence number.
var sequentialPrefix = regexp.MustCompile(`^(\d{5})_`)

// devNamePattern restricts the throwaway dev-database name (derived from
// DATABASE_URL's path) to a safe, unquoted-identifier-like charset before
// it is ever interpolated into DDL. Postgres has no parameterized form of
// CREATE/DROP DATABASE, so this validation — not query parameters — is
// what makes that interpolation safe.
var devNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// createExtensionPattern extracts the extension name from a CREATE
// EXTENSION statement (with or without IF NOT EXISTS, quoted or not,
// case-insensitive), for cross-checking hand-written migrations against
// sql/schema.sql's own declarations. It is deliberately loose (it doesn't
// require a trailing semicolon or validate the rest of the statement) —
// this is a drift *detector*, not a SQL parser; false negatives (missing a
// genuinely malformed statement) are far less costly here than a brittle
// parser that breaks on a harmless formatting variation. Applied only
// after sqlLineCommentPattern has stripped "--" comments (see
// extensionNames) — both schema.sql and 00001_extensions.sql document
// themselves with prose that can otherwise contain the literal words
// "CREATE EXTENSION" inside a comment, which must never count as a
// declaration.
var createExtensionPattern = regexp.MustCompile(`(?i)CREATE\s+EXTENSION\s+(?:IF\s+NOT\s+EXISTS\s+)?"?([a-zA-Z_][a-zA-Z0-9_]*)"?`)

// sqlLineCommentPattern matches a "--" line comment through the end of its
// line, so extensionNames can strip comments before scanning for CREATE
// EXTENSION statements.
var sqlLineCommentPattern = regexp.MustCompile(`(?m)--.*$`)

// undiffableObjectPattern matches the start of a CREATE statement for an
// object class Atlas's community-edition differ silently drops from
// generated migrations: sequences, views, functions, and triggers (see
// checkNoUndiffableObjects' doc comment for how this was verified).
// Applied only after sqlLineCommentPattern has stripped "--" comments,
// exactly like createExtensionPattern — schema.sql's own prose can
// otherwise contain these words too.
var undiffableObjectPattern = regexp.MustCompile(`(?i)CREATE\s+(?:OR\s+REPLACE\s+)?(TRIGGER|FUNCTION|VIEW|SEQUENCE)\s+"?([a-zA-Z_][a-zA-Z0-9_]*)"?`)

func main() {
	name := flag.String("name", "", "migration name suffix (default: \"init\" for an empty directory, \"update\" otherwise)")
	check := flag.Bool("check", false,
		"report whether sql/schema.sql has changes not yet captured by a migration, without writing anything "+
			"(exit non-zero if it does); also validates hand-written extension migrations against sql/schema.sql")
	flag.Parse()

	if err := run(*name, *check, migrationsDir, schemaFile); err != nil {
		fmt.Fprintln(os.Stderr, "migrate-gen:", err)
		if errors.Is(err, errAtlasNotFound) {
			fmt.Fprintln(os.Stderr, atlasInstallHint)
		}
		os.Exit(1)
	}
}

// run generates a new migration from schemaFile into migrationsDir (or, in
// check mode, reports whether it would). migrationsDir and schemaFile are
// parameters rather than the package constants directly so tests can point
// this at a temporary project layout without a process-wide os.Chdir.
func run(name string, check bool, migrationsDir, schemaFile string) error {
	// Cheapest, dependency-free checks first: fail fast on a static
	// extension-declaration mismatch or an undiffable object declaration
	// before requiring Atlas on PATH or a reachable database at all.
	if err := checkExtensionDeclarations(migrationsDir, schemaFile); err != nil {
		return err
	}
	if err := checkNoUndiffableObjects(schemaFile); err != nil {
		return err
	}

	if _, err := exec.LookPath("atlas"); err != nil {
		return errAtlasNotFound
	}

	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		return errors.New("DATABASE_URL is required: it must point at a reachable Postgres server " +
			"(a throwaway <dbname>_atlasdev database is created on it for Atlas's dev-url)")
	}

	if name == "" {
		name = "update"
		if !hasSQLFiles(migrationsDir) {
			name = "init"
		}
	}

	devURL, cleanup, err := createDevDatabase(adminURL)
	if err != nil {
		return fmt.Errorf("create dev database: %w", err)
	}
	defer cleanup()

	// Generate and post-process entirely in a temp directory seeded with
	// migrationsDir's current contents, so a failure at any point below
	// leaves the real migrationsDir untouched — see publish and the
	// package doc comment.
	parent := filepath.Dir(migrationsDir)
	if parent == "" {
		parent = "."
	}
	workDir, err := os.MkdirTemp(parent, "migrations.gen-*")
	if err != nil {
		return fmt.Errorf("create temporary generation directory: %w", err)
	}
	defer func() {
		if removeErr := os.RemoveAll(workDir); removeErr != nil {
			fmt.Fprintln(os.Stderr, "migrate-gen: remove temporary directory:", workDir, removeErr)
		}
	}()
	// os.MkdirTemp defaults to the restrictive 0700; override to the
	// conventional directory mode before publish ever moves this directory
	// into migrationsDir's place, so the published directory doesn't carry
	// a permission no other tool in this project would give it.
	if err = os.Chmod(workDir, conventionalDirMode); err != nil {
		return fmt.Errorf("set conventional permissions on temporary generation directory: %w", err)
	}

	if err = copyMigrationsDir(migrationsDir, workDir); err != nil {
		return fmt.Errorf("seed temporary directory from %s: %w", migrationsDir, err)
	}

	before := sqlFiles(workDir)

	if diffErr := runAtlas(
		"migrate", "diff", name,
		"--dir", "file://"+workDir,
		"--dir-format", dirFormat,
		"--to", "file://"+schemaFile,
		"--dev-url", devURL,
	); diffErr != nil {
		return fmt.Errorf("atlas migrate diff: %w", diffErr)
	}

	created := newFiles(before, sqlFiles(workDir))
	if len(created) == 0 {
		if check {
			return writeNoDrift(schemaFile)
		}
		fmt.Println("migrate-gen: no schema changes; migration directory already matches sql/schema.sql")
		return nil
	}

	for _, f := range created {
		if headerErr := ensureGooseHeader(f); headerErr != nil {
			return fmt.Errorf("add goose header to %s: %w", f, headerErr)
		}
	}

	renamed, err := renameSequential(workDir, created)
	if err != nil {
		return fmt.Errorf("rename to sequential filenames: %w", err)
	}

	if hashErr := runAtlas("migrate", "hash", "--dir", "file://"+workDir, "--dir-format", dirFormat); hashErr != nil {
		return fmt.Errorf("atlas migrate hash: %w", hashErr)
	}

	if check {
		return writeDrift(schemaFile, migrationsDir, renamed)
	}

	if err := publish(migrationsDir, workDir); err != nil {
		return fmt.Errorf("publish generated migrations: %w", err)
	}

	for _, f := range renamed {
		fmt.Println("migrate-gen: generated", filepath.Join(migrationsDir, filepath.Base(f)))
	}
	return nil
}

func writeNoDrift(schemaFile string) error {
	fmt.Printf("migrate-gen -check: %s matches the migration directory; no drift\n", schemaFile)
	return nil
}

func writeDrift(schemaFile, migrationsDir string, renamed []string) error {
	fmt.Printf("migrate-gen -check: %s has changes not captured by any migration in %s:\n", schemaFile, migrationsDir)
	for _, f := range renamed {
		fmt.Println("  would generate", filepath.Base(f))
	}
	return fmt.Errorf("schema drift detected: %d migration file(s) needed under %s to capture %s",
		len(renamed), migrationsDir, schemaFile)
}

// publish atomically replaces dir's contents with the complete generated
// state in workDir.
//
// When dir already exists, this is two renames on paths within the same
// parent directory (guaranteed: workDir was created via
// os.MkdirTemp(filepath.Dir(dir), ...)), so each is a single
// filesystem-atomic syscall: dir moves aside to backupDir, then workDir
// takes dir's place, then backupDir is removed. A crash between the first
// and second rename leaves dir itself transiently missing — an obvious,
// loud failure, never a silent partial state — while both the previous
// state (still complete and intact at backupDir) and the new state (still
// complete and intact at workDir, simply not yet moved into place) are
// fully recoverable by hand.
//
// When dir does not exist yet — the very first `migrate-gen` run in a
// project, before any migrations/ directory has ever been created — there
// is nothing to move aside, so publish skips straight to installing
// workDir at dir. Every other function in this file already tolerates a
// missing migrationsDir the same way: copyMigrationsDir treats it as
// empty, sqlFiles returns none, nextSequence returns 1, and run defaults
// the generated name to "init" (see their doc comments) — publish must
// too, or the very first generation in a project would always fail.
func publish(dir, workDir string) error {
	if _, statErr := os.Stat(dir); statErr != nil {
		if !os.IsNotExist(statErr) {
			return fmt.Errorf("stat %s: %w", dir, statErr)
		}
		if err := os.Rename(workDir, dir); err != nil {
			return fmt.Errorf("publish %s: %w", dir, err)
		}
		return nil
	}

	backupDir := workDir + ".bak"
	if err := os.Rename(dir, backupDir); err != nil {
		return fmt.Errorf("move aside current %s: %w", dir, err)
	}
	if err := os.Rename(workDir, dir); err != nil {
		if restoreErr := os.Rename(backupDir, dir); restoreErr != nil {
			return fmt.Errorf("publish %s (restoring previous contents also failed: %w): %w", dir, restoreErr, err)
		}
		return fmt.Errorf("publish %s: %w", dir, err)
	}
	if err := os.RemoveAll(backupDir); err != nil {
		fmt.Fprintln(os.Stderr, "migrate-gen: remove backup directory:", backupDir, err)
	}
	return nil
}

// conventionalFileMode is the permission this tool writes migration files
// with: the standard non-executable, world-readable mode a normal editor
// save or `git checkout` produces (matches the real committed
// apps/server/migrations/*.sql and atlas.sum, verified directly). Used
// instead of a more restrictive mode so files copied through the
// temporary generation directory (see copyMigrationsDir) and then
// published come out identical to files that were never touched by this
// tool at all — no mixed permissions within migrationsDir depending on
// which specific run last regenerated a neighboring file.
const conventionalFileMode = 0o644

// conventionalDirMode is migrationsDir's permission after publish — see
// conventionalFileMode; os.MkdirTemp's own default (0700) is deliberately
// overridden to this before anything is copied into the temp directory
// (see run), so the directory ends up matching convention too, not just
// its files.
const conventionalDirMode = 0o755

// copyMigrationsDir copies every regular file directly inside src (never
// subdirectories — migrationsDir never has any) into dst, preserving
// names and writing conventionalFileMode regardless of src's own file
// modes. It seeds the temporary generation directory with the existing
// migration set (including atlas.sum) so Atlas's diff can replay history
// exactly as it would against the real directory, without this tool ever
// writing into the real directory itself. A missing src (no migrations
// generated yet) is not an error: dst is simply left empty.
func copyMigrationsDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name())) //nolint:gosec // e.Name() comes from os.ReadDir(src), never external input
		if err != nil {
			return fmt.Errorf("read %s: %w", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, conventionalFileMode); err != nil { //nolint:gosec // e.Name() comes from os.ReadDir(src), never external input
			return fmt.Errorf("write %s: %w", e.Name(), err)
		}
	}
	return nil
}

// extensionNames returns the lowercased set of extension names named by
// every CREATE EXTENSION statement found in sqlText, after stripping "--"
// line comments — a comment mentioning "CREATE EXTENSION" in prose (both
// schema.sql and 00001_extensions.sql do exactly this) must never count as
// a declaration.
func extensionNames(sqlText string) map[string]bool {
	stripped := sqlLineCommentPattern.ReplaceAllString(sqlText, "")
	names := map[string]bool{}
	for _, m := range createExtensionPattern.FindAllStringSubmatch(stripped, -1) {
		names[strings.ToLower(m[1])] = true
	}
	return names
}

// checkExtensionDeclarations verifies that the extensions named by
// hand-written CREATE EXTENSION statements anywhere under migrationsDir
// are exactly the extensions schemaFile declares. Atlas's differ never
// generates CREATE EXTENSION (see the package doc comment), so every such
// statement under migrationsDir is by construction hand-written; this is
// the only mechanism that keeps that hand-written set in sync with
// schemaFile as either one changes.
func checkExtensionDeclarations(migrationsDir, schemaFile string) error {
	schemaBytes, err := os.ReadFile(schemaFile) //nolint:gosec // schemaFile is a program parameter (constant in production, temp-dir path in tests), never external input
	if err != nil {
		return fmt.Errorf("read %s: %w", schemaFile, err)
	}
	declared := extensionNames(string(schemaBytes))

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		if os.IsNotExist(err) {
			entries = nil
		} else {
			return fmt.Errorf("read %s: %w", migrationsDir, err)
		}
	}

	handWritten := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(migrationsDir, e.Name())) //nolint:gosec // e.Name() comes from os.ReadDir(migrationsDir), never external input
		if err != nil {
			return fmt.Errorf("read %s: %w", e.Name(), err)
		}
		for extName := range extensionNames(string(data)) {
			handWritten[extName] = true
		}
	}

	var missingMigration, missingSchema []string
	for extName := range declared {
		if !handWritten[extName] {
			missingMigration = append(missingMigration, extName)
		}
	}
	for extName := range handWritten {
		if !declared[extName] {
			missingSchema = append(missingSchema, extName)
		}
	}
	if len(missingMigration) == 0 && len(missingSchema) == 0 {
		return nil
	}
	sort.Strings(missingMigration)
	sort.Strings(missingSchema)
	return fmt.Errorf("extension declarations drifted between %s and hand-written migrations in %s: "+
		"declared in schema with no hand-written CREATE EXTENSION: %v; "+
		"hand-written CREATE EXTENSION with no matching schema declaration: %v",
		schemaFile, migrationsDir, missingMigration, missingSchema)
}

// checkNoUndiffableObjects fails if schemaFile declares a trigger,
// function, view, or sequence: Atlas community edition's schema differ
// silently drops all four object classes from generated migrations. This
// was verified empirically against the pinned Atlas version (see
// atlasVersion) with a live dev database: a CREATE SEQUENCE alongside a
// CREATE TABLE produced a migration containing only the table; a
// CREATE FUNCTION + CREATE TRIGGER pair (the shape Phase 2A's
// spec-mandated 3-resumes-per-user DB trigger will take — see
// docs/specs/aboutme-design.md and docs/plans/implementation-plan.md)
// produced a migration containing only the table it was attached to, with
// no error and no warning. Left unchecked, `migrate-gen` would report "no
// schema changes" for a schema.sql that actually needs one, and the CI
// drift gate (this command's own -check flag) would report clean for a
// database that never received the object — confirmed end-to-end: adding
// a bare CREATE SEQUENCE to the real committed sql/schema.sql and running
// the real drift gate script passed with EXIT=0.
//
// Unlike extensions (checkExtensionDeclarations), these four classes have
// no hand-written-migration escape hatch yet, so any declaration is
// unconditionally rejected rather than cross-checked. When Phase 2A adds
// the 3-resume trigger, that phase must extend this cross-check to real
// hand-written-migration diffing for the object it introduces (the same
// pattern checkExtensionDeclarations already established for extensions)
// — not simply delete or loosen this check.
func checkNoUndiffableObjects(schemaFile string) error {
	data, err := os.ReadFile(schemaFile) //nolint:gosec // schemaFile is a program parameter (constant in production, temp-dir path in tests), never external input
	if err != nil {
		return fmt.Errorf("read %s: %w", schemaFile, err)
	}
	stripped := sqlLineCommentPattern.ReplaceAllString(string(data), "")

	m := undiffableObjectPattern.FindStringSubmatch(stripped)
	if m == nil {
		return nil
	}
	return fmt.Errorf(
		"%s declares a %s (%q): Atlas community edition's schema differ silently ignores triggers, "+
			"functions, views, and sequences — migrate-gen would generate no migration for it and the "+
			"drift gate would report no drift. These object classes have no hand-written-migration "+
			"cross-check yet (unlike extensions — see checkExtensionDeclarations); do not declare one in "+
			"%s until that support exists",
		schemaFile, strings.ToLower(m[1]), m[2], schemaFile,
	)
}

// createDevDatabase creates a throwaway "<dbname>_atlasdev" database on the
// same Postgres server as adminURL, for Atlas's required --dev-url (a
// scratch database Atlas uses to compute the diff; it is never the target
// of any migration). It returns a connection URL for the new database and
// a cleanup function that drops it.
func createDevDatabase(adminURL string) (devURL string, cleanup func(), err error) {
	u, err := url.Parse(adminURL)
	if err != nil {
		return "", nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	baseName := strings.TrimPrefix(u.Path, "/")
	if baseName == "" {
		return "", nil, errors.New("DATABASE_URL must include a database name")
	}
	devName := baseName + "_atlasdev"
	// Postgres has no parameterized CREATE/DROP DATABASE, so devName is
	// interpolated directly into DDL below; this charset check is what
	// makes that safe rather than the query-parameter mechanism used
	// elsewhere in the codebase.
	if !devNamePattern.MatchString(devName) {
		return "", nil, fmt.Errorf("database name %q (from DATABASE_URL) must match %s", devName, devNamePattern)
	}

	admin, err := sql.Open("pgx", adminURL)
	if err != nil {
		return "", nil, fmt.Errorf("open admin connection: %w", err)
	}

	ctx := context.Background()
	// Best-effort: drop any leftover dev database from a previous crashed
	// run before (re)creating it. devName is validated above.
	if _, dropErr := admin.ExecContext(ctx, `DROP DATABASE IF EXISTS "`+devName+`"`); dropErr != nil { //nolint:gosec // devName validated by devNamePattern
		fmt.Fprintln(os.Stderr, "migrate-gen: drop stale dev database:", dropErr)
	}
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE "`+devName+`"`); err != nil { //nolint:gosec // devName validated by devNamePattern
		if closeErr := admin.Close(); closeErr != nil {
			fmt.Fprintln(os.Stderr, "migrate-gen: close admin connection:", closeErr)
		}
		return "", nil, fmt.Errorf("create database %s: %w", devName, err)
	}

	devU := *u
	devU.Path = "/" + devName
	devURL = devU.String()

	cleanup = func() {
		if _, dropErr := admin.ExecContext(context.Background(), `DROP DATABASE IF EXISTS "`+devName+`"`); dropErr != nil { //nolint:gosec // devName validated by devNamePattern
			fmt.Fprintln(os.Stderr, "migrate-gen: drop dev database:", dropErr)
		}
		if closeErr := admin.Close(); closeErr != nil {
			fmt.Fprintln(os.Stderr, "migrate-gen: close admin connection:", closeErr)
		}
	}
	return devURL, cleanup, nil
}

// runAtlas invokes the Atlas CLI with args. This is a local, contributor-
// only dev tool (never runs on a server or with untrusted input): args are
// always either fixed subcommand tokens ("migrate", "diff", "hash",
// "--dir", ...) or values this program built itself (schemaFile,
// migrationsDir/workDir, devURL from createDevDatabase). exec.Command never
// invokes a shell, so there is no shell-injection surface even for the
// one operator-supplied value (-name).
func runAtlas(args ...string) error {
	cmd := exec.CommandContext(context.Background(), "atlas", args...) //nolint:gosec // dev-only tool; args are static tokens or self-constructed values, no shell involved
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func hasSQLFiles(dir string) bool {
	return len(sqlFiles(dir)) > 0
}

// sqlFiles returns the set of *.sql paths (dir-joined) currently in dir.
// A missing directory is treated as empty rather than an error, since the
// migrations directory does not exist until the first migration is
// generated.
func sqlFiles(dir string) map[string]bool {
	set := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return set
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			set[filepath.Join(dir, e.Name())] = true
		}
	}
	return set
}

// newFiles returns the paths present in after but not before, sorted so
// callers see them in the order Atlas wrote them.
func newFiles(before, after map[string]bool) []string {
	var out []string
	for f := range after {
		if !before[f] {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

// ensureGooseHeader prepends "-- +goose Up" to path's content if not
// already present. With --dir-format goose (dirFormat), Atlas already
// writes its own "-- +goose Up"/"-- +goose Down" headers (verified
// empirically against the pinned Atlas version), so in practice this is
// now a defensive, idempotent no-op — kept rather than removed as a
// belt-and-suspenders guard against a future Atlas version or dir-format
// change silently dropping the header again, which would otherwise
// produce a migration file goose can't recognize as goose-format at all.
// path is always one this program just discovered under migrationsDir via
// sqlFiles/os.ReadDir (never user input), so there is no path-traversal
// input to guard against here.
func ensureGooseHeader(path string) error {
	data, err := os.ReadFile(path) //nolint:gosec // path always comes from sqlFiles(migrationsDir), never external input
	if err != nil {
		return err
	}
	content := string(data)
	if strings.Contains(content, "-- +goose Up") {
		return nil
	}
	return os.WriteFile(path, []byte("-- +goose Up\n"+content), 0o600) //nolint:gosec // same path as the ReadFile above
}

// renameSequential renames each Atlas-timestamped path in created (sorted
// chronologically, since Atlas's timestamps are string-sortable) to the
// next available NNNNN_name.sql sequence number in dir. Paths that don't
// match the timestamped pattern are left as-is (defensive: lets a
// hand-tweaked or already-sequential name pass through unchanged rather
// than erroring).
func renameSequential(dir string, created []string) ([]string, error) {
	next, err := nextSequence(dir)
	if err != nil {
		return nil, err
	}

	sorted := append([]string(nil), created...)
	sort.Strings(sorted)

	renamed := make([]string, 0, len(sorted))
	for _, path := range sorted {
		base := filepath.Base(path)
		m := timestampedName.FindStringSubmatch(base)
		if m == nil {
			renamed = append(renamed, path)
			continue
		}

		newBase := fmt.Sprintf("%0*d_%s", sequenceDigits, next, m[1])
		newPath := filepath.Join(dir, newBase)
		if err := os.Rename(path, newPath); err != nil {
			return nil, fmt.Errorf("rename %s: %w", base, err)
		}
		renamed = append(renamed, newPath)
		next++
	}
	return renamed, nil
}

// nextSequence returns one past the highest NNNNN_ prefix currently in
// dir (1 if none exist yet).
func nextSequence(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 1, nil
		}
		return 0, err
	}

	max := 0
	for _, e := range entries {
		m := sequentialPrefix.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	return max + 1, nil
}
