# Fixed rate operations and results

Status: Accepted detail of [rate storage](rate-storage.md) under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). The
[scaling index](README.md) records whether it is built.

This contract fixes results, role checks, errors and the database-time test
seam. It preserves schema 18, algorithms, limits, function inputs, grants and
lock order. [OAuth attempts](admission-attempts.md) retains P22 identity and
receipt semantics; [rate identities](rate-identities.md) retains R5 encoding.
The internal reserve result adds only replayed to distinguish a retained
historical admission from fresh work. It adds no stored or public API field.
[Claim transport](claim-operations.md) supplies the verified one-call sqlc
value/presence pattern; it does not prove these rate operations.

## Inputs and direct roles

Function names and argument order remain:

1. runtime_admit_token_rate(policy_id text,key_digest bytea)
2. runtime_password_failure_state(key_digest bytea)
3. runtime_record_password_failure(key_digest bytea)
4. runtime_clear_password_failure(key_digest bytea)
5. runtime_admit_slug_change(key_digest bytea)
6. runtime_reserve_oauth_failed_grant(attempt_id uuid,client_id uuid)
7. runtime_finish_admission_attempt(attempt_id uuid,outcome text)
8. runtime_cleanup_admission_attempt_receipts(page_size integer)
9. runtime_cleanup_rate_buckets(policy_id text,page_size integer)

Functions 1-7 require exact session_user='aboutme_app'. Functions 8-9 require
exact session_user='aboutme_maintenance'. Check session_user directly at
function entry, before input/catalog lookup, and never accept current_user, SET
ROLE, role membership, owner, lifecycle, proof, restore, migrator, or PUBLIC as
a substitute. Wrong role is 42501 with a fixed message. Existing EXECUTE grants
match this matrix; the check is defense in depth.

Do not declare these definers STRICT. Explicitly reject SQL NULL inputs so a
NULL cannot bypass the direct-role check or return a null composite. key_digest
must be exactly 32 bytes. P22 UUIDs must be nonnull and not the all-zero UUID.
page_size is 1..256. Finish outcome is exactly failure, neutral, or success.
Each function returns exactly one nonnull composite row on success.

## PostgreSQL result composites

All seven result types and nine definers belong to aboutme_runtime_owner. Revoke
PUBLIC type usage and function execution. Definers use SECURITY DEFINER,
search_path=pg_catalog and fixed qualified SQL; helpers have no runtime grants.

The field order is fixed. Every value marked required is nonnull. A nullable
field uses SQL NULL, never a sentinel zero, empty string, or false.

runtime_rate_decision_result, returned by functions 1 and 5:

1. allowed boolean required
2. retry_after_seconds integer required, >=0
3. bucket_kind text required, private or overflow
4. partition smallint nullable, 1 or 2 exactly for private; null for overflow
5. effective_at timestamptz required

runtime_password_failure_result, returned by functions 2 and 3:

1. exhausted boolean required
2. retry_after_seconds integer required, >=0
3. bucket_kind text required, private or overflow
4. partition smallint nullable
5. effective_at timestamptz required
6. allocated boolean required
7. activity_refreshed boolean required

runtime_password_clear_result, returned by function 4:

1. cleared boolean required
2. bucket_kind text required, always private
3. partition smallint nullable

runtime_failed_grant_reserve_result, returned by function 6:

1. allowed boolean required
2. retry_after_seconds integer required, >=0
3. bucket_kind text nullable, private or overflow when present
4. partition smallint nullable
5. replayed boolean required

runtime_admission_attempt_finish_result, returned by function 7:

1. resolution text required: caller_finished, caller_replay, system_noop, or
   absent_noop
2. stored_outcome text nullable: failure, neutral, or success

runtime_admission_receipt_cleanup_result, returned by function 8:

1. deleted_count integer required, >=0 and <=page_size

runtime_rate_cleanup_result, returned by function 9:

1. deleted_count integer required, >=0 and <=page_size
2. effective_at timestamptz required
3. policy_idle boolean required

## Decision and password matrices

For functions 1 and 5:

- Allowed: allowed=true, retry=0. bucket_kind identifies the charged private or
  overflow bucket; private has partition 1/2 and overflow has null partition.
- Denied: allowed=false, retry is a positive ceiling. The selected bucket shape
  is still returned as above. P12 denial returns its accepted retry value 1.
- There is no absent or replay result. A committed denial is authority to deny,
  never to retry internally or refund earlier ordered debt.

For function 2, allocated is always false:

