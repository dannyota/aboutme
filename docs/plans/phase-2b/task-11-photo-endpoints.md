# Task 11: Per-resume photo intake and lifecycle

This task implements [D11–D14, D18, and D19](decisions.md) and the
[photo-intake design](../../design/api.md#photo-intake). Account avatar import
is separate and has no upload route in v1.

**Tier:** High risk (untrusted binary input, memory and time bounds,
authorization, non-transactional object writes).

**Files:** create
`apps/server/internal/media/{admission.go,admission_test.go,normalize.go,normalize_test.go}`;
modify `apps/server/internal/resumeapi/photo.go` to replace Task 4's stub;
create `photo_test.go` and `photo_contract_test.go`. Task 3 has already pinned
the image dependency and landed the sole photo-key constructor/parser in
`media.go` with its tests in `media_test.go`. This task does not edit `go.mod`
or `go.sum`.

## HTTP behavior

| Operation                    | Contract                                                                                                                                                                                                                                                                                                                |
| ---------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `POST /resumes/{id}/photo`   | Accept one bounded raw multipart `file` part, normalize it, store a private immutable candidate, and commit its server-derived key through the ordinary `If-Match`, idempotency, validation, and CAS path. Replacement clears any old crop because it refers to different pixels. Return `200` with the updated resume. |
| `GET /resumes/{id}/photo`    | Stream the normalized object to the owner only with its canonical media type, authenticated-API `Cache-Control: no-store`, a key-derived strong ETag, and conditional `304` support. No photo returns `404 media_not_found`.                                                                                            |
| `PATCH /resumes/{id}/photo`  | Accept only `{crop: PhotoCrop or null}`. Change or clear the crop while preserving the key read inside the CAS transaction. No photo is `404 media_not_found`; success is `200` with the updated resume. No object I/O occurs.                                                                                          |
| `DELETE /resumes/{id}/photo` | Clear the document reference and enqueue the exact old key in the same transaction. No photo returns `404 media_not_found`. Return `204`; the scheduled worker performs physical deletion.                                                                                                                              |

The key is `resumes/{resumeID}/photo-{32 lowercase hex from crypto/rand}.{ext}`.
`resumeID` is the owning row's canonical lowercase hyphenated UUID. `ext` is
exactly `jpg` or `png`. One media-package constructor and parser own that
grammar. Every GET validates the stored key against the expected resume ID
before backend I/O. Every cleanup mutation validates it before a queue write. A
malformed or cross-resume key never reaches a backend. The normalizer supplies
the extension; no request field, filename, input type, or metadata influences
the key or extension.

`media.NewPhotoKey(randSource, resumeID, ext)` reads exactly 16 injected random
bytes and renders them as 32 lowercase hex characters.
`media.ParsePhotoKey(expectedResumeID, key)` accepts only the constructor's
byte-for-byte grammar and requires the embedded UUID to equal the expected ID.

The crop PATCH uses the ordinary bounded JSON chain and write-rate policy, not
the upload permit or upload-rate policy. Its exact body is
`{crop: PhotoCrop|null}` at the declared wire version. `null` removes the `crop`
property; it does not store JSON null. A value follows exactly the released
version's `$defs.photoCrop` required fields and bounds. The route adds no hidden
key, aspect-ratio, or geometry field. It hashes the exact accepted JSON bytes,
then reads the current photo inside the idempotency transaction, changes only
its crop, and persists the complete current document before emitting the
declared version.

## Intake boundary

The upload path follows this order:

1. Authenticate, validate CSRF and exact Origin, enforce the upload rate limit,
   validate the multipart media type and boundary, `Idempotency-Key`,
   `If-Match`, wire version, canonical target, and declared `Content-Length`,
   then acquire the one task-wide photo permit before reading the body. A
   declared length over 2,162,688 bytes is `413 media_too_large`. Wait at most
   one second for the permit. A miss returns `503 media_busy` with
   `Retry-After: 1` and does not read.
2. This exact route bypasses the ordinary buffering `BodyLimit`; Step 1 has
   already rejected a declared oversized request. After admission, set the
   60-second read boundary with `http.NewResponseController(w).SetReadDeadline`,
   wrap the body in `http.MaxBytesReader(w, r.Body, 2162688)`, and stream it
   without a temporary file. Limit the raw file part to 2,097,152 bytes. Use
   `NextRawPart`, never `ParseMultipartForm`. Require one part named `file`;
   reject transfer encodings, extra parts, non-empty bytes after the closing
   boundary, and filenames over 255 UTF-8 bytes.
3. Build D18's operation scope from method, the exact `uploadResumePhoto`
   `operationId`, and canonical resume ID. Hash the separate fingerprint fields:
   resolved wire version, parsed `If-Match` revision, and exact file bytes.
   Multipart framing, boundary text, part headers, and filename are excluded.
   Call `IdempotencyStore.Inspect` before decode; return a committed replay or
   reuse error without normalization or object `Put`. `Execute` remains the
   concurrency authority after candidate creation.
4. Normalize synchronously. Five seconds is the fail-closed measured release
   gate defined below, not a request-time cancellation timer. The permit remains
   held until every decoder and pixel buffer stops; cancellation never leaves a
   detached goroutine. Record elapsed time, but do not turn the release
   threshold into a per-request rejection. Write the normalized candidate under
   a separate cancellable five-second object-store deadline.

Unsupported container bytes return `415 media_type_unsupported`. A declared or
observed request overflow, a wrapped `*http.MaxBytesError`, and a file byte at
limit+1 all return `413 media_too_large`. Malformed multipart returns
`400 request_invalid`. A recognized image rejected by the normalization contract
returns `422 media_invalid` with only the closed reason values in D19. Permit
exhaustion returns `503 media_busy`. Decoder and backend details never enter a
response or log.

## Normalization

The normalizer independently scans the container and uses JPEG, PNG, or WebP
decoders. Before full decode, it checks source width and height in `1..8192` and
an overflow-safe pixel product at most 16,777,216. Full decode must confirm the
same bounds. APNG, animated WebP, invalid chunk or segment lengths, truncation,
dimension disagreement, and data after the terminal container marker fail.

Apply one valid Exif orientation in `1..8`; no tag means 1. Reject malformed,
duplicate, or conflicting orientation metadata. Convert to 8-bit NRGBA, treat
decoded samples as sRGB, and strip all source metadata and profiles. Output is:

- opaque pixels: baseline 4:2:0 JPEG, quality 85, maximum edge 2,048;
- any non-opaque pixel: non-interlaced PNG with fixed `png.BestSpeed`, maximum
  edge 1,024.

Use `golang.org/x/image/draw.CatmullRom` and never upscale. Preserve aspect
ratio with the long edge fixed to the chosen limit and the short edge rounded to
the nearest positive pixel. If output exceeds 2,097,152 bytes, retry opaque
images at `1792, 1536, 1280, 1024, 768, 512` or alpha images at
`896, 768, 640, 512`, skipping sizes that would upscale. Reject a result still
over the limit at 512. Repeated normalization with the same pinned toolchain and
input must produce byte-identical output.

## Object and database lifecycle

A fresh request normalizes before `Put`. Only `PutCreated, nil` establishes a
request-owned private candidate and permits transactional `Execute`. Record a
hard deadline five minutes after that successful `Put` return. `Execute` must
start and commit within the remaining interval. It refuses to start after the
deadline, binds its transaction context and PostgreSQL local statement and idle
transaction timeouts to the remaining interval, and never resumes that candidate
after timeout. A timeout during commit is `CommitUnknown` and retains the
candidate. A replay, key reuse, stale CAS, validation error, or definite
transaction failure best-effort deletes that proved-created candidate. An
ambiguous database commit leaves it private because the database may reference
it. `PutUnknown` stops before any database or idempotency write and retains the
key because a lost remote response cannot distinguish this request's new object
from a collision winner. The 48-hour orphan reconciliation repairs an
unreachable unknown or crash candidate only after proving there is no live
reference or deletion job. Its 48-hour minimum age is far beyond the candidate
deadline, so a paused request cannot commit a reference after a sweep deletes
the object.

Candidate creation is bounded and collision-safe. Generate a key and call the
create-only backend at most three times. `PutNotCreated, ErrAlreadyExists`
deletes nothing and retries with new randomness. Any other proved-not-created
error stops without cleanup. `PutUnknown` also stops, retains the exact key for
reconciliation, returns the existing `500 internal_error`, and makes no
database/idempotency write. Three collisions return the same error without
deleting any collided key. Compensation tracks and may delete only a key
returned as `PutCreated` for this request.

Replacement reads the prior key inside the CAS transaction, commits a new key
with no crop, and enqueues that exact old key in the same transaction. Photo and
resume deletion enqueue their transaction-read keys while clearing the reference
or deleting the row. A replay creates no duplicate job because the immutable
object key is unique. Request paths perform no object deletion and never perform
prefix deletion for an old reference. Exact-key compensation of a candidate
proved created by the same request is the sole request-path delete. The P8-priv
worker treats `media.ErrNotFound` as success and records retry/audit state
without logging the key or backend detail. Live data never points at an object
selected by cleanup.

The mutation validates the transaction-read key against the exact grammar and
expected resume ID before changing the reference or enqueuing work. Validation
failure performs no object I/O, aggregate write, or queue write and records a
key-invariant metric without the key. Owner GET of an invalid stored key returns
`500 internal_error` without object I/O. Crop also validates its
transaction-read key; an invalid or cross-resume value returns
`500 internal_error`, writes nothing, and makes no object call. This is an
internal invariant failure, not a client-writable key path.

## Steps

- [ ] **Step 1: authorization and admission tests first.** Prove session, CSRF,
      Origin, ownership indistinguishability, route-only multipart allowance,
      upload rate limiting before body read, one-per-task admission, one-second
      busy response, and permit release on every exit. A counting body must show
      zero reads for each rejected session, CSRF, rate, header, and permit case.
- [ ] **Step 2: transport boundary tests.** Exercise request and file limits at
      the exact byte and byte+1 boundaries, short bodies with large declared
      lengths, chunked bodies, truncated boundaries, wrong/missing/duplicate
      parts, transfer encodings, a 255-byte and 256-byte filename, extra fields,
      and non-empty epilogues. Cover direct and wrapped `MaxBytesError` values;
      every overflow is `media_too_large`, and every rejection leaves both
      stores unchanged. Assert no complete-request buffering occurs before the
      handler's streaming read.
- [ ] **Step 3: normalizer tests.** Cover JPEG, PNG, lossy and lossless WebP;
      every Exif orientation; no orientation; duplicate, malformed, and
      conflicting orientation; APNG and animated WebP; zero and over-limit
      dimensions; pixel-product overflow; 16-bit PNG conversion; truncated and
      inconsistent containers; legal metadata holding HTML; JPEG/PNG/WebP with
      trailing HTML; and known decoder-regression fixtures. Assert output type,
      dimensions, alpha rule, fixed ladders, byte ceiling, determinism, and
      absence of Exif/GPS/XMP/IPTC/comments/thumbnails/ICC/source chunks.
- [ ] **Step 4: key and read tests.** Prove the key pattern, randomness,
      three-attempt collision bound, create-only same-key concurrency that
      leaves the winner's referenced bytes unchanged, resume-ID prefix,
      canonical lowercase UUID and extension, constructor/parser round trip, no
      request influence, private conditional GET, strong ETag, `304`, and that
      GET returns normalized bytes rather than the source container. Inject
      malformed and another-resume stored keys: GET returns `500` and cleanup
      skips them, with zero backend calls and no key value in logs or metrics.
      `If-None-Match` is singleton; repeated lines, comma folding, weak tags, or
      malformed values fail as `400 request_invalid` without a backend read. An
      exact current strong tag returns `304`; a different well-formed strong tag
      returns the full `200` response.
- [ ] **Step 4a: crop tests.** Limit and limit-plus-one each `x`, `y`, `width`,
      and `height`; reject unknown fields, a supplied key, missing coordinates,
      non-finite values, and crop without a current photo. A valid crop and
      `null` clear both use the ordinary JSON write limiter and full-aggregate
      CAS, preserve the transaction-read key byte-for-byte, bump the parent
      revision, and perform zero object calls. `null` removes the property.
      Validate against the exact declared-version `$defs.photoCrop`; do not add
      an unversioned shape. Race replacement against crop and prove stale
      `If-Match` cannot attach a crop to a different key. Replacement success
      always stores the new key with crop absent, even when the old photo had a
      crop.
- [ ] **Step 5: lifecycle and failure-injection tests.** Cover upload, replace,
      photo delete, absent cleanup, stale CAS, validation failure, definite and
      ambiguous database commit outcomes, definite-not-created and unknown
      object `Put` outcomes, response loss after remote create and after remote
      collision, candidate-delete failure, and deletion-job insert failure. An
      unknown `Put` makes no database write and never deletes the named key.
      Race a preflight read with a transaction-time photo-key change; only the
      key returned inside the winning transaction may enter the cleanup job.
      Assert exact row, job, and object state after each fault and prove replay
      never duplicates the job. Photo delete and whole-resume delete share the
      same canonical-key check and atomic reference-revocation-plus-enqueue
      rule. A queue failure rolls back the mutation; later worker failure cannot
      replace stored success or restore access.
- [ ] **Step 6: idempotency and concurrency tests.** Hash raw file-part bytes,
      not multipart framing or names, as one fingerprint field. A committed
      replay returns before decode and `Put`; changed wire version,
      precondition, or bytes within the same operation scope and key returns
      `409`. The same key on another canonical resume target is a distinct
      mutation. Deterministically race two fresh inspections and prove `Execute`
      selects one database winner and compensation leaves only its referenced
      object. For crop, the same key and exact JSON bytes replay without a
      revision bump; different bytes under that operation/key return `409`;
      neither path makes an object call.
- [ ] **Step 6a: candidate lifetime tests.** Freeze the clock after a proved
      create, pause before `Execute`, cross the five-minute deadline, and prove
      the transaction never starts and compensation handles the candidate. Pause
      a running transaction at the deadline and require definite rollback or
      `CommitUnknown`, never a later resumed commit. Advance through the 48-hour
      reconciliation cutoff and prove a swept unreferenced key can never become
      referenced afterward.
- [x] **Step 7: freeze and run the resource gate.** Before task dispatch, freeze
      `apps/server/internal/media/testdata/normalization-benchmark-manifest.json`
      with exact fixture hashes for every accepted boundary and decoder
      regression, pinned toolchain, local host identity, CPU quota, exact 512
      MiB controlled-cgroup settings, **selected production Graviton instance
      class and task CPU/memory settings**, three warm-ups, twenty measured
      samples per fixture, and ignored raw-evidence path
      `.superpowers/p2b/media-normalization/<commit>/results.json`. Missing or
      placeholder fields block dispatch; the production target is frozen now
      even though AWS execution waits for P9A authorization. The manifest also
      records the complete `systemd-run --user --scope` command that applies its
      CPU/memory properties and invokes
      `(cd apps/server && go test ./internal/media -run '^TestNormalizationBudget$' -count=1 -v)`.
      Each sample runs in a fresh helper process; `/usr/bin/time -v` maximum RSS
      minus a no-decode helper baseline defines RSS delta, and monotonic elapsed
      time defines duration. Retain every raw result, not only aggregates. After
      implementation, run each fixture synchronously; any measured sample over
      five seconds or peak resident-set-size delta over 192 MiB fails the gate.
      Separately prove the 60-second streaming-read and five-second `Put`
      deadlines cancel their I/O, every exit releases the permit, and no
      decoder, goroutine, buffer, or in-process write survives a returned
      response. A private object with an explicit `PutUnknown` outcome may
      remain for reconciliation; this is stored residual state, not surviving
      request work. Do not implement a detached request-time decoder timeout.
      Preserve the manifest and corpus for P9A's required rerun on the selected
      production ARM64 Graviton class and task cgroup. The provisional local
      gate passed against source commit
      `0513351fb8690affbc729f118f1489955e2c4558`: all 160 measured samples
      across eight fixtures stayed within budget. The worst duration was
      3.025443354 seconds and the worst RSS delta was 193,585,152 bytes. The
      ignored raw evidence is at the manifest's commit-keyed path.

- [ ] **Step 8: backend and contract parity.** Run the complete lifecycle suite
      on filesystem and fail-closed S3 backends. Assert every status, error,
      header, and media type against `docs/api/openapi.yaml`.
- [ ] **Step 9: implement; green.** Keep container parsing and normalization in
      `internal/media`; the HTTP handler owns only request policy and lifecycle.
- [ ] **Step 10: gate.** Run `make test-db-up`, `make test-s3-up`,
      `make server-build server-vet server-test`, `make server-test-p2b`,
      `make server-test-s3`, `make server-test-p2b-s3`, `make api-check`, and
      the focused media/resource checks. Connected `make scan` runs once at the
      unchanged phase candidate. The integration owner must have applied the
      shared targets from `integration-handoffs.md`; no target may skip when its
      service is absent.
- [ ] **Step 11: handoff.** Report the owned paths, failing-test evidence, exact
      checks, frozen benchmark manifest, raw resource evidence, and lifecycle
      fault matrix to the integration owner. Do not stage or commit.

**Phase-review focus:** At W4, the one fresh phase reviewer checks resource
exhaustion, parser differentials, metadata removal, key influence, timeout
cleanup, authorization, and ambiguous-commit handling. The same reviewer
confirms fixes.

## Acceptance mapping

| Row          | What this task contributes                                                     |
| ------------ | ------------------------------------------------------------------------------ |
| AC-MEDIA-001 | Owner-only format and byte-boundary enforcement before object writes           |
| AC-MEDIA-002 | Server-derived traversal-safe immutable key with no request influence          |
| AC-MEDIA-003 | Crop/key preservation, replacement clearing, and transactional cleanup enqueue |
| AC-MEDIA-004 | Complete HTTP lifecycle parity on both storage backends                        |
| AC-MEDIA-006 | Failure compensation and ambiguous-commit safety                               |
| AC-MEDIA-008 | Canonical output, orientation, metadata removal, and trailing-data rejection   |
| AC-MEDIA-009 | Dimension, pixel, concurrency, time, and measured memory bounds                |
