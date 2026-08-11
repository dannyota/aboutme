# Task 11: Per-resume photo upload, read, replace, and delete

The master plan's P2B task line: "media upload endpoint + storage abstraction
(local in dev, S3 in prod) — this covers the **per-resume photo**; the
account-level `users.avatar_key` is populated from the OAuth profile fetch in P1
(no separate upload)". Spec §3 puts the photo in the document
(`personalDetails.photo`, `key` = an S3 object key, not a URL) and §5 renders it
with a CSS crop. Implements [D11](decisions.md), [D12](decisions.md),
[D13](decisions.md), and [D14](decisions.md).

**Tier:** High risk (untrusted binary input, resource bounds, authorization).

**Files:** modify `apps/server/internal/resumeapi/photo.go` (replacing Task 4's
stub); create `photo_test.go`, `photo_contract_test.go`.

## Behavior

| Operation                    | Contract                                                                                                                                                                                                                                                                                                                                      |
| ---------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `POST /resumes/{id}/photo`   | `multipart/form-data`, exactly one part named `file`. Sniffs the content type from the bytes, accepts only `image/jpeg`, `image/png`, `image/webp`, stores under a server-derived key, and writes `personalDetails.photo.key` through the ordinary write path (`If-Match`, `Idempotency-Key`, validation, CAS). `200` with the updated resume |
| `GET /resumes/{id}/photo`    | Streams the object to the **owner only**, with the stored content type, `Cache-Control: private, no-cache, must-revalidate`, and a strong `ETag` derived from the key; honors `If-None-Match` with `304`. No photo → `404 media_not_found`                                                                                                    |
| `DELETE /resumes/{id}/photo` | Clears `personalDetails.photo` through the same write path and deletes the object. No photo → `404 media_not_found`. `204`                                                                                                                                                                                                                    |

**The key is server-derived** (D11):
`resumes/{resumeID}/photo-{32 lowercase hex from crypto/rand}.{ext}`, `ext` from
the **sniffed** type. A client-supplied key is never read from the request, and
a `photo` object in a `personal-details` body is rejected by Task 9.

**Order of operations on upload** (deliberate, and tested): sniff and bound
first, then `Put` the object, then the document CAS write. A CAS failure
therefore leaves one orphan object, which is collected by the prefix sweep at
resume delete (D13's accepted residual). The reverse order would leave a
document referencing an object that does not exist — a broken image for the user
rather than an invisible byte on disk.

**Replace** reads the previous key from the document inside the request, writes
the new object and the new document, then deletes the previous object **after**
the commit; a delete failure is logged, not surfaced.

**`multipart/form-data` is permitted on this route only** — the forward-binding
decision in `../phase-1-deferred.md`: DD-C6's `Content-Type` gate is not the
load-bearing CSRF control, exact `Origin` plus the synchronizer token are. Every
other route keeps requiring `application/json`.

## Steps

- [ ] **Step 1: failing authorization and CSRF tests first.** All three
      operations with no session → `401`; the two mutations with no CSRF token →
      `403`; all three against another user's real resume id → responses
      byte-identical to a nonexistent id; `multipart/form-data` accepted here
      and **rejected** on every other mutating route; a JSON body on the upload
      route → `415`.
- [ ] **Step 2: failing bounds and type tests.** A body above the media budget →
      `413 media_too_large` with **no object written** (assert the backend is
      empty); a body at the budget accepted; a `.jpg` filename whose bytes are a
      PDF, an SVG, or HTML → `415 media_type_unsupported` (the sniff decides,
      never the filename or the client's `Content-Type`); a zero-byte part →
      `415`; two parts, a part with the wrong name, and no part at all →
      `400 request_invalid`; a part whose declared size and actual size disagree
      → rejected without a partial object.
- [ ] **Step 3: failing key tests.** The generated key matches the schema's
      photo-key pattern and passes the traversal check; two uploads for the same
      resume produce different keys; the key contains the resume id as its
      prefix segment; a key is never taken from the request under any field name
      (assert by sending `key`, `photo`, and `filename` fields containing
      `../../other/secret.jpg` and observing the stored key).
- [ ] **Step 4: failing lifecycle tests.** Upload then `GET` returns the same
      bytes and content type; `If-None-Match` with the returned `ETag` → `304`;
      replace deletes the previous object and leaves exactly one under the
      prefix; delete clears `personalDetails.photo` and removes the object;
      `GET` after delete → `404`; a stale `If-Match` on upload or delete → `412`
      **and no object written or removed**.
- [ ] **Step 5: failing idempotency test.** A replayed upload with the same key
      and body returns the stored response and does **not** write a second
      object; a different body under the same key → `409` with no object
      written.
- [ ] **Step 6: failing backend-parity test.** The whole suite runs against the
      filesystem backend by default and against the S3-compatible backend when
      `TEST_S3_ENDPOINT` is set, with no behavioral difference.
- [ ] **Step 7: failing contract test.** Handler statuses, codes, media types,
      and headers agree with `docs/api/openapi.yaml`.
- [ ] **Step 8: implement; green.**
- [ ] **Step 9: gate.** `make server-build server-vet server-test`;
      `REQUIRE_TEST_DB=1 … go test ./internal/resumeapi/... -race -count=1 -v`;
      then the same with `REQUIRE_TEST_S3=1` and the local service up;
      `make api-check`; `make semgrep`.
- [ ] **Step 10: commit** —
      `git commit -m "feat(resumeapi): add per-resume photo upload, read, and delete" -- apps/server/internal/resumeapi`
- [ ] **Step 11: independent defect review**, asked specifically about resource
      exhaustion on upload, whether any input can influence the stored key, and
      whether the owner-only read can be bypassed.

## Acceptance mapping

| Row          | What this task contributes                                                     |
| ------------ | ------------------------------------------------------------------------------ |
| AC-MEDIA-001 | Owner-only, bounded, content-sniffed upload; rejection before any object write |
| AC-MEDIA-002 | Server-derived, pattern-valid, unguessable key; no client influence            |
| AC-MEDIA-003 | Replace and delete remove the previous object; no reachable orphan             |
| AC-MEDIA-004 | The HTTP surface behaves identically on both storage backends                  |
| AC-MEDIA-005 | Records that the account avatar has no upload surface (documented, not built)  |
