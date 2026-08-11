# Task 6: Renderer core (continuous mode)

Satisfies **AC-REN-006** (purity) and the NEW-M7 re-check inside **AC-SEC-001**;
structural prerequisite for AC-REN-001/002.

**Files:** create the renderer tree per the file-structure table
(`ResumeDocument.vue`, `ResumeHeader.vue`, `LayoutColumns.vue`,
`SectionRenderer.vue`, `sections/*.vue` ×8, `primitives/*` ×7,
`useResumeStyles.ts`, `icons.ts`, `formatDate.ts`, `pageMetrics.ts`);
`apps/web/test/renderer/{styles,chips,icons,photo,dates,sections}.test.ts`.
`lucide-vue-next` is already installed by Task 0 (B8); this task does not touch
`package.json`.

**Interfaces (produced):**

```ts
// ResumeDocument.vue props — the renderer contract (spec §5). Types come
// from @aboutme/schema; the renderer never redefines the document shape.
// Callers MUST pass an already-projected, current-schema_version document —
// the renderer performs no migration and exposes no schemaVersion prop (D21;
// spec §5's "handles current schema_version only" guard is satisfied by the
// server's migrate-on-read projection, not by a renderer-side check).
interface Props {
  personalDetails: PersonalDetails;
  content: Content;
  customization: Customization;
  mode: "continuous" | "paged"; // Task 6 implements continuous; Task 7 paged
  assetBase?: string; // default '/assets/' (D14)
}

// useResumeStyles.ts — pure: Customization → Record<'--r-*', string>
// (font family/size, colors, spacing, heading style, line height). All
// styling flows through these CSS custom properties; components consume
// var(--r-*), never customization directly (keeps golden diffs local).
export function useResumeStyles(c: Customization): Record<string, string>;
```

Rendering rules pinned here (all golden-visible): sections render in
`layout.sections` order — `columns: 1` renders `main` then `sidebar` in order
(spec's one-column decision: nothing silently unrendered); `SectionRenderer`
dispatches on `sectionType` with an exhaustive switch (compile-time `never`
check, mirroring the existing schema-contract pattern); `DateRange` formats via
`formatDate.ts` (D11 fixed table, `dateFormat` variants `MM/YYYY`, `Mon YYYY`,
`YYYY`; `present` renders the fixed string `Present` — flagged with D11);
`RichText` calls `sanitizeRichText` (Task 3) on every render; chips per D12;
photo per D14; icons per D13; hidden entries per D18; heading style
(`uppercase`/`titlecase`/`normal`) implemented via CSS transform driven by a CSS
var, `showRule` a bottom border; `skill`/`language` `sectionDisplay.style`
variants `text`/`tag`/`bar`/`dots` each a distinct DOM shape (level absent →
name only, never a zero-width bar — absence is meaningful).

- [ ] **Step 1: Failing purity + smoke test.** `sections.test.ts` opens with a
      file-level `// @vitest-environment node` pragma (B7 — the global
      `environment: 'nuxt'` happy-dom would otherwise let a stray `document.*`
      call in the renderer tree silently succeed, defeating this test's
      purpose). Render `ResumeDocument` with `fixtures/minimal.json` via
      **plain** `renderToString(createSSRApp(...))` — no Nuxt, no
      `mountSuspended`. Assert non-empty HTML containing the fixture's
      `fullName`. Run → FAIL.
- [ ] **Step 2: Build bottom-up with TDD per module** (each module: failing unit
      test → implement → pass): `useResumeStyles` (table: customization →
      expected var map), `formatDate` (all three formats × y-only/y+m ×
      present/closed ranges), `icons` (known key → component, unknown → null),
      `ContactChip` (D12 matrix: four URL types linkify **only** with
      `https://`-prefixed values — a `javascript:`/`//`/`mailto:` value in a
      URL-typed chip renders as text, direct NEW-M7 evidence; email/phone/
      location/custom always text), `Photo` (crop math from a fixed rect →
      expected style bindings; assetBase composition), `DateRange`,
      `EntryHeader`, `SectionHeading`, then the eight sections, then
      `LayoutColumns` (2-col placement; 1-col main-then-sidebar order), then
      `ResumeHeader`, then `ResumeDocument` (continuous mode only; `paged`
      throws `not implemented` until Task 7).
- [ ] **Step 3: Draft-permissiveness rendering tests** (D18): render
      `fixtures/draft-cleared-name-empty-section.json` and `draft-partial.json`
      — no placeholder text, no crash, empty section renders heading only,
      hidden entries absent from output.
- [ ] **Step 4: Gate + commit.** Full web gate. Commit renderer + test paths.
