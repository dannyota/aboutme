//nolint:govet // Sequential migration cleanup keeps each error next to its operation.
package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var compositionDatabaseCounter atomic.Uint64
var compositionDatabaseName = regexp.MustCompile(`^aboutme_migrate_test_[0-9]+_[0-9]+$`)

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

func loadTemplateName(t *testing.T, through int64) (string, bool) {
	t.Helper()
	raw, ok := templateNames.Load(through)
	if !ok {
		return "", false
	}
	name, ok := raw.(string)
	if !ok {
		t.Fatalf("template cache holds %T for version %d", raw, through)
	}
	return name, true
}

func ensureTestTemplate(t *testing.T, base string, through int64) string {
	t.Helper()
	if name, ok := loadTemplateName(t, through); ok {
		return name
	}
	templateBuildMu.Lock()
	defer templateBuildMu.Unlock()
	if name, ok := loadTemplateName(t, through); ok {
		return name
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
