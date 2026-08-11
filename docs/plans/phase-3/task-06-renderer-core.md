# Task 6: Renderer core (continuous mode)

Satisfies **AC-REN-006** (purity), **AC-REN-007** (accessibility floor),
**AC-REN-008** (the 2026-08-11 tokens), and the NEW-M7 re-check inside
**AC-SEC-001**; structural prerequisite for AC-REN-001/002.

**Authority for everything below:** `docs/specs/templates/contract.md` §§5–7
(what renders), `docs/specs/templates/tokens.md` §§3–6 (the token vocabulary,
the derived color roles, the clamp, the point-of-use fallbacks), and
`docs/specs/templates/print.md` §§3–7 (the DOM class names and the
`print-color-adjust` requirement this task must satisfy for P7A). Those three
files are later and more specific than `aboutme-design.md` §5; where they
disagree with the prose in this plan, they win, and the disagreement is a plan
bug to report.

**Files:** create the renderer tree per the file-structure table
(`ResumeDocument.vue`, `ResumeHeader.vue`, `LayoutColumns.vue`,
`SectionRenderer.vue`, `sections/*.vue` ×8, `primitives/*` ×7,
`useResumeStyles.ts`, `clampContrast.ts`, `icons.ts`, `formatDate.ts`,
`pageMetrics.ts`);
`apps/web/test/renderer/{styles,clamp,chips,icons,photo,dates,sections}.test.ts`.
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
  lng: string; // resumes.lng — emitted as lang= on the resume root
  mode: "continuous" | "paged"; // Task 6 implements continuous; Task 7 paged
  assetBase?: string; // default '/assets/' (D14)
}

// clampContrast.ts — pure, its own tested module (tokens.md §5). OKLCH,
// hue- and chroma-preserving, L stepped by 0.005 toward the direction the
// surface's relative luminance selects, terminating at #000000 on a light
// surface or #ffffff on a dark one. Same inputs, same output, in preview,
// SSR, and print.
export function clampContrast(
  color: string,
  surface: string,
  target: number,
): string;

// useResumeStyles.ts — pure: Customization → the CSS custom properties of
// tokens.md §§3–6, in that document's UNPREFIXED vocabulary (--color-*,
// --fs-*, --lh-*, --header-align, --gap-*, --page-margin-*, --rule-*,
// --sidebar-ratio, --column-gutter, --photo-size, --bar-height, --dot-size,
// --tag-padding, --icon-size). There is no --r-* prefix: print.md's break
// and repaint rules are written against these exact names.
//
// The result is NOT one flat root map. Every clamped color role is resolved
// once per surface, against the surface the element actually paints on
// (tokens.md §4.2) — code must not hoist a clamped role to the document
// root. `header` and `sidebar` are emitted only when
// effectiveSurfaceTarget() selects that region.
export interface ResumeStyles {
  root: Record<string, string>; // page surface: --color-surface
  header?: Record<string, string>; // header-band scope, when tinted
  sidebar?: Record<string, string>; // sidebar-column scope, when tinted
  pageMargin: { x: string; y: string }; // feeds the @page rule below
}
export function useResumeStyles(c: Customization): ResumeStyles;
```

## Rendering rules pinned here (all golden-visible)

**Structure and order.** Sections render in `layout.sections` order —
`columns: 1` renders `main` then `sidebar` in order (contract §7: nothing
silently unrendered). `SectionRenderer` dispatches on `sectionType` with an
exhaustive switch (compile-time `never` check, mirroring the existing
schema-contract pattern). A key in `layout.sections` with no `content` entry
renders nothing and must not crash (contract §4).

**Entry slots.** `contract.md` §5.2's mapping table is the authority — this plan
does not restate it. Implement it verbatim, including the asymmetries it names:
`custom` has `city` but no `country`; `project` has neither; `certificate`
carries a single `{y,m?}` and never a range; `language` has no body;
`employerLink`/`schoolLink`/`titleLink` wrap specific slots.

**Absence and emptiness.** `contract.md` §6's table is the authority, adopted
verbatim (this supersedes D18's earlier "heading only" wording): a section with
no entries, or whose every entry is hidden, is **omitted entirely — heading,
rule, and section gap included**. A section whose `displayName` is absent or
`""` renders no heading text and no substitute (contract §5.3); its icon and
rule still render. Hidden entries and details are absent from the DOM, never
`display: none`.

**Dates.** `DateRange` formats via `formatDate.ts` (D11 fixed table,
`dateFormat` variants `MM/YYYY`, `Mon YYYY`, `YYYY`; `present` renders the fixed
string `Present`; the range separator is U+2013 EN DASH per contract §5.4).

**Contacts.** Chips render in `details[]` array order (ADR 0013). Only the four
https-validated types linkify, and the renderer re-checks the `https://` prefix
itself (NEW-M7); `email`/`phone`/`location`/`custom` are plain text in v1.
`label`, when present and non-empty, replaces the type's default label (contract
§5.1, ADR 0013) — this ships in P3, not later.

**Links.** `text-decoration: underline` on every inline link — contact anchors,
entry link slots, and anchors inside rich text — renderer-fixed, identical in
every template, unsettable by any preset (`tokens.md` §6; contract §9.8 resolved
2026-08-11). `--color-link` is the color role and nothing else.

**Rich text.** `RichText` calls `sanitizeRichText` (Task 3) on every client
render; on SSR the string passes through, because Go is the sole SSR
sanitization authority (ADR 0012).

**Headings.** `heading.style` drives `text-transform` through a CSS var **and**
`letter-spacing`: `0.06em` for `uppercase`, `0` for `titlecase` and `normal`
(`tokens.md` §3.3). Because `text-transform` is locale-sensitive in Chromium,
the resume root carries an explicit `lang` taken from the `lng` prop; without it
the print container's locale can change casing and break snapshot determinism
(`print.md` §7). `showRule` is a bottom border whose `--rule-gap` disappears
with it.

