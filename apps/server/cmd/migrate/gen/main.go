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
// Triggers, functions, procedures, views, sequences, rules, and policies
// are ALSO not covered, for the same reason (Atlas community edition's
// differ silently drops all of them — verified empirically for triggers,
// functions, views, and sequences, see checkUndiffableObjects). A bare
// CREATE FUNCTION or CREATE [OR REPLACE] [CONSTRAINT] TRIGGER statement —
// the shape Phase 2A's spec-mandated 3-resumes-per-user DB trigger takes —
// gets a real hand-written-migration cross-check instead of an
// unconditional reject, the same pattern checkExtensionDeclarations
// already established for extensions (see checkUndiffableObjects'
// crossCheckFunctionsAndTriggers). Every other matched class, and any
// ALTER that mutates a function or trigger, has no such escape hatch and
// is rejected unconditionally.
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
// generated migrations: sequences, views, functions, triggers, procedures,
// rules, and policies, in every CONSTRAINT/MATERIALIZED/RECURSIVE/TEMP/
// TEMPORARY/UNLOGGED variant (see checkUndiffableObjects' doc comment for
// how the base set was verified). Applied only after stripSQLComments has
// stripped comments, exactly like createExtensionPattern — schema.sql's
// own prose can otherwise contain these words too. This is a drift
// *detector*, not a SQL parser (see createExtensionPattern's doc comment
// for the same tradeoff): it deliberately permits modifier combinations
// that aren't valid Postgres grammar (e.g. "CREATE RECURSIVE SEQUENCE")
// rather than risk a false negative on a real one.
//
// The class keyword is matched with a trailing \b and the name afterward
// is captured in its own, wholly optional, lenient group ([^\s(;]+ — any
// run of non-whitespace, non-paren, non-semicolon bytes): a detector must
// never make FIRING contingent on the declared name's shape (regression
// for review finding Minor 4 — a digit-leading double-quoted name like
// "1st_policy" made the previous strict [a-zA-Z_][a-zA-Z0-9_]* name group
// fail to match at all, which failed the ENTIRE statement match, silently
// accepting the object). Group 2 may come back "" for a genuinely
// malformed statement; every real caller of this pattern only relies on
// group 2 for a human-readable name in an error message, never for
// control flow.
var undiffableObjectPattern = regexp.MustCompile(
	`(?i)CREATE\s+(?:OR\s+REPLACE\s+)?` +
		`(?:(?:CONSTRAINT|MATERIALIZED|RECURSIVE|TEMP|TEMPORARY|UNLOGGED)\s+)*` +
		`(TRIGGER|FUNCTION|PROCEDURE|VIEW|SEQUENCE|RULE|POLICY)\b` +
		`(?:\s+([^\s(;]+))?`,
)

// alterFunctionOrTriggerPattern matches ALTER FUNCTION or ALTER TRIGGER —
// the B4 addendum: unlike a bare CREATE FUNCTION/TRIGGER, an ALTER is never
// eligible for the D9 cross-check (see checkUndiffableObjects), since the
// cross-check only ever compares CREATE statement text. An ALTER that
// retargets a function body or renames a trigger must be rejected
// unconditionally, with no escape hatch, ever.
var alterFunctionOrTriggerPattern = regexp.MustCompile(`(?i)\bALTER\s+(FUNCTION|TRIGGER)\s+"?([a-zA-Z_][a-zA-Z0-9_]*)"?`)

// alterTableTriggerTogglePattern matches ALTER TABLE ... {ENABLE|DISABLE}
// TRIGGER trigger_name — same B4 rationale as alterFunctionOrTriggerPattern:
// silently toggling a trigger off is exactly as invisible to Atlas's differ
// as dropping it outright, and must never get a cross-check escape hatch.
var alterTableTriggerTogglePattern = regexp.MustCompile(`(?is)\bALTER\s+TABLE\s+.*?\b(ENABLE|DISABLE)\s+TRIGGER\s+"?([a-zA-Z_][a-zA-Z0-9_]*)"?`)

