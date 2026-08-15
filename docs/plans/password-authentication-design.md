# Password authentication design

Status: Draft for written review (design decisions approved 2026-08-15)

## Purpose

Add email-and-password authentication without replacing Google, GitHub, or
LinkedIn authentication. One account may have one optional password credential
and any number of linked provider identities. Existing opaque sessions remain
the only authenticated browser session mechanism.

This is a deliberate change to the Approved v4 provider-only scope in
[`../design/product.md`](../design/product.md) and
[`../design/security.md`](../design/security.md). Implementation starts by
updating those authorities and accepting an ADR. Code must not land while the
provider-only text and this design disagree.

## Decisions

- Password sign-in uses the account email, not a username.
- `users.email` stays the unique, case-insensitive account email.
- A provider signup creates an account from its verified email with no password.
- A signed-in provider user may add a password after recent reauthentication.
- An account may link Google, GitHub, and LinkedIn identities whose provider
  emails differ from the account email.
- A provider identity belongs to exactly one account and cannot be moved.
- Provider email is used only when a provider creates a new account. Later
  provider email claims are ignored and are neither displayed nor synchronized.
- Accounts are never merged by email.
- Password registration must verify the email before creating a user or session.
- Password reset revokes every session and does not log the user in.
- Production mail uses AWS SES v2 in `ap-southeast-1`. Local development uses a
  bounded native mail-capture service and makes no AWS call.
- Password removal and account-email change remain out of scope.

The selected architecture keeps authentication authority in the application.
Cognito-backed password-only authentication would split account and session
authority. Moving every provider and session to Cognito would be a larger,
provider-locking migration.

## Account and identity invariants

`users` remains the account authority. It owns the canonical email, display
name, account lifecycle, resumes, and sessions.

`identities` remains the provider identity authority. A row is identified by the
provider and stable provider subject. Its global unique constraint prevents one
provider account from being linked to two internal accounts.

`password_credentials` supplies a second authentication method. It contains zero
or one row per user. A user with no row is a provider-only user. A user with a
row may sign in through password or any linked provider.

One canonical email parser is shared by provider account creation, password
registration, login lookup, database writes, and rate-limit keys. It accepts a
bounded ASCII addr-spec with no display name, comments, controls, surrounding
space, or internationalized spelling, and stores the whole address in lowercase.
Provider signup cannot create an account from a claim outside that grammar.
Provider linking does not inspect the email claim. A migration preflight proves
that every existing account email already has the canonical form before the
shared parser becomes authoritative.

Provider identity resolution follows these rules:

1. A returning provider subject resolves its existing identity. Provider email
   is not read for account identity or account-email updates.
2. A new provider subject may create a user only when the provider supplies the
   required verified registration email.
3. If that email already belongs to an account, signup creates nothing and does
   not link automatically. The provider-authenticated browser receives a closed
   instruction to sign in through an existing method and link from settings.
4. An authenticated provider-link transaction binds the stable subject to the
   current user. A different provider email is accepted and not persisted.
5. A subject already bound to another user returns a generic link conflict and
   changes nothing.

## Data model

An additive migration introduces four bounded tables.

| Table                    | Purpose and required state                                                                                                                                            |
| ------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `password_credentials`   | `user_id` primary key and cascading foreign key, encoded Argon2id hash, created time, changed time                                                                    |
| `password_registrations` | UUID, unique normalized email, bounded name, encoded password hash, unique SHA-256 token digest, created time, 24-hour expiry                                         |
| `password_reset_tokens`  | UUID, unique user ID, unique SHA-256 token digest, created time, 30-minute expiry                                                                                     |
| `auth_email_jobs`        | UUID, closed message kind and state, scope key, optional token digest, encryption key ID, nonce, ciphertext, attempts, next-attempt time, expiry, sent/terminal times |

All token digests are exactly 32 bytes. Encoded password hashes and encrypted
payloads have explicit byte ceilings. Database constraints enforce closed kinds,
states, expiry ordering, attempt bounds, and the zero-or-one active token rule.
Account deletion cascades through credentials, reset tokens, and associated
jobs. Expired pending registrations and terminal jobs are removed in bounded
batches.

