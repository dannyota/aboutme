# Task 7.1.4 and 7.1.5: Private redemption and owner PDF

Implement the HTTP adapters for the queue and snapshot packages. Read the
[private print contract](print-contract.md), ADR 0023, security design, and
existing resume route chains. Acceptance: `AC-PDF-002` through `AC-PDF-004`.

## Slice 7.1.4: Private redemption

One Sol author owns only `apps/server/internal/printapi/*.go`. Export:

```go
type Redeemer interface {
    Redeem(context.Context, renderjob.Redemption) (renderjob.Snapshot, error)
}
func NewRedeemHandler(Redeemer) (http.Handler, error)
```

The handler serves only the exact POST path and closed request in the private
contract. It returns the queue's exact frozen payload, not a reserialized or
newly loaded document. Validate nonzero canonical IDs, exact header counts and
shapes, body/media limits, and cookie/query absence before calling the queue.
All failed authority has identical 404 bytes. Never log request or error detail.
The integration owner registers this handler on its separate private listener.

- [ ] Write a failing test proving missing authority never reaches redemption.
- [ ] Implement the adapter with a five-second read/write deadline and request
      cancellation. Never return while a detached redemption callback is
      running.
- [ ] Test duplicate/malformed headers and keys, extra/trailing fields, cookie,
      encoded/near-match paths, wrong method, compressed/oversized bodies, bad
      media type, replay, cancellation, oversized returned payload, and opaque
      failures.
- [ ] Test that payload metadata equals the queue's snapshot bindings before
      delivery; mismatch fails closed and cannot render a different revision.
- [ ] A real HTTP request proves POST bytes and Content-Length, method denial,
      and a slow-body deadline. Use a controlled redeemer in adapter tests; the
      queue owns atomic token consumption and has its own adversarial tests.
- [ ] Run from the repository root:

```sh
flock --close .dev/phase-7/heavy.lock sh -c \
  'cd apps/server && go test -race -count=1 ./internal/printapi'
```

## Slice 7.1.5: Owner PDF

One Sol author owns only `apps/server/internal/resumeapi/pdf*.go` and
`apps/server/internal/resumeapi/routes.go`. Add `Options.PrintQueue` and the
corresponding service field using this narrow dependency:

```go
type PrintQueue interface {
    Render(context.Context, renderjob.Request) (renderjob.Result, error)
}
```

Register GET and HEAD `/api/v1/resumes/{id}/pdf` in the route catalog and
OpenAPI. HEAD performs the same authorization and output selection without a
body. Keep the existing cookie/session, canonical-client-IP, and read-limit
chain. This binary route accepts no schema-version negotiation, body, or query
option. It creates no mutation, idempotency record, or CSRF exception.

The queue's `Prepare` callback reads the owner-scoped current resume and freezes
it using `printsnapshot.FromOwner` and `Marshal`. This keeps photo and snapshot
allocation behind queue admission. Capture only a safe HTTP error classification
from preparation; do not expose raw queue, database, or browser errors.

If the source has a photo, validate its private key against the resume ID before
the backend read. Read at most 2,097,153 bytes under the job context, close on
cancellation, and reject overflow or extension/content-type mismatch. No photo
reference means no backend call. Every preparation path must finish and close
its body before the queue returns.

Successful output uses `Content-Type: application/pdf`, actual Content-Length,
`Content-Disposition: attachment; filename="resume.pdf"`, and
`Cache-Control: no-store, no-transform`. Keep owner output out of public caches.
Do not emit a document schema header or interpret If-None-Match; reject that
unsupported request option. Later edits do not change the frozen export.

Missing and wrong-owner resumes produce the same existing owner 404. Missing or
invalid sessions retain the existing 401. Busy, timed-out, canceled, absent, or
failed print infrastructure produces a generic 503 with `Retry-After: 1` when
the request can still receive a response. Limit render admission to 10/minute
per account and 10/minute per client IP using the bounded ADR 0018 limiter. Test
with an injected clock. Other existing route limits remain unchanged.

- [ ] Observe a failing route test before adding the route or handler.
- [ ] Implement the smallest adapter and route dependency wiring.
- [ ] Cover owner/wrong-owner/missing/session matrix, body/query/negotiation
      rejection, GET/HEAD headers and body, no photo, photo failure and privacy,
      frozen revision after an edit, output limits, queue 503, and rate
      admission.
- [ ] Exercise cancellation during photo read and render with deterministic
      barriers. Assert bodies close and no extra renderer starts.
- [ ] Run from the repository root:

```sh
flock --close .dev/phase-7/heavy.lock sh -c \
  'cd apps/server && go test -race -count=1 ./internal/resumeapi -run "PDF|Route|SecurityMatrix"'
```

Both authors report failing-first evidence, owned files, exact passed checks,
and unresolved boundaries. No Git, new dependencies, generated files, shared
configuration, container operations, or full CI. The integration owner writes
OpenAPI/client/runtime changes, inspects edits, and reruns each key check.
