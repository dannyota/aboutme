# Task 8: Template presets, registry, and apply

Satisfies **AC-REN-004** and follows accepted
[ADR 0008](../../adr/0008-template-apply-semantics.md). Its placement rule is
the flat `layout.placement: "keep" | "byType"` value, with `sidebarSectionTypes`
as its sibling input. `layout.sections` is a total function of the document's
content keys. The one- or two-column toggle uses a separate preserve-semantics
operation. Step 0 confirms the ADR still agrees with D10. If they differ, stop
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
verified; concurrent edits to any generator or preset path are forbidden.

Preset shape (D10 / ADR 0008): a preset carries a placement **rule**, never a
key list. See `docs/design/templates/presets/` for the rationale behind each
preset. Phase 3 cannot start until `docs/design/templates/README.md` records
owner approval. This task validates, generates types for, and wires up the
existing 20; it does not choose them.

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

// applyTemplate.ts (hand-written, pure — semantics per ADR 0008)
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
// Exactly-once holds BY CONSTRUCTION for every input. Everything else is
// replaced from the preset. Content is read, never written. The customize
// panel's 1↔2-column toggle is a DIFFERENT operation (preserve semantics),
// not an applyTemplate call.
```

- [ ] **Step 0: ADR gate.** Confirm `docs/adr/0008-template-apply-semantics.md`
      exists on the base commit and matches the semantics above; record its
      status line in the task report. Divergence → stop, report to the
      integration owner.
- [ ] **Step 1: Failing registry test** (`templates.test.ts`): `TEMPLATES`'s id
      set equals the `packages/schema/templates/` directory listing (so the test
      cannot drift from the data again — do not hard-code the id count or list);
      validate the preset rule shape separately; then remove the rule-only
      `placement` and `sidebarSectionTypes` keys, inject computed
      `layout.sections`, and validate the resulting customization against
      `resume.schema.json`'s customization `$def` via ajv; every preset's font
      family ∈ the schema enum; every `byType` list ⊆ the schema's `sectionType`
      enum with no duplicates; ids unique.
- [ ] **Step 2: Generator validation/emission for the 20 committed presets**;
      regenerate; pass. Negative generator tests: an out-of-enum font and an
      out-of-enum `sidebarSectionTypes` entry each fail generation loudly.
- [ ] **Step 2a: Contrast conformance.** Assert all 20 committed presets pass
      `colors.md` §5's contrast targets before clamping, under its normative mix
      space (gamma-encoded sRGB, `color-mix(in srgb, …)` — not linear light or
      OKLab), per `contract.md` §8's per-preset conformance requirement. Then
      derive the runtime level colors and assert every filled mark passes 3:1
      against both its surface and the actual returned track. The track itself
      has no contrast floor and may equal the surface after fallback.
- [ ] **Step 3: `applyTemplate` tests**: `keep` preserves `layout.sections`
      byte-for-byte; `byType` — property test over generated content-key sets
      (seeded, deterministic): result always satisfies exactly-once, sidebar
      holds exactly the byType-matched keys in rule order, main holds the rest
      in current visual order; empty content → two empty arrays; everything else
      replaced from the preset; inputs never mutated; output (with real content)
      validates against the schema.
- [ ] **Step 4: Gates.** Run `make schema-check` and
      `make web-lint web-typecheck web-test web-build`. Report the owned-path
      diff and exact output to the integration owner.
