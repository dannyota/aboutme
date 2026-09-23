# Authenticator-app second-factor contract

Status: Proposed for the authenticator-app release. The owner approved all seven
product choices below on 2026-09-22.

This contract adds time-based one-time password (TOTP) verification to the
v0.4.2 pending, recovery, session, and authentication-epoch boundary without
weakening passkeys. The [key-management design](totp-key-management.md) owns
sealing, the key ring, rotation, re-encryption, and key failures.

## Owner decisions

The owner approved these product choices on 2026-09-22:

1. Approved: Use the broad-compatibility RFC 6238 profile of HMAC-SHA-1, a
   20-byte secret, six digits, a 30-second period, and one step of clock skew.
   TOTP is not phishing-resistant. The settings UI says so and recommends a
   passkey when the browser supports one.
2. Approved: Allow one active TOTP credential per account. Starting replacement
   leaves the current credential active. Successful proof atomically replaces
   it.
3. Approved: Show the setup QR code and grouped Base32 secret once. Use the
   canonical account email in the authenticator label and the canonical origin
   host as issuer. The browser renders the QR code locally.
4. Approved: List pending methods in the fixed order passkey, authenticator
   code, then recovery code. A user may choose any listed method.
5. Approved: Reuse one recovery-code set across passkeys and TOTP. Adding or
   replacing TOTP does not regenerate codes. Removing the final active factor
   deletes the set and disables second-factor enforcement.
6. Approved: Send bilingual security mail after TOTP addition, replacement,
   removal, and final disablement. Starting or abandoning setup sends no mail.
7. Approved: Raise the production minimum-release fence to v0.4.6 before TOTP
   enrollment is enabled. Once TOTP enrollment can succeed, a supported rollback
   below v0.4.6 is forbidden.

## Release surface

The authenticator-app release preserves every v0.4.2 passkey, pending, recovery,
error, cookie, CSRF, cache, and epoch rule. `GET /api/v1/capabilities` adds
`totpEnrollment: boolean`, true only when TOTP enrollment and replacement may
start and complete.

`GET /api/v1/me/second-factor` adds the required Boolean `totpEnabled`:

```json
{
  "data": {
    "enabled": true,
    "passkeys": [],
    "totpEnabled": true,
    "recoveryCodesRemaining": 10
  }
}
```

An unenrolled account returns false, an empty passkey array, false, and zero. No
state response returns a TOTP credential ID, secret, ciphertext, key ID, nonce,
last-used step, or provisioning URI.

Pending status keeps the v0.4.2 shape. `methods` gains the closed value `totp`
when the account has an active TOTP credential. Its fixed order is `passkey`,
`totp`, then `recovery`, omitting methods the account lacks. An enrolled account
with no method returns `503 authentication_unavailable`. `totp` stays listed
during a TOTP cool-down or key failure; verification then returns `429` or `503`
as defined below. Passkey assertion options on an account with no active passkey
return `404 factor_not_found` as the passkey contract defines, and TOTP
verification on an account with no active TOTP credential returns the same
error. Neither creates state or counts a failure.

Every v0.4.2 and TOTP route, including capabilities, sends the router's exact
`Cache-Control: no-store, no-transform` on every success and error.

The new routes are:

| Method   | Path                                       | Purpose                               |
| -------- | ------------------------------------------ | ------------------------------------- |
| `POST`   | `/api/v1/auth/second-factor/totp/verify`   | Complete pending authentication       |
| `POST`   | `/api/v1/me/second-factor/totp/enrollment` | Start enrollment or replacement       |
| `PUT`    | `/api/v1/me/second-factor/totp/enrollment` | Prove and install the proposed secret |
| `DELETE` | `/api/v1/me/second-factor/totp`            | Remove the active TOTP credential     |

No route accepts an arbitrary method name. TOTP adds no resume schema, public
route, renderer, PDF, MCP tool, OAuth scope, or portable-export field.

## TOTP profile and code verification

The service implements RFC 6238 with HMAC-SHA-1, a 20-byte random secret, six
decimal digits, a 30-second period, and `T0=0`. HMAC-SHA-1 is allowed only for
this interoperability profile. New security hashing continues to use the
algorithms required by its own contract.

Input is exactly six ASCII digits. Spaces, hyphens, Unicode digits, signs, or a
different length fail as `400 request_invalid` before cryptographic or database
work.