- Existing private: kind private, stored partition 1/2, activity_refreshed=true.
  exhausted=false has retry=0; exhausted=true has a positive ceiling retry.
- Absent with routable enabled capacity: kind private, partition=null,
  exhausted=false, retry=0, activity_refreshed=false. No row is allocated.
- Saturated or no-enabled-partition route: kind overflow, partition=null,
  activity_refreshed=true. exhausted and retry follow the same zero/positive
  relation. State changes only overflow last_seen.

For function 3, one selected bucket is always recorded:

- Newly allocated private: kind private, partition 1/2, allocated=true,
  activity_refreshed=true.
- Existing private: same shape with allocated=false and activity_refreshed=true.
- Overflow: partition=null, allocated=false, activity_refreshed=true.
- The post-record state sets exhausted=false/retry=0 below the limit and
  exhausted=true/positive ceiling retry at the limit. It never returns a
  positive retry with exhausted=false or zero retry with exhausted=true.

For function 4:

- Existing private: cleared=true, kind private, partition is the former 1/2. The
  bucket deletion and active_keys decrement committed.
- Absent private: cleared=false, kind private, partition=null. No allocation or
  overflow read/clear occurs.
- Overflow is never a return kind. Clear has no replay or retry result.

## P22 reserve matrix

A fresh allowed reserve returns allowed=true, retry=0, exact kind, private
partition 1/2 or overflow/null, and replayed=false. Its pending receipt
committed.

A fresh denied reserve returns allowed=false, positive ceiling retry,
bucket_kind=null, partition=null, and replayed=false. It inserts no attempt and
adds no debt. The deliberately absent bucket identity preserves
admission-attempts.md's “only when allowed” result boundary.

An exact retained-attempt replay verifies attempt and client identity and
returns the immutable historical admitted bucket_kind and partition with
allowed=true, retry=0, and replayed=true. Pending and terminal retained receipts
return the same shape; the result discloses neither receipt state nor a current
effective time. It does not reevaluate or reroute through a current bucket that
may no longer exist. R5 rejects every replay for new work. The replay exists
only to resolve durable identity; it cannot grant work, consume a second slot,
reset a window, or become a caller retry/readback after an ambiguous reserve.

Conflicting attempt UUID reuse with another client is AM002. A definitive denial
has no receipt and therefore no same-UUID replay guarantee; the caller
obligation to generate one fresh UUID per request remains. Nil attempt or client
is 22023. After terminal receipt GC, the same UUID is absent; callers still
never reuse it.

## P22 finish matrix

- First effective caller finish: resolution=caller_finished and stored_outcome
  equals the requested outcome.
- Exact replay of a caller-finished receipt with the same outcome:
  resolution=caller_replay and stored_outcome is that outcome.
- Caller-finished receipt plus a different requested outcome: AM002, no row.
- A system-terminal receipt from expiry or private-success sibling clearing:
  resolution=system_noop and stored_outcome is its stored neutral outcome for
  every requested outcome. It cannot restore or charge debt.
- Missing attempt, including after receipt GC: resolution=absent_noop and
  stored_outcome=null for every requested outcome. It changes no bucket.

Only absent_noop has null stored_outcome. Finish never returns allowed, retry,
bucket, partition, or caller identity. Nil attempt/invalid outcome is 22023.

## Cleanup matrices

Function 8 returns one count from 0 through page_size. Zero is a successful
no-op. It deletes only terminal receipts older than the accepted 24-hour
retention and never returns or deletes pending identity/debt.

Function 9 returns deleted_count from 0 through page_size, its sampled effective
time, and the locked post-normalization policy_idle predicate. policy_idle=true
means no ordinary bucket, normalized neutral overflow, and no P22 pending row
for that policy. Terminal receipts do not affect it. false means at least one
such condition remains; it is not an error or permission to erase debt. Neither
cleanup function retries, admits work, or returns private digest/attempt data.

## Errors and information boundary

- 42501: wrong direct session_user.
- 22023: null/malformed digest, nil UUID, invalid outcome, invalid page size, or
  input shape outside the fixed operation.
- 55000: a caller-supplied policy ID is unknown or selects an algorithm outside
  that fixed function, or a valid request is unavailable because admission/write
  entry or partition generation is stale or the operation cannot safely apply.
  This is input/inapplicability against an otherwise valid installed catalog.
- AM002: retained P22 attempt identity or caller outcome conflicts.
- AM001: an accepted policy's stored catalog row is missing or differs from its
  fixed seed, or stored marker, clock, counter, bucket, attempt, or deferred
  assertion state is corrupt. Catalog absence/drift never becomes caller 55000.

