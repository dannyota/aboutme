# Second-factor authentication

Status: Approved for v0.4.2; proposed for v0.4.3.

Accounts may add a passkey or an authenticator-app code as an optional second
factor. A primary password or linked provider remains required. A passkey is not
a password replacement, and the service does not offer usernameless login.

Passkeys ship in v0.4.2 and time-based one-time passwords (TOTP) in v0.4.3. Both
cover enrollment, sign-in, step-up, recovery, notifications, and both locales.

The owner selected optional passkeys, TOTP, local user verification, and
single-use recovery codes, and rejected password, provider, email, support, or
operator reset paths. That choice accepts a residual risk: an attacker with a
primary credential or recent session can enroll the first factor and keep its
recovery codes. Notification detects the event but cannot restore access.

## Assurance boundary

An active factor is an active passkey or an active TOTP credential. An account
has second-factor enforcement while it has at least one active factor of any
type. The final active factor is the last one across all types, so removing a
passkey while TOTP remains, or TOTP while a passkey remains, is never final.
Enforcement has four rules:

1. Primary authentication creates a pending authentication, not a session, for
   an enrolled account.
2. Only a valid active factor or recovery code completes the pending
   authentication and creates a session.
3. A sensitive action on an enrolled account requires primary and second-factor
   verification in the same 15-minute recent-reauthentication window.
4. Cookie sessions and connected-agent grants must carry the account's current
   authentication epoch. A stale epoch authenticates nothing.

The account row owns a monotonically increasing `auth_epoch`. A fully
authenticated session and an OAuth grant copy the current value when issued.
Cookie and bearer authentication compare the copied value with the account on
every request. Factor enrollment, replacement, removal, recovery-code
regeneration, and disabling the final factor increment the epoch in the same
transaction as the change.

Every epoch change revokes all live connected-agent grants and their token
families. Authorization codes carry the exact grant ID and authentication epoch;
exchange requires that same live grant and current account epoch. The change
revokes every browser session except the current session used for the deliberate
change, then returns a replacement session cookie at the new epoch. The row has
new token and CSRF secrets and no rotation lineage. It copies the prior creation
time, absolute expiry, device metadata, and primary-proof time, so the mutation
does not extend either window. Activity time advances to the mutation time.

First-factor enrollment sets the replacement factor-proof time to its verified
completion time. A change that leaves enforcement enabled copies the prior
factor-proof time. Final-factor removal sets it null. If there is no current
session, the change revokes all sessions and issues none.

Ordinary 24-hour session rotation copies `auth_epoch` and the nullable
factor-proof time unchanged. ADR 0015 still requires it to copy primary proof,
absolute expiry, and device metadata unchanged.

All session, grant, and authorization-code issuers take the existing user-row
lock before reading the epoch and writing authority. A cookie-authenticated
sensitive mutation carries the concrete caller session ID into its transaction.
After locking the user, it locks that session and rechecks liveness, ownership,
epoch, and required verification times before writing. Factor mutations take the
same locks before factor, grant, token, or notification rows. The lock order is
OAuth client when applicable, user, session, factor policy, credential, TOTP
enrollment, pending authentication or ceremony, OAuth grant, authorization code,
token, then email outbox.

Regular authenticated routes accept a live current-epoch session regardless of
the age of its factor proof. Existing sensitive actions keep the 15-minute
window and require both timestamps for an enrolled account. These actions
include provider link and unlink, password add or change, factor management,
recovery-code regeneration, account deletion, slug release, session revocation,
logout everywhere, and connected-agent consent. Denying an agent grant does not
need recent reauthentication; approving or silently reusing one does.

## Pending authentication

Password and provider login both enforce the same boundary. After a valid
primary credential, the server locks the user and reads the factor policy. An
unenrolled account receives the existing fully authenticated session. An
enrolled account receives a pending authentication instead.

The browser holds a 256-bit random `__Host-auth-pending` token. PostgreSQL holds
only its SHA-256 digest and a separate 256-bit CSRF secret. The cookie is
`Secure; HttpOnly; SameSite=Strict; Path=/` with no `Domain`. A pending row
binds one account, purpose, authentication epoch, primary verification time,
current session when the purpose is reauthentication, bounded return path,
creation time, five-minute expiry, and attempt count. It does not authorize
`/me`, a resume route, consent, MCP, or any other session route.

Pending purposes are `login` and `reauth`. Login completion issues a fresh
session with both verification timestamps set to the completion time. Reauth
completion updates only the bound live session after confirming its account and
epoch. It sets both verification timestamps to the completion time. No primary
reauthentication endpoint updates the session before second-factor completion.

