# Task 1: The whole P2B OpenAPI surface, contract-first

Implements [D1](decisions.md) (contract-first), [D3](decisions.md) (the wire-
version header), [D7](decisions.md) (the `Error.details` amendment), and
[D16](decisions.md) (the document stays opaque in OpenAPI). This task ships **no
Go code**: it lands the contract every later task implements against. The
integration owner regenerates the TypeScript client exactly once from the
accepted source.

**Tier:** Normal (contract + docs; it ships no enforcement path). It is
nonetheless a **fan-out gate**: waves 2 and 3 do not dispatch until the
integration owner has reviewed this diff, because every route task is written
against it.

**Files:** modify `docs/api/openapi.yaml`; create
`docs/api/test/resumes.contract.test.ts`. The generated client is an
integration-owner path, not a task-author path.

## Surface to add

Fifteen operations across nine paths, all under the existing
`https://aboutme.vn/api/v1` server, all in a new `resumes` tag (the photo
operations additionally in a new `media` tag).

| Path                                           | Methods                          | Notes                                                                  |
| ---------------------------------------------- | -------------------------------- | ---------------------------------------------------------------------- |
| `/resumes`                                     | `GET`, `POST`                    | `POST` takes `Idempotency-Key` and **no** `If-Match` (D6)              |
| `/resumes/{id}`                                | `GET`, `PATCH`, `DELETE`         | `PATCH` body is `{title?, lng?}`; `DELETE` → `204`, no body            |
| `/resumes/{id}/entries/{sectionKey}`           | `PATCH`                          | Upsert ONE entry; identity is `entry.id` in the body                   |
| `/resumes/{id}/entries/{sectionKey}/{entryId}` | `DELETE`                         | → `204`                                                                |
| `/resumes/{id}/sections/{sectionKey}`          | `PATCH`                          | `displayName`, `iconKey`, `entryOrder` — content only, never placement |
| `/resumes/{id}/structure`                      | `PATCH`                          | The only create/delete/move/reorder-section endpoint                   |
| `/resumes/{id}/personal-details`               | `PATCH`                          | Whole object                                                           |
| `/resumes/{id}/customization`                  | `PATCH`                          | Ordered `{op:set,path,value}` / `{op:unset,path}` union                |
| `/resumes/{id}/photo`                          | `POST`, `GET`, `PATCH`, `DELETE` | Upload, binary read, crop-only JSON mutation, and delete               |

## Interfaces this task fixes for every later task

- **Components (new):** `ResumeSummary` (id, title, revision, `live`, `slug`,
  `schemaVersion`, timestamps — no document), `Resume` (summary + `document`),
  `ResumeDocument`, `CustomizationDelta`, `StructureCommand`, `EntryUpsert`,
  `SectionPatch`, `PersonalDetailsPatch`, and `PhotoCropPatch`.
  `PersonalDetailsPatch` is the versioned whole client-owned object but has a
  JSON Schema `not: {required: [photo]}` guard, so OpenAPI validation rejects
  the server-owned field without restating the other personal-detail fields.
  `PhotoCropPatch` is a separate command whose `crop` points to the schema-owned
  `$defs.photoCrop`; it does not restate or accept `PhotoRef`.
- **Components (amended):** `Error` gains an optional `details` object (D7).
  Both existing required fields stay required; no other frozen shape changes.
- **Parameters (new):** `ResumeID`, `SectionKey`, `EntryID`,
  `SchemaVersionHeader` (`X-Resume-Schema-Version`, optional,
  integer-as-string), and `IfNoneMatch` (one optional strong object tag on photo
  GET). The schema-version parameter appears on every JSON resume read and every
  mutation, including all three deletes; it does not appear on the binary photo
  GET. `IfMatch` and `IdempotencyKey` are **reused verbatim** from `components`
  — never redefined.
- **Ordered-operation bounds:** both `StructureCommand[]` and
  `CustomizationDelta[]` have `maxItems: 100`, sourced from
  [the numeric budgets](../budgets.md), with valid 100-item and rejected
  101-item examples/tests. The 256 KiB request limit remains independent.
- **Structure indexes:** every `createSection.index` and `moveSection.index` is
  a zero-based integer insertion position with `minimum: 0`. Commands are
  evaluated in list order against the result of prior commands. A create into a
  target column of length `N` accepts `0..N`. A move removes the source key
  first, then accepts `0..N` against the resulting target column; this also
  governs a same-column move. `N` appends. An integer outside the current bound
  is `422 document_invalid`; a non-integer is `400 request_invalid`.