// dropFunctionOrTriggerPattern matches DROP FUNCTION or DROP TRIGGER,
// applied only to a migration's "-- +goose Up" section (see
// crossCheckFunctionsAndTriggers): the D9 cross-check never permits
// dropping a function or trigger there — only the matching "-- +goose
// Down" section may legitimately do that as part of a rollback.
var dropFunctionOrTriggerPattern = regexp.MustCompile(`(?i)\bDROP\s+(FUNCTION|TRIGGER)\s+(?:IF\s+EXISTS\s+)?"?([a-zA-Z_][a-zA-Z0-9_]*)"?`)

// leadingOrReplacePattern matches a leading "CREATE OR REPLACE" so it can
// be elided to "CREATE" before comparing two statements — B3 normalization
// stage 4: "CREATE FUNCTION f()" and "CREATE OR REPLACE FUNCTION f()" for
// the identical body must compare equal.
var leadingOrReplacePattern = regexp.MustCompile(`(?i)^CREATE\s+OR\s+REPLACE\s+`)

// undiffableStatementNamePattern captures, immediately after the FUNCTION
// or TRIGGER keyword (and any leading CONSTRAINT modifier for TRIGGER —
// already consumed by undiffableObjectPattern's caller before this runs),
// the declared object's raw name span: bare, schema-qualified
// ("public.foo"), double-quoted ("\"Foo\""), or both. Anchored at the
// correct token — not the first identifier-shaped substring anywhere in
// the statement, which could otherwise latch onto a column name, a
// referenced table, or an argument type first — see B3 normalization
// stage 5.
var undiffableStatementNamePattern = regexp.MustCompile(
	`(?i)\b(FUNCTION|TRIGGER)\s+((?:"[^"]+"|[a-zA-Z_][a-zA-Z0-9_]*)(?:\.(?:"[^"]+"|[a-zA-Z_][a-zA-Z0-9_]*))?)`,
)

// triggerTargetTablePattern captures a CREATE TRIGGER statement's target
// table — the "ON table_name" clause that always follows the trigger's
// own name and event list (BEFORE/AFTER/INSTEAD OF ...), and always
// precedes EXECUTE FUNCTION — anchored via the non-greedy .*? so it finds
// THAT "ON", not some other occurrence of the word. A bare trigger name is
// not its real Postgres identity: two different tables may each have
// their own, independent, same-named trigger (see undiffableObjectIdentity).
var triggerTargetTablePattern = regexp.MustCompile(
	`(?is)\bTRIGGER\s+(?:"[^"]+"|[a-zA-Z_][a-zA-Z0-9_]*)\s+.*?\bON\s+((?:"[^"]+"|[a-zA-Z_][a-zA-Z0-9_]*)(?:\.(?:"[^"]+"|[a-zA-Z_][a-zA-Z0-9_]*))?)`,
)

