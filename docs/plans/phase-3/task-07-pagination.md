# Task 7: Pagination — pure engine + editor paged mode

Satisfies **AC-REN-002** (with Task 9's goldens and Task 11's real-browser
measurement).

**Files:** create
`apps/web/app/components/resume/{paginate.ts,measure.ts, PagedResume.vue}`,
`apps/web/test/renderer/paginate.test.ts`; modify `ResumeDocument.vue`
(`mode: 'paged'` delegates to `PagedResume`).

This author writes `paginate.adversarial.test.ts` from the Task 7 section of
[adversarial coverage](adversarial-coverage.md), test-first, before the
implementation.

**Interfaces (produced):**

```ts
// paginate.ts — pure, deterministic, no DOM.
export interface BlockRef {
  sectionKey: string;
  kind: "heading" | "entry";
  entryIndex?: number; // present iff kind === 'entry'
  column: "main" | "sidebar";
}
export interface PaginationRequest {
  document: Resume;
  context: RenderContext;
  columns: 1 | 2;
  blocks: BlockRef[];
  page: ResumeStyles["page"];
}
export interface MeasuredBlock extends BlockRef {
  heightPx: number;
  // Semantic space before this block when its predecessor remains on the same
  // page/column. The first block on a page pays zero.
  gapBeforePx: number;
}
export interface PlacedBlock extends BlockRef {
  heightPx: number;
  appliedGapBeforePx: number;
  overflow: boolean;
}
export interface PlacedHeader {
  heightPx: number;
  bodyGapPx: number; // resolved --gap-section when body shares page 1
}
export interface Page {
  header?: PlacedHeader; // page 1 only
  main: PlacedBlock[];
  sidebar: PlacedBlock[];
  contentHeightPx: number;
  overflow: boolean; // page requires more than ordinary paper height
}
export type PaginationErrorCode =
  | "pagination_measurement_required"
  | "invalid_measurement"
  | "invalid_page_geometry"
  | "non_normalized_one_column_flow";
export class PaginationError extends Error {
  readonly code: PaginationErrorCode;
  constructor(code: PaginationErrorCode, message: string);
}
// Breaks at entry boundaries only; a heading never ends a page with zero
// of its entries following on the same page (pulled to the next page);
// a block taller than one page occupies its own expanded page and carries
// overflow: true in the returned PlacedBlock; all other blocks carry false.
// PagedResume renders that flag visibly. Content is never clipped. The PDF
// remains authoritative.
// Per-column pagination,
// page count = max(columns) (D7).
export function paginate(
  blocks: MeasuredBlock[],
  columns: 1 | 2,
  pageContentHeightPx: number,
  headerHeightPx: number,
  headerBodyGapPx: number,
): Page[];

// measure.ts — browser adapter plus an SSR-callable injection seam.
export interface MeasuredLayout {
  columns: 1 | 2;
  headerHeightPx: number;
  headerBodyGapPx: number;
  blocks: MeasuredBlock[];
}
export type MeasurePagination = (
  request: PaginationRequest,
) => MeasuredLayout | Promise<MeasuredLayout>;
export const PaginationMeasureKey: InjectionKey<MeasurePagination>;
// The browser adapter separately accepts (root: HTMLElement, request) after
// mount and measures the exact paged-wrapper CSS after fonts are ready.
```

`PagedResume.vue` renders `paginate()` output as paper-width page boxes with the
paper height as `min-height`. It consumes the resolved page value from Task 6:
`a4` is 794 × 1123 CSS px and `letter` is 816 × 1056 CSS px at 96 dpi. It never
uses A4 as an editor-only default after a Letter document has been selected. An
ordinary page stays at the selected width and height. A page containing an
oversized block expands to
`2 × pageMarginY + optionalHeaderHeight + optionalHeaderBodyGap + max(mainColumnHeight, sidebarColumnHeight)`;
later pages flow after that computed box and never overlap or clip it. Each page
re-renders its blocks' section/entry slices. **Page padding is not a constant.**
It comes from the resolved `marginXmm` / `marginYmm`, which `useResumeStyles`
derives from `spacing.pageMargin` and defaults to 15 mm per axis (`geometry.md`
§6; `print.md` §2 fixes the same 15 mm in `@page`). The `pageContentHeightPx`
fed to `paginate()` is therefore
`page.heightPx − 2 × page.marginYmm × 96 / 25.4`, computed per document — all 20
committed presets set `spacing.pageMargin`, so a hard-coded 48 px would
mis-paginate every one of them. The hidden pass uses the exact paged wrapper CSS
but zeroes sibling margins/gaps while measuring each rect. It then derives
`gapBeforePx` from the same resolved `--gap-section`, `--gap-heading`, and
`--gap-entry` values: section-to-section, heading-to-first-entry, or
entry-to-entry respectively. Pagination adds a gap only when both adjacent
blocks stay in the same column and page; the first block pays zero. The rendered
page uses the returned `appliedGapBeforePx`, so measurement and slicing cannot
disagree. Internal `--gap-block` spacing is already inside the entry rect.

