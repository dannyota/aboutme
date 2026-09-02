# Phase PU — Application UI system implementation plan

Status: **Planned** (2026-09-02). Approved design; no code has landed.

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild every page and editor panel outside the pure renderer on
Tailwind v4 and shadcn-vue primitives behind a small shared component layer,
switch text fields to commit-on-blur, and make the editor preview render even
while the owner photo is unavailable.

**Architecture:** Generated primitives in `app/components/ui`, shared composites
in `app/components/app`, and surfaces that only compose them. Tailwind loads
without Preflight; a chrome reset scoped to `html[data-ui="app"]` excludes the
renderer roots, so the preview, public page, and print browser keep rendering
identical pixels. Tokens move to the document root for teleported dialogs and
menus. The editor core is untouched: intents, coordinator, store, and API are
the same; only the field UI stops exposing presence buttons.

**Tech Stack:** Nuxt 4.5.2, Vue 3.5.40, TypeScript 6.0.3, Tailwind CSS 4.3.3
with `@tailwindcss/vite` 4.3.3, shadcn-nuxt 2.8.2, shadcn-vue CLI 2.8.2 via
`npx`, reka-ui 2.10.4, class-variance-authority 0.7.1, clsx 2.1.1,
tailwind-merge 3.6.0, `@lucide/vue` 1.31.0, Vitest 4.1.10 with
`@nuxt/test-utils`, Playwright 1.62.1, Go 1.27.0 for the dev seed.

**Spec:** [ADR 0029](../../adr/0029-application-ui-toolkit.md) and the
"Application UI" and "Pure renderer" sections of
[`docs/design/web.md`](../../design/web.md). The design wins over this plan.

## Global Constraints

- The renderer under `app/components/resume/**`, `app/components/public/**`,
  `app/public/**`, `server/**`, and `app/pages/_harness/**` does not change. The
  renderer golden HTML and screenshot suites must pass unchanged at every task
  boundary; a diff there is a leak, never a baseline update.
