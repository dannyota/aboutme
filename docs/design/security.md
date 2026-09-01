# Authentication and security

Authentication uses external providers plus an optional application-owned
password credential, opaque server-side sessions, one exact web origin, and a
fail-closed request boundary. The service stores no plaintext passwords and no
provider refresh tokens. A password exists only as an Argon2id hash, a
verification or reset token only as a SHA-256 digest, and an email job only as
encrypted payload bytes.

People are authenticated by cookie sessions. Agents are authenticated by bearer
tokens issued by the service's own OAuth 2.1 authorization server. The two
worlds do not overlap: a cookie never authorizes an agent route, and a bearer
token never reaches a cookie route.

## Provider identity

| Provider | Protocol              | Identity and registration email rule                                       |
| -------- | --------------------- | -------------------------------------------------------------------------- |
| Google   | OpenID Connect (OIDC) | `sub`; registration requires `email_verified=true`                         |
| LinkedIn | OIDC                  | `sub`; registration requires an explicitly verified email                  |
| GitHub   | OAuth 2.0             | Numeric user ID; registration uses the verified primary email from its API |

Provider subject, not email, is identity. A verified email already owned by a
different provider causes a generic `email_already_registered` result and no
database write. The response never names the existing provider. Linking a second
provider starts only from an authenticated account. V1 has no provider unlink or
account-email-change endpoint.

`GET /me` orders linked identities by `(created_at, id)`, oldest first. The
settings UI uses that stable first identity as its default reauthentication
provider. Equal timestamps must not make that choice depend on a PostgreSQL scan
plan.

## Password authentication

An account holds zero or one password credential alongside its linked provider
identities. A provider-only account has no credential; adding a password never
removes a provider identity, and a provider identity cannot move between
accounts.
[ADR 0025](../adr/0025-password-authentication-and-identity-linking.md) records
why authentication authority stays in the application.

One canonical email parser is shared by provider account creation, password
registration, login lookup, database writes, and rate-limit keys. It accepts a
bounded ASCII addr-spec with no display name, comments, controls, surrounding
space, or internationalized spelling, and stores the whole address in lowercase.
Accounts are never merged by email, and a provider email is never synchronized
into the account email.

Registration verifies the email before creating a user or session. A pending
credential and an encrypted verification-mail job are committed; the single-use
token creates the account and credential atomically, or yields to a concurrent
provider signup on the unique email. Login verifies the Argon2id hash outside a
transaction, then locks and rechecks the credential and account before creating
the same opaque session as a provider login. Password add or change requires
recent reauthentication and atomically creates one fresh non-lineage current
session while revoking every old session. Reset revokes every session and never
logs in. Password removal and account-email change are out of scope.

## OAuth transaction

All providers use authorization code with PKCE S256. OIDC providers also use a
nonce and validate signature, issuer, audience, expiry, and nonce. GitHub has a
distinct callback and no invented OIDC checks.

The server stores transaction state, purpose, the opaque-handle hash, PKCE
verifier, exact provider redirect URI, bounded login return path, expiry, and
OIDC nonce. The return path is one same-origin relative path of at most 2,048
bytes; invalid input becomes `/app/resumes`. The browser holds only the 256-bit
opaque `__Host-oauth-tx` handle. The database stores its SHA-256 hash. A
transaction is consumed atomically once and expires after ten minutes.

A privileged transaction binds the user who started it, not one concrete session
ID. Its callback must authenticate a live session for that same user; a session
for another user fails through the no-oracle path. Reauthentication updates only
the concrete session that completes the callback.

The transaction cookie is `Secure; HttpOnly; SameSite=Lax; Path=/` with no
`Domain`. Every callback clears it on success or failure. Provider access and ID
tokens exist only for the code exchange and profile fetch, then are discarded;
no provider refresh token is stored.

OAuth start methods are purpose-specific:

- `GET /api/v1/auth/{provider}/start` starts an unauthenticated login only.
- Authenticated link and reauthentication starts use a CSRF-protected `POST`.
  The response contains an authorize URL that the browser opens as a top-level
  navigation.
- A `GET` carrying `purpose=link` or `purpose=reauth` returns `405` and creates
  no transaction.

[ADR 0014](../adr/0014-oauth-start-methods.md) records why privileged starts do
not use links or redirects.

## Sessions

The browser cookie contains a 256-bit opaque token. PostgreSQL stores only its
SHA-256 hash. The cookie is
`__Host-session; Secure; HttpOnly; SameSite=Lax; Path=/` with no `Domain`
attribute.

