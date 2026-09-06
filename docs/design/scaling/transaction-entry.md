# Write transaction entry

Status: Accepted under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). This is
the target contract. Local implementation and hosted proof remain Phase 10
gates.

## Invariant

No production application, maintenance, lifecycle, proof, migration, fixture, or
recovery path may lock an application row or mutate a tracked table before it
enters the runtime write barrier. A table trigger is too late to acquire the
barrier after SELECT FOR UPDATE. Triggers assert prior entry and reject missing
entry immediately; they never acquire or wait for the barrier.

## Exported Go seam

Add `WriteTxRunner` under `apps/server/internal/store` with two methods:

```go
WithWriteTx(context.Context, pgx.TxOptions, func(*Queries) error) error
ExecWrite(context.Context, func(*Queries) error) error
```

`NewWriteTxRunner(*pgxpool.Pool) WriteTxRunner` constructs the runner. ExecWrite
uses default transaction options for a former autocommit mutation. The concrete
runner, raw begin and transaction are private. Callbacks receive only
transaction-bound Queries, with no Commit, Rollback, Begin, Conn or finish
method. A savepoint helper, if needed, must keep its nested transaction private.

The runner acquires a pool connection, begins a transaction, and immediately
calls SECURITY DEFINER `runtime_enter_write()` before its callback. No query,
hook, metric or SELECT FOR UPDATE may intervene. Entry:

1. takes `pg_advisory_xact_lock_shared(runtime-write-barrier-key)`;
2. reads runtime_write_state only after the shared advisory lock, without a row
   lock that would serialize ordinary writers;
3. rejects write_gate closing/closed with the existing unavailable mapping; and
4. creates or validates an owner-only temporary marker bound to backend PID,
   transaction ID and session-role OID, then records entry.

