package migrations

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

func TestApplyCommitResponseLossRetiresWithoutReconnect(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ProvisionDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, db, LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	var database string
	if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&database); err != nil {
		t.Fatal(err)
	}
	base, err := url.Parse(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	base.Path = "/" + database
	config, err := pgx.ParseConfig(base.String())
	if err != nil {
		t.Fatal(err)
	}
	fault := newMigrationWireFault(t, net.JoinHostPort(config.Host, strconv.Itoa(int(config.Port))))
	config.DialFunc = fault.dial
	faultDB := stdlib.OpenDB(*config)
	faultDB.SetMaxOpenConns(1)
	faultDB.SetMaxIdleConns(1)
	t.Cleanup(func() {
		if err := faultDB.Close(); err != nil {
			t.Errorf("close fault database: %v", err)
		}
	})
	fs15 := embeddedMigrationMap(t, map[string]string{
		"00015_response_loss.sql": "-- +goose Up\n-- +goose StatementBegin\nSELECT public.runtime_begin_migration_write('migration-00015');\nCREATE TABLE public.response_loss_probe(id integer);\nSELECT public.runtime_finish_write();\n-- +goose StatementEnd\n\n-- +goose Down\n",
	})
	if _, err := applyFS(ctx, faultDB, fs15, LocalAdminMigratorIdentity()); err == nil {
		t.Fatal("lost COMMIT response returned success")
	}
	fault.assertSocketClosedAtReturn(t)
	waitMigrationWire(t, fault.ready, "server commit ReadyForQuery")
	waitMigrationWire(t, fault.returned, "injected response fault")
	waitMigrationWire(t, fault.cancelSeen, "blocked asynchronous CancelRequest")
	waitMigrationWire(t, fault.closed, "application socket close")
	if fault.connections.Load() != fault.atFault.Load() {
		t.Fatalf("application connections=%d at fault=%d", fault.connections.Load(), fault.atFault.Load())
	}
	var head int64
	if err := db.QueryRowContext(ctx, `SELECT max(version_id) FILTER (WHERE is_applied) FROM public.goose_db_version`).Scan(&head); err != nil {
		t.Fatal(err)
	}
	if head != 15 {
		t.Fatalf("server did not commit before response loss: head=%d", head)
	}
}

func TestStatusCommitResponseLossReturnsErrorWithoutReconnect(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ProvisionDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, db, LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	var database string
	if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&database); err != nil {
		t.Fatal(err)
	}
	base, err := url.Parse(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	base.Path = "/" + database
	config, err := pgx.ParseConfig(base.String())
	if err != nil {
		t.Fatal(err)
	}
	fault := newMigrationWireFault(t, net.JoinHostPort(config.Host, strconv.Itoa(int(config.Port))))
	config.DialFunc = fault.dial
	faultDB := stdlib.OpenDB(*config)
	faultDB.SetMaxOpenConns(1)
	faultDB.SetMaxIdleConns(1)
	t.Cleanup(func() {
		if err := faultDB.Close(); err != nil {
			t.Errorf("close Status fault database: %v", err)
		}
	})
	_, statusErr := Status(ctx, faultDB, LocalAdminMigratorIdentity())
	if statusErr == nil {
		t.Fatal("lost read-only COMMIT response returned success")
	}
	fault.assertSocketClosedAtReturn(t)
	waitMigrationWire(t, fault.ready, "server Status commit ReadyForQuery")
	waitMigrationWire(t, fault.returned, "Status injected response fault")
	waitMigrationWire(t, fault.cancelSeen, "blocked Status asynchronous CancelRequest")
	waitMigrationWire(t, fault.closed, "Status application socket close")
	if fault.connections.Load() != fault.atFault.Load() {
		t.Fatalf("application connections=%d at fault=%d error=%v", fault.connections.Load(), fault.atFault.Load(), statusErr)
	}
}

