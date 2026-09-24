# Second-factor authentication

An account may add passkeys, an authenticator app, or both as an optional second
factor. A password or linked provider stays the required primary factor. A
passkey never replaces a password, and the service offers no usernameless login.
[ADR 0048](../adr/0048-passkey-second-factor-authentication.md) and
[ADR 0049](../adr/0049-totp-second-factor-authentication.md) record the choices
and their residual risk.

This page holds the rules shared by every factor. Exact wire, storage, and mail
shapes live in:

- the [passkey contract](passkey-second-factor-contract.md): primary and pending
  responses, WebAuthn JSON, recovery download, shared mail events, and passkey
  storage;
- the [authenticator-app contract](totp-second-factor-contract.md): TOTP routes,
  failure budget, and storage;
- [TOTP key management](totp-key-management.md): sealing, key ring, rotation,
  and key failures;
- the [release fence](passkey-release-fence.md): the production minimum release;
- [Numeric budgets](budgets.md): every count, lifetime, rate, and size.

## Assurance boundary

An active factor is an active passkey or an active TOTP credential. An account
has second-factor enforcement while it holds at least one active factor of any
type. Enforcement means:

1. Primary authentication creates a pending authentication, not a session.
2. Only a valid active factor or recovery code completes it and issues a
   session.
3. A sensitive action requires primary and factor proof inside the same
   15-minute recent-reauthentication window.
4. Cookie sessions and connected-agent grants must carry the account's current
   authentication epoch. A stale epoch authenticates nothing.

`users.auth_epoch` only increases. A fully authenticated session and an OAuth
grant copy it when issued. Cookie and bearer authentication compare the copy
with the account on every request. Authorization codes carry the exact grant ID
and epoch, and exchange requires that live grant and the current epoch.

Factor enrollment, replacement, removal, and recovery-code regeneration advance
the epoch in the same transaction as the change. The change revokes every live
connected-agent grant and token family and every browser session except the
current one. It returns a replacement session cookie at the new epoch: new token
and CSRF secrets, no rotation lineage, and the prior creation time, absolute
expiry, device metadata, and primary-proof time. Activity time becomes the
mutation time. Factor-proof time is:

- the completion time after first-factor enrollment;
- unchanged after a change that leaves enforcement on;
- null after final-factor removal.

With no current session, the change revokes every session and issues none.
Ordinary 24-hour rotation ([ADR 0015](../adr/0015-session-rotation-delivery.md))
copies the epoch and factor-proof time unchanged.

Every session, grant, and authorization-code issuer takes the user-row lock
before reading the epoch. A cookie-authenticated sensitive mutation carries its
session ID into the transaction, locks the user, then locks that session and
rechecks liveness, ownership, epoch, and proof times before writing. The global
lock order is: OAuth client when applicable, user, session, factor policy,
credential, TOTP enrollment, pending authentication or ceremony, OAuth grant,
authorization code, token, then mail outbox.

Regular routes accept a live current-epoch session whatever the age of its
factor proof. Sensitive actions keep the 15-minute window and, on an enrolled
account, require both proof times: provider link and unlink, password add or
change, factor management, recovery-code regeneration, account deletion, slug
release, session revocation, logout everywhere, and approving or silently
reusing an agent grant. Denying an agent grant needs no recent proof.

## Pending authentication

Password and provider login share one boundary. After a valid primary
credential, the server locks the user and reads the factor policy. An unenrolled
account gets the ordinary session. An enrolled account gets a pending
authentication instead.

The browser holds a 256-bit `__Host-auth-pending` token
(`Secure; HttpOnly; SameSite=Strict; Path=/`, no `Domain`). PostgreSQL holds its
SHA-256 digest and a separate 256-bit CSRF secret. The row binds account,
purpose, epoch, primary-proof time, the bound session for reauthentication, a
validated return path, creation, a five-minute expiry, and a failure count. It
authorizes nothing but the pending routes: not `/me`, resumes, consent, or MCP.

Purposes are `login` and `reauth`. Login completion issues a fresh session with
both proof times set to the completion time. Reauth completion updates only the
bound live session, after checking its account and epoch, and sets both proof
times. No primary reauthentication updates a session before factor completion.

