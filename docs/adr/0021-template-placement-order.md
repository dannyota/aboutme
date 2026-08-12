# 0021 — Template placement has one deterministic total order

Status: Accepted (2026-08-12)

## Context

[ADR 0008](0008-template-apply-semantics.md) makes template placement a pure
function of current section placement, content, and an ordered
`sidebarSectionTypes` list. It does not define three tie-breaks needed for a
deterministic implementation:

- the order of keys with the same selected section type;
- the relative order of keys that begin in different columns; or
- how duplicate type selectors and invalid current placement fail.

[ADR 0009](0009-section-order-authority.md) makes `layout.sections` the sole
section-order authority. The renderer already defines the document's linear
one-column order as `main` followed by `sidebar`. Template application can use
that same order without reading the unordered `content` map.

A preset also cannot know the meaning of a user-created custom section. The
generic `custom` type is therefore not a valid sidebar selector; custom sections
follow the default main branch.

## Decision

Template application has one current visual order: `layout.sections.main`
followed by `layout.sections.sidebar`.

Before applying either placement rule, `applyTemplate` validates the current
aggregate. The two arrays must contain every content key exactly once and must
not contain a key absent from `content`. Invalid input returns a typed error; it
is never repaired from `content` map iteration.

`"keep"` requires `sidebarSectionTypes` to be absent and returns both validated
arrays byte-for-byte.

For `"byType"`:

- `sidebarSectionTypes` is required, contains no duplicates, and excludes
  `custom`;
- for each listed type in list order, matching keys are appended to `sidebar` in
  current visual order;
- every unselected key remains in `main` in current visual order; and
- every custom section remains in `main`, regardless of its previous column.

An empty document produces two empty arrays. Inputs are never mutated. Invalid
current placement or an invalid selector list produces a deterministic error, so
valid output always contains every content key exactly once.

This decision supplements ADR 0008's placement rule and leaves ADR 0009's
ordering authority unchanged. If accepted, it supersedes only the unspecified
ordering and validation details above.

## Consequences

- Independent implementations and golden generation produce the same section
  order without relying on object-key iteration.
- A selected type may move from either current column, but keys of that type do
  not reorder among themselves.
- Unselected built-in sections and all custom sections move to the main column.
- Preset validation can reject duplicate selectors and `custom` before a user
  applies the preset. Runtime validation still fails closed for malformed
  current documents.
- Property tests cover cross-column order, repeated section types, custom
  sections, duplicate selectors, missing keys, extra keys, and input
  immutability.
