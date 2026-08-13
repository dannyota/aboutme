# Task 6: Renderer core (continuous mode)

Satisfies **AC-REN-006** (purity), **AC-REN-007** (accessibility floor),
**AC-REN-008** (the 2026-08-11 tokens), and the NEW-M7 re-check inside
**AC-SEC-004**; structural prerequisite for AC-REN-001/002.

**Authority for everything below:** `docs/design/web.md` and
`docs/design/templates/`: `contract.md` §§5–7, `tokens.md` §3, `colors.md`
§§4–5, `geometry.md` §§6–7, and `print.md` §§3–7. Where this plan disagrees with
those files, stop and correct the plan before implementation.

**Files:** create the renderer tree per the file-structure table
(`ResumeDocument.vue`, `ResumeHeader.vue`, `LayoutColumns.vue`,
`SectionRenderer.vue`, `sections/*.vue` ×8, `primitives/*` ×7,
`resolveRenderModel.ts`, `useResumeStyles.ts`, `clampContrast.ts`, `icons.ts`,
`formatDate.ts`, `pageMetrics.ts`);
`apps/web/test/renderer/{styles,clamp,chips,icons,photo,dates,sections}.test.ts`.
`@lucide/vue` is already installed by Task 0 (B8); this task does not touch
`package.json`.

This author also writes `bounds.adversarial.test.ts` and
`plain-fields.adversarial.test.ts` from the Task 6 sections of
[adversarial coverage](adversarial-coverage.md), test-first, before the
implementation. Pagination cases belong to Task 7.

**Interfaces (produced):**

```ts
import { CURRENT_VERSION } from "@aboutme/schema/released";

// ResumeDocument.vue props — the renderer contract. Resume and the current
// version constant come from generated schema exports; the renderer never
// redefines or migrates the document shape.
interface RenderContext {
  lng: string; // resumes.lng — emitted as lang= on the resume root
  mode: "continuous" | "paged"; // Task 6 implements continuous; Task 7 paged
  // Required exactly when document.personalDetails.photo is present. The
  // controller authorizes and creates this URL; the renderer never uses key.
  photoUrl?: string;
}
interface Props {
  document: Resume;
  context: RenderContext;
}

export type ResumeRenderErrorCode =
  | "unsupported_schema_version"
  | "photo_url_required"
  | "unexpected_photo_url"
  | "render_mode_unavailable";
export class ResumeRenderError extends Error {
  readonly code: ResumeRenderErrorCode;
  constructor(code: ResumeRenderErrorCode, message: string);
}

// ResumeDocument compares document.schemaVersion with CURRENT_VERSION before
// rendering. A version or photo/context mismatch throws ResumeRenderError with
// the exact code above. Task 6 throws render_mode_unavailable for `paged`;
// Task 7 replaces that branch with PagedResume.

// resolveRenderModel.ts — the single pure boundary from raw document and
// render context to renderer input. Ordered sections, columns, header behavior,
// sectionDisplay, dateFormat, pageFormat, language, mode, and authorized photo
// state remain typed structural fields. Child components receive only their
// model slice and never read raw Customization.
export function resolveRenderModel(
  document: Resume,
  context: RenderContext,
): ResolvedRenderModel;

// clampContrast.ts — pure, its own tested module (colors.md §5). It scores
// black and white by their minimum contrast over every required surface,
// searches OKLCH L toward the better endpoint, and returns null when even that
// endpoint cannot satisfy all surfaces. Same inputs, same output, in preview,
// SSR, and print.
export function clampAgainst(
  color: string,
  surfaces: readonly string[],
  target: number,
): string | null;

// The level fill must pass 3:1 against both its actual track and surface. If
// that pair is unsatisfiable, the track falls back to the surface and the fill
// uses the single-surface clamp.
export function deriveLevelColors(
  accent: string,
  surface: string,
): { solid: string; track: string };

// useResumeStyles.ts — pure: the resolved model's CSS-valued token projection
// → the CSS custom properties of tokens.md §§3–6, in that document's
// UNPREFIXED vocabulary (--color-*,
// --fs-*, --lh-*, --header-align, --gap-*, --page-margin-*, --rule-*,
// --sidebar-ratio, --column-gutter, --photo-size, --bar-height, --dot-size,
// --tag-padding, --icon-size). There is no --r-* prefix: print.md's break
// and repaint rules are written against these exact names.
//
// The result is NOT one flat root map. Every clamped color role is resolved
// once per surface, against the surface the element actually paints on
// (colors.md §4.2) — code must not hoist a clamped role to the document
// root. `header` and `sidebar` are emitted only when
// effectiveSurfaceTarget() selects that region.
export type ResolvedPageGeometry = {
  marginXmm: number;
  marginYmm: number;
} & (
  | { format: "a4"; widthPx: 794; heightPx: 1123 }
  | { format: "letter"; widthPx: 816; heightPx: 1056 }
);
export interface ResumeStyles {
  root: Record<string, string>; // page surface: --color-surface
  header?: Record<string, string>; // header-band scope, when tinted
  sidebar?: Record<string, string>; // sidebar-column scope, when tinted
  page: ResolvedPageGeometry;
}
export function useResumeStyles(tokens: ResumeStyleTokens): ResumeStyles;
export function renderPageRule(page: ResumeStyles["page"]): string;
```

