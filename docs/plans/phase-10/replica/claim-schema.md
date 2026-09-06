# R1.5 shared claim schema implementation plan

> For workers: use superpowers:subagent-driven-development or
> superpowers:executing-plans within AGENTS.md ownership. ADR 0024 keeps one
> author per task and one fresh phase review; it supersedes per-task review
> defaults.

**Goal:** Store fleet concurrency claims, atomic scopes and retained release
receipts before exposing acquire, promote, release or cleanup operations.

**Architecture:** Migration 17 follows the accepted version-16 transition
schema. Four owner-controlled tables, a fixed six-row policy catalog and
deferred assertions preserve exact claim identity and charged capacity. Runtime
operations and caller integration follow in separate R1/R5 slices.

**Tech stack:** PostgreSQL 18, Goose, Go and pgx; repository pins apply.

**Spec:** [Shared claims](../../../design/scaling/shared-claims.md),
[claim identities](../../../design/scaling/claim-identities.md),
[runtime schema](../../../design/scaling/runtime-schema.md),
[membership](../../../design/scaling/replica-membership.md) and
[transaction entry](../../../design/scaling/transaction-entry.md).

## Scope, ownership and acceptance

This slice contributes to AC-INF-009 and Task 10.18's fleet admission rows.
Caller caps, joined external work, exact fencing and hosted acceptance remain
open. It does not complete R1, R5 or Phase 10.

Root assigns one implementation author only after migration 16 is accepted.
Exclusive paths are:

- `apps/server/migrations/00017_runtime_shared_claim_schema.sql`;
- `apps/server/migrations/runtime_shared_claim_schema_test.go`;
- `apps/server/migrations/runtime_shared_claim_schema_helpers_test.go`.

Root owns generated sqlc output, existing helpers/tests, query sources, plans,
shared files, Git and migration numbering. Report required shared-file edits. A
designer or reviewer of this slice may not implement it.

No earlier migration edit, public callable claim operation, role creation,
dependency change, local queue integration, rate bucket, native migration,
container change or cloud action belongs here. Runtime logins receive no new
direct table access or helper execution. The later fixed functions receive the
named grants from the accepted design.

Use isolated harness databases in the one shared container. Run at most one
heavy command at a time. Setup, concurrency waits and cleanup must be bounded.
Use only the public fixture DSN from AGENTS.md; never read secret values.

## Task 1: Prove the missing schema

- [x] Add `TestRuntimeSharedClaimSchemaObjectsExist` using the existing
      protected migration harness. Require these four tables:
      shared_claim_policies, shared_claim_scope_summaries, shared_claim_requests
      and shared_claim_scopes.
- [x] Before writing migration 17, run from `apps/server` and record the four
      expected missing-table failures:

```sh
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./migrations -run '^TestRuntimeSharedClaimSchemaObjectsExist$'
```

## Task 2: Install complete claim records

- [x] Follow the version-16 protected owner pattern. The first Up statement
      enters `runtime_begin_migration_write('migration-00017')`; the last calls
      `runtime_finish_write()`. Down is inert. Seed exactly the six accepted
      policy rows, with no summary, parent or scope. Preserve existing data and
      capacity/controller generations. Protected migration advances write
      generation once.
- [x] Fix policy IDs, kinds, running/waiting limits, queue flags and deadline
      modes to the catalog. Reject unknown combinations and runtime catalog
      update/delete. The catalog is not a caller configuration surface.
- [x] Add summary keys, catalog foreign keys, 32-byte digests, nonnegative
      counts and positive nonwrapping allocation ordinals. A deferred owner
      assertion checks the exact catalog limits and live child counts. Local
      CHECK expressions must not query other tables.
- [x] Add parents with non-nil claim and replica IDs, exact policy/work shape,
      immutable identity, request digest and legal waiting/running/released
      states. C01 alone requires non-nil work_id and an exact admitted_at plus
      20-second deadline. Other policies require both work_id and deadline null.
      Release fields exist exactly for released state and use the accepted
      joined/canceled/expired/fenced enum. Release never changes identity.
- [x] Add scopes with parent/policy and summary foreign keys, request ordinals
      1/2, allocation ordinals and mirrored parent state. Enforce one scope for
      C01-C04; C05 has IP first and optional account second. Request and
      allocation ordinals are distinct. Several claims may share one scope.
- [x] Recompute the request digest from normalized rows with a fixed owner
      helper. Use the accepted byte frame, not JSON or text UUID encoding. Scope
      HMAC encoding remains R5-owned; SQL receives only 32-byte digests.
- [x] Schedule deferred whole-record assertions on every parent, scope or
      summary mutation that can invalidate identity, shape or counts. Initial
      creation must be complete at commit. A later invalid write must fail even
      after an earlier forced constraint check passed.
