# Public transition store transport

Status: Accepted detail under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md).
Implementation and local concurrency proof remain Phase 10 gates.

This is the copied-value boundary for the
[fixed SQL functions](transition-functions.md) and
[operation contract](transition-operations.md).

## Values

internal/store owns closed string-backed TransitionState, TransitionOperation,
TargetKind, TransitionClass, and TargetResultKind plus copied scalar structs. It
never imports publicstate. R2/R3 adapters convert their private plans/results to
these store values.

Exact exported transport values are:

```go
type TransitionTargetInput struct {
 Kind TargetKind
 ResumeID *uuid.UUID
 ExpectedGeneration int64
 Class TransitionClass
}
type TransitionBegin struct {
 TransitionID uuid.UUID
 CreatedAt time.Time
 DeadlineAt time.Time
 TargetDigest [32]byte
 State TransitionState
}
type TransitionRequiredReplica struct {
 ReplicaID uuid.UUID
 InstanceID string
 ReleaseDigest string
 Acked bool
}
type TransitionRequiredSet struct {
 ParentState TransitionState
 TargetDigest [32]byte
 Replicas []TransitionRequiredReplica
}
type TransitionRecoveryReplica struct {
 ReplicaID uuid.UUID
 InstanceID string
 ReleaseDigest string
 AckedAt *time.Time
}
type TransitionAck struct { AckedAt time.Time; Replayed bool }
type TransitionRecovery struct {
 TransitionID uuid.UUID
 TargetDigest [32]byte
 ParentState TransitionState
 Operation TransitionOperation
 InitiatorReplicaID uuid.UUID
 CreatedAt time.Time
 DeadlineAt time.Time
 TerminalAt *time.Time
 TerminalErrorCode *string
 Targets []TransitionRecoveryTarget
 RequiredReplicas []TransitionRecoveryReplica
}
type TransitionRecoveryTarget struct {
 Ordinal int32
 Kind TargetKind
 ResumeID *uuid.UUID
 ExpectedGeneration int64
 Class TransitionClass
 ResultKind *TargetResultKind
 ResultGeneration *int64
 ResultRecordedAt *time.Time
}
type TransitionTerminal struct {
 State TransitionState
 TerminalAt time.Time
 Replayed bool
}
type TransitionRecoveryTerminal struct {
 State TransitionState
 TerminalAt time.Time
}
type TransitionCommitTargetResult struct {
 Kind TargetResultKind
 Generation *int64
}
type TransitionCommitResults struct {
 Targets []TransitionCommitTargetResult
}
type TransitionReconciliation struct {
 TransitionID uuid.UUID
 TargetDigest [32]byte
 ParentState TransitionState
 Operation TransitionOperation
 TerminalAt *time.Time
 TerminalErrorCode *string
 Targets []TransitionReconciliationTarget
}
type TransitionReconciliationTarget struct {
 Ordinal int32
 Kind TargetKind
 ResumeID *uuid.UUID
 ExpectedGeneration int64
 Class TransitionClass
 ResultKind *TargetResultKind
 ResultGeneration *int64
 ResultRecordedAt *time.Time
 CurrentPresent bool
 CurrentGeneration *int64
 ProvenRetired bool
 BlockingTransitionIDs []uuid.UUID
}
```

Every slice and pointed-to scalar is newly allocated/copied for the returned
value. ListRequired uses Acked; Recover uses nullable AckedAt. Commit generation
is nonnull exactly for generation kind and null exactly for retired.
Reconciliation fields match the accepted transition-reconciliation.md names and
nullability. TransitionRecoveryTerminal is used only by RecoverRollback because
its accepted SQL result has state and terminal_at only. The store does not infer
or fabricate a replay flag. TransitionTerminal is reserved for functions whose
SQL result actually includes replayed.

## Interfaces

Context-first scalar APIs:

