package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

const (
	lifecycleStoreOperationID = "op-scale-out-1"
	lifecycleStoreExpected    = int64(7)
	lifecycleStoreEvidenceID  = "ec2-evidence-1"
	lifecycleStoreLeaveID     = "op-leave-1"
	lifecycleStoreRequestID   = "req-terminate-1"
)

// lifecycleCapacityProjection builds the fixed prepare-scale-out projection:
// desired two, the one active serving replica, partition 1 enabled,
// partition 2 disabled and both wake fields absent.
func lifecycleCapacityProjection(operationID string, expected int64) RuntimePrepareScaleOutRow {
	return RuntimePrepareScaleOutRow{
		DesiredReplicas: 2, ActiveServingReplicas: 1, ActiveMaintenanceReplicas: 0,
		Partition1Enabled: true, Partition2Enabled: false,
		CapacityGeneration: expected + 4, ControllerGeneration: expected + 1,
		ControllerOperationID: operationID, AdmissionEnabled: true, LifecyclePhase: "online",
	}
}

func lifecycleCapacityValues(p RuntimePrepareScaleOutRow) []any {
	return []any{p.DesiredReplicas, p.ActiveServingReplicas, p.ActiveMaintenanceReplicas, p.Partition1Enabled, p.Partition2Enabled,
		p.CapacityGeneration, p.ControllerGeneration, p.ControllerOperationID, p.AdmissionEnabled, p.LifecyclePhase,
		p.WriteGateValue, p.WriteGatePresent, p.WriteGenerationValue, p.WriteGenerationPresent, p.Replayed}
}

func lifecycleStubTransport(values []any) (*runtimeLifecycleTransport, *registrationRunnerStub, *registrationDBStub) {
	db := &registrationDBStub{values: values}
	runner := &registrationRunnerStub{queries: New(db)}
	return &runtimeLifecycleTransport{runner: runner}, runner, db
}

func TestRuntimeLifecycleTransportPrepareScaleOutCallsOneFunction(t *testing.T) {
	projection := lifecycleCapacityProjection(lifecycleStoreOperationID, lifecycleStoreExpected)
	transport, runner, db := lifecycleStubTransport(lifecycleCapacityValues(projection))
	result, err := transport.PrepareScaleOut(context.Background(), lifecycleStoreExpected, lifecycleStoreOperationID)
	if err != nil || runner.calls != 1 || db.calls != 1 || !strings.Contains(db.sql, "runtime_prepare_scale_out") {
		t.Fatalf("result=%+v error=%v runner=%d queries=%d sql=%q", result, err, runner.calls, db.calls, db.sql)
	}
	if result.DesiredReplicas != 2 || result.ActiveServingReplicas != 1 || result.ActiveMaintenanceReplicas != 0 ||
		!result.Partition1Enabled || result.Partition2Enabled || !result.AdmissionEnabled || result.Replayed ||
		result.CapacityGeneration != lifecycleStoreExpected+4 || result.ControllerGeneration != lifecycleStoreExpected+1 ||
		result.ControllerOperationID != lifecycleStoreOperationID || result.LifecyclePhase != "online" ||
		result.WriteGate != nil || result.WriteGeneration != nil {
		t.Fatalf("decoded result=%+v", result)
	}
	replayed := lifecycleCapacityProjection(lifecycleStoreOperationID, lifecycleStoreExpected)
	replayed.Replayed = true
	transport, _, _ = lifecycleStubTransport(lifecycleCapacityValues(replayed))
	result, err = transport.PrepareScaleOut(context.Background(), lifecycleStoreExpected, lifecycleStoreOperationID)
	if err != nil || !result.Replayed || result.WriteGate != nil || result.WriteGeneration != nil {
		t.Fatalf("replayed result=%+v error=%v", result, err)
	}
}

func TestRuntimeLifecycleTransportZerosEveryInvalidCapacityRow(t *testing.T) {
	mutate := func(name string, change func(*RuntimePrepareScaleOutRow)) struct {
		name       string
		projection RuntimePrepareScaleOutRow
	} {
		p := lifecycleCapacityProjection(lifecycleStoreOperationID, lifecycleStoreExpected)
		change(&p)
		return struct {
			name       string
			projection RuntimePrepareScaleOutRow
		}{name, p}
	}
	for _, test := range []struct {
		name       string
		projection RuntimePrepareScaleOutRow
	}{
		mutate("desired_one", func(p *RuntimePrepareScaleOutRow) { p.DesiredReplicas = 1 }),
		mutate("desired_three", func(p *RuntimePrepareScaleOutRow) { p.DesiredReplicas = 3 }),
		mutate("two_active_serving", func(p *RuntimePrepareScaleOutRow) { p.ActiveServingReplicas = 2 }),
		mutate("zero_active_serving", func(p *RuntimePrepareScaleOutRow) { p.ActiveServingReplicas = 0 }),
		mutate("negative_maintenance", func(p *RuntimePrepareScaleOutRow) { p.ActiveMaintenanceReplicas = -1 }),
		mutate("partition_one_disabled", func(p *RuntimePrepareScaleOutRow) { p.Partition1Enabled = false }),
		mutate("partition_two_enabled", func(p *RuntimePrepareScaleOutRow) { p.Partition2Enabled = true }),
		mutate("stale_controller_generation", func(p *RuntimePrepareScaleOutRow) { p.ControllerGeneration = lifecycleStoreExpected }),
		mutate("skipped_controller_generation", func(p *RuntimePrepareScaleOutRow) { p.ControllerGeneration = lifecycleStoreExpected + 2 }),
		mutate("zero_capacity_generation", func(p *RuntimePrepareScaleOutRow) { p.CapacityGeneration = 0 }),
		mutate("negative_capacity_generation", func(p *RuntimePrepareScaleOutRow) { p.CapacityGeneration = -1 }),
		mutate("other_operation_id", func(p *RuntimePrepareScaleOutRow) { p.ControllerOperationID = "op-other" }),
		mutate("empty_operation_id", func(p *RuntimePrepareScaleOutRow) { p.ControllerOperationID = "" }),
		mutate("unknown_phase", func(p *RuntimePrepareScaleOutRow) { p.LifecyclePhase = "paused" }),
		mutate("offline_with_admission", func(p *RuntimePrepareScaleOutRow) { p.LifecyclePhase = "offline" }),
		mutate("online_without_admission", func(p *RuntimePrepareScaleOutRow) { p.AdmissionEnabled = false }),
		mutate("present_write_gate", func(p *RuntimePrepareScaleOutRow) { p.WriteGateValue, p.WriteGatePresent = "closing", true }),
		mutate("present_write_generation", func(p *RuntimePrepareScaleOutRow) { p.WriteGenerationValue, p.WriteGenerationPresent = 12, true }),
	} {
		t.Run(test.name, func(t *testing.T) {
			transport, runner, _ := lifecycleStubTransport(lifecycleCapacityValues(test.projection))
			runner.afterErr = errors.New("must not reach finish")
			result, err := transport.PrepareScaleOut(context.Background(), lifecycleStoreExpected, lifecycleStoreOperationID)
			if err == nil || result != (RuntimeCapacityResult{}) || errors.Is(err, runner.afterErr) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
	driverErr := errors.New("driver failed")
	valid := lifecycleCapacityValues(lifecycleCapacityProjection(lifecycleStoreOperationID, lifecycleStoreExpected))
	for _, test := range []struct {
		name   string
		runner *registrationRunnerStub
	}{
		{"runner", &registrationRunnerStub{err: driverErr}},
		{"finish_or_commit_after_decode", &registrationRunnerStub{queries: New(&registrationDBStub{values: valid}), afterErr: driverErr}},
		{"query", &registrationRunnerStub{queries: New(&registrationDBStub{err: driverErr})}},
		{"required_null", &registrationRunnerStub{queries: New(&registrationDBStub{values: append([]any{nil}, valid[1:]...)})}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := (&runtimeLifecycleTransport{runner: test.runner}).PrepareScaleOut(context.Background(), lifecycleStoreExpected, lifecycleStoreOperationID)
			if err == nil || result != (RuntimeCapacityResult{}) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if test.name != "required_null" && !errors.Is(err, driverErr) {
				t.Fatalf("driver cause not wrapped: %v", err)
			}
		})
	}
}

func TestRuntimeLifecycleTransportRejectsCapacityInputsWithoutQuery(t *testing.T) {
	for _, test := range []struct {
		name       string
		expected   int64
		operation  string
		nilContext bool
	}{
		{name: "nil_context", expected: lifecycleStoreExpected, operation: lifecycleStoreOperationID, nilContext: true},
		{name: "zero_generation", expected: 0, operation: lifecycleStoreOperationID},
		{name: "negative_generation", expected: -1, operation: lifecycleStoreOperationID},
		{name: "empty_operation", expected: lifecycleStoreExpected, operation: ""},
		{name: "long_operation", expected: lifecycleStoreExpected, operation: strings.Repeat("o", 129)},
		{name: "control_character_operation", expected: lifecycleStoreExpected, operation: "op\n1"},
		{name: "high_byte_operation", expected: lifecycleStoreExpected, operation: "opé"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &registrationRunnerStub{}
			transport := &runtimeLifecycleTransport{runner: runner}
			ctx := context.Background()
			if test.nilContext {
				ctx = nil
			}
			result, err := transport.PrepareScaleOut(ctx, test.expected, test.operation) //nolint:staticcheck // Explicit nil-context rejection is part of the boundary.
			if err == nil || result != (RuntimeCapacityResult{}) || runner.calls != 0 {
				t.Fatalf("result=%+v error=%v calls=%d", result, err, runner.calls)
			}
		})
	}
	result, err := NewRuntimeLifecycleTransport(nil).PrepareScaleOut(context.Background(), lifecycleStoreExpected, lifecycleStoreOperationID)
	if err == nil || result != (RuntimeCapacityResult{}) {
		t.Fatalf("nil pool result=%+v error=%v", result, err)
	}
	result, err = (&runtimeLifecycleTransport{}).PrepareScaleOut(context.Background(), lifecycleStoreExpected, lifecycleStoreOperationID)
	if err == nil || result != (RuntimeCapacityResult{}) {
		t.Fatalf("nil runner result=%+v error=%v", result, err)
	}
	var _ RuntimeLifecycleTransport = (*runtimeLifecycleTransport)(nil)
}

// lifecycleReplicaProjection builds a replica-result projection whose five
// optional capacity fields are absent, which is the shape of every ordinary
// action except finish-scale-in.
func lifecycleReplicaProjection(id uuid.UUID, kind, state, operationID string, expected int64) runtimeLifecycleReplicaProjection {
	return runtimeLifecycleReplicaProjection{
		ReplicaID: id, ReplicaKind: kind, State: state,
		CapacityGeneration: expected + 4, ControllerGeneration: expected + 1,
		ControllerOperationID: operationID, AdmissionEnabled: true, LifecyclePhase: "online",
	}
}

func lifecycleReplicaValues(p runtimeLifecycleReplicaProjection) []any {
	return []any{p.ReplicaID, p.ReplicaKind, p.State,
		p.DesiredReplicasValue, p.DesiredReplicasPresent,
		p.ActiveServingReplicasValue, p.ActiveServingReplicasPresent,
		p.ActiveMaintenanceReplicasValue, p.ActiveMaintenanceReplicasPresent,
		p.Partition1EnabledValue, p.Partition1EnabledPresent,
		p.Partition2EnabledValue, p.Partition2EnabledPresent,
		p.CapacityGeneration, p.ControllerGeneration, p.ControllerOperationID,
		p.AdmissionEnabled, p.LifecyclePhase, p.Replayed}
}

// lifecycleActivate calls the activation with the accepted registration
// identity and the supplied replacement predecessor.
func lifecycleActivate(transport RuntimeLifecycleTransport, replaced *uuid.UUID) (RuntimeReplicaResult, error) {
	i := registrationStoreIdentity()
	return transport.ActivateReplicaCapacity(context.Background(), lifecycleStoreExpected, lifecycleStoreOperationID,
		i.ReplicaID, i.InstanceID, i.ContainerInstanceARN, i.CaddyTaskARN, i.GoTaskARN, i.NuxtTaskARN, i.ReleaseDigest, replaced)
}

func TestRuntimeLifecycleTransportActivateReplicaCapacityCallsOneFunction(t *testing.T) {
	identity := registrationStoreIdentity()
	replaced := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	for _, test := range []struct {
		name, kind string
		replaced   *uuid.UUID
		replayed   bool
	}{
		{name: "initial_serving", kind: "serving"},
		{name: "replacement_serving", kind: "serving", replaced: &replaced},
		{name: "maintenance", kind: "maintenance"},
		{name: "replayed_serving", kind: "serving", replayed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			projection := lifecycleReplicaProjection(identity.ReplicaID, test.kind, "active", lifecycleStoreOperationID, lifecycleStoreExpected)
			projection.Replayed = test.replayed
			transport, runner, db := lifecycleStubTransport(lifecycleReplicaValues(projection))
			result, err := lifecycleActivate(transport, test.replaced)
			if err != nil || runner.calls != 1 || db.calls != 1 || !strings.Contains(db.sql, "runtime_activate_replica_capacity") {
				t.Fatalf("result=%+v error=%v runner=%d queries=%d sql=%q", result, err, runner.calls, db.calls, db.sql)
			}
			if result.ReplicaID != identity.ReplicaID || result.ReplicaKind != test.kind || result.State != "active" ||
				result.DesiredReplicas != nil || result.ActiveServingReplicas != nil || result.ActiveMaintenanceReplicas != nil ||
				result.Partition1Enabled != nil || result.Partition2Enabled != nil ||
				result.CapacityGeneration != lifecycleStoreExpected+4 || result.ControllerGeneration != lifecycleStoreExpected+1 ||
				result.ControllerOperationID != lifecycleStoreOperationID || !result.AdmissionEnabled ||
				result.LifecyclePhase != "online" || result.Replayed != test.replayed {
				t.Fatalf("decoded result=%+v", result)
			}
		})
	}
}

