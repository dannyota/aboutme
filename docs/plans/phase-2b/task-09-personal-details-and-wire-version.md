# Task 9: Personal details + the old-client wire-version proof

Implements `PATCH /resumes/{id}/personal-details` from the
[endpoint groups](../../design/api.md#endpoint-groups) and **AC-SAVE-004**, the
wire-version compatibility row assigned to P2B: "P2B binds that machinery to the
real HTTP path and proves an old-client write is projected, target-validated,
persisted as the complete current document, and emitted in a declared supported
version."

**Tier:** High risk (schema/version handling, CAS).

**Files:** modify `apps/server/internal/resumeapi/personal_details.go`
(replacing Task 4's stub); create `personal_details_test.go`,
`wireversion_e2e_test.go`, `personal_details_contract_test.go`.

## Behavior

The body is the **whole** `personalDetails` object at the caller's declared wire
version — not a delta. Whole-object replacement is what makes clearing a field
expressible: an absent key means "never entered" and `""` means "explicitly
cleared" under the [resume aggregate](../../design/data.md#resume-aggregate),
and a delta protocol cannot distinguish "omitted because unchanged" from
"cleared" without a sentinel the spec forbids.

`personalDetails.photo` is **not** writable here: the photo key is
server-derived (D11), and crop has its own photo PATCH. Whole-object replacement
applies only to client-owned personal-detail fields. Inside the transaction the
handler copies the current server-owned `photo` unchanged into the replacement;
a request body carrying `photo` is `422 document_invalid`. This prevents a name
or contact edit from clearing the reference and leaking an orphan.

## Steps

- [ ] **Step 1: failing whole-object tests first.** Setting the object replaces
      it wholesale; omitting `fullName` removes it; `fullName: ""` persists as
      an empty string and reloads as one; an empty `details` array and a
      `details` entry with a cleared `value` both round-trip unchanged (the
      cleared-contact case AC-DOC-009 pins at the store layer, proven here at
      the HTTP layer); a 17th `details` entry → `422`; a contact value with a
      non-`https` scheme for a URL-typed chip → `422` (AC-SEC-004's HTTP
      evidence); a body carrying `photo` → `422`. Upload a photo, then replace
      personal details and prove the transaction-read key and crop remain
      byte-identical. Race a photo replacement against the details PATCH and
      prove stale preflight state cannot restore an old key.
- [ ] **Step 2: failing wire-version end-to-end tests (AC-SAVE-004).** Use the
      production projector after P3: current v2, accepted and emitted versions
      v1 and v2, immutable released schemas, and the real adjacent converters.
      Drive real HTTP requests: 1. a request declaring
      `X-Resume-Schema-Version: 1` with a released-v1 body is accepted,
      converted up, validated at v2, and **persisted as the complete
      current-version document** (assert the stored row's `schema_version` and
      all parts, not only the response); 2. the response is emitted at v1 and
      echoes the v1 header; 3. every field representable in v1 survives a
      write-at-v1/read-at-v1 byte comparison; 4. an undeclared version fails
      closed with `400 unsupported_schema_version` and writes nothing; 5. a
      `GET` declaring v1 emits v1 while storage stays byte-identical v2, with
      unchanged `revision` and `updated_at`; 6. seed a v2-only font, make the v1
      personal-details write, and prove the stored v2 font remains unchanged
      while the v1 response carries only its declared fallback. Synthetic
      projectors are forbidden for these released-version proofs.
- [ ] **Step 3: failing current-version identity test.** With current v2,
      absence of the header and explicit `X-Resume-Schema-Version: 2` produce
      byte-identical stored documents and responses. The explicit v1 case must
      traverse the real converters and is covered in Step 2.
- [ ] **Step 4: own the granular old-client proof.** This case is specified in
      [adversarial coverage](adversarial-coverage.md) before implementation and
      is authored in this task's `wireversion_e2e_test.go`. After all W3 route
      files land, the W4 integration run executes
      `TestWireVersion_AcceptProjectPersistEmit`, which sends a v1 entry delta
      through the real entry route, proves down-emit → fragment apply →
      up-accept, and asserts the complete stored row is current v2. Run
      `(cd apps/server && REQUIRE_TEST_DB=1 TEST_DATABASE_URL="${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable}" go test ./internal/resumeapi -run '^TestWireVersion_AcceptProjectPersistEmit$' -race -count=1 -v)`.
      This task reports its fixture and projector setup for that later run; its
      own gate does not depend on a sibling route. It never weakens the shared
      coverage checklist or edits Task 7's route/tests.
- [ ] **Step 5: failing contract test.** Handler statuses, codes, and the
      request/response shapes agree with `docs/api/openapi.yaml`, including the
      `X-Resume-Schema-Version` header on both sides. Validate a request
      carrying `photo` against OpenAPI and prove `PersonalDetailsPatch` rejects
      it; prove the separate `PhotoCropPatch` is not accepted on this route.
- [ ] **Step 6: implement; green.**
- [ ] **Step 7: gate.** Run `make test-db-up`,
      `make server-build server-vet server-test`,
      `(cd apps/server && REQUIRE_TEST_DB=1 TEST_DATABASE_URL="${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable}" go test ./internal/resumeapi/... -race -count=1 -v)`,
      and `make api-check`.
- [ ] **Step 8: handoff.** Report the owned paths, failing-test evidence, exact
      checks, released-version fixtures, and stored-row assertions to the
      integration owner. Do not stage or commit.

**Phase-review focus:** At W4, the one fresh phase reviewer checks whether any
accepted-version path can persist a non-current document and whether an emitted
version can leak a field the target schema does not declare. The same reviewer
confirms fixes.

## Acceptance mapping

| Row         | What this task contributes                                                               |
| ----------- | ---------------------------------------------------------------------------------------- |
| AC-SAVE-004 | The whole row: accept → project → target-validate → persist at current → emit, over HTTP |
| AC-DOC-009  | HTTP evidence for the cleared-contact-value round trip                                   |
| AC-DOC-012  | HTTP evidence that accepted/emitted declarations are honored and fail closed             |
| AC-SEC-004  | HTTP evidence for the contact-chip URL-scheme allowlist                                  |
