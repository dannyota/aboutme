# R1.6 shared rate schema implementation plan

> For workers: use superpowers:subagent-driven-development or
> superpowers:executing-plans within AGENTS.md ownership. ADR 0024 keeps one
> author per task and one fresh phase review; it supersedes per-task review
> defaults.

**Goal:** Store all fleet rate policies, bounded bucket state and retained OAuth
attempts before exposing admission or cleanup operations.

**Architecture:** Migration 18 follows the accepted version-17 claim schema.
Seven owner-controlled tables, exact seeds and deferred assertions preserve
partition counts and pending-window debt. Fixed runtime operations and caller
composition are separate R1/R5/R7 slices.

**Tech stack:** PostgreSQL 18, Goose, Go and pgx; repository pins apply.

**Spec:** [Rate storage](../../../design/scaling/rate-storage.md),
[admission](../../../design/scaling/admission.md),
[policy catalog](../../../design/scaling/policy-catalog.md),
[OAuth attempts](../../../design/scaling/admission-attempts.md),
[rate identities](../../../design/scaling/rate-identities.md),
[runtime schema](../../../design/scaling/runtime-schema.md) and
[transaction entry](../../../design/scaling/transaction-entry.md).

## Scope, ownership and acceptance

This slice contributes to AC-INF-009 and Task 10.18's fleet admission rows. It
does not complete R1, R5, R7 or Phase 10. Existing process-local admission
remains composed until its caller tasks pass.

Root assigns one implementation author only after migration 17 is accepted and
committed. Exclusive paths are:

- `apps/server/migrations/00018_runtime_shared_rate_schema.sql`;
- `apps/server/migrations/runtime_shared_rate_schema_test.go`;
- `apps/server/migrations/runtime_shared_rate_schema_helpers_test.go`.

Root owns migration numbering, generated sqlc output, query sources, existing
tests/helpers, plans, shared files and Git. Report any required shared-file
change. Designers and reviewers do not implement this slice.

No earlier migration edit, callable admission/cleanup operation, Go adapter,
caller change, new key/version field, role creation, dependency change, native
migration, container change or cloud action belongs here. SQL receives no raw
email, IP, bearer or HMAC key. Runtime logins receive no new table DML or helper
execution. Use isolated harness databases in the one shared container, the
public fixture DSN from AGENTS.md and one heavy command at a time. Bound every
setup, concurrency wait and cleanup; cancel and join concurrent work.

## Task 1: Prove missing storage

- [x] Add the focused existence test using the protected migration harness:

