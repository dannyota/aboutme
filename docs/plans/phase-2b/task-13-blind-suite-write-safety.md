# Task 13: Blind adversarial suite E — HTTP write safety and wire version

ADR 0011 puts concurrency, CAS, and idempotency in the high-risk tier. A
**second fresh worker**, different from Task 12's and from every implementation
author, derives this suite from the written contracts **before reading any
implementation diff or author test**.

**Inputs the blind author gets:**
[resume write safety](../../design/api.md#resume-write-safety),
[document versions](../../design/data.md#document-versions),
`docs/api/openapi.yaml` at the phase head, this plan's
[decisions.md](decisions.md) (D3, D4, D6, D7, D8, D15, D18) and
[http-contract.md](http-contract.md), traceability rows AC-SAVE-001/002/004 and
AC-DOC-010/012, P2A's `task-07-idempotency-store.md` and
`task-08-doc-shape-migration.md` **Interfaces blocks**, and this plan's
Interfaces blocks.

**Inputs withheld:** every non-test `.go` file under `internal/resumeapi`; Task
2's `internal/resume` implementation and author tests; migration 00006 and its
focused migration test; `apps/server/sql/queries.sql`; generated
`apps/server/internal/store/**`; and every `resumeapi` author test. The blind
author sees only the frozen authority/interfaces above and behavior observed by
running the live system.

**Files:** create
`apps/server/internal/resumeapi/writesafety_adversarial_test.go`. No
implementation author may edit it.

## Minimum matrix (the blind author may add, never subtract)

| Test                                         | Assert                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| -------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `TestIfMatch_ExistingWritesAndCreate`        | Across every existing-resume mutation: missing → `428`; `*`, unquoted, weak (`W/"r1"`), `"1"`, `"r"`, `"r-1"`, `"r 1"`, an enormous number, and a non-numeric revision → `400`; stale → `412`. Create accepts an absent `If-Match` subject to its other inputs and rejects any supplied value with `400 precondition_not_supported`. Every rejection leaves row bytes, `revision`, `updated_at`, and object storage unchanged                                                                                       |
| `Test412CarriesWinningState`                 | The `412` body's `details.revision` and `details.document` byte-match a fresh authenticated `GET` taken immediately after, for every existing-resume mutation                                                                                                                                                                                                                                                                                                                                                       |
| `TestConcurrentSameRevision_OneWinner`       | N concurrent writers at revision R on the same resume → exactly one `2xx` at R+1 and N−1 `412`s; deterministic under `-race -count=20`; mixed routes (entry, section, structure, customization) in the same race still yield one winner                                                                                                                                                                                                                                                                             |
| `TestIdempotency_ReplayIsByteIdentical`      | Same key and complete D18 identity replays identical status, `Location`/`ETag`/`X-Resume-Schema-Version` where applicable, deterministic Content-Type/cache policy, and body bytes, with no re-execution or second row/object. `Date` and `X-Request-ID` are valid, fresh, and distinct                                                                                                                                                                                                                             |
| `TestIdempotency_BodylessReplay`             | Fresh and replayed resume-delete, entry-delete, and photo-delete responses are 204 with zero body bytes and **no Content-Type header**; internal JSON `null` never appears on the wire. Entry/photo delete preserve identical new-parent `ETag` and schema-version headers; resume delete has neither. Each request hashes a zero-length payload, so an absent versus valid optional JSON Content-Type still replays                                                                                                |
| `TestHeadersAndDeleteBodiesFailClosed`       | Every singleton mutation header rejects repeated field lines and comma folding with its declared code. Ordinary routes may already be read by bounded outer `BodyLimit`, but rejection precedes handler decode, inspection, transaction, and any state change; streaming upload header rejection proves zero body reads. All three DELETE routes accept immediate EOF but reject positive length without a handler read, and reject whitespace, `{}`, chunked bytes, or trailing data before inspection or mutation |
| `TestIdempotency_CanonicalVectors`           | Independently encode and verify D18's six immutable operation/request digest vectors: entry, create, and upload. Exact OpenAPI `operationId` spelling, tuple domain, field names/order, lengths, absence marker, and raw bytes are all covered                                                                                                                                                                                                                                                                      |
| `TestIdempotency_ChangedFingerprintRejected` | Under the same user, canonical operation identity, and key, changing the resolved wire version, parsed precondition, another declared semantic input, or bounded JSON/file bytes returns `409` with zero deltas. Changing only multipart framing or filename still replays                                                                                                                                                                                                                                          |
| `TestIdempotency_ScopedByUserAndOperation`   | The same key used by a different user or a different canonical operation identity—including another concrete target—is a **distinct** mutation and never a cross-target replay                                                                                                                                                                                                                                                                                                                                      |
| `TestIdempotency_ConcurrentSameKey`          | N concurrent identical requests → exactly one mutation commits and the rest replay it; no request observes a partially applied document                                                                                                                                                                                                                                                                                                                                                                             |
| `TestPhotoCrop_WriteSafety`                  | Crop set and null-clear hash their exact JSON bytes, preserve the key read in the CAS transaction, and make zero object calls. Same-key same-bytes replay without another revision; changed bytes return `409`. A crop racing replacement cannot attach to the new key at a stale revision, and successful replacement clears the old crop                                                                                                                                                                          |
| `TestIdempotency_CSRFRetryReusesKey`         | A request rejected with `403 csrf_rejected` and retried with the **same** key and body mutates exactly once — the `../phase-1-deferred.md` forward contract                                                                                                                                                                                                                                                                                                                                                         |
| `TestIdempotency_FailureLeavesNoRecord`      | A mutation rejected at validation, bounds, or CAS leaves no idempotency record, so a corrected retry with the same key succeeds rather than replaying a failure                                                                                                                                                                                                                                                                                                                                                     |
| `TestIdempotency_BoundedRetention`           | From ADR 0016 and budgets, prove 200/201 oldest-first per-user request cleanup, neighbour/unexpired isolation, composite-index plan, exact retained-row/body-plus-header byte counters under concurrency, both retained-capacity boundaries, expired backlog still counted, replay-at-capacity, exact Retry-After branches, and no mutation on new-key rejection                                                                                                                                                    |
| `TestWireVersion_AcceptProjectPersistEmit`   | Against the production current-v2 projector, immutable v1/v2 schemas, and real converters: a v1 entry-upsert route delta down-emits, applies, up-accepts, and persists the **complete v2 document** (assert stored parts and `schema_version`) before emitting v1; write-then-read at v1 loses no representable field                                                                                                                                                                                               |
| `TestWireVersion_FailsClosed`                | Undeclared, non-numeric, negative, zero, and far-future versions → `400 unsupported_schema_version` with no write. A synthetic projector is allowed only to prove unrepresentable registry states such as accept-only versus emit-only                                                                                                                                                                                                                                                                              |
| `TestReadsNeverWrite`                        | Concurrent `GET`s against a row below the current version leave bytes, `revision`, and `updated_at` unchanged (P2A D18 at the HTTP layer)                                                                                                                                                                                                                                                                                                                                                                           |
| `TestRevisionSerializedAsString`             | Every response carrying a revision serializes it as a JSON string, and a revision beyond 2^53 round-trips exactly                                                                                                                                                                                                                                                                                                                                                                                                   |

## Steps

- [ ] **Step 1 (blind author): write the suite from the contracts; run.** Any
      red is a real finding routed to an implementation author.
- [ ] **Step 2: gate.** Run `make test-db-up`, then
      `(cd apps/server && REQUIRE_TEST_DB=1 TEST_DATABASE_URL="${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable}" go test ./internal/resumeapi/... -race -count=1 -v)`.
      Repeat that package command with `-race -count=20`. A case that passes
      "usually" is a failure.
- [ ] **Step 3: handoff.** Report the owned test file, first-run findings, exact
      checks, and real v1↔v2 fixtures to the integration owner. Do not stage or
      commit.
- [ ] **Step 4: attest independence** in the task report.

## Acceptance mapping

| Row         | What this task contributes                              |
| ----------- | ------------------------------------------------------- |
| AC-SAVE-001 | The independent half of the stale-precondition contract |
| AC-SAVE-002 | The independent half of the idempotency contract        |
| AC-SAVE-004 | The independent half of the old-client write/emit proof |
| AC-DOC-010  | HTTP-level evidence that reads never write              |