// gooseUpMarker and gooseDownMarker locate a goose migration file's
// "-- +goose Up"/"-- +goose Down" section headers (see gooseUpSection).
var (
	gooseUpMarker   = regexp.MustCompile(`(?m)^--\s*\+goose\s+Up\b.*$`)
	gooseDownMarker = regexp.MustCompile(`(?m)^--\s*\+goose\s+Down\b.*$`)
)

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
	if err := checkUndiffableObjects(migrationsDir, schemaFile); err != nil {
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

// stripSQLComments returns sql with every "--" line comment and /* ... */
// block comment removed — B3 normalization stage 1 — while leaving any
// single-quoted string literal or dollar-quoted span's content completely
// untouched, including one that happens to contain "--" or "/*" as
// literal data (e.g. DEFAULT '--'). It is a thin reassembly of
// scanSQLSegments' segments (which recognize comments only in plain,
// non-quoted context — see that function's doc comment): regression for
// review finding Minor 5, where an earlier version ran the comment
// regexes as a blind pre-pass over raw text, misreading a literal "--"
// inside a quoted string as a real comment and stripping past it —
// corrupting the literal into an unbalanced quote that made every later
// stage (scanSQLSegments' own quote-tracking, then splitStatements) merge
// the rest of the file into one dangling verbatim span.
func stripSQLComments(sql string) string {
	var out strings.Builder
	for _, seg := range scanSQLSegments(sql) {
		out.WriteString(seg.text)
	}
	return out.String()
}

// matchDollarTagAt reports whether sql[i:] begins with a Postgres
// dollar-quote delimiter ("$$" or "$tag$", tag being any run of letters,
// digits, or underscores) and returns that delimiter's exact text — the
// same text closes the span, verbatim.
func matchDollarTagAt(sql string, i int) (string, bool) {
	if sql[i] != '$' {
		return "", false
	}
	j := i + 1
	for j < len(sql) && isIdentByte(sql[j]) {
		j++
	}
	if j < len(sql) && sql[j] == '$' {
		return sql[i : j+1], true
	}
	return "", false
}

func isIdentByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// sqlSegment is one contiguous span of already-comment-stripped SQL text,
// tagged with whether it is verbatim (inside a single-quoted string
// literal or a dollar-quoted span, where Postgres content compares
// byte-for-byte) or plain (ordinary syntax, safe to reformat or split on).
type sqlSegment struct {
	text     string
	verbatim bool
}

// scanSQLSegments splits sql into alternating plain/verbatim segments by
// tracking single-quoted string literals ('...', with a doubled single
// quote as the SQL escape) and dollar-quoted spans ($$...$$ or
// $tag$...$tag$) — the two verbatim-content forms Postgres recognizes
// inside a statement. Shared by splitStatements (a plain segment's ';'
// ends a statement; one inside a verbatim segment never does),
// collapseWhitespace (only a plain segment's whitespace is safe to
// collapse — B3 normalization stage 2), and stripSQLComments.
//
// "--" line comments and /* ... */ block comments (B3 normalization stage
// 1) are recognized and discarded in THIS single pass too, rather than as
// a separate pre-pass over raw text (regression for review finding
// Minor 5): a comment marker is checked for only while in plain
// (non-quoted) context, BEFORE the quote/dollar-quote checks below, so it
// is never misread as starting inside an already-open quoted span (a "--"
// or "/*" that is really just data in a string literal, e.g.
// DEFAULT '--', must never be treated as a comment), and — just as
// importantly — a quote character appearing inside a real comment (e.g.
// "-- don't strip me") is never misread as opening a string literal
// either, since comment bytes are skipped before the quote checks ever
// see them.
//
// An unterminated quote/dollar-quote/block-comment (malformed SQL) is
// treated as running to the end of the text, rather than panicking or
// looping forever looking for a close that will never come.
func scanSQLSegments(sql string) []sqlSegment {
	var segments []sqlSegment
	var plain strings.Builder
	flushPlain := func() {
		if plain.Len() > 0 {
			segments = append(segments, sqlSegment{text: plain.String()})
			plain.Reset()
		}
	}

	i := 0
	for i < len(sql) {
		if i+1 < len(sql) && sql[i] == '-' && sql[i+1] == '-' {
			j := i + 2
			for j < len(sql) && sql[j] != '\n' {
				j++
			}
			i = j
			continue
		}
		if i+1 < len(sql) && sql[i] == '/' && sql[i+1] == '*' {
			end := strings.Index(sql[i+2:], "*/")
			if end == -1 {
				break
			}
			i = i + 2 + end + 2
			continue
		}
		if sql[i] == '\'' {
			flushPlain()
			j := i + 1
			for j < len(sql) {
				if sql[j] == '\'' {
					if j+1 < len(sql) && sql[j+1] == '\'' {
						j += 2
						continue
					}
					j++
					break
				}
				j++
			}
			segments = append(segments, sqlSegment{text: sql[i:j], verbatim: true})
			i = j
			continue
		}
		if tag, ok := matchDollarTagAt(sql, i); ok {
			flushPlain()
			closeIdx := strings.Index(sql[i+len(tag):], tag)
			if closeIdx == -1 {
				segments = append(segments, sqlSegment{text: sql[i:], verbatim: true})
				return segments
			}
			closeAt := i + len(tag) + closeIdx + len(tag)
			segments = append(segments, sqlSegment{text: sql[i:closeAt], verbatim: true})
			i = closeAt
			continue
		}
		plain.WriteByte(sql[i])
		i++
	}
	flushPlain()
	return segments
}

// splitStatements splits sql (assumed already comment-stripped) into
// individual statements, each still carrying its terminating ";"
// verbatim. A ";" inside a single-quoted string literal or a dollar-quoted
// span never ends a statement — this is what lets a function body's own
// internal semicolons (inside its dollar-quoted body) survive intact
// rather than truncating the statement at the first one, e.g.
// "$$ BEGIN ...; END; $$ LANGUAGE plpgsql;" extracts as ONE statement
// terminated by the final ";", not the first one inside the body.
func splitStatements(sql string) []string {
	var statements []string
	var current strings.Builder
	for _, seg := range scanSQLSegments(sql) {
		if seg.verbatim {
			current.WriteString(seg.text)
			continue
		}
		start := 0
		for idx := 0; idx < len(seg.text); idx++ {
			if seg.text[idx] == ';' {
				current.WriteString(seg.text[start : idx+1])
				if stmt := strings.TrimSpace(current.String()); stmt != "" {
					statements = append(statements, stmt)
				}
				current.Reset()
				start = idx + 1
			}
		}
		current.WriteString(seg.text[start:])
	}
	if rest := strings.TrimSpace(current.String()); rest != "" {
		statements = append(statements, rest)
	}
	return statements
}

// collapseWhitespace collapses runs of whitespace to a single space,
// leaving byte-for-byte content inside a single-quoted string literal or a
// dollar-quoted span untouched — B3 normalization stage 2. Reformatting a
// migration or schema.sql's whitespace outside any quoted span (extra
// blank lines, reindentation) must never look like a body change, but
// whitespace INSIDE a function's dollar-quoted body is part of that
// body's real text and must never be erased by this stage, or a real
// one-token body edit whose surrounding whitespace also happens to differ
// would be masked as "no diff".
func collapseWhitespace(sql string) string {
	var out strings.Builder
	for _, seg := range scanSQLSegments(sql) {
		if seg.verbatim {
			out.WriteString(seg.text)
			continue
		}
		lastWasSpace := false
		for i := 0; i < len(seg.text); i++ {
			c := seg.text[i]
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
				if !lastWasSpace {
					out.WriteByte(' ')
					lastWasSpace = true
				}
				continue
			}
			out.WriteByte(c)
			lastWasSpace = false
		}
	}
	return strings.TrimSpace(out.String())
}

