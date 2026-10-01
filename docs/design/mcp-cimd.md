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

## Why

Claude registers a new DCR client on every fresh connection ([Claude connector
authentication][claude-auth]). Each one costs a row, a registration under the
shared egress bucket, and, after consent, one of the user's ten grants. With
Client ID Metadata Documents (CIMD), every Claude Code user presents the same
`client_id`, so a reconnect reuses one client and one grant per user, and
registration traffic from Claude stops.

## Sources

- [MCP authorization specification][mcp-auth], 2025-11-25: authorization servers
  SHOULD support CIMD and MAY support DCR; clients prefer CIMD when the server
  advertises it. Its CIMD security section requires the redirect host on the
  consent screen and recommends a warning for loopback-only redirects.
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

## Discovery

Authorization server metadata adds
`"client_id_metadata_document_supported":true` while the feature flag is on
([Release plan](#release-plan)). `registration_endpoint` stays, and
`token_endpoint_auth_methods_supported` stays `["none"]`. A client without CIMD
support, and every client while the flag is off, keeps using DCR.

The server tells the two kinds of `client_id` apart by form. A value that starts
with `https://` is a client ID URL. Any other value must be a canonical UUID, as
DCR issues. Anything else is an unknown client. No value can be both.

## Client ID URL rules

The `client_id` is checked as a string before any fetch. All of these must hold,
or the request fails with no network access:

1. 9 to 256 bytes of printable ASCII, with no `#`.
2. Scheme exactly `https`, lowercase.
3. Host is a lowercase DNS name in ASCII (an IDN arrives as `xn--` labels), with
   at least one dot and no trailing dot. IP literals are refused.
4. No userinfo, no port (not even `:443`), no query, and no fragment.
5. A path of at least one character after the leading `/`, with no `.` or `..`
   segment in raw or percent-encoded form.
6. The host is not the canonical public host or a subdomain of it, and does not
   end in `.localhost`, `.local`, `.internal`, `.home.arpa`, or `.onion`, and is
   not `localhost`.

Rules 3, 4, and 6 are stricter than the draft, which allows ports and queries.
They remove port scanning through aboutme, keep one cache key per client, and
give the consent screen a readable name (**Owner decision** C10).

Comparison is byte for byte everywhere, as the draft requires: the document's
`client_id`, the cache key, the stored column, and the `client_id` sent to the
token and revocation endpoints.

## Fetch

The server fetches the URL at the authorization endpoint and when consent is
submitted, only on a cache miss. The token, refresh, and revocation endpoints
never fetch ([Stored clients](#stored-clients)).

- One `GET` with `Accept: application/json` and a fixed `User-Agent`. It sends
  no cookie, no credentials, and nothing about the user or the request.
- TLS 1.2 or later, system roots, server name equal to the URL host.
- Redirects are not followed. Any status other than `200` fails.
- `Content-Type` must be `application/json` or `application/<name>+json`, with
  optional parameters.
- Compression is off. The body is read to at most 5,120 bytes, the draft's
  recommended cap; one byte more fails.
- Deadlines: 2 seconds to resolve and connect, 5 seconds for the whole fetch,
  body included.
- Concurrent requests for one URL share one fetch. At most four fetches run at
  once; a fifth fails at once rather than queue.

## SSRF guard

The fetcher has its own HTTP client. It never uses the default client, a
provider client, or a proxy from the environment (`Proxy` is nil).

1. **Resolve once, then pin.** The fetcher resolves the host and checks every
   returned A and AAAA address. If any address is refused, the fetch fails, so a
   mixed answer cannot slip a private address through. It then dials a checked
   address directly and sends the original host as the TLS server name and
   `Host`. No second lookup happens, so DNS rebinding cannot swap the address.
2. **Check at connect.** The dialer's `Control` hook checks the socket's remote
   address again before connecting, as a second line if any code path skips
   step 1.
3. **Refuse every non-global address.** Allowed addresses are global unicast
   only, per the IANA special-purpose registries (RFC 6890 and updates). An
   IPv4-mapped IPv6 address is unmapped and checked as IPv4. The refused set
   includes at least:

| Family | Refused ranges                                                                                                                                                                                                                                                                            |
| ------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| IPv4   | `0.0.0.0/8`, `10.0.0.0/8`, `100.64.0.0/10` (CGNAT), `127.0.0.0/8`, `169.254.0.0/16` (instance and task metadata), `172.16.0.0/12`, `192.0.0.0/24`, `192.0.2.0/24`, `192.88.99.0/24`, `192.168.0.0/16`, `198.18.0.0/15`, `198.51.100.0/24`, `203.0.113.0/24`, `224.0.0.0/4`, `240.0.0.0/4` |
| IPv6   | `::/128`, `::1/128`, `64:ff9b::/96` and `64:ff9b:1::/48` (NAT64), `100::/64`, `2001::/23` (including Teredo), `2001:db8::/32`, `2002::/16` (6to4), `fc00::/7` (including AWS `fd00:ec2::254`), `fe80::/10`, `fec0::/10`, `ff00::/8`                                                       |

The guard matters on the production host. Go runs in the `app` task with host
networking
([Single-host production](single-host-production.md#host-and-networking),
[ADR 0025](../adr/0025-single-host-production.md)), so it shares the host
loopback with Caddy, reaches instance metadata, and sits in the VPC beside the
database. The fetch leaves through the host's Elastic IP like other outbound
calls; there is no egress proxy. The guard lives in Go, not in host networking,
so it holds unchanged on the host
[ADR 0027](../adr/0027-vietnam-hosted-production.md) describes.

**Tests.** Go unit tests build the fetcher through a package-internal
constructor that takes an address policy and a root pool, and point it at an
`httptest` TLS server on loopback. A table test feeds every refused range above
through the production policy. The dev-https proof needs a real fetch, so the
server reads `OAUTH_CIMD_DEV_STUB_ADDR`, a loopback `host:port`. When set, the
fetcher dials that address for every client ID URL and trusts the dev CA.
Configuration refuses to start when it is set and `ENV` is `prod` or `staging`,
and a config test proves it. No setting opens the guard in production.

## Document rules

The body must be one JSON object with no duplicate member at any level, as DCR
already requires. Members:

| Member                       | Rule                                                                                                |
| ---------------------------- | --------------------------------------------------------------------------------------------------- |
| `client_id`                  | Required string, byte-for-byte equal to the URL                                                     |
| `client_name`                | Required; the DCR bounds: 1 to 64 code points after NFC, no control characters                      |
| `redirect_uris`              | Required; 1 to 5 entries, each passing the DCR redirect grammar                                     |
| `token_endpoint_auth_method` | Required and exactly `none` (**Owner decision** C4)                                                 |
| `grant_types`                | If present, a non-empty subset of `authorization_code` and `refresh_token`                          |
| `response_types`             | If present, exactly `["code"]`                                                                      |
| `scope`                      | If present, names only `resumes:read` and `resumes:write`; grants nothing                           |
| `application_type`           | If present, `native` needs loopback redirects only and `web` needs `https` redirects only           |
| `client_secret`, `jwks`      | `client_secret`, `client_secret_expires_at`, `jwks`, and `jwks_uri` make the document invalid       |
| Any other member             | Ignored, never stored, never fetched; this covers `client_uri`, `logo_uri`, `policy_uri`, `tos_uri` |

aboutme accepts public clients only. A document naming `private_key_jwt` or any
secret-based method is invalid, because the server advertises only `none` and
the draft forbids shared secrets. Such a client should not have chosen CIMD
here.

**Redirect matching.** An `https` redirect matches a document entry byte for
byte. An `https` redirect's host must also equal the client ID URL's host
(**Owner decision** C3), so the consent screen names one host for a web client.
A loopback redirect (`http` with `localhost`, `127.0.0.1`, or `[::1]`) matches
an entry with the same scheme, host, path, and query, with the port ignored on
both sides, as [RFC 8252 section 7.3][rfc8252] requires for loopback IP
literals. Claude Code needs the same match for `localhost` (**Owner decision**
C2). DCR clients keep exact matching, port included.

## Errors

| Where                                 | Failure                                                                            | Response                                                     |
| ------------------------------------- | ---------------------------------------------------------------------------------- | ------------------------------------------------------------ |
| Authorize, consent page, consent POST | URL rule, address refused, fetch, status, type, size, deadline, document, redirect | `400` closed page; never redirects to the presented URI      |
| Authorize, consent page, consent POST | A fetch limit or the concurrency cap                                               | `429` closed page with `Retry-After`; never redirects        |
| Authorize                             | Valid client and redirect, bad other parameter                                     | Existing error redirect (`invalid_request`, `invalid_scope`) |
| Token, refresh, revoke                | URL passes the rules but has no stored client, or fails the rules                  | `401` `invalid_client`, as rule 8 of the compatibility note  |
| Token, refresh                        | URL client differs from the code's or refresh token's client                       | `400` `invalid_grant`                                        |
| Token                                 | Any client authentication other than `none`                                        | Existing rejection, unchanged                                |

A failed fetch means the redirect URI is untrusted, so the server cannot send an
error back to the client, as RFC 6749 section 4.1.2.1 requires. The page text
stays closed: the user learns the app could not be checked, not why. Logs carry
the closed reason.

## Cache

The cache holds validated values, never the body: the URL, `client_name`, the
redirect list, and the expiry. It lives in Go memory, holds at most 1,000
entries, evicts the least recently used, and empties on restart.

- Lifetime comes from `Cache-Control: max-age`, clamped to a floor of 5 minutes
  and a ceiling of 1 hour. `no-store`, `no-cache`, a missing header, or another
  directive gets the floor (**Owner decision** C7).
- The floor bounds fetches per client to twelve an hour however the client sets
  its headers, and matches Claude Code's own `max-age=300`. The ceiling bounds
  how long a removed redirect URI or a replaced name stays in use.
- No negative caching: the draft forbids caching errors and invalid documents. A
  per-URL fetch limit takes its place ([Rate limits](#rate-limits)).
- An expired entry is refetched on the next authorization. If that fetch fails,
  the authorization fails; a stale entry is never served.

**When the document changes.** New values apply to the next authorization after
the entry expires. A removed redirect URI stops matching then. Codes already
issued stay valid for their 60 seconds, since each code binds its exact redirect
URI. A grant and its refresh tokens do not depend on the document: they keep
working when a redirect URI is removed, the name changes, or the document
disappears, until the user revokes or the 30-day family ends (**Owner decision**
C9). A new `client_name` reaches the stored client at the next approved consent.

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

The host and name render as text, never markup or links. The copy needs owner
approval (**Owner decision** C6). Connected agents in Settings shows the same
host beside the stored name.

**Silent reauthorization.** Today an authorize request whose user already holds
a live grant covering the scopes gets a code without the consent page. For a URL
client with a loopback redirect, the server always shows consent instead
(**Owner decision** C5). Every Claude Code user shares one `client_id` and its
loopback redirects, so without this rule any local process could take a code
silently by naming that `client_id` and its own port. A URL client with an
`https` redirect keeps silent reauthorization: its code can only reach the
document's own host.

## Stored clients

A URL client gets a row in `oauth_clients`, so codes, grants, and tokens keep
their foreign keys and every token path stays the same:

- A new nullable column `metadata_url` holds the URL, unique when present.
  `client_name` and `redirect_uris` hold the last validated document's values.
- The row is created or updated only by an approved consent, which is signed in
  and CSRF-checked. An anonymous authorize request reads the cache and writes
  nothing.
- The token, refresh, and revocation endpoints find the row by `metadata_url`
  and never fetch. Claude allows 10 seconds for token requests and 30 for
  refresh; a remote fetch there would spend that budget on a third party.
- One row per URL means one live grant per user and client. A reconnect replaces
  the user's grant for that client instead of adding one toward the ten-grant
  limit.
- The idle-client sweep removes a URL client's row like any other, 24 hours
  after its last grant and token end. The next approved consent creates it
  again.

## Rate limits

All use the bounded limiter of [ADR 0007](../adr/0007-bounded-rate-limiter.md)
and count fetches, not requests; a cache hit costs nothing. The numbers need
owner approval for [Budgets](budgets.md) (**Owner decision** C7).

| Limit                      | Value       | Key                                                |
| -------------------------- | ----------- | -------------------------------------------------- |
| Fetches per client address | 30 an hour  | Canonical address; an IPv6 address by its `/64`    |
| Fetches per client ID URL  | 6 a minute  | URL; stops a broken client from retrying in a loop |
| Fetches in total           | 600 an hour | Global; bounds aboutme as a request source         |
| Concurrent fetches         | 4           | Process                                            |

Authorization requests come from the user's browser, not from the client's
egress, so the shared egress-range bucket of
[rule 5](mcp-client-compatibility.md#5-registration-rate-from-shared-egress)
does not apply.

## Abuse cases

- **A look-alike name.** An attacker hosts a document with `client_name`
  `Claude Code`. The consent page leads with the attacker's host, and the guide
  teaches people to check it. A reserved-name list or a "verified" mark for
  known hosts is out of scope (**Owner decision** C8).
- **A real client's URL with a local listener.** An attacker names Claude Code's
  URL and a loopback port it controls. The attacker must already run code on the
  user's computer. The loopback warning shows, and silent reauthorization is off
  for loopback redirects, so the user sees consent every time.
- **A real client's URL with a foreign redirect.** The redirect must appear in
  the real document, which the attacker does not control. It fails.
- **aboutme as a scanner or reflector.** Fetches reach only port 443 on global
  addresses, the user never sees the response body or the reason for a failure,
  and the global limit bounds total traffic.
- **A slow or huge document.** The deadline, the byte cap, and the concurrency
  cap bound the cost of one fetch.

## Data and privacy

| Data                              | Where               | Kept                                                |
| --------------------------------- | ------------------- | --------------------------------------------------- |
| URL, `client_name`, redirect list | Memory cache        | Clamped cache lifetime, or until evicted or restart |
| URL, `client_name`, redirect list | `oauth_clients` row | Until 24 hours after its last grant and token end   |
| Fetched body                      | Nowhere             | Parsed and dropped                                  |

Logs, traces, and metrics carry the URL host, a SHA-256 digest of the URL, the
client row ID, and a closed outcome such as `address_refused`, `status`,
`content_type`, `too_large`, `deadline`, `document_invalid`,
`client_id_mismatch`, `redirect_mismatch`, or `rate_limited`. They never carry
the body, the full URL path, a `client_name`, or a redirect URI. As the draft
notes, a fetch tells the client's operator that someone is signing in at aboutme
around that time; the request carries nothing more. The privacy notice needs no
change: the only stored data describes the client, not the user.

## Older clients and loss

DCR clients, their rows, grants, and tokens do not change. The Go SDK runner
registers through DCR with no client ID URL configured; the existing Go SDK
proof confirms it keeps that path once the metadata advertises CIMD (assumption
until that proof runs).

Turning the flag off stops advertising CIMD and refuses new authorizations for
URL clients. Token, refresh, and revocation keep serving existing URL clients,
so connected users stay connected. Claude rereads metadata about every five
minutes ([lazy authentication][claude-lazy]) and then falls back to DCR for new
connections.

Rolling back to a release without CIMD code leaves the column in place, unread.
A refresh naming a URL `client_id` then fails `invalid_grant`, so each
URL-client user reconnects through DCR, as after other client-visible rollbacks.
Removing the column would first delete URL-client rows, which cascades to their
grants and tokens; that is a deliberate, separate change.

## Size

One additive migration: the column (at most 256 bytes) and a partial unique
index. The cache holds at most 1,000 small entries, well under 2 MiB, inside the
existing Go and Chromium memory cap. Each fetch reads at most 5,120 bytes.

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
   the owner proof below. The [MCP guide](mcp-guide.md) changes after that
   proof.

**Design text to update in the build:** [Data](data.md) for `oauth_clients`,
[Security](security.md#agent-authorization-and-the-bearer-world),
[API](api.md#agent-access-and-the-bearer-world), the OAuth server contract in
[MCP owner workflow](mcp-owner-workflow.md#oauth-server-contract),
[Budgets](budgets.md), and [Single-host production](single-host-production.md)
for the flag. New `AC-MCP-*` rows in the traceability matrix follow approval.

## Acceptance checks

- Metadata names `client_id_metadata_document_supported` only with the flag on.
- Each URL rule rejects its neighbor without a network call: `http`, a port,
  `:443`, a query, a fragment, userinfo, `/`, a dot segment, an IP literal,
  uppercase, the canonical host, and each refused name suffix.
- The guard refuses every listed range, including a hostname whose answer mixes
  a public and a private address, and a mapped IPv4 address.
- A redirect response, a non-200 status, a wrong type, a 5,121-byte body, and a
  slow body each fail closed with the `400` page.
- The document rules reject a `client_id` mismatch, a missing or wrong
  `token_endpoint_auth_method`, `private_key_jwt`, a `client_secret`, six
  redirects, and a duplicate member.
- Loopback redirects match with any port for URL clients only; an `https`
  redirect on another host fails.
- Cache lifetimes clamp to 5 minutes and 1 hour; a failed refetch is never
  served stale; errors are not cached.
- An anonymous authorize writes no row; an approved consent creates one; a
  reconnect reuses the grant.
- A loopback URL client always reaches consent despite a live grant.
- Token and refresh never fetch, and an unknown URL client gets
  `401 invalid_client`.
- Logs from every failure path hold no body, path, name, or redirect URI.
- A dev-https proof drives the TypeScript SDK client with a client ID URL served
  by the stub: discovery, consent, token, `tools/list`, a forced refresh, and
  revoke. Production config refuses `OAUTH_CIMD_DEV_STUB_ADDR`.
- Owner proof in production: Claude Code connects with its client ID URL,
  reconnects without a second grant, and fails after a revoke.

## Owner decisions

| ID  | Decision                                                                 | Recommendation                                           |
| --- | ------------------------------------------------------------------------ | -------------------------------------------------------- |
| C1  | Build CIMD now, behind `OAUTH_CIMD_ENABLED`, off until the owner proof   | Yes; turning it off keeps existing grants working        |
| C2  | Port-agnostic loopback match, `localhost` included, for URL clients only | Yes; Claude Code's document needs it, DCR stays exact    |
| C3  | An `https` redirect's host must equal the client ID URL's host           | Yes; consent then names one host                         |
| C4  | `token_endpoint_auth_method` must be present and `none`                  | Yes; RFC 7591 reads an absent value as a secret method   |
| C5  | No silent reauthorization for loopback redirects of URL clients          | Yes; one shared `client_id` makes it a silent-code path  |
| C6  | Consent copy: host first, name as a quote, loopback warning              | Approve the Vietnamese and English text above            |
| C7  | Cache floor 5 minutes, ceiling 1 hour, and the four fetch limits         | Approve for Budgets                                      |
| C8  | No reserved-name list and no "verified" mark                             | Leave out; revisit if impersonation appears              |
| C9  | Grants survive document changes and removal                              | Yes; the draft leaves it to the server                   |
| C10 | Refuse ports, queries, and IP literals in a client ID URL                | Yes; stricter than the draft, no known client needs them |

[claude-auth]: https://claude.com/docs/connectors/building/authentication
[claude-lazy]: https://claude.com/docs/connectors/building/lazy-authentication
[mcp-auth]:
  https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization
[cimd-02]:
  https://datatracker.ietf.org/doc/html/draft-ietf-oauth-client-id-metadata-document-02
[rfc8252]: https://www.rfc-editor.org/rfc/rfc8252#section-7.3
