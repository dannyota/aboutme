# Task 14: Blind adversarial suite F — bounds, hostile payloads, media

ADR 0011 puts rich-text sanitization and resource bounds in the high-risk tier.
A **third fresh worker**, different from Tasks 12 and 13's authors and from
every implementation author, derives this suite from the written contracts
**before reading any implementation diff or author test**.

**Inputs the blind author gets:** design spec §3 (the size-bounds bullet and the
entry-fields table), §5's sanitizer-contract bullet,
[`../budgets.md`](../budgets.md), `packages/schema/resume.schema.json` and the
shared hostile corpus, `docs/api/openapi.yaml` at the phase head, traceability
rows AC-DOC-004/007/011, AC-SEC-001/003, and AC-MEDIA-001…004, and this plan's
[decisions.md](decisions.md) (D8, D11, D13, D14) plus its Interfaces blocks.

**Inputs withheld:** every non-test `.go` file under `internal/resumeapi` and
`internal/media`, and every author test in those packages.

**Files:** create `apps/server/internal/resumeapi/bounds_adversarial_test.go`.
No implementation author may edit it.

## Minimum matrix (the blind author may add, never subtract)

| Test                                  | Assert                                                                                                                                                                                                                                                                                                                                                                              |
| ------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `TestEveryBound_LimitAndLimitPlusOne` | Derive the bound set **independently** from `budgets.md` and the schema — request body 256 KB, document 512 KB, 24 sections, 64 entries/section, 16 KB rich text **in UTF-8 bytes**, 16 details, title 160 code points, `lng` 35 — and drive each at the limit (accepted) and limit+1 (rejected) through the real handler. A completeness guard fails if a schema bound has no case |
| `TestRejectionWritesNothing`          | For every rejection above: full-row bytes, `revision`, `updated_at`, row count, and object storage identical before and after                                                                                                                                                                                                                                                       |
| `TestByteVsCodePointBounds`           | Rich text of 9,000 `é` characters (18,000 UTF-8 bytes) is rejected even though it is under a code-point limit; a title of 160 astral-plane code points is accepted and 161 rejected                                                                                                                                                                                                 |
| `TestHostileCorpusThroughHTTP`        | Every payload in the shared hostile corpus written through each rich-text-carrying route is neutralized in the persisted document and in the `GET` response, judged by a predicate this author writes from the allowlist data — **not** by importing P3's `sanitizetest` helper                                                                                                     |
| `TestSanitizerCannotBeBypassed`       | Hostile content in a section `displayName`, an entry title, a customization value, and a `personalDetails` field is either sanitized or rejected — never stored raw and echoed                                                                                                                                                                                                      |
| `TestStrictDecode`                    | Unknown fields, duplicate JSON keys, trailing data after the JSON value, a JSON array where an object is expected, deeply nested JSON, and `null` for a required field → `400 request_invalid`, never a `500`                                                                                                                                                                       |
| `TestMalformedMultipart`              | Truncated multipart, a bad boundary, a 100 MB declared `Content-Length` with a short body, a part with a 10 MB filename, a zip bomb, an SVG, and a polyglot JPEG/HTML → rejected from the vocabulary with no object written                                                                                                                                                         |
| `TestMediaKeyCannotBeInfluenced`      | No request field, filename, header, or path parameter changes the stored key; keys from two uploads differ; no stored key contains `..`, a leading `/`, a backslash, or a NUL                                                                                                                                                                                                       |
| `TestMediaOrphans`                    | After a failed CAS on upload, no document references a missing object; after resume delete, no object remains under the resume's prefix; a neighbour resume's objects are untouched                                                                                                                                                                                                 |
| `TestRateLimitsHold`                  | Read, write, and upload policies each reject over-limit traffic with `429` + `Retry-After` before the body is read, and one user's traffic cannot exhaust another's budget                                                                                                                                                                                                          |
| `TestNoUnboundedWork`                 | A request with 10,000 customization deltas, 10,000 structure commands, or an entry with 10,000 array members is rejected by a declared bound rather than processed                                                                                                                                                                                                                  |

## Steps

- [ ] **Step 1 (blind author): write the suite from the contracts; run.** Any
      red is a real finding routed to an implementation author. Deriving the
      bound set independently is the point of this suite — transcribing the
      implementation's constants voids it.
- [ ] **Step 2: gate.**
      `REQUIRE_TEST_DB=1 … go test ./internal/resumeapi/...     -race -count=1 -v`,
      and the media cases additionally with `REQUIRE_TEST_S3=1` against the
      local service.
- [ ] **Step 3: commit** —
      `git commit -m "test(resumeapi): add adversarial bounds and media suite" -- apps/server/internal/resumeapi/bounds_adversarial_test.go`
- [ ] **Step 4: attest independence** in the task report, including how the
      bound set was derived.

## Acceptance mapping

| Row          | What this task contributes                                         |
| ------------ | ------------------------------------------------------------------ |
| AC-DOC-004   | Independently derived limit+1 matrix through the real HTTP surface |
| AC-DOC-007   | Byte-measured rich-text bound proven at the HTTP boundary          |
| AC-DOC-011   | 512 KB document bound proven at the HTTP boundary                  |
| AC-SEC-003   | Independent neutralization evidence for the wired sanitizer        |
| AC-MEDIA-001 | Independent evidence for upload bounds and type rejection          |
| AC-MEDIA-002 | Independent evidence that the key cannot be influenced             |
| AC-MEDIA-003 | Independent evidence that no reachable orphan survives             |
