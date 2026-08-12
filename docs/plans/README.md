# Delivery plans

[`implementation-plan.md`](implementation-plan.md) is the current roadmap. It
owns phase order, current state, and release gates. A phase directory owns the
detailed tasks for that phase.

## Layout

| Path                                      | Purpose                                                        |
| ----------------------------------------- | -------------------------------------------------------------- |
| `implementation-plan.md`                  | Current roadmap, dependencies, and blockers                    |
| `budgets.md`                              | Numeric limits and measurement rules                           |
| `phase-<id>/README.md`                    | Active or future phase task index                              |
| `phase-<id>/task-*.md`                    | One dispatchable task                                          |
| `phase-<id>/acceptance-catalog-r<N>.md`   | Versioned active-phase acceptance catalog                      |
| `phase-<id>/acceptance-catalog-r<N>.json` | Optional frozen machine index when the phase tool requires one |
| `traceability/`                           | Acceptance ownership and evidence                              |
| `uat-phase-*.md`                          | Immutable legacy automated acceptance catalogs                 |
| `phase-9/README.md`                       | Complete-deployment browser UAT plan                           |

Completed plans and acceptance reports are history. Do not rewrite their
criteria or verdicts. Correct a future run with a new versioned catalog and a
recorded reason.

Active catalogs live inside their phase directory. A fresh catalog author owns
the exact file, derives it before the acceptance run, and freezes it before the
acceptance worker sees product evidence. Legacy `uat-phase-*` files remain
historical only; new phases do not add to that naming scheme.

A task is dispatchable only when its design authority, acceptance rows, numeric
budgets, file ownership, predecessors, and verification command are closed.
`Landed` means code exists. It does not mean the task or phase passed review or
acceptance.
