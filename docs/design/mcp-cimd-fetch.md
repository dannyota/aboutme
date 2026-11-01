# MCP CIMD metadata fetching

Status: implemented and enabled in production. See
[ADR 0018](../adr/0018-mcp-agent-access.md) for the owner proof and outstanding
verification. [MCP Client ID Metadata Documents](mcp-cimd.md) holds the
motivation, sources, release order, and the approval record.

URL validation, fetching, document validation, cache, and rate limits belong to
this boundary. See [authorization](mcp-cimd-authorization.md) and
[verification](mcp-cimd-verification.md).

## Client ID URL rules

The `client_id` is checked as a string before any fetch. All of these must hold,
or the request fails with no network access:

1. Length within [Budgets](budgets.md#cimd); bytes `0x21` to `0x7e`, excluding
   `#`, raw backslash, and invalid percent escapes.
2. Scheme exactly `https`, lowercase.
3. Host is a lowercase DNS name in ASCII (an IDN arrives as `xn--` labels), with
   at least one dot and no trailing dot. Each label is 1 to 63 ASCII letters,
   digits, or hyphens, with no leading or trailing hyphen. Refuse empty labels,
   percent escapes in the host, IP literals, and numeric-address spellings. No
   IDNA conversion or case folding creates an alias for an accepted URL.
4. No userinfo, no port (not even `:443`), no query, and no fragment.
5. A path of at least one character after the leading `/`, with no `.` or `..`
   segment in raw or percent-encoded form. Split raw path segments and
   percent-decode each once for this check; also split on decoded `/` so an
   encoded separator cannot hide a dot segment. Never clean or reserialize the
   accepted URL for identity comparison or the request target.
6. The host is not the canonical public host or a subdomain of it, and does not
   end in `.localhost`, `.local`, `.internal`, `.home.arpa`, or `.onion`, and is
   not `localhost`.

Departures from the draft:

- Rules 3, 4, and 6 are stricter: the draft allows a port and says a query
  "SHOULD NOT" appear. They remove port scanning through aboutme, keep one cache
  key per client, and give the consent screen a readable name.
- The cache floor applies even to `no-store` and `no-cache` ([Cache](#cache)).
  The draft recommends honoring HTTP caching and permits lifetime bounds.
  Overriding a storage prohibition is an explicit local-policy departure, not a
  claim of RFC 9111 shared-cache compliance.

Comparison is byte for byte everywhere, as the draft requires: the document's
`client_id`, the cache key, the stored column, and the `client_id` sent to the
token endpoint. Revocation identifies authority only by token.

## Fetch

The server resolves metadata at authorize, consent GET, and consent POST
(including denial), fetching only on a cache miss or expiry. The token, refresh,
and revocation endpoints never fetch
([Stored clients](mcp-cimd-authorization.md#stored-clients)).

- One `GET` with `Accept: application/json` and a fixed `User-Agent`. It sends
  no cookie, no credentials, no Referer, and nothing about the user or the
  authorization request. Use `User-Agent: aboutme-cimd/1`.
- TLS 1.2 or later, system roots, server name equal to the URL host.
- Redirects are not followed. Any status other than `200` fails.
- `Content-Type` must be `application/json` or `application/<name>+json`, with
  optional parameters.
- Compression is off; refuse non-identity `Content-Encoding`. Check both a
  declared Content-Length and bounded stream reads. Body and deadline limits
  come from [Budgets](budgets.md#cimd); the whole deadline starts before DNS.
- Concurrent requests for one URL share one admitted fetch. Each waiter keeps
  its request cancellation; the shared operation has its own bounded deadline.
  No unbounded task or queue starts before admission. The concurrency cap in
  Budgets rejects excess distinct misses immediately. Close the body and release
  admission on every exit.

## SSRF guard

The fetcher has its own HTTP client. It never uses the default client, a
provider client, or a proxy from the environment (`Proxy` is nil).

1. **Resolve once, then pin.** The fetcher resolves the host and checks every
   returned A and AAAA address. If any address is refused, the fetch fails, so a
   mixed answer cannot slip a private address through. It then dials a checked
   address directly and sends the original host as the TLS server name and
   `Host`. No second lookup happens, so DNS rebinding cannot swap the address.
2. **Check at connect.** Check the literal address passed to the dialer, its
   approved-address membership, and destination port before dialing. Go's
   [`ControlContext`](https://pkg.go.dev/net#Dialer) runs before connect, so it
   checks its address argument, not a connected socket's peer. Never hand a
   hostname to a fallback dialer. Disable connection reuse and transport retries
   for this fetcher so each cache miss follows this same checked path; never use
   HTTP/2 connection coalescing, an alternate service, or a proxy.
3. **Allow listed address space only.** An IPv4 address is allowed unless a
   refused IPv4 range below holds it. An IPv6 address must sit in `2000::/3` and
   outside every refused IPv6 range below; everything else is refused, including
   the IPv4-compatible `::/96` (for example `::7f00:1`). An IPv4-mapped address
   (`::ffff:0:0/96`) is unmapped and its embedded IPv4 address checked instead,
   both at DNS validation and at connect. This is the sole special-purpose entry
   with conditional acceptance: it represents an IPv4 destination, so only an
   IPv4 address that passes the full IPv4 policy may be dialed, as IPv4. Go's
   `netip.Addr.IsGlobalUnicast` alone is not the check: it accepts `::/96`
   addresses such as `::7f00:1`.

The refusal tables cover every entry in the IANA special-purpose registries
([IPv4][iana4], [IPv6][iana6]), retrieved 2026-10-14; both registries report
2025-10-09 as their last update. Except for the mapped-address handling above,
refuse every entry, including globally reachable and terminated allocations, as
required by [the CIMD draft's SSRF rule][cimd-ssrf]. A registry flag never
creates an allow-exception. The tables also retain the local multicast and
deprecated site-local refusals. There are no narrower allow-exceptions inside
any refused range:

| Family | Refused ranges                                                                                                                                                                                                                                                                                                                                     |
| ------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| IPv4   | `0.0.0.0/8`, `10.0.0.0/8`, `100.64.0.0/10` (CGNAT), `127.0.0.0/8`, `169.254.0.0/16` (instance and task metadata), `172.16.0.0/12`, `192.0.0.0/24`, `192.0.2.0/24`, `192.31.196.0/24`, `192.52.193.0/24`, `192.88.99.0/24`, `192.168.0.0/16`, `192.175.48.0/24`, `198.18.0.0/15`, `198.51.100.0/24`, `203.0.113.0/24`, `224.0.0.0/4`, `240.0.0.0/4` |
| IPv6   | `::/128`, `::1/128`, `64:ff9b::/96` and `64:ff9b:1::/48` (NAT64), `100::/64`, `100:0:0:1::/64`, `2001::/23` (including Teredo), `2001:db8::/32`, `2002::/16` (6to4), `2620:4f:8000::/48`, `3fff::/20` (documentation), `5f00::/16`, `fc00::/7` (including AWS `fd00:ec2::254`), `fe80::/10`, `fec0::/10`, `ff00::/8`                               |

These narrower registry entries are refused through their containing ranges
above, including entries that IANA marks globally reachable:

| Refused containing range | Contained registry entries                                                                                                                                      |
| ------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `0.0.0.0/8`              | `0.0.0.0/32`                                                                                                                                                    |
| `192.0.0.0/24`           | `192.0.0.0/29`, `192.0.0.8/32`, `192.0.0.9/32`, `192.0.0.10/32`, `192.0.0.170/32`, `192.0.0.171/32`                                                             |
| `192.88.99.0/24`         | `192.88.99.2/32`                                                                                                                                                |
| `240.0.0.0/4`            | `255.255.255.255/32`                                                                                                                                            |
| `2001::/23`              | `2001::/32`, `2001:1::1/128`, `2001:1::2/128`, `2001:1::3/128`, `2001:2::/48`, `2001:3::/32`, `2001:4:112::/48`, `2001:10::/28`, `2001:20::/28`, `2001:30::/28` |

The guard matters on the production host. Go runs in the `aboutme-server`
container with host networking, so it shares the host loopback with Caddy.
PostgreSQL runs on the same Vietnam host
([Vietnam production](vietnam-production.md#host),
[ADR 0027](../adr/0027-vietnam-hosted-production.md)). The fetch leaves through
the host's floating IP like other outbound calls; there is no egress proxy. The
guard lives in Go, not in host networking.

**Tests.** Go unit tests build the fetcher through a package-internal
constructor that takes an address policy and a root pool, and point it at an
`httptest` TLS server on loopback. A table test feeds every refused range above
through the production policy. The dev-https proof needs a real fetch, so the
server reads `OAUTH_CIMD_DEV_STUB_ADDR`, a loopback `host:port`. When set, the
fetcher dials that address for every client ID URL and trusts the dev CA.
Configuration accepts the stub only in explicit `dev`; it rejects all other
environments, including `prod` and `staging`, even with CIMD disabled. Require a
literal loopback address, never a hostname. Preserve the original Host, SNI, and
certificate-name verification; the proof must supply a dev CA through the
existing development trust setup, never `InsecureSkipVerify`. Production
construction always uses the production address policy. Config tests prove that
no setting opens the guard in production.

## Document rules

The body must be one JSON object with no duplicate member at any level. The
existing DCR decoder checks only top-level duplicates, so a dedicated CIMD
validator must also inspect nested ignored values. Reject invalid UTF-8, a
trailing JSON value, `null` for any known member, or a known member of the wrong
type. Required text members are strings; URI and type lists are arrays of
strings. The body cap also bounds unknown nested values. Decode duplicate keys
before discarding unknown values. Members:

| Member                       | Rule                                                                                                         |
| ---------------------------- | ------------------------------------------------------------------------------------------------------------ |
| `client_id`                  | Required string, byte-for-byte equal to the URL                                                              |
| `client_name`                | Required; the DCR bounds: 1 to 64 code points after NFC, no control characters                               |
| `redirect_uris`              | Required; 1 to 5 entries, each passing the DCR redirect grammar                                              |
| `token_endpoint_auth_method` | Required and exactly `none`                                                                                  |
| `grant_types`                | If present, a nonempty string array including `authorization_code`; unknown values are ignored               |
| `response_types`             | If present, exactly `["code"]`                                                                               |
| `scope`                      | If present, names only `resumes:read` and `resumes:write`; grants nothing                                    |
| `application_type`           | If present, only `native` (loopback redirects only) or `web` (`https` redirects only); any other value fails |
| `client_secret`, `jwks`      | `client_secret`, `client_secret_expires_at`, `jwks`, and `jwks_uri` make the document invalid                |
| Any other member             | Ignored, never stored, never fetched; this covers `client_uri`, `logo_uri`, `policy_uri`, `tos_uri`          |

aboutme accepts public clients only. A document naming `private_key_jwt` or any
secret-based method is invalid, because the server advertises only `none` and
the draft forbids shared secrets. Such a client should not have chosen CIMD
here. Code exchange can issue refresh tokens whether or not `refresh_token`
appears in metadata. Only `authorization_code` and `refresh_token` reach the
existing token dispatcher; a metadata value never enables another grant. A
present `scope` uses the existing `ParseScopes` grammar and remains descriptive;
the authorization request and owner consent determine granted scopes.

**Redirect matching.** An `https` redirect matches a document entry byte for
byte. An `https` redirect's host must also equal the client ID URL's host, so
the consent screen names one host for a web client. A loopback redirect (`http`
with `localhost`, `127.0.0.1`, or `[::1]`) matches an entry with the same
scheme, host, path, and query, with only the port removed on both sides before
byte-for-byte comparison of the remaining URI, as [RFC 8252 section
7.3][rfc8252] requires for loopback IP literals. Claude Code needs the same
match for `localhost`. DCR clients keep exact matching, port included. For CIMD
loopback URIs, require an absent port or decimal integer from 1 to 65535; never
equate `localhost`, `127.0.0.1`, and `[::1]`. Query presence, spelling,
ordering, percent encoding, path, scheme, and host remain exact. HTTPS host
comparison uses the parsed DNS hostname, case-insensitively, without a
trailing-dot alias; the complete HTTPS redirect still has to match its document
entry exactly. Validate every document redirect before caching, not only the
selected entry. Do not resolve or fetch any redirect URI. Code exchange always
matches the exact issued redirect, including the chosen loopback port.

## Cache

The process-local cache holds immutable validated values, never the body: the
exact URL, normalized `client_name`, redirect list, and expiry. Document-only
validation fields are discarded after validation. It is shared across users,
evicts least recently used entries within both [Budgets](budgets.md#cimd)
bounds, and empties on restart.

- Select `s-maxage` if present, else `max-age`, clamped to the floor and ceiling
  in Budgets. If the selected directive is malformed, duplicated, negative, or
  overflows, use the floor. Use the floor when neither directive exists or any
  `private`, `no-store`, or `no-cache` directive is present. Apply that local
  policy even when it departs from RFC 9111 storage rules. `Expires` is ignored.
  Start the selected local lifetime at receipt; no conditional request, stale
  reuse, or background revalidation occurs.
- The floor reduces repeated fetches while an entry remains resident. Eviction
  and restart can cause earlier fetches; only the fetch limiter is a hard
  request-rate bound. The ceiling bounds reuse of removed redirect URIs or
  replaced names after the last successful fetch.
- Never cache errors or invalid documents. Shared waiters receive the same
  in-flight failure, then that in-flight record is removed. A limiter refusal is
  rate state, not a cached document or response.
- On expiry the next metadata resolution must fetch. A failure never serves
  stale metadata or falls back to the stored client row.

**Which redirect list counts.** For a URL client, the authorization endpoint,
the consent page, consent submission, and silent reauthorization each check the
redirect URI against the current document: the cached entry, or a fresh fetch
when the entry is missing or expired. The stored row's `redirect_uris` is a
record of the last approved document and is never checked for a URL client. A
code issued from an existing grant passes the same document check, so a grant
cannot carry a redirect the document no longer lists. DCR clients keep checking
their stored row.

**When the document changes.** New values apply to the next authorization after
the entry expires. A removed redirect URI stops matching then, including for
silent reauthorization. Codes already issued stay valid for their 60 seconds,
since each code binds its exact redirect URI. Document changes do not revoke
grants or tokens. A grant has no time-based expiry; existing refresh families
keep their absolute lifetime from
[Security](security.md#agent-authorization-and-the-bearer-world). User
revocation and authentication-epoch changes still end authority. Removal of a
redirect or document cannot extend a code or token lifetime. A new `client_name`
reaches the stored client at the next approved consent.

## Rate limits

Use the bounded limiter of [ADR 0007](../adr/0007-bounded-rate-limiter.md), with
the address, URL, global, and concurrency limits in [Budgets](budgets.md#cimd).
Cache hits and coalesced waiters cost no fetch admission. A leader checks all
limits before DNS; a launched attempt counts even if DNS, TLS, HTTP, or
validation fails. Check the canonical address from the trusted-proxy boundary,
never a client-supplied forwarding header. URL keys are SHA-256 of exact
accepted URL bytes; the cache itself compares complete URLs. The limiter
preserves its bounded overflow policy under random-URL churn.

An exceeded rate bucket returns its positive, rounded-up seconds until retry;
when more than one blocks, use the maximum. Concurrency refusal returns the
complete-fetch deadline from Budgets as `Retry-After` in seconds. Admission must
not hold a database transaction open.

Authorization requests come from the user's browser, so the egress-range bucket
of [rule 5](mcp-client-compatibility.md#5-registration-rate-from-shared-egress)
does not apply.

[rfc8252]: https://www.rfc-editor.org/rfc/rfc8252#section-7.3
[iana4]: https://www.iana.org/assignments/iana-ipv4-special-registry/
[iana6]: https://www.iana.org/assignments/iana-ipv6-special-registry/
[cimd-ssrf]:
  https://www.ietf.org/archive/id/draft-ietf-oauth-client-id-metadata-document-02.html#section-8.6
