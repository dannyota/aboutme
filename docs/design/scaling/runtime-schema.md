# Runtime schema and privileges

Status: Accepted under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). This is
the target contract. Local implementation and hosted proof remain Phase 10
gates.

## Durable schema contract

Root owns the migration, SQL sources, generated sqlc code, grants, and schema
tests. Names below are contract names; authors do not edit migration files until
root assigns the schema slice.

## runtime_replicas

- replica_id uuid primary key, generated randomly by composition at each boot.
- instance_id text not null; container_instance_arn text not null.
- caddy_task_arn, go_task_arn, nuxt_task_arn text not null.
- release_digest text not null; state text not null check in
  ('joining','active','draining','left','terminating','fenced').
- joined_at timestamptz not null; join_ready_at, activated_at, draining_at,
  left_at, termination_requested_at, fenced_at nullable.
- unique(instance_id, replica_id); unique active incarnation per instance via a
  partial unique index for joining/active/draining/terminating.
- state/timestamp checks: active requires join_ready_at and activated_at;
  draining requires activated_at and draining_at; left requires draining_at and
  left_at; terminating requires termination_requested_at; fenced requires
  fenced_at. Left and fenced are terminal. No delete path exists.

## runtime_capacity

- singleton boolean primary key fixed true; desired_replicas smallint check
  1..2; generation bigint > 0; controller_generation bigint > 0;
  controller_operation_id text not null; admission_enabled boolean;
  lifecycle_phase check in ('offline','starting','online','stopping');
  updated_at; updated_by evidence ID.
- readable by app role, writable only through lifecycle-role functions.
- shared_rate_partitions allocation-enabled flags change in the same lifecycle
  transaction. Offline/stopping disables new allocation in both partitions but
  retains all debt. desired_replicas remains bounded 1..2 as the next/online
  target; it is not changed to zero to mirror the external UAT ASG.

## runtime_termination_intents

- replica_id primary key references runtime_replicas; instance_id and release
  digest must match; request_id unique; reason enum; requested_at.
- lifecycle-command role inserts once and atomically changes active/draining to
  terminating. No cancel/reopen edge exists. App role has SELECT only.

## runtime_leave_receipts

- replica_id primary key references runtime_replicas; controller_generation and
  operation_id bind the prepared scale-in; instance_id/release_digest must
  match; joined_transition_count and joined_claim_count fixed zero; recorded_at
  uses database time.
- finish_graceful_leave inserts once and changes draining to left atomically.
  Exact replay is idempotent; any mismatch fails. Owner trigger and revoked DML
  make the receipt immutable. It cannot be created for terminating or fenced.

## runtime_fencing_proofs

- replica_id uuid primary key references runtime_replicas.
- instance_id, release_digest text not null and must match the replica row.
- adapter text check adapter='ec2_terminated_v1'.
- evidence_id text unique not null; requested_at, observed_terminated_at,
  recorded_at timestamptz not null; observed state fixed to 'terminated'.
- inserted only by the lifecycle fencing role. Immutable by trigger and grants.
- one definer-owned function validates the referenced row and atomically inserts
  proof. It changes joining/active/draining/terminating to fenced. A terminal
  left row stays left and gains audit proof only. App role has SELECT only. App
  has no direct runtime_replicas update. Named app functions permit join-ready
  and drain changes; lifecycle functions alone activate, terminate, and fence.

## Database privilege boundary

- A non-login `aboutme_runtime_owner` owns new tables, triggers, indexes and
  SECURITY DEFINER functions. Revoke ALL on each new table, sequence and
  function from PUBLIC. Revoke CREATE on schema public from app,
  lifecycle-command, fencing-proof, and maintenance roles.
- Every definer function sets `search_path = pg_catalog`, schema-qualifies all
  `public.<table>` references, validates fixed enums and UUIDs, and uses no
  dynamic SQL. Grant EXECUTE only to its named role.
- App role gets SELECT only on proof, leave receipt, termination intent and
  capacity. It gets no direct DML on new runtime, transition, rate or claim
  tables. Exact app definer functions mediate register/mark-join-ready/drain,
  transition and admission. App cannot activate a replica or enable a capacity
  partition.
- Two non-app roles get no table DML. `aboutme_lifecycle_command` may execute
  only prepare_scale_out, activate_replica_capacity, prepare_scale_in,
  finish_scale_in, begin_uat_shutdown, finish_uat_shutdown, begin_wake,
  finalize_stop_receipt, and begin_replica_termination. `aboutme_fencing_proof`
  may execute only record_ec2_termination. The app role alone may execute
  finish_graceful_leave for its authenticated replica after local join proof.
  Each function checks an expected controller generation and operation ID.
