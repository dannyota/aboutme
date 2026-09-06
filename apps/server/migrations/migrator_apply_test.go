//nolint:govet // Sequential migration cleanup keeps each error next to its operation.
package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
)

func TestApplyFreshStopsAt13ThenReentersFor14(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ProvisionDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
	results, err := Apply(ctx, db, LocalAdminMigratorIdentity())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 14 {
		t.Fatalf("applied %d migrations, want 14", len(results))
	}
	var head, generation int64
	var historyOwner, recordedOwner string
	var enforcement int16
	if err := db.QueryRowContext(ctx, `SELECT (SELECT max(version_id) FILTER (WHERE is_applied) FROM public.goose_db_version),s.generation,pg_get_userbyid(c.relowner),s.migration_history_owner,s.migrator_enforcement_version FROM public.runtime_write_state s CROSS JOIN pg_class c WHERE s.singleton AND c.oid='public.goose_db_version'::regclass`).Scan(&head, &generation, &historyOwner, &recordedOwner, &enforcement); err != nil {
		t.Fatal(err)
	}
	if head != 14 || generation != 2 || historyOwner != "aboutme_runtime_owner" || recordedOwner != historyOwner || enforcement != 1 {
		t.Fatalf("head=%d generation=%d owner=%q recorded=%q enforcement=%d", head, generation, historyOwner, recordedOwner, enforcement)
	}
	second, err := Apply(ctx, db, LocalAdminMigratorIdentity())
	if err != nil || len(second) != 0 {
		t.Fatalf("repeat Apply results=%d error=%v", len(second), err)
	}
}

func TestApplyExistingAboutme13RequiresExplicitAdoption(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	provider, err := NewProvider(db, FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 13); err != nil {
		t.Fatal(err)
	}
	var beforeGeneration int64
	if err := db.QueryRowContext(ctx, `SELECT generation FROM public.runtime_write_state WHERE singleton`).Scan(&beforeGeneration); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, db, LocalAdminMigratorIdentity()); !errors.Is(err, ErrHistoryAdoptionRequired) {
		t.Fatalf("Apply error=%v", err)
	}
	firstAdoption, err := AdoptHistoryOwner(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if firstAdoption.Outcome() != Applied || firstAdoption.AmbiguousCause() != nil {
		t.Fatalf("first adoption outcome=%v cause=%v", firstAdoption.Outcome(), firstAdoption.AmbiguousCause())
	}
	database := currentCompositionDatabase(ctx, t, db)
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`GRANT CREATE ON DATABASE %s TO aboutme_runtime_owner`, database)); err != nil {
		t.Fatal(err)
	}
	if _, err := AdoptHistoryOwner(ctx, db); !errors.Is(err, ErrMigrationProvisioningDrift) {
		t.Fatalf("already-converged adoption broader grant error=%v", err)
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`REVOKE CREATE ON DATABASE %s FROM aboutme_runtime_owner`, database)); err != nil {
		t.Fatal(err)
	}
	secondAdoption, err := AdoptHistoryOwner(ctx, db)
	if err != nil {
		t.Fatalf("idempotent adoption: %v", err)
	}
	if secondAdoption.Outcome() != AlreadyConverged || secondAdoption.AmbiguousCause() != nil {
		t.Fatalf("second adoption outcome=%v cause=%v", secondAdoption.Outcome(), secondAdoption.AmbiguousCause())
	}
	var adoptedGeneration int64
	if err := db.QueryRowContext(ctx, `SELECT generation FROM public.runtime_write_state WHERE singleton`).Scan(&adoptedGeneration); err != nil {
		t.Fatal(err)
	}
	if adoptedGeneration != beforeGeneration+1 {
		t.Fatalf("adoption generation=%d want %d", adoptedGeneration, beforeGeneration+1)
	}
	if _, err := Apply(ctx, db, LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	if _, err := AdoptHistoryOwner(ctx, db); !errors.Is(err, ErrMigrationHistoryCorrupt) {
		t.Fatalf("enforced history adoption error=%v", err)
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`GRANT CREATE ON DATABASE %s TO aboutme_runtime_owner`, database)); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, db, LocalAdminMigratorIdentity()); !errors.Is(err, ErrMigrationProvisioningDrift) {
		t.Fatalf("Apply broader provisioning grant error=%v", err)
	}
}

