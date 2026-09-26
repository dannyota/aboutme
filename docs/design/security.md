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
provider starts only from an authenticated account. There is no
account-email-change endpoint.

`DELETE /me/identities/{identityId}` unlinks one of the caller's identities. It
is a cookie-authenticated mutation with the full CSRF rule set below, requires
recent reauthentication, and is limited per `(account, client IP)`. A bearer
token never authenticates it, so an agent cannot unlink. It works for a provider
that `PROVIDER_LOGIN_ENABLED` has turned off. It refuses with
`last_sign_in_method` when the account would keep no password credential and no
identity of an enabled provider, and it writes nothing then. The check and the
delete run in one transaction under the user-row lock that password mutations
and session issuers take, so concurrent unlinks cannot both remove the last
methods. The same transaction writes one `identity_unlinked` lifecycle audit
event with its kind and time only. A malformed, unknown, or foreign ID returns
the uniform not-found response. Sessions record no provider, so every session
stays signed in.

`GET /me` returns each linked identity's `id`, `provider`, and `createdAt`,
never the provider subject, ordered by `(created_at, id)`, oldest first. The
settings UI uses that stable first identity as its default reauthentication
provider. Equal timestamps must not make that choice depend on a PostgreSQL scan
plan.

Provider login is gated per provider by `PROVIDER_LOGIN_ENABLED`, default off.
It takes blank or `false` (none), `true` (all three), or a comma list such as
`google`. A disabled provider has no start or callback route, so its login,
settings link, and reauthentication starts return the uniform not-found
response. With `ENV` set to `prod` or `staging`, only an enabled provider needs
its client ID and secret, and production can enable Google and LinkedIn.
[ADR 0027](../adr/0027-provider-login-flag.md),
[ADR 0039](../adr/0039-per-provider-login-enablement.md), and
[ADR 0058](../adr/0058-linkedin-sign-in-in-production.md) record the decision.

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

`PASSWORD_REGISTRATION_ENABLED=false` turns off email-and-password sign-up: the
register route is not registered and returns the uniform not-found response.
Blank or `true` keeps it on, and any other value stops startup. Verification of
pending registrations, login, reset, and password add or change still work, and
provider sign-up is unaffected. The capabilities read reports
`passwordRegistration` so the web hides the sign-up form.

## Second factor

An account may add passkeys or an authenticator app. An enrolled account needs
primary proof followed by an active factor or a single-use recovery code.
Primary proof creates a bounded pending authentication, not a session, and the
pending cookie reaches no session route. An account authentication epoch binds
sessions, agent grants, and authorization codes; every factor change advances it
and revokes stale authority in the same transaction. Sensitive actions on an
enrolled account need both proofs inside the 15-minute window. TOTP secrets are
sealed under a runtime key ring, and a key failure disables TOTP only. The
[second-factor design](second-factor-authentication.md) owns the rules.

## OAuth transaction

Google and GitHub use authorization code with PKCE S256. LinkedIn uses its
documented confidential flow without PKCE; the OIDC nonce defends against code
injection ([ADR 0058](../adr/0058-linkedin-sign-in-in-production.md)). OIDC
providers use a nonce and validate signature, issuer, audience, expiry, and
nonce. GitHub has a distinct callback and no invented OIDC checks.

The server stores transaction state, purpose, the opaque-handle hash, PKCE
verifier, exact provider redirect URI, bounded login return path, expiry, and
OIDC nonce. The return path is one same-origin relative path of at most 2,048
bytes; invalid input becomes `/app/resumes`. The login and registration pages
apply the same rule to `next`:

- The path starts with exactly one `/` and holds no backslash or control
  character, both as written and after percent-decoding, so `/%2F%2Fhost` and
  `/%5Chost` are rejected. The decoded path has no `.` or `..` segment, so
  `/.//host` and `/%2e//host` are rejected too.
- It parses to the same origin, with no scheme or host.
- For `/app/new`, matched as the router matches it (any case, one optional
  trailing slash), only `sample`, `template`, and `lng` survive, each at most
  once. `lng` must be `en` or `vi`. `sample` and `template` must be known
  template ids on the pages and well-formed ids on the server. Every other
  parameter and the fragment are dropped, and the path is kept. The new-resume
  page re-checks that the ids exist.

The browser holds only the 256-bit opaque `__Host-oauth-tx` handle. The database
stores its SHA-256 hash. A transaction is consumed atomically once and expires
after ten minutes.

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

| Control           | Rule                                                                                                                                           |
| ----------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| Idle expiry       | 30 days; `last_seen_at` updates at most once per hour                                                                                          |
| Absolute expiry   | 90 days                                                                                                                                        |
| Rotation          | After 24 hours; one admitted winner and at most one successor per predecessor                                                                  |
| Rotation delivery | Successor use sets the predecessor deadline to `min(existing deadline, now + 60 seconds)`; it never extends it                                 |
| Recent reauth     | 15 minutes                                                                                                                                     |
| Sensitive actions | Provider link, password add/change, factor management, account deletion, slug release, session/grant revoke, and consent require recent reauth |
| Visibility        | Account settings list sessions and permit per-session revoke                                                                                   |

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
callbacks use that exact origin. Production and the local HTTPS harness use
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
`plain` is rejected. The authorize request is validated in full: `client_id`,
exact registered `redirect_uri`, `response_type`, requested scopes, and the
challenge. Validation finishes before any redirect or user interaction, so an
open-redirect or `redirect_uri` substitution attempt fails closed.

