# 3. Data model and document contract

PostgreSQL owns relational identity, concurrency, and lifecycle state. A
resume's editable content remains one versioned JSON aggregate split across
three `jsonb` columns for storage and access control.

## Relational model

The migration files are the exact schema authority. This table describes the
intended model, not replacement DDL.

| Table                            | Purpose and key rules                                                                                                     |
| -------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| `users`                          | UUIDv7 identity, unique case-insensitive email, display name, provider avatar key, authentication epoch, timestamps       |
| `identities`                     | Provider subject linked to one user; unique `(provider, provider_user_id)`; never merged by email                         |
| `oauth_transactions`             | One-use state, purpose, PKCE verifier, redirect URI, expiry, and OpenID Connect nonce                                     |
| `sessions`                       | Hashed opaque token, CSRF secret, epoch, activity and expiry, rotation lineage, primary and factor proof times            |
| `password_credentials`           | Zero-or-one Argon2id hash per user, cascading foreign key, created and changed times                                      |
| `password_registrations`         | Pending email verification: unique canonical email, name, hash, token digest, 24-hour expiry                              |
| `password_reset_tokens`          | Single-use reset token digest with 30-minute expiry; unique per user                                                      |
| `auth_email_jobs`                | Encrypted verification/reset/notification payload with bounded lease, retry, and terminal state                           |
| `oauth_clients`                  | Public agent client ID, bounded name, bounded redirect-URI list, created and last-used times                              |
| `oauth_authorization_codes`      | Unique code digest, client/user/grant, epoch, scopes, S256 challenge, redirect URI, 60-second expiry, consumed time       |
| `oauth_grants`                   | Unique live (user, client) pair, epoch, granted scopes, created and revoked times                                         |
| `oauth_tokens`                   | Unique token digest, closed kind, family ID, rotated-from lineage, client/user/grant, expiry, revoked and last-used times |
| `second_factor_policies`         | One per enrolled user: random WebAuthn user handle, enforcement start, and last attempt-mail time                         |
| `webauthn_credentials`           | Owned credential ID, COSE public key, counter, backup flags, transports, and use times                                    |
| `second_factor_recovery_codes`   | Single-use, domain-separated recovery-code digests and creation time                                                      |
| `pending_authentications`        | Hashed pending token, CSRF secret, purpose, epoch, optional session binding, expiry, and failure count                    |
| `webauthn_ceremonies`            | One-use challenge, purpose, account/session or pending binding, epoch, expiry, and optional proposed user handle          |
| `totp_credentials`               | Zero or one per user: sealed secret, key ID, nonce, format, last used step, failure count, last failure, and cool-down    |
| `totp_enrollments`               | Ten-minute sealed setup secret bound to account, session, epoch, and issuer; token digest; one row per user               |
| `authentication_security_events` | Rejected non-increasing passkey counter values, retained for 180 days                                                     |
| `resumes`                        | Owner, title, optional slug, publish flags, document version, revision, locale, and three JSON parts                      |
| `resume_preview_cards`           | One per live resume: 16-hex card version, PNG of 1 to 524,288 bytes, render time; cascades with the resume                |
| `slug_tombstones`                | Released slug and release time only, no account link; deleted by the privacy sweep 180 days after release                 |
| `idempotency_records`            | User, concrete operation identity, mutation key, semantic request fingerprint, stored response, expiry                    |
| `idempotency_usage`              | One per-user retained-record and stored-response-byte counter maintained transactionally                                  |
| `media_deletion_jobs`            | Exact immutable object key, due time, bounded retry state, terminal outcome, audit timestamps                             |
| `lifecycle_audit_events`         | Fixed account-deletion, provider-unlink, and media events, independent of deleted accounts and retained for 180 days      |
| `privacy_sweep_state`            | Durable cursor for bounded weekly media reconciliation                                                                    |
| `public_state`                   | Singleton durable discovery generation advanced with public-membership mutations                                          |
| `resume_view_days`               | Daily real and filtered view counts per resume; no personal data; 400 days; see viewer analytics                          |
| `resume_share_signal_days`       | Daily link-preview fetches per resume and platform; no personal data; 400 days                                            |
| `resume_view_events`             | Append-only consented or signed-in views with closed fields, 90 days; durations in `resume_view_durations`                |
| `view_consents`                  | Append-only viewer agree, decline, and withdraw records with notice version; 180 days                                     |
| `resume_viewers`                 | Per-resume signed-in viewer: provider subject, name, verified email; 90 days after the last view                          |
| `viewer_passes`                  | Hashed 7-day viewer pass bound to one resume and one viewer                                                               |

Runtime coordination tables, such as the write barrier, replica membership,
claims, rate buckets and publication transitions, are not part of the current
schema. Their design is kept in [the scaling contract](scaling/README.md) as
reference for a later fleet
([ADR 0038](../adr/0038-single-baseline-and-plain-migrator.md)).

