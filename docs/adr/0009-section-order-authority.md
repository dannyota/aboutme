# 0009 — Section order lives in `customization.layout.sections`, not in `content` key order

Status: Accepted (2026-08-02)

## Context

The design spec is frozen, and one of its statements cannot be satisfied by the
storage it also mandates.

§3 describes the document's section container:

> `content` = ordered map
> `sectionKey → {entries[], displayName, iconKey, sectionType}`

The same section stores that container in a `jsonb` column. Postgres `jsonb`
does not preserve object key order: it normalizes keys on input (sorting by
length, then bytewise) and there is no mechanism to recover the order a client
sent. A round trip through storage therefore destroys any ordering `content`
carried. "Ordered map" is unimplementable as written against the mandated column
type.

Phase 2A needs this resolved before it appends the `resumes` DDL, because the
alternatives diverge at the schema: preserving key order would require either a
`json` column (giving up jsonb indexing, containment operators, and the
normalization that makes byte-comparison of stored documents meaningful) or a
parallel order array inside `content` (a second ordering authority, which the
aggregate invariant would then have to keep consistent with the first).

Neither is necessary, because the spec already designates a different authority
for section order and placement in three separate places:

- §3's aggregate invariant: every `content` section key must appear **exactly
  once** across the `customization.layout.sections` arrays.
- §4's route table: `PATCH /resumes/{id}/structure` is "**the only way to
  create, delete, move or reorder a section**", and it writes `content` and
  `customization.layout` in one transaction. The sibling
  `PATCH /resumes/{id}/sections/{sectionKey}` is explicitly scoped to "name /
  icon / entry order (content only; **never changes section placement**)".
- §5's renderer tree: `LayoutColumns` takes "placement from
  `customization.layout.sections`", and the one-column decision of 2026-08-01
  has the renderer emit `main` followed by `sidebar` "in order" — that order
  being the arrays', not `content`'s.

Every consumer of section order in the spec already reads
`customization.layout.sections`. Nothing reads `content` key order.

## Decision

**`customization.layout.sections` is the sole authority for section order and
placement. `content` is an unordered map keyed by section key.**

Consequently:

- `content` is stored in `jsonb`, and jsonb's key normalization is harmless by
  construction rather than by accident.
- Entry order within a section remains the `entries` array's order, which is a
  JSON array and is preserved everywhere. This ADR changes nothing about it.
- No order array, no ordinal field, and no `json`-typed column is introduced.
- A reader that needs sections in order iterates `customization.layout.sections`
  and looks each key up in `content`. Iterating `content` is only ever valid for
  order-independent work (validation, size accounting, search).

§3's "ordered map" is superseded by this ADR for the ordering claim only. The
rest of that sentence — the key shape and the per-section value shape — stands.
The spec file is frozen and is not edited; this ADR is the authority, per
`docs/README.md`.

## Consequences

- The aggregate invariant is load-bearing, not merely defensive: it is what
  guarantees the order authority is total. A `content` key missing from
  `layout.sections` has no defined position and would be silently unrendered,
  which is exactly what "exactly once across the arrays" already forbids and
  what `validateLayoutSections` (AC-DOC-008) already enforces on every write.
- P2A's store may treat the three jsonb columns as byte-comparable after
  normalization. Codec round-trip tests assert stability of the assembled
  document, not of `content`'s key order — a test asserting key order would be
  asserting a property the storage does not provide.
- P3's renderer must derive placement from `customization.layout.sections` only.
  Iterating `content` to emit sections is the defect this ADR names in advance;
  the phase's golden snapshots cover both column modes, so an
  order-from-`content` implementation shows up as a snapshot diff.
- P2B's `PATCH /resumes/{id}/structure` is the only endpoint that may reorder
  sections, and it writes both columns in one transaction. A reorder expressed
  as a `content`-only patch has no effect and must be rejected rather than
  silently accepted.
- ADR 0008's `applyTemplate` computes `layout.sections` as a total function of
  the document's content keys. That composes with this decision: the computed
  arrays are the order authority the moment they are written, and the
  exactly-once property holds by construction.