// elideOrReplace removes a leading "OR REPLACE" (B3 normalization stage
// 4), so "CREATE FUNCTION f()" and "CREATE OR REPLACE FUNCTION f()" for
// the identical body compare equal — Postgres doesn't distinguish the two
// for any purpose this cross-check cares about.
func elideOrReplace(stmt string) string {
	return leadingOrReplacePattern.ReplaceAllString(stmt, "CREATE ")
}

// canonicalObjectName reduces a raw, possibly schema-qualified and/or
// double-quoted object name span (as captured by
// undiffableStatementNamePattern) to a bare lowercase identifier — B3
// normalization stage 5. sql/schema.sql declaring "public.enforce_max_resumes"
// and a migration hand-writing "enforce_max_resumes" (or
// "\"enforce_max_resumes\"") name the same underlying object.
func canonicalObjectName(raw string) string {
	if idx := strings.LastIndex(raw, "."); idx != -1 {
		raw = raw[idx+1:]
	}
	raw = strings.Trim(raw, `"`)
	return strings.ToLower(raw)
}

// normalizeUndiffableStatement applies the B3 normalization pipeline to a
// single extracted CREATE FUNCTION/TRIGGER statement (comment-stripping,
// stage 1, already happened at the whole-file level — see
// stripSQLComments): whitespace is collapsed outside any quoted/
// dollar-quoted span (stage 2), "OR REPLACE" is elided (stage 4), and the
// declared object's own name is reduced to canonical form at its
// declaration site (stage 5) so a schema-qualifier or quoting difference
// there — never a real body change — doesn't register as one. It returns
// the canonical name (for matching the same object across schema.sql and
// migrationsDir) and the normalized statement text; comparing the latter
// with a plain Go string == is stage 3 (case-sensitive, no folding).
func normalizeUndiffableStatement(stmt string) (name, normalized string, err error) {
	ws := elideOrReplace(collapseWhitespace(stmt))

	loc := undiffableStatementNamePattern.FindStringSubmatchIndex(ws)
	if loc == nil {
		return "", "", fmt.Errorf("internal error: could not locate a FUNCTION/TRIGGER name in statement: %s", ws)
	}
	raw := ws[loc[4]:loc[5]]
	canonical := canonicalObjectName(raw)
	normalized = ws[:loc[4]] + canonical + ws[loc[5]:]
	return canonical, normalized, nil
}

