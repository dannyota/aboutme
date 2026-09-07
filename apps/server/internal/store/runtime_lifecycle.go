package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RuntimeCapacityResult is the owned, validated result of one fixed lifecycle
// action whose result kind is capacity. WriteGate and WriteGeneration are
// absent for every ordinary action; the same type later carries both of them
// for wake.
type RuntimeCapacityResult struct {
	DesiredReplicas           int16
	ActiveServingReplicas     int16
	ActiveMaintenanceReplicas int16
	Partition1Enabled         bool
	Partition2Enabled         bool
	AdmissionEnabled          bool
	Replayed                  bool
	CapacityGeneration        int64
	ControllerGeneration      int64
	ControllerOperationID     string
	LifecyclePhase            string
	WriteGate                 *string
	WriteGeneration           *int64
}

// RuntimeLifecycleTransport executes the six fixed ordinary lifecycle
// controller actions. Each call runs one generated function inside the write
// barrier and returns a decoded result only after the transaction finished
// and committed.
type RuntimeLifecycleTransport interface {
	PrepareScaleOut(context.Context, int64, string) (RuntimeCapacityResult, error)
	ActivateReplicaCapacity(context.Context, int64, string, uuid.UUID, string,
		string, string, string, string, string, *uuid.UUID) (RuntimeReplicaResult, error)
	PrepareScaleIn(context.Context, int64, string, uuid.UUID, string,
		string) (RuntimeReplicaResult, error)
	FinishScaleIn(context.Context, int64, string, uuid.UUID, string, string,
		*string, string) (RuntimeReplicaResult, error)
	PrepareMaintenanceDrain(context.Context, int64, string, uuid.UUID, string,
		string) (RuntimeReplicaResult, error)
	BeginReplicaTermination(context.Context, int64, string, string, uuid.UUID,
		string, string, string) (RuntimeReplicaResult, error)
}

// runtimeLifecycleReplicaProjection is the shared generated result shape of
// the five fixed replica-result queries.
type runtimeLifecycleReplicaProjection = RuntimeActivateReplicaCapacityRow

type runtimeLifecycleTransport struct{ runner WriteTxRunner }

// NewRuntimeLifecycleTransport constructs a lifecycle transport backed by pool.
func NewRuntimeLifecycleTransport(pool *pgxpool.Pool) RuntimeLifecycleTransport {
	return &runtimeLifecycleTransport{runner: NewWriteTxRunner(pool)}
}

// PrepareScaleOut books the second serving slot of the fixed fleet.
func (s *runtimeLifecycleTransport) PrepareScaleOut(ctx context.Context, expected int64, operationID string) (RuntimeCapacityResult, error) {
	if err := validateLifecycleCall(ctx, expected, operationID); err != nil {
		return RuntimeCapacityResult{}, err
	}
	return runLifecycleOperation(ctx, s.writeRunner(), func(q *Queries) (RuntimePrepareScaleOutRow, error) {
		return q.RuntimePrepareScaleOut(ctx, RuntimePrepareScaleOutParams{ExpectedControllerGeneration: expected, OperationID: operationID})
	}, func(p RuntimePrepareScaleOutRow) (RuntimeCapacityResult, error) {
		return decodeRuntimeLifecycleScaleOut(p, operationID, expected)
	})
}

