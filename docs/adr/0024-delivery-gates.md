# 0024: GitHub CI is the full delivery gate

Status: Accepted (2026-08-11, 2026-08-12, 2026-09-19, 2026-09-20), by the
owner's repository policy.

## Context

Delivery once applied the same ceremony to every change: blind test authors,
per-task reviewers, frozen acceptance catalogs, and five phase gates, and then a
full local `make ci` as the gate of record. Most of the cost bought little for a
pre-release codebase with one owner, and the laptop lost work to out-of-memory
failures running full suites.

A tag and deployment must refer to the exact commit that passed every required
hosted check. A later commit cannot inherit an earlier commit's green result.
The repository is public, so a secret in history is exposed on push and cannot
be recalled.

## Decision

- **One author pass.** The author writes the failing test first, makes the
  smallest correct change, and runs only checks that are safe under the local
  resource policy. When the regression test cannot run safely, the author
  records the exact unrun command and the expected failure before implementing
  the change. No gate requires the author to observe the failure locally.
- **Adversarial cases belong to the owning task.** The author writes the cases
  for write safety, races and CAS, size bounds, hostile input, authorization,
  and CSRF. There is no separate blind test author.
- **Per-commit gitleaks.** Each commit runs the pre-commit gitleaks scan.
- **One fresh review.** One reviewer who authored none of the work reads each
  plan or release before push. Local-only and test-only changes skip this
  review. Findings return to the author, and the same reviewer confirms each
  fix. A security-sensitive change (authentication, sessions, CSRF, sanitizing,
  concurrency and CAS, idempotency, media privacy, publish revocation) needs the
  reviewer to confirm those invariants by name.
- **GitHub CI is the full gate.** It runs `make ci`, Semgrep, and full-history
  gitleaks, plus heavy builds, typechecks, full suites, race tests, linters, and
  browser suites. A red run is fixed forward at once.
- **Exact green commit.** A release commit is pushed alone to `main`. Tagging
  and deployment wait for green GitHub CI on that exact commit.
- **No full local gate.** Full local `make ci` is prohibited, including while
  debugging a CI failure. The manager may scope one local diagnostic process
  across all worktrees, with a memory limit of at most 2 GiB, no swap, at most
  two CPUs, and a time limit. Prefer a hosted rerun when hosted evidence can
  answer the question. Diagnose a failed or out-of-memory command before any
  local rerun; never repeat it unchanged to seek a pass.
- **Reuse evidence.** The manager and reviewer reuse check evidence from the
  exact commit under review and do not duplicate a check for role-specific
  evidence.
- **Browser checks early.** User-visible changes are exercised through scripted
  headless Playwright suites as they land. A check that proves user interface
  behavior with no hosted CI equivalent may run as a narrow, bounded local
  exception under the same resource limits; it cannot replace or weaken the
  release proof.
- **Correctable criteria.** An acceptance criterion that is wrong,
  unsatisfiable, or tests the wrong thing is fixed when found, with the change
  noted.

`AGENTS.md` and `instructions/` carry the operational detail.

## Compatibility, security, and size

This is delivery process only. It changes no product behavior, schema, API,
stored data, migration, or client contract.

Per-commit gitleaks is the boundary before a secret can enter public Git
history. Hosted Semgrep and full-history gitleaks remain required on the exact
release commit. Bounded local diagnostics limit the effect of hostile or faulty
test inputs on the shared laptop.

## Consequences

- A defect that a second independent test author would have caught can reach the
  review instead. That is the accepted trade.
- Environment-specific defects surface in hosted CI rather than on the laptop.
- An independent acceptance pass returns only through a new ADR.

## History

- Former ADR 0011 (2026-08-11): risk-tiered review, two phase gates, local
  `make ci` as the gate of record, per-commit gitleaks, phase-batched Semgrep,
  and early browser checks. Its tier table, phase gates, and parallel design
  lane were process choices that 0024 and 0046 replaced.
- Former ADR 0024 (2026-08-12): one author pass, adversarial cases in the owning
  task, one review per phase, correctable criteria.
- Former ADR 0046 (2026-09-19, updated 2026-09-20): GitHub CI as the full gate,
  replacing the local gate of record and the phase-level review. Its rules are
  the core of this record.
