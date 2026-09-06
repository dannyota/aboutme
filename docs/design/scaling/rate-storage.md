# Fleet rate storage

Status: Accepted detail of [fleet admission](admission.md) under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). R1 owns
schema/store work, R5 owns encoding/adapters and R7 owns callers. Implementation
and local proof remain pending.

Preserve all 24 policies in the [policy catalog](policy-catalog.md).
[Rate identities](rate-identities.md) fixes typed key encoding and deployment
key versions. [OAuth attempts](admission-attempts.md) fixes P22 reservations and
terminal receipts. This contract changes storage and time authority while
retaining caller responses and ordered debt.

## Closed policy catalog table

shared_rate_policies is owner-owned and seeded with exactly P01-P24:

- policy_id text primary key with the exact catalog spelling;
- algorithm text in token_bucket, fixed_window, rolling_slug;
- capacity integer positive; window_microseconds bigint positive;
- key shapes live in immutable shared_rate_policy_key_shapes(policy_id,
  key_shape) with key_shape in ip, peer_ip, account, account_ip, email_digest,
  oauth_client, token, user and primary key(policy_id,key_shape);
- allow_success_clear boolean;
- denied_attempt_adds_debt boolean;
- ordinary_idle_microseconds bigint fixed to 86,400,000,000;
- max_keys_per_partition integer fixed 10000.

Checks bind each policy to its accepted literal values. The shape relation binds
P05 to ip and account_ip. Middleware policies P01-P05, P13-P16 and P19 also
accept peer_ip solely on the failed canonical-client-IP path. Every other policy
has its single catalog shape. The tables have no runtime UPDATE/DELETE path. P22
remains fixed_window with its separate attempts. No caller supplies capacity,
window, algorithm, key shape, cleanup horizon, or partition limit.

## Shared time

shared_policy_clocks has policy_id primary key/FK, high_water_at and last_raw_at
not null, and anomaly_count bigint nonnegative. Each mutation locks the clock,
samples clock_timestamp once, and sets effective_now to greatest(high_water_at,
raw_now). It records raw_now and never lowers high_water_at. Database timestamps
have PostgreSQL microsecond precision. Callers provide no time.

A backward sample advances no token, window, rolling cutoff, idle expiry, or P22
expiry. A forward sample may refill only to capacity. Anomaly counting is
observability and never changes a decision.

## Partition and key state

shared_rate_partitions has primary key (policy_id, partition), partition 1 or 2,
enabled boolean, capacity_generation positive bigint, operation_id bounded
printable text, active_keys integer 0..10000, and updated_at database time.

active_keys equals the committed count of shared_rate_buckets rows for that
policy/partition. Only the owner definer that inserts/deletes a bucket changes
it, under the locked partition row. Disabling a partition never changes
active_keys, deletes a row, moves a key, clears debt, or prevents an existing
key from being evaluated. Disabled partitions cannot own a new key. Re-enabling
exposes the same rows and debt. Lifecycle toggles flags only through its
accepted capacity action.

shared_rate_buckets has primary key (policy_id,key_digest), key_digest bytea
length 32, partition 1 or 2, algorithm matching catalog, last_seen not null,
and:

- token_numerator bigint nullable;
- refill_at timestamptz nullable;
- window_started_at timestamptz nullable;
- count integer nullable;
- rolling_events timestamptz[] nullable.

It also has unique (policy_id,partition,key_digest). token_bucket requires only
nonnegative token_numerator and refill_at. fixed_window requires count 0..10.
For P07, window_started_at is null exactly when count=0. For P22, count may be
zero with a nonnull start while a stored pending attempt remains bound to that
exact bucket identity and window_started_at, even after its effective_until. The
owner mutators and a deferred owner constraint trigger assert the at-rest
relation: start is null exactly when count is zero and no stored pending row is
bound to the bucket; every stored pending row matches the bucket's current
window identity. The matching overflow assertion uses policy and window
identity. A row CHECK never queries attempts. For each exact P22 bucket/window
identity, committed count plus the number of stored pending rows is at most 10;
expired-but-unresolved pending rows still count toward this at-rest bound. The
trigger does not compare effective_until with the policy clock or require
unrelated buckets to be swept when that clock advances. rolling_slug requires
only a one-dimensional rolling_events array with cardinality 0..30, no nulls,
nondecreasing values, and count equal to cardinality; window_started_at,
token_numerator and refill_at are null. Definers maintain array shape; an owner
constraint trigger independently asserts it.

shared_rate_overflow has policy_id primary key/FK and the same algorithm state,
last_seen, and shape checks, but no key identity or partition. It is always
present. Success clear is a no-op on overflow. Overflow never contributes to
active_keys and has no identity-specific deletion.

## Exact token arithmetic