The outbox payload contains the destination and exact email fields. It is
encrypted with AES-256-GCM before commit. Runtime configuration provides an
active key ID and a bounded current-plus-previous key ring. Each row stores its
key ID. This allows a key rotation to retain decryption only through the 24-hour
maximum job lifetime. Plaintext email bodies and bearer tokens never exist in
PostgreSQL.

Replacing a registration or reset token cancels its unsent jobs in the same
transaction. Before delivery, the worker recomputes the token digest from the
decrypted payload and confirms that the current, unexpired token row still
matches. A stale email is never sent after replacement.

## Password policy and hashing

Passwords use these exact input rules:

- Reject more than 1,024 UTF-8 bytes before normalization.
- Apply Unicode Normalization Form C consistently.
- Require 15 through 128 Unicode code points after normalization.
- Preserve spaces and case. Do not trim, case-fold, or truncate.
- Allow Unicode, paste, autofill, and password managers.
- Do not impose character-class rules, password hints, security questions,
  periodic expiry, or reuse questions.
- Password confirmation is a client-side editing guard. The API receives one
  password value and never stores a confirmation value.

Every new password is checked against a versioned, license-compatible bundled
common-password blocklist with pinned source provenance and checksum, then the
Have I Been Pwned Pwned Passwords range API. The request sends only the first
five characters of the SHA-1 digest and requests padded results. The full
password and full digest never leave the process. A bounded prefix cache stores
public range results. Registration, add, change, and reset fail closed with
`503 authentication_unavailable` when the breach service is needed but
unavailable. Existing password and provider login continue to work.

Credentials use Argon2id with this initial policy:

- memory: 64 MiB;
- iterations: 3;
- parallelism: 1;
- random salt: 16 bytes;
- result: 32 bytes;
- encoded format: version, algorithm version, parameters, salt, and result.

Successful login rehashes an older valid encoding after authentication. The
release gate benchmarks the policy on the deployment CPU. It may lower cost only
through a reviewed authority change and never below the OWASP minimum of
Argon2id with 19 MiB, 2 iterations, and parallelism 1.

Each process admits at most two hash operations and queues at most 16. Unknown
emails and users without a credential perform the same admitted dummy Argon2id
verification as a wrong password. Queue overflow returns the same closed `503`
response for every account state.

## Registration and verification

The registration page collects name, email, password, and a local confirmation.
The browser submits only name, email, and password.

The server performs request validation, password policy checks, breach checks,
and Argon2id hashing before it looks up email ownership. This keeps public work
and dependency failures independent of account existence.

For an unowned email, one transaction replaces any pending registration, stores
the password hash and token digest, and commits an encrypted verification email
job. It creates no user or session. For an owned email, the response is the same
generic `202` but no pending credential or email job is created. A provider-only
user must sign in through the provider and add a password in settings.

The email link is `/verify-email#token=<base64url-token>`. URL fragments are not
sent in the HTTP request. The client extracts the token, immediately removes the
fragment with `history.replaceState`, and posts it to the API. The page sends
`Referrer-Policy: no-referrer` and loads no third-party resource.

Verification atomically consumes the pending row and inserts the user and
credential. The existing unique user-email constraint is final authority. If a
provider signup won the race, verification consumes the pending registration,
creates no password, and directs the email owner to sign in through an existing
method. Verification never creates a session; success redirects to login.

## Login and reauthentication

Password login accepts exact JSON containing email and password. The route has
no CORS surface and requires the exact configured `Origin`, with the existing
exact `Referer` fallback. JSON media type, size, duplicate-key, unknown-field,
and scalar rules fail before authentication work.

Unknown email, provider-only account, and wrong password return the same status,
body shape, body length, and cookie behavior. Unknown and provider-only paths
run the dummy hash. A malformed or unsupported stored credential is an internal
integrity failure: it emits a closed `503`, creates no session, and raises a
secret-free operational signal.