- **Security:** every operation declares `sessionCookie`; every mutating
  operation additionally declares `csrfToken` (AND, not OR), matching the
  existing auth operations.
- **Operation identity:** every operation has one unique, frozen `operationId`.
  The contract test pins the complete set. It includes `createResume`,
  `upsertResumeEntry`, and `uploadResumePhoto` exactly because D18's immutable
  digest vectors use those strings.

## Operation-specific response matrix

OpenAPI declares only responses reachable for that operation. Shared boundary
errors are still written on each affected operation; they are not copied from a
blanket list that gives safe reads CSRF or precondition failures.

Every authenticated response has `Cache-Control: no-store` and `X-Request-ID`.
Those request-scoped headers are not stored for replay. This is the exact
success contract:

| Operation                                                                                    | Status and body                                         | Additional headers                                                                            |
| -------------------------------------------------------------------------------------------- | ------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `GET /resumes`                                                                               | `200 application/json`, `{data: ResumeSummary[]}`       | `X-Resume-Schema-Version`; no `ETag` or `Location`                                            |
| `POST /resumes`                                                                              | `201 application/json`, `{data: Resume}`                | canonical relative `Location: /api/v1/resumes/{id}`; parent `ETag`; `X-Resume-Schema-Version` |
| `GET` or `PATCH /resumes/{id}`                                                               | `200 application/json`, `{data: Resume}`                | parent `ETag`; `X-Resume-Schema-Version`; no `Location`                                       |
| `DELETE /resumes/{id}`                                                                       | `204`, zero bytes                                       | no `Content-Type`, `ETag`, `Location`, or `X-Resume-Schema-Version`                           |
| Entry upsert, section patch, structure patch, personal-details patch, or customization patch | `200 application/json`, `{data: Resume}`                | new parent `ETag`; `X-Resume-Schema-Version`; no `Location`                                   |
| Entry delete                                                                                 | `204`, zero bytes                                       | new parent `ETag`; `X-Resume-Schema-Version`; no `Content-Type` or `Location`                 |
| `POST /resumes/{id}/photo`                                                                   | `200 application/json`, `{data: Resume}`                | new parent `ETag`; `X-Resume-Schema-Version`; no `Location`                                   |
| `PATCH /resumes/{id}/photo`                                                                  | `200 application/json`, `{data: Resume}`                | new parent `ETag`; `X-Resume-Schema-Version`; no `Location`                                   |
| `GET /resumes/{id}/photo`                                                                    | `200 image/jpeg` or `image/png`, exact normalized bytes | object `ETag`; no `Location` or schema-version header                                         |
| Conditional `GET /resumes/{id}/photo`                                                        | `304`, zero bytes                                       | same object `ETag`; no `Content-Type`, `Location`, or schema-version header                   |
| `DELETE /resumes/{id}/photo`                                                                 | `204`, zero bytes                                       | new parent `ETag`; `X-Resume-Schema-Version`; no `Content-Type` or `Location`                 |

A parent `ETag` is exactly `"r<revision>"` for the revision in the response or
the new revision after a bodyless child deletion. The object `ETag` is the
quoted strong digest derived from the immutable normalized object key. A
response whose JSON body contains resume data, including a `412` winning
document, carries the emitted `X-Resume-Schema-Version`. No other success or
error response invents one.