At most five pending authentications may be live for one account. Creating a
sixth expires the oldest under the user lock. Each pending authentication
permits five failed passkey, TOTP, or recovery completions in total. Starting a
passkey ceremony does not count as a failure. Exhaustion consumes the row,
clears the cookie, and requires primary authentication again. Factor attempts
are also limited to 10 per 15 minutes per `(account, client IP)` and 30 per
minute per client IP. Every limiter uses the bounded policy from
[ADR 0018](../adr/0018-bounded-rate-limiter.md).

Password login returns the existing `204` for an unenrolled account. It returns
`202` with `{data:{secondFactorRequired:true}}` and the pending cookie for an
enrolled account. A provider callback for an enrolled account sets the pending
cookie and redirects to `/login/second-factor`. Neither path issues a session.

`GET /api/v1/auth/second-factor` reads only the pending cookie. It returns the
available methods, expiry, validated return path, and pending CSRF token. Every
pending mutation requires that token, exact Origin, JSON content type, and the
pending cookie. Missing, expired, consumed, wrong-epoch, wrong-session, and
foreign pending credentials produce the same `401 authentication_required`.

Successful verification consumes the pending row atomically, clears its cookie,
and returns `204`. Verification failure returns `401 verification_failed`
without naming the account, credential, or failed method. A rate rejection uses
the existing `429` envelope and `Retry-After`.

Older web clients remain safe. They receive no session after primary login and
cannot cross the pending boundary. They may show a generic error or return to
login until upgraded. Unenrolled accounts keep the old response and behavior.
The additive routes stay under API v1 because only an account that opted into
the new requirement sees the new login outcome.

## Passkeys

Passkeys use Web Authentication (WebAuthn) as an account-bound second factor.
The relying party uses the host from the canonical `PUBLIC_ORIGIN` as its exact
RP ID. Production therefore uses `aboutme.vn`; the HTTPS development harness
uses `localhost`. Registration and assertion verification require the exact
configured origin, expected ceremony type, SHA-256 RP ID hash, user presence,
and user-verification flag. Both reject `crossOrigin: true` and any `topOrigin`
because aboutme does not embed WebAuthn ceremonies.

Passkey enrollment requires a stable HTTPS domain host or `localhost`. An IP
literal or host that is not a valid WebAuthn RP ID makes startup reject an
enabled passkey-enrollment flag. Changing the canonical host makes existing
passkeys unusable, so a self-hosted operator must preserve the host or ensure
users have TOTP or recovery codes before a planned origin change.

