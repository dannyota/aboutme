# Runtime implementation tasks

Status: Ready for bounded dispatch under
[ADR 0035](../../../adr/0035-replica-coordination-and-uat-lifecycle.md). Root
assigns exclusive paths before each task. Infrastructure wiring waits for the
local runtime proof.

## Delivery model

ADR 0024 applies: each task has one author who writes failing tests first and
runs narrow checks. There is no per-task reviewer. After integration, one fresh
Sol phase reviewer checks the named security/concurrency invariants. Root owns
planning, shared files, Git, full make ci, connected make scan, and hosted AWS
authorization. Workers do not edit paths outside assigned ownership.

## Acceptance mapping

- AC-INF-009: Tasks R1-R8 local proof, I1-I3 infrastructure adapters, H1 hosted
  1-to-2-to-1 proof. It covers fences, deletion, render, SSE, fleet admission,
  heavy work, connection envelope, crash, and graceful scale-in.
- AC-RT-001: R6 preserves revision fanout, reconnect and unconditional refetch;
  H1 proves cross-replica loss/reconnect. No new SSE frame.
- AC-RT-002: R2/R3/R6 prove public stream close before successful revocation,
  cached-read denial, and preserved NonDraining behavior.
- Task 10.18 local proof rows map as follows: publication R1-R3; render R4; SSE
  R6; limits R5/R7; resources R8; topology I1/I2; lifecycle I3/H1.

## Dependency order

R0 precedes R1 schema/store, then R1a live write-entry proof, then R2–R7 caller
integration, then R8 final coverage. R2 precedes R3. R4 and R5 may run in
parallel after R1a. R6 depends on R2 and R5 claims; R7 depends on R5 claims. An
unused Go helper with fake tests may precede R1, but R1a cannot pass without its
real database function. Only unchanged local proof permits I1–I3 wiring; H1 is
last. Root serializes migrations, generated artifacts, configuration roots,
Makefile, manifests, workflows and shared infrastructure files.

## R0 - Tracked contract and plan integration

Owner: root/integration owner only.

Owned paths: docs/design relevant sections; ADR 0035 and supersession pointers
for 0018/0022/0034; docs/architecture.md; Task 10.18 and new task files;
traceability AC-INF-009, AC-RT-001/002; runbooks; OpenAPI assertion of no
change.

Done: ADR 0035 and the design contain the accepted decisions; topology fixes
lifecycle-owned capacity/partition/replica activation order; UAT no-op
suppression is accepted or omitted; task ownership and checks are recorded. Rate
ambiguity and heavy-work scope are fixed in the policy catalog.

R0 checks passed: `make docs-fmt` (including docs-lint) and `make api-check`.
These do not complete runtime or hosted acceptance.

## Staged schema enforcement

R1 installs new runtime tables, protected functions and their own assertion
triggers. It does not attach the write-entry trigger to legacy application and
job tables while their callers still use old transaction entry. R1a proves the
store entry API; R2–R7 migrate callers in their exclusive packages. R8 installs
the final legacy-table assertion triggers and verifies complete mutator
coverage.

No production configuration flag bypasses enforcement. Finalize-stop is absent
or uncallable until the final coverage migration is installed and all local
adversarial checks pass. Each intermediate commit retains a usable local app;
none permits stopped-UAT scheduling or multiple serving replicas. Root assigns
one migration at a time and owns generated output. This changes implementation
order only; the accepted final write-barrier contract remains mandatory.

The first bounded slice may add the Go write-entry API and fake-transaction
checks before the database function. It is not composed into callers until R1
provides the real function and live-DB entry/lock checks pass.

## R1 - Runtime coordination and admission schema/store

Start with [R1.1 cluster role bootstrap](role-bootstrap.md), then
[R1.2 write foundation](write-foundation.md). The unused Go helper is locally
checked and committed as `f45cf93`; it replaces the raw starter at `986fe44`.
Live entry, clean reuse and physical discard proof now pass against B1.

After B3, [R1.3 membership schema](membership-schema.md) installs the durable
membership and lifecycle ledger tables without exposing unimplemented
operations. [R1.4 transition schema](transition-schema.md) follows with exact
targets, acknowledgements and terminal outcomes. Then
[R1.5 claim schema](claim-schema.md) installs atomic claim scopes and release
receipts. [R1.6 rate schema](rate-schema.md) installs exact policies, bounded
buckets and stored OAuth attempt debt. Later R1 slices install fixed definers;
R1 exits only after their complete store and real-role proof.

[R1.7 registration](replica-registration.md) begins the fixed operations with
serving/maintenance register and joining-only mark-ready plus typed store
transport. It follows rate schema 18 and adds no activation or caller wiring.

