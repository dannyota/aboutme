# R1.3 membership schema

Install the durable membership and lifecycle ledger schema before adding their
callable operations. This is a bounded part of R1, not its exit gate.

## Authorities and acceptance

Use [runtime schema](../../../design/scaling/runtime-schema.md),
[membership](../../../design/scaling/replica-membership.md),
[lifecycle operations](../../../design/scaling/lifecycle-operations.md),
[lifecycle replay](../../../design/scaling/lifecycle-replay.md), and
[transaction entry](../../../design/scaling/transaction-entry.md). The accepted
design is committed at `1149ac5`.

This slice contributes to AC-INF-009 and Task 10.18's topology/lifecycle rows.
It proves storage constraints and privileges only. Activation, fencing,
admission, transition, caller, and hosted acceptance belong to later slices;
wake is deferred under ADR 0036.

## Ownership

One implementation author owns:

- `apps/server/migrations/00015_runtime_membership_schema.sql`;
- `apps/server/migrations/runtime_membership_schema_test.go`;
- `apps/server/migrations/runtime_membership_schema_helpers_test.go`.

Root explicitly delegates this migration number and serializes later numbers.
Root owns existing migration tests, generated sqlc output, manifests and all
other files. The author reports any needed shared-file edit. No historical SQL,
Git operation, role creation, database container change, native database
migration, caller wiring, or cloud action belongs to this task.

## Stored objects

Install only these eight tables and their required indexes, constraints,
owner-only assertion functions, and triggers:

1. `runtime_replicas`.
2. `runtime_replica_tasks`.
3. `runtime_capacity`.
4. `runtime_lifecycle_operations`.
5. `runtime_lifecycle_operation_steps`.
6. `runtime_termination_intents`.
7. `runtime_leave_receipts`.
8. `runtime_fencing_proofs`.

Install the four owner composite result types from membership. The ledger stores
ordinary typed columns, not composite or JSON payloads. Apply its exact
action/result nullable matrix and typed once-only replacement predecessor.

Use the accepted capacity seed. It creates no replica, operation, receipt or
proof. Rate partitions belong to the later admission schema; when created they
start disabled. Do not add node-to-partition columns or invent capacity-row
partition flags. This schema slice neither creates nor enables rate allocation.

The existing write-state generation advances once through migration framing;
capacity and controller generations start at one. Do not reset the write gate,
accepted-writer timestamp or existing business data.

Identity is immutable. Enforce exact normalized trios, global cross-role task
uniqueness, one nonterminal incarnation per instance, and composite identity
references from intent/leave/proof rows. Release digests may be shared by two
replicas. Enforce the exact state graph and required timestamp shape. Terminal
membership and evidence cannot be reopened, deleted or truncated.

The lifecycle ledger enforces structural workflow/action compatibility, parent
workflow identity, applicable result fields and positive generations. Every
fresh result generation is expected generation plus one. Operation, step,
intent, leave and proof rows are immutable. Runtime action predicates and
canonical digest computation belong to the later fixed definer slice; this slice
stores the fixed 32-byte digests and does not claim to prove their content.

## Privileges and framing

Use runtime_owner ownership, `search_path=pg_catalog`, qualified object names,
fixed SQL and PUBLIC revocation. Attach ordinary write-entry statement
assertions to every new mutable table, including TRUNCATE. These assertions mark
bookkeeping, never accepted business writes. Owner row/constraint triggers
enforce immutable identity, exact child sets and evidence/result shape.

Grant only the documented read privileges. No app, maintenance, lifecycle, proof
or restore role gets direct new-table DML or trigger-helper execution. Do not
grant an unimplemented operation, wake or final-stop function. Future R1 slices
install those fixed functions and their named-role EXECUTE grants; R8 adds no
extra privilege gate.

Apply through the protected migrator. The first Up statement is
`runtime_begin_migration_write('migration-00015')`; the last is
`runtime_finish_write()`. Use the established transactional owner-role pattern.
Keep Down inert under the forward-migration policy.

## Failing-first proof

Use the shared database container and isolated databases from the existing
migration harness. Observe missing schema fail before writing the migration.
Test real PostgreSQL behavior, including:

- Fresh and existing version-14 databases converge without losing application
  data; capacity seed is exact and contains no invented evidence.
- Missing/extra/mismatched trio children fail at commit; concurrent cross-role
  reuse and concurrent nonterminal instance reuse allow only one valid winner.
- Bad UUID, instance, ARN bounds, release, state, timestamp and identity changes
  fail. Same-build distinct replicas succeed. Deployment-prefix validation
  remains in the future private adapter; this schema enforces stored bounds.
- Evidence rejects tuple mismatch, duplicate conflicting IDs, mutation, deletion
  and truncation. Replica terminal state cannot reopen.
- Every lifecycle result shape rejects missing required and populated forbidden
  fields. Replacement predecessor consumption is unique. Wrong parent workflow
  and incompatible action fail.
- Direct DML, helper execution, missing write entry and hostile search paths
  fail through the real named roles. Valid owner-managed fixtures retain the
  barrier and explicit finish; no trigger-disabling fixture is allowed.
- Rollback leaves seed/evidence and generation unchanged. Catalog inspection
  proves exact owners, grants, search paths, triggers and composite types.

## Checks and report

Run from `apps/server`, using the public fixture DSN from AGENTS.md:

```sh
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./migrations -run '^TestRuntimeMembershipSchema'
GOGC=50 golangci-lint run ./migrations --tests
```

At most one heavy command runs for this author. Do not run `make ci`,
`make scan` or a full server test wave. Root reruns the key tests and affected
migration, database, integration and sqlc checks before acceptance.

Report changed paths, exact red/green evidence, real-role and concurrency cases,
commands/results, required root edits and unresolved contract boundaries in
`.dev/phase-10/runtime-membership-schema-author-report.txt`. Stop at a concrete
design conflict rather than inventing a contract.

## Local evidence

The author observed the missing eight tables fail before adding migration 15.
The root race check passed in 39.603 seconds. Root also passed
`make server-migration-test server-test-db server-test-integration` after fixing
foundation tests that had assumed migration 14 remained the latest version.
Those tests now use a fixed version-14 fixture; general migration tests still
exercise the current head.

`make server-build server-vet server-test` passed. Scoped migration and store
lint reported zero issues. Root documentation formatting and lint also passed.

Committed locally at `05e49ce`. The clean-tree `make sqlc-check` passed after
the commit. Both test and native databases are at version 15, write generation 4
and enforcement version 1. Native `make migrate migrate-check` passed, with user
and resume counts unchanged. This evidence does not complete R1 or hosted
acceptance.
