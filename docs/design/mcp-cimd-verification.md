# MCP CIMD verification

Status: approved design; implementation is next.
[MCP Client ID Metadata Documents](mcp-cimd.md) holds the motivation, sources,
release order, and the approval record.

Abuse cases and acceptance checks cover [metadata fetching](mcp-cimd-fetch.md)
and [authorization](mcp-cimd-authorization.md).

## Abuse cases

- **A look-alike name.** An attacker hosts a document with `client_name`
  `Claude Code`. The [consent page](mcp-cimd-authorization.md#consent) leads
  with the attacker's host, and the guide teaches people to check it. A
  reserved-name list or a "verified" mark for known hosts is out of scope.
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

## Attacker coverage and limits

| Attacker                                   | Path, control, and remaining limit                                                                                                                                                                                                                                                                                                      |
| ------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Anonymous visitor                          | A supplied URL reaches grammar checks, bounded admission, DNS policy, pinned TLS, capped decode, then the cache. No client row is written until authenticated approval. Distinct URLs can still consume the finite fetch budget and deny new CIMD authorization temporarily.                                                            |
| Signed-in attacker                         | Consent uses the current session's user, CSRF, exact Origin, recent authentication, and epoch. Client upsert, user cap, grant, and code share a transaction. Sharing a URL row never shares a grant or token across users. A shared display name can change on another user's approval; the URL host remains the identity.              |
| Hostile document or resume content         | Metadata names render only as isolated text; ignored metadata cannot fetch a logo, script, or secondary URL. Resume content remains behind the existing sanitizer and MCP handler chain, with no new renderer input.                                                                                                                    |
| Local process claiming a legitimate client | It can supply its own PKCE challenge and loopback port, so PKCE alone cannot identify the app. Mandatory interactive consent and the warning prevent silent issue. Approval can still authorize that malicious process; CIMD supplies no local-process attestation.                                                                     |
| Stolen session or MCP token                | CIMD does not elevate a bearer token to browser consent. Existing recent-authentication and factor policy guard grant issue; token kind, scope, epoch, expiry, rotation, and revocation govern `/mcp`. A stolen current token retains its granted powers until those gates stop it.                                                     |
| Network position between browser and edge  | Existing HTTPS, secure cookies, state, issuer, and PKCE checks apply. The independent fetch uses system-root TLS and original-host verification after address pinning. A public-client document is not a credential.                                                                                                                    |
| Compromised container on the host          | The SSRF guard blocks request-controlled private destinations, including host loopback, but cannot constrain a process that can replace its own code or use its own network socket. Host networking is not an isolation guarantee.                                                                                                      |
| Database writer without application keys   | A stored redirect-list edit does not override the fetched document while the URL identity remains intact. A writer can change that identity, clear `metadata_url` to reclassify the row as DCR, insert a chosen token digest, or alter grant ownership. CIMD adds no signing key or defense against that database-authority compromise. |
| Leaked backup                              | No raw codes, tokens, PKCE verifiers, or fetched bodies are added. Client URLs, names, redirects, and account-to-grant relationships remain visible in database copies. Restore and retention protections remain those of the existing backup system.                                                                                   |

CIMD introduces no client secret, signing key, or key-recovery flow. The client
keeps its PKCE verifier and tokens; aboutme keeps digests and existing session
keys under the established custody rules. Revocation stays owner-scoped in
Settings or token-based through `/oauth/revoke`, and must reach `/mcp` on the
next request. A lost client URL is a new client identity, with no automatic
transfer of its grants. A removed document cannot recover a lost token; a new
valid authorization is required.

A taken-over client domain can serve a new document and callback on the same
host. Under approved HTTPS silent reauthorization and grant survival, it can
receive new codes for an existing grant. Host display, TLS, and PKCE do not
prove continued ownership by the original publisher. The owner accepts this
limit through those policies; users can revoke the grant. No verified-client
claim or remote document hash should imply otherwise.

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
- Cache lifetimes follow [Budgets](budgets.md#cimd), including malformed,
  duplicate, overflowing, zero, `private`, `no-cache`, and `no-store` cases.
  Maximum-size entries obey both count and memory caps. Eviction/restart can
  refetch; failed refetch never serves stale data, and errors are not cached.
- After the document drops a redirect and the entry expires, silent
  reauthorization with that redirect fails although the row still lists it.
- A document whose `grant_types` adds `jwt-bearer` is accepted, and that grant
  type stays refused at the token endpoint.
- Anonymous authorize, consent GET, and denial write no client row. Approval
  creates one; same-user reapproval preserves the live grant ID and timestamp.
  Concurrent approvals by the same user create one live grant; approvals by
  different users share only the client row. Grant cap, stale session, CSRF, or
  transaction failure rolls back the row and metadata update too.
- A loopback URL client always reaches consent despite a live grant.
- Code exchange and refresh never fetch. Exercise the error matrix, including
  unknown URL rows, found tokens with mismatched clients, omitted refresh IDs,
  bad code/resource/PKCE, and an internal UUID used as a URL-client alias. The
  failed-grant limiter still applies to known URL clients. Revocation accepts
  only its existing token form and returns `200` for an unknown token.
- Logs from every failure path hold no body, path, name, or redirect URI.
- A dev-https proof drives the TypeScript SDK client with a client ID URL served
  by the stub: discovery, consent, token, `tools/list`, a forced refresh, and
  revoke. Production config refuses `OAUTH_CIMD_DEV_STUB_ADDR`.
- Flag off omits metadata and refuses new URL authorize/consent with zero DNS
  calls, while code exchange, refresh, revoke, and owner grant listing still
  work. The parent MCP flag still disables the entire agent surface.
- Preserve existing DCR/Go SDK scenarios, updating only their shared return-line
  assertions. Apply the migration with existing rows, approve CIMD, restart, and
  prove UUID foreign keys remain intact. Refuse a pre-CIMD binary while URL rows
  exist and MCP is enabled; prove the MCP-off rollback cannot serve any agent
  route. Refuse destructive down migration while URL rows exist.
- Both consent languages show the approved host-first copy, quoted untrusted
  name, return line, and loopback warning at 390 px and 1440 px. Long hosts and
  hostile bidi/HTML names cannot hide or replace the trusted host. Check
  loading, error focus, keyboard approval/denial, and Connected agents.
- Both CIMD and DCR loopback return lines show the actual validated hostname in
  parentheses before the final period, as text, in both languages.
- Owner proof in production follows the sequence below.

## Production enablement proof

Deploy the first 0.8 CIMD release with the flag off after independent review and
exact-commit green CI. Verify that metadata omits CIMD and existing DCR access
works. Apply the additive migration before serving the new binary and deploy
backend and frontend together. No local test, fixture, or stub address enters
production configuration.

The owner then enables `OAUTH_CIMD_ENABLED=true` through the deployment
configuration and restarts the service. Verify the metadata field and allow for
client discovery caching before testing. Use the designated owner test account
to connect Claude Code by CIMD, approve, list/read, reconnect, and prove one
live grant for that URL. Reconnect must show consent for loopback. Exercise
refresh, revoke from Settings, then require the next MCP request to fail and
refresh to reject the revoked authority. Record outcomes only, never tokens,
request URLs, or resume content.

Hosted Claude's document URL remains unverified until its own connection is
observed through redacted server outcomes. A Claude Code proof alone cannot
claim hosted Claude support. If the proof fails, turn the flag off to stop new
CIMD authorization; revoke the test grant separately. Existing URL grants and
tokens survive flag disablement. Publish guide changes only for demonstrated
clients after a successful proof.