`renderPageRule` emits the complete print-only rule from `print.md` §2: A4 uses
`size: 210mm 297mm`, Letter uses `size: 8.5in 11in`, and margin order is `y x`.
It never inserts the rule itself. P7 inserts it only on the print route; public
continuous HTML and editor paged mode do not carry `@page`.

The resume root sets `font-synthesis: none`. A browser-facing style test checks
the computed root value so a missing weight or style cannot be synthesized.

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
every template, unsettable by any preset (`geometry.md` §6; `limitations.md`
§9.8). `--color-link` is the color role and nothing else.

**Rich text.** `RichText` emits one `.rich-text` container and calls
`sanitizeRichText` (Task 3) on every client render. On SSR the string passes
through, because Go is the sole SSR sanitization authority (ADR 0012). An SSR
test uses only the committed Go-sanitized artifact. Raw hostile corpus bytes are
never passed to the SSR component as a shortcut for a client-sanitizer test.

**Headings.** `heading.style` drives `text-transform` through a CSS var **and**
`letter-spacing`: `0.06em` for `uppercase`, `0` for `titlecase` and `normal`
(`tokens.md` §3.3). Because `text-transform` is locale-sensitive in Chromium,
the resume root carries an explicit `lang` taken from `context.lng`; without it
the print container's locale can change casing and break snapshot determinism
(`print.md` §7). `showRule` is a bottom border whose `--rule-gap` disappears
with it.

**The eight optional leaves** (`tokens.md` §2 — all 20 committed presets set
`header` and `spacing.pageMargin`, four set `surfaceTarget: "header"`, three set
`"sidebar"`). Each fallback is applied at the point of use and never written
back into the document:

- `colors.accent` absent → `colors.primary`.
- `header.align` absent → `left`; `header.detailsLayout` absent → `inline`;
  `header.iconStyle` absent → `outline` (`tokens.md` §3.4). The enum is `none` |
  `outline`; `solid` was dropped 2026-08-11.
- `spacing.pageMargin` absent → `15mm` on both axes (`geometry.md` §6).
- `colors.surface` + `layout.surfaceTarget` resolve through
  `effectiveSurfaceTarget()` (`colors.md` §4.1), including both silent
  degradations to `none`: no `colors.surface`, and `sidebar` at
  `layout.columns: 1`. Both stored states are legal and must render, never
  reject.

**Color roles.** Every color reaching the page is a derived role from
`colors.md` §4 — mix in gamma-encoded sRGB (`color-mix(in srgb, …)`, never
linear light or OKLab), then clamp. `--color-track` is the one role with **no**
contrast floor and cannot have one (capped at 1.61:1 on white even for a black
accent), so a level widget must remain correct when the track is invisible
(`colors.md` §4 note) — do not "fix" the track by clamping it.

**Level widgets.** `skill`/`language` `sectionDisplay.style` variants
`text`/`tag`/`bar`/`dots` are each a distinct DOM shape. Level **absent** → name
only, never a zero-width bar; level **`0`** → an explicit value rendering zero
of five filled; `0` and absent must render differently (contract §5.6). Every
present non-text widget has `role="img"` and exact accessible name
`<entry name>: <n> of 5`; absent emits neither widget nor accessible name. This
remains deterministic when the track falls back to the surface. The `text` style
renders **no widget at all**.

**DOM class contract (P7A depends on it).** The renderer emits exactly
`.resume-header`, `.resume-section`, `.section-heading`, `.entry`,
`.entry-header`, `.entry-body` — `print.md` §3's break rules are written against
these names and silently stop working if they drift. Heading and entry remain
sibling blocks; chained `break-after: avoid` rules provide the print guarantee
without an overlapping wrapper.

**Print-fidelity properties the renderer owns.** `print-color-adjust: exact`
(with the `-webkit-` prefix) goes on the resume root — without it level bars,
tag chips, and every surface tint vanish from the PDF while remaining in preview
(`print.md` §6). `useResumeStyles` resolves format-derived page dimensions and
validated point-of-use margins. `renderPageRule` emits their exact dynamic
`@page` rule; P7A consumes it and does not recompute it.

Photo per D14; icons per D13.

## Steps

- [x] **Step 0: approval and registry gate.** Record the dated Draft v4 and
      template-contract owner approval. Verify Task 5B's generated
      `@aboutme/schema/released` export resolves `CURRENT_VERSION` and that the
      two Task 4 renderer files predate this implementation diff. Stop on any
      mismatch.
