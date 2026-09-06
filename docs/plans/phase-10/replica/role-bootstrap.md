# R1.1 - Cluster role bootstrap

Status: Implemented and checked locally. Phase review and integration gates
remain pending.
[ADR 0035](../../../adr/0035-replica-coordination-and-uat-lifecycle.md) and
[runtime privileges](../../../design/scaling/runtime-schema.md) own the final
boundary. This slice creates no database schema or stop authority.

## Role mapping

- `aboutme_runtime_owner`: NOLOGIN, NOSUPERUSER, NOCREATEDB, NOCREATEROLE,
  NOREPLICATION, NOBYPASSRLS, NOINHERIT. It owns new runtime tables, indexes,
  triggers, and SECURITY DEFINER functions. It is never a member of the app
  role, and the app is never its member.
- `aboutme_migrator`: existing hosted LOGIN role. Retain its planned schema-DDL
  scope. Bootstrap grants it membership in aboutme_runtime_owner with SET
  permitted and inherited privileges disabled. This lets migration SQL create or
  alter a runtime-owner object without making runtime_owner a login or giving
  the app ownership. It gets no lifecycle/proof runtime execution.
- `aboutme_app`: existing hosted LOGIN role. Do not replace or broaden its
  existing application-table grants in this slice. Add EXECUTE only on
  runtime_enter_write and runtime_finish_write; no DML on foundation tables.
  Accepted-write marking is owner-triggered and exposes no callable app
  function.
- `aboutme_restore_verify`: existing hosted LOGIN role. Retain read-only restore
  verification scope. It gets no write-entry or runtime table privilege.
- `aboutme_lifecycle_command`, `aboutme_fencing_proof`, and
  `aboutme_maintenance`: distinct LOGIN, NOSUPERUSER, NOCREATEDB, NOCREATEROLE,
  NOREPLICATION, NOBYPASSRLS, NOINHERIT roles. Foundation grants
  runtime_enter_write/runtime_finish_write to each, but no table DML and no
  other role membership. Later migrations grant only their accepted named
  functions/table columns.

All six LOGIN roles are explicitly NOSUPERUSER, NOCREATEDB, NOCREATEROLE,
NOREPLICATION, NOBYPASSRLS. aboutme_app and aboutme_restore_verify have no role
membership. aboutme_migrator has only the runtime-owner SET membership above.
Connection limits are not privilege drift; bootstrap preserves them.

This foundation creates LOGIN roles without setting passwords. It reads,
initializes, or rotates no role password. Existing password and
credential-expiry state are outside privilege verification and remain unchanged.
Hosted credential initialization is a later, explicit runtime-bootstrap
operation using distinct SSM Parameter Store standard SecureString values under
the retained KMS key. No Secrets Manager substitution is introduced.

Root-selected source seam: `apps/server/internal/dbroles/**` plus
`apps/server/cmd/db-role-bootstrap/**`. The command accepts no password input.
It uses a dedicated `CLUSTER_BOOTSTRAP_DATABASE_URL` whose database is exactly
`postgres`, with the existing admin authentication mechanism. It reports only
role-name/outcome enums and never prints the URL.

## Provisioning

Implement one idempotent fixed-name bootstrap command; root owns its wiring. It
connects to the common `postgres` maintenance database as the explicit cluster
bootstrap/admin identity. An empty setup creates all seven fixed roles with
exact attributes and the one migrator-to-owner membership. A partial setup fails
without mutation. For an existing role or membership it verifies every required
attribute and fails closed on any drift. Routine ensure never ALTERs an existing
role, repairs membership, sets a password, or drops a role. Fixed role names are
compiled constants.

The cluster bootstrap identity is the current `aboutme` image-initialization
role in local/CI and the RDS master/bootstrap credential in hosted UAT. It is
not an application role in the final hosted topology. The current local/test
server may continue using `aboutme` during this foundation slice. Thus role
creation is additive and does not revoke or change current aboutme/aboutme_dev
behavior.

Cluster role setup must precede any database applying migration 00013:

- Makefile `test-db-up` provisions roles through database `postgres` after
  PostgreSQL readiness and before migrating aboutme_dev. Reuse runs it
  idempotently too.
- CI PostgreSQL setup runs the same command before any make database target.
- Compose adds a one-shot role-bootstrap service after postgres health and makes
  migrate depend on it. It runs on existing volumes as well as fresh init, so it
  is not only a docker-entrypoint init script.
