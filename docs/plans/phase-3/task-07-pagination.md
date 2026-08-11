# Task 7: Pagination — pure engine + editor paged mode

Satisfies **AC-REN-002** (with Task 9's goldens and Task 11's real-browser
measurement).

**Files:** create
`apps/web/app/components/resume/{paginate.ts,measure.ts, PagedResume.vue}`,
`apps/web/test/renderer/paginate.test.ts`; modify `ResumeDocument.vue`
(`mode: 'paged'` delegates to `PagedResume`).

**Interfaces (produced):**

```ts
// paginate.ts — pure, deterministic, no DOM.
export interface BlockRef {
  sectionKey: string;
  kind: "heading" | "entry";
  entryIndex?: number; // present iff kind === 'entry'
  column: "main" | "sidebar";
}
export interface MeasuredBlock extends BlockRef {
  heightPx: number;
}
export interface Page {
  main: BlockRef[];
  sidebar: BlockRef[];
}
// Breaks at entry boundaries only; a heading never ends a page with zero
// of its entries following on the same page (pulled to the next page);
// a block taller than one page occupies its own page (overflow clipped —
// approximate by design, the PDF is authoritative). Per-column pagination,
// page count = max(columns) (D7).
export function paginate(
  blocks: MeasuredBlock[],
  pageContentHeightPx: number,
): Page[];

// measure.ts — browser-only adapter: measures rendered block heights via
// getBoundingClientRect on a hidden measurement pass. Never imported by
// paginate.ts or any test that must stay deterministic.
```

`PagedResume.vue` renders `paginate()` output as fixed-size page boxes
(`pageMetrics.ts`: A4 794×1123 / Letter 816×1056, 48 px padding — D7), each page
re-rendering its blocks' section/entry slices. It accepts an injectable
`measure` function prop (defaulting to the DOM adapter) so SSR/tests supply the
synthetic measurer.

- [ ] **Step 1: Failing engine tests.** Table-driven `paginate` cases: empty
      input → one empty page; blocks exactly filling a page → break after;
      heading-orphan pull; oversized block; two-column independent flow with
      unequal page counts; determinism (same input twice → deep-equal output).
      Run → FAIL; implement; PASS.
- [ ] **Step 2: `PagedResume` with synthetic measurer.** Component test: render
      paged mode with the committed synthetic measurer
      (`test/renderer/synthetic-measure.ts`: height = fixed base per kind +
      deterministic function of text length — committed, versioned, referenced
      by Task 9's goldens) and assert page count and block distribution for
      `fixtures/full.json`.
- [ ] **Step 3: Boundary rule evidence.** Assert no entry is ever split across
      pages (block granularity is the invariant; the master plan's "editor
      approximate" honesty note goes in the component doc comment).
- [ ] **Step 4: Gate + commit.**
