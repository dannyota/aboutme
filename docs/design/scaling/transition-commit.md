# Public transition commit fence

Status: Accepted under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). This is
the target contract. Local implementation and hosted proof remain Phase 10
gates.

[Durable public transitions](public-transitions.md) defines targets, deadlines,
acks, results, and recovery reads. This contract defines the business execution
fence, transaction capability, rollback, and lock order. It composes with
[transaction entry](transaction-entry.md),
[replica coordination](runtime-coordination.md), and the current
`publicstate.Plan` and `CommittedState` API.

## Commit entry

```sql
runtime_lock_public_transition_for_commit(
  transition_id uuid,
  initiator_replica_id uuid,
  initiator_instance_id text,
  initiator_release_digest text,
  target_digest bytea
) RETURNS TABLE(
  deadline_at timestamptz,
  target_count integer,
  ack_count integer
)
```

R3 invokes this as the first callback query after `WriteTxRunner` entry. It
locks the parent and requires:

- closing state and exact initiator tuple and digest;
- database time no later than the durable deadline;
- no initiator termination intent or fencing proof;
- one exact ack for every required replica;
- current `public_state.discovery_generation` and each existing target resume
  revision equal every expected generation.

A missing resume is mismatch, including one planned for retirement. Generation
reads are nonlocking, so this first query does not invert the existing inner
lock order. The later mutation compare-and-set remains final authority. Any
failed predicate returns SQLSTATE `55000`, and no business query runs.

## Transaction capability

Successful commit entry creates or validates owner-owned
`pg_temp.public_transition_commit_gate_v1`. It then acquires:

```sql
pg_try_advisory_xact_lock(940647822, pg_backend_pid())
```

Namespace `940647822` is big-endian SHA-256 prefix `3811258e` of
`aboutme.public-transition-commit-gate.v1`. An already-held guard or malformed,
nonempty marker is `AM001`.

The marker contains backend PID, xid8, session-role OID, transition UUID, target
digest, and `finished=false`. The app role has no marker privilege. A fixed
owner-created deferred constraint trigger rejects transaction commit unless
`finished=true`. Forcing the trigger early only checks; it cannot finish or
remove the marker. The advisory guard prevents `DISCARD TEMP` followed by a
second gate. Savepoint rollback restores marker and guard together.

The row lock alone grants no commit authority. Callers cannot supply a boolean
or construct the capability.

The marker's pending deferred INSERT assertion is sufficient through terminal
completion. PostgreSQL refuses `DISCARD TEMP` while that trigger event remains
pending. `SET CONSTRAINTS ALL IMMEDIATE` before terminal completion invokes the
assertion and fails; rolling that statement back to a savepoint restores the
pending event. The caller cannot remove the unfinished marker and its assertion.

Calling the general `runtime_finish_write()` first may finish the separate
runtime marker, but it does not finish this transition marker. The unfinished
transition assertion still blocks commit and `DISCARD TEMP`. Transition
terminalization after general finish also fails because its tracked DML requires
an unfinished runtime write entry. The only safe disposition is rollback.

After the terminal function marks this capability finished, its deferred check
may pass. If a caller then forces constraints and discards temporary objects
before general finish, `runtime_finish_write()` fails because its own marker is
missing. In the required order, transition terminalization precedes general
finish; after both checks have completed, discarding temporary objects cannot
undo either completed state change. The two advisory guards prevent either entry
from being recreated in that transaction.

## Atomic business commit

```sql
runtime_commit_public_transition(
  transition_id uuid,
  target_digest bytea,
  result_kinds text[],
  result_generations bigint[]
) RETURNS TABLE(terminal_at timestamptz, replayed boolean)
```

Arrays map exactly to target ordinals. R3 invokes the function after all
existing business, idempotency, media-reference, deletion-job, and generation
work, but before `WriteTxRunner` finish. It requires the unfinished marker,
advisory guard, and parent lock from commit entry. Missing, mismatched, or
finished capability is `AM001`.

The function does not repeat the deadline test after authorized business work
has begun. It rechecks closing state and digest. It proves a discovery result
equals `public_state`, a generated resume exists at the result revision, and a
retired resume is absent. It writes every target result and changes the parent
to committed atomically, then marks the capability finished. No new mutation
transaction replays a committed transition.

The store exposes `WithTransitionCommitFence`. Its callback receives only
transaction-bound `*store.Queries`. The method runs commit entry first, invokes
the callback, requires one complete `CommittedState`, records the terminal
results, then returns to `WriteTxRunner`. It exposes no raw transaction, commit,
rollback, connection, capability, or post-finish query.

