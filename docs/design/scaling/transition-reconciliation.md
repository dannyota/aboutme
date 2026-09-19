# Public transition reconciliation

Status: Accepted under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). The
[scaling index](README.md) records whether it is built.

Use one database snapshot and one local apply lock to reconcile durable
[transition outcomes](transition-recovery.md) with current public state.

## Fixed snapshot interface

Do not change runtime_recover_public_transition. Add this separate fixed read:

```sql
runtime_reconcile_public_transition(
  transition_id uuid,
  target_digest bytea
) RETURNS TABLE(
  parent_state text,
  operation text,
  terminal_at timestamptz,
  terminal_error_code text,
  target_ordinal integer,
  target_kind text,
  resume_id uuid,
  expected_generation bigint,
  class text,
  result_kind text,
  result_generation bigint,
  result_recorded_at timestamptz,
  current_present boolean,
  current_generation bigint,
  proven_retired boolean,
  blocking_transition_ids uuid[]
)
```

transition_id must be non-nil and target_digest exactly 32 octets. Missing or
mismatched parent, invalid storage shape, more than four rows, a null array, or
an inconsistent result is AM001. The function returns exactly one row per stored
target in ordinal order. It accepts no identity, current-state assertion,
generation, blocker list or caller boolean.

`current_present` is true for discovery only when the public_state singleton
exists, and true for a resume only when that exact resume UUID currently exists.
`current_generation` is its discovery generation or resume revision when present
and null exactly when absent. For a resume target, `proven_retired` is true when
any immutable committed transition target for that exact resume UUID has
result_kind retired. The requested transition need not be the retirement: an
older generation or rollback notification may arrive after a later committed
retirement. It is never derived from current absence. Discovery can never be
proven retired. More than one retirement result for one UUID is accepted only
when every matching row is valid committed evidence; UUID reuse remains
forbidden.

`blocking_transition_ids` contains the requested transition ID when its parent
is closing or unresolved, plus every other closing or unresolved transition
whose normalized targets intersect this row's discovery or resume target. The
array is UUID ordered with no duplicates. Fixed read-only checks validate each
blocker's stored shape and digest against the storage contract; they never call
a trigger function as an ordinary function. The caller needs blocker identity
only to prevent an open; blocker state or mutable current evidence is not
returned as authority. More than one blocker for a target is AM001 because
serialized begin forbids overlapping closing/unresolved ownership; arrays are
therefore bounded to zero or one UUID.

Implement it as one `STABLE SECURITY DEFINER` PL/pgSQL function invoked by one
top-level SELECT statement. PostgreSQL gives every SQL command issued by a
STABLE function the calling statement's established MVCC snapshot. The function
performs no write, lock, advisory operation, temporary DDL or volatile clock
read. Its fixed owner-only queries read the parent/results, public_state,
resumes and intersecting closing/unresolved rows from that one snapshot. A
multi-statement autocommit composition and a VOLATILE function are forbidden
because either could observe multiple READ COMMITTED snapshots.

aboutme_runtime_owner owns the function and reads the underlying owner tables.
Set search_path=pg_catalog and qualify every object. Revoke EXECUTE from PUBLIC,
migrator, maintenance, lifecycle-command, fencing-proof and restore; grant only
EXECUTE to aboutme_app. No login role receives SELECT on transition tables or
the current business tables through this contract. The function returns no
request body, slug, user, audit, media key or replica identity.

## Store boundary

The matching private Go value is:

```go
type TransitionReconciliation struct {
    TransitionID uuid.UUID
    TargetDigest [32]byte
    ParentState TransitionState
    Operation TransitionOperation
    TerminalAt *time.Time
    TerminalErrorCode *string
    Targets []TransitionReconciliationTarget
}

type TransitionReconciliationTarget struct {
    Ordinal int32
    Kind TargetKind
    ResumeID *uuid.UUID
    ExpectedGeneration int64
    Class TransitionClass
    ResultKind *TargetResultKind
    ResultGeneration *int64
    ResultRecordedAt *time.Time
    CurrentPresent bool
    CurrentGeneration *int64
    ProvenRetired bool
    BlockingTransitionIDs []uuid.UUID
}
```

The store exposes only
`LoadTransitionReconciliation(context.Context, uuid.UUID, [32]byte) (TransitionReconciliation, error)`.
It has no transaction, query, rows or callback parameter. The method issues the
one function SELECT, reads at most four rows, checks stable repeated parent
fields and ordinal order, deep-copies arrays and byte values, consumes rows,
checks Rows.Err and closes Rows before return. No `pgx.Rows`, connection or
slice backed by driver memory escapes. A query, iteration or cleanup error
returns no snapshot; the coordinator keeps fences closed. The store defines
closed string-backed TransitionState, TransitionOperation, TargetKind,
TransitionClass and TargetResultKind types and rejects unknown database values
before returning. The publicstate adapter converts these private store values;
store does not import publicstate. The
[transport contract](transition-transport.md) requires an explicit pool lease
and physical-retirement capability before the read. AM001 retires that exact
backend even after clean protocol completion. Every uncertain cleanup path
returns no snapshot and cannot release an unproved connection for reuse.

