# Resume template system

Status: **Draft v2** (2026-08-12). Not approved.

Defines what a resume template is in aboutme: the interface every template
implements against the versioned [data contract](../data.md), the design tokens
it may control, and how it behaves under Chromium's print engine. It fixes the
boundary between what the template decides, what the user decides, and what the
document carries, precisely enough that two designers working from it
independently produce compatible templates. Bound by
[ADR 0008](../../adr/0008-template-apply-semantics.md) and
[ADR 0009](../../adr/0009-section-order-authority.md).

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

The preset rationale files record Draft v1 designs. Their JSON has landed, but
neither that repository state nor an old plan's “sign-off” wording approves this
Draft v2 contract. Approval is explicit and dated here after independent review.