[R1.8 claim operations](claim-operations.md) follows registration migration 19
with fixed acquire/resolve/promote/release, receipt cleanup and typed transport.
R5 keeps operation identity and retry state; exact fenced cleanup remains with
the later lifecycle proof slice.

[R1.9 rate operations](rate-operations.md) follows claim operations migration 20
with nine fixed functions, bounded cleanup and scalar store transport. R5/R7
retain key encoding, operation state and caller responses.

[R1.10 ordinary lifecycle operations](lifecycle-operations.md) follows migration
21 with six controller actions, immutable replay and atomic logical partition
changes. Leave, exact EC2 proof, exclusive wake and transition functions remain
separate later slices. [R1.11 membership evidence](membership-evidence.md)
follows migration 22 with three fixed leave/proof functions and retained proof
request/count fields. Cleanup stays atomic under a bounded caller context, with
no fixed claim-row bound. Exclusive wake and transition functions still follow
separately.

[B3 migrator composition](migrator-composition.md) protects history before any
runtime migration after 00013. Its fixed object manifest and local adoption
preserve existing database rows while converging ownership on runtime_owner.

The [OAuth reservation contract](../../../design/scaling/admission-attempts.md)
fixes P22 attempt schema, common rate lock order and bounded terminal receipt
cleanup. [Rate storage](../../../design/scaling/rate-storage.md) fixes all 24
policies, integer/rolling state, seeds, stored-pending assertions and bounded
ordinary/overflow cleanup with an atomic policy_idle result. R1 implements
store/schema only; R5 and R7b own callers.

[Fixed rate operations](../../../design/scaling/rate-operations.md) fixes nine
result/role/error matrices, P22 historical replay denial, scalar transport and
the owner-only database-time test seam. Each returned runner error exposes zero
authority. The operation migration and R5/R7 callers remain separate slices.

The [shared claim schema](../../../design/scaling/shared-claims.md) and
[identity contract](../../../design/scaling/claim-identities.md) fix multi-scope
SSE, queue ordinals, replica binding, receipt retention and bounded ambiguity
resolution. R1 adds their store/schema; R5 and the named caller tasks compose
it.

[Fixed claim operations](../../../design/scaling/claim-operations.md) supplies
the result/nullability matrix, direct-login role/kind checks, scoped AM002
conflicts and one-call query transport. R1 returns no authority on runner
errors; R5 alone owns ClaimOperation and retry state. The result type belongs in
a later operation migration, with claim schema 17 unchanged.

The [membership](../../../design/scaling/replica-membership.md),
[lifecycle](../../../design/scaling/lifecycle-operations.md) and
[replay](../../../design/scaling/lifecycle-replay.md) contracts and
[literal digest vectors](../../../design/scaling/lifecycle-vectors.md) fix exact
task trios, serving/maintenance kinds, logical partitions and immutable
controller results. The
[ordinary controller contract](../../../design/scaling/lifecycle-controller.md)
fixes their grants, scalar transport, partition checks and replay-error limits.
[Public transitions](../../../design/scaling/public-transitions.md) and
[commit](../../../design/scaling/transition-commit.md) fix the target digest,
original deadline, parent-first execution fence and separate lifecycle recovery
of a proved fenced initiator. R1 installs their real named-role grants. R8 owns
composition and readiness; it introduces no additional SQL privilege gate.

[Recovery evidence](../../../design/scaling/transition-recovery.md) fixes the
ordinary parent-locked rollback and atomic outcome authority. No unresolved
writer is installed. R1 adds the separate
[reconciliation read](../../../design/scaling/transition-reconciliation.md) and
private store value; R2 holds its local apply mutex across that one read, driver
cleanup and fence application. R3 validates response receipts separately.

[Exclusive lifecycle entry](../../../design/scaling/lifecycle-write-entry.md)
defines the only two wake methods, owner marker, fixed assertion catalog and
historical write-state result. R1 installs the functions and bounded changes to
ordinary/migrator entry checks in a new migration; it never edits 00013. R1a
proves the private wake runner beside the shared runner. Wake functions never
use ordinary entry or finish. The wake statement assertion checks
table/operation; owner row triggers check the operation ID, workflow and action
against the entry-bound marker.

Owner paths: root-assigned migration number, apps/server/sql runtime query
source or approved new SQL files, generated store output, store/migration tests.
These are normally root-owned; root must explicitly assign and serialize them.

Fail first:

