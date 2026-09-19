# Ordinary lifecycle operation contract

## Scope

This contract fixes SQL and store behavior for six ordinary lifecycle actions:
prepare-scale-out, activate-replica-capacity, prepare-scale-in, finish-scale-in,
prepare-maintenance-drain and begin-replica-termination. It does not implement
begin/complete wake, registration, ready, graceful leave, EC2 proof, final stop,
claims, rates, server composition or AWS checks.

Authorities are [lifecycle operations](lifecycle-operations.md),
[replay](lifecycle-replay.md), [vectors](lifecycle-vectors.md),
[membership](replica-membership.md) and [schema](runtime-schema.md).
[Fixed operation transport](claim-operations.md#query-transport-and-go-ownership)
fixes the WriteTxRunner pattern.

## SQL signatures

Keep these exact functions and argument orders:

```sql
runtime_prepare_scale_out(
  expected_controller_generation bigint, operation_id text
) RETURNS runtime_capacity_result

runtime_activate_replica_capacity(
  expected_controller_generation bigint, operation_id text, replica_id uuid,
  instance_id text, container_instance_arn text, caddy_task_arn text,
  go_task_arn text, nuxt_task_arn text, release_digest text,
  replaced_replica_id uuid
) RETURNS runtime_replica_result

runtime_prepare_scale_in(
  expected_controller_generation bigint, operation_id text, replica_id uuid,
  instance_id text, release_digest text
) RETURNS runtime_replica_result

runtime_finish_scale_in(
  expected_controller_generation bigint, operation_id text, replica_id uuid,
  instance_id text, release_digest text, leave_operation_id text,
  fencing_evidence_id text
) RETURNS runtime_replica_result

runtime_prepare_maintenance_drain(
  expected_controller_generation bigint, operation_id text, replica_id uuid,
  instance_id text, release_digest text
) RETURNS runtime_replica_result

runtime_begin_replica_termination(
  expected_controller_generation bigint, operation_id text, request_id text,
  replica_id uuid, instance_id text, release_digest text, reason text
) RETURNS runtime_replica_result
```

Only replaced_replica_id and leave_operation_id are nullable. A replacement
activation requires replaced_replica_id; every other activation requires null.
Graceful finish requires leave_operation_id; abrupt finish requires null.
fencing_evidence_id is required in both finish branches because physical removal
always has exact EC2 audit proof.

## Roles and fixed errors

Every public wrapper is SECURITY DEFINER, owned by aboutme_runtime_owner, uses
search_path=pg_catalog, qualifies all objects and uses no dynamic SQL. Before
any mutable read, require session_user exactly aboutme_lifecycle_command. Grant
that role EXECUTE on these six functions. Revoke EXECUTE from PUBLIC,
aboutme_app, aboutme_maintenance, aboutme_fencing_proof, aboutme_restore_verify
and aboutme_migrator. Owner helpers have no login grant. No wrapper grants table
DML or accepts a caller role, timestamp, capacity, partition flag, result or
digest.

Errors are fixed:

- 22023: null/invalid scalar input, nil UUID, nonpositive expected generation,
  text outside its stored bounds or invalid reason;
- 42501: wrong direct session_user;
- 55000: closed ordinary write gate, stale current generation, missing or
  out-of-order predecessor, valid but unavailable source state, capacity bound,
  visible closing/unresolved transition or unfenced terminating incarnation;
- AM002: an existing operation/action has a different workflow, arguments or
  argument digest; supplied tuple/evidence conflicts with immutable identity;
- AM001: independently provable retained-field corruption, recomputable stored
  result_digest mismatch, invalid historical result shape, incomplete catalog/
  partition set, impossible durable row or owner-marker corruption.

Fixed errors contain no ARN, instance, release, request, evidence or digest
value. Constraint/driver errors are preserved as causes but cannot be treated as
successful authority.

## Common transaction and replay protocol

Each store call uses one ordinary WriteTxRunner transaction and invokes one SQL
function. The wrapper first locks public_state, then runtime_capacity. It then
inserts/locks the operation parent, reads/inserts its action row, and takes the
action-specific rows below. It never waits on a public transition parent.
Visible closing/unresolved checks are nonlocking.

Validate scalar shape and direct role before mutable reads. Compute the argument
digest with the owner-only canonical helper from lifecycle-vectors.md. For an
existing action, validate parent workflow and expected_generation, then compute
the supplied canonical argument digest. Any difference from stored
argument_digest is AM002. Migration 00015 does not retain every canonical
argument, so this schema cannot distinguish a changed replay request from an
otherwise well-shaped stored argument_digest tamper. It must not claim AM001
argument-corruption detection. Both cases fail closed. A future requirement to
distinguish them needs durable storage for every canonical argument.

Validate every stored result column and recompute result_digest because all
canonical result fields are retained. A result mismatch, invalid retained field
or stored shape is AM001. Exact replay returns the immutable stored projection
with replayed=true without checking current replica/capacity prerequisites and
without changing any row or generation. Later lifecycle or membership changes
cannot alter replay output.

For a fresh action, require expected_controller_generation equal current
runtime_capacity.controller_generation and the exact predecessor graph.
prepare-scale-out creates scale_out, prepare-scale-in creates scale_in and
begin-replica-termination creates replica_termination. Activation under an
existing compatible parent uses that parent's workflow; with no parent, null
replacement creates initial_serving and nonnull replacement creates
replacement_serving. Finish and maintenance drain require their existing parent
and never create one. Any conflicting existing parent is AM002. Reject overflow
before addition. Increment capacity generation and controller generation once,
require result_generation=expected+1, set controller_operation_id=operation_id,
store the complete typed result and both digests, and return replayed=false.
Rejection/rollback increments neither.

The reusable digest boundary consists of two exact owner-only helpers:

```sql
runtime_lifecycle_digest_field(field_type smallint, payload bytea)
RETURNS bytea

runtime_lifecycle_digest(kind smallint, framed_fields bytea)
RETURNS bytea
```

A null payload emits null presence and zero length. A nonnull payload emits
present, including a present empty text payload. field_type accepts only the
four fixed tags and validates UUID=16 bytes, int8=8 and boolean=1 with value
zero or one. Fixed wrappers produce payloads with pg_catalog.uuid_send,
pg_catalog.int8send, pg_catalog.convert_to(text,'UTF8') and the fixed boolean
byte. The digest helper accepts kind 1 or 2 and hashes the fixed domain, NUL,
kind and framed concatenation with pg_catalog.sha256.

No login receives EXECUTE on either helper. Only fixed owner lifecycle wrappers
assemble fields; callers cannot supply framed bytes. The helpers implement
lifecycle-vectors.md, including explicit null fields, result_kind, duplicated
result/controller generation and duplicated operation/controller-operation ID.
They accept no JSON, alternate domain or algorithm. Later wake uses the same
private helpers with its fixed fields.

## Database time and evidence

Install `runtime_sample_lifecycle_time() RETURNS timestamptz` as an owner-only,
no-argument VOLATILE helper whose production body is exactly
`SELECT pg_catalog.clock_timestamp()`. Revoke EXECUTE from PUBLIC and every
login role. It is distinct from runtime_sample_rate_time, so lifecycle and rate
test seams cannot alter each other's clock.

After all action rows are locked, call this helper once. For a fresh action,
effective_at is GREATEST(sample, capacity.updated_at, operation created_at/step
times and every nonnull timestamp on the affected replica, intent, leave receipt
or proof). Use effective_at for every new replica state timestamp,
termination-intent requested_at, capacity.updated_at and ledger recorded_at in
that action. Generation/CAS remains ordering authority.

One-session migration tests may replace the helper with a fixed literal only in
an owner transaction that rolls back. Independent-pool tests use a disposable
isolated database: replace and commit before any pool opens, close and join all
pools/connections, then restore the exact production body or drop the database.
Serialize the whole fixture lifetime. A test first proves no login can execute,
replace or influence the helper. There is no runtime setter, caller timestamp,
configuration switch or shared-database replacement.

Set runtime_capacity.updated_by=operation_id on every fresh lifecycle action,
matching controller_operation_id. Replay preserves both historical values.
Callers supply no separate updated_by value.

## Partition consistency

Any function that reads or changes logical allocation locks all 48
shared_rate_partitions rows after its capacity/replica/action locks, ordered by
partition numeric then policy_id. Require exactly P01 through P24 with rows 1
and 2. Within each ordinal, enabled, capacity_generation and operation_id must
be uniform across all 24 policies. Missing, extra, duplicated or split state is
AM001. active_keys may differ and is never changed by lifecycle.

When a flag changes, update all 24 rows for that ordinal in the same transaction
with the new capacity generation, operation_id and effective_at. Never delete,
move or reset buckets/debt. Rate operations lock clock then partition and never
capacity, so this capacity-to-partition wait has no reverse capacity edge.

## Action contracts

prepare_scale_out uses workflow scale_out. Require online/true, desired one,
exactly one active serving replica, partition 1 uniformly enabled, partition 2
uniformly disabled, no unfenced terminating replica and no closing/unresolved
transition. Lock all partition rows for the consistency check but change none.
Set desired two only. Return runtime_capacity_result with current counts/flags
and null wake fields.

activate_replica_capacity supports initial_serving, scale_out,
replacement_serving, uat_serving_wake and maintenance_wake according to the
existing operation parent. Require online/true, exact joining+join_ready replica
and full immutable task trio, no intent, no closing/unresolved transition and no
unfenced terminating replica. Lock target replica before all partition rows.

- Initial-serving and UAT-serving-wake first activations require desired one,
  zero active serving, null replacement, its exact workflow predecessor rules,
  and both partition 1 and partition 2 uniformly disabled. It then enables
  partition 1 across all policies. A retained enabled partition 1 after a fenced
  desired-one replica cannot use this branch; it requires replacement_serving
  with the exact nonnull fenced predecessor.
- Scale-out second serving requires desired two, one active serving, exact
  prepare-scale-out predecessor and null replacement; require partition 1
  enabled and enable partition 2.
- Replacement serving requires a nonnull exact fenced serving predecessor,
  unchanged desired, active serving below desired, no unfinished scale-in and no
  earlier action consuming that predecessor. Change no partition flag.
- Maintenance requires the exact maintenance-wake complete predecessor, no other
  maintenance replica in joining, active, draining or terminating state, and
  null replacement. Retained left/fenced maintenance rows do not block it.
  Change no partition flag or desired count.

Change target joining to active and set activated_at. A replacement does not
infer process death; it uses already-recorded fenced membership. Return replica
result with every count/partition pointer null.

prepare_scale_in uses workflow scale_in and is its first action. Require
online/true, desired two, exactly two active serving replicas, exact active
serving target, no intent/other drain/unfenced termination/closing or unresolved
transition, and both partition ordinals uniformly enabled. Lock target then all
partitions for validation; change no flag. Change only target active to draining
and set draining_at. Return replica result with nullable capacity fields absent.

prepare_maintenance_drain requires maintenance_wake parent and exact completed
activation predecessor, online/true, exactly one active maintenance target, no
other maintenance drain and no intent. Change only target active to draining and
set draining_at. It does not inspect/change desired or rate partitions and
returns nullable capacity fields absent.

begin_replica_termination uses workflow replica_termination and its sole action.
Lock exact target then its intent. Require immutable tuple and no prior intent.
startup_failed accepts joining, readiness_failed accepts active, and
drain_failed accepts draining plus the exact prepare-scale-in or
prepare-maintenance-drain action that selected it. Insert request-bound intent,
change target to terminating and set the shared effective time. Do not change
desired or partitions. SQL proves only the reason and source-state relation.
Server composition must prove that readiness_failed is node-specific after
suppressing fleet-wide RDS failure; SQL has no cloud health or per-process
signal.

finish_scale_in requires the same scale_in parent's prepare action and exact
selected target, desired two, and all other serving incarnations terminal except
zero or one active survivor. Lock target, then fixed receipt/proof/intent rows,
then every partition row. Graceful requires target left, matching nonnull leave
receipt and matching EC2 proof. Abrupt requires target fenced, null leave ID,
matching intent and matching proof. Every reference must match exact
replica/instance/release and operation evidence; absence or mismatch fails.
Require partition 1 uniformly enabled and partition 2 uniformly enabled, then
set desired one and disable partition 2 across all policies. Preserve partition
1 and every debt row. Return target identity/state plus desired/count/flags in
the replica result. Zero-survivor is valid.

## Go transport

Define:

```go
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

func NewRuntimeLifecycleTransport(*pgxpool.Pool) RuntimeLifecycleTransport
```

All methods are context-first scalar methods in SQL order. They expose no
identity/config object, callback, Queries, transaction, connection, digest,
timestamp or retry. Server composition owns trusted identity selection and
controller inputs.

Reuse RuntimeReplicaResult from registration: three optional counts use
`*int16`, two optional partition flags use `*bool`, and all other fields are
values. Only finish-scale-in populates those optional fields.

RuntimeCapacityResult has `DesiredReplicas`, `ActiveServingReplicas` and
`ActiveMaintenanceReplicas` as `int16`; `Partition1Enabled`,
`Partition2Enabled`, `AdmissionEnabled` and `Replayed` as `bool`;
`CapacityGeneration` and `ControllerGeneration` as `int64`;
`ControllerOperationID` and `LifecyclePhase` as `string`; `WriteGate` as
`*string`; and `WriteGeneration` as `*int64`. Prepare-scale-out requires both
wake fields null. The same type later carries both fields nonnull for wake.

Each query uses one MATERIALIZED function result and explicit scalar casts for
all composite attributes. This prevents repeated volatile function evaluation
and preserves null presence. Decode/copy inside WriteTxRunner's callback; expose
the result only after finish and commit succeed. Any input, SQL, decode, finish,
commit or cleanup error returns the zero result with the wrapped cause. There is
no internal retry or ambiguous-result authority.