Verification calculates the previous, current, and next time steps when they are
nonnegative. It compares all three six-byte candidates in constant time and
selects the greatest matching step. It accepts a match only when that step is
greater than the credential's stored `last_used_step`. The transaction locks the
user and credential before rechecking the account epoch and advancing the step.
Concurrent verification of one code across login and reauthentication has one
winner. A replay is `401 verification_failed` and counts as one pending failure.

The service clock is injected. A time before the Unix epoch fails as
`503 authentication_unavailable`. Dependency, decryption, and clock failures do
not increment pending failures.

Enrollment proof has no stored step, so it accepts the greatest matching
nonnegative step in the window and stores it as the initial `last_used_step`.
The setup code therefore cannot complete a login. The server never logs or emits
a code, secret, candidate, matched step, HMAC input, or HMAC output.

## Provisioning data

Enrollment start generates one 20-byte secret from the server entropy source.
Its canonical secret is 32 uppercase, unpadded RFC 4648 Base32 characters. The
JSON `secret` field groups those characters into eight groups of four separated
by one ASCII space. Input never accepts this secret back from the browser.

The issuer is the lowercase ASCII hostname from `PUBLIC_ORIGIN`, without a port.
Production therefore uses `aboutme.vn`; local HTTPS uses `localhost`. Startup
rejects an enabled TOTP enrollment flag when that host is an IP literal or is
not a canonical ASCII DNS hostname; an internationalized host must use its
A-label form. The label is `<issuer>:<canonical account email>`. The server
creates this exact provisioning shape with RFC 3986 percent encoding:

```text
otpauth://totp/<escaped-issuer>:<escaped-email>?secret=<base32>&issuer=<escaped-issuer>&algorithm=SHA1&digits=6&period=30
```

The encoder percent-encodes the issuer and email as separate label components
and keeps only their separator colon literal. Query parameters occur in the
shown order. Spaces encode as `%20`, never `+`. The complete URI is at most
2,048 ASCII bytes.

The web app renders the QR code without a network request and holds the URI and
secret only in component memory. Dialog close, navigation, account-state change,
completion, logout, or unmount clears both. Neither enters a URL, browser
storage, analytics, logs, traces, screenshots, mail, or account export. The open
dialog permits secret copy.

## Enrollment and replacement API

Both management calls require a live current-epoch cookie session, the session
CSRF token, exact Origin, exact JSON media type, strict JSON, and recent proof.
First-factor enrollment requires recent primary proof. Enrollment or replacement
on an already enrolled account requires recent primary proof and recent proof
from any active factor or recovery code.

`POST /api/v1/me/second-factor/totp/enrollment` accepts exactly `{}`. It returns
`200` with one-time plaintext:

```json
{
  "data": {
    "enrollmentId": "43-character-unpadded-base64url-token",
    "secret": "ABCD EFGH IJKL MNOP QRST UVWX YZ23 4567",
    "provisioningUri": "otpauth://totp/aboutme.vn:user%40example.com?secret=ABCDEFGHIJKLMNOPQRSTUVWXYZ234567&issuer=aboutme.vn&algorithm=SHA1&digits=6&period=30",
    "expiresAt": "2026-09-20T09:10:00Z"
  }
}
```

`enrollmentId` is 32 random bytes in canonical unpadded base64url. PostgreSQL
stores only its SHA-256 digest. The ten-minute enrollment binds the account,
session, epoch, encrypted proposed secret, issuer, timestamps, and format.
Completion, supersession, and removal delete the enrollment row, so no used row
keeps ciphertext.

Under the user lock, start deletes the account's existing enrollment row,
expired or not, before inserting the new one. It changes no active credential,
recovery code, enforcement, epoch, session, grant, or mail, so abandoning
replacement cannot lock out the user.

`PUT /api/v1/me/second-factor/totp/enrollment` accepts exactly:

```json
{
  "enrollmentId": "43-character-unpadded-base64url-token",
  "code": "123456"
}
```

Unknown, malformed, foreign, expired, deleted, wrong-session, or wrong-epoch
enrollment IDs collapse to `400 enrollment_invalid`. A well-shaped invalid or
replayed code returns `401 verification_failed`. Enrollment proof skips the
pending-attempt counter and never changes the active credential's failure
budget. TOTP start, completion, and removal share the existing factor-management
bucket of 10 per hour per `(account, client IP)` with passkey management and
regeneration. Completion also uses the shared code limits of 10 per 15 minutes
per `(account, client IP)`, plus 30 per minute per client IP.

