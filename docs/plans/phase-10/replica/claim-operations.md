# R1.8 fixed claim operations implementation plan

> For workers: use superpowers:subagent-driven-development or
> superpowers:executing-plans within AGENTS.md ownership. ADR 0024 keeps one
> author per task and one fresh phase review; it supersedes per-task review
> defaults.

**Goal:** Acquire, resolve, promote and release durable claims through fixed
role-bound functions, with atomic capacity and no result authority on error.

**Architecture:** Migration 20 follows accepted registration migration 19. It
uses schema 17 unchanged, adds the eighteen-field runtime_claim_result type and
five fixed operations, and installs bounded maintenance receipt cleanup. Typed
store transport uses the existing write runner. R5 retains operation identity,
encoding, retry state and caller admission.

**Tech stack:** PostgreSQL 18, Goose, Go, pgx and sqlc; repository pins apply.

**Spec:** [Fixed operations](../../../design/scaling/claim-operations.md),
[shared claims](../../../design/scaling/shared-claims.md),
[claim identities](../../../design/scaling/claim-identities.md),
[membership](../../../design/scaling/replica-membership.md),
[write entry](../../../design/scaling/transaction-entry.md) and
[runtime schema](../../../design/scaling/runtime-schema.md).

## Ownership and acceptance

This slice contributes to AC-INF-009 and Task 10.18's fleet admission rows. It
does not complete R1/R5, prove local join, authenticate a process through shared
credentials, or authorize hosted work. Exact EC2 proof and its owner-only fenced
cleanup are a later serialized lifecycle slice; no caller can request fencing.

Root assigns one Sol implementation author after migration 19 is committed:

- `apps/server/migrations/00020_runtime_shared_claim_operations.sql`;
- `apps/server/migrations/runtime_shared_claim_operations_test.go`;
- `apps/server/migrations/runtime_shared_claim_operations_helpers_test.go`;
- `apps/server/sql/runtime_claims.sql`;
- `apps/server/internal/store/runtime_claims.go`;
- `apps/server/internal/store/runtime_claims_test.go`;
- `apps/server/internal/store/runtime_claim_receipts.go`;
- `apps/server/internal/store/runtime_claim_receipts_test.go`.

Root delegates only this new SQL source. Generated files, existing migrations,
shared helpers, plans, configuration, manifests and Git remain root-owned. Root
runs sqlc generation after the author releases query sources for generation.
Designers and reviewers of these operations cannot implement them.

No earlier migration edit, new stored field, policy change, HMAC encoder, key
read, ClaimOperation, retry loop, caller wiring, new dependency,
container/native migration or cloud action belongs here. Use isolated databases
in the shared container, the public fixture DSN and one heavy command at a time.
Bound setup, lock observation and cleanup; cancel and join test goroutines.

## Go boundary

RuntimeClaimTransport has five methods, each with context first and scalar
arguments in the exact SQL signature order: AcquireSingle, AcquireSSE, Promote,
Release and Resolve. Each returns (RuntimeClaimRow, error). Use uuid.UUID for
required UUIDs, *uuid.UUID for optional work, [32]byte for required digests and
*[32]byte for optional account digest. Enum arguments are validated strings.
NewRuntimeClaimTransport takes only *pgxpool.Pool.

RuntimeClaimRow mirrors the SQL result matrix with owned values. Outcome and
ClaimID are required values. Nullable identity, state, time, digest and count
fields retain presence explicitly. Use two fixed ordered scope slots; each
present slot has a kind, [32]byte digest and optional allocation ordinal. The
decoder validates scope_count against those slots. It never exports a driver
row, buffer, callback, transaction or Queries value.

RuntimeClaimReceiptStore is a separate maintenance interface. Its
GCReleasedClaimReceipts(context.Context) returns (RuntimeClaimReceiptGCResult,
error), with nonnegative DeletedClaimCount and DeletedScopeSummaryCount.
NewRuntimeClaimReceiptStore takes *pgxpool.Pool. It shares no acquire or fencing
entry point. A runner error returns the zero result for both interfaces.

## Task 1: Prove missing operations

- [ ] Add TestRuntimeSharedClaimOperationsFunctionsExist. Check all five exact
      fixed signatures, GC and the ordered composite attributes. Observe the
      missing-function/type failures before migration 20 exists:

```sh
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./migrations -run '^TestRuntimeSharedClaimOperationsFunctionsExist$'
```

- [ ] Write each negative role, state, replay and capacity case before its
      implementation. Use complete valid fixtures and exact SQLSTATE/constraint
      expectations. An unrelated error does not prove the intended boundary.

## Task 2: Implement fixed SQL operations

- [ ] Use protected migration framing, first runtime_begin_migration_write and
      last runtime_finish_write, with inert Down. Preserve all existing rows and
      generations except the single migration write-generation advance.
- [ ] Install runtime_claim_result in exact accepted order/types. Owner helpers
      and public wrappers use SECURITY DEFINER, search_path=pg_catalog and fixed
      qualified SQL. Revoke PUBLIC type usage and helper execution. Grant only
      the exact app/maintenance function matrix from fixed operations.
- [ ] Validate direct session_user before mutable reads. App requires serving
      claims; maintenance uses only maintenance C03 and GC. Reject fenced
      release with 42501. No SET ROLE or membership fallback. Found rows undergo
      stored role/kind checks before any result is returned.
