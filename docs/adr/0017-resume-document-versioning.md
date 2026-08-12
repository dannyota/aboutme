# 0017 — Resume document versions use explicit adjacent converters

Status: Proposed (2026-08-12)

## Context

Stored resumes and older clients can outlive a schema release. Editing a
released schema in place breaks validation and generated types for those
documents. Updating rows during a read adds write contention and can bump a
revision without a user edit.

## Decision

Each released document version has an immutable JSON Schema and retained Go and
TypeScript types. A checked-in manifest names every released version and the
current version. Unknown versions fail closed.

Conversions are pure, deterministic functions between adjacent versions. The
service composes those functions to project a stored document into the current
shape for validation and use. A normal read does not persist the projection.

A separate backfill may persist the projected current document with compare-
and-swap against the observed schema version and user-visible revision. It does
not bump that revision. Any concurrent resume write wins and the backfill
retries later.

An API request may declare a supported version. The server projects to that
shape for input and output while storage remains current. Creating a new schema
version updates the manifest, schemas, converters, generated types, tests,
OpenAPI examples, and consumers in one reviewed contract change.

The first compatibility release makes v2 current and declares both v1 and v2
accepted and emitted. Emitting a v2 document at v1 normally remains lossless.
The only declared exception is `customization.font.family`: a v2 catalog ID that
v1 cannot represent emits the catalog entry's explicit v1 fallback. The emission
policy rejects any other changed value. A v1 mutation that does not target the
font preserves the stored v2 font; it does not persist the fallback that was
present only in the down-emitted working document. A v1 mutation that does
target the font maps that chosen v1 family back to its v2 ID.

## Consequences

- Released schema files and versioned generated outputs never change.
- Adding font choices or fields to the v1 document requires v2; widening the v1
  enum is not allowed.
- Converter tests must prove exact round trips where lossless. The v2-to-v1 font
  exception proves the declared fallback and byte-for-value preservation of
  every non-font field. Any other loss fails closed.
