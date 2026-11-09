# Sign-in security

This page holds the sign-in rules of [Authentication and security](security.md):
provider identity, password authentication, and the OAuth transaction.

## Provider identity

| Provider | Protocol              | Identity and registration email rule                                       |
| -------- | --------------------- | -------------------------------------------------------------------------- |
| Google   | OpenID Connect (OIDC) | `sub`; registration requires `email_verified=true`                         |
| LinkedIn | OIDC                  | `sub`; registration requires an explicitly verified email                  |
| GitHub   | OAuth 2.0             | Numeric user ID; registration uses the verified primary email from its API |

Provider subject, not email, is identity. During new-account login, a verified
email already owned by another account causes a generic
`email_already_registered` result and no database write. The response never
names the existing provider. Linking starts only from an authenticated account
and does not compare provider email. There is no account-email-change endpoint.

`DELETE /me/identities/{identityId}` unlinks one of the caller's identities. It
is a cookie-authenticated mutation with the full
[CSRF rule set](security.md#csrf-and-canonical-origin), requires recent
reauthentication, and is limited per `(account, client IP)`. A bearer token
never authenticates it, so an agent cannot unlink. It works for a provider that
`PROVIDER_LOGIN_ENABLED` has turned off. It refuses with `last_sign_in_method`
when the account would keep no password credential and no identity of an enabled
provider, and it writes nothing then. The check and the delete run in one
transaction under the user-row lock that password mutations and session issuers
take, so concurrent unlinks cannot both remove the last methods. The same
transaction writes one `identity_unlinked` lifecycle audit event with its kind
and time only. A malformed, unknown, or foreign ID returns the uniform not-found
response. Sessions record no provider, so every session stays signed in.

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
[ADR 0016](../adr/0016-sign-in-providers.md) record the decision.

## Password authentication

An account holds zero or one password credential alongside its linked provider
identities. A provider-only account has no credential; adding a password never
removes a provider identity, and a provider identity cannot move between
accounts. [ADR 0015](../adr/0015-accounts-passwords-and-sessions.md) records why
authentication authority stays in the application.

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

## OAuth transaction

Google and GitHub use authorization code with PKCE S256. LinkedIn uses its
documented confidential flow without PKCE and returns no nonce claim, so no
check binds a LinkedIn code to the starting browser
([ADR 0016](../adr/0016-sign-in-providers.md)). OIDC providers send a nonce and
validate signature, issuer, audience, expiry, and nonce; LinkedIn's nonce is
checked only when present. GitHub has a distinct callback and no invented OIDC
checks.

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

- `GET /api/v1/auth/{provider}/start` starts an unauthenticated login by
  default.
- The same `GET` route also starts the unauthenticated `view` purpose with
  `?purpose=view&slug={slug}`, which gates one public resume for sign in to view
  and never an account
  ([sign in to view](viewer-analytics/sign-in-to-view.md#sign-in-flow)).
- Authenticated link and reauthentication starts use a CSRF-protected `POST`.
  The response contains an authorize URL that the browser opens as a top-level
  navigation.
- A `GET` carrying `purpose=link` or `purpose=reauth` returns `405` and creates
  no transaction. Any other or unrecognized `purpose` value still starts login.

[ADR 0016](../adr/0016-sign-in-providers.md) records why privileged starts do
not use links or redirects.
