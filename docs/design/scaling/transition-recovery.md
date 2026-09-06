# Public transition recovery evidence

Status: Accepted under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). R1 owns
fixed SQL/store operations; R2/R3 own caller integration. Implementation and
local concurrency proof remain pending.

## Outcome authority

This contract defines the smallest ordinary recovery operation that may land
after the four-table storage migration. It adds no table column and no callable
closing-to-unresolved operation. The transition parent, target results and
business mutation commit in one PostgreSQL transaction. That atomic record is
the durable commit evidence; current application rows are validation inputs for
closing rollback, not a second commit oracle.

Authorities are [transition storage](transition-storage.md),
[public transitions](public-transitions.md),
[transition commit](transition-commit.md) and
[runtime coordination](runtime-coordination.md). Current behavioral evidence is
apps/server/internal/publicstate/coordinator.go and recovery.go,
apps/server/internal/resumeapi/recovery.go and writesafety.go,
apps/server/internal/accountapi/recovery.go, and the schema and queries named
below.

## Fixed outcome rule

After locking one transition parent, exactly these durable outcomes exist:

1. committed: the same transaction committed all business writes, complete
   immutable target results and the committed parent. The transition is the
   publication authority.
2. rolled_back: no business transaction for that transition committed because
   only the commit-capability transaction may change the parent to committed.
3. closing: no committed transition transaction exists at the instant the
   recovery transaction obtains the parent lock. Any open business transaction
   must first finish or roll back because it holds that same row lock.
4. unresolved: terminal failed-closed state. Ordinary recovery cannot create or
   repair it.

A lost COMMIT response does not create a third result. If COMMIT reached
PostgreSQL, parent and results are committed. If it did not, recovery eventually
locks a still-closing parent. A lock timeout, statement timeout, connection loss
or ambiguous recovery commit returns unavailable and changes no inferred state.

## Ordinary recovery rollback

Keep the accepted signature:

```sql
runtime_recover_rollback_public_transition(
  transition_id uuid,
  initiator_replica_id uuid,
  initiator_instance_id text,
  initiator_release_digest text,
  target_digest bytea
) RETURNS TABLE(
  state text,
  terminal_at timestamptz
)
```

The function is SECURITY DEFINER owned by aboutme_runtime_owner. It uses
search_path=pg_catalog, qualified fixed SQL and no dynamic SQL. PUBLIC and every
non-app login role have no EXECUTE. aboutme_app receives EXECUTE only. The
private R8 adapter supplies its fixed replica, instance and release tuple;
shared app login is not incarnation authentication. No HTTP body, reason,
boolean, expected result or error text is an argument.

It runs in a fresh WriteTxRunner transaction. Write entry acquires the shared
runtime barrier first. The function then:

1. Locks public_transitions by transition_id FOR UPDATE. Missing is AM001.
2. Compares target_digest and all three immutable initiator fields in every
   state. A mismatch is AM001 with no mutation.
3. For committed or rolled_back, returns the immutable state and terminal_at. It
   does not inspect current business rows and does not rewrite terminal data.
4. For unresolved, returns AM001 and leaves the row unchanged.
5. For closing, reads targets in ordinal order. It requires the complete stored
   target set and digest assertion to remain valid.
6. Through one fixed nonlocking validation query, reads public_state for a
   discovery target and every resume target in UUID order. Discovery must equal
   expected_generation. Every resume UUID must exist at expected_generation.
7. Any missing target, changed generation, missing resume, malformed result or
   invariant failure returns AM001 without terminalizing. It does not infer
   retirement from absence.
8. If every target is unchanged, changes closing to rolled_back with fixed
   terminal_error_code business_not_started, one database terminal_at sample,
   null fencing evidence and null target results. It emits the accepted terminal
   notification in the transaction and returns the stored row.

The stored name business_not_started means no business change from this
transition committed. It does not prove that no business statement executed. A
callback may have issued SQL before its transaction rolled back. Current-row
validation proves only that reopening the expected public generation is safe; it
is not execution-attribution evidence.

These nonlocking reads are validation, not commit attribution. Begin rejected
every visible intersecting closing or unresolved transition before this parent
was created. While this parent remains closing, every supported intersecting
writer must own this exact transition and acquire this parent before business
SQL. Recovery already owns that parent, so no supported writer can advance or
remove an affected generation between the validation statement and rollback. An
uncommitted business writer is also excluded by the parent lock. This is the
same execution-fence proof used by commit entry and preserves the accepted lock
graph: shared runtime barrier, then transition parent, with no business-row
lock. The function takes no public_state, resume, membership, capacity, replica,
slug, user, session, idempotency, tombstone, audit or media-job lock.

This proof depends on complete R2/R3 caller migration: no supported public
generation writer may bypass transition overlap and parent fencing. Such a
bypass is an implementation defect and must fail the writer-inventory test; it
is not repaired by adding a reverse recovery lock edge.

The function need not wait until deadline_at. It is an exact same-incarnation
abandon operation after an ambiguous business transaction. Once it owns the
parent, a delayed original transaction cannot later pass the parent execution
fence. The caller remains bounded by its recovery context and gets no internal
retry. An independent invocation may reread the durable result.

## Why application evidence is not the outcome oracle

### resume_update and resume_publication

