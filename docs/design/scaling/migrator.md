# Dedicated database migration runner

Status: Accepted target under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md).
Implementation follows the
[B3 task](../../plans/phase-10/replica/migrator-composition.md). The
[write-entry contract](transaction-entry.md) owns the barrier and markers;
[database provisioning](migration-provisioning.md) owns fixed grants and
adoption.

## Fixed identities and API

Use the same explicit identity for both public entry points:

```go
Apply(ctx context.Context, db *sql.DB, identity MigrationIdentity,
    lockOpts ...lock.SessionLockerOption) ([]*goose.MigrationResult, error)
Status(ctx context.Context, db *sql.DB, identity MigrationIdentity) ([]*goose.MigrationStatus, error)
```

`MigrationIdentity` is an unexported-field enum constructed only by:

- `DirectMigratorIdentity()`: connection session_user must already be exact
  aboutme_migrator. Hosted migrate uses this.
- `LocalAdminMigratorIdentity()`: connection session_user must be exact aboutme
  and `current_setting('is_superuser')='on'`. Apply keeps this identity as
  aboutme only for an existing aboutme-owned pre-13 bootstrap. Fresh and
  protected stages execute fixed SET SESSION AUTHORIZATION aboutme_migrator.
  There is no role-name input. Native, Compose, CI, and tests use this
  constructor.

The migrate CLI requires fixed `MIGRATION_IDENTITY=direct|local-aboutme`; empty
is an error. It never accepts a role name. Hosted task composition hardcodes
direct and resolves only its existing aboutme_migrator SSM SecureString into
DATABASE_URL. Native/Compose/CI hardcode local-aboutme beside their existing
aboutme admin DSN. Hosted manifests and their structural test reject
local-aboutme and expose no admin database credential to the migrate task. This
is admission configuration, not a database bypass: runtime functions still
accept only session_user aboutme_migrator.

Tests that need arbitrary synthetic migration files use an internal
`applyFS(ctx,db,fsys,identity,...)`. `NewProvider` remains available only for
low-level Goose tests that deliberately do not claim production runtime safety;
all production and migration integration callers use Apply/applyFS.

## One backend and ordered session locks

Implement `runtimeSessionLocker`, wrapping Goose's PostgreSQL SessionLocker.
Goose Provider.initialize obtains one `*sql.Conn` and passes that exact handle
to SessionLock before version reads. runIndividually uses it for SQL migration
transactions. SessionUnlock receives it during cleanup.

Protected SessionLock order on that handle:

1. Establish/verify identity; local mode runs fixed session authorization.
2. Call runtime_enter_migrator. It takes the runtime SESSION shared lock before
   reading runtime state.
3. Call the wrapped Goose SessionLock.
4. Recheck backend PID, the confirmed session-entry outcome, runtime lock,
   enforcement metadata, Goose owner/shape/trigger/grants, and applied version
   under both locks.

B1 provides `runtime_read_migrator_metadata()` as a SECURITY DEFINER, read-only
accessor granted only to migrator. It returns gate, generation,
migrator_enforcement_version, and migration_history_owner from the singleton.
Apply calls it only after runtime_enter_migrator has acquired the runtime
SESSION barrier on that same backend. It supports the locked classification and
recheck; it is never a pre-entry probe. Migrator gets no direct
runtime_write_state SELECT. The accessor accepts no input, exposes no receipt
state, and does not authorize a write. Status has the explicit read-only
no-advisory exception described below.

If step 2 returns 55000, reset local session authorization after confirmed
runtime cleanup and return unavailable. AM001 or ambiguity poisons the backend.
Any wrapped Goose SessionLock error calls bounded runtime cleanup and then
poisons the backend. The pinned locker retries pg_try_advisory_lock internally,
but its returned error does not expose a stable acquired/not-acquired type for
safe reuse. Session end releases any possibly acquired lock.