func TestStatusFreshIsReadOnly(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := Status(ctx, db, LocalAdminMigratorIdentity()); !errors.Is(err, ErrMigrationHistoryMissing) {
		t.Fatalf("Status error=%v", err)
	}
	var history bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('public.goose_db_version') IS NOT NULL`).Scan(&history); err != nil {
		t.Fatal(err)
	}
	if history {
		t.Fatal("Status created migration history")
	}
}

func TestApplyConcurrentBootstrapRechecksAfterGooseLock(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ProvisionDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		count int
		err   error
	}
	start := make(chan struct{})
	outcomes := make(chan outcome, 2)
	for range 2 {
		go func() {
			<-start
			results, err := Apply(ctx, db, LocalAdminMigratorIdentity())
			outcomes <- outcome{count: len(results), err: err}
		}()
	}
	close(start)
	total := 0
	for range 2 {
		result := <-outcomes
		if result.err != nil {
			t.Fatalf("concurrent Apply: %v", result.err)
		}
		total += result.count
	}
	if total != 14 {
		t.Fatalf("concurrent Apply total=%d want 14", total)
	}
}

func TestApplyMismatchFailsWithoutStateMutation(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ProvisionDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, db, LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE public.users OWNER TO aboutme`); err != nil {
		t.Fatal(err)
	}
	var before int64
	if err := db.QueryRowContext(ctx, `SELECT generation FROM public.runtime_write_state WHERE singleton`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, db, LocalAdminMigratorIdentity()); err == nil {
		t.Fatal("mixed-owner Apply succeeded")
	}
	var after int64
	if err := db.QueryRowContext(ctx, `SELECT generation FROM public.runtime_write_state WHERE singleton`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("failed Apply changed generation %d -> %d", before, after)
	}
}

func TestPreFoundationHistoryPrefixCorruptionNeverMutates(t *testing.T) {
	for name, corruption := range map[string]string{
		"gap":         `DELETE FROM public.goose_db_version WHERE version_id=3`,
		"duplicate":   `INSERT INTO public.goose_db_version(version_id,is_applied) VALUES(3,true)`,
		"down":        `UPDATE public.goose_db_version SET is_applied=false WHERE version_id=3`,
		"unknown":     `INSERT INTO public.goose_db_version(version_id,is_applied) VALUES(99,true)`,
		"default":     `ALTER TABLE public.goose_db_version ALTER COLUMN tstamp DROP DEFAULT`,
		"primary-key": `ALTER TABLE public.goose_db_version DROP CONSTRAINT goose_db_version_pkey`,
		"table-acl":   `GRANT SELECT ON public.goose_db_version TO aboutme_app`,
		"column-acl":  `GRANT SELECT(version_id) ON public.goose_db_version TO aboutme_app`,
		"trigger":     `CREATE TRIGGER hostile_history_trigger BEFORE INSERT ON public.goose_db_version FOR EACH ROW EXECUTE FUNCTION public.enforce_resume_cap()`,
	} {
		t.Run(name, func(t *testing.T) {
			db := newCompositionTestDatabase(t)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			provider, err := NewProvider(db, FS)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := provider.UpTo(ctx, 5); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(ctx, corruption); err != nil {
				t.Fatal(err)
			}
			var rowsBefore int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM public.goose_db_version`).Scan(&rowsBefore); err != nil {
				t.Fatal(err)
			}
			if _, err := Status(ctx, db, LocalAdminMigratorIdentity()); !errors.Is(err, ErrMigrationHistoryCorrupt) {
				t.Fatalf("Status error=%v", err)
			}
			if _, err := Apply(ctx, db, LocalAdminMigratorIdentity()); !errors.Is(err, ErrMigrationHistoryCorrupt) {
				t.Fatalf("Apply error=%v", err)
			}
			var rowsAfter int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM public.goose_db_version`).Scan(&rowsAfter); err != nil {
				t.Fatal(err)
			}
			if rowsAfter != rowsBefore {
				t.Fatalf("history rows changed %d -> %d", rowsBefore, rowsAfter)
			}
		})
	}
}

func TestApplyAcceptsOnlyExactCleanConvergenceErrors(t *testing.T) {
	token := errors.New("handoff")
	bootstrap := &bootstrapSessionLocker{handoffToken: token, completedClean: true}
	protected := &runtimeSessionLocker{completedClean: true}
	if !cleanBootstrapHandoff(fmt.Errorf("initialize: %w", token), bootstrap) {
		t.Fatal("exact clean handoff rejected")
	}
	if !cleanBootstrapAlreadyApplied(fmt.Errorf("initialize: %w", goose.ErrAlreadyApplied), bootstrap) ||
		!cleanProtectedAlreadyApplied(fmt.Errorf("initialize: %w", goose.ErrAlreadyApplied), protected) {
		t.Fatal("exact clean AlreadyApplied rejected")
	}
	for name, err := range map[string]error{
		"cleanup-sibling": errors.Join(token, errors.New("cleanup")),
		"nested":          fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", token)),
		"foreign":         fmt.Errorf("initialize: %w", errors.New("other handoff")),
	} {
		if cleanBootstrapHandoff(err, bootstrap) {
			t.Errorf("%s handoff accepted", name)
		}
	}
	bootstrap.completedClean = false
	protected.completedClean = false
	if cleanBootstrapAlreadyApplied(fmt.Errorf("initialize: %w", goose.ErrAlreadyApplied), bootstrap) ||
		cleanProtectedAlreadyApplied(fmt.Errorf("initialize: %w", goose.ErrAlreadyApplied), protected) {
		t.Fatal("AlreadyApplied with failed cleanup accepted")
	}
}

