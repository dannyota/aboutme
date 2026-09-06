# Shared concurrency claims

Status: Accepted detail of [fleet admission](admission.md) under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md).
[R1](../../plans/phase-10/replica/runtime-tasks.md) owns schema/store work. R5
owns the claim adapter; R4, R6 and R7 own callers. Local proof is pending.

## Policy catalog

One claim parent owns one scope for C01-C04. C05 owns IP and, for an owner
stream, account scopes atomically. The fixed catalog has sole primary key
(policy_id, scope_kind), with these immutable values:

| Policy               | Kind    | Running | Waiting | Queue | Deadline   |
| -------------------- | ------- | ------: | ------: | ----- | ---------- |
| render.global_claim  | global  |       1 |       8 | yes   | 20 seconds |
| password.hash        | global  |       2 |      16 | yes   | none       |
| mail.send            | global  |       2 |       0 | no    | none       |
| mcp.user_concurrent  | user    |       4 |       0 | no    | none       |
| sse.fleet_account_ip | ip      |     100 |       0 | no    | none       |
| sse.fleet_account_ip | account |      20 |       0 | no    | none       |

`shared_claim_policies` records running_limit, waiting_limit, queue_enabled and
deadline_mode (`none` or `render_20s`). Checks reject other combinations.
Runtime code compares the rows with fixed constants and fails on drift. Runtime
policy updates/deletes are forbidden.

The [identity contract](claim-identities.md) fixes 32-byte scope/request
digests, replica binding and the operation-local ambiguity window. Database rows
never contain render snapshots, capabilities, controllers, artifacts, callbacks,
SSE subscriptions or local queue authority. Photo admission and the 2,000-stream
task limit remain local.

## Scope summaries

`shared_claim_scope_summaries` has primary key (policy_id, scope_kind,
scope_digest), a catalog foreign key, running_count, waiting_count, next_ordinal
and updated_at. Digest length is 32; counts are nonnegative; next_ordinal starts
at one and never wraps.

Local CHECK constraints cover local fields only. Locked mutators enforce catalog
limits. A deferred owner assertion verifies fixed catalog values, running and
waiting bounds, and exact counts of live child rows. A CHECK cannot query the
catalog table. The assertion is a trigger, never an ordinary function call.

Keep an empty summary while any retained child references it. Only receipt
cleanup may remove it after all children are gone and both counts are zero. Its
next lifetime may start ordinal one; no old capacity or queue position survives
that deletion.

## Claim parents and scopes

`shared_claim_requests` has:

- non-nil claim_id UUID primary key, fixed policy_id and replica_id foreign key;
- unique (claim_id, policy_id) for child binding;
- state `waiting`, `running` or `released`;
- non-nil work_id only for C01; null for all other policies;
- database admitted_at, deadline_at exactly admitted_at+20 seconds for C01 and
  null otherwise, and released_at/release_reason only when released;
- release_reason `joined`, `canceled`, `expired` or `fenced`;
- scope_count one or two and 32-byte request_digest.

Identity is immutable. Allowed state edges are waiting to running, waiting to
released, and running to released. Released is terminal.

`shared_claim_scopes` repeats policy_id and has a composite foreign key to its
parent, a foreign key to its scope summary, a 32-byte scope_digest and mirrored
parent state. It has two distinct ordinals:

- request_ordinal is 1 or 2. Primary key (claim_id, request_ordinal) and unique
  (claim_id, scope_kind) bind parent shape. C05 uses IP first and optional
  account second; other policies use their sole catalog kind first.
- allocation_ordinal is positive and copied from summary.next_ordinal. Unique
  (policy_id, scope_kind, scope_digest, allocation_ordinal) orders coexisting
  claims within one scope. It never uses the request ordinal.

A deferred owner assertion checks exact child count, policy, states, digest and
C05 shape. The parent foreign key restricts deletion until children are removed.
No parent can commit a partial IP/account reservation.

## Functions and privileges

All tables, indexes, triggers and definer functions belong to
aboutme_runtime_owner. Revoke PUBLIC and direct runtime-role DML. Definer
functions set search_path=pg_catalog, qualify names and accept no dynamic SQL.
App receives only these narrow operations:

- `runtime_acquire_single_claim(claim_id, policy_id, replica_id, work_id, scope_kind, scope_digest)`
  accepts only C01-C04 and exact kind/work shape.
- `runtime_acquire_sse_claim(claim_id, replica_id, ip_digest, account_digest)`
  uses null account_digest for public and two scopes otherwise.
- `runtime_promote_claim(claim_id, expected_replica_id, request_digest)` accepts
  queued C01/C02 work and returns the resulting claim state.
