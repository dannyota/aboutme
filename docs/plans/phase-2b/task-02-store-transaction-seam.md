# Task 2: Transaction seam on `internal/resume` for HTTP callers

Every P2B mutation must commit its database effects in the **same** transaction
as its idempotency record ([D15](decisions.md)). Existing-document mutations are
full-aggregate read-modify-writes; create and revision-CAS delete use their own
explicit transaction operations. P2A's `IdempotencyStore.Execute` hands its
callback a `*store.Queries` bound to that transaction, but `resume.Store`'s
methods are pool-backed and their tx-scoped cores (`createTx`, `saveDocumentTx`,
`saveTitleTx`) are unexported. This task exports the seam — and nothing else. It
ships no HTTP code.

**Tier:** High risk (CAS, transactions, ownership scoping).

**Files:** create `apps/server/internal/resume/service.go`, `service_test.go`;
modify `apps/server/internal/resume/store.go`, `idempotency.go`,
`idempotency_test.go`, `apps/server/internal/resume/export_test.go`,
`apps/server/sql/queries.sql`, new migration
`apps/server/migrations/00006_bound_retention_and_media_cleanup.sql`, focused
`apps/server/cmd/migrate/retention_media_cleanup_test.go`, and the regenerated
`apps/server/internal/store/**`. **This task holds the exclusive migration +
queries.sql + `internal/store` window for the whole phase. Because migrations
and generated files are integration-owner paths, the integration owner is the
implementation author for this task; independent test and review roles stay
separate.**

## Interfaces

Produces, in package `resume`:

```go
// The tx-scoped mirrors of the pool-backed methods. Each takes the
// transaction-bound *store.Queries that IdempotencyStore.Execute supplies
// to its callback, so a read-modify-write and its idempotency record
// commit or roll back together. Every one is scoped by id AND userID:
// a wrong-owner id is ErrNotFound, identical to a nonexistent one (P2A
// D17 — no existence oracle).
func (s *Store) CreateTx(ctx context.Context, qtx *store.Queries,
    userID uuid.UUID, title string, lng *string, doc schema.Resume) (Resume, error)
func (s *Store) GetTx(ctx context.Context, qtx *store.Queries,
    userID, id uuid.UUID) (Resume, error)
func (s *Store) ListTx(ctx context.Context, qtx *store.Queries,
    userID uuid.UUID) ([]Resume, error)
func (s *Store) SaveDocumentTx(ctx context.Context, qtx *store.Queries,
    userID, id uuid.UUID, doc schema.Resume, expectedRevision int64) (int64, error)
func (s *Store) SaveMetadataAndDocumentTx(ctx context.Context, qtx *store.Queries,
    userID, id uuid.UUID, title string, lng *string, doc schema.Resume,
    expectedRevision int64) (int64, error)
func (s *Store) DeleteTx(ctx context.Context, qtx *store.Queries,
    userID, id uuid.UUID, expectedRevision int64) (Resume, error)

// EnqueueMediaDeletionTx records cleanup work in the caller's transaction.
// The caller validates key against resumeID before this call. The document,
// not this job, remains the media ownership authority.
func (s *Store) EnqueueMediaDeletionTx(ctx context.Context,
    qtx *store.Queries, resumeID uuid.UUID, key string) error

// InspectIdempotency returns an already committed replay or key-reuse error
// without running a mutation. operation is D18's method + registered operation
// + canonical concrete targets. requestHash covers the resolved wire version,
// precondition, declared semantic inputs, and bounded payload bytes. Inspect is an
// optimization for external media preparation, not the concurrency authority;
// Execute must still decide after the candidate object is written.
func (s *IdempotencyStore) Inspect(ctx context.Context, userID uuid.UUID,
    operation string, key uuid.UUID, requestHash [32]byte) (StoredResponse, bool, error)

type CommitOutcome uint8
const (
    CommitNotAttempted CommitOutcome = iota
    CommitDefinitelyRolledBack
    CommitCommitted
    CommitUnknown
)
type ExecuteResult struct {
    Response StoredResponse
    Replayed bool
    Outcome CommitOutcome
}
var ErrIdempotencyCapacity = errors.New("resume: idempotency capacity exceeded")

func (s *IdempotencyStore) Execute(ctx context.Context, userID uuid.UUID,
    operation string, key uuid.UUID, requestHash [32]byte,
    mutate func(*store.Queries) (StoredResponse, error)) (ExecuteResult, error)
```

