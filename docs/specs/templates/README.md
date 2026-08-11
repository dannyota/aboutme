# Resume template system

DRAFT v1 (2026-08-11) — not approved.

Defines what a resume template is in aboutme: the interface every template
implements against the frozen document contract of
[`../aboutme-design.md`](../aboutme-design.md) §3, the design tokens it may
control, and how it behaves under Chromium's print engine. It fixes the boundary
between what the template decides, what the user decides, and what the document
carries, precisely enough that two designers working from it independently
produce compatible templates. Bound by
[ADR 0008](../../adr/0008-template-apply-semantics.md) and
[ADR 0009](../../adr/0009-section-order-authority.md).

- [`contract.md`](contract.md) — what a template is, the decision boundary,
  apply and ordering semantics, per-`sectionType` rendering, absence and hiding
  rules, column behavior, and open contract pressure.
- [`tokens.md`](tokens.md) — every token with type, range, baseline, and owner;
  typography, color roles, spacing, headings, rules; the accessibility floor.
- [`print.md`](print.md) — `@page` geometry, break and widow rules, two-column
  fragmentation, photo handling, and the determinism snapshot tests depend on.

Concrete template designs are written against this contract and add no
requirements to it.
