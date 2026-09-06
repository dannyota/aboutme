# Database migration provisioning and adoption

Status: Accepted target under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). The
[dedicated runner](migrator.md) owns identities, lock order and history
validation. [B3](../../plans/phase-10/replica/migrator-composition.md) owns
implementation.

## Database-local privileges

Before migration on a target database without runtime_write_state, the existing
bootstrap administrator performs one separately named, idempotent database-local
provision step. It:

- grants aboutme_migrator CONNECT, TEMPORARY, and CREATE on that database;
- grants USAGE and CREATE WITH GRANT OPTION on schema public to migrator;
- grants runtime_owner TEMPORARY on the database as B1 requires;
- verifies exact owners/grants and fails closed before mutation on drift.

Provisioning is permitted only while runtime_write_state is absent. Migrator is
the accepted reviewed-DDL principal and retains this scoped schema grant option.
It may grant runtime_owner CREATE inside 00013/00014, transfer fixed objects,
then revoke the child grant in the same transaction. Runtime_owner never retains
schema CREATE. Once the foundation exists, Apply only validates these grants;
any repair is an explicit barrier-entered migration. Thus an out-of-band grant
cannot race finalization.

This changes database grants, not the aboutme cluster role. CREATE on the
database is required for the trusted citext extension migration on a fresh
database. The provisioner takes no password and accepts no arbitrary role.
Database-role provisioning creates and verifies the migrator schema CREATE grant
with grant option. When 00013 runs as non-superuser migrator, it grants
runtime_owner schema CREATE, transfers only its new foundation objects, revokes
that child grant, and asserts runtime_owner has no residual CREATE. Live tests
use SET SESSION AUTHORIZATION to prove the real migrator can perform this
sequence and cannot grant database or non-schema privileges from it. B1 proves
this schema grant-transfer-revoke sequence through the real non-superuser role.
Fresh databases run migrations 1..13 as migrator, so it owns Goose history and
may transfer it to runtime_owner in 00014. Existing databases with inert
enforcement version 0 whose recorded history owner is aboutme use a separate
fixed prebaseline `migrate adopt-history-owner` command. As authenticated
aboutme admin it takes the runtime shared SESSION advisory lock, then Goose's
session lock, before object/state locks. In one transaction it validates gate
open, enforcement 0, maximum version 13, recorded owner aboutme, no B3 trigger,
and the fixed prebaseline local/test database class: aboutme, aboutme_dev, or
names matching `^aboutme_migrate_(cmd_)?test_[0-9]+_[0-9]+$`. It requires
authenticated session_user aboutme with is_superuser on; a name alone grants no
authority. It atomically installs and verifies the provisioning grants: migrator
CONNECT/TEMPORARY/database CREATE, schema USAGE and CREATE WITH GRANT OPTION,
and runtime_owner TEMPORARY. It validates every manifest object exists with
expected aboutme owner, while B1 foundation objects already have runtime_owner.
Unexpected owners, missing/extra manifest objects, or broader relevant-role ACLs
fail before mutation; unrelated ACLs are neither broadened nor repaired. It then
grants runtime_owner CREATE on public and transfers only the fixed owner-bearing
manifest tables and functions, including Goose history. The Goose identity
sequence must follow the table's internal dependency; adoption validates its
resulting owner and does not independently ALTER it if PostgreSQL forbids that
operation. It verifies dependent indexes, row types and triggers converged
without OID changes, and revokes that child grant. It then locks the state row
last and atomically records migration_history_owner as aboutme_runtime_owner
plus one generation advance, writer_kind migrator, and operation
migration-history-adoption. It samples database time after the lock and applies
the timestamp high-water rule. Rows, object identities and definitions remain
unchanged. Preserve non-owner effective privileges; ACL comparison permits only
the expected old/new owner and grantor OID normalization plus explicit
provisioning/Goose grants. It releases Goose then runtime lock after commit.
Ambiguous commit or cleanup is not retried; reread resolves the exact
owner/metadata state. It changes only the enumerated ownership and privilege
metadata. It never uses REASSIGN OWNED or changes application rows, object
identities or definitions.

This adoption is allowed only before final-stop authority and before enforcement
version 1. The command has fixed roles/table/key and accepts no role or SQL
input. It exists only for the known prebaseline local/test aboutme-owned v13
class. Hosted fresh UAT uses initial pre-foundation provisioning, owns history
as migrator, proceeds directly to 00014, and has no adoption task or retained
admin credential. Adoption preserves existing data; recreation remains optional.

## Fixed ownership manifest

The [version-13 manifest](../../plans/phase-10/replica/migrator-manifest.md)
fixes every transferred table and function and every validated index, sequence
and row type. Fresh migration 00014 and local adoption converge on
aboutme_runtime_owner for those exact objects. Already-owned foundation objects
are verified, never transferred. Exclude extension members and PostgreSQL
internals. Reject a missing, extra or mixed-owner object before mutation.

Execute reviewed fixed ALTER statements only. Never use REASSIGN OWNED, a schema
wildcard or a catalog name as an ALTER target. Verify unchanged OIDs, rows,
definitions, dependencies and non-owner effective privileges after transfer.
Only expected owner/grantor ACL normalization and named grants may change.
PostgreSQL transfers the Goose identity sequence with its table; verify that
result without an independent ALTER SEQUENCE OWNER.

## Hosted initial provisioning boundary

Task 10.8's one-shot `deploy/aws/scripts/db-bootstrap.sh` performs initial
database-local provisioning before foundation installation. Its bootstrap
execution role supplies Task 10.4's `/aboutme/<env>/db/master-password` only to
that one-shot task. The later migrate task uses only
`/aboutme/<env>/db/migrator-password` and hardcodes MIGRATION_IDENTITY=direct.
Values resolve inside the consuming process; no secret is a command argument,
log, workflow output or tracked value. This records the task boundary; actual
hosted role/SSM wiring remains an infrastructure task after local runtime proof.
The hosted migrate task has no admin credential or local adoption mode.

Cluster role bootstrap remains separate from this database-local step. It uses
the fixed seven-role contract and does not rotate credentials implicitly. The
older three-role/bootstrap ownership wording in infrastructure tasks must be
aligned before their implementation. Production still requires Phase 11
approval.