| Control           | Rule                                                                                                                                |
| ----------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| Idle expiry       | 30 days; `last_seen_at` updates at most once per hour                                                                               |
| Absolute expiry   | 90 days                                                                                                                             |
| Rotation          | After 24 hours; one admitted winner and at most one successor per predecessor                                                       |
| Rotation delivery | Successor use sets the predecessor deadline to `min(existing deadline, now + 60 seconds)`; it never extends it                      |
| Recent reauth     | 15 minutes                                                                                                                          |
| Sensitive actions | Provider link, password add/change, account deletion, slug release, per-session revoke, and logout-everywhere require recent reauth |
| Visibility        | Account settings list sessions and permit per-session revoke                                                                        |

Logout revokes the session, expires the cookie, and sends `Clear-Site-Data`.
Logout-everywhere revokes all sessions. Password reset revokes every session and
creates none; password add/change revokes every session and creates one fresh
current session. [ADR 0015](../adr/0015-session-rotation-delivery.md) defines
rotation convergence and the lost-response case.

## CSRF and canonical origin

Cookie-authenticated mutations require all of:

- The synchronizer token returned in the authenticated `/me` response body.
- `Content-Type: application/json` for JSON bodies. Bodiless mutations omit it,
  and the photo upload route uses its specified media type.
- An exact `Origin` match, with exact `Referer` fallback only when needed.
- A valid session.

CSRF-token comparison is constant-time. Web v1 exposes no credentialed
cross-origin resource sharing (CORS) surface.

The application has one configured public origin. Apex and `www` cannot both
serve the authenticated application; one redirects before auth routes. Provider
callbacks use that exact origin. Production and user acceptance testing use
HTTPS because `__Host-` cookies are always `Secure`. Plain HTTP native
development is suitable for non-authenticated work only.

Startup lowercases the scheme and host and removes a default port. It does not
canonicalize internationalized domain names or equivalent IPv6 spellings.
Operators must configure the exact browser-serialized origin; an equivalent-
looking but differently serialized host fails the exact CSRF comparison.

A route chooses its authentication mode once. CSRF is required when that mode is
cookie, and the presence of an arbitrary `Authorization` header never bypasses
it. The agent token routes below choose bearer instead and never parse a cookie;
the deferred mobile client will make the same one-time choice.

## Agent authorization and the bearer world

`internal/oauthsrv` is a first-party OAuth 2.1 authorization server. Agents are
public clients that register dynamically, then obtain tokens through
authorization code with PKCE. `code_challenge_method=S256` is required and
`plain` is rejected. The authorize request is validated in full — `client_id`,
exact registered `redirect_uri`, `response_type`, requested scopes, and the
challenge — before any redirect or user interaction, so an open-redirect or
`redirect_uri` substitution attempt fails closed.

Authorization codes are single-use, expire in 60 seconds, and bind the client,
user, scopes, challenge, and exact redirect URI. Replaying a consumed code
revokes every token issued from it. Access and refresh tokens are opaque 256-bit
random values with distinguishing prefixes, stored only as 32-byte SHA-256
digests and compared in constant time after exact shape decoding. Refresh tokens
rotate on every use inside one family; presenting a superseded refresh token
revokes the whole family. An access token lives one hour and a refresh family
has a 30-day absolute lifetime. Revocation through RFC 7009 or the settings UI
kills the grant and its token families in one transaction. Exact rate, cap, and
body bounds live in [the numeric budgets](../plans/budgets.md).

Scopes are closed to `resumes:read` and `resumes:write` and are enforced inside
the resource server, per tool, never by the client. A token grants no publish,
public-read, session, or account surface. Missing scope returns a closed
`403 scope_denied` without touching resume state, and absent, malformed,
expired, revoked, superseded, and cross-user tokens produce byte-identical
closed 401 responses naming the protected-resource metadata URL.

Cookie isolation is explicit: `/mcp`, `/oauth/token`, `/oauth/register`, and
`/oauth/revoke` never parse cookies, so no CSRF surface exists there. The
authorize and consent surfaces keep the full session, CSRF, and exact-Origin
chain. Client-supplied names are bounded, sanitized text rendered as text, never
markup. Token material, code material, PKCE verifiers, and resume content never
enter logs, traces, metrics labels, errors, or panic text; logs carry client ID,
grant ID, token row ID, tool name, resume ID, and closed outcomes only. Existing
per-user resume caps, sanitizer versioning, and media privacy bounds apply to
agent writes unchanged. [ADR 0026](../adr/0026-mcp-agent-access.md) records this
boundary.

## Client address and rate limits

Caddy is the only component that interprets forwarding headers. It validates the
production origin path, discards viewer-supplied forwarding headers, derives one
canonical address, and sends it to Go. Go accepts that value only from
configured trusted-proxy CIDRs and never parses `X-Forwarded-For`.