`Execute` returns an `ExecuteResult` on every path, including errors. Failure
before a transaction begins is `CommitNotAttempted`; callback failure or a
confirmed rollback is `CommitDefinitelyRolledBack`; a successful commit or
replay is `CommitCommitted`; connection loss or another indeterminate commit
result is `CommitUnknown`. Media compensation may delete a candidate after the
first two outcomes. It may also delete this request's candidate when
`Replayed=true`, including the `Replayed=true, CommitCommitted` result: the
stored response proves another execution already owns the referenced object. A
non-replayed `CommitCommitted` result is the winner and every `CommitUnknown`
result is retained. Injected disconnect-at-commit tests prove every
classification and the valid `Replayed`/outcome pairs.

`StoredResponse` contains status, body, and only these deterministic response
headers: `Location`, `ETag`, and `X-Resume-Schema-Version`. Migration 00006 adds
`response_headers jsonb NOT NULL DEFAULT '{}'::jsonb`; existing records get the
empty object. The HTTP writer derives JSON `Content-Type` from a non-204 body
and the outer middleware supplies `Cache-Control: no-store`. `Date` and
`X-Request-ID` are fresh for every request and are never persisted. Tests assert
the exact header set per operation: create has `Location`; every success that
returns a revision has `ETag`; JSON resume responses and bodyless child deletes
have `X-Resume-Schema-Version`; resume delete has neither header; and every
bodyless delete response has no `Content-Type`.

`StoredResponse` uses one internal sentinel for bodyless success: status 204 is
stored with `response_body = 'null'::jsonb`. Both the first response and every
replay translate that sentinel to exactly zero body bytes and omit JSON
`Content-Type`; they never emit the four bytes `null`. Every non-204 success
uses the normalized `jsonb` bytes returned by the insert, so first and replay
are byte-identical. Capacity accounting includes the canonical JSON bytes for
both stored body and stored approved headers. It counts the body sentinel `null`
as four bytes even though the HTTP writer emits none. Tests cover all three
bodyless mutation routes.

The query window adds two CAS statements and replaces the unbounded cleanup:

```sql
-- name: UpdateResumeMetadataAndDocumentCAS :one
UPDATE resumes
SET title = sqlc.arg(title), lng = sqlc.narg(lng),
    personal_details = sqlc.arg(personal_details),
    content = sqlc.arg(content), customization = sqlc.arg(customization),
    schema_version = sqlc.arg(schema_version),
    revision = revision + 1, updated_at = now()
WHERE id = $1 AND user_id = $2 AND revision = $3
RETURNING revision;

-- name: DeleteResumeForUserCAS :one
DELETE FROM resumes
WHERE id = $1 AND user_id = $2 AND revision = $3
RETURNING *;

-- name: DeleteExpiredIdempotencyRecordsForUser :one
WITH doomed AS (
  SELECT id FROM idempotency_records
  WHERE user_id = $1 AND expires_at <= $2
  ORDER BY expires_at, id
  LIMIT $3
  FOR UPDATE SKIP LOCKED
), deleted AS (
  DELETE FROM idempotency_records AS records
  USING doomed WHERE records.id = doomed.id
  RETURNING records.response_body, records.response_headers
)
SELECT count(*)::bigint AS deleted_records,
       COALESCE(sum(octet_length(response_body::text) +
                    octet_length(response_headers::text)), 0)::bigint
         AS deleted_bytes
FROM deleted;

-- CreateIdempotencyRecord changes from :exec to :one and ends with:
RETURNING response_body, response_headers,
          octet_length(response_body::text) +
          octet_length(response_headers::text) AS stored_bytes;
```

The same window adds exact sqlc operations: `NormalizeIdempotencyResponse`,
`GetOrCreateIdempotencyUsageForUpdate`, `TryReserveIdempotencyUsage`,
`ReleaseIdempotencyUsage`, `GetIdempotencyCapacityRetryAfter`, and
`DeleteExpiredIdempotencyRecordForKey`, and
`DeleteExpiredIdempotencyRecordsGlobal`. The per-user and exact-key deletes are
`:one` aggregate queries returning `deleted_records` and `deleted_bytes`, even
when both are zero. The global batch is `:many`, grouped by `user_id`, with the
same two totals. The sweep calls `ReleaseIdempotencyUsage` once per returned
user before committing that cleanup transaction. Insert returns the normalized
stored body, approved headers, and exact `stored_bytes`. No caller recomputes
bytes or repairs counters in Go.

