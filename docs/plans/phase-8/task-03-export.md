# Task 8.3 — Portable account export

**Owner:** Terra author. **Acceptance:** AC-PRIV-002. **Predecessor:** 8.1 HTTP
contract and 8.2 service scaffold. Read API/data/operations design, the phase
index and existing owner projection.

**Owned paths:** `apps/server/internal/accountapi/export*.go`,
`apps/server/sql/account_export.sql`. Coordinate with 8.2's service fields; do
not edit its files, generated code, migrations, or Git.

GET `/api/v1/me/export` is cookie-only, queryless and bodiless. It accepts no
resume-schema, conditional, or idempotency header. It requires a live session,
uses a separate 5/minute account-and-IP limiter, and emits exact
`Cache-Control: no-store, no-transform`, JSON, a fixed attachment name
`aboutme-export.json`, and the current resume schema response header.

The closed envelope is `{data:{exportVersion:1,exportedAt,account,resumes}}`.
Account fields are id, email, name, createdAt, updatedAt, linkedProviders. Each
resume contains id, title, slug, live, downloadEnabled, seoGeoEnabled, revision
(decimal string), schemaVersion, lng, createdAt, updatedAt, document, and photo.
Photo is null or `{mediaType,data,crop}`; data is standard base64 of the
normalized JPEG/PNG and crop is the original crop or null. Remove
document.photo's backend reference entirely; preserve every other owner field,
including hidden sections and private entries. Include incomplete drafts.

Read account and documents in one database snapshot, order resumes by
(created_at,id), project each to the current schema, and freeze bytes before
success. Get only validated owned media keys, cap each object at 2 MiB, and fail
if a referenced object is absent or invalid. The complete JSON is at most 12
MiB; reject overflow before headers. Bound the request at 20 seconds and each
photo read at five seconds. Cancellation closes the reader and joins cleanup.
Reject empty or truncated images and validate normalized dimensions before
decoding pixels. No partial archive or secret metadata.

- [ ] Write missing/foreign/expired auth and exact transport tests; observe red.
- [ ] Implement the export and projection.
- [ ] Test zero/three resumes, current/legacy schema, hidden/private content,
      incomplete draft, crop, max documents/photos, overflow, unknown MIME,
      truncated/missing object, reader close/error, canceled request and
      snapshot races.
- [ ] Seed sentinel credentials, provider subjects and keys; assert none appear
      in the output or logs. Test limit exhaustion and unsupported methods.
- [ ] Run `go test -race -count=1 ./internal/accountapi` from `apps/server`.

Report red/green evidence, changed paths, and required query generation.
