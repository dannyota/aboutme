# Phase 2B exit criteria

Every item must pass at one unchanged candidate commit.

## HTTP and write safety

- [x] Every documented resume and owner-photo route is registered, reachable,
      and matches OpenAPI status, envelope, headers, and error codes.
- [x] Singleton mutation headers reject repeated field lines and comma folding.
      All three DELETE routes accept only an empty body, hash zero payload
      bytes, and return first and replayed `204` responses with no
      `Content-Type`.
- [x] Create needs an idempotency key and rejects `If-Match`. Existing-resume
      mutations require both a valid idempotency key and valid `If-Match`.
- [x] Same-revision contenders have one winner. A stale request gets `412` with
      the current revision and document.
- [x] Same-key same-body retries replay without mutation. Changed bodies return
      key reuse. The identity includes method, concrete target, resolved wire
      version, precondition, and bounded payload bytes. Mutation and response
      record commit or roll back together.
- [x] Real released v1 requests accept through the production projector,
      convert, store a complete v2 document, and emit v1. Unknown versions fail
      closed. Synthetic projectors cover only states releases cannot represent.
- [x] Every JSON and aggregate bound rejects before a write. Rich text is
      sanitized at the one aggregate boundary before validation and storage.
- [x] Customization `set` and `unset` use separate schema-derived allowlists.
      Optional leaves and optional object roots can become truly absent;
      required or structural paths cannot be unset.
- [x] Wrong-owner and nonexistent IDs are indistinguishable on every route.
- [x] CSRF, Origin, content type, and route-rate policies fail closed.
- [x] No construction stub, `501`, or `not_implemented` remains in source or at
      runtime. OpenAPI contains none of them.

## Media

- [x] Upload is owner-only. The request, raw file, dimensions, pixels, read,
      normalize, object write, concurrency, and measured memory use obey the
      exact budgets. Filenames and client media-type claims grant nothing.
- [x] The photo chain authenticates and validates CSRF, route rate, headers, and
      heavy-work admission before reading. It bypasses buffering `BodyLimit`,
      streams through `MaxBytesReader`, and maps every request or file overflow
      to `413 media_too_large`.
- [x] Every accepted source is a fully decoded static JPEG, PNG, or WebP. Exif
      orientation is applied. Source metadata, profiles, optional chunks, and
      trailing bytes never enter the canonical JPEG or PNG object.
- [x] Keys are server-derived. Filesystem and S3 backends pass the same
      fail-closed conformance suite, including traversal and paginated listing.
      Every object read/delete first proves the exact key grammar and expected
      resume ID; malformed or cross-resume stored keys never reach a backend.
- [x] Crop PATCH sets or removes only crop, preserves the transaction-read key,
      and performs no object I/O. Photo replacement clears the old crop because
      the normalized pixels and key changed.
- [x] A committed idempotent replay avoids a new object write. Concurrent
      proved-created candidates leave only the database winner after successful
      compensation. Failed compensation stays private for reconciliation. An
      unknown remote `Put` stops before database mutation and remains private
      for reconciliation without unsafe deletion.
- [x] Replace and delete validate the exact old key, then change the database
      reference and enqueue cleanup in one transaction. Photo delete and
      whole-resume delete share enqueue, replay, and rollback behavior. A
      current document never points at an object selected by cleanup.
- [x] Failure injection covers proved-created, proved-not-created, and unknown
      object writes; definite and ambiguous database commits; lost create and
      collision responses; deletion-job insert failure; candidate compensation
      failure; and the create-to-commit deadline. P8-priv owns worker deletion
      failure and reconciliation failure injection.
- [x] The frozen normalization benchmark passes provisionally on its pinned
      local controlled-cgroup profile. Five seconds is measured; request-time
      decoding is synchronous and no work remains after return. P9A owns the
      launch-gate rerun on the selected production ARM64 Graviton task. Local
      evidence for source `7c4eb4e5bfbc602a0a1493254fd4a084b4808dd4` is the
      frozen manifest and its ignored commit-keyed `results.json`: 160/160
      samples passed, with a 1.416436078-second worst duration and 193,290,240
      byte worst RSS delta.
