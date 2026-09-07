package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// headMigrationVersion returns the highest embedded migration version and
// asserts the set is contiguous from one, so version-relative expectations
// below survive a new migration while still catching enumeration breakage.
func headMigrationVersion(t *testing.T) int64 {
	t.Helper()
	sources, err := migrationSourcesFromFS(FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) == 0 {
		t.Fatal("no embedded migration sources")
	}
	for i, source := range sources {
		if source.Version != int64(i+1) {
			t.Fatalf("migration versions are not contiguous from one: index %d is version %d", i, source.Version)
		}
	}
	return sources[len(sources)-1].Version
}

func TestTemplateDatabaseNameIsVersionedAndSourceBound(t *testing.T) {
	head, err := TemplateDatabaseName(FS, 0)
	if err != nil {
		t.Fatal(err)
	}
	again, err := TemplateDatabaseName(FS, 0)
	if err != nil || again != head {
		t.Fatalf("head=%s again=%s error=%v", head, again, err)
	}
	headVersion := headMigrationVersion(t)
	headPrefix := fmt.Sprintf("aboutme_migrate_template_%d_", headVersion)
	if !templateDatabasePattern.MatchString(head) || !localMigrationDatabasePattern.MatchString(head) || !strings.HasPrefix(head, headPrefix) {
		t.Fatalf("head name %q want prefix %q", head, headPrefix)
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
	if _, err = TemplateDatabaseName(fstest.MapFS{}, 0); err == nil {
		t.Fatal("empty source set produced a template name")
	}
}

func TestThroughFSHidesHigherVersionsFromGlobAndOpen(t *testing.T) {
	limited := throughFS{FS: FS, through: 19}
	names, err := fs.Glob(limited, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 19 || names[len(names)-1] != "00019_runtime_replica_registration.sql" {
		t.Fatalf("glob through 19 returned %d names ending in %q", len(names), names[len(names)-1])
	}
	if _, err = fs.ReadFile(limited, "00020_runtime_shared_claim_operations.sql"); err == nil {
		t.Fatal("version 20 is readable through a version 19 view")
	}
	if _, err = fs.ReadFile(limited, "00019_runtime_replica_registration.sql"); err != nil {
		t.Fatalf("version 19 is not readable through a version 19 view: %v", err)
	}
	sources, err := migrationSourcesFromFS(limited)
	if err != nil || len(sources) != 19 || sources[len(sources)-1].Version != 19 {
		t.Fatalf("sources through 19 count=%d error=%v", len(sources), err)
	}
	unlimited := throughFS{FS: FS}
	names, err = fs.Glob(unlimited, "*.sql")
	if want := int(headMigrationVersion(t)); err != nil || len(names) != want {
		t.Fatalf("unbounded view count=%d want=%d error=%v", len(names), want, err)
	}
}

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

func disposableCloneName() string {
	return fmt.Sprintf("aboutme_migrate_test_%d_%d", time.Now().UnixNano(), compositionDatabaseCounter.Add(1))
}

func TestEnsureTemplateDatabaseBuildsOnceAndClonesProvisioned(t *testing.T) {
	base := templateAdminDSN(t)
	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := admin.Close(); closeErr != nil {
			t.Errorf("close template admin: %v", closeErr)
		}
	})
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
		if _, dropErr := DropStaleTemplateDatabases(cleanup, base, expectedTemplateNames(t)); dropErr != nil {
			t.Errorf("drop templates: %v", dropErr)
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
	clone := disposableCloneName()
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if _, dropErr := admin.ExecContext(cleanup, `DROP DATABASE IF EXISTS `+clone+` WITH (FORCE)`); dropErr != nil {
			t.Errorf("drop clone: %v", dropErr)
		}
	})
	if err = CloneTemplateDatabase(ctx, base, name, clone); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + clone
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("close clone: %v", closeErr)
		}
	})
	head := headMigrationVersion(t)
	statuses, err := Status(ctx, db, LocalAdminMigratorIdentity())
	if want := int(head - 19); err != nil || PendingCount(statuses) != want {
		t.Fatalf("clone pending=%d want=%d error=%v", PendingCount(statuses), want, err)
	}
	if err = ProvisionDatabase(ctx, db); err != nil {
		t.Fatalf("clone provisioning is not idempotent: %v", err)
	}
	var generation int64
	var registration bool
	if err = db.QueryRowContext(ctx, `SELECT generation,to_regprocedure('public.runtime_register_serving_replica(uuid,text,text,text,text,text,text)') IS NOT NULL FROM public.runtime_write_state WHERE singleton`).Scan(&generation, &registration); err != nil || !registration {
		t.Fatalf("clone state generation=%d registration=%t error=%v", generation, registration, err)
	}
	if _, err = applyFS(ctx, db, runtimeTransitionFixtureFS(t, head), LocalAdminMigratorIdentity()); err != nil {
		t.Fatalf("upgrade clone: %v", err)
	}
	statuses, err = Status(ctx, db, LocalAdminMigratorIdentity())
	if err != nil || PendingCount(statuses) != 0 {
		t.Fatalf("clone after upgrade pending=%d error=%v", PendingCount(statuses), err)
	}
	if _, err = admin.ExecContext(ctx, `ALTER DATABASE `+name+` IS_TEMPLATE false`); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := EnsureTemplateDatabase(ctx, base, fixture, 19)
	if err != nil || rebuilt != name {
		t.Fatalf("rebuild name=%s error=%v", rebuilt, err)
	}
	if _, isTemplate, _ := templateRow(t, admin, name); !isTemplate {
		t.Fatal("half-built template was not rebuilt")
	}
	// A clone from a missing template must fail on the CREATE DATABASE, so
	// the clone name itself has to be inside the disposable class.
	missingClone := disposableCloneName()
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if _, dropErr := admin.ExecContext(cleanup, `DROP DATABASE IF EXISTS `+missingClone+` WITH (FORCE)`); dropErr != nil {
			t.Errorf("drop missing-template clone: %v", dropErr)
		}
	})
	if err = CloneTemplateDatabase(ctx, base, "aboutme_migrate_template_19_0000000000000000", missingClone); !errors.Is(err, ErrTemplateMissing) {
		t.Fatalf("missing template error=%v", err)
	}
	if exists, _, _ := templateRow(t, admin, missingClone); exists {
		t.Fatalf("clone %s was created from a missing template", missingClone)
	}
	if err = CloneTemplateDatabase(ctx, base, name, "aboutme_prod"); err == nil {
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
		if _, dropErr := DropStaleTemplateDatabases(cleanup, base, expectedTemplateNames(t)); dropErr != nil {
			t.Errorf("drop templates: %v", dropErr)
		}
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

func TestDropStaleTemplateDatabasesSkipsANameLockedByAnotherSession(t *testing.T) {
	base := templateAdminDSN(t)
	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := admin.Close(); closeErr != nil {
			t.Errorf("close template admin: %v", closeErr)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Two cheap fixture "templates" outside the real template set: their
	// content does not matter to DropStaleTemplateDatabases, only that
	// their names satisfy templateDatabasePattern.
	stamp := time.Now().UnixNano()
	locked := fmt.Sprintf("aboutme_migrate_template_1_%016x", stamp)
	unlocked := fmt.Sprintf("aboutme_migrate_template_1_%016x", stamp+1)
	for _, name := range []string{locked, unlocked} {
		if _, createErr := admin.ExecContext(ctx, `CREATE DATABASE `+name); createErr != nil {
			t.Fatal(createErr)
		}
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if _, dropErr := admin.ExecContext(cleanup, `DROP DATABASE IF EXISTS `+locked+` WITH (FORCE)`); dropErr != nil {
			t.Errorf("drop locked fixture: %v", dropErr)
		}
	})

	// A second session holds the exact advisory lock EnsureTemplateDatabase
	// and DropStaleTemplateDatabases use for `locked`, simulating a build
	// or clone in flight in another process.
	holder, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := holder.Close(); closeErr != nil {
			t.Errorf("close lock holder: %v", closeErr)
		}
	})
	holderConn, err := holder.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := holderConn.Close(); closeErr != nil {
			t.Errorf("close lock holder conn: %v", closeErr)
		}
	})
	if _, lockErr := holderConn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtext($1)::bigint)`, locked); lockErr != nil {
		t.Fatal(lockErr)
	}
	t.Cleanup(func() {
		if _, unlockErr := holderConn.ExecContext(context.Background(), `SELECT pg_advisory_unlock(hashtext($1)::bigint)`, locked); unlockErr != nil {
			t.Errorf("unlock fixture: %v", unlockErr)
		}
	})

	dropped, err := DropStaleTemplateDatabases(ctx, base, expectedTemplateNames(t))
	if err != nil {
		t.Fatal(err)
	}
	var sawLocked, sawUnlocked bool
	for _, name := range dropped {
		if name == locked {
			sawLocked = true
		}
		if name == unlocked {
			sawUnlocked = true
		}
	}
	if sawLocked {
		t.Fatalf("locked template %s was dropped while its lock was held", locked)
	}
	if !sawUnlocked {
		t.Fatalf("unlocked template %s was not dropped: dropped=%v", unlocked, dropped)
	}
	if exists, _, _ := templateRow(t, admin, locked); !exists {
		t.Fatalf("locked template %s was removed despite the held lock", locked)
	}
	if exists, _, _ := templateRow(t, admin, unlocked); exists {
		t.Fatalf("unlocked template %s still exists", unlocked)
	}
}

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
	if want := int(headMigrationVersion(t) - 19); err != nil || PendingCount(statuses) != want {
		t.Fatalf("version 19 pending=%d want=%d error=%v", PendingCount(statuses), want, err)
	}
}
