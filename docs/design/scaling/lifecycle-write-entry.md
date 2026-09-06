# Exclusive lifecycle write entry

Status: Accepted under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). This is
the target contract. Local implementation and hosted proof remain Phase 10
gates.

This contract defines the exclusive transaction path for only `begin_wake` and
`complete_wake`. [Transaction entry](transaction-entry.md) remains the ordinary
shared path. [Lifecycle operations](lifecycle-operations.md) defines wake state,
and [lifecycle replay](lifecycle-replay.md) defines immutable action identity.
[Wake operations](wake-operations.md) fixes fresh predicates, time and
transport. [Wake migrations](wake-migrations.md) fixes protected migration while
closing.

## Fixed Go API

The store exposes one fixed interface:

```go
type LifecycleWakeStore interface {
    BeginWake(
        context.Context,
        int64,  // expected controller generation
        string, // operation ID
        WakeMode,
        int64, // expected write generation
    ) (RuntimeCapacityResult, error)
    CompleteWake(
        context.Context,
        int64,  // expected controller generation
        string, // operation ID
        int64,  // expected write generation
    ) (RuntimeCapacityResult, error)
}

func NewLifecycleWakeStore(*pgxpool.Pool) LifecycleWakeStore
```

`WakeMode` admits only `serving` and `maintenance`. The concrete runner,
connection lease, transaction, entry, commit, rollback, retirement capability,
and SQL calls are private. There is no callback, generic command, raw
transaction, SQL string, advisory key, role selector, or gate override.

Both methods acquire one pool connection and capture the accepted
`pgtransport.Capability` before SQL. They begin a default read-write transaction
and immediately call their matching fixed entry function with the same validated
operation ID; begin also supplies the same fixed wake mode. No query, hook,
metric, row lock, or application callback may intervene. After entry they call
their one matching wake function with those exact values, then immediately
commit. They issue no `runtime_enter_write`, `runtime_finish_write`, other
lifecycle action, or query.

## Exclusive entry

The only callable entries are:

```sql
runtime_enter_begin_wake(operation_id text, wake_mode text) RETURNS void
runtime_enter_complete_wake(operation_id text) RETURNS void
```

Only `aboutme_lifecycle_command` gets EXECUTE. Each wrapper calls an owner-only
helper with its literal command. Begin entry accepts only `serving` or
`maintenance`; complete entry has no caller-selected mode. Both validate the
operation ID as 1 through 128 printable ASCII bytes before taking durable locks.
No login can call the helper or choose a command.

Entry first requires exact `session_user = 'aboutme_lifecycle_command'`. It then
takes the exclusive transaction advisory lock on the existing fixed 64-bit
`aboutme.runtime-write-barrier.v1` key. It does not inspect the current write
gate: fresh begin starts from `closed`, fresh complete starts from `closing`,
and an exact historical replay may run after later gate changes.

Entry rejects any normal or wake migrator session row, ordinary
`runtime_write_entry_v1` row, ordinary finish guard, wake marker row, or wake
finish guard already held by this backend. It creates or validates owner-owned
`pg_temp.runtime_lifecycle_write_entry_v1` with fixed static DDL. Validation
checks:

- temporary namespace and persistence;
- owner `aboutme_runtime_owner`;
- exact columns, types, nullability, primary key, checks, and ON COMMIT DELETE
  ROWS behavior;
- exactly one enabled deferred AFTER INSERT constraint trigger with the fixed
  owner function;
- no privilege for PUBLIC or any login role.

The one row contains current xid8, backend PID, session-role OID, command
`begin_wake` or `complete_wake`, exact operation ID, wake mode, and
`finished=false`. Begin entry stores its supplied mode. Complete entry initially
stores null mode. After locking and validating the immutable operation parent
and begin predecessor, complete wake calls an owner-only fixed helper to bind
that parent's `serving` or `maintenance` mode before any durable write. The
helper accepts no caller value and changes only null to the derived mode once. A
lookalike, extra row, mismatch, or catalog ambiguity is SQLSTATE `AM001`; it is
never repaired or dropped.

Entry rejects a wake finish guard already held, then leaves the exclusive
barrier held for the transaction. Ordinary `runtime_enter_write` and migrator
entry must reject a wake marker or guard. Wake entry rejects their markers and
guards. A transaction cannot mix entry modes.

## Finish assertion and guard

The wake definer finishes the marker only after its durable state changes. It
uses:

```sql
pg_try_advisory_xact_lock(258257617, pg_backend_pid())
```

