# Protected migrations during wake

Status: Accepted detail of [exclusive wake](wake-operations.md) and
[protected migration](migrator.md) under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md).
Implementation and runtime proof remain Phase 10 gates.

> **Implementation deferred.**
> [ADR 0036](../../adr/0036-single-replica-launch-and-pipeline-migrations.md)
> retires this path for the first release, which runs one serving replica and
> migrates from the deployment rather than from a waking fleet. The contract
> stays accepted for a later second replica.

Normal migration entry requires an open gate. This fixed path permits protected
versions during the recorded wake's closing gate while ordinary writers remain
unavailable. It adds no closed-gate bypass or final-stop authority.

## Fixed session entry

The committed begin releases its exclusive barrier. Protected migrations then
take the ordinary shared barrier, and complete later waits for that shared lock
to be released before taking the exclusive barrier. The current
runtime_enter_migrator remains open-gate only. Add this separate fixed session
surface:

```sql
runtime_enter_wake_migrator() RETURNS void
runtime_exit_wake_migrator() RETURNS void
```

Only aboutme_migrator gets EXECUTE. Both are runtime-owner SECURITY DEFINER,
fixed search_path=pg_catalog, and accept no operation ID, mode, gate or bypass
flag. Wake entry takes the same session-level shared runtime barrier used by
normal protected migration, then validates one active wake from durable state:

- write gate closing, lifecycle starting and admission disabled;
- capacity.controller_operation_id names an immutable uat_serving_wake or
  maintenance_wake parent with an exact committed begin_wake step;
- that step has valid retained shape/digest, result gate closing, phase
  starting, admission false, desired one, zero counts and both partitions
  disabled;
- its stored controller generation equals current capacity controller
  generation, and its stored write generation is no greater than current write
  generation;
- there is no complete_wake step for that parent, no nonterminal replica, and no
  visible closing/unresolved transition;
- migrator enforcement is 1, history owner is runtime_owner, and the protected
  history trigger/ACL/catalog state is exact.

These predicates derive the wake operation and mode from immutable current
capacity evidence. A caller cannot select another workflow. The shared barrier
excludes competing wake completion and final-stop operations until exit.
Ordinary lifecycle, app, maintenance and proof entry remain rejected by closing.
The source admission below constrains the admitted migration itself.

## Session marker

Wake entry creates or validates a distinct owner-only temporary
runtime_wake_migrator_session_v1 marker with singleton, backend PID,
session-role OID, entry generation, wake operation ID, wake mode and entered-at
columns. It uses ON COMMIT PRESERVE ROWS and the same strict static catalog/ACL
rules as the normal migrator marker. An empty valid retained table is reusable.
A nonempty normal migrator marker, wake marker mismatch, ordinary/wake write
marker, or finish guard is AM001. Normal runtime_enter_migrator likewise rejects
a nonempty wake-migrator marker; it does not turn closing into normal admission.

runtime_begin_migration_write is replaced in the forward migration to accept
exactly one validated normal or wake migrator session marker while the shared
barrier is held. It derives entry_generation only from that marker. Per-version
canonical operation IDs, dirty marking, runtime_finish_write, history INSERT
trigger, write-generation advance and Goose transaction framing remain
unchanged. The history trigger still authenticates session_user=migrator and the
exact per-version write marker; it does not treat the wake operation ID as a
migration ID.

runtime_read_migrator_metadata remains the existing fixed SECURITY DEFINER read
with its current ACL and no advisory-lock or session-marker requirement. Status
keeps its accepted read-only no-advisory use. ApplyWake calls the same accessor
only after its fixed wake entry has acquired and validated the wake session,
then applies a closing-specific metadata check in its private Go locker. The
accessor itself gains no wake branch and is not replaced.

## Per-version write entry

The same forward migration also replaces runtime_require_write_entry. Its
ordinary roles and normal-migrator branch still require gate open. Its sole
closing branch requires all of these facts:

- session_user and transaction writer_kind are exactly migrator;
- exactly one valid nonempty wake-migrator session marker is bound to this
  backend and held shared barrier, while the normal migrator marker is empty;
- the transaction write marker's backend, xid, role, writer kind and
  entry_generation match that wake session marker;
- the wake marker's operation/mode still passes the complete durable active-wake
  predicate above and current write generation is at least entry_generation.

