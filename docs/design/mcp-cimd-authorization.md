# MCP CIMD authorization

Status: approved design; implementation is next.
[MCP Client ID Metadata Documents](mcp-cimd.md) holds the motivation, sources,
release order, and the approval record.

Discovery, consent, stored clients, errors, privacy, and compatibility belong to
this boundary. See [fetch](mcp-cimd-fetch.md) and
[verification](mcp-cimd-verification.md).

## Discovery

Authorization server metadata adds
`"client_id_metadata_document_supported":true` while the feature flag is on
([Release order](mcp-cimd.md#release-order)). `registration_endpoint` stays, and
`token_endpoint_auth_methods_supported` stays `["none"]`. A client without CIMD
support, and every client while the flag is off, keeps using DCR.

The server tells the two kinds of `client_id` apart by form. A value that starts
with `https://` is a client ID URL. Preserve the existing UUID parser for DCR;
anything else is an invalid client identifier. A public UUID lookup must require
`metadata_url IS NULL`: the internal UUID of a URL client is never an alternate
public client ID. A URL lookup uses exact `metadata_url` equality with no URL
normalization. A URL need not already have a row to reach consent.

Internal lookups and row locks still accept UUIDs for both kinds. Keep those
store operations separate from public client resolution so token, revocation,
and cleanup paths can follow their existing foreign keys.

`OAUTH_CIMD_ENABLED` is a startup Boolean: unset or `false` disables CIMD,
`true` enables it, and other values fail configuration. `MCP_ENABLED` remains
the parent gate for the entire agent surface. With MCP off, CIMD exposes no
route. With only CIMD off, authorize and both consent operations refuse URL
clients before DNS, cache access, or storage writes. Metadata omits the CIMD
property rather than emitting `false`. Existing token and revocation authority
continues as described under [Older clients and loss](#older-clients-and-loss).
The metadata response keeps its existing cache policy; clients can temporarily
see an old advertisement after a flag change. Enforcement never trusts it.

## Errors

| Where               | Failure                                                                            | Response                                                                        |
| ------------------- | ---------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| `/oauth/authorize`  | URL, fetch, document, redirect, or disabled CIMD                                   | `400` plain text `Bad Request`; no `Location`                                   |
| `/oauth/authorize`  | Fetch admission refused                                                            | `429` plain text `Too Many Requests`, `Retry-After`; no `Location`              |
| Consent GET or POST | URL, fetch, document, redirect, or disabled CIMD                                   | `400 request_invalid` in the existing API envelope; no `redirectTo`             |
| Consent GET or POST | Fetch admission refused                                                            | `429 rate_limited`, message `too many requests`, `Retry-After`; no `redirectTo` |
| Authorize           | Trusted client and redirect, invalid other parameter                               | Existing `invalid_request` or `invalid_scope` redirect                          |
| Code exchange       | Malformed URL client                                                               | `400 invalid_client`, as for a malformed DCR ID                                 |
| Code exchange       | Well-formed URL with no row, or internal UUID alias                                | `401 invalid_client`                                                            |
| Code exchange       | Known client differs from the code                                                 | `400 invalid_grant`                                                             |
| Refresh             | Found refresh token and a supplied different client ID                             | `400 invalid_grant`, no rotation or revocation                                  |
| Refresh             | Missing, malformed, or wrong-kind token and a supplied well-formed URL with no row | `401 invalid_client`                                                            |
| Refresh             | Other missing, dead, malformed token or malformed claimed client ID                | `400 invalid_grant`                                                             |

Client lookup failures caused by the database produce the existing generic
`500`, never an unknown-client error. Token errors keep the existing fixed
`error_description`, `The request is invalid.` Code, resource, form, scope,
PKCE, and failed-grant checks retain their existing precedence except the
required URL-to-row lookup. The failed-grant limiter keys a stored URL client by
its internal UUID, just like DCR; unknown clients remain under the IP limit.
CIMD never consumes a client secret or assertion. The closed token form rejects
`client_secret`, `client_assertion`, and `client_assertion_type` as unknown
fields. An HTTP Authorization header supplies no client identity or authority;
all public-client code, PKCE, token, and claimed-client checks still apply.

`/oauth/revoke` keeps exactly `token` and optional `token_type_hint`; it does
not accept `client_id` and performs no metadata lookup or fetch. A validly
shaped unknown token still returns `200`. Known tokens locate their client
through the token row. This preserves the RFC 7009 no-oracle behavior.

A failed document check never returns to an untrusted URI, including on denial.
Go returns plain text or JSON, not a new HTML error page. Nuxt maps consent
`400` to its existing bilingual invalid-request message, and `429` to its
existing unavailable message; neither includes a remote failure reason. Session,
CSRF, reauthentication, and grant-limit failures keep their existing contracts.
Consent responses retain `Cache-Control: no-store, no-transform`. Authorize
responses carrying request or code state must also be non-cacheable.

## Consent

For a client ID URL, the consent page leads with the host, not the self-asserted
name:

- “**{host}** muốn truy cập CV của bạn.” / “**{host}** wants access to your
  resumes.”
- Under it: “Ứng dụng tự gọi là “{client_name}”.” / “The app calls itself
  “{client_name}”.”
- Then the existing line naming where approval returns
  ([rule 6](mcp-client-compatibility.md#6-where-approval-returns)).
- For a loopback redirect, a warning: “Chỉ cho phép nếu bạn vừa bắt đầu đăng
  nhập từ một ứng dụng trên máy tính này.” / “Approve only if you just started
  this sign-in from an app on this computer.”

The host and name render as text, never markup or links. C6 approves both
languages. Connected agents in Settings shows the same host beside the stored
name.

**Silent reauthorization.** An authorize request whose user already holds a live
grant covering the scopes gets a code without the consent page. For a URL client
with a loopback redirect, the server always shows consent instead. Every Claude
Code user shares one `client_id` and its loopback redirects, so without this
rule any local process could take a code silently by naming that `client_id` and
its own port. A URL client with an `https` redirect keeps silent
reauthorization: its code can only reach a redirect the current document lists,
on the client ID URL's own host. The claude.ai document in
[Sources](mcp-cimd.md#sources) fits that case: its one redirect is on
`claude.ai`, the client ID URL's host, so it passes C3, and C5 leaves its silent
reauthorization on.

## Stored clients

A URL client has an `oauth_clients` row with an internal UUID. Codes, grants,
and tokens keep their UUID foreign keys. The public identifier is always the
exact URL; browser consent and token requests never substitute the internal
UUID.

- Add nullable `metadata_url text`, checked against the URL byte bound, and a
  partial unique index on non-null values with bytewise equality
  (`COLLATE "C"`). Existing DCR rows stay null. No backfill or document hash on
  a grant is needed: metadata changes do not revoke grants under the approved
  policy.
- `ConsentQuery` carries the public string separately from the resolved row
  identity. Each authorize or consent operation resolves one immutable metadata
  snapshot before database locks. Reuse that validated snapshot through that
  operation's redirect sink; never fall back to `registeredRedirect` for a URL
  client. A later operation resolves metadata again under the cache rules.
- Anonymous authorization and consent reads create no row. A denial can return
  `access_denied` for a validated URL and redirect without creating a row.
- Approval inserts or locks the unique URL row, then locks user, concrete
  session, factor policy, and existing grant in the established order. Apply
  `requireConsentAuthority`, the user grant cap, and the existing grant upsert;
  commit the metadata update, grant, and code together. Any failure rolls back
  the new row and metadata too. A concurrent first approval must resolve the
  same URL to the same UUID through the unique index, not create two clients.
- Fetch before entering that transaction. Metadata validity is evaluated for the
  operation's snapshot; an expiry during a bounded database transaction does not
  trigger network I/O under a lock. HTTPS silent authorization must recheck the
  live grant, scope coverage, session authority, and epoch under locks. Loopback
  URL clients never enter silent code issue.
- `client_name` and `redirect_uris` are the latest approved snapshot. They are
  shared client display metadata, so approval by one account can update the
  displayed name for another account connected to the same URL. Identity and
  host remain the URL. No per-grant display history is retained.
- Reapproval updates the existing live grant's scopes and epoch; its ID and
  `created_at` remain unchanged, as in `UpsertOAuthGrant`. It does not consume
  another grant slot or replace an active refresh family. A revoked grant is not
  revived; a fresh approval creates a new live grant.
- Code exchange resolves a URL to a row before checking the code binding, then
  locks the same row again. Refresh with a found token compares a supplied
  client ID to that row's public identifier before the reuse check. Omitted
  refresh `client_id` remains allowed. Token paths never fetch or require the
  flag. Internal UUID aliases fail even if the code or token belongs to them.
- The idle-client sweep retains its current predicate: the row was created more
  than the idle age in [Budgets](budgets.md) ago and has neither an unrevoked
  grant nor a live token. It is not a timer starting at last token expiry. A
  grant remains until revoked or deleted, even after all families expire. Sweep
  and approval serialize through the client-row lock.

## Session API contract

In `docs/api/openapi.yaml`, the consent GET `client_id` parameter and
`OAuthConsentDecisionRequest.client_id` become a string union of the DCR UUID
and the accepted CIMD URL grammar, bounded by [Budgets](budgets.md#cimd).
Describe CIMD loopback matching on `redirect_uri`. The POST body, duplicate
member rejection, required authorization fields, and `decision` stay intact.

Add these required response fields to `OAuthConsentResponse.data` and
`OAuthAgentGrant`:

| Field        | Type                | Meaning                                            |
| ------------ | ------------------- | -------------------------------------------------- |
| `clientKind` | `"dcr"` or `"cimd"` | Server-derived from the stored row or resolved URL |
| `clientHost` | string or null      | ASCII URL hostname for CIMD; null for DCR          |

Do not expose the full metadata URL or internal client UUID in these views.
Consent GET derives the host from the validated request; grant listing derives
it from stored `metadata_url` without fetching. Add `429` and `Retry-After` to
both consent operations. Existing `clientName`, scopes, grant ID, timestamps,
and decision success `{data:{redirectTo}}` remain. Regenerate the web API types
from OpenAPI; raw OAuth and MCP routes remain outside OpenAPI under ADR 0018.

## Consent layout

Keep the existing single-column container: full available width, maximum 26 rem,
horizontal padding 1.5 rem, vertical padding 4 rem. Use the same layout at all
widths, including the 390 px and 1440 px proof viewports. For CIMD, use the
approved host-first sentence as the `h1` (1.25 rem, weight 600), the quoted name
below (1 rem, top margin 1 rem), then the return line (1 rem, top margin 0.25
rem) and loopback warning (1 rem, top margin 0.5 rem). Preserve the scope list,
approval and denial buttons, loading, failure focus, and keyboard order.

Keep ASCII hosts visible in full with wrapping, isolated left-to-right
rendering, and no ellipsis. Isolate the self-asserted name in its own text span
so bidi formatting cannot move text into the host sentence. Show no icon, logo,
link, reserved-name badge, or verified mark. In Connected agents, append the
host in parentheses beside the existing name, with wrapping on small screens.
DCR keeps its existing presentation, with the shared loopback return line from
[rule 6](mcp-client-compatibility.md#6-where-approval-returns).

The return line includes the actual redirect hostname as text, including for
loopback, as [MCP requires][mcp-security]. Rule 6 owns its exact bilingual copy.
Preserve C6's approved warning unchanged.

## Data and privacy

| Data                              | Where               | Kept                                                |
| --------------------------------- | ------------------- | --------------------------------------------------- |
| URL, `client_name`, redirect list | Memory cache        | Clamped cache lifetime, or until evicted or restart |
| URL, `client_name`, redirect list | `oauth_clients` row | Until eligible under the stored-client sweep above  |
| Fetched body                      | Nowhere             | Parsed and dropped                                  |

Structured logs may carry the URL host, a SHA-256 digest of the URL, the client
row ID when present, and a closed outcome such as `address_refused`, `status`,
`content_type`, `too_large`, `deadline`, `document_invalid`,
`client_id_mismatch`, `redirect_mismatch`, or `rate_limited`. They never carry
the body, the full URL path, a `client_name`, or a redirect URI. Metrics use
closed outcome labels only, never hosts, URL digests, or row IDs. Redact
transport errors such as `url.Error`, request query strings, and headers before
logging; generic middleware must not log the URL-valued client ID. As the draft
notes, a fetch tells the client's operator that someone is signing in at aboutme
around that time; the request carries no account identifier. A client can put
identifying data in a custom URL path, so do not claim the stored URL is always
non-personal. Keep raw URLs out of telemetry and do not add user identifiers to
fetches.

**Migration.** Add one forward migration after the current `00011` head;
serialize its number with the primary. Add the column, constraint, and partial
index before the new binary starts. DCR keeps null values; no existing grants,
codes, tokens, or resume data are rewritten. Keep existing `aboutme_app` table
permissions and prove access through the migration tests. Do not edit generated
sqlc output by hand. The column has the URL bound in Budgets, with index and row
overhead in addition.

## Older clients and loss

DCR clients, rows, grants, and tokens do not change. The Go SDK runner has no
client ID URL configuration in the current runner; retain its DCR proof.
Existing DCR grants are not merged into URL grants. A first CIMD approval uses
the existing grant cap and grant-limit message.

Turning the flag off stops advertising CIMD and refuses new authorizations for
URL clients. Token, refresh, and revocation keep serving existing URL clients,
so connected users stay connected. Claude rereads metadata about every five
minutes ([lazy authentication][claude-lazy]) and then falls back to DCR for new
connections.

Use CIMD flag disablement in a CIMD-aware binary for normal rollback. Permit a
binary without CIMD support only when no URL-client rows remain or
`MCP_ENABLED=false` keeps the entire agent surface disabled. Keep MCP disabled
until a CIMD-aware binary returns or the URL rows have been removed. The
deployment check needs only a Boolean existence query, never client or account
data in its output. Recheck with the serving app stopped under the deployment
operation lock before starting the target, including failed-deploy restoration.
A query or configuration failure refuses the start; it never means no URL rows.
Retain the CIMD-aware deployment helpers from clean current main when selecting
older application images, as the existing deployment eligibility gate requires.

Keeping the column alone does not make an old binary safe: its unrestricted UUID
lookup treats a URL row as DCR and can silently issue codes from the stored
redirect list, bypassing the current document and loopback consent rules. Code
exchange naming a URL fails the old UUID parser; a refresh omitting `client_id`
and an issued access token can still work while MCP is enabled. Binary rollback
is neither a revocation mechanism nor a connection guarantee.

Keep the additive migration on binary rollback. A destructive down migration
must refuse while URL rows exist, since dropping the column would also turn
internal UUIDs into DCR aliases. Deliberately removing those rows first cascades
their codes, grants, and tokens and requires a separate owner-authorized
data-loss operation. Affected clients then reconnect through DCR.

[claude-lazy]: https://claude.com/docs/connectors/building/lazy-authentication
[mcp-security]:
  https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization/security-considerations#localhost-redirect-uri-risks
