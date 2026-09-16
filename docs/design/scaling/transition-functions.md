# Fixed public transition functions

Status: Accepted detail under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). The
[scaling index](README.md) records whether it is built.

The [operation contract](transition-operations.md) fixes entry, locks, roles and
errors. The [store transport](transition-transport.md) fixes copied values and
connection cleanup. These functions use the accepted
[transition storage](transition-storage.md) without adding a stored field.

## State and identity

- Operations are resume_update, resume_publication, resume_retire, and
  account_retire. Target classes are non_draining and revoking.
- A parent starts closing and may become committed, rolled_back, or unresolved.
  This slice creates closing, committed, and rolled_back only. It installs no
  unresolved writer.
- Target order and digest are authoritative in transition-storage.md. There are
  one through four targets. Discovery, when present, is ordinal zero. Resume
  targets follow in ascending UUID byte order. SQL recomputes the SHA-256 digest
  with existing owner-only `runtime_public_transition_target_digest(uuid)`.
- A committed parent has one exact immutable result per target and all required
  acks. Rolled-back and closing parents have no target results. Current absence
  never proves retirement.
- Original monotonic five-second caller context bounds pool wait, ordinary write
  entry, SQL locks, local close/join, ack wait, business work, and terminal
  call. No SQL method creates a new budget or retries automatically.

## Function results

Definitions use p_ input names to avoid collisions while preserving accepted
argument order and types.

1. runtime_begin_public_transition takes:

   p_transition_id uuid, p_initiator_replica_id uuid, p_initiator_instance_id
   text, p_initiator_release_digest text, p_operation text,
   p_remaining_milliseconds integer, p_kinds text[], p_resume_ids uuid[],
   p_expected_generations bigint[], p_classes text[], p_supplied_digest bytea.

   It returns exactly one row: transition_id uuid, created_at timestamptz,
   deadline_at timestamptz, target_digest bytea, state text. All are required;
   state is closing. Exact ID replay returns the original row after validating
   every retained immutable identity, stored target, digest, and required
   replica set. remaining_milliseconds is transient budget projection, not
   retained replay identity. Replay validates its 1..5000 shape but neither
   compares it with the original call nor rewrites created_at/deadline_at.

2. runtime_list_public_transition_replicas takes p_transition_id uuid and
   p_target_digest bytea. It returns one required row per replica in UUID order:
   parent_state text, replica_id uuid, instance_id text, release_digest text,
   target_digest bytea, acked boolean. All fields are required; parent_state is
   closing. Missing parent, target mismatch, terminal parent, malformed required
   set, or zero required rows raises an error rather than returning an empty
   slice. Terminal/missing can never masquerade as successful zero acks.

3. runtime_ack_public_transition takes p_transition_id uuid, p_replica_id uuid,
   p_instance_id text, p_release_digest text, p_target_digest bytea,
   p_revoking_count integer, p_non_draining_count integer. It returns exactly
   one required row: acked_at timestamptz, replayed boolean. Fresh insert is
   false. Exact immutable ack replay is true. Metrics are nonnegative and part
   of exact replay identity.

4. runtime_recover_public_transition takes p_transition_id uuid and
   p_target_digest bytea. It returns one row per target ordinal with the exact
   accepted 19 columns: parent_state, operation, initiator_replica_id,
   created_at, deadline_at, nullable terminal_at, nullable terminal_error_code,
   target_ordinal, target_kind, nullable resume_id, expected_generation, class,
   nullable result_kind, nullable result_generation, nullable
   result_recorded_at, and required_replica_ids, required_instance_ids,
   required_release_digests, required_acked_at arrays.

   The first three arrays have no null element. required_acked_at has the same
   length/order and permits null elements exactly for missing acks. All four
   arrays are nonnull, equal length, nonempty, and repeated identically on each
   target row. Missing/malformed detail returns no partial snapshot.