Login loads a credential snapshot, verifies it outside a database transaction,
then starts a short transaction. That transaction locks and rechecks the exact
credential and account state before creating the existing opaque session. If a
password change or reset replaced the snapshot, login retries the verification
once from a fresh snapshot or returns the generic failure. An old password can
never create a session after reset commits.

Successful password login creates the same `__Host-session` cookie, server-side
session row, CSRF secret, rotation lineage, 30-day idle expiry, 90-day absolute
expiry, and 15-minute recent-reauthentication state as provider login.

Password reauthentication is an authenticated JSON mutation. It requires the
existing CSRF, Origin, session, and media-type checks, then verifies the current
credential. Its commit locks and rechecks the session and credential before it
updates only the completing session's reauthentication time.

## Add and change password

`PUT /api/v1/me/password` accepts one new password. It requires a live session,
CSRF, exact Origin, and recent reauthentication. A provider-only account adds a
credential. An account with a credential replaces it.

The transaction locks the user, current session, and credential state and
rechecks live-session and recent-reauthentication requirements after the lock. A
successful add or change rotates the current session and revokes every other
session. It commits a non-click security-notification email job with the
credential update. Notification delivery failure does not roll back the
credential change.

Password removal is absent. This avoids introducing last-authenticator and
account-recovery rules in this phase.

## Forgot password and reset

Forgot-password accepts an email and always returns the generic registration
`202`. It performs bounded lookup work. Only an existing account with a password
gets a new reset token and encrypted email job. Provider-only and unknown emails
create neither.

The reset link is `/reset-password#token=<base64url-token>` and follows the same
fragment removal and referrer policy as verification. Reset accepts the token
and one new password.

After rate admission, reset first hashes the token and performs a bounded live
token preflight. An invalid token returns the closed token failure before breach
lookup or Argon2id work. A live token permits password policy, breach, and hash
work. The final transaction locks and rechecks the token, user, credential, and
relevant sessions. It then replaces the credential, consumes the token, revokes
every session, and commits a non-click security-notification job. Reset never
logs in or preserves a session.

A concurrent password change rechecks its session under the same user lock. If
reset committed first, that session is revoked and change fails. If change
committed first, reset follows and becomes the final credential. A login using
the former credential fails its post-hash snapshot check.

Every user-bound password transaction follows one lock order: user, credential,
reset token when applicable, then sessions. Email jobs are inserted only after
the authoritative rows are locked. Verification has no user yet, so it locks the
pending registration and lets the unique user-email insert arbitrate a
concurrent provider signup. Tests pause each boundary to prove no deadlock,
stale session creation, partial credential write, or duplicate account.

## Email delivery

Production uses the AWS SDK for Go v2 SES client in `ap-southeast-1`. The
deployment requires a verified sending domain, production sending access, and a
configuration set that reports delivery, bounce, and complaint signals through
SNS or CloudWatch alarms. AWS credentials use the standard runtime credential
chain and are never stored in repository files.

A bounded worker claims jobs with short leases and `SKIP LOCKED`. It decrypts
only the claimed payload, validates current token authority when applicable,
sends through SES, and clears ciphertext after SES accepts the message. It
retries only classified temporary failures with capped exponential backoff and
injected jitter. Permanent failure, expiry, or the eighth failed attempt marks
the job terminal and clears ciphertext. Logs carry job ID, kind, attempt, and a
closed outcome only. An ambiguous SES result may cause a duplicate email, but
cannot duplicate a token or account mutation; every token remains single-use.

Native development starts a loopback-only mail-capture command through
`make dev-native`. It retains a bounded number of messages in memory, exposes
their links on a local page, resets on restart, and never initializes AWS.
Automated tests inject an in-memory sender. Real SES and DNS changes are outside
local implementation authority and require explicit owner approval after UAT.

## HTTP contract

The OpenAPI source and generated client add:

| Method and path                       | Purpose                                         |
| ------------------------------------- | ----------------------------------------------- |
| `POST /api/v1/auth/password/register` | Validate and queue email verification           |
| `POST /api/v1/auth/password/verify`   | Consume verification token and create account   |
| `POST /api/v1/auth/password/login`    | Create an opaque session                        |
| `POST /api/v1/auth/password/forgot`   | Queue password reset when eligible              |
| `POST /api/v1/auth/password/reset`    | Consume reset token and replace credential      |
| `POST /api/v1/auth/password/reauth`   | Refresh recent reauthentication for one session |
| `PUT /api/v1/me/password`             | Add or replace the current account credential   |

