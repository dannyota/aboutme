# R1.9 fixed rate operations implementation plan

> For workers: use superpowers:subagent-driven-development or
> superpowers:executing-plans within AGENTS.md ownership. ADR 0024 keeps one
> author per task and one fresh phase review; it supersedes per-task review
> defaults.

**Goal:** Enforce the fixed fleet rate policies and bounded cleanup through
database operations that preserve debt, time and result authority.

**Architecture:** Migration 21 follows accepted claim operations migration 20.
It uses schema 18 unchanged and installs the nine fixed functions and seven
result types. A scalar store transport owns each write transaction. R5 retains
canonical key encoding, policy/domain types and caller operation state.

**Tech stack:** PostgreSQL 18, Goose, Go, pgx and sqlc; repository pins apply.

**Spec:** [Rate operations](../../../design/scaling/rate-operations.md),
[rate storage](../../../design/scaling/rate-storage.md),
[OAuth attempts](../../../design/scaling/admission-attempts.md),
[policy catalog](../../../design/scaling/policy-catalog.md),
[rate identities](../../../design/scaling/rate-identities.md),
[admission](../../../design/scaling/admission.md) and
[write entry](../../../design/scaling/transaction-entry.md).

## Ownership and acceptance

This slice contributes to AC-INF-009 and Task 10.18 fleet limits. It does not
complete R1/R5/R7, change HTTP/MCP responses, or authorize multi-replica or
hosted use. Existing callers remain local until their separate integration tasks
pass.

Root assigns one Sol implementation author after migration 20 is committed:

- `apps/server/migrations/00021_runtime_shared_rate_operations.sql`;
- `apps/server/migrations/runtime_shared_rate_operations_test.go`;
- `apps/server/migrations/runtime_shared_rate_operations_helpers_test.go`;
- `apps/server/sql/runtime_rates.sql`;
- `apps/server/internal/store/runtime_rates.go`;
- `apps/server/internal/store/runtime_rates_test.go`;
- `apps/server/internal/store/runtime_rate_maintenance.go`;
- `apps/server/internal/store/runtime_rate_maintenance_test.go`.

Root delegates only the new SQL source. Existing migrations, shared helpers,
generated files, plans, config, manifests and Git remain root-owned. Request
root generation after releasing SQL/query sources, before dependent store code.
Designers and reviewers of rate operations may not implement this slice.

No stored schema extension, key/version input, encoder, role, dependency, caller
composition, lifecycle operation, container/native migration, cloud action or
secret read. Use isolated harness databases in the shared container and the
public fixture DSN. One heavy command at a time; bound all setup, lock waits and
cleanup. Cancel and join every concurrent test operation.

## Interfaces

RuntimeRateTransport has seven context-first scalar methods matching the exact
SQL input order: AdmitToken, FailureState, RecordFailure, ClearFailureSuccess,
AdmitChangedSlug, ReserveFailedGrant and FinishAttempt. Use string
policy/outcome, [32]byte digest and uuid.UUID attempt/client values. It accepts
no R5 type, caller timestamp or driver handle. NewRuntimeRateTransport takes
`*pgxpool.Pool`.

Decoded result types mirror the seven accepted SQL composites with Runtime
prefixed names. Preserve nullable partition, bucket kind and stored outcome
explicitly with pointers. Validate the per-method presence/value matrix before
returning an owned result. No Policy, RateKey or attempt-domain type lives here.

RuntimeRateMaintenanceStore separately exposes context-first CleanupRateBuckets
with policy and int32 page size, and CleanupAdmissionAttemptReceipts with int32
page size. NewRuntimeRateMaintenanceStore takes `*pgxpool.Pool`. Its result
counts are nonnegative and no larger than the requested page. It exposes no
admission.

All methods run through WriteTxRunner, execute one generated function and return
the exact zero result on any returned error. Panic follows bounded cleanup and
rethrow. No transport retries or treats a decoded value as committed authority.

## Task 1: Prove missing fixed functions