Each limiter stores at most 10,000 per-key buckets. A fully refilled bucket
expires because it is equivalent to a new bucket; 24 hours without any request
is the hard idle expiry. Active entries are never evicted to give an attacker a
fresh bucket. When the map is full, every untracked key shares one bounded
global overflow bucket with the same budget as one ordinary key. Admission
refusal is not an alternative. Policies can key by IP, account, or
account-and-IP. [ADR 0018](../adr/0018-bounded-rate-limiter.md) records the
failure model.

Anonymous login starts are limited to 30 per minute per client IP. Authenticated
provider-link and reauthentication starts are separately limited to 30 per
minute per `(account, client IP)` pair. Each start deletes only a bounded batch
of expired OAuth transactions before inserting one row, so unauthenticated
traffic cannot turn cleanup into unbounded request work.

Password routes add their own bounded policies: login admission and a per-email
failure budget, registration/forgot per-email and per-IP, verification/reset
token consumption, and `(account, client IP)` for add/change/reauthentication.
Exact values live in [the numeric budgets](../plans/budgets.md). Unknown,
provider-only, and wrong-password states stay byte-identical.

Agent routes add registration and token policies per client IP, a failed-grant
budget per client, tool-call budgets per token and per user, a
concurrent-request cap per user, and a live-grant cap per account. All compose
the same bounded limiter and the canonical Caddy client address.

## Public artifact revocation

Every public response revalidates current slug, live state, route flags, and
public generation at the origin before a shared cache can reuse bytes. The
revocation fence drains old-generation origin responses before unpublish,
delete, or rename returns success. A cache hit, an object key, and an SSE event
are never authorization. [ADR 0022](../adr/0022-public-artifact-revocation.md)
owns this boundary.

## Internal print authority

Go authorizes and freezes one render snapshot, then grants Nuxt a random 256-bit
one-use capability bound to resume, snapshot version and digest, caller,
`nuxt-print` audience, and a maximum 60-second lifetime. Redemption is atomic
over a loopback or deployment-private internal interface. Chromium carries no
account cookie, and an ID-only request never renders. Tokens are absent from
URLs and logs. Caddy's external `/print/**` denial remains defense in depth. Go
retains capability and consumed-job state only inside a reserved slot in the
bounded render queue. At most one unused capability exists per active job. An
unused record is removed at its 60-second expiry; consumed state and controller
authority are removed on acceptance, discard, render timeout, or process loss.
Only the controlling render job may submit completed bytes for a terminal
generation and digest check. Completion has one atomic winner; later attempts
receive a generic not-active result without a terminal tombstone. Nuxt and
Chromium never publish artifacts.
[ADR 0023](../adr/0023-private-print-capability.md) defines the protocol.

## Untrusted document content

Rich text uses one versioned allowlist and shared hostile corpus. Go sanitizes
every write and re-sanitizes every document before it crosses into a server-side
rendering (SSR) surface, including public HTML and the internal print route.
DOMPurify runs before client-side `innerHTML` assignments only; it does not ship
in the server-rendered bundle. SSR proves neutralization of the Go-sanitized
input rather than running a Node DOM sanitizer. A strict content security policy
remains a backstop. [ADR 0012](../adr/0012-ssr-sanitizer-authority.md) owns this
split.

## Untrusted media

A compressed-byte limit does not bound decoded image memory. Photo intake checks
dimensions and pixel count before full decode, confirms the decoded bounds,
rejects animation and malformed containers, and runs under one task-wide permit.
Body read and object write have cancellable deadlines. Normalization is
synchronous: its five-second ceiling is a measured release gate, and a request
never returns while decoder work remains. A busy task rejects before reading the
body.

Only decoded pixels cross the storage boundary. The normalizer applies one valid
Exif orientation and re-encodes a static JPEG or PNG. It drops Exif and GPS
data, XMP, IPTC, comments, thumbnails, ICC profiles, unknown optional chunks,
and trailing bytes. Decoder details, filenames, and metadata never enter
responses or logs. Every rejection before object-write dispatch leaves both
PostgreSQL and object storage unchanged. A remote create with an unknown outcome
leaves PostgreSQL unchanged and may leave one unreachable private object; the
request never deletes its uncertain key, and bounded reconciliation later proves
whether it is unreferenced. [ADR 0019](../adr/0019-private-media-delivery.md)
owns the storage and failure boundary; [the API design](api.md#photo-intake)
owns the normalization contract.

Removing a media reference and enqueuing its exact-key deletion job are one
transaction. Private storage plus reference-gated reads revoke access at commit;
physical deletion may follow asynchronously and targets completion within 24
hours. Overdue deletion is audited, alerted, and retried. Weekly orphan
reconciliation covers crash candidates and queue/accounting gaps without making
object existence an authority.

Secrets never enter source, Terraform state where avoidable, URLs, or logs.
Production fails closed when trusted proxies, provider credentials, origin
settings, or origin-secret configuration is incomplete.