// captureParenSpan returns the substring of s starting at the first '('
// at or after index start through its matching ')', respecting
// single-quoted string literals (so a parenthesis inside a default-value
// string literal, e.g. DEFAULT 'a)b', is never mistaken for a real one)
// and nested parentheses (e.g. a type modifier like numeric(10,2), or a
// default expression like DEFAULT (1+2)). ok is false if s has no such
// balanced span (malformed SQL) — every real caller already knows stmt
// matched undiffableObjectPattern with class FUNCTION and is only
// extracting the argument list that must exist right after the name.
func captureParenSpan(s string, start int) (span string, ok bool) {
	i := start
	for i < len(s) && s[i] != '(' {
		i++
	}
	if i >= len(s) {
		return "", false
	}
	openAt := i
	depth := 0
	for i < len(s) {
		switch s[i] {
		case '\'':
			j := i + 1
			for j < len(s) {
				if s[j] == '\'' {
					if j+1 < len(s) && s[j+1] == '\'' {
						j += 2
						continue
					}
					j++
					break
				}
				j++
			}
			i = j
			continue
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[openAt : i+1], true
			}
		}
		i++
	}
	return "", false
}

// undiffableObjectIdentity computes the composite identity D9 uses to
// tell two same-named objects apart — regression for review finding
// Important 1: a bare name is not either class's real identity in
// Postgres. A TRIGGER's name is scoped to its target table (two different
// tables may each have their own, independent, same-named trigger); a
// FUNCTION's name is scoped to its argument list (overloading). Keying
// the cross-check's maps on the bare name alone let a migration's OTHER
// same-named object (on a different table, or a different overload)
// silently stand in for the one schema.sql actually declares, or vice
// versa. key is the composite identity, safe as a map key — it is
// prefixed with class, so a FUNCTION and TRIGGER that happen to share a
// bare name never collide either, a real failure the bare-name key hit in
// practice. name is the canonical bare name and detail a short
// human-readable qualifier ("ON table" or the normalized argument list),
// both for error messages only.
func undiffableObjectIdentity(class, stmt string) (key, name, detail string, err error) {
	m := undiffableStatementNamePattern.FindStringSubmatchIndex(stmt)
	if m == nil {
		return "", "", "", fmt.Errorf("internal error: could not locate a FUNCTION/TRIGGER name in statement: %s", stmt)
	}
	name = canonicalObjectName(stmt[m[4]:m[5]])

	switch class {
	case "FUNCTION":
		argSpan, ok := captureParenSpan(stmt, m[5])
		if !ok {
			return "", "", "", fmt.Errorf("internal error: could not locate function %q's argument list: %s", name, stmt)
		}
		normalizedArgs := collapseWhitespace(argSpan)
		return "FUNCTION:" + name + ":" + normalizedArgs, name, normalizedArgs, nil
	case "TRIGGER":
		tm := triggerTargetTablePattern.FindStringSubmatch(stmt)
		if tm == nil {
			return "", "", "", fmt.Errorf("internal error: could not locate trigger %q's target table (ON <table>): %s", name, stmt)
		}
		table := canonicalObjectName(tm[1])
		return "TRIGGER:" + name + ":" + table, name, "ON " + table, nil
	default:
		return "", "", "", fmt.Errorf("internal error: undiffableObjectIdentity called for unsupported class %q", class)
	}
}

// undiffableDecl is one CREATE FUNCTION/TRIGGER statement extracted from
// sql/schema.sql, pending the D9 cross-check against migrationsDir.
type undiffableDecl struct {
	class     string // "FUNCTION" or "TRIGGER"
	statement string // raw, as extracted — normalized lazily by the cross-check
}

