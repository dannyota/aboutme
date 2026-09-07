package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dannyota/aboutme/apps/server/migrations"
)

type registrationRunnerStub struct {
	queries  *Queries
	err      error
	afterErr error
	calls    int
}

func (r *registrationRunnerStub) ExecWrite(_ context.Context, callback func(*Queries) error) error {
	r.calls++
	if r.err != nil {
		return r.err
	}
	if err := callback(r.queries); err != nil {
		return err
	}
	return r.afterErr
}

func (r *registrationRunnerStub) WithWriteTx(context.Context, pgx.TxOptions, func(*Queries) error) error {
	return errors.New("unexpected WithWriteTx")
}

type registrationDBStub struct {
	values []any
	err    error
	calls  int
	sql    string
}

func (d *registrationDBStub) Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected Exec")
}
func (d *registrationDBStub) Query(context.Context, string, ...interface{}) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}
func (d *registrationDBStub) QueryRow(_ context.Context, sql string, _ ...interface{}) pgx.Row {
	d.calls++
	d.sql = sql
	return registrationRowStub{values: d.values, err: d.err}
}

type registrationRowStub struct {
	values []any
	err    error
}

type registrationFinishResponseLostTx struct {
	pgx.Tx
	err error
}

func (tx *registrationFinishResponseLostTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	tag, err := tx.Tx.Exec(ctx, sql, arguments...)
	if err == nil && strings.Contains(sql, "runtime_finish_write") {
		return tag, tx.err
	}
	return tag, err
}

func (r registrationRowStub) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("store test: wrong scan width")
	}
	for index := range dest {
		if r.values[index] == nil {
			return errors.New("store test: required result is null")
		}
		reflect.ValueOf(dest[index]).Elem().Set(reflect.ValueOf(r.values[index]))
	}
	return nil
}

func registrationStoreIdentity() RuntimeReplicaIdentity {
	return RuntimeReplicaIdentity{ReplicaID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), InstanceID: "i-11111111111111111", ContainerInstanceARN: "arn:container:1", CaddyTaskARN: "arn:task:caddy:1", GoTaskARN: "arn:task:go:1", NuxtTaskARN: "arn:task:nuxt:1", ReleaseDigest: "sha256:" + strings.Repeat("1", 64)}
}

func registrationStoreValues(kind string, id uuid.UUID) []any {
	return []any{id, kind, "joining", int16(0), false, int16(0), false, int16(0), false, false, false, false, false, int64(2), int64(1), "bootstrap", true, "online", false}
}

