# 0038 — Single baseline migration and plain migrator

Status: Accepted (2026-09-17), by the human owner's direction.

## Context

Nothing is deployed and no customer data exists. The production database is
empty. Migrations 00013 to 00023 install the replica-coordination runtime and a
staged, protected migrator. The first release runs one replica under
[ADR 0036](0036-single-replica-launch-and-pipeline-migrations.md), and the
server calls none of that runtime. It costs about 5,000 lines of SQL, 3,000
lines of migrator code, most of the migration test time, and every future schema
change has to fit its staging and ownership rules.

A review of the migrations found that no migration grants `aboutme_app` any
privilege on the business tables. Local, test and Compose runs connect as a
superuser, so the gap never showed. The hosted server, which connects as
`aboutme_app`, would have failed on its first query.

Once real accounts exist, collapsing history is no longer possible without a
data migration. It is cheap only now.

## Decision

Before the first deploy:

- One baseline migration, `00001_baseline.sql`, replaces migrations 00001 to
  00012 with the schema they produce, keeping every table, column order,
  constraint, index, function and trigger name. It grants `aboutme_app` exactly
  SELECT, INSERT, UPDATE and DELETE on each business table. Every later
  migration grants explicitly, and a catalog test enforces the grants.
- Migrations 00013 to 00023 and the code that exists only for them are removed:
  the runtime schema and functions, the store runtime transports, the write
  barrier runner and the migration connection transport.
- The migrator is plain goose with a PostgreSQL session advisory lock. It always
  runs as `aboutme_migrator`, which owns every schema object. There are no
  identity modes, no provisioning stage and no history adoption.
- Two database roles remain: `aboutme_migrator` and `aboutme_app`. One
  idempotent `db-setup` command, run as the database owner, creates them, sets
  the database and schema grants, and can store their SCRAM verifiers. It
  replaces `db-role-bootstrap`, `migrate provision` and `db-set-login`.
- The migration baseline marker is reset once to the new baseline. The
  append-only guard allows exactly this reset through a pinned, one-time
  exemption that a following commit removes.
- The unreleased `v0.1.0` tag is withdrawn; the first release is a later tag.

## Consequences

- Schema changes become ordinary goose migrations with explicit grants.
- A later second replica needs the coordination runtime again. Its accepted
  design stays in [the scaling contract](../design/scaling/README.md) and in git
  history, and would return as new migrations.
- Local development and test databases are recreated once.
- The hosted app can reach its tables, which a test now proves.

## Compatibility and review

This record supersedes, for the first release:

- [ADR 0020](0020-uat-migration-baseline.md): one reset of the baseline marker
  and of the migrations it froze. The marker rule stands for everything after.
- [ADR 0035](0035-replica-coordination-and-uat-lifecycle.md): the installed
  runtime schema and the protected, staged migrator. Its coordination design
  stands as the reference for a later second replica.
- [ADR 0036](0036-single-replica-launch-and-pipeline-migrations.md): the clauses
  that keep migrations 13 to 23, their transports and the protected migrator
  installed. Its single-replica decision and deploy-step migrations stand.

Public HTTP and SSE contracts, privacy deadlines, private media authorization
and every data invariant from migrations 00001 to 00012 are unchanged.