// matchAlterUndiffableObject reports whether stmt is an ALTER statement
// the B4 addendum unconditionally rejects (ALTER FUNCTION, ALTER TRIGGER,
// or ALTER TABLE ... {ENABLE|DISABLE} TRIGGER), returning the affected
// object's class and name.
func matchAlterUndiffableObject(stmt string) (class, name string, ok bool) {
	if m := alterFunctionOrTriggerPattern.FindStringSubmatch(stmt); m != nil {
		return strings.ToUpper(m[1]), m[2], true
	}
	if m := alterTableTriggerTogglePattern.FindStringSubmatch(stmt); m != nil {
		return "TRIGGER", m[2], true
	}
	return "", "", false
}

// unconditionalRejectError formats the rejection message for every
// undiffable declaration that is NOT eligible for the D9 cross-check: any
// matched class other than a bare CREATE [OR REPLACE] FUNCTION or
// CREATE [OR REPLACE] [CONSTRAINT] TRIGGER (see checkUndiffableObjects),
// including every ALTER variant the B4 addendum added — an ALTER is never
// eligible regardless of which object class it targets, since the
// cross-check only ever compares CREATE statement text. location
// describes where the violation was found in human-readable form: either
// schemaFile directly, or (regression for review finding Important 3) a
// phrase describing a hand-written migration's own "-- +goose Up"
// section — the B4 addendum is enforced there too, not only against
// schema.sql, since an ALTER hiding in a migration is exactly as invisible
// to Atlas's differ as one hiding in schema.sql.
func unconditionalRejectError(location, class, name string) error {
	return fmt.Errorf(
		"%s declares or alters a %s (%q): Atlas community edition's schema differ silently ignores triggers, "+
			"functions, procedures, views, sequences, rules, and policies, in every CREATE or ALTER form "+
			"(including CONSTRAINT/MATERIALIZED/RECURSIVE/TEMP/TEMPORARY/UNLOGGED variants) — migrate-gen "+
			"would generate no migration for it and the drift gate would report no drift. Only a bare "+
			"CREATE [OR REPLACE] FUNCTION or CREATE [OR REPLACE] [CONSTRAINT] TRIGGER statement is ever "+
			"eligible for the hand-written-migration cross-check (see checkUndiffableObjects); a %s has "+
			"no such escape hatch — do not declare one in %s",
		location, strings.ToLower(class), name, strings.ToLower(class), location,
	)
}

// checkUndiffableObjects replaces the previous checkNoUndiffableObjects:
// it fails if schemaFile declares a trigger, function, procedure, view,
// sequence, rule, or policy — Atlas community edition's schema differ
// silently drops all of these from generated migrations (verified
// empirically for triggers, functions, views, and sequences against the
// pinned Atlas version, see atlasVersion, with a live dev database: a
// CREATE SEQUENCE alongside a CREATE TABLE produced a migration containing
// only the table; a CREATE FUNCTION + CREATE TRIGGER pair produced a
// migration containing only the table it was attached to, with no error
// and no warning — confirmed end-to-end: adding a bare CREATE SEQUENCE to
// the real committed sql/schema.sql and running the real drift gate
// script passed with EXIT=0).
//
// Unlike the other classes, a bare CREATE FUNCTION or
// CREATE [OR REPLACE] [CONSTRAINT] TRIGGER statement gets the D9
// statement-level cross-check (crossCheckFunctionsAndTriggers) instead of
// an unconditional reject: its normalized text must match the last
// occurrence of a matching hand-written migration under migrationsDir
// (the same pattern checkExtensionDeclarations already established for
// extensions), with names matching in both directions and no
// DROP FUNCTION/TRIGGER permitted in a migration's "-- +goose Up"
// section. Every other matched class — including every ALTER that mutates
// a function or trigger (the B4 addendum) — has no such escape hatch and
// is rejected unconditionally, with the same message shape as before.
func checkUndiffableObjects(migrationsDir, schemaFile string) error {
	data, err := os.ReadFile(schemaFile) //nolint:gosec // schemaFile is a program parameter (constant in production, temp-dir path in tests), never external input
	if err != nil {
		return fmt.Errorf("read %s: %w", schemaFile, err)
	}
	stripped := stripSQLComments(string(data))

	var pending []undiffableDecl
	for _, stmt := range splitStatements(stripped) {
		if class, name, ok := matchAlterUndiffableObject(stmt); ok {
			return unconditionalRejectError(schemaFile, class, name)
		}
		m := undiffableObjectPattern.FindStringSubmatch(stmt)
		if m == nil {
			continue
		}
		class := strings.ToUpper(m[1])
		if class != "FUNCTION" && class != "TRIGGER" {
			return unconditionalRejectError(schemaFile, class, m[2])
		}
		pending = append(pending, undiffableDecl{class: class, statement: stmt})
	}

	return crossCheckFunctionsAndTriggers(migrationsDir, schemaFile, pending)
}

