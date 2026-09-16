# Public transition operation contract

Status: Accepted detail under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). The
[scaling index](README.md) records whether it is built.

The [fixed SQL catalog](transition-functions.md) and
[store transport](transition-transport.md) complete the private contract for
[public transitions](public-transitions.md),
[atomic commit](transition-commit.md), [recovery](transition-recovery.md) and
[reconciliation](transition-reconciliation.md). They add no public API,
unresolved writer, authority flag, response receipt or retry policy.

## Entry, locks and transaction capability

Begin, ack, rollback, recover-rollback, fenced recovery, and the complete
business commit fence use ordinary WriteTxRunner. Existing runtime_enter_write
runs before the fixed function and therefore before its first row lock,
including replay/readback branches. Function bodies assert the existing entry;
they never enter or finish it. runtime_finish_write and commit remain last.

Begin locks public_state and uses nonlocking overlap/membership/capacity checks.
It never locks an existing transition parent. It inserts targets in ordinal
order and required replicas in UUID order. Ack locks parent, required replica
detail, exact runtime replica, then ack. It rechecks closing, deadline,
active/draining state, identity, and absence of intent/proof after every wait.

Business commit uses one transaction for all of these steps:

1. WriteTxRunner enters the ordinary barrier.
2. runtime_lock_public_transition_for_commit is the callback's first query. It
   locks the parent first and performs exact
   initiator/digest/deadline/ack/current generation checks before any business
   row lock or DML.
3. That function creates/validates owner-owned
   pg_temp.public_transition_commit_gate_v1 and acquires the fixed advisory
   transaction guard (940647822, backend PID). The row records backend PID,
   xid8, session-role OID, transition UUID, digest, finished=false.
4. Only then does the caller callback run unchanged business SQL through the
   same transaction-bound Queries and existing business lock order.
5. The callback returns one complete typed target result plan. The store maps it
   to ordinal arrays; it cannot provide results for unplanned targets, omit a
   target, change identity, or choose stored timestamps.
6. runtime_commit_public_transition verifies marker, guard, parent lock, stored
   targets, and current committed business facts; writes all target results and
   parent terminal state atomically; then marks the gate finished.
7. Control returns directly to WriteTxRunner, which calls runtime_finish_write
   and Commit. No caller query/hook runs between terminalization and finish.

The transition gate's owner-only deferred trigger rejects commit while
unfinished. Owner helper `runtime_assert_public_transition_commit_gate()`
validates the marker/guard/finished state. No login executes it. Forced
constraints, DISCARD TEMP, repeated entry, wrong transaction/session, missing
parent lock, or capability mismatch is AM001. The row lock alone is never
authority. Savepoint rollback restores marker/guard state.

WithTransitionCommitFence must not be built as two committed store calls. It
exposes no pgx.Tx, connection, driver, raw marker, commit/rollback, capability,
finish method, or post-terminal callback. Callback error/panic rolls back the
parent and every business change. Omitting terminalization cannot commit.

## Rollback and recovery

Ordinary rollback is parent-first, accepts only canceled, deadline,
drain_failed, ack_missing, or business_not_started, and stores no arbitrary
error. It performs no business DML. Exact replay returns immutable terminal
state; committed readback never rewrites results. It validates p_error_code as
an allowed input, but committed readback has no stored rollback reason to
compare and returns committed/replayed=true after identity and digest checks. A
rolled_back readback requires the exact stored reason and returns replayed=true;
a different stored rollback reason is AM001. Fresh rollback alone returns false.

Recover-rollback runs in a fresh ordinary writer. It locks parent first, checks
identity/digest in every state, and for closing validates expected generations
with one nonlocking query. Missing/changed business rows are AM001 and leave
closing unchanged. It never creates unresolved or infers retirement. If a lost
business commit succeeded, recovery waits then reads committed. If it rolled
back, recovery may record business_not_started in its own atomic transaction.

Fenced recovery runs only after membership proof commits and in a separate
ordinary writer. Its caller holds no public_state, capacity, replica, lifecycle
ledger, or AWS lock. It locks transition parent first, then reads immutable
proof/fenced membership without locking. Exact closing becomes rolled_back with
initiator_fenced and proof reference. It creates no ack/result/business write.
Fresh initiator_fenced returns replayed=false; exact same initiator-fenced
replay returns true. Committed or independently rolled_back readback remains
unchanged and returns replayed=false. Unresolved fails closed. Proof role cannot
call recovery.

runtime_recover_public_transition is an ordinary read, outside WriteTxRunner. It
reconstructs immutable historical parent/target/required/ack result only. It
does not lock, mutate, reconcile current state, or grant local-open authority.

Reconciliation is separate. runtime_reconcile_public_transition is one STABLE
SECURITY DEFINER function invoked by one top-level SELECT outside WriteTxRunner.
It uses one established MVCC statement snapshot and performs no write, lock,
advisory operation, temporary DDL, volatile helper, or callback. It returns at
most four rows. The local apply mutex belongs to R2 and is held across this one
read and one local application; no PostgreSQL lock is held while waiting for it.