Registration uses a 32-byte random challenge, a stable random 32-byte user
handle with no personal data, required user verification and resident key, no
authenticator-attachment restriction, `attestation: "none"`, ES256 then RS256, a
five-minute timeout and expiry, and every active credential in
`excludeCredentials`. The
[passkey contract](passkey-second-factor-contract.md#webauthn-json) fixes the
exact options.

The policy admits platform, roaming, and synced passkeys. Local verification
stays inside the authenticator; the server receives no biometric data. The
user-verification flag is required, so touch alone is insufficient. Attestation
is `none` because aboutme has no device or vendor policy.

The server stores credential ID, COSE public key, user ID, sign counter, backup
eligibility and state, known transport hints, creation time, and last-use time.
Credential ID is globally unique and 16 to 1,023 bytes. A public key is at most
2,048 bytes. An account may hold five passkeys. Transport hints are untrusted
display and browser-routing hints and never change authorization.

An assertion ceremony supplies all active account credential IDs through
`allowCredentials` and again sets `userVerification: "required"`. The returned
credential must belong to the pending account. A returned user handle, when
present, must equal that account's stored handle. The server verifies the exact
challenge, type, origin, RP ID hash, presence and verification flags, public-key
signature, and credential binding. The server requests no WebAuthn extensions;
unrequested client extension output never changes authentication.

The stored signature counter follows the WebAuthn rule. A zero counter may stay
zero. If either value is nonzero, the returned value must be greater. Assertion
completion locks the user and credential before comparing and advancing the
counter in the same transaction as ceremony and pending consumption. A
non-increasing value rejects the assertion and records a bounded security event.

`webauthn_ceremonies` stores a random public ID, challenge digest, account,
purpose, session or pending binding, epoch, timestamps, and a nullable proposed
user handle. Before a policy exists, registration options store their new
32-byte handle in that ceremony and send it to WebAuthn. Completion locks the
user and claims the ceremony. If the policy is still absent, one transaction
uses that handle to create the policy, first credential, and recovery codes,
then advances the epoch. Competing first-enrollment ceremonies retain the old
epoch and fail. A ceremony lasts five minutes; a new one consumes the prior live
ceremony for the same binding and purpose. Replay, wrong binding, and concurrent
completion fail closed. Cleanup removes at most 200 expired rows per run.

## TOTP

TOTP uses the interoperable RFC 6238 profile: HMAC-SHA-1, a 20-byte random
secret, six decimal digits, a 30-second period, and `T0=0`. Enrollment returns
an `otpauth://totp/` URI whose issuer is the canonical origin host and whose
label holds the account email, plus the secret as grouped Base32 text. The web
app renders the QR code locally.

Verification tests the previous, current, and next step in constant time,
chooses the greatest match, and accepts it only when it is greater than the
stored `last_used_step`. The credential lock makes one code single-use across
login, reauth, and concurrent requests. Enrollment stores its proof step. The
server never logs a code, secret, candidate, or matched step.
[RFC 6238](https://www.rfc-editor.org/rfc/rfc6238.html#section-5.2) recommends
this step and skew. Single-use steps, per-row attempts, and a per-account
failure budget bound guessing. Secrets follow the 160-bit
[RFC 4226 recommendation](https://www.rfc-editor.org/rfc/rfc4226.html#section-4).

An account holds at most one active TOTP credential and one live enrollment.
Starting an enrollment consumes the prior enrollment under the user lock. A
replacement does not alter the active credential until the new secret proves one
valid code. Successful replacement atomically installs the new secret and
advances the epoch. An incomplete enrollment expires after ten minutes and has
no effect on enforcement.

TOTP secrets must remain recoverable, so PostgreSQL stores AES-256-GCM
ciphertext bound to its account, row, record kind, format, and key identifier.
Runtime holds one active 32-byte key and at most one previous key. Each key
identifier is derived from its key value, so a stored identifier names exactly
one key. New writes use the active key. Successful verification lazily
re-encrypts a previous-key row, and a bounded command re-encrypts the rest
before the previous key is removed.

A missing or malformed key ring fails startup. A stored identifier outside the
ring or an authenticated-decryption failure makes TOTP unavailable, not the
service. TOTP work that needs a secret fails closed with
`503 authentication_unavailable`. `/readyz`, passkeys, recovery codes, and
accounts without TOTP are unaffected. The server emits a fixed secret-free log
signal that a production alarm watches. Key values come from the runtime secret
path and never enter source, state output, logs, metrics, documentation, or
command output. No failure accepts a code without verification. The
[authenticator-app contract](totp-second-factor-contract.md) and
[key-management design](totp-key-management.md) fix the exact TOTP rules.

## Recovery codes

The first completed factor enrollment creates ten recovery codes. Each code
contains 128 random bits encoded as 26 uppercase Crockford Base32 characters,
with an `amr_` prefix and display-only hyphen groups. Input accepts ASCII
hyphens and spaces, canonicalizes the remaining characters, and rejects every
other shape before database work.

The response displays the codes once. It uses
`Cache-Control: no-store, no-transform` and never puts a code in a URL, cookie,
browser storage, analytics, log, trace, metric, email, or account export.
PostgreSQL stores only domain-separated SHA-256 digests because 128 random bits
make offline guessing infeasible.

Verification locks the user, deletes exactly one matching unused digest, and
completes the pending authentication in one transaction. Concurrent use has one
winner. The security email states that a recovery code was used and gives the
remaining count, but never contains a code. Using a code does not disable or
replace any factor.

Regeneration requires recent primary plus active-factor or recovery-code
verification. It atomically replaces every digest, advances the epoch, revokes
other sessions and grants, rotates the current session, and returns the new set
once. Removing the final active factor deletes all remaining recovery digests.

Recovery codes are the only lost-factor recovery path. Password reset revokes
sessions but preserves factor enforcement. Provider login, linking, and
relinking also preserve it. The service has no operator account and no support
override. A user who loses every factor and every recovery code cannot regain
access through the application.

The
[OWASP MFA guidance](https://cheatsheetseries.owasp.org/cheatsheets/Multifactor_Authentication_Cheat_Sheet.html)
requires recovery not to become a weaker bypass and factor-change notification.

## Enrollment and management

Factor management is a cookie-only account surface. Every mutation requires a
live current-epoch session, synchronizer CSRF token, exact Origin, strict JSON,
and recent reauthentication. The first factor requires recent primary
reauthentication. Once enforcement exists, both primary and an active factor or
recovery code must be recent.

A passkey enrollment has separate options and completion calls. Completion
proves the new credential before it is stored. A TOTP enrollment has separate
start and completion calls. Start returns the secret once; completion proves a
code before the credential becomes active. Adding another passkey or adding TOTP
preserves existing recovery codes. Removing one credential preserves enforcement
while another active credential remains. Removing the final credential disables
enforcement and deletes the recovery-code set.

The factor-policy row and the first credential plus recovery-code set are
created in one transaction. The policy row cannot exist without an active
factor. Removing the final active factor deletes the policy row in the same
transaction. Every removal path decides final versus non-final from one shared
count of active factors of every type, taken under the user lock. Enrollment
decides first versus later from policy existence, not from its own factor type.
The passkey release implements both rules, so a later factor type joins the
count without changing a passkey route.

The account may use the credential being removed to satisfy the preceding recent
factor proof. This permits deliberate final-factor removal without an email or
operator bypass. The 15-minute window, current session binding, CSRF, exact
Origin, epoch change, revocation, session rotation, and notification form the
removal boundary.

The passkey release's `GET /api/v1/me/second-factor` returns `enabled`, passkey
IDs and timestamps, and `recoveryCodesRemaining`. The TOTP release adds
`totpEnabled`. Neither returns credential material or a recovery code.

The API additions are:

| Method   | Path                                         | Purpose                                                   |
| -------- | -------------------------------------------- | --------------------------------------------------------- |
| `GET`    | `/api/v1/auth/second-factor`                 | Read pending methods, expiry, return path, and CSRF token |
| `POST`   | `/api/v1/auth/second-factor/passkey/options` | Start a pending assertion ceremony                        |
| `POST`   | `/api/v1/auth/second-factor/passkey/verify`  | Complete pending authentication with an assertion         |
| `POST`   | `/api/v1/auth/second-factor/totp/verify`     | Complete pending authentication with a TOTP code          |
| `POST`   | `/api/v1/auth/second-factor/recovery/verify` | Complete pending authentication with a recovery code      |
| `GET`    | `/api/v1/me/second-factor`                   | Read account factor state                                 |
| `POST`   | `/api/v1/me/second-factor/passkeys/options`  | Start passkey registration                                |
| `POST`   | `/api/v1/me/second-factor/passkeys`          | Verify and store a passkey                                |
| `DELETE` | `/api/v1/me/second-factor/passkeys/{id}`     | Remove one owned passkey                                  |
| `POST`   | `/api/v1/me/second-factor/totp/enrollment`   | Start TOTP enrollment or replacement                      |
| `PUT`    | `/api/v1/me/second-factor/totp/enrollment`   | Verify and complete TOTP enrollment                       |
| `DELETE` | `/api/v1/me/second-factor/totp`              | Remove TOTP                                               |
| `POST`   | `/api/v1/me/second-factor/recovery-codes`    | Replace every recovery code and return the new set once   |

WebAuthn binary fields use unpadded base64url in JSON. An options response
contains a random `ceremonyId` plus a standards-shaped `publicKey` object. A
completion body carries that ID and one credential response. Unknown, malformed,
foreign, expired, consumed, and wrong-purpose ceremony IDs collapse to
`400 challenge_invalid`. Missing or foreign internal passkey IDs use the same
`404 factor_not_found`.

Enrollment start and completion recheck their flag. A disabled completion
consumes its enrollment state, stores no factor, and returns `404`.
Authentication, removal, recovery, and state reads keep working. The passkey
release adds only `passkeyEnrollment`; the TOTP release adds `totpEnrollment`.

## Storage and bounds

The passkey release adds `auth_epoch`, default `0`, to users, sessions, and
OAuth grants, plus nullable `sessions.second_factor_verified_at`. Authorization
codes gain `grant_id` and `auth_epoch` with no default. Both end `NOT NULL`, and
an insert trigger fills either one when an insert omits it. Its new relations
are `second_factor_policies` with user ID, random user handle, and enabled time;
`webauthn_credentials`; `second_factor_recovery_codes`;
`pending_authentications`; and `webauthn_ceremonies`.

The TOTP release adds `totp_credentials` and `totp_enrollments`. Every relation
uses an account foreign key with `ON DELETE CASCADE`. Account deletion therefore
removes credentials, ciphertext, challenges, and digests. Security credentials
are excluded from portable account export. TOTP credentials have an index on
their encryption-key identifier.

Passkey request bodies are at most 32 KiB. `clientDataJSON` is at most 4 KiB,
attestation objects are at most 16 KiB, authenticator data is at most 4 KiB,
signatures are at most 1 KiB, and user handles are at most 64 bytes. TOTP and
recovery request bodies use the existing 4 KiB authentication limit. The server
rejects duplicate JSON fields, unknown fields, invalid UTF-8, padded or
noncanonical base64url, and trailing data before cryptographic work.

The accepting release must add every count, lifetime, rate, body, field, and
cleanup limit in this document to [Budgets](budgets.md) before implementation.
That reviewed table remains the numeric authority used by budget tests.

Credential, challenge, pending, and recovery state stays inside PostgreSQL so a
restart cannot reopen a replay. Cleanup is bounded to 200 expired rows per run.
Logs use account-independent outcomes and internal row IDs where needed. They
never contain email, credential ID, public key, client data, authenticator data,
signature, challenge, TOTP material, recovery material, pending token, or CSRF
secret.

## Security notifications

The encrypted mail outbox sends bilingual notifications for first enrollment,
factor add, replacement, removal or disablement, recovery-code regeneration or
use, and attempt exhaustion. Each names the action and UTC time and advises
password change and session revocation. It contains no factor material,
state-changing link, raw IP, or account identifier.

The mutation and outbox insert commit together, and insertion failure aborts the
mutation. Existing asynchronous delivery rules apply. Recovery-code consumption
therefore always records its notification job.

## Web integration

`/login/second-factor` offers the pending account's methods. It handles missing
WebAuthn support, cancellation, expiry, exhausted attempts, and return paths
without treating the pending cookie as a session. Recovery needs no WebAuthn. A
method value the page does not know shows a refresh prompt, calls no guessed
route, and grants nothing.

Account security controls live on `/app/settings/sessions` beside password,
linked identity, session, and connected-agent controls. The UI lists passkeys by
stable creation order and date, shows TOTP state and remaining recovery-code
count, and provides add, replace, remove, regenerate, copy, and download flows.
One-time recovery and TOTP secrets leave component state when their dialog
closes and never enter persisted client state.

Every visible or accessible string and email has Vietnamese and English copy.
Brand names, protocol identifiers, codes, and user values remain unchanged.
Language never changes factor state or provisioning data.

## Migration, compatibility, and rollback

Both database migrations are additive and forward-only. Existing users,
sessions, and grants receive epoch `0`; no factor policy exists after backfill,
so behavior is unchanged. The release does not change the resume document
schema, renderer, public pages, or MCP tool schema.

The passkey migration deletes outstanding 60-second OAuth authorization codes
before making their new grant and epoch bindings required. A connected agent may
need to restart authorization, but no grant, token, resume, or account data is
lost.

A compatibility insert trigger binds flag-off old-image code inserts to the
unique live grant and current epoch, so pre-enrollment rollback still works.

The server must understand every stored factor before enrollment is enabled.
`PASSKEY_ENROLLMENT_ENABLED` and `TOTP_ENROLLMENT_ENABLED` default to false.
Each release deploys its migration, server, web UI, and mail copy with the flag
off, then proves unenrolled production login and disabled enrollment.

A singleton DynamoDB item holds the durable production minimum and a nonexpiring
operation lock. The lock serializes deploy, rollback, restoration, and
activation, so a lower target cannot act on a read made before a concurrent
fence raise. Supported operations use the current main checkout and dedicated
roles. OpenTofu, old scripts with AWS-login credentials, and AWS administration
remain privileged bypasses that require factor-compatibility proof. The
[release-fence contract](passkey-release-fence.md) fixes the item, IAM, and
failure order.

Enrollment enablement first raises the fence to the capable, healthy release,
then enables the flag. Failure between those writes leaves enrollment disabled
and rollback restricted, which is safe. After enablement, an owner-authorized
synthetic account proves pending isolation, recovery, revocation, and both
locales. Turning a flag off stops new enrollment but never lowers the fence or
disables verification, removal, state reads, or recovery.

A rollback before flag enablement may use the previous image and leave additive
storage in place. After enablement, rollback means a forward fix or an image at
or above the fence. An image that ignores an enrolled factor is invalid.

Tests cover enrollment and recovery races, challenge replay, origin, RP, user
verification, counters, TOTP skew and reuse, primary-login bypass, stale
authority, factor loss, flags, fences, notifications, and both locales.

The passkey release adds shared pending, recovery, state, passkey, and
capability routes. The TOTP release adds its routes and capability and joins the
shared active-factor count without changing any passkey route or stored
credential.