- [x] Preserve terminal release receipts. A child cannot disappear while its
      parent is waiting/running. Receipt cleanup may delete released children
      before their parent atomically, and remove a zero summary only after all
      children are gone. No age-based live reclamation is introduced. The fixed
      24-hour/256-parent cleanup function is a later slice.
- [x] Attach ordinary write-entry assertions to all four tables. Reject
      TRUNCATE, including multi-table/CASCADE forms. Owner helpers use SECURITY
      DEFINER, pg_catalog search path and fixed qualified SQL. Revoke PUBLIC and
      named runtime login table/helper access.

The schema cannot prove local join, operation age, exact EC2 death, database
time sampling after acquisition locks or the caller's private replica factory.
Fixed operations and R5/R8 composition must prove those boundaries. It must not
add an app-callable fenced release, repair function or general claim input.

## Task 3: Prove identity, capacity and receipt constraints

- [x] Add each negative test before its constraint. Complete valid fixtures must
      fail on the intended SQLSTATE and named constraint or assertion; an
      unrelated error is not evidence.
- [x] Test each required and forbidden parent field separately, including SQL
      NULL behavior, nil UUIDs, digest lengths, every legal state edge and each
      forbidden edge. Released identity/state/result data is immutable.
- [x] Pin the published 165-byte SSE vector and literal digest independently in
      Go and SQL. Changing claim, policy, replica, work presence/UUID, scope
      count, request ordinal, kind or scope digest with the old request digest
      must fail. Allocation, state and timestamps are not digest inputs.
- [x] Prove every fixed catalog row and reject drift. Exercise each running and
      waiting boundary with multiple claims sharing a summary. Detect
      understated and overstated counts, an invalid policy kind, a wrong
      parent-policy binding and a mismatched child state.
- [x] Prove C05 IP/account insertion and release are atomic. Missing, extra,
      swapped, duplicate or cross-parent scopes fail. Denied or rolled-back
      fixtures leave no partial count or child.
- [x] Prove allocation ordinal uniqueness within a scope, independent request
      ordinal reuse across claims, and nondecreasing summary allocation state.
      Old retained children prevent summary deletion/recreation. A removed,
      unreferenced zero summary may later start a new lifetime at one.
- [x] Prove waiting/running rows cannot be deleted, released children are
      deleted before the parent, and referenced/nonzero summaries cannot be
      deleted. No timestamp alone changes or deletes a live claim.
- [x] Use pinned independent connections for duplicate claim/allocation
      conflicts and a same-scope count update race. Gate on observed database
      lock waits, not sleeps; assert the exact losing result and final counts.
- [x] Exercise every real named login for denied direct DML and helper
      execution. Inspect exact owners, triggers, PUBLIC/column privileges and
      search paths. Owner writes without entry fail; a hostile search path
      cannot redirect helpers.
- [x] Upgrade a populated version-16 fixture without changing membership,
      lifecycle, transition or business rows. A failed migration rolls back its
      schema, seed rows and write generation. Use protected owner entry; never
      disable a trigger for fixtures.

## Task 4: Verify and release

- [x] Run from `apps/server` with the public fixture DSN:

```sh
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./migrations -run '^TestRuntimeSharedClaimSchema'
GOGC=50 golangci-lint run ./migrations --tests
```

- [x] Inspect the owned changes and report exact red/green evidence, object and
      helper names, role/concurrency results, shared-file needs and any
      unresolved boundary in
      `.dev/phase-10/runtime-shared-claim-schema-author-report.txt`. Release all
      three files. Root rereads the changes, reruns key checks, generates sqlc
      output and runs affected migration/database/Go gates before staging,
      scanning and committing.

Stop at a concrete authority conflict or missing semantic decision and report it
to root. Do not replace a fixed invariant with a permissive constraint.

## Integration evidence

The author observed the four missing objects before migration 17 and the C05
account-only failure before its shape fix. The final author report maps each
plan row to named tests. Root inspected the SQL, fixtures and exact error
checks, then reran the focused race suite: 115.730 seconds, passing.

Root checks passed:

- `make server-migration-test`: migration harness 278.922 seconds; CLI 6.129
  seconds.
- `make server-test-db server-test-integration server-build server-vet server-test`.
- `GOGC=50 golangci-lint run ./migrations ./internal/store --tests`: zero
  issues.
- `make sqlc-gen`: four generated claim models, inspected by root.
- Native `make migrate migrate-check`: version 17, unchanged user/resume counts.
  Both local databases are version 17, write generation 6, enforcement 1 and
  history owner aboutme_runtime_owner.

Logs are under ignored `.dev/phase-10/runtime-shared-claim-schema-root-*` and
`.dev/phase-10/native-claim-schema-*`. No callable claim operation, caller
admission, exact EC2 fence or hosted check is claimed by this schema slice.