Authorization codes are single-use, expire in 60 seconds, and bind the client,
user, scopes, challenge, and exact redirect URI. Replaying a consumed code
revokes every token issued from it. Access and refresh tokens are opaque 256-bit
random values with distinguishing prefixes, stored only as 32-byte SHA-256
digests and compared in constant time after exact shape decoding. Refresh tokens
rotate on every use inside one family; presenting a superseded refresh token
revokes the whole family. An access token lives one hour and a refresh family
has a 30-day absolute lifetime. Revocation through RFC 7009 or the settings UI
kills the grant and its token families in one transaction. Exact rate, cap, and
body bounds live in [the numeric budgets](budgets.md).

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

Anonymous login starts, privileged provider starts, password routes, and agent
routes each add their own policies on the same limiter and canonical client
address; [the numeric budgets](budgets.md) list them. Each OAuth start deletes
only a bounded batch of expired transactions before inserting one row, so
cleanup never becomes unbounded request work. Unknown, provider-only, and
wrong-password login states stay byte-identical.

## Viewer analytics

View counting sets no cookie and stores nothing about viewers. Resumes that
require sign-in to view gate their public routes behind a signed per-resume pass
cookie. The OAuth purpose `view` verifies a viewer for one resume, keeps nothing
about them, and never reads, creates, or signs in an account.
[Viewer analytics](viewer-analytics/README.md) owns the rules.

## No operator surface

The public application has no privileged role, operator session, or route that
reads or changes another account's data. `/admin` is a reserved public root that
Caddy denies. Operator actions are command-line tools that require a database
URL and a database-name guard. [ADR 0028](../adr/0028-no-operator-surface.md)
owns this boundary.

## Public artifact revocation

A cache hit, an object key, and an SSE event are never authorization. Every
public reuse revalidates at the origin, and unpublish, delete, and rename wait
for the revocation fence
([ADR 0022](../adr/0022-public-artifact-revocation.md)).

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

## Content Security Policy and framework headers

Every Nuxt-rendered response carries a Content Security Policy and never
`X-Powered-By`. Public resume HTML, the internal print route, and the render
harness use a fully locked-down policy with no origin of its own
(`default-src 'none'`, `base-uri 'none'`, `form-action 'none'`); every other
Nuxt page (`/`, `/login`, `/templates/**`, `/app/**`, and anything else Nuxt
serves) uses a policy scoped to the app's own origin instead
(`default-src 'self'`, `base-uri 'self'`, `form-action 'self'`). Both keep
`frame-ancestors 'none'` and `object-src 'none'`, and both forbid
`'unsafe-eval'` outright. `apps/web/app/utils/csp.ts` holds the exact strings.

`script-src` never carries `'unsafe-inline'` on either policy: Nuxt's own
hydration payload is externalized into a same-origin script file for every
response. The homepage and template pages each render their JSON-LD as a
`<script type="application/ld+json">` block. Per the HTML spec, a script with
that type is a data block the browser never executes
(<https://html.spec.whatwg.org/multipage/scripting.html#data-block>), so
`script-src` does not govern it and these pages add no source for it. Public
resume HTML's JSON-LD, generated by the Go API
(`apps/server/internal/publicformat/jsonld.go`), still earns a response-specific
`'sha256-<hash>'` source in its own separate policy. `style-src` keeps
`'unsafe-inline'` on both Nuxt policies: the resume renderer writes per-document
customization as inline `style` attributes, an unbounded, per-render value set
no fixed hash or nonce list could cover.

The Go API sends its own separate, fully locked-down policy on every JSON,
media, and event-stream response; it never serves HTML.

The editor validates a fetched resume document in the browser against the resume
schema. A runtime `ajv.compile()` call emits a `new Function`, which
`script-src` without `'unsafe-eval'` blocks, so the editor's validator is
instead an Ajv standalone compile of the schema, generated at build time into a
plain module with no eval of any kind
(`apps/web/scripts/generate-document-validator.mjs`,
`apps/web/app/editor/documentValidator.generated.mjs`).

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

A compressed-byte limit does not bound decoded memory, so photo intake checks
dimensions and pixel count before full decode, rejects animation and malformed
containers, and runs under one task-wide permit; a busy task rejects before
reading the body. Only re-encoded static JPEG or PNG pixels reach storage, with
every metadata block, profile, and trailing byte dropped. Decoder details,
filenames, and metadata never enter responses or logs. A rejection before the
object write leaves PostgreSQL and storage unchanged; an object write with an
unknown outcome is never deleted by the request and is left to reconciliation.
[The API design](api.md#photo-intake) owns normalization, the
[deployment design](deployment.md#media) owns the write and deletion order, and
[ADR 0019](../adr/0019-private-media-delivery.md) owns the storage boundary.

Secrets never enter source, OpenTofu state where avoidable, URLs, or logs.
Production fails closed when trusted proxies, provider credentials, origin
settings, or origin certificate configuration is incomplete.