Otherwise closing returns 55000 for a clean unavailable state or AM001 for a
marker/lock/binding contradiction. Closed is never accepted. Existing protected
table triggers and runtime_finish_write continue to call this one helper, so
they inherit the exact wake-session branch without a second finish function.
Finish updates only the existing generation, last-write, writer and updated-at
fields with its lock-then-clock high-water rules; it cannot change the gate,
accepted-writer time, capacity or wake ledger.

runtime_exit_wake_migrator rejects an active write marker, revalidates the wake
session marker and held shared barrier, deletes that marker and performs the
sole shared unlock. It then proves both migrator marker tables empty and the
shared lock absent. It never completes the wake or mutates durable state.

## Fixed Go runner

```go
func ApplyWake(context.Context, *sql.DB, MigrationIdentity, ...lock.SessionLockerOption) ([]*goose.MigrationResult, error)
```

ApplyWake supports the same fixed Direct and LocalAdmin identities as Apply;
LocalAdmin remains test/local impersonation of exact aboutme_migrator, not a new
SQL role. It accepts no operation ID or mode. It rejects fresh, pre-foundation,
enforcement-zero and adoption-required databases. It uses the same catalog-only
preflight, one ApplyVersion per protected version, Goose lock, backend PID
binding, contiguous history checks and static transactional-source validation as
protected Apply. Its runtime SessionLocker calls runtime_enter_wake_migrator
before Goose's lock and runtime_exit_wake_migrator during cleanup on that same
backend. Each new per-version backend rederives the same active wake under the
shared barrier.

No reconnect or retry occurs within a failed version. Capture the existing
pgtransport close-only capability before identity or SQL. Every ambiguous
entry/version/commit/unlock/cleanup outcome physically retires the exact socket
before return while leaving caller-owned *sql.DB open. Clean no-pending and
successful paths exit both locks, reset local impersonation, prove marker tables
empty, physically retire the migration backend under the existing B3 policy, and
return without an artificial cleanup error. The frozen version-13 adoption
manifest is unchanged.

## Source admission

Before it opens a provider or backend, ApplyWake validates every embedded source
against a compiled wake-safe registry. Each entry fixes the positive version,
exact SHA-256 of the embedded migration bytes, and an explicit reviewed
wake_safe true or false classification. Missing, duplicate, hash-mismatched,
nontransactional or non-SQL source fails before SQL. After wake entry and the
Goose lock establish the protected history version, ApplyWake requires every
candidate version still to apply to have wake_safe=true before invoking its
ApplyVersion. The registry is code-owned and has no environment, CLI or database
override. Already-applied authority-changing setup versions may remain
wake_safe=false; they are never executed in the wake window.

A source may be classified wake-safe only when its reviewed Up transaction does
not insert/update/delete/truncate, replace ownership/trigger enforcement on, or
call a mutator for runtime_capacity, runtime_write_state gate,
runtime_lifecycle_operations, runtime_lifecycle_operation_steps,
runtime_replicas, runtime_replica_tasks, public transitions or their required/
target/ack rows. It also cannot replace the wake/migrator barrier, marker,
require-entry, finish, digest or active-wake predicate functions during that
wake. The mandatory migration framing and Goose history INSERT are exempt;
runtime_finish_write may advance only write generation, last_write_at,
last_writer_kind, last_writer_operation_id and updated_at under its existing
clamps. It leaves write_gate and last_accepted_writer_at unchanged.

The registry is source admission for trusted, immutable migration sources, not a
SQL parser or a claim that the shared barrier constrains the admitted migration
itself. Its classification is recorded during the repository's existing
migration design, authoring and phase review; it adds no separate approval or
review workflow. A migration that changes wake authority must be applied while
the gate is open before final stop, or use a separately accepted recovery
design. ApplyWake never runs it and never repairs closing state.

## Required proof

- Normal Apply rejects closing. ApplyWake rejects open/closed, invalid or
  completed wake identity, unsupported source and contaminated marker state.
- Zero, one and multiple pending versions preserve the wake operation, gate,
  capacity, partitions and accepted-writer time. Each committed version advances
  write generation once. Rejection or rollback advances nothing.
- Wrong role, mixed markers, lookalikes, guard reuse, PID change and failed
  source/hash admission grant no authority. Every protected trigger and finish
  uses the exact wake-migrator branch while ordinary roles remain blocked.
- Complete waits on an active migration's shared barrier. A failed or ambiguous
  version retires its exact backend before return and never retries internally.
- Existing Status keeps its read-only metadata access without an advisory lock.
  The version-13 adoption manifest remains unchanged.

These checks belong to the later wake implementation slice. They have not run.
