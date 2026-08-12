# Phase 2A exit criteria

The phase closes only when the candidate commit satisfies every item. A later
product-code change invalidates evidence for each affected path.

## Product and contract

- [ ] Goose migrations `00004` and `00005` remain append-only. Empty-to-head,
      previous-to-head, concurrent-lock, and partial-failure migration tests
      pass.
- [ ] Direct SQL and store tests prove the three-resume cap, ownership
      indistinguishability, title bounds, and every relational constraint.
- [ ] Every document bound has passing limit and failing limit+1 coverage. The
      schema-walk completeness guard finds no untested bound.
- [ ] Cleared optional contact values round-trip unchanged through live writes.
- [ ] Same-revision writes have one winner. Losers receive the winning revision
      and document without an existence oracle.
- [ ] Idempotency replays the stored result, rejects a changed body, serializes
      contenders, and rolls back mutation and record together.
- [ ] Reads project old rows without writing. CAS backfill does not bump the
      user revision and loses safely to document or title changes.
- [ ] Released schemas, retained types, registries, accepted/emitted versions,
      and adjacent converters pass append-only and fail-closed tests.
- [ ] No production package outside `internal/resume` calls a generated resume
      write method. The phase review records this as a review rule, not a custom
      static-analysis guarantee.

## Evidence and checks

- [ ] `make ci` passes once, without concurrent heavy workers, at the candidate
      commit.
- [ ] `make schema-check`, `make sqlc-check`, `make server-migration-test`, and
      `make server-test-db` pass with live suites required and `-count=1`.
- [ ] `make scan` passes. If the connected scan cannot run, the phase is
      blocked; offline Semgrep alone is not phase-exit evidence.
- [ ] P2A-owned traceability rows have explicit `PROVEN` state and exact test or
      command evidence. Handoffs have a named owner and downstream gate.

## Independent gates

- [ ] A fresh phase reviewer that authored none of the phase reports no blocking
      defect in behavior, design fit, interface stability, assumptions, or
      traceability. Fixes receive independent re-review.
- [ ] A fresh acceptance worker runs
      [catalog revision 2](acceptance-catalog-r2.md) at the exact commit without
      editing product code, tests, fixtures, or criteria. Every row is `PASS`.
      `FAIL`, `BLOCKED`, missing evidence, or an undisclosed retry fails.