- [x] **Step 1: Failing purity + smoke test.** `sections.test.ts` opens with a
      file-level `// @vitest-environment node` pragma (B7 — the global
      `environment: 'nuxt'` happy-dom would otherwise let a stray `document.*`
      call in the renderer tree silently succeed, defeating this test's
      purpose). Render `ResumeDocument` with `fixtures/minimal.json` via
      **plain** `renderToString(createSSRApp(...))` — no Nuxt, no
      `mountSuspended`. Assert non-empty HTML containing the fixture's
      `fullName`. Run → FAIL.
- [x] **Step 2: Build bottom-up with TDD per module** (each module: failing unit
      test → implement → pass): `clampAgainst` and `deriveLevelColors` (table:
      color × required surfaces × target → expected result; input that already
      passes remains unchanged; black and white are scored by their actual
      minimum contrast; the first hue-preserving passing step wins; no passing
      endpoint returns `null`; returned endpoints themselves pass),
      `resolveRenderModel` (raw document/context → ordered structural model and
      CSS token projection, input immutability, and no raw customization
      retained; every exact `ResumeRenderError` code), `useResumeStyles`
      (resolved projection → expected root map and format-derived page value,
      plus the per-surface maps for a `header`-tinted and a `sidebar`-tinted
      preset, plus each of the eight optional tokens absent → its fallback
      value, plus both `effectiveSurfaceTarget` degradations), `formatDate` (all
      three formats × y-only/y+m × present/closed ranges), `icons` (known key →
      component, unknown → null), `ContactChip` (D12/ADR 0013 matrix: four URL
      types linkify **only** with `https://`-prefixed values — a
      `javascript:`/`//`/`mailto:` value in a URL-typed chip renders as text,
      direct NEW-M7 evidence; email/phone/location/custom always text; `label`
      present and non-empty replaces the default label; every anchor is
      underlined), `Photo` (crop math from a fixed rect → expected style
      bindings; explicit `context.photoUrl` use and metadata/context mismatch),
      `DateRange`, `EntryHeader`, `SectionHeading`, then the eight sections
      (contract §5.2's mapping and §5.6's absent-versus-`0` DOM and accessible-
      name widget cases), then `LayoutColumns` (2-col placement; 1-col
      main-then-sidebar order; sidebar tint degrading to `none` at
      `columns: 1`), then `ResumeHeader`, then `ResumeDocument` (continuous mode
      only; `paged` throws
      `new ResumeRenderError("render_mode_unavailable", ...)` until Task 7).
- [x] **Step 2a: Contrast regressions.** Include the old direction heuristic's
      `clampAgainst("#b7b7b7", ["#b7b7b7"], 4.5)` mid-surface counterexample:
      the algorithm must select the actually passing black direction, not a
      `relativeLuminance >= 0.5` branch. Include accent `#959595` on white,
      whose 80%-toward-surface track is `#eaeaea`; the final fill must pass 3:1
      against both `#ffffff` and its actual returned track. Add an unsatisfiable
      multi-surface case that returns `null`. Exercise the level helper's
      failure branch and prove it replaces the derived track with the surface
      before the single-surface clamp. For every preset surface, assert each
      filled bar and dot passes 3:1 against both the surface and actual track.
- [x] **Step 3: Draft-permissiveness rendering tests** (contract §6, superseding
      D18): render `fixtures/draft-cleared-name-empty-section.json` and
      `draft-partial.json` — no placeholder text, no crash, **empty section
      omitted entirely including its heading, rule, and gap**, section with an
      absent/`""` `displayName` renders no heading text and no substitute,
      hidden entries and hidden details absent from the output, separators
      emitted only between two present values.
- [x] **Step 4: Class-contract and print-property assertions.** Assert the
      rendered HTML carries the six `print.md` §3 class names, that heading and
      first entry are non-overlapping siblings with both avoid rules, that the
      resume root carries `lang` from `context.lng` and
      `print-color-adjust: exact`. Assert exact A4 and Letter `renderPageRule`
      output, including explicit and default margins, and prove continuous HTML
      does not insert it. Task 7 repeats the absence check for editor-paged
      HTML. These are P7A's inputs; a rename here is a silent P7A regression.
- [x] **Step 5: Renderer-only gate.** Before the pagination adversarial file
      exists, run
      `(cd apps/web && npx vitest run test/renderer/bounds.adversarial.test.ts test/renderer/plain-fields.adversarial.test.ts)`,
      then `make web-lint web-typecheck web-test web-build`. Record the
      unchanged two-file test diff and this task's separate implementation diff.
      The integration owner runs `make ci` once at the final unchanged phase
      candidate, not after this task.

## Acceptance mapping

- AC-SEC-004: the renderer rechecks URL-contact `https://` values and never
  linkifies an unknown or non-URL contact type.
- AC-REN-006/007/008: the generated current-version guard, pure resolved model,
  typed failures, dynamic page geometry, and token/accessibility rules pass in
  the DOM-free renderer suite.