Server-owned relational rows use PostgreSQL UUIDv7 defaults, except the two TOTP
tables described below. Client-generated UUIDs occur only inside resume
documents as entry identifiers.

`media_deletion_jobs` is cleanup state, not media ownership. A transaction that
removes a photo reference enqueues its validated exact key in the same commit.
The document reference remains the sole ownership authority; the ledger only
drives bounded physical deletion and records its outcome.

The four `oauth_*` tables are agent-authorization state and hold no secret
material. Code and token values exist only as 32-byte SHA-256 digests; PKCE
verifiers are never stored at all. Database constraints enforce the closed kind
and scope sets, expiry ordering, the single live grant per (user, client), and
the bounded redirect-URI count. Account deletion cascades through grants, codes,
and tokens. Expired codes and terminal tokens are removed in bounded batches,
and an idle client with no live grant and no live token is garbage-collected.

Second-factor rows cascade from their account. Pending tokens, recovery codes,
ceremony challenges, and enrollment tokens are stored only as digests; TOTP
secrets are stored only sealed. The
[passkey contract](passkey-second-factor-contract.md#postgresql-shape) and the
[authenticator-app contract](totp-second-factor-contract.md#postgresql-shape-and-bounds)
own the exact columns, constraints, cleanup, and grants. Portable export
excludes every second-factor table.

`public_state` has one checked singleton row and a positive monotonic
`discovery_generation`. A transaction that changes a resume slug, live state,
discovery eligibility, or deletes a public resume increments it with the same
commit. Go loads the committed generation before readiness and uses it for
aggregate discovery cache keys, entity tags, and the in-process response fence.
Aggregate discovery contains only a fixed heading and the slug-ordered eligible
public URLs; it has no other mutable resume field, so other document edits
cannot change its bytes.

Database constraints enforce global slug uniqueness and format, valid publish
flag combinations, and the three-resume cap. Resume creation also locks the
owner row so concurrent callers cannot race past the cap. Reserved slug checks,
document validation, and write policy run in the Go domain boundary.

`resumes.lng` is nullable draft metadata with a 35-character database bound. An
HTTP write treats null or `""` as unset. A non-empty value must be a well-formed
BCP 47 tag; the server canonicalizes it and checks the canonical form against
the bound before persisting. The read projection maps null, `""`, invalid, or
overlong legacy values to `und` and valid ones to their canonical form. API
responses and every render context use that projection, so the renderer never
receives an empty, invalid, or overlong root language.

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
| `project`     | `title`, `subtitle`, `link`, `dates`, `description`                               |
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
tested in both directions. The current release is document v4, and the server
accepts and emits v1 to v4. It declares three lossy emissions, and every other
value must remain equal:

- Emitting v1, v2, or v3 drops v4's `customization.header.photoPosition` and
  every project entry's `subtitle`
  ([ADR 0044](../adr/0044-header-photo-position-and-project-subtitle.md)).
- Emitting v1 or v2 drops v3's `personalDetails.details[].display` and
  `customization.font.textAlign`
  ([ADR 0041](../adr/0041-contact-link-display-and-body-justify.md)).
- Emitting v1 replaces a font ID that v1 cannot represent with the catalog
  entry's explicit v1 fallback.

Old-client mutations keep the stored font unless the operation explicitly
targets that field. They always keep the stored `textAlign`, and each surviving
detail's `display`, matched by its unique id. A v1 to v3 client write keeps the
stored `photoPosition` while both documents have a header, and each surviving
project entry's `subtitle`, matched by entry id. Retained types support
compatibility testing; HTTP delta application may remain generic so handlers do
not need one compiled code path per old version.
[ADR 0017](../adr/0017-resume-document-versioning.md) records this boundary. A
release that raises the document version cannot be rolled back once it has
stored the new version; see the
[production runbook](../runbooks/production.md#rollback).

## Schema and migrations

- `packages/schema/resume.schema.json` is the editable document-shape source.
- `packages/schema/released-versions.json` declares immutable releases.
- Code generation derives discriminators and entry definitions from the schema;
  conformance tests keep JSON Schema, TypeScript, and Go aligned
  ([ADR 0006](../adr/0006-schema-derived-codegen.md)).
- `apps/server/migrations/*.sql` is the sole relational schema source. Migration
  DDL is hand-written, applied by the embedded goose command, read by sqlc, and
  is append-only under the baseline marker
  ([ADR 0010](../adr/0010-goose-only-migrations.md),
  [ADR 0020](../adr/0020-uat-migration-baseline.md),
  [ADR 0038](../adr/0038-single-baseline-and-plain-migrator.md)).
- Generated artifacts are committed and changed only through their source and
  generator.

There is no separate declarative relational schema file. Reading migrations and
sqlc queries together gives the applied schema and typed access layer without a
second schema source that can drift.

`apps/server/migrations/.uat-baseline` freezes existing migrations. The
integration gate rejects changing the marker or any existing migration; only new
forward migrations may be added. Goose tracks applied versions, not file
checksums, at runtime.
