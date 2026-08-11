# Design decisions this plan makes beyond the spec

The spec fixes the resume API's **policy** (endpoints, envelope, write safety,
bounds) but leaves mechanism open. Each gap gets an explicit decision here,
flagged for independent review — never a TODO.

D-numbers in this file are **P2B-local**. Decisions from the predecessor plans
are cited with their phase, e.g. `P2A D12`, `P3 D20`.

| #   | Gap in the spec                                                                                                                                                        | Decision made here                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| D1  | Several parallel lanes each add paths to one `docs/api/openapi.yaml`                                                                                                   | **Contract-first.** Task 1 lands the complete P2B surface (paths, schemas, parameters, examples, error shapes) and regenerates the TypeScript client **once**, before any handler exists. No later task edits the document; each adds a Go conformance test instead. A route task that finds the contract wrong stops and reports the exact diff; the integration owner lands it as a separate reviewed commit. Rejected: per-task contract diffs merged by the owner — that serializes the document behind every lane, re-runs the drift gate and client regeneration N times, and makes the client artifact a moving target for P4                                                                                                                                                                                                                                   |
| D2  | Six route lanes must register handlers on one mux without contending for one file                                                                                      | Task 4 registers **every** P2B route up front, each pointing at a stub in its own file that returns `501 not_implemented`, with a test asserting the full route table exists and every stub 501s. Route tasks then replace exactly one file's stub. Ownership stays disjoint, the route table is complete and tested from wave 2, and no task edits `cmd/server/main.go` after Task 4                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| D3  | The spec says the server declares accepted and emitted wire versions, but not how a request declares its own                                                           | An optional request header **`X-Resume-Schema-Version: <n>`**; absent means the server's `docmigrate.CurrentVersion`. Every response carries the same header naming the version its body is emitted at. An undeclared/unsupported value is `400 unsupported_schema_version`, listing accepted versions in `details`. A header, not a body field: the granular PATCH bodies are deltas, not documents, so there is no document root to carry `schemaVersion`, and this matches how `If-Match` and `Idempotency-Key` already travel                                                                                                                                                                                                                                                                                                                                      |
| D4  | How a **delta** written by an old client reaches a current-version stored document (P2A's converters take whole canonical documents, not fragments)                    | **Down-emit → apply → up-accept.** Load the stored document (already projected to current by `Store.Get`), `EmitWire` it down to the caller's declared version, apply the caller's delta at that version, `AcceptWire` back up to current, validate at current, persist the **complete** document through the codec, and emit the response at the caller's declared version. With one released version every step is the identity path. The delta is applied **generically** (`map[string]any`) so no typed struct for a non-current version is ever needed (P2A D13: typed structs exist only for the current version), and the request fragment itself is validated against the **declared version's immutable schema** from `schema.RawSchemas` rather than a Go struct. This uses both converter directions P2A already built and tested, and invents no new route |
| D5  | Where rich-text sanitization runs                                                                                                                                      | Exactly once, in the kernel's persist helper, over the assembled document — not per handler. Task 5 owns `sanitize_doc.go` (the document walk); Task 4 owns the call site. They land as one wave unit because neither file compiles alone. One choke point means a new rich-text field cannot be added to a route that forgot to sanitize it, and the walk is schema-driven so a new rich-text field in the schema fails the walk's completeness test                                                                                                                                                                                                                                                                                                                                                                                                                  |
| D6  | The spec says "every write carries `If-Match`", but a create has no prior revision                                                                                     | `If-Match` is **required on every mutation except `POST /resumes`**; supplying it on create is `400 precondition_not_supported` (silently ignoring it would let a client believe it had a guarantee it does not have). Create's protection is `Idempotency-Key`, which **every** mutation requires without exception — missing or malformed is `400 idempotency_key_required` / `400 idempotency_key_invalid`                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| D7  | The envelope is `{data}` XOR `{error:{code,message}}`, but a `412` must return the current revision and document, and a validation failure must list offending entries | Extend `components.schemas.Error` with an **optional `details` object**. This is additive and backward compatible (both existing required fields stay), and it is the **one** amendment P2B makes to a P0-frozen shape — flagged for explicit owner sign-off at Task 1's contract review. `412` carries `details: {revision, document}`; a document/bounds rejection carries `details: {issues: [{path, code, message}]}`; an unsupported wire version carries `details: {acceptedVersions}`. Rejected: a bare `{data}` body on an error status (breaks the envelope rule) and a second top-level key (breaks generated clients)                                                                                                                                                                                                                                       |
| D8  | No closed error-code vocabulary exists for resume routes                                                                                                               | One closed list, defined once in the kernel, asserted by a test that fails when a handler writes a code outside it (the precedent is `internal/auth`'s documented closed vocabulary). See the table below. Reusing `internal/api`'s generic codes (`internal_error`, `method_not_allowed`, `body_too_large`) is explicit, not accidental, and those are documented at their own definition                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| D9  | Rate limits are "per route, owned by the owning phase" but no numbers exist for resume routes                                                                          | Three policies, keyed by **account + client IP composite** (the middleware already supports a `KeyFunc`): reads, writes, and media upload. The numbers are budget rows the **owner lands in `../budgets.md` before dispatch** (the same treatment P2A's title/`lng`/TTL numbers got), not constants a worker invents. Proposal to ratify: reads 600/min, writes 240/min (a ~1 s autosave debounce sustains ~60/min per open editor, so the ceiling must clear a multi-tab editing session), media upload 20/h                                                                                                                                                                                                                                                                                                                                                          |
| D10 | "S3 in prod, local in dev" — but the spec names no local mechanism, and the repo has none                                                                              | See [D10 in full](#d10--media-storage-in-a-local-first-repo) below                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| D11 | The schema bounds the photo key's characters but defines no key construction                                                                                           | The key is **server-derived and never client-supplied**: `resumes/{resumeID}/photo-{32 lowercase hex from crypto/rand}.{ext}`, where `ext` comes from the **sniffed** content type restricted to `jpg`/`png`/`webp`. It satisfies the schema's pattern (first character alphanumeric, allowed set only), contains no `..`, and its random component makes an object key unguessable from a resume id. A client-supplied key would be an arbitrary-object-write and cross-tenant-overwrite primitive                                                                                                                                                                                                                                                                                                                                                                    |
| D12 | Nothing says how a stored photo is read back                                                                                                                           | `GET /api/v1/resumes/{id}/photo` streams the object for the **owner only**, with `Cache-Control: private, no-cache, must-revalidate` plus a strong `ETag` derived from the object key (the key is immutable per upload, so the ETag is stable and a `304` is cheap). The public/CDN `/assets` path the spec's §6 deployment row implies is **not** built here — it belongs with the public read surface (P5A) and the CloudFront behavior matrix (PI). Note that the spec's own authoritative §2 route table has no `/assets` row: open question Q2                                                                                                                                                                                                                                                                                                                    |
| D13 | Whether media needs its own table                                                                                                                                      | **No new migration.** The key lives in the document; object lifetime is derived from it. Replace deletes the previous key read inside the same request; resume delete sweeps the `resumes/{resumeID}/` prefix. A crash between object write and document commit can orphan one object, which the prefix sweep collects at resume delete; that residual is accepted and recorded. Rejected: a `media_objects` table — it buys exact GC at the cost of a serialized migration, a second write path, and its own consistency problem                                                                                                                                                                                                                                                                                                                                      |
| D14 | Upload transport                                                                                                                                                       | `multipart/form-data` with exactly one part named `file`, per `../phase-1-deferred.md`'s forward-binding decision. The router gets a **per-path body-limit override** for this one route, because the global 256 KB `BodyLimit` sits outside the mux and an inner middleware cannot relax it — see [D14 in full](#d14--the-global-body-limit-blocks-photo-upload) below                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| D15 | How a granular delta becomes a whole-document write                                                                                                                    | Read-modify-write inside one transaction, persisted through the codec — see [D15 in full](#d15--read-modify-write-in-one-transaction-through-the-codec) below                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| D16 | Whether OpenAPI restates the resume document's shape                                                                                                                   | **No.** `ResumeDocument` and its parts are objects governed by `packages/schema/resume.schema.json` at the version named by `X-Resume-Schema-Version`; the client's document types come from `packages/schema/gen/ts`. This document owns the envelope, headers, statuses, and error shapes. Restating a 24-section, byte-bounded schema in a second file would create two sources of truth for one contract, drifting silently — the exact failure mode `resume.schema.json` prevents                                                                                                                                                                                                                                                                                                                                                                                 |

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
| `media_not_found`            | 404    | `GET`/`DELETE` photo with no photo on the document                  |

`422` versus `400`: a syntactically well-formed request whose **content** the
domain rejects is `422`; a request the server cannot parse or whose headers are
wrong is `400`. `412` is only ever precondition staleness, and `409` only ever a
domain conflict — the distinction the spec's write-safety paragraph makes
explicitly, citing RFC 9110.

## D10 — Media storage in a local-first repo

The spec's deployment row says "Media/avatars: **S3 (ap-southeast-1)** behind
CloudFront `/assets`", and the folder map reserves `internal/media/`. The repo
has **no** object storage of any kind today: `deploy/compose.yml` runs postgres,
migrate, server, web, and caddy, and no `S3`/`MINIO`/`MEDIA` configuration
exists in `.env.example` or `internal/config`. The owner's rule is that
everything runs locally until the product is done, and AWS wiring stays in PI.
`make dev` is now UAT-only (heavyweight compose); daily work runs the native
stack against one Postgres container.

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
API, so PI swaps the endpoint and credentials and changes no code.

Two backends risk under-testing the production path, so the mitigation is
mechanical: **one conformance suite, two backends**, and the S3 run is
fail-closed at the phase gate through `REQUIRE_TEST_S3=1` — exactly the
skip-or-fail-closed shape `testutil.RequireMigratedTestDatabaseURL` already uses
for Postgres, so a gate run can never pass vacuously by skipping.

Rejected alternatives: **LocalStack** (a whole AWS emulator for one bucket;
heavier image, slower start); **filesystem only in dev** (leaves the SDK code
path — signing, path-style addressing, error mapping — completely unexercised
until AWS, which is precisely the class of defect that surfaces at 3 a.m. in
PI); **a public-read bucket served through a new Caddy `/assets` route** (adds a
row the spec's authoritative route table does not have, and makes every
unpublished resume's photo world-readable by URL).

## D14 — The global body limit blocks photo upload

`api.New` wraps the mux in `BodyLimit(opts.BodyLimitBytes)` with
`DefaultBodyLimitBytes = 256 KB`, and health paths already get their own smaller
chain by path dispatch **outside** the mux. An inner middleware cannot raise an
outer `http.MaxBytesReader` ceiling, so a 2 MiB photo cannot be uploaded without
touching the router. Task 4 therefore adds a **third path-dispatched chain** for
the photo upload path, mirroring the existing health-path special case exactly:
same outer middleware (RequestID, SecurityHeaders, Logging, NoStoreCache,
RateLimit), a larger `BodyLimit` for that path only, and the session/CSRF chain
inside. The larger number is a budget row the owner lands, not a worker
constant.

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
