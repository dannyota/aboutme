# Task 1: The whole P2B OpenAPI surface, contract-first

Implements [D1](decisions.md) (contract-first), [D3](decisions.md) (the wire-
version header), [D7](decisions.md) (the `Error.details` amendment), and
[D16](decisions.md) (the document stays opaque in OpenAPI). This task ships **no
Go code**: it lands the contract every later task implements against, and
regenerates the TypeScript client exactly once.

**Tier:** Normal (contract + docs; it ships no enforcement path). It is
nonetheless a **fan-out gate**: waves 2 and 3 do not dispatch until the
integration owner has reviewed this diff, because every route task is written
against it.

**Files:** modify `docs/api/openapi.yaml`; create
`docs/api/test/resumes.contract.test.ts`; regenerate
`apps/web/app/api/generated/openapi.ts` via `make api-gen`.

## Surface to add

Fourteen operations across nine paths, all under the existing
`https://aboutme.vn/api/v1` server, all in a new `resumes` tag (the photo
operations additionally in a new `media` tag).

| Path                                           | Methods                  | Notes                                                                  |
| ---------------------------------------------- | ------------------------ | ---------------------------------------------------------------------- |
| `/resumes`                                     | `GET`, `POST`            | `POST` takes `Idempotency-Key` and **no** `If-Match` (D6)              |
| `/resumes/{id}`                                | `GET`, `PATCH`, `DELETE` | `PATCH` body is `{title?, lng?}`; `DELETE` → `204`, no body            |
| `/resumes/{id}/entries/{sectionKey}`           | `PATCH`                  | Upsert ONE entry; identity is `entry.id` in the body                   |
| `/resumes/{id}/entries/{sectionKey}/{entryId}` | `DELETE`                 | → `204`                                                                |
| `/resumes/{id}/sections/{sectionKey}`          | `PATCH`                  | `displayName`, `iconKey`, `entryOrder` — content only, never placement |
| `/resumes/{id}/structure`                      | `PATCH`                  | The only create/delete/move/reorder-section endpoint                   |
| `/resumes/{id}/personal-details`               | `PATCH`                  | Whole object                                                           |
| `/resumes/{id}/customization`                  | `PATCH`                  | `{deltas: [{path, value}]}` from a fixed allowlist                     |
| `/resumes/{id}/photo`                          | `POST`, `GET`, `DELETE`  | `POST` is `multipart/form-data`, one part named `file`                 |

## Interfaces this task fixes for every later task

- **Components (new):** `ResumeSummary` (id, title, revision, `live`, `slug`,
  `schemaVersion`, timestamps — no document), `Resume` (summary + `document`),
  `ResumeDocument`, `CustomizationDelta`, `StructureCommand`, `EntryUpsert`,
  `SectionPatch`, `PhotoRef`.
- **Components (amended):** `Error` gains an optional `details` object (D7).
  Both existing required fields stay required; no other frozen shape changes.
- **Parameters (new):** `ResumeID`, `SectionKey`, `EntryID`,
  `SchemaVersionHeader` (`X-Resume-Schema-Version`, optional,
  integer-as-string). `IfMatch` and `IdempotencyKey` are **reused verbatim**
  from `components` — never redefined.
- **Security:** every operation declares `sessionCookie`; every mutating
  operation additionally declares `csrfToken` (AND, not OR), matching the
  existing auth operations.

## Steps

- [ ] **Step 1: failing document tests first.** Write
      `docs/api/test/resumes.contract.test.ts` before touching the document. It
      must fail now and pass after Step 2, asserting mechanically over the
      parsed document rather than by inspection: 1. every path under `/resumes`
      exists with exactly the methods in the table above, and no extra ones; 2.
      every mutating resume operation declares `Idempotency-Key`; 3. every
      mutating resume operation **except** `POST /resumes` declares `If-Match`,
      and `POST /resumes` declares none (D6); 4. every resume operation declares
      `sessionCookie`, and every mutating one also declares `csrfToken`; 5.
      every operation documents `401`, `403`, `404`, and `429`, every mutation
      documents `409`, `412`, `422`, and `428`, and every one of those responses
      uses the `Error` schema; 6. every declared example validates against its
      own schema; 7. `Error.details` is optional and `code`/`message` are still
      required.
- [ ] **Step 2: write the surface.** Add the paths, components, parameters, and
      examples. Each operation's `description` cites the design-spec row it
      implements. Error responses enumerate the exact codes from
      [D8](decisions.md) in their descriptions, so the closed vocabulary is
      discoverable from the contract, not only from Go.
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
- [ ] **Step 4: regenerate and prove no drift.** `make api-gen`, commit
      `apps/web/app/api/generated/openapi.ts`, then `make api-check` — Redocly
      lint, the vitest contract suite, and P0F's non-mutating drift comparison
      all green. Run `make web-typecheck` to prove the regenerated surface still
      compiles at the consumer.
- [ ] **Step 5: owner contract review.** The integration owner reads the diff
      against design spec §4 and signs off the D7 amendment explicitly before
      wave 2 dispatches. Record the sign-off in the task report.
- [ ] **Step 6: commit** —
      `git commit -m "feat(api): add the resume and media HTTP contract" -- docs/api apps/web/app/api/generated`

## Acceptance mapping

| Row          | What this task contributes                                                    |
| ------------ | ----------------------------------------------------------------------------- |
| AC-API-001   | Keeps the generated-client drift gate green over the extended surface         |
| AC-SAVE-001  | The `412` response shape (`details.revision`, `details.document`) is declared |
| AC-SAVE-002  | `Idempotency-Key` presence and the `409` reuse response are declared          |
| AC-SAVE-004  | `X-Resume-Schema-Version` request/response semantics are declared             |
| AC-SAVE-005  | The customization delta body and its `422` rejection are declared             |
| AC-MEDIA-001 | The multipart upload's limits, `415`, and `413` responses are declared        |