**The seven optional tokens** (`tokens.md` §2 — all 20 committed presets set
`header` and `spacing.pageMargin`, four set `surfaceTarget: "header"`, three set
`"sidebar"`). Each fallback is applied at the point of use and never written
back into the document:

- `header.align` absent → `left`; `header.detailsLayout` absent → `inline`;
  `header.iconStyle` absent → `outline` (`tokens.md` §3.4). The enum is `none` |
  `outline`; `solid` was dropped 2026-08-11.
- `spacing.pageMargin` absent → `15mm` on both axes (`tokens.md` §6).
- `colors.surface` + `layout.surfaceTarget` resolve through
  `effectiveSurfaceTarget()` (`tokens.md` §4.1), including both silent
  degradations to `none`: no `colors.surface`, and `sidebar` at
  `layout.columns: 1`. Both stored states are legal and must render, never
  reject.

**Color roles.** Every color reaching the page is a derived role from
`tokens.md` §4 — mix in gamma-encoded sRGB (`color-mix(in srgb, …)`, never
linear light or OKLab), then clamp. `--color-track` is the one role with **no**
contrast floor and cannot have one (capped at 1.61:1 on white even for a black
accent), so a level widget must remain correct when the track is invisible
(`tokens.md` §4 note) — do not "fix" the track by clamping it.

**Level widgets.** `skill`/`language` `sectionDisplay.style` variants
`text`/`tag`/`bar`/`dots` are each a distinct DOM shape. Level **absent** → name
only, never a zero-width bar; level **`0`** → an explicit value rendering zero
of five filled; `0` and absent must render differently (contract §5.6). The
`text` style renders **no widget at all**.

**DOM class contract (P7A depends on it).** The renderer emits exactly
`.resume-header`, `.resume-section`, `.section-heading`, `.section-lead`,
`.entry`, `.entry-header`, `.entry-body` — `print.md` §3's break rules are
written against these names and silently stop working if they drift.
`.section-lead` is a wrapper the renderer emits around the heading **and the
first entry's header**, which is what makes that pairing unbreakable (`print.md`
§3).

**Print-fidelity properties the renderer owns.** `print-color-adjust: exact`
(with the `-webkit-` prefix) goes on the resume root — without it level bars,
tag chips, and every surface tint vanish from the PDF while remaining in preview
(`print.md` §6). `useResumeStyles` also emits
`margin: var(--page-margin-y) var(--page-margin-x)` into the `@page` rule
(`tokens.md` §6); P7A consumes it and does not recompute it.

Photo per D14; icons per D13.

## Steps

- [ ] **Step 1: Failing purity + smoke test.** `sections.test.ts` opens with a
      file-level `// @vitest-environment node` pragma (B7 — the global
      `environment: 'nuxt'` happy-dom would otherwise let a stray `document.*`
      call in the renderer tree silently succeed, defeating this test's
      purpose). Render `ResumeDocument` with `fixtures/minimal.json` via
      **plain** `renderToString(createSSRApp(...))` — no Nuxt, no
      `mountSuspended`. Assert non-empty HTML containing the fixture's
      `fullName`. Run → FAIL.
- [ ] **Step 2: Build bottom-up with TDD per module** (each module: failing unit
      test → implement → pass): `clampContrast` (table: color × surface × target
      → expected hex, including the black/white termination case and a
      near-black surface under dark text), `useResumeStyles` (table:
      customization → expected root map, plus the per-surface maps for a
      `header`-tinted and a `sidebar`-tinted preset, plus each of the seven
      optional tokens absent → its fallback value, plus both
      `effectiveSurfaceTarget` degradations), `formatDate` (all three formats ×
      y-only/y+m × present/closed ranges), `icons` (known key → component,
      unknown → null), `ContactChip` (D12/ADR 0013 matrix: four URL types
      linkify **only** with `https://`-prefixed values — a
      `javascript:`/`//`/`mailto:` value in a URL-typed chip renders as text,
      direct NEW-M7 evidence; email/phone/location/custom always text; `label`
      present and non-empty replaces the default label; every anchor is
      underlined), `Photo` (crop math from a fixed rect → expected style
      bindings; assetBase composition), `DateRange`, `EntryHeader`,
      `SectionHeading`, then the eight sections (contract §5.2's mapping and
      §5.6's absent-versus-`0` widget cases), then `LayoutColumns` (2-col
      placement; 1-col main-then-sidebar order; sidebar tint degrading to `none`
      at `columns: 1`), then `ResumeHeader`, then `ResumeDocument` (continuous
      mode only; `paged` throws `not implemented` until Task 7).
- [ ] **Step 3: Draft-permissiveness rendering tests** (contract §6, superseding
      D18): render `fixtures/draft-cleared-name-empty-section.json` and
      `draft-partial.json` — no placeholder text, no crash, **empty section
      omitted entirely including its heading, rule, and gap**, section with an
      absent/`""` `displayName` renders no heading text and no substitute,
      hidden entries and hidden details absent from the output, separators
      emitted only between two present values.
- [ ] **Step 4: Class-contract and print-property assertions.** Assert the
      rendered HTML carries the seven `print.md` §3 class names, that
      `.section-lead` wraps the heading together with the first entry's header,
      that the resume root carries `lang` from the `lng` prop and
      `print-color-adjust: exact`, and that the emitted `@page` margin resolves
      from `--page-margin-x/y`. These are P7A's inputs; a rename here is a
      silent P7A regression.
- [ ] **Step 5: Gate + commit.** Full web gate; `make ci` before handoff (ADR
      0011 gate of record). Commit renderer + test paths.