Namespace `258257617` is big-endian SHA-256 prefix `0f64b2d1` of
`aboutme.lifecycle-write-finish.v1`. No other operation uses it. A held or
unavailable key is `AM001`; finish never waits after row locks.

The owner-only marker assertion requires the exclusive barrier, exact backend,
xid, role, command, operation ID, non-null valid mode, and finish guard. Commit
with `finished=false` fails. Forcing constraints invokes only the assertion.
Savepoint rollback restores the marker, guard, and durable changes together.

Pending trigger events prevent `DISCARD TEMP` before finish. After forced checks
and finish, the guard prevents a second wake entry even if temporary objects are
discarded. Commit or rollback releases both advisory locks. No caller can mark
the row finished directly.

## Assertion-trigger branch

The R1 migration replaces the bounded B1 assertion checks; it does not edit
historical migration 00013. `runtime_assert_write_entry()` remains a trigger and
is never called as an ordinary SQL function.

For ordinary entry, the statement trigger composes the existing owner-only
`runtime_require_write_entry()` behavior unchanged. For exclusive wake entry,
the BEFORE STATEMENT branch can inspect no row. It checks only the exact marker,
exclusive lock, session role, xid, PID, `TG_TABLE_NAME`, and `TG_OP`. It accepts
only this fixed statement catalog:

- UPDATE of `runtime_capacity`;
- INSERT of `runtime_lifecycle_operations`;
- INSERT of `runtime_lifecycle_operation_steps`;
- UPDATE of `runtime_write_state` as the last durable mutation.

No statement branch reads `NEW`. Owner-only BEFORE ROW INSERT triggers provide
the value checks that statement triggers cannot:

- On `runtime_lifecycle_operations`, only a `begin_wake` marker may insert. The
  new operation ID must equal the marker. Its workflow kind must be
  `uat_serving_wake` for marker mode `serving` or `maintenance_wake` for mode
  `maintenance`. A complete-wake marker cannot insert a parent.
- On `runtime_lifecycle_operation_steps`, the new operation ID must equal the
  marker, `NEW.action` must equal its exact `begin_wake` or `complete_wake`
  command, and `NEW.workflow_kind` must match the marker mode with the same
  mapping. Its parent and predecessor foreign-key/shape checks remain.

The step trigger binds the fixed entry invocation to the exact action before
insertion. An owner deferred assertion may recheck parent, step, and result
shape at commit, but it cannot replace these BEFORE ROW checks.

No exclusive branch permits application business tables, membership,
transitions, claims, rates, maintenance tables, stop receipts, or any other
lifecycle action. Existing owner-only immutability triggers remain.

`runtime_assert_business_write()` never accepts exclusive wake entry. Wake
bookkeeping cannot set `accepted_write` or change `last_accepted_writer_at`.
Direct DML remains revoked from lifecycle-command; the branch exists only for
the named SECURITY DEFINER functions.

## SQL actions

```sql
runtime_begin_wake(
  expected_controller_generation bigint,
  operation_id text,
  wake_mode text,
  expected_write_generation bigint
) RETURNS runtime_capacity_result

runtime_complete_wake(
  expected_controller_generation bigint,
  operation_id text,
  expected_write_generation bigint
) RETURNS runtime_capacity_result
```

`runtime_capacity_result` has these fields in fixed order:

```text
desired_replicas smallint
active_serving_replicas smallint
active_maintenance_replicas smallint
partition_1_enabled boolean
partition_2_enabled boolean
capacity_generation bigint
controller_generation bigint
controller_operation_id text
admission_enabled boolean
lifecycle_phase text
write_gate text
write_generation bigint
replayed boolean
```

Wake action rows store every field except derived `replayed`. Both write fields
are non-null only for wake actions. Begin stores `closing`; complete stores
`open`. Replay returns the immutable historical values, not current state.

After exclusive entry, each function locks the first four items in this order. A
fresh action then locks the fifth item:

1. `public_state` singleton;
2. `runtime_capacity` singleton;
3. existing lifecycle operation parent, or inserts a fresh parent while the
   capacity singleton serializes first use;
4. matching action row, if it exists, or inserts it for a fresh action;
5. `runtime_write_state`, last, only after the action is known to be fresh.

The functions do not lock a replica, transition, claim, rate, business, or
stop-receipt row. Receipt invalidation is represented by the accepted
`runtime_write_state` fields and occurs in its final update, not by taking a
separate receipt row lock.

