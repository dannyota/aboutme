# Plan-specific design decisions

The [Draft v4 design](../../design/README.md) fixes product and architecture
policy. This plan fixes the execution mechanisms needed to implement it. Each
remaining gap has one reviewable decision rather than an implied default.

D-numbers in this file are **P2B-local**. Decisions from the predecessor plans
are cited with their phase, e.g. `P2A D12`, `P3 D20`.

| #   | Gap in the spec                                                                                                                                                        | Decision made here                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| D1  | Several parallel lanes each add paths to one `docs/api/openapi.yaml`                                                                                                   | **Contract-first.** Task 1 lands the complete P2B surface (paths, schemas, parameters, examples, error shapes) and regenerates the TypeScript client **once**, before any handler exists. No later task edits the document; each adds a Go conformance test instead. A route task that finds the contract wrong stops and reports the exact diff; the integration owner lands it as a separate reviewed commit. Rejected: per-task contract diffs merged by the owner — that serializes the document behind every lane, re-runs the drift gate and client regeneration N times, and makes the client artifact a moving target for P4                                                                                                                                                                                                                                                                                                                                                                                                         |
| D2  | Six route lanes must register handlers on one mux without contending for one file                                                                                      | Task 4 registers **every** P2B route up front. Each route file begins as a construction-only `501 not_implemented` stub. Route tasks replace exactly one file's stub, so ownership stays disjoint and no task edits `cmd/server/main.go` after Task 4. `not_implemented` is not a shipped API code: a phase-head source and black-box gate fails if any stub, `501`, or `not_implemented` remains                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| D3  | The spec says the server declares accepted and emitted wire versions, but not how a request declares its own                                                           | An optional request header **`X-Resume-Schema-Version: <n>`**; absent means the server's `docmigrate.CurrentVersion`. Every JSON resume read and every mutation accepts it; the binary photo GET does not. Every mutation resolves it for its fingerprint, including bodyless deletes. A response containing resume data carries the emitted version; bodyless entry/photo deletes also carry it with their new parent ETag. Whole-resume delete and binary photo reads emit no schema-version header. An unsupported value is `400 unsupported_schema_version` with `details.acceptedVersions`. A header, not a body field: granular PATCH bodies are deltas, so no document root can carry it                                                                                                                                                                                                                                                                                                                                              |
| D4  | How a **delta** written by an old client reaches a current-version stored document (P2A's converters take whole canonical documents, not fragments)                    | **Down-emit → apply → up-accept.** Load the stored document (already projected to current by `Store.Get`), `EmitWire` it to the caller's declared version, apply the caller's delta at that version, `AcceptWire` it back to current v2, validate at v2, persist the **complete** document through the codec, and emit the response at the declared version. Production tests use the released v1↔v2 converters and immutable schemas. Synthetic projectors test only registry states that released data cannot represent, such as accept-only versus emit-only versions. The delta is applied **generically** (`map[string]any`), and the fragment is validated against the declared version's immutable schema from `schema.RawSchemas`                                                                                                                                                                                                                                                                                                    |
| D5  | Where rich-text sanitization runs                                                                                                                                      | Exactly once, in the kernel's persist helper, over the assembled document — not per handler. Task 5 owns `sanitize_doc.go` (the document walk); Task 4 owns the call site. They land as one wave unit because neither file compiles alone. One choke point means a new rich-text field cannot be added to a route that forgot to sanitize it, and the walk is schema-driven so a new rich-text field in the schema fails the walk's completeness test                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| D6  | The spec says "every write carries `If-Match`", but a create has no prior revision                                                                                     | `If-Match` is **required on every mutation except `POST /resumes`**; supplying it on create is `400 precondition_not_supported` (silently ignoring it would let a client believe it had a guarantee it does not have). Create's protection is `Idempotency-Key`, which **every** mutation requires without exception — missing or malformed is `400 idempotency_key_required` / `400 idempotency_key_invalid`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| D7  | The envelope is `{data}` XOR `{error:{code,message}}`, but a `412` must return the current revision and document, and a validation failure must list offending entries | Extend `components.schemas.Error` with an **optional `details` object**. This is additive and backward compatible (both existing required fields stay), and it is the **one** amendment P2B makes to a P0-frozen shape — flagged for explicit owner sign-off at Task 1's contract review. `412` carries `details: {revision, document}`; a document/bounds rejection carries `details: {issues: [{path, code, message}]}`; an unsupported wire version carries `details: {acceptedVersions}`. Rejected: a bare `{data}` body on an error status (breaks the envelope rule) and a second top-level key (breaks generated clients)                                                                                                                                                                                                                                                                                                                                                                                                             |
| D8  | No closed error-code vocabulary exists for resume routes                                                                                                               | One production list is defined in the kernel and enforced over every handler error call site. The list below is operation-specific in OpenAPI rather than copied onto every route. Middleware-owned generic codes remain defined and tested in `internal/api`. During W2 and W3 only, the route-stub registry permits the separate construction code `not_implemented`; the phase-head gate rejects that code and every `501` before acceptance                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| D9  | Rate limits are route-owned but need numbers                                                                                                                           | Three account-and-client-IP policies are adopted in `../budgets.md`: reads 600/min, writes 240/min, and photo uploads 20/h. The write ceiling clears several editors at the expected one-second autosave cadence without making the limiter inert                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| D10 | "S3 in prod, local in dev" — but the spec names no local mechanism, and the repo has none                                                                              | See [D10 in full](#d10--media-storage-in-a-local-first-repo) below                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| D11 | The schema bounds the photo key's characters but defines no key construction                                                                                           | The key is **server-derived and never client-supplied**: `resumes/{resumeID}/photo-{32 lowercase hex from crypto/rand}.{ext}`. `resumeID` is the canonical lowercase hyphenated UUID for the owning row; `ext` is exactly `jpg` or `png` from normalized output. One constructor and parser own this grammar. Before any read or delete, the parser proves the stored key is canonical and embeds the expected resume ID; a malformed or cross-resume key never reaches a backend. An input WebP is never stored as WebP. A client-supplied key would permit arbitrary or cross-tenant object access                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| D12 | Nothing says how a stored photo is read back                                                                                                                           | Owner reads stream through `GET /api/v1/resumes/{id}/photo` with authenticated-API `Cache-Control: no-store`, a key-derived strong ETag, and conditional 304 support. `If-None-Match` accepts one well-formed strong tag: an exact match is 304 and a different tag is 200; repeated, comma-folded, weak, or malformed values are `400 request_invalid`. Future public reads pass through Go after a live-state check and use the public revalidation policy. V1 has no direct public `/assets` route; object existence never grants access                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| D13 | Whether media needs its own table and how crashes are repaired                                                                                                         | **The document remains the sole media-ownership record; a durable deletion-job table records only cleanup work.** P2B migration 00006 adds that ledger beside bounded idempotency retention. Replacement or delete validates the exact old key, removes the reference, and enqueues `(resume_id, object_key)` in the same PostgreSQL transaction. API success may precede object deletion because every read is reference-gated. P8-priv drains due jobs toward the 24-hour target and records terminal outcomes; the bounded 48-hour orphan reconciliation repairs upload crashes and queue/accounting gaps. Rejected: no durable cleanup record, which cannot support the deletion target, and a second media-ownership table, which would compete with the document                                                                                                                                                                                                                                                                       |
| D14 | Upload transport                                                                                                                                                       | `multipart/form-data` with exactly one raw part named `file`. The upload route bypasses the buffering `BodyLimit` chain; it does not get a larger buffering override. After session, CSRF, route-rate, header, and heavy-work admission checks, the handler wraps the body in `http.MaxBytesReader` at 2,162,688 bytes and streams it with a 60-second read boundary. The parser separately caps file bytes at 2,097,152 and uses `NextRawPart`, never `ParseMultipartForm`; see [D14 in full](#d14--the-buffering-body-limit-must-not-read-photo-bytes)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| D15 | How a granular delta becomes a whole-document write                                                                                                                    | Read-modify-write inside one transaction, persisted through the codec — see [D15 in full](#d15--read-modify-write-in-one-transaction-through-the-codec) below                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| D16 | Whether OpenAPI restates the resume document's shape                                                                                                                   | **No.** `ResumeDocument` and its parts are objects governed by `packages/schema/resume.schema.json` at the version named by `X-Resume-Schema-Version`; the client's document types come from `packages/schema/gen/ts`. This document owns the envelope, headers, statuses, and error shapes. Restating a 24-section, byte-bounded schema in a second file would create two sources of truth for one contract, drifting silently — the exact failure mode `resume.schema.json` prevents                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| D17 | Whether resume language changes after create                                                                                                                           | `PATCH /api/v1/resumes/{id}` accepts both `title` and `lng`. The HTTP boundary maps null/empty to unset, parses non-empty input with `golang.org/x/text/language`, canonicalizes it, then rejects a canonical form over 35 characters before the full-aggregate CAS. Reads map null, empty, invalid legacy, or overlong canonical legacy data to `und`. Full localization remains deferred                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| D18 | How upload side effects preserve idempotency                                                                                                                           | Records are scoped by user, canonical operation identity, and key. The identity is method + registered operation + normalized concrete target parameters. Every mutation hashes a length-prefixed tuple of resolved wire version, parsed precondition or the create sentinel, other declared semantic inputs, and bounded raw JSON or file bytes. An upload hash excludes multipart framing, boundary text, part headers, and filename. Inspect a committed upload replay or key reuse before full decode. A fresh upload normalizes first and calls create-only storage. Only `PutCreated` proceeds to transactional `Execute`, under a five-minute create-to-commit deadline. A definite failed database commit best-effort deletes that candidate; an ambiguous database commit leaves it for the orphan sweep. `PutUnknown` stops before `Execute` and is not deleted because the key may name a collision winner. Concurrent proved-created candidates converge on one database winner, and only the referenced object survives cleanup |
| D19 | A 2 MiB file can decode far beyond the task memory limit and retain private metadata                                                                                   | See [D19 in full](#d19--decode-and-normalize-every-photo) below. Intake checks dimensions and pixels before full decode, rejects animation and invalid structure, applies Exif orientation, strips all metadata, and stores only a bounded canonical JPEG or PNG. One task-wide permit and the stage budgets contain memory, CPU, and slow uploads                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |

## Error-code vocabulary (D8)

Closed list. A new code is a reviewed decision, not an ad-hoc literal.

| Code                         | Status | Meaning                                                             |
| ---------------------------- | ------ | ------------------------------------------------------------------- |
| `session_required`           | 401    | Reused verbatim from `internal/auth` — never reinvented             |
| `csrf_rejected`              | 403    | Reused verbatim from `internal/auth`                                |
| `resume_not_found`           | 404    | Also the wrong-owner answer (P2A D17: no existence oracle)          |
| `resume_cap_exceeded`        | 409    | 4th resume; a domain conflict, not a precondition failure           |
| `idempotency_key_required`   | 400    | Missing `Idempotency-Key` on a mutation                             |
| `idempotency_key_invalid`    | 400    | Present but not a UUID                                              |
| `idempotency_key_reuse`      | 409    | Same key, different body — the spec's "rejected", a domain conflict |
| `precondition_required`      | 428    | Mutation with no `If-Match` where one is required                   |
| `precondition_malformed`     | 400    | `If-Match` present but not `"r<digits>"`                            |
| `precondition_not_supported` | 400    | `If-Match` supplied on `POST /resumes` (D6)                         |
| `revision_mismatch`          | 412    | Stale `If-Match`; `details` carries current revision + document     |
| `document_invalid`           | 422    | Schema/aggregate/bounds rejection; `details.issues[]`               |
| `request_invalid`            | 400    | Malformed JSON, unknown field (strict decode), missing path part    |
| `unsupported_schema_version` | 400    | `X-Resume-Schema-Version` not in the accepted set (D3)              |
| `customization_path_denied`  | 422    | Delta path outside the fixed allowlist (AC-SAVE-005)                |
| `media_type_unsupported`     | 415    | Sniffed content type outside the closed set                         |
| `media_too_large`            | 413    | Upload above the media budget                                       |
| `media_invalid`              | 422    | Recognized image is unsafe, malformed, or cannot normalize          |
| `media_busy`                 | 503    | Task-wide media permit was not acquired within one second           |
| `media_not_found`            | 404    | `GET`/`DELETE` photo with no photo on the document                  |

The shared router can also return its existing `invalid_client_ip`,
`bad_request`, `body_too_large`, `rate_limited`, `method_not_allowed`,
`not_found`, and `internal_error` codes. They remain owned by `internal/api`.
Task 1 assigns only the reachable subset to each operation. The photo streaming
path maps its own request and part overflow to `media_too_large`; it never emits
`body_too_large` for that overflow.

`not_implemented` at `501` belongs to a separate construction-only registry. It
is allowed only in Task 4 route stubs and is never added to OpenAPI or the
production list above. Each route task removes its own stub. Before W4 starts, a
source scan, the route-table test, and `TestNoRouteAnswers501` must prove that
no stub, `501`, or `not_implemented` remains. A missing route implementation
blocks the phase; it is not a documented runtime state.

`422` versus `400`: a syntactically well-formed request whose **content** the
domain rejects is `422`; a request the server cannot parse or whose headers are
wrong is `400`. `412` is only ever precondition staleness, and `409` only ever a
domain conflict — the distinction the spec's write-safety paragraph makes
explicitly, citing RFC 9110.

## D10 — Media storage in a local-first repo

The [deployment design](../../design/deployment.md#media) puts production media
in private S3 and reserves `internal/media` as the authorization boundary. The
repository has no object storage yet. Daily work uses the native stack against
one PostgreSQL container. The current `make dev` Compose deployment is suitable
for smoke and self-host checks, but it remains HTTP-only and cannot produce P9
authentication evidence. P9 owns an isolated HTTPS-on-443 UAT harness. AWS
wiring stays in PI.

**Decision.** `internal/media` defines one `Backend` interface with two
implementations behind identical conformance tests:

| Backend | Used by                          | Mechanism                                                                                     |
| ------- | -------------------------------- | --------------------------------------------------------------------------------------------- |
| `fs`    | native dev, all unit tests       | Rooted filesystem store; no container, no credentials; refuses any key escaping its root      |
| `s3`    | compose/UAT, staging, production | AWS SDK for Go v2 `s3` client with a configurable endpoint, region, and path-style addressing |

The **local S3-compatible service is MinIO**, pinned by exact tag and fully
qualified (`docker.io/minio/minio:<pinned>`) like every other image in
`compose.yml`, with a one-shot `docker.io/minio/mc:<pinned>` container that
creates the bucket and exits — the same one-shot pattern the `migrate` service
already establishes. It is added as a compose service **and** as a standalone
`make test-s3-up` / `test-s3-down` target so the S3 conformance suite can run
natively without booting the whole UAT stack. MinIO is dev/self-host tooling
only; nothing in `apps/server` depends on MinIO specifically, only on the S3
API. PI leaves endpoint and static credentials unset and uses the ECS task-role
default credential chain; local Compose supplies a custom endpoint, path-style
mode, and one complete disposable static-key pair. No code changes between
modes.

Two backends risk under-testing the production path, so the mitigation is
mechanical: **one conformance suite, two backends**, and the S3 run is
fail-closed at the phase gate through `REQUIRE_TEST_S3=1` — exactly the
skip-or-fail-closed shape `testutil.RequireMigratedTestDatabaseURL` already uses
for Postgres, so a gate run can never pass vacuously by skipping.

The standalone test contract is fixed before Task 3 starts:

| Item        | Contract                                                                                                                                                                                                                                |
| ----------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Service     | One rootless Podman container named `aboutme-test-s3`, bound only to `127.0.0.1:20091`; its image and the matching `mc` image are pinned exactly by Task 3                                                                              |
| State       | `make test-s3-up` is idempotent, creates the private `aboutme-test` bucket, writes generated disposable credentials to git-excluded `.dev/test-s3.env` with mode `0600`, waits for the host-side endpoint, and never prints credentials |
| Variables   | The target file supplies `TEST_S3_ENDPOINT`, `TEST_S3_REGION`, `TEST_S3_BUCKET`, `TEST_S3_ACCESS_KEY_ID`, `TEST_S3_SECRET_ACCESS_KEY`, and `TEST_S3_FORCE_PATH_STYLE=true`                                                              |
| Gate        | `make server-test-s3` requires the service and credential file, sets `REQUIRE_TEST_S3=1`, and runs the shared `internal/media` suite. Missing state fails; it never skips                                                               |
| HTTP parity | `make server-test-p2b-s3` requires the shared database and test S3 service, then runs the `internal/resumeapi` lifecycle suite against the S3 backend                                                                                   |
| Teardown    | `make test-s3-down` removes only `aboutme-test-s3` and its disposable credential file. The integration owner runs it after checking no media suite is active                                                                            |

The Compose service uses deployment `MEDIA_*` variables from `.env` with no
default credentials. The standalone `TEST_S3_*` values are test-only and never
fall back to deployment values.

Rejected alternatives: **LocalStack** (a whole AWS emulator for one bucket;
heavier image, slower start); **filesystem only in dev** (leaves the SDK code
path — signing, path-style addressing, error mapping — completely unexercised
until AWS, which is precisely the class of defect that surfaces at 3 a.m. in
PI); **a public-read bucket served through a new Caddy `/assets` route** (adds a
row the spec's authoritative route table does not have, and makes every
unpublished resume's photo world-readable by URL).

## D14 — The buffering body limit must not read photo bytes

`api.New` wraps ordinary routes in `BodyLimit(opts.BodyLimitBytes)`. That
middleware calls `io.ReadAll`, so raising its ceiling for a photo would buffer
the complete multipart request before authentication and heavy-work admission.
An inner middleware also cannot raise its outer `http.MaxBytesReader` ceiling.

Task 4 adds a third path-dispatched chain for exactly
`POST /api/v1/resumes/{id}/photo`. It keeps RequestID, SecurityHeaders, Logging,
NoStoreCache, and the outer client-IP limiter, but omits `BodyLimit`. The
route-owned chain then runs, in order: `RequireSession`, multipart-aware
`RequireCSRF`, the account-and-client-IP upload limiter, validation of media
type, boundary, idempotency, precondition, wire version, target, and declared
length, then the task-wide heavy-work permit. A declared length over 2,162,688
bytes is `media_too_large`. None of those stages reads `r.Body`.

After admission, the handler sets the 60-second server read boundary and wraps
`r.Body` with `http.MaxBytesReader(w, r.Body, 2162688)`, and streams the
multipart reader. It uses `NextRawPart`, limits the raw file part to 2,097,152
bytes, rejects transfer encodings, extra parts, non-empty epilogues, and
filenames over 255 UTF-8 bytes, and never calls `ParseMultipartForm`. A declared
or observed request overflow, including a wrapped `*http.MaxBytesError`, and a
file-part byte at limit+1 map to `413 media_too_large`, not the ordinary
`body_too_large` code. Timeout or client cancellation stops the streaming read
and releases the permit.

This is called out prominently because it is the one place where P2B must modify
shared `internal/api` code, and because getting it wrong in the other direction
— raising the global limit — would hand every JSON route a 2 MiB buffering
budget.

## D15 — Read-modify-write, in one transaction, through the codec

P2A D12 binds every write path to persist the **full** document through the
codec; a `jsonb_set`-style granular write could reintroduce an old document
shape where the backfill CAS cannot see it. So every granular endpoint is
read-modify-write:

1. Inside the idempotency `Execute` callback's transaction, read the resume
   scoped by `id + user_id` (P2A D17).
2. Apply the delta to the in-memory `schema.Resume`.
3. Sanitize (D5), validate at current version, enforce bounds.
4. CAS-write the whole document at the caller's `If-Match` revision.

A concurrent writer landing between the read and the CAS makes the CAS miss, so
the caller gets `412` with the winning document — the same guarantee a
`SELECT … FOR UPDATE` would give, without holding a row lock across validation.
No new sqlc query is needed for any of it.

## D18 — Idempotency identifies one concrete mutation

The record's operation scope is the lowercase SHA-256 hex digest of ADR 0016's
binary tuple. Its fields alternate name and value: `method`, uppercase method;
`operation`, the exact OpenAPI `operationId`; then each registered target name
and canonical value in route order. UUIDs use lowercase hyphenated form. Decoded
section keys use their validated UTF-8 bytes without Unicode normalization.
Alternate URL escapes therefore cannot invent a different target. The existing
`route` column stores this digest; no schema change is needed for the naming
correction.

The request hash uses the same encoding with ADR 0016's request domain. Its
fields alternate name and value in this order:

1. `wire_version`, then the resolved unsigned decimal value with no leading
   zero;
2. `if_match`, then the parsed unsigned decimal revision, or `absent` for
   `POST /resumes`;
3. each registered semantic-input name and canonical value in declared order;
4. `payload`, then the exact bounded raw JSON body or raw `file` part bytes.

Malformed headers and targets fail before inspection. Multipart boundaries, part
headers, upload filenames, and `Content-Type` are transport metadata and are
excluded. JSON whitespace remains part of the bounded body bytes. The crop PATCH
hashes its exact bounded JSON bytes. Each bodyless DELETE hashes one zero-length
`payload` field whether its optional singleton JSON `Content-Type` is absent or
present. Any nonempty DELETE body is rejected before inspection. Therefore a
replay must name the same operation scope, wire contract, precondition, and
payload. Within one user/operation/key scope, changing a fingerprint field
produces `409 idempotency_key_reuse`.

The tuple encoder writes the UTF-8 domain, `00`, a four-byte unsigned big-endian
field count, then each field's four-byte unsigned big-endian length and bytes.
These vectors are immutable tests:

| Tuple            | Fields                                                                                                                                                                              | SHA-256                                                            |
| ---------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| operation        | `method`, `PATCH`, `operation`, `upsertResumeEntry`, `resume_id`, `01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f`, `section_key`, `work`, `entry_id`, `01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60` | `cb58dbc9b27ce8bcb0b3944e1728b5bd64f7a73dfc02a7a509ea6dbd9a74a4f6` |
| request          | `wire_version`, `2`, `if_match`, `42`, `payload`, exact UTF-8 `{"entry":{"id":"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60","title":"Engineer"}}`                                          | `a2a62948b5a388770183b9432076c79e9c78f0bfbd1d4c29359d3b882949bf5d` |
| create operation | `method`, `POST`, `operation`, `createResume`                                                                                                                                       | `6c2be02042f6fffe1a4cd202618012e1c3007fa64dc9a86f8630f084560e341f` |
| create request   | `wire_version`, `2`, `if_match`, `absent`, `payload`, exact UTF-8 `{}`                                                                                                              | `5f271718e815f9a55c7f1a4dae30f9ebdc196ff0cce7a7b156febc260d9de745` |
| photo operation  | `method`, `POST`, `operation`, `uploadResumePhoto`, `resume_id`, `01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f`                                                                             | `96a20fbd9cb1b9e2b61e27629c8672d2ccb5a96c7f44d9ae6db741c5fd8094ac` |
| photo request    | `wire_version`, `2`, `if_match`, `42`, `payload`, exact bytes `00 ff 10 0a`                                                                                                         | `126d8283725a991ab0814e10874b9c2a52f287ffc71418708364848108fcbb63` |

## D19 — Decode and normalize every photo

The compressed-byte limit alone does not bound decoded memory or remove private
metadata. Intake therefore has one ordered boundary:

1. Authenticate, validate CSRF and Origin, apply the upload rate limit, and
   validate request headers before reading the body.
2. Acquire the one task-wide photo permit. Wait at most one second. Failure is
   `503 media_busy` with `Retry-After: 1` and an unread body. Hold the permit
   until no decoder, pixel buffer, or object writer from the request remains.
3. Read for at most 60 seconds through the request and part byte limits. Accept
   exactly one raw `file` part. Hash its exact bytes, excluding multipart
   framing and filename, then inspect committed idempotency replay or reuse
   before image decode.
4. Select JPEG, PNG, or WebP by decoder. Run `DecodeConfig` and an independent
   container scan before full decode. Each source edge must be in `1..8192` and
   the overflow-safe pixel product must be at most 16,777,216. Reject APNG,
   animated WebP, malformed lengths, truncation, inconsistent dimensions, and
   bytes after JPEG EOI, PNG IEND, or the WebP RIFF boundary.
5. Fully decode and confirm the same bounds. Accept no orientation tag as
   orientation 1. Apply one valid Exif orientation in `1..8`; malformed,
   duplicate, or conflicting orientation metadata is `media_invalid`.
6. Convert decoded samples to 8-bit non-premultiplied RGBA and treat them as
   sRGB. Drop all source Exif/GPS, XMP, IPTC, comments, thumbnails, ICC
   profiles, and optional chunks. Never copy a source container into output.
7. Apply orientation, then downscale with `golang.org/x/image/draw.CatmullRom`
   without upscaling. Opaque output is baseline 4:2:0 JPEG at quality 85 and
   starts at a 2,048-pixel maximum edge. Alpha output is non-interlaced PNG with
   fixed `png.BestSpeed` compression and starts at a 1,024-pixel maximum edge.
8. If output exceeds 2,097,152 bytes, retry opaque images at maximum edges
   `1792, 1536, 1280, 1024, 768, 512` or alpha images at `896, 768, 640, 512`,
   skipping any step that would upscale. Reject when the 512-pixel result still
   exceeds the limit.
9. Treat five seconds as the measured admission ceiling for normalization, not
   as a request-time cancellation promise the decoders cannot honor. Before Task
   11 dispatch, a fail-closed benchmark runs every accepted boundary and
   regression fixture with the pinned toolchain on the declared local
   controlled-cgroup profile; any normalization over five seconds blocks P2B.
   P9A repeats the same manifest on the selected production ARM64 Graviton task,
   where a failure blocks launch. The request path runs the decoder
   synchronously and records elapsed time, but does not enforce the release
   threshold as a cancellation timer. It never starts detached decode work or
   returns while decoder work remains. Object `Put` has its own real five-second
   context deadline. The resource gate measures a maximum 192 MiB
   resident-set-size increase for the complete intake path.

Unrecognized containers return `415 media_type_unsupported`. A recognized image
that fails structure, animation, dimensions, orientation, trailing-data, or
normalization checks returns `422 media_invalid`. `error.details.reason` uses
only `malformed`, `animated`, `dimensions`, `orientation`, `trailing_data`, or
`normalization_failed`; decoder text never reaches a client or log. Oversize
request or file bytes return `413 media_too_large`. Every rejection in this
intake and normalization matrix occurs before object-write dispatch and leaves
PostgreSQL and object storage unchanged. D18 owns the separate remote
`PutUnknown` residual.
