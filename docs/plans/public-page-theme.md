# Public page bar and light/dark theme

Status: planned; ships after [sign in to view](viewer-analytics.md). Design: [public page bar and color scheme](../design/public-page-theme.md). The decision change is a proposed edit to [ADR 0020](../adr/0020-application-visual-identity.md) and [ADR 0004](../adr/0004-resume-document-contract.md); no new ADR. Mockups: `.dev/design/public-theme/` in the main checkout (ignored), rendered from production template pages by a scratch script; they predate the seal logo, so the bar's mark in them is stale.

|Part|Outcome|Risk|
|-|-|-|
|Page bar|Full-width bar with the seal mark, a muted credit, and a 32 px outlined Download PDF button; download-off state; not sticky|Low: markup and CSS; the validator hooks stay; no contract change|
|Color scheme|Owner picks Light, Dark, or Match device per resume; the public page and Web preview render the dark palette rule; PDF, print, card, and gate stay light|Medium: document v5, renderer roles, ADR 0020 and ADR 0004 edits|

Recommended release split (open decision 2 in the design): the page bar as one release, then the color scheme as the next. If the owner keeps one release, both still land in this order and deploy once.

## Before any code

- The owner answers the design's open decisions 1 to 6. The page bar needs only decisions 2 and 3.
- The owner approves the proposed ADR 0020 and ADR 0004 edits before any color scheme code. The manager then turns each "Proposed" History line into the accepted wording, folds ADR 0004's v5 subsection into its version table, and updates `docs/adr/README.md` (the 0020 row says "white resume") and `docs/design/decisions.md` if its 0020 or 0004 row needs it.
- Sign in to view is on `main`. Its `PublicResumeApp.vue` changes, `apps/web/app/public/overlay/JoinInvite.vue`, and `PublicGate.vue` are the base these steps edit; recheck the file list against them.

## Order

