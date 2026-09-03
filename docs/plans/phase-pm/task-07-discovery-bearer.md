# Task 07 — Discovery metadata and bearer/scope middleware

**Acceptance:** AC-MCP-005, AC-MCP-006.

**Depends on:** T03 primitives; T06 token semantics.

**Owned paths:** T07 paths in `file-structure.md`.

## Contract

- `HandleMetadata` serves RFC 8414 JSON: issuer = canonical origin,
  `authorization_endpoint`, `token_endpoint`, `registration_endpoint`,
  `revocation_endpoint`, `response_types_supported: ["code"]`,
  `grant_types_supported: ["authorization_code","refresh_token"]`,
  `code_challenge_methods_supported: ["S256"]`,
  `token_endpoint_auth_methods_supported: ["none"]`,
  `scopes_supported: ["resumes:read","resumes:write"]`.
- `HandleProtectedResourceMetadata` serves RFC 9728 JSON: `resource` = canonical
  origin, `authorization_servers` = [canonical origin],
  `bearer_methods_supported: ["header"]`.
- Both derive every URL from the configured canonical origin only; the Host
  header, forwarded headers, and request URL never influence output. GET only;
  stable bytes; `Cache-Control: public, max-age=3600`.
- `mcpapi.Bearer.Authenticate` implements M7: single exact header, shape parse,
  digest lookup joined to grant and user, expiry, revocation, supersession, and
  token-kind checks, bounded `last_used_at` touch, closed 401 with the RFC 9728
  `WWW-Authenticate` pointer. `RequireScope` returns the closed `scope_denied`
  mapping. Cookies are never parsed on this path.

## TDD cycle

- [x] Write metadata REDs: exact document bytes for a fixed origin, header
      substitution attempts (Host, X-Forwarded-*), method matrix, cache header.
- [x] Write bearer REDs: absent/duplicated/malformed headers, wrong prefix,
      access-vs-refresh kind confusion, expired at 3,600 s ± 1 s, revoked,
      superseded, cross-user digest, and byte-identical 401 bodies across every
      failure class.
- [x] Write scope REDs: read token on mutating requirement → closed 403; write
      token passes both.
- [x] Write a last-used RED with injected clock: two calls 30 s apart touch
      once; 61 s apart touch twice.
- [x] Run the expected RED:

  ```sh
  cd apps/server && go test ./internal/oauthsrv ./internal/mcpapi -race -count=1 -run 'Metadata|Bearer|Scope'
  ```

- [x] Implement; rerun to GREEN, then `make server-build server-vet`.

## Adversarial checklist

- A syntactically valid but unknown token and a revoked token are
  indistinguishable in bytes and status.
- The 401 challenge never echoes the presented token or a reason detail.
- A request with both a session cookie and a bearer header behaves exactly as
  bearer-only (cookie ignored, not merely rejected).

## Handoff

Report the metadata documents, failure matrix, RED/GREEN outputs. Suggested
commit: `feat(auth): add oauth discovery and bearer boundary`.
