# Authentication and agent access

This page describes implemented behavior. See the
[running-system overview](../architecture.md) for deployment and dependencies.

## Implemented HTTP surface

The [OpenAPI document](../api/openapi.yaml) is the exact JSON API authority.
OAuth protocol and MCP endpoints follow their protocol contracts and the
accepted [agent-access ADR](../adr/0018-mcp-agent-access.md). The implemented
surface includes:

- `GET` and `HEAD` health and readiness probes;
- an unauthenticated capabilities read that reports the enabled providers,
  password-registration availability, and agent-access availability;
- Google and LinkedIn OpenID Connect plus GitHub OAuth login, whose routes are
  registered only for the providers `PROVIDER_LOGIN_ENABLED` enables (none by
  default);
- email-and-password registration, verification, login, reauthentication,
  add/change, and reset;
- authenticated, CSRF-protected provider link and reauthentication starts;
- OAuth dynamic client registration, S256 authorization-code consent, token
  exchange/rotation/revocation, discovery metadata, and bearer-authenticated
  Streamable HTTP MCP;
- current-user lookup, logout, session listing, per-session revoke, and
  logout-everywhere;
- owner-only resume list, create, read, metadata update, and delete;
- owner-only personal-details, entry, section, structure, and customization
  mutations;
- owner-only photo upload, read, crop, and delete over private media;
- owner-only publish, unpublish, rename, and slug delete, with public JSON,
  photo, HTML, Markdown, sitemap, robots, and llms.txt reads gated by publish
  state, SEO/GEO, and download flags;
- owner and anonymous public Server-Sent Events (SSE) carrying revision
  metadata;
- JSON envelopes, request IDs, body bounds, security and cache headers,
  trusted-proxy client-IP handling, and rate limiting.

Browser sessions use opaque PostgreSQL-backed tokens with rotation and bounded
predecessor grace. Cookie-authenticated mutations retain session, Origin, and
CSRF checks. The HTTP boundary emits structured logs.

Password credentials are one Argon2id hash per user; verification and reset
tokens are stored only as digests. Transactional authentication mail is sealed
into an encrypted outbox and delivered by a bounded worker through Bizfly SMTP
(production), SES, or a loopback-only, secret-authenticated capture server
(development). Every password login and credential mutation serializes on the
user lock and fences session issuance and reset.

See the [web API client source map](../../apps/web/README.md#api-client) for
generated TypeScript types and regeneration commands.

## Implemented agent access

Users authorize public agent clients with account-wide `resumes:read` and
`resumes:write` scopes. Client redirect URIs are exact-match; codes are
60-second, digest-only, single-use, and bound to S256 PKCE. Access and rotating
refresh tokens are stored only as digests. Code replay, refresh-token replay,
and connected-agent revocation invalidate the applicable token family.

The `/mcp` bearer boundary never reads cookies. Its fifteen tools provide
private editor parity for resume read/write and photo operations, including
delete of private or published resumes, but expose no publish, unpublish, or
public-read capability. Mutations share the REST ownership, validation,
sanitizing, bounds, and revision compare-and-swap path. The settings page lists
live grants and revokes them through the existing session, Origin, and CSRF
chain.

`make dev-https-mcp-check` proves registration, provider login, consent, token
exchange, exact tool discovery, agent-created editor content, grant revocation,
and the revoked token's closed 401 over the trusted local HTTPS origin. The
proof writes only bounded boolean/error-count evidence and cleans its reserved
user, resume, client, grant, code, and token rows.

Local native development and HTTPS proof commands seed one account and one
private sample resume; Compose and cloud never run the seed.