- [ ] Validate nil IDs, enum/work/scope shapes and digest lengths with 22023.
      Compute request digests from exact inputs; callers never supply one to
      acquire. Validate durable rows/catalog with AM001. Supplied identity or
      terminal-reason conflicts use AM002. State unavailability uses 55000.
      Fixed errors contain no identity, ARN, digest or evidence values.
- [ ] Preserve capacity, replica, parent, catalog and sorted-summary lock order.
      Acquire requires online admission and an active exact replica. UUID replay
      verifies immutable identity and ordered children before returning the
      current durable state. Never allocate a second charge on replay.
- [ ] Acquire one scope for C01-C04 or IP/optional-account atomically for C05.
      Choose running, then waiting only for queued policies. Denial leaves no
      parent, child, summary charge or partial reservation and returns the exact
      denied matrix, including C01 work identity but no timestamps/ordinals.
- [ ] Sample database time after locks. Allocate positive summary ordinals
      without wrapping. C01 keeps its original admitted_at plus twenty seconds
      through replay and promotion. The smallest live waiting ordinal alone may
      promote into free capacity. Blocked promotion returns waiting unchanged.
- [ ] Expired C01 waiting may release/expired atomically; time never reclaims
      running work. Release remains available during drain and updates every
      child, parent and summary together. Terminal replay requires the same
      reason; a released claim never reacquires capacity.
- [ ] Resolve returns one consistent exact parent/ordered-scope result after
      identity checks. A missing resolve/promote/release result contains only
      absent and claim_id. It never echoes unverified expected fields. Preserve
      accepted lock order when serializing against release and receipt cleanup.
- [ ] GC selects at most 256 released parents by UUID, rechecks the fixed
      twenty-four-hour cutoff under locks, then locks catalog/summaries in
      order. Delete children before parents and only fully unreferenced zero
      summaries. Return one count row even for no work. Never select live claims
      by age.

## Task 3: Prove database boundaries

- [ ] Check every result attribute, nullable combination and policy/work/scope
      shape. Pin the published digest independently in Go and SQL. Test exact
      replay and every changed immutable input; corrupted stored identity must
      not become absent or denied.
- [ ] Use real sessions for each named login, exact function grants, denied
      helper/table access, direct-role checks and cross-kind claims. Inspect
      owners, search paths, PUBLIC/column ACLs and fixed error fields. A hostile
      search path cannot redirect helpers.
- [ ] Exercise every running/waiting capacity at the limit and one beyond, with
      multiple claims sharing a summary. Cover atomic C05 denial/release,
      queue-order promotion, expired waiter versus live running, release during
      drain and exact terminal reason replay. Ordinal exhaustion leaves all rows
      intact.
- [ ] Use two pools and pinned connections for duplicate acquire, concurrent
      capacity, C05 overlapping scopes, promotion/release, acquire/drain and
      resolve/release/GC races. Observe actual lock waits rather than sleeping;
      assert exact results and final parent/child/count equality. Owner fixtures
      may enact accepted membership transitions; they do not prove EC2 fencing.
- [ ] Check GC before/at the cutoff, 256 versus 257 parents, mixed live/released
      rows, referenced summaries, ordinal lifetime and empty count results.
      Rollback preserves receipts and counts. No clock or heartbeat substitutes
      for joined release or later exact fencing.
- [ ] Upgrade populated version 19 to 20 without altering membership, lifecycle,
      transition, claim, rate or business rows. Inject a migration failure and
      prove type/functions/grants and write generation roll back together.

## Task 4: Implement generated transport

- [ ] Add six fixed queries using one MATERIALIZED function result and explicit
      casts/value-presence projection for every nullable field. Use sqlc.narg
      for nullable inputs. Request root generation before dependent Go work.
      Never expand a volatile call per field or hand-edit generated output.
- [ ] Write failing tests, then implement the five scalar operations and the
      separate GC store with WriteTxRunner. Call one generated function in the
      callback, validate/copy before it ends, and expose values only after
      successful finish and commit. Never retry.
- [ ] Test malformed input, nil pool/context, all result matrices, absent versus
      present zero values, invalid returned rows, driver errors, callback panic,
      finish failure and commit ambiguity. Every runner error returns zero
      authority, including after a running result was decoded.
- [ ] Prove entry/function/finish/commit order, exactly one function execution,
      AM001 physical retirement and stable copied digests/timestamps after
      connection reuse. Reuse the existing real transport fault harness; report
      required shared-harness edits to root.

## Task 5: Verify and release

- [ ] Run focused checks from apps/server:

```sh
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./migrations -run '^TestRuntimeSharedClaimOperations'
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./internal/store -run '^TestRuntimeClaim'
GOGC=50 golangci-lint run ./migrations ./internal/store --tests
```

- [ ] Report exact red/green evidence, role/replay/lock results, all changed
      paths, generation needs and unresolved boundaries in
      `.dev/phase-10/runtime-shared-claim-operations-author-report.txt`. Release
      all eight paths. Root inspects and reruns key checks before broader gates,
      staging, gitleaks and commit.

R5 proves the five-minute private retry lifetime and local admission authority.
R8 proves trusted replica/key composition and joined work. The lifecycle slice
proves exact fenced cleanup and its races. None is satisfied by this transport.
