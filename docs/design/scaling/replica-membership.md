# Replica membership and fencing

Status: Accepted under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). The
[scaling index](README.md) records whether it is built.

This contract defines immutable replica identity, join readiness, graceful
leave, exact EC2 fencing, role grants, and the lock boundary with public
transitions. It does not change a public API.

## Replica schema

runtime_replicas retains the accepted columns and adds `replica_kind text` fixed
to `serving` or `maintenance`. This is the durable field needed by the accepted
private no-ALB maintenance path; a wrapper cannot fix an unstored
`serving=false` property. Kind is immutable. Serving replicas count against
desired_replicas and may become public-ready. Maintenance replicas do not count
against public desired capacity, cannot become public-ready, and may acquire
only shared-claim policy C03 mail.send. Both kinds use the same
joining/active/draining/left/terminating/ fenced lifecycle and exact EC2 proof
rule.

Do not persist a node-to-rate-partition assignment. shared_rate_partitions 1 and
2 are logical fleet allocation capacity, not replica slots. Flags change only at
named lifecycle boundaries. First serving activation enables partition 1 and
second serving activation enables partition 2. Proof and node loss preserve both
flags even when no serving replica survives. Replacement restores active
capacity without changing flags. Proved scale-in disables partition 2, and final
shutdown disables both. Offline, stopping, and maintenance-only wake keep both
disabled.

Identity constraints:

- replica_id is nonzero UUID, generated once by private composition at every
  boot.
- instance_id matches exact lower-case `i-[0-9a-f]{17}` and is <=19 bytes.
- container_instance_arn and each task ARN are nonempty <=512-byte ASCII
  strings, match the pinned region/account/cluster ARN prefix supplied by
  private deployment config, and are pairwise role-specific. SQL compares their
  full strings; it does not parse identity from an untrusted suffix.
- release_digest matches `sha256:[0-9a-f]{64}` exactly.
- release_digest identifies a build and may be shared by both replicas of that
  build. It has no global uniqueness constraint. Once stored for a replica UUID,
  changing that UUID's release digest or any other identity field conflicts.
- unique (instance_id, replica_id); one nonterminal incarnation per instance.
- `runtime_replica_tasks` normalizes the trio: task_arn text primary key;
  replica_id foreign key to runtime_replicas; task_role text fixed to caddy, go,
  or nuxt; unique (replica_id, task_role). Owner immutability triggers reject
  update/delete. A deferred constraint trigger requires exactly three child rows
  and requires each role's child ARN to equal the corresponding immutable parent
  column before commit. Registration inserts children in caddy, go, nuxt order.
  The global task_arn primary key therefore rejects cross-column and concurrent
  reuse rather than relying on three independent parent-column constraints.
  Exact identity never changes.
- state/timestamp/kind checks match runtime-schema.md. State moves only
  joining->active->draining->left, joining/active/draining->terminating->fenced,
  or joining/active/draining->fenced through verified proof. Left/fenced
  terminal.

Every fresh successful register, mark-ready, graceful-leave, or EC2-proof
transaction increments capacity generation exactly once and returns the
resulting value. It never increments controller generation. A proof that adds
audit evidence to an already-left replica still increments capacity generation
because durable membership evidence changed. Exact replay, rejected calls,
read-only recovery, and rollback increment neither generation. The later
fenced-transition recovery changes only its transition parent and increments
neither capacity nor controller generation. App, maintenance, and proof roles
change capacity generation only through their named definer functions and have
no direct capacity DML.

runtime_termination_intents, runtime_leave_receipts and runtime_fencing_proofs
use the exact fields in runtime-schema.md. Add composite foreign keys to
immutable replica_id/instance_id/release_digest identity. Request/evidence IDs
are 1..128 printable ASCII. Each is unique within its record type; a proof may
reuse only its exact replica's intent request ID. Intents and receipts have
owner triggers that reject UPDATE/DELETE. Proof rows reject UPDATE/DELETE and
duplicate mismatch. The [evidence operation contract](membership-evidence.md)
adds durable proof request_id and reclaimed_claim_count in a later migration,
with an empty legacy-proof prerequisite and exact historical replay.

## Database time

