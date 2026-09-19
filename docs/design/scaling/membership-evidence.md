# Membership evidence operations

The two graceful-leave wrappers and exact EC2 proof function use the
[membership](replica-membership.md), [lifecycle](lifecycle-operations.md),
[claim](claim-operations.md) and [write-entry](transaction-entry.md) contracts.
[SQL signatures and store transport](membership-evidence-transport.md) fix the
three callable methods. This slice adds the two retained proof fields needed for
exact replay in a later migration; migration 00015 remains unchanged.

## Existing durable rows and result types

- runtime_leave_receipts is keyed by replica_id and stores immutable
  replica_id/instance_id/release_digest, controller_generation, operation_id,
  joined_transition_count=0, joined_claim_count=0, and recorded_at.
- runtime_fencing_proofs is keyed by replica_id and has unique evidence_id. The
  later membership-evidence migration adds request_id text NOT NULL with the
  accepted printable 1..128-byte check and UNIQUE, plus reclaimed_claim_count
  integer NOT NULL CHECK >=0. The resulting immutable row stores replica
  identity, adapter=ec2_terminated_v1, request/evidence IDs, raw requested_at,
  raw observed_terminated_at, observed_state=terminated, historical reclaimed
  parent count, and recorded_at.
- runtime_leave_result is exactly replica_id, state, receipt_operation_id,
  controller_generation, joined_transition_count, joined_claim_count,
  recorded_at, replayed.
- runtime_fence_result is exactly replica_id, state, evidence_id,
  reclaimed_claim_count, recorded_at, replayed.
- Receipts and proofs are immutable. They have no delete path. Claim parents and
  scopes released as fenced remain ordinary immutable released receipts until
  the accepted 24-hour claim GC makes them eligible.

## Safe proof-schema upgrade

Migration 00015 remains byte-for-byte unchanged. The later serialized
membership-evidence migration first requires runtime_fencing_proofs to be empty,
then adds request_id and reclaimed_claim_count as required fields, their checks,
and request_id uniqueness before installing any proof writer. If the table is
nonempty, fail closed with a fixed value-free diagnostic and leave the complete
database transaction unchanged. Do not fabricate request IDs or reclaimed counts
and do not delete evidence.

This is safe because no proof-writing function exists before that later
migration. Ordinary populated business, membership, claim, and rate rows are
preserved. A migration fixture must owner-insert one legacy-shaped proof, show
the migration rejects it, and prove that proof plus all other state is
unchanged. No equally exact backfill authority exists in the current schema:
neither request_id nor historical reclaimed count can be reconstructed after
claims change or GC.

## Common validation, time, and generation rules

Validate the direct session_user and scalar shapes before mutable reads. Reject
nil UUID, malformed instance/release identity, nonpositive expected generation,
nonprintable or out-of-bound operation/request/evidence IDs, null timestamps,
requested_at later than observed_terminated_at, or observed state other than
terminated with 22023. SQL compares full immutable identity values and emits no
supplied or stored value in message, detail, hint, schema, table, or column.

After all rows for that operation are locked, call owner-only
`public.runtime_sample_membership_time()` exactly once. It reuses the isolated
test-clock contract from lifecycle operations: runtime_owner-owned, volatile, no
arguments, no login EXECUTE, production body exactly one clock_timestamp, and
serialized one-session replacement/restoration only in isolated migration tests.
For a fresh event, effective_at is GREATEST of the raw database sample,
runtime_capacity.updated_at, and every nonnull prior timestamp on the affected
replica and applicable operation/evidence rows. External requested_at and
observed_terminated_at are stored raw in the proof; they do not enter ordering
authority or impose a skew ceiling. Use effective_at for left_at or fenced_at,
proof/receipt recorded_at, capacity.updated_at, and claim release/summary
updated timestamps created by the same proof transaction.

Every fresh successful leave or proof increments runtime_capacity.generation
exactly once, checks overflow first, and returns the new value only through the
durable membership effects/result contract. It does not increment or rewrite
runtime_capacity.controller_generation. It preserves desired_replicas,
controller_operation_id, admission_enabled, lifecycle_phase, and all rate
partition flags/debt. Set capacity.updated_by to the exact fixed function name
for the fresh public wrapper. Exact replay, rejection, rollback, and read-only
inspection increment neither generation.

## Graceful leave lock and predicate contract

Both wrappers use one owner-only fixed-kind implementation. Lock in this order:

1. public_state singleton;
2. runtime_capacity singleton;
3. exact runtime_replicas row;
4. existing termination intent and leave receipt for the target;
5. target-owned live claim parents in ascending UUID order.

The immutable lifecycle operation parent and prepare step are read as durable
selection evidence without a row lock. Leave does not insert or change lifecycle
rows. It therefore preserves the required public_state, capacity, replica,
evidence, claim UUID, summary prefix and creates no replica-to-ledger wait edge.