// ActivateReplicaCapacity moves one registered, join-ready incarnation into
// the fleet under the workflow its operation parent fixes.
func (s *runtimeLifecycleTransport) ActivateReplicaCapacity(ctx context.Context, expected int64, operationID string,
	replicaID uuid.UUID, instanceID, containerInstanceARN, caddyTaskARN, goTaskARN, nuxtTaskARN, releaseDigest string,
	replaced *uuid.UUID) (RuntimeReplicaResult, error) {
	var zero RuntimeReplicaResult
	if err := validateLifecycleCall(ctx, expected, operationID); err != nil {
		return zero, err
	}
	if err := validateRuntimeReplicaIdentity(RuntimeReplicaIdentity{ReplicaID: replicaID, InstanceID: instanceID,
		ContainerInstanceARN: containerInstanceARN, CaddyTaskARN: caddyTaskARN, GoTaskARN: goTaskARN,
		NuxtTaskARN: nuxtTaskARN, ReleaseDigest: releaseDigest}); err != nil {
		return zero, err
	}
	owned, err := ownLifecycleReplacement(replaced, replicaID)
	if err != nil {
		return zero, err
	}
	return runLifecycleOperation(ctx, s.writeRunner(), func(q *Queries) (runtimeLifecycleReplicaProjection, error) {
		return q.RuntimeActivateReplicaCapacity(ctx, RuntimeActivateReplicaCapacityParams{
			ExpectedControllerGeneration: expected, OperationID: operationID, ReplicaID: replicaID,
			InstanceID: instanceID, ContainerInstanceArn: containerInstanceARN, CaddyTaskArn: caddyTaskARN,
			GoTaskArn: goTaskARN, NuxtTaskArn: nuxtTaskARN, ReleaseDigest: releaseDigest, ReplacedReplicaID: owned})
	}, func(p runtimeLifecycleReplicaProjection) (RuntimeReplicaResult, error) {
		return decodeRuntimeLifecycleReplica(p, operationID, expected, replicaID, runtimeLifecycleActivateRules)
	})
}

// PrepareScaleIn selects the exact active serving replica that leaves the
// fleet and moves it to draining.
func (s *runtimeLifecycleTransport) PrepareScaleIn(ctx context.Context, expected int64, operationID string,
	replicaID uuid.UUID, instanceID, releaseDigest string) (RuntimeReplicaResult, error) {
	var zero RuntimeReplicaResult
	if err := validateLifecycleTarget(ctx, expected, operationID, replicaID, instanceID, releaseDigest); err != nil {
		return zero, err
	}
	return runLifecycleOperation(ctx, s.writeRunner(), func(q *Queries) (runtimeLifecycleReplicaProjection, error) {
		row, err := q.RuntimePrepareScaleIn(ctx, RuntimePrepareScaleInParams{ExpectedControllerGeneration: expected,
			OperationID: operationID, ReplicaID: replicaID, InstanceID: instanceID, ReleaseDigest: releaseDigest})
		return runtimeLifecycleReplicaProjection(row), err
	}, func(p runtimeLifecycleReplicaProjection) (RuntimeReplicaResult, error) {
		return decodeRuntimeLifecycleReplica(p, operationID, expected, replicaID, runtimeLifecycleScaleInRules)
	})
}

// FinishScaleIn retires the drained serving replica against its exact leave
// or fencing evidence and disables the second logical partition.
func (s *runtimeLifecycleTransport) FinishScaleIn(ctx context.Context, expected int64, operationID string,
	replicaID uuid.UUID, instanceID, releaseDigest string, leaveOperationID *string, fencingEvidenceID string) (RuntimeReplicaResult, error) {
	var zero RuntimeReplicaResult
	if err := validateLifecycleTarget(ctx, expected, operationID, replicaID, instanceID, releaseDigest); err != nil {
		return zero, err
	}
	if !runtimePrintableText(fencingEvidenceID, 128) {
		return zero, errors.New("store: invalid lifecycle fencing evidence identity")
	}
	owned, err := ownLifecycleEvidence(leaveOperationID)
	if err != nil {
		return zero, err
	}
	return runLifecycleOperation(ctx, s.writeRunner(), func(q *Queries) (runtimeLifecycleReplicaProjection, error) {
		row, queryErr := q.RuntimeFinishScaleIn(ctx, RuntimeFinishScaleInParams{ExpectedControllerGeneration: expected,
			OperationID: operationID, ReplicaID: replicaID, InstanceID: instanceID, ReleaseDigest: releaseDigest,
			LeaveOperationID: owned, FencingEvidenceID: fencingEvidenceID})
		return runtimeLifecycleReplicaProjection(row), queryErr
	}, func(p runtimeLifecycleReplicaProjection) (RuntimeReplicaResult, error) {
		return decodeRuntimeLifecycleReplica(p, operationID, expected, replicaID, runtimeLifecycleFinishRules)
	})
}

