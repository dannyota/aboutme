# Task 00 — Authorities and records

**Acceptance:** AC-UI-001…006 rows `PLANNED`.

**Depends on:** the integrated PM candidate.

**Owned paths:** T00 paths in `file-structure.md`.

## Contract

- ADR 0029 is accepted and integrated: the decision index lists it, the design
  README states every ADR through 0029 is integrated, and the web design carries
  the "Application UI" section plus the preview-without-photo sentence.
- `docs/plans/traceability/ac-ui.md` exists with AC-UI-001…006 `PLANNED`, and
  the index counts 135 rows across 17 prefixes.
- The master plan is revision 23 with the PU row, the delivery order, and the
  dependency graph edges `PU --> P5B` and `PU --> P9`.
- `docs/plans/phase-pu/` holds the README, decisions, component contracts, file
  structure, adversarial coverage, exit criteria, and task files 00–13.

## Steps

- [ ] Run the record checks:

  ```sh
  npx prettier --check 'docs/**/*.md'
  npx markdownlint-cli2 'docs/**/*.md'
  ```

  Expected: both pass. If prettier reports a file, run `make docs-fmt` and
  re-check.

- [ ] Confirm the design has no stale field-button text:

  ```sh
  grep -rn "Set/Clear/Remove\|explicit presence intents" docs/design
  ```

  Expected: no output.

- [ ] Stage and commit the records only:

  ```sh
  git add -- docs/adr/0029-application-ui-toolkit.md docs/design/README.md docs/design/web.md docs/design/decisions.md docs/plans/traceability/README.md docs/plans/traceability/ac-ui.md docs/plans/implementation-plan.md docs/plans/phase-pu
  git diff --cached --name-only
  git commit -m "docs: adopt the application UI toolkit and plan phase PU" -- docs/adr/0029-application-ui-toolkit.md docs/design/README.md docs/design/web.md docs/design/decisions.md docs/plans/traceability/README.md docs/plans/traceability/ac-ui.md docs/plans/implementation-plan.md docs/plans/phase-pu
  ```

  `AGENTS.md` stays unstaged.

## Handoff

Report the commit hash and the two check outputs.
