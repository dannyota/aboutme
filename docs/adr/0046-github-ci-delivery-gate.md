# 0046: GitHub CI is the full delivery gate

Status: Accepted (2026-09-19), by the owner's repository policy.

Supersedes the local full-gate and phase-level review clauses of
[ADR 0011](0011-risk-tiered-delivery-gates.md) and
[ADR 0024](0024-single-pass-delivery-gates.md). Their author discipline,
adversarial test ownership, per-commit gitleaks, and early browser checks stand.

## Context

ADRs 0011 and 0024 made a full local `make ci` run the gate of record and tied
the fresh review to a phase. The repository policy now assigns the complete gate
to GitHub CI. Local workers run the narrowest affected checks, which keeps
feedback focused and avoids competing full builds on a memory-bounded laptop.

A tag and deployment must refer to the exact commit that passed every required
hosted check. A later commit cannot inherit an earlier commit's green result.

## Decision

- The author writes the failing test first, makes the smallest correct change,
  and runs the narrowest affected checks. The author owns adversarial cases for
  write safety, races, bounds, hostile input, authorization, and CSRF.
- Each commit runs the pre-commit gitleaks scan.
- One fresh reviewer reads each plan or release before push. Local-only and
  test-only changes skip this review. Findings return to the author, and the
  same reviewer confirms each fix.
- GitHub CI is the full gate. It runs `make ci`, Semgrep, and full-history
  gitleaks. A red run is fixed forward at once.
- A release commit is pushed alone to `main`. Tagging and deployment wait for
  green GitHub CI on that exact commit.
- A full local `make ci` run remains available to debug a CI failure. It runs
  alone because of its resource cost.

## Compatibility, security, and size

This decision changes delivery process only. It changes no product behavior,
schema, API, stored data, migration, or client contract. Older clients have no
new behavior, and no data conversion or loss rule applies.

Per-commit gitleaks remains the boundary before a secret can enter public Git
history. Hosted Semgrep and full-history gitleaks remain required on the exact
release commit. The record adds no runtime dependency, request bytes, stored
bytes, or deployment resource.

## Release

The repository policy already applies this process. This record makes the ADR
history agree with that policy. It requires no application migration or deploy.
No owner decision remains open.