## Rollback and unresolved state

```sql
runtime_rollback_public_transition(
  transition_id uuid,
  initiator_replica_id uuid,
  initiator_instance_id text,
  initiator_release_digest text,
  target_digest bytea,
  error_code text
) RETURNS TABLE(state text, terminal_at timestamptz, replayed boolean)
```

The fixed error values are `canceled`, `deadline`, `drain_failed`,
`ack_missing`, and `business_not_started`. Error strings are not stored. The
function locks the parent and changes closing to rolled_back. Exact rollback
replay is idempotent. Committed returns its state without mutation. Unresolved
or mismatched identity or digest is `AM001`.

```sql
runtime_recover_rollback_public_transition(
  transition_id uuid,
  initiator_replica_id uuid,
  initiator_instance_id text,
  initiator_release_digest text,
  target_digest bytea
) RETURNS TABLE(state text, terminal_at timestamptz)
```

Recovery uses an independent `WriteTxRunner` transaction. The
[recovery evidence contract](transition-recovery.md) fixes parent-first locking,
identity validation in every state and a nonlocking expected-generation check
only for closing. It records `business_not_started` only when that check passes.
The code means no business change from the transition committed; it does not
claim no SQL ran. Committed and rolled-back results remain immutable.

R1/R3 install no unresolved writer. The atomic parent, target results and
business commit are the outcome authority. Response receipts affect exact HTTP
replay only. Unsafe closing recovery leaves closing unchanged and readiness
unavailable. R2 uses the separate
[reconciliation snapshot](transition-reconciliation.md) before changing a local
fence, preserving later generations, proved retirement and current blockers.

## Fenced-initiator recovery

```sql
runtime_recover_fenced_public_transition(
  transition_id uuid,
  target_digest bytea,
  initiator_replica_id uuid,
  fencing_evidence_id text
) RETURNS TABLE(
  state text,
  terminal_at timestamptz,
  replayed boolean
)
```

Only `aboutme_lifecycle_command` receives EXECUTE. App, maintenance,
fencing-proof, restore, and PUBLIC are denied. Assertion and validation helpers
remain owner-only.

The lifecycle controller invokes this after the fencing-proof transaction
commits. It starts a separate ordinary `WriteTxRunner` transaction with the
shared runtime barrier first. It must hold no `public_state`, capacity, replica,
controller-operation-ledger, or AWS-side lock that participates in a database
lock order. The function locks the transition parent first.

It requires exact transition UUID and digest, exact immutable initiator replica,
and a recorded `ec2_terminated_v1` proof identified by `fencing_evidence_id`.
The proof's replica, instance, and release tuple must match both the
transition's initiator snapshot and `runtime_replicas`; that replica must be
`fenced`. Proof and fenced membership are immutable, so the function reads them
after taking the parent without a row lock. Timeout, database loss, ALB health,
ECS state, advisory-lock loss, or a proof for another host is never sufficient.

If the parent is closing, the function changes it to rolled_back with fixed
terminal error `initiator_fenced`, records the immutable
`recovery_fencing_evidence_id`, and stores no target result. It does not create
an ack or execute business SQL. The foreign key and terminal-state checks make
the proof reference immutable and require it on exact replay.

Committed and rolled_back states remain immutable and return their durable
state. A committed or independently rolled-back parent returns `replayed=false`
without writing a proof reference. Exact replay of `initiator_fenced` returns
`replayed=true` only when transition, digest, initiator, proof reference, and
terminal reason match. A bad transition, digest, initiator, or proof is `AM001`
in every state. Unresolved remains failed closed and cannot be repaired by this
function.

Taking the parent lock waits behind an open business transaction. If business
commits, recovery returns committed without mutation. If business rolls back and
leaves closing, fenced recovery may roll it back. A delayed original initiator
then observes terminal rollback and cannot pass commit entry.

This function operates only while the ordinary runtime write gate is open. It
has no gate bypass. Final stop already requires every transition terminal. A
corrupt shutdown or wake remains unavailable instead of reopening the gate for
recovery.

## Lock graph

Every mutator runs inside `WriteTxRunner`: shared runtime barrier first,
`runtime_finish_write()` and commit last. Transition functions never call entry
or finish.

- Begin: `public_state`; nonlocking overlap and membership/capacity predicates;
  insert targets in ordinal order and replicas in UUID order. Begin never locks
  or waits on an existing transition parent.
- Ack: transition parent; required detail; exact `runtime_replicas` row; ack. It
  takes no public-state, resume, account, or slug lock.
