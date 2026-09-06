# R1.2 - Write barrier foundation

Status: Accepted for bounded implementation after the PostgreSQL 18.4
temporary-marker probe and the same fresh phase review. Runtime and hosted proof
remain pending.
[Transaction entry](../../../design/scaling/transaction-entry.md),
[runtime privileges](../../../design/scaling/runtime-schema.md), and
[UAT lifecycle](../../../design/scaling/uat-lifecycle.md) own the contract.

## Scope and order

R1.1 role bootstrap is committed as `132447e`. The old unused Go helper at
`986fe44` must be replaced before any caller adopts it.

1. B1 installs inert SQL primitives and proves them through real roles.
2. B2 implements the private Go runner and proves its lifecycle against B1.
3. B3 composes the dedicated migrator and protected Goose bookkeeping before
   another runtime migration lands. Root settles its fixture identities and
   enforcement version before dispatch.

No slice exposes a gate-closing or final-stop function, enables multiple serving
replicas, or attaches a write assertion to a legacy application table. Local
application credentials continue to work. Final enforcement waits for R8.

## B1 ownership and schema

Root explicitly delegates only these serialized paths to one implementation
author who did not design this slice:

- `apps/server/migrations/00013_runtime_write_foundation.sql`
- `apps/server/migrations/runtime_write_foundation_test.go`
- `apps/server/migrations/runtime_write_foundation_helpers_test.go`

Root owns shared migration harnesses, generated files, manifests and Git. B1
does not change a cluster role, credential, database owner or legacy table
grant. It uses the existing one shared database container and disposable test
databases through the established migration helper.

Create public.runtime_write_state with the fields and constraints in the UAT
lifecycle contract. Seed generation 1, gate open, writer kind bootstrap,
operation migration-00013 and one transaction timestamp for all timestamps.
Bound operation IDs to 1–128 octets. Writer kind permits bootstrap, app,
maintenance, lifecycle, proof and migrator. Also seed
migrator_enforcement_version smallint NOT NULL CHECK IN (0,1) to 0. Set
migration_history_owner text NOT NULL, bounded to 1–63 octets, to the existing
public.goose_db_version owner name read from pg_class. These fields record
installed migrator enforcement, not a configurable bypass or final legacy
coverage. B3 changes them atomically with its table protection.

The runtime barrier key is `3727108639518528074`: the first eight big-endian
bytes of SHA-256 over `aboutme.runtime-write-barrier.v1`, interpreted as signed
int64. SQL and tests use this value; tests recompute it. Goose and cluster-role
bootstrap keep their distinct keys.

No persistent marker table is created. Static PL/pgSQL DDL creates
pg_temp.runtime_write_entry_v1 with ON COMMIT DELETE ROWS. Its exact columns are
transaction_id xid8 primary key, backend_pid integer, session_role_oid oid,
writer_kind text, entry_generation bigint, dirty boolean, accepted_write
boolean, operation_id text nullable, and finished boolean. All except
operation_id are NOT NULL; generation is positive and the three flags default
false. Operation ID, when present, uses the same octet bound. Writer kinds
exclude bootstrap. Each backend has at most its current transaction's row.

The owner-created migrator session marker is pg_temp.runtime_migrator_session_v1
with ON COMMIT PRESERVE ROWS. Its singleton row records backend PID,
session-role OID, entry generation and entry time. Validate both marker
relations' owner, temporary namespace, persistence, exact columns, constraints
and trigger state before reuse. A name/type/index collision or lookalike is
AM001; never adopt or repair it.

## B1 function contract

Every function is owned by aboutme_runtime_owner, SECURITY DEFINER, with
search_path pg_catalog and qualified permanent references. No dynamic SQL is
used in a definer function. Helpers are permitted only for shared validation;
they receive no login EXECUTE grant.

- runtime_enter_write returns generation bigint and writer_kind text. It
  acquires the shared transaction barrier before state reads, checks gate open,
  infers kind from the exact app/maintenance/lifecycle-command/fencing-proof
  session_user, and records a bound marker. Exact entry before finish is
  idempotent; entry after finish is AM001.
- runtime_assert_write_entry is a BEFORE STATEMENT trigger. It checks the
  unfinished marker and the backend's granted ShareLock in pg_locks for the
  current database and bigint key: classid 867785103, objid 2177536586,
  objsubid 1. It checks gate open and marks dirty. It never acquires a lock or
  updates runtime_write_state.
