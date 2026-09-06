# OAuth failed-grant reservations

Status: Accepted detail of [fleet admission](admission.md) under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md).
[R1](../../plans/phase-10/replica/runtime-tasks.md) owns schema/store work, R5
owns the admission package, and R7b owns OAuth callers. Implementation and live
proof remain pending.

## Policy and identity

P22 `oauth.failed_grant` retains the OAuth client UUID key and ten failures plus
pending reservations per 15 minutes. The first admitted reservation starts the
fixed window, before its result is known. Later reservations and failures do not
extend it. Denial retains HTTP 429, positive ceiling Retry-After and OAuth
`invalid_request`. Store failure retains OAuth 500 `server_error` and starts no
grant validation.

The caller generates a fresh non-nil attempt UUID before each request's reserve
operation. An ambiguous reserve is never retried, refunded or read back to
authorize work. A later independent request uses a new UUID.

For the shared bucket key, hash the fixed UTF-8 bytes
`aboutme.oauth.failed_grant.v1` followed by the client's 16 UUID bytes with
SHA-256. SQL derives this from its client UUID input using pg_catalog.sha256,
convert_to and uuid_send. This 32-byte representation preserves the UUID key; it
adds no secret or email input. The attempt retains the client UUID so exact
replay checks do not depend on hash equality alone.

## Attempt rows

Create owner-only `public.shared_admission_attempts` with these fields. Fields
are non-null unless the table specifies nullable or scope-dependent values.

| Field                                                       | Constraint                                                            |
| ----------------------------------------------------------- | --------------------------------------------------------------------- |
| attempt_id uuid                                             | Primary key, non-nil UUID                                             |
| policy_id text                                              | Exact `oauth.failed_grant`                                            |
| client_id uuid                                              | Non-null; token caller preserves its non-nil client check             |
| bucket_kind text                                            | `private` or `overflow`                                               |
| partition smallint                                          | 1 or 2 for private; null for overflow                                 |
| key_digest bytea                                            | 32 bytes for private; null for overflow                               |
| window_started_at, reserved_at, effective_until timestamptz | Non-null                                                              |
| state text                                                  | `pending` or `terminal`                                               |
| outcome text                                                | Nullable `failure`, `neutral` or `success`                            |
| terminal_reason text                                        | Nullable `caller_finish`, `private_success_clear` or `window_expired` |
| terminal_at timestamptz                                     | Null while pending                                                    |

Require `window_started_at <= reserved_at < effective_until` and
`effective_until = window_started_at + interval '15 minutes'`. Pending rows have
no outcome, terminal reason or terminal time. Terminal rows have all three and
`terminal_at >= reserved_at`. Private-success clear requires private scope and
neutral outcome. Window expiry requires neutral outcome and
`terminal_at >= effective_until`.

Index pending private rows by policy/key/attempt, pending overflow rows by
policy/attempt, pending expiry by effective_until/attempt, and terminal rows by
terminal_at/attempt. Include state in each appropriate partial predicate.

Do not reference buckets or OAuth clients with a foreign key. Success removes
the private bucket, and client retention may remove the client while terminal
receipts must remain available. Definer functions atomically bind the attempt to
its exact bucket and window. Attempt IDs and concurrent-work claim IDs are
separate types and tables.

## Write boundary and SQL surface

WriteTxRunner owns zero-argument runtime_enter_write, callback execution,
runtime_finish_write and commit. Reserve, finish and cleanup are operations on
its transaction-bound Queries. They never enter or finish another write. Attach
runtime_assert_write_entry as a statement trigger to new mutable tables. P22
rate, attempt and cleanup mutations are bookkeeping. They never call the
business-write assertion or extend last_accepted_writer_at. Never call a trigger
function as an ordinary function.

All new objects belong to aboutme_runtime_owner. Revoke PUBLIC and direct
runtime-role table DML. Definer functions use search_path=pg_catalog and fixed,
qualified names:

- `runtime_reserve_oauth_failed_grant(uuid,uuid)` takes attempt and client IDs.
  It returns allowed, retry_after_seconds and, only when allowed, bucket_kind
  and partition. The internal result also includes replayed: false for fresh
  allowance/denial, true for exact retained identity replay. Grant EXECUTE only
  to app. [Fixed rate results](rate-operations.md) specifies the exact matrix.
- `runtime_finish_admission_attempt(uuid,text)` takes an attempt and a closed
  outcome enum. It returns one row with resolution and stored_outcome.
  Resolution is `caller_finished`, `caller_replay`, `system_noop` or
  `absent_noop`; only absent_noop has a null stored_outcome. Grant EXECUTE only
  to app.
- `runtime_cleanup_admission_attempt_receipts(integer)` accepts a page size from
  1 through 256 and returns the deleted count. Grant EXECUTE only to
  maintenance.

There is no app-readable attempt lookup. Domain types expose ReserveFailedGrant
and FinishAttempt with context/error semantics; they expose no pgx handle or
caller clock. Shared-store errors preserve each caller's existing response.

## Lock order and time

Every rate mutation, including maintenance, holds the runtime barrier then the
policy clock. Sample database time once after locking that clock and clamp it to
its durable high-water value. Use that effective time for all window and
terminal decisions. Production callers cannot supply time.

