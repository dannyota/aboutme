package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RuntimeLeaveResult is the owned, validated result of one fixed graceful
// leave. Both joined counts are structurally zero: SQL records a receipt only
// after it has proven the exact replica owns no live claim and no closing or
// unresolved public transition.
type RuntimeLeaveResult struct {
	ReplicaID             uuid.UUID
	State                 string
	ReceiptOperationID    string
	ControllerGeneration  int64
	JoinedTransitionCount int32
	JoinedClaimCount      int32
	RecordedAt            time.Time
	Replayed              bool
}

// RuntimeFenceResult is the owned, validated result of one exact EC2
// termination proof. ReclaimedClaimCount is the historical count of claim
// parents this proof moved out of waiting or running, never a recount of the
// replica's current claims.
type RuntimeFenceResult struct {
	ReplicaID           uuid.UUID
	State               string
	EvidenceID          string
	ReclaimedClaimCount int32
	RecordedAt          time.Time
	Replayed            bool
}

// RuntimeMembershipEvidenceTransport executes the two fixed-kind graceful
// leave wrappers and the exact EC2 termination proof. Each call runs one
// generated function inside the write barrier and returns a decoded result
// only after the transaction finished and committed.
type RuntimeMembershipEvidenceTransport interface {
	FinishServingGracefulLeave(context.Context, uuid.UUID, string, string, int64, string) (RuntimeLeaveResult, error)
	FinishMaintenanceGracefulLeave(context.Context, uuid.UUID, string, string, int64, string) (RuntimeLeaveResult, error)
	RecordEC2Termination(context.Context, uuid.UUID, string, string, string, string, time.Time, time.Time,
		string) (RuntimeFenceResult, error)
}

// runtimeMembershipLeaveProjection is the shared generated result shape of the
// two fixed graceful-leave queries.
type runtimeMembershipLeaveProjection = RuntimeFinishServingGracefulLeaveRow

type runtimeMembershipEvidenceTransport struct{ runner WriteTxRunner }

// NewRuntimeMembershipEvidenceTransport constructs a membership evidence
// transport backed by pool.
func NewRuntimeMembershipEvidenceTransport(pool *pgxpool.Pool) RuntimeMembershipEvidenceTransport {
	return &runtimeMembershipEvidenceTransport{runner: NewWriteTxRunner(pool)}
}

// FinishServingGracefulLeave records the leave of the exact drained serving
// replica against its immutable prepare_scale_in evidence.
func (s *runtimeMembershipEvidenceTransport) FinishServingGracefulLeave(ctx context.Context, replicaID uuid.UUID,
	instanceID, releaseDigest string, expected int64, operationID string) (RuntimeLeaveResult, error) {
	if err := validateMembershipLeaveCall(ctx, replicaID, instanceID, releaseDigest, expected, operationID); err != nil {
		return RuntimeLeaveResult{}, err
	}
	return runMembershipEvidenceOperation(ctx, s.writeRunner(), func(q *Queries) (runtimeMembershipLeaveProjection, error) {
		return q.RuntimeFinishServingGracefulLeave(ctx, RuntimeFinishServingGracefulLeaveParams{
			ReplicaID: replicaID, InstanceID: instanceID, ReleaseDigest: releaseDigest,
			ExpectedControllerGeneration: expected, OperationID: operationID})
	}, func(p runtimeMembershipLeaveProjection) (RuntimeLeaveResult, error) {
		return decodeRuntimeMembershipLeave(p, replicaID, expected, operationID)
	})
}

// FinishMaintenanceGracefulLeave records the leave of the exact drained
// maintenance replica against its immutable prepare_maintenance_drain
// evidence.
func (s *runtimeMembershipEvidenceTransport) FinishMaintenanceGracefulLeave(ctx context.Context, replicaID uuid.UUID,
	instanceID, releaseDigest string, expected int64, operationID string) (RuntimeLeaveResult, error) {
	if err := validateMembershipLeaveCall(ctx, replicaID, instanceID, releaseDigest, expected, operationID); err != nil {
		return RuntimeLeaveResult{}, err
	}
	return runMembershipEvidenceOperation(ctx, s.writeRunner(), func(q *Queries) (runtimeMembershipLeaveProjection, error) {
		row, err := q.RuntimeFinishMaintenanceGracefulLeave(ctx, RuntimeFinishMaintenanceGracefulLeaveParams{
			ReplicaID: replicaID, InstanceID: instanceID, ReleaseDigest: releaseDigest,
			ExpectedControllerGeneration: expected, OperationID: operationID})
		return runtimeMembershipLeaveProjection(row), err
	}, func(p runtimeMembershipLeaveProjection) (RuntimeLeaveResult, error) {
		return decodeRuntimeMembershipLeave(p, replicaID, expected, operationID)
	})
}

// RecordEC2Termination records the caller's authenticated external EC2
// evidence, fences the exact incarnation and releases the claims it still
// owned. It never calls AWS and never infers death from a lost heartbeat,
// lock, deadline or connection.
func (s *runtimeMembershipEvidenceTransport) RecordEC2Termination(ctx context.Context, replicaID uuid.UUID,
	instanceID, releaseDigest, requestID, evidenceID string, requestedAt, observedTerminatedAt time.Time,
	observedState string) (RuntimeFenceResult, error) {
	var zero RuntimeFenceResult
	if err := validateMembershipReplicaCall(ctx, replicaID, instanceID, releaseDigest); err != nil {
		return zero, err
	}
	if !runtimePrintableText(requestID, 128) || !runtimePrintableText(evidenceID, 128) {
		return zero, errors.New("store: invalid membership evidence identity")
	}
	if err := validateMembershipTerminationEvidence(requestedAt, observedTerminatedAt, observedState); err != nil {
		return zero, err
	}
	return runMembershipEvidenceOperation(ctx, s.writeRunner(), func(q *Queries) (RuntimeRecordEC2TerminationRow, error) {
		return q.RuntimeRecordEC2Termination(ctx, RuntimeRecordEC2TerminationParams{
			ReplicaID: replicaID, InstanceID: instanceID, ReleaseDigest: releaseDigest, RequestID: requestID,
			EvidenceID: evidenceID, RequestedAt: requestedAt, ObservedTerminatedAt: observedTerminatedAt,
			ObservedState: observedState})
	}, func(p RuntimeRecordEC2TerminationRow) (RuntimeFenceResult, error) {
		return decodeRuntimeMembershipFence(p, replicaID, evidenceID)
	})
}