// PrepareMaintenanceDrain moves the one active maintenance replica to
// draining. It inspects neither desired capacity nor the rate partitions.
func (s *runtimeLifecycleTransport) PrepareMaintenanceDrain(ctx context.Context, expected int64, operationID string,
	replicaID uuid.UUID, instanceID, releaseDigest string) (RuntimeReplicaResult, error) {
	var zero RuntimeReplicaResult
	if err := validateLifecycleTarget(ctx, expected, operationID, replicaID, instanceID, releaseDigest); err != nil {
		return zero, err
	}
	return runLifecycleOperation(ctx, s.writeRunner(), func(q *Queries) (runtimeLifecycleReplicaProjection, error) {
		row, err := q.RuntimePrepareMaintenanceDrain(ctx, RuntimePrepareMaintenanceDrainParams{ExpectedControllerGeneration: expected,
			OperationID: operationID, ReplicaID: replicaID, InstanceID: instanceID, ReleaseDigest: releaseDigest})
		return runtimeLifecycleReplicaProjection(row), err
	}, func(p runtimeLifecycleReplicaProjection) (RuntimeReplicaResult, error) {
		return decodeRuntimeLifecycleReplica(p, operationID, expected, replicaID, runtimeLifecycleDrainRules)
	})
}

// BeginReplicaTermination records one immutable request-bound termination
// intent and moves the exact incarnation to terminating.
func (s *runtimeLifecycleTransport) BeginReplicaTermination(ctx context.Context, expected int64, operationID, requestID string,
	replicaID uuid.UUID, instanceID, releaseDigest, reason string) (RuntimeReplicaResult, error) {
	var zero RuntimeReplicaResult
	if err := validateLifecycleTarget(ctx, expected, operationID, replicaID, instanceID, releaseDigest); err != nil {
		return zero, err
	}
	if !runtimePrintableText(requestID, 128) {
		return zero, errors.New("store: invalid lifecycle termination request identity")
	}
	if !runtimeLifecycleReasons[reason] {
		return zero, errors.New("store: invalid lifecycle termination reason")
	}
	return runLifecycleOperation(ctx, s.writeRunner(), func(q *Queries) (runtimeLifecycleReplicaProjection, error) {
		row, err := q.RuntimeBeginReplicaTermination(ctx, RuntimeBeginReplicaTerminationParams{ExpectedControllerGeneration: expected,
			OperationID: operationID, RequestID: requestID, ReplicaID: replicaID, InstanceID: instanceID,
			ReleaseDigest: releaseDigest, Reason: reason})
		return runtimeLifecycleReplicaProjection(row), err
	}, func(p runtimeLifecycleReplicaProjection) (RuntimeReplicaResult, error) {
		return decodeRuntimeLifecycleReplica(p, operationID, expected, replicaID, runtimeLifecycleTerminateRules)
	})
}

// ownLifecycleEvidence copies the caller's optional evidence identifier and
// bounds it exactly as the SQL does, so no live caller pointer reaches the
// driver.
func ownLifecycleEvidence(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	owned := *value
	if !runtimePrintableText(owned, 128) {
		return nil, errors.New("store: invalid lifecycle leave operation identity")
	}
	return &owned, nil
}

// ownLifecycleReplacement copies the caller's optional replacement
// predecessor, so no live caller pointer reaches the driver.
func ownLifecycleReplacement(replaced *uuid.UUID, replicaID uuid.UUID) (*uuid.UUID, error) {
	if replaced == nil {
		return nil, nil
	}
	owned := *replaced
	if owned == uuid.Nil || owned == replicaID {
		return nil, errors.New("store: invalid lifecycle replacement identity")
	}
	return &owned, nil
}

// writeRunner returns the configured runner, or nil when the transport was
// built without one.
func (s *runtimeLifecycleTransport) writeRunner() WriteTxRunner {
	if s == nil {
		return nil
	}
	return s.runner
}

// runLifecycleOperation executes one generated lifecycle function inside the
// write runner. The row is validated and copied before the callback returns,
// so an invalid result rolls the transaction back, and no value escapes
// unless finish and commit both succeed.
func runLifecycleOperation[P, R any](ctx context.Context, runner WriteTxRunner, call func(*Queries) (P, error), decode func(P) (R, error)) (R, error) {
	var zero R
	if runner == nil {
		return zero, errors.New("store: nil lifecycle transport runner")
	}
	var decoded R
	err := runner.ExecWrite(ctx, func(q *Queries) error {
		projected, callErr := call(q)
		if callErr != nil {
			return fmt.Errorf("store: execute lifecycle operation: %w", callErr)
		}
		decoded, callErr = decode(projected)
		return callErr
	})
	if err != nil {
		return zero, err
	}
	return decoded, nil
}