- idempotency_records bind user_id, route, key and request_hash and retain the
  response for 24 hours. They are deleted by request-path or hourly retention in
  apps/server/sql/queries.sql and privacy_retention.sql.
- A resume revision may advance again in a later valid transition. Its current
  equality with expected+1 cannot attribute that value to an old operation.
- Slug ownership may change again. slug_tombstones describe a release but are
  keyed by slug rather than transition and their released user reference becomes
  null on account deletion.

### resume_retire

- Current resume absence is not operation identity. It may reflect a later
  deletion and therefore cannot prove that this transition retired the row.
- media_deletion_jobs are durable cleanup work, not ownership. Completed rows
  are deleted by bounded retention. A resume without a photo has no such row.
- A slug tombstone exists only for a prior slug and is not a transition receipt.

### account_retire

- Deleting users cascades their idempotency records, so idempotency absence is
  expected on successful account deletion.
- lifecycle_audit_events account_deleted rows use a caller-planned UUID, but
  bounded retention later deletes them. Their absence cannot disprove an old
  commit.
- Media jobs exist only for resumes with photos and completed jobs are later
  removed. Tombstone user references become null through ON DELETE SET NULL.
- Current user or resume absence can result from a later operation and does not
  identify this transition.

Sources are apps/server/migrations/00004_add_resume_tables.sql,
00006_bound_retention_and_media_cleanup.sql, apps/server/sql/queries.sql,
account_deletion.sql and privacy_retention.sql. Existing resumeapi/recovery.go
and accountapi/recovery.go combine these observations to resolve today's local
in-memory fence after an ambiguous commit. They must not remain the durable
transition state oracle once transition commit is atomic.

## Response reconciliation stays separate

For a committed resume mutation, the caller may read its existing
idempotency_records key on a fresh connection to recover the exact HTTP
response. It must require the existing request hash and stored response shape,
as current resumeapi recovery does. A matching record allows the response
replay. Missing, expired or conflicting response evidence returns the original
ambiguous error or the existing unavailable mapping; it does not change the
committed transition, close its already-proven results or create unresolved
state.

For account deletion, the public HTTP success is the existing fixed response.
The committed transition results prove every retirement and discovery result. No
retained account-owned idempotency row is required.

Immutable terminal results establish this transition's historical outcome. R2
uses the fixed [reconciliation snapshot](transition-reconciliation.md) before
changing a local fence. That contract preserves later generations and proved
retirements across delayed events and process restarts.

## Closing-to-unresolved boundary

Install no runtime_mark_public_transition_unresolved function in R1/R3. The
four-table constraints make parent, ordered targets, digest, results, required
replicas and acks internally coherent. The remaining observations do not prove a
contradiction attributable to this transition:

- target generation mismatch or resume absence proves only that rollback cannot
  safely reopen the expected generation;
- missing idempotency, audit, tombstone or media-job evidence may be normal
  retention, cascade, completion cleanup or an operation that never needed it;
- a conflicting retained idempotency record is bound to a request key, not to
  transition_id, and the same key may be fresh after expiry;
- timeout, unavailable SQL, adapter mismatch and local recovery failure are not
  durable evidence.

On any such case, leave closing unchanged and readiness unavailable. Emit a
bounded internal anomaly metric/log without request data. Operators repair the
underlying invariant through a later reviewed recovery design; no caller can
forge a terminal state through text or a boolean. Marking unresolved provides no
extra safety over durable closing and would destroy the only legal future
rollback edge.

No later column is needed for this contract. If a future product requirement
demands response replay beyond existing idempotency retention, it needs a
separate immutable transition-to-response receipt with a defined retention
period. That is outside current product scope and is not required for public
generation recovery.

## Concurrent and failure cases

- Business holds parent and commits: recovery waits, then returns committed
  target results without reading mutable application evidence.
- Business holds parent and rolls back: recovery obtains closing, validates
  expected target generations and writes business_not_started.
- Recovery rolls back first: a delayed business entry sees rolled_back and runs
  no business SQL.
- Two recovery callers: the parent serializes them; one writes rollback and the
  other returns its immutable terminal row.
- A later valid mutation advances a formerly committed target: replay of the old
  committed transition still returns its stored result; it does not demand that
  current row equal the historical result.
- Closing plus changed/missing target: recovery returns AM001, leaves closing
  and does not call an unresolved writer.
- Recovery COMMIT response lost: the next invocation returns rolled_back if the
  terminal write committed, or repeats the closing validation if it did not.
- Database, lock or statement timeout: no inference and no retry in the same
  invocation.
- Exact EC2 termination: only runtime_recover_fenced_public_transition may use
  its immutable proof. Ordinary recovery accepts no proof and infers no death.
- Delayed notification: runtime_recover_public_transition reads the parent and
  immutable results; notification order carries no authority.

## Required proof

The R1 author proves the real-role grant matrix, every tuple/digest mismatch,
immutable terminal replay, closing-generation validation, parent-lock races,
response loss and terminal notification only after commit. R2/R3 prove complete
writer coverage, response-retention behavior and the
[reconciliation matrix](transition-reconciliation.md).

Run focused migration/store tests with independent pinned connections. Root runs
`make sqlc-check server-test-db server-test-integration server-migration-test`
after integration. These checks have not run for these future functions.
