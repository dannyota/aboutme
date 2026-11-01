# MCP CIMD authorization

Status: implemented and enabled in production. See
[ADR 0018](../adr/0018-mcp-agent-access.md) for the owner proof and outstanding
verification. [MCP Client ID Metadata Documents](mcp-cimd.md) holds the
motivation, sources, release order, and the approval record.

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
required URL-to-row lookup and the grant identity and refusal rules below. CIMD
never consumes a client secret or assertion. The closed token form rejects
`client_secret`, `client_assertion`, and `client_assertion_type` as unknown
fields. An HTTP Authorization header supplies no client identity or authority;
all public-client code, PKCE, token, and claimed-client checks still apply.

A failed document check never returns to an untrusted URI, including on denial.
Go returns plain text or JSON, not a new HTML error page. Nuxt maps consent
`400` to its existing bilingual invalid-request message, and `429` to its
existing unavailable message; neither includes a remote failure reason. Session,
CSRF, reauthentication, and grant-limit failures keep their existing contracts.
Consent responses retain `Cache-Control: no-store, no-transform`. Authorize
responses carrying request or code state must also be non-cacheable.

## Revocation

Owner decision [C12](mcp-cimd.md#owner-decisions) (2026-10-14) approves
possession-only revocation, the timing limit below, and ignoring invalid hints.

`POST /oauth/revoke` keeps the exact `application/x-www-form-urlencoded` media
type and [body bound](budgets.md). Allow only `token` (required, non-empty),
`token_type_hint` (optional), and `client_id` (optional, non-empty), each once
after decoding. Unknown keys, duplicates, invalid encoding, oversized bodies,
and missing or empty required values return the closed `400 invalid_request`
from [Errors](#errors). Wrong method and media type retain `405` and `415`.
Ignore all hint values, including empty, unknown, and opposite-kind values, per
[RFC 7009 section 2.2][revoke-response].

Validate a supplied `client_id` before token parsing, transactions, or any
database access: `https://` values use the
[URL grammar](mcp-cimd-fetch.md#client-id-url-rules), others the DCR UUID
parser. Failure uses the closed `400`. Never resolve the claim, read a metadata
cache, perform DNS, or fetch. Cookies and Authorization headers supply no
authority. Both CIMD flag states use this contract. Existing outer admission
stays; no limiter is added. Revocation never consults or changes token-IP or
failed-grant budgets, including overflow; exhausted grants remain revocable.
Outer refusal depends on traffic only, never credential state.

Possession selects authority. A non-empty malformed token returns empty `200`
without database access. Parsed tokens get a digest lookup across both kinds; a
miss returns `200`. Any retained row, including expired, rotated, or revoked
ones, selects its own client, user, then grant locks. Revoke that grant and all
its families atomically; return `200` after commit. Other grants survive.
Concurrent refresh shares these locks: no successor survives revocation.

Compare claims only with the locked client: exact URL bytes for CIMD, parsed
UUID equality with `metadata_url IS NULL` for DCR. Internal UUIDs never match
URL clients. Mismatch still revokes. After commit, log `client_id_mismatch` with
only stored client, grant, and token row IDs. Never log claims or hashes, tokens
or digests, forms, cookies, Authorization headers, or raw database errors.
Telemetry failure cannot affect success; metrics use closed labels. Omitted or
matching claims emit no mismatch event.

All admitted well-formed forms have the same empty `200` and application headers
for malformed, unknown, live, expired, rotated, revoked, and mismatched tokens.
No credential-dependent cookie, challenge, redirect, retry, or rate header;
transport dates and request IDs are independent of token state. Malformed forms
return the same closed error for every token. This is response equality, **not
constant-time or fault-path equivalence**: syntax rejection and malformed tokens
do no database work, misses do a lookup, and hits lock and write. Matches and
mismatches follow the same transaction path, apart from bounded telemetry. Do
not add claim-dependent lookups or delays. Timing and database failures can
still distinguish these work classes.

Database failure returns generic `500`, never success or unknown-token `200`.
Roll back uncommitted writes. Failed commit acknowledgement leaves an unknown
outcome: assume authority may remain and retry idempotently. Reconnection after
successful logout requires fresh authorization; no grant is restored.

[RFC 7009 sections 2.1 and 5][rfc7009] require a public client ID and
token/client binding. Optional IDs and revocation on mismatch deliberately
depart from that rule. [RFC 6749 section 2.3][rfc6749] limits public-client
authentication; it does not waive binding. A holder can omit or replace the
claim. An error exposes binding; silent `200` refusal can leave live authority.
Even a retained dead token can disconnect its still-live grant. Token entropy,
HTTPS, body bounds, and outer admission limit abuse. Issuance keeps its binding
checks.

Ship one backend patch after approval, review, and green CI, then prove logout.
No migration, new flag, key, or stored field. Older binaries can reject claimed
IDs: retry without the ID, which remains supported, or revoke in Settings.

[rfc7009]: https://www.rfc-editor.org/rfc/rfc7009#section-2.1
[revoke-response]: https://www.rfc-editor.org/rfc/rfc7009#section-2.2
[rfc6749]: https://www.rfc-editor.org/rfc/rfc6749#section-2.3

## Failed-grant admission

Technical amendment approved by the primary for the owner on 2026-10-14: isolate
failed-grant admission by the stored grant, for both CIMD and DCR. Naming a
public client identifier must not spend any owner's grant budget.

After the existing token-IP admission and form decoding, resolve the presented
credential with the existing strict parser and digest lookup. An authorization
code row supplies its `GrantID`; a refresh-token row supplies its `GrantID` only
when both the parsed token and stored row have refresh kind. Use that stored
grant UUID as the failed-grant key. Never derive it from the claimed client, a
cookie, a supplied grant ID, or metadata fetching. The same grant's codes and
refresh tokens share a bucket; different owners of one client have different
grant UUIDs. A supplied wrong client cannot redirect debt to that client's
grants. Internal UUID aliases still fail public client validation.

Missing, malformed, wrong-kind, or never-issued credentials reserve no grant
slot and create no failed-grant key, including no overflow debt. They remain
under the existing token-IP limit and return the existing token errors. An
admission lookup database failure returns generic `500` without a reservation or
token mutation; it must not mean an unknown credential. This lookup chooses
admission identity only. Token processing must still recheck all authority and
bindings under the existing locks. A consumed, expired, superseded, or revoked
credential whose row remains can select only its own stored grant.

Keep the existing bounded fixed-window reservation algorithm: reserve before
processing, count pending reservations against the limit, retain a failure only
for the existing counted `invalid_grant` outcomes, and release neutral outcomes.
Success clears only that tracked grant's window; stale completions cannot
restore cleared debt. Expiry, monotonic time, maximum keys, shared overflow,
overflow-success behavior stay unchanged. A refusal blocks token issue even when
its public response hides the refusal, as defined below. Numeric limits live in
[Budgets](budgets.md). Unknown guesses cannot fill this map; holders of enough
real grants can still reach the shared overflow boundary of
[ADR 0007](../adr/0007-bounded-rate-limiter.md). Requests sharing an IP still
share the separate token-IP budget.

### Refusal response and client recovery

A grant-bucket refusal must not reveal whether a candidate credential has a
stored row. Determine the response through the existing token validation rules,
without token mutations. A request that lacks valid authority keeps the error it
would receive without the grant limiter. In particular, stored and never-issued
candidates that fail with `invalid_grant` must have identical status, body
bytes, and headers before and after exhaustion:

- Status `400`; body exactly
  `{"error":"invalid_grant","error_description":"The request is invalid."}`.
- `Content-Type: application/json` and `Cache-Control: no-store`.
- No `Retry-After`, rate-limit header, cookie, or other signal of grant state.
  All other response headers follow the same path as ordinary `invalid_grant`;
  transport-generated dates and request IDs must be independent of credential
  existence and admission outcome.

The [error matrix](#errors) still governs malformed requests and unknown
clients; grant refusal cannot override their errors either. Token-IP refusal
remains before lookup and keeps its `429` and `Retry-After` for all candidates.

Return the existing `429 invalid_request` with its fixed error description and
positive `Retry-After` only if the request would otherwise be authorized to
receive tokens. Row existence alone is insufficient. For a code, require the
correct public client, exact redirect, S256 verifier, live unconsumed code, live
grant and user, and matching authentication epoch. For refresh, require a
current live refresh token and family, live grant and user, matching epoch, and
a matching public client when supplied; omission remains allowed. Both paths
must pass all existing form, resource, scope, and binding checks.

On refusal, run those checks under the existing token transaction lock order,
re-reading state rather than trusting the admission lookup. Use the same
validation predicates as issuance, but stop before any write. Do not consume a
code, issue or rotate tokens, revoke a code's tokens or a reused refresh family,
clear debt, or finish a nonexistent reservation. End the transaction without
business changes. A database failure remains generic `500`, never evidence of
valid authority. An admitted request keeps normal token processing, including
reuse revocation. A concurrent consume, rotation, revocation, or epoch change
that wins the locks must prevent a subsequent valid-authority classification.

A client presenting valid authority still learns to wait from `Retry-After`, the
remaining grant window rounded up to at least one second. It must not retry
before that delay. The delay does not extend credential lifetime: an expired
code requires fresh authorization, and refresh can be retried only while its
authority remains live. A client presenting invalid authority receives no
throttle hint; it must stop retrying the unchanged request and correct its
parameters or obtain fresh authorization through its ordinary `invalid_grant`
recovery. Older clients need no new error or header handling.

This preserves useful backoff without creating buckets for unknown guesses. The
cost is read-only authority validation even for a refused stored grant; token-IP
admission still bounds that work per address. It promises response equivalence,
not constant-time database access. Returning `400` for every refusal would also
hide existence but would tell a client with live authority to reconnect
unnecessarily. Returning `429` before authority validation would expose stored
credentials. Shared overflow follows the same response rules.

### Scope and rollout

DCR has the same flaw: its public UUID identifies a client, not the caller or
the grant owner. It therefore receives this same limiter correction; its
registration, redirects, consent, and token authority stay unchanged. Using
existing stored grant UUIDs avoids new composite keys, per-credential buckets,
or a second source-address policy. Counting arbitrary presented credentials
would let anonymous key churn fill the bounded map and spend shared overflow.

A stolen code or refresh token can spend its own grant's allowance, but cannot
select another grant. Existing revocation and recovery rules apply; this adds no
key, secret, persistent data, or telemetry field. Apply the correction with the
CIMD backend before enablement, including while CIMD is off. No migration,
client update, or data loss is required. Restart clears process-local limiter
state as before; older clients keep the same token forms and error bodies.

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
