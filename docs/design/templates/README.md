# Resume template system

Status: **Approved v2** (2026-08-12).

Defines what a resume template is in aboutme: the interface every template
implements against the versioned [data contract](../data.md), the design tokens
it may control, and how it behaves under Chromium's print engine. It fixes the
boundary between what the template decides, what the user decides, and what the
document carries, precisely enough that two designers working from it
independently produce compatible templates. Bound by
[ADR 0008](../../adr/0008-template-apply-semantics.md),
[ADR 0009](../../adr/0009-section-order-authority.md), and
[ADR 0021](../../adr/0021-template-placement-order.md), which fixes the total
order and fail-closed validation for template placement.

- [`contract.md`](contract.md) — template identity, apply and ordering
  semantics, rendering, absence, hiding, columns, and conformance.
- [`limitations.md`](limitations.md) — accepted limits and required warnings.
- [`tokens.md`](tokens.md) — token vocabulary and typography.
- [`colors.md`](colors.md) — color roles, surfaces, and accessibility floors.
- [`geometry.md`](geometry.md) — spacing, page geometry, and preset boundaries.
- [`print.md`](print.md) — `@page` geometry, break and widow rules, two-column
  fragmentation, photo handling, and the determinism snapshot tests depend on.
- [`presets/`](presets/) — rationale for each preset in the released set.

Concrete template designs are written against this contract and add no
requirements to it.

The preset rationale files record the v1 preset designs. Their JSON has landed
and is the input the renderer and preset registry build against.
