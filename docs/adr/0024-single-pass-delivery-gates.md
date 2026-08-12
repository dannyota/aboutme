# 0024 — Delivery uses one author pass and one phase review

Status: Accepted (2026-08-12)

Supersedes the process parts of [ADR 0011](0011-risk-tiered-delivery-gates.md).
ADR 0011's product decisions (local `make ci` as the gate of record, per-commit
gitleaks, phase-batched Semgrep, early browser checks) stay in force.

## Context

ADR 0011 cut five phase gates to two and made review effort follow risk. Three
costs survived that cut:

- Every high-risk task carried a second agent whose only job was to write tests
  before the implementer's diff existed, plus the chronology evidence proving it
  never read that diff.
- Every phase carried a frozen acceptance catalog, a separate fresh catalog
  author, and a read-only acceptance worker who reran the catalog and recorded
  per-row expected/observed evidence.
- Criteria were immutable during a run, so a criterion that was wrong failed the
  phase instead of being corrected.

Phase 2A paid that bill: five catalog revisions and two failed acceptance runs,
most of them evidence and process defects rather than product defects. The
repository has no users, no production data, no cloud footprint, and one owner.
Assurance priced for a live service is being charged against a pre-release
codebase, and it is the dominant cost per landed change.

## Decision

**One author pass per task.** The author writes the failing test first,
implements the smallest correct change, and runs the narrowest affected checks.
There is no separate blind test author and no per-task reviewer.

**Adversarial cases move into the owning task.** The independent suites — write
safety, CAS and autosave races, size-bound matrices, hostile input, authz and
CSRF — remain required coverage. They are stated as test checklists in the task
that owns the behavior, and the implementing author writes them. Their content
is unchanged; their choreography is removed.

**One review per phase.** Before the phase is pushed, a fresh worker that
authored none of it reads the integrated diff for defects, design fit, interface
stability, and traceability. Blocking findings go back to an author; the same
reviewer confirms the fix. Security-sensitive phases — authentication, sessions,
CSRF, sanitizing, concurrency and CAS, idempotency, media privacy, publish
revocation — require the reviewer to confirm those invariants by name.

**Phase exit is a checklist, not a ceremony.** The phase directory holds one
`exit-criteria.md`. At one unchanged candidate commit the integration owner runs
that checklist plus `make ci` and connected `make scan`. There is no frozen
acceptance catalog, no separate acceptance worker, and no per-row evidence
transcript. A failing item is fixed and the checklist is rerun; the phase
records the final state and what changed between runs.

**Criteria are correctable.** A criterion that is wrong, unsatisfiable, or tests
the wrong thing is fixed when it is found, in the same phase, with the change
noted. Correctness of the product outranks immutability of the checklist.

**Unchanged:** `make ci` is the gate of record, `gitleaks` runs per commit
because the repository is public, Semgrep runs at the phase gate, and no AWS,
DNS, or other cloud mutation happens before local UAT passes and the human owner
authorizes the exact scope.

## Consequences

- A defect that a second independent test author would have caught can now reach
  the phase review instead. That is the accepted trade. The phase review and the
  full local gate remain, so the defect is caught before push, not after
  release.
- Cost per landed change drops from roughly four agent passes to two.
- Completed phase records, their catalogs, and their verdicts stay as written.
  This ADR governs phases that have not yet run their gate.
- Restore the independent acceptance pass when the service has real users or
  production data. That is a new ADR at that time, not a silent revert.
