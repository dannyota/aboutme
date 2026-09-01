# Task 09 — Rate policies, caps, config, and composition

**Acceptance:** AC-MCP-008.

**Depends on:** T04–T08 handlers; ADR 0018 limiter store.

**Owned paths:** T09 paths in `file-structure.md`. Integration owner
(config/composition window).

## Contract

- `oauthsrv/rate.go`: per-IP admission for `/oauth/register` (5/h) and
  `/oauth/token` (30/min), plus the failed-grant bucket (10 per 15 min per
  client ID, cleared on success), composing the ADR 0018 bounded store and the
  canonical Caddy client address. Exact `Retry-After` on 429.
- `mcpapi/rate.go`: per-token (120/min) and per-user (240/min) tool-call
  admission, the ≤ 4 concurrent `/mcp` requests per user semaphore (excess
  returns closed `rate_limited`, never queues unbounded), and the 4 MiB body cap
  before JSON-RPC parsing.
- The 10-live-grant cap is enforced in T05; this task wires its closed error
  through config so the number is a single constant sourced from budgets.
- `config.go`: canonical origin reuse, limiter numbers, and MCP enable/disable
  flag validation (fail closed on partial config). `.env.example` gains names
  only.
- `main.go`: compose store contract → oauthsrv service → mcpapi bearer + server
  → routes, with the same shutdown join pattern the mail worker uses. Routes
  register only when the public-roots v6 registry says so.
- Compose the session-authenticated OpenAPI adapters omitted from T05's
  service-only task: `GET|POST /api/v1/oauth/consent`, `GET /api/v1/me/agents`,
  and `DELETE /api/v1/me/agents/{grantId}`. GET routes require a session.
  Mutations require a session, CSRF token, and exact Origin; grant revocation is
  owner-scoped and revokes the grant's tokens in the established client → user →
  grant lock order.
- The top-level router leaves exact `/mcp` to its route-owned 4 MiB cap. Other
  and escaped near matches retain the ordinary 256 KiB cap.
- `deploy/compose.yml` passes `MCP_ENABLED` to the server so the validated flag
  can enable the feature outside native development.

## TDD cycle

- [x] Write limiter REDs per route: exact numbers at limit/limit+1, bounded key
      stores, overflow bucket behavior, `Retry-After` bytes, success clearing
      the failed-grant bucket.
- [x] Write concurrency REDs: 4 in-flight `/mcp` requests admit, the 5th
      closed-fails, completion releases exactly one slot.
- [x] Write config REDs: every new variable validated, partial configuration
      fails startup readiness, no secret value in error text.
- [x] Write composition REDs in `cmd/server`: startup wires routes exactly per
      registry; shutdown joins cleanly under in-flight MCP requests.
- [x] Write session-route REDs for authentication, CSRF and exact-Origin, strict
      4,096-byte consent JSON, safe consent metadata, bounded grant listing, and
      owner-scoped grant-and-token revocation.
- [x] Run the expected RED:

  ```sh
  cd apps/server && go test ./internal/oauthsrv ./internal/mcpapi ./internal/auth ./internal/api ./internal/config ./cmd/server -race -count=1
  ```

- [x] Implement; rerun to GREEN, then
      `make server-build server-vet server-test`.

## Adversarial checklist

- Limiter keys are token row IDs and user IDs, never token material.
- A token-rate-limited agent cannot bypass via a second token beyond the
  per-user ceiling.
- Failed-grant admission reserves the slot before token processing, so
  concurrent invalid grants cannot overshoot the per-client ceiling.
- The semaphore cannot leak slots on panic or client disconnect (cancellation
  test).
- Missing, revoked, and foreign agent grants return byte-identical 404 bodies.

## Handoff

Report limiter tables, composition diff, RED/GREEN outputs. Suggested commit:
`feat(mcp): bound agent access rates and compose services`.