- Bootstrap uses explicit grants and default-privilege revocation. Existing
  broad app grants must not grant new-table DML. Tests inspect
  information_schema and attempt forbidden statements through every real role,
  including denial of proof execution to lifecycle-command and lifecycle
  execution to proof writer.
- Cluster roles are provisioned once outside per-database goose migrations.
  Goose owns database-local functions, grants, and revokes but never CREATE ROLE
  or DROP ROLE, so applying/rolling back aboutme cannot break aboutme_dev on the
  same cluster. Local/test bootstrap creates shared cluster roles idempotently,
  then applies independent database grants.
- Hosted database passwords remain standard SSM Parameter Store SecureString
  values under the retained KMS key required by docs/design/deployment.md. ECS
  task execution roles resolve only their named parameter. Do not substitute
  Secrets Manager, pass plaintext through Step Functions, or share credentials
  among app, maintenance, lifecycle-command, and fencing-proof roles.

## Write entry and completion

`runtime_write_state` is defined in
[UAT lifecycle](uat-lifecycle.md#runtime_write_state). The
[transaction contract](transaction-entry.md) requires explicit
runtime_finish_write immediately before commit. A deferred assertion only checks
completion; it cannot advance generation because SET CONSTRAINTS can force it
early.

Each backend uses an owner-created `pg_temp.runtime_write_entry_v1` with ON
COMMIT DELETE ROWS. Fixed static PL/pgSQL DDL creates its table and constraint
trigger. Entry validates ownership, shape, constraints and trigger before reuse.
No login has marker-table privileges. Marker mismatch is project SQLSTATE AM001
and requires destruction of the physical connection. Ordinary unavailable state
uses 55000. A committed marker lookalike is never adopted or repaired. The
transaction finish guard in the transaction contract survives DISCARD TEMP;
temporary ownership alone does not prevent marker removal after forced checks.

Grant enter/finish to app, maintenance, lifecycle-command and fencing-proof.
Migrator receives only its session entry, per-migration begin, finish and exit
primitives and the bounded read-only metadata accessor. Restore receives none.
All assertion helpers remain owner-only. The state update is explicit and once
per dirty transaction. A checked table/operation catalog marks accepted
application writes automatically; maintenance and bookkeeping do not extend the
application writer tail.

## public_transitions

- transition_id uuid primary key; initiator_replica_id references replicas.
- operation text constrained to the existing mutation inventory; state check in
  ('closing','committed','rolled_back','unresolved').
- created_at, deadline_at, terminal_at timestamptz; deadline_at is exactly the
  caller's existing five-second deadline and cannot be extended.
- target_digest bytea length 32; terminal_error_code nullable and bounded; no
  request body or secret. Per-target rows are the only committed-result source.
- closing rows may become committed or rolled_back once. unresolved is only for
  invalid/corrupt recovery evidence and fails readiness; no normal retry writes
  through it.

## public_transition_targets

- primary key (transition_id, ordinal); unique transition/discovery singleton;
  unique transition/resume_id.
- kind check in ('discovery','resume'); discovery uses resume_id null; resume
  uses uuid. expected_generation bigint > 0.
- class check in ('non_draining','revoking'); discovery must be revoking.
- ordinal 0 is discovery when present; resume targets follow in ascending UUID
  byte order. A digest covers every ordered field.
- result_kind nullable check in ('generation','retired'); result_generation
  bigint nullable; result_recorded_at nullable. While closing, all are null. At
  committed terminal state each target has exactly one result: generation
  requires result_generation > 0; retired requires null. Discovery permits
  generation only. Rollback leaves all result fields null.
- A terminal function/constraint trigger refuses committed until every result is
  valid and prevents all result changes after terminal state.

## public_transition_replicas

- primary key (transition_id, replica_id); snapshot_state fixed 'required'.
- replica release/instance identity copied for audit; target_digest bytea.
- row exists for every active, draining or terminating unfenced incarnation
  snapshotted at creation. A terminating incarnation cannot ack, so begin may
  reject immediately rather than wait; it cannot omit that incarnation.

## public_transition_acks

- primary key (transition_id, replica_id), foreign key to required snapshot.
- target_digest, acked_at, local_result text fixed 'closed'.
- revoking_count, non_draining_count nonnegative for metrics only.
- insert is idempotent only when all values match; mismatch is corruption and
  marks runtime unready. An ack cannot be updated or reused.

## Notification channels

- aboutme_public_transition carries only version and transition UUID.
- aboutme_runtime_terminal carries version, transition UUID, terminal state.
- NOTIFY is a wake-up after commit. Payload never carries authority or private
  identity. Agents also poll authoritative rows every 250 ms while any closing
  transition exists and every readiness cache refresh while idle.
