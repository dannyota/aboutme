# Delivery plans

[`implementation-plan.md`](implementation-plan.md) is the current roadmap. It
owns phase order, current state, and release gates. Active phases and tasks use
numbers, such as Phase 10 and task 10.14. A phase directory owns the detailed
tasks for that phase while the phase is active.

[The v0.4.x roadmap](v0.4-roadmap.md) proposes small releases for Vietnamese
localization, optional two-factor authentication, MCP acceptance, and link
previews. It does not mark those features implemented or approved.

## Layout

| Path                                     | Purpose                                       |
| ---------------------------------------- | --------------------------------------------- |
| `implementation-plan.md`                 | Current roadmap, dependencies, and blockers   |
| `phase-<number>/README.md`               | Active or future phase task index             |
| `phase-<number>/task-*.md`               | One dispatchable task                         |
| `phase-<number>/exit-criteria.md`        | Phase exit checklist                          |
| `phase-<number>/adversarial-coverage.md` | Adversarial cases the owning tasks must cover |
| `traceability/`                          | Acceptance ownership and evidence             |

## Lifecycle

A phase exits through its `exit-criteria.md` checklist and green GitHub CI on
the exact candidate commit, after one fresh review of the integrated diff.
Authors run the affected checks locally. See
[ADR 0046](../adr/0046-github-ci-delivery-gate.md). A criterion that turns out
to be wrong is corrected in the same phase, with the change noted.

When a phase exits, delete its directory and any design draft it carried. Git
history keeps them. The traceability rows the phase proved, the architecture
narrative, and the code are the record of what it built. Numeric limits live in
[`../design/budgets.md`](../design/budgets.md), not in a phase plan.

A task is dispatchable when its design authority, acceptance rows, numeric
budgets, file ownership, predecessors, and verification command are settled.
`Landed` means code exists. It does not mean the task or phase passed review.
