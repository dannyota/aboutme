# 0012: A template preset carries a placement rule with one total order

Status: Accepted (2026-08-02, 2026-08-12, 2026-10-01).

## Context

Templates are customization presets stored as JSON in the repository
(`packages/schema/templates/*.json`). Applying one replaces the document's
customization and leaves content untouched. But `customization` contains
`layout.sections`, which lists the document's own section keys, including
user-created custom sections with generated UUIDs. A preset committed to the
repository cannot know them. A literal "replace all customization" writes
`layout.sections` that omits or invents keys, and the aggregate validator
(`validateLayoutSections`, AC-DOC-008) rejects it.

Preserving `layout.sections` and letting a preset carry only typography and
spacing would satisfy the validator, but then applying a two-column preset to a
document with an empty sidebar gives a two-column layout with an empty second
column, and templates would differ only by fonts and spacing.

Placement also needs deterministic tie-breaks: the order of keys with the same
selected type, the order of keys that start in different columns, and how
duplicate selectors and invalid current placement fail. A preset cannot know the
meaning of a user-created custom section.

## Decision

A preset does not carry section keys. It carries a placement rule, and
`applyTemplate` computes `layout.sections` as a total function of the document's
current placement and content keys. Applying a template replaces the rest of
`customization` wholesale and never changes `content`, with two kinds of
exception:

- **Kept:** the owner's `pageFormat`, `colorScheme`, `font.textAlign`, and
  `header.photoPosition`, or their absence. ADR 0013 names the last two.
- **Derived:** `dateFormat` follows the resume language, `MM/YYYY` for a `vi`
  primary subtag and `Mon YYYY` otherwise, never the preset's value.

[Template contract](../design/templates/contract.md) §3 holds the exact rules.

The current visual order is `layout.sections.main` followed by
`layout.sections.sidebar`, the same linear order the renderer uses for one
column. Before applying either rule, `applyTemplate` validates the current
aggregate: the two arrays contain every content key exactly once and no key
absent from `content`. Invalid input returns a typed error; it is never repaired
from `content` map iteration.

- `layout.placement: "keep"` requires `sidebarSectionTypes` to be absent and
  returns both validated arrays byte for byte. One-column presets use it.
- `layout.placement: "byType"` requires an ordered `sidebarSectionTypes` list
  with no duplicates that excludes `custom`. For each listed type in list order,
  matching keys are appended to `sidebar` in current visual order. Every
  unselected key, and every custom section, stays in `main` in current visual
  order.

An empty document produces two empty arrays. Inputs are never mutated. Valid
output always contains every content key exactly once, by construction.

The one-column and two-column toggle in the customize panel is a different
operation from applying a template and keeps its own preserve-and-move
semantics.

## Consequences

- The preset schema and its generated types express `layout.placement`, and
  presets are validated like any other schema-derived artifact. Preset
  validation rejects duplicate selectors and `custom` before a user applies the
  preset; runtime validation still fails closed for malformed documents.
- `applyTemplate` is a pure function of the document's content keys and current
  placement and is unit-testable without a database. Every consumer, including
  the customize panel and template thumbnails, uses it rather than
  reimplementing placement.
- The naive "replace all customization" implementation is the one most likely to
  appear. An independent adversarial suite derived from this record tests it,
  and property tests cover cross-column order, repeated section types, custom
  sections, duplicate selectors, missing keys, extra keys, and input
  immutability.
- A selected type may move from either column, but keys of that type do not
  reorder among themselves.
- Golden fixtures render each preset against one- and two-column documents, so
  an empty-sidebar regression shows in a snapshot diff.

## History

- Former ADR 0008 (2026-08-02): placement rule instead of a section list.
- Former ADR 0021 (2026-08-12): the total order, validation, and custom-section
  rule. Both unchanged in substance.
- 2026-10-01: the apply exceptions are listed here. The kept settings were
  already in the design; the language-derived `dateFormat` is the owner's new
  rule, replacing the preset's value.