Migration 00006 adds `(user_id, expires_at, id)` for deterministic cleanup and
`response_headers jsonb NOT NULL DEFAULT '{}'::jsonb` with an object-type check
to `idempotency_records`. It also adds
`idempotency_usage(user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE, retained_records bigint NOT NULL, stored_bytes bigint NOT NULL)`
with non-negative checks. At one transaction timestamp it first deletes every
already-expired legacy record, then backfills one row per user from all retained
records before enabling the checks. Backfill, insert, cleanup return values, and
quota accounting all use the one byte expression
`octet_length(response_body::text) + octet_length(response_headers::text)`; JSON
`null` therefore counts as four and empty approved headers count as two. A
prior-head migration test seeds live and expired records with object/array/null
bodies and proves purge, counts, bytes, and the next cleanup cannot go negative.
The usage row counts every physically retained row, including expired backlog,
and is updated in the same transaction as record insert/delete under the
existing user lock. A new insert that would exceed 50,000 retained records or 1
GiB returns the typed capacity error without committing the mutation. The HTTP
kernel maps it to `429 rate_limited`; `Retry-After` is one second while an
expired retained row remains, otherwise the rounded-up interval to the earliest
expiry. An existing unexpired key still replays. It is a new append-only
migration; released migrations remain byte-identical.

The same migration adds `media_deletion_jobs` as a work ledger, not an ownership
table. Each outstanding row contains a UUID job ID, the resume ID, one unique
canonical object key, `enqueued_at`, `next_attempt_at`, and a non-negative
attempt count. Database checks require the key's embedded canonical resume ID to
equal `resume_id`. An index on `(next_attempt_at, id)` supports bounded
oldest-due claims. Jobs have no foreign key to `resumes`, because resume and
account deletion must not cascade away pending physical deletion. P8-priv owns
claim leases, retry state, terminal audit records, and removal of completed
jobs.

`Execute` keeps P2A's cleanup-on-error property with two explicit transactions.
First it begins a cleanup transaction, locks the user, deletes at most 200
expired rows, releases their exact retained counters, and commits. Failure stops
before the mutation transaction. It then begins the mutation transaction, locks
the same user, deletes this exact key if expired and releases that row's
counters in the same transaction, checks replay/reuse/capacity, invokes the
callback, reserves usage, inserts the normalized result, and commits. A callback
or commit rollback does not roll back the already committed bounded cleanup.
Both transactions use the same user-first lock order. Tests fail the mutation
after cleanup and prove cleanup persists, then fail cleanup and prove the
callback never runs.

`user_id` never appears in a `SET` clause (P2A owner correction 3(2): the cap
trigger fires on `UPDATE OF user_id` and would falsely raise
`resumes_user_cap_exceeded`). No `SET TRANSACTION ISOLATION LEVEL` appears
anywhere: the trigger's lock-then-count is race-proof only at READ COMMITTED
(P2A owner correction 3(1)).

## Steps

- [ ] **Step 1: failing seam tests.** Against a live database, drive each new
      method through a hand-rolled `pgx` transaction and assert: (a) it behaves
      identically to the pool-backed method it mirrors, including `ErrNotFound`,
      `ErrCapExceeded`, `ErrTitleTooLong`, and `*RevisionMismatchError` with the
      winning document; (b) rolling the transaction back leaves **zero**
      observable effect — row count, row bytes, `revision`, and `updated_at` all
      unchanged; (c) a wrong-owner id and a nonexistent id return byte-identical
      errors on every method.
- [ ] **Step 2: failing metadata and delete CAS tests.**
      `SaveMetadataAndDocumentTx` writes `title`, `lng`, and the caller-supplied
      already-projected, sanitized, validated current-version aggregate under
      one revision CAS; the store persists that document unchanged and does not
      add a second sanitizer. A stale revision changes nothing. `DeleteTx` takes
      the expected revision and returns the deleted row so its caller can
      validate and enqueue media cleanup in the same transaction. On a CAS miss
      it re-reads the scoped winner so the HTTP layer can produce `412`; wrong
      owner and missing remain indistinguishable. Cover `lng` clearing,
      canonical length, and title 160/161 before any statement runs.
- [ ] **Step 3: failing composition test.** Two `IdempotencyStore.Execute` calls
      with different keys, whose callbacks use `CreateTx` and `SaveDocumentTx`,
      both commit; a callback that returns an error after calling
      `SaveDocumentTx` leaves neither the mutation nor an idempotency record.
      This is the exact composition every route in wave 3 relies on.
- [ ] **Step 3a: failing media inspection tests.** `Inspect` returns a live
      same-hash response, returns `ErrIdempotencyKeyReuse` for a changed hash,
      treats an absent or expired record as fresh, and never claims to reserve a
      key. A deterministic race proves two fresh inspections can both miss and
      that the following `Execute` calls still select one database winner.
