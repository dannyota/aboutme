# Integrated localization review brief

Role: reviewer. Model: `gpt-5.6-sol`. Start after the manager verifies every
author report, the Vietnamese dogfood report, native-speaker human review
evidence, and the acceptance close-out report.

## Objective and authority

Review the complete v0.4.0 localization release once. Read `AGENTS.md`,
`docs/adr/0047-bilingual-resume-workspace.md`,
`docs/design/editor-localization.md`, `docs/design/product.md`,
`docs/design/web.md`, `AC-EDITOR-018`, `AC-UI-014`, and every task report.

You are read-only. Own no files. Do not edit, stage, commit, push, tag, deploy,
or rerun a failed check into a pass. Review the exact integrated diff from its
recorded base through the candidate commit. Exclude unrelated working-tree
changes.

## Required review

- Confirm each localized route, shell, field, control, status, dialog, title,
  error, accessible name, live region, publish state, and PDF state has both
  locale entries and no unreviewed literal.
- Trace locale toggles through list, blank and sample creation, editor drafts,
  conflicts, publish, and PDF. Confirm no toggle changes resume language,
  authored or default document data, title, sample, public settings, commands,
  revision, URLs, requests, or PDF bytes, and no toggle triggers a write.
- Confirm controllers retain semantic states and codes. Known issue codes map to
  safe local copy. Unknown outcomes do not expose server messages or HTML.
- Adversarially review invalid cookies, hostile user text, auth and session
  loss, CSRF and Origin handling, reauthentication, publish privacy, PDF bounds,
  pending and uncertain outcomes, focus preservation, and concurrent locale and
  save events.
- Confirm settings and `/authorize` remain English, the preview keeps resume
  `lang`, renderer and public output are unchanged, stable hooks remain, and
  editor catalogs do not enter public-render or print chunks.
- Check the source-manifest diff, client JavaScript byte delta, browser
  evidence, dogfood evidence, native-speaker human review, and every claimed
  command result.
- Confirm `AC-EDITOR-018` and `AC-UI-014` cite complete evidence before they are
  `PROVEN`. If either row lacks required evidence, require it to remain
  `PLANNED` and keep release acceptance open.

## Report contract

Rank findings by severity. For each finding, give the failure scenario, exact
path and line, violated authority, and owning author task. Name every invariant
confirmed. List checks inspected, checks not run with reason, evidence paths,
and open items. If fixes land, reread only the corrected integrated diff and
confirm or reject each fix. Use short plain text with no em dash.