Protected SessionUnlock reverses ownership: wrapped Goose unlock first, then
runtime_exit_migrator, then RESET SESSION AUTHORIZATION in local mode. Each step
uses a five-second context derived from context.WithoutCancel. Any ambiguity,
AM001, backend mismatch, or reset failure returns an error. Every migration
backend is physically retired after these cleanup attempts, including successful
migrations. Goose calls SessionUnlock before returning the migration or Commit
error and does not pass that error to the locker. Retiring every backend closes
the gap where a lost Commit response could otherwise return a reusable
connection before Apply learns of the error. The caller's sql.DB stays open. The
definer functions enforce session-marker deletion and an empty transaction
marker; Go never SELECTs their owner-only temporary tables. Administrative tests
verify those rows independently.

Responsibility is split deliberately. SQL runtime_exit_migrator does not infer
autocommit from transaction timestamps or xid allocation. It rejects an active
write marker or missing runtime session lock, clears its session marker, and
unlocks exactly once. Go calls it as one standalone statement only after Goose
migration transactions and wrapped Goose SessionUnlock complete, then verifies
PID, lock absence, identity and exposed catalog state on the same backend before
identity reset and retirement. Successful definer execution supplies the
private-row proof. Unknown execution outcomes destroy the backend, without
expanding marker grants or the metadata accessor.

Apply and Status support the pinned pgx stdlib driver. Verify exact
`*stdlib.Conn` through Raw before any identity, lock or Goose work in bootstrap
and protected Apply, and before Status session authorization or BEGIN.
Retirement uses sql.Conn.Raw to require that type, calls its exported
Conn().Close with a separate five-second cleanup context, and confirms IsClosed.
Never retain the driver handle outside Raw. Return nil from Raw after successful
physical close so Goose's following sql.Conn.Close does not turn a successful
migration into ErrConnDone. The dead wrapper cannot execute SQL or pass pgx
ResetSession. A close error is reported even when IsClosed confirms the socket
cannot be reused. If the Raw callback runs but IsClosed is false, return
driver.ErrBadConn so database/sql synchronously closes and discards the wrapper.
SessionUnlock still returns a cleanup failure and preserves the primary
migration error. If Raw cannot invoke the callback, report an invariant failure
and do not retry or claim confirmed backend death without proof. Unsupported
drivers fail before execution.

There is no reconnect or retry during a failed operation. Bootstrap and
protected stages use ApplyVersion once per exact embedded version, each on a new
locked backend that retires before the call returns. Protected versions recheck
runtime state and history under both locks. Every stage checks its recorded
backend PID. Return completed results with the first migration, Commit,
identity, lock or cleanup error. Tests prove physical closure precedes Apply's
return, success returns no artificial cleanup error, and the caller's DB remains
usable.