func TestRuntimeReplicaRegistrationStoreFourOperationsAndResult(t *testing.T) {
	identity := registrationStoreIdentity()
	for _, test := range []struct {
		name, kind, sqlName string
		call                func(RuntimeReplicaRegistrationStore) (RuntimeReplicaResult, error)
	}{
		{"register_serving", "serving", "runtime_register_serving_replica", func(s RuntimeReplicaRegistrationStore) (RuntimeReplicaResult, error) {
			return s.RegisterServing(context.Background(), identity)
		}},
		{"register_maintenance", "maintenance", "runtime_register_maintenance_replica", func(s RuntimeReplicaRegistrationStore) (RuntimeReplicaResult, error) {
			return s.RegisterMaintenance(context.Background(), identity)
		}},
		{"ready_serving", "serving", "runtime_mark_serving_replica_join_ready", func(s RuntimeReplicaRegistrationStore) (RuntimeReplicaResult, error) {
			return s.MarkServingJoinReady(context.Background(), identity)
		}},
		{"ready_maintenance", "maintenance", "runtime_mark_maintenance_replica_join_ready", func(s RuntimeReplicaRegistrationStore) (RuntimeReplicaResult, error) {
			return s.MarkMaintenanceJoinReady(context.Background(), identity)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := &registrationDBStub{values: registrationStoreValues(test.kind, identity.ReplicaID)}
			runner := &registrationRunnerStub{queries: New(db)}
			store := &runtimeReplicaRegistrationStore{runner: runner}
			result, err := test.call(store)
			if err != nil || result.ReplicaID != identity.ReplicaID || result.ReplicaKind != test.kind || result.State != "joining" || result.CapacityGeneration != 2 || result.ControllerGeneration != 1 || !result.AdmissionEnabled || result.LifecyclePhase != "online" {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if result.DesiredReplicas != nil || result.ActiveServingReplicas != nil || result.ActiveMaintenanceReplicas != nil || result.Partition1Enabled != nil || result.Partition2Enabled != nil || runner.calls != 1 || db.calls != 1 || !strings.Contains(db.sql, test.sqlName) {
				t.Fatalf("transport result=%+v runner=%d query=%d sql=%q", result, runner.calls, db.calls, db.sql)
			}
		})
	}
}

func TestRuntimeReplicaRegistrationStoreRejectsInputsWithoutQuery(t *testing.T) {
	identity := registrationStoreIdentity()
	for _, mutate := range []func(*RuntimeReplicaIdentity){
		func(i *RuntimeReplicaIdentity) { i.ReplicaID = uuid.Nil },
		func(i *RuntimeReplicaIdentity) { i.InstanceID = "bad" },
		func(i *RuntimeReplicaIdentity) { i.ContainerInstanceARN = "" },
		func(i *RuntimeReplicaIdentity) { i.CaddyTaskARN = i.GoTaskARN },
		func(i *RuntimeReplicaIdentity) { i.NuxtTaskARN = strings.Repeat("x", 513) },
		func(i *RuntimeReplicaIdentity) { i.ReleaseDigest = "bad" },
	} {
		bad := identity
		mutate(&bad)
		runner := &registrationRunnerStub{}
		result, err := (&runtimeReplicaRegistrationStore{runner: runner}).RegisterServing(context.Background(), bad)
		if err == nil || result != (RuntimeReplicaResult{}) || runner.calls != 0 {
			t.Fatalf("result=%+v error=%v calls=%d", result, err, runner.calls)
		}
	}
	result, err := (&runtimeReplicaRegistrationStore{runner: &registrationRunnerStub{}}).RegisterServing(nil, identity) //nolint:staticcheck // Explicit nil-context rejection is part of the boundary.
	if err == nil || result != (RuntimeReplicaResult{}) {
		t.Fatalf("nil context result=%+v error=%v", result, err)
	}
	result, err = NewRuntimeReplicaRegistrationStore(nil).RegisterServing(context.Background(), identity)
	if err == nil || result != (RuntimeReplicaResult{}) {
		t.Fatalf("nil pool result=%+v error=%v", result, err)
	}
}

func TestRuntimeReplicaRegistrationStoreReturnsZeroOnTransportAndDecodeFailure(t *testing.T) {
	identity := registrationStoreIdentity()
	driverErr := errors.New("driver failed")
	for _, test := range []struct {
		name   string
		runner *registrationRunnerStub
	}{
		{"runner", &registrationRunnerStub{err: driverErr}},
		{"finish_or_commit", &registrationRunnerStub{queries: New(&registrationDBStub{values: registrationStoreValues("serving", identity.ReplicaID)}), afterErr: driverErr}},
		{"query", &registrationRunnerStub{queries: New(&registrationDBStub{err: driverErr})}},
		{"required_null", &registrationRunnerStub{queries: New(&registrationDBStub{values: append([]any{nil}, registrationStoreValues("serving", identity.ReplicaID)[1:]...)})}},
		{"optional_present", &registrationRunnerStub{queries: New(&registrationDBStub{values: func() []any { v := registrationStoreValues("serving", identity.ReplicaID); v[4] = true; return v }()})}},
		{"wrong_state", &registrationRunnerStub{queries: New(&registrationDBStub{values: func() []any { v := registrationStoreValues("serving", identity.ReplicaID); v[2] = "unknown"; return v }()})}},
		{"invalid_operation", &registrationRunnerStub{queries: New(&registrationDBStub{values: func() []any {
			v := registrationStoreValues("serving", identity.ReplicaID)
			v[15] = "bad\noperation"
			return v
		}()})}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := (&runtimeReplicaRegistrationStore{runner: test.runner}).RegisterServing(context.Background(), identity)
			if err == nil || result != (RuntimeReplicaResult{}) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if (test.name == "runner" || test.name == "query" || test.name == "finish_or_commit") && !errors.Is(err, driverErr) {
				t.Fatalf("driver cause not wrapped: %v", err)
			}
		})
	}
}

