# R1.13 protected wake migration implementation plan

> For workers: use superpowers:subagent-driven-development or
> superpowers:executing-plans within AGENTS.md ownership. ADR 0024 keeps one
> author per task and one fresh phase review; it supersedes per-task review
> defaults.

**Goal:** Apply admitted migration sources during an exact recorded wake while
ordinary writers remain blocked by the closing gate.

**Architecture:** Migration 25 follows exclusive wake 24. It adds a fixed wake
migrator session and the narrow closing branch in protected per-version entry.
ApplyWake preserves B3's one-backend-per-version execution and physical
retirement. A compiled registry binds source admission to exact migration bytes.

**Tech stack:** PostgreSQL 18, Goose, Go, database/sql and pgx; repository pins
apply.

**Spec:** [Wake migrations](../../../design/scaling/wake-migrations.md),
[wake operations](../../../design/scaling/wake-operations.md),
[exclusive entry](../../../design/scaling/lifecycle-write-entry.md),
[migrator](../../../design/scaling/migrator.md),
[provisioning](../../../design/scaling/migration-provisioning.md),
[retirement](../../../design/scaling/migrator-retirement.md) and
[write entry](../../../design/scaling/transaction-entry.md).

## Ownership and acceptance

This slice contributes to AC-INF-009 and Task 10.18's wake/write-barrier rows.
It does not complete R8 writer coverage, controller composition, final stop or
hosted acceptance. R8 composes this path only after its local prerequisites
pass.

Root assigns one Sol author after migration 24 is accepted and committed. The
author must not have designed or reviewed this slice. Exclusive new paths:

- `apps/server/migrations/00025_runtime_wake_migrator.sql`;
- `apps/server/migrations/runtime_wake_migrator_test.go`;
- `apps/server/migrations/runtime_wake_migrator_helpers_test.go`;
- `apps/server/migrations/migrator_wake.go`;
- `apps/server/migrations/migrator_wake_test.go`;
- `apps/server/migrations/migrator_wake_session.go`;
- `apps/server/migrations/migrator_wake_session_test.go`;
- `apps/server/migrations/migrator_wake_sources.go`;
- `apps/server/migrations/migrator_wake_sources_test.go`;
- `apps/server/migrations/migrator_wake_wire_fault_test.go`.

Root also delegates `migrator_session.go` and `migrator_session_test.go` in that
directory for the minimal private reuse needed by the two fixed locker modes.
The normal constructor, entry/exit SQL, open-gate checks and cleanup contract
stay unchanged. No exported mode, callback, SQL selector or bypass is added.

Root owns `migrations.go`, the static caller guard, the compiled production
entries in `apps/server/migrations/migrator_wake_registry.go`, generated files,
earlier migrations, manifests, plans and Git. The author reports exact shared
edits before dependent checks. The author owns registry validation and test
fixtures; root records literal production hashes/classifications after
inspecting each final source. No frozen version-13 adoption manifest change is
permitted.

Use isolated harness databases in the shared container, the public fixture DSN
and one heavy command at a time. Bound setup, lock waits and cleanup. Cancel and
join all goroutines before dropping fixtures. No native/shared database
migration, container change, dependency change, secret read, cloud call or
production caller wiring belongs here.

## Fixed surface

```sql
runtime_enter_wake_migrator() RETURNS void
runtime_exit_wake_migrator() RETURNS void
```

Only direct aboutme_migrator executes these functions. They derive operation and
mode from durable capacity/wake evidence and accept no arguments.

```go
func ApplyWake(context.Context, *sql.DB, MigrationIdentity, ...lock.SessionLockerOption) ([]*goose.MigrationResult, error)
```

The public wrapper uses embedded FS and the compiled registry only. Direct and
LocalAdmin keep their current fixed identity semantics. The caller retains its
database pool. Private test fixtures may supply an isolated filesystem and exact
registry; no runtime environment, CLI or database override exists.

## Task 1: Establish failing boundary tests

