// Hermetic unit tests for the migrations package: no database required, so
// these run as part of the default `go test ./...`. The database-backed
// harness tests (empty->head, previous-release->head, concurrent runners,
// partial-failure recovery) live in harness_test.go, gated behind
// TEST_DATABASE_URL like internal/store's integration test.
package migrations_test

import (
	"path/filepath"
	"testing"

	"github.com/dannyota/aboutme/apps/server/migrations"
)

// TestFS_EmbedsAtLeastOneMigration guards the go:embed directive itself:
// "//go:embed *.sql" with zero matching files is a compile error, so this
// can only fail if someone changes the embed pattern to something looser
// (e.g. a glob that could legitimately match nothing) or generates the
// migrations directory incorrectly at build time.
func TestFS_EmbedsAtLeastOneMigration(t *testing.T) {
	t.Parallel()

	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir(.) error: %v", err)
	}

	var sqlFiles []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".sql" {
			sqlFiles = append(sqlFiles, e.Name())
		}
	}
	if len(sqlFiles) == 0 {
		t.Fatal("migrations.FS contains no .sql files")
	}
}

// TestFS_EmbedsOnlySQLFiles guards the embed pattern's narrowness (see
// migrations.FS's doc comment): this directory also holds the package's own
// Go sources and test files, so a pattern any looser than "*.sql" would
// bake non-migration files into the shipped server binary. Asserting on the
// whole embedded set, rather than probing one known filename, keeps the
// guard meaningful as the directory's non-SQL contents change.
func TestFS_EmbedsOnlySQLFiles(t *testing.T) {
	t.Parallel()

	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir(.) error: %v", err)
	}

	for _, e := range entries {
		if e.IsDir() {
			t.Errorf("migrations.FS embeds directory %q, but only *.sql files should be embedded", e.Name())
			continue
		}
		if filepath.Ext(e.Name()) != ".sql" {
			t.Errorf("migrations.FS embeds %q, but only *.sql files should be embedded", e.Name())
		}
	}
}

func TestNewProvider_NilDatabaseIsRejected(t *testing.T) {
	t.Parallel()

	_, err := migrations.NewProvider(nil, migrations.FS)
	if err == nil {
		t.Fatal("NewProvider(nil, ...) error = nil, want error")
	}
}