Successful completion uses the canonical lock order below, rechecks every proof,
deletes the enrollment, and inserts or replaces the active credential. When no
policy exists, it generates a cryptographically random 32-byte
`webauthn_user_handle` and inserts the policy with it. Each candidate insert
runs inside its own savepoint, because a unique violation aborts the
transaction. A collision rolls back to that savepoint and gets at most two fresh
retries, for three candidates total. Three collisions return
`503 authentication_unavailable` and create no policy, credential, recovery
code, epoch change, session, grant revocation, or mail. Later passkey enrollment
reuses the persisted handle exactly. Completion advances the epoch, revokes
other sessions and all connected-agent grants and token families, and returns a
replacement current session. It preserves creation time, absolute expiry, device
metadata, primary-proof time, and prior factor-proof time. First-factor
completion sets factor-proof time to its completion time.

Completion returns `200`:

```json
{
  "data": {
    "totpEnabled": true,
    "recoveryCodes": ["amr_00000-00000-00000-00000-00000-0"]
  }
}
```

The real first-factor response has ten recovery codes. `recoveryCodes` is
present only when this completion created the account's first active factor. It
is omitted when a passkey already enabled enforcement and on TOTP replacement.
It is never null or an empty array. The existing recovery display, download,
storage, cleanup, and regeneration contract applies unchanged.

Enrollment start and completion both recheck `TOTP_ENROLLMENT_ENABLED`. When
false, start creates nothing and returns the uniform `404 route_not_found`.
Completion deletes a matching enrollment, installs nothing, and returns the
same 404. Verification, removal, state reads, and recovery ignore this flag.

## Pending verification API

`POST /api/v1/auth/second-factor/totp/verify` accepts exactly:

```json
{ "code": "123456" }
```

It uses the pending cookie, pending CSRF token, exact Origin, and v0.4.2 check
order. The body limit is 4,096 bytes. Duplicate, unknown, missing, malformed, or
noncanonical fields return `400 request_invalid`. An absent, expired, consumed,
wrong-epoch, or wrong-session pending row returns `401 authentication_required`.
A valid code returns `204`, consumes the pending row, advances the credential
step in the same transaction, and clears the pending cookie.

An invalid or replayed code returns `401 verification_failed` and increments the
shared pending failure count. The fifth total failed passkey, TOTP, or recovery
attempt consumes the pending row, enqueues the existing exhaustion mail subject
to the cap below, and clears the cookie. Existing account and client-IP
second-factor rate limits apply across methods.

## Per-account TOTP failure budget

Each valid primary login creates a new pending row, so the TOTP credential
carries an IP-independent budget in PostgreSQL: `failed_attempts`, from 0
through 1,000, and nullable `cooldown_until`.

Verification locks the credential in the canonical order. When `cooldown_until`
is later than the transaction time, it returns `429 rate_limited` with
`Retry-After` set to the remaining whole seconds, at most 86,400. It does no
decryption, counts no pending failure, and changes no row. Otherwise each
invalid or replayed code sets `failed_attempts` to the lesser of its value plus
one and 1,000. When the new value is a multiple of five, `cooldown_until`
becomes the transaction time plus 15 minutes times
`2^min(failed_attempts/5 - 1, 7)`, capped at 24 hours, and the exhaustion mail
is enqueued subject to the cap. At the 1,000 ceiling every further failure keeps
the value at 1,000 and sets a 24-hour cool-down. A valid code, replacement, or
removal resets both fields.

A password holder with any number of client IPs thus gets at most 35 guesses in
the first day and 5 per day after that, under 0.6 percent success in a year. The
cool-down covers only TOTP. Passkey and recovery verification never read these
fields, so the budget cannot lock out another method.

Residual risk: a password holder can keep TOTP in cool-down indefinitely. The
user's ways out are waiting, a passkey, or a recovery code. A completed password
reset changes neither `cooldown_until` nor `failed_attempts`, so an attacker who
controls the mailbox cannot reset the budget. The new password still stops the
attacker from starting another cool-down.

