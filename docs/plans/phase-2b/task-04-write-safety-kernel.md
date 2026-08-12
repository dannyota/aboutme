# Task 4: Write-safety HTTP kernel, route table, and per-route policy

The single place every P2B request passes through. It implements the
[resume write-safety contract](../../design/api.md#resume-write-safety) over the
whole surface, so no route re-implements `If-Match`, idempotency, error mapping,
or wire-version handling — and no route can forget one. Implements
[D2](decisions.md) (stub route table), [D3](decisions.md)/[D4](decisions.md)
(wire version), [D6](decisions.md)–[D9](decisions.md), [D14](decisions.md)
(streaming photo-path dispatch), and [D15](decisions.md).

**Tier:** High risk (authorization, CSRF chain, idempotency, CAS).

**Files:** create
`apps/server/internal/resumeapi/{routes.go,chain.go,writesafety.go,wireversion.go,errors.go,persist.go,routes_test.go,chain_test.go,writesafety_test.go,wireversion_test.go,errors_test.go,persist_test.go,testutil_test.go}`
and the seven stub handler files
`{resumes.go,entries.go,sections.go,structure.go,personal_details.go,customization.go,photo.go}`;
modify `apps/server/internal/api/router.go` + `router_test.go` and
`apps/server/internal/api/ratelimit.go` + `ratelimit_test.go`,
`apps/server/internal/auth/csrf.go` + `csrf_test.go`, and
`apps/server/cmd/server/main.go`. `middleware.go` remains unchanged: the photo
route bypasses its buffering `BodyLimit` and applies `MaxBytesReader` in Task 11
only after admission.

> **Wave 2 lands as one unit.** `persist.go` calls `sanitizeDocument`, which
> Task 5 defines in `sanitize_doc.go` in this same package. Neither file
> compiles alone; the integration owner builds after both land.

## Interfaces

```go
package resumeapi

type Service struct{ /* store, idempotency, projector, media, clock, origin */ }

func New(store *resume.Store, idem *resume.IdempotencyStore,
    proj *docmigrate.Projector, blobs media.Backend, opts Options) *Service

// RegisterRoutes matches api.New's register signature, exactly as
// auth.Service.RegisterRoutes does. It wires EVERY P2B route at once; a
// route whose handler file is still under construction answers the
// construction-only 501 not_implemented sentinel. No sentinel may remain at
// the phase head.
func (s *Service) RegisterRoutes(mux *http.ServeMux)

// executeMutation is the common write envelope. It parses and enforces the
// headers, wire version, canonical operation identity, bounds, idempotency,
// response storage, and error mapping, then invokes exactly one typed
// transaction operation below.
func (s *Service) executeMutation(w http.ResponseWriter, r *http.Request,
    spec mutationSpec)

type mutationSpec struct {
    RegisteredOperation string
    RequireMatch        bool // false only for POST /resumes (D6)
    Decode              func(*http.Request) (boundedInput, error)
    CanonicalTargets    func(boundedInput) ([]string, error)
    Prepare             func(context.Context, boundedInput,
        idempotencyInspection) (preparedInput, error)
    Run                 mutationOperation
    Finalize            func(context.Context, preparedInput,
        resume.ExecuteResult, error)
}

type mutationRunResult struct {
    Response resume.StoredResponse
}

type mutationOperation interface {
    Run(context.Context, *store.Queries, mutationContext,
        preparedInput) (mutationRunResult, error)
}

// The package supplies explicit operations for create, existing-resume
// aggregate mutation, metadata+aggregate CAS, revision-CAS delete, and photo
// candidate lifecycle. A route registers exactly one operation. Any operation
// that persists document content must project to current, sanitize, validate,
// and persist the complete aggregate before it can return success.
```

`executeMutation` derives the idempotency operation scope from method,
`RegisteredOperation`, and `CanonicalTargets`. Concrete targets are not moved
into the request fingerprint. The same key on a different canonical operation is
a distinct mutation, as ADR 0016 requires.

The explicit operations have these contracts:

- **Create:** rejects `If-Match`, assembles the current document, sanitizes and
  validates it, then calls `CreateTx`.
- **Aggregate mutation:** reads the scoped resume, down-emits, applies the route
  delta, up-accepts, sanitizes, validates, and calls `SaveDocumentTx`.
- **Metadata mutation:** follows the same full projection and sanitation path,
  then calls `SaveMetadataAndDocumentTx` so title, language, current document,
  schema version, and revision commit together.
- **Delete:** calls revision-aware `DeleteTx`; a miss returns the scoped winner
  for `412`, while missing/wrong owner stays `404`. Its transaction validates
  the deleted row's photo key and enqueues exact-key cleanup before commit.
- **Photo upload/replace:** owns candidate preparation and compensation, but
  commits the document through the aggregate operation only after storage
  reports `PutCreated`. A not-attempted or definitely rolled-back database
  commit removes that proved-created candidate; an ambiguous database commit
  leaves it for the bounded sweep. `PutUnknown` stops before `Execute` and is
  never request-deleted because the key may belong to a collision winner.
- **Photo crop:** uses the ordinary JSON aggregate operation. It preserves the
  key read in that transaction and has no external preparation, cleanup, or
  object I/O.
- **Photo delete:** clears the reference through the aggregate operation and
  enqueues the validated transaction-read old key before commit.

This task also closes ADR 0018's current-code gap before adding route-specific
limiters. Each stored limiter entry records the injected-clock time of its
latest request, including a rejection. The bounded sweep removes an entry when
it is fully refilled or has been idle for at least 24 hours. Clock rollback is
clamped before both checks. The existing 10,000-entry bound, one shared overflow
bucket, and no-active-eviction rule remain unchanged.

For operations with no external candidate, `Prepare` and `Finalize` are no-ops.
Resume delete and photo replace/delete enqueue cleanup inside `Execute`; queue
failure rolls back the mutation. `Finalize` handles only external candidate
compensation. It never returns an error and cannot replace a stored HTTP result.
Candidate cleanup failure is logged and measured while first response and replay
keep the same stored success. The upload path uses the same envelope in this
fixed order: bounded raw read → extract all canonical targets (including
body-owned `entry.id` where applicable) → build fingerprint → `Inspect` → on a
fresh key normalize and `Put` a candidate outside PostgreSQL → transactional
`Execute`/CAS → `Finalize` → stored-response writer. A preflight replay never
prepares a candidate. A concurrent replay after preparation deletes only that
request's unreferenced candidate. `Replayed=true`, `CommitNotAttempted`, and
`CommitDefinitelyRolledBack` delete it. A non-replayed `CommitCommitted` result
is the winner; every `CommitUnknown` is retained. No external I/O occurs inside
`Run`. Once `Prepare` succeeds, `Finalize` runs for every `Execute` result or
error.

The transaction callback stores only `Response` in the idempotency record. Old-
object cleanup is durable database work inside that callback, so replay never
duplicates it and rollback or an unknown commit outcome never triggers direct
object I/O. Candidate compensation is separate: it deletes this request's new
candidate on a replay, key conflict, `CommitNotAttempted`, or
`CommitDefinitelyRolledBack`, and retains it on `CommitUnknown`. Tests change
the photo key between preflight and the transaction and prove only the
transaction-read validated key can be enqueued.

`Execute` emits `Replayed=true` only with `CommitCommitted`. `Finalize`
validates that invariant before choosing compensation. An injected impossible
`Replayed`/outcome pair is an internal invariant failure: it retains the
candidate, records the failure without the key, and never risks deleting a
possibly referenced object. Task 2 proves the production `Execute` result
matrix; Task 4 proves this consumer fails closed.

An idempotency retained-capacity rejection maps to the existing
`429 rate_limited` response. `Retry-After` is one second while expired backlog
remains and otherwise rounds up the earliest retained expiry. The rejection
introduces no undeclared error code.

Task 4 also adds this narrow package-`auth` entry point:

```go
// RequireCSRFMultipart applies the same token and origin checks as
// RequireCSRF but accepts only multipart/form-data for a body. The photo POST
// is its sole caller; RequireCSRF remains JSON-only.
func RequireCSRFMultipart(allowedOrigin string) api.Middleware
```

## Steps

- [ ] **Step 1: failing header-contract tests.** Table-driven over every write
      route, asserting the exact status and code from [D8](decisions.md):
      missing `Idempotency-Key` → `400 idempotency_key_required`; non-UUID key →
      `400 idempotency_key_invalid`; missing `If-Match` where required →
      `428 precondition_required`; `If-Match: *`, `If-Match: 42`,
      `If-Match: "42"`, `If-Match: W/"r42"`, and an empty value →
      `400 precondition_malformed`; `If-Match` on `POST /resumes` →
      `400 precondition_not_supported`. Every rejection writes **no** database
      row: assert row count and bytes unchanged. `Idempotency-Key`, `If-Match`,
      `X-Resume-Schema-Version`, and `Content-Type` are singleton headers:
      repeated field lines or a comma-folded value fail. On ordinary JSON and
      DELETE routes, the outer bounded `BodyLimit` may already have buffered the
      request; rejection still precedes handler decode, idempotency inspection,
      and transaction. Only the streaming photo upload proves zero body reads
      for these header failures. Map them to `idempotency_key_invalid`,
      `precondition_malformed`, `unsupported_schema_version`, and
      `request_invalid` respectively; upload Content-Type failure remains
      `415 media_type_unsupported`. Combined-error cases freeze handler
      precedence as idempotency key → precondition → wire version → bounded
      decode. The photo route's outer session, CSRF/media type, and upload-rate
      checks still precede that handler order.
- [ ] **Step 2: failing envelope and vocabulary tests.** Every response body
      other than a declared `204` is `{data}` or `{error:{code,message}}` and
      nothing else; every `204` has zero bytes and no `Content-Type` header on
      both first response and replay. `details` appears only where
      [D7](decisions.md) allows it; a test enumerates every `WriteError` call
      site in the package (parsed from the AST or a registry) and fails on a
      code outside the closed vocabulary. The `internal/auth` codes
      `session_required` and `csrf_rejected` are reused verbatim, never
      redefined. The construction registry permits only `not_implemented` at
      `501`; a separate phase-head test fails if that literal, status, or any
      registered stub remains after W3. Resume, entry, and photo DELETE accept
      an absent body and an optional singleton JSON Content-Type. A positive
      Content-Length is rejected before any handler read; otherwise a one-byte
      probe of the bounded outer buffer accepts only immediate EOF. `{}`, one
      whitespace byte, chunked nonempty data, trailing data, and duplicate or
      comma-folded Content-Type are `400 request_invalid` before idempotency
      inspection or transaction. Each accepted delete fingerprints one
      zero-length payload; the optional Content-Type is transport metadata and
      does not change replay identity.
- [ ] **Step 3: failing wire-version tests.** No header → the current version; a
      declared accepted version → accepted, and every response for which D3
      declares `X-Resume-Schema-Version` echoes it; an undeclared, non-numeric,
      negative, or absurd version → `400 unsupported_schema_version` with
      `details.acceptedVersions`; the response document is the one `EmitWire`
      produced for that version. Drive v1↔v2 through the production projector,
      immutable released schemas, and real adjacent converters. Use a synthetic
      projector only for registry states released data cannot represent, such as
      a version accepted for writes but not emitted. Enumerate every JSON resume
      read and every mutation, including all deletes, and prove the header is in
      scope; prove binary photo GET does not accept or emit it.
- [ ] **Step 4: failing precondition and idempotency tests.** Stale `If-Match` →
      `412 revision_mismatch` whose `details.revision` and `details.document`
      byte-match a fresh `GET` (AC-SAVE-001); replay of the same key and body →
      the stored status, approved deterministic headers, and body,
      byte-identical, with the mutation **not** re-run (spy counter)
      (AC-SAVE-002). The approved stored headers are `Location`, `ETag`, and
      `X-Resume-Schema-Version`; JSON `Content-Type` and
      `Cache-Control: no-store` are deterministic writer/middleware output;
      `Date` and `X-Request-ID` remain fresh and distinct. The same key with a
      different body → `409 idempotency_key_reuse` with zero database deltas; a
      handler error after a transaction operation writes leaves neither the
      mutation nor the record. Record in the package doc, verbatim, that a
      `csrf_rejected` retry **must reuse the same `Idempotency-Key`** — the
      forward contract P2A's Task 7 and `../phase-1-deferred.md` hand to P2B and
      P4. Assert D18's canonical scope: method, registered operation, and
      canonical concrete targets. Assert the separate fingerprint: resolved wire
      version, parsed precondition, other declared semantic inputs, and exact
      bounded body bytes. Crop hashes its exact JSON bytes. Each bodyless delete
      hashes zero bytes whether its optional singleton JSON Content-Type is
      absent or present. The same key on a different concrete target is a
      distinct mutation, never a replay.
- [ ] **Step 5: failing route-table test.** Every path and method from Task 1's
      contract is registered; an unregistered method on a registered path is
      `405`; every route is behind `RequireSession` then `RequireCSRF` (assert
      by driving each route with no cookie → `401 session_required`, and each
      mutation with a good cookie but no token → `403 csrf_rejected`); every
      stub answers the construction-only `501 not_implemented` until its owning
      task replaces it. A parallel test asserts the registered set **equals**
      the OpenAPI document's set — neither side may grow silently. OpenAPI must
      never declare the construction sentinel.
- [ ] **Step 6: failing rate-limit and body-limit tests.** Reads, writes, and
      media upload each use their own policy keyed by account + client IP via
      `api.RateLimiterConfig.Key`, with the numbers read from the budget
      constants the owner landed. Establish the photo route's outer order as
      session → CSRF and multipart media type → account-and-client-IP upload
      limit → handler, with every rejection before a body read. Task 11 adds
      header validation and the permit inside that handler and proves the full
      order before replacing the stub. Add `auth.RequireCSRFMultipart`; keep
      `auth.RequireCSRF` JSON-only. A JSON route with a 300 KB body is ordinary
      `413 body_too_large`. Exact `POST /api/v1/resumes/{id}/photo` bypasses
      buffering `BodyLimit`; malformed, escaped, near-match, wrong-method, and
      non-photo paths cannot enter that branch. Task 11 owns its streaming
      request and file limits, whose overflow is `413 media_too_large`.
- [ ] **Step 7: implement; green.** The common order is key → precondition →
      wire version → strict bounded decode → canonical operation/target
      extraction → semantic fingerprint → inspection → optional external
      preparation → `Execute` (selected transaction operation → normalized
      record → commit) → telemetry-only finalize → stored-response writer. Each
      operation's conditional order matches its contract above; no delete fakes
      a document write and no create fakes an existing read. Add a completeness
      test that enumerates every mutating OpenAPI route and proves it registers
      exactly one operation. Add an invariant test that every operation which
      persists document bytes reaches the one project/sanitize/validate helper
      before a store call. The order test uses spies for body reads, inspection,
      normalization, object I/O, transaction, commit classification, and
      cleanup; no transposition may pass. A deletion-job failure rolls back the
      database mutation. Candidate-compensation failure after a concurrent
      replay is logged and measured but cannot change the stored success status,
      headers, or body. Inject every impossible `Replayed`/outcome pair and
      prove `Finalize` retains the candidate and reports the invariant without
      exposing its key. Race tests prove a stale preflight key is never
      enqueued, replay never runs the callback, and only the key read by the
      winning transaction enters the queue. `Cache-Control: no-store` stays on
      every response (the outer chain already guarantees it; assert it rather
      than re-add it).
- [ ] **Step 7a: close the limiter expiry contract.** Add fake-clock tests that
      reclaim a fully refilled entry, keep an unrefilled entry from idle expiry
      with periodic allowed and rejected requests, expire an unrefilled entry
      only after 24 hours with no request, prove a rejection resets the idle
      clock, clamp a backward clock step, and reclaim capacity without evicting
      another active entry. Update `internal/api/ratelimit.go` with the smallest
      state needed; retain the overflow and concurrent-admission tests
      unchanged.
- [ ] **Step 8: the shared test harness.** `testutil_test.go` builds an
      `httptest` server over the real router with a live database and the
      filesystem media backend, plus helpers to create a user, a session cookie,
      a CSRF token, and a resume. Every wave-3 and wave-4 task uses it, so no
      task invents its own harness.
- [ ] **Step 9: gate.** Run `make test-db-up`,
      `make server-build server-vet server-test`,
      `(cd apps/server && REQUIRE_TEST_DB=1 TEST_DATABASE_URL="${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable}" go test ./internal/resumeapi/... -race -count=1 -v)`,
      `make api-check`, and `make check`.
- [ ] **Step 10: handoff.** Report the owned paths, failing-test evidence, exact
      checks, route inventory, and remaining construction stubs to the
      integration owner. Do not stage or commit.

**Phase-review focus:** At W4, the one fresh phase reviewer checks whether any
order in Step 7 can be transposed without a test failing, whether any path
reaches a write without every check, and whether candidate compensation fails
closed. The same reviewer confirms fixes.

## Acceptance mapping

| Row         | What this task contributes                                                             |
| ----------- | -------------------------------------------------------------------------------------- |
| AC-SAVE-001 | The whole `412` contract, including the `details` payload                              |
| AC-SAVE-002 | The whole HTTP idempotency contract over P2A's primitive                               |
| AC-SAVE-004 | The wire-version header, accept/emit plumbing, and the down-emit/apply/up-accept order |
| AC-SEC-002  | Extends the existing CSRF evidence to every resume route (P1 owns the row)             |
