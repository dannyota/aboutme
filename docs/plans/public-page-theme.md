# Public page bar and color scheme (0.6.8, 0.6.9)

Status: planned. Design: [public page bar and theme](../design/public-page-theme.md). One feature per release: the page bar ships first with no contract change; the color scheme needs an ADR and a schema version, so it ships second. Mockups: `.dev/design/public-theme/` in the main checkout (ignored), rendered from production template pages by a scratch script.

|Release|Outcome|Risk|
|-|-|-|
|0.6.8 Page bar|Full-width Aurora bar with the mark, a muted credit, and a 32 px outlined Download PDF button; download-off state; not sticky|Low: markup and CSS; the validator hooks stay|
|0.6.9 Color scheme|Owner picks Light, Dark, or Match device per resume; public page and Web preview render the dark palette rule; PDF, print, and card stay light|Medium: schema version, renderer roles, ADR amending ADR 0050|

## 0.6.8 Page bar

|Role|Files|Work|
|-|-|-|
|frontend|`apps/web/app/components/public/PublicResumeApp.vue`; new `apps/web/app/components/public/PublicMark.vue`; `apps/web/app/components/resume/ResumeDocument.vue` (public page CSS block); `apps/web/test/public-render/render.test.ts`, `hydration.test.ts`; `apps/web/e2e/screenshot.spec.ts`; web source manifest if a file is added|Move the bar out of `.public-measure`, add the inner row, mark, and icon; replace the bar CSS with the spec's tokens and sizes; keep the three class names; extend the geometry test (full width, 32 px button on a fine pointer, hidden in print)|
|qa|`apps/web/e2e/baselines/public--classic-serif--2560.png`, `public--modern-sidebar--2560.png`, `public--modern-sidebar--390.png`; `deploy/dev-https-browser/public.spec.ts` if it asserts bar layout|Regenerate the three baselines in CI; confirm the public proof still finds both links|
|designer|none|Finish review of the built bar at 390 and 1440 px against the mockups|

Go needs no change: the credit anchor keeps one text child and the download href is unchanged. The reviewer confirms the validator still passes on a Vietnamese and an English page, with download on and off.

## 0.6.9 Color scheme

Order: architect first, then backend and frontend in parallel on disjoint paths, then qa.

|Role|Files|Work|
|-|-|-|
|architect|new ADR amending ADR 0050's white-sheet rule for the public page on screen; new `packages/schema/resume.v5.schema.json` and `resume.schema.json`; `packages/schema/released-versions.json`; `docs/design/data.md` (document versions); `docs/design/templates/tokens.md` (leaf table: 26 author-controlled, 11 optional); `docs/design/templates/contract.md` §2 and §3 (kept on apply); `docs/design/templates/colors.md` (pointer to the dark rule)|Add optional `customization.colorScheme` enum `light`, `dark`, `system`, absent means light; state older-client and loss rules; presets never set it; `applyTemplate` keeps it|
|backend|`packages/schema` Go output; `apps/server` document validation and MCP customization tool description; link-preview card renderer (assert it ignores the leaf); `docs/api/openapi.yaml` examples if they list customization|Accept and store the leaf; MCP can set it; tests that the card and PDF ignore it|
|frontend|`packages/schema` TS output and `apps/web/app/editor/documentValidator.generated.mjs` (regenerated); new `apps/web/app/components/resume/darkPalette.ts` and its test; `resolveRenderModel.ts`, `useResumeStyles` (dark role set as `--dark-color-*`); `ResumeDocument.vue` (screen-only scheme rules, `color-scheme`); `PublicResumeApp.vue` (`data-color-scheme`); `EditorPreview.vue` (Web mode only); `apps/web/app/components/editor/customization/fields.ts`, `labels.ts`; `apps/web/app/i18n/editor-controls.ts`; `apps/web/app/editor/commands.ts` (set path); `apps/web/app/pages/_harness/render.vue` (`scheme` query)|Dark rule as specified with a table test over all 20 presets and both columns; field first in Colors with the spec's vi and en copy; preview follows the device preference for Match device|
|qa|new baselines `public--modern-sidebar--dark--390.png`, `public--creative-accent--dark--1440.png`, `public--executive-band--dark--1440.png`, `public--classic-serif--system--1440.png` (emulated dark preference); `apps/web/e2e/screenshot.spec.ts` cells; dev-https public proof|Print media on a dark page computes the light surface; the PDF of a dark resume matches its light PDF; the preview card is unchanged|
|designer|none|Finish review of three templates in both schemes at 390 and 1440 px, and the editor field in both languages|

Unchanged baselines in both releases: every `*--paged.png` and `*--continuous.png`, `print-baselines/`, `chrome--*`, `template-*`, and all golden HTML.

## Open

- The owner named one release, 0.6.8. This plan splits it in two under the one-feature rule; the manager confirms or merges them.
- The ADR for the dark scheme needs owner approval before 0.6.9 code starts.
