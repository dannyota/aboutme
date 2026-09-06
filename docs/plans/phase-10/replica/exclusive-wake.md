# R1.12 exclusive wake implementation plan

> For workers: use superpowers:subagent-driven-development or
> superpowers:executing-plans within AGENTS.md ownership. ADR 0024 keeps one
> author per task and one fresh phase review; it supersedes per-task review
> defaults.

**Goal:** Begin and complete an exact wake through the exclusive write barrier,
with immutable replay and no result authority on transport error.

**Architecture:** Migration 24 follows membership evidence 23. It installs the
two fixed wake entries/actions, private marker/guard and bounded assertion
branches. The private store captures the physical connection before SQL, calls
entry first and commits immediately after its matching action. Protected wake
migration remains a separate later slice; R8 cannot compose wake until both
pass.

**Tech stack:** PostgreSQL 18, Goose, Go, pgx and sqlc; repository pins apply.

**Spec:** [Exclusive entry](../../../design/scaling/lifecycle-write-entry.md),
[wake operations](../../../design/scaling/wake-operations.md),
[wake migrations](../../../design/scaling/wake-migrations.md),
[lifecycle controller](../../../design/scaling/lifecycle-controller.md),
[replay](../../../design/scaling/lifecycle-replay.md),
[vectors](../../../design/scaling/lifecycle-vectors.md),
[write entry](../../../design/scaling/transaction-entry.md),
[migrator](../../../design/scaling/migrator.md) and
[physical retirement](../../../design/scaling/migrator-retirement.md).

## Ownership and acceptance

This slice contributes to AC-INF-009 and Task 10.18's lifecycle/write-barrier
rows. It does not install ApplyWake, source admission, final stop, local
readiness, controller composition or cloud resources.

Root assigns one Sol author after migration 23 is accepted and committed:

- `apps/server/migrations/00024_runtime_exclusive_wake.sql`;
- `apps/server/migrations/runtime_exclusive_wake_test.go`;
- `apps/server/migrations/runtime_exclusive_wake_helpers_test.go`;
- `apps/server/sql/runtime_wake.sql`;
- `apps/server/internal/store/runtime_wake.go`;
- `apps/server/internal/store/runtime_wake_test.go`;
- `apps/server/internal/store/runtime_wake_live_test.go`;
- `apps/server/internal/store/runtime_wake_retirement_test.go`.

The author must not have designed or reviewed this slice. Root owns earlier
migrations, generated files, shared store/migrator/pgtransport sources and
fixtures, configuration, plans and Git. Report any needed shared edit before
dependent work; do not modify the frozen version-13 adoption manifest.

Use isolated harness databases in the one shared container and the public
fixture DSN. One heavy command at a time. Bound every setup, query, race and
cleanup. Cancel and join all goroutines and processes before fixture cleanup. No
shared/native migration, container reset, secret read, cloud action, dependency
change or caller wiring.

## Fixed surface

The SQL surface is exactly runtime_enter_begin_wake(text,text),
runtime_enter_complete_wake(text), runtime_begin_wake(bigint,text,text,bigint)
and runtime_complete_wake(bigint,text,bigint). Only direct
aboutme_lifecycle_command executes them. Inputs and existing
runtime_capacity_result order remain fixed by the entry contract.

Implement LifecycleWakeStore, NewLifecycleWakeStore, WakeModeServing and
WakeModeMaintenance exactly as specified. Reuse RuntimeCapacityResult from
migration 22's store contract. Every returned field is required for wake;
WriteGate and WriteGeneration pointers must both be nonnil. There is no exported
runner, callback, driver handle, command selector, gate override or retry
method.

## Task 1: Prove the surface is absent

- [ ] Before migration 24 exists, run a bounded test reporting all four missing
      exact signatures:

```sh
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./migrations -run '^TestRuntimeExclusiveWakeSurfaceExists$'
```

- [ ] Add failing state, replay, marker/guard, role and transport cases before
      their implementation. Require the intended SQLSTATE with otherwise-valid
      fixtures. Do not claim an unrelated fixture error proves a rejection.

## Task 2: Install exclusive entry and containment

- [ ] Frame migration 24 with runtime_begin_migration_write first and
      runtime_finish_write last; Down is inert. Reuse existing result types,
      lifecycle digest helpers and runtime_sample_lifecycle_time.
- [ ] Each fixed entry checks exact session_user, printable operation ID and
      fixed mode before locks. Take the exclusive transaction barrier first;
      never infer authority from current gate or acquire it after a row lock.
- [ ] Create or validate the exact owner-created temporary lifecycle marker with
      ON COMMIT DELETE ROWS. Pin every column/type/nullability, constraint,
      index, trigger, owner and ACL. Reject hostile relation/type/index names,
      extra rows or catalog ambiguity without adoption, repair or drop.
- [ ] Bind PID, xid8, session-role OID, literal command and operation ID. Begin
      stores its fixed mode; complete derives mode once from the immutable
      parent and predecessor before durable DML. Empty valid tables are
      reusable.
- [ ] Reject ordinary write rows, ordinary finish guards, migrator session rows
      and existing wake rows/guards before establishing new authority. Update
      ordinary/migrator entry coexistence checks in this forward migration;
      preserve their existing open-gate behavior and immutable earlier SQL.
- [ ] Implement the fixed nonwaiting finish guard and explicit private finish
      helper. Only that helper sets finished after durable work. The deferred
      trigger merely asserts finish; forcing it never completes a transaction.
