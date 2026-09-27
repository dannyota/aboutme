# 0004: Resume document validation, code generation, order, and versions

Status: Accepted (2026-08-01, 2026-08-02, 2026-08-12, 2026-09-19). Document v5
below is Proposed, pending owner approval.

## Context

An autosaving editor persists on every pause, not on submit. A rule that
requires every domain field forces the editor to invent a year, level, or title
to save a half-typed entry, and collapsing "never entered" and "explicitly
cleared" into `""` destroys information the editor and publish checks need.

A byte-compare drift test certifies only that generation is deterministic. A
generator whose entry-type list is hardcoded reproduces its own omission on
every run, so a new `sectionType` could pass AJV and the TypeScript union while
the Go dispatch silently rejects it.

The document stores `content` in a `jsonb` column. PostgreSQL `jsonb` does not
preserve object key order, so `content` cannot carry section order.

Stored resumes and older clients outlive a schema release. Editing a released
schema in place breaks validation and generated types for those documents, and
updating rows during a read adds write contention and can bump a revision
without a user edit.

## Decision

### Draft-permissive, publish-strict

Stored documents are draft-permissive. A save requires only `id` and the
`sectionType` discriminator on each entry; every domain field is optional.
Completeness is enforced only at `POST /resumes/{id}/publish`, which applies
per-type rules (for example `work` needs `jobTitle` and `employer`) and returns
`422` listing the offending entries. A missing key means "never entered" and
`""` means "explicitly cleared". Never fabricate a sentinel value to satisfy a
schema.

### Schema-derived code generation

Code generation derives every discriminator and entry definition from the JSON
Schema at generation time, never from a list in the generator. A conformance
test enumerates every `sectionType` in `resume.schema.json` and asserts that
AJV, the generated TypeScript union, and the Go dispatch each accept a same-type
sample and reject cross-type entries. It runs in CI beside the drift check, not
instead of it.

### Section order

`customization.layout.sections` is the sole authority for section order and
placement. `content` is an unordered map keyed by section key.

- Entry order within a section is the `entries` array order.
- No order array, ordinal field, or `json`-typed column exists.
- A reader that needs sections in order iterates `customization.layout.sections`
  and looks each key up in `content`. Iterating `content` is valid only for
  order-independent work (validation, size accounting, search).
- Every `content` key appears exactly once across the `layout.sections` arrays,
  and no array names a missing key (`validateLayoutSections`, AC-DOC-008).
- `PATCH /resumes/{id}/structure` is the only endpoint that creates, deletes,
  moves, or reorders a section, and it writes `content` and
  `customization.layout` in one transaction. A `content`-only reorder is
  rejected.

### Versions and converters

Each released document version has an immutable JSON Schema and retained Go and
TypeScript types. A checked-in manifest names every released version and the
current version. Unknown versions fail closed.

Conversions are pure, deterministic functions between adjacent versions. The
service composes them to project a stored document into the current shape for
validation and use. A normal read does not persist the projection. A separate
backfill may persist the projected document with compare-and-swap against the
observed schema version and user-visible revision; it does not bump that
revision, and any concurrent write wins.

An API request may declare a supported version. The server projects to that
shape for input and output while storage stays current. A new schema version
updates the manifest, schemas, converters, generated types, tests, OpenAPI
examples, and consumers in one reviewed change.

The server accepts and emits v1 to v4; v4 is current. Down-emission is lossless
except for these declared losses:

| Emitted | Loss                                                                                 |
| ------- | ------------------------------------------------------------------------------------ |
| v1      | A v2 font catalog ID that v1 cannot represent emits the entry's explicit v1 fallback |
| v2      | Detail `display` and `customization.font.textAlign` are dropped                      |
| v3      | `customization.header.photoPosition` and every project `subtitle` are dropped        |

An older client write never erases a field it cannot express:

- A v1 write that does not target the font keeps the stored font; a v1 write
  that targets it maps the chosen v1 family back to its v2 ID.
- A v1 or v2 write keeps the stored `textAlign`, and the stored `display` of
  every detail that survives the write, matched by detail id. Detail ids are
  therefore unique; store validation rejects a repeated id.
- A v1 to v3 write keeps the stored `photoPosition` while both the stored and
  the written document have a header; removing the header removes it. Each
  project entry that survives keeps its stored `subtitle`, matched by entry id;
  an added entry gets none.

### Document v5 (Proposed)

v5 adds one optional leaf, `customization.colorScheme`, an enum of `light`,
`dark`, and `system`; absent means `light`. ADR 0020 owns what it renders. Once
approved, the server accepts and emits v1 to v5 and v5 is current.

- Up from v4 adds nothing: a v4 document is a v5 document without the leaf.
- Emitting v4 drops `customization.colorScheme`. It is v5's one declared loss,
  and it composes with the older losses in the table above.
- A v1 to v4 write keeps the stored `colorScheme` unconditionally, because
  `customization` exists in every version.

## Consequences

- Absence and `""` stay distinct through Go structs, JSON, and TypeScript types.
- New domain fields are always optional, so adding one is additive, never a
  backfill of every document.
- Go has no sum types, so the `sectionType` dispatch is hand-written outside
  generated output; the conformance test is what keeps it aligned.
- The store treats the three `jsonb` columns as byte-comparable after
  normalization. Codec tests assert stability of the assembled document, never
  of `content` key order.
- Released schema files and versioned generated outputs never change. Adding a
  field to a released version needs a new version; widening a released enum is
  not allowed.
- Converter tests prove exact round trips where lossless, and each declared loss
  exactly. Any other loss fails closed.
- A release that stores a new version cannot be rolled back to an older release,
  which fails closed on that version. Fix forward; the
  [production runbook](../runbooks/production.md#rollback) records this.
- The [data design](../design/data.md#document-versions) states the version
  rules; ADR 0013 owns the v3 and v4 fields' rendering, and ADR 0020 owns the
  proposed v5 field's.

## History

- Former ADR 0005 (2026-08-01): draft-permissive documents. Unchanged.
- Former ADR 0006 (2026-08-01): schema-derived code generation. Unchanged.
- Former ADR 0009 (2026-08-02): section order in `layout.sections`, correcting
  the design's "ordered map". Unchanged.
- Former ADR 0017 (2026-08-12): adjacent converters and the v2 font fallback.
  Unchanged.
- The document v3 and v4 compatibility rules came from former ADRs 0041 and 0044
  (2026-09-19), which released those versions.
- Proposed (2026-09-27, pending owner approval): document v5 with the optional
  `customization.colorScheme`, its v4 loss, and its older-client write rule.