Each mutator samples clock_timestamp once after acquiring its rows. For a new
database event, effective_at is greatest(raw sample,
runtime_capacity.updated_at, and all nonnull prior timestamps on the affected
replica/operation). Store raw external requested_at and observed_terminated_at
only in fencing proof; require requested_at <= observed_terminated_at. External
observed time is diagnostic and has no database-clock skew ceiling. Exact
terminated identity is the proof authority. Generation/CAS, not wall-clock
order, is authoritative. Callers never supply
joined/ready/activated/draining/left/recorded time.

## Typed results

Define owner composite results so sqlc returns typed rows rather than overloaded
booleans:

runtime_replica_result( replica_id uuid, replica_kind text, state text,
desired_replicas smallint, active_serving_replicas smallint,
active_maintenance_replicas smallint, partition_1_enabled boolean,
partition_2_enabled boolean, capacity_generation bigint, controller_generation
bigint, controller_operation_id text, admission_enabled boolean, lifecycle_phase
text, replayed boolean)

The desired/count/partition fields are nonnull only for finish-scale-in. Other
replica-result actions require them null. Common and replica fields are always
nonnull.

runtime_capacity_result( desired_replicas smallint, active_serving_replicas
smallint, active_maintenance_replicas smallint, partition_1_enabled boolean,
partition_2_enabled boolean, capacity_generation bigint, controller_generation
bigint, controller_operation_id text, admission_enabled boolean, lifecycle_phase
text, write_gate text, write_generation bigint, replayed boolean)

write_gate and write_generation are nonnull only for begin-wake and
complete-wake results. Other action result shapes require both null.

runtime_leave_result( replica_id uuid, state text, receipt_operation_id text,
controller_generation bigint, joined_transition_count integer,
joined_claim_count integer, recorded_at timestamptz, replayed boolean)

runtime_fence_result( replica_id uuid, state text, evidence_id text,
reclaimed_claim_count integer, recorded_at timestamptz, replayed boolean)