| Operations                                                               | Success      | Error statuses and codes                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| ------------------------------------------------------------------------ | ------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /resumes`                                                           | `200`        | `400 invalid_client_ip\|unsupported_schema_version`; `401 session_required`; `429 rate_limited`; `500 internal_error`                                                                                                                                                                                                                                                                                                                                           |
| `POST /resumes`                                                          | `201`        | `400 invalid_client_ip\|idempotency_key_required\|idempotency_key_invalid\|precondition_not_supported\|request_invalid\|unsupported_schema_version`; `401 session_required`; `403 csrf_rejected`; `409 resume_cap_exceeded\|idempotency_key_reuse`; `413 body_too_large`; `422 document_invalid`; `429 rate_limited`; `500 internal_error`                                                                                                                      |
| `GET /resumes/{id}`                                                      | `200`        | `400 invalid_client_ip\|request_invalid\|unsupported_schema_version`; `401 session_required`; `404 resume_not_found`; `429 rate_limited`; `500 internal_error`                                                                                                                                                                                                                                                                                                  |
| `PATCH /resumes/{id}`                                                    | `200`        | `400 invalid_client_ip\|idempotency_key_required\|idempotency_key_invalid\|precondition_malformed\|request_invalid\|unsupported_schema_version`; `401 session_required`; `403 csrf_rejected`; `404 resume_not_found`; `409 idempotency_key_reuse`; `412 revision_mismatch`; `413 body_too_large`; `422 document_invalid`; `428 precondition_required`; `429 rate_limited`; `500 internal_error`                                                                 |
| `DELETE /resumes/{id}`                                                   | `204`        | `400 invalid_client_ip\|idempotency_key_required\|idempotency_key_invalid\|precondition_malformed\|request_invalid\|unsupported_schema_version`; `401 session_required`; `403 csrf_rejected`; `404 resume_not_found`; `409 idempotency_key_reuse`; `412 revision_mismatch`; `413 body_too_large`; `428 precondition_required`; `429 rate_limited`; `500 internal_error`                                                                                         |
| Entry upsert, section patch, structure patch, and personal-details patch | `200`        | Same as item `PATCH`, including `422 document_invalid`                                                                                                                                                                                                                                                                                                                                                                                                          |
| Entry delete                                                             | `204`        | Same as item `DELETE`                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| Customization patch                                                      | `200`        | Same as item `PATCH`, plus `422 customization_path_denied`                                                                                                                                                                                                                                                                                                                                                                                                      |
| `POST /resumes/{id}/photo`                                               | `200`        | `400 invalid_client_ip\|idempotency_key_required\|idempotency_key_invalid\|precondition_malformed\|request_invalid\|unsupported_schema_version`; `401 session_required`; `403 csrf_rejected`; `404 resume_not_found`; `409 idempotency_key_reuse`; `412 revision_mismatch`; `413 media_too_large`; `415 media_type_unsupported`; `422 media_invalid\|document_invalid`; `428 precondition_required`; `429 rate_limited`; `503 media_busy`; `500 internal_error` |
| `PATCH /resumes/{id}/photo`                                              | `200`        | Same as item `PATCH`, plus `404 media_not_found`; no upload-only `415`, `media_too_large`, `media_invalid`, `media_busy`, or object-store response                                                                                                                                                                                                                                                                                                              |
| `GET /resumes/{id}/photo`                                                | `200`, `304` | `400 invalid_client_ip\|request_invalid`; `401 session_required`; `404 resume_not_found\|media_not_found`; `429 rate_limited`; `500 internal_error`                                                                                                                                                                                                                                                                                                             |
| `DELETE /resumes/{id}/photo`                                             | `204`        | `400 invalid_client_ip\|idempotency_key_required\|idempotency_key_invalid\|precondition_malformed\|request_invalid\|unsupported_schema_version`; `401 session_required`; `403 csrf_rejected`; `404 resume_not_found\|media_not_found`; `409 idempotency_key_reuse`; `412 revision_mismatch`; `413 body_too_large`; `428 precondition_required`; `429 rate_limited`; `500 internal_error`                                                                        |

`405 method_not_allowed` is defined once for each registered path and carries
the correct `Allow` header. Construction-only `501 not_implemented` is absent
from OpenAPI.

## Steps

- [ ] **Step 1: failing document tests first.** Write
      `docs/api/test/resumes.contract.test.ts` before touching the document. It
      must fail now and pass after Step 2, asserting mechanically over the
      parsed document rather than by inspection: 1. every path under `/resumes`
      exists with exactly the methods in the table above, and no extra ones; 2.
      every mutating resume operation declares `Idempotency-Key`; 3. every
      mutating resume operation **except** `POST /resumes` declares `If-Match`,
      and `POST /resumes` declares none (D6); 4. every resume operation declares
      `sessionCookie`, and every mutating one also declares `csrfToken`; 5. each
      operation matches the response matrix above exactly, with no impossible
      CSRF, precondition, document, or media response copied onto another
      operation, and every error response uses `Error`; 6. every declared
      example validates against its own schema; 7. `Error.details` is optional
      and `code`/`message` are still required; 8. no operation declares `501` or
      `not_implemented`; 9. every success content schema, media type, and
      response-header set equals the success table, including all three distinct
      `204` contracts and photo `304`; 10. `Idempotency-Key`, `If-Match`,
      `X-Resume-Schema-Version`, `If-None-Match`, and `Content-Type`
      descriptions declare singleton handling: repeated field lines and comma-
      folded values are invalid; 11. each DELETE has no OpenAPI request body,
      permits only an absent or singleton JSON `Content-Type`, and defines its
      idempotency payload as zero bytes; 12. a personal-details request
      containing `photo` fails schema validation, while crop validates only
      through the separate `PhotoCropPatch` component; 13. structure index
      schemas are integers with `minimum: 0`, and their descriptions and
      examples pin zero-based insertion, inclusive append, remove-before-
      measure same-column moves, and sequential command evaluation.
- [ ] **Step 2: write the surface.** Add the paths, components, parameters, and
      examples. Each operation's `description` cites the design clause it
      implements. Error responses enumerate the exact codes from
      [D8](decisions.md) in their descriptions, so the closed vocabulary is
      discoverable from the contract, not only from Go. The photo upload
      documents the exact request and file ceilings, dimension and pixel bounds,
      static-format rule, canonical output, `media_invalid` reason vocabulary,
      `media_busy` with `Retry-After: 1`, and the fact that only normalized JPEG
      or PNG bytes are stored and served. The crop PATCH body is exactly
      `{crop: PhotoCrop|null}`: null clears the crop; the value is governed by
      `resume.schema.json#/$defs/photoCrop`, and the contract test compares its
      bounds and required fields to that source. It accepts no key or other
      photo field, preserves the current key, and performs no object I/O. Photo
      POST constructs a new `PhotoRef` containing only its server-derived key,
      so successful replacement clears any crop attached to the old pixels.
      Structure command descriptions define each index against the command-time
      layout. Examples cover create at `index == N`, a forward same-column move
      after removal, and a later command addressing an earlier command's result.