`GET /api/v1/me` adds the non-null Boolean `hasPassword`. Provider identities
remain provider and subject based; provider emails are not exposed through the
linked-identity response.

The public error vocabulary is closed:

| Status and code                  | Meaning                                                                           |
| -------------------------------- | --------------------------------------------------------------------------------- |
| `202` generic accepted response  | Registration or forgot-password request processed without revealing account state |
| `400 request_invalid`            | Invalid JSON, envelope, field type, token shape, or forbidden field               |
| `400 credential_token_invalid`   | Well-shaped token is absent, expired, consumed, or replaced                       |
| `401 authentication_failed`      | Unknown email, no credential, or wrong password                                   |
| `401 reauth_failed`              | Password reauthentication failed                                                  |
| `403 reauth_required`            | Existing recent-reauthentication window is not satisfied                          |
| `413 body_too_large`             | Request exceeded its route budget                                                 |
| `415 media_type_unsupported`     | Exact JSON media type was absent                                                  |
| `422 password_invalid`           | Closed password-policy issue such as length, common, or breached                  |
| `429 rate_limited`               | Bounded limiter rejected the attempt; includes exact `Retry-After`                |
| `503 authentication_unavailable` | Required hash, breach, database, encryption, or queue dependency unavailable      |

Registration and forgot-password return a fixed JSON `202`. Login, verification,
reset, reauthentication, and password add/change succeed with `204`; login and
password change may also set the required session cookie. Malformed token shape
is `request_invalid`; every well-shaped absent, expired, consumed, or replaced
token is `credential_token_invalid`. Responses never include an email, provider
name, token state, hash parameter, or dependency detail.

## Rate limits and resource admission

Password routes use ADR 0018's single-node bounded limiter and the existing
canonical client address from Caddy. Active buckets are never evicted. Unknown
keys share the bounded overflow bucket when capacity is full.

- Login: 30 requests per minute per IP. Track 10 failed attempts per 15 minutes
  per normalized-email HMAC. A request still performs admitted verification so a
  correct password can succeed; a wrong password receives `429` after the
  failure budget is exhausted. Success clears that email failure bucket.
- Registration and forgot-password: 5 per hour per normalized-email HMAC and 20
  per hour per IP.
- Verification and reset token consumption: 10 per hour per IP.
- Password add, change, and reauthentication: 10 per hour per `(account, IP)`.
- Every route also remains subject to the outer 300 requests per minute per IP.

Limiter keys never contain raw email in logs or metrics. The server derives the
email key with a dedicated keyed HMAC and a runtime secret. There is no
permanent account lockout. A future multi-node deployment requires the
distributed rate-limit decision already called out by ADR 0018.

## Web experience

The existing login page adds an email/password form and retains the Google,
GitHub, and LinkedIn actions. New fixed roots are registered before their Nuxt
routes land:

- `/register`;
- `/verify-email`;
- `/forgot-password`;
- `/reset-password`.

Account settings show whether a password is set and allow add or change after
recent reauthentication. Linked providers remain independently visible. The UI
does not show provider-reported email and does not offer account-email change,
password removal, automatic merge, or provider transfer.

Forms support password managers, paste, keyboard operation, explicit labels,
safe issue summaries, light and dark themes, and the existing Nova/Zinc/Emerald
application tokens. Client checks improve feedback but never replace server
validation.

## Security and privacy

- Passwords, bearer tokens, decrypted email payloads, account emails, provider
  claims, session values, and hashes never enter logs, traces, metrics labels,
  errors, panic text, or analytics.
- Logs use fixed event names, request ID, method, closed outcome, and job ID.
- Token comparison is constant-time after exact shape decoding.
- Every mutation has exact media type, body ceiling, strict JSON, Origin, and
  applicable CSRF/session checks before expensive or stateful work.