- Business: transition parent first, then the unchanged inner mutation order.
- Rollback/recovery: transition parent, then its target and required rows in
  fixed order. It takes no business lock.
- Fenced recovery: transition parent, then nonlocking immutable proof and fenced
  membership validation. The caller holds no membership or controller ledger
  database lock when it enters.

All begins serialize on `public_state`. A visible closing or unresolved overlap
rejects immediately without `FOR UPDATE`. If business has an uncommitted
terminal update, READ COMMITTED exposes its prior closing version, so begin
rejects instead of waiting. This removes the `public_state -> old transition`
edge while business may hold `transition -> public_state`.

Membership register, activate, drain, leave, and fence use their accepted
`public_state -> capacity -> replica` order. They never lock or wait on a
transition parent. Where membership operations require transition predicates,
they use nonlocking visible closing/unresolved reads and fail closed. Fencing
proof neither reads nor predicates on transitions. An in-flight ack may wait for
its replica row because the membership writer never waits on the ack's parent;
it then rechecks state. Proof never changes or acks a transition.

After the parent, resume publication keeps ordered slug advisory locks,
`public_state`, resume, session or token, then mutation/media/idempotency work.
A resume-only edit omits `public_state` as it does now. Account deletion keeps:

1. Ordered slug advisory locks.
2. `public_state`.
3. Canonical-email advisory lock.
4. Registration.
5. User.
6. Resume rows in UUID order.
7. Current session.
8. Established mutation, media, and audit work.

No path locks a business or membership row and then waits on a transition row.

## Grants

`aboutme_runtime_owner` owns marker objects, functions, and transition tables.
Every function sets `search_path = pg_catalog`, qualifies objects, and uses no
dynamic SQL. Revoke PUBLIC and direct DML. The app role gets only the named
EXECUTE grants. Fencing, maintenance, and restore get none. The sole lifecycle
exception is EXECUTE on `runtime_recover_fenced_public_transition` for
`aboutme_lifecycle_command`; it grants no other transition mutation. Owner-only
triggers enforce framing, immutable results, and finished capability.

R8's private app adapter supplies replica identity to every app mutator. Shared
app credentials do not authenticate an incarnation. Every function matches the
tuple to stored immutable membership. No HTTP request or general store caller
sets identity, timestamps, terminal results, or notification payloads.

## Acceptance

- Missing ack, expired deadline, stale generation, or missing target fails
  before the first business query.
- Forced constraints, `DISCARD TEMP`, repeated gate/finish, savepoint rollback,
  foreign guard holder, and marker corruption cannot manufacture authority.
- Calling `runtime_finish_write` before the terminal operation leaves the
  transition assertion pending. Commit and `DISCARD TEMP` fail, and later
  terminal DML fails against the finished runtime marker.
- Forced transition constraints followed by `DISCARD TEMP` cannot erase an
  unfinished marker. Savepoint recovery retains the assertion.
- Omitting the terminal operation cannot commit business DML.
- Terminal completion and forced constraints followed by `DISCARD TEMP` before
  general finish makes general finish fail on its missing marker.
- Terminal completion, general finish, `DISCARD TEMP`, and commit preserves the
  already completed durable state and cannot re-enter either capability.
- A paused initiator loses to rollback and cannot pass commit entry. Recovery
  waits or returns unavailable while an open business transaction holds parent.
- Committed parent rejects missing, extra, mismatched, or mutable results.
- Publish, unpublish, private/live delete, and multi-resume account deletion
  produce complete results atomically with business and idempotency evidence.
- Ambiguous commit resolves committed, rolled_back and safe closing-to-rollback
  through independent connections. Unsafe closing remains unchanged; no
  unresolved writer or retained-response heuristic can replace outcome evidence.
- Fenced recovery waits behind a parent-locked business transaction and returns
  its committed result or rolls back only a still-closing parent.
- Fenced recovery succeeds with zero surviving serving replicas after exact EC2
  proof, then permits the existing replacement sequence. A delayed initiator
  cannot write after terminal rollback.
- Nil, forged, mismatched, other-host, or ECS-only evidence fails. Cross-role
  calls fail, and committed or unresolved targets/results never change.
- Begin holding `public_state` rejects an older visible overlap without waiting
  while business holds its parent and requests `public_state`.
- Ack waiting on a replica writer completes because membership never waits on
  its parent, then rejects leave/fence on recheck.
- Two-pool account deletion races with same-email registration and overlapping
  slug/public-state mutation preserve the stated order and do not deadlock.
- Real-role tests reject direct DML, forged ack/result/unresolved state,
  cross-role execution, and writes outside `WriteTxRunner`.