- [ ] **Step 3: the document stays opaque (D16).** `ResumeDocument`,
      `PersonalDetails`, `Section`, and `Customization` are declared as objects
      whose **shape is governed by `packages/schema/resume.schema.json`** at the
      version named by `X-Resume-Schema-Version`, with a description saying so
      and naming `packages/schema/gen/ts` as the source of the client's document
      types. Add a test asserting that pointer text is present and that the
      referenced files exist. Rationale: duplicating a 24-section, byte-bounded
      document schema into OpenAPI creates a second source of truth for the same
      contract, and the drift between them would be silent — exactly the failure
      mode `resume.schema.json` exists to prevent. The wire contract this
      document owns is the **envelope, headers, statuses, and error shapes**.
- [ ] **Step 4: request regeneration.** Report the exact `make api-gen` command
      and expected generated path. The integration owner runs it, reviews the
      generated diff, then runs `make api-check` and `make web-typecheck`.
- [ ] **Step 5: owner contract review.** The integration owner reads the diff
      against the [API design](../../design/api.md) and signs off the D7
      amendment before wave 2 dispatches. Record the sign-off in the task
      report.
- [ ] **Step 6: handoff.** Report the owned file list, failing-test evidence,
      exact checks, contract-review decision, and requested client-generation
      command to the integration owner. Do not stage or commit.

## Acceptance mapping

| Row          | What this task contributes                                                    |
| ------------ | ----------------------------------------------------------------------------- |
| AC-API-001   | Keeps the generated-client drift gate green over the extended surface         |
| AC-SAVE-001  | The `412` response shape (`details.revision`, `details.document`) is declared |
| AC-SAVE-002  | `Idempotency-Key` presence and the `409` reuse response are declared          |
| AC-SAVE-004  | `X-Resume-Schema-Version` request/response semantics are declared             |
| AC-SAVE-005  | The customization delta body and its `422` rejection are declared             |
| AC-MEDIA-001 | The multipart upload's limits, `415`, and `413` responses are declared        |
| AC-MEDIA-005 | The contract proves there is no account-avatar upload operation               |
| AC-MEDIA-008 | The canonical-output and `media_invalid` contract is declared                 |
| AC-MEDIA-009 | Resource bounds and `media_busy` admission behavior are declared              |