func TestRuntimeLifecycleTransportZerosEveryInvalidActivationRow(t *testing.T) {
	identity := registrationStoreIdentity()
	mutate := func(name string, change func(*runtimeLifecycleReplicaProjection)) struct {
		name       string
		projection runtimeLifecycleReplicaProjection
	} {
		p := lifecycleReplicaProjection(identity.ReplicaID, "serving", "active", lifecycleStoreOperationID, lifecycleStoreExpected)
		change(&p)
		return struct {
			name       string
			projection runtimeLifecycleReplicaProjection
		}{name, p}
	}
	for _, test := range []struct {
		name       string
		projection runtimeLifecycleReplicaProjection
	}{
		mutate("other_replica", func(p *runtimeLifecycleReplicaProjection) {
			p.ReplicaID = uuid.MustParse("33333333-3333-4333-8333-333333333333")
		}),
		mutate("nil_replica", func(p *runtimeLifecycleReplicaProjection) { p.ReplicaID = uuid.Nil }),
		mutate("unknown_kind", func(p *runtimeLifecycleReplicaProjection) { p.ReplicaKind = "worker" }),
		mutate("empty_kind", func(p *runtimeLifecycleReplicaProjection) { p.ReplicaKind = "" }),
		mutate("joining_state", func(p *runtimeLifecycleReplicaProjection) { p.State = "joining" }),
		mutate("draining_state", func(p *runtimeLifecycleReplicaProjection) { p.State = "draining" }),
		mutate("terminating_state", func(p *runtimeLifecycleReplicaProjection) { p.State = "terminating" }),
		mutate("left_state", func(p *runtimeLifecycleReplicaProjection) { p.State = "left" }),
		mutate("empty_state", func(p *runtimeLifecycleReplicaProjection) { p.State = "" }),
		mutate("present_desired", func(p *runtimeLifecycleReplicaProjection) {
			p.DesiredReplicasValue, p.DesiredReplicasPresent = 1, true
		}),
		mutate("present_serving", func(p *runtimeLifecycleReplicaProjection) {
			p.ActiveServingReplicasValue, p.ActiveServingReplicasPresent = 1, true
		}),
		mutate("present_maintenance", func(p *runtimeLifecycleReplicaProjection) {
			p.ActiveMaintenanceReplicasValue, p.ActiveMaintenanceReplicasPresent = 0, true
		}),
		mutate("present_partition_1", func(p *runtimeLifecycleReplicaProjection) {
			p.Partition1EnabledValue, p.Partition1EnabledPresent = true, true
		}),
		mutate("present_partition_2", func(p *runtimeLifecycleReplicaProjection) {
			p.Partition2EnabledValue, p.Partition2EnabledPresent = false, true
		}),
		mutate("stale_controller_generation", func(p *runtimeLifecycleReplicaProjection) {
			p.ControllerGeneration = lifecycleStoreExpected
		}),
		mutate("zero_capacity_generation", func(p *runtimeLifecycleReplicaProjection) { p.CapacityGeneration = 0 }),
		mutate("other_operation_id", func(p *runtimeLifecycleReplicaProjection) { p.ControllerOperationID = "op-other" }),
		mutate("unknown_phase", func(p *runtimeLifecycleReplicaProjection) { p.LifecyclePhase = "paused" }),
		mutate("stopping_with_admission", func(p *runtimeLifecycleReplicaProjection) { p.LifecyclePhase = "stopping" }),
	} {
		t.Run(test.name, func(t *testing.T) {
			transport, runner, _ := lifecycleStubTransport(lifecycleReplicaValues(test.projection))
			runner.afterErr = errors.New("must not reach finish")
			result, err := lifecycleActivate(transport, nil)
			if err == nil || result != (RuntimeReplicaResult{}) || errors.Is(err, runner.afterErr) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}

func TestRuntimeLifecycleTransportRejectsActivationInputsWithoutQuery(t *testing.T) {
	identity := registrationStoreIdentity()
	nilUUID := uuid.Nil
	for _, test := range []struct {
		name string
		call func(RuntimeLifecycleTransport) (RuntimeReplicaResult, error)
	}{
		{"nil_context", func(s RuntimeLifecycleTransport) (RuntimeReplicaResult, error) {
			return s.ActivateReplicaCapacity(nil, lifecycleStoreExpected, lifecycleStoreOperationID, identity.ReplicaID, //nolint:staticcheck // Explicit nil-context rejection is part of the boundary.
				identity.InstanceID, identity.ContainerInstanceARN, identity.CaddyTaskARN, identity.GoTaskARN, identity.NuxtTaskARN, identity.ReleaseDigest, nil)
		}},
		{"zero_generation", func(s RuntimeLifecycleTransport) (RuntimeReplicaResult, error) {
			return s.ActivateReplicaCapacity(context.Background(), 0, lifecycleStoreOperationID, identity.ReplicaID,
				identity.InstanceID, identity.ContainerInstanceARN, identity.CaddyTaskARN, identity.GoTaskARN, identity.NuxtTaskARN, identity.ReleaseDigest, nil)
		}},
		{"empty_operation", func(s RuntimeLifecycleTransport) (RuntimeReplicaResult, error) {
			return s.ActivateReplicaCapacity(context.Background(), lifecycleStoreExpected, "", identity.ReplicaID,
				identity.InstanceID, identity.ContainerInstanceARN, identity.CaddyTaskARN, identity.GoTaskARN, identity.NuxtTaskARN, identity.ReleaseDigest, nil)
		}},
		{"nil_replica", func(s RuntimeLifecycleTransport) (RuntimeReplicaResult, error) {
			return s.ActivateReplicaCapacity(context.Background(), lifecycleStoreExpected, lifecycleStoreOperationID, uuid.Nil,
				identity.InstanceID, identity.ContainerInstanceARN, identity.CaddyTaskARN, identity.GoTaskARN, identity.NuxtTaskARN, identity.ReleaseDigest, nil)
		}},
		{"malformed_instance", func(s RuntimeLifecycleTransport) (RuntimeReplicaResult, error) {
			return s.ActivateReplicaCapacity(context.Background(), lifecycleStoreExpected, lifecycleStoreOperationID, identity.ReplicaID,
				"i-XYZ", identity.ContainerInstanceARN, identity.CaddyTaskARN, identity.GoTaskARN, identity.NuxtTaskARN, identity.ReleaseDigest, nil)
		}},
		{"malformed_release", func(s RuntimeLifecycleTransport) (RuntimeReplicaResult, error) {
			return s.ActivateReplicaCapacity(context.Background(), lifecycleStoreExpected, lifecycleStoreOperationID, identity.ReplicaID,
				identity.InstanceID, identity.ContainerInstanceARN, identity.CaddyTaskARN, identity.GoTaskARN, identity.NuxtTaskARN, "sha256:zz", nil)
		}},
		{"empty_arn", func(s RuntimeLifecycleTransport) (RuntimeReplicaResult, error) {
			return s.ActivateReplicaCapacity(context.Background(), lifecycleStoreExpected, lifecycleStoreOperationID, identity.ReplicaID,
				identity.InstanceID, "", identity.CaddyTaskARN, identity.GoTaskARN, identity.NuxtTaskARN, identity.ReleaseDigest, nil)
		}},
		{"long_arn", func(s RuntimeLifecycleTransport) (RuntimeReplicaResult, error) {
			return s.ActivateReplicaCapacity(context.Background(), lifecycleStoreExpected, lifecycleStoreOperationID, identity.ReplicaID,
				identity.InstanceID, identity.ContainerInstanceARN, strings.Repeat("a", 513), identity.GoTaskARN, identity.NuxtTaskARN, identity.ReleaseDigest, nil)
		}},
		{"space_in_arn", func(s RuntimeLifecycleTransport) (RuntimeReplicaResult, error) {
			return s.ActivateReplicaCapacity(context.Background(), lifecycleStoreExpected, lifecycleStoreOperationID, identity.ReplicaID,
				identity.InstanceID, identity.ContainerInstanceARN, identity.CaddyTaskARN, "arn task go", identity.NuxtTaskARN, identity.ReleaseDigest, nil)
		}},
		{"duplicate_arn", func(s RuntimeLifecycleTransport) (RuntimeReplicaResult, error) {
			return s.ActivateReplicaCapacity(context.Background(), lifecycleStoreExpected, lifecycleStoreOperationID, identity.ReplicaID,
				identity.InstanceID, identity.ContainerInstanceARN, identity.CaddyTaskARN, identity.CaddyTaskARN, identity.NuxtTaskARN, identity.ReleaseDigest, nil)
		}},
		{"nil_replacement_uuid", func(s RuntimeLifecycleTransport) (RuntimeReplicaResult, error) {
			return s.ActivateReplicaCapacity(context.Background(), lifecycleStoreExpected, lifecycleStoreOperationID, identity.ReplicaID,
				identity.InstanceID, identity.ContainerInstanceARN, identity.CaddyTaskARN, identity.GoTaskARN, identity.NuxtTaskARN, identity.ReleaseDigest, &nilUUID)
		}},
		{"self_replacement", func(s RuntimeLifecycleTransport) (RuntimeReplicaResult, error) {
			self := identity.ReplicaID
			return s.ActivateReplicaCapacity(context.Background(), lifecycleStoreExpected, lifecycleStoreOperationID, identity.ReplicaID,
				identity.InstanceID, identity.ContainerInstanceARN, identity.CaddyTaskARN, identity.GoTaskARN, identity.NuxtTaskARN, identity.ReleaseDigest, &self)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &registrationRunnerStub{}
			result, err := test.call(&runtimeLifecycleTransport{runner: runner})
			if err == nil || result != (RuntimeReplicaResult{}) || runner.calls != 0 {
				t.Fatalf("result=%+v error=%v calls=%d", result, err, runner.calls)
			}
		})
	}
	result, err := lifecycleActivate(NewRuntimeLifecycleTransport(nil), nil)
	if err == nil || result != (RuntimeReplicaResult{}) {
		t.Fatalf("nil pool result=%+v error=%v", result, err)
	}
	if result, err = lifecycleActivate(&runtimeLifecycleTransport{}, nil); err == nil || result != (RuntimeReplicaResult{}) {
		t.Fatalf("nil runner result=%+v error=%v", result, err)
	}
}

// lifecycleArgumentDB records the arguments of one generated call. The
// shared registration stub records only the SQL text, so this override is the
// only way to inspect what the transport actually sent.
type lifecycleArgumentDB struct {
	*registrationDBStub
	args []any
}

func (d *lifecycleArgumentDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	d.args = args
	return d.registrationDBStub.QueryRow(ctx, sql, args...)
}

// lifecycleMutatingRunner mutates caller state between the transport call and
// the generated query, so a transport that kept the caller's pointer would
// send the changed value.
type lifecycleMutatingRunner struct {
	queries *Queries
	mutate  func()
}

func (r *lifecycleMutatingRunner) ExecWrite(_ context.Context, callback func(*Queries) error) error {
	r.mutate()
	return callback(r.queries)
}

func (r *lifecycleMutatingRunner) WithWriteTx(context.Context, pgx.TxOptions, func(*Queries) error) error {
	return errors.New("unexpected WithWriteTx")
}

// TestRuntimeLifecycleTransportOwnsItsNullableArguments proves the transport
// sends a copy of every caller pointer: a caller that changes its replacement
// predecessor or its leave operation while the call runs cannot change what
// the database sees.
func TestRuntimeLifecycleTransportOwnsItsNullableArguments(t *testing.T) {
	identity := registrationStoreIdentity()
	original := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	replaced := original
	projection := lifecycleReplicaProjection(identity.ReplicaID, "serving", "active", lifecycleStoreOperationID, lifecycleStoreExpected)
	db := &lifecycleArgumentDB{registrationDBStub: &registrationDBStub{values: lifecycleReplicaValues(projection)}}
	runner := &lifecycleMutatingRunner{queries: New(db), mutate: func() {
		replaced = uuid.MustParse("44444444-4444-4444-8444-444444444444")
	}}
	if _, err := lifecycleActivate(&runtimeLifecycleTransport{runner: runner}, &replaced); err != nil {
		t.Fatal(err)
	}
	if len(db.args) != 10 {
		t.Fatalf("activation sent %d arguments", len(db.args))
	}
	sent, ok := db.args[9].(*uuid.UUID)
	if !ok || sent == nil || *sent != original || sent == &replaced {
		t.Fatalf("replacement argument was not an owned copy: %v ok=%v", db.args[9], ok)
	}
}

// lifecycleReplicaMethod describes one fixed replica-result method: the SQL
// function it must call, the immutable kinds and terminal states its result
// may carry, whether it publishes the capacity fields, and how it is called.
type lifecycleReplicaMethod struct {
	name, sqlName string
	kinds, states []string
	capacity      bool
	call          func(RuntimeLifecycleTransport) (RuntimeReplicaResult, error)
}

var (
	lifecycleAllKinds  = []string{"serving", "maintenance"}
	lifecycleAllStates = []string{"joining", "active", "draining", "left", "terminating", "fenced"}
)

func lifecycleAllows(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// lifecycleMatrixProjection builds the result one method must accept for the
// supplied kind and state, including the fixed finish-scale-in capacity
// fields when the method publishes them.
func lifecycleMatrixProjection(method lifecycleReplicaMethod, kind, state string) runtimeLifecycleReplicaProjection {
	p := lifecycleReplicaProjection(registrationStoreIdentity().ReplicaID, kind, state, lifecycleStoreOperationID, lifecycleStoreExpected)
	if method.capacity {
		p.DesiredReplicasValue, p.DesiredReplicasPresent = 1, true
		p.ActiveServingReplicasValue, p.ActiveServingReplicasPresent = 1, true
		p.ActiveMaintenanceReplicasValue, p.ActiveMaintenanceReplicasPresent = 0, true
		p.Partition1EnabledValue, p.Partition1EnabledPresent = true, true
		p.Partition2EnabledValue, p.Partition2EnabledPresent = false, true
	}
	return p
}

func lifecycleReplicaMethods() []lifecycleReplicaMethod {
	identity := registrationStoreIdentity()
	return []lifecycleReplicaMethod{
		{name: "prepare_scale_in", sqlName: "runtime_prepare_scale_in", kinds: []string{"serving"}, states: []string{"draining"},
			call: func(s RuntimeLifecycleTransport) (RuntimeReplicaResult, error) {
				return s.PrepareScaleIn(context.Background(), lifecycleStoreExpected, lifecycleStoreOperationID,
					identity.ReplicaID, identity.InstanceID, identity.ReleaseDigest)
			}},
		{name: "finish_scale_in", sqlName: "runtime_finish_scale_in", kinds: []string{"serving"}, states: []string{"left", "fenced"}, capacity: true,
			call: func(s RuntimeLifecycleTransport) (RuntimeReplicaResult, error) {
				return s.FinishScaleIn(context.Background(), lifecycleStoreExpected, lifecycleStoreOperationID,
					identity.ReplicaID, identity.InstanceID, identity.ReleaseDigest, nil, lifecycleStoreEvidenceID)
			}},
		{name: "prepare_maintenance_drain", sqlName: "runtime_prepare_maintenance_drain", kinds: []string{"maintenance"}, states: []string{"draining"},
			call: func(s RuntimeLifecycleTransport) (RuntimeReplicaResult, error) {
				return s.PrepareMaintenanceDrain(context.Background(), lifecycleStoreExpected, lifecycleStoreOperationID,
					identity.ReplicaID, identity.InstanceID, identity.ReleaseDigest)
			}},
		{name: "begin_replica_termination", sqlName: "runtime_begin_replica_termination", kinds: lifecycleAllKinds, states: []string{"terminating"},
			call: func(s RuntimeLifecycleTransport) (RuntimeReplicaResult, error) {
				return s.BeginReplicaTermination(context.Background(), lifecycleStoreExpected, lifecycleStoreOperationID,
					lifecycleStoreRequestID, identity.ReplicaID, identity.InstanceID, identity.ReleaseDigest, "readiness_failed")
			}},
	}
}

// TestRuntimeLifecycleTransportReplicaMethodsCallOneFunctionEach proves each
// fixed replica method reaches exactly one generated function and decodes
// every field of its own result shape.
func TestRuntimeLifecycleTransportReplicaMethodsCallOneFunctionEach(t *testing.T) {
	identity := registrationStoreIdentity()
	for _, method := range lifecycleReplicaMethods() {
		t.Run(method.name, func(t *testing.T) {
			projection := lifecycleMatrixProjection(method, method.kinds[0], method.states[0])
			transport, runner, db := lifecycleStubTransport(lifecycleReplicaValues(projection))
			result, err := method.call(transport)
			if err != nil || runner.calls != 1 || db.calls != 1 || !strings.Contains(db.sql, method.sqlName) {
				t.Fatalf("result=%+v error=%v runner=%d queries=%d sql=%q", result, err, runner.calls, db.calls, db.sql)
			}
			if result.ReplicaID != identity.ReplicaID || result.ReplicaKind != method.kinds[0] || result.State != method.states[0] ||
				result.CapacityGeneration != lifecycleStoreExpected+4 || result.ControllerGeneration != lifecycleStoreExpected+1 ||
				result.ControllerOperationID != lifecycleStoreOperationID || !result.AdmissionEnabled ||
				result.LifecyclePhase != "online" || result.Replayed {
				t.Fatalf("decoded result=%+v", result)
			}
			if !method.capacity {
				if result.DesiredReplicas != nil || result.ActiveServingReplicas != nil || result.ActiveMaintenanceReplicas != nil ||
					result.Partition1Enabled != nil || result.Partition2Enabled != nil {
					t.Fatalf("absent capacity fields decoded: %+v", result)
				}
				return
			}
			if result.DesiredReplicas == nil || *result.DesiredReplicas != 1 || result.ActiveServingReplicas == nil || *result.ActiveServingReplicas != 1 ||
				result.ActiveMaintenanceReplicas == nil || *result.ActiveMaintenanceReplicas != 0 ||
				result.Partition1Enabled == nil || !*result.Partition1Enabled ||
				result.Partition2Enabled == nil || *result.Partition2Enabled {
				t.Fatalf("present capacity fields decoded: %+v", result)
			}
			replayed := lifecycleMatrixProjection(method, method.kinds[0], method.states[0])
			replayed.Replayed = true
			transport, _, _ = lifecycleStubTransport(lifecycleReplicaValues(replayed))
			result, err = method.call(transport)
			if err != nil || !result.Replayed {
				t.Fatalf("replayed result=%+v error=%v", result, err)
			}
		})
	}
}

// TestRuntimeLifecycleTransportEnforcesEveryReplicaMatrix walks the full
// kind and state matrix of each fixed method and requires that only its own
// accepted combinations decode.
func TestRuntimeLifecycleTransportEnforcesEveryReplicaMatrix(t *testing.T) {
	for _, method := range lifecycleReplicaMethods() {
		for _, kind := range lifecycleAllKinds {
			for _, state := range lifecycleAllStates {
				t.Run(method.name+"_"+kind+"_"+state, func(t *testing.T) {
					want := lifecycleAllows(method.kinds, kind) && lifecycleAllows(method.states, state)
					transport, runner, _ := lifecycleStubTransport(lifecycleReplicaValues(lifecycleMatrixProjection(method, kind, state)))
					if !want {
						runner.afterErr = errors.New("must not reach finish")
					}
					result, err := method.call(transport)
					if want {
						if err != nil || result.ReplicaKind != kind || result.State != state {
							t.Fatalf("accepted shape rejected: result=%+v error=%v", result, err)
						}
						return
					}
					if err == nil || result != (RuntimeReplicaResult{}) || errors.Is(err, runner.afterErr) {
						t.Fatalf("rejected shape accepted: result=%+v error=%v", result, err)
					}
				})
			}
		}
	}
}

// TestRuntimeLifecycleTransportEnforcesEveryCapacityPresence proves each
// fixed method requires exactly its own presence of the five optional
// capacity fields, in both directions.
func TestRuntimeLifecycleTransportEnforcesEveryCapacityPresence(t *testing.T) {
	for _, method := range lifecycleReplicaMethods() {
		for _, field := range []string{"desired", "serving", "maintenance", "partition_1", "partition_2"} {
			t.Run(method.name+"_"+field, func(t *testing.T) {
				p := lifecycleMatrixProjection(method, method.kinds[0], method.states[0])
				switch field {
				case "desired":
					p.DesiredReplicasPresent = !p.DesiredReplicasPresent
				case "serving":
					p.ActiveServingReplicasPresent = !p.ActiveServingReplicasPresent
				case "maintenance":
					p.ActiveMaintenanceReplicasPresent = !p.ActiveMaintenanceReplicasPresent
				case "partition_1":
					p.Partition1EnabledPresent = !p.Partition1EnabledPresent
				case "partition_2":
					p.Partition2EnabledPresent = !p.Partition2EnabledPresent
				}
				transport, runner, _ := lifecycleStubTransport(lifecycleReplicaValues(p))
				runner.afterErr = errors.New("must not reach finish")
				result, err := method.call(transport)
				if err == nil || result != (RuntimeReplicaResult{}) || errors.Is(err, runner.afterErr) {
					t.Fatalf("result=%+v error=%v", result, err)
				}
			})
		}
	}
}

// TestRuntimeLifecycleTransportRejectsReplicaTargetsWithoutQuery proves every
// fixed replica method rejects the malformed target tuple before the runner.
func TestRuntimeLifecycleTransportRejectsReplicaTargetsWithoutQuery(t *testing.T) {
	identity := registrationStoreIdentity()
	for _, test := range []struct {
		name string
		call func(RuntimeLifecycleTransport) (RuntimeReplicaResult, error)
	}{
		{"scale_in_nil_context", func(s RuntimeLifecycleTransport) (RuntimeReplicaResult, error) {
			return s.PrepareScaleIn(nil, lifecycleStoreExpected, lifecycleStoreOperationID, identity.ReplicaID, identity.InstanceID, identity.ReleaseDigest) //nolint:staticcheck // Explicit nil-context rejection is part of the boundary.
		}},
		{"scale_in_zero_generation", func(s RuntimeLifecycleTransport) (RuntimeReplicaResult, error) {
			return s.PrepareScaleIn(context.Background(), 0, lifecycleStoreOperationID, identity.ReplicaID, identity.InstanceID, identity.ReleaseDigest)
		}},
		{"scale_in_long_operation", func(s RuntimeLifecycleTransport) (RuntimeReplicaResult, error) {
			return s.PrepareScaleIn(context.Background(), lifecycleStoreExpected, strings.Repeat("o", 129), identity.ReplicaID, identity.InstanceID, identity.ReleaseDigest)
		}},
		{"scale_in_nil_replica", func(s RuntimeLifecycleTransport) (RuntimeReplicaResult, error) {
			return s.PrepareScaleIn(context.Background(), lifecycleStoreExpected, lifecycleStoreOperationID, uuid.Nil, identity.InstanceID, identity.ReleaseDigest)
		}},
		{"scale_in_malformed_instance", func(s RuntimeLifecycleTransport) (RuntimeReplicaResult, error) {
			return s.PrepareScaleIn(context.Background(), lifecycleStoreExpected, lifecycleStoreOperationID, identity.ReplicaID, "i-0", identity.ReleaseDigest)
		}},
		{"scale_in_malformed_release", func(s RuntimeLifecycleTransport) (RuntimeReplicaResult, error) {
			return s.PrepareScaleIn(context.Background(), lifecycleStoreExpected, lifecycleStoreOperationID, identity.ReplicaID, identity.InstanceID, "sha1:"+strings.Repeat("a", 64))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &registrationRunnerStub{}
			result, err := test.call(&runtimeLifecycleTransport{runner: runner})
			if err == nil || result != (RuntimeReplicaResult{}) || runner.calls != 0 {
				t.Fatalf("result=%+v error=%v calls=%d", result, err, runner.calls)
			}
		})
	}
	for _, method := range lifecycleReplicaMethods() {
		t.Run(method.name+"_nil_runner", func(t *testing.T) {
			result, err := method.call(&runtimeLifecycleTransport{})
			if err == nil || result != (RuntimeReplicaResult{}) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if result, err = method.call(NewRuntimeLifecycleTransport(nil)); err == nil || result != (RuntimeReplicaResult{}) {
				t.Fatalf("nil pool result=%+v error=%v", result, err)
			}
		})
	}
}

// lifecycleFinish calls the finish with the accepted target tuple and the
// supplied optional leave operation.
func lifecycleFinish(transport RuntimeLifecycleTransport, leave *string, evidence string) (RuntimeReplicaResult, error) {
	i := registrationStoreIdentity()
	return transport.FinishScaleIn(context.Background(), lifecycleStoreExpected, lifecycleStoreOperationID,
		i.ReplicaID, i.InstanceID, i.ReleaseDigest, leave, evidence)
}

// TestRuntimeLifecycleTransportFinishScaleInBranchesAndCapacityMatrix proves
// the graceful and abrupt branches both decode, and that the fixed retirement
// matrix is enforced: desired one, zero or one survivor, partition 1
// preserved and partition 2 disabled.
func TestRuntimeLifecycleTransportFinishScaleInBranchesAndCapacityMatrix(t *testing.T) {
	method := lifecycleReplicaMethods()[1]
	leave := lifecycleStoreLeaveID
	for _, test := range []struct {
		name, state string
		leave       *string
		survivors   int16
	}{
		{name: "graceful_with_survivor", state: "left", leave: &leave, survivors: 1},
		{name: "abrupt_with_survivor", state: "fenced", survivors: 1},
		{name: "abrupt_without_survivor", state: "fenced", survivors: 0},
		{name: "graceful_without_survivor", state: "left", leave: &leave, survivors: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := lifecycleMatrixProjection(method, "serving", test.state)
			p.ActiveServingReplicasValue = test.survivors
			transport, runner, db := lifecycleStubTransport(lifecycleReplicaValues(p))
			result, err := lifecycleFinish(transport, test.leave, lifecycleStoreEvidenceID)
			if err != nil || runner.calls != 1 || db.calls != 1 || !strings.Contains(db.sql, "runtime_finish_scale_in") {
				t.Fatalf("result=%+v error=%v runner=%d queries=%d sql=%q", result, err, runner.calls, db.calls, db.sql)
			}
			if result.State != test.state || result.ActiveServingReplicas == nil || *result.ActiveServingReplicas != test.survivors ||
				result.DesiredReplicas == nil || *result.DesiredReplicas != 1 ||
				result.Partition1Enabled == nil || !*result.Partition1Enabled ||
				result.Partition2Enabled == nil || *result.Partition2Enabled {
				t.Fatalf("decoded result=%+v", result)
			}
		})
	}
	mutate := func(name string, change func(*runtimeLifecycleReplicaProjection)) struct {
		name       string
		projection runtimeLifecycleReplicaProjection
	} {
		p := lifecycleMatrixProjection(method, "serving", "fenced")
		change(&p)
		return struct {
			name       string
			projection runtimeLifecycleReplicaProjection
		}{name, p}
	}
	for _, test := range []struct {
		name       string
		projection runtimeLifecycleReplicaProjection
	}{
		mutate("desired_two", func(p *runtimeLifecycleReplicaProjection) { p.DesiredReplicasValue = 2 }),
		mutate("desired_zero", func(p *runtimeLifecycleReplicaProjection) { p.DesiredReplicasValue = 0 }),
		mutate("two_survivors", func(p *runtimeLifecycleReplicaProjection) { p.ActiveServingReplicasValue = 2 }),
		mutate("negative_survivors", func(p *runtimeLifecycleReplicaProjection) { p.ActiveServingReplicasValue = -1 }),
		mutate("negative_maintenance", func(p *runtimeLifecycleReplicaProjection) { p.ActiveMaintenanceReplicasValue = -1 }),
		mutate("partition_one_disabled", func(p *runtimeLifecycleReplicaProjection) { p.Partition1EnabledValue = false }),
		mutate("partition_two_enabled", func(p *runtimeLifecycleReplicaProjection) { p.Partition2EnabledValue = true }),
	} {
		t.Run(test.name, func(t *testing.T) {
			transport, runner, _ := lifecycleStubTransport(lifecycleReplicaValues(test.projection))
			runner.afterErr = errors.New("must not reach finish")
			result, err := lifecycleFinish(transport, nil, lifecycleStoreEvidenceID)
			if err == nil || result != (RuntimeReplicaResult{}) || errors.Is(err, runner.afterErr) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}

// TestRuntimeLifecycleTransportRejectsFinishEvidenceWithoutQuery covers the
// two evidence identifiers: the leave operation is optional but bounded when
// present, and the fencing evidence is always required.
func TestRuntimeLifecycleTransportRejectsFinishEvidenceWithoutQuery(t *testing.T) {
	empty, long, control := "", strings.Repeat("l", 129), "op\tleave"
	for _, test := range []struct {
		name     string
		leave    *string
		evidence string
	}{
		{name: "empty_leave", leave: &empty, evidence: lifecycleStoreEvidenceID},
		{name: "long_leave", leave: &long, evidence: lifecycleStoreEvidenceID},
		{name: "control_leave", leave: &control, evidence: lifecycleStoreEvidenceID},
		{name: "empty_evidence", evidence: ""},
		{name: "long_evidence", evidence: strings.Repeat("e", 129)},
		{name: "control_evidence", evidence: "ec2\nevidence"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &registrationRunnerStub{}
			result, err := lifecycleFinish(&runtimeLifecycleTransport{runner: runner}, test.leave, test.evidence)
			if err == nil || result != (RuntimeReplicaResult{}) || runner.calls != 0 {
				t.Fatalf("result=%+v error=%v calls=%d", result, err, runner.calls)
			}
		})
	}
}

// TestRuntimeLifecycleTransportOwnsTheLeaveArgument proves the optional leave
// operation reaches the driver as an owned copy.
func TestRuntimeLifecycleTransportOwnsTheLeaveArgument(t *testing.T) {
	method := lifecycleReplicaMethods()[1]
	leave := lifecycleStoreLeaveID
	db := &lifecycleArgumentDB{registrationDBStub: &registrationDBStub{values: lifecycleReplicaValues(lifecycleMatrixProjection(method, "serving", "left"))}}
	runner := &lifecycleMutatingRunner{queries: New(db), mutate: func() { leave = "op-leave-forged" }}
	if _, err := lifecycleFinish(&runtimeLifecycleTransport{runner: runner}, &leave, lifecycleStoreEvidenceID); err != nil {
		t.Fatal(err)
	}
	if len(db.args) != 7 {
		t.Fatalf("finish sent %d arguments", len(db.args))
	}
	sent, ok := db.args[5].(*string)
	if !ok || sent == nil || *sent != lifecycleStoreLeaveID || sent == &leave {
		t.Fatalf("leave argument was not an owned copy: %v ok=%v", db.args[5], ok)
	}
}

// lifecycleTerminate calls the termination with the accepted target tuple.
func lifecycleTerminate(transport RuntimeLifecycleTransport, requestID, reason string) (RuntimeReplicaResult, error) {
	i := registrationStoreIdentity()
	return transport.BeginReplicaTermination(context.Background(), lifecycleStoreExpected, lifecycleStoreOperationID,
		requestID, i.ReplicaID, i.InstanceID, i.ReleaseDigest, reason)
}

// TestRuntimeLifecycleTransportTerminationReasonsAndRequestBounds proves each
// accepted reason reaches the one function and that every reason or request
// identifier the SQL rejects is refused before the runner.
func TestRuntimeLifecycleTransportTerminationReasonsAndRequestBounds(t *testing.T) {
	identity := registrationStoreIdentity()
	for _, reason := range []string{"startup_failed", "readiness_failed", "drain_failed"} {
		t.Run("accepts_"+reason, func(t *testing.T) {
			projection := lifecycleReplicaProjection(identity.ReplicaID, "serving", "terminating", lifecycleStoreOperationID, lifecycleStoreExpected)
			transport, runner, db := lifecycleStubTransport(lifecycleReplicaValues(projection))
			result, err := lifecycleTerminate(transport, lifecycleStoreRequestID, reason)
			if err != nil || runner.calls != 1 || db.calls != 1 || !strings.Contains(db.sql, "runtime_begin_replica_termination") {
				t.Fatalf("result=%+v error=%v runner=%d queries=%d sql=%q", result, err, runner.calls, db.calls, db.sql)
			}
			if result.State != "terminating" || result.DesiredReplicas != nil || result.Partition1Enabled != nil {
				t.Fatalf("decoded result=%+v", result)
			}
		})
	}
	for _, test := range []struct{ name, requestID, reason string }{
		{"empty_reason", lifecycleStoreRequestID, ""},
		{"unknown_reason", lifecycleStoreRequestID, "node_failed"},
		{"uppercase_reason", lifecycleStoreRequestID, "STARTUP_FAILED"},
		{"padded_reason", lifecycleStoreRequestID, " drain_failed"},
		{"empty_request", "", "drain_failed"},
		{"long_request", strings.Repeat("r", 129), "drain_failed"},
		{"control_request", "req\x00terminate", "drain_failed"},
	} {
		t.Run("rejects_"+test.name, func(t *testing.T) {
			runner := &registrationRunnerStub{}
			result, err := lifecycleTerminate(&runtimeLifecycleTransport{runner: runner}, test.requestID, test.reason)
			if err == nil || result != (RuntimeReplicaResult{}) || runner.calls != 0 {
				t.Fatalf("result=%+v error=%v calls=%d", result, err, runner.calls)
			}
		})
	}
}

// TestRuntimeLifecycleTransportEntryFunctionFinishCommitOrder pins the write
// barrier order of every fixed action and proves no decoded value survives a
// lost finish, an ambiguous commit or a poisoned backend.
//
// The asserted sequence names the operation in the function position, so a
// method wired to the wrong query fails here as well as in its stub test.
func TestRuntimeLifecycleTransportEntryFunctionFinishCommitOrder(t *testing.T) {
	identity := registrationStoreIdentity()
	active := lifecycleReplicaProjection(identity.ReplicaID, "serving", "active", lifecycleStoreOperationID, lifecycleStoreExpected)
	capacity := lifecycleCapacityProjection(lifecycleStoreOperationID, lifecycleStoreExpected)

	runner, lease := claimOrderRunner(lifecycleCapacityValues(capacity))
	result, err := (&runtimeLifecycleTransport{runner: runner}).PrepareScaleOut(context.Background(), lifecycleStoreExpected, lifecycleStoreOperationID)
	if err != nil || result.DesiredReplicas != 2 || lease.tx.functions != 1 {
		t.Fatalf("result=%+v error=%v functions=%d", result, err, lease.tx.functions)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:runtime_prepare_scale_out", "finish", "commit", "release")

	invalid := lifecycleReplicaProjection(identity.ReplicaID, "serving", "joining", lifecycleStoreOperationID, lifecycleStoreExpected)
	runner, lease = claimOrderRunner(lifecycleReplicaValues(invalid))
	replica, err := lifecycleActivate(&runtimeLifecycleTransport{runner: runner}, nil)
	if err == nil || replica != (RuntimeReplicaResult{}) {
		t.Fatalf("invalid replica=%+v error=%v", replica, err)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:runtime_activate_replica_capacity", "rollback", "release")

	runner, lease = claimOrderRunner(lifecycleReplicaValues(active))
	lease.tx.finishErr = errors.New("finish lost")
	replica, err = lifecycleActivate(&runtimeLifecycleTransport{runner: runner}, nil)
	if !errors.Is(err, lease.tx.finishErr) || replica != (RuntimeReplicaResult{}) {
		t.Fatalf("finish failure replica=%+v error=%v", replica, err)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:runtime_activate_replica_capacity", "finish", "rollback", "destroy")

	draining := lifecycleReplicaProjection(identity.ReplicaID, "serving", "draining", lifecycleStoreOperationID, lifecycleStoreExpected)
	runner, lease = claimOrderRunner(lifecycleReplicaValues(draining))
	lease.tx.commitErr = errors.New("commit ambiguous")
	replica, err = (&runtimeLifecycleTransport{runner: runner}).PrepareScaleIn(context.Background(), lifecycleStoreExpected,
		lifecycleStoreOperationID, identity.ReplicaID, identity.InstanceID, identity.ReleaseDigest)
	if !errors.Is(err, lease.tx.commitErr) || replica != (RuntimeReplicaResult{}) {
		t.Fatalf("commit failure replica=%+v error=%v", replica, err)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:runtime_prepare_scale_in", "finish", "commit", "destroy")

	runner, lease = claimOrderRunner(nil)
	lease.tx.queryErr = &pgconn.PgError{Code: "AM001", Message: "lifecycle step is corrupt"}
	replica, err = lifecycleFinish(&runtimeLifecycleTransport{runner: runner}, nil, lifecycleStoreEvidenceID)
	if !isSQLState(err, "AM001") || replica != (RuntimeReplicaResult{}) {
		t.Fatalf("AM001 replica=%+v error=%v", replica, err)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:runtime_finish_scale_in", "rollback", "destroy")

	terminating := lifecycleReplicaProjection(identity.ReplicaID, "maintenance", "terminating", lifecycleStoreOperationID, lifecycleStoreExpected)
	runner, lease = claimOrderRunner(lifecycleReplicaValues(terminating))
	replica, err = lifecycleTerminate(&runtimeLifecycleTransport{runner: runner}, lifecycleStoreRequestID, "startup_failed")
	if err != nil || replica.State != "terminating" || lease.tx.functions != 1 {
		t.Fatalf("terminate replica=%+v error=%v functions=%d", replica, err, lease.tx.functions)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:runtime_begin_replica_termination", "finish", "commit", "release")

	maintenance := lifecycleReplicaProjection(identity.ReplicaID, "maintenance", "draining", lifecycleStoreOperationID, lifecycleStoreExpected)
	runner, lease = claimOrderRunner(lifecycleReplicaValues(maintenance))
	replica, err = (&runtimeLifecycleTransport{runner: runner}).PrepareMaintenanceDrain(context.Background(), lifecycleStoreExpected,
		lifecycleStoreOperationID, identity.ReplicaID, identity.InstanceID, identity.ReleaseDigest)
	if err != nil || replica.State != "draining" || lease.tx.functions != 1 {
		t.Fatalf("drain replica=%+v error=%v functions=%d", replica, err, lease.tx.functions)
	}
	assertEvents(t, lease.events, "acquire", "begin", "entry", "function:runtime_prepare_maintenance_drain", "finish", "commit", "release")
}

// lifecycleCommandPool returns a single-connection pool authenticated as
// aboutme_lifecycle_command, the only login allowed to execute the six fixed
// controller wrappers.
func lifecycleCommandPool(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	cfg.MinConns = 0
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, authErr := conn.Exec(ctx, `SET SESSION AUTHORIZATION aboutme_lifecycle_command`)
		return authErr
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// lifecycleLiveIdentity builds one distinct registered incarnation.
func lifecycleLiveIdentity(n int) RuntimeReplicaIdentity {
	return RuntimeReplicaIdentity{
		ReplicaID:            uuid.MustParse(fmt.Sprintf("00000000-0000-4000-8000-%012d", n)),
		InstanceID:           fmt.Sprintf("i-%017x", n),
		ContainerInstanceARN: fmt.Sprintf("arn:container:%d", n),
		CaddyTaskARN:         fmt.Sprintf("arn:task:caddy:%d", n),
		GoTaskARN:            fmt.Sprintf("arn:task:go:%d", n),
		NuxtTaskARN:          fmt.Sprintf("arn:task:nuxt:%d", n),
		ReleaseDigest:        "sha256:" + strings.Repeat("a", 64),
	}
}

// lifecycleLiveJoin registers and readies one serving or maintenance
// incarnation through the accepted registration surface.
func lifecycleLiveJoin(t *testing.T, store RuntimeReplicaRegistrationStore, identity RuntimeReplicaIdentity, kind string) {
	t.Helper()
	ctx := context.Background()
	register, ready := store.RegisterServing, store.MarkServingJoinReady
	if kind == "maintenance" {
		register, ready = store.RegisterMaintenance, store.MarkMaintenanceJoinReady
	}
	if result, err := register(ctx, identity); err != nil || result.State != "joining" {
		t.Fatalf("register %s: result=%+v error=%v", kind, result, err)
	}
	if result, err := ready(ctx, identity); err != nil || result.State != "joining" {
		t.Fatalf("ready %s: result=%+v error=%v", kind, result, err)
	}
}

// lifecycleControllerGeneration reads the current capacity singleton.
func lifecycleControllerGeneration(t *testing.T, admin *sql.DB) (int64, int16, string) {
	t.Helper()
	var generation int64
	var desired int16
	var operation string
	if err := admin.QueryRowContext(context.Background(),
		`SELECT controller_generation,desired_replicas,updated_by FROM public.runtime_capacity WHERE singleton`).Scan(&generation, &desired, &operation); err != nil {
		t.Fatal(err)
	}
	return generation, desired, operation
}

func TestRuntimeLifecycleTransportLiveRoleBoundaryInBothDirections(t *testing.T) {
	dsn, admin := testutil.NewMigratedTestDatabase(t)
	ctx := context.Background()
	appPool := newWriteRunnerAppPool(t, dsn, nil)
	registration := NewRuntimeReplicaRegistrationStore(appPool)
	first := lifecycleLiveIdentity(1)
	lifecycleLiveJoin(t, registration, first, "serving")

	commandPool := lifecycleCommandPool(t, dsn)
	lifecycle := NewRuntimeLifecycleTransport(commandPool)
	activated, err := lifecycle.ActivateReplicaCapacity(ctx, 1, "op-initial-serving", first.ReplicaID, first.InstanceID,
		first.ContainerInstanceARN, first.CaddyTaskARN, first.GoTaskARN, first.NuxtTaskARN, first.ReleaseDigest, nil)
	if err != nil || activated.State != "active" || activated.ReplicaKind != "serving" || activated.ControllerGeneration != 2 ||
		activated.ControllerOperationID != "op-initial-serving" || activated.Replayed || activated.DesiredReplicas != nil {
		t.Fatalf("activated=%+v error=%v", activated, err)
	}

	for _, test := range []struct {
		name string
		pool *pgxpool.Pool
	}{
		{"app", appPool},
		{"maintenance", newMaintenancePool(t, dsn)},
	} {
		t.Run("denies_"+test.name, func(t *testing.T) {
			denied, deniedErr := NewRuntimeLifecycleTransport(test.pool).PrepareScaleOut(ctx, 2, "op-forbidden-"+test.name)
			if !isSQLState(deniedErr, "42501") || denied != (RuntimeCapacityResult{}) {
				t.Fatalf("denied=%+v error=%v", denied, deniedErr)
			}
			second := lifecycleLiveIdentity(2)
			terminate, terminateErr := NewRuntimeLifecycleTransport(test.pool).BeginReplicaTermination(ctx, 2, "op-forbidden-intent-"+test.name,
				"req-forbidden-"+test.name, second.ReplicaID, second.InstanceID, second.ReleaseDigest, "startup_failed")
			if !isSQLState(terminateErr, "42501") || terminate != (RuntimeReplicaResult{}) {
				t.Fatalf("terminate=%+v error=%v", terminate, terminateErr)
			}
		})
	}

	// The other direction: the controller login cannot forge the app-owned
	// registration evidence its own actions consume.
	forged, err := NewRuntimeReplicaRegistrationStore(commandPool).RegisterServing(ctx, lifecycleLiveIdentity(3))
	if !isSQLState(err, "42501") || forged != (RuntimeReplicaResult{}) {
		t.Fatalf("forged registration=%+v error=%v", forged, err)
	}
	ready, err := NewRuntimeReplicaRegistrationStore(commandPool).MarkServingJoinReady(ctx, first)
	if !isSQLState(err, "42501") || ready != (RuntimeReplicaResult{}) {
		t.Fatalf("forged readiness=%+v error=%v", ready, err)
	}

	generation, desired, updatedBy := lifecycleControllerGeneration(t, admin)
	if generation != 2 || desired != 1 || updatedBy != "op-initial-serving" {
		t.Fatalf("denied roles changed capacity: generation=%d desired=%d updated_by=%q", generation, desired, updatedBy)
	}
	var operations int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM public.runtime_lifecycle_operations WHERE operation_id LIKE 'op-forbidden%'`).Scan(&operations); err != nil || operations != 0 {
		t.Fatalf("forbidden operations=%d error=%v", operations, err)
	}
}

// lifecyclePartitionState reports the enabled count and distinct operation
// evidence of one logical partition across all 24 policies.
func lifecyclePartitionState(t *testing.T, admin *sql.DB, partition int16) (int, int, string) {
	t.Helper()
	var rows, enabled int
	var operations string
	if err := admin.QueryRowContext(context.Background(),
		`SELECT count(*),count(*) FILTER (WHERE enabled),string_agg(DISTINCT operation_id,',') FROM public.shared_rate_partitions WHERE partition=$1`,
		partition).Scan(&rows, &enabled, &operations); err != nil {
		t.Fatal(err)
	}
	return rows, enabled, operations
}

// lifecycleFenceFixture records the fenced membership and EC2 proof that the
// fencing and proof slices will own. Finish-scale-in consumes them as already
// accepted durable evidence.
func lifecycleFenceFixture(t *testing.T, admin *sql.DB, identity RuntimeReplicaIdentity, evidenceID string) {
	t.Helper()
	claimLiveOwnerWrite(t, admin,
		fmt.Sprintf(`UPDATE public.runtime_replicas SET state='fenced',fenced_at=clock_timestamp() WHERE replica_id='%s'`, identity.ReplicaID),
		fmt.Sprintf(`INSERT INTO public.runtime_fencing_proofs(replica_id,instance_id,release_digest,adapter,request_id,evidence_id,requested_at,observed_terminated_at,observed_state,reclaimed_claim_count)`+
			` VALUES('%s','%s','%s','ec2_terminated_v1','request-%s','%s',clock_timestamp(),clock_timestamp(),'terminated',0)`,
			identity.ReplicaID, identity.InstanceID, identity.ReleaseDigest, evidenceID, evidenceID))
}

func TestRuntimeLifecycleTransportLiveOrdinaryScaleOutAndScaleIn(t *testing.T) {
	dsn, admin := testutil.NewMigratedTestDatabase(t)
	ctx := context.Background()
	registration := NewRuntimeReplicaRegistrationStore(newWriteRunnerAppPool(t, dsn, nil))
	lifecycle := NewRuntimeLifecycleTransport(lifecycleCommandPool(t, dsn))
	first, second := lifecycleLiveIdentity(11), lifecycleLiveIdentity(12)

	lifecycleLiveJoin(t, registration, first, "serving")
	if activated, err := lifecycle.ActivateReplicaCapacity(ctx, 1, "op-initial", first.ReplicaID, first.InstanceID,
		first.ContainerInstanceARN, first.CaddyTaskARN, first.GoTaskARN, first.NuxtTaskARN, first.ReleaseDigest, nil); err != nil || activated.State != "active" {
		t.Fatalf("initial activation=%+v error=%v", activated, err)
	}
	if rows, enabled, _ := lifecyclePartitionState(t, admin, 1); rows != 24 || enabled != 24 {
		t.Fatalf("partition 1 rows=%d enabled=%d after initial activation", rows, enabled)
	}
	if rows, enabled, _ := lifecyclePartitionState(t, admin, 2); rows != 24 || enabled != 0 {
		t.Fatalf("partition 2 rows=%d enabled=%d after initial activation", rows, enabled)
	}

	booked, err := lifecycle.PrepareScaleOut(ctx, 2, "op-scale-out")
	if err != nil || booked.DesiredReplicas != 2 || booked.ActiveServingReplicas != 1 || booked.ActiveMaintenanceReplicas != 0 ||
		!booked.Partition1Enabled || booked.Partition2Enabled || booked.ControllerGeneration != 3 || booked.Replayed ||
		booked.ControllerOperationID != "op-scale-out" || booked.WriteGate != nil || booked.WriteGeneration != nil {
		t.Fatalf("booked=%+v error=%v", booked, err)
	}
	retained := booked
	replayed, err := lifecycle.PrepareScaleOut(ctx, 2, "op-scale-out")
	if err != nil || !replayed.Replayed || replayed.DesiredReplicas != 2 || replayed.ControllerGeneration != 3 {
		t.Fatalf("replayed=%+v error=%v", replayed, err)
	}
	if generation, _, _ := lifecycleControllerGeneration(t, admin); generation != 3 {
		t.Fatalf("replay advanced the controller generation to %d", generation)
	}
	if stale, staleErr := lifecycle.PrepareScaleOut(ctx, 2, "op-stale"); !isSQLState(staleErr, "55000") || stale != (RuntimeCapacityResult{}) {
		t.Fatalf("stale generation=%+v error=%v", stale, staleErr)
	}
	var strayOperations int
	if strayErr := admin.QueryRowContext(ctx, `SELECT count(*) FROM public.runtime_lifecycle_operations WHERE operation_id='op-stale'`).Scan(&strayOperations); strayErr != nil || strayOperations != 0 {
		t.Fatalf("rejected action left %d operation rows: %v", strayOperations, strayErr)
	}

	lifecycleLiveJoin(t, registration, second, "serving")
	grown, err := lifecycle.ActivateReplicaCapacity(ctx, 3, "op-scale-out", second.ReplicaID, second.InstanceID,
		second.ContainerInstanceARN, second.CaddyTaskARN, second.GoTaskARN, second.NuxtTaskARN, second.ReleaseDigest, nil)
	if err != nil || grown.State != "active" || grown.ReplicaID != second.ReplicaID || grown.ControllerGeneration != 4 ||
		grown.DesiredReplicas != nil || grown.Partition2Enabled != nil {
		t.Fatalf("second activation=%+v error=%v", grown, err)
	}
	if rows, enabled, _ := lifecyclePartitionState(t, admin, 2); rows != 24 || enabled != 24 {
		t.Fatalf("partition 2 rows=%d enabled=%d after scale out", rows, enabled)
	}

	// Replay after a later successful action still answers with the stored
	// projection: one active serving replica, not the two that exist now.
	historical, err := lifecycle.PrepareScaleOut(ctx, 2, "op-scale-out")
	if err != nil || !historical.Replayed || historical.ActiveServingReplicas != 1 || historical.Partition2Enabled ||
		historical.ControllerGeneration != 3 || historical.CapacityGeneration != retained.CapacityGeneration {
		t.Fatalf("historical replay=%+v error=%v", historical, err)
	}
	if retained != booked {
		t.Fatalf("retained result changed after connection reuse: retained=%+v booked=%+v", retained, booked)
	}

	drained, err := lifecycle.PrepareScaleIn(ctx, 4, "op-scale-in", second.ReplicaID, second.InstanceID, second.ReleaseDigest)
	if err != nil || drained.State != "draining" || drained.ReplicaID != second.ReplicaID || drained.ControllerGeneration != 5 ||
		drained.DesiredReplicas != nil || drained.Partition1Enabled != nil {
		t.Fatalf("drained=%+v error=%v", drained, err)
	}
	terminating, err := lifecycle.BeginReplicaTermination(ctx, 5, "op-terminate", "req-terminate-1", second.ReplicaID,
		second.InstanceID, second.ReleaseDigest, "drain_failed")
	if err != nil || terminating.State != "terminating" || terminating.ControllerGeneration != 6 || terminating.DesiredReplicas != nil {
		t.Fatalf("terminating=%+v error=%v", terminating, err)
	}
	lifecycleFenceFixture(t, admin, second, "ec2-evidence-12")

	finished, err := lifecycle.FinishScaleIn(ctx, 6, "op-scale-in", second.ReplicaID, second.InstanceID, second.ReleaseDigest, nil, "ec2-evidence-12")
	if err != nil || finished.State != "fenced" || finished.ReplicaID != second.ReplicaID || finished.ControllerGeneration != 7 ||
		finished.DesiredReplicas == nil || *finished.DesiredReplicas != 1 ||
		finished.ActiveServingReplicas == nil || *finished.ActiveServingReplicas != 1 ||
		finished.ActiveMaintenanceReplicas == nil || *finished.ActiveMaintenanceReplicas != 0 ||
		finished.Partition1Enabled == nil || !*finished.Partition1Enabled ||
		finished.Partition2Enabled == nil || *finished.Partition2Enabled || finished.Replayed {
		t.Fatalf("finished=%+v error=%v", finished, err)
	}
	if rows, enabled, operations := lifecyclePartitionState(t, admin, 2); rows != 24 || enabled != 0 || operations != "op-scale-in" {
		t.Fatalf("partition 2 rows=%d enabled=%d operations=%q after finish", rows, enabled, operations)
	}
	if rows, enabled, _ := lifecyclePartitionState(t, admin, 1); rows != 24 || enabled != 24 {
		t.Fatalf("partition 1 rows=%d enabled=%d after finish", rows, enabled)
	}
	if generation, desired, updatedBy := lifecycleControllerGeneration(t, admin); generation != 7 || desired != 1 || updatedBy != "op-scale-in" {
		t.Fatalf("capacity after finish: generation=%d desired=%d updated_by=%q", generation, desired, updatedBy)
	}
	finishReplay, err := lifecycle.FinishScaleIn(ctx, 6, "op-scale-in", second.ReplicaID, second.InstanceID, second.ReleaseDigest, nil, "ec2-evidence-12")
	if err != nil || !finishReplay.Replayed || finishReplay.State != "fenced" ||
		finishReplay.ActiveServingReplicas == nil || *finishReplay.ActiveServingReplicas != 1 || finishReplay.ControllerGeneration != 7 {
		t.Fatalf("finish replay=%+v error=%v", finishReplay, err)
	}
	conflict, err := lifecycle.FinishScaleIn(ctx, 6, "op-scale-in", second.ReplicaID, second.InstanceID, second.ReleaseDigest, nil, "ec2-evidence-other")
	if !isSQLState(err, "AM002") || conflict != (RuntimeReplicaResult{}) {
		t.Fatalf("changed argument conflict=%+v error=%v", conflict, err)
	}
	// Each decoded result owns its own optional fields: the replay allocated
	// separate pointers and the first result still reads its own values after
	// the pooled connection was reused four times.
	if *finished.DesiredReplicas != 1 || *finished.ActiveServingReplicas != 1 || !*finished.Partition1Enabled || *finished.Partition2Enabled ||
		finished.DesiredReplicas == finishReplay.DesiredReplicas || finished.Partition1Enabled == finishReplay.Partition1Enabled {
		t.Fatalf("retained finish result changed after connection reuse: %+v", finished)
	}
}

// lifecycleMaintenanceWakeFixture records the maintenance-wake parent, its
// two completed wake steps and the capacity generations those steps advanced.
// The wake slice will own this surface; until then the ordinary maintenance
// actions consume it as already accepted durable rows.
func lifecycleMaintenanceWakeFixture(t *testing.T, admin *sql.DB, operationID string, beginExpected int64) {
	t.Helper()
	step := func(action string, expected int64, gate string, writeGeneration int64) string {
		return fmt.Sprintf(`INSERT INTO public.runtime_lifecycle_operation_steps(operation_id,action,workflow_kind,`+
			`expected_generation,result_generation,argument_digest,result_digest,result_kind,`+
			`desired_replicas,active_serving_replicas,active_maintenance_replicas,partition_1_enabled,partition_2_enabled,`+
			`capacity_generation,controller_generation,controller_operation_id,admission_enabled,lifecycle_phase,write_gate,write_generation)`+
			` VALUES('%s','%s','maintenance_wake',%d,%d,decode(repeat('%s',32),'hex'),decode(repeat('%s',32),'hex'),'capacity',`+
			`1,1,0,true,false,%d,%d,'%s',true,'online','%s',%d)`,
			operationID, action, expected, expected+1, "11", "12", expected+1, expected+1, operationID, gate, writeGeneration)
	}
	claimLiveOwnerWrite(t, admin,
		fmt.Sprintf(`INSERT INTO public.runtime_lifecycle_operations(operation_id,workflow_kind) VALUES('%s','maintenance_wake')`, operationID),
		step("begin_wake", beginExpected, "closing", 5),
		step("complete_wake", beginExpected+1, "open", 6),
		fmt.Sprintf(`UPDATE public.runtime_capacity SET generation=%d,controller_generation=%d,controller_operation_id='%s',`+
			`updated_by='%s',updated_at=clock_timestamp() WHERE singleton`, beginExpected+2, beginExpected+2, operationID, operationID))
}

func TestRuntimeLifecycleTransportLiveMaintenanceCampaign(t *testing.T) {
	dsn, admin := testutil.NewMigratedTestDatabase(t)
	ctx := context.Background()
	serving := lifecycleLiveIdentity(21)
	maintenance := lifecycleLiveIdentity(22)
	lifecycle := NewRuntimeLifecycleTransport(lifecycleCommandPool(t, dsn))

	lifecycleLiveJoin(t, NewRuntimeReplicaRegistrationStore(newWriteRunnerAppPool(t, dsn, nil)), serving, "serving")
	if activated, err := lifecycle.ActivateReplicaCapacity(ctx, 1, "op-initial", serving.ReplicaID, serving.InstanceID,
		serving.ContainerInstanceARN, serving.CaddyTaskARN, serving.GoTaskARN, serving.NuxtTaskARN, serving.ReleaseDigest, nil); err != nil || activated.State != "active" {
		t.Fatalf("initial activation=%+v error=%v", activated, err)
	}
	lifecycleMaintenanceWakeFixture(t, admin, "op-maintenance", 2)
	lifecycleLiveJoin(t, NewRuntimeReplicaRegistrationStore(newMaintenancePool(t, dsn)), maintenance, "maintenance")

	joined, err := lifecycle.ActivateReplicaCapacity(ctx, 4, "op-maintenance", maintenance.ReplicaID, maintenance.InstanceID,
		maintenance.ContainerInstanceARN, maintenance.CaddyTaskARN, maintenance.GoTaskARN, maintenance.NuxtTaskARN, maintenance.ReleaseDigest, nil)
	if err != nil || joined.State != "active" || joined.ReplicaKind != "maintenance" || joined.ControllerGeneration != 5 ||
		joined.DesiredReplicas != nil || joined.Partition1Enabled != nil {
		t.Fatalf("maintenance activation=%+v error=%v", joined, err)
	}
	assertServingUntouched := func(stage string) {
		t.Helper()
		if rows, enabled, operations := lifecyclePartitionState(t, admin, 1); rows != 24 || enabled != 24 || operations != "op-initial" {
			t.Fatalf("%s changed partition 1: rows=%d enabled=%d operations=%q", stage, rows, enabled, operations)
		}
		if rows, enabled, _ := lifecyclePartitionState(t, admin, 2); rows != 24 || enabled != 0 {
			t.Fatalf("%s changed partition 2: rows=%d enabled=%d", stage, rows, enabled)
		}
		if _, desired, _ := lifecycleControllerGeneration(t, admin); desired != 1 {
			t.Fatalf("%s changed desired to %d", stage, desired)
		}
	}
	assertServingUntouched("maintenance activation")

	draining, err := lifecycle.PrepareMaintenanceDrain(ctx, 5, "op-maintenance", maintenance.ReplicaID, maintenance.InstanceID, maintenance.ReleaseDigest)
	if err != nil || draining.State != "draining" || draining.ReplicaKind != "maintenance" || draining.ControllerGeneration != 6 ||
		draining.ReplicaID != maintenance.ReplicaID || draining.DesiredReplicas != nil || draining.Partition2Enabled != nil || draining.Replayed {
		t.Fatalf("maintenance drain=%+v error=%v", draining, err)
	}
	assertServingUntouched("maintenance drain")

	replayed, err := lifecycle.PrepareMaintenanceDrain(ctx, 5, "op-maintenance", maintenance.ReplicaID, maintenance.InstanceID, maintenance.ReleaseDigest)
	if err != nil || !replayed.Replayed || replayed.State != "draining" || replayed.ControllerGeneration != 6 {
		t.Fatalf("maintenance drain replay=%+v error=%v", replayed, err)
	}
	if generation, _, _ := lifecycleControllerGeneration(t, admin); generation != 6 {
		t.Fatalf("maintenance drain replay advanced the controller generation to %d", generation)
	}
	missing, err := lifecycle.PrepareMaintenanceDrain(ctx, 6, "op-no-parent", maintenance.ReplicaID, maintenance.InstanceID, maintenance.ReleaseDigest)
	if !isSQLState(err, "55000") || missing != (RuntimeReplicaResult{}) {
		t.Fatalf("parentless drain=%+v error=%v", missing, err)
	}
}

// lifecycleCancelAfterEntryTx cancels the caller's context once the write
// barrier is open, so cancellation arrives between entry and commit rather
// than before the transaction exists.
type lifecycleCancelAfterEntryTx struct {
	pgx.Tx
	cancel context.CancelFunc
}

func (tx *lifecycleCancelAfterEntryTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	tag, err := tx.Tx.Exec(ctx, sql, arguments...)
	if err == nil && strings.Contains(sql, "runtime_enter_write") {
		tx.cancel()
	}
	return tag, err
}

// lifecycleLiveJoined registers and readies one serving incarnation and
// returns it with the admin pool of its database.
func lifecycleLiveJoined(t *testing.T, n int) (string, *sql.DB, RuntimeReplicaIdentity) {
	t.Helper()
	dsn, admin := testutil.NewMigratedTestDatabase(t)
	identity := lifecycleLiveIdentity(n)
	lifecycleLiveJoin(t, NewRuntimeReplicaRegistrationStore(newWriteRunnerAppPool(t, dsn, nil)), identity, "serving")
	return dsn, admin, identity
}

// lifecycleLiveActivate runs the initial activation through the supplied
// runner with the fixed bootstrap generation.
func lifecycleLiveActivate(ctx context.Context, runner WriteTxRunner, identity RuntimeReplicaIdentity) (RuntimeReplicaResult, error) {
	return (&runtimeLifecycleTransport{runner: runner}).ActivateReplicaCapacity(ctx, 1, "op-initial", identity.ReplicaID,
		identity.InstanceID, identity.ContainerInstanceARN, identity.CaddyTaskARN, identity.GoTaskARN,
		identity.NuxtTaskARN, identity.ReleaseDigest, nil)
}

// lifecycleLiveState reports the state of one incarnation, the controller
// generation and the recorded step count.
func lifecycleLiveState(t *testing.T, admin *sql.DB, identity RuntimeReplicaIdentity) (string, int64, int) {
	t.Helper()
	var state string
	var generation int64
	var steps int
	if err := admin.QueryRowContext(context.Background(),
		`SELECT (SELECT state FROM public.runtime_replicas WHERE replica_id=$1),`+
			`(SELECT controller_generation FROM public.runtime_capacity WHERE singleton),`+
			`(SELECT count(*) FROM public.runtime_lifecycle_operation_steps)`, identity.ReplicaID).Scan(&state, &generation, &steps); err != nil {
		t.Fatal(err)
	}
	return state, generation, steps
}

func TestRuntimeLifecycleTransportLiveFinishFailureRollsBackAndReturnsZero(t *testing.T) {
	dsn, admin, identity := lifecycleLiveJoined(t, 31)
	wantErr := errors.New("simulated lost lifecycle finish response")
	runner := liveWrappedRunner(lifecycleCommandPool(t, dsn), func(tx pgx.Tx) pgx.Tx {
		return &registrationFinishResponseLostTx{Tx: tx, err: wantErr}
	})
	result, err := lifecycleLiveActivate(context.Background(), runner, identity)
	if !errors.Is(err, wantErr) || result != (RuntimeReplicaResult{}) {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if state, generation, steps := lifecycleLiveState(t, admin, identity); state != "joining" || generation != 1 || steps != 0 {
		t.Fatalf("rolled-back state=%q generation=%d steps=%d", state, generation, steps)
	}
}

func TestRuntimeLifecycleTransportLiveCommitAmbiguityReturnsZeroWithoutAuthority(t *testing.T) {
	dsn, admin, identity := lifecycleLiveJoined(t, 32)
	wantErr := errors.New("simulated lost lifecycle commit response")
	runner := liveWrappedRunner(lifecycleCommandPool(t, dsn), func(tx pgx.Tx) pgx.Tx {
		return &commitResponseLostTx{Tx: tx, err: wantErr}
	})
	result, err := lifecycleLiveActivate(context.Background(), runner, identity)
	if !errors.Is(err, wantErr) || result != (RuntimeReplicaResult{}) {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	// The action committed. The caller receives no authority for it, and the
	// transport neither retries nor replays on its own.
	if state, generation, steps := lifecycleLiveState(t, admin, identity); state != "active" || generation != 2 || steps != 1 {
		t.Fatalf("committed state=%q generation=%d steps=%d", state, generation, steps)
	}
}

func TestRuntimeLifecycleTransportLiveCanceledContextReturnsZeroWithoutWriting(t *testing.T) {
	dsn, admin, identity := lifecycleLiveJoined(t, 33)
	pool := lifecycleCommandPool(t, dsn)
	dead, cancelDead := context.WithCancel(context.Background())
	cancelDead()
	result, err := lifecycleLiveActivate(dead, NewWriteTxRunner(pool), identity)
	if !errors.Is(err, context.Canceled) || result != (RuntimeReplicaResult{}) {
		t.Fatalf("pre-canceled result=%+v error=%v", result, err)
	}
	if state, generation, steps := lifecycleLiveState(t, admin, identity); state != "joining" || generation != 1 || steps != 0 {
		t.Fatalf("pre-canceled state=%q generation=%d steps=%d", state, generation, steps)
	}

	// Cancellation arriving after the write barrier opened exercises the
	// rollback and lease-destroy path instead of the acquire path.
	live, cancelLive := context.WithCancel(context.Background())
	defer cancelLive()
	runner := liveWrappedRunner(pool, func(tx pgx.Tx) pgx.Tx {
		return &lifecycleCancelAfterEntryTx{Tx: tx, cancel: cancelLive}
	})
	result, err = lifecycleLiveActivate(live, runner, identity)
	if !errors.Is(err, context.Canceled) || result != (RuntimeReplicaResult{}) {
		t.Fatalf("mid-transaction result=%+v error=%v", result, err)
	}
	if state, generation, steps := lifecycleLiveState(t, admin, identity); state != "joining" || generation != 1 || steps != 0 {
		t.Fatalf("mid-transaction state=%q generation=%d steps=%d", state, generation, steps)
	}
	// The pool still works after the canceled transaction was discarded.
	if activated, activateErr := lifecycleLiveActivate(context.Background(), NewWriteTxRunner(pool), identity); activateErr != nil || activated.State != "active" {
		t.Fatalf("recovered activation=%+v error=%v", activated, activateErr)
	}
}

// TestRuntimeLifecycleTransportInterfaceExposesOnlyScalars pins the fixed
// transport surface: six context-first scalar methods returning one owned
// result type, with no callback, Queries, transaction, connection, digest or
// timestamp anywhere in the signature.
func TestRuntimeLifecycleTransportInterfaceExposesOnlyScalars(t *testing.T) {
	surface := reflect.TypeOf((*RuntimeLifecycleTransport)(nil)).Elem()
	if surface.NumMethod() != 6 {
		t.Fatalf("transport exposes %d methods", surface.NumMethod())
	}
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	uuidType := reflect.TypeOf(uuid.UUID{})
	results := map[reflect.Type]bool{reflect.TypeOf(RuntimeCapacityResult{}): true, reflect.TypeOf(RuntimeReplicaResult{}): true}
	scalars := map[reflect.Kind]bool{reflect.Int64: true, reflect.String: true}
	for i := 0; i < surface.NumMethod(); i++ {
		method := surface.Method(i)
		if method.Type.NumIn() < 3 || method.Type.In(0) != contextType {
			t.Fatalf("%s is not context-first: %v", method.Name, method.Type)
		}
		for argument := 1; argument < method.Type.NumIn(); argument++ {
			parameter := method.Type.In(argument)
			if parameter.Kind() == reflect.Pointer {
				parameter = parameter.Elem()
			}
			if parameter != uuidType && !scalars[parameter.Kind()] {
				t.Fatalf("%s argument %d is %v", method.Name, argument, method.Type.In(argument))
			}
		}
		if method.Type.NumOut() != 2 || !results[method.Type.Out(0)] || method.Type.Out(1) != errorType {
			t.Fatalf("%s returns %v", method.Name, method.Type)
		}
	}
}

func TestRuntimeLifecycleTransportLiveCorruptStepRetiresBackendAndReturnsZero(t *testing.T) {
	dsn, admin, identity := lifecycleLiveJoined(t, 34)
	ctx := context.Background()
	pool := lifecycleCommandPool(t, dsn)
	if activated, err := lifecycleLiveActivate(ctx, NewWriteTxRunner(pool), identity); err != nil || activated.State != "active" {
		t.Fatalf("activated=%+v error=%v", activated, err)
	}
	var oldPID int32
	if err := NewWriteTxRunner(pool).ExecWrite(ctx, func(q *Queries) error {
		return q.db.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&oldPID)
	}); err != nil {
		t.Fatal(err)
	}
	claimLiveOwnerWrite(t, admin,
		`ALTER TABLE public.runtime_lifecycle_operation_steps DISABLE TRIGGER runtime_lifecycle_steps_immutable`,
		`UPDATE public.runtime_lifecycle_operation_steps SET result_digest=decode(repeat('ff',32),'hex') WHERE operation_id='op-initial'`,
		`ALTER TABLE public.runtime_lifecycle_operation_steps ENABLE TRIGGER runtime_lifecycle_steps_immutable`)

	result, err := lifecycleLiveActivate(ctx, NewWriteTxRunner(pool), identity)
	if !isSQLState(err, "AM001") || result != (RuntimeReplicaResult{}) {
		t.Fatalf("corrupt replay result=%+v error=%v", result, err)
	}
	assertBackendGone(t, admin, oldPID)
	var replacementPID int32
	if err := NewWriteTxRunner(pool).ExecWrite(ctx, func(q *Queries) error {
		return q.db.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&replacementPID)
	}); err != nil {
		t.Fatal(err)
	}
	if replacementPID == oldPID {
		t.Fatalf("poisoned backend PID %d was reused", oldPID)
	}
}
