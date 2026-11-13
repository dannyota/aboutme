package migrations_test

import (
	"path/filepath"
	"testing"

	"danny.vn/aboutme/apps/server/migrations"
)

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