On a nil callback error the private commit method calls runtime_finish_write,
then Commit without an intervening query or hook. Finish advances generation
once for dirty writes and marks the transaction finished. Triggers reject later
tracked DML. A deferred constraint trigger only asserts finish; it never changes
generation. PostgreSQL permits callers to force
[deferred constraint triggers early](https://www.postgresql.org/docs/18/sql-set-constraints.html),
so a deferred generation update cannot enforce this ordering.

Callback error or panic uses a five-second cancellation-independent rollback;
panic is then rethrown. Ordinary unavailable SQLSTATE 55000 permits connection
reuse only after confirmed rollback. Project SQLSTATE AM001 means marker or
backend contamination: rollback, remove the connection from the pool with
Hijack, and close the physical connection. Ambiguous entry/finish, unknown
cleanup and every commit error also destroy the backend. Cleanup is bounded. No
path retries internally. Wrapped errors preserve driver causes so the operation
owner can resolve an ambiguous commit through a fresh read connection under its
existing idempotency rules.

The supported write API exposes no raw transaction. A structural production
write-path inventory also rejects direct Conn, Begin, Commit, Rollback and
runtime_finish_write use outside this wrapper and the separately controlled
migrator. Proved read-only exports, LISTEN and recovery connections remain
outside the write API. This prevents accidental escape through driver
interfaces; it does not claim language-level containment of malicious compiled
Go.

## Transaction marker

`pg_temp.runtime_write_entry_v1` is created by runtime_owner using fixed static
PL/pgSQL DDL, with ON COMMIT DELETE ROWS and a deferred AFTER INSERT constraint
trigger. Before reuse, entry verifies its owner, temporary namespace and
persistence, exact columns, constraints and trigger. A lookalike, unexpected row
or mismatched marker raises AM001 and destroys the backend; it is never adopted,
repaired or dropped by the definer function.

The row carries xid8 transaction ID, backend PID, session-role OID, writer kind,
entry generation, dirty and accepted-write flags, bounded operation ID, and
finished state. Exact entry before finish is idempotent; entry after finish is
contamination. Exact repeated finish cannot advance generation again.

Finish also takes a transaction advisory lock in the reserved two-int namespace
`295306100`, with `pg_backend_pid()` as the second key. The namespace is the
first four big-endian SHA-256 bytes of `aboutme.runtime-write-finish.v1`; it is
distinct from media cleanup's `0x61626d65`. No other operation uses this
namespace. Finish uses `pg_try_advisory_xact_lock` before the final state-row
lock; an occupied key fails AM001 rather than waiting. Entry and migration begin
reject a guard already held by this backend. An unfinished marker with a held
guard is contamination; repeated finish requires a valid finished marker and its
guard.

The guard survives `DISCARD TEMP`, which can remove even an owner-created marker
after its deferred trigger has fired. Pending trigger events prevent discard
before finish. The guard prevents discard after finish from allowing re-entry in
the same transaction. Commit, rollback and savepoint rollback release the
corresponding transaction lock with the marker/state changes.

Forced constraint checking before finish fails. Savepoint recovery may catch
that error, but cannot remove the outer transaction's finish requirement.
Rollback of dirty work or finish restores the marker and generation together.
Successful commit clears marker rows; a pooled backend retains only an empty,
owner-validated table. No login receives direct marker privileges.

## Assertion trigger

`runtime_assert_write_entry()` is a definer-owned BEFORE INSERT/UPDATE/DELETE
statement trigger on every tracked mutable table. It checks both the unfinished
marker matching backend/transaction/session identity and a granted ShareLock for
this backend/key in pg_catalog.pg_locks. It does not call either advisory-lock
function and does not wait. Missing/mismatched marker, missing lock, or closed
gate raises the fixed SQLSTATE described above. A concurrent writer may advance
generation after entry; that does not invalidate the marker because the held
shared lock already prevents gate closure. Direct autocommit DML fails before
changing a row.

The assertion marks the transaction dirty. Business-table assertions also mark
accepted application writes using owner-controlled table/operation
classification. Bookkeeping and maintenance do not extend the application writer
tail. No callable manual acceptance marker exists. The receipt finalizer uses
the exclusive sibling lock and its named definer function; it does not use the
ordinary runner.

## Inner lock order

The shared advisory lock is the common outer lock. Once the callback starts,
existing relative orders remain unchanged:

- ADR 0022: public_state, discovery, then affected resume UUID byte order.
- ADR 0016/resume: user, idempotency record, usage, then operation rows.
- Account deletion: slug, public_state, canonical email, registration, user,
  ordered resumes, current session.
- OAuth: client/user/grant/code/token order already encoded by each flow.
- Auth mail: exact registration/reset/user scope, leased job, then outcome.
- Maintenance: command advisory session lock, then its documented page/row
  order. The command takes runtime shared entry before the command advisory
  lock.

[Maintenance sessions](maintenance-entry.md) bind that outer barrier, fixed
command lock and every write transaction to one leased backend, including idle
gaps. Auth mail uses the ordinary runner and preserves its scope-before-job
order.

No callback explicitly locks runtime_write_state. The runner's finish update is
last. Finalize-stop takes the exclusive advisory lock, then runtime_write_state
and the documented source rows. A writer either enters first and finishes before
the finalizer gets exclusive, or waits before holding any application row. This
removes the review's deadlock cycle.

## Legacy direct transaction entry points to migrate

Production pgxpool Begin/BeginTx:

- privacyretention/worker.go:167,302
- accountapi/export.go:177 (read-only SERIALIZABLE export; declare read-only and
  keep outside WriteTx unless its callback gains DML)
- oauthsrv/session_http.go:280
- oauthsrv/token_endpoint.go:195,283
- oauthsrv/revoke.go:53
- oauthsrv/consent.go:100,161,239
- oauthsrv/clients_gc.go:20
- resume/idempotency.go:188
- authmail/worker.go:173,224,243,282

Production pgx.BeginFunc:

- resume/store.go:67,123,151
- auth/provider_identity.go:129,156
- auth/password_service.go:230,310,374,512,602,696,782,849
- auth/session.go:142,249

Command database/sql BeginTx:

- cmd/mcp-uat-fixture/fixture.go:149,178
- cmd/dev-seed/seed.go:215

All line numbers are from base 0c6612b and are navigation evidence. Authors must
rerun the structural search after edits; generated code and tests do not count
as transaction-entry owners.

## Autocommit mutation policy and current seams

Mutable hosted roles have no allowed autocommit DML. Convert each
single-statement case to ExecWrite. Multi-statement and lock-first cases use
WithWriteTx. Reads, LISTEN, readiness probes, and pure export transactions
remain outside.

Current pool-backed or direct autocommit mutation seams requiring conversion or
an explicit read-only proof are:

- auth/transaction.go CreateOAuthTransaction; auth/start.go expired OAuth
  cleanup; user/user.go CreateUser.
- auth/session.go RevokeSession, RevokeSessionForUser, RevokeAllSessions,
  TouchLastSeenAt, TouchReauthenticatedAt, and rotation-grace delivery paths
  when invoked outside its existing transaction callbacks.
- oauthsrv/clients.go CreateOAuthClient; mcpapi/bearer.go
  TouchOAuthTokenLastUsed.
- resume/backfill.go BackfillResumeDocumentCAS and resume/idempotency.go expired
  record/usage cleanup paths when q is pool-backed.
- authmail/worker.go expired-lease requeue and password/mail cleanup calls made
  before its explicit transaction; all claim/finalize transactions migrate at
  entry even when their DML already occurs inside a transaction.
- mediacleanup/deletion.go and reconcile.go pool-backed claim, overdue,
  complete, requeue, orphan-create, and cursor writes. External S3 calls remain
  outside a database transaction, but every database phase uses ExecWrite and
  preserves the existing claim/ambiguity protocol.
- resumeapi/accountapi recovery pools are read-only today. Any recovery DML must
  use ExecWrite; structural tests keep these pools read-only.
- cmd/password-auth-fixture, cmd/p5a-native-fixture, cmd/dev-seed, and
  cmd/mcp-uat-fixture direct ExecContext DML. Development/test fixture roles use
  the same entry helper or a database/sql equivalent; there is no trigger
  bypass.

Generated sqlc methods are not independent entry points because `Queries` can
wrap either pool or transaction. Constructors exposed to service code must stop
providing a pool-backed mutating Queries value. Split read queries from write
callbacks or enforce by package-private construction. A static test extracts
mutating names from `sql/*.sql`, finds non-test callers, and rejects calls whose
receiver is not transaction-bound.

## Migration policy

The [dedicated migrator](migrator.md) fixes the runner API and staged history
validation. [Provisioning and adoption](migration-provisioning.md) fixes the
exact ownership transition for fresh and existing databases.

The migrator uses one dedicated backend for runtime_enter_migrator, Goose's
session lock, all migrations and runtime_exit_migrator. Entry takes the session
shared barrier before Goose or table reads, checks the gate and creates an
owner-validated temporary session marker with ON COMMIT PRESERVE ROWS. Exit
clears it and releases exactly one lock. Entry exceptions unlock; ambiguity or
marker mismatch closes the physical backend. No reconnect may continue a run.
The pinned-connection runner calls exit as a standalone autocommit statement
after migration transactions end. SQL rejects an active write marker or missing
session lock; it does not infer autocommit from timestamps or transaction IDs.

Each transactional Up after migration 00013 starts with
runtime_begin_migration_write and ends with runtime_finish_write. Begin marks
dirty unconditionally so pure DDL advances generation once. The operation ID
binds the migration version. NO TRANSACTION is forbidden. The pinned Goose
v3.27.3 Provider executes the Up body, inserts its version, then commits that
same transaction. The version table is the sole post-finish bookkeeping case:
runtime_owner owns the table and an assertion trigger permits only migrator
INSERT with matching backend/xid, finished marker, exact version and
is_applied=true. App and runtime roles get no DML. In its direct login state,
the migrator gets required reads and exact INSERT, with no UPDATE, DELETE,
TRUNCATE or TRIGGER grant. The initial dirty mark covers this write. Tests pin
ordering, statement shape and transaction identity across upgrades.

Reviewed migration code may SET ROLE runtime_owner to alter its objects. These
direct-role restrictions do not sandbox malicious migration SQL. Source review,
framing, the dedicated backend and session barrier govern that trusted DDL path.

Goose may initialize its version table even through a lock-free provider.
Status/PendingCount/check must first validate history through read-only catalog
queries, including the expected owner and enabled assertion trigger for the
installed enforcement version. Foundation state records
migrator_enforcement_version=0 and the original migration_history_owner; it
requires that owner and shape without the later trigger. The proved migrator
enforcement migration records version 1 and runtime_owner atomically with its
ownership, grants and trigger. Version 1 alone does not authorize final stop.
Missing history and absent runtime state report an uninitialized database
without creating a provider or version-zero row. Runtime state with missing or
malformed history is corruption; apply/check/status fail before any repair.
Apply alone may initialize a fresh database before migration 00013.

Before foundation installation, database provisioning grants migrator CONNECT,
TEMPORARY and CREATE on the target database, schema USAGE and CREATE WITH GRANT
OPTION, and runtime_owner database TEMPORARY. Migrator grants runtime_owner
schema CREATE only within an object-transfer transaction and revokes it before
commit. A bounded read-only metadata function exposes gate, generation,
enforcement version and history owner to migrator without direct state-table
access. Existing prebaseline local/test history owned by aboutme uses a fixed,
data-preserving adoption under the runtime barrier; fresh hosted databases use
the direct migrator path.

Migration 00013 installs the barrier and migrator primitives as the sole
schema-installer exception. No migration 00014 lands until dedicated-backend
entry, transaction framing and version bookkeeping are proved. `begin_wake`
opens only migration admission while app/maintenance stay closed. Cluster role
creation stays outside Goose. Final-stop authority remains absent until all
caller and table coverage is complete.

## Exact caller migration strategy

1. Add runtime_enter_write/runtime_finish_write, assertion triggers,
   WriteTxRunner, and database/sql migrator equivalent with failing tests.
2. Inject WriteTxRunner beside read pools. Migrate direct Begin/BeginFunc sites
   package by package without changing their inner query order or error mapping.
3. Convert listed autocommit DML to ExecWrite. Remove service access to
   pool-backed mutating Queries.
4. Add a generated mutator-call inventory gate and a database privilege test
   that direct DML fails with missing entry.
5. Only then enable finalize-stop receipts. Until the inventory is empty and
   adversarial tests pass, no-op suppression remains disabled.

## Failing-first cases and commands NOT RUN

- OAuth/account/auth-mail transaction holds its first SELECT FOR UPDATE row,
  then finalizer requests exclusive: writer completes without deadlock and
  finalizer waits outside all rows.
- Finalizer holds exclusive before writer entry: the runner waits or returns its
  bounded unavailable result before writer locks any application row.
- Direct pool sqlc mutator, raw autocommit Exec, marker without advisory lock,
  advisory lock without marker, marker from another transaction, and closed gate
  all fail before row change.
- Commit/rollback clears marker and xact lock; reused pooled connection cannot
  inherit authority. Existing ambiguous commit resolution remains unchanged.
- Forced constraints, savepoint rollback, finish failure, lookalike marker,
  poisoned-backend discard, pure DDL and exact Goose bookkeeping are proved
  independently. The temporary-marker feasibility probe is not this proof.

Read-only rg transaction-entry/caller searches produced the inventories above.
Commands NOT RUN: targeted `go test -race -count=1` for store, OAuth,
accountapi, authmail, resume, mediacleanup, privacyretention, fixtures, and
migration; make sqlc-check server-test-db server-test-integration
server-migration-test. Root owns make ci and make scan.