An account holds at most five live pending rows; a sixth expires the oldest
under the user lock. Each row allows five failed completions across all methods.
Starting a passkey ceremony is not a failure. Exhaustion consumes the row,
clears the cookie, and forces primary authentication again. Factor attempts also
use per-`(account, client IP)` and per-IP limits on the bounded limiter of
[ADR 0018](../adr/0018-bounded-rate-limiter.md).

Missing, expired, consumed, wrong-epoch, wrong-session, and foreign pending
credentials all return the same `401 authentication_required`. A failed factor
returns `401 verification_failed` without naming the account, credential, or
method.

An older web client never receives a session after primary login, so it cannot
cross the pending boundary; at worst it shows an error until reloaded. A method
value the page does not know shows a refresh prompt, calls no route, and grants
nothing.

## Passkeys

Passkeys use Web Authentication (WebAuthn) as an account-bound second factor.
The relying-party (RP) ID is the host of the canonical `PUBLIC_ORIGIN`:
`aboutme.vn` in production and `localhost` in the HTTPS development harness.
Registration and assertion require the exact origin, ceremony type, SHA-256 RP
ID hash, user presence, and user verification. Both reject `crossOrigin: true`
and any `topOrigin`, because aboutme never embeds a ceremony.

Startup rejects an enabled passkey-enrollment flag when the host is an IP
literal or not a valid RP ID. Changing the canonical host makes every passkey
unusable, so a self-hosted operator must keep the host or make sure users hold
TOTP or recovery codes first.

The policy admits platform, roaming, and synced passkeys. Verification stays on
the authenticator; the server receives no biometric data. The user-verification
flag is required. The server requests `attestation: "none"` and no extensions,
and extension output never changes authentication. Transport hints are untrusted
display data.

Each account has one random 32-byte WebAuthn user handle on its factor policy,
with no personal data. Before a policy exists, registration options store a
proposed handle on the ceremony. Completion locks the user and claims the
ceremony; if the policy is still absent, one transaction creates the policy with
that handle, the first credential, and the recovery codes, then advances the
epoch. A competing first-enrollment ceremony keeps the old epoch and fails.

An assertion lists every active credential of the pending account. The returned
credential must belong to that account, and a returned user handle must equal
the stored one. Signature counters follow WebAuthn: zero may stay zero,
otherwise the new value must be greater. Completion locks the user and
credential and advances the counter in the same transaction as ceremony and
pending consumption. A non-increasing value rejects the assertion and records a
bounded security event.

A ceremony lasts five minutes. A new ceremony consumes the prior live one for
the same binding and purpose. Replay, wrong binding, and concurrent completion
fail closed.

## TOTP

Time-based one-time passwords (TOTP) use the RFC 6238 profile that common
authenticator apps support: HMAC-SHA-1, a 20-byte secret, six digits, a
30-second period, and `T0=0`. Verification accepts the previous, current, or
next step, and only a step greater than the stored `last_used_step`. The
credential lock makes each step single-use across login, reauthentication, and
concurrent requests. Enrollment stores its proof step, so the setup code cannot
also complete a login.

An account holds at most one active TOTP credential and one live enrollment.
Starting setup supersedes an earlier incomplete one. Replacement leaves the
active credential in force until the new secret proves a code. The settings UI
states that TOTP is not phishing-resistant and recommends a passkey when the
browser supports one.

The server must recover TOTP secrets to verify codes, so it stores them sealed
with AES-256-GCM under a runtime key ring. A key failure disables TOTP only:
passkeys, recovery codes, accounts without TOTP, and `/readyz` keep working, and
no failure accepts an unverified code.

## Recovery codes

The first completed factor enrollment creates ten recovery codes. Each holds 128
random bits as 26 uppercase Crockford Base32 characters with an `amr_` prefix
and display-only hyphen groups. Input accepts ASCII hyphens and spaces,
canonicalizes the rest, and rejects any other shape before database work.

Codes appear once, in a `Cache-Control: no-store, no-transform` response. They
never enter a URL, cookie, browser storage, analytics, log, trace, metric,
email, or account export. PostgreSQL stores only domain-separated SHA-256
digests.

Verification locks the user, deletes exactly one matching unused digest, and
completes the pending authentication in one transaction; concurrent use has one
winner. Using a code changes no factor. The security email gives the remaining
count and never a code.