func TestAdoptCommitResponseLossReconcilesAfterPhysicalRetirement(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	provider, err := NewProvider(db, FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, upErr := provider.UpTo(ctx, 13); upErr != nil {
		t.Fatal(upErr)
	}
	var database string
	var before int64
	if queryErr := db.QueryRowContext(ctx, `SELECT current_database(),generation FROM public.runtime_write_state WHERE singleton`).Scan(&database, &before); queryErr != nil {
		t.Fatal(queryErr)
	}
	base, err := url.Parse(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	base.Path = "/" + database
	config, err := pgx.ParseConfig(base.String())
	if err != nil {
		t.Fatal(err)
	}
	fault := newMigrationWireFault(t, net.JoinHostPort(config.Host, strconv.Itoa(int(config.Port))))
	config.DialFunc = fault.dial
	faultDB := stdlib.OpenDB(*config)
	t.Cleanup(func() {
		if closeErr := faultDB.Close(); closeErr != nil {
			t.Errorf("close adoption fault database: %v", closeErr)
		}
	})
	result, err := AdoptHistoryOwner(ctx, faultDB)
	if err != nil {
		t.Fatal(err)
	}
	fault.assertSocketClosedAtReturn(t)
	if result.Outcome() != ReconciledConverged || result.AmbiguousCause() == nil {
		t.Fatalf("outcome=%v cause=%v", result.Outcome(), result.AmbiguousCause())
	}
	waitMigrationWire(t, fault.ready, "adoption commit ReadyForQuery")
	waitMigrationWire(t, fault.cancelSeen, "blocked adoption CancelRequest")
	waitMigrationWire(t, fault.closed, "adoption application socket close")
	if fault.connections.Load() != 2 {
		t.Fatalf("application connections=%d, want mutation plus reconciliation", fault.connections.Load())
	}
	var after int64
	if err := db.QueryRowContext(ctx, `SELECT generation FROM public.runtime_write_state WHERE singleton`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before+1 {
		t.Fatalf("generation=%d want %d", after, before+1)
	}
}

func TestStatusRollbackResponseLossPreservesValidationAndRetires(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ProvisionDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, db, LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	var database string
	if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&database); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE public.goose_db_version DROP CONSTRAINT goose_db_version_pkey`); err != nil {
		t.Fatal(err)
	}
	base, err := url.Parse(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	base.Path = "/" + database
	config, err := pgx.ParseConfig(base.String())
	if err != nil {
		t.Fatal(err)
	}
	fault := newMigrationWireFault(t, net.JoinHostPort(config.Host, strconv.Itoa(int(config.Port))), "ROLLBACK")
	config.DialFunc = fault.dial
	faultDB := stdlib.OpenDB(*config)
	t.Cleanup(func() {
		if closeErr := faultDB.Close(); closeErr != nil {
			t.Errorf("close rollback fault database: %v", closeErr)
		}
	})
	_, statusErr := Status(ctx, faultDB, LocalAdminMigratorIdentity())
	if !errors.Is(statusErr, ErrMigrationHistoryCorrupt) {
		t.Fatalf("Status error=%v", statusErr)
	}
	fault.assertSocketClosedAtReturn(t)
	waitMigrationWire(t, fault.ready, "Status rollback ReadyForQuery")
	waitMigrationWire(t, fault.cancelSeen, "blocked rollback CancelRequest")
	waitMigrationWire(t, fault.closed, "rollback application socket close")
}

func TestApplyCatalogProbeFaultRetiresWithoutReconnect(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var database string
	if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&database); err != nil {
		t.Fatal(err)
	}
	base, err := url.Parse(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	base.Path = "/" + database
	config, err := pgx.ParseConfig(base.String())
	if err != nil {
		t.Fatal(err)
	}
	fault := newMigrationWireFault(t, net.JoinHostPort(config.Host, strconv.Itoa(int(config.Port))), "CATALOG_PROBE")
	config.DialFunc = fault.dial
	faultDB := stdlib.OpenDB(*config)
	t.Cleanup(func() {
		if closeErr := faultDB.Close(); closeErr != nil {
			t.Errorf("close catalog fault database: %v", closeErr)
		}
	})
	if _, err := Apply(ctx, faultDB, LocalAdminMigratorIdentity()); err == nil {
		t.Fatal("catalog response fault returned success")
	}
	fault.assertSocketClosedAtReturn(t)
	waitMigrationWire(t, fault.ready, "catalog probe ReadyForQuery")
	waitMigrationWire(t, fault.cancelSeen, "blocked catalog CancelRequest")
	waitMigrationWire(t, fault.closed, "catalog application socket close")
	if fault.connections.Load() != fault.atFault.Load() {
		t.Fatalf("application connections=%d at fault=%d", fault.connections.Load(), fault.atFault.Load())
	}
}

func TestAdoptUncommittedAmbiguityReconcilesCompetingWinner(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	provider, err := NewProvider(db, FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, upErr := provider.UpTo(ctx, 13); upErr != nil {
		t.Fatal(upErr)
	}
	var database string
	var before int64
	if queryErr := db.QueryRowContext(ctx, `SELECT current_database(),generation FROM public.runtime_write_state WHERE singleton`).Scan(&database, &before); queryErr != nil {
		t.Fatal(queryErr)
	}
	base, err := url.Parse(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	base.Path = "/" + database
	config, err := pgx.ParseConfig(base.String())
	if err != nil {
		t.Fatal(err)
	}
	fault := newMigrationWireFault(t, net.JoinHostPort(config.Host, strconv.Itoa(int(config.Port))))
	fault.failBeforeWrite = true
	fault.blockSecond = true
	config.DialFunc = fault.dial
	faultDB := stdlib.OpenDB(*config)
	t.Cleanup(func() {
		if closeErr := faultDB.Close(); closeErr != nil {
			t.Errorf("close competing adoption database: %v", closeErr)
		}
	})
	type adoptionAnswer struct {
		result AdoptionResult
		err    error
	}
	answer := make(chan adoptionAnswer, 1)
	go func() {
		result, adoptErr := AdoptHistoryOwner(ctx, faultDB)
		answer <- adoptionAnswer{result: result, err: adoptErr}
	}()
	waitMigrationWire(t, fault.returned, "pre-write COMMIT fault")
	waitMigrationWire(t, fault.cancelSeen, "blocked ambiguous CancelRequest")
	waitMigrationWire(t, fault.closed, "ambiguous mutation socket close")
	waitMigrationWire(t, fault.secondSeen, "bounded reconciliation connection")
	winner, err := AdoptHistoryOwner(ctx, db)
	if err != nil || winner.Outcome() != Applied {
		t.Fatalf("competing adopter result=%v error=%v", winner.Outcome(), err)
	}
	close(fault.secondGate)
	got := <-answer
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.result.Outcome() != ReconciledConverged || got.result.AmbiguousCause() == nil {
		t.Fatalf("ambiguous adopter outcome=%v cause=%v", got.result.Outcome(), got.result.AmbiguousCause())
	}
	fault.assertSocketClosedAtReturn(t)
	if fault.connections.Load() != 2 {
		t.Fatalf("fault-path connections=%d", fault.connections.Load())
	}
	var after int64
	if err := db.QueryRowContext(ctx, `SELECT generation FROM public.runtime_write_state WHERE singleton`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before+1 {
		t.Fatalf("generation=%d want %d", after, before+1)
	}
}
