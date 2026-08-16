# 3. Data model and document contract

PostgreSQL owns relational identity, concurrency, and lifecycle state. A
resume's editable content remains one versioned JSON aggregate split across
three `jsonb` columns for storage and access control.

## Relational model

The migration files are the exact schema authority. This table describes the
intended model, not replacement DDL.

| Table                    | Purpose and key rules                                                                                  |
| ------------------------ | ------------------------------------------------------------------------------------------------------ |
| `users`                  | UUIDv7 identity, unique case-insensitive email, display name, provider avatar key, timestamps          |
| `identities`             | Provider subject linked to one user; unique `(provider, provider_user_id)`; never merged by email      |
| `oauth_transactions`     | One-use state, purpose, PKCE verifier, redirect URI, expiry, and OpenID Connect nonce                  |
| `sessions`               | Hashed opaque token, CSRF secret, activity and expiry times, rotation lineage, and recent-reauth time  |
| `password_credentials`   | Zero-or-one Argon2id hash per user, cascading foreign key, created and changed times                   |
| `password_registrations` | Pending email verification: unique canonical email, name, hash, token digest, 24-hour expiry           |
| `password_reset_tokens`  | Single-use reset token digest with 30-minute expiry; unique per user                                   |
| `auth_email_jobs`        | Encrypted verification/reset/notification payload with bounded lease, retry, and terminal state        |
| `resumes`                | Owner, title, optional slug, publish flags, document version, revision, locale, and three JSON parts   |
| `slug_tombstones`        | Released slug and release time; the former owner becomes nullable on account deletion                  |
| `idempotency_records`    | User, concrete operation identity, mutation key, semantic request fingerprint, stored response, expiry |
| `idempotency_usage`      | One per-user retained-record and stored-response-byte counter maintained transactionally               |
| `media_deletion_jobs`    | Exact immutable object key, due time, bounded retry state, terminal outcome, audit timestamps          |
| `public_state`           | Singleton durable discovery generation advanced with public-membership mutations                       |

Server-owned relational rows use PostgreSQL UUIDv7 defaults. Client-generated
UUIDs occur only inside resume documents as entry identifiers.

`media_deletion_jobs` is cleanup state, not media ownership. A transaction that
removes a photo reference enqueues its validated exact key in the same commit.
The document reference remains the sole ownership authority; the ledger only
drives bounded physical deletion and records its outcome.

`public_state` has one checked singleton row and a positive monotonic
`discovery_generation`. A transaction that changes a resume slug, live state,
discovery eligibility, or deletes a public resume increments it with the same
commit. P5A owns its forward migration and every public-state caller. Go loads
the committed generation before readiness and uses it for aggregate discovery
cache keys, entity tags, and the in-process response fence. Aggregate discovery
contains only a fixed heading and the slug-ordered eligible public URLs; it has
no other mutable resume field, so other document edits cannot change its bytes.

Database constraints enforce global slug uniqueness and format, valid publish
flag combinations, and the three-resume cap. Resume creation also locks the
owner row so concurrent callers cannot race past the cap. Reserved slug checks,
document validation, and write policy run in the Go domain boundary.

`resumes.lng` is nullable draft metadata with a 35-character database bound. An
HTTP write treats null or `""` as unset. A non-empty value must parse as a
well-formed BCP 47 language tag. The server canonicalizes it, checks the
canonical form against the 35-character bound, and only then persists it. The
read projection maps null, `""`, an invalid legacy value, or a legacy value
whose canonical form exceeds the bound to `und`; a valid bounded value maps to
the same canonical form. API responses and every render context use that total
projection, so the renderer never receives an empty, invalid, or overlong root
language. This requires no migration because the database remains the coarse
stored-length boundary and Go owns semantic validation.

## Resume aggregate

The canonical document has these top-level values:

- `schemaVersion`
- `personalDetails`
- `content`
- `customization`

`content` is an **unordered** map from a stable section key to a section.
`customization.layout.sections.main` and `.sidebar` are the sole section-order
and placement authority. Every content key appears exactly once across those two
arrays. [ADR 0009](../adr/0009-section-order-authority.md) records why `content`
key order cannot be authoritative in PostgreSQL `jsonb`.

Every section carries a `sectionType`, an ordered `entries` array, and optional
display metadata. Entry IDs are unique across the entire resume, not only inside
one section.

