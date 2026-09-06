# Runtime schema and privileges

Status: Accepted under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). This is
the target contract. Local implementation and hosted proof remain Phase 10
gates.

## Durable schema contract

Root owns the migration, SQL sources, generated sqlc code, grants, and schema
tests. Names below are contract names; authors do not edit migration files until
root assigns the schema slice.

[Replica membership](replica-membership.md),
[lifecycle operations](lifecycle-operations.md) and
[lifecycle replay](lifecycle-replay.md) define the exact identity, function and
replay contracts. [Public transitions](public-transitions.md) and
[transition commit](transition-commit.md) define the ordered target digest,
typed functions, transaction capability and recovery actors.
[Transition storage](transition-storage.md) fixes the four-table columns,
terminal matrix, digest assertion and immutable child sets.
[Recovery evidence](transition-recovery.md) fixes parent-locked rollback and
separates response receipts from commit authority.
[Reconciliation](transition-reconciliation.md) fixes the one-snapshot read and
local ordering that preserve later generations and proved retirements.
[Wake migrations](wake-migrations.md) adds migrator-only fixed session entry and
exact source admission during a recorded closing-gate wake. Normal migration and
read-only Status retain their existing contracts.
[Exclusive lifecycle entry](lifecycle-write-entry.md) fixes the two wake
methods, marker, table/action catalog and historical write-generation results.

[Rate storage](rate-storage.md) fixes owner-controlled shared_rate_policies,
shared_rate_policy_key_shapes, shared_policy_clocks, shared_rate_partitions,
shared_rate_buckets, shared_rate_overflow and shared_admission_attempts. App
receives only fixed admission calls; maintenance receives only bounded cleanup
calls. Key-version evidence remains private R8 composition input, with no new
replica column, registration argument or lifecycle ledger field.

[Fixed claim operations](claim-operations.md) adds a result-only composite in a
later operation migration, exact role/kind checks and value/presence transport.
It preserves claim storage. Its AM002 conflict code also fixes the previously
unnamed membership/lifecycle replay code; stored corruption remains AM001.

## runtime_replicas

- replica_id uuid primary key, generated randomly by composition at each boot.
- replica_kind text fixed to serving or maintenance; immutable. Only serving
  replicas count toward desired capacity and public readiness. Maintenance
  replicas may acquire only C03 mail.send from the shared-claim catalog.
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

## runtime_replica_tasks

- task_arn text primary key; replica_id references runtime_replicas; task_role
  fixed to caddy, go or nuxt; unique(replica_id, task_role).
- Rows are immutable. A deferred assertion requires exactly three children and
  an exact match with the parent's three role-specific ARNs. The global primary
  key prevents concurrent cross-role task reuse.

## runtime_capacity

- singleton boolean primary key fixed true; desired_replicas smallint check
  1..2; generation bigint > 0; controller_generation bigint > 0;
  controller_operation_id text not null; admission_enabled boolean;
  lifecycle_phase check in ('offline','starting','online','stopping');
  updated_at; updated_by evidence ID.
- Readable by app; writable only through fixed owner-controlled definers.
- Membership definers also advance generation for fresh register, ready, leave
  and proof changes. They preserve controller_generation and never grant app
  direct capacity DML. Lifecycle actions advance both generations once. Replay,
  rejected calls, rollback and transition recovery advance neither.
- shared_rate_partitions allocation-enabled flags change in the same lifecycle
  transaction. Offline/stopping disables new allocation in both partitions but
  retains all debt. desired_replicas remains bounded 1..2 as the next/online
  target; it is not changed to zero to mirror the external UAT ASG.
- Rate partitions are logical fleet capacity, with no node-to-partition
  assignment. Either exact serving replica may be replaced or selected for
  scale-in; retained rate debt does not move with a physical node. First and
  second serving activation enable partitions 1 and 2. Failure and fencing
  preserve their flags, including with zero survivors; replacement inherits that
  capacity. Proved scale-in disables partition 2 and final shutdown disables
  both. Maintenance activation changes neither.