Blockization is mode-aware before measurement. With `columns: 2`, main and
sidebar remain independent flows. With `columns: 1`, it emits every main section
followed by every sidebar section as one `main` flow, preserves the order within
each source list, leaves `Page.sidebar` empty, and renders the result full-width
with main treatment. `paginate` receives `columns` and rejects a one-column
request containing a sidebar flow, so no caller can silently drop or separately
paginate those sections.

`ResumeDocument` keeps its public `{document, context}` props. `PagedResume`
builds `PaginationRequest` from those props and the normalized block order. If
`PaginationMeasureKey` is provided, its async setup awaits that DOM-free
provider and renders the resulting pages on the first SSR pass. Vue's
`renderToString` therefore needs no element or mounted hook. Tests create a Vue
app and call `app.provide(PaginationMeasureKey, syntheticMeasure)` before
rendering. With no provider, the browser branch first renders the hidden
measurement tree, then calls the browser adapter with its root after mount.
Product code never imports a test measurer. SSR paged mode without a provider
throws `PaginationError` with `pagination_measurement_required` instead of
emitting an unpaged or empty document. Non-finite or non-positive block heights,
or non-finite or negative gaps and header measurements, throw
`invalid_measurement`. Non-finite or non-positive page dimensions, and
non-finite or negative margins, throw `invalid_page_geometry`. A one-column
request carrying a sidebar flow throws `non_normalized_one_column_flow`. No
branch throws a bare string or substitutes an empty document.

The full-width `.resume-header` is one measured atomic box on page 1. Its
height, plus `--gap-section` only when body content also remains on page 1,
reduces both columns' first-page capacity. It never repeats. If the header alone
exceeds ordinary content height, page 1 expands visibly around it and body flow
starts on page 2. The paginator also treats a section heading plus its first
entry and their heading gap as one placement group. If the pair does not fit the
remaining capacity, both move together. If the pair exceeds a fresh ordinary
page, it gets one dedicated expanded page; `Page.overflow` marks the page even
when neither child is individually oversized. The browser adapter waits for the
selected face and its catalog fallback through `document.fonts` before its first
measurement. A font-load or layout change invalidates the measurements and
performs one deterministic remeasure. The component sets
`data-pagination-settled="true"` only after that pass completes; browser tests
and screenshots wait for that marker.

- [ ] **Step 1: Failing engine tests.** Table-driven `paginate` cases: empty
      input → one empty page with its header; header height reduces both first-
      page columns; oversized header expands page 1 and starts body on page 2;
      blocks plus semantic gaps exactly filling a page → break after; a page
      break suppresses the new page's leading gap; heading-orphan pull;
      heading-plus-first-entry exact fit, move-together, and pair-only overflow;
      oversized block on one expanded, visibly marked page with its exact
      computed expanded height including same-page gaps, no overlap, and no
      hidden content; two-column independent flow with unequal page counts;
      one-column normalization of main then sidebar into one ordered full-width
      flow, with no sidebar treatment or omitted block; rejection of a
      non-normalized one-column sidebar flow; determinism (same input twice →
      deep-equal output). Table-driven `pageMetrics` cases too: A4 and Letter ×
      absent `spacing.pageMargin` (15 mm) × an explicit margin × the `0` mm
      bound → expected `pageContentHeightPx`. Run → FAIL; implement; PASS.
- [ ] **Step 2: `PagedResume` with synthetic measurer.** Component test: render
      paged mode with the committed synthetic measurer
      (`test/renderer/synthetic-measure.ts`: height = fixed base per kind +
      deterministic function of text length — committed, versioned, referenced
      by Task 9's goldens) and assert page count and block distribution for
      `fixtures/full.json`. The test app provides it with
      `PaginationMeasureKey`; no renderer prop is added. Browser-adapter tests
      hold font readiness pending and prove no measurement occurs, then resolve
      it and prove one settled measurement; a simulated font/layout change
      causes exactly one remeasure.
- [ ] **Step 2a: format-derived paper.** Render the same document in A4 and
      Letter. Assert page boxes are exactly 794 × 1123 and 816 × 1056 before
      scaling, page capacity uses the matching height and resolved margins, and
      the paged editor HTML contains no `@page` rule. Assert every invalid input
      above yields the exact `PaginationError.code`.
- [ ] **Step 3: Boundary rule evidence.** Assert no entry is ever split across
      pages (block granularity is the invariant; the master plan's "editor
      approximate" honesty note goes in the component doc comment).
- [ ] **Step 4: gate.** Run `make web-lint web-typecheck web-test web-build`.

## Acceptance mapping

- AC-REN-002: both display modes preserve section order and content; paged
  preview breaks only at entry boundaries and exposes oversized entries.
