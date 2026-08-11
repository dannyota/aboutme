# Task 13: Blind adversarial suite E — HTTP write safety and wire version

ADR 0011 puts concurrency, CAS, and idempotency in the high-risk tier. A
**second fresh worker**, different from Task 12's and from every implementation
author, derives this suite from the written contracts **before reading any
implementation diff or author test**.

**Inputs the blind author gets:** design spec §4's write-safety paragraph and
§3's wire-version-compatibility row, `docs/api/openapi.yaml` at the phase head,
this plan's [decisions.md](decisions.md) (D3, D4, D6, D7, D8, D15) and
[http-contract.md](http-contract.md), traceability rows AC-SAVE-001/002/004 and
AC-DOC-010/012, P2A's `task-07-idempotency-store.md` and
`task-08-doc-shape-migration.md` **Interfaces blocks**, and this plan's
Interfaces blocks.

**Inputs withheld:** every non-test `.go` file under `internal/resumeapi`, and
every author test in that package.

**Files:** create
`apps/server/internal/resumeapi/writesafety_adversarial_test.go`. No
implementation author may edit it.

## Minimum matrix (the blind author may add, never subtract)

| Test                                           | Assert                                                                                                                                                                                                                                                                        |
| ---------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `TestIfMatch_MissingMalformedStale_EveryWrite` | Across every mutating route: missing → `428`; `*`, unquoted, weak (`W/"r1"`), `"1"`, `"r"`, `"r-1"`, `"r 1"`, an enormous number, and a non-numeric revision → `400`; stale → `412`. Every rejection leaves row bytes, `revision`, `updated_at`, and object storage unchanged |
| `Test412CarriesWinningState`                   | The `412` body's `details.revision` and `details.document` byte-match a fresh authenticated `GET` taken immediately after, for **every** mutating route                                                                                                                       |
| `TestConcurrentSameRevision_OneWinner`         | N concurrent writers at revision R on the same resume → exactly one `2xx` at R+1 and N−1 `412`s; deterministic under `-race -count=20`; mixed routes (entry, section, structure, customization) in the same race still yield one winner                                       |
| `TestIdempotency_ReplayIsByteIdentical`        | Same key + same body replays the stored status, headers that matter, and body bytes, with the mutation not re-executed and no second row/object                                                                                                                               |
| `TestIdempotency_DifferentBodyRejected`        | Same key + different body → `409` with zero deltas, including when interleaved with valid replays                                                                                                                                                                             |
| `TestIdempotency_ScopedByUserAndRoute`         | The same key used by a different user, or on a different route, is a **distinct** mutation — never a cross-user replay                                                                                                                                                        |
| `TestIdempotency_ConcurrentSameKey`            | N concurrent identical requests → exactly one mutation commits and the rest replay it; no request observes a partially applied document                                                                                                                                       |
| `TestIdempotency_CSRFRetryReusesKey`           | A request rejected with `403 csrf_rejected` and retried with the **same** key and body mutates exactly once — the `../phase-1-deferred.md` forward contract                                                                                                                   |
| `TestIdempotency_FailureLeavesNoRecord`        | A mutation rejected at validation, bounds, or CAS leaves no idempotency record, so a corrected retry with the same key succeeds rather than replaying a failure                                                                                                               |
| `TestWireVersion_AcceptProjectPersistEmit`     | Against the synthetic multi-version projector: an old-version write is persisted as the **complete current-version** document (assert stored parts and `schema_version`) and emitted at the declared version; write-then-read at the old version loses no field               |
| `TestWireVersion_FailsClosed`                  | Undeclared, non-numeric, negative, zero, and far-future versions → `400 unsupported_schema_version` with no write; an emit-only version cannot be used to accept, and vice versa                                                                                              |
| `TestReadsNeverWrite`                          | Concurrent `GET`s against a row below the current version leave bytes, `revision`, and `updated_at` unchanged (P2A D18 at the HTTP layer)                                                                                                                                     |
| `TestRevisionSerializedAsString`               | Every response carrying a revision serializes it as a JSON string, and a revision beyond 2^53 round-trips exactly                                                                                                                                                             |

## Steps

- [ ] **Step 1 (blind author): write the suite from the contracts; run.** Any
      red is a real finding routed to an implementation author.
- [ ] **Step 2: gate.**
      `REQUIRE_TEST_DB=1 … go test ./internal/resumeapi/...     -race -count=1 -v`;
      every concurrency case additionally at `-count=20`. A case that passes
      "usually" is a failure.
- [ ] **Step 3: commit** —
      `git commit -m "test(resumeapi): add adversarial HTTP write-safety suite" -- apps/server/internal/resumeapi/writesafety_adversarial_test.go`
- [ ] **Step 4: attest independence** in the task report.

## Acceptance mapping

| Row         | What this task contributes                              |
| ----------- | ------------------------------------------------------- |
| AC-SAVE-001 | The independent half of the stale-precondition contract |
| AC-SAVE-002 | The independent half of the idempotency contract        |
| AC-SAVE-004 | The independent half of the old-client write/emit proof |
| AC-DOC-010  | HTTP-level evidence that reads never write              |
