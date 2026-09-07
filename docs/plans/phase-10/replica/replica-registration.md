# R1.7 replica registration implementation plan

> For workers: use superpowers:subagent-driven-development or
> superpowers:executing-plans within AGENTS.md ownership. ADR 0024 keeps one
> author per task and one fresh phase review; it supersedes per-task review
> defaults.

**Goal:** Add fixed serving and maintenance registration/readiness operations
with typed results and no automatic retry.

**Architecture:** Migration 19 follows accepted schema 18 and uses the existing
membership tables and runtime_replica_result type. Four role-bound wrappers
preserve current-state registration replay and joining-only readiness. A store
adapter runs one generated query inside WriteTxRunner and exposes a result only
after successful completion.

**Tech stack:** PostgreSQL 18, Goose, Go, pgx and sqlc; repository pins apply.

**Spec:** [Membership](../../../design/scaling/replica-membership.md),
[coordination](../../../design/scaling/runtime-coordination.md),
[lifecycle](../../../design/scaling/lifecycle-operations.md),
[schema](../../../design/scaling/runtime-schema.md),
[write entry](../../../design/scaling/transaction-entry.md) and
[fixed result transport](../../../design/scaling/claim-operations.md).

## Ownership and acceptance

This slice contributes to AC-INF-009 and Task 10.18's membership/fence rows. It
does not activate a replica, complete R1/R8, or authorize hosted work. R8
retains trusted identity, key-version checks, local probes and readiness wiring.

Root assigns one Sol implementation author after schema 18 is committed:

- `apps/server/migrations/00019_runtime_replica_registration.sql`;
- `apps/server/migrations/runtime_replica_registration_test.go`;
- `apps/server/migrations/runtime_replica_registration_helpers_test.go`;
- `apps/server/sql/runtime_membership.sql`;
- `apps/server/internal/store/runtime_membership.go`;
- `apps/server/internal/store/runtime_membership_test.go`.

Root delegates only this new SQL source; existing SQL, generated files, shared
helpers/tests, configuration, manifests, plans and Git remain root-owned. Root
alone runs sqlc generation when the author requests it. Do not edit an earlier
migration or compose these methods into callers. No new role, dependency,
container/native database change, secret read, cloud action or public API.

Use isolated databases in the shared container and the public fixture DSN. One
heavy command at a time. Bound setup, row-lock observation and cleanup. Cancel
and join each test goroutine; no sleep-based race proof.

## Interfaces

All SQL signatures and runtime_replica_result attributes are fixed by
membership. Input names may have `p_` prefixes; argument order/types do not
change. The four functions are runtime_register_serving_replica,
runtime_register_maintenance_replica, runtime_mark_serving_replica_join_ready
and runtime_mark_maintenance_replica_join_ready. Each receives the same complete
replica, EC2, container instance, task trio and release identity.

The store interface is:

```go
type RuntimeReplicaIdentity struct {
 ReplicaID uuid.UUID
 InstanceID string
 ContainerInstanceARN string
 CaddyTaskARN string
 GoTaskARN string
 NuxtTaskARN string
 ReleaseDigest string
}

type RuntimeReplicaRegistrationStore interface {
 RegisterServing(context.Context, RuntimeReplicaIdentity) (RuntimeReplicaResult, error)
 RegisterMaintenance(context.Context, RuntimeReplicaIdentity) (RuntimeReplicaResult, error)
 MarkServingJoinReady(context.Context, RuntimeReplicaIdentity) (RuntimeReplicaResult, error)
 MarkMaintenanceJoinReady(context.Context, RuntimeReplicaIdentity) (RuntimeReplicaResult, error)
}

func NewRuntimeReplicaRegistrationStore(*pgxpool.Pool) RuntimeReplicaRegistrationStore
```

RuntimeReplicaResult mirrors all fourteen accepted composite attributes with
value UUID/text/generation fields and nullable count/flag fields. The decoder
validates the action matrix: all count/partition fields are absent for these
four operations; common and replica fields are present. Use a private validated
constructor/decoder in runtime_membership.go, with no callback or driver handle
in the interface. The schema's generated RuntimeReplica model is unchanged.