func registrationLiveDatabase(t *testing.T) (string, *sql.DB) {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		if os.Getenv("REQUIRE_TEST_DB") == "1" {
			t.Fatal("TEST_DATABASE_URL is required")
		}
		t.Skip("TEST_DATABASE_URL not set")
	}
	server, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := server.Close(); closeErr != nil {
			t.Errorf("close registration server pool: %v", closeErr)
		}
	})
	name := fmt.Sprintf("aboutme_migrate_test_%d_%d", time.Now().UnixNano(), writeRunnerDatabaseCounter.Add(1))
	if !writeRunnerDatabaseName.MatchString(name) {
		t.Fatalf("unsafe disposable database name %q", name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, createErr := server.ExecContext(ctx, `CREATE DATABASE `+name); createErr != nil {
		t.Fatal(createErr)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, dropErr := server.ExecContext(cleanup, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`); dropErr != nil {
			t.Errorf("drop registration database: %v", dropErr)
		}
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	dsn := u.String()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := admin.Close(); closeErr != nil {
			t.Errorf("close registration database pool: %v", closeErr)
		}
	})
	if err := migrations.ProvisionDatabase(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := migrations.Apply(ctx, admin, migrations.LocalAdminMigratorIdentity()); err != nil {
		t.Fatalf("upgrade registration store database: %v", err)
	}
	return dsn, admin
}

func TestRuntimeReplicaRegistrationStoreLiveCommitAmbiguityReturnsZero(t *testing.T) {
	dsn, admin := registrationLiveDatabase(t)
	pool := newWriteRunnerAppPool(t, dsn, nil)
	wantErr := errors.New("simulated lost registration commit response")
	store := &runtimeReplicaRegistrationStore{runner: liveWrappedRunner(pool, func(tx pgx.Tx) pgx.Tx {
		return &commitResponseLostTx{Tx: tx, err: wantErr}
	})}
	identity := registrationStoreIdentity()
	result, err := store.RegisterServing(context.Background(), identity)
	if !errors.Is(err, wantErr) || result != (RuntimeReplicaResult{}) {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	var rows int
	if err := admin.QueryRowContext(context.Background(), `SELECT count(*) FROM public.runtime_replicas WHERE replica_id=$1`, identity.ReplicaID).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("committed rows=%d error=%v", rows, err)
	}
}

func TestRuntimeReplicaRegistrationStoreLiveValuesSurviveConnectionReuse(t *testing.T) {
	dsn, _ := registrationLiveDatabase(t)
	pool := newWriteRunnerAppPool(t, dsn, nil)
	store := NewRuntimeReplicaRegistrationStore(pool)
	identity := registrationStoreIdentity()

	fresh, err := store.RegisterServing(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	retained := fresh
	replay, err := store.RegisterServing(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := store.MarkServingJoinReady(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	readyReplay, err := store.MarkServingJoinReady(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.CapacityGeneration != 2 || fresh.Replayed || replay.CapacityGeneration != 2 || !replay.Replayed || ready.CapacityGeneration != 3 || ready.Replayed || readyReplay.CapacityGeneration != 3 || !readyReplay.Replayed {
		t.Fatalf("fresh=%+v replay=%+v ready=%+v readyReplay=%+v", fresh, replay, ready, readyReplay)
	}
	if retained != fresh || retained.ReplicaID != identity.ReplicaID || retained.ReplicaKind != "serving" || retained.State != "joining" || retained.ControllerGeneration != 1 || retained.ControllerOperationID != "bootstrap-uncomposed-v1" || retained.LifecyclePhase != "online" || !retained.AdmissionEnabled {
		t.Fatalf("retained fresh result changed after connection reuse: retained=%+v fresh=%+v", retained, fresh)
	}
}

func TestRuntimeReplicaRegistrationStoreLiveFinishFailureReturnsZero(t *testing.T) {
	dsn, admin := registrationLiveDatabase(t)
	pool := newWriteRunnerAppPool(t, dsn, nil)
	wantErr := errors.New("simulated lost registration finish response")
	store := &runtimeReplicaRegistrationStore{runner: liveWrappedRunner(pool, func(tx pgx.Tx) pgx.Tx {
		return &registrationFinishResponseLostTx{Tx: tx, err: wantErr}
	})}
	identity := registrationStoreIdentity()
	result, err := store.RegisterServing(context.Background(), identity)
	if !errors.Is(err, wantErr) || result != (RuntimeReplicaResult{}) {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	var rows int
	if err := admin.QueryRowContext(context.Background(), `SELECT count(*) FROM public.runtime_replicas WHERE replica_id=$1`, identity.ReplicaID).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("rolled-back rows=%d error=%v", rows, err)
	}
}

func TestRuntimeReplicaRegistrationStoreLiveRequiredNullScanReturnsZero(t *testing.T) {
	dsn, admin := registrationLiveDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := admin.ExecContext(ctx, `GRANT CREATE ON SCHEMA public TO aboutme_runtime_owner`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, revokeErr := admin.ExecContext(cleanup, `REVOKE CREATE ON SCHEMA public FROM aboutme_runtime_owner`); revokeErr != nil {
			t.Errorf("revoke runtime owner schema create: %v", revokeErr)
		}
	})
	pool := newWriteRunnerAppPool(t, dsn, nil)
	store := NewRuntimeReplicaRegistrationStore(pool)
	base := []string{"p_replica.replica_id", "p_replica.replica_kind", "p_replica.state", "NULL::smallint", "NULL::smallint", "NULL::smallint", "NULL::boolean", "NULL::boolean", "p_capacity.generation", "p_capacity.controller_generation", "p_capacity.controller_operation_id", "p_capacity.admission_enabled", "p_capacity.lifecycle_phase", "p_replayed"}
	for _, test := range []struct {
		name, null string
		index      int
	}{
		{"replica_id", "NULL::uuid", 0}, {"replica_kind", "NULL::text", 1}, {"state", "NULL::text", 2},
		{"capacity_generation", "NULL::bigint", 8}, {"controller_generation", "NULL::bigint", 9}, {"controller_operation", "NULL::text", 10},
		{"admission", "NULL::boolean", 11}, {"lifecycle", "NULL::text", 12}, {"replayed", "NULL::boolean", 13},
	} {
		t.Run(test.name, func(t *testing.T) {
			fields := append([]string(nil), base...)
			fields[test.index] = test.null
			definition := `SET ROLE aboutme_runtime_owner; CREATE OR REPLACE FUNCTION public.runtime_replica_result_value(p_replica public.runtime_replicas,p_capacity public.runtime_capacity,p_replayed boolean) RETURNS public.runtime_replica_result LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog AS $$ SELECT ROW(` + strings.Join(fields, ",") + `)::public.runtime_replica_result $$; RESET ROLE`
			if _, err := admin.ExecContext(ctx, definition); err != nil {
				t.Fatal(err)
			}
			result, err := store.RegisterServing(ctx, registrationStoreIdentity())
			if err == nil || result != (RuntimeReplicaResult{}) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
	var rows int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM public.runtime_replicas`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("rolled-back rows=%d error=%v", rows, err)
	}
}