- Registration and recovery emails contain hard-coded canonical-origin links.
  Request headers never choose the host.
- Verification and reset pages use the existing CSP plus
  `Referrer-Policy: no-referrer` and load no third-party resource.
- Account existence cannot affect public response bytes or cookies. Email and
  storage side effects are private server state; the design does not claim that
  a person controlling the submitted mailbox cannot infer whether mail arrived.
- Security notifications are sent after password add, change, and reset.

## Verification

The implementation plan must assign one author per task and one independent
phase reviewer. Tests include:

- password Unicode, byte and code-point boundaries, encoded-hash parsing,
  parameter upgrades, dummy verification, concurrency admission, and resource
  ceilings;
- bundled blocklist and HIBP prefix/padding/cache/failure behavior without ever
  sending or recording a full digest;
- token entropy, digest storage, constant-time checks, expiry, replacement,
  single use, encrypted outbox payloads, and encryption-key rotation;
- live-database races for login versus reset, reset versus change, duplicate
  verification, provider signup versus verification, duplicate reset, and
  provider-subject collision;
- route matrices for JSON type, body size, duplicate and unknown fields, Origin,
  CSRF, session, rate limits, hash admission, no-write rejection, cookies, and
  byte-identical enumeration responses;
- SES temporary/permanent errors, leases, bounded retries, expiry, ciphertext
  clearing, cancellation, and secret-free errors and logs;
- one Playwright process through the HTTPS authentication overlay: register,
  inspect local mail, verify without auto-login, password login, provider-only
  account password add, different-email provider link, reset, and proof that
  every old session is revoked.

The integration owner runs the unchanged-candidate phase checklist, `make ci`,
and connected `make scan`. A fresh reviewer names the password, token, session,
CSRF, Origin, enumeration, rate-limit, provider-link, email-encryption, SES, and
concurrency invariants in the verdict.

## Rollout and authority changes

Password authentication is its own phase after the current Phase 4 and Phase 5A
work is integrated. It does not share partially edited UI or generated-contract
ownership with those phases.

The implementation plan starts with these serialized owner changes:

1. Update Approved v4 product, security, data, API, deployment, and web design
   text and accept the password-identity ADR.
2. Add password budgets, route registry entries, acceptance rows, OpenAPI, and
   generated client types.
3. Add the migration and regenerate sqlc output.
4. Build and verify the server, mail worker, local capture, and UI in bounded
   task waves.
5. Pass local HTTPS UAT before requesting any AWS or DNS action.
6. Configure the SES verified domain, production sending access, configuration
   set, and alarms only after explicit owner approval.
7. Deploy the migration before application code. Existing users begin with no
   credential and continue to sign in through linked providers.

No existing provider route, identity, or session is migrated. The feature is
additive and does not make a provider-only account less usable.

## References

- [NIST SP 800-63B, Authentication and Authenticator Management](https://pages.nist.gov/800-63-4/sp800-63b.html)
- [RFC 9106, Argon2 Memory-Hard Function](https://www.rfc-editor.org/rfc/rfc9106.html)
- [OWASP Password Storage Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html)
- [OWASP Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)
- [OWASP Forgot Password Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Forgot_Password_Cheat_Sheet.html)
- [Have I Been Pwned API v3](https://haveibeenpwned.com/API/V3)
- [OpenID Connect Core, Claim Stability and Uniqueness](https://openid.net/specs/openid-connect-core-1_0-final.html#ClaimStability)
- [Google OpenID Connect](https://developers.google.com/identity/openid-connect/openid-connect)
- [LinkedIn OpenID Connect](https://learn.microsoft.com/en-us/linkedin/consumer/integrations/self-serve/sign-in-with-linkedin-v2)
- [GitHub REST API: Email addresses](https://docs.github.com/en/rest/users/emails)
- [Amazon SES verified identities](https://docs.aws.amazon.com/ses/latest/dg/verify-addresses-and-domains.html)
- [Amazon SES regions](https://docs.aws.amazon.com/ses/latest/dg/regions.html)
- [Amazon SES sending monitoring](https://docs.aws.amazon.com/ses/latest/dg/monitor-sending-activity.html)
