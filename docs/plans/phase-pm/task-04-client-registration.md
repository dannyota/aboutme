# Task 04 — Dynamic client registration and idle-client GC

**Acceptance:** AC-MCP-001.

**Depends on:** T01 `OAuthQueries`; T03 primitives.

**Owned paths:** T04 paths in `file-structure.md`.

## Contract

`HandleRegister` implements RFC 7591 restricted to M1:

- POST only; exact `application/json`; ≤ 4,096 bytes; strict JSON (unknown and
  duplicate fields rejected); closed accepted fields `client_name`,
  `redirect_uris`, optional `token_endpoint_auth_method` equal to `none`.
- Validation runs entirely through T03 primitives before any database write.
  Response is 201 with `client_id`, echoed canonical metadata, and
  `token_endpoint_auth_method: none`; errors use the M4 closed vocabulary with
  fixed generic descriptions.
- No cookie is read; no CSRF applies; the route sits behind the T09 per-IP
  limiter (this task takes the limiter as an injected admission interface and
  tests with a fake).
- GC: on each successful registration and once at service start, delete ≤ 200
  clients with no live grant, no live token, and `created_at` older than 24
  hours, using the bounded contract query.

## TDD cycle

- [ ] Write route matrix REDs: method, media type, size 4096/4097, strict JSON
      (duplicate key, unknown field, wrong scalar), each M1 boundary through the
      handler, and closed error bodies byte-compared.
- [ ] Write GC REDs with injected clock: 24 h ± 1 s boundaries, live-grant and
      live-token protection, 200-row batch bound, and idempotent repeat.
- [ ] Write a cookie-isolation RED: a request carrying a valid session cookie
      behaves byte-identically to one without it.
- [ ] Run the expected RED:

  ```sh
  cd apps/server && go test ./internal/oauthsrv -race -count=1 -run 'Register|ClientGC'
  ```

- [ ] Implement handler and GC against the store contract; rerun to GREEN, then
      `make server-build server-vet`.

## Adversarial checklist

- Registration cannot create unbounded rows faster than GC and the limiter
  allow; the test proves the cap math.
- A hostile `client_name` (markup, RTL controls, zero-width) is either rejected
  by grammar or stored canonically and returned as text.
- Response never echoes non-canonical input spellings.

## Handoff

Report handler behavior table, GC evidence, RED/GREEN outputs. Suggested commit:
`feat(auth): add oauth dynamic client registration`.