- [ ] **Step 3aa: failing commit-outcome tests.** Inject failure before the
      transaction begins, after mutation but before commit, a definite rollback
      at commit, connection loss during commit, success, and replay. Assert the
      exact
      `Execute(ctx, userID, operation, key, requestHash, mutate)     (ExecuteResult, error)`
      result on every path. Candidate deletion is safe exactly when
      `Replayed=true`, `Outcome=CommitNotAttempted`, or
      `Outcome=CommitDefinitelyRolledBack`. It is unsafe for a non-replayed
      `CommitCommitted` winner and every `CommitUnknown`. The matrix includes
      concurrent replay as `Replayed=true, CommitCommitted`, plus invalid
      `Replayed`/outcome pairs that fail closed.
- [ ] **Step 3b: failing bounded-cleanup tests.** Seed 201 expired rows for one
      user plus live and neighbour-user rows. One mutation removes exactly the
      oldest 200 in `(expires_at,id)` order; a later mutation removes the
      backlog; live and neighbour rows remain. Query-plan evidence uses the new
      composite index. Concurrent cleanup stays bounded and deadlock-free.
- [ ] **Step 3c: failing retained-capacity tests.** Under the user lock, admit a
      new key when its insert stays within 50,000 retained records and 1 GiB,
      and reject an insert that exceeds either bound without committing the
      mutation. Still replay an existing unexpired key at the bound. Leave more
      than 200 expired rows behind and prove they remain counted until deletion,
      so concurrent contenders cannot overshoot either usage counter. A rollback
      changes neither counter. `Retry-After` is one while expired backlog
      remains and otherwise rounds up the earliest expiry.
- [ ] **Step 3d: failing replay-identity test.** Make record insertion return
      PostgreSQL's stored body and approved-header `jsonb` values and use those
      normalized bytes for the first response. Assert first and replay status,
      approved headers, and body are byte-identical even when input object keys
      were not in PostgreSQL's canonical order. Assert `Date` and `X-Request-ID`
      are valid, fresh, and distinct.
- [ ] **Step 3e: failing media-deletion enqueue tests.** In one transaction,
      remove a photo reference or whole resume and enqueue its exact validated
      key. Commit persists both changes; rollback persists neither. Duplicate
      enqueue of the immutable key is idempotent. A malformed key, a key for a
      different resume, a missing owner, or a stale revision changes neither the
      aggregate nor the queue. Resume/account deletion does not cascade the
      pending job. The due-order index supports the P8-priv bounded claim.
- [ ] **Step 4: add migration and queries; regenerate.** Add the composite
      cleanup index, usage table/backfill, the two CAS queries, bounded per-user
      and global cleanup, the approved-header column, response
      normalization/insert-returning, conditional usage reservation/decrement,
      capacity-retry queries, and the deletion-job table/enqueue operation. The
      integration owner authors the focused prior-head proof at
      `apps/server/cmd/migrate/retention_media_cleanup_test.go`; no route or
      blind- suite task may edit that migration test. Run `make sqlc-gen`,
      include generated files, and prove released migrations are unchanged and
      the new migration applies from the prior head.
- [ ] **Step 5: implement; green.** The pool-backed methods become thin wrappers
      that open a transaction and delegate, so there is exactly one
      implementation of each behavior and P2A's existing tests still pass
      unchanged — run them.
- [ ] **Step 6: gate.** Run `make test-db-up`, `make server-build server-vet`,
      and
      `(cd apps/server && REQUIRE_TEST_DB=1 TEST_DATABASE_URL="${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable}" go test ./internal/resume/... -race -count=1)`.
      Repeat the same package command with `-race -count=20`, then run
      `make sqlc-check server-migration-test`.
- [ ] **Step 7: handoff.** Record the exact owned diff, failing-test evidence,
      checks, generated-file list, and migration/query delta. Do not stage or
      commit until the independent review and phase integration decision.
- [ ] **Step 8: independent defect review** by a worker that did not author the
      change; blocking findings fixed by a different worker and re-reviewed.

## Acceptance mapping

| Row          | What this task contributes                                                        |
| ------------ | --------------------------------------------------------------------------------- |
| AC-SAVE-001  | The CAS path that produces `*RevisionMismatchError` with the winning document     |
| AC-SAVE-002  | The transaction seam that lets a mutation and its idempotency record share a tx   |
| AC-DOC-001   | HTTP-level evidence path for the cap: `CreateTx` keeps the store lock and trigger |
| AC-MEDIA-003 | Reference revocation and exact-key cleanup work commit together                   |
| AC-MEDIA-006 | The safe preflight and durable cleanup seam around external object writes         |