For a token policy with accepted capacity C and window W microseconds, one token
is W numerator units and the full bucket is C*W. Store only integer numerator.
At effective_now, validate C, W, current numerator and full=C*W with checked
arithmetic. Let missing=full-current and threshold=ceil(missing/C) microseconds.
First compare the PostgreSQL timestamp interval effective_now-refill_at with
that small bounded threshold, without converting the possibly huge elapsed
interval to bigint microseconds. If it meets the threshold, assign full
directly. Otherwise convert the now-proved-bounded elapsed interval to whole
microseconds; elapsed*C is bounded by missing and safe to add. This caps before
multiplication, so any valid forward timestamp jump saturates rather than
failing on intermediate overflow. Set refill_at to effective_now. An allow
requires numerator >= W and subtracts W. A denial subtracts nothing. This
exactly represents the rational rate C/W at PostgreSQL clock precision.

For denial, D=W-token_numerator. Compute ceil(D/C) by quotient plus a nonzero-
remainder bit, without adding C-1. Convert microseconds to seconds the same way;
Retry-After is at least one. Allowed returns zero. Both outcomes update
last_seen.

## Fixed-window and rolling state

For P07, first recorded failure sets window_started_at=effective_now,count=1.
Later failures inside 15 minutes increment through 10. State and denial do not
increment. At effective_now >= start+15 minutes, state is empty and the next
failure starts a new window. Private ClearSuccess deletes the bucket and
decrements its partition active_keys under the same transaction; overflow clear
does nothing. P09 remains token bucket and shares only the typed email-digest
key source, not P07 state.

P12 first prunes events <= effective_now-1 hour. If fewer than 30 remain, append
and allow. If 30 remain, remove the oldest, append effective_now, and deny. Thus
the array is bounded at 30 and a denied attempt delays future admission exactly
as current code. Retry-After remains the accepted fixed 1 second for denial.
Every attempt updates last_seen.

P07 FailureState locks the clock and performs a nonlocking route read. An
existing private bucket is authoritative even in a disabled partition. Lock it,
evaluate the unchanged debt/window state, and set only last_seen=effective_now.
If absent, lock partitions 1 then 2 and route from committed enabled and
active_keys values. If any enabled partition is below 10000, return unexhausted
without allocation; there is no bucket whose activity can be refreshed. If every
enabled partition is full, or none is enabled, lock/read overflow and set only
its last_seen=effective_now. An expired ordinary row still counts until
RecordFailure's bounded cleanup removes it. FailureState never sweeps, inserts,
deletes, changes active_keys, changes count/debt, or starts/resets a window.
ClearSuccess routes only to the exact existing private bucket. If absent, return
cleared=false/bucket_kind=private without reading, clearing or allocating
overflow. If present, lock its partition then bucket, delete it, decrement
active_keys, and return cleared=true/private; it does not refresh last_seen.

P22 bucket count plus pending attempts is evaluated exactly as
admission-attempts.md. For the selected bucket, reserve, finish, or cleanup
locks the bucket and its pending attempts, resolves expired pending rows, then
enforces the operation rule that the window is absent exactly when committed
count and effective pending debt are both zero. Clearing/resetting is local to
that selected bucket. A clock advance while evaluating key A does not mutate key
B; an expired but unresolved pending row for B remains stored and keeps B's
at-rest window identity valid until an operation locks and resolves B. Its
SHA-256 domain+UUID digest, reservation/finish behavior, window timestamps,
terminal reasons, partial indexes, ambiguity resolution, and 24-hour terminal
receipt retention are unchanged. Pending expiry is not proof that work ended and
never authorizes bucket deletion without the accepted locked resolution path.

## Seed contract

The migration samples clock_timestamp once and seeds:

- exactly 24 immutable policy rows and the 35 accepted policy/key-shape rows;
- exactly 24 clock rows with high_water_at=last_raw_at=sample and
  anomaly_count=0;
- partitions 1 and 2 for every policy, both disabled, active_keys=0,
  capacity_generation=1, operation_id=bootstrap-uncomposed-v1,
  updated_at=sample;
- one overflow row per policy: token buckets full with
  refill_at/last_seen=sample; fixed windows count 0/start null/last_seen=sample;
  rolling array empty,count 0, last_seen=sample;
- no ordinary bucket and no admission-attempt row.

The seed creates no admission or debt. First serving activation enables logical
partition 1; second enables 2. Existing native limiters remain authoritative
until R5/R7 caller composition, so disabled target rows do not change current
behavior.

## Allocation and cleanup