- runtime_assert_business_write has the same assertion. For an app session it
  also marks accepted_write and derives operation_id from fixed owner-supplied
  trigger arguments plus TG_OP. It exposes no manual acceptance function.
- runtime_finish_write validates entry, updates state once when dirty using one
  clock_timestamp value, and sets finished. Clean finish changes no generation;
  exact repeated finish is idempotent. Rollback changes no durable generation.
- runtime_assert_write_finished is the DEFERRABLE INITIALLY DEFERRED AFTER
  INSERT constraint trigger on the marker. It only checks the current marker's
  finished state. Forced constraints before finish fail; after finish they
  validate without deleting the marker. Savepoint recovery cannot bypass finish.
- runtime_enter_migrator checks the exact migrator session, acquires the shared
  session barrier before Goose, checks gate open and records its session marker.
  Repeated entry must not accumulate lock counts. Any entry failure releases its
  newly acquired session lock.
- runtime_begin_migration_write(operation_id text) requires that session marker
  and lock, creates the current transaction marker and marks dirty
  unconditionally. Its operation ID binds the migration version.
- runtime_exit_migrator requires the exact session marker in autocommit, clears
  it and releases exactly one session lock. Missing lock or ambiguous marker is
  contamination; the caller closes the dedicated backend.

Ordinary unavailable uses SQLSTATE 55000. Marker/identity/shape mismatch,
missing lock or re-entry after finish uses project SQLSTATE AM001. Do not use
PostgreSQL's P0004, which means assert_failure. Trigger errors change no rows.

## B1 grants and intermediate behavior

Runtime owner owns state and functions. If owner transfer needs CREATE on
public, grant it only within the migration transaction and revoke before commit.
Give runtime_owner the database TEMPORARY privilege needed to create markers;
leave existing database privileges and owners unchanged.

Revoke PUBLIC table/function privileges. No login gets state DML. Grant
enter/finish to app, maintenance, lifecycle-command and fencing-proof. Grant
migrator entry/begin/finish/exit only to migrator. Restore gets no write
function. Assertion helpers get no login EXECUTE.

B1 attaches only the temporary-marker constraint trigger, not a legacy or
Goose-table trigger. It leaves Goose ownership and grants unchanged. B3 installs
Goose protection at its explicit enforcement version; checks must not require
that protection from a database containing only inert B1.

## Failing-first proof

B1 tests use the migration package's disposable database helper and dedicated
connections with SET SESSION AUTHORIZATION for each fixed role. Always reset
session state or physically close the connection, including fatal paths. Tests
may create a probe table in that disposable database, owned by runtime_owner,
grant bounded test access and attach the real assertion functions. No probe
table ships in migration SQL.

- Catalog proof covers exact owner, grants, search paths, marker shape, trigger
  function/mode, SQLSTATEs and absence of stop authority.
- Direct DML without entry, lock-only, wrong/stale marker, closed gate and every
  marker lookalike fail before row changes. Valid marker SELECT/DML/TRUNCATE,
  DROP/ALTER/trigger-disable/GRANT attempts by app grant no authority.
- Entry before DML succeeds. Many dirty statements advance once; clean entry
  does not advance; rollback advances zero. Only business writes by app extend
  last_accepted_writer_at. Concurrent writers finish without losing increments.
- Forced constraints before/after finish, constraints already immediate,
  repeated finish, savepoint rollback of dirty work/finish and pooled-backend
  reuse preserve the transaction boundary.
- Separate connections prove an exclusive holder blocks entry before any probe
  row lock; an entered writer finishes before exclusive acquisition. A trigger
  never acquires the barrier after a row lock.
- Migrator session entry/exit does not leak lock counts. Pure DDL and DDL plus
  DML advance exactly once; rollback advances zero. Other roles cannot call
  migrator functions. Lost session lock or mismatched marker fails.

Author command under apps/server, with TEST_DATABASE_URL set to the public local
test DSN and REQUIRE_TEST_DB=1:

```sh
go test -race -count=1 ./migrations -run RuntimeWrite
```

Root inspects SQL/tests and reruns that command. Root then runs make
server-migration-test, server-test-db, server-test-integration and sqlc-check,
regenerating and committing only actual generated changes. B2 owns physical
pool-discard, callback and commit-ambiguity tests. B3 owns same-backend Goose
ordering, direct-role grants and read-only missing-history checks. Those proofs
do not pass merely because B1 passes.
