# MCP agent access design

Status: Approved for implementation planning (2026-09-01)

## Purpose

Let users bring their own agents to build resumes. aboutme hosts a remote MCP
server over the existing resume API, and agents authenticate through a
first-party OAuth 2.1 authorization server. Publishing stays human-only in the
web UI.

This is a deliberate change to the Approved v4 scope in
[`../design/product.md`](../design/product.md),
[`../design/api.md`](../design/api.md), and
[`../design/security.md`](../design/security.md). Implementation starts by
updating those authorities and accepting an ADR. Code must not land while the
Approved v4 text and this design disagree.

## Decisions

- Agents connect to one remote Streamable HTTP MCP endpoint at `/mcp` on the
  canonical origin. No local stdio distribution in v1.
- Agents authenticate with OAuth 2.1: authorization code with PKCE (S256
  required) and rotating refresh tokens. Public clients only; dynamic client
  registration is open and bounded.
- Consent grants account-wide scopes: `resumes:read` and `resumes:write`. Photo
  tools ride on `resumes:write`. There is no per-resume grant in v1.
- The tool surface is editor parity minus publish: resume lifecycle, content,
  and photo operations. There is no publish, unpublish, or public-read tool.
- The MCP handler and the authorization server live in the existing Go server
  behind Caddy. Tools dispatch into the same validation and service chain as the
  REST handlers; there is no second validation path.
- Sessions and cookies are never read at `/mcp`, `/oauth/token`,
  `/oauth/register`, or `/oauth/revoke`. The authorize and consent surfaces use
  the existing session, CSRF, and Origin chain.
- Deferred: publish scope, per-resume grants, confidential clients, a local
  stdio wrapper, SSE streaming and server notifications.

## Architecture and topology

Two new internal packages:

- `internal/oauthsrv` owns the authorization server: dynamic client
  registration, the authorize flow, token issue/rotate/revoke, and the discovery
  documents.
- `internal/mcpapi` owns the resource server: the Streamable HTTP handler built
  on the pinned official `modelcontextprotocol/go-sdk`, bearer-token middleware,
  scope checks, and the tool registry.

New fixed public roots (registry v5 → v6):

| Root                                      | Surface                              |
| ----------------------------------------- | ------------------------------------ |
| `/.well-known/oauth-authorization-server` | RFC 8414 metadata (GET)              |
| `/.well-known/oauth-protected-resource`   | RFC 9728 metadata (GET)              |
| `/oauth/authorize`                        | Authorize entry (GET, session-aware) |
| `/oauth/token`                            | Token endpoint (POST, bearer world)  |
| `/oauth/register`                         | Dynamic client registration (POST)   |
| `/oauth/revoke`                           | RFC 7009 revocation (POST)           |
| `/mcp`                                    | MCP Streamable HTTP endpoint         |
| `/authorize`                              | Nuxt consent page (session)          |

The MCP handler runs in stateless JSON mode: each POST carries one JSON-RPC
message and returns one JSON response. No SSE stream is opened and no server
notification is sent in v1.

## OAuth 2.1 authorization server

Registration (RFC 7591) is unauthenticated and bounded. A request supplies a
client name and up to 5 redirect URIs; each URI must be `https://` or loopback
(`http://127.0.0.1`, `http://localhost`, any port). The server stores only name,
redirect URIs, and timestamps, and returns a public `client_id` with
`token_endpoint_auth_method: none`. Clients with no grant and no live token are
garbage-collected after a bounded idle period (initially 24 hours; the exact
value is a phase budget row).

The authorize flow validates `client_id`, exact registered `redirect_uri`,
`response_type=code`, requested scopes, and a `code_challenge` with
`code_challenge_method=S256` before any redirect. `plain` is rejected. An
unauthenticated browser is redirected to login and returns with the pending
request intact. The consent page shows the client name and requested scopes.
Approval records a grant per (user, client, scopes); a later request with the
same client and equal-or-narrower scopes skips consent. Denial redirects with
`error=access_denied`.