Regeneration needs recent primary proof plus recent factor or recovery proof. It
replaces every digest, advances the epoch, revokes other sessions and grants,
rotates the current session, and returns the new set once.

Recovery codes are the only lost-factor path. Password reset, provider login,
linking, and relinking all keep enforcement. There is no operator or support
override ([ADR 0028](../adr/0028-no-operator-surface.md)). Losing every factor
and every recovery code is permanent account loss.

## Enrollment and management

Factor management is a cookie-only account surface. Every mutation requires a
live current-epoch session, the synchronizer CSRF token, exact Origin, strict
JSON, and recent reauthentication. The first factor needs recent primary proof.
Once enforcement exists, it needs recent primary proof and recent proof from an
active factor or recovery code. The credential being replaced or removed may
supply that factor proof, so a deliberate final removal needs no bypass.

Passkey and TOTP enrollment each prove the new credential before storing it.
Adding a passkey or TOTP to an enrolled account keeps the recovery codes.

The factor policy exists exactly while an active factor exists. First enrollment
creates the policy, first credential, and recovery codes in one transaction.
Every removal decides final versus non-final from one count of active factors of
every type, taken under the user lock; no handler counts one type alone.
Removing a passkey while TOTP remains, or TOTP while a passkey remains, is not
final. Final removal deletes the policy and every recovery digest in the same
transaction. Enrollment decides first versus later from policy existence, not
from its own factor type.

`GET /api/v1/me/second-factor` returns `enabled`, passkey IDs and times,
`totpEnabled`, and `recoveryCodesRemaining`, all from one snapshot. It never
returns credential material or a code.

`PASSKEY_ENROLLMENT_ENABLED` and `TOTP_ENROLLMENT_ENABLED` default to false and
gate only enrollment start and completion. Completion rechecks its flag; when
off, it consumes the pending enrollment state, stores nothing, and returns the
uniform `404 not_found`. Verification, removal, recovery, regeneration, and
state reads ignore both flags.

## Security notifications

The encrypted mail outbox sends bilingual mail for first enrollment, factor
addition, replacement, and removal, final disablement, recovery-code
regeneration and use, and attempt exhaustion. Each names the action and UTC
time, and advises a password change and session revocation. It contains no
factor material, state-changing link, IP, or account identifier.

The mutation and its outbox insert commit together; a failed insert aborts the
mutation. Attempt-exhaustion mail is capped per account, but the cap never
suppresses the state change.

## Web integration

`/login/second-factor` offers the pending account's methods. It handles missing
WebAuthn support, cancellation, expiry, exhaustion, and the return path without
treating the pending cookie as a session. Recovery works without WebAuthn.

Security controls live on `/app/settings/sessions` beside password, identity,
session, and connected-agent controls. The page lists passkeys by creation
order, shows TOTP state and the remaining recovery-code count, and offers add,
replace, remove, regenerate, copy, and download. One-time recovery codes and
TOTP secrets live only in component state and leave it when their dialog closes.

Every string, accessible name, and email has Vietnamese and English copy. Brand
names, protocol identifiers, codes, and user values stay unchanged. Language
never changes factor state or provisioning data.

## Logging and storage

Credential, challenge, pending, and recovery state lives in PostgreSQL, so a
restart cannot reopen a replay. Every factor table cascades from its account;
account deletion removes credentials, ciphertext, challenges, and digests, and
portable export excludes them. Logs use closed outcomes and internal row IDs.
They never contain an email, credential ID, public key, client or authenticator
data, signature, challenge, TOTP material, recovery code, pending token, or CSRF
secret.

## Release floor and rollback

The server must understand every stored factor type. Production therefore keeps
a durable minimum release: v0.4.2 (numeric 4002) before passkey enrollment and
v0.4.7 (numeric 4007) before TOTP enrollment. A v0.4.2 server counts only
passkeys, so it could delete the policy and recovery codes of an account that
still holds TOTP. The [release fence](passkey-release-fence.md) enforces the
floor on every supported deploy, rollback, and restoration.

Turning an enrollment flag off stops new enrollment only. It never lowers the
floor or disables verification, removal, state reads, or recovery. Rollback
below the floor is a forward fix instead.
