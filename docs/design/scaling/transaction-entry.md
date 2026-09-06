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

Add under `apps/server/internal/store`:

- `type WriteTxStarter interface { BeginWrite(context.Context, pgx.TxOptions) (pgx.Tx, error) }`
- `NewWriteTxStarter(*pgxpool.Pool) WriteTxStarter`
- `WithWriteTx(ctx, starter, options, func(*store.Queries) error) error`
- `ExecWrite(ctx, starter, func(*store.Queries) error) error` for a mutation
  that was formerly one autocommit statement.

BeginWrite calls pool.BeginTx, then immediately calls the SECURITY DEFINER
`runtime_enter_write()` on that transaction before returning it. No callback,
query, hook, metric query, or SELECT FOR UPDATE may run between BeginTx and
runtime_enter_write. Entry:

1. takes `pg_advisory_xact_lock_shared(runtime-write-barrier-key)`;
2. reads runtime_write_state only after the shared advisory lock, without a row
   lock that would serialize ordinary writers;
3. rejects write_gate closing/closed with the existing unavailable mapping; and
4. sets a transaction-local marker bound to the current backend transaction ID.

If entry fails, BeginWrite rolls back with the existing bounded independent
cleanup and returns no transaction. WithWriteTx commits once on nil callback
error and otherwise rolls back. It does not retry an ambiguous commit. Existing
operation-specific ambiguous-resolution and idempotency code remains the owner.

## Assertion trigger

`runtime_assert_write_entry()` is a definer-owned BEFORE INSERT/UPDATE/DELETE
statement trigger on every tracked mutable table. It checks both the local
marker matching the backend transaction ID and a granted ShareLock for this
backend/key in pg_catalog.pg_locks. It does not call either advisory-lock
function and does not wait. Missing/mismatched marker, missing lock, or closed
gate raises one fixed SQLSTATE mapped to unavailable. A concurrent writer may
advance generation after entry; that does not invalidate the marker because the
held shared lock already prevents gate closure. Direct autocommit DML fails
before changing a row.

The deferred write-state trigger remains the last database action. It advances
generation once per transaction near commit. It never establishes entry. The
receipt finalizer uses the exclusive sibling lock and its named definer
function; it is not routed through BeginWrite.

## Inner lock order

The shared advisory lock is the common outer lock. Once BeginWrite returns,
existing relative orders remain unchanged:

- ADR 0022: public_state, discovery, then affected resume UUID byte order.
- ADR 0016/resume: user, idempotency record, usage, then operation rows.
- Account deletion: slug, public_state, canonical email, registration, user,
  ordered resumes, current session.
- OAuth: client/user/grant/code/token order already encoded by each flow.
- Auth mail: leased job, exact registration/reset/user scope, then outcome.
- Maintenance: command advisory session lock, then its documented page/row
  order. The command takes runtime shared entry before the command advisory
  lock.

No ordinary writer explicitly locks runtime_write_state later. Its deferred
generation update is last. Finalize-stop takes the exclusive advisory lock, then
runtime_write_state and the documented source rows. A writer either enters first
and finishes before the finalizer gets exclusive, or waits before holding any
application row. This removes the review's deadlock cycle.

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

Goose cannot issue ordinary DML through an unentered pooled connection. The
migrator calls a separate runtime_enter_migrator on the same dedicated
database/sql connection before goose checks/applies migrations. It acquires a
session shared barrier, checks the gate, and sets a session marker accepted only
for the configured migrator role/backend. Goose then takes its existing session
advisory migration lock and runs its internal transactions. The dedicated
connection is always unlocked and closed, so no pool borrower inherits the
marker. `begin_wake` opens only the migration phase, with app/maintenance
admission still closed. Migration bootstrap that creates runtime_write_state is
a versioned one-time schema-installer exception; later migrations have no
bypass. Cluster CREATE ROLE/DROP ROLE stays outside goose as specified in
runtime-coordination.md.

## Exact caller migration strategy

1. Add runtime_enter_write, assertion/deferred triggers, WriteTxStarter, and
   database/sql migrator equivalent with failing tests.
2. Inject WriteTxStarter beside read pools. Migrate direct Begin/BeginFunc sites
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
- Finalizer holds exclusive before writer entry: BeginWrite waits or returns its
  bounded unavailable result before writer locks any application row.
- Direct pool sqlc mutator, raw autocommit Exec, marker without advisory lock,
  advisory lock without marker, marker from another transaction, and closed gate
  all fail before row change.
- Commit/rollback clears marker and xact lock; reused pooled connection cannot
  inherit authority. Existing ambiguous commit resolution remains unchanged.

Read-only rg transaction-entry/caller searches produced the inventories above.
Commands NOT RUN: targeted `go test -race -count=1` for store, OAuth,
accountapi, authmail, resume, mediacleanup, privacyretention, fixtures, and
migration; make sqlc-check server-test-db server-test-integration
server-migration-test. Root owns make ci and make scan.