// validateLifecycleCall rejects a nil context and the two scalars every fixed
// action takes, before the runner is touched.
func validateLifecycleCall(ctx context.Context, expected int64, operationID string) error {
	if ctx == nil {
		return errors.New("store: nil lifecycle context")
	}
	if expected <= 0 || !runtimePrintableText(operationID, 128) {
		return errors.New("store: invalid lifecycle operation identity")
	}
	return nil
}

// validateLifecycleTarget rejects a nil context, the two fixed scalars and
// the replica tuple the SQL requires nonnull, before the runner is touched.
func validateLifecycleTarget(ctx context.Context, expected int64, operationID string, replicaID uuid.UUID, instanceID, releaseDigest string) error {
	if err := validateLifecycleCall(ctx, expected, operationID); err != nil {
		return err
	}
	if replicaID == uuid.Nil || !runtimeInstanceIDPattern.MatchString(instanceID) || !runtimeReleaseDigestPattern.MatchString(releaseDigest) {
		return errors.New("store: invalid lifecycle replica identity")
	}
	return nil
}

// runtimeLifecycleReasons is the closed termination-reason enum SQL accepts.
var runtimeLifecycleReasons = map[string]bool{"startup_failed": true, "readiness_failed": true, "drain_failed": true}

// runtimeLifecyclePhases is the closed capacity phase enum of migration 15.
var runtimeLifecyclePhases = map[string]bool{"offline": true, "starting": true, "online": true, "stopping": true}

// validateLifecycleHeader checks the header every fixed action result
// carries. The ledger keys controller_operation_id to the operation ID and
// result_generation to one above the expected controller generation, so a
// result that names another operation or another generation is corrupt
// whether it is fresh or replayed. The subtraction avoids overflowing an
// expected generation at the bigint bound.
func validateLifecycleHeader(operationID string, expected, capacityGeneration, controllerGeneration int64, controllerOperationID, phase string, admission bool) error {
	if capacityGeneration <= 0 || controllerGeneration <= 0 || controllerGeneration-1 != expected {
		return errors.New("store: invalid lifecycle result generation")
	}
	if controllerOperationID != operationID || !runtimeLifecyclePhases[phase] || admission != (phase == "online") {
		return errors.New("store: invalid lifecycle result evidence")
	}
	return nil
}

// decodeRuntimeLifecycleScaleOut validates the fixed prepare-scale-out
// matrix: the action books desired two behind the one active serving replica,
// keeps partition 1 enabled, leaves partition 2 disabled, and carries neither
// wake field.
func decodeRuntimeLifecycleScaleOut(p RuntimePrepareScaleOutRow, operationID string, expected int64) (RuntimeCapacityResult, error) {
	var zero RuntimeCapacityResult
	if err := validateLifecycleHeader(operationID, expected, p.CapacityGeneration, p.ControllerGeneration, p.ControllerOperationID, p.LifecyclePhase, p.AdmissionEnabled); err != nil {
		return zero, err
	}
	if p.WriteGatePresent || p.WriteGenerationPresent {
		return zero, errors.New("store: unexpected lifecycle write gate")
	}
	if p.DesiredReplicas != 2 || p.ActiveServingReplicas != 1 || p.ActiveMaintenanceReplicas < 0 ||
		!p.Partition1Enabled || p.Partition2Enabled {
		return zero, errors.New("store: invalid lifecycle capacity result")
	}
	return RuntimeCapacityResult{
		DesiredReplicas: p.DesiredReplicas, ActiveServingReplicas: p.ActiveServingReplicas,
		ActiveMaintenanceReplicas: p.ActiveMaintenanceReplicas, Partition1Enabled: p.Partition1Enabled,
		Partition2Enabled: p.Partition2Enabled, AdmissionEnabled: p.AdmissionEnabled, Replayed: p.Replayed,
		CapacityGeneration: p.CapacityGeneration, ControllerGeneration: p.ControllerGeneration,
		ControllerOperationID: p.ControllerOperationID, LifecyclePhase: p.LifecyclePhase,
	}, nil
}