Existing-key admission locks clock then bucket and evaluates it even if its
partition is disabled. New-key admission locks clock, partitions 1 then 2, and
runs bounded legitimate expiry for the requested policy before selecting the
lowest enabled partition with active_keys<10000. It examines and deletes at most
one eligible oldest row per full enabled partition; ordinary bulk cleanup stays
a maintenance job. Insert and active_keys increment commit together. If none can
own it, lock/evaluate the policy overflow; capacity refusal never grants work.

runtime_cleanup_rate_buckets(policy_id text,page_size integer) is
maintenance-only. page_size is 1..256. It locks the policy clock, samples
effective_now, selects at most page_size eligible ordinary rows ordered by
last_seen,key_digest, then locks their partition rows in numeric order and
bucket rows in key order. It rechecks:

- token bucket is full at effective_now or idle at least 24 hours;
- fixed window carries no count/debt or is idle at least 24 hours;
- rolling array is empty after pruning or is idle at least 24 hours.

Candidate discovery is a nonlocking read: no FOR UPDATE and no SKIP LOCKED. Then
lock every affected partition in numeric order and candidate buckets in
key-digest order, and recheck identity and eligibility. Lock the selected
policy's one overflow row after the ordinary bucket rows. For P22, then lock all
pending attempts for the candidate ordinary buckets and overflow in UUID order.
Resolve only rows expired at effective_now under the accepted
neutral/window-expired rules, and reject an ordinary deletion while any
effective pending row remains. The 24-hour idle backstop never overrides
effective pending debt.

In the same transaction, normalize overflow only through the selected
algorithm's accepted elapsed-time rule. Token overflow refills, with checked
arithmetic, only to the full numerator. P07 overflow clears count and start only
when its fixed window has expired. P12 overflow prunes only events at or before
the accepted one-hour cutoff. P22 overflow resolves only expired pending
attempts, clears an expired committed window, and sets start null only after
committed count and effective pending debt are both zero. Overflow is never
deleted. Cleanup performs no consume, failure increment, rolling append,
reservation, synthetic admission, or 24-hour idle erasure of overflow debt.

Delete only rechecked ordinary rows and decrement each partition active_keys by
its exact deleted count. While still holding the policy clock and acquired rows,
compute policy_idle. It is true exactly when no ordinary bucket exists for the
policy, its normalized overflow is neutral (token numerator full; fixed count
zero, start null and no effective pending; or rolling event array empty), and
for P22 no pending attempt exists in any bucket. Expired P22 rows count as
pending until this or another accepted selected-bucket operation resolves them.
Terminal receipts do not affect policy_idle. Because one call deletes at most
page_size ordinary rows, policy_idle remains false until repeated calls remove
every eligible ordinary row; an ineligible/debt-carrying row also keeps it
false. P22 receipt cleanup remains its separate accepted function. There is no
global cleanup call or caller-selected retention.

## SQL and Go surface

All mutations are SECURITY DEFINER, runtime_owner-owned, search_path pg_catalog,
fully qualified, no dynamic SQL, with PUBLIC/table DML revoked. They run through
WriteTxRunner and fixed statement triggers. Bookkeeping does not extend
last_accepted_writer_at.

- runtime_admit_token_rate(policy_id,key_digest) returns allowed boolean,
  retry_after_seconds integer, bucket_kind private|overflow, partition nullable,
  effective_at timestamptz.
- runtime_password_failure_state(key_digest) returns exhausted boolean,
  retry_after_seconds integer, bucket_kind private|overflow, partition nullable,
  effective_at, allocated boolean fixed false, activity_refreshed boolean.
  Existing private and selected overflow results return activity_refreshed=true.
  Absent routable private state returns private/null partition, unexhausted, and
  activity_refreshed=false; saturated routing returns overflow.
- runtime_record_password_failure(key_digest) returns exhausted boolean,
  retry_after_seconds integer, bucket_kind private|overflow, partition nullable,
  effective_at, allocated boolean, activity_refreshed boolean. allocated=true
  only when this call creates a private bucket; it is false for an existing
  private bucket and overflow. Every successfully selected and recorded private
  or overflow bucket returns activity_refreshed=true.
- runtime_clear_password_failure(key_digest) returns cleared boolean,
  bucket_kind private fixed, partition nullable. Absent returns false/null and
  never reads overflow; present returns true and its former partition.
- runtime_admit_slug_change(key_digest) returns allowed boolean,
  retry_after_seconds integer,bucket_kind,partition nullable,effective_at.
- runtime_reserve_oauth_failed_grant(attempt_id,client_id),
  runtime_finish_admission_attempt(attempt_id,outcome), and
  runtime_cleanup_admission_attempt_receipts(page_size) retain their exact
  accepted P22 shapes and grants.
- runtime_cleanup_rate_buckets(policy_id,page_size) returns deleted_count
  integer, effective_at timestamptz, policy_idle boolean. policy_idle is the
  locked, post-normalization predicate above, not a promise that later admission
  stays idle after this transaction commits.