- [x] The media backend exposes stable bounded pages and update time for
      AC-MEDIA-007. P2B supplies the durable exact-key queue. P8-priv implements
      its 24-hour deletion target plus the adopted 48-hour orphan-reconciliation
      page, run, retry, concurrency, cursor, metric, and dry-run rules before
      launch.

## Checks and evidence

- [x] `make ci`, `make api-check`, `make sqlc-check`, and the live database and
      fail-closed `make server-test-s3`, `make server-test-p2b`, and
      `make server-test-p2b-s3` suites pass at the candidate commit. Migration
      00006 applies from the prior head with idempotency bounds and the media
      cleanup ledger; released migrations remain byte-identical.
- [x] `make scan` passes; offline-only scanning does not close the phase gate.
- [x] Every case in [adversarial coverage](adversarial-coverage.md) is present
      and passing, with its bound set derived from `budgets.md` and the schema
      rather than from implementation constants.
- [x] Construction stubs are gone:
      `! rg -n --glob '*.go' --glob '!**/*_test.go' 'not_implemented|StatusNotImplemented' apps/server/internal/resumeapi`,
      the route-table equality test, and `TestNoRouteAnswers501` all pass, and
      OpenAPI contains no `501` response.
- [x] `docs/architecture.md` describes the actual resume routes, media backends,
      failure boundary, and remaining gaps.
- [x] AC-SAVE-001/002/004/005, the P2B half of AC-SEC-003, and
      AC-MEDIA-001/002/004/005/008/009 are `PROVEN` with exact evidence. The P2B
      slices of AC-MEDIA-003/006 are evidenced, but those cross-phase rows
      remain `PLANNED` until P8-priv proves account deletion and worker
      behavior. AC-MEDIA-007 remains `PLANNED` with P8-priv ownership. Borrowed
      rows retain their original owner.
- [x] Every integration handoff is applied or has a named owner and downstream
      gate.

## Phase review

- [x] A fresh reviewer that authored none of the phase finds no blocking defect
      in behavior, design fit, interface stability, assumptions, or
      traceability, and confirms by name the authorization, CSRF, CAS,
      idempotency, sanitizing, and media-privacy invariants. Fixes are confirmed
      by the same reviewer.

## Evidence

The reviewed implementation candidate is
`e1e29fd1ed1e3acd6f2be73c3fa42ded4095d07b`. The record commit is accepted and
pushed only after the integration owner reruns `make ci` and connected
`make scan` without changing it. This avoids a self-referential commit hash in
the record.

- `make ci` passed the documentation, schema, API, Go, web, SQL, migration,
  route-table, live-database, filesystem-media, S3 conformance, and full S3
  resume-lifecycle checks. Migration 00006 passed its prior-head application
  test. The pre-UAT released-migration hash gate reported that no UAT baseline
  exists yet, so no released migration bytes exist to compare.
- Connected `make scan` passed as scan `210394481`: 343 targets, four
  nonblocking findings, and zero blocking findings. Full-history gitleaks
  scanned 259 commits and about 16.53 MB without finding a leak.
- The adversarial map records 48 rows: 30 Exact, 18 Composed, and 0 Open. Its
  complete filesystem and S3 handler suites passed through `make ci`.
- The fresh reviewer reported no remaining defect after confirming
  authorization, no-oracle behavior, CSRF and Origin handling, CAS, idempotency,
  wire-version conversion, sanitizing, bounds, media privacy, storage semantics,
  failure recovery, traceability, and benchmark provenance.
- P8-priv still owns deletion-worker and reconciliation behavior for
  AC-MEDIA-003/006/007. P9A still owns the production ARM64 normalization
  benchmark rerun. Those named downstream gates do not reopen P2B's completed
  slices.