// gooseUpSection returns only the text between a goose migration file's
// "-- +goose Up" marker and its "-- +goose Down" marker (or the end of the
// file, if there is no Down section) — B2: the D9 cross-check and its
// DROP FUNCTION/TRIGGER guard never scan anything else. A file with no Up
// marker at all (not a goose migration this tool recognizes) contributes
// nothing.
func gooseUpSection(content string) string {
	upLoc := gooseUpMarker.FindStringIndex(content)
	if upLoc == nil {
		return ""
	}
	rest := content[upLoc[1]:]
	if downLoc := gooseDownMarker.FindStringIndex(rest); downLoc != nil {
		return rest[:downLoc[0]]
	}
	return rest
}

// readMigrationsUp returns the concatenation, in filename (chronological
// sequence-number) order, of every *.sql file's "-- +goose Up" section
// under migrationsDir — the only text the D9 cross-check (and its
// DROP FUNCTION/TRIGGER guard) ever scans (B2). A missing migrationsDir
// (no migrations generated yet) is not an error: it contributes no text,
// same as an empty directory.
func readMigrationsUp(migrationsDir string) (string, error) {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read %s: %w", migrationsDir, err)
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var combined strings.Builder
	for _, name := range names {
		data, readErr := os.ReadFile(filepath.Join(migrationsDir, name)) //nolint:gosec // name comes from os.ReadDir(migrationsDir), never external input
		if readErr != nil {
			return "", fmt.Errorf("read %s: %w", name, readErr)
		}
		combined.WriteString(gooseUpSection(string(data)))
		combined.WriteString("\n")
	}
	return combined.String(), nil
}

// migrationUndiffableDecl is one CREATE FUNCTION/TRIGGER statement found
// in migrationsDir's hand-written "-- +goose Up" sections, keyed by its
// composite identity (see undiffableObjectIdentity) in
// crossCheckFunctionsAndTriggers' migrationByKey map.
type migrationUndiffableDecl struct {
	class      string // "FUNCTION" or "TRIGGER"
	name       string // canonical bare name, for error messages
	detail     string // "ON table" or the normalized argument list, for error messages
	normalized string // last occurrence (file + in-file order) wins
}