func TestAdoptConcurrentLoserIsAlreadyConverged(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	provider, err := NewProvider(db, FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 13); err != nil {
		t.Fatal(err)
	}
	var before int64
	if err := db.QueryRowContext(ctx, `SELECT generation FROM public.runtime_write_state WHERE singleton`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	type answer struct {
		result AdoptionResult
		err    error
	}
	answers := make(chan answer, 2)
	for range 2 {
		go func() {
			result, err := AdoptHistoryOwner(ctx, db)
			answers <- answer{result: result, err: err}
		}()
	}
	outcomes := map[AdoptionOutcome]int{}
	for range 2 {
		answer := <-answers
		if answer.err != nil {
			t.Fatal(answer.err)
		}
		outcomes[answer.result.Outcome()]++
	}
	if outcomes[Applied] != 1 || outcomes[AlreadyConverged] != 1 {
		t.Fatalf("outcomes=%v", outcomes)
	}
	var after int64
	if err := db.QueryRowContext(ctx, `SELECT generation FROM public.runtime_write_state WHERE singleton`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before+1 {
		t.Fatalf("generation=%d want %d", after, before+1)
	}
}

func TestAdoptionReconciliationRequiresExactSnapshotAndAllowsLaterWriters(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	provider, err := NewProvider(db, FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 13); err != nil {
		t.Fatal(err)
	}
	before, err := validateVersion13Manifest(ctx, db, "aboutme")
	if err != nil {
		t.Fatal(err)
	}
	for i := range before.objects {
		before.objects[i].rowCount++ // Post-retirement application row changes are diagnostic only.
	}
	history, foundation, err := readAdoptionEvidence(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	var generation int64
	if err := db.QueryRowContext(ctx, `SELECT generation FROM public.runtime_write_state WHERE singleton`).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	if _, err := AdoptHistoryOwner(ctx, db); err != nil {
		t.Fatal(err)
	}
	oldConn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	oldBackend, err := captureMigrationBackend(oldConn)
	if err != nil {
		t.Fatal(err)
	}
	var oldPID int32
	if err := oldConn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&oldPID); err != nil {
		t.Fatal(err)
	}
	if err := finalizeMigrationBackend(ctx, oldBackend, oldConn, false); err != nil {
		t.Fatal(err)
	}
	if err := reconcileAdoption(ctx, db, currentCompositionDatabase(ctx, t, db), oldPID, generation, before, history, foundation); err != nil {
		t.Fatalf("exact generation+1 reconciliation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE public.runtime_write_state SET last_writer_kind='app',last_writer_operation_id='wrong-at-generation-one' WHERE singleton`); err != nil {
		t.Fatal(err)
	}
	if err := reconcileAdoption(ctx, db, currentCompositionDatabase(ctx, t, db), oldPID, generation, before, history, foundation); !errors.Is(err, ErrMigrationHistoryCorrupt) {
		t.Fatalf("wrong generation+1 writer reconciliation error=%v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE public.runtime_write_state SET generation=generation+1,last_writer_kind='app',last_writer_operation_id='later-write' WHERE singleton`); err != nil {
		t.Fatal(err)
	}
	if err := reconcileAdoption(ctx, db, currentCompositionDatabase(ctx, t, db), oldPID, generation, before, history, foundation); err != nil {
		t.Fatalf("later generation reconciliation: %v", err)
	}
	database := currentCompositionDatabase(ctx, t, db)
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`GRANT CREATE ON DATABASE %s TO aboutme_runtime_owner`, database)); err != nil {
		t.Fatal(err)
	}
	if err := reconcileAdoption(ctx, db, database, oldPID, generation, before, history, foundation); !errors.Is(err, ErrMigrationProvisioningDrift) {
		t.Fatalf("broader provisioning reconciliation error=%v", err)
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`REVOKE CREATE ON DATABASE %s FROM aboutme_runtime_owner`, database)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `SET ROLE aboutme_runtime_owner; GRANT UPDATE ON public.goose_db_version TO aboutme_app; RESET ROLE`); err != nil {
		t.Fatal(err)
	}
	if err := reconcileAdoption(ctx, db, currentCompositionDatabase(ctx, t, db), oldPID, generation, before, history, foundation); !errors.Is(err, ErrMigrationHistoryCorrupt) {
		t.Fatalf("changed ACL reconciliation error=%v", err)
	}
	if _, err := db.ExecContext(ctx, `SET ROLE aboutme_runtime_owner; REVOKE UPDATE ON public.goose_db_version FROM aboutme_app; GRANT SELECT(version_id) ON public.goose_db_version TO aboutme_app; RESET ROLE`); err != nil {
		t.Fatal(err)
	}
	if err := reconcileAdoption(ctx, db, currentCompositionDatabase(ctx, t, db), oldPID, generation, before, history, foundation); !errors.Is(err, ErrMigrationHistoryCorrupt) {
		t.Fatalf("changed column ACL reconciliation error=%v", err)
	}
}

func currentCompositionDatabase(ctx context.Context, t *testing.T, db *sql.DB) string {
	t.Helper()
	var database string
	if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&database); err != nil {
		t.Fatal(err)
	}
	return database
}
