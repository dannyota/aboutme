# Durable public transitions

Status: Accepted under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). This is
the target contract. Local implementation and hosted proof remain Phase 10
gates.

This contract extends the current `publicstate.Plan` and `CommittedState` with
durable fleet coordination. It preserves public HTTP, OpenAPI, server-sent event
(SSE), idempotency, and lease behavior.
[Transition commit](transition-commit.md) defines the business execution fence
and lock graph. [Runtime schema](runtime-schema.md),
[replica coordination](runtime-coordination.md), and
[transaction entry](transaction-entry.md) remain authoritative for membership
and write framing.

## Durable tables

The [fixed function catalog](transition-functions.md),
[operation contract](transition-operations.md) and
[store transport](transition-transport.md) define exact SQL arguments, result
presence, replay outcomes and connection cleanup for this protocol.

The schema adds `public_transitions`, `public_transition_targets`,
`public_transition_replicas`, and `public_transition_acks` as specified in
[runtime schema](runtime-schema.md#public_transitions) and the exact
[storage contract](transition-storage.md). These constraints are required:

- Transition, replica, and resume UUIDs are non-nil.
- `deadline_at = created_at + interval '5 seconds'`.
- `terminal_at` is null exactly while state is `closing`.
- Every carried target digest is 32 octets.
- Each parent has 1..4 targets. Ordinals are contiguous and nonnegative.
  Discovery is ordinal zero; resume targets follow it, or start at zero when
  discovery is absent.
- Expected generations are positive.
- Closing, rolled-back and unresolved targets have no result.
- Every committed target has exactly one immutable `generation` or `retired`
  result. Discovery is Revoking and permits only `generation`.
- Required replica identity matches its immutable membership tuple. An ack's
  identity and digest match its required replica and parent.
- `recovery_fencing_evidence_id` is nullable and references the unique
  `runtime_fencing_proofs.evidence_id`. It is non-null exactly for a
  lifecycle-command rollback whose terminal error code is `initiator_fenced`.
  Every other transition leaves it null.

Owner-only constraint triggers reject an incomplete parent commit, any result
change after terminal state, and any ack update or delete. The one permitted
result write occurs atomically while its closing parent is locked. All four
tables have a `runtime_assert_write_entry` BEFORE statement trigger. Transition
bookkeeping does not extend `last_accepted_writer_at`.

The deferred whole-parent assertion also checks initial closing creation,
recomputes the ordered digest and requires a nonempty required set. Committed
requires every required ack. Other states may retain any observed ack subset,
including full coverage. Child insertion locks the parent and requires closing.
Separate owner triggers always reject TRUNCATE, including under valid entry.

## Target digest

Go and PostgreSQL hash this binary encoding with SHA-256:

1. ASCII `aboutme.public-transition-targets.v1` followed by one zero byte.
2. Target count as unsigned 32-bit big-endian.
3. For each target: ordinal as unsigned 32-bit big-endian; kind byte `0` for
   discovery or `1` for resume; 16 UUID bytes, all zero for discovery; expected
   generation as signed positive 64-bit big-endian; class byte `0` for
   NonDraining or `1` for Revoking.

Discovery comes first. Resume UUIDs follow in raw-byte order. The digest
excludes SQL text, JSON, locale text, operation, replicas, timestamps, and
results.

Discovery generation 7 Revoking followed by resume
`00112233-4455-6677-8899-aabbccddeeff` generation 42 NonDraining has encoded
hex:

```text
61626f75746d652e7075626c69632d7472616e736974696f6e2d746172676574732e76310000000002000000000000000000000000000000000000000000000000000000000701000000010100112233445566778899aabbccddeeff000000000000002a00
```

Its digest is:

```text
34cd4cb37c1cf4467283a97904ce4170484ad2c335c277cb465403b9752ac68c
```

## Operation categories

The closed internal values are `resume_update`, `resume_publication`,
`resume_retire`, and `account_retire`.

- NonDraining edit and photo mutations use `resume_update`.
- Publish, unpublish, slug, and discovery changes use `resume_publication`.
- `deleteResume` uses `resume_retire`.
- Account deletion uses `account_retire`.

The existing route and idempotency operation digest remains unchanged. Creating
a resume changes no existing public generation and starts no transition.

## Begin and deadline

```sql
runtime_begin_public_transition(
  transition_id uuid,
  initiator_replica_id uuid,
  initiator_instance_id text,
  initiator_release_digest text,
  operation text,
  remaining_milliseconds integer,
  kinds text[],
  resume_ids uuid[],
  expected_generations bigint[],
  classes text[],
  supplied_digest bytea
) RETURNS TABLE(
  transition_id uuid,
  created_at timestamptz,
  deadline_at timestamptz,
  target_digest bytea,
  state text
)
```

Arrays have equal lengths and use subscript order. The function rejects invalid
shape, order, value, digest, initiator tuple, inactive initiator, an unfenced
terminating incarnation, or an intersecting visible closing/unresolved target.
Exact transition-ID replay returns the original row only when every immutable
input and target matches; mismatch is SQLSTATE `AM001`.

R2 creates one five-second deadline from its injected monotonic clock before any
begin work. Immediately before SQL it passes the positive remaining whole
milliseconds with ceiling, capped at 5,000. The function requires 1 through
5,000, then computes:

```text
deadline_at = statement_timestamp() + remaining_milliseconds
created_at  = deadline_at - 5 seconds
```

`created_at` is the original operation window projected onto database wall time,
not insertion time. After locks, one `clock_timestamp()` sample must be before
the deadline and not before `statement_timestamp()`; otherwise begin returns
SQLSTATE `55000` without insertion. Forward database-clock movement shortens the
attempt. Backward movement fails it.

The unchanged monotonic deadline controls begin, local close, and ack waiting.
Pool wait, write entry, SQL lock wait, local drain, and network wait consume the
same original budget. Millisecond ceiling cannot extend real work because that
context still expires at the original instant. The stored timestamps always
satisfy the exact five-second constraint.

While holding `public_state`, begin checks the discovery target against its
generation. It reads resume targets in UUID order and requires each row at its
expected revision. Missing or stale targets insert nothing. It snapshots every
active or draining incarnation. An unfenced terminating incarnation rejects the
whole begin. Membership activation and begin serialize on `public_state`.

## Required replicas and acknowledgements

```sql
runtime_list_public_transition_replicas(
  transition_id uuid,
  target_digest bytea
) RETURNS TABLE(
  parent_state text,
  replica_id uuid,
  instance_id text,
  release_digest text,
  target_digest bytea,
  acked boolean
)
```

Rows use replica UUID order. Missing, terminal, or mismatched parent state is
explicit; it is never confused with an empty required set.

```sql
runtime_ack_public_transition(
  transition_id uuid,
  replica_id uuid,
  instance_id text,
  release_digest text,
  target_digest bytea,
  revoking_count integer,
  non_draining_count integer
) RETURNS TABLE(acked_at timestamptz, replayed boolean)
```

R8 binds the fixed replica, instance, and release tuple in a private adapter.
Replicas share the app database role, so this is a stored-tuple compare-and-set,
not database authentication. After locking parent and required detail, ack locks
the exact `runtime_replicas` row. It requires matching identity and digest,
closing state before deadline, active or draining membership, and no termination
intent or fencing proof. It inserts local result `closed`. Exact replay is
idempotent; changed metrics or identity are `AM001`.

A draining replica may ack only a transition that already requires its exact
tuple after its local close and join. It cannot begin new ownership. An ack that
waited behind leave or fence rechecks membership and fails. Proof never mutates
or acknowledges a transition. A fenced required incarnation keeps recovery
closed or unresolved.

NonDraining seals old admission without canceling or waiting existing leases.
Discovery and Revoking cancel and join applicable local leases before ack.

## Recovery read

```sql
runtime_recover_public_transition(
  transition_id uuid,
  target_digest bytea
) RETURNS TABLE(
  parent_state text,
  operation text,
  initiator_replica_id uuid,
  created_at timestamptz,
  deadline_at timestamptz,
  terminal_at timestamptz,
  terminal_error_code text,
  target_ordinal integer,
  target_kind text,
  resume_id uuid,
  expected_generation bigint,
  class text,
  result_kind text,
  result_generation bigint,
  result_recorded_at timestamptz,
  required_replica_ids uuid[],
  required_instance_ids text[],
  required_release_digests text[],
  required_acked_at timestamptz[]
)
```

The result has one row per target ordinal. Each row repeats four equal-length
required-replica arrays in UUID order; null ack elements mean missing ack. A
transition created by an active initiator cannot have an empty required set.
Missing detail, invalid terminal results, or digest mismatch is `AM001`, never a
partial result.

Committed results are complete and immutable. Publish and edit record the new
resume generation. Publication changes also record discovery generation. Resume
deletion records retirement and discovery only when planned. Account deletion
records every planned resume retired plus discovery. Rollback records no result.

Delayed, duplicate, or reordered notifications only wake reconciliation. This
read supplies historical transition details. R2 applies the separate fixed
[reconciliation snapshot](transition-reconciliation.md) under its local apply
mutex before opening or retiring a fence. It preserves later generations,
retained retirement evidence and every current closing/unresolved blocker.
Current row absence alone never proves retirement.

Same-incarnation recovery runs with admission closed. It may replay and ack a
closing transition only after quarantine joined every local callback and rebuilt
the exact target fences, while membership remains active/draining and no
termination intent, proof, or lifecycle termination has begun. Draining never
reopens admission. Every mutating recovery call uses R8's same tuple-bound
adapter. A read-only recovery snapshot grants no authority.

A dead initiator follows the separate lifecycle recovery in
[transition commit](transition-commit.md#fenced-initiator-recovery). It never
uses app impersonation, an acknowledgement, or a timeout as death proof.

## Notifications, polling, and grants

Begin calls `pg_notify('aboutme_public_transition', '1:' || transition_uuid)` in
its transaction. Terminal functions notify `aboutme_runtime_terminal` with
`1:<uuid>:<state>`. PostgreSQL delivers after commit. Payloads contain no
target, identity, error, or authority.

R2 polls every 250 milliseconds while any transition is closing and rereads all
closing/unresolved and terminal rows after listener recovery. Malformed,
unknown, duplicate, and stale notices are wake-only. SSE adds no frame; clients
reconnect and refetch through existing revision transport.

`aboutme_runtime_owner` owns all objects. Functions set
`search_path = pg_catalog`, qualify every object, and use no dynamic SQL. Revoke
PUBLIC and direct table DML. The app role receives only the named EXECUTE
grants. Lifecycle, fencing, maintenance, and restore receive no transition
mutation grant except the lifecycle command's single fenced-initiator recovery
function. Owner-only assertion triggers validate immutable and terminal state.
Identity text comes only from the R8 adapter and must match immutable
membership.

## Acceptance

- Digest vector, ordering, nil UUID, shape, and terminal constraints.
- Overlapping begins, begin versus activation, and stale/missing generations.
- Active/draining snapshot, terminating rejection, forged and late ack, exact
  replay, and conflicting replay.
- NonDraining lease completion and Revoking/discovery cancellation and join.
- Five-second budget across pool, entry, lock, drain, and ack waits.
- Delayed, duplicate, reordered, and lost notification repair through reads.
- Same-incarnation replay, draining behavior, and termination/fence rejection.
- Fenced-initiator recovery requires the exact immutable EC2 proof reference and
  never manufactures an ack or committed result.
- Real-role denial of direct DML, cross-role execution, forged result, and
  writes outside [WriteTxRunner](transaction-entry.md#write-transaction-api).
- Business DML cannot commit when transition terminalization is omitted or
  general write finish runs before the transition capability is finished.