// crossCheckFunctionsAndTriggers is the D9 cross-check: every FUNCTION or
// TRIGGER checkUndiffableObjects finds declared in schemaFile (declared)
// must have a matching hand-written migration under migrationsDir —
// Atlas's differ can never generate one (see the package doc comment) —
// and that migration's own declaration must be byte-for-byte the SAME
// statement after B3 normalization, not merely the same object identity,
// or a hand-edited schema.sql and a stale migration could silently
// disagree about the function/trigger's actual body while this gate
// reports clean. Conversely, a migration that hand-writes a
// FUNCTION/TRIGGER schema.sql never declares is also rejected, keeping
// the two in sync in both directions. Identity is composite, never a bare
// name (see undiffableObjectIdentity — regression for review finding
// Important 1): a TRIGGER's identity is its name plus its target table,
// and a FUNCTION's is its name plus its argument list, since Postgres
// itself scopes both that way and a bare-name key let one same-named
// object silently stand in for a different one.
//
// Only migrationsDir's "-- +goose Up" sections are ever scanned (B2 —
// see readMigrationsUp): a migration's own "-- +goose Down" section
// legitimately rolls a FUNCTION/TRIGGER back with its own DROP
// statements, which must never be misread as declaring — or illegally
// dropping — anything the Up-side check cares about. Within that Up text,
// both a DROP FUNCTION/TRIGGER and any ALTER that mutates one (the B4
// addendum — regression for review finding Important 3) are rejected
// unconditionally: a hand-written migration can retarget a function body
// or silently disable a trigger exactly as invisibly to Atlas's differ as
// schema.sql declaring the ALTER directly.
func crossCheckFunctionsAndTriggers(migrationsDir, schemaFile string, declared []undiffableDecl) error {
	upRaw, err := readMigrationsUp(migrationsDir)
	if err != nil {
		return err
	}
	upStripped := stripSQLComments(upRaw)
	migrationsUpLocation := fmt.Sprintf("a hand-written migration's \"-- +goose Up\" section under %s", migrationsDir)

	if m := dropFunctionOrTriggerPattern.FindStringSubmatch(upStripped); m != nil {
		return fmt.Errorf(
			"a hand-written migration's \"-- +goose Up\" section under %s drops a %s (%q): the D9 "+
				"cross-check for %s never allows DROP FUNCTION/TRIGGER there — only the matching "+
				"\"-- +goose Down\" section may roll the object back",
			migrationsDir, strings.ToLower(m[1]), m[2], schemaFile,
		)
	}

	migrationByKey := map[string]migrationUndiffableDecl{}
	for _, stmt := range splitStatements(upStripped) {
		if class, name, ok := matchAlterUndiffableObject(stmt); ok {
			return unconditionalRejectError(migrationsUpLocation, class, name)
		}
		m := undiffableObjectPattern.FindStringSubmatch(stmt)
		if m == nil {
			continue
		}
		class := strings.ToUpper(m[1])
		if class != "FUNCTION" && class != "TRIGGER" {
			// Migrations may freely hand-write any other undiffable class
			// (e.g. a VIEW); only FUNCTION/TRIGGER are ever cross-checked.
			continue
		}
		key, name, detail, idErr := undiffableObjectIdentity(class, stmt)
		if idErr != nil {
			return idErr
		}
		_, normalized, normErr := normalizeUndiffableStatement(stmt)
		if normErr != nil {
			return normErr
		}
		migrationByKey[key] = migrationUndiffableDecl{class: class, name: name, detail: detail, normalized: normalized}
	}

	declaredKeys := map[string]bool{}
	for _, d := range declared {
		key, name, detail, idErr := undiffableObjectIdentity(d.class, d.statement)
		if idErr != nil {
			return idErr
		}
		_, schemaNormalized, normErr := normalizeUndiffableStatement(d.statement)
		if normErr != nil {
			return normErr
		}
		declaredKeys[key] = true

		mig, ok := migrationByKey[key]
		if !ok {
			return fmt.Errorf(
				"%s declares %s %q (%s) with no matching hand-written migration under %s: Atlas's differ "+
					"can never generate one for a %s (see the package doc comment) — hand-write a migration "+
					"whose \"-- +goose Up\" section creates it with the identical statement",
				schemaFile, strings.ToLower(d.class), name, detail, migrationsDir, strings.ToLower(d.class),
			)
		}
		if mig.normalized != schemaNormalized {
			return fmt.Errorf(
				"%s's declaration of %s %q (%s) does not match the last hand-written migration under %s "+
					"that creates it: the two have drifted — keep sql/schema.sql and the migration's "+
					"\"-- +goose Up\" CREATE statement identical (see the D9 cross-check)",
				schemaFile, strings.ToLower(d.class), name, detail, migrationsDir,
			)
		}
	}

	var orphanKeys []string
	for key := range migrationByKey {
		if !declaredKeys[key] {
			orphanKeys = append(orphanKeys, key)
		}
	}
	if len(orphanKeys) > 0 {
		sort.Strings(orphanKeys)
		mig := migrationByKey[orphanKeys[0]]
		return fmt.Errorf(
			"a hand-written migration under %s declares %s %q (%s) with no matching declaration in %s: "+
				"the D9 cross-check requires sql/schema.sql to declare every function/trigger a migration "+
				"hand-writes, so the two never drift silently",
			migrationsDir, strings.ToLower(mig.class), mig.name, mig.detail, schemaFile,
		)
	}

	return nil
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