Never lock, wait on, or update a public transition parent. Under public_state,
use one nonlocking visible predicate for zero closing/unresolved transitions
owned by the exact replica. The zero count is returned/stored as
joined_transition_count=0. It is a fail-closed precondition, not a lock or join
protocol. The transition may already hold parent then wait for replica; holding
public_state plus the membership locks preserves the accepted ordering and its
post-wait state recheck.

Require the exact replica identity, draining state, wrapper-fixed kind, no
termination intent, no prior conflicting receipt, zero owned waiting/running
claims, and the exact historical prepare evidence:

- Serving wrapper requires a scale_in operation parent and its prepare_scale_in
  step. The step must name this replica/kind/state selection.
- Maintenance wrapper requires a maintenance_wake parent and its
  prepare_maintenance_drain step. The step must name this maintenance replica.
- p_operation_id equals that immutable parent and step operation_id.
- p_expected_controller_generation equals the prepare step's stored
  result_generation, and the receipt stores that value.
- Current runtime_capacity.controller_generation may be greater because
  unrelated valid controller actions may intervene. Leave must not require it
  still equal the prepare generation. The immutable step plus exact target and
  current draining state are the authority. Current controller generation less
  than the stored prepare result is AM001 durable corruption.

This interpretation makes the caller argument an exact historical-selection
reference, not a fresh current-generation CAS. It follows lifecycle replay's
rule that later actions may advance controller generation and avoids rejecting a
locally joined target solely because unrelated controller work committed.

Fresh leave inserts one receipt with zero counts, sets draining to left and
left_at=effective_at, updates capacity generation once, and returns:

- replica_id: exact target;
- state: left;
- receipt_operation_id: stored operation_id;
- controller_generation: stored historical prepare result generation;
- joined_transition_count: 0;
- joined_claim_count: 0;
- recorded_at: stored receipt recorded_at;
- replayed: false.

Existing receipt handling is receipt-first replay authority after identity and
role validation. An exact receipt/identity/operation/generation match returns
the stored fields with replayed=true even after later controller or membership
changes. It performs no live predicates, claim scan, state change, or generation
increment. Same replica with any different immutable receipt identity,
operation, or generation is AM002. Malformed receipt shape or receipt/member
foreign-identity contradiction is AM001.

## EC2 proof lock and mutation contract

Lock only this prefix/order:

1. public_state singleton;
2. runtime_capacity singleton;
3. exact runtime_replicas row;
4. matching runtime_termination_intents row when present, then the proof row;
5. this replica's live claim parents in ascending claim UUID order;
6. affected catalog rows and summaries in the accepted canonical policy/kind and
   bytewise digest order.

Proof takes no runtime_lifecycle_operations or step lock. It never locks, waits
on, reads as a predicate, acknowledges, or changes a public transition parent. A
closing/unresolved transition owned by the replica neither blocks nor weakens
exact terminated evidence. Fenced-transition recovery is a later, separate
lifecycle-command action that locks the transition parent first. Proof cannot
execute or imply it.

Require exact replica identity and observed_state terminated. When a termination
intent exists, p_request_id must equal its immutable request_id and its replica
tuple must match. Any intent anywhere with that globally unique request_id must
belong to this exact replica tuple; valid cross-replica reuse is AM002. An
intent reason/source-state contradiction is AM001. With or without an intent,
request_id is durable proof identity. Exact replay compares every stored proof
field: replica/instance/release, adapter, request_id, evidence_id, requested_at,
observed_terminated_at and observed_state. It validates and returns the stored
reclaimed_claim_count; callers supply no expected reclaimed count.

For a fresh proof:

- left remains left; insert audit proof, reclaim zero claims, and still advance
  capacity generation once because durable evidence changed;
- joining, active, draining, or terminating becomes fenced with
  fenced_at=effective_at;
- release only waiting/running parents owned by this exact replica;
- for every affected parent, lock its children/catalog/summaries canonically,
  decrement the exact waiting/running count once per scope, set parent and all
  children released/fenced with released_at=effective_at, and preserve identity,
  admitted/deadline, request digest, scopes, and allocation ordinals;
- never delete parent, child, or summary, reset next_ordinal, alter another
  replica's claim, or infer claim release from age;
- reclaimed_claim_count counts claim parents changed from waiting/running to
  released in this transaction, not scope rows.

The claim cleanup implementation belongs after the ordinary lifecycle operation
slice and after fixed claim operations are available. Define owner-only
`public.runtime_release_fenced_replica_claims(p_replica_id uuid, p_effective_at timestamptz) RETURNS integer`.
It hard-codes fenced, returns one nonnegative changed-parent count, and is
executable by no login. runtime_record_ec2_termination is its sole public
caller. The helper neither enters/finishes a transaction nor exposes a Go
method.