| `sectionType` | Type-specific fields                                                              |
| ------------- | --------------------------------------------------------------------------------- |
| `profile`     | `text`                                                                            |
| `work`        | `jobTitle`, `employer`, `employerLink`, `city`, `country`, `dates`, `description` |
| `education`   | `degree`, `school`, `schoolLink`, `city`, `country`, `dates`, `description`       |
| `skill`       | `name`, optional `level` from 0–5, `infoHtml`                                     |
| `language`    | `name`, optional `level` from 0–5                                                 |
| `certificate` | `title`, `titleLink`, `issuer`, `date`, `description`                             |
| `project`     | `title`, `link`, `dates`, `description`                                           |
| `custom`      | `title`, `titleLink`, `subtitle`, `city`, `dates`, `description`                  |
| Every entry   | Client-generated UUID `id` and optional `isHidden`                                |

A date range is `{start:{y,m?}, end:{y,m?}|null, present:boolean}`. Start may
not follow end. `present=true` requires a null end; `present=false` requires an
end. Certificate `date` is a single `{y,m?}`.

## Draft and publish validation

Stored documents are draft-permissive. Each entry requires only its ID and the
enclosing section's discriminator; domain fields may be absent or empty while
the user types. Absence means “never entered.” An empty string means “explicitly
cleared.” Both states survive every round trip.
[ADR 0005](../adr/0005-draft-permissive-documents.md) records the rationale.

Publishing runs a separate versioned policy. It requires a non-blank full name,
at least one visible entry, and the declared required fields for each visible
entry type. A failure returns structured issues so the editor can focus the
offending fields. New document fields begin optional; they do not force an
all-document migration.

## Bounds and invariants

The domain boundary validates the fully assembled aggregate on every write:

- Request JSON at most 256 KiB, except the bounded photo-upload route.
- Canonical resume document at most 512 KiB.
- At most 24 sections and 64 entries per section.
- Rich text at most 16 KiB measured as UTF-8 bytes.
- Unique entry IDs across every section.
- Valid date ordering, URL schemes, photo keys, and section placement.
- Customization changes limited to a fixed path allowlist; structural paths are
  excluded from ordinary customization deltas.

Create, delete, move, or reorder section operations write `content` and layout
together through one transactional structure command. Field-level changes may
use granular requests, but storage always validates and persists the complete
aggregate.

## Document versions

Every released document version retains an immutable `resume.v<N>.schema.json`
and generated Go and TypeScript types. A reviewed manifest declares the current,
accepted, and emitted versions; generators do not discover releases by scanning
filenames.

Reads project old stored documents in memory and never write. A user write
persists the projected current shape with revision compare-and-swap (CAS).
Background backfill compares the observed schema version and revision. It does
not bump the revision, and it loses cleanly to any concurrent resume write.
Adjacent up and down converters are explicit, validated at every step, and
tested in both directions. The v2 compatibility release accepts and emits both
v1 and v2. Its only declared lossy emission is a v2 font ID that v1 cannot
represent: emission uses the catalog entry's explicit v1 fallback, and every
non-font value must remain equal. Old-client mutations preserve the stored v2
font unless the operation explicitly targets that field. Retained types support
compatibility testing; HTTP delta application may remain generic so handlers do
not need one compiled code path per old version.
[ADR 0017](../adr/0017-resume-document-versioning.md) records this boundary.

## Schema and migrations

- `packages/schema/resume.schema.json` is the editable document-shape source.
- `packages/schema/released-versions.json` declares immutable releases.
- Code generation derives discriminators and entry definitions from the schema;
  conformance tests keep JSON Schema, TypeScript, and Go aligned
  ([ADR 0006](../adr/0006-schema-derived-codegen.md)).
- `apps/server/migrations/*.sql` is the sole relational schema source. Migration
  DDL is hand-written, applied by the embedded goose command, read by sqlc, and
  becomes append-only at the first UAT baseline
  ([ADR 0010](../adr/0010-goose-only-migrations.md),
  [ADR 0020](../adr/0020-uat-migration-baseline.md)).
- Generated artifacts are committed and changed only through their source and
  generator.

There is no separate declarative relational schema file. Reading migrations and
sqlc queries together gives the applied schema and typed access layer without a
second schema source that can drift.

Before the first local UAT baseline, migration history is development-only. The
integration owner may correct it and recreate the shared development database
after every live-database worker is idle. The first UAT candidate adds
`apps/server/migrations/.uat-baseline`. After that marker lands, the integration
gate rejects changing the marker or any existing migration; only new forward
migrations may be added. Goose tracks applied versions, not file checksums, at
runtime.