Use `*int16` for the three nullable count fields and `*bool` for the two
nullable partition flags. Validation errors require fixed private-value-free
messages, a nonnil error and a zero result; no exported sentinel or exact
message contract is added. Preserve wrapped driver causes for existing error
handling.

## Task 1: Prove missing operations

- [x] Add a bounded live test that checks to_regprocedure for each exact
      seven-argument signature and reports each missing function. Run it before
      migration 19 exists:

```sh
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./migrations -run '^TestRuntimeReplicaRegistrationFunctionsExist$'
```

- [x] Cover role, replay, identity and generation cases for each implementation
      rule. Use complete valid tuple/task/transition fixtures. Assert AM002 for
      supplied conflict, 55000 for unavailable state and AM001 for durable
      corruption; an arbitrary SQL error is not evidence.

## Task 2: Implement role-bound registration and readiness

- [x] Frame migration 19 with runtime_begin_migration_write first and
      runtime_finish_write last. Down is inert. Preserve existing rows; add no
      seed, ledger, generation field, configuration or identity parameter.
- [x] Create fixed-kind wrappers over owner-only helpers. App executes only the
      two serving wrappers; maintenance executes only the two maintenance
      wrappers. Use session_user for the direct role/kind boundary, qualified
      static SQL, SECURITY DEFINER and search_path=pg_catalog. No runtime login
      executes helpers or gains table DML.
- [x] Validate the complete tuple and source bounds. Private deployment owns the
      pinned ARN prefixes; SQL compares full strings and fixed role order,
      without extracting trust from a suffix or accepting a caller timestamp.
- [x] Registration locks public_state then capacity, handles exact UUID replay
      and inserts one joining row plus caddy/go/nuxt children. Different tuple,
      live instance reuse and cross-role task reuse conflict without partial
      rows. New registration may remain joining under offline/stopping or
      visible closing/unresolved state; it never makes readiness true.
- [x] Exact registration replay verifies all immutable fields and all three
      children. Return current replica/capacity state even after activation,
      drain, leave, termination or fence. Increment neither generation and never
      reconstruct a historical registration result.
- [x] Mark-ready locks public_state, capacity and replica. Require joining,
      exact tuple, no termination intent, and exactly (online,true) or
      (starting,false) for lifecycle/admission, with no visible
      unresolved/closing transition. Set join_ready_at once, keep joining and
      change no partition. Exact replay is idempotent only while joining. SQL
      does not claim local probe or key-version evidence.
- [x] Preserve ordinary write entry: a closed/closing gate rejects before the
      fixed function. A starting lifecycle creates no bypass. Both rate
      partitions may be disabled when marking ready; activation owns their later
      enablement.
- [x] Sample database time once after needed row locks and clamp it to capacity
      updated_at and relevant prior timestamps. Every fresh register/mark-ready
      increments capacity generation once; controller generation is unchanged.
      Replay, rejection and rollback increment neither. Detect bigint exhaustion
      without wrapping or partial mutation.
- [x] Set capacity.updated_by to the exact public function name for the fresh
      wrapper call. These four fixed literals are internal evidence; no caller
      operation ID is added. Exact replay preserves updated_by and updated_at.
- [x] Assert runtime_require_write_entry before the first row lock, including
      exact replay. The fixed operation never acquires the barrier itself.
- [x] Never lock or wait on a transition parent. Use only the accepted
      nonlocking predicates while holding membership locks. Return exact
      runtime_replica_result fields with null count/partition outputs and
      replayed true only for exact replay.

## Task 3: Prove SQL state, role and concurrency boundaries

- [x] Cover nil UUID, every tuple field bound, malformed instance/release, same
      release across distinct replicas, duplicate/cross-role task reuse,
      missing/extra/mismatched normalized children and each replay-field change.
- [x] Prove serving/maintenance role separation with real sessions. Wrong
      wrapper/helper/table access fails 42501. Verify owners, PUBLIC revocation,
      helper secrecy, search_path and no sensitive values in fixed errors.
      Direct calls without write entry reject before any row lock, including
      replay while another session holds public_state. Hostile search paths
      cannot redirect helper reads or writes.
