# Fleet admission, render and realtime

Status: Accepted under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). This is
the target contract. Local implementation and hosted proof remain Phase 10
gates.

## Shared-time model

This intentionally changes rate-policy time authority. Current local limiters
receive an injected process clock and clamp it monotonically in memory. The
distributed policies use PostgreSQL time and a durable high-water value so all
replicas make one ordered decision after restart.

## shared_policy_clocks schema contract

- policy_id text primary key; high_water_at timestamptz not null; last_raw_at
  timestamptz not null; anomaly_count bigint >= 0.
- Each admission transaction locks this row and samples clock_timestamp once.
  effective_now = greatest(high_water_at, raw_now). Update both observations.
- A backward adjustment stays clamped and cannot extend a window, suppress due
  cleanup, restore tokens, or reset idle age.
- A forward adjustment advances refill/expiry only up to the existing bucket
  capacity. No bucket can hold more than its current burst/limit. The durable
  high-water never moves backward later. Count and metric any absolute raw jump
  above an implementation constant selected only for observability.
- Tests replace the owner-only no-argument sampling helper in disposable
  databases through the
  [fixed clock seam](rate-operations.md#database-time-test-seam). Production
  callers cannot provide or change time. This differs from current local
  injected-clock behavior under ADR 0035, which supersedes ADR 0018 for fleet
  time authority.

## Rate schema contract

[Rate storage](rate-storage.md) fixes the policy catalog, exact integer state,
seeds, pending-window assertions, bounded cleanup and SQL results.
[Rate identities](rate-identities.md) fixes typed canonical keys, distinct
raw-peer fallback and pinned deployment key versions. These details preserve the
policy numbers and caller behavior below.

## shared_rate_partitions

- primary key (policy_id, partition); partition in (1,2); enabled boolean;
  capacity_generation bigint > 0 and operation_id text not null; active_keys
  integer check 0..10000; updated_at.
- First serving activation enables partition 1; second serving activation
  enables partition 2. Failure and fencing preserve both flags, even with zero
  survivors; replacement inherits that logical capacity. Proved scale-in
  disables partition 2 and final shutdown disables both, retaining active rows
  and debt. Maintenance activation changes neither. These changes use the same
  expected-generation transaction as the lifecycle action. Stale controller
  generations cannot toggle a flag.

## shared_rate_buckets

- primary key (policy_id, key_digest); partition 1 or 2; algorithm enum
  ('token_bucket','fixed_window','rolling_slug'); algorithm fields constrained
  to its policy: token_numerator bigint, refill_at, window_started_at, count,
  rolling_events and last_seen. Integer numerator units retain the exact
  accepted rational refill rate at PostgreSQL microsecond precision.
- unique policy/partition/key; key_digest is HMAC/UUID/IP composite output from
  the existing canonical caller and contains no bearer/email plaintext.
- Rows expire only when fully refilled or their algorithm carries no debt, or
  after the accepted 24-hour idle backstop. A rejected request updates
  last_seen. Cleanup decrements active_keys in the same locked transaction. No
  active row is evicted.

## shared_rate_overflow

- policy_id primary key; same algorithm-state columns and constraints as one
  ordinary key; no identity column and no identity-specific clear operation.
- One authoritative overflow bucket exists per distributed policy. When no
  enabled partition can own a new key after legitimate expiry cleanup, the
  request is evaluated against overflow. It may admit. Capacity refusal is not
  an alternative.

Policy IDs are fixed enums in Go and database checks: outer API; resume
read/write/photo; password login IP, login-failure email, register/forgot IP and
email, verify/reset IP, account mutation; provider starts; owner PDF account and
IP; account export/delete; public artifact request/render miss; public SSE
request; OAuth register/token/failed grant; MCP token/user; changed-slug
attempt. Keep separate policies separate even when keys and numbers match.

The two current outer API route chains use one shared `api.outer_request`
policy. This corrects their independent counters to enforce the design's global
300/client-IP/minute budget. Health and public route bypasses stay unchanged.
Provider starts retain one `auth.provider_start` policy with the current
anonymous and authenticated key shapes. No new provider-start split is added.

## Token-bucket transaction

1. Follow the
   [common rate lock order](admission-attempts.md#lock-order-and-time). Lock
   policy clock and existing private bucket. If absent, lock partitions in
   numeric order, remove only legitimately expired rows, then claim the first
   enabled partition with active_keys < 10000. Otherwise lock overflow.
2. Refill by effective elapsed time, capped at existing Requests burst. A
   successful admission consumes one token. A denial consumes none and returns
   positive ceiling-rounded Retry-After of at least one second.
3. Commit the bucket, last_seen, clock and capacity changes together. Database
   error returns unavailable and grants no work.

## Fixed-window and rolling transaction

- Password failures preserve first-failure 15-minute window, threshold 10,
  denial without extension, State without mutation except last_seen, failure
  record, and private-only ClearSuccess. State never allocates or sweeps; it
  uses committed capacity and refreshes only the selected existing private or
  overflow bucket. ClearSuccess deletes only an existing private bucket with its
  active_keys decrement. Overflow clear is a no-op.
- OAuth failed grants count committed failures plus pending reservations against
  10/15 minutes. Admit creates a globally unique attempt UUID in
  shared_admission_attempts. Finish invalid converts pending to failure; neutral
  resolves its pending debt; success clears a private bucket and its pending
  debt but never overflow debt. First pending starts the window. Retain terminal
  receipts for 24 hours after terminal_at, with maintenance pages of at
  most 256. An exact caller-finish replay is idempotent; a different caller
  outcome conflicts during that horizon. System-cleared/expired attempts and
  absent-after-deletion finishes are harmless no-ops. The
  [attempt contract](admission-attempts.md) fixes schema, lock order, replay and
  cleanup details.
- Slug attempts preserve rolling 30/account/hour, including denied attempt debt
  and its existing caller error without invented Retry-After.

## Composite and ambiguous admission

MCP token then user admission preserves current order. Run both in one database
transaction. If token admits and user denies, commit the token debt and return
the user denial. Do not refund. Owner PDF account then IP and multi-dimensional
password policies likewise preserve their current caller order unless existing
tests prove a different sequence.

Ambiguous token/fixed-window commit returns the caller's established unavailable
path and accepts possibly committed restrictive debt. The server does not retry,
refund, report allowed, or start downstream work. This needs no rate-operation
result table. A later independent client request is a new admission and may add
new debt normally; only hidden automatic retry is forbidden.

Claims differ because they authorize concurrent work. Acquire uses a
caller-generated claim UUID. On an ambiguous response, the caller queries that
exact UUID after database authority returns. It starts work only after finding a
matching committed claim and scope. Confirmed absence permits one acquisition
attempt with the same UUID; unavailable or contradictory state launches no work.
Release is idempotent by UUID. This gives positive resolution without allowing
an unknown acquisition to overspend concurrency.

## Exported Go interfaces and caller migration

package admission (new) exposes concepts, not pgx:

- type RateKey []byte; type Policy string.
- type Decision struct { Allowed bool; RetryAfterSeconds int }.
- type RateStore interface { Admit(context.Context, Policy, RateKey) (Decision,
  error) AdmitComposite(context.Context, []Request) ([]Decision, error)
  FailureState(context.Context, Policy, RateKey) (FailureState, error)
  RecordFailure(context.Context, Policy, RateKey) (...)
  ClearSuccess(context.Context, Policy, RateKey) error
  FinishAttempt(context.Context, AttemptID, Outcome) error
  Ready(context.Context) error }.

Exact names may follow repository style, but these operations and context/error
semantics are fixed. `api.RateLimit` accepts a context-aware RateAdmitter and
maps denied to its existing 429/Retry-After. Store errors never invoke the
protected handler and use the caller's current dependency-failure path: ordinary
API/public routes use their representation-specific unavailable response;
password uses 503 `authentication_unavailable`; resume mutation uses 503
`public_state_busy` with Retry-After 1; account uses 503 `account_unavailable`;
MCP uses 503 `agent_access_unavailable`; OAuth register and token use existing
500 `server_error`. This adds no status, body field, header or public schema.
Auth/password/OAuth/MCP, resume/account/public handlers retain current key
creation and response writers. Their constructors receive narrow policy
interfaces. Remove caller-provided production clocks; fake store clocks remain
available to deterministic tests.

## Shared concurrency schema and algorithm

Use the [shared claim schema](shared-claims.md): immutable policy catalog,
locked scope summaries, one claim parent and one or two scope children. SSE IP
and optional account charges are atomic. Request order and queue allocation
order use separate ordinals. Release removes charged capacity while retaining
terminal receipts for 24 hours; maintenance deletes at most 256 receipts per
transaction. Live claims are never removed by receipt age.

The [identity and ambiguity contract](claim-identities.md) fixes scope/request
encoding, replica consistency and operation-local retry permission. After an
ambiguous acquire, exact positive resolution is required. Confirmed absence
allows one same-UUID acquisition only at operation age below five minutes with
the original context and all caller deadlines still live. Restart or lost
operation state cannot reconstruct that permission.

Graceful cancellation releases after local work joins. Abrupt claims remain
until verified EC2 termination proof for their replica. TTL, deadline, lock loss
and replacement never establish that proof.

Policies: render global one running/eight waiting; password hash global two
running/16 waiting; mail send global two running; MCP four running per user and
no waiting; SSE 100 per IP and 20 per account with atomic two-dimensional claim.
The per-task SSE 2,000 and photo intake one/wait one second remain local.

This classification has no authoritative contradiction. `budgets.md` leaves hash
two/16 and mail two-send concurrency unqualified, while it explicitly says photo
intake, SSE task admission, and cache ownership are per task/instance. Current
password/mail comments describe the one-process implementation, which is the
Task 10.18 gap; the inventory is evidence, not design authority. Each Go task
still enforces 512 MiB, so global claims supplement local memory safety.

MCP denial retains its current MCP error and Retry-After 1. SSE identity denial
retains 429 and Retry-After 5; shared-store failure uses existing 503 and 5.
Render denial remains 503 Retry-After 1 through current owner/public callers.

## Strict local render affinity

- Queue may generate an inert job UUID before acquiring its shared render claim.
  It installs no local job, capability, controller, snapshot or callback until
  exact positive acquisition. The claim binds that UUID; the UUID alone grants
  nothing. Its admitted_at starts the unchanged 20-second attempt deadline.
- All authority remains in that queue. Database stores only claim ID, job ID,
  replica ID, state, and deadline. It never stores snapshot bytes, capability or
  controller hashes, artifact bytes, or terminal authority.
- Queued-to-running promotion atomically checks original deadline and the one
  global running count. It does not extend deadline.
- Go Chromium reaches paired Nuxt at the node-local fixed port. Nuxt redeems
  through Caddy's API-only bridge listener; Caddy forwards only the redemption
  operation to paired Go loopback. No ALB, public Caddy route, Cloud Map, or
  arbitrary service discovery participates.
- Redemption remains one in-memory compare-and-set over job/resume/audience/
  capability/snapshot digest/expiry. Ambiguous redemption response is consumed
  and never retried. Controller completion remains unexported and in process;
  job ID alone grants nothing. Completion recomputes terminal artifact digest.
- Failure, cancellation, and deadline cancel and join local work, remove local
  authority, then release claim. Crash does not retry or release until external
  fence. Caller retry creates a wholly new attempt and authority.

## Realtime

- Retain one pooled PostgreSQL LISTEN connection per Go replica and local hub.
  No SSE stream owns a DB connection. Revision NOTIFY stays lossy invalidation.
- Before local subscription, atomically acquire shared IP and optional account
  claims, then local per-task/FD admission. On local failure release shared
  claims. Close releases both idempotently.
- Keep 2,000 streams per task, 100/IP fleet, 20/account fleet, eight queued
  events, two-second write deadline, 25-second heartbeat, and 25% FD headroom.
- Owner session validity is rechecked before every revision and heartbeat.
  Public streams hold the existing local revoking lease. Fleet transition ack
  waits their local cancellation/release before revocation success.
- Listener/shared coordination loss closes subscriptions and rejects admission.
  Graceful drain rejects new streams and closes existing within one heartbeat;
  revocation uses five seconds. No final event is added. Reconnect performs
  unconditional refetch; missed events and abrupt loss repair that way.

## Workers and scheduled UAT boundary

- Auth mail already uses durable jobs/leases. Add global two-send claims. A new
  worker cannot retry a lease whose old send may still execute until the old
  replica is fenced; provider ambiguity retains existing terminal/retry rules.
  Do not run two independent full-concurrency workers by accident.
- Media deletion/orphan modes retain PostgreSQL advisory overlap locks, exact
  pages/runs, four object workers, 30-second job lease, five-second I/O, retry
  and 24-hour removal target. Scheduled tasks, not web replicas, own execution.
- Privacy, idempotency, OAuth cleanup, and retention commands remain bounded
  one-shot tasks with existing database locks and deadlines.
- UAT no-op suppression follows uat-lifecycle.md. It remains disabled until
  hosted startup and orphan-list evidence satisfy that contract. Missing, stale,
  ambiguous, due, or backlog proof keeps RDS running.

## Observability

Metrics: decisions by policy/outcome/private/overflow; partition active keys;
overflow saturation; Retry-After; DB latency/error/ambiguous result; raw clock
jump and clamp; pending attempt age; claims running/waiting/blocked-on-fence;
render queue/deadline; SSE local/fleet counts and slow disconnects. Labels never
contain raw IP, account, email, token, capability, job content, or arbitrary
key.