- [x] Add TestRuntimeSharedRateOperationsFunctionsExist. Check all nine exact
      signatures and seven composite field orders/types. Observe the expected
      missing objects before migration 21 exists:

```sh
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./migrations -run '^TestRuntimeSharedRateOperationsFunctionsExist$'
```

- [x] Add failing role, arithmetic, state and concurrency cases before their
      implementation. Complete valid fixtures must fail on the intended SQLSTATE
      or named assertion; arbitrary database errors are not evidence.

## Task 2: Install fixed operations and time

- [x] Use protected first-entry/last-finish migration framing with inert Down.
      Preserve all existing rows and generations except one migration write
      generation advance. Add only the accepted result types, wrappers/helpers
      and exact execution grants; no runtime table or configuration field.
- [x] Make all objects runtime_owner-owned. Revoke PUBLIC type usage and
      function execution. Fix SECURITY DEFINER/search_path=pg_catalog and
      qualified SQL. App executes the seven admission operations; maintenance
      executes only the two cleanup operations. Check direct session_user before
      mutable reads. No STRICT wrapper, role-membership fallback or direct DML.
- [x] Explicitly validate NULL, digest length, UUID, page and outcome inputs.
      Preserve 22023 invalid shape, 42501 wrong role, 55000 caller policy or
      algorithm mismatch/unavailability, AM002 retained attempt conflict and
      AM001 installed state corruption. Fixed errors disclose no private values.
- [x] Add owner-only runtime_sample_rate_time(), a no-argument volatile helper
      with the exact clock_timestamp body and no login grants. Every operation
      locks its policy clock before sampling once and clamping to high_water_at.
      Persist raw/effective observations and bounded anomaly count; any chosen
      jump threshold affects observation only. No caller time or runtime setter.
- [x] Preserve clock, numeric partitions, bucket/overflow and ordered-attempt
      lock order. Use nonlocking routing/candidate reads and revalidate after
      locking. Never take a partition after a bucket or lock membership,
      transitions, users, resumes, clients, tokens or emails from a rate
      function.

## Task 3: Enforce all algorithms and allocation

- [x] Token operations accept only fixed token policies. Use integer numerator
      units and the accepted saturation-before-multiplication arithmetic. Admit
      consumes one whole token; denial consumes none. Return retry zero on allow
      and positive exact ceiling on denial. Cap long forward jumps and clamp
      backward samples without debt or idle reset.
- [x] Existing keys remain charged in disabled partitions. New keys first run
      only bounded legitimate expiry, at most one eligible oldest row per full
      enabled partition, then choose the lowest enabled partition below 10000.
      Insert/decrement/increment counts atomically. If no partition owns the new
      key, evaluate shared overflow; capacity pressure never grants work.
- [x] P07 State allocates/sweeps nothing and changes only selected last_seen:
      existing private and saturated/no-enabled overflow refresh activity;
      absent routable private returns unexhausted with null partition and no
      activity. Record preserves first-window/nonextension and exact count ten.
      Clear deletes only an existing private bucket, with count decrement; an
      absent clear never reads or changes overflow.
- [x] P12 prunes the exact rolling-hour cutoff, admits below thirty and retains
      the newest denied attempt by replacing the oldest at thirty. Keep the
      array bounded and ordered. Every denial returns Retry-After one.
- [x] P22 derives the accepted unkeyed SHA-256 client key. New reserve counts
      failures plus pending debt, starts the first fifteen-minute window and
      denies without inserting an attempt at ten. Expiry resolves only the
      selected bucket's pending rows; unrelated expired pending debt stays
      bound.
- [x] Exact retained reserve replay checks attempt/client identity and returns
      historical allowed shape with replayed=true for pending or terminal rows,
      even after private bucket deletion. It never reroutes or creates debt.
      Fresh allow/deny has replayed=false. No lookup or retry function is added.