```go
func TestRuntimeSharedRateSchemaObjectsExist(t *testing.T) {
 db, ctx := runtimeMembershipDB(t)
 for _, name := range []string{
  "shared_rate_policies", "shared_rate_policy_key_shapes",
  "shared_policy_clocks", "shared_rate_partitions",
  "shared_rate_buckets", "shared_rate_overflow",
  "shared_admission_attempts",
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

- [x] Before writing migration 18, run from `apps/server` and record all seven
      expected missing-table failures:

```sh
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./migrations -run '^TestRuntimeSharedRateSchemaObjectsExist$'
```

## Task 2: Install exact state and seeds

- [x] Follow protected owner framing. First Up statement enters
      `runtime_begin_migration_write('migration-00018')`; last calls
      `runtime_finish_write()`. Down is inert. Preserve existing rows and
      capacity/controller generations; migration advances write generation once.
- [x] Create the immutable 24-row literal catalog and its 35-row key-shape
      relation. Bind exact policy spellings, algorithms, capacities, windows,
      clear/debt flags, idle microseconds and partition limit. P05 adds
      account_ip beside ip. The ten accepted failed-key middleware policies add
      peer_ip. Reject any unknown combination or catalog update/delete.
- [x] Pin allow_success_clear true only for P07/P22 and denied_attempt_adds_debt
      true only for P12; all other values are false. The first flag describes
      private-only success clear, never overflow clear. No denied P22
      reservation adds debt. Callable behavior remains a later slice; this
      migration proves the exact catalog literals.
- [x] Create clocks and two partitions per policy with catalog keys. Enforce
      nonnegative anomaly/count fields, positive capacity generation, partition
      1/2 and active_keys 0..10000. Preserve clock high-water monotonicity and
      immutable row identity. Use the existing 1..128 printable-ASCII operation
      ID contract. A deferred owner assertion checks active_keys against exact
      ordinary rows for that policy/partition.
- [x] Create ordinary and overflow state with the exact nullable algorithm
      matrix. The ordinary primary key is policy/digest32; its partition key
      binds the matching partition. Overflow has one policy key and no identity
      or partition. Bind algorithm and token capacity to the fixed catalog;
      token numerator stays within 0..capacity*window_microseconds. Local CHECKs
      never query another table.
- [x] Enforce token numerator/refill with other fields null; P07 count 0..10
      with start null exactly when count is zero; P22's stored-pending shape;
      and P12's array/count shape. Rolling arrays contain 0..30 nonnull sorted
      timestamps and count equals cardinality. Accept the empty array. Reject
      multidimensional nonempty arrays, null elements and wrong ordering.
- [x] Create attempts with the exact fields, nullable scope/state matrices and
      four partial indexes from OAuth attempts. Enforce non-nil attempt IDs,
      fixed policy, private/overflow shape, timing order and exact 15-minute
      expiry. Pending has no terminal data; terminal has complete outcome,
      reason and time. Private-success clear requires private/neutral; expiry
      requires neutral and terminal_at at or after effective_until.
- [x] Keep attempt identity immutable and terminal receipts stable. A pending
      row may become terminal through its accepted result matrix; no terminal
      row returns to pending or changes its outcome. Pending rows cannot be
      deleted as receipts. Terminal deletion is schema-permitted; its 24-hour
      cutoff and 256-row bound belong to the later maintenance function.
- [x] Add no foreign key from an attempt to a bucket or OAuth client. Deferred
      owner assertions bind every stored pending row to the exact private
      policy/partition/digest/window or overflow policy/window. Require
      committed count plus stored pending count at most ten. A P22 window is
      absent exactly when count and stored pending count are zero. Include
      expired but unresolved pending rows; never compare their expiry with a
      policy clock in this at-rest assertion.
- [x] Schedule deferred assertions after each bucket, overflow, partition or
      attempt change that can invalidate counts or pending bindings. Initial
      records must be complete at commit. A successful forced check does not
      excuse a later invalid mutation. Terminal receipts may outlive a removed
      bucket/client without triggering a false pending check.
- [x] Add ordinary write-entry and no-TRUNCATE guards to all seven tables.
      Preserve the always-present overflow and fixed catalog. Owner helpers use
      SECURITY DEFINER, search_path pg_catalog and qualified fixed SQL. Revoke
      PUBLIC and direct named runtime-role table/helper privileges.
- [x] Sample clock_timestamp once for all seeds: 24 policies, 35 key shapes, 24
      clocks, 48 disabled empty partitions at capacity_generation 1 and
      operation_id bootstrap-uncomposed-v1, and 24 neutral/full overflow rows.
      Seed no ordinary buckets or attempts. Clocks start with identical high
      water/raw time and anomaly_count zero; every seeded timestamp uses the one
      sample.

This slice adds no runtime time sampler, token arithmetic, allocation, expiry,
success clear, cleanup or lifecycle toggle function. It cannot prove a caller's
key derivation, closed key rotation, work joining, clock sampling after locks,
receipt age, runtime lock order or HTTP response. Those proofs remain in the
fixed-operation and caller tasks. Owner schema fixtures use protected entry and
the documented row-lock order; they never disable triggers.

## Task 3: Prove malformed and concurrent records fail

- [x] Cover each negative rule with a test. Start from complete valid fixtures
      and require the exact SQLSTATE plus named constraint or column. Exercise
      every required/forbidden field independently, especially nullable
      matrices; an unrelated SQL error is not proof.
- [x] Compare all seed rows and literal values, not just counts. Check the
      complete 35-shape set, one shared P01 policy and both P05 shapes. Reject
      catalog drift and a policy/algorithm mismatch.
- [x] Exercise all token policy numerator bounds, fixed counts -1/0/10/11 and
      P07 empty/window matrix. Check rolling arrays at 0/1/29/30/31 elements,
      duplicates in sorted order, reversed order, nulls, two dimensions and
      mismatched count. Test ordinary and overflow independently.
- [x] Prove active_keys matches rows at zero, one and 10000, rejects 10001,
      overstatement and understatement, and rolls back with failed inserts or
      deletes. Toggling a partition's flag preserves existing rows and debt.
- [x] Prove P22 first pending accepts count zero/non-null start; missing bucket,
      wrong partition/key/window and ten failures plus one pending fail.
      Exercise all allowed outcome/reason shapes, immutable terminal results,
      pending deletion and terminal retention without bucket/client foreign
      keys.
- [x] Advance an unrelated policy clock while a P22 key retains an expired but
      unresolved pending row. Its matching stored window remains valid. Removing
      that bucket or resetting its window before resolving that row must fail.
      Then terminalize the row and remove the bucket/count atomically; preserve
      the terminal receipt. No timestamp change alone resolves or removes an
      attempt.
- [x] Force deferred checks, then make a later bad count/window change and
      require rejection. Use two pinned connections with observed row-lock waits
      for duplicate policy/key allocation and exact partition-count updates.
      Assert the exact losing result and final count; do not sleep as proof of a
      race.
- [x] Test all seven write-entry/no-TRUNCATE guards, including multi-table
      CASCADE. Exercise all real named login roles for denied table DML and
      helper execution. Inspect exact owners, triggers, column/PUBLIC grants,
      SECURITY DEFINER and search paths. Hostile search paths cannot redirect a
      helper; owner mutation without entry fails.
- [x] Upgrade a populated fixture explicitly from version 17 to 18, preserving
      membership, lifecycle, transition, claim and business rows. A failing
      migration fixture rolls back new schema, seeds and write generation.

## Task 4: Verify and release

- [x] Run from `apps/server` with the public fixture DSN:

```sh
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./migrations -run '^TestRuntimeSharedRateSchema'
GOGC=50 golangci-lint run ./migrations --tests
```

- [x] Inspect the three owned paths and report exact red/green evidence,
      table/helper names, roles, concurrency, shared-file needs and any contract
      boundary in `.dev/phase-10/runtime-shared-rate-schema-author-report.txt`.
      Release all three files. Root inspects and reruns key checks, generates
      sqlc output and runs the migration/database/Go gates before staging,
      scanning and committing.

Stop at a concrete authority conflict or missing decision. Do not replace an
invariant with a permissive constraint or add a callable operation for fixtures.

## Local acceptance evidence

The missing-table test failed for all seven tables before migration 18. The
author's final focused race suite passed in 131.203s and scoped lint reported
zero issues. Root inspected the SQL, both test files and seven generated models.
Root's original full focused race passed in 120.525s. After fixing missing
matrices and confounded P22 fixtures, root reran all changed test groups with
`-race -count=1`; they passed in 28.462s. No SQL change followed those checks.
Additional negative cases were added during acceptance. The clock criterion now
names the unrelated-policy storage mutation used by this slice; keyed clock
operations belong to migration 21.

Root completed `make server-migration-test` (migration package 395.072s, CLI
6.310s), `make server-test-db server-test-integration`,
`make server-build server-vet server-test` and scoped migrations/store lint. The
final fixtures prove stored-window private/overflow admission at nine, ten and
rejected eleven pending attempts, independent field constraints, real forced
checks before a later invalid mutation, and joined race cleanup.

Root ran `make sqlc-gen` and inspected all seven new models. Native
`make migrate migrate-check` and before/after user/resume count comparison pass.
Both local databases are version 18, write generation 7, enforcement version 1
and migration-history owner aboutme_runtime_owner. No container reset occurred.
The post-commit sqlc consistency check and final phase gates remain separate.
