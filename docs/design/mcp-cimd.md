# MCP Client ID Metadata Documents

Status: approved design; implementation is next.

C1 to C9 approved by the owner on 2026-10-13; C10 and C11 decided as recommended
on the owner's instruction the same day. The [decision record](#owner-decisions)
includes the approved bilingual consent copy. The owner approved the
loopback-hostname addition to C6 on 2026-10-14.

This design lets an MCP client name itself with an HTTPS URL instead of
registering. The authorization server fetches that URL, reads the client's
metadata, and checks the request against it. It keeps
[ADR 0018](../adr/0018-mcp-agent-access.md): one endpoint, first-party OAuth
2.1, public clients with PKCE, account-wide scopes, and no publish tool. Dynamic
client registration (DCR) stays, unchanged, as
[MCP client compatibility](mcp-client-compatibility.md) defines it.

The design covers [metadata fetching](mcp-cimd-fetch.md),
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

- [MCP authorization specification][mcp-auth], 2026-07-28, checked through the
  latest-version link on 2026-10-14. [Client registration][mcp-registration]
  prefers CIMD over DCR after pre-registration; DCR is deprecated but permitted
  as fallback. [CIMD security][mcp-security] requires the redirect hostname on
  consent and recommends a warning for loopback. The previously cited 2025-11-25
  edition has the same hostname requirement. This feature does not upgrade the
  pinned MCP SDK or claim full conformance to a newer transport.
- [OAuth Client ID Metadata Document][cimd-02],
  `draft-ietf-oauth-client-id-metadata-document-02` (6 July 2026), the current
  published revision, checked on 2026-10-14. MCP cites `-00`; this design uses
  `-02` and states its local restrictions in the fetch contract. The
  Internet-Draft remains work in progress, not an RFC.
- [Claude connector authentication][claude-auth] and [lazy
  authentication][claude-lazy]: Claude uses CIMD only when the server metadata
  has both `"client_id_metadata_document_supported": true` and `"none"` in
  `token_endpoint_auth_methods_supported`.
- Historical document observations below come from the existing design. Direct
  re-fetch attempts on 2026-10-13 failed in the research tool, so their current
  headers and bodies still need verification during the owner proof.
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

## Trade-offs and alternatives

CIMD avoids registration churn but makes new authorization depend on DNS, TLS,
and a client-operated document. DCR remains the fallback for clients without a
stable public URL. Pre-registering selected clients would require operator
coordination; fetching at token use would add an external dependency to every
refresh. Neither is required. Public client URLs identify document publishers;
they do not authenticate the process requesting a code.

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

## Release order

