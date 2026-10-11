# MCP CIMD verification

Status: proposal. Nothing here is built.
[MCP Client ID Metadata Documents](mcp-cimd.md) holds the motivation, sources,
release prerequisites, and owner decisions.

Abuse cases and acceptance checks cover [metadata fetching](mcp-cimd-fetch.md)
and [authorization](mcp-cimd-authorization.md).

## Abuse cases

- **A look-alike name.** An attacker hosts a document with `client_name`
  `Claude Code`. The [consent page](mcp-cimd-authorization.md#consent) leads
  with the attacker's host, and the guide teaches people to check it. A
  reserved-name list or a "verified" mark for known hosts is out of scope
  (**Owner decision** C8).
- **A real client's URL with a local listener.** An attacker names Claude Code's
  URL and a loopback port it controls. The attacker must already run code on the
  user's computer. The [loopback warning](mcp-cimd-authorization.md#consent)
  shows, and silent reauthorization is off for loopback redirects, so the user
  sees consent every time.
- **A real client's URL with a foreign redirect.** It fails: the redirect must
  appear in the real document
  ([Document rules](mcp-cimd-fetch.md#document-rules)).
- **aboutme as a scanner or reflector.**
  [Fetches](mcp-cimd-fetch.md#client-id-url-rules) reach only port 443 on
  [global addresses](mcp-cimd-fetch.md#ssrf-guard), the user never sees the
  response body or the [reason for a failure](mcp-cimd-authorization.md#errors),
  and the [global limit](mcp-cimd-fetch.md#rate-limits) bounds total traffic.

## Acceptance checks

- Metadata names `client_id_metadata_document_supported` only with the flag on.
- Each URL rule rejects its neighbor without a network call: `http`, a port,
  `:443`, a query, a fragment, userinfo, `/`, a dot segment, an IP literal,
  uppercase, the canonical host, and each refused name suffix.
- The guard refuses every listed range, `::7f00:1`, a mapped private IPv4
  address, and a hostname whose answer mixes a public and a private address.
- A redirect response, a non-200 status, a wrong type, a 5,121-byte body, and a
  slow body each fail closed with the `400` page.
- The document rules reject a `client_id` mismatch, a missing or wrong
  `token_endpoint_auth_method`, `private_key_jwt`, a `client_secret`, six
  redirects, and a duplicate member.
- Loopback redirects match with any port for URL clients only; an `https`
  redirect on another host fails.
- Cache lifetimes clamp to 5 minutes and 1 hour, `private, max-age=3600` gets 5
  minutes, a failed refetch is never served stale, and errors are not cached.
- After the document drops a redirect and the entry expires, silent
  reauthorization with that redirect fails although the row still lists it.
- A document whose `grant_types` adds `jwt-bearer` is accepted, and that grant
  type stays refused at the token endpoint.
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
