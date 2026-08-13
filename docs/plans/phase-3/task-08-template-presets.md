# Task 8: Template presets, registry, and apply

Satisfies **AC-REN-004** and follows accepted
[ADR 0008](../../adr/0008-template-apply-semantics.md) plus the exact ordering
and validation rules in [ADR 0021](../../adr/0021-template-placement-order.md).
Its placement rule is the flat `layout.placement: "keep" | "byType"` value, with
`sidebarSectionTypes` as its sibling input. `layout.sections` is a total
function of validated current placement and the document's content keys. The
one- or two-column toggle uses a separate preserve-semantics operation. Step 0
requires both ADRs to be accepted and consistent with D10. If they differ, stop
and correct the plan before implementation.

**Files:** validate and wire in the 20 existing presets in
`packages/schema/templates/` and `packages/schema/test/templates.test.ts`;
modify `packages/schema/scripts/generate.mjs` (validate presets — shape,
placement rule, fonts — generation **fails** on an invalid preset — and emit
`packages/schema/gen/ts/templates.ts`); modify `packages/schema/package.json`
(`./templates` export); create
`apps/web/app/components/resume/applyTemplate.ts` +
`apps/web/test/renderer/apply-template.test.ts`.

This is the final exclusive generator window. Task 1 and Task 5B must already be
verified. This author writes `apply-template.adversarial.test.ts` from the Task
8 section of [adversarial coverage](adversarial-coverage.md), test-first, before
the implementation. Concurrent edits to any generator or preset path are
forbidden.

Preset shape (D10 / ADR 0008): a preset carries a placement **rule**, never a
key list. See `docs/design/templates/presets/` for the rationale behind each
preset. This task validates, generates types for, and wires up the existing 20;
it does not choose them.

```ts
// gen/ts/templates.ts (generated) — shape matches the 20 committed preset
// JSONs byte-for-byte (e.g. packages/schema/templates/modern-sidebar.json):
// flat placement string, sidebarSectionTypes as a sibling key, surfaceTarget
// inside layout.
export interface TemplatePreset {
  id: string;
  name: string;
  description: string;
  customization: Omit<Customization, "layout"> & {
    layout: {
      columns: 1 | 2;
      placement: "keep" | "byType";
      sidebarSectionTypes?: readonly SectionType[]; // present iff placement === "byType"
      // Derive this union from the schema enum during generation so preset
      // validation and the emitted type cannot drift.
      surfaceTarget?: "none" | "header" | "sidebar"; // see tokens.md §"effectiveSurfaceTarget"
    };
  };
}
export const TEMPLATES: readonly TemplatePreset[];

// applyTemplate.ts (hand-written, pure — semantics per ADRs 0008 and 0021)
export type TemplateApplyErrorCode =
  "invalid_current_placement" | "invalid_preset_placement";
export class TemplateApplyError extends Error {
  readonly code: TemplateApplyErrorCode;
  constructor(code: TemplateApplyErrorCode, message: string);
}
export function applyTemplate(
  current: Customization,
  preset: TemplatePreset,
  content: Content,
): Customization;
// Computes layout.sections as a TOTAL function of content's actual keys:
//   'keep'   → current placement preserved verbatim;
//   'byType' → sidebar = content keys whose sectionType is in
//              sidebarSectionTypes (ordered by that list, then by current
//              visual order within a type); main = every remaining key in
//              current visual order (main then sidebar).
// Before either rule, current arrays must contain every content key exactly
// once and no unknown key. Duplicate selectors, a custom selector, or a
// selector on "keep" throws TemplateApplyError. For valid inputs, exactly-once
// holds BY CONSTRUCTION. Everything else is replaced from the preset. Content
// is read, never written. The customize panel's 1↔2-column toggle is a
// DIFFERENT operation (preserve semantics), not an applyTemplate call.
```

- [x] **Step 0: ADR gate.** Confirm ADRs 0008 and 0021 both exist on the base
      commit, are accepted, and match the semantics above; record both status
      lines in the task report. Divergence → stop, report to the integration
      owner.
- [x] **Step 1: Failing registry test** (`templates.test.ts`): `TEMPLATES`'s id
      set equals the `packages/schema/templates/` directory listing (so the test
      cannot drift from the data again — do not hard-code the id count or list);
      validate the preset rule shape separately; then remove the rule-only
      `placement` and `sidebarSectionTypes` keys, inject computed
      `layout.sections`, and validate the resulting customization against
      `resume.schema.json`'s customization `$def` via ajv; every preset's font
      family ∈ the schema enum; every `byType` list ⊆ the schema's `sectionType`
      enum with no duplicates; ids unique.
- [x] **Step 2: Generator validation/emission for the 20 committed presets**;
      regenerate; pass. Negative generator tests: an out-of-enum font, an
      out-of-enum `sidebarSectionTypes` entry, a duplicate selector, `custom`,
      and a selector present on `keep` each fail generation loudly.
- [x] **Step 2a: Contrast conformance.** Assert all 20 committed presets' page
      palettes pass `colors.md` §5's contrast targets before clamping, under its
      normative mix space (gamma-encoded sRGB, `color-mix(in srgb, …)` — not
      linear light or OKLab). For a tinted region, assert the per-surface
      runtime clamp produces passing roles; this clamp is intentionally
      load-bearing for `executive-band`, as its approved preset rationale
      states. Derive runtime level colors on every actual surface and assert
      every filled mark passes 3:1 against both its surface and the actual
      returned track. The track itself has no contrast floor and may equal the
      surface after fallback.
- [x] **Step 3: `applyTemplate` tests**: `keep` preserves `layout.sections`
      byte-for-byte after exact-once validation; `byType` — property test over
      generated content-key sets (seeded, deterministic): current visual order
      is main then sidebar, sidebar holds matched keys by selector rank then
      current position, and main holds unselected and custom keys in current
      order. Empty content → two empty arrays. Missing, duplicate, or extra
      current-placement keys and invalid selector lists throw the exact typed
      error. Everything else is replaced from the preset; inputs never mutate;
      output with real content validates against the schema.
- [x] **Step 4: Gates.** Run `make schema-check` and
      `(cd apps/web && npx vitest run test/renderer/apply-template.adversarial.test.ts)`,
      then `make web-lint web-typecheck web-test web-build`. Report the
      owned-path diff and exact output to the integration owner.