// runtimeLifecycleReplicaRules fixes the action-shaped result matrix of one
// replica-result method: the immutable replica kinds and the terminal state
// its ledger row may carry, and whether the action publishes the capacity
// counts and partition flags. Finish-scale-in is the only ordinary action
// that publishes them.
type runtimeLifecycleReplicaRules struct {
	kinds    []string
	states   []string
	capacity bool
}

var (
	runtimeLifecycleActivateRules  = runtimeLifecycleReplicaRules{kinds: []string{"serving", "maintenance"}, states: []string{"active"}}
	runtimeLifecycleScaleInRules   = runtimeLifecycleReplicaRules{kinds: []string{"serving"}, states: []string{"draining"}}
	runtimeLifecycleFinishRules    = runtimeLifecycleReplicaRules{kinds: []string{"serving"}, states: []string{"left", "fenced"}, capacity: true}
	runtimeLifecycleDrainRules     = runtimeLifecycleReplicaRules{kinds: []string{"maintenance"}, states: []string{"draining"}}
	runtimeLifecycleTerminateRules = runtimeLifecycleReplicaRules{kinds: []string{"serving", "maintenance"}, states: []string{"terminating"}}
)

func runtimeLifecycleAllows(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// decodeRuntimeLifecycleReplica validates one replica-result row against the
// fixed matrix of its action and returns an owned copy. Every optional field
// is copied out of a local, so the result aliases no generated row.
func decodeRuntimeLifecycleReplica(p runtimeLifecycleReplicaProjection, operationID string, expected int64, replicaID uuid.UUID, rules runtimeLifecycleReplicaRules) (RuntimeReplicaResult, error) {
	var zero RuntimeReplicaResult
	if err := validateLifecycleHeader(operationID, expected, p.CapacityGeneration, p.ControllerGeneration, p.ControllerOperationID, p.LifecyclePhase, p.AdmissionEnabled); err != nil {
		return zero, err
	}
	if p.ReplicaID != replicaID || !runtimeLifecycleAllows(rules.kinds, p.ReplicaKind) || !runtimeLifecycleAllows(rules.states, p.State) {
		return zero, errors.New("store: invalid lifecycle replica result")
	}
	if p.DesiredReplicasPresent != rules.capacity || p.ActiveServingReplicasPresent != rules.capacity ||
		p.ActiveMaintenanceReplicasPresent != rules.capacity || p.Partition1EnabledPresent != rules.capacity ||
		p.Partition2EnabledPresent != rules.capacity {
		return zero, errors.New("store: invalid lifecycle capacity presence")
	}
	result := RuntimeReplicaResult{ReplicaID: p.ReplicaID, ReplicaKind: p.ReplicaKind, State: p.State,
		CapacityGeneration: p.CapacityGeneration, ControllerGeneration: p.ControllerGeneration,
		ControllerOperationID: p.ControllerOperationID, AdmissionEnabled: p.AdmissionEnabled,
		LifecyclePhase: p.LifecyclePhase, Replayed: p.Replayed}
	if !rules.capacity {
		return result, nil
	}
	// Finish-scale-in retires the second serving slot: desired one, zero or
	// one surviving serving replica, the preserved partition 1 and the
	// disabled partition 2.
	if p.DesiredReplicasValue != 1 || p.ActiveServingReplicasValue < 0 || p.ActiveServingReplicasValue > 1 ||
		p.ActiveMaintenanceReplicasValue < 0 || !p.Partition1EnabledValue || p.Partition2EnabledValue {
		return zero, errors.New("store: invalid lifecycle capacity result")
	}
	desired, serving, maintenance := p.DesiredReplicasValue, p.ActiveServingReplicasValue, p.ActiveMaintenanceReplicasValue
	partition1, partition2 := p.Partition1EnabledValue, p.Partition2EnabledValue
	result.DesiredReplicas, result.ActiveServingReplicas, result.ActiveMaintenanceReplicas = &desired, &serving, &maintenance
	result.Partition1Enabled, result.Partition2Enabled = &partition1, &partition2
	return result, nil
}