The token function accepts only catalog token policies. Password functions
hardcode P07. Slug hardcodes P12. P22 functions hardcode P22. Unknown or
algorithm-mismatched policy is 55000 before mutation. App executes
admission/state/record/clear and P22 reserve/finish. Maintenance executes only
both cleanup functions. Lifecycle may toggle partitions only through its
existing capacity definers. No role has direct DML or a generic state-update
function.

Go defines Policy, RateKey [32]byte, Decision, FailureState, AttemptReservation,
AttemptResolution, and CleanupResult. RateStore exposes AdmitToken,
FailureState, RecordFailure, ClearFailureSuccess, AdmitChangedSlug,
ReserveFailedGrant, FinishAttempt, and cleanup only on the maintenance adapter.
Callers retain their current response mapping and ordering. Store errors remain
unavailable and never become allow, retry, refund, or identity disclosure.

## Lock order

After the runtime write barrier: policy clock; for allocation, partitions 1 then
2; bucket or overflow; P22 attempts in UUID order. An existing FailureState
bucket omits partition locks. Absent FailureState locks partitions 1 then 2 to
select from committed capacity, then locks overflow only when capacity is
saturated or no partition is enabled. Cleanup uses clock, nonlocking candidate
discovery, partitions numeric, ordinary buckets by key digest, the one overflow
row, then all selected P22 pending attempts by UUID. It computes policy_idle
before releasing those locks. Lifecycle uses capacity before partitions and
never locks clocks/buckets. No rate function locks membership, transition, user,
resume, token, client, or email rows.

## Adversarial failing-first obligations

- Seed set/count/literals, closed catalog, ownership, real-role grants, PUBLIC
  and direct DML denial, statement-entry assertions, search_path and no dynamic
  SQL.
- Fixed digest vectors for every shape; IPv4-mapped equivalence; IPv6
  distinction; nil UUID/malformed IP refusal; P05 anonymous/authenticated
  separation; peer/viewer same-byte separation and fallback response; per-policy
  allowed-shape rejection; policy and secret separation; no raw
  IP/email/bearer/display key in DB/log/error.
- Token exact-boundary refill/consume/deny/retry for every catalog rate, long
  forward jump cap, backward clock clamp, bigint checked arithmetic,
  concurrency.
- P07 first-window and nonextension; State on existing private and selected
  saturated overflow refreshes only last_seen; absent routable State allocates
  nothing and refreshes nothing; State never sweeps or changes debt/window;
  enabled/disabled 10000-key routing; Clear never refreshes; failure increment,
  private clear, overflow no-clear, anti-enumeration and identical caller
  response.
- P12 29/30/31, exact cutoff, denied newest debt, repeated denial, backward
  clock, 24-hour idle, overflow sharing, and two-pool serialization.
- P22 count-zero first pending with nonnull window, pending counts as debt,
  finish outcomes, empty-state restoration, private success clear, overflow
  debt, UUID conflict/replay, expiry before bucket/receipt GC, 24-hour receipt
  and page 256; advancing the clock through key A may leave key B's expired
  unresolved pending row and matching nonnull window at rest until B is selected
  and resolved.
- Existing disabled-partition key still charged; no new disabled ownership;
  enable/disable preserves rows/debt; active_keys equals rows under
  insert/cleanup races; 10000 boundary; overflow admission; no capacity-based
  allow.
- Cleanup nonlocking discovery then partition/bucket locks, exact eligibility,
  page 1/256/reject 0/257, deterministic recheck, active_keys decrement, crash
  rollback, no effective P22 detachment, and allocation/ClearSuccess two-pool
  races; overflow token partial/full refill, P07 before/at expiry, P12 cutoff,
  P22 live/expired pending and count, forbidden idle erasure/synthetic
  admission, policy_idle false for remaining page/debt/pending and true only for
  exact neutral state, with terminal receipts ignored.
- P01 two chains share policy; P13 then P14 and P23 then P24 preserve partial
  debt; P17 before cache-miss-only P18; P19 bypass and all current HTTP/MCP
  responses.
- Store ambiguity and database outage fail closed without automatic retry. No
  shared claim, local C06 photo, public transition, or product limit changes.
- Rotation with keyed ordinary debt, overflow debt, P22 pending, active/waiting
  claims, mixed readiness tuples, partial rollout, and ambiguous cleanup remains
  closed; debtless terminal P22 receipt retention does not block the zero-debt
  proof; neither key is exposed to SQL or a caller-selected generation.

R1 authors run the focused migration/store tests and affected database gates.
R5/R7 authors run their owned package tests. Root owns migration numbering, sqlc
generation, full phase checks and hosted authorization. No runtime acceptance is
claimed here.
