# Exclusive wake operations

Status: Accepted detail of [exclusive write entry](lifecycle-write-entry.md)
under [ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). The
[scaling index](README.md) records whether it is built.

> **Implementation deferred.**
> [ADR 0036](../../adr/0036-single-replica-launch-and-pipeline-migrations.md)
> retires this path for the first release, which runs one serving replica and
> migrates from the deployment rather than from a waking fleet. The contract
> stays accepted for a later second replica.

This contract fixes fresh state, historical replay and the scalar store result
for begin_wake and complete_wake. The entry contract retains exact SQL
signatures, markers, statement/row containment and finish guards.
[Wake migrations](wake-migrations.md) defines the protected closing-gate path.

## Lock order and fresh reads

Both actions take the exclusive barrier before any row lock. Their durable lock
order is:

1. public_state singleton;
2. runtime_capacity singleton;
3. existing operation parent, or insert the fresh begin parent while capacity
   serializes first use;
4. matching action row if present;
5. runtime_write_state last, and only for a fresh action.

They never lock or wait on replica, transition-parent, claim, rate, business,
receipt, migration-history or other lifecycle-operation rows. They use
nonlocking single-statement predicates for replicas and closing/unresolved
transitions. They read all 48 shared_rate_partitions rows without locking after
capacity is locked. Only lifecycle/final-stop operations change enabled flags
and those writers first lock capacity; rate writers may change active_keys but
cannot change enabled. Require exactly P01..P24 for ordinals 1 and 2 and uniform
enabled/capacity-generation/operation evidence within each ordinal. Missing,
extra, duplicate or split catalog state is AM001. Wake changes no partition row
or debt.

These current-state predicates apply only to a fresh action. Historical replay
validates its retained parent and result without current partition checks.

## Fresh begin

Fresh runtime_begin_wake requires:

- expected controller and write generations are positive and equal the locked
  current rows; both can increment without overflow;
- migrator_enforcement_version is 1 and migration_history_owner is exactly
  aboutme_runtime_owner;
- write gate closed; lifecycle offline; admission disabled;
- desired_replicas=1, zero active serving and maintenance counts, and no
  nonterminal replica in joining/active/draining/terminating;
- both logical partitions uniformly disabled;
- no visible closing or unresolved public transition;
- operation_id unused and mode fixes the new parent workflow.

It creates parent then begin_wake step, changes lifecycle to starting while
keeping admission false, and keeps desired/count/partition values unchanged. It
increments capacity generation, controller generation and write generation once.
It sets capacity controller_operation_id and updated_by to operation_id. Its
immutable result contains the post-action generations, phase starting, admission
false, gate closing, zero counts, desired one and both flags false. The
write-state update from closed to closing invalidates the stop receipt by
generation; it neither locks nor mutates the immutable receipt.

## Fresh complete

Fresh runtime_complete_wake requires:

- exact existing parent of wake workflow and exact committed begin_wake step;
- the begin result mode derived from workflow, result shape/digest valid, and
  begin result generation <= supplied expected controller generation;
- supplied controller/write generations equal current locked rows and both can
  increment without overflow;
- migrator_enforcement_version is 1 and migration_history_owner is exactly
  aboutme_runtime_owner;
- write gate closing; lifecycle starting; admission disabled;
- desired one, zero active serving/maintenance, no nonterminal replica, both
  logical partitions disabled, and no visible closing/unresolved transition.

It changes lifecycle to online and admission true. It keeps desired, counts and
partitions unchanged. It increments the same three generations once, sets both
capacity evidence fields to operation_id, and stores the post-action result with
gate open and phase online. runtime_write_state is the last durable update.

The database predicates prove only state represented in these tables. Before a
fresh complete call, the serialized controller must separately prove that
required migrations are at the accepted head, reconciliation completed, due-work
planning completed, and deployment prerequisites passed. Those checks cannot be
inferred by these SQL functions and are not converted into caller booleans. The
serialized controller operation owns their ordering. Registration, local probes,
ready and activation occur after complete. A failure before complete leaves
closing/starting and ordinary writes unavailable.

## Time and digest

After all action locks and the fresh write-state row lock, call existing
owner-only runtime_sample_lifecycle_time() once. Compute effective_at as
GREATEST(sample, capacity.updated_at, write_state.updated_at, parent.created_at,
write_state.last_write_at, predecessor.recorded_at where present). Use it for
capacity.updated_at, write_state.updated_at and step.recorded_at. Keep
last_write_at and last_accepted_writer_at separate: set last_write_at and
updated_at to effective_at, last_writer_kind to lifecycle, and
last_writer_operation_id to operation_id; leave last_accepted_writer_at
unchanged. The sampler retains its fixed no-argument clock_timestamp body,
denied-login ACL and isolated rollback/replacement test seam; add no second wake
clock.

