# R1.4 public transition schema implementation plan

> For workers: use superpowers:subagent-driven-development or
> superpowers:executing-plans within AGENTS.md ownership. ADR 0024 keeps one
> author per task and one fresh phase review; it supersedes per-task review
> defaults.

**Goal:** Store exact transition targets, required replicas, acknowledgements
and immutable terminal outcomes before exposing their runtime operations.

**Architecture:** Migration 16 follows the protected version-15 baseline. Four
owner-controlled tables and fixed assertion functions enforce complete durable
records. Callable operations and the private commit capability are separate R1
slices.

**Tech stack:** PostgreSQL 18, Goose, Go and pgx; existing repository pins
apply.

**Spec:** [Transition storage](../../../design/scaling/transition-storage.md),
[public transitions](../../../design/scaling/public-transitions.md),
[transition commit](../../../design/scaling/transition-commit.md),
[runtime schema](../../../design/scaling/runtime-schema.md) and
[transaction entry](../../../design/scaling/transaction-entry.md).

## Scope, ownership and acceptance

This slice contributes to AC-INF-009 and Task 10.18's publication/fence rows.
AC-RT-001/002 still require caller and hosted proof. No R1 or phase exit is
claimed here.

One implementation author owns only:

- `apps/server/migrations/00016_runtime_public_transition_schema.sql`;
- `apps/server/migrations/runtime_public_transition_schema_test.go`;
- `apps/server/migrations/runtime_public_transition_schema_helpers_test.go`.

Root explicitly delegates migration 16 and serializes later numbers. Root owns
generated sqlc output, existing helpers/tests, SQL query sources, manifests,
plans and all shared files. Report any needed shared-file change. Designers and
reviewers of this slice do not implement it.

No historical SQL edit, Git operation, database container change, native
migration, role creation, new dependency, cloud action, callable transition
function, notification wiring, store method or caller change belongs here.

Use the single shared database container and isolated harness databases. Run at
most one heavy command at a time. Keep secrets out of tools and reports; use the
public fixture DSN from AGENTS.md. Every test has bounded setup and cleanup.

## Task 1: Prove the missing storage

- [x] Add a failing object-existence test using the existing
      `runtimeMembershipDB` harness:

```go
func TestRuntimePublicTransitionSchemaObjectsExist(t *testing.T) {
 db, ctx := runtimeMembershipDB(t)
 for _, name := range []string{
  "public_transitions", "public_transition_targets",
  "public_transition_replicas", "public_transition_acks",
 } {
  var exists bool
  if err := db.QueryRowContext(ctx,
   `SELECT to_regclass('public.' || $1) IS NOT NULL`,
   name).Scan(&exists); err != nil {
   t.Fatal(err)
  }
  if !exists {
   t.Errorf("table %s is missing", name)
  }
 }
}
```

- [x] Run the focused race command below before creating migration 16. Record
      all four missing tables as the expected failure.

```sh
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./migrations -run '^TestRuntimePublicTransitionSchemaObjectsExist$'
```

## Task 2: Install the exact schema and prove containment

- [x] Add the four tables, immediate restrictive foreign keys, supporting
      indexes, fixed owner digest helper and owner triggers from transition
      storage. Use the version-15 owner-role migration pattern. The first Up
      statement is `runtime_begin_migration_write('migration-00016')`; the last
      is `runtime_finish_write()`. Down is inert. No data is seeded into these
      tables. Existing data, capacity and controller generations are preserved;
      protected migration framing advances write generation once.
- [x] Add failing cases before each constraint or assertion that they exercise.
      Use complete valid fixtures so each negative result identifies the
      intended PostgreSQL SQLSTATE and named constraint or column. Do not accept
      any arbitrary SQL error as proof.
- [x] Prove the exact parent matrix field by field: every required null fails,
      every forbidden nonnull fails, every enum rejects an unknown value and
      `initiator_fenced` requires its evidence reference. Cross-reference tests
      cover initiator, required replica, parent digest and ack digest mismatch.