All counts are nonnegative. A mismatched replay raises AM002 under the
[fixed operation error contract](claim-operations.md#roles-and-errors);
closed/stale/unready raises 55000; marker/catalog corruption raises AM001. Error
details contain no ARN, instance, release or evidence value.

## Registration and join readiness

runtime_register_serving_replica( replica_id uuid, instance_id text,
container_instance_arn text, caddy_task_arn text, go_task_arn text,
nuxt_task_arn text, release_digest text) -> runtime_replica_result

runtime_register_maintenance_replica( replica_id uuid, instance_id text,
container_instance_arn text, caddy_task_arn text, go_task_arn text,
nuxt_task_arn text, release_digest text) -> runtime_replica_result

Under one transaction, validate the complete tuple, lock public_state then
runtime_capacity, insert joining, then insert its three normalized task rows in
caddy/go/nuxt order. When the UUID already exists, exact replay verifies the
immutable parent and all three children, performs no mutation or generation
increment, and returns the membership and capacity state visible in that replay
transaction with replayed=true. It does not promise the historical state
returned by the original registration. The caller must accept only joining as
registration progress; active, draining, left, terminating, or fenced is an
authoritative current outcome. Reuse of a UUID, instance, or cross-role task
with a different tuple conflicts. The same release digest may identify several
replicas. Concurrent cross-role task reuse loses on the child primary key; the
deferred exact-three assertion prevents a partial trio. A new row remains
joining and unready when closing/unresolved transitions or
unapproved/offline/stopping capacity are visible.

runtime_mark_serving_replica_join_ready( replica_id uuid, instance_id text,
container_instance_arn text, caddy_task_arn text, go_task_arn text,
nuxt_task_arn text, release_digest text) -> runtime_replica_result

runtime_mark_maintenance_replica_join_ready( replica_id uuid, instance_id text,
container_instance_arn text, caddy_task_arn text, go_task_arn text,
nuxt_task_arn text, release_digest text) -> runtime_replica_result

Lock public_state, capacity and replica. Require joining, exact identity/trio,
no termination intent, and no visible closing or unresolved transition. The
accepted capacity pairs are exactly (online,true) or (starting,false) for
(lifecycle_phase,admission_enabled). Set join_ready_at once and keep joining.
Exact replay is idempotent. Mark-ready replay retains the joining prerequisite;
it does not inherit registration's later-state replay permission. The private
adapter calls this only after normal query, transition listener, revision
LISTEN, paired Nuxt/Caddy, shared admission rollback and unresolved-transition
probes pass. SQL cannot infer those local probes.

Ordinary write entry must still find an open write gate. A starting lifecycle
does not bypass a closing/closed gate; the normal UAT sequence remains
complete-wake, register, mark-ready, then activate. Mark-ready neither requires
enabled rate partitions nor changes them: first readiness precedes the
activation that enables partition 1. Joining grants no serving or claim work.

Fresh registration and mark-ready set capacity.updated_by to their exact public
function name. These are fixed internal evidence literals. Replay preserves
updated_by and updated_at; callers supply no additional operation ID.

Use the two register functions as wrappers over an owner-only helper. They store
their fixed replica_kind. Grant runtime_mark_maintenance_replica_join_ready only
to maintenance, with the database/listener/coordination probes applicable to a
private node and no public-readiness result. Maintenance may join and own
allowed claims but cannot call the serving ready wrapper or make Caddy
public-ready. The app wrapper stores serving and app can call only serving
mark-ready.

These functions receive replica identity supplied by a private operation
adapter. Serving replicas share one app database login, so SQL does not
authenticate a per-process principal. Exact tuple comparison is CAS/bug
containment, not a new credential boundary. Maintenance is role-distinct because
its accepted login is separate. No caller may provide timestamps, state,
partition or generation output.

## Graceful leave

runtime_finish_serving_graceful_leave( replica_id, instance_id, release_digest,
expected_controller_generation, operation_id) -> runtime_leave_result

runtime_finish_maintenance_graceful_leave( replica_id, instance_id,
release_digest, expected_controller_generation, operation_id) ->
runtime_leave_result

Lock public_state, capacity and replica, then claim parents by UUID. Never lock
or wait on a transition parent. Require draining, exact prepare-scale-in
generation/ operation and identity, no termination intent, a nonlocking visible
predicate that finds zero owned closing/unresolved transition, and zero waiting/
running claim. Insert immutable receipt with both zero counts and change to left
in one transaction. Exact replay returns the receipt. It is the app's final
database mutation after all local callbacks/processes join; SQL proves durable
ownership counts, while R8 proves the local join. It cannot release work or make
readiness reopen. The wrappers fix and verify serving or maintenance kind.
Serving leave requires the matching prepare-scale-in step. Maintenance leave
requires the matching prepare-maintenance-drain step. The expected controller
generation and receipt bind that step's historical result generation. Unrelated
later controller actions do not invalidate a prepared leave. Neither role can
call the other wrapper.

## Exact EC2 fencing

runtime_record_ec2_termination( replica_id uuid, instance_id text,
release_digest text, request_id text, evidence_id text, requested_at
timestamptz, observed_terminated_at timestamptz, observed_state text) ->
runtime_fence_result

Lock public_state, capacity, replica, matching intent when present and proof
row, then claims by UUID and summaries in claim canonical order. Never lock,
wait on, ack, mutate or predicate proof insertion on a transition parent. A
visible closing/unresolved transition owned by the dead incarnation does not
weaken exact EC2 evidence and cannot block fencing. Require observed_state
exactly `terminated`, complete identity match, unique evidence, and request
match when an intent exists. The proof writer itself must have immediately
described that exact EC2 ID as terminated; SQL records its authenticated input
but does not call AWS. Exact proof replay compares the stored request ID even
without an intent and returns the stored reclaimed-claim count after later claim
receipt cleanup. Request IDs are unique among proofs and cannot name another
replica's intent. Mismatch conflicts.

For left, insert audit proof and retain left. For joining/active/draining/
terminating without leave, insert proof, set fenced and atomically release only
that replica's claims through the owner helper. It cannot ack a transition,
commit business work, revive a mutation, alter another replica, or change a
transition row. Any closing/unresolved transition remains unchanged for its
separate recovery protocol. Cleanup processes all owned live claims atomically
under a bounded caller context; it has no fixed row/work bound. Timeout grants
no success, and only confirmed rollback proves unchanged rows. Proof never
infers death from ECS, ALB, heartbeat, deadline, lock or database loss. If proof
commit is ambiguous, resolve by evidence ID and full tuple; absence authorizes
one same-evidence retry, mismatch remains closed.

After the fencing transaction commits, lifecycle-command may call
`runtime_recover_fenced_public_transition(transition_id, target_digest, initiator_replica_id, fencing_evidence_id)`
in a separate WriteTxRunner transaction. That function takes no membership or
controller lock. It locks the transition parent first, validates the immutable
initiator tuple through the exact fencing proof and fenced membership row, and
may CAS only `closing` to `rolled_back` with reason `initiator_fenced`. Proof
cannot call it or mutate a transition.

## Lock order

After WriteTxRunner's shared runtime barrier, operations take only the needed
prefix/subsequence of:

1. public_state singleton;
2. runtime_capacity singleton;
3. runtime_lifecycle_operations parent, then operation steps;
4. runtime_replicas in UUID byte order, then normalized task children in role
   order;
5. termination intent, leave receipt and fencing proof for those replicas;
6. claim parents in claim UUID order;
7. claim policy/summaries in their accepted canonical order;
8. shared_rate_partitions in numeric order.

Membership register/activate/drain/leave never lock or wait on a transition
parent. While holding their membership locks they use only nonlocking visible
closing/ unresolved predicates and fail closed. Fencing neither waits nor
predicates on a transition: exact EC2 proof may fence the dead incarnation and
reclaim only its claims while its transition stays unchanged. An ack already
holding its parent may later wait on the replica; it must then recheck exact
active/draining state and fails if membership committed left/fenced. This
preserves the transition contract's parent->replica and business
parent->public_state directions without a reverse membership edge. Claim acquire
does not touch public_state and keeps capacity->replica->claim->summary. Rate
admission normally touches no membership row and keeps its policy order. No
lifecycle callback holds an AWS, HTTP, local mutex or application row lock while
entering these database locks.

## Privileges and composition

- aboutme_runtime_owner owns tables, indexes, triggers, functions and types but
  cannot login. PUBLIC has ALL revoked. All functions fix
  search_path=pg_catalog, schema-qualify persistent objects and use no dynamic
  SQL.
- aboutme_app executes serving register, mark-ready and serving-graceful-leave
  plus later app transition/admission functions. It has only accepted SELECT
  visibility and no direct runtime/capacity/intent/receipt/proof/rate/claim DML.
- aboutme_maintenance executes private register, maintenance-ready and
  maintenance-graceful leave, shared-claim C03 mail.send, and its existing named
  job-lease functions. It cannot mark serving ready, open the gate or activate
  capacity.
- aboutme_lifecycle_command executes prepare-scale-out, activate-capacity,
  prepare-scale-in, finish-scale-in, prepare-maintenance-drain,
  begin-replica-termination, begin-wake, complete-wake, and the separate
  runtime_recover_fenced_public_transition. It cannot record proof, forge leave,
  ack or commit a transition, direct DML, or call claim fence cleanup.
- aboutme_fencing_proof executes only runtime_record_ec2_termination. It cannot
  prepare lifecycle, activate, forge leave/ack, or direct DML.
- assertion/immutability/helper triggers and claim reclamation helpers are
  owner-only and attach only as triggers or internal calls. No login executes
  them.

R1 installs owner-owned tables, constraints, types, definer bodies and the exact
named runtime-role grants above. PUBLIC remains revoked. Tests connect as every
real role and prove allowed calls and cross-role denials. Do not create
finalize_stop_receipt or grant final-stop in this slice. Do not compose app
constructors, readiness or lifecycle tasks yet.

After rates, claims and transitions exist and their local tests pass, R8 wires
constructors against the fixed functions and real role grants. R8 constructs the
private identity, membership, lifecycle, and proof adapters before registration,
probes, or activation can run. Successful one-replica registration, join,
activation, and aggregate readiness then permit public serving. Two-replica
serving remains disabled until the unchanged candidate proves R1-R8 locally.
Database grants remain the security authority; composition supplies private
identity and controls public enablement timing.

## Required membership cases

[Required cases](membership-cases.md) cover identity, readiness, leave, fencing,
generations, role grants and two-pool races. The evidence contract adds exact
proof replay fields and migration checks.
