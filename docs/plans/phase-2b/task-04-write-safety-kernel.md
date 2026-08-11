# Task 4: Write-safety HTTP kernel, route table, and per-route policy

The single place every P2B request passes through. It implements design spec
§4's write-safety paragraph over the whole surface, so no route re-implements
`If-Match`, idempotency, error mapping, or wire-version handling — and no route
can forget one. Implements [D2](decisions.md) (stub route table),
[D3](decisions.md)/[D4](decisions.md) (wire version),
[D6](decisions.md)–[D9](decisions.md), [D14](decisions.md) (per-path body
limit), and [D15](decisions.md).

**Tier:** High risk (authorization, CSRF chain, idempotency, CAS).

**Files:** create
`apps/server/internal/resumeapi/{routes.go,chain.go,writesafety.go,wireversion.go,errors.go,persist.go,testutil_test.go}`
plus their tests, and the seven stub handler files
`{resumes.go,entries.go,sections.go,structure.go,personal_details.go,customization.go,photo.go}`;
modify `apps/server/internal/api/router.go` + `router_test.go` and
`apps/server/cmd/server/main.go`.

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
// route whose handler file is still a stub answers 501 not_implemented.
func (s *Service) RegisterRoutes(mux *http.ServeMux)

// mutate is the one write path (D15). It parses and enforces the write
// envelope, runs apply inside IdempotencyStore.Execute's transaction, and
// writes the response. apply receives the stored document down-emitted to
// the caller's declared wire version as a generic tree, and mutates it in
// place; the kernel converts back up, sanitizes, validates, and CAS-writes
// the complete document.
func (s *Service) mutate(w http.ResponseWriter, r *http.Request, spec mutationSpec)

type mutationSpec struct {
    Route        string // e.g. "PATCH /resumes/{id}/entries/{sectionKey}"
    RequireMatch bool   // false only for POST /resumes (D6)
    Apply        func(ctx applyContext, doc map[string]any) error
    Status       int    // 200 or 201
}
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
      row: assert row count and bytes unchanged.
- [ ] **Step 2: failing envelope and vocabulary tests.** Every response body is
      `{data}` or `{error:{code,message}}` and nothing else; `details` appears
      only where [D7](decisions.md) allows it; a test enumerates every
      `WriteError` call site in the package (parsed from the AST or a registry)
      and fails on a code outside the closed vocabulary. The `internal/auth`
      codes `session_required` and `csrf_rejected` are reused verbatim, never
      redefined.
- [ ] **Step 3: failing wire-version tests.** No header → the current version; a
      declared accepted version → accepted, and the response's
      `X-Resume-Schema-Version` echoes it; an undeclared, non-numeric, negative,
      or absurd version → `400 unsupported_schema_version` with
      `details.acceptedVersions`; the response document is the one `EmitWire`
      produced for that version. Drive the non-identity cases through a
      **synthetic** projector (P2A Task 8's construction), since production v1
      has one version.
- [ ] **Step 4: failing precondition and idempotency tests.** Stale `If-Match` →
      `412 revision_mismatch` whose `details.revision` and `details.document`
      byte-match a fresh `GET` (AC-SAVE-001); replay of the same key and body →
      the stored response, byte-identical, with the mutation **not** re-run (spy
      counter) (AC-SAVE-002); the same key with a different body →
      `409 idempotency_key_reuse` with zero database deltas; a handler error
      after a write inside `Apply` leaves neither the mutation nor the record.
      Record in the package doc, verbatim, that a `csrf_rejected` retry **must
      reuse the same `Idempotency-Key`** — the forward contract P2A's Task 7 and
      `../phase-1-deferred.md` hand to P2B and P4.
- [ ] **Step 5: failing route-table test.** Every path and method from Task 1's
      contract is registered; an unregistered method on a registered path is
      `405`; every route is behind `RequireSession` then `RequireCSRF` (assert
      by driving each route with no cookie → `401 session_required`, and each
      mutation with a good cookie but no token → `403 csrf_rejected`); every
      stub answers `501 not_implemented` until its owning task replaces it. A
      parallel test asserts the registered set **equals** the OpenAPI document's
      set — neither side may grow silently.
- [ ] **Step 6: failing rate-limit and body-limit tests.** Reads, writes, and
      media upload each use their own policy keyed by account + client IP via
      `api.RateLimiterConfig.Key`, with the numbers read from the budget
      constants the owner landed; over-limit returns `429` with `Retry-After`
      before the body is read. A JSON route with a 300 KB body is `413` (global
      limit) while the photo route accepts a body above 256 KB and rejects above
      the media budget — the [D14](decisions.md) path-dispatched chain in
      `api.New`, added the same way the health chain already is.
- [ ] **Step 7: implement; green.** `mutate` order is fixed and tested: key →
      precondition → strict decode → wire version → `Execute` → read → down-emit
      → apply → up-accept → sanitize → validate/bounds → CAS → record → respond.
      `Cache-Control: no-store` stays on every response (the outer chain already
      guarantees it; assert it rather than re-add it).
- [ ] **Step 8: the shared test harness.** `testutil_test.go` builds an
      `httptest` server over the real router with a live database and the
      filesystem media backend, plus helpers to create a user, a session cookie,
      a CSRF token, and a resume. Every wave-3 and wave-4 task uses it, so no
      task invents its own harness.
- [ ] **Step 9: gate.** `make server-build server-vet server-test`;
      `REQUIRE_TEST_DB=1 … go test ./internal/resumeapi/... -race -count=1 -v`;
      `make api-check` (the route-set equality test consumes the document);
      `make check`.
- [ ] **Step 10: commit** —
      `git commit -m "feat(resumeapi): add the resume write-safety kernel and route table" -- apps/server/internal/resumeapi apps/server/internal/api apps/server/cmd/server`
- [ ] **Step 11: independent defect review.** Ask specifically whether any order
      in Step 7 can be transposed without a test failing, and whether any path
      reaches a write without passing every check.

## Acceptance mapping

| Row         | What this task contributes                                                             |
| ----------- | -------------------------------------------------------------------------------------- |
| AC-SAVE-001 | The whole `412` contract, including the `details` payload                              |
| AC-SAVE-002 | The whole HTTP idempotency contract over P2A's primitive                               |
| AC-SAVE-004 | The wire-version header, accept/emit plumbing, and the down-emit/apply/up-accept order |
| AC-SEC-002  | Extends the existing CSRF evidence to every resume route (P1 owns the row)             |