Authorization codes are single-use, expire in 60 seconds, and bind the client,
user, scopes, `code_challenge`, and exact `redirect_uri`. The token endpoint
accepts exact `application/x-www-form-urlencoded` with a closed parameter set
and verifies the PKCE `code_verifier` by S256 digest in constant time. A
replayed code revokes every token already issued from it.

Tokens are opaque 256-bit random values with distinguishing prefixes and are
stored as SHA-256 digests only. Access tokens live 1 hour. Refresh tokens rotate
on every use inside one token family with a 30-day absolute lifetime; presenting
a superseded refresh token revokes the whole family. Revocation (RFC 7009 or the
settings UI) revokes the grant and its token family in one transaction.

Discovery follows the MCP authorization spec: an unauthenticated or insufficient
request to `/mcp` returns `401` with a `WWW-Authenticate` header naming the
protected-resource metadata URL, which names the authorization server. The
issuer is the canonical origin; request headers never choose it.

## Data model

One additive migration introduces four bounded tables.

| Table                       | Purpose and required state                                                                                                                                |
| --------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `oauth_clients`             | UUID, public client ID, bounded name, bounded redirect-URI list, created and last-used times                                                              |
| `oauth_authorization_codes` | UUID, unique SHA-256 code digest, client and user IDs, scopes, S256 challenge, exact redirect URI, 60-second expiry, consumed time                        |
| `oauth_grants`              | UUID, unique live (user, client) pair, scopes, created and revoked times                                                                                  |
| `oauth_tokens`              | UUID, unique SHA-256 token digest, closed kind (access/refresh), family ID, rotated-from link, client/user/grant IDs, expiry, revoked and last-used times |

Digests are exactly 32 bytes. Database constraints enforce closed kinds and
scopes, expiry ordering, the single live grant per (user, client), and the
bounded redirect-URI count. Account deletion cascades through grants, codes, and
tokens. Expired codes and terminal tokens are removed in bounded batches. Token
material, code material, and verifier values never exist in PostgreSQL.

## MCP resource server and tools

Bearer middleware hashes the presented token, loads the live token row, grant,
and user in one query path, rejects expired or revoked state, and records
last-used. Read tools require `resumes:read`; every mutating tool requires
`resumes:write`. Missing scope returns the closed `403 scope_denied` without
touching resume state.

Fifteen tools map one-to-one onto existing editor operations:

- Read: `list_resumes`, `get_resume`.
- Lifecycle: `create_resume`, `delete_resume`, `update_resume_metadata`.
- Content: `upsert_entry`, `delete_entry`, `update_section`, `update_structure`,
  `update_personal_details`, `update_customization`.
- Photo: `get_photo` (base64 with content type), `upload_photo` (base64,
  existing media type and size ceilings), `update_photo_crop`, `delete_photo`.

Each tool calls the same validation, sanitizer, bounds, and store chain as its
REST handler; shared checks that currently live only in an HTTP handler are
factored into functions both callers use. Every mutating tool takes the same
revision validator the editor sends and returns the new one; a lost race returns
the closed `revision_conflict` tool error instructing the agent to re-read.
Responses return the canonical stored state after sanitizing, so an agent
observes exactly what a hostile-markup strip did.

The tool error vocabulary is closed: `validation_failed`, `revision_conflict`,
`not_found`, `payload_too_large`, `scope_denied`, `rate_limited`,
`agent_access_unavailable`. Server instructions describe the document shape, the
read-modify-write loop, and the absence of publish tools.

## Rate limits and resource admission

All limits compose the ADR 0018 bounded limiter and the canonical client address
from Caddy. Exact numbers are budget rows owned by the phase T00; initial
values:

- `/oauth/register`: 5 per hour per IP.
- `/oauth/token`: 30 per minute per IP; failed grants also count against a
  10-per-15-minutes per-client bucket.
- `/mcp` tool calls: 120 per minute per token and 240 per minute per user, with
  at most 4 concurrent MCP requests per user.
- Active grants: at most 10 live grants per user; the 11th consent is refused
  with a closed error until one is revoked.
- Request bodies reuse the existing per-route document and media ceilings.
- Every route remains subject to the outer per-IP limit.

## Web experience

