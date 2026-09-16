# R1.10 ordinary lifecycle operations implementation plan

> For workers: use superpowers:subagent-driven-development or
> superpowers:executing-plans within AGENTS.md ownership. ADR 0024 keeps one
> author per task and one fresh phase review; it supersedes per-task review
> defaults.

**Goal:** Add the six fixed controller actions with immutable replay results,
exact replacement evidence and atomic fleet partition changes.

**Architecture:** Migration 22 follows accepted rate operations 21. It uses the
membership ledger from 15, transition visibility from 16 and rate partitions
from 18. One generated query per store method runs inside WriteTxRunner.
Graceful leave and EC2 proof landed in the membership evidence slice. Exclusive
wake and caller composition are deferred under ADR 0036.

**Tech stack:** PostgreSQL 18, Goose, Go, pgx and sqlc; repository pins apply.

**Spec:**
[Controller contract](../../../design/scaling/lifecycle-controller.md),
[lifecycle](../../../design/scaling/lifecycle-operations.md),
[replay](../../../design/scaling/lifecycle-replay.md),
[digest vectors](../../../design/scaling/lifecycle-vectors.md),
[membership](../../../design/scaling/replica-membership.md),
[rate storage](../../../design/scaling/rate-storage.md) and
[write entry](../../../design/scaling/transaction-entry.md).

## Ownership and acceptance

This slice contributes to AC-INF-009 and Task 10.18's membership, capacity and
lifecycle rows. It does not complete R1/R8 or permit hosted scaling. R8 owns
trusted identity, node readiness, shared-RDS failure suppression and all
external effects.

Root assigns one Sol author after migration 21 is accepted and committed:

- `apps/server/migrations/00022_runtime_lifecycle_operations.sql`;
- `apps/server/migrations/runtime_lifecycle_operations_test.go`;
- `apps/server/migrations/runtime_lifecycle_operations_helpers_test.go`;
- `apps/server/sql/runtime_lifecycle.sql`;
- `apps/server/internal/store/runtime_lifecycle.go`;
- `apps/server/internal/store/runtime_lifecycle_test.go`.

The author must not have designed or reviewed this slice. Root owns generated
files, existing migrations/query sources/store adapters, shared harnesses,
plans, configuration and Git. Report required shared-file edits. Do not change
registration, claim or rate implementations to make controller fixtures pass.

Use isolated harness databases in the one shared container, the public fixture
DSN and one heavy command at a time. Bound setup, waits and cleanup; cancel and
join all concurrent work. No container recreation, shared/native migration,
secret read, cloud call, dependency change or caller wiring.

## Fixed interfaces

Implement only runtime_prepare_scale_out, runtime_activate_replica_capacity,
runtime_prepare_scale_in, runtime_finish_scale_in,
runtime_prepare_maintenance_drain and runtime_begin_replica_termination. Keep
the exact SQL signatures, nullable inputs and result composites in the
controller contract. Only lifecycle-command executes these wrappers.

Implement RuntimeLifecycleTransport and NewRuntimeLifecycleTransport exactly as
specified there. Reuse registration's RuntimeReplicaResult. Define
RuntimeCapacityResult in the new store file for later wake reuse. Its write
gate/generation pointers are absent for prepare-scale-out. Finish-scale-in is
the only ordinary replica result with present count/partition pointers.

Add the owner-only runtime_lifecycle_digest_field, runtime_lifecycle_digest and
runtime_sample_lifecycle_time helpers with their exact accepted signatures.
Additional private helpers may remove repeated fixed SQL; grant none to a login.
Add no callback, generic action selector or caller digest/time/result input to a
public interface.

## Task 1: Prove the missing surface

- [x] Add a bounded live existence test for the six exact signatures. Observe
      all six missing before migration 22 exists:

```sh
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./migrations -run '^TestRuntimeLifecycleOperationsFunctionsExist$'
```

- [x] Add failing state, role, replay and malformed-input cases before each
      rule. Use complete valid fixtures and assert the intended SQLSTATE;
      unrelated errors do not prove the case.

## Task 2: Install the fixed ledger and action operations

