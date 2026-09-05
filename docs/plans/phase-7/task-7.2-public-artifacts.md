# Task 7.2: Public PDF and share image

Expose generation-bound PDF and PNG artifacts through the public origin gate.
The implementation owner writes failing tests before production changes.

## Authorities

- ADR 0022, ADR 0023, and ADR 0032.
- `docs/design/api.md`, `docs/design/templates/print.md`, and
  `docs/design/budgets.md`.
- `print-contract.md` and the queue interface in `task-7.1-owner-print.md`.
- Acceptance: AC-PDF-005, AC-PDF-006, AC-PUB-003, AC-PUB-005.

## Public handlers

One author owns `apps/server/internal/publicapi/` except `html.go` and
`html_test.go`; `apps/server/internal/publicresume/reader.go` and its tests; and
representation constants in `apps/server/internal/publicstate/coordinator.go`.
The owner may add artifact-specific files and tests in those packages. Root owns
composition, OpenAPI, generated types, plans, and traceability.

- Add GET and HEAD `/api/v1/public/resumes/{slug}/pdf` and
  `/api/v1/public/resumes/{slug}/og.png`. Reject queries and request bodies with
  the existing public `400`. Preserve exact slug recognition and
  indistinguishable public `404` behavior. Unsupported methods return `405`,
  `Allow: GET, HEAD`.
- PDF requires the current slug, live state, and `download_enabled=true`. PNG
  requires current slug and live state. Neither requires discovery.
- Read through `Reader` and hold the generation lease through render, cache
  insertion, conditional comparison, and origin body completion. No cache lookup
  or `304` precedes the current-state gate. Recheck current generation and
  representation eligibility before accepting render completion.
- Inject the shared queue through a small
  `Render(context.Context, renderjob.Request) (renderjob.Result, error)`
  interface. Queue `Prepare` freezes the admitted public snapshot using
  `printsnapshot.FromPublic` and `Marshal`; read normalized photos through
  `Reader.ReadPhoto` under the lease. The queue's generation validator must
  reread state and reject changed or revoked generations. Release every acquired
  validation lease.
- Keep one render cache key per format with resume ID, committed revision,
  representation, fixed variant, format version, app digest, and renderer
  digest. Cache lifetime remains at most 60 seconds. No result from a canceled
  lease is cached or served. Bound PDF at 16,777,216 bytes and PNG at 4,194,304
  bytes. Existing small-response bounds remain unchanged.
- Return byte-derived strong ETags, `no-cache, must-revalidate`, correct
  Content-Length, application/pdf or image/png, and fixed
  `attachment; filename="resume.pdf"` for PDF. HEAD selects the same complete
  response but omits bytes. Follow existing strict conditional parsing.
- Apply a shared 20/minute expensive-render limit per trusted client IP to PDF
  and PNG cache misses. Cache hits still pass a 300/minute IP limit. The public
  dispatcher bypasses the ordinary API global limiter, so wire this explicitly.
  Use existing rate-limit primitives and clock injection. Rate rejection is
  `429` with Retry-After; unavailable queue/render is opaque public `503`.
- Cancel a slow origin write when the lease context is canceled. Use a response
  write deadline and stop/join the cancellation callback before releasing the
  lease or returning; a late callback must not affect a reused connection.
  Unsupported socket cancellation must preserve the five-second fail-before-
  commit fence rule. Never report mutation success with an old handler active.

Required adversarial tests cover flags independently, malformed variants,
HEAD/304, old cached generations, rename/unpublish/delete, cancellation during
queue preparation/render and origin writes, real HTTP slow or aborted viewers,
unsupported cancellation and drain failure, changed generation at completion,
queue saturation, shared miss limits, output bounds, and secret-free errors. Use
existing coordinator/store fixtures; do not duplicate mutation machinery.

Exact check:

```sh
flock --close .dev/phase-7/heavy.lock sh -c \
  'cd apps/server && go test -race -count=1 ./internal/publicapi ./internal/publicresume ./internal/publicstate'
```

## Cache storage

A separate author owns `apps/server/internal/publiccache/` only. Preserve the
constructor and immutable-copy contract. Add a hard 33,554,432-byte aggregate
body limit in addition to the entry count. Reject a single over-budget value
before copying. Replacement, expiry, purge, and oldest-entry eviction must keep
byte accounting exact under concurrent access. Copy a selected value while it
remains protected against concurrent replacement. Do not add a new dependency.
Test the exact bound, overflow, replacement, expiry, eviction, caller mutation,
and concurrent access under race.

```sh
flock --close .dev/phase-7/heavy.lock sh -c \
  'cd apps/server && go test -race -count=1 ./internal/publiccache'
```

## Social metadata

A separate author owns `apps/web/server/workers/public-render/render.ts`, its
metadata tests, and `apps/server/internal/publicapi/html.go` and `html_test.go`.
Add exactly these escaped values to successful public HTML:

- `property="og:image"`: canonical absolute image URL from the current slug.
- `property="og:image:width"`: `1200`.
- `property="og:image:height"`: `630`.
- `name="twitter:card"`: `summary_large_image`.
- `name="twitter:image"`: the same canonical absolute image URL.

Extend Go's HTML validator narrowly to validate those exact names, values,
counts, and canonical origin/slug. Do not admit arbitrary metadata or weaken
script, URL, CSS, or private-field checks. Keep discovery-disabled noindex
behavior. Test missing, duplicate, attacker-controlled, and escaped metadata,
including download-disabled and discovery-disabled resumes.

```sh
flock --close .dev/phase-7/heavy.lock sh -c \
  'cd apps/web && npx vitest run test/public-render && npx eslint server/workers/public-render/render.ts test/public-render'
flock --close .dev/phase-7/heavy.lock sh -c \
  'cd apps/server && go test -race -count=1 ./internal/publicapi -run HTML'
```

## Report

List changed paths, failing-first evidence, exact checks and outcomes, uncovered
uncertainty, and required root-owned integration edits. Do not use Git, modify
manifests or generated files, access secrets, run full CI, or change containers.