The Nuxt consent page at `/authorize` is session-authenticated, shows the client
name and requested scopes, and submits approval or denial through the existing
CSRF-protected POST chain. Login redirects preserve the pending authorize
request. The settings page adds a "Connected agents" block: each grant shows
client name, scopes, created and last-used times, and a revoke action wired like
the sessions list. The UI never displays token material.

## Security and privacy

- Token material, code material, PKCE verifiers, and resume content never enter
  logs, traces, metrics labels, errors, or panic text. Logs carry client ID,
  grant ID, token row ID, tool name, resume ID, and closed outcomes only.
- Digest comparison is constant-time after exact shape decoding.
- `/mcp`, `/oauth/token`, `/oauth/register`, and `/oauth/revoke` ignore cookies
  entirely; no CSRF surface exists there. The consent POST keeps the full
  session, CSRF, and exact-Origin chain.
- Redirects go only to the exact registered redirect URI; open-redirect and
  `redirect_uri` substitution attempts fail closed before user interaction.
- Scope enforcement happens inside the resource server per tool, not in the
  client; a token never grants publish, public-read, session, or account surface
  access.
- Existing per-user resume caps, sanitizer versioning, and media privacy bounds
  apply to agent writes unchanged.
- Client-supplied names are bounded, sanitized text; the consent page and
  settings render them as text, never markup.

## Verification

One author per task, one independent phase reviewer. Tests include:

- PKCE S256 verification, `plain` rejection, code single-use and replay
  revocation races, exact redirect-URI matching, and token-endpoint parameter
  strictness;
- refresh rotation and family reuse-detection races, revocation versus in-flight
  tool calls, and expiry boundaries;
- DCR bounds, redirect-URI grammar, garbage collection, and registration rate
  limits;
- bearer middleware matrices: absent, malformed, expired, revoked, and
  cross-user tokens produce byte-identical closed failures;
- tool-level matrices proving each tool enforces scope, bounds, sanitizing, and
  CAS identically to its REST counterpart, including hostile markup and
  oversized payloads through MCP;
- an integration run driving the official Go SDK client through discovery,
  authorization, token exchange, and every tool against the live server;
- one Playwright process through the HTTPS overlay: login, authorize, consent,
  an agent builds a resume over MCP, the result is visible in the editor, and
  revocation cuts the agent off (`make dev-https-mcp-check`).

The integration owner runs the unchanged-candidate phase checklist, `make ci`,
and connected `make scan`. The fresh reviewer names the PKCE, code-replay,
token-rotation, revocation, scope, CSRF/cookie-isolation, redirect, enumeration,
rate-limit, sanitizing, and CAS invariants in the verdict.

## Rollout and authority changes

PM is its own phase and shares no partially edited surface with P5B, P6, P7, or
P8. The implementation plan starts with these serialized owner changes:

1. Update Approved v4 product, API, security, data, and web design text and
   accept the MCP agent access ADR.
2. Add budget rows, public-roots registry v6, acceptance rows, and the
   `modelcontextprotocol/go-sdk` pin.
3. Add the migration and regenerate sqlc output.
4. Build and verify the authorization server, resource server, tools, and UI in
   bounded task waves.
5. Pass the native HTTPS MCP UAT before any cloud or DNS action.

The feature is additive: no existing route, session, or resume behavior changes
for users who never authorize an agent.

## References

- [MCP specification: authorization](https://modelcontextprotocol.io/specification/latest/basic/authorization)
- [MCP specification: transports](https://modelcontextprotocol.io/specification/latest/basic/transports)
- [OAuth 2.1 (draft-ietf-oauth-v2-1)](https://datatracker.ietf.org/doc/html/draft-ietf-oauth-v2-1)
- [RFC 7636, Proof Key for Code Exchange](https://www.rfc-editor.org/rfc/rfc7636.html)
- [RFC 7591, Dynamic Client Registration](https://www.rfc-editor.org/rfc/rfc7591.html)
- [RFC 8414, Authorization Server Metadata](https://www.rfc-editor.org/rfc/rfc8414.html)
- [RFC 9728, Protected Resource Metadata](https://www.rfc-editor.org/rfc/rfc9728.html)
- [RFC 7009, Token Revocation](https://www.rfc-editor.org/rfc/rfc7009.html)
- [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)