- [x] Finish implements failure conversion, neutral-only release, atomic private
      success/sibling clear, and overflow success that preserves other debt.
      Restore an empty window only when count and stored pending are zero.
      Preserve exact caller replay, system no-op for every later outcome and
      absent no-op after receipt GC. Never charge a later window from expiry.

## Task 4: Implement bounded cleanup

- [x] Rate cleanup accepts page 1..256, discovers candidates without locks, then
      locks partitions numeric, ordinary digests, overflow and selected P22
      attempts in accepted order. Recheck each candidate and decrement counts
      only with deletion. The idle backstop never detaches effective P22 debt.
- [x] Normalize overflow only through matured token refill, fixed expiry or
      rolling pruning. Resolve expired P22 pending rows through the accepted
      path. Never delete overflow, erase live debt by idle age, synthesize an
      admission or add a global cleanup operation.
- [x] Return policy_idle only when no ordinary row, neutral overflow and no P22
      pending row remain. Terminal receipts do not block it. A partial page or
      remaining debt returns false. This predicate lasts only through commit.
- [x] Receipt cleanup deletes at most page_size terminal rows at the accepted
      twenty-four-hour cutoff in terminal_at/UUID order. It locks clock then
      receipts, never a bucket or partition, and excludes pending rows. Both
      cleanup functions return one valid zero-count row when no work exists.

## Task 5: Prove SQL and store boundaries

- [x] Test every policy's exact refill/consume/deny/retry boundaries and checked
      arithmetic, backward and long forward clocks; all P07 State/Record/Clear
      routes; P12 29/30/31 and repeated denied debt; and the full P22 reserve,
      replay, outcome, expiry, sibling and retained-receipt matrices.
- [x] Prove 10000-key ownership, lowest enabled routing, both-disabled overflow,
      existing disabled debt, count equality, and both cleanup page/cutoff/idle
      boundaries. Cover overflow debt and policy_idle false until all debt is
      gone. Use complete fixtures with no disabled trigger or forged count.
- [x] Use independent pools and observed lock waits for same policy/key,
      allocation versus cleanup/clear, P22 success/failure/expiry and partition
      enable/disable. Independent policies must not serialize on rate locks.
      Assert final rows/debt/counts and exact losing outcomes after joined work.
- [x] Test actual named roles, fixed error privacy, all owners/ACLs/search
      paths, helper replacement denial and hostile search paths. Use the
      accepted disposable-database clock fixture for deterministic
      independent-pool cases; change its helper only before pool work and
      restore/drop after join.
- [x] Add nine one-call MATERIALIZED scalar queries with explicit nullable
      value/presence fields; request root sqlc generation before store work.
      Test every result matrix, NULL versus present zero, copied values after
      connection reuse and one real function execution per method.
- [x] Test entry/query/finish/commit order; malformed/nil input, driver/decode
      failure, cancellation, panic, AM001 physical retirement and commit
      ambiguity. Every returned error yields zero authority, even after a
      decoded allow, replay, clear or policy_idle result. Never retry.
- [x] Upgrade a populated version 20 fixture to 21 without changing prior
      business, membership, lifecycle, transition, claim or rate rows. Injected
      failure rolls back functions/types/grants and write generation together.

## Task 6: Verify and release

- [x] Run focused checks from apps/server:

```sh
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./migrations -run '^TestRuntimeSharedRateOperations'
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./internal/store -run '^TestRuntimeRate'
GOGC=50 golangci-lint run ./migrations ./internal/store --tests
```

- [x] Report exact red/green commands, policy/role/time/lock evidence, all
      paths, generation needs and unresolved boundaries in
      `.dev/phase-10/runtime-shared-rate-operations-author-report.txt`. Release
      all eight paths. Root inspects, reruns key checks and owns broader gates,
      staging, gitleaks and commit.

## Implementation evidence

Delivered in three commits: migration 21 with its tests and query sources, the
store transport, and the fresh review fixes. Two authors wrote the halves; the
integration owner reran every check before each commit and ran sqlc generation
between them.

### Checks at the final commit