For a fresh action, validate the operation graph, wake mode, current controller
generation, current write generation, and current gate before mutation. Begin
requires `closed` and moves it to `closing`. Complete requires the same
operation's successful begin predecessor, `closing`, and its prerequisites, then
moves to `open`. Both increment capacity generation, controller generation, and
write generation exactly once. They update controller operation evidence and
write-state timestamps.

The function locks `runtime_write_state`, then samples the fixed lifecycle clock
once. It clamps against prior capacity, parent/predecessor, write-state updated
time and last-write time, as specified in [wake operations](wake-operations.md).
It leaves `last_accepted_writer_at` unchanged. The write-state row is the last
durable state mutation. The function inserts its immutable typed action result,
then updates write state last; the action insert contains the already computed
post-action values and commits atomically with that update. It then takes the
wake finish guard and marks only the temporary marker finished.

An existing exact action is replay. The function locks and validates the parent
and action, marker-bound operation ID and mode, argument digest, fixed command,
and stored result. It does not require current gate or generations to equal the
historical result, does not lock or update `runtime_write_state`, and increments
nothing. It marks the private operation marker finished and returns the stored
result with `replayed=true`. A replay after later lifecycle or ordinary writes
therefore returns its original gate and write generation.

## Errors and cleanup

- SQLSTATE `42501`: wrong role or direct helper/function access.
- SQLSTATE `22023`: malformed operation ID, mode, generation, or argument.
- SQLSTATE `55000`: well-formed but stale generation, wrong fresh gate,
  incomplete predecessor, or unavailable lifecycle state. It grants no retry
  inside the method.
- SQLSTATE `AM002`: immutable operation/action replay conflict.
- SQLSTATE `AM001`: marker, catalog, barrier, guard, command, backend, xid,
  role, or assertion contamination. The connection is poisoned.

If entry fails, no other SQL runs. A definite SQL failure uses a detached
five-second rollback on the same connection. Confirmed rollback plus valid
cleanup may return a clean backend only for ordinary `22023`, `55000`, or action
conflict. `AM001`, ambiguous begin, entry, definer, rollback, commit, or
cleanup, and every commit error physically retire the exact backend through the
accepted `pgtransport.Capability`. Caller-owned `*pgxpool.Pool` stays open.

The runner preserves the primary error and joins cleanup errors. It never
replays inside the method. After an ambiguous result, the controller makes a new
explicit method call; the immutable ledger returns exact replay or conflict. No
method reconstructs historical results from current rows.

## Grants and composition

`aboutme_runtime_owner` owns marker objects, helpers, action functions, types,
and triggers. Revoke PUBLIC. Grant lifecycle-command only the two entry wrappers
and matching wake functions. App, maintenance, fencing-proof, migrator, and
restore receive none. Lifecycle-command has no marker, table DML, helper,
ordinary entry, ordinary finish, or final-stop grant through this contract.

The controller calls begin wake, performs
[protected wake migration](wake-migrations.md) and read-only
reconciliation/planning, then calls complete wake. R8 proves the external
prerequisites before that fixed call. Serving or maintenance registration and
activation happen later through their existing fixed methods. A clean closed
wake needs no bypass. Corrupt shutdown or wake remains unavailable. Final stop
requires all transitions terminal before gate closure.

## Acceptance

- Fresh begin accepts only `closed`; fresh complete accepts only its predecessor
  and `closing`. Each advances all three generations once and returns the exact
  historical gate and generations.
- Entry failure executes no other SQL. Ordinary, migrator, finalizer, and
  generic lifecycle entry cannot substitute for a wake entry.
- Wrong role, helper access, table/action, marker, guard, PID, xid, command, and
  mixed ordinary/wake entry fail without durable mutation.
- Entry/action operation-ID mismatch, begin parent workflow mismatch, complete
  parent insertion, wrong step action, and step workflow mismatch fail in the
  owner BEFORE ROW trigger.
- Exclusive assertion permits only the closed table/action catalog. It rejects
  every business table and leaves `last_accepted_writer_at` unchanged.
- Forced constraints, savepoint rollback, `DISCARD TEMP`, repeated finish, and
  re-entry cannot erase or recreate unfinished authority.
- Stale controller/write generation, wrong mode/gate, missing predecessor, and
  conflicting operation/action fail without increments.
- Replay after later gate, controller, capacity, or write-generation changes
  returns the original result and increments nothing.
- Two fresh calls with one operation ID serialize at capacity and produce one
  parent/action or a fixed conflict.
- Rollback wire failure, commit response loss, cleanup ambiguity, and AM001
  physically retire the exact backend. The wrapper never retries.
- Begin and complete do not change the accepted-writer tail.