5. runtime_lock_public_transition_for_commit takes p_transition_id uuid,
   p_initiator_replica_id uuid, p_initiator_instance_id text,
   p_initiator_release_digest text, p_target_digest bytea. It returns exactly
   one required row: deadline_at timestamptz, target_count integer, ack_count
   integer. target_count is 1..4. ack_count is the positive count of exact acked
   required replicas and must equal the stored required-replica count. Target
   and replica cardinalities are independent. This is capability entry, not a
   durable result and is never exposed outside the transaction callback.

6. runtime_commit_public_transition takes p_transition_id uuid, p_target_digest
   bytea, p_result_kinds text[], and p_result_generations bigint[]. It returns
   exactly one required row: terminal_at timestamptz, replayed boolean. This
   function never accepts a replayed committed parent in a new transaction.
   replayed is false only on authorized fresh terminalization; every other value
   is corruption.

7. runtime_rollback_public_transition takes p_transition_id uuid,
   p_initiator_replica_id uuid, p_initiator_instance_id text,
   p_initiator_release_digest text, p_target_digest bytea, p_error_code text. It
   returns state text, terminal_at timestamptz, replayed boolean. terminal_at is
   required for every returned terminal state. Fresh closing-to-rolled_back
   returns false; exact same rollback returns true. An already committed parent
   returns committed with replayed=true because the call is a durable terminal
   readback. A rolled-back replay requires the same stored reason; changed
   reason is AM001. Committed readback compares no nonexistent rollback reason.
   False is reserved for a fresh closing-to-rolled_back change.

8. runtime_recover_rollback_public_transition takes transition ID, initiator
   replica/instance/release tuple, and target digest in that order. It returns
   state text and terminal_at timestamptz, both required. Committed or
   rolled_back is immutable readback. Closing changes to rolled_back with
   business_not_started only after its exact nonlocking generation proof.

9. runtime_recover_fenced_public_transition keeps the accepted order:
   transition_id uuid, target_digest bytea, initiator_replica_id uuid,
   fencing_evidence_id text. It returns required state text, terminal_at
   timestamptz, replayed boolean. It is a separate lifecycle-command operation.

10. runtime_reconcile_public_transition keeps its accepted 16-column row:
    parent_state, operation, nullable terminal_at, nullable terminal_error_code,
    target_ordinal, target_kind, nullable resume_id, expected_generation, class,
    nullable result_kind/result_generation/result_recorded_at, current_present,
    nullable current_generation, proven_retired, blocking_transition_ids uuid[].
    It returns exactly one row per target ordinal and no partial snapshot.

## SQL declarations