- [x] Enter runtime_begin_migration_write('migration-00022') first and finish
      last. Down is inert. Preserve existing business/runtime rows. Add no seed,
      table, role, migration-history edit or generation reset.
- [x] Use runtime-owner SECURITY DEFINER functions, search_path pg_catalog,
      qualified static SQL and explicit direct session_user checks. Revoke
      PUBLIC and every non-lifecycle login from the six wrappers. No login
      executes helpers, alters their definitions or gains runtime table DML.
- [x] Validate every scalar, nullable branch and source bound before mutation.
      Return 22023 for malformed inputs, 42501 for wrong role, 55000 for
      unavailable/stale state, AM002 for supplied replay/identity conflict and
      AM001 only for independently provable stored corruption. Errors contain no
      supplied identity or evidence values.
- [x] Lock public_state, capacity and the operation parent before action rows,
      then needed replicas in UUID order, immutable evidence and partitions.
      Never lock or wait on a transition parent; use accepted nonlocking visible
      predicates. Do not perform external I/O under database locks.
- [x] Infer fresh workflow only through the accepted first-action graph. Finish
      and maintenance drain require an existing compatible parent. Validate each
      predecessor's action, target and generation. An intervening controller
      action may advance generation; require predecessor result at most expected
      and expected equal current for a fresh action.
- [x] For replay, compare the supplied canonical argument digest and retained
      fields. Any supplied digest mismatch is AM002. Do not claim full original
      argument reconstruction from schema 15. Recompute the stored result hash
      from every retained result field and reject corruption with AM001. Return
      historical results after later membership/capacity changes; no current
      prerequisite check, row mutation or generation increment on exact replay.
- [x] Install canonical helpers and compute all ten published vectors from
      literal SQL values, including the later wake encodings without installing
      wake actions. An independent Go test encoder must match every byte count
      and SHA-256. Include typed nulls, both replacement variants, both wake
      modes, result_kind and the repeated header/result fields. No runtime Go
      digest API or generated expected hash may replace fixed literals.
- [x] Sample runtime_sample_lifecycle_time once after needed locks. Clamp to the
      accepted prior evidence. Fresh actions increment capacity/controller
      generations once, set both operation evidence fields to operation_id and
      persist one immutable action result. Detect bigint exhaustion before
      addition; reject/rollback/replay changes neither generation.
- [x] Lock all 48 rate-partition rows in partition/policy order when an action
      reads or changes allocation. Validate the exact catalog and uniform
      enabled/generation/operation evidence per ordinal. Update all 24 rows of a
      changed ordinal atomically. Preserve every active_keys count, bucket,
      overflow row and pending attempt.
- [x] Prepare scale-out requires one active serving replica, desired one,
      partition 1 enabled and partition 2 disabled. It sets desired two only.
      Second activation requires its exact predecessor and enables partition 2.
- [x] Initial and booked first serving activation require desired one, zero
      active serving and both partitions disabled. Exact joining/ready identity
      and full task trio are required. A retained enabled partition 1 requires a
      nonnull exact fenced predecessor; consume that predecessor once and change
      neither flag. Support one or two separately bound replacements while
      active count remains below unchanged desired.
- [x] Maintenance activation requires its completed maintenance wake and zero
      other nonterminal maintenance replica: joining, active, draining or
      terminating. Retained left/fenced history does not block it. It changes
      neither desired nor partitions. Maintenance drain requires that activation
      and changes only its exact active target to draining.
- [x] Prepare scale-in selects an exact active serving target at desired two
      with both flags enabled. Keep desired and flags while draining. Finish
      requires the prepared target, exact graceful leave plus proof or abrupt
      fence plus intent/proof, and zero or one survivor with others terminal.
      Set desired one and disable partition 2 while retaining all debt.
- [x] Termination records one immutable request/identity/reason intent and
      changes only the selected incarnation to terminating. Enforce joining for
      startup_failed, active for readiness_failed and prepared draining for
      drain_failed. SQL proves state/evidence, while R8 proves node-specific
      readiness failure after shared-RDS suppression. Add no death inference,
      claim release, transition ack, leave or public readiness authority.

## Task 3: Prove replay, state, grants and races

