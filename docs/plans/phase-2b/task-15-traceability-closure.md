# Task 15: Close traceability, docs, and handoffs

The acceptance criteria already exist before dispatch. This task records exact
evidence and closes the living documentation; it must not invent criteria after
the implementation has landed.

**Tier:** Normal. It changes documentation only.

**Files:** `docs/plans/traceability/{ac-media,ac-save,ac-sec,ac-doc}.md`,
`docs/architecture.md`,
`docs/plans/phase-2b/{README.md,exit-criteria.md,integration-handoffs.md}`. No
other status, handoff, or shared integration file is in scope.

## Steps

- [ ] Add a `State` value and exact file plus test name for
      AC-MEDIA-001/002/004/005/008/009, AC-SAVE-001/002/004/005, and the P2B
      half of AC-SEC-003. Record the P2B evidence slices for AC-MEDIA-003/006
      but keep those cross-phase rows `PLANNED` until P8-priv proves account
      deletion and worker behavior. Keep AC-MEDIA-007 `PLANNED` under P8-priv
      while citing P2B's queue and paginated-backend handoff evidence. No row
      becomes `PROVEN` from a report alone.
- [ ] Add P2B HTTP evidence to borrowed AC-DOC rows without changing their
      owning phase.
- [ ] Verify the traceability prefix and row totals mechanically. Correct the
      index if a reviewed criterion changed before dispatch.
- [ ] Update `docs/architecture.md` with the actual resume routes, media
      backends, failure boundary, and remaining gaps. Describe repository state,
      not intended completion.
- [ ] Walk [integration handoffs](integration-handoffs.md). Each row is applied
      or has a named owner and downstream gate.
- [ ] Prove construction stubs are gone with
      `! rg -n --glob '*.go' --glob '!**/*_test.go' 'not_implemented|StatusNotImplemented' apps/server/internal/resumeapi`,
      the route-table equality test, and Task 12's `TestNoRouteAnswers501`.
      OpenAPI must contain no `501` response.
- [ ] Update this phase's task index with exact `landed`, `reviewed`, and
      `proven` states. Do not collapse them into “complete.”
- [ ] Run `make docs-fmt` and `make docs-lint`. Verify every changed link and
      report all evidence to the integration owner. Do not stage or commit.

## Acceptance mapping

This task closes evidence for every P2B-owned row. The immutable phase
acceptance worker adjudicates the rows at the candidate commit.