One release, backend and frontend together, with the flag `OAUTH_CIMD_ENABLED`
off in production:

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
[MCP owner workflow](mcp-owner-workflow.md#oauth-server-contract), and
[Vietnam production](vietnam-production.md) for the flag. New `AC-MCP-*` rows in
the traceability matrix accompany implementation.

## Implementation boundaries

These are code navigation points, not claims that CIMD is implemented. Paths
below are relative to `apps/server/` unless another root is named.

| File and existing entry point                                                                                                                                                  | Required change                                                                                                                                       |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| `internal/config/config.go`: `Load`; `internal/config/agent_access.go`: `AgentAccessConfig`, `loadAgentAccessConfig`, `ValidateAgentAccess`                                    | Load and validate the CIMD flag, stub restriction, and complete fetch budgets                                                                         |
| `cmd/server/main.go`: `newAgentAccessRoutes`                                                                                                                                   | Construct one process-wide fetcher/cache and pass it to OAuth service; keep the parent MCP gate                                                       |
| `internal/oauthsrv/clients.go`: `ServiceDependencies`, `Service`, `NewService`                                                                                                 | Accept the configured CIMD resolver; keep `HandleRegister` and DCR validation behavior                                                                |
| New `internal/oauthsrv/cimd*.go`                                                                                                                                               | Own URL parsing, immutable validated metadata, pinned fetch, cache, rate admission, and public-ID resolution                                          |
| `internal/oauthsrv/redirect.go`: `ValidateRedirectURI`, `ValidateClientName`; `rate.go`: `registerAddressKey`, `NewRatePolicies`                                               | Reuse DCR grammar and canonical-address policy; apply CIMD matching and fetch budgets separately so DCR does not widen                                |
| `internal/oauthsrv/metadata.go`: `HandleMetadata`                                                                                                                              | Advertise the startup flag; keep protected-resource metadata unchanged                                                                                |
| `internal/oauthsrv/authorize.go`: `authorizeRequest`, `consentQueryFromValues`, `HandleAuthorize`, `redirectIssuedCode`                                                        | Preserve public string identity, validate current metadata, suppress silent loopback issue, and use the validated redirect snapshot at the final sink |
| `internal/oauthsrv/consent.go`: `ConsentQuery`, `ConsentContext`, `ConsentDecision`, `denyConsent`, `approveConsent`, `approveExistingGrant`, `validateConsentQuery`, `values` | Separate wire URL from UUID foreign keys, resolve before locks, and upsert only during approval                                                       |
| `internal/oauthsrv/session_http.go`: view types, `decodeConsentDecision`, `handleConsentContext`, `handleAgentGrants`, `writeConsentSessionError`                              | Decode URL IDs; emit client kind/host and the closed fetch failures                                                                                   |
| `internal/oauthsrv/token_endpoint.go`: `grantRateClientID`, `exchangeAuthorizationCode`, `rotateRefreshToken`, `missingRefreshTokenError`                                      | Resolve stored public identity without fetching; reject internal UUID aliases and preserve limiter and token binding                                  |
| `sql/queries.sql`: client lookups and `ListLiveOAuthGrantsForUser`; `internal/store/oauth_contract.go`: `OAuthQueries`                                                         | Add exact URL lookup/upsert, expose metadata URL to owner views, and preserve lock order                                                              |
| `migrations/`: next forward migration                                                                                                                                          | Add nullable URL, constraint, and unique index under the authorization contract                                                                       |
| Generated `internal/store/models.go`, `queries.sql.go`, `querier.go`                                                                                                           | Regenerate with sqlc from migration/query sources                                                                                                     |
| `internal/oauthsrv/revoke.go`: `HandleRevoke`, `revokeToken`; `clients_gc.go`: `CollectIdleClients`                                                                            | Preserve token-based revocation and existing sweep predicate; add URL-client regression coverage                                                      |
| `internal/mcpapi/bearer.go`: `Authenticate`; `server.go`: `NewServer`                                                                                                          | Preserve token, epoch, scope, and tool boundaries; prove URL-client tokens through the existing chain                                                 |

Frontend owners update `apps/web/app/pages/authorize.vue` (`load`, `submit`,
`returnToLine`, and template), `app/i18n/consent.ts`,
`app/composables/useOAuthConsent.ts` (`isConsentView` and error mapping),
`app/composables/agentGrants.ts` (`isGrant`), and
`app/components/settings/ConnectedAgents.vue`. Coordinate
`docs/api/openapi.yaml` and generated `apps/web/app/api/generated/openapi.ts`
with the backend contract owner. Deployment flag plumbing belongs in the devops
lane and ships with the feature, defaulted off.

The production setting lives in `deploy/vn/host/env/server.env`; document it in
`.env.example`. The HTTPS harness entry points are `scripts/dev-https.sh`,
`deploy/dev-https-browser/run.sh`, and
`deploy/dev-https-browser/mcp-ts-sdk.spec.ts`. The primary assigns those shared
paths explicitly to devops and QA; the Go SDK runner stays on DCR. Devops also
enforces the
[binary rollback boundary](mcp-cimd-authorization.md#older-clients-and-loss) in
`deploy/vn/scripts/deploy-host.sh`: `step_render`, `step_restore_previous`, and
the final `step_app_up` check cover explicit rollback and failed-deploy
restoration. `deploy/vn/scripts/deploy.sh` coordinates that check under the
existing operation lock before a pre-CIMD image can serve traffic.

## Acceptance checks

See [Acceptance checks](mcp-cimd-verification.md#acceptance-checks).

## Owner decisions

All recommendations below are decided as recorded at the top of this page. C6
includes both languages at [Consent](mcp-cimd-authorization.md#consent).

| ID  | Decision                                                                 | Approved recommendation                                                                                                                                                                                           |
| --- | ------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| C1  | Build CIMD now, behind `OAUTH_CIMD_ENABLED`, off until the owner proof   | Yes; turning it off keeps existing grants working                                                                                                                                                                 |
| C2  | Port-agnostic loopback match, `localhost` included, for URL clients only | Yes; Claude Code's document needs it, DCR stays exact                                                                                                                                                             |
| C3  | An `https` redirect's host must equal the client ID URL's host           | Yes; consent then names one host                                                                                                                                                                                  |
| C4  | `token_endpoint_auth_method` must be present and `none`                  | Yes; RFC 7591 reads an absent value as a secret method                                                                                                                                                            |
| C5  | No silent reauthorization for loopback redirects of URL clients          | Yes; one shared `client_id` makes it a silent-code path                                                                                                                                                           |
| C6  | Consent copy: host first, name as a quote, loopback warning              | Bilingual copy approved 2026-10-13; owner approved appending `({redirect_host})` before the loopback return line's final period in both languages on 2026-10-14; see [Consent](mcp-cimd-authorization.md#consent) |
| C7  | Cache lifetime and fetch limits in [Budgets](budgets.md#cimd)            | Approved values live in Budgets                                                                                                                                                                                   |
| C8  | No reserved-name list and no "verified" mark                             | Leave out; revisit if impersonation appears                                                                                                                                                                       |
| C9  | Grants survive document changes and removal                              | Yes; the draft leaves it to the server                                                                                                                                                                            |
| C10 | Refuse ports, queries, and IP literals in a client ID URL                | Yes; stricter than the draft, no known client needs them                                                                                                                                                          |
| C11 | Ignore unknown `grant_types` values in a document, unlike DCR            | Yes; the claude.ai document lists `jwt-bearer`                                                                                                                                                                    |

[claude-auth]: https://claude.com/docs/connectors/building/authentication
[claude-lazy]: https://claude.com/docs/connectors/building/lazy-authentication
[mcp-auth]:
  https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization
[cimd-02]:
  https://datatracker.ietf.org/doc/html/draft-ietf-oauth-client-id-metadata-document-02
[mcp-registration]:
  https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization/client-registration
[mcp-security]:
  https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization/security-considerations
