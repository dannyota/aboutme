# MCP CIMD metadata fetching

Status: proposal. Nothing here is built.
[MCP Client ID Metadata Documents](mcp-cimd.md) holds the motivation, sources,
release prerequisites, and owner decisions.

URL validation, fetching, document validation, cache, and rate limits belong to
this boundary. See [authorization](mcp-cimd-authorization.md) and
[verification](mcp-cimd-verification.md).

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

Departures from the draft:

- Rules 3, 4, and 6 are stricter: the draft allows a port and says a query
  "SHOULD NOT" appear. They remove port scanning through aboutme, keep one cache
  key per client, and give the consent screen a readable name (**Owner
  decision** C10).
- The cache floor applies even to `no-store` and `no-cache` ([Cache](#cache)).
  The draft says the server "SHOULD respect HTTP cache headers" but "MAY define
  its own upper and/or lower bounds"; applying a floor to `no-store` stretches
  that allowance (**Owner decision** C7).

Comparison is byte for byte everywhere, as the draft requires: the document's
`client_id`, the cache key, the stored column, and the `client_id` sent to the
token and revocation endpoints.

## Fetch

The server fetches the URL at the authorization endpoint and when consent is
submitted, only on a cache miss. The token, refresh, and revocation endpoints
never fetch ([Stored clients](mcp-cimd-authorization.md#stored-clients)).

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
3. **Allow listed address space only.** An IPv4 address is allowed unless a
   refused IPv4 range below holds it. An IPv6 address must sit in `2000::/3` and
   outside every refused IPv6 range below; everything else is refused, including
   the IPv4-compatible `::/96` (for example `::7f00:1`). An IPv4-mapped address
   (`::ffff:0:0/96`) is unmapped and its embedded IPv4 address checked instead.
   Go's `netip.Addr.IsGlobalUnicast` alone is not the check: it accepts `::/96`
   addresses such as `::7f00:1`. The ranges follow the IANA special-purpose
   registries (RFC 6890 and updates). The refused set includes at least:

| Family | Refused ranges                                                                                                                                                                                                                                                                            |
| ------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| IPv4   | `0.0.0.0/8`, `10.0.0.0/8`, `100.64.0.0/10` (CGNAT), `127.0.0.0/8`, `169.254.0.0/16` (instance and task metadata), `172.16.0.0/12`, `192.0.0.0/24`, `192.0.2.0/24`, `192.88.99.0/24`, `192.168.0.0/16`, `198.18.0.0/15`, `198.51.100.0/24`, `203.0.113.0/24`, `224.0.0.0/4`, `240.0.0.0/4` |
| IPv6   | `::/128`, `::1/128`, `64:ff9b::/96` and `64:ff9b:1::/48` (NAT64), `100::/64`, `2001::/23` (including Teredo), `2001:db8::/32`, `2002::/16` (6to4), `fc00::/7` (including AWS `fd00:ec2::254`), `fe80::/10`, `fec0::/10`, `ff00::/8`                                                       |

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
Configuration refuses to start when it is set and `ENV` is `prod` or `staging`,
and a config test proves it. No setting opens the guard in production.

## Document rules

The body must be one JSON object with no duplicate member at any level, as DCR
already requires. Members:

| Member                       | Rule                                                                                                           |
| ---------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `client_id`                  | Required string, byte-for-byte equal to the URL                                                                |
| `client_name`                | Required; the DCR bounds: 1 to 64 code points after NFC, no control characters                                 |
| `redirect_uris`              | Required; 1 to 5 entries, each passing the DCR redirect grammar                                                |
| `token_endpoint_auth_method` | Required and exactly `none` (**Owner decision** C4)                                                            |
| `grant_types`                | If present, includes `authorization_code`; other values are ignored and never granted (**Owner decision** C11) |
| `response_types`             | If present, exactly `["code"]`                                                                                 |
| `scope`                      | If present, names only `resumes:read` and `resumes:write`; grants nothing                                      |
| `application_type`           | If present, `native` needs loopback redirects only and `web` needs `https` redirects only                      |
| `client_secret`, `jwks`      | `client_secret`, `client_secret_expires_at`, `jwks`, and `jwks_uri` make the document invalid                  |
| Any other member             | Ignored, never stored, never fetched; this covers `client_uri`, `logo_uri`, `policy_uri`, `tos_uri`            |

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

## Cache

The cache holds validated values, never the body: the URL, `client_name`, the
redirect list, and the expiry. It lives in Go memory, holds at most 1,000
entries, evicts the least recently used, and empties on restart. The cache stays
under 2 MiB, inside the Go memory cap.

- The cache is shared by every user, so it reads `Cache-Control` as an RFC 9111
  shared cache does. The lifetime is `s-maxage` if present, else `max-age`,
  clamped to a floor of 5 minutes and a ceiling of 1 hour. The floor alone
  applies when the header is missing or malformed, has neither directive, or
  holds `private`, `no-store`, or `no-cache`. So `private, max-age=3600` gets 5
  minutes, `public, max-age=86400` gets 1 hour, and `Expires` is ignored
  (**Owner decision** C7).
- The floor bounds fetches per client to twelve an hour however the client sets
  its headers, and matches Claude Code's own `max-age=300`. The ceiling bounds
  how long a removed redirect URI or a replaced name stays in use.
- No negative caching: the draft says the server "MUST NOT cache error
  responses" and "MUST NOT cache documents which are invalid or malformed". A
  per-URL fetch limit takes its place ([Rate limits](#rate-limits)).
- An expired entry is refetched on the next authorization. If that fetch fails,
  the authorization fails; a stale entry is never served.

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
since each code binds its exact redirect URI. A grant and its refresh tokens do
not depend on the document: they keep working when a redirect URI is removed,
the name changes, or the document disappears, until the user revokes or the
30-day family ends (**Owner decision** C9). A new `client_name` reaches the
stored client at the next approved consent.

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

Authorization requests come from the user's browser, so the egress-range bucket
of [rule 5](mcp-client-compatibility.md#5-registration-rate-from-shared-egress)
does not apply.

[rfc8252]: https://www.rfc-editor.org/rfc/rfc8252#section-7.3