This cleanup has no fixed row-count or work bound. It may process every live
claim owned by the exact replica, proportional to distinct user/IP/account
scopes. It runs under the caller's bounded operation context, processes claim
UUIDs and canonical summary locks in order, and returns one scalar without an
unbounded Go result or SQL array. It reclaims all claims atomically before
fencing commits and never pages or commits partial cleanup. Timeout,
cancellation or any statement/finish error returns zero authority. Confirmed
rollback preserves all pre-call rows. Ambiguous completion retires the physical
connection and resolves only through one same-evidence call. A timeout is not
proof success. No global cap, age release, cursor, retry ledger, or weaker EC2
rule is added.

Fresh proof returns exact replica_id, resulting state left or fenced, stored
evidence_id, reclaimed parent count, stored recorded_at, and replayed=false.
Existing proof with the exact stored tuple returns those durable fields with
replayed=true and no mutation. It returns stored reclaimed_claim_count and never
recomputes that historical value from current claims after release or GC.

## Proof ambiguity and replay

The transport returns authority only after WriteTxRunner reports successful
finish and commit. Any input, SQL, decode, finish, commit, or cleanup error
returns the zero result plus wrapped cause. It never returns left/fenced or a
receipt/evidence result alongside an error.

After ambiguous proof commit, server composition retains the exact private
request and may issue one same-argument runtime_record_ec2_termination call
through a fresh runner. The function atomically resolves-or-inserts by
evidence/replica uniqueness:

- exact stored proof returns replayed=true;
- absence permits that same-evidence retry to become the one fresh insert;
- evidence ID used for another replica, proof mismatch, replica proof under a
  different evidence ID, or intent/request mismatch is AM002;
- stored impossible/malformed evidence is AM001.

There is no separate proof SELECT grant or public resolver function. The same
idempotent definer is the narrow readback boundary. No retry follows definite
rejection, and no new evidence ID substitutes for an ambiguous one.

## Roles and SQLSTATE

- aboutme_app alone executes serving graceful leave.
- aboutme_maintenance alone executes maintenance graceful leave.
- aboutme_fencing_proof alone executes runtime_record_ec2_termination.
- lifecycle-command, the other runtime roles, restore, and PUBLIC cannot execute
  these functions. No login executes owner helpers or directly mutates receipt,
  proof, replica, capacity, claim, or summary tables.
- Each SECURITY DEFINER checks exact direct session_user before mutable reads,
  uses search_path=pg_catalog, schema-qualified static SQL, and no dynamic SQL.

Error mapping:

- 22023: malformed/nil scalar, time order, observed state, text bound;
- 42501: wrong direct login or fixed-kind wrapper violation;
- 55000: valid but unavailable source state, termination intent that blocks
  leave, live claim/transition that blocks leave, or missing prepare evidence;
- AM002: supplied immutable tuple, receipt identity, proof identity, evidence
  reuse, intent request, or exact replay arguments conflict with valid durable
  identity;
- AM001: marker/catalog/assertion corruption, impossible stored timestamp/state,
  contradictory durable receipt/proof/member/intent/claim/summary evidence, or
  count underflow. AM001 keeps existing physical-connection retirement.

## Required proof

- Real-role ACL and session_user tests for the two fixed-kind leave wrappers,
  proof-only fencing, cross-role denial, direct DML/helper denial, and sanitized
  22023/42501/55000/AM002/AM001 errors.
- Leave exact historical prepare binding with current controller generation
  equal and greater after unrelated actions; wrong target/kind/operation/
  generation; no prepare; intent; live claim; visible owned closing/unresolved;
  receipt replay after later actions; and conflicting same-replica receipt.
- Proof for joining/active/draining/terminating and audit-only left; exact
  intent request; no-intent proof with stored request identity; changed
  no-intent request; cross-replica request reuse; fresh and replayed historical
  reclaimed count; immutable field mutation denial; evidence conflict; same
  proof replay; ambiguous commit then same-evidence retry; no transition
  predicate while its parent lock is held; and separate parent-first
  fenced-transition recovery only.
- Capacity generation advances once for each fresh leave/proof, including left
  audit proof; controller generation, desired, capacity policy, and partitions
  remain unchanged; replay/rejection/rollback change neither generation.
- Exact-replica multi-policy claim cleanup, parent-count result, canonical
  locks, all scopes decremented once, released/fenced immutable receipt shape,
  retained summaries/ordinals, no other replica release, count corruption fail
  closed, and later ordinary 24-hour GC only.
- Transport call order enter/function/finish/commit, one function evaluation,
  owned result lifetime, zero authority on every error, and no internal retry.
