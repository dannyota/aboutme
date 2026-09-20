# 0046: GitHub CI is the full delivery gate

Status: Accepted (2026-09-19), by the owner's repository policy.

Policy update: Accepted (2026-09-20), by the owner.

Supersedes the local full-gate and phase-level review clauses of
[ADR 0011](0011-risk-tiered-delivery-gates.md) and
[ADR 0024](0024-single-pass-delivery-gates.md). Their author discipline,
adversarial test ownership, per-commit gitleaks, and early browser checks stand.

## Context

ADRs 0011 and 0024 made a full local `make ci` run the gate of record and tied
the fresh review to a phase. The repository policy now assigns the complete gate
to GitHub CI. Heavy builds, typechecks, full suites, race tests, linters,
browser suites, and Semgrep run in hosted CI. The laptop has lost work to an
out-of-memory failure, so these checks do not run locally.

A tag and deployment must refer to the exact commit that passed every required
hosted check. A later commit cannot inherit an earlier commit's green result.

## Decision

- The author writes the failing test first, makes the smallest correct change,
  and runs only checks that are safe under the local resource policy. When the
  regression test cannot run safely, the author records the exact unrun command
  and the expected failure before implementing the change. No delivery gate
  requires the author to observe the test fail locally. The author owns
  adversarial cases for write safety, races, bounds, hostile input,
  authorization, and CSRF.
- Each commit runs the pre-commit gitleaks scan.
- One fresh reviewer reads each plan or release before push. Local-only and
  test-only changes skip this review. Findings return to the author, and the
  same reviewer confirms each fix.
- GitHub CI is the full gate. It runs `make ci`, Semgrep, and full-history
  gitleaks. Hosted CI also runs heavy builds, typechecks, full suites, race
  tests, linters, and browser suites. A red run is fixed forward at once.
- A release commit is pushed alone to `main`. Tagging and deployment wait for
  green GitHub CI on that exact commit.
- Full local `make ci` is prohibited, including while debugging a CI failure.
- The manager may scope one local diagnostic process across all worktrees. The
  process must enforce a memory limit of at most 2 GiB, disable swap, use at
  most two CPUs, and have a time limit. Prefer a hosted rerun when hosted
  evidence can answer the same question.
- Diagnose a failed or out-of-memory command before any local rerun. Do not
  repeat the same command unchanged merely to seek a passing result.
- The manager and reviewer reuse check evidence from the exact commit under
  review. They do not duplicate a check to create role-specific evidence.
- A check that proves actual user interface behavior and has no hosted CI
  equivalent may run as a narrow, bounded local exception. The manager scopes
  the exception under the same resource limits. The exception cannot replace or
  weaken the release proof required for that commit.

## Compatibility, security, and size

This decision changes delivery process only. It changes no product behavior,
schema, API, stored data, migration, or client contract. Older clients have no
new behavior, and no data conversion or loss rule applies.

Per-commit gitleaks remains the boundary before a secret can enter public Git
history. Hosted Semgrep and full-history gitleaks remain required on the exact
release commit. Bounded local diagnostics limit the effect of hostile or faulty
test inputs on the shared laptop. The record adds no runtime dependency, request
bytes, stored bytes, or deployment resource.

## Release

The repository policy applies this process as soon as the policy text and role
instructions agree with this record. Existing release commits need no rerun. The
change requires no application migration or deploy. The owner approved all
choices in the 2026-09-20 policy update, so no owner decision remains open.
