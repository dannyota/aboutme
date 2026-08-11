# Task 8: Template presets + registry + apply — ADR 0008 gate satisfied

Satisfies **AC-REN-004**. This task was previously **hard-blocked on
`docs/adr/0008-template-apply-semantics.md`** (owner-authored — resolves the
frozen-spec §5 "full customization replace" vs §3 exactly-once conflict). **That
block is now satisfied: the ADR is Accepted and committed at this base**, and
its placement-rule semantics (`"keep"` /
`{"byType": {"sidebarSectionTypes": [...]}}`, `layout.sections` a total function
of the document's content keys, exactly-once by construction, the 1↔2-column
toggle as a separate preserve-semantics operation) match D10 verbatim — verified
during this audit, not merely asserted. Step 0 below still re-confirms this at
execution time (the base commit could move between this audit and execution); if
the landed ADR ever diverges from the D10 summary below, **stop and report** —
the ADR wins, this plan is corrected, no improvisation.

**Files:** wire in the 20 committed presets in `packages/schema/templates/`
(already authored by P3-design — this task authors none of them),
`packages/schema/test/templates.test.ts`; modify `generate.mjs` (validate
presets — shape, placement rule, fonts — generation **fails** on an invalid
preset — and emit `gen/ts/templates.ts`); modify `packages/schema/package.json`
(`./templates` export); create
`apps/web/app/components/resume/applyTemplate.ts` +
`apps/web/test/renderer/apply-template.test.ts`.

Preset shape (D10 / ADR 0008): a preset carries a placement **rule**, never a
key list. 20 presets are already committed in `packages/schema/templates/`
(P3-design, sign-off already obtained) — see `docs/specs/templates/presets/` for
their rationale docs. This task validates, generates types for, and wires up the
existing 20; it does not author or choose any of them.

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
      surfaceTarget?: "header" | "sidebar"; // see tokens.md §"effectiveSurfaceTarget"
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
      each preset's customization, with a computed placement injected, validates
      against `resume.schema.json`'s customization `$def` via ajv; every
      preset's font family ∈ the schema enum; every `byType` list ⊆ the schema's
      `sectionType` enum with no duplicates; ids unique.
- [ ] **Step 2: Generator validation/emission for the 20 committed presets**;
      regenerate; pass. Negative generator tests: an out-of-enum font and an
      out-of-enum `sidebarSectionTypes` entry each fail generation loudly.
- [ ] **Step 2a: Contrast conformance.** Assert all 20 committed presets pass
      `tokens.md` §5's contrast targets before clamping, under its normative mix
      space (gamma-encoded sRGB, `color-mix(in srgb, …)` — not linear light or
      OKLab), per `contract.md`:277's per-preset conformance requirement.
- [ ] **Step 3: `applyTemplate` tests**: `keep` preserves `layout.sections`
      byte-for-byte; `byType` — property test over generated content-key sets
      (seeded, deterministic): result always satisfies exactly-once, sidebar
      holds exactly the byType-matched keys in rule order, main holds the rest
      in current visual order; empty content → two empty arrays; everything else
      replaced from the preset; inputs never mutated; output (with real content)
      validates against the schema.
- [ ] **Step 4: Gates + commit.** `make schema-check` and the full web gate.
      Serialize `generate.mjs` edits with Task 1 through the integration owner
      if concurrent.
