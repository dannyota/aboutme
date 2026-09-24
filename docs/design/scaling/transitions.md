# Durable public transitions

Status: Accepted under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). Not built.
The [scaling index](README.md) holds the shared rules.

A publication mutation must close every replica's local fence for its targets
before business SQL runs. This page extends `publicstate.Plan` and
`CommittedState` with durable fleet state and keeps the
[ADR 0022](../../adr/0022-public-artifact-revocation.md) live-state gate,
idempotency and lease behavior.

## Categories

| Operation            | Callers                                |
| -------------------- | -------------------------------------- |
| `resume_update`      | Edit and photo mutations               |
| `resume_publication` | Publish, unpublish, slug and discovery |
| `resume_retire`      | Resume deletion                        |
| `account_retire`     | Account deletion                       |

Each target's class comes from the existing `publicstate` plan. NonDraining
seals new admission and lets old leases finish. Revoking cancels and joins
applicable local leases before acknowledging. Discovery is always Revoking.
Creating a resume starts no transition. The route and idempotency operation
digest do not change.

## Tables

- `public_transitions`: UUID, initiator tuple (foreign key to immutable
  membership), operation, state `closing`, `committed`, `rolled_back` or
  `unresolved`, `created_at`, `deadline_at = created_at + 5 seconds`,
  `terminal_at` (null exactly while closing), 32-byte target digest, terminal
  error code, and `recovery_fencing_evidence_id` (set only for
  `initiator_fenced`).
- `public_transition_targets`: 1 to 4 per parent, contiguous ordinals from 0.
  Discovery, when present, is ordinal 0; resumes follow in UUID byte order. Each
  has a positive expected generation and class. A committed target has exactly
  one result: `generation` with a positive value, or `retired`. Discovery allows
  only `generation`. Other states have no result.
- `public_transition_replicas`: every `active`, `draining` or unfenced
  `terminating` incarnation at begin. The set is never empty.
- `public_transition_acks`: one per required replica, local result `closed`,
  plus revoking and non-draining counts for metrics.

Owner triggers reject an incomplete commit, any result change after a terminal
state, ack updates, deletes and `TRUNCATE`. A deferred assertion recomputes the
target digest. Committed requires every ack. Terminal states are final.

## Target digest

Go and PostgreSQL hash this encoding with SHA-256:

1. ASCII `aboutme.public-transition-targets.v1`, then one zero byte.
2. Target count, unsigned 32-bit big-endian.
3. Per target: ordinal (u32 BE); kind byte `0` discovery or `1` resume; 16 UUID
   bytes, all zero for discovery; expected generation (i64 BE); class byte `0`
   NonDraining or `1` Revoking.

Test vector: discovery generation 7 Revoking, then resume
`00112233-4455-6677-8899-aabbccddeeff` generation 42 NonDraining, encodes as

```text
61626f75746d652e7075626c69632d7472616e736974696f6e2d746172676574732e76310000000002000000000000000000000000000000000000000000000000000000000701000000010100112233445566778899aabbccddeeff000000000000002a00
```

and hashes to
`34cd4cb37c1cf4467283a97904ce4170484ad2c335c277cb465403b9752ac68c`.

## Begin

The coordinator sets one five-second monotonic deadline before any work. Pool
wait, lock wait, local drain and ack waiting all spend that budget. Immediately
before SQL it passes the remaining whole milliseconds, rounded up and capped at
5,000. The function sets `deadline_at = statement_timestamp() + remaining` and
`created_at = deadline_at - 5 seconds`. A clock sample after its locks must fall
before the deadline, or begin fails with `55000` and inserts nothing.

Under the `public_state` lock, begin requires every target at its expected
generation, an active initiator, no unfenced `terminating` replica, and no
visible closing or unresolved transition that shares a target. It reads that
overlap without locking, so it never waits on an older transition. It snapshots
the required replicas and notifies `aboutme_public_transition` with `1:<uuid>`.
Exact replay by transition UUID returns the original row.

## Acknowledge

Each replica verifies the digest and closes its targets under its local
publicstate mutex. After the local close and join, it inserts the exact ack. The
ack requires closing state before the deadline and `active` or `draining`
membership with no intent or proof. A `draining` replica may ack only a
transition that already requires it. Local admission stays closed until the
replica observes a terminal state. A restarted agent rebuilds closed fences from
closing rows before readiness.

The initiator polls acks every 250 milliseconds until the deadline. A missing
ack, cancellation or drain timeout rolls the transition back. No business SQL
runs.

## Commit fence

The business transaction's first query locks the parent and requires closing
state, the exact initiator tuple and digest, database time no later than the
deadline, no intent or proof for the initiator, every ack, and every target
still at its expected generation. A resume that no longer exists does not match.
Any failure is `55000` before business SQL.

