# Phase 2A exit criteria

The phase closes only when the candidate commit satisfies every item. A later
product-code change invalidates evidence for each affected path.

Task 12 documentation and handoffs are landed. Candidate
`2ce66d36b7aab2f9814c4e894b937c5e80bcb520` passed the pre-UAT cap-trigger
correction checks, both owner gates, the fresh phase review, and every catalog
revision 5 row with zero retries. The later documentation-only closure commit
persists the report and status changes without claiming that its own commit was
product-gated.

## Product and contract

- [x] Corrected pre-UAT migration `00005` permits a no-op owner assignment at
      the cap while inserts and real owner moves remain capped. Empty-to-head,
      previous-to-head, concurrent-lock, and partial-failure migration tests
      pass. The first UAT baseline will freeze the migration history.
- [x] Direct SQL and store tests prove the three-resume cap, ownership
      indistinguishability, title bounds, and every relational constraint.
- [x] Every document bound has passing limit and failing limit+1 coverage. The
      schema-walk completeness guard finds no untested bound.
- [x] Cleared optional contact values round-trip unchanged through live writes.
- [x] Same-revision writes have one winner. Losers receive the winning revision
      and document without an existence oracle.
- [x] The P2A idempotency primitive replays the stored result, rejects a changed
      caller-supplied body hash, serializes contenders, and rolls back mutation
      and record together. Its narrower current interface is recorded for P2B,
      not presented as the complete Draft v4 HTTP contract.
- [x] Reads project old rows without writing. CAS backfill does not bump the
      user revision and loses safely to document or title changes.
- [x] Released schemas, retained types, registries, accepted/emitted versions,
      and adjacent-converter machinery pass append-only and fail-closed tests.
      Synthetic versions prove both converter directions; real HTTP persistence
      remains P2B-owned AC-SAVE-004.
- [x] No production package outside `internal/resume` calls a generated resume
      write method. The phase review records this as a review rule, not a custom
      static-analysis guarantee.

## Evidence and checks

- [x] `make ci` passes once, without concurrent heavy workers, at the candidate
      commit.
- [x] `make schema-check`, `make sqlc-check`, `make server-migration-test`, and
      `make server-test-db` pass with live suites required and `-count=1`.
- [x] `make scan` passes. If the connected scan cannot run, the phase is
      blocked; offline Semgrep alone is not phase-exit evidence.
- [x] After revision 5 acceptance passes, P2A-owned traceability rows have
      explicit `PROVEN` state and exact test or command evidence. AC-DOC-001 may
      remain `PENDING` at the tested candidate and changes only in the
      documentation-only closure commit. Handoffs have a named owner and
      downstream gate.
- [x] The owner verifies that `server-test-db` still includes the resume live-DB
      suite and that local and hosted CI retain the released-schema guard plus
      generated-type drift coverage. Any missing shared-file edit blocks the
      candidate.
- [x] P2B handoffs explicitly bind D14 customization paths, the same-key CSRF
      retry contract, D12(ii) complete-document persistence, and AC-SAVE-004's
      real HTTP/OpenAPI proof. P8 retains the bounded hourly global expiry
      sweep; the P2A active-user reap does not replace it.

## Independent gates

- [x] A fresh phase reviewer that authored none of the phase reports no blocking
      defect in behavior, design fit, interface stability, assumptions, or
      traceability. Fixes receive independent re-review.
- [x] A fresh acceptance worker runs catalog revision 5 at the exact candidate
      without editing product code, tests, fixtures, or criteria. Every row is
      `PASS`. `FAIL`, `BLOCKED`, missing evidence, a candidate mismatch, or any
      retry fails. The owner then persists the report and active status changes
      in a documentation-only closure commit that cites, but does not replace,
      the tested candidate.
