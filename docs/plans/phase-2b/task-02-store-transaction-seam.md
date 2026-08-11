# Task 2: Transaction seam on `internal/resume` for HTTP callers

Every P2B mutation is a read-modify-write that must run inside the **same**
transaction as its idempotency record ([D15](decisions.md)). P2A's
`IdempotencyStore.Execute` hands its callback a `*store.Queries` bound to that
transaction, but `resume.Store`'s methods are pool-backed and their tx-scoped
cores (`createTx`, `saveDocumentTx`, `saveTitleTx`) are unexported. This task
exports the seam — and nothing else. It ships no HTTP code.

**Tier:** High risk (CAS, transactions, ownership scoping).

**Files:** create `apps/server/internal/resume/service.go`, `service_test.go`;
modify `apps/server/internal/resume/store.go`,
`apps/server/internal/resume/export_test.go`, `apps/server/sql/queries.sql`
(append one statement), and the regenerated `apps/server/internal/store/**`.
**This task holds the exclusive queries.sql + `internal/store` window for the
whole phase.**

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
func (s *Store) SaveMetadataTx(ctx context.Context, qtx *store.Queries,
    userID, id uuid.UUID, title string, lng *string, expectedRevision int64) (int64, error)
func (s *Store) DeleteTx(ctx context.Context, qtx *store.Queries,
    userID, id uuid.UUID) error
```

And one appended statement (no migration — both columns already exist):

```sql
-- name: UpdateResumeMetadataCAS :one
UPDATE resumes
SET title = $4, lng = $5, revision = revision + 1, updated_at = now()
WHERE id = $1 AND user_id = $2 AND revision = $3
RETURNING revision;
```

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
- [ ] **Step 2: failing metadata tests.** `SaveMetadataTx` writes `title` and
      `lng` together under one CAS; a stale revision leaves both columns
      unchanged; a `nil` `lng` clears the column and a present one sets it;
      `lng` longer than the DB check fails without a partial write; a title of
      exactly 160 code points is accepted and 161 is rejected **before** any
      statement runs.
- [ ] **Step 3: failing composition test.** Two `IdempotencyStore.Execute` calls
      with different keys, whose callbacks use `CreateTx` and `SaveDocumentTx`,
      both commit; a callback that returns an error after calling
      `SaveDocumentTx` leaves neither the mutation nor an idempotency record.
      This is the exact composition every route in wave 3 relies on.
- [ ] **Step 4: append the query and regenerate.** Add
      `UpdateResumeMetadataCAS`, run `make sqlc-gen`, commit the regenerated
      `internal/store`, and prove `make sqlc-check` is green with no drift and
      `apps/server/migrations/` untouched.
- [ ] **Step 5: implement; green.** The pool-backed methods become thin wrappers
      that open a transaction and delegate, so there is exactly one
      implementation of each behavior and P2A's existing tests still pass
      unchanged — run them.
- [ ] **Step 6: gate.** `make server-build server-vet` and
      `REQUIRE_TEST_DB=1 … go test ./internal/resume/... -race -count=1`;
      concurrency cases additionally at `-count=20`. Then `make sqlc-check`.
- [ ] **Step 7: commit** —
      `git commit -m "feat(resume): expose transaction-scoped store operations" -- apps/server/internal/resume apps/server/sql/queries.sql apps/server/internal/store`
- [ ] **Step 8: independent defect review** by a worker that did not author the
      change; blocking findings fixed by a different worker and re-reviewed.

## Acceptance mapping

| Row         | What this task contributes                                                        |
| ----------- | --------------------------------------------------------------------------------- |
| AC-SAVE-001 | The CAS path that produces `*RevisionMismatchError` with the winning document     |
| AC-SAVE-002 | The transaction seam that lets a mutation and its idempotency record share a tx   |
| AC-DOC-001  | HTTP-level evidence path for the cap: `CreateTx` keeps the store lock and trigger |