- `runtime_release_claim(claim_id, expected_replica_id, request_digest, reason)`
  accepts joined/canceled/expired after local work is joined or never started.
- `runtime_resolve_claim(claim_id, expected_replica_id, request_digest)` checks
  the replica and digest before reading the exact parent and ordered scopes. The
  adapter checks every expected field before work.

IDs are UUIDs, digests are bytea and enums are checked text values. There is no
general array/JSON scope input. Acquire/promote/release return typed claim
results; missing, partial or contradictory resolution grants no work.

The exact EC2 proof function calls an owner-only helper to release that
replica's live claims and decrement summaries in the same proof transaction. App
cannot choose fenced release. App, lifecycle and proof logins cannot call the
helper directly. Graceful leave requires zero live claims after local join; it
never reclaims them itself.

Only maintenance may execute `runtime_gc_released_claim_receipts()`, with no
arguments and fixed 24-hour/256-parent bounds. It receives no direct table DML.
Other logins receive no receipt-cleanup or summary-repair privilege.

Every mutation uses WriteTxRunner. It owns entry before row locks and finish
immediately before commit. Callbacks use generated Queries and never call entry,
finish, Commit or Rollback. Table assertions remain trigger-only.

## Acquisition and lock order

After the outer runtime write barrier:

1. Lock runtime_capacity and require online admission.
2. Lock the factory's exact replica row and require active. Other states admit
   nothing.
3. Insert/lock the claim parent. On UUID conflict, compare all immutable request
   fields and children. Exact replay returns existing state; conflicting reuse
   fails.
4. Lock catalog rows in (policy_id, scope_kind) order.
5. Create missing summaries and lock them in bytewise (policy_id, scope_kind,
   scope_digest) order.
6. Require capacity in every scope. Queued policies choose running if available,
   then waiting if allowed. Denial leaves no parent/child or partial charge.
7. Allocate summary ordinals, insert all children and update counts atomically.

Sample database time after locks. C01's original 20-second deadline begins at
admission, including waiting time. Promotion uses the same lock order; only the
smallest live waiting allocation ordinal may take a free running slot. An
expired C01 waiter may become released/expired in this operation. A running
claim is never reclaimed merely because its deadline passed.

Release follows capacity, replica, parent, catalog and summary order and remains
available during drain. Exact digest/reason replay returns released; a different
fingerprint or terminal reason conflicts. It updates all child/parent states and
counts together.

Scale-in and termination lock capacity then replica before changing admission
state. New acquisition either commits before that change and must join, or waits
and rejects. Finish-leave checks zero live claims under capacity/replica locks
without acquiring a summary. EC2 proof takes capacity/replica, then parents by
UUID, catalog and sorted summaries. No reverse lock order is allowed.

## Released receipts

Released parents and children are replay receipts, not charged capacity. Keep
them for 24 hours from released_at. Maintenance locks at most 256 eligible
parents by claim UUID, then affected catalog and summaries in the same order. It
deletes children before parents atomically, then may remove fully unreferenced
zero-count summaries. Selection rechecks the exact cutoff under locks.

Never select, release or delete waiting/running claims by age, deadline,
heartbeat, disconnect or replacement. Abrupt reclamation requires verified EC2
termination. The
[five-minute operation bound](claim-identities.md#operation-lifetime) prevents
an old ambiguous request from using post-GC absence to reacquire. Receipt
retention bounds time and per-page work, not absolute traffic volume.

## Caller boundaries and proof

R4 may generate an inert render job UUID before acquiring C01. It installs no
queue entry, snapshot, capability, controller, callback or local authority until
an exact positive claim exists. The acquire request binds that UUID in one round
trip. Job ID alone grants nothing. Keep local render affinity and the returned
admission time/deadline unchanged.

R7 preserves hash two/16, two actual mail sends with durable lease/fence rules,
and MCP four/user with no waiting. R6 acquires IP/optional-account atomically
before local Hub/FD admission; local failure releases the shared claim. Closing
a stream joins local work before release. No stream holds a DB connection.
Existing status, body, Retry-After, queue, heartbeat and local caps stay fixed.

Prove exact caps with two pools; atomic SSE denial; UUID/replica/work/scope
replay and conflicts; promotion order; late positive response; cancellation and
original deadlines; drain/fence races; receipt-GC boundaries; count drift and
forged catalog/parent/child states. Include several same-scope claims so request
ordinal cannot accidentally limit capacity to one. Scope/digest vectors and
ambiguity age boundaries are in the identity contract.

R1 owns serialized schema, queries and store tests. R5 owns the adapter and
encoder; caller packages follow afterward. R8 owns key/readiness composition and
resource checks. No public HTTP, MCP, SSE or OpenAPI field changes.