- schema constraints reject invalid replica states, forged/ambiguous graceful
  leave, mutable/mismatched fencing proof, target order/digest/result mismatch,
  committed parent with incomplete results, duplicate required replica/ack,
  illegal transition edge, invalid rate fields, >10,000 partition ownership,
  overflow refusal, conflicting claims;
- two independent pools prove transition snapshot/join serialization, business
  commit state CAS, rollback exclusion of paused initiator, notification after
  commit, database-time clamp, concurrent capacity allocation, atomic composite
  debt, reservation finish, claim caps, delayed terminal replay, account-retired
  results, and proof-gated cleanup;
- role tests prove PUBLIC/app/lifecycle direct DML is revoked, app cannot forge
  proof/fenced/terminating/capacity state, lifecycle executes only its named
  definer functions, search_path is pg_catalog, and proof is immutable.
- race a locked business parent against fenced-initiator recovery; recover with
  zero surviving serving replicas; reject wrong-host/ECS-only/missing proof and
  cross-role execution; preserve committed/unresolved terminal evidence;
- prove omitted transition completion and finish-before-terminal fail; pending
  deferred assertions block DISCARD TEMP, forced checks cannot remove an
  unfinished capability, and completed/discarded guards reject re-entry.
- prove closed-to-closing and closing-to-open wake, historical replay after
  later writes, mixed-entry rejection, wrong-role/table/action rejection,
  unchanged accepted-writer tail, and physical backend retirement on ambiguity.

Implement: sqlc boundaries for all schema operations; no handler SQL. Preserve
existing public_state and resume transaction lock order.

Narrow commands not run:

- make sqlc-check server-test-db server-migration-test
- under apps/server: go test -count=1 ./internal/store ./migrations

## R1a - Central write transaction entry

Owner: one store/runtime author. Exclusive paths are store write-entry helpers
and their unit/live-DB tests only. R1 owns database function source; root owns
generated output. R1a never edits a caller package.

Fail first: the private runner enters before any callback/query and finishes
immediately before commit; failed entry exposes no callback. Cancellation
permits bounded cleanup. AM001, ambiguous entry/finish, failed cleanup and
commit error destroy the physical backend without retry. Marker-only, lock-only,
stale marker and closed gate fail before row change. Forced constraints cannot
bypass finish. Independent pools prove entry waits before any row lock when the
finalizer holds exclusive and a poisoned backend is replaced.

Replace the unused helper with the exact WriteTxRunner interface from
[transaction entry](../../../design/scaling/transaction-entry.md). Its callback
receives only Queries; raw transactions stay private. R2–R7 migrate each caller
within their assigned package. R3 proves the account and resume lock cases;
R7a/R7b prove auth/OAuth; R7d proves auth-mail/maintenance. R8 owns
command/fixture/migrator wiring, the generated mutator inventory gate, and
complete legacy trigger coverage. No-op suppression remains unavailable.

Narrow checks: under apps/server, `go test -race -count=1 ./internal/store`.
Real entry checks require TEST_DATABASE_URL; a fake-only pass does not complete
R1a. Root later runs the affected integration and migration targets.

## R2 - Distributed publicstate coordinator

