# MCP CIMD verification

Status: implemented and enabled in production. The owner proof passed with
Claude Code on 2026-10-14; hosted Claude's CIMD connection remains unverified.
CI, independent review, and hosted HTTPS proof execution remain outstanding.
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
- **A shared client as a denial target.** Anonymous guesses naming a real URL or
  DCR UUID cannot spend an owner's failed-grant allowance. A stored code or
  refresh token selects only its own grant under
  [failed-grant admission](mcp-cimd-authorization.md#failed-grant-admission).
- **Credential existence through throttling.** Repeating a candidate with a
  wrong verifier or client must not distinguish a stored credential from a
  never-issued one through status, body, or headers. The
  [refusal response](mcp-cimd-authorization.md#refusal-response-and-client-recovery)
  exposes grant backoff only after read-only validation of live authority.
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

### Revocation with a claimed client

The proposed [revocation contract](mcp-cimd-authorization.md#revocation) owns
request validation, authority, responses, timing limits, and failure recovery.
It adds no key or token custody path. Apply this attacker trace alongside the
existing controls above; response equality does not hide database timing.

| Attacker                                   | Path, control, and remaining limit                                                                                                                                                                                                                                                                            |
| ------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Anonymous visitor                          | A claimed URL or UUID passes grammar only and never resolves a client. Missing authority cannot select a victim's grant. Random token guesses reach bounded digest lookup; hits take a different database path, so timing can reveal existence. Outer admission limits traffic, not all distributed guessing. |
| Signed-in attacker                         | Cookies supply no authority. Naming a victim's client or sharing that client does not select the victim's grant; the presented token's stored foreign keys select the only mutation target.                                                                                                                   |
| Hostile resume or metadata content         | No content is executed, rendered, or fetched here. A script can submit a form, but a cookie or claimed client cannot replace possession of the victim's token. Existing token storage and injection defenses remain necessary.                                                                                |
| Stolen session or MCP token                | A stolen session alone grants no authority here. A stolen token can destroy its own grant, even if expired or rotated while its row remains; it cannot select another grant. Settings remains the owner's independent recovery path, followed by fresh authorization.                                         |
| Local process claiming a legitimate client | A public client ID alone does nothing. Access to the token store permits revocation and disconnection; the claim does not attest the process.                                                                                                                                                                 |
| Network position between client and edge   | HTTPS and endpoint certificate validation protect the submitted token. Captured tokens remain bearer authority; replay revokes the same grant and cannot restore it.                                                                                                                                          |
| Compromised container on the host          | A container with application database authority can bypass revocation rules. Compromise of another container does not itself grant that authority; existing isolation and credentials set the boundary.                                                                                                       |
| Database writer without application keys   | Client metadata tampering can change the mismatch event; token or grant ownership tampering can redirect mutation. Database authority remains trusted, with no new signing protection.                                                                                                                        |
| Leaked backup                              | Stored digests do not supply the raw high-entropy token needed for revocation. Existing account-to-grant relationships remain exposed; no claimed value enters storage or logs. A restored database can restore authority under existing restore procedures.                                                  |

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
- Address-policy fixtures cover every entry from both registries retrieved for
  the [SSRF guard](mcp-cimd-fetch.md#ssrf-guard), including contained entries,
  globally reachable entries, terminated entries, and the local refusals. For
  each refused prefix, check its first and last addresses and an interior
  address where one exists. Check adjacent addresses against the whole policy,
  since a neighbor can belong to another refused range.
- Explicit refusal cases include `192.31.196.1`, `192.52.193.1`, `192.175.48.1`,
  `2620:4f:8000::1`, `100:0:0:1::1`, and `5f00::1`. The guard also refuses
  `::7f00:1` and other non-mapped IPv6 addresses outside `2000::/3`. Globally
  reachable flags never override the refusal tables.
- Test each refused IPv4 case in IPv4-mapped form too. Ordinary public fixtures
  `8.8.8.8` and `2606:4700::1111` pass the address policy; `::ffff:8.8.8.8`
  passes only through unmapping and dialing the checked IPv4 literal. Assert the
  same policy at DNS validation and at connect, with no connection for refusal
  cases.
- A hostname whose answer mixes an allowed address with any refused address
  fails before any connection, including public/private and public/special-use
  pairs in either answer order and across A and AAAA answers.
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
  failed-grant limiter follows the admission rule linked above. Revocation
  follows the [revocation checks](#revocation-checks).
- Two owners approve the same URL client and receive distinct grants. With a
  fixed clock, send a full failure allowance of anonymous code guesses naming
  that URL, then require the other owner's valid exchange and refresh to
  succeed. Repeat with well-shaped never-issued codes and refresh tokens, not
  only malformed strings. Use distinct source IPs to isolate grant admission
  from the token-IP budget; also cover one shared IP below that budget.
- Exhaust one owner's grant with counted failures using that owner's real code
  or refresh token. Its next otherwise valid attempt gets `429` and
  `Retry-After`; an invalid attempt keeps its ordinary error. The other owner's
  valid exchange and refresh still succeed. Run these two-owner cases with CIMD
  on and off, and repeat for a shared DCR client. A wrong claimed client must
  never charge that client's other grants; omitted refresh `client_id` must
  select the same grant as its correct explicit value.
- With a fixed clock and a fresh token-IP budget, interleave eleven requests for
  one stored code with eleven for one well-shaped never-issued code from the
  same address. Use the same existing client, redirect, and syntactically valid
  wrong verifier; only the candidate code differs. All twenty-two responses must
  match the closed `invalid_grant` status, exact body bytes, and full
  application headers, including absent `Retry-After`. Assert token-IP admission
  succeeds on every request so it cannot mask the grant refusal.
- Repeat that paired sequence for a stored refresh token and a well-shaped
  never-issued refresh token, naming the same existing but mismatched client.
  Reset the address budget between sequences. Run both sequences for CIMD with
  the flag on and off and for DCR, with a tracked grant and with an untracked
  grant that exhausts shared overflow. Pin or normalize only
  credential-independent transport dates and request IDs when comparing headers.
  Verify only stored candidates spend grant debt, unknown candidates allocate
  nothing, and the exhausted bucket still blocks an otherwise valid request for
  that grant.
- Repeat response comparisons with retained consumed, expired, superseded, and
  revoked credentials, and with wrong client, redirect, verifier, resource, or
  stale epoch as applicable. Each pair uses otherwise matching requests and
  stays below token-IP admission. Check other token errors retain their matrix
  precedence even when a stored grant's bucket is exhausted.
- A correct code request against an exhausted bucket receives the ordinary OAuth
  throttle body and positive `Retry-After`; correct refresh requests with
  omitted and explicit client IDs do too. Refusal creates no token and changes
  no credential or revocation state. After the returned delay, a still-live
  refresh succeeds; an expired code requires fresh authorization. A request
  whose authority becomes invalid before it obtains the validation locks must
  return its ordinary error without a throttle hint. Cover database failure
  during the read-only check and concurrent consume, rotation, revocation, and
  epoch changes; retain admitted reuse-revocation coverage.
- Unknown credentials create no failed-grant entries or overflow reservations,
  including under key churn and with the map at capacity. Cover wrong token
  kind, stored terminal credentials, database lookup errors, concurrent
  reservations, neutral release, success reset, stale completions, window
  expiry, and bounded overflow under the amended key. Keep the token-IP
  admission regression. Assert no code consumption, token rotation, or
  revocation when admission refuses or its lookup fails.
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

### Revocation checks

Exercise the proposed [contract](mcp-cimd-authorization.md#revocation) through
`HandleRevoke`, not only the form helper. Use independent DCR and CIMD fixtures,
each with multiple token families, a second grant for the same user, and another
owner's grant for the same client. Run both CIMD flag states; reset mutable
fixtures between cases. Use synthetic credentials only in test evidence.

- Cover both token kinds with omitted, matching, and mismatched IDs. Include
  another DCR UUID, existing and never-stored URLs, a URL client's internal
  UUID, and DCR parser-accepted UUID spellings. URL comparisons must not
  normalize equivalent percent encodings. Only the token's grant and every
  family under it are revoked; unrelated grants survive. Check the next `/mcp`
  request is `401`, refresh fails with `invalid_grant`, and outstanding codes
  cannot issue.
- Pair live tokens with retained expired, rotated, and revoked rows, well-shaped
  never-issued tokens of both kinds, and non-empty malformed tokens. Cross with
  omitted IDs, matching IDs, other existing IDs, and never-stored IDs. Compare
  status, exact empty body bytes, and all application headers. Pin or normalize
  only independent transport dates and request IDs. Repeat with no cookie, an
  unrelated session cookie, and Basic or Bearer Authorization; headers alone
  with no form token must not revoke anything.
- Instrument database, cache, DNS, and fetch boundaries. Malformed forms and
  malformed IDs do zero database work, even with live tokens. Non-empty
  malformed tokens do none after valid form checks. Unknown parsed tokens do
  only the normal digest lookup transaction. Found tokens use the same locks and
  writes for matching and mismatched IDs; no claimed-client lookup or network
  call occurs in any case. Assert these work classes, not wall-clock equality.
- Cover missing or empty token, empty ID, unknown fields (`client_secret`,
  `client_assertion`, `client_assertion_type`, `resource`, `grant_type`), and
  duplicates of every allowed key, including percent-encoded key aliases.
  Include invalid form escapes, the body limit and one byte over, wrong method,
  and absent, duplicate, or wrong Content-Type. Compare each closed error with
  live, unknown, and malformed candidates; assert no state change or input echo.
- Reuse all URL-grammar boundary fixtures, including size, whitespace, encoded
  dot segments, forbidden hostnames, ports, and malformed UUIDs. Combine each
  bad ID with live, unknown, and malformed tokens to prove validation
  precedence.
- Omitted, empty, unknown (`id_token`), correct, and opposite-kind hints all
  produce the same revocation result. Duplicate hints remain malformed forms.
- Reproduce the inferred Claude Code sequence with its public URL: refresh
  revocation followed by access revocation. Repeat each request and reverse the
  order. Both return `200`; no replay restores authority or affects other
  grants.
- Exhaust token-IP, stored failed-grant, and shared overflow budgets through
  token requests while outer admission allows revocation. Revoke must still
  succeed and leave all those limiter states unchanged. Separately simulate
  outer refusal before the handler: it must do no lookup or mutation and must
  not depend on token state. Hosted routing coverage proves the real edge
  policy.
- Capture telemetry across success, mismatch, syntax failure, lookup failure,
  and rollback. Only committed mismatches have the closed event and stored IDs;
  matching or omitted claims do not. Assert the contract's redaction using
  synthetic marker values, including a claimed URL with a sensitive path.
  Simulate telemetry failure after commit: the revocation result stays `200`.
- Inject failures at transaction begin, digest lookup, each lock, each mutation,
  and commit. Verify generic `500`, no secret echo, and rollback where failure
  precedes commit. Simulate commit acknowledgement loss without asserting that
  tokens stayed live; retry must end with the grant dead and empty `200`.
- Serialize concurrent revoke/revoke and revoke/refresh through both lock
  orders. A refresh that wins first has its successor revoked; one that follows
  revoke cannot issue. Reauthorization after revocation creates a distinct
  grant; replaying an old retained token must not revoke the replacement grant.
- Preserve the owner workflow's token-plus-hint and Go SDK cases. No claim about
  hosted Claude logout follows from the inferred Claude Code request shape.

## Production enablement proof

The owner completed the Claude Code proof on 2026-10-14. CI, independent review,
and hosted HTTPS proof execution remain outstanding.

For deployments, require independent review and exact-commit green CI. For a
production enablement proof, verify that metadata omits CIMD with the flag off
and that existing DCR access works. Apply the additive migration before serving
the binary and deploy backend and frontend together. No local test, fixture, or
stub address enters production configuration. With `OAUTH_CIMD_ENABLED=true`,
verify the metadata field and allow for client discovery caching before testing.
Use the designated owner test account to connect Claude Code by CIMD, approve,
list/read, reconnect, and prove one live grant for that URL. Reconnect must show
consent for loopback. Exercise refresh, revoke from Settings, then require the
next MCP request to fail and refresh to reject the revoked authority. Record
outcomes only, never tokens, request URLs, or resume content.

Hosted Claude's document URL remains unverified until its own connection is
observed through redacted server outcomes. A Claude Code proof alone cannot
claim hosted Claude support. If the proof fails, turn the flag off to stop new
CIMD authorization; revoke the test grant separately. Existing URL grants and
tokens survive flag disablement. Publish guide changes only for demonstrated
clients after a successful proof.
