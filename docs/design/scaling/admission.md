# Fleet admission, render and realtime

Status: Accepted under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). Not built.
The [scaling index](README.md) holds the shared rules.

With two replicas, every limit in the [policy catalog](policy-catalog.md) must
hold across the fleet. Rate policies P01 to P24 and claim classes C01 to C05
move to PostgreSQL. C06 photo intake and the per-task SSE cap stay local. Each
caller keeps its key construction, order, response and `Retry-After`.

## Shared time

`shared_policy_clocks` holds one row per policy: `high_water_at`, `last_raw_at`
and `anomaly_count`. Each rate mutation locks its clock, samples
`clock_timestamp()` once and uses
`effective_now = greatest(high_water_at, raw)`. The high-water value never moves
back. A backward step refills nothing and extends no window. A forward jump
refills only up to capacity. Anomaly counts are metrics only. This replaces ADR
0018's injected process clock for fleet policies.

Tests replace the owner-only sampling helper only in a disposable database.
Production has no time input.

## Rate state

`shared_rate_policies` is a closed, immutable catalog seeded with P01 to P24:
algorithm (`token_bucket`, `fixed_window` or `rolling_slug`), capacity, window,
allowed key shapes, a 24-hour idle horizon and 10,000 keys per partition.
Success clear exists only for P07 and P22. Denied attempts add debt only for
P12. Callers supply none of these values.