Owner paths: apps/server/internal/publicstate/** only.

Exported contract:

- Coordinator AcquireResume/AcquireDiscovery retain lease semantics.
- Begin(ctx, Plan) returns Transition with Close(ctx, deadline), Commit callback
  integration, Rollback and Recover. Concrete implementation uses a narrow
  RuntimeStore interface and local fence adapter.
- Run(ctx), Ready(), BeginDrain(), Close() expose listener lifecycle, quarantine
  and authoritative same-incarnation replay.

Fail first with two coordinator objects and scripted independent stores: joining
race; target order/digest; duplicate/dropped/reordered NOTIFY repaired by poll;
NonDraining seals without cancel/wait; Revoking/discovery cancel and wait;
five-second missing ack rolls back before mutation callback; terminal result
replay; listener loss quarantines; stale ack/proof-as-ack rejected; same
incarnation recovers only without intent/proof and after callbacks join;
fleet-wide RDS outage keeps liveness healthy; terminating never reopens.

Narrow command not run: under apps/server, go test -count=1
./internal/publicstate.

## R3 - Mutation and deletion integration

Owner paths: apps/server/internal/resumeapi/**,
apps/server/internal/accountapi/** and apps/server/internal/resume/**. If public
reader changes are needed, assign apps/server/internal/publicresume/**
separately to avoid overlap.

Fail first: every mutation preserves idempotency recheck; expected generation
mismatch; fleet close before business callback; timeout/cancel before SQL;
state-CAS loser; definite rollback; commit response loss resolved from the
atomic transition with separate response-receipt validation; unsafe closing
recovery unready; account deletion global/UUID lock order, three plan attempts,
session/email recheck, media-reference and job atomicity. Prove no object write
or compensation repeats. Migrate resume/account rate callers to frozen R5
interfaces within these owned packages; no separate admission author touches
them.

Narrow command, under apps/server:

```sh
go test -count=1 ./internal/resumeapi ./internal/accountapi ./internal/resume
```

## R4 - Fleet render claim and strict origin affinity

Owner paths: apps/server/internal/renderjob/**, internal/printapi/**,
internal/printrender/**. Composition/routing config remains R8/root-owned.

Fail first: two queues admit exactly one running/eight waiting; deadline begins
at claim; promotion after deadline fails; paired Nuxt redemption once; wrong
node/replica/job/resume/audience/token opaque failure; job ID cannot complete;
controller remains unexported; digest recomputed; ambiguous redemption consumed;
crash claim remains charged until fencing; cleanup never retries; caller retry
gets fresh authority and its own original 20 seconds.

Narrow command not run: under apps/server, go test -count=1 ./internal/renderjob
./internal/printapi ./internal/printrender.

## R5 - Distributed rate and claim packages

Owner paths: new apps/server/internal/admission/** plus
apps/server/internal/api/ratelimit.go and its tests. Store implementation
remains R1. Root resolves package name before dispatch.

[Rate identities](../../../design/scaling/rate-identities.md) fixes the single
typed encoder, distinct canonical/peer IP domains, P05 shapes and deployment key
versions. R5 preserves caller canonicalization and receives no raw-key SQL or
generation override. P22 retains its fixed unkeyed UUID hash.

Fail first for every algorithm: replica one/two exact budget; burst/refill;
denial debt; 24-hour idle and fully-refilled expiry; rejected request last_seen;
one/two 10,000 partitions; scale-in retains partition-two debt; overflow admits;
no active eviction; private/overflow clear; backwards clock clamp; forward jump
capacity cap/metric; concurrent allocation; DB failure uses current caller error
with possible restrictive debt and no retry/refund; ambiguous claim resolves
exact UUID before work; atomic release/fence cleanup; Retry-After rounding.

Narrow command not run: under apps/server, go test -count=1 ./internal/admission
./internal/api.

## R6 - Realtime fleet admission and drain

Owner paths: apps/server/internal/realtime/**and
apps/server/internal/realtimeapi/**.

Fail first: LISTEN on each replica; notification from either reaches local hubs;
listener loss unavailable; two hubs race 101st IP/21st account; each retains
2,000 task cap and eight-event queue; claim rollback on local/FD failure; owner
session and public generation recheck; revocation closes before ack; graceful
drain within heartbeat; abrupt loss claim remains until fence; reconnect always
refetches; no SSE connection acquires DB session; no new event shape.

Narrow command not run: under apps/server, go test -count=1 ./internal/realtime
./internal/realtimeapi ./internal/realtimebench.

## R7 - Policy caller migration and worker ownership

Split into disjoint author subtasks only after R5 interfaces freeze:

- R7a auth/password: internal/auth/**, internal/password/**, internal/user/**,
  excluding cmd/server composition.
- R7b OAuth/MCP: internal/oauthsrv/**, internal/mcpapi/**.
- R7c public API: admission call sites in internal/publicapi/** only. R3 owns
  all resume/account callers; R6 owns realtime callers.
- R7d workers: internal/authmail/**, mediacleanup/** and privacyretention/**. R8
  owns one-shot command wiring. Enforce global password two/16 and mail two-send
  claims where their owners compose them; keep explicit per-task rows local.

R7d follows
[maintenance sessions](../../../design/scaling/maintenance-entry.md). Root first
assigns its store primitive and database functions as a serialized slice. The
fixed lock catalog and eight allowed pool reads feed R8's existing operation
inventory. Callers preserve scope-before-job auth-mail ordering and must prove
quarantine never cleans up pgx concurrently with an active callback.

Each subtask writes current-response parity tests plus two-store concurrency,
overflow, DB loss, cancellation and ambiguity tests. Preserve every inventory
number and key shape. Photo admission remains per task. Outer and route-specific
limiters remain distinct. MCP keeps ordered partial debt. OAuth pending finish
and password no-oracle behavior remain exact.

Narrow commands not run: focused go test -count=1 for only assigned packages;
then make server-build server-vet server-test after serialized integration.

## R8 - Composition, readiness, shutdown, and resource envelope

Owner: integration owner. Root-owned paths include cmd/server/main.go,
internal/config/**, internal/store/store.go cap, root Makefile, deploy
manifests, tool/lock/generated roots, command fixtures, migrator entry wiring,
the generated mutator inventory gate, and any shared test harness.

Fail first: exact replica/deployment identity required; partial trio rejected;
joining cannot become ready; duplicate task/instance and changed identity
rejected; two replicas may share a build digest; normal query, revision LISTEN,
transition listener, admission probe, paired Caddy/Nuxt probe;
quarantine/replay; ordered SIGTERM; coordinator loss cancels and joins without
liveness restart; termination intent prevents reopen; pool max 12; two app
pools + five independent four-connection auxiliary tasks + admin reserve = 60;
2,000 SSE adds zero DB connections; ASG max two grants no third pool.

R8 verifies the pinned admission/password-email key-version tuple before adapter
construction. Exact target evidence gates the existing activation call; no
membership/lifecycle SQL expands for key versions. Prove rejection of missing,
mixed or unverifiable versions and the closed rotation predicate: joined/fenced
work, zero claims, and policy_idle from every policy while new admission remains
closed. No old-key fallback or artificial clock advance. Write the executable
rotation runbook only after the runtime and infrastructure exist; keep its
private controller evidence out of this public repository.

Narrow commands not run: make server-build server-vet server-test; relevant
composition tests under apps/server/cmd/server and internal/config/store.

## I1 - Exact replica topology and paired routes

Owner: root-assigned infrastructure author after local proof. Root-owned
OpenTofu modules, manifests and Caddy config must be serialized with topology
work.

Mock proof: one node has exact Caddy host, Go host and Nuxt bridge DAEMON tasks;
fixed bindings prevent duplicates; aggregate readiness closes for missing or
wrong release; Go-to-Nuxt and Nuxt-to-Caddy-to-Go stay node-local; public paths
deny internal render/redeem; security groups expose no internal listener.

## I2 - EC2 termination fencing adapter

Owner: infrastructure/lifecycle author; separate from R1 app schema author.

Local adapter tests use a fake EC2 control plane and fake fence writer. They
prove exact instance/incarnation/digest binding; only terminated accepted;
pending/stopping/STOPPED ECS/unhealthy/deregistered/lock-loss rejected; evidence
id replay idempotent only if exact; control-plane/write ambiguity records no
assumed proof; least-privilege proof role can call only the fence function.

AWS calls are hosted proof, never unit-test prerequisites.

## I3 - Lifecycle, scaling and no-op decision integration

Owner: infrastructure lifecycle author after no-op evidence prerequisites pass.

Mock proof: lifecycle-owned database capacity stays 1..2 and serializes replica
activation with partition enablement; app cannot enable it; ASG maximum two;
graceful scale-in binds exact target/generation, joins work, commits left
receipt, terminates only that instance, and disables new partition-two ownership
while keeping debt; missing/ambiguous leave requires fencing; fleet RDS outage
suppresses readiness replacement; isolated failure gets one intent and one
replacement after DB recovery; crash replacement does not erase old incarnation;
terminated proof enables only later retry/claim cleanup; UAT off reconciles ASG
0/0/0, ECS desired state and temporary ALB removal. No-op implementation follows
the accepted UAT lifecycle contract and must preserve hourly, 24-hour, daily,
weekly, deletion, retention, mail and seven-day obligations; missing proof keeps
RDS running.

## H1 - Hosted production-shaped proof

Owner: integration owner under existing exact AWS authorization, after R1-I3.

Drive actual 1-to-2-to-1 while writes, publish/revocation, account/private-media
deletion, render/redemption, owner/public SSE, rate limits and pool measurement
run. Inject graceful scale-in and abrupt node loss. Verify missing ack makes the
current mutation fail before SQL; terminate exact node; record proof; fresh
retry succeeds; old claims clean only after proof; no stale public/private bytes
become reachable; limits do not multiply; connections stay <=60; no third node.

## Phase review and exit

Fresh Sol reviewer authored none of the phase. It names and confirms: public
generation fences, NonDraining, account deletion, private-media reference
revocation, ambiguous commit, idempotency, render capability/controller and
deadline, SSE authorization/reconnect/drain, rate overflow/debt/time, heavy-work
claims, external fencing authority, 60-connection envelope, one-node pairing,
and no public contract drift. Findings return to authors; same reviewer confirms
fixes. Integration owner runs exit criteria, make ci and connected make scan at
one unchanged candidate commit.

## Verification status

Runtime and infrastructure checks remain pending implementation. The integration
owner records commands and results in the phase evidence at the candidate
commit. Design acceptance is not a passing implementation check.