- [x] Cover offline, starting, online and stopping registration; closing and
      unresolved transition visibility; no implicit activation; ready rejection
      outside joining and exact ready replay under each accepted pair. Test
      closed/closing gate rejection and both-disabled partition success without
      a partition mutation. Test current registration replay after every later
      state with no generation change.
- [x] Populate future capacity/replica timestamps to prove the clock clamp, and
      exercise capacity generation at its positive bigint boundary. A failed
      write leaves parent, children, timestamps and generations intact.
- [x] Use independent pinned connections to race same UUID, same live EC2
      identity, cross-role task reuse, and registration/ready serialization on
      public_state. Observe the intended lock wait, then assert exact loser
      state and no partial trio. Hold a transition parent independently and
      prove membership does not wait on it.
- [x] Upgrade a populated fixture explicitly from version 18 to 19. Assert
      membership, lifecycle, transition, claim, rate and business rows survive;
      an injected migration failure rolls back functions/grants and generation.

## Task 4: Add generated queries and store adapter

- [x] Add four exact query methods to the owned SQL source. Use the one-call
      materialized result and explicit casts/value-presence projections for all
      composite fields. Nullable inputs use sqlc.narg where needed. Never use
      expression expansion of a volatile function or depend on inferred
      composite nullability.
- [x] Request root sqlc generation after SQL/query sources are ready. Root
      inspects generated types before this dependent store work begins. Do not
      hand-edit generated methods or querier.go.
- [x] Cover adapter tests for each fixed function, full result matrix, nil
      pool/context or malformed identity, driver failure, invalid returned
      state, missing fields and ambiguous completion. Implement only the four
      interface methods and shared private decoding.
- [x] Use WriteTxRunner for each method. Decode/copy inside its callback; return
      the zero result on any error, including finish or commit failure. No retry
      or caller-supplied state/generation/time. Prove one SQL function execution
      and that successful values survive connection reuse.

## Task 5: Verify and release

- [x] Run these focused checks from apps/server:

```sh
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./migrations -run '^TestRuntimeReplicaRegistration'
REQUIRE_TEST_DB=1 TEST_DATABASE_URL=... go test -race -count=1 ./internal/store -run '^TestRuntimeReplicaRegistration'
GOGC=50 golangci-lint run ./migrations ./internal/store --tests
```

- [x] Report exact red/green commands, replay/role/lock results, changed paths,
      root generation needs and unresolved boundaries in
      `.dev/phase-10/runtime-replica-registration-author-report.txt`. Release
      all six owned files. Root inspects and reruns key checks, then owns
      broader migration/database/Go gates, staging, gitleaks and commit.

Stop at a concrete authority conflict or missing semantic decision. Do not add
activation, leave, fencing, lifecycle actions, local probes or caller
composition to satisfy a fixture. Those have separate R1/R8 ownership.

## Implementation evidence

The four-signature absence check failed before migration 19 was added. Initial
adapter implementation preceded its tests; adapter test-first ordering is not
claimed. The checklist records final functional coverage. This process deviation
does not change the repository's test-first rule for later work.

Registration now asserts existing write entry before taking row locks, including
replay. Fixture corrections preserve the intended SQLSTATE and roll back a begun
transaction before resetting session identity. Required-NULL scans, live
finish/commit errors and connection reuse are covered.

Integration checks passed:

- Focused migration race: 95.919 seconds.
- Focused store race, including live reuse: 6.782 seconds.
- Full make server-migration-test: migrations 499.883 seconds; CLI 6.944
  seconds.
- make server-build server-vet server-test and make server-test-db
  server-test-integration.
- Scoped migration/store lint: zero issues.
- Native migrate/migrate-check and unchanged user/resume counts. Both local
  databases are version 19, write generation 8, enforcement 1, with protected
  history ownership retained.

sqlc generation succeeded and all four mappings were inspected. The clean-tree
make sqlc-check runs after commit, before dependent work. Activation, trusted
identity/probe composition and hosted proof remain later slices.