- [ ] Before migration 25 exists, test both exact signatures and observe the
      intended missing-function failure:

```sh
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./migrations -run '^TestRuntimeWakeMigratorSurfaceExists$'
```

- [ ] Add failing tests for normal Apply rejecting closing and ApplyWake
      requiring the exact active wake. Use otherwise-valid migration 24 fixtures
      so unrelated registration, partition or ledger errors cannot satisfy the
      assertion.
- [ ] Add failing source-admission and per-version cleanup tests before their
      implementation. Preserve exact red/green commands and results.

## Task 2: Install the fixed session and closing entry

- [ ] Use protected framing, runtime_begin_migration_write first and
      runtime_finish_write last, with inert Down. Migration 25 itself runs under
      normal open-gate Apply and is classified wake_safe=false.
- [ ] Install owner SECURITY DEFINER functions with search_path=pg_catalog,
      qualified static SQL, exact session_user checks and fixed value-free
      errors. Revoke PUBLIC and every other login's execution.
- [ ] Enter the session-level shared runtime barrier before any inner lock or
      wake metadata read. Validate every active-wake predicate from the spec:
      closing/starting/nonadmitting, exact current operation and begin digest,
      no completion, zero nonterminal replicas, retained disabled-partition
      results, no visible closing/unresolved transition, and exact protected
      history enforcement.
- [ ] Create or strictly validate the owner-only temporary
      runtime_wake_migrator_session_v1 table. Pin its seven columns, types,
      constraints, index, owner and ACLs. Use ON COMMIT PRESERVE ROWS. Reuse
      only an empty valid retained table; never adopt, repair or drop a
      lookalike.
- [ ] Bind backend PID, direct session role, entry generation, derived wake
      operation/mode and entered-at. Reject mixed normal/wake session rows,
      ordinary/exclusive transaction markers and finish guards. Add symmetric
      wake-marker rejection to normal migrator entry in this forward migration.
- [ ] Replace runtime_begin_migration_write to accept exactly one validated
      migrator session under the held shared barrier. Preserve canonical
      migration-NNNNN IDs and the separate per-version write marker.
- [ ] Replace runtime_require_write_entry with the sole closing branch from the
      spec: exact migrator kind/role, bound wake session, matching transaction
      entry generation, held barrier and still-valid durable active wake.
      Ordinary roles and the normal migrator remain open-only. Closed never
      grants entry. Preserve 55000 unavailability versus AM001 contradiction.
- [ ] Keep runtime_read_migrator_metadata's SQL and ACL unchanged. Existing
      Status retains read-only access without any advisory/session marker.
      Existing history trigger and runtime_finish_write use the common helper;
      no second finish API or history bypass is introduced.
- [ ] Exit only after the per-version write marker is inactive. Revalidate the
      exact wake session and lock, delete its marker, unlock once and prove both
      migrator markers empty and shared lock absent. Never complete the wake.

## Task 3: Bind sources before opening a backend

- [ ] Validate every embedded source against one compiled entry containing its
      positive version, exact SHA-256 of full source bytes and explicit
      wake_safe classification. Reject missing/extra/duplicate versions,
      duplicate sources, hash mismatch, non-SQL and nontransactional source.
      Reuse protected framing validation; never classify by a SQL keyword scan.
- [ ] Run source validation before catalog preflight, provider construction or
      backend acquisition. Prove rejected sources execute zero SQL even with an
      otherwise valid pool or a pool that would fail on acquisition.
- [ ] Under runtime entry and the Goose lock, validate contiguous history and
      every pending candidate's wake_safe=true before any ApplyVersion runs.
      Already-applied false entries are accepted; any pending false entry
      rejects the full pending set without applying its earlier safe prefix.
- [ ] Test exact-byte changes, truncated source, mismatched version, duplicate
      entry, absent classification, Go migration, NO TRANSACTION and missing
      framing. Classification remains part of existing migration review, with no
      new approval workflow or mutable override.
