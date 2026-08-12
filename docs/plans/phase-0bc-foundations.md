# Phase 0B/0C — Server, data layer, web skeleton, dev stack

> Executed in parallel with Phase 0A's remaining tasks. File sets are disjoint
> by design; shared build files (root `Makefile`, `.github/workflows/ci.yml`,
> root `package.json`) are owned by the integration owner (Fable) — implementers
> report the lines they need and never edit those files themselves.

**Base:** branch `phase-0a-contracts`. **Design:**
[route ownership](../design/system.md#route-ownership),
[schema and migrations](../design/data.md#schema-and-migrations), and the
[environment model](../design/deployment.md#environment-model). **Budgets:**
`budgets.md` — pgx pool ≤ 20, request body ≤ 256 KB.

## Global constraints (inherited)

- Latest stable, pinned exactly; lockfiles/`go.sum` committed.
- Google style guides; `gofmt`/`goimports`; Google TS via ESLint.
- Conventional Commits; never mention AI/agents/automation; no trailers.
- TDD: failing test first, observed failure, then implementation.
- No `sudo`; Go tools via `go install`, JS tools via npm.

---

## Task B1 — Go server skeleton

**Files (exclusive):** `apps/server/**`

- Module `github.com/dannyota/aboutme/apps/server` (Go 1.26).
- `internal/config`: env parsing with validation and typed struct — `PORT`
  (default 8080), `DATABASE_URL`, `LOG_LEVEL`, `ENV` (`dev|staging|prod`).
  Missing required vars fail fast with a clear message.
- `internal/store`: pgx/v5 pool constructor; **pool size capped at 20**
  (budget); `Ping` helper; context-aware close.
- `internal/api`: chi (or net/http `ServeMux`) router; **error envelope**
  `{"error":{"code":"...","message":"..."}}` and success `{"data":...}`;
  request-ID middleware; `slog` structured logging (JSON in non-dev); body-size
  limit middleware at **256 KB**.
- Health endpoints per the
  [route-ownership design](../design/system.md#route-ownership): `GET /healthz`
  — liveness, always 200 while the process runs, **never touches the database**.
  `GET /readyz` — readiness, checks DB with a short timeout, 200 when ready /
  503 with the error envelope when not. A DB outage must not make liveness fail
  (no restart loop).
- `cmd/server/main.go`: wiring only — config → store → router → HTTP server with
  graceful shutdown on SIGINT/SIGTERM.
- Tests: table-driven config tests; `httptest` tests for both health endpoints
  (including `/readyz` 503 when the DB is unreachable); envelope shape test;
  body-limit test (257 KB body rejected).
- Must pass `go build ./... && go vet ./... && go test ./...`.

**Report to the integration owner:** the exact `Makefile` targets and CI job
steps you need (do not edit those files).

---

## Task C1 — Nuxt web skeleton

**Files (exclusive):** `apps/web/**`

- Latest stable Nuxt 4 + Vue 3, TypeScript, pinned exactly.
- ESLint with Google TypeScript style; `vue-tsc --noEmit` typecheck clean.
- Vitest configured with at least one real component test that asserts rendered
  output (not a tautology).
- One SSR page at `/` rendering a static placeholder; confirm it is
  server-rendered (the markup appears in `curl` output, not only after
  hydration).
- `nuxt.config.ts`: `ssr: true`; dev server on port 3000; API base URL read from
  runtime config (`NUXT_PUBLIC_API_BASE`, default `/api/v1`).
- Scripts: `dev`, `build`, `lint`, `typecheck`, `test`.

**Report to the integration owner:** the exact `Makefile` targets and CI job
steps you need (do not edit those files).

---

## Task C2 — Dev stack (podman compose)

**Files (exclusive):** `deploy/**`, `.env.example` (append only)

- `deploy/compose.yml` for **podman compose**: services `postgres` (latest
  stable, healthcheck, named volume), `server`, `web`, `caddy`.
- `deploy/caddy/Caddyfile` implementing the
  [route-ownership design](../design/system.md#route-ownership) on ONE origin
  (`localhost`): `/api/v1/*` → server:8080; everything else → web:3000.
  Same-origin so cookies/CORS match production.
- `deploy/server.Dockerfile` (multi-stage; Chromium is added in a later phase —
  leave a documented seam, do not install it now) and `deploy/web.Dockerfile`.
- Append the new variables to `.env.example` with empty values.
- Verify by actually running `podman compose up -d`, curling
  `http://localhost/healthz` and `http://localhost/` through Caddy, then
  `podman compose down`. Paste the real output.

**Report to the integration owner:** the exact `Makefile` targets (`dev`,
`dev-down`) you need (do not edit those files).

---

## Phase exit criteria

- [ ] `apps/server` builds, vets, and tests clean; `/healthz` and `/readyz`
      behave per the
      [route-ownership design](../design/system.md#route-ownership) (liveness
      independent of the DB).
- [ ] `apps/web` lints, typechecks, tests, and server-renders `/`.
- [ ] `podman compose up` serves both apps through Caddy on one origin.
- [ ] Integration owner has merged the reported Makefile/CI additions.
- [ ] Opus 5 has reviewed each task diff; blocking findings resolved.