- scripts/dev-native.sh, dev-https.sh, and p5a-native-http-capture.sh already
  require make test-db-up before migration or fixture creation. That
  prerequisite runs the shared bootstrap; these scripts need no duplicate
  implementation. An overridden native database on another cluster requires
  standalone make db-role-bootstrap with that cluster's explicit postgres admin
  connection before native startup.
- Test temporary-database helpers bootstrap cluster roles through a separate
  admin connection to `postgres` before CREATE DATABASE. They never use the
  temporary/target database for this lock. Migration 00013 in each new database
  then creates its local objects/grants.
- internal/testutil MigrateTestDatabase calls the shared bootstrap helper
  against `postgres` before migrations.Apply. Concurrent package processes
  serialize through the same advisory lock in that one common database.

The bootstrap lock is distinct from goose and runtime barriers. Derive its
signed int64 from the first eight big-endian bytes of SHA-256 over
`aboutme.cluster-role-bootstrap.v1`: digest
`9a901c196fe58a64f9d649d75db6a1510079c3427f8559628ef4e0f83fa3a5ab`, bytes
`9a901c196fe58a64`, signed value `-7309311299645240732`. Source stores the
integer and a test recomputes it.

## Interface and ownership

The implementation author owns `apps/server/internal/dbroles/**` and
`apps/server/cmd/db-role-bootstrap/**`. The package exposes
`Ensure(ctx context.Context, db *sql.DB) (Result, error)` and the fixed
bootstrap lock ID. `Result` contains created/verified counts, without
identifiers from connection configuration. Ensure requires
`current_database() = 'postgres'`, serializes creation/verification in one
transaction, validates all existing roles and memberships before changing
anything, and commits once. Failure or uncertain commit returns an error without
an internal retry. A later invocation rereads the catalog. New role creation
never supplies a password.

Existing role drift is an error. Required membership may be created when the
role setup is new; a missing or changed edge in an existing complete setup is
reported for explicit repair. Ordinary role members cannot gain another fixed
role. Membership held by the trusted bootstrap administrator is distinguished
from membership held by application or worker roles; PostgreSQL may grant a
non-superuser role creator administrative membership automatically. Verify the
actual PostgreSQL 18 semantics in tests and documentation before encoding that
exception.

The command accepts only `CLUSTER_BOOTSTRAP_DATABASE_URL`. It rejects unknown
arguments, wrong database, missing configuration and malformed URLs. It emits
fixed outcome/count fields; it never prints a connection URL or raw driver
error. It bounds work to 30 seconds and uses cancellation-independent rollback
cleanup capped at five seconds. No credential input or rotation flag exists.

Root owns Makefile, workflow, Compose, Dockerfile, scripts, environment example,
test helpers and documentation wiring. The author reports required changes to
those paths. The command must be wired before migration 00013 can land.

## Failing-first checks

- Unit tests cover first creation, exact reuse, partial setup, every forbidden
  role attribute/membership, wrong database, missing privilege, rollback,
  cancellation, commit-response loss and absence of internal retry.
- Drift validation precedes mutation; an existing role is never altered.
- Two independent connections to postgres serialize concurrent ensures even when
  their eventual target databases differ.
- Live tests use the one shared PostgreSQL container. They may create the seven
  missing fixed roles, then inspect and reuse them. They never drop roles or
  alter existing role attributes/passwords to manufacture failure. Use unit
  fixtures for drift and existing-role failure cases.
- Privilege tests use the local admin's `SET SESSION AUTHORIZATION` and reset it
  on the same dedicated connection. No password read or write is needed.
- Existing aboutme and aboutme_dev data, ownership and login behavior stay
  intact.

Run, under apps/server:

```sh
go test -race -count=1 ./internal/dbroles ./cmd/db-role-bootstrap
```

Set `TEST_DATABASE_URL` to the repository's shared local test database for live
checks. The test derives a dedicated postgres connection without printing the
URL. Root reruns the key check, applies the shared wiring, and runs affected
migration/integration checks. Full CI and connected scan remain phase gates.

Local evidence: role and CLI race tests, make server-build server-vet
server-test, make server-test-db, make server-migration-test
server-test-integration, make operational-test, and make docs-fmt passed. The
server image built and its role-bootstrap command verified all seven existing
roles through the shared host database. No runtime schema, legacy grant change,
or hosted deployment is included.

[PostgreSQL 18 advisory locks](https://www.postgresql.org/docs/18/view-pg-locks.html)
are database-local.
[Role membership](https://www.postgresql.org/docs/18/role-membership.html)
controls SET and INHERIT separately. These are why every bootstrap uses the same
database and migrator membership grants SET without inherited ownership.