- [ ] Give root the ordered production source list and suggested classifications
      with reasons. Root inspects authority-changing Up work, computes hashes
      from final bytes and adds literal entries. Do not auto-mark sources safe.

## Task 4: Compose ApplyWake with B3 cleanup

- [ ] Add the private fixed wake locker path using existing backend capture,
      identity, Goose locking, PID binding and bounded retirement primitives.
      Capture physical-close capability before identity or SQL. Enter wake
      first, acquire Goose second, then validate closing-specific metadata via
      the existing accessor.
- [ ] Reject fresh, pre-foundation, missing/foreign history, enforcement-zero
      and adoption-required databases. Never route ApplyWake to bootstrap,
      adoption or normal Apply. Preserve normal Apply and Status behavior.
- [ ] Execute one ApplyVersion per protected version on a pinned backend.
      Rederive active wake on every new backend; never reconnect or retry inside
      a failed version. Preserve the existing exact clean already-applied
      handling and stop when cleanup contributes an error.
- [ ] Clean success and no-pending paths release Goose then runtime locks, reset
      local impersonation, verify marker/lock absence and physically retire the
      backend. Keep the caller's *sql.DB open and report no artificial cleanup
      error. Ambiguity retires the exact socket before returning its error.
- [ ] Report the minimal ApplyWake wrapper and static-guard allowlist edits to
      root. The guard must permit only the exact new protected composition;
      retain all raw provider/bulk migration denials and existing regressions.

## Task 5: Prove wake isolation and transport failures

- [ ] Cover both modes, malformed/missing/wrong/completed wake, stale current
      controller generation, invalid write generation, every nonterminal replica
      state, transition blocker, retained partition-flag inconsistency and
      history tampering.
- [ ] Exercise wrong direct login, SET ROLE, hostile search path, marker
      relation/type/index collisions, extra rows, wrong
      PID/role/generation/mode, mixed markers, guard reuse, active write exit
      and repeated unlock.
- [ ] Zero, one and multiple pending safe versions preserve gate, capacity, wake
      parent/steps, partition/debt, membership and accepted-writer time. Each
      committed version advances write generation once. Failure rolls back
      business/schema/history and generation together.
- [ ] Ordinary app/maintenance/lifecycle/proof and normal migrator calls still
      reject closing. Test actual protected DML and finish through the wake
      branch; the session marker alone cannot manufacture a per-version entry.
- [ ] With independent pinned sessions, observe complete waiting on the
      migration's shared barrier. Cancel and join contenders. No race is proved
      with an arbitrary sleep or by holding the wrong lock.
- [ ] Inject entry, version statement, commit response, Goose unlock, runtime
      unlock, identity reset and cleanup failures. Observe the application's
      actual socket close before return using the existing fault seam. A lost
      response cannot cause a second execution, clean reuse or pool closure.
- [ ] Verify Status under closing and a held migration lock remains read-only
      and takes no advisory lock. Confirm the frozen 110-object adoption
      manifest is unchanged. Upgrade populated 24 to 25 without unrelated row
      changes and prove migration failure restores prior functions and grants.

## Task 6: Verify and release

- [ ] Run from apps/server with the public fixture DSN:

```sh
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./migrations -run '^(TestRuntimeWakeMigrator|TestMigratorWake|TestMigrationCaller|TestMigrationProvider)'
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./migrations -run '^TestMigrator(Session|Apply|Status)'
GOGC=50 golangci-lint run ./migrations --tests
```

- [ ] Record all changed paths, exact red/green evidence, source admission,
      role/lock/state and physical-retirement results, shared edits and
      remaining boundaries in
      `.dev/phase-10/runtime-wake-migrator-author-report.txt`. Release all owned
      paths. Root inspects/reruns key checks, updates the plan and owns broader
      gates, gitleaks, staging and commit.

The full wake sequence still needs R8's prerequisite ordering and writer
coverage. No SQL readiness boolean or final-stop authority is added here.
