# Phase PV — Application visual identity

Status: **Revision 1, active** (2026-09-04). The human owner approved
[`design.md`](design.md) on 2026-09-04; T00 records are in the tree; work is
paused before T01.

PV replaces the application chrome's look with the stamped-document identity and
finishes the unmerged toolkit rebuild. Every page and editor panel outside the
pure renderer changes; the renderer, the API, the publish contract, and the MCP
boundary do not.

## Authorities and boundaries

- Product truth and positioning: [`PRODUCT.md`](../../../PRODUCT.md) and
  [`../../design/product.md`](../../design/product.md).
- Application UI toolkit: ADR 0029 (arrives with the T01 rebase) and the
  "Application UI" section of [`../../design/web.md`](../../design/web.md).
- Visual identity: [`design.md`](design.md), adopted as ADR 0030 and a web.md
  amendment in T00. The Impeccable direction contract lives in
  `.impeccable/surfaces/apps-web-app-pages-index-vue.md` (seed `aac522e4`,
  code-led); builders reload it before editing any surface.
- Renderer isolation and preview fidelity: "Pure renderer" in web.md; the
  renderer golden HTML and screenshot suites must pass unchanged.
- Publish behavior and copy: P5B contract, now in
  [`../../architecture.md`](../../architecture.md#implemented-public-publish-and-ssr).
- Single-pass delivery:
  [ADR 0024](../../adr/0024-single-pass-delivery-gates.md).

PV owns `AC-UI-001` through `AC-UI-013` (001–006 arrive with the rebase and are
re-proven; 007–013 are new, defined in T00). It preserves `AC-EDITOR-015`,
`AC-PUB-006` through `AC-PUB-010`, and `AC-MCP-007`.

## Base decision

Decided by the human owner on 2026-09-04: PV builds on `codex/phase-pu` (46
commits, closed 2026-09-03, never merged) rebased onto `main`. The rebase is
T01, run by the integration owner, and gets the fresh phase review PU never
received on `main` before any restyling starts. `.worktrees/phase-pu` already
holds that branch at `a4c9d21`.

## Task index

| Task | Work                                                                                                                | Owner                                                                       | Predecessor | Narrow check                                                                                                                                                                       |
| ---- | ------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------- | ----------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| T00  | [ADR 0030, design amendments, traceability, records](task-00-authorities-and-records.md)                            | Integration owner                                                           | None        | `make docs-fmt`, `npx markdownlint-cli2` on owned paths                                                                                                                            |
| T01  | [Rebase phase PU onto main; publish dialog on the Dialog primitive; fresh review](task-01-rebase-toolkit-branch.md) | Integration owner; fresh reviewer                                           | T00         | `make web-lint web-typecheck web-test web-build`, `npx vitest run test/renderer`, `make web-e2e`                                                                                   |
| T02  | [Tokens, chrome font, seal, and state marks](task-02-tokens-seal-state-marks.md)                                    | High-judgment author                                                        | T01         | `cd apps/web && npx vitest run test/app/theme.test.ts test/app/seal.test.ts test/app/state-mark.test.ts test/renderer`                                                             |
| T03  | [Landing page](task-03-landing.md)                                                                                  | Implementer                                                                 | T02         | `cd apps/web && npx vitest run test/landing.test.ts test/landing-sample.test.ts`                                                                                                   |
| T04  | [Auth and consent pages](task-04-auth-pages.md)                                                                     | Implementer                                                                 | T02         | `cd apps/web && npx vitest run test/login.test.ts test/register.test.ts test/forgot-password.test.ts test/reset-password.test.ts test/verify-email.test.ts test/authorize.test.ts` |
| T05  | [Resume list](task-05-resume-list.md)                                                                               | Implementer                                                                 | T02         | `cd apps/web && npx vitest run test/editor/resume-list.test.ts test/app/relative-time.test.ts`                                                                                     |
| T06  | [Settings](task-06-settings.md)                                                                                     | Implementer                                                                 | T02         | `cd apps/web && npx vitest run test/password-settings.test.ts test/connected-agents.test.ts test/sessions-settings.test.ts test/app/user-agent.test.ts test/sessions.test.ts`      |
| T07  | [Editor shell and narrow layout](task-07-editor-shell.md)                                                           | High-judgment author                                                        | T02         | `cd apps/web && npx vitest run test/editor/editor-shell.test.ts test/editor/editor-preview.test.ts test/app/app-shell.test.ts`                                                     |
| T08  | [Inspector labels and grouping](task-08-inspector-labels.md)                                                        | Implementer                                                                 | T07         | `cd apps/web && npx vitest run test/editor/customization-panel.test.ts test/editor/personal-details.test.ts test/editor/field-labels.test.ts`                                      |
| T09  | [Publish dialog and the stamp](task-09-publish-dialog.md)                                                           | Implementer                                                                 | T07         | `cd apps/web && npx vitest run test/editor/publish-dialog.test.ts test/editor/use-stamp.test.ts test/editor/editor-shell.test.ts test/editor/editor-preview.test.ts`               |
| T10  | [Finish review, browser proofs, records, exit](task-10-finish-review-records-exit.md)                               | Integration owner; Impeccable reviewer and documenter; fresh phase reviewer | T03–T09     | [`exit-criteria.md`](exit-criteria.md)                                                                                                                                             |

## Waves

| Wave | Tasks              | Start condition            | Heavy limit                                       |
| ---- | ------------------ | -------------------------- | ------------------------------------------------- |
| W0   | T00                | Design approved            | Owner records only                                |
| W1   | T01                | T00 committed              | Owner alone: install, build, renderer suites, e2e |
| W2   | T02                | T01 reviewed and committed | One Vitest run                                    |
| W3   | T03, T04, T05, T06 | T02 committed              | Four disjoint Vitest runs, no build               |
| W4   | T07, T08, T09      | W3 reports accepted        | Three disjoint Vitest runs                        |
| W5   | T10                | W4 reports accepted        | Owner alone: build, detector, proofs, then gates  |

T08 and T09 start after T07 because they render inside the shell T07 rebuilds.
Each author writes the named failing test first and owns the adversarial cases
in [`adversarial-coverage.md`](adversarial-coverage.md). There is no per-task
review. The integration owner rereads each result, reruns its key check, and
commits coherent work. One fresh non-author reviews the integrated diff in T10.

## Model assignment

Per AGENTS.md: T01, T02, and T07 are high-judgment (Opus or `gpt-5.6-sol`);
T03–T06, T08, and T09 implement from complete contracts (Haiku or
`gpt-5.6-luna`); the T01 rebase review and the T10 phase review use Sonnet or
`gpt-5.6-sol`. The Impeccable finish reviewer and documenter are the plugin's
shipped subagents, spawned fresh in T10.

## Delivery result

The phase is complete when a visitor sees the rendered sample resume with its
seal on the landing page, an owner sees publish state on the resume list and a
settings page they can read, the editor shows one red control and keeps the
sheet whole on a phone, publishing stamps the preview, the Impeccable finish
review returns `ship`, `DESIGN.md` records the built world, and every renderer
baseline is byte-identical to `main`.