- [x] Cover every action/workflow branch and every required predecessor. Reject
      wrong first action, skipped/cross-kind/post-terminal action, conflicting
      operation ID, changed arguments and consumed replacement. Replay after
      later successful actions returns all original fields.
- [x] Build valid owner fixtures with corrupt retained result hashes/shapes and
      require AM001. Test changed supplied arguments and a mismatching
      well-shaped stored argument digest as AM002. Do not disable triggers or
      make a malformed fixture fail on an unrelated constraint.
- [x] Exercise all input bounds, nullable inputs, reason/state combinations,
      exact identity/evidence mismatches and result matrices. Prove no partial
      intent, parent, step, membership, generation or partition change on error.
- [x] Prove one and two fenced replacements, consume-once predecessor and final
      slot bounds. Reject null-replacement first activation when partition 1
      remains enabled after node loss. Test either scale-in target, graceful and
      abrupt completion, target-dead/survivor-dead/both-dead and historical
      zero-survivor replay.
- [x] Test all-24 partition consistency and exact disabled/enabled branches.
      Test every nonterminal maintenance blocker and retained terminal history.
      Populate ordinary/overflow debt and P22 pending rows and prove every
      lifecycle change preserves them. Maintenance changes no serving flags.
- [x] Pin future timestamps and generation boundaries. Use only the accepted
      isolated time-helper replacement: rollback one-session fixtures; replace
      before opening pools in a disposable database, then close/join and restore
      or drop. No runtime setter or shared/native replacement.
- [x] Connect as every real named login. Prove lifecycle success and every
      cross-role/helper/table denial with 42501, exact owner/search-path/ACL
      metadata and hostile search-path resistance. No app or proof role can
      forge controller, leave or fencing evidence through this surface.
- [x] Use independent pinned pools and observed lock waits for competing fresh
      operation IDs, final serving activation slot, replacement predecessor
      reuse, concurrent scale-in/replacement, and rate allocation versus a flag
      change. Assert the exact loser and final rows. Hold a transition parent
      and prove lifecycle does not wait on it. No sleeps as race proof.
- [x] Upgrade a populated version 21 fixture explicitly to 22. Preserve
      business, membership, transition, claim, rate and receipt rows. An
      injected failure rolls back all new functions/grants and write-generation
      advance.

## Task 4: Add generated queries and store transport

- [x] Add six queries in the owned SQL source. Each uses one MATERIALIZED
      function call and explicit scalar casts/value-presence fields. Preserve
      nullable replacement and leave inputs with sqlc.narg; no composite
      expression expansion or inferred nullability.
- [x] Request root generation when SQL/query sources are ready. Inspect the
      resulting parameter and row types before dependent store implementation.
      Root owns all generated files and shared changes.
- [x] Write failing tests for six method mappings and all result fields.
      Validate action shapes, fixed kind/state, positive generations, count
      bounds and pointer presence. Copy decoded values before leaving the runner
      callback.
- [x] Use one WriteTxRunner call and one generated query per method. Return
      exact zero result on nil/malformed input, query/decode/finish/commit/
      cancellation/cleanup error. Preserve causes and existing physical
      retirement. Never retry internally or return speculative success.
- [x] Prove real one-call execution, rollback versus committed state, result
      ownership after connection reuse and zero authority under ambiguous
      completion. Keep the interface free of callbacks and driver handles.

## Task 5: Verify and release

- [x] Run from apps/server with the public fixture DSN:

```sh
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./migrations -run '^TestRuntimeLifecycleOperations'
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./internal/store -run '^TestRuntimeLifecycle'
GOGC=50 golangci-lint run ./migrations ./internal/store --tests
```

- [x] Report exact red/green evidence, action/role/replay/lock coverage, changed
      paths, generation needs and concrete boundaries in
      `.dev/phase-10/runtime-lifecycle-operations-author-report.txt`. Release
      all six files. Root inspects/reruns key checks and owns sqlc, broad
      migration/database/Go gates, staging, gitleaks and commit.

Stop at a concrete contract conflict. Do not add wake, graceful leave, proof,
fenced cleanup, transition recovery, final stop or caller composition to make
fixtures pass. Use protected owner fixtures for their already accepted durable
rows until the corresponding operation slice exists.