```go
type RuntimePublicTransitionTransport interface {
  Begin(context.Context, uuid.UUID, uuid.UUID, string, string, string, int32,
    []TransitionTargetInput, [32]byte) (TransitionBegin, error)
  ListRequired(context.Context, uuid.UUID, [32]byte) (TransitionRequiredSet, error)
  Ack(context.Context, uuid.UUID, uuid.UUID, string, string, [32]byte,
    int32, int32) (TransitionAck, error)
  Recover(context.Context, uuid.UUID, [32]byte) (TransitionRecovery, error)
  Rollback(context.Context, uuid.UUID, uuid.UUID, string, string, [32]byte,
    string) (TransitionTerminal, error)
  RecoverRollback(context.Context, uuid.UUID, uuid.UUID, string, string,
    [32]byte) (TransitionRecoveryTerminal, error)
  LoadTransitionReconciliation(context.Context, uuid.UUID, [32]byte) (TransitionReconciliation, error)
}

type RuntimeFencedTransitionRecovery interface {
  RecoverFenced(context.Context, uuid.UUID, [32]byte, uuid.UUID, string) (TransitionTerminal, error)
}

```

Constructors accept only the role-specific pgxpool.Pool. Fenced recovery stays
separate so an app transport cannot gain lifecycle execution.

```go
func NewRuntimePublicTransitionTransport(
  *pgxpool.Pool,
) RuntimePublicTransitionTransport
func NewTransitionCommitStore(*pgxpool.Pool) TransitionCommitStore
func NewRuntimeFencedTransitionRecovery(
  *pgxpool.Pool,
) RuntimeFencedTransitionRecovery
```

TransitionTargetInput contains Kind TargetKind, nullable ResumeID, positive
ExpectedGeneration, and Class TransitionClass. The store validates one through
four targets, exact discovery/resume shape and canonical order, then privately
derives kinds, nullable resume IDs, generations, and classes arrays in fixed SQL
argument order. Callers never supply parallel nullable arrays. SQL remains the
digest and stored-target authority.

```go
type TransitionCommitStore interface {
  WithTransitionCommitFence(context.Context, uuid.UUID, uuid.UUID, string,
    string, [32]byte, func(*Queries) (TransitionCommitResults, error)) (TransitionTerminal, error)
}

```

The callback receives transaction-bound Queries only. TransitionCommitResults
contains fixed ordered targets, each with kind generation plus positive value,
or retired plus no generation. It contains no transition/initiator/digest,
timestamp, replay flag, SQL strings, or authority boolean. Store validates it
against the entry target count and SQL validates it against durable targets and
business facts.

## Queries and connection cleanup

sqlc queries call each volatile function once with a MATERIALIZED CTE and
explicit casts. Nullable scalar output uses value-plus-presence projection;
never COALESCE a missing required value into valid authority. Nullable inputs
use sqlc.narg only for per-ordinal discovery resume ID and retired generation
elements during internal array construction; public methods use typed target
values and do not expose parallel nullable arrays.

Recover decodes the four arrays explicitly. Ack timestamps use a nullable-array
scanner that preserves null elements; no zero time stands for missing ack. It
checks equal lengths, UUID order, nonempty set, repeated-array equality across
rows, at most four targets, ordinal continuity, enum/result matrix, and
Rows.Err. Rows.Close returns no error in pgx/v5. Reconcile follows its accepted
copied-value boundary. Every byte/array/time value is copied before rows close;
no pgx.Rows, driver buffer, generated query type, or connection escapes.

Every pure read explicitly acquires one *pgxpool.Conn and captures a
pgtransport.Capability before Query or QueryRow. It closes Rows, checks Rows.Err
and completes validation/copying before Capability.ReleaseCleanPGX and pool
Release. It Hijacks the same pool connection and calls RetirePGX under the
bounded cleanup context on any AM001, even after clean protocol completion; on
any query, scan, iteration, cancellation, or cleanup path whose clean reuse is
unproved; or on clean-release failure. QueryRow uses the same lease so the exact
backend can be retired. Every error returns a zero value/no snapshot. No
Pool.Query path may surrender the lease before this decision.

internal/store never imports publicstate. R2/R3 map existing publicstate.Plan,
ResumeTarget, TransitionClass, and CommittedState to these transport values and
preserve the existing Coordinator and Transition caller surface. Any exact
CoordinatorConfig/NewCoordinator dependency injection is a separate R2 contract
that root must fix before R2 authoring; R1 must not choose or alter it.
