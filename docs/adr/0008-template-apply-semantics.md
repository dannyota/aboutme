# 0008 — A template preset carries a placement rule, not a section list

Status: Accepted (2026-08-02)

## Context

The design spec is frozen, and two of its statements cannot both be satisfied.

§5 defines template application:

> Templates: **customization presets as JSON in repo**
> (`packages/schema/templates/*.json`), no DB table v1; apply = full
> customization replace, content untouched

§3 defines the aggregate invariant the store enforces:

> the three jsonb columns are ONE aggregate, not three independent documents.
> Every `content` section key must appear **exactly once** across the
> `customization.layout.sections` arrays; those arrays are bounded,
> deduplicated, and may not reference a missing key.

`customization` contains `layout.sections`, which enumerates the document's own
section keys. A preset committed to the repository cannot know them: section
keys include user-created custom sections with generated UUIDs
(`packages/schema/fixtures/full.json` places `a6a0a5fa-…` in `sidebar`). A
literal "full customization replace" therefore writes a `layout.sections` that
either omits content keys or references keys that do not exist, and the
store-layer aggregate validator (`validateLayoutSections`, AC-DOC-008) rejects
the result on write. Template apply, as literally specified, cannot be
implemented.

The Phase 3 plan first proposed resolving this by preserving `layout.sections`
verbatim and letting a preset carry only typography and spacing. That satisfies
both constraints, but a preset can then never express placement: applying the
two-column `sidebar` or `compact` preset to a document whose `sidebar` array is
empty — the state of every document created in one-column mode, and of three of
the four golden fixtures — produces a two-column layout with an empty second
column. Templates would differ only by fonts and spacing, and the golden
snapshots would enshrine a broken layout as the reference rendering.

## Decision

A preset does not carry section keys. It carries a **placement rule**, and
`applyTemplate` computes `layout.sections` as a total function of the document's
actual content keys:

- `layout.placement: "keep"` — preserve the document's current placement.
  One-column presets use this.
- `layout.placement: "byType"` with an ordered `sidebarSectionTypes` list —
  every content key whose section type appears in the list goes to `sidebar`, in
  that order; every other key goes to `main`, preserving its current relative
  order.

Because the function assigns each content key to exactly one column, the
exactly-once invariant holds **by construction** rather than by validation, and
a preset can express real placement without knowing any key.

Everything else in §5's sentence stands: applying a template replaces the rest
of `customization` wholesale, and `content` is untouched.

The one-column ↔ two-column toggle in the customize panel is a **different
operation** from applying a template and keeps its own preserve-and-move
semantics. §5's preservation language describes that toggle.

## Consequences

- `packages/schema/templates/*.json` gains a `layout.placement` shape; the
  preset schema and its generated types must express it, and presets are
  validated against it like any other schema-derived artifact.
- `applyTemplate` takes the document's content keys as an argument and returns a
  complete `customization`. It is a pure function and is unit-testable without a
  database.
- This is the rule an implementer is most likely to invert — the naive
  implementation is the specified-but-unimplementable one — so it is covered by
  the phase's independent blind adversarial suite, derived from this ADR and the
  spec rather than from the implementation.
- Golden fixtures render each preset against documents in both one- and
  two-column states, so an empty-sidebar regression is visible in a snapshot
  diff rather than only in production.
- Spec §5's "apply = full customization replace" is superseded by this ADR for
  the `layout.sections` sub-object only. The spec file itself is frozen and is
  not edited; this ADR is the authority, per `docs/README.md`.
- P4's customize panel and P7B's template thumbnails both consume
  `applyTemplate`; they inherit this contract rather than reimplementing
  placement.