Every `second_factor_attempts_exhausted` job, from any method, obeys one
per-account cap. Migration 00005 adds nullable
`second_factor_policies.attempt_mail_at`. Under the policy lock, the job is
enqueued only when that value is null or at least one hour old, and the same
transaction sets it to the transaction time. A suppressed mail never suppresses
the state change. An account receives at most 24 exhaustion mails a day.

## Removal, recovery, and races

`DELETE /api/v1/me/second-factor/totp` uses no request body. It requires the
same current session, CSRF, Origin, and recent-proof boundary as other factor
management. Success is `204`; a missing credential is `404 factor_not_found`.

Removal deletes any TOTP enrollment. An active factor is an active passkey or
TOTP credential. This release registers the TOTP counter with the shared
active-factor count defined in
[Second-factor authentication](second-factor-authentication.md#enrollment-and-management).
TOTP removal and passkey removal both decide final versus non-final only from
that count, taken under the user lock, so no passkey route or handler changes.
While another factor remains, removal keeps the policy and recovery-code set.
When TOTP is the final active factor, removal deletes the policy and every
recovery digest and clears factor proof on the replacement current session.
Removing the last passkey while TOTP remains is never final. Every removal
advances the epoch, revokes other sessions and connected-agent authority,
rotates the current session, and enqueues mail atomically.

The credential being replaced or removed may have supplied the recent factor
proof. A user who lost TOTP may use a passkey or recovery code instead. Nothing
else bypasses factor enforcement, and losing every factor and recovery code is
permanent account loss.

Every TOTP path uses one lock order: user, current session when present, factor
policy, TOTP credential, TOTP enrollment, and pending authentication last when
applicable. OAuth grants, codes, tokens, and mail follow as the shared design
requires. Concurrent start, completion, replacement, removal, code or recovery
use, session rotation, and deletion have one valid winner and no partial state.

## PostgreSQL shape and bounds

Migration `00005_totp_second_factor.sql` adds these relations:

- `totp_credentials`: application-generated UUIDv7 `id`, unique `user_id`, key
  identifier, 12-byte nonce, 36-byte ciphertext, format version 1, nonnegative
  `last_used_step`, `failed_attempts` from 0 through 1,000, nullable
  `cooldown_until`, `created_at`, and `updated_at`;
- `totp_enrollments`: application-generated UUIDv7 `id`, unique 32-byte token
  digest, `user_id`, concrete `session_id`, authentication epoch, issuer, key
  identifier, 12-byte nonce, 36-byte ciphertext, format version 1, and creation
  and expiry times; and
- nullable `second_factor_policies.attempt_mail_at` for the exhaustion-mail cap.

Replacement updates the existing credential row, preserves its ID and creation
time, and advances `updated_at`. Both user foreign keys use `ON DELETE CASCADE`.
The enrollment session foreign key also cascades, but only when the session row
is deleted. Revocation leaves the row, so completion's lock and liveness check
on the current session, which must equal the bound session, is the guard.
Credential and enrollment key identifiers have indexes. Enrollment cleanup uses
`(expires_at,id)`. A unique `totp_enrollments.user_id` permits at most one
enrollment row per account. Issuer is 1 to 253 canonical ASCII hostname bytes.

Database checks enforce every byte length, the 26-byte `tk1_` key identifier
shape, closed format version, expiry after creation, nonnegative step, and the
failure range. Cleanup deletes at most 200 expired enrollment rows per admitted
start. Account deletion removes all plaintext-derived state. Portable export
excludes both relations.

Migration 00005 also replaces `auth_email_jobs_kind_check` and
`auth_email_jobs_scope_check`. The kind constraint adds `totp_added`,
`totp_replaced`, and `totp_removed` to the complete v0.4.2 set. The scope
constraint requires `user_id`, forbids registration and reset scope, and keeps
the existing token-digest rule. `aboutme_app` gets only `SELECT`, `INSERT`,
`UPDATE`, and `DELETE` on both new tables. The down section deletes only the
three TOTP job kinds, restores the exact v0.4.2 constraints, and drops both
tables and the policy column. It first raises an exception when any
`totp_credentials` row exists, because dropping it would strand a TOTP-only
policy. Down is supported only before enrollment enablement. Production never
runs a down migration. TOTP mail insertion stays inside the factor mutation
transaction.

These are the exact TOTP bounds. [Budgets](budgets.md) repeats each for budget
tests:

- one active TOTP credential and one enrollment row per account;
- 20 secret bytes, 32 Base32 characters, and a 39-character grouped display;
- six digits, 30-second period, and previous/current/next-step verification;
- ten-minute enrollment lifetime and 32-byte enrollment token;
- 4,096-byte verification and management request bodies;
- 2,048-byte provisioning URI;
- 1 to 253 issuer bytes;
- three policy-handle candidates per first TOTP completion;
- 10 factor-management actions per hour per `(account, client IP)`, shared with
  passkey management;
- per-account TOTP cool-down every fifth consecutive failure, 15 minutes
  doubling to at most 24 hours, and a 1,000-failure counter ceiling;
- one `second_factor_attempts_exhausted` mail per account per hour;
- one active and at most one previous 32-byte key, and 26-byte derived key IDs;
- at most three distinct stored key IDs read at startup and every five minutes;
- 12 nonce bytes and 36 ciphertext bytes;
- 200-row cleanup and re-encryption batches;
- 10,000 re-encryption rows and 30 minutes per run;
- a ten-minute wait after the key switch before the final rotation count; and
- one `totp_unavailable` log line per reason per minute per process.

## Security mail and locales

The authenticator-app release adds closed mail kinds `totp_added`,
`totp_replaced`, and `totp_removed`. First-factor completion uses
`second_factor_enabled`. Final factor removal uses `second_factor_disabled`.
Existing recovery events stay unchanged. The existing attempt event also marks a
TOTP cool-down start, and every attempt mail obeys the per-account cap.

The events use encrypted payload version 2 with only `to` and `occurredAt`.
Every template contains fixed Vietnamese first and English second. It names an
authenticator app, the action, and UTC time, then advises password change and
session revocation. It contains no code, secret, provisioning URI, QR image,
credential or enrollment ID, key ID, nonce, ciphertext, IP, user agent, account
ID, or state-changing link.

The factor mutation and outbox insert commit together; insert or encryption
failure aborts it. Starting, expiring, superseding, or abandoning an enrollment
sends no mail.

Every new screen, error, label, warning, title, accessible name, and live
announcement has Vietnamese and English text. Protocol names, codes, issuer,
account email, and provisioning data remain unchanged when locale changes.

## Migration, mixed versions, and loss

Migration 00005 is additive and forward-only. It creates no credential or
enrollment for existing accounts. Its only change to a v0.4.2 relation is the
nullable policy column, which changes no row value. It changes no passkey,
policy, recovery, pending, session, grant, authorization-code, token, or mail
row and deletes no data. Rollback leaves the empty or populated tables in place.

Before enrollment enablement, v0.4.2 and v0.4.6 application tasks may overlap
because no TOTP row can exist. The v0.4.6 web treats absent or malformed new
capability and state fields as false during that window. The flag stays false
until every running server and web asset supports TOTP.

After TOTP enrollment can succeed, an older server cannot provide every active
method or manage a TOTP-only account. A v0.4.2 passkey removal counts only
passkeys, so it could delete the policy and recovery codes while TOTP stays
active. The durable production floor must therefore be v0.4.6, numeric release
4006, before the flag turns on. `deploy.sh` and `fence.sh` refuse any app
revision with TOTP enrollment on while the fence item is missing or below 4006,
as the
[release fence](passkey-release-fence.md#authenticator-app-key-re-encryption)
defines. Older browser assets fail closed: primary login still creates no
session, and the v0.4.2 pending page shows a refresh prompt for the unknown
`totp` value, calls no route for it, and grants nothing. Existing recovery may
still work, but compatibility does not depend on it.

Turning `TOTP_ENROLLMENT_ENABLED` off stops new setup and replacement. It never
lowers the floor, removes a credential, disables TOTP verification, changes
recovery, or permits primary-only login. After enablement, rollback means a
forward fix or an image at or above the v0.4.6 fence.

The only intentional loss is ephemeral: starting a new enrollment deletes the
prior incomplete enrollment, and cleanup removes expired enrollment rows. Active
credentials and recovery codes are unchanged until verified completion or
deliberate removal.

## Verification and release

GitHub CI owns every build, test, lint, database, migration, browser,
infrastructure, Semgrep, and gitleaks gate. Production deploys valid keys with
enrollment off, proves compatibility, raises the serialized floor to v0.4.6,
enables enrollment, and proves the flow with the authorized fictional account.
The scripted production proof keeps every secret, URI, and code in process
memory and prints only fixed step outcomes.