That row lock and state predicate are the execution fence. A paused initiator
whose transition was rolled back cannot pass it. A recovery writer waits behind
an open business transaction.

Passing the fence creates a per-transaction commit capability. An owner-owned
temporary marker, a deferred constraint trigger and a per-backend advisory guard
together block commit until the terminal function marks the capability finished.
The guard matters because `SET CONSTRAINTS` and `DISCARD TEMP` could otherwise
remove a marker. Callers cannot create or finish the capability.

After the existing mutation, media, deletion-job, generation and idempotency
work, `runtime_commit_public_transition` checks each result against current
rows, writes every target result and sets `committed` in the same transaction:

- publish or edit: the new resume generation;
- rename or unpublish: the new generation and the discovery generation;
- resume deletion: `retired`, and discovery when planned;
- account deletion: `retired` for every deleted resume, and discovery.

Terminal functions notify `aboutme_runtime_terminal` with `1:<uuid>:<state>`.
Notification payloads carry no authority.

## Outcomes and recovery

Once a caller holds the parent lock, exactly one of these holds:

1. `committed`: business rows and results committed together.
2. `rolled_back`: no business change from this transition committed.
3. `closing`: nothing committed yet; an open business transaction would hold the
   lock.
4. `unresolved`: failed closed. No ordinary path creates or repairs it.

A lost `COMMIT` response creates no other outcome. Definite failure rolls back
with a fixed code: `canceled`, `deadline`, `drain_failed`, `ack_missing` or
`business_not_started`.

Ordinary recovery runs in a fresh transaction and locks the parent first. It
returns committed or rolled-back state unchanged. For closing, it reads current
target generations without locking. If every target is unchanged, it rolls back
with `business_not_started`. Otherwise it returns `AM001` and leaves the row
closing and readiness false. The parent lock excludes every writer, so the
unlocked read is safe.

Current application rows are never commit evidence. Idempotency records expire
after 24 hours, generations advance again, and audit rows, media jobs and slug
tombstones are retained for bounded periods or cascade away. Idempotency records
still replay the exact HTTP response when present.

### Fenced-initiator recovery

After the proof transaction commits, `aboutme_lifecycle_command` may call
`runtime_recover_fenced_public_transition` in a separate transaction. It holds
no membership or controller lock when it starts. It locks the parent, then
requires the exact digest, the initiator tuple, a matching `ec2_terminated_v1`
proof and a `fenced` initiator. A closing parent becomes `rolled_back` with
`initiator_fenced` and the evidence ID. Committed and rolled-back parents are
returned unchanged. This lets a replacement start when no serving replica
survives.

## Reconciliation

A replica never opens a fence from notification order. The coordinator holds its
local apply mutex, calls `runtime_reconcile_public_transition` once, and applies
the result before it releases the mutex.

The function is one `STABLE` statement, so it reads a single snapshot. It
returns one row per target with the historical result, whether the target exists
now and at which generation, `proven_retired`, and any closing or unresolved
transition that shares the target. `proven_retired` is true only when a
committed result for that exact resume UUID is `retired`. Current absence never
proves retirement.

Local state follows this precedence:

1. `retired` is final for the process lifetime.
2. A proven retirement with a present row is corruption; the target stays
   closed.
3. A proven retirement with an absent row retires the target.
4. A blocker, or absent or inconsistent evidence, keeps the target closed.
5. Otherwise the target opens at the higher of its local generation and the
   current generation, and only when that is at least the historical result or
   expectation.

A restarted process starts every fence closed and rebuilds it this way.

## Lock graph

- Begin: `public_state`, then inserts; never an existing transition parent.
- Ack: parent, required rows, then the exact replica row.
- Business: parent, then the existing inner order.
- Recovery: parent, then its targets; no business lock.

After the parent, publication keeps slug advisory locks, `public_state`, resume,
then session or token. Account deletion keeps slug advisory locks,
`public_state`, the canonical-email advisory lock, registration, user, resumes
in UUID order, and the current session. No path locks a business or membership
row and then waits on a transition row.

## Required proof

- Digest vector, ordering, shape, and terminal constraints.
- Overlapping begins, begin racing activation, and stale or missing generations.
- Forged, late and replayed acks, and a `draining` replica.
- The five-second budget across every wait.
- Delayed, duplicate, reordered and lost notifications repaired by reads.
- Forced constraints, `DISCARD TEMP` and savepoints cannot commit business rows
  without terminal results.
- A paused initiator loses to rollback. Recovery waits on an open business
  transaction.
- Fenced recovery with zero survivors, and a delayed initiator after it.
- Later generations never regress. Retirement survives restart. Absence alone
  never retires.
- Real-role denial of direct DML and cross-role calls.
- Two-pool account deletion racing same-email registration and slug changes
  without deadlock.