- A private path that can allocate/delete a bucket or change active_keys locks
  partitions in numeric order, then its bucket, then attempts in UUID order.
  This includes allocation, bucket cleanup, private success and neutral finish
  that may remove the last debt.
- Existing-key admission that updates or resets in place locks clock, bucket,
  then attempts. It never later requests a partition lock. Other bucket writers
  hold the same clock, so they cannot delete the bucket between routing and its
  row lock. A routing mismatch is corruption/unavailable with rollback and no
  internal retry.
- Overflow locks clock, the single policy overflow row, then attempts in UUID
  order. Receipt-only cleanup locks clock then terminal rows in terminal_at/UUID
  order; it touches no bucket or partition.

Use a nonlocking routing read where needed, then revalidate all identity fields
under the required locks. No path takes bucket then partition or attempt then
bucket. General rate cleanup follows the same order and never deletes a P22
bucket carrying effective pending debt.

Lifecycle changes lock runtime capacity then partitions. They neither lock
policy clocks/buckets nor delete buckets. After waiting, rate work revalidates
partition enabled/generation state. These orders add no reverse lock edge.

## Reserve and finish

Reserve checks an existing attempt UUID for exact policy/client identity under
the same lock order. Conflicting reuse fails. Stored replay exists for
idempotency; an OAuth caller must never use it to authorize an ambiguously
admitted request.

An exact retained pending or terminal receipt returns its historical admitted
bucket shape with allowed=true, retry zero and replayed=true. It does not
reroute through a current bucket or consume another slot. R5 rejects every
replay for new work. No attempt state or timestamp is added to the result; the
caller still never retries or reads back an ambiguous reserve to authorize work.

For a new key, legitimately expire debt before allocating the first enabled
partition with fewer than 10,000 keys. If none has capacity, evaluate the shared
overflow bucket. Private capacity alone never forces denial.

Reset an expired window and resolve its expired pending attempts as neutral. If
failures plus effective pending reservations reach ten, update last_seen and
clock, return positive Retry-After and insert no attempt. Otherwise start the
window if empty and insert one reservation ending at that window's expiry.
Pending count is derived from effective attempt rows, not a mutable counter.
Constrain committed failures to 0..10. The window is absent exactly when both
committed failures and effective pending debt are zero.

That is the evaluated state after the selected bucket resolves expiry. At rest,
the owner deferred assertion instead binds stored pending rows to the exact
bucket/window and requires committed failures plus stored pending count at most
ten. A stored pending row may have passed effective_until until its bucket is
selected. A zero-failure bucket keeps its window while such a row remains.
Advancing the policy clock for another key never forces an unrelated bucket
sweep. See [rate storage](rate-storage.md) for exact nullable state.

Finish revalidates the exact attempt, bucket and original window:

- Neutral resolves only its reservation.
- Failure converts effective pending debt into one committed failure. Expired
  attempts resolve neutral and never charge a later window.
- Private success clears the private failures and pending debt. It records the
  caller's success, resolves pending siblings as neutral/private_success_clear,
  and removes the bucket with its active_keys decrement atomically.
- Overflow success resolves only its own reservation and preserves other
  reservations and shared failures.

An exact caller-finish replay returns its recorded outcome. A different outcome
conflicts while that receipt exists. A system-cleared or expired attempt makes
every later caller finish a harmless no-op, regardless of requested outcome. It
cannot restore or charge debt. This preserves current late-finish behavior.

## Receipt retention

Retain terminal attempt receipts for 24 hours after terminal_at. This is an
explicit replay horizon, independent of the rate bucket's 24-hour idle backstop.
Maintenance deletes at most 256 eligible terminal rows per transaction, ordered
by terminal_at/UUID, using the same effective policy time. Pending rows are not
receipt-GC candidates; window expiry resolves their admission debt first.

After receipt deletion, FinishAttempt for the absent UUID is a stale no-op. It
grants no work and changes no debt. Callers never reuse attempt UUIDs for new
requests. Conflict detection is retained for the 24-hour receipt horizon; this
does not promise an unbounded outcome archive or an absolute row-count cap. No
cleanup function or partial index alone proves production storage bounds.

The maintenance-only [rate cleanup](rate-storage.md) also normalizes the one
selected overflow bucket through accepted refill/expiry rules. It resolves only
expired pending attempts, preserves effective debt and never deletes overflow.
Its atomic policy_idle result requires no ordinary bucket, neutral overflow and
no pending P22 row; retained terminal receipts do not affect it.

## Required local proof

Prove malformed rows and cross-role SQL fail; ten concurrent reservations admit
exactly ten; first pending starts the window; denial adds no attempt; overflow
preserves shared debt; success/neutral/failure/expiry races preserve active_keys
and terminal semantics; backward time clamps; and ambiguity starts no work.

Use independent pools for allocation, private success, neutral-last-debt
deletion, expiry and lifecycle partition toggles. Prove no hidden retry, no lock
cycle and no counter drift. Also prove client deletion preserves receipts,
24-hour boundary behavior, page sizes 1/256 and rejection of 0/257, exact
replay, conflicting caller outcomes, system-terminal late finishes and
absent-after-GC no-op. R1 adds no OAuth caller or admission-package
implementation. Pin a fixed client UUID and expected digest in SQL and Go to
prove their bucket key encodings agree.