## Roles and errors

aboutme_app receives EXECUTE on begin, list-required, ack, recovery read,
commit-entry, commit-terminal, rollback, recover-rollback, and reconciliation.
aboutme_lifecycle_command receives only fenced recovery. Maintenance,
fencing-proof, restore, migrator, and PUBLIC receive none of these transition
operations. App cannot call fenced recovery. Lifecycle cannot call app mutation
or reads. No login gets transition-table DML, marker access, helper execution,
or result fabrication access.

Every definer is runtime_owner-owned, sets search_path=pg_catalog, qualifies all
persistent objects, uses static SQL, checks exact direct session_user before
mutable reads, and emits fixed value-free errors.

- 22023: malformed UUID/digest/text/count/array/ordinal/result input.
- 42501: wrong direct login/function role.
- 55000: valid but unavailable begin/ack/commit-entry state, deadline, current
  generation, membership, overlap, or ordinary closed write gate.
- AM001: every public-transition immutable
  replay/identity/digest/metric/terminal mismatch; missing or contradictory
  stored detail; malformed marker/guard; corrupt target/result/required/ack
  shape; unsafe recover-rollback evidence; unresolved input. Existing transition
  rules deliberately retain AM001 rather than the membership/lifecycle/claim
  AM002 contract.

AM001 triggers physical connection retirement for a writer. A pure read returns
no snapshot and its connection is not reused when corruption/row cleanup is
uncertain. Any callback, decode, iteration, finish, cleanup, or commit error
returns zero result/authority. Commit ambiguity destroys the physical connection
and is resolved only by fresh recovery reads/writers under accepted rules.

## Required proof

- Begin target vector/order/digest, array lower bounds/nulls, deadline
  projection and backward/forward clock, stale/missing generation, overlap,
  active/draining snapshot, terminating rejection, exact/conflicting ID replay,
  notify on commit.
- List missing/terminal/mismatch explicit error, nonempty UUID order, ack flags.
- Ack tuple/digest/metrics replay, late ack, drain/fence/leave waits and
  recheck, local joined prerequisite at adapter, no proof-as-ack.
- Commit entry must be first callback query; missing ack/deadline/current
  mismatch runs zero business SQL. Marker/guard collision, forced constraints,
  DISCARD TEMP, savepoint, double entry, general finish first, omitted
  terminalization, callback panic/error, terminal result order/shape, business
  fact verification.
- Atomic business/result/parent commit and rollback together under injected wire
  loss. Fresh recovery distinguishes committed from closing without response
  evidence or current-row commit inference.
- One target with two required replicas and four targets with one required
  replica both pass commit entry with complete acks. One target with one of two
  required acks fails before business SQL. No decoder applies a target-derived
  cap to required-replica arrays.
- Ordinary rollback/replay/committed readback; recover-rollback waits on
  business, unchanged generation proof, changed/missing rows remain closing, two
  callers, ambiguous recovery commit.
- Fenced recovery exact proof/tuple/reference, parent-first wait, committed
  race, no ack/result, cross-role denial, and no proof/capacity/membership lock
  held.
- Recover/reconcile nullable matrices, array null elements, malformed/reordered/
  partial rows, delayed/lost notifications, one STABLE snapshot, later
  generation monotonicity, absorbing retirement, blocker preservation,
  connection cleanup.
- Real-role grants/direct-DML/helper/marker denials; ordinary entry before first
  lock on every mutator/replay; zero authority on every error; AM001 retirement;
  unchanged public HTTP/OpenAPI/SSE frames and AC-INF-009/AC-RT-001/002 rows.

## Implementation order

1. Begin/list/ack/recovery-read SQL plus app scalar transport. Depends on schema
   00016, registration/membership transport, and ordinary WriteTxRunner.
2. Commit gate/marker helpers plus WithTransitionCommitFence and terminal
   commit. Depends on group 1; owns adversarial marker and isolated fully fenced
   atomic business tests. It must remain one coherent migration/store slice.
3. Ordinary rollback and recover-rollback. Depends on group 1 and complete
   isolated generation-fence fixtures.
4. Fenced recovery. Depends on membership-evidence proof fields/functions and
   uses a lifecycle-role pool only.
5. STABLE reconciliation read and decoder. Depends only on schema 00016 and can
   be reviewed separately because it performs no write or lock.

Root serializes the eventual migration(s), query source, generated store files,
shared constructors, and test harness. Groups may use separate later migrations
in dependency order; no number is reserved here. The commit gate and its
terminal function must never be split across deployments that expose one without
the other.

R1 fixed operations/store precede R1a, then R2-R7 caller migration, then R8
coverage. R1 may install uncomposed functions and prove them with isolated fully
fenced fixtures. Complete production writer coverage is required before caller
composition or multi-replica authority, not before authoring R1 functions. The
later structural writer inventory proves every generation/deletion/publication
writer uses overlap and parent fencing; R1 tests do not claim that production
coverage early.