- Tailwind never loads Preflight. The stylesheet contract in
  [decisions U2](decisions.md#u2--stylesheet-contract) is frozen; a task that
  needs a base rule adds it to `base.css` inside the frozen selector guard.
- Dependencies are exactly the [U1 pins](decisions.md#u1--dependencies). No
  other package enters `apps/web/package.json`; the owner pins any transitive
  package a generated primitive imports directly.
- Surfaces render no raw `<button>`, `<input>`, `<select>`, `<textarea>`, or
  hand-written dialog. The crop stage and the ProseMirror content root are the
  only exceptions. A task that needs a new control adds it to `components/app`
  with a test, never inline.
- Every test hook listed in [file-structure](file-structure.md#retained-hooks)
  survives unless the owning task file names its replacement. Tests query by
  role, label, `data-testid`, and `data-action`, never by tag, class, or index.
- Text fields follow the [U4 commit rule](decisions.md#u4--field-commit-rule).
  No Set, Clear, or Remove button remains on a text field. The `FieldIntent`
  type loses its `clear` member; the editor core is not edited.
- Copy follows [U7](decisions.md#u7--copy-rules): sentence case, buttons name
  the action, empty states direct, errors explain. Existing strings that a
  browser proof asserts stay verbatim unless the task file lists the change.
- Each task has one author who writes RED first and owns its adversarial cases
  from [adversarial-coverage](adversarial-coverage.md). No per-task reviewer.
  One non-author performs the ADR 0024 phase review after the records commit.
- Workers edit only the paths their task owns and never use Git. The owner
  serializes `package.json`, `package-lock.json`, `nuxt.config.ts`,
  `eslint.config.mjs`, `app/app.vue`, the stylesheet contract, generated
  primitives, the Makefile, and final records.
- Code blocks in the task files were formatted by Prettier and use double
  quotes; copied code follows the repository ESLint style (single quotes,
  semicolons), which `npx eslint --fix` applies.
- At most three heavy checks run at once. Full `make ci` and connected
  `make scan` run alone on one unchanged candidate commit.

## Plan documents

- [Decisions](decisions.md) freezes U1–U8: dependency pins, the stylesheet
  contract, the three layers, the field commit rule, the test-hook policy,
  visual tokens, copy rules, and verification evidence.
- [Component contracts](component-contracts.md) freezes the props, emits, and
  slots of every `components/app` composite that later tasks consume.
- [File structure](file-structure.md) assigns every path once and lists the
  retained test hooks.
- [Adversarial coverage](adversarial-coverage.md) lists the cases each task's
  author must cover.
- [Exit criteria](exit-criteria.md) is the unchanged-candidate phase gate.

## Task index

| Task                                    | Deliverable                                                               | Acceptance         | Owner                |
| --------------------------------------- | ------------------------------------------------------------------------- | ------------------ | -------------------- |
| [00](task-00-authorities-records.md)    | ADR 0029, design amendment, traceability, roadmap, plan commit            | UI-001…006 PLANNED | Integration owner    |
| [01](task-01-toolkit-foundation.md)     | Dependencies, stylesheet contract, tokens, primitives, renderer isolation | UI-001             | Integration owner    |
| [02](task-02-preview-photo-fallback.md) | Seed without photo; preview renders while the photo read is pending       | UI-004             | Preview author       |
| [03](task-03-shared-composites.md)      | `components/app` composites with tests                                    | UI-002/003         | Composites author    |
| [04](task-04-entry-pages.md)            | Landing, sign-in, registration, recovery, verification, consent pages     | UI-002/005/006     | Entry author         |
| [05](task-05-resume-list.md)            | Resume list page, create/rename/delete dialogs                            | UI-002/005/006     | List author          |
| [06](task-06-settings.md)               | Settings page, password settings, connected agents                        | UI-002/005/006     | Settings author      |
| [07](task-07-editor-shell.md)           | Editor shell, preview toolbar, status, errors, conflicts, session loss    | UI-002/004/005/006 | Shell author         |
| [08](task-08-personal-details.md)       | Personal details panel and contact list on the commit rule                | UI-002/003/006     | Personal author      |
| [09](task-09-section-entries.md)        | Section panel, eight entry forms, date fields, rich-text toolbar          | UI-002/003/006     | Entries author       |
| [10](task-10-structure-templates.md)    | Structure and template panels                                             | UI-002/005/006     | Structure author     |
| [11](task-11-customization.md)          | Customization panel with switches and native selects                      | UI-002/003/006     | Customization author |
| [12](task-12-photo-panel.md)            | Photo panel and crop editor                                               | UI-002/004/005/006 | Photo author         |
| [13](task-13-cleanup-proofs-records.md) | Legacy CSS removal, boundary lint, visual review, browser proofs, records | UI-001…006 PROVEN  | Integration owner    |

## Frozen waves

Phase PU starts from the integrated PM candidate. A wave starts only when its
start condition holds; shared owner windows never overlap a task that reads or
writes the same surface.

| Wave | Tasks            | Start condition                    | Heavy limit                                    |
| ---- | ---------------- | ---------------------------------- | ---------------------------------------------- |
| W0   | 00, 02           | Plan approved                      | Owner records; one Go check and one Vitest run |
| W1   | 01               | T00 committed                      | Owner alone: install, build, renderer suites   |
| W2   | 03               | T01 committed                      | One Vitest run                                 |
| W3   | 04, 05, 06       | T03 committed                      | Three disjoint Vitest runs                     |
| W4a  | 07               | T03 committed; W3 reports accepted | One Vitest run                                 |
| W4b  | 08, 09           | T07 committed                      | Two disjoint Vitest runs                       |
| W5   | 10, 11, 12       | T07 committed                      | Three disjoint Vitest runs                     |
| W6   | 13, review, exit | T00–T12 reports accepted           | Owner alone: build, browser proofs, then gates |

After W4b, the integration owner confirms that only the T13-owned legacy
`OptionalField.vue` still emits `clear`, and W5 starts after the integrated web
typecheck is green. T13 deletes that final producer, narrows `FieldIntent` to
`set | unset`, and runs web typecheck before its gates.

## Dispatch and completion

The integration owner commits this approved plan and dispatches W0. Each task
brief names the task file, the integrated base commit, the authorities, the
acceptance IDs, the owned paths, the exact check, and the report format.

After T13, the owner updates the master plan, traceability, architecture
narrative, and the web README, then commits those records locally. One fresh
non-author reviews the complete candidate and confirms by name the renderer
isolation, the dialog focus invariants, the field commit rule, the hostile-text
rendering, the test-hook retention, and the theme and CSP behavior. Findings
return to the owning author and the same reviewer confirms fixes. The owner then
runs the exit checklist, `make ci`, and connected `make scan` on one unchanged
candidate before push, and deletes this directory at exit.