Definers validate predictable cases before a constraint error. Messages, detail,
hint, schema, table, column, and Go wrapping contain fixed operation terms only.
They never include policy key material, digest, client/attempt UUID, email, IP,
token, user, bucket contents, timing history, or supplied value. Driver,
cancellation, serialization, finish, and commit errors pass as unavailable
errors; no function or transport retries them.

## One-way Go transport

internal/store owns generated scalar query rows and nine fixed methods with the
same scalar arguments as SQL. Each mutating method runs through WriteTxRunner
and calls one generated query. It neither accepts nor exposes Policy, RateKey,
attempt-domain objects, a clock, pgx rows, Queries, or a raw connection.

Each generated query uses one
`WITH result AS MATERIALIZED (SELECT public.runtime_...(args) AS r)` and
projects every field once. Required fields are plain scalars. Every nullable
field projects both its scalar and an `IS NOT NULL` presence boolean. The
decoder rejects a mismatch between presence and value or any matrix violation,
copies values into an owned internal transport value, and does so before the
WriteTxRunner callback returns.

Only successful runtime_finish_write and an observed successful commit expose
the decoded transport value. Any ordinary returned callback, decode, finish,
cancellation, rollback, or ambiguous commit error returns the zero transport
value plus error. No allowed, denied, replay, no-op, cleanup count, or
policy_idle value is returned alongside an error. A callback panic follows
WriteTxRunner's accepted path: bounded cleanup joins, the transaction is rolled
back/connection handled, and the original panic is rethrown. The transport
neither converts that panic to an error nor exposes its decoded value. This is
zero authority: R5 cannot start work, deny from a decoded row, clear debt, or
retry merely because the SELECT produced a value.

R5 imports internal/store. It alone owns Policy, RateKey, digest encoding, P22
attempt operation state, domain decisions, and public response mapping. It maps
a committed internal scalar value only after validating the expected
policy/shape. internal/store never imports R5 and never canonicalizes
identities. Maintenance cleanup uses a separate store adapter and exposes no
admission method.

## Database-time test seam

All production operations call one owner-owned, no-argument volatile helper that
normally returns clock_timestamp(). It has no EXECUTE grant to app, maintenance,
or any other login; SECURITY DEFINER rate functions can invoke it by owner
rights. No production function accepts time, interval, epoch, clock mode, or
test flag.

One-session migration tests may replace the helper with a fixed literal inside
one test-only owner transaction, exercise the function in that session, and roll
back. Independent-pool tests instead use a disposable isolated test database.
Before any test connection or pool starts work, the owner fixture replaces and
commits the no-argument helper body with the fixed literal. After all pool work
is canceled and joined and every connection is closed, the owner fixture
restores the exact clock_timestamp body or drops the disposable database. Tests
serialize the entire fixture lifetime; they never replace a helper in a database
used by another test or process. A test first proves app and maintenance cannot
execute, replace, or influence the helper. This is a sampling seam, not a
runtime setter, callable timestamp parameter, or general state-update API.
Production migration verification fixes the helper definition to the exact
clock_timestamp body.

## Failing-first obligations

- One-call materialization for all nine functions; exact field order/types;
  nullable presence matrices; volatile helper invoked once; owned-value copy.
- Every allowed/denied/password/clear/reserve/finish/cleanup row above,
  including retry zero/positive rules, private/overflow partitions, and absent
  shapes.
- P22 pending and terminal exact reserve replay cannot start work; conflict,
  system-noop, absent-after-GC, nil client/attempt, and outcome matrix.
- Direct session_user matrix for app, maintenance, lifecycle, proof, restore,
  migrator, owner where forbidden, SET ROLE, inherited role, and PUBLIC.
- Every fixed SQLSTATE and secret-free diagnostic. Corrupt presence/value rows
  fail closed. No hidden retry after serialization, cancellation, or ambiguity.
- WriteTxRunner callback success followed by finish failure and commit ambiguity
  returns zero authority; cancellation rolls back; panic performs bounded joined
  cleanup and rethrows the original panic; no path leaks a row handle.
- Owner-only no-argument clock replacement is deterministic: one-session
  rollback and committed disposable-database multi-pool fixtures both
  restore/drop after all work joins. Production roles cannot call or influence
  it; forward/backward boundary cases still use the accepted durable high-water
  clock.

These operation, role, concurrency and clock checks remain unrun. The existing
isolated sqlc/pgx probe verifies transport mechanics only.
