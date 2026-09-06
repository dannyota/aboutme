package migrations

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestProtectedMigrationSources(t *testing.T) {
	valid := `-- +goose Up
-- leading comment
SELECT public.runtime_begin_migration_write('migration-00014');
CREATE FUNCTION public.probe() RETURNS text LANGUAGE sql AS $body$
  SELECT '; runtime_finish_write(); -- +goose Down'::text
$body$;
SELECT public.runtime_finish_write();
-- +goose Down
SELECT 'inert';`
	tests := []struct {
		name  string
		files fstest.MapFS
		ok    bool
	}{
		{"valid quoted body", framingFS("00014_probe.sql", valid), true},
		{"valid later version", framingFS("12345_probe.sql", strings.Replace(valid, "00014", "12345", 1)), true},
		{"missing begin", framingFS("00014_probe.sql", strings.Replace(valid, "SELECT public.runtime_begin_migration_write('migration-00014');", "SELECT 1;", 1)), false},
		{"mismatched begin", framingFS("00014_probe.sql", strings.Replace(valid, "migration-00014", "migration-00015", 1)), false},
		{"code before begin", framingFS("00014_probe.sql", strings.Replace(valid, "SELECT public.runtime_begin", "SELECT 1; SELECT public.runtime_begin", 1)), false},
		{"code after finish", framingFS("00014_probe.sql", strings.Replace(valid, "SELECT public.runtime_finish_write();\n-- +goose Down", "SELECT public.runtime_finish_write(); SELECT 2;\n-- +goose Down", 1)), false},
		{"finish only in quoted text", framingFS("00014_probe.sql", strings.Replace(valid, "SELECT public.runtime_finish_write();\n-- +goose Down", "SELECT 'runtime_finish_write();';\n-- +goose Down", 1)), false},
		{"no transaction", framingFS("00014_probe.sql", "-- +goose NO TRANSACTION\n"+valid), false},
		{"lowercase no transaction", framingFS("00014_probe.sql", valid+"\n-- +goose no transaction\n"), false},
		{"quoted-line no transaction", framingFS("00014_probe.sql", strings.Replace(valid, "  SELECT ';", "-- +goose NO TRANSACTION\n  SELECT ';", 1)), false},
		{"environment substitution", framingFS("00014_probe.sql", strings.Replace(valid, "-- +goose Up", "-- +goose Up\n-- +goose ENVSUB ON", 1)), false},
		{"escaped E string", framingFS("00014_probe.sql", strings.Replace(valid, "CREATE FUNCTION", `SELECT E'escaped \'; finish text';`+"\nCREATE FUNCTION", 1)), true},
		{"go migration", framingFS("00014_probe.go", "package migrations"), false},
		{"duplicate version", fstest.MapFS{"00014_one.sql": {Data: []byte(valid)}, "00014_two.sql": {Data: []byte(valid)}}, false},
		{"overflow version", framingFS("999999999999999999999_probe.sql", valid), false},
		{"malformed numeric name", framingFS("14probe.sql", valid), false},
		{"pre-protection ignored", framingFS("00013_legacy.go", "package migrations"), true},
	}
	for _, control := range []string{"COMMIT", "END", "ROLLBACK", "ABORT", "PREPARE TRANSACTION 'x'", "START TRANSACTION", "BEGIN"} {
		tests = append(tests, struct {
			name  string
			files fstest.MapFS
			ok    bool
		}{"outer " + control, framingFS("00014_probe.sql", strings.Replace(valid, "CREATE FUNCTION", control+";\nCREATE FUNCTION", 1)), false})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateProtectedMigrationSources(tt.files)
			if (err == nil) != tt.ok {
				t.Fatalf("error=%v ok=%t", err, tt.ok)
			}
		})
	}
}

func TestProtectedMigrationSourcesEmbedded(t *testing.T) {
	if err := validateProtectedMigrationSources(FS); err != nil {
		t.Fatal(err)
	}
}

func TestGooseMigrationOrdering(t *testing.T) {
	dir := gooseModuleDir(t)
	provider := readGooseSource(t, dir, "provider_run.go")
	assertSourceOrder(t, provider,
		"conn, err := p.db.Conn(ctx)",
		"l.SessionLock(ctx, conn)",
		"p.ensureVersionTable(ctx, conn)",
	)
	assertSourceOrder(t, provider,
		"return beginTx(ctx, conn, func(tx *sql.Tx) error {",
		"p.runMigration(ctx, tx, m, direction)",
		"p.maybeInsertOrDelete(ctx, tx, m.Version, direction)",
	)
	beginTxStart := strings.Index(provider, "func beginTx(")
	if beginTxStart < 0 {
		t.Fatal("pinned Goose source lacks beginTx")
	}
	beginTx := provider[beginTxStart:]
	assertSourceOrder(t, beginTx,
		"tx, err := conn.BeginTx(ctx, nil)",
		"if err := fn(tx); err != nil",
		"return tx.Commit()",
	)
	postgresStore := readGooseSource(t, dir, "internal/dialects/postgres.go")
	if !strings.Contains(postgresStore, `INSERT INTO %s (version_id, is_applied) VALUES ($1, $2)`) {
		t.Fatal("pinned Goose PostgreSQL history INSERT changed")
	}
}

func framingFS(name, contents string) fstest.MapFS {
	return fstest.MapFS{name: {Data: []byte(contents)}}
}

func gooseModuleDir(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "go", "list", "-m", "-f", "{{.Dir}} {{.Version}}", "github.com/pressly/goose/v3").Output()
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 || fields[1] != "v3.27.3" {
		t.Fatalf("Goose module=%q", strings.TrimSpace(string(out)))
	}
	return fields[0]
}

func readGooseSource(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func assertSourceOrder(t *testing.T, source string, terms ...string) {
	t.Helper()
	position := -1
	for _, term := range terms {
		next := strings.Index(source[position+1:], term)
		if next < 0 {
			t.Fatalf("pinned Goose source lacks %q after byte %d", term, position)
		}
		position += next + 1
	}
}
