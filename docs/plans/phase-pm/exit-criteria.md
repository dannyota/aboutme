# Phase PM exit criteria

The integration owner checks every item at one unchanged candidate commit.
Failed or unsatisfiable items are corrected and rerun under ADR 0024.

## Authorities, routes, storage, and dependencies

- [ ] Approved v4, ADR 0026, budgets, OpenAPI, traceability, and the phase plan
      agree on OAuth/MCP behavior; the spec and design text do not disagree
      anywhere.
- [ ] The v6 public-root source regenerates byte-identical Go/Nuxt/Caddy/test
      consumers and adds exactly four root rows — `.well-known`, `oauth`, and
      `mcp` dispatched to Go and `authorize` dispatched to Nuxt — without
      changing existing dispatch; the finer OAuth and well-known paths dispatch
      inside the Go routers.
- [ ] `github.com/modelcontextprotocol/go-sdk` v1.7.0 is a direct exact
      dependency; no unreviewed dependency or toolchain drift exists.
- [ ] Migration 00009 passes fresh up/down, constraint, cleanup, lock-order, and
      concurrent sqlc tests with `-count=1`. Digests are exactly 32 bytes;
      closed kinds and scopes are constraint-enforced; account deletion cascades
      through clients' grants, codes, and tokens.
- [ ] OpenAPI and the generated client contain exactly the four consent and
      grant operations with strict bodies and no token-bearing field.

## OAuth protocol invariants

- [ ] Registration enforces the M1 grammar at every boundary (name code points,
      URI count/bytes/scheme/host, strict JSON) and idle-client GC deletes only
      grantless, tokenless, 24-hour-old clients in bounded sweeps.
- [ ] Authorize validates client, exact redirect URI, response type, scopes, and
      S256 challenge before any redirect; `plain` and missing PKCE are rejected;
      the login round-trip preserves the request; consent is skipped only for a
      live equal-or-narrower grant.
- [ ] Codes are single-use under a row lock with a 60-second TTL bound to
      client, user, scopes, challenge, and redirect URI; replay revokes every
      token issued from the code; two concurrent exchanges of one code yield
      exactly one success.
- [ ] The token endpoint accepts only the M4 closed form vocabulary, verifies
      PKCE by constant-time digest compare, rotates refresh tokens in families,
      revokes a family on superseded-token reuse, and enforces the 1-hour/30-day
      lifetimes at exact boundaries.
- [ ] Revocation (endpoint and settings) revokes grant plus token families in
      one transaction; a revoked token fails `/mcp` on the next request.
- [ ] Discovery metadata derives only from the canonical origin; a 401 from
      `/mcp` carries the RFC 9728 `WWW-Authenticate` pointer; header-driven
      issuer substitution is impossible.

## Resource server and tool invariants

- [ ] `/mcp`, `/oauth/token`, `/oauth/register`, and `/oauth/revoke` never read
      cookies; the consent operations enforce session, CSRF, and exact Origin.
      Absent, malformed, expired, revoked, superseded, and cross-user tokens
      produce byte-identical closed 401s.
- [ ] Each of the fifteen tools enforces its applicable scope, bounds, strict
      validation, and stored-state rules identically to its REST counterpart.
      Each of the twelve mutations also uses the shared sanitizer,
      caller-controlled UUID idempotency, and CAS chain. Exact retries return
      the retained success without another mutation; changed fingerprints fail
      closed and write nothing. Shared-chain tests include create replay,
      hostile markup, and oversized payloads through MCP; responses return the
      canonical stored state and new revision; `revision_conflict` surfaces lost
      races.
- [ ] No publish, unpublish, or public-read capability is reachable through any
      tool, scope, or error path.
- [ ] The REST surface is behavior-identical for users with no agent
      authorization (existing suites pass unchanged).
- [ ] Rate policies enforce every M5 number with bounded key stores, exact
      `Retry-After`, the 4-concurrent cap, and the 10-grant cap; limiter keys
      never contain token material.
- [ ] Logs, metrics, errors, panics, and evidence contain no token or code
      material, PKCE verifiers, or resume content; secret-free log checks pass
      on every new path.

## Web, live evidence, records, and final gate

- [ ] The consent page shows client name and scopes as text, approves and denies
      through the CSRF chain, survives the login round-trip, and passes
      keyboard, light/dark, and issue-focus tests.
- [ ] Connected agents lists grants with last-used, revokes through the
      generated client, and refreshes; revocation is visible to a live agent as
      a closed 401.
- [ ] The later PF capability gate preserves PM behavior: `MCP_ENABLED=false`
      reports `agentAccess=false`, hides Connected agents, and makes no grant
      request; `MCP_ENABLED=true` reports `agentAccess=true` and exposes the
      tested consent and revocation flow. The MCP HTTPS proof also enables the
      provider-login flag required by its seeded sign-in.
- [ ] `make dev-https-mcp-check` passes end to end with the M9 evidence:
      registration, authorize, consent, token exchange, tool list, resume built
      by the agent, editor visibility, revocation, and revoked-rejection, with
      all error counters zero.
- [ ] Every T00–T13 report matches the handoff format; shared edits and unrun
      checks are resolved or block exit.
- [ ] The owner updates and commits the master plan/index, architecture,
      runbook, and trace evidence before review; focused record checks pass.
- [ ] The original non-author reviewer confirms the fixes in `7899535` and
      reviews the current PM surface after `7899535`, including PF's capability
      gating and HTTPS-harness changes. The verdict names the PKCE, code-replay,
      token-rotation, revocation, scope, cookie-isolation, redirect,
      enumeration, rate-limit, sanitizing, CAS, no-publish, disabled-surface,
      and live-proof invariants.
- [ ] `make ci` passes alone, then connected `SEMGREP_APP_TOKEN` `make scan`
      passes alone on the same unchanged candidate.
