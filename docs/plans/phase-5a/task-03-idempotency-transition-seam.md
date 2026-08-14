# Task 03 — Idempotency transition re-probe seam

**Owner:** Idempotency author in W2.

**Acceptance:** AC-PUB-002/004 primitive prerequisite for Task 02's rows 8, 12,
15, and 22. Task 02 alone owns the named catalog.

**Authorities:** `revocation.md`, ADR 0016, ADR 0019, ADR 0022, and Phase 2B
decisions D15/D18.

**Files:** The Task 03 row in `file-structure.md`. Do not edit SQL/sqlc,
resumeapi, fences, or public handlers.

**Interfaces:** Produces exact `Recheck` while preserving the existing
`StoredResponse`, `CommitOutcome`, `ExecuteResult`, and `Execute` API repeated
below. `Recheck` uses a short user-lock transaction to inspect the exact record
without writes. `Execute` remains the final serialized decision authority and
the sole owner of expired-record deletion and retained-usage release.

## Step 1 — RED the second probe

- [ ] Add compile and behavior tests for this exact producer interface, which
      Task 07 repeats verbatim:

  ```go
  type StoredResponse struct {
    Status int
    Body json.RawMessage
    Headers map[string]string
  }
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
  type RecheckDecision uint8

  const (
    RecheckFresh RecheckDecision = iota
    RecheckReplay
    RecheckReuse
  )

  type RecheckResult struct {
    Decision RecheckDecision
    Response StoredResponse
  }

  func (s *IdempotencyStore) Recheck(
    ctx context.Context,
    userID uuid.UUID,
    operation string,
    key uuid.UUID,
    requestHash [32]byte,
  ) (RecheckResult, error)
  func (s *IdempotencyStore) Execute(
    ctx context.Context,
    userID uuid.UUID,
    operation string,
    key uuid.UUID,
    requestHash [32]byte,
    mutate func(qtx *store.Queries) (StoredResponse, error),
  ) (ExecuteResult, error)
  ```

- [ ] Test its short user-lock transaction, exact replay/reuse/fresh decision,
      and current-retention expiry: an expired record returns `RecheckFresh`.
      Cover no key reservation, mutation, expired-record deletion, or retained
      usage update; cancellation; lock, lookup, and commit failures; malformed
      stored response; and no writes.
- [ ] Test final `Execute` repeats the decision after fence close. A record
      appearing between `Recheck` and `Execute` replays/conflicts without
      calling mutation. Pin definite and unknown commit outcomes; request
      cancellation after commit begins is never proof of rollback.
- [ ] Run RED:

  ```sh
  (cd apps/server && go test ./internal/resume/... -race -count=1 -run 'Test(IdempotencyRecheck|SameKeyContender|ExecuteFinalRecheck|CommitOutcome)')
  ```

  Expected: `Recheck` and its closed enum do not exist.

## Step 2 — GREEN without moving transaction authority

- [ ] Extract one exact-record decision helper shared by `Inspect`, `Recheck`,
      and `Execute`; it treats an absent or expired record as fresh and
      preserves existing operation identity, fingerprint, stored response, and
      error semantics. `Execute` separately retains its expired-record deletion
      and usage-release behavior.
- [ ] Implement `Recheck` as a short transaction that locks the user, inspects
      the exact record, returns the decision, and commits without DML. It never
      reserves capacity, deletes expired records, updates retained usage, or
      invokes the callback.
- [ ] Keep `Execute` the only expired-record deletion, retained-usage,
      transaction/commit-outcome, and final serialized decision authority.
      Expose only test blockers needed to deterministically place contenders.
- [ ] Run GREEN:

  ```sh
  (cd apps/server && go test ./internal/resume/... -race -count=1 -run 'Test(IdempotencyRecheck|SameKeyContender|ExecuteFinalRecheck|CommitOutcome)')
  make server-build server-vet server-test
  ```

## Executable RED → GREEN checkpoints

- [ ] RED: implement Task 02 catalog row 22. Its blocker lets one request
      reserve the key, commits its stored response, then asserts the contender's
      pre-CAS `Recheck` returns `RecheckReplay` with those exact bytes. Run
      `go test ./internal/resume -race -count=1 -run 'Test.*Contender.*Recheck'`
      from `apps/server` and observe `Recheck` is undefined.
- [ ] Minimal GREEN: implement `Recheck` as one short, read-only user-lock
      transaction with an exact retained-record lookup. It compares the stored
      request hash and returns only `RecheckFresh`, `RecheckReplay`, or
      `RecheckReuse`; an expired record returns `RecheckFresh` without deletion
      or retained-usage changes. Do not shift `Execute`'s final serialized
      decision or expired-record cleanup authority. Rerun the RED command, then
      `(cd apps/server && go test ./internal/resume -race -count=20 -run 'Test(Idempotency|SameKeyContender)')`.

## Completion

- [ ] Return the exact handoff report, enum numeric values, and transaction
      boundaries.
- [ ] Suggest commit: `refactor(idempotency): add serialized mutation recheck`.
