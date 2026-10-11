# MCP Client ID Metadata Documents

Status: proposal. Nothing here is built. Each choice marked **Owner decision**
waits for the owner; the list is in [Owner decisions](#owner-decisions).

This design lets an MCP client name itself with an HTTPS URL instead of
registering. The authorization server fetches that URL, reads the client's
metadata, and checks the request against it. It keeps
[ADR 0018](../adr/0018-mcp-agent-access.md): one endpoint, first-party OAuth
2.1, public clients with PKCE, account-wide scopes, and no publish tool. Dynamic
client registration (DCR) stays, unchanged, as
[MCP client compatibility](mcp-client-compatibility.md) defines it.

The proposal covers [metadata fetching](mcp-cimd-fetch.md),
[authorization](mcp-cimd-authorization.md), and
[verification](mcp-cimd-verification.md).

## Why

Claude registers a new DCR client on every fresh connection ([Claude connector
authentication][claude-auth]). Each one costs a row, a registration under the
shared egress bucket, and, after consent, one of the user's ten grants. With
Client ID Metadata Documents (CIMD), every user of one client presents the same
`client_id`, so a reconnect reuses one client and one grant per user.

Claude's documentation names Claude Code's client ID URL. It says the hosted
apps use a document "that Anthropic hosts" but names no URL; that they send the
claude.ai document in [Sources](#sources) is **unverified** until the owner
proof.

## Sources

- [MCP authorization specification][mcp-auth], 2025-11-25: authorization servers
  SHOULD support CIMD and MAY support DCR; clients prefer CIMD when the server
  advertises it. Its CIMD security section says authorization servers "MUST
  clearly display the redirect URI hostname during authorization" and "SHOULD
  display additional warnings for `localhost`-only redirect URIs".
- [OAuth Client ID Metadata Document][cimd-02],
  `draft-ietf-oauth-client-id-metadata-document-02` (6 July 2026), the current
  revision. The MCP specification cites `-00`; the rules below follow `-02`,
  which is stricter, and satisfy `-00` too.
- [Claude connector authentication][claude-auth] and [lazy
  authentication][claude-lazy]: Claude uses CIMD only when the server metadata
  has both `"client_id_metadata_document_supported": true` and `"none"` in
  `token_endpoint_auth_methods_supported`.
- Claude Code's document at
  `https://claude.ai/oauth/claude-code-client-metadata`, fetched 2026-10-01:
  `application/json`, `Cache-Control: public, max-age=300`, about 300 bytes,
  `token_endpoint_auth_method` `none`, and the redirects
  `http://localhost/callback` and `http://127.0.0.1/callback` with no port.
- A document at `https://claude.ai/oauth/mcp-oauth-client-metadata`, fetched
  2026-10-01 but not named in Claude's documentation: `client_name` `Claude`,
  `application/json`, `Cache-Control: public, max-age=300`,
  `token_endpoint_auth_method` `none`, the one redirect
  `https://claude.ai/api/mcp/auth_callback`, and `grant_types`
  `authorization_code`, `refresh_token`, and
  `urn:ietf:params:oauth:grant-type:jwt-bearer`.

## Discovery

See [Discovery](mcp-cimd-authorization.md#discovery).

## Client ID URL rules

See [Client ID URL rules](mcp-cimd-fetch.md#client-id-url-rules).

## Fetch

See [Fetch](mcp-cimd-fetch.md#fetch).

## SSRF guard

See [SSRF guard](mcp-cimd-fetch.md#ssrf-guard).

## Document rules

See [Document rules](mcp-cimd-fetch.md#document-rules).

## Errors

See [Errors](mcp-cimd-authorization.md#errors).

## Cache

See [Cache](mcp-cimd-fetch.md#cache).

## Consent

See [Consent](mcp-cimd-authorization.md#consent).

## Stored clients

See [Stored clients](mcp-cimd-authorization.md#stored-clients).

## Rate limits

See [Rate limits](mcp-cimd-fetch.md#rate-limits).

## Abuse cases

See [Abuse cases](mcp-cimd-verification.md#abuse-cases).

## Data and privacy

See [Data and privacy](mcp-cimd-authorization.md#data-and-privacy).

## Older clients and loss

See [Older clients and loss](mcp-cimd-authorization.md#older-clients-and-loss).

## Release plan

One release, backend and frontend together, with the flag `OAUTH_CIMD_ENABLED`
off in production (**Owner decision** C1):

1. Backend: migration, URL rules, fetcher and guard, cache, limits, document and
   redirect rules, row lookup at the token endpoints, metadata gated by the
   flag, and OpenAPI for a client host and kind on the consent and grant-list
   responses.
2. Frontend: the consent lines and loopback warning, and the host in Connected
   agents.
3. CI proof with the flag on, then the owner turns it on in production and runs
   the [owner proof](mcp-cimd-verification.md#acceptance-checks). The
   [MCP guide](mcp-guide.md) changes after that proof.

**Design text to update in the build:** [Data](data.md) for `oauth_clients`,
[Security](security.md#agent-authorization-and-the-bearer-world),
[API](api.md#agent-access-and-the-bearer-world), the OAuth server contract in
[MCP owner workflow](mcp-owner-workflow.md#oauth-server-contract),
[Budgets](budgets.md), and [Vietnam production](vietnam-production.md) for the
flag. New `AC-MCP-*` rows in the traceability matrix follow approval.

## Acceptance checks

See [Acceptance checks](mcp-cimd-verification.md#acceptance-checks).

## Owner decisions

| ID  | Decision                                                                 | Recommendation                                                               |
| --- | ------------------------------------------------------------------------ | ---------------------------------------------------------------------------- |
| C1  | Build CIMD now, behind `OAUTH_CIMD_ENABLED`, off until the owner proof   | Yes; turning it off keeps existing grants working                            |
| C2  | Port-agnostic loopback match, `localhost` included, for URL clients only | Yes; Claude Code's document needs it, DCR stays exact                        |
| C3  | An `https` redirect's host must equal the client ID URL's host           | Yes; consent then names one host                                             |
| C4  | `token_endpoint_auth_method` must be present and `none`                  | Yes; RFC 7591 reads an absent value as a secret method                       |
| C5  | No silent reauthorization for loopback redirects of URL clients          | Yes; one shared `client_id` makes it a silent-code path                      |
| C6  | Consent copy: host first, name as a quote, loopback warning              | Approve the [Vietnamese and English text](mcp-cimd-authorization.md#consent) |
| C7  | Cache floor 5 minutes, ceiling 1 hour, and the four fetch limits         | Approve for Budgets                                                          |
| C8  | No reserved-name list and no "verified" mark                             | Leave out; revisit if impersonation appears                                  |
| C9  | Grants survive document changes and removal                              | Yes; the draft leaves it to the server                                       |
| C10 | Refuse ports, queries, and IP literals in a client ID URL                | Yes; stricter than the draft, no known client needs them                     |
| C11 | Ignore unknown `grant_types` values in a document, unlike DCR            | Yes; the claude.ai document lists `jwt-bearer`                               |

[claude-auth]: https://claude.com/docs/connectors/building/authentication
[claude-lazy]: https://claude.com/docs/connectors/building/lazy-authentication
[mcp-auth]:
  https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization
[cimd-02]:
  https://datatracker.ietf.org/doc/html/draft-ietf-oauth-client-id-metadata-document-02