| Check                                       | Result          |
| ------------------------------------------- | --------------- |
| `^TestRuntimeSharedRateOperations`, `-race` | 22 tests, 69.6s |
| `^TestRuntimeRate` store, `-race`           | 26 tests, 3.1s  |
| Whole `./internal/store`, `-race`           | 10.5s           |
| Whole `./migrations`, `-race`               | 251.6s          |
| `golangci-lint` migrations and store        | 0 issues        |
| `make sqlc-check`                           | no drift        |

### Fresh review

CLEAR WITH FINDINGS. All five invariants confirmed with cited evidence: role
isolation, no caller-controlled time, checked arithmetic, capacity pressure
never granting work, and zero authority on error. The reviewer independently
proved that token refill saturates before multiplication and cannot overflow,
and that a hostile caller search path cannot substitute its own clock.

Three defects were fixed and one open item was raised.

1. Two token helpers were declared IMMUTABLE while adding an interval to a
   timestamptz. No answer was wrong, because the interval carries its whole
   quantity in the microsecond field, but a later refactor to a day component
   would have made them timezone dependent under an unchanged label. Both are
   STABLE, and the tests now pin the declared volatility of all twenty-six
   installed functions.
2. Bucket cleanup read the stored algorithm and passed it back as the expected
   algorithm, so the catalog mismatch check compared a row to itself. Cleanup is
   policy agnostic by contract, so a NULL expectation now means any algorithm
   and the check still does real work for the eight callers that fix one. A test
   across all twenty-four policies would fail if cleanup named a concrete
   algorithm.
3. The two token helpers disagreed on the same numeric contract. Neither shape
   is reachable through the nine operations, so this is contract parity.

### Required follow-up, not yet done

`shared_rate_buckets` has no index supporting the `ORDER BY last_seen` expiry
candidate scan. Migration 18 gave `shared_admission_attempts` four
purpose-shaped indexes but gave this table none, so this reads as an omission.
With both partitions full, about twenty thousand keys, every new-key admission
reads every row of the partition twice while holding the exclusive
`shared_policy_clocks` row lock that serializes all work for that policy. That
turns key exhaustion into a throughput collapse of the rate limiter itself.

The index belongs in schema 18 as
`(policy_id, partition, last_seen, key_digest)`. Because migration versions must
stay contiguous, adding it as a new migration would take the number reserved for
lifecycle operations and shift every later reservation, while editing schema 18
in place consumes no number but requires recreating the native development
database. That choice is the owner's and is open. **This must land before UAT.**
Confirm with `EXPLAIN` at ten thousand keys.

### Planner dependency worth making explicit

`runtime_rate_bucket_eligible` is SECURITY DEFINER, never inlinable, and carries
PostgreSQL's default function cost. That default is what makes the planner apply
the cheap partition comparison before the expensive eligibility call, which is
why removing the partition column from the expiry index left the allocation scan
unchanged. The behaviour is correct by default rather than by declaration; an
explicit `COST` would make the dependency deliberate. The `EXPLAIN` test pins
the chosen plan, so a planner change fails loudly rather than regressing
silently.

### Open, lower priority

Migration 20 asserts write entry before checking the role, while migration 21
checks the role first. `runtime_require_write_entry` reads the mutable write
gate, so migration 21 matches the contract and migration 20 does not. The
reviewer recommends a later forward `CREATE OR REPLACE` for migration 20's five
definers rather than an in-place edit, since migration 21 builds on the database
migration 20 produces. Defense in depth, not a live vulnerability: the EXECUTE
grants already confine those functions to app and maintenance.

### Left to callers

R5 must prove that a replayed reserve cannot start work. The decoded reserve
result carries `Allowed` true alongside `Replayed` true, so a caller branching
on `Allowed` alone would grant work from a replay. An attempt UUID is also a
capability that clears real debt, so it must not be logged, echoed or reused.

R5/R7 prove canonical key shapes, ordered caller debt, replay rejection and
unchanged responses. R8 proves private key versions, composition and rotation.
These boundaries remain open after the database/store slice.