- [x] Prove target count 1..4, nil IDs, duplicate discovery/resume, ordinal
      gaps, discovery class/position, raw UUID ordering and positive
      generations. Independently encode the published Go vector; SQL recomputes
      from ordered target rows and must match its literal digest. Change each
      encoded field while preserving the old digest and require rejection.
- [x] Prove complete results for committed parents and all-null results for
      closing, rolled_back and unresolved. Test both generation and retirement,
      reject retired discovery and every partial result shape. Prove missing
      acks reject commit. Zero, partial and full ack sets are valid for the
      other three states, with no result authority.
- [x] Prove deferred checks on initial closing creation and later updates. Force
      constraints before a later invalid change and show that the later change
      still fails. Parent/target identity, terminal rows, required rows and acks
      reject mutation/deletion. Child insert after terminal fails.
- [x] Use two pinned connections to race terminalization against child insert
      and duplicate required/ack insertion. Gate on observed row-lock waits, not
      sleeps. The loser must have the exact intended constraint/state failure;
      no incomplete parent or late child may commit.
- [x] Prove all four no-TRUNCATE guards under valid owner write entry, including
      a multi-table/CASCADE attempt. Test every real named login role for denied
      direct DML and helper execution. Inspect owners, search paths, PUBLIC
      revocation, column grants and exact triggers. Owner writes without entry
      fail; a hostile search path cannot redirect a helper.
- [x] Prove a version-15 database containing membership, lifecycle and business
      fixtures upgrades without changing their rows. A failing schema fixture
      rolls back its rows and write generation. Reuse membership fixture framing
      with owner-managed writes through protected entry; never disable a trigger
      or skip entry.

The schema does not claim proof of historical membership selection, current
revision checks, local close/join, original caller deadline, notification
delivery, external fencing authority or commit capability. Their fixed
operations and callers must prove them later. R1/R3 install no unresolved
mutator; [recovery evidence](../../../design/scaling/transition-recovery.md)
defines parent-locked rollback and the outcome authority.

## Task 3: Verify and release to root

- [x] Run from `apps/server` with the public fixture DSN:

```sh
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./migrations -run '^TestRuntimePublicTransitionSchema'
GOGC=50 golangci-lint run ./migrations --tests
```

- [x] Inspect the owned diff and report exact red/green evidence, tables and
      helper names, real-role and concurrency results, shared-file needs and
      unresolved contract boundaries in
      `.dev/phase-10/runtime-public-transition-schema-author-report.txt`.
      Release all three owned files. Root rereads them, reruns key tests,
      generates sqlc output and runs the affected migration/database/Go gates.
      Root alone stages, scans and commits.

Stop at a concrete authority conflict or missing semantic decision and report it
to root. Do not replace a contract with a permissive constraint or invent a
callable operation to make a fixture pass.

## Local evidence

Migration 16 and its generated models are implemented. The author observed the
four missing tables before implementation and the nullable terminal/result
matrix failures before correction. Root inspected the SQL and tests, bounded
failure cleanup, and pinned the preservation fixture to versions 15 and 16.

Root verification passed:

- Focused transition race suite: 126.743 seconds.
- `make server-migration-test`, `make server-test-db server-test-integration`
  and `make server-build server-vet server-test`.
- Migration, store and account API lint: zero issues.
- Native `make migrate migrate-check`; user and resume counts stayed unchanged.

Both local databases are at migration 16, write generation 5 and migrator
enforcement version 1, with migration history owned by `aboutme_runtime_owner`.
The broader checks exposed two existing test assumptions: the membership
TRUNCATE check now uses CASCADE to reach its immutability guard, and the account
mail lock observer treats a NULL wait event as not yet blocked. Their focused
race tests and the affected database gates pass.

Callable operations, commit capability, store adapters and caller proofs remain
open. Full phase `make ci`, connected `make scan` and fresh phase review have
not run for this slice; they belong to the completed phase candidate.