Only the separate
[adoption reconciliation](migration-provisioning.md#adoption-result-and-ambiguous-outcomes)
may open one read-only connection after confirmed retirement to prove
convergence.

Production Apply never calls Goose Up, UpByOne, UpTo, HasPending, GetDBVersion
or Status. Pinned UpTo calls HasPending, which initializes history without the
SessionLocker before UpTo enters its locked migration path. ApplyVersion enters
locked initialization directly. Never call Provider.Close on the caller's DB;
discard the provider after its connection cleanup instead.

## Fresh and staged Apply

Apply first performs only catalog existence and owner probes. It does not read
runtime_write_state, call the metadata accessor, or read Goose rows before the
appropriate session lock. Those catalog probes route the connection to a
bootstrap, protected, or adoption-required candidate path; the locked recheck
below makes the authoritative classification.

It then chooses exactly one state:

1. Fresh: runtime_write_state and goose_db_version both absent. Use a bootstrap
   session locker that establishes direct migrator or fixed local impersonation,
   acquires only Goose's session lock, rechecks both absences, and runs
   ApplyVersion for the exact embedded sequence 1..13. Each version establishes
   identity and locks before history creation or reads. Goose may create
   history/version zero inside version 1's locked lifecycle. Migration 00013 is
   the sole unprotected installer exception. If the recorded history owner is
   migrator, discard this provider and continue through a new protected
   provider; no manual adoption stop is required.
2. Pre-foundation: history exists with maximum version 0..12 and runtime state
   absent. Owner aboutme requires LocalAdmin identity and remains session_user
   aboutme while Goose-locked Apply advances only through 13. Owner migrator
   requires direct/fixed impersonated migrator. Validate the manifest prefix for
   that version. Direct identity against aboutme-owned history fails from the
   catalog owner probe without reading Goose rows. Close after 13; no protected
   migration runs as admin. Other identity/owner combinations fail before write.
3. Foundation exists: establish exact migrator identity, acquire the runtime
   barrier, then acquire the Goose lock. Only now call the metadata accessor and
   read Goose rows. Under both locks, enforcement 0 requires maximum version 13.
   History owner migrator proceeds to 00014; runtime_owner means fixed adoption
   already ran and also proceeds. Catalog owner aboutme routes to
   ErrHistoryAdoptionRequired before entry, but the adoption command must repeat
   the full classification under runtime then Goose locks before writing. Any
   other owner or locked mismatch is corruption.
4. Protected under the same two-lock recheck: enforcement version=1,
   migration_history_owner is aboutme_runtime_owner, version >=14, and the exact
   enabled trigger/privileges exist. Use the protected provider for all pending
   migrations.

After version 13, Apply never continues to 14 in the bootstrap provider. It
retires that connection and starts a new protected provider when owner is
migrator. Only an existing aboutme-owned database requires the fixed adoption
command and another Apply invocation. A bootstrap race rechecks after Goose lock
before choosing; protected entry takes runtime then Goose locks.

Bootstrap walks from source version 1 without an unlocked history read. An
already-applied candidate is consumed only after its locked lifecycle proves the
exact identity, owner and valid contiguous history prefix, with that version
applied once. Cleanup must also succeed. It produces no local MigrationResult.
Any joined cleanup error stops even if it also contains ErrAlreadyApplied.
Missing sources, gaps, duplicate or down history rows, unknown versions and
state mismatches fail before mutation. An empty bootstrap source set fails; an
already-valid version 13 routes directly to adoption or protected entry.

If a peer installs foundation during bootstrap, the Goose-locked catalog probe
may observe its existence without reading runtime metadata. The locker cleans
up, retires its backend, and returns a fresh private token bound to that locker
and PID. Only `errors.Unwrap(err)==token` with proved clean retirement permits
new protected classification. For peer-applied versions, require
`errors.Unwrap(err)==goose.ErrAlreadyApplied` plus the locked proof above.
Pinned Goose wraps each clean outcome once; joined cleanup errors change that
shape. Neither arbitrary errors nor an errors.Is match permit continuation.

Any other combination is ErrMigrationHistoryCorrupt: runtime without history,
history >=13 without matching runtime state, enforcement 0 at a version other
than 13, enforcement 1 below 14, unexpected owner/shape/trigger/grant, or an
unknown enforcement value. Apply never repairs these states implicitly.

## Migration 00014 and later framing

00014 is the first framed migration. Its first executable Up statement is
runtime_begin_migration_write('migration-00014'); this marks dirty
unconditionally. Its body:

1. Require no residual runtime_owner schema CREATE, then grant it temporarily in
   both fresh and adopted paths. If manifest source owner is migrator, transfer
   every fixed table/function owner-bearing object, allow the Goose identity
   sequence to follow its table dependency, validate dependent indexes/composite
   types/triggers and unchanged OIDs. If adoption already moved the complete
   manifest, require runtime_owner throughout. Keep the temporary schema grant
   through the following function DDL. Partial/mixed owner state is corruption.
2. SET ROLE aboutme_runtime_owner.
3. Create the exact owner SECURITY DEFINER BEFORE INSERT ROW assertion trigger.
4. Revoke PUBLIC and all runtime-role history privileges. Grant direct
   aboutme_migrator SELECT and INSERT only.
5. Update runtime_write_state atomically to migrator_enforcement_version=1 and
   migration_history_owner='aboutme_runtime_owner'.
6. RESET ROLE, revoke the original migrator's child schema CREATE grant to
   runtime_owner, and prove no residual CREATE.
7. Call runtime_finish_write as the last executable migration-body statement.

Pinned Goose then executes in the same xid:
`INSERT INTO public.goose_db_version(version_id,is_applied) VALUES (14,true)`.
The SECURITY DEFINER trigger has current_user runtime_owner. It requires exact
migrator session_user plus the bound backend PID, xid, finished migrator marker,
canonical operation ID `migration-%05d` parsed as N>=14, NEW.version_id=N, and
NEW.is_applied=true. It permits no other INSERT. UPDATE/DELETE/TRUNCATE remain
ungranted and have denial triggers where statement triggering is applicable.
Goose commits only after that INSERT. Down is inert repository cargo and removes
no protection in deployed operation.

Ownership convergence preserves non-owner effective privileges on legacy
objects. Compare ACLs after normalizing only PostgreSQL's expected old/new owner
and grantor OID rewrite; byte-for-byte ACL equality is not required. R8 alone
changes the planned legacy grants/triggers. After Goose ownership changes, only
migrator receives the explicit direct SELECT/INSERT history grants above.

Every N>=14 transactional Up uses canonical operation ID `migration-%05d` with
N's zero-padded decimal version in the corresponding first begin call and uses
finish last. The history trigger parses and bounds that canonical ID, then
requires NEW.version_id=N; it never hardcodes 14. NO TRANSACTION and Go
migrations are forbidden by a static embedded-FS check. Any Goose upgrade must
re-prove provider initialization, session connection identity, run order,
version SQL, and cleanup behavior.

## Read-only status and check

Status never constructs a Goose Provider. It uses one pinned sql.Conn and fixed
`BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY`, COMMIT and ROLLBACK
statements. It takes no advisory lock:

- no runtime state and no history: return typed ErrMigrationHistoryMissing;
  migrate -check prints fixed uninitialized/pending output and exits nonzero;
- no runtime state plus valid pre-13 history: read history directly with the
  pinned SELECT shape and report applied/pending sources without Provider;
- enforcement 0: require valid state, owner recorded by 00013, exact history
  shape, no claim that the 00014 trigger exists, and maximum version exactly 13;
- enforcement 1: require runtime_owner, exact enabled trigger, direct-role
  grants, and maximum version at least 14;
- all missing/malformed/mixed states: ErrMigrationHistoryCorrupt.

To close the catalog race, precheck reads owner, columns, triggers, ACL and
history from one REPEATABLE READ, READ ONLY snapshot. It never calls
ensureVersionTable. A concurrent committed migration is reported from either
snapshot boundary on the next invocation; it cannot yield a mixed catalog view.
Status builds MigrationStatus values from embedded sources plus direct history
rows. PendingCount remains a pure slice count. Check never creates
history/version zero.

Status acquires one dedicated sql.Conn. Direct mode verifies exact session_user.
Local mode verifies authenticated session_user aboutme and superuser. It first
uses catalog-only probes to select one fixed read identity before opening the
snapshot: history owned by aboutme stays authenticated aboutme for valid
pre-foundation or inert version-13 status. That read-only snapshot directly
reads only the bounded runtime metadata columns and revalidates absent runtime
with pre-13 history, or enforcement 0/head 13/recorded owner aboutme. Protected
state or any different owner fails without an identity switch or retry. Direct
Status rejects aboutme-owned history from its catalog probe. History owned by
migrator/runtime_owner uses fixed migrator identity. No identity changes inside
the read transaction. It records backend PID, begins REPEATABLE READ READ ONLY,
reads one snapshot, and commits. It verifies the same PID; impersonated local
mode then RESET SESSION AUTHORIZATION before release, while direct mode
re-verifies session_user remains migrator. Query/cancellation failure gets a
five-second independent ROLLBACK through the same Conn. Do not use sql.Tx here:
its Rollback has no context and pgx stdlib retains the canceled BeginTx context.
Cleanup is synchronous; never return the lease while rollback is pending. A
read-only COMMIT error, reset failure, rollback failure, PID mismatch or cleanup
ambiguity retires the physical backend. Confirmed commit or rollback plus
identity reset allows clean Status reuse. Status never retries or changes
backend.
