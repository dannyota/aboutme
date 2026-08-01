# 0005 — Draft-permissive, publish-strict document validation

Status: Accepted (2026-08-01)

## Context

The original rule was the opposite of this one: every domain field on every
entry was required, with empty strings as the sentinel for "not entered yet." An
adversarial design review rejected it. An autosaving editor persists on every
keystroke pause, not on submit — a required-field rule forces it to fabricate a
plausible year, level, or title just to save a half-typed entry, which corrupts
the document with data the user never wrote. Worse, collapsing "never entered,"
"unknown," and "explicitly cleared" all into `""` destroys information the
editor and publish-time checks both need: there is no way to tell a field the
user blanked out on purpose from one they haven't reached yet.

## Decision

Stored resume documents are **draft-permissive**: every save requires only `id`
and the section's `sectionType` discriminator on each entry, and all domain
fields are optional. Completeness is enforced only at
`POST /resumes/{id}/publish`, which applies per-type rules (e.g. `work` needs
`jobTitle` + `employer`) and returns `422` listing the offending entries.
Absence and `""` remain distinct: a missing key means "never entered," `""`
means "explicitly cleared." Never fabricate a sentinel value to satisfy a
schema.

## Consequences

- Absence vs. `""` must stay distinguishable end-to-end — through Go structs,
  JSON serialization, and TypeScript types — rather than being normalized away
  at any layer.
- New domain fields are always introduced optional, so adding one is an additive
  schema change, never an all-document backfill migration.
- A separate publish-time validation layer is required, independent of the
  draft-save path, and is the only place completeness is enforced.
