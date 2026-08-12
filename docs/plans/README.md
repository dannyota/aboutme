# Delivery plans

[`implementation-plan.md`](implementation-plan.md) is the current roadmap. It
owns phase order, current state, and release gates. A phase directory owns the
detailed tasks for that phase.

## Layout

| Path                                 | Purpose                                        |
| ------------------------------------ | ---------------------------------------------- |
| `implementation-plan.md`             | Current roadmap, dependencies, and blockers    |
| `budgets.md`                         | Numeric limits and measurement rules           |
| `phase-<id>/README.md`               | Active or future phase task index              |
| `phase-<id>/task-*.md`               | One dispatchable task                          |
| `phase-<id>/exit-criteria.md`        | Phase exit checklist                           |
| `phase-<id>/adversarial-coverage.md` | Adversarial cases the owning tasks must cover  |
| `traceability/`                      | Acceptance ownership and evidence              |
| `uat-phase-*.md`                     | Immutable legacy automated acceptance catalogs |
| `phase-9/README.md`                  | Complete-deployment browser UAT plan           |

Completed plans and acceptance reports are history. Do not rewrite their
criteria or verdicts.

An active phase exits through its `exit-criteria.md` checklist plus `make ci`
and connected `make scan` at one unchanged candidate commit, after one fresh
review of the integrated diff. See
[ADR 0024](../adr/0024-single-pass-delivery-gates.md). A criterion that turns
out to be wrong is corrected in the same phase, with the change noted. Legacy
`uat-phase-*` files stay historical; new phases do not use that naming scheme.

A task is dispatchable when its design authority, acceptance rows, numeric
budgets, file ownership, predecessors, and verification command are settled.
`Landed` means code exists. It does not mean the task or phase passed review.