1. Page bar (frontend, then qa, designer, reviewer).
2. Document v5 (backend, with the architect's design text in the same change).
3. Color scheme in the renderer and editor (frontend), after step 2 is on `main` so the generated types exist.
4. Proofs and baselines (qa), then the designer's finish review and one reviewer pass over steps 2 and 3.

## 1. Page bar

|Role|Files|Work|
|-|-|-|
|frontend|`apps/web/app/components/public/PublicResumeApp.vue`; new `apps/web/app/components/public/PublicMark.vue`; new shared mark geometry module under `apps/web/app/components/app/` and `AppLogo.vue` importing it; `apps/web/app/components/resume/ResumeDocument.vue` (public page CSS block); `apps/web/test/public-render/render.test.ts`, `hydration.test.ts`; the `AppLogo` tests; `apps/web/e2e/screenshot.spec.ts`; web source manifest for new files|Move the bar out of `.public-measure`, add the inner row, mark, and icon; replace the bar CSS with the spec's tokens and sizes; keep the three class names and the credit anchor's exact shape; extend the geometry test (full width, 32 px button on a fine pointer, hidden in print)|
|qa|`apps/web/e2e/baselines/public--classic-serif--2560.png`, `public--modern-sidebar--2560.png`, `public--modern-sidebar--390.png`; `deploy/dev-https-browser/public.spec.ts` if it asserts bar layout|Regenerate the three baselines in CI; confirm the public proof still finds both links|
|designer|none|Finish review of the built bar at 390 and 1440 px in both languages|

Go needs no change: the credit anchor keeps one text child, the download href is unchanged, and the validator accepts the SVG and span inside the download anchor. The reviewer confirms the validator passes on a Vietnamese and an English page, with download on and off; a Go validator test pins that markup.

## 2. Document v5

|Role|Files|Work|
|-|-|-|
|backend|new `packages/schema/resume.v5.schema.json`; `resume.schema.json`; `released-versions.json` (append v5, current 5); regenerated `packages/schema/gen/` Go and TS and `apps/web/app/editor/documentValidator.generated.mjs`; `packages/schema/test/` (new v5 document test); fixtures and samples whose `schemaVersion` is pinned; new `apps/server/internal/resume/docmigrate/v4_v5.go` and test, `docmigrate.go`; `apps/server/internal/resumeapi/persist.go` (`keepV5Fields` for v1 to v4 writes), `customization_allowlist.go` (set and unset `colorScheme`), new `wireversion_v5_test.go`; `apps/server/internal/publicresume/projection.go` (copy the leaf); `apps/server/internal/printsnapshot/snapshot.go` (drop it); `docs/api/openapi.yaml` (`PublicCustomization.colorScheme`) and the regenerated web client; tests across `apps/server`, `apps/web/test`, and `deploy/dev-https-browser` that pin the current version; `docs/runbooks/production.md` (rollback line names document v5)|Add the optional enum; converters v4 to v5 (no change) and v5 to v4 (drop); older writes keep the stored value; MCP can set and unset it through `update_customization`; the preview card envelope is untouched. The v4 release touched about 150 files, mostly version pins; expect the same|
|architect|`docs/design/data.md` (document versions: v5, its loss, the write rule); `docs/design/templates/tokens.md` (leaf table: 28 leaves, 26 author-controlled, 11 optional); `docs/design/templates/contract.md` §2 and §3 (kept on apply); `docs/design/templates/colors.md` (pointer to the dark rule)|Text for the same change, supplied to the backend change|

## 3. Color scheme in the renderer and editor

|Role|Files|Work|
|-|-|-|
|frontend|new `apps/web/app/components/resume/darkPalette.ts` and its test; `resolveRenderModel.ts` (`RenderContext.colorScheme`), `useResumeStyles.ts` (`--dark-color-*` at each scope); `ResumeDocument.vue` (screen-only scheme rules, root `color-scheme` and ground); `PublicResumeApp.vue` (`data-color-scheme`, context); `apps/web/app/public/overlay/JoinInvite.vue` (bar tokens per scheme, per decision 5); `apps/web/app/components/editor/EditorPreview.vue` (Web mode only); `apps/web/app/components/editor/customization/fields.ts`, `labels.ts`, `CustomizationPanel.vue`; `apps/web/app/i18n/editor-controls.ts`; `apps/web/app/editor/commands.ts` (set and unset paths); `apps/web/app/components/resume/applyTemplate.ts` and `apps/web/app/editor/templateDiff.ts` (keep on apply); `apps/web/app/pages/_harness/render.vue` (`scheme` query)|Dark rule as specified, with a table test over all 20 presets and both columns; the field first in Colors with the spec's copy; the preview follows the device preference for Match device; the gate stays light|

## 4. Proofs, baselines, review

|Role|Files|Work|
|-|-|-|
|qa|new baselines `public--modern-sidebar--dark--390.png`, `public--creative-accent--dark--1440.png`, `public--executive-band--dark--1440.png`, `public--classic-serif--system--1440.png` (emulated dark preference); `apps/web/e2e/screenshot.spec.ts` cells; dev-https public proof|Print media on a dark page computes the light surface; the PDF of a dark resume matches its light PDF; the preview card is unchanged; a sign-in resume's gate is light and its invite follows the scheme|
|designer|`DESIGN.md` (the white-resume line gains the owner's dark scheme; the public chrome paragraph points at the bar)|Finish review of three templates in both schemes at 390 and 1440 px, and the editor field in both languages|
|reviewer|none|One pass over steps 2 and 3: loss and write rules, allowlist, projection and snapshot, no CSS built from the enum, no script added, validator unchanged|

Unchanged baselines in every step: every `*--paged.png` and `*--continuous.png`, `print-baselines/`, `chrome--*`, `template-*`, and all golden HTML.

## Writing rules for briefs

Code, comments, tests, and living docs cite the design page, ADR 0020, ADR 0004, or `AC-*` IDs, never this plan or its steps. No em dashes. Human-read Markdown keeps Prettier's 80-column wrap. Run `make pre-push` before every push; GitHub CI is the gate. Commit messages never mention AI or agents.

## Open

- `docs/plans/README.md` still numbers this work 0.6.10 in its table and release order line; the manager replaces that with "after sign in to view".