```sql
runtime_begin_public_transition(
 p_transition_id uuid, p_initiator_replica_id uuid,
 p_initiator_instance_id text, p_initiator_release_digest text,
 p_operation text, p_remaining_milliseconds integer, p_kinds text[],
 p_resume_ids uuid[], p_expected_generations bigint[], p_classes text[],
 p_supplied_digest bytea
) RETURNS TABLE(transition_id uuid, created_at timestamptz,
 deadline_at timestamptz, target_digest bytea, state text)

runtime_list_public_transition_replicas(
 p_transition_id uuid, p_target_digest bytea
) RETURNS TABLE(parent_state text, replica_id uuid, instance_id text,
 release_digest text, target_digest bytea, acked boolean)

runtime_ack_public_transition(
 p_transition_id uuid, p_replica_id uuid, p_instance_id text,
 p_release_digest text, p_target_digest bytea, p_revoking_count integer,
 p_non_draining_count integer
) RETURNS TABLE(acked_at timestamptz, replayed boolean)

runtime_recover_public_transition(
 p_transition_id uuid, p_target_digest bytea
) RETURNS TABLE(parent_state text, operation text,
 initiator_replica_id uuid, created_at timestamptz, deadline_at timestamptz,
 terminal_at timestamptz, terminal_error_code text, target_ordinal integer,
 target_kind text, resume_id uuid, expected_generation bigint, class text,
 result_kind text, result_generation bigint, result_recorded_at timestamptz,
 required_replica_ids uuid[], required_instance_ids text[],
 required_release_digests text[], required_acked_at timestamptz[])

runtime_lock_public_transition_for_commit(
 p_transition_id uuid, p_initiator_replica_id uuid,
 p_initiator_instance_id text, p_initiator_release_digest text,
 p_target_digest bytea
) RETURNS TABLE(deadline_at timestamptz, target_count integer,
 ack_count integer)

runtime_commit_public_transition(
 p_transition_id uuid, p_target_digest bytea, p_result_kinds text[],
 p_result_generations bigint[]
) RETURNS TABLE(terminal_at timestamptz, replayed boolean)

runtime_rollback_public_transition(
 p_transition_id uuid, p_initiator_replica_id uuid,
 p_initiator_instance_id text, p_initiator_release_digest text,
 p_target_digest bytea, p_error_code text
) RETURNS TABLE(state text, terminal_at timestamptz, replayed boolean)

runtime_recover_rollback_public_transition(
 p_transition_id uuid, p_initiator_replica_id uuid,
 p_initiator_instance_id text, p_initiator_release_digest text,
 p_target_digest bytea
) RETURNS TABLE(state text, terminal_at timestamptz)

runtime_recover_fenced_public_transition(
 p_transition_id uuid, p_target_digest bytea,
 p_initiator_replica_id uuid, p_fencing_evidence_id text
) RETURNS TABLE(state text, terminal_at timestamptz, replayed boolean)

runtime_reconcile_public_transition(
 p_transition_id uuid, p_target_digest bytea
) RETURNS TABLE(parent_state text, operation text, terminal_at timestamptz,
 terminal_error_code text, target_ordinal integer, target_kind text,
 resume_id uuid, expected_generation bigint, class text, result_kind text,
 result_generation bigint, result_recorded_at timestamptz,
 current_present boolean, current_generation bigint, proven_retired boolean,
 blocking_transition_ids uuid[])
```

The presence matrices stated above define nullable output; PostgreSQL RETURNS
TABLE syntax itself does not express NOT NULL.

## Validation, clocks and notifications

All UUIDs are non-nil, digests exactly 32 bytes, text values closed/bounded, and
counts/generations positive or nonnegative as specified. Target input and result
arrays are nonnull, one-dimensional, lower bound one, equal length and have one
through four elements without unexpected nulls. Recovery required-replica arrays
are nonnull, nonempty, equal length and UUID ordered. Their length is
independent of target count and has no target-derived cap. Discovery uses null
resume ID and generation result only; resume uses a non-nil ID. Result arrays
map exactly to stored ordinal order.

Begin alone uses statement_timestamp to project the original deadline:
deadline=statement timestamp+remaining whole milliseconds and created=deadline-
five seconds. Remaining milliseconds is 1..5000. After its locks, call
owner-only `runtime_sample_public_transition_time()` exactly once. Its
production body is one clock_timestamp; it uses the same
runtime_owner/no-login/isolated test-clock contract as other runtime samplers.
Sample before statement time or at/after deadline is 55000. Replay returns
stored time and does not resample.

The first call's remaining_milliseconds is not stored separately. Its only
durable effect is the accepted created_at/deadline_at pair. Replay remains
bounded by the caller's still-live original monotonic context, returns those
timestamps unchanged, and never resets or extends that deadline. No retained
milliseconds or new deadline field is added merely to compare retries.

Ack samples the same helper after its rows are locked and requires the sample
strictly before deadline. Equality or later returns 55000 without an ack or
notification. Terminal mutators sample it once after parent/detail locks.
Effective terminal time is greatest of the sample and parent/child prior times.
Commit does not repeat the deadline after commit entry authorized business work.
Read functions use no volatile clock.

Begin emits pg_notify aboutme_public_transition with `1:<uuid>`. Every fresh
terminal mutation emits aboutme_runtime_terminal with `1:<uuid>:<state>`.
Replay/readback emits no notification. PostgreSQL delivery after commit is the
only observable notification; payload grants no authority.