- `shared_rate_partitions`: rows (policy, 1) and (policy, 2) with `enabled`,
  capacity generation and `active_keys` from 0 to 10,000. Lifecycle actions
  toggle all 24 rows of one partition together
  ([membership](membership.md#lifecycle-actions)). Disabling keeps rows and
  debt, and an existing key is still charged.
- `shared_rate_buckets`: primary key (policy, 32-byte key digest), partition,
  algorithm state and `last_seen`. `active_keys` always equals the row count.
- `shared_rate_overflow`: one per policy, with the same state and no identity.
  When no enabled partition can take a new key after legitimate expiry, the
  request is charged to overflow. Capacity never causes a refusal, and no active
  key is evicted.

The seed creates both partitions disabled, full overflow buckets and no keys.

### Algorithms

- **Token bucket.** One token is W units of an integer numerator, and a full
  bucket is C × W, for capacity C and window W in microseconds. Refill is exact
  at microsecond precision, capped before multiplication so no jump overflows.
  An allow subtracts W. A denial subtracts nothing and returns a `Retry-After`
  rounded up to at least one second.
- **P07 login failures.** The first failure starts a 15-minute window, and later
  failures do not extend it. Ten failures exhaust it. Reading state only
  refreshes `last_seen` and never allocates. A successful login deletes an
  existing private bucket and never clears overflow.
- **P12 slug changes.** Up to 30 event times per hour. A denial replaces the
  oldest event with the current one, so denied attempts carry debt.
  `Retry-After` is 1.
- **P22 failed OAuth grants.** The caller reserves an attempt UUID before grant
  validation. Committed failures plus pending reservations are capped at 10 per
  15 minutes, and the first reservation starts the window. Finish converts the
  reservation to a failure, releases it as neutral, or on success clears the
  private bucket. Overflow success releases only its own reservation. Expired
  reservations resolve as neutral. `shared_admission_attempts` keeps terminal
  receipts for 24 hours. An exact replay returns the stored outcome, and a
  different outcome conflicts. The bucket key is SHA-256 over
  `aboutme.oauth.failed_grant.v1` and the client UUID.

### Allocation and cleanup

An existing key locks its clock and bucket, even in a disabled partition. A new
key locks partitions 1 then 2, removes at most one expired row per full
partition, then takes the lowest enabled partition below 10,000 or falls back to
overflow.

Maintenance calls `runtime_cleanup_rate_buckets(policy, page_size)` and the P22
receipt cleanup with page sizes 1 to 256. Discovery reads without locking, then
locks and rechecks each row. A row is removable when it carries no debt or has
been idle 24 hours. P22 rows with live pending reservations are never removed.
Overflow is normalized but never deleted. Cleanup returns `policy_idle` only
when no ordinary bucket, overflow debt or pending reservation remains.

## Key identity

Go alone builds keys through a typed encoder, never from a string. It computes
HMAC-SHA-256 with the admission key over:

1. `aboutme.rate-key.v1` and a zero byte;
2. the policy ID, prefixed by a u16 big-endian length;
3. a one-byte component count;
4. per component: a type byte (1 IP, 2 peer IP, 3 account, 4 email digest, 5
   OAuth client, 6 token, 7 user), a u16 big-endian length and the payload.

IPs are unmapped once and encoded as a family byte plus 4 or 16 bytes. UUIDs are
16 bytes and must be non-nil. Email is the existing 32-byte HMAC digest.
Component order is fixed per policy; `account_ip` is account then IP. When the
canonical client IP fails, middleware policies charge the socket peer under the
distinct peer type and keep their current responses. SQL sees only the policy ID
and the final digest.

Every replica pins one deployment tuple: the admission key version and the
password-email key version. Composition checks the loaded versions before it
builds any adapter. The controller checks the same tuple before each activation.
A mixed or unverifiable tuple keeps the replica unready.

A key change moves every rate and claim identity. Rotation therefore:

1. closes all application admission and activation on every replica;
2. joins or fences all work and proves zero running and waiting claims;
3. runs the bounded cleanups until every policy returns `policy_idle`, letting
   windows mature at database time;
4. switches the tuple and recomposes every replica, with no old-key fallback;
5. reopens only after every activated replica matches the new tuple.

Any doubt keeps admission closed.

## Caller behavior

- Composite policies keep their order in one transaction and never refund: P13
  then P14, P23 then P24, and the multi-part password policies. A later denial
  keeps the earlier debt.
- An ambiguous rate commit takes the caller's unavailable path. It accepts
  possible debt, starts no work and never retries.
- Store failure never runs the protected handler. Ordinary and public routes use
  their unavailable response. Password returns 503 `authentication_unavailable`.
  Resume mutation returns 503 `public_state_busy` with `Retry-After: 1`. Account
  returns 503 `account_unavailable`. MCP returns 503 `agent_access_unavailable`.
  OAuth register and token return 500 `server_error`.
- The two outer API chains share one P01 policy: 300 per client IP per minute.
  Health and public bypasses stay unchanged.

## Shared claims

Claims authorize concurrent work, so an ambiguous acquire must be resolved
before work starts.

| Policy                 | Scope   | Running | Waiting | Deadline   |
| ---------------------- | ------- | ------: | ------: | ---------- |
| `render.global_claim`  | global  |       1 |       8 | 20 seconds |
| `password.hash`        | global  |       2 |      16 | none       |
| `mail.send`            | global  |       2 |       0 | none       |
| `mcp.user_concurrent`  | user    |       4 |       0 | none       |
| `sse.fleet_account_ip` | ip      |     100 |       0 | none       |
| `sse.fleet_account_ip` | account |      20 |       0 | none       |

Tables: an immutable catalog; `shared_claim_scope_summaries` with running and
waiting counts and a never-wrapping allocation ordinal; `shared_claim_requests`
with a caller UUID, replica, state (`waiting`, `running` or `released`), a
render job UUID for C01 only, a deadline for C01 only, and a release reason
(`joined`, `canceled`, `expired` or `fenced`); and `shared_claim_scopes`, one or
two per request. An SSE request charges its IP and optional account scopes
atomically. A denial leaves no partial charge.

Acquisition locks capacity (admission must be on), the caller's `active` replica
row, the request, catalog rows, then scope summaries in byte order. Promotion
gives a free running slot to the oldest live waiter. C01's 20 seconds start at
admission and include waiting. An expired waiter may be released, but a running
claim is never reclaimed by age.

Scope digests are HMAC-SHA-256 with the admission key over
`aboutme.shared-claim.scope.v1`, a zero byte, version `0x01`, then
length-prefixed policy, kind and value. The request digest is SHA-256 over the
claim, policy, replica, optional work UUID and ordered scope digests. The pinned
SSE vector hashes to
`0733ec593ac2751902ebac6d8175187ea5c2e695f47264b36b1608a2576cbbac`.

Ambiguity rules:

- An ambiguous acquire starts no work. The caller resolves the exact UUID.
- Confirmed absence allows one reacquire with the same UUID, only in the same
  operation, under five minutes old, with the original context and deadlines
  still live. A restart loses that permission.
- A late positive result after cancellation releases only after the caller
  proves local work never started or has joined.
- Release is idempotent by UUID and digest. Released receipts stay 24 hours, and
  maintenance deletes at most 256 per transaction.

Graceful cancellation releases after local work joins. After a crash, claims
stay charged until [EC2 termination proof](membership.md#termination-proof).
Time, heartbeats and lock loss never release a live claim.

## Render affinity

The render queue may create an inert job UUID first. It installs no job,
snapshot, capability, controller or callback until the claim is confirmed. The
database stores only claim, job, replica, state and deadline. Snapshots,
capabilities, artifacts and completion authority stay in the initiating Go
process. Redemption stays one in-memory compare-and-set, and an ambiguous
redemption is consumed, never retried. Failure, cancellation and deadline cancel
and join local work, then release the claim. A caller retry is a new attempt
with new authority.

## Realtime

- Each replica keeps one pooled revision `LISTEN` connection and a local hub. No
  stream holds a database connection. `NOTIFY` stays lossy invalidation, and
  reconnect refetches.
- A stream acquires its shared IP and account claims, then local admission. A
  local failure releases the shared claims.
- Limits: 2,000 streams per task, 100 per IP and 20 per account across the
  fleet, eight queued events, a two-second write deadline, a 25-second heartbeat
  and 25 percent file-descriptor headroom.
- Owner session validity is rechecked before each revision and heartbeat.
  Revocation acknowledgement waits for local stream cancellation.
- Coordination loss closes streams and rejects admission. Drain closes streams
  within one heartbeat, and revocation within five seconds. No SSE event is
  added.

## Workers

- Auth mail keeps its durable leases and gains the fleet `mail.send` claim. A
  lease whose old send may still run is not retried until that replica is
  fenced.
- Media cleanup keeps its advisory locks, four object workers, 30-second lease,
  five-second I/O and 24-hour removal target.
- Privacy, idempotency and OAuth cleanup stay bounded one-shot jobs under their
  existing locks.

## Lock order

Rate mutations lock the policy clock, then partitions 1 then 2 when allocating,
then a bucket or overflow, then P22 attempts in UUID order. Lifecycle locks
capacity before partitions and never touches clocks or buckets. Rate functions
lock no membership, transition or business row. Claims follow the
[membership lock order](membership.md#lock-order).

## Required proof

- Digest vectors for every key shape, IPv4-mapped equality, IPv6 distinction,
  and policy, domain and key separation.
- Exact token boundaries, jump caps and clock clamps for every policy.
- P07, P12 and P22 window, debt and clear rules, including overflow and
  concurrent reservations.
- The 10,000-key boundary, disabled partitions, overflow, and cleanup page
  limits of 1 and 256.
- Exact claim caps with two pools, atomic SSE denial, promotion order, late
  positives, the five-minute boundary, and drain and fence races.
- Ambiguity and outage fail closed with no automatic retry.
- Replica counts one and two for every policy and claim class.
