# Task 9: Personal details + the old-client wire-version proof

Design spec §4's `PATCH /resumes/{id}/personal-details` row ("whole object"),
and **AC-SAVE-004** — the row the spec's wire-version-compatibility line assigns
to P2B: "P2B binds that machinery to the real HTTP path and proves an old-client
write is projected, target-validated, persisted as the complete current
document, and emitted in a declared supported version."

**Tier:** High risk (schema/version handling, CAS).

**Files:** modify `apps/server/internal/resumeapi/personal_details.go`
(replacing Task 4's stub); create `personal_details_test.go`,
`wireversion_e2e_test.go`, `personal_details_contract_test.go`.

## Behavior

The body is the **whole** `personalDetails` object at the caller's declared wire
version — not a delta. Whole-object replacement is what makes clearing a field
expressible: an absent key means "never entered" and `""` means "explicitly
cleared" (spec §3), and a delta protocol cannot distinguish "omitted because
unchanged" from "cleared" without a sentinel the spec forbids.

`personalDetails.photo` is **not** writable here: the photo key is
server-derived (D11) and set only by the upload endpoint. A body carrying
`photo` is `422 document_invalid` — accepting it would be a client-supplied
object-key write, which is exactly what D11 exists to prevent.

## Steps

- [ ] **Step 1: failing whole-object tests first.** Setting the object replaces
      it wholesale; omitting `fullName` removes it; `fullName: ""` persists as
      an empty string and reloads as one; an empty `details` array and a
      `details` entry with a cleared `value` both round-trip unchanged (the
      cleared-contact case AC-DOC-009 pins at the store layer, proven here at
      the HTTP layer); a 17th `details` entry → `422`; a contact value with a
      non-`https` scheme for a URL-typed chip → `422` (AC-SEC-004's HTTP
      evidence); a body carrying `photo` → `422`.
- [ ] **Step 2: failing wire-version end-to-end tests (AC-SAVE-004).** With a
      **synthetic** projector whose `CurrentVersion` is 2 and whose accepted set
      includes 1 — the construction P2A Task 8 built and tested — drive real
      HTTP requests: 1. a request declaring `X-Resume-Schema-Version: 1` with a
      v1 body is accepted, converted up, validated at v2, and **persisted as the
      complete current-version document** (assert the stored row's
      `schema_version` and its full parts, not just the response); 2. the
      response body is emitted at v1 and the response header echoes
      `X-Resume-Schema-Version: 1`; 3. no v1 field is lost across the round trip
      (write at v1, read at v1, byte-compare); 4. a request declaring an
      **undeclared** version fails closed with `400 unsupported_schema_version`
      and writes nothing; 5. a `GET` declaring v1 emits a v1 document while
      storage stays at v2 — reads never write (P2A D18), asserted on stored
      bytes, `revision`, and `updated_at`.
- [ ] **Step 3: failing identity test.** In the production v1 configuration the
      whole path is the identity: absent header, `X-Resume-Schema-Version: 1`,
      and an explicit current version all produce byte-identical stored
      documents and responses.
- [ ] **Step 4: failing granular-old-client test.** The same v1-declared request
      through `PATCH …/entries/{sectionKey}` is applied at v1 and persisted at
      current — proving the down-emit/apply/up-accept model ([D4](decisions.md))
      works for a **fragment**, not only for a whole object. This is the case
      the spec's wording does not spell out and that a whole-document-only
      implementation would silently fail.
- [ ] **Step 5: failing contract test.** Handler statuses, codes, and the
      request/response shapes agree with `docs/api/openapi.yaml`, including the
      `X-Resume-Schema-Version` header on both sides.
- [ ] **Step 6: implement; green.**
- [ ] **Step 7: gate.** `make server-build server-vet server-test`;
      `REQUIRE_TEST_DB=1 … go test ./internal/resumeapi/... -race -count=1 -v`;
      `make api-check`.
- [ ] **Step 8: commit** —
      `git commit -m "feat(resumeapi): add personal-details saves and wire-version negotiation" -- apps/server/internal/resumeapi`
- [ ] **Step 9: independent defect review**, asked specifically whether any
      accepted-version path can persist a non-current document, and whether an
      emitted version can leak a field the target schema does not declare.

## Acceptance mapping

| Row         | What this task contributes                                                               |
| ----------- | ---------------------------------------------------------------------------------------- |
| AC-SAVE-004 | The whole row: accept → project → target-validate → persist at current → emit, over HTTP |
| AC-DOC-009  | HTTP evidence for the cleared-contact-value round trip                                   |
| AC-DOC-012  | HTTP evidence that accepted/emitted declarations are honored and fail closed             |
| AC-SEC-004  | HTTP evidence for the contact-chip URL-scheme allowlist                                  |
