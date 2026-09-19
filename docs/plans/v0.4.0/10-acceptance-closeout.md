# Localization acceptance close-out brief

Role: manager. This is manager-owned integration work, not a worker dispatch.
Start after all automated, browser, dogfood, and native Vietnamese-speaking
human review evidence is available.

## Objective and authority

Record the final evidence for `AC-EDITOR-018` and `AC-UI-014` without changing
their meaning or rewriting earlier evidence. Read `AGENTS.md`,
`docs/adr/0047-bilingual-resume-workspace.md`,
`docs/design/editor-localization.md`, every author report, browser evidence,
dogfood evidence, and the native-speaker human review.

## Owned paths

- Modify `docs/plans/traceability/ac-editor.md`.
- Modify `docs/plans/traceability/ac-ui.md`.

Do not edit product source, other acceptance rows, design, ADRs, runbooks, or
deployment files during close-out.

## Evidence and state rules

- Update `AC-EDITOR-018` and `AC-UI-014` from `PLANNED` to `PROVEN` only when
  unit, source, browser, phone, desktop, both-locale, sample-start, data
  preservation, accessibility, publish, PDF, and required human wording review
  evidence all pass.
- The rows cite exact commands, retained evidence paths, and the human reviewer
  record. Do not claim a check that did not run.
- After committing the close-out, the manager report and reviewer dispatch
  record the final release candidate commit hash. The committed rows do not cite
  their own commit.
- If the human review or any required proof is absent or failed, leave the
  affected row `PLANNED`, name the missing evidence, and keep release acceptance
  open. Agent dogfood is supporting evidence only.

## Exact checks and report

Run:

```bash
node_modules/.bin/prettier --check docs/plans/traceability/ac-editor.md \
  docs/plans/traceability/ac-ui.md
npx markdownlint-cli2 docs/plans/traceability/ac-editor.md \
  docs/plans/traceability/ac-ui.md
git diff --check -- docs/plans/traceability/ac-editor.md \
  docs/plans/traceability/ac-ui.md
```

Report the exact changed files, final state of both rows, evidence cited, every
check and result, skipped checks with reason, and open items. Use short plain
text with no em dash.
