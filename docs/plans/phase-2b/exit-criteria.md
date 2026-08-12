# Phase 2B exit criteria

Every item must pass at one unchanged candidate commit.

## HTTP and write safety

- [ ] Every documented resume and owner-photo route is registered, reachable,
      and matches OpenAPI status, envelope, headers, and error codes.
- [ ] Singleton mutation headers reject repeated field lines and comma folding.
      All three DELETE routes accept only an empty body, hash zero payload
      bytes, and return first and replayed `204` responses with no
      `Content-Type`.
- [ ] Create needs an idempotency key and rejects `If-Match`. Existing-resume
      mutations require both a valid idempotency key and valid `If-Match`.
- [ ] Same-revision contenders have one winner. A stale request gets `412` with
      the current revision and document.
- [ ] Same-key same-body retries replay without mutation. Changed bodies return
      key reuse. The identity includes method, concrete target, resolved wire
      version, precondition, and bounded payload bytes. Mutation and response
      record commit or roll back together.
- [ ] Real released v1 requests accept through the production projector,
      convert, store a complete v2 document, and emit v1. Unknown versions fail
      closed. Synthetic projectors cover only states releases cannot represent.
- [ ] Every JSON and aggregate bound rejects before a write. Rich text is
      sanitized at the one aggregate boundary before validation and storage.
- [ ] Customization `set` and `unset` use separate schema-derived allowlists.
      Optional leaves and optional object roots can become truly absent;
      required or structural paths cannot be unset.
- [ ] Wrong-owner and nonexistent IDs are indistinguishable on every route.
- [ ] CSRF, Origin, content type, and route-rate policies fail closed.
- [ ] No construction stub, `501`, or `not_implemented` remains in source or at
      runtime. OpenAPI contains none of them.

## Media

- [ ] Upload is owner-only. The request, raw file, dimensions, pixels, read,
      normalize, object write, concurrency, and measured memory use obey the
      exact budgets. Filenames and client media-type claims grant nothing.
- [ ] The photo chain authenticates and validates CSRF, route rate, headers, and
      heavy-work admission before reading. It bypasses buffering `BodyLimit`,
      streams through `MaxBytesReader`, and maps every request or file overflow
      to `413 media_too_large`.
- [ ] Every accepted source is a fully decoded static JPEG, PNG, or WebP. Exif
      orientation is applied. Source metadata, profiles, optional chunks, and
      trailing bytes never enter the canonical JPEG or PNG object.
- [ ] Keys are server-derived. Filesystem and S3 backends pass the same
      fail-closed conformance suite, including traversal and paginated listing.
      Every object read/delete first proves the exact key grammar and expected
      resume ID; malformed or cross-resume stored keys never reach a backend.
- [ ] Crop PATCH sets or removes only crop, preserves the transaction-read key,
      and performs no object I/O. Photo replacement clears the old crop because
      the normalized pixels and key changed.
- [ ] A committed idempotent replay avoids a new object write. Concurrent
      proved-created candidates leave only the database winner after successful
      compensation. Failed compensation stays private for reconciliation. An
      unknown remote `Put` stops before database mutation and remains private
      for reconciliation without unsafe deletion.
- [ ] Replace and delete validate the exact old key, then change the database
      reference and enqueue cleanup in one transaction. Photo delete and
      whole-resume delete share enqueue, replay, and rollback behavior. A
      current document never points at an object selected by cleanup.
- [ ] Failure injection covers proved-created, proved-not-created, and unknown
      object writes; definite and ambiguous database commits; lost create and
      collision responses; deletion-job insert failure; and worker deletion
      failure.
- [ ] The frozen normalization benchmark passes provisionally on its pinned
      local controlled-cgroup profile. Five seconds is measured; request-time
      decoding is synchronous and no work remains after return. P9A owns the
      launch-gate rerun on the selected production ARM64 Graviton task.
- [ ] The media backend exposes stable bounded pages and update time for
      AC-MEDIA-007. P2B supplies the durable exact-key queue. P8-priv implements
      its 24-hour deletion target plus the adopted 48-hour orphan-reconciliation
      page, run, retry, concurrency, cursor, metric, and dry-run rules before
      launch.

## Checks and evidence

- [ ] `make ci`, `make api-check`, `make sqlc-check`, and the live database and
      fail-closed `make server-test-s3`, `make server-test-p2b`, and
      `make server-test-p2b-s3` suites pass at the candidate commit. Migration
      00006 applies from the prior head with idempotency bounds and the media
      cleanup ledger; released migrations remain byte-identical.
- [ ] `make scan` passes; offline-only scanning does not close the phase gate.
- [ ] Every case in [adversarial coverage](adversarial-coverage.md) is present
      and passing, with its bound set derived from `budgets.md` and the schema
      rather than from implementation constants.
- [ ] Construction stubs are gone:
      `! rg -n --glob '*.go' --glob '!**/*_test.go' 'not_implemented|StatusNotImplemented' apps/server/internal/resumeapi`,
      the route-table equality test, and `TestNoRouteAnswers501` all pass, and
      OpenAPI contains no `501` response.
- [ ] `docs/architecture.md` describes the actual resume routes, media backends,
      failure boundary, and remaining gaps.
- [ ] AC-SAVE-001/002/004/005, the P2B half of AC-SEC-003, and
      AC-MEDIA-001/002/004/005/008/009 are `PROVEN` with exact evidence. The P2B
      slices of AC-MEDIA-003/006 are evidenced, but those cross-phase rows
      remain `PLANNED` until P8-priv proves account deletion and worker
      behavior. AC-MEDIA-007 remains `PLANNED` with P8-priv ownership. Borrowed
      rows retain their original owner.
- [ ] Every integration handoff is applied or has a named owner and downstream
      gate.

## Phase review

- [ ] A fresh reviewer that authored none of the phase finds no blocking defect
      in behavior, design fit, interface stability, assumptions, or
      traceability, and confirms by name the authorization, CSRF, CAS,
      idempotency, sanitizing, and media-privacy invariants. Fixes are confirmed
      by the same reviewer.