Use the existing owner-only lifecycle digest helpers and published wake vectors.
Argument fields are exactly those in lifecycle-vectors, without duplicate
function-header fields. Result digest includes every stored result column in the
published order, including result_kind, repeated result/controller generation,
repeated operation/controller-operation ID, gate and write generation.

## Replay and errors

An existing exact action validates marker binding, parent workflow, supplied
expected generation/mode argument digest, every retained result field, result
shape and recomputed result digest. It binds complete's marker mode from the
parent. It does not check current gate, generations, counts, partitions or
controller prerequisites; does not lock write_state; changes no durable row;
finishes the private marker; and returns the immutable stored result with
replayed=true.

Migration 15 does not retain every canonical argument. Any supplied canonical
argument-digest mismatch, including an indistinguishable well-shaped stored
argument_digest alteration, is AM002. AM001 is limited to independently provable
retained-field/result-shape corruption, recomputable result_digest mismatch,
impossible parent/action/catalog state, or marker/guard contamination.

Errors are: 22023 malformed scalar; 42501 wrong role/direct helper access; 55000
valid but stale generation, wrong fresh gate/state, missing predecessor or
unavailable prerequisite; AM002 immutable operation/action/argument conflict;
AM001 independently proven corruption or contaminated backend. Error text
contains no supplied identifier, digest or evidence. Rejection and rollback
increment nothing and grant no authority.

## Private Go transport

Use the accepted interface unchanged:

```go
type WakeMode string

const WakeModeServing WakeMode = "serving"
const WakeModeMaintenance WakeMode = "maintenance"

type LifecycleWakeStore interface {
    BeginWake(context.Context, int64, string, WakeMode, int64) (RuntimeCapacityResult, error)
    CompleteWake(context.Context, int64, string, int64) (RuntimeCapacityResult, error)
}

func NewLifecycleWakeStore(*pgxpool.Pool) LifecycleWakeStore
```

Reject any other WakeMode before acquiring a connection. Reuse the existing
RuntimeCapacityResult: required scalar capacity fields, WriteGate *string and
WriteGeneration*int64, plus Replayed bool. Wake decoding requires both pointers
nonnil; non-wake callers continue to require nil. Copy every value inside the
transaction and expose it only after commit and clean release.

Implement a private fixed runner beside WriteTxRunner. It acquires one
pgxpool.Conn and immediately captures pgtransport.Capability before Begin. Each
method has a fixed entry SQL literal and fixed one-call MATERIALIZED action
projection with explicit scalar casts. It exposes no callback, tx, Queries,
connection, action selector or retry. After the action returns, it commits
immediately. No ordinary enter/finish is called.

Every input, entry, query, NULL/decode, marker-finish, commit or cleanup error
returns the zero RuntimeCapacityResult. Definite 22023/55000/AM002 action
failure may use a detached five-second rollback and ReleaseCleanPGX only after
confirmed rollback and clean marker/catalog state. AM001, ambiguous
Begin/entry/action/ rollback/commit/cleanup, every commit error, or failed clean
release hijacks the lease and calls Capability.RetirePGX with a detached
five-second context. PhysicalClosed must be true. Preserve and wrap the primary
error and join retirement errors. The caller pool remains open. There is no
internal replay; the controller may make a new explicit call with the same
immutable identity.

## Prerequisite ownership

The serialized controller owns proof that migrations, reconciliation, due-work
planning and deployment prerequisites passed before fresh complete. SQL checks
only the represented durable state listed above. The fixed signature stays
unchanged: there is no prerequisite boolean or new durable evidence ledger in
this slice.

Pre-complete reconciliation is limited to read-only authoritative snapshots and
local fence reconstruction. If it discovers a required database mutation, the
wake stays closing; it cannot use ordinary entry as a bypass. Due-work planning
before complete is read-only. Job claims, cleanup, mail/media writes and durable
run results occur only after complete opens ordinary admission. External
deployment reconciliation may use its existing AWS/S3 controller authority but
does not create database write authority. Final stop guarantees transitions
terminal before closed, so a valid wake does not need fenced-transition mutation
to reach complete.