// writeRunner returns the configured runner, or nil when the transport was
// built without one.
func (s *runtimeMembershipEvidenceTransport) writeRunner() WriteTxRunner {
	if s == nil {
		return nil
	}
	return s.runner
}

// runMembershipEvidenceOperation executes one generated membership evidence
// function inside the write runner. The row is validated and copied before the
// callback returns, so an invalid result rolls the transaction back, and no
// value escapes unless finish and commit both succeed.
func runMembershipEvidenceOperation[P, R any](ctx context.Context, runner WriteTxRunner, call func(*Queries) (P, error), decode func(P) (R, error)) (R, error) {
	var zero R
	if runner == nil {
		return zero, errors.New("store: nil membership evidence transport runner")
	}
	var decoded R
	err := runner.ExecWrite(ctx, func(q *Queries) error {
		projected, callErr := call(q)
		if callErr != nil {
			return fmt.Errorf("store: execute membership evidence operation: %w", callErr)
		}
		decoded, callErr = decode(projected)
		return callErr
	})
	if err != nil {
		return zero, err
	}
	return decoded, nil
}

// validateMembershipReplicaCall rejects a nil context and the exact replica
// tuple every membership evidence operation takes nonnull, before the runner
// is touched.
func validateMembershipReplicaCall(ctx context.Context, replicaID uuid.UUID, instanceID, releaseDigest string) error {
	if ctx == nil {
		return errors.New("store: nil membership evidence context")
	}
	if replicaID == uuid.Nil || !runtimeInstanceIDPattern.MatchString(instanceID) ||
		!runtimeReleaseDigestPattern.MatchString(releaseDigest) {
		return errors.New("store: invalid membership replica identity")
	}
	return nil
}

// validateMembershipLeaveCall adds the historical prepare-selection scalars a
// graceful leave takes.
func validateMembershipLeaveCall(ctx context.Context, replicaID uuid.UUID, instanceID, releaseDigest string,
	expected int64, operationID string) error {
	if err := validateMembershipReplicaCall(ctx, replicaID, instanceID, releaseDigest); err != nil {
		return err
	}
	if expected <= 0 || !runtimePrintableText(operationID, 128) {
		return errors.New("store: invalid membership leave operation identity")
	}
	return nil
}

// runtimeMembershipObservedStates is the closed external observed-state enum
// the SQL accepts. Only an observed termination is proof.
var runtimeMembershipObservedStates = map[string]bool{"terminated": true}

// validateMembershipTerminationEvidence rejects external EC2 evidence the SQL
// would reject. The two times are stored raw and impose no database-clock skew
// ceiling, so only their mutual order is required.
func validateMembershipTerminationEvidence(requestedAt, observedTerminatedAt time.Time, observedState string) error {
	if requestedAt.IsZero() || observedTerminatedAt.IsZero() || observedTerminatedAt.Before(requestedAt) ||
		!runtimeMembershipObservedStates[observedState] {
		return errors.New("store: invalid membership termination evidence")
	}
	return nil
}

// decodeRuntimeMembershipLeave validates one graceful-leave row and returns an
// owned copy. A recorded leave always names the exact target in state left,
// stores the supplied operation and the historical prepare result generation,
// and carries zero joined counts, whether it is fresh or replayed.
func decodeRuntimeMembershipLeave(p runtimeMembershipLeaveProjection, replicaID uuid.UUID, expected int64, operationID string) (RuntimeLeaveResult, error) {
	var zero RuntimeLeaveResult
	if p.ReplicaID != replicaID || p.State != "left" || p.ReceiptOperationID != operationID ||
		p.ControllerGeneration != expected {
		return zero, errors.New("store: invalid membership leave result")
	}
	if p.JoinedTransitionCount != 0 || p.JoinedClaimCount != 0 || p.RecordedAt.IsZero() {
		return zero, errors.New("store: invalid membership leave evidence")
	}
	// The conversion copies every scalar field out of the generated row, so
	// the result aliases no driver buffer, and a regenerated row shape that
	// stopped matching this contract would fail to compile here.
	return RuntimeLeaveResult(p), nil
}

// decodeRuntimeMembershipFence validates one termination-proof row and returns
// an owned copy. A recorded proof names the exact target under the supplied
// evidence identity, leaves the member either fenced or already left, and
// reclaims nothing from a member that had already left gracefully.
func decodeRuntimeMembershipFence(p RuntimeRecordEC2TerminationRow, replicaID uuid.UUID, evidenceID string) (RuntimeFenceResult, error) {
	var zero RuntimeFenceResult
	if p.ReplicaID != replicaID || p.EvidenceID != evidenceID || (p.State != "fenced" && p.State != "left") {
		return zero, errors.New("store: invalid membership fence result")
	}
	if p.ReclaimedClaimCount < 0 || (p.State == "left" && p.ReclaimedClaimCount != 0) || p.RecordedAt.IsZero() {
		return zero, errors.New("store: invalid membership fence evidence")
	}
	return RuntimeFenceResult(p), nil
}