- [ ] Replace the bounded statement assertion with ordinary behavior unchanged
      and only the four accepted wake table/operation pairs. Check table schema
      as well as name. It must not inspect NEW. Business assertions never admit
      wake or extend the accepted-writer timestamp.
- [ ] Add owner BEFORE ROW checks for exact wake parent/step operation, command,
      workflow and mode. Complete cannot insert a parent. Preserve existing
      ledger immutability, typed result and predecessor assertions.

## Task 3: Implement fresh actions and immutable replay

- [ ] After exclusive entry, lock public_state, capacity, operation parent and
      matching action. Lock write_state last only after proving the action
      fresh. Never lock replica, transition, rate, business or receipt rows.
- [ ] Fresh begin requires the exact closed/offline/nonadmitting state,
      protected migration metadata, desired one, zero nonterminal replicas, both
      partitions disabled and no closing/unresolved transition. Validate exact
      expected controller/write generations and all generation bounds.
- [ ] Fresh complete requires its exact wake parent/begin result and the fixed
      closing/starting/nonadmitting matrix. Validate stored result digest,
      predecessor relation, current generations, protected metadata and the same
      zero-work/disabled-partition predicates.
- [ ] Validate all 48 rate partition identities and uniform evidence with
      nonlocking reads under capacity. No wake action changes partitions, debt,
      membership or accepted-writer time. Replay skips current partition/state
      predicates and returns only its retained result.
- [ ] Sample the existing lifecycle clock once after all required locks. Clamp
      to capacity, write-state updated/last-write, parent and predecessor times.
      Fresh begin/complete each advance capacity, controller and write
      generations once, store exact post-action fields and write write_state
      last. Bookkeeping records lifecycle and operation ID.
- [ ] Use the published argument/result field order and literal wake hashes.
      Preserve argument-mismatch AM002 versus independently provable stored
      result/marker corruption AM001. Exact replay changes no durable row and
      finishes only its private temporary marker.
- [ ] No R8 prerequisite boolean or SQL migration/deployment claim is added. Use
      valid owner fixtures for represented state. Actual ApplyWake and the
      serialized prerequisite sequence are proved in their later slices.

## Task 4: Prove SQL containment and concurrency

- [ ] Cover fresh begin/complete gate/state/generation boundaries, both modes,
      immutable mode conflict, missing predecessor, all stored result fields,
      zero/nonzero replica states, partition corruption and generation overflow.
- [ ] Replay after later gate, write/controller/capacity generation and current
      partition changes returns historical results without touching write_state.
      Prove no new parent/step/generation on replay, rejection or rollback.
- [ ] Exercise wrong session role, direct helper/table access, exact owners,
      ACLs/search paths and hostile catalogs. Every fixed error keeps supplied
      identifiers, digests and evidence out of diagnostics.
- [ ] Cover command/operation/mode/parent mismatches, mixed entry modes, foreign
      and repeated guards, forced constraints, DISCARD TEMP, savepoint rollback,
      early/repeated finish, unfinished commit and business-table writes.
- [ ] Use independent pinned sessions and observed lock waits to prove ordinary
      writers and wake serialize at the outer barrier. Verify no reverse wait on
      held transition/rate/replica rows and no inner lock before entry.
- [ ] Upgrade populated version 23 to 24 preserving business, membership,
      evidence, lifecycle, transition, claims and rate debt. Inject a later
      migration failure and prove functions/triggers/grants and generation roll
      back together. Preserve the ordinary migrator and read-only Status path.

## Task 5: Generate and implement the private runner

- [ ] Add exact fixed entry/action queries. Each action uses one MATERIALIZED
      call and explicit casts or value/presence fields. Reject every missing
      required attribute rather than substituting zero, false or empty text.
- [ ] Request root generation after SQL/query sources are ready. Root inspects
      generated parameters/results before dependent store implementation. Never
      hand-edit generated query methods or interfaces.
- [ ] Write failing tests for nil/malformed input, method/argument binding,
      entry-first order, one action evaluation, required NULLs and all error
      paths. Acquire a pool lease and capture pgtransport.Capability before
      Begin. Call only the matching entry, action, then immediate Commit.
- [ ] Decode/copy inside the private transaction and expose a result only after
      successful commit and clean release. Never call ordinary enter/finish,
      invoke an application callback or retry internally. Prove copied values
      survive actual connection reuse.
- [ ] Confirm definite allowed-error rollback before clean reuse. AM001,
      ambiguous entry/action/rollback/cleanup and every commit error physically
      retire the exact connection. Preserve primary and cleanup error causes;
      caller-owned pool remains open. Every returned error has the zero result.
- [ ] Use the accepted transport seams to prove actual socket closure before
      return for lost commit/rollback responses and failed cleanup. IsClosed or
      logical lease removal alone is insufficient. Bound cleanup independently
      of canceled caller context and join the fault fixture.

## Task 6: Verify and release

- [ ] Run from apps/server with the public fixture DSN:

```sh
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./migrations -run '^TestRuntimeExclusiveWake'
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./internal/store -run '^TestRuntimeExclusiveWake'
GOGC=50 golangci-lint run ./migrations ./internal/store --tests
```

- [ ] Report exact red/green evidence, all eight changed paths,
      state/replay/role/lock/retirement proof and any shared edit or boundary in
      `.dev/phase-10/runtime-exclusive-wake-author-report.txt`. Release all
      owned paths. Root inspects/reruns key checks and owns generated/broad
      gates, plans, gitleaks, staging and commit.

ApplyWake and its source registry remain a separate implementation after this
slice. No controller may compose a partial wake sequence. Final-stop authority
remains absent until R8's complete writer coverage and local proof pass.