- The installation seed is desired one, capacity/controller generations one,
  online, admission enabled and `bootstrap-uncomposed-v1` for both controller
  operation and updated_by. Both logical partitions start disabled. It creates
  no replica, ledger or termination evidence. See the exact
  [installation seed](lifecycle-operations.md#installation-seed). The existing
  write foundation stays open; only finalization creates closed UAT.

## Lifecycle operation ledger

`runtime_lifecycle_operations` has an immutable operation_id primary key and
closed workflow_kind. `runtime_lifecycle_operation_steps` has primary key
(operation_id, action), a foreign key to its parent, expected/result controller
generations, fixed 32-byte argument/result digests, typed immutable historical
result columns and database recorded_at. Replay reads those stored columns,
never current membership or capacity rows. Workflow/action checks enforce the
exact predecessor map in [lifecycle replay](lifecycle-replay.md). No deletion
path exists.

Every lifecycle operation locks public_state then runtime_capacity before
reading or inserting its operation parent and action. The singleton serializes
fresh IDs. Each fresh action increments controller generation once; exact replay
returns stored durable fields with replayed=true. The derived replayed flag is
excluded from the result digest.

The exact
[nullable result matrix](lifecycle-replay.md#operation-schema-and-digest)
requires capacity counts and flags for capacity actions and finish-scale-in.
Replica actions store exact replica fields; wake actions alone store gate and
write generation. The typed `argument_replaced_replica_id` is non-null only for
replacement activation and has a unique partial index preventing reuse. No
argument or historical result uses JSON.

## runtime_termination_intents

- replica_id primary key references runtime_replicas; instance_id and release
  digest must match; request_id unique; reason fixed to startup_failed,
  readiness_failed or drain_failed; requested_at. The exact source-state and
  controller prerequisites are in
  [lifecycle operations](lifecycle-operations.md).
- lifecycle-command role inserts once and atomically changes
  joining/active/draining to terminating. No cancel/reopen edge exists. App role
  has SELECT only.

## runtime_leave_receipts

- replica_id primary key references runtime_replicas; controller_generation and
  operation_id bind the historical prepare step result, even after later
  controller actions; instance_id/release_digest must match;
  joined_transition_count and joined_claim_count fixed zero; recorded_at uses
  database time.
- finish_graceful_leave inserts once and changes draining to left atomically.
  Exact replay is idempotent; any mismatch fails. Owner trigger and revoked DML
  make the receipt immutable. It cannot be created for terminating or fenced.

## runtime_fencing_proofs

- replica_id uuid primary key references runtime_replicas.
- instance_id, release_digest text not null and must match the replica row.
- adapter text check adapter='ec2_terminated_v1'.
- evidence_id text unique not null; request_id text unique not null, 1..128
  printable ASCII bytes; reclaimed_claim_count integer not null and >=0.
  requested_at, observed_terminated_at and recorded_at timestamptz not null;
  observed state fixed to 'terminated'.
- A later membership-evidence migration adds request_id and
  reclaimed_claim_count only when the legacy proof table is empty. It rejects
  nonempty legacy evidence without deleting rows or inventing history. Migration
  00015 remains unchanged. [Evidence operations](membership-evidence.md) fix the
  upgrade and historical replay contract.
- inserted only through the aboutme_fencing_proof wrapper. Immutable by trigger
  and grants, including both added fields.
- one definer-owned function validates the referenced row and atomically inserts
  proof. It changes joining/active/draining/terminating to fenced. A terminal
  left row stays left and gains audit proof only. App has no direct
  runtime_replicas update. Named app functions register, mark ready and finish
  serving leave. Lifecycle functions activate and prepare drain/termination; the
  fencing-proof wrapper alone records EC2 proof.

## Database privilege boundary

- A non-login `aboutme_runtime_owner` owns new tables, triggers, indexes and
  SECURITY DEFINER functions. Revoke ALL on each new table, sequence and
  function from PUBLIC. Revoke CREATE on schema public from app,
  lifecycle-command, fencing-proof, and maintenance roles.
- Before later runtime migrations, fresh migration 00014 and fixed local/test
  adoption converge the enumerated legacy objects on runtime_owner. Follow
  [migration provisioning](migration-provisioning.md); preserve application
  rows, object identities, definitions and non-owner effective privileges.
- Every definer function sets `search_path = pg_catalog`, schema-qualifies all
  `public.<table>` references, validates fixed enums and UUIDs, and uses no
  dynamic SQL. Grant EXECUTE only to its named role.
- App role gets SELECT only on proof, leave receipt, termination intent and
  capacity. It gets no direct DML on new runtime, transition, rate or claim
  tables. Exact app definer functions mediate register/mark-join-ready/drain,
  transition and admission. App cannot activate a replica or enable a capacity
  partition.
- Lifecycle-command and fencing-proof get no table DML. Lifecycle-command
  executes only the named capacity, maintenance-drain and wake operations in
  [lifecycle operations](lifecycle-operations.md), plus the separately gated UAT
  shutdown/final-stop functions. Capacity actions use expected controller
  generation and operation ID. Lifecycle-command also receives the narrow
  [fenced-initiator rollback](transition-commit.md) function; it takes no
  capacity or controller-ledger locks and cannot acknowledge or commit work.
  Fencing-proof executes only runtime_record_ec2_termination, which never reads
  or mutates transition rows. App and maintenance receive distinct fixed-kind
  graceful-leave wrappers after their local join proof. Shared app credentials
  do not authenticate a per-process incarnation; the private adapter binds the
  exact immutable tuple and SQL checks it.
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

Grant ordinary enter/finish to app, maintenance, lifecycle-command and
fencing-proof for their shared write methods. Only lifecycle-command receives
the two fixed wake entry and action functions. Wake methods use their exclusive
marker and finish assertion, never ordinary entry or finish. The wake branch
accepts only its fixed table/action catalog and no business writes. Its
statement trigger checks entry identity and table/operation only. Owner BEFORE
ROW triggers bind operation-parent workflow and step action/workflow to the
marker's exact operation ID and mode. The fixed entry signatures are
`runtime_enter_begin_wake(operation_id text, wake_mode text)` and
`runtime_enter_complete_wake(operation_id text)`. Migrator receives only its
session entry, per-migration begin, finish and exit primitives and the bounded
read-only metadata accessor. Restore receives none. All assertion helpers remain
owner-only. The state update is explicit and once per dirty transaction. A
checked table/operation catalog marks accepted application writes automatically;
maintenance and bookkeeping do not extend the application writer tail.

## public_transitions

- transition_id uuid primary key; required initiator_replica_id,
  initiator_instance_id and initiator_release_digest use a composite foreign key
  to immutable membership.
- operation text constrained to the existing mutation inventory; state check in
  ('closing','committed','rolled_back','unresolved').
- created_at, deadline_at, terminal_at timestamptz; deadline_at equals
  created_at plus five seconds. Begin projects the original remaining monotonic
  budget onto database statement time and derives created_at from that deadline.
  The original caller deadline still bounds all work; it never restarts on SQL,
  lock wait or recovery. See [public transitions](public-transitions.md).
- target_digest bytea length 32; terminal_error_code uses the exact closed
  [state matrix](transition-storage.md#parent-identity-and-state). No request
  body or secret is stored. Per-target rows are the only committed-result
  source.
- recovery_fencing_evidence_id nullable text references the unique fencing proof
  evidence_id. It is non-null exactly for rolled_back with reason
  initiator_fenced, and immutable. Other outcomes keep it null.
- closing rows may become committed, rolled_back or unresolved once. Terminal
  rows are immutable. Unresolved requires proved durable recovery evidence and
  fails readiness; its callable resolver remains gated by the
  [evidence contract](transition-storage.md#unresolved-evidence-boundary).

## public_transition_targets

- primary key (transition_id, ordinal); unique transition/discovery singleton;
  unique transition/resume_id.
- kind check in ('discovery','resume'); discovery uses resume_id null; resume
  uses uuid. expected_generation bigint > 0.
- class check in ('non_draining','revoking'); discovery must be revoking.
- Each parent has 1..4 targets with contiguous ordinals from zero. Discovery is
  first when present; resumes follow in ascending UUID byte order. An owner
  helper recomputes the exact ordered target digest for a deferred assertion.
- result_kind nullable check in ('generation','retired'); result_generation
  bigint nullable; result_recorded_at nullable. While closing, all are null. At
  committed terminal state each target has exactly one result: generation
  requires result_generation > 0; retired requires null. Discovery permits
  generation only. Rollback and unresolved leave all result fields null.
- A terminal function/constraint trigger refuses committed until every result is
  valid and prevents all result changes after terminal state.

## public_transition_replicas

- primary key (transition_id, replica_id); snapshot_state fixed 'required'.
- replica release/instance identity copied for audit; target_digest bytea.
- Composite foreign keys bind immutable membership and parent digest. Every
  parent has a nonempty required set. Rows cannot change or be deleted.
- row exists for every active, draining or terminating unfenced incarnation
  snapshotted at creation. A terminating incarnation cannot ack, so begin may
  reject immediately rather than wait; it cannot omit that incarnation.

## public_transition_acks

- primary key (transition_id, replica_id), foreign key to required snapshot.
- The foreign key also binds target_digest. Instance/release identity comes from
  that immutable required row and is not duplicated in the ack.
- target_digest, acked_at, local_result text fixed 'closed'.
- revoking_count, non_draining_count nonnegative for metrics only.
- insert is idempotent only when all values match; mismatch is corruption and
  marks runtime unready. An ack cannot be updated or reused.
- Committed parents require every required ack. Closing, rolled_back and
  unresolved permit any observed subset, including the full set. Owner
  assertions reject terminal child inserts and all four tables reject TRUNCATE,
  including under valid write entry.

## Notification channels

- aboutme_public_transition carries only version and transition UUID.
- aboutme_runtime_terminal carries version, transition UUID, terminal state.
- NOTIFY is a wake-up after commit. Payload never carries authority or private
  identity. Agents also poll authoritative rows every 250 ms while any closing
  transition exists and every readiness cache refresh while idle.
