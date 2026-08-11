# Task 7: Idempotency record store (replay / reject / rollback primitive)

Closes the store primitive in AC-SAVE-003 and supplies the substrate for
AC-SAVE-002, whose HTTP behavior P2B closes. Implements D11. Also the forward
contract from `../phase-1-deferred.md`: the client's `csrf_rejected` retry
**reuses the same `Idempotency-Key`** — this primitive is what makes that retry
safe (same key + same body ⇒ replay, never a double mutation); record that
sentence in the package doc so P2B/P4 inherit it as written contract, not
accident.

**Files:** create `apps/server/internal/resume/idempotency.go`,
`idempotency_test.go`.

**Interfaces.** Produces:

```go
package resume

const IdempotencyTTL = 24 * time.Hour // D11; flagged for review

type StoredResponse struct {
    Status int
    Body   json.RawMessage
}

var ErrIdempotencyKeyReuse = errors.New(
    "resume: idempotency key reused with a different request body")

type IdempotencyStore struct {
    pool *store.Pool
    q    *store.Queries
    now  func() time.Time
}

// Execute serializes all of a user's mutation transactions on the existing
// user-row lock before the live-key lookup. For concurrent committed same-key
// calls, only the leader invokes mutate; a follower replays or rejects the
// committed record. mutate may run again only after an earlier attempt rolled
// back or crashed, so it MUST perform every database write through the supplied
// qtx and MUST NOT perform non-transactional side effects. Flow (D11): committed
// preflight { reap this user's expired rows }; tx { lock user; delete-if-expired
// same-key row; lookup live record: matching hash → replay, different hash →
// key reuse, absent → mutate then insert record }. The unique constraint
// remains a fail-closed backstop.
func (s *IdempotencyStore) Execute(ctx context.Context, userID uuid.UUID,
    route string, key uuid.UUID, bodyHash [32]byte,
    mutate func(qtx *store.Queries) (StoredResponse, error),
) (resp StoredResponse, replayed bool, err error)
```

- [x] **Step 1: failing sequential tests.** First call runs `mutate`, stores +
      returns response; second call same key+hash → replayed=true, returns bytes
      identical to the persisted PostgreSQL `jsonb` representation, `mutate` NOT
      invoked (spy counter), no new row. Because PostgreSQL normalizes `jsonb`,
      the first mutation result and replay must be JSON-semantically equivalent;
      byte identity is required between the stored row and every replay, not
      between an arbitrary caller-formatted first body and normalized storage;
      same key different hash → `ErrIdempotencyKeyReuse`, `mutate` not invoked,
      zero writes; `mutate` returning an error → nothing persisted (record row
      absent, mutation rolled back); expired record (injected clock past TTL) +
      same key → treated as fresh: old row replaced, new execution. Seed an
      unrelated expired record and prove the committed preflight removes it even
      when the current attempt ends in key reuse or a mutation error.
- [x] **Step 2: failing real-CAS convergence tests.** Two same-key callers start
      from the same resume revision and each callback invokes Task 6's real
      transaction-scoped SaveDocument core. Before the fix, the loser callback
      ran and surfaced `RevisionMismatch`; after locking, a same-hash follower
      skips its callback and replays the winner, while a different-hash follower
      skips its callback and returns `ErrIdempotencyKeyReuse`. Both cases prove
      exactly one document mutation and one idempotency record commit. Ordinary
      callback-error rollback remains covered by Step 1.
- [x] **Step 2b: failing composition test (B7).** A separate case runs two
      `Execute` calls with different idempotency keys for the same user, each
      `mutate` calling `createTx`, back to back — both resumes exist, cap
      accounting is correct (this is `resume.Store`'s real cap check running
      inside `IdempotencyStore`'s tx, not a bypass), and a call that would be a
      4th resume for that user still surfaces `ErrCapExceeded` from inside
      `mutate`, which `Execute` propagates without inserting an idempotency
      record (nothing to replay for a rejected mutation).
- [x] **Step 3: implement; green.** Injected `now` for TTL; SHA-256 is the
      caller's job (P2B hashes the raw body — keep the primitive
      transport-agnostic).
- [x] **Step 4: gate.** Same live-DB command + tally as Task 6 Step 5.
- [x] **Step 5: initial implementation commit** —
      `git commit -m "feat(resume): add transactional idempotency record store" -- apps/server/internal/resume`
- [x] **Review follow-up 1:** first independent review found the callback
      exactly-once overclaim and rollback-prone expiry reap.
- [x] **Review follow-up 2:** a fresh author added committed preflight reaping,
      error-path tests, and the transaction-only callback contract.
- [x] **Review follow-up 3:** fresh re-review confirmed those fixes but found
      that a concurrent real CAS callback can fail before the unique-insert
      replay path, so callers do not converge.
- [x] **Review follow-up 4a:** a fresh author serializes contenders before
      lookup/mutate and adds real tx-scoped CAS race coverage.
- [x] **Review follow-up 4b:** a new independent reviewer passes the result; the
      integration owner reruns the focused race test at `-race -count=10`.
- [x] **Review follow-up 4c:** synchronize the owner plan, commit the corrective
      diff (`22169e8`), and integrate the reviewed checkpoint (`5805ddc`).