## Local application order

The coordinator acquires its local publicstate transition-apply mutex before
starting that read. While holding the mutex it calls only this fixed store
method. The single statement ends and its rows are closed before the immutable
value returns. The store invokes no caller callback during SQL or pgx cleanup.
The coordinator then applies that unchanged snapshot once while still holding
the mutex and releases the mutex last.

Local Begin/Close/terminal application uses the same mutex. Local close and join
finish before ack SQL starts; no caller holds a PostgreSQL transition/business
lock while waiting for this mutex. A later begin committed before the read is
visible as an intersecting closing transition. A later begin committed after the
snapshot cannot obtain this replica's ack until its local close acquires the
mutex after reconciliation. It therefore closes the fence before its business
commit. A terminal change committed after a closing snapshot causes this pass to
keep the fence closed; its terminal event triggers another reconciliation.

Local resume state has this monotonic precedence while the mutex is held:

1. retired is absorbing for the process lifetime. No generation or rollback
   replay can change it to closed or open.
2. validate retirement/current-row consistency. Proved retirement plus a present
   row reports corruption and leaves a nonretired target closed. An
   already-retired target stays retired but still reports the contradiction.
3. proved retirement plus absence moves a nonretired target to retired.
4. a blocker or inconsistent/absent evidence keeps a nonretired target closed.
5. an eligible generation may open only at max(local known generation, current
   snapshot generation). It never lowers a local generation.

The process starts resume fences closed after restart. Its first reconciliation
does not rely on remembered local retirement: the same one-snapshot query finds
the immutable committed retirement for the UUID and returns proven_retired.
Transition rows/results have no delete path, so this authority survives process
restart and delayed notification order.

## Result application

Apply this exact resume matrix with the local precedence rules:

- proven_retired plus absent current row: retire the local target, regardless of
  whether the requested old transition has generation, rollback or retired
  outcome;
- proven_retired plus present current row: corruption, keep a nonretired target
  closed; an already-retired local target remains retired and readiness reports
  the database contradiction;
- committed plus generation result and present current revision at least the
  result generation: open at the current revision only when no intersecting
  closing/unresolved transition exists;
- committed plus generation result and absent current row: keep closed and
  unavailable; absence has no retirement authority;
- rolled_back plus present current revision at least expected_generation: open
  at the current revision only when no intersecting closing/unresolved
  transition exists;
- rolled_back plus absent current row: keep closed and unavailable; rollback
  never retires;
- any present revision below the historical result or expectation: corruption,
  keep closed;
- closing or unresolved, or any intersecting closing/unresolved transition: keep
  closed regardless of current row.

For discovery, committed generation and rolled_back expectation are lower
bounds. Open only at the current public_state generation when it is at least the
bound and no intersecting closing/unresolved transition exists. A lower value or
missing singleton keeps discovery closed.

Thus only an immutable committed retired result for the exact UUID authorizes
retirement. Current absence never does. Delayed or reordered replay cannot
regress a higher generation, reopen a retired resume or override a later
transition fence. The coordinator decides the HTTP response after releasing the
local apply mutex. A legitimately removed 24-hour response receipt does not
invalidate committed publication authority. A later request after idempotency
expiry follows existing fresh-request semantics and uses a new transition ID.

## Required proof

- Real-role EXECUTE and direct table/helper denial, exact owner/search path, one
  STABLE statement snapshot and no volatile nested read.
- Nil/missing transition, wrong digest, malformed parent/target/result, more
  than four rows, inconsistent repeated fields and malformed blocker arrays
  return no snapshot. Never truncate a blocker set.
- Row iteration or cleanup error returns no snapshot and closes admission. No
  driver-owned memory or connection escapes the method.
- Begin before the snapshot is visible as a blocker. Begin after the snapshot
  waits for local close before ack. Terminal commit after a closing snapshot
  keeps the current pass closed. Local close and apply use the same mutex.
- Later generation never regresses. A later committed retirement followed by an
  old generation/rollback event remains retired in-process and after restart.
  Absence without immutable retirement evidence stays closed.
- Proved retirement plus present row reports corruption, including when local
  state is already retired. An unresolved/closing blocker never opens a target.
- Expired response receipts do not reverse committed publication authority.

The store owns the serialized migration, fixed query and store tests. The
coordinator owns the local mutex, fence application and multi-coordinator tests.
Run their focused Go tests and the affected migration/database gates after
implementation. No runtime proof is claimed by this design.
