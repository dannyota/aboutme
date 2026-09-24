# Authenticator-app second-factor contract

This contract adds time-based one-time password (TOTP) codes to the pending,
recovery, session, and epoch boundary of the
[second-factor design](second-factor-authentication.md) without weakening
passkeys. [ADR 0049](../adr/0049-totp-second-factor-authentication.md) records
the product choices. The [passkey contract](passkey-second-factor-contract.md)
owns the shared pending, state, recovery, and mail shapes. The
[key-management design](totp-key-management.md) owns sealing and keys.
[Numeric budgets](budgets.md) owns every bound.

## Release surface

The TOTP routes are:

| Method   | Path                                       | Purpose                               |
| -------- | ------------------------------------------ | ------------------------------------- |
| `POST`   | `/api/v1/auth/second-factor/totp/verify`   | Complete pending authentication       |
| `POST`   | `/api/v1/me/second-factor/totp/enrollment` | Start enrollment or replacement       |
| `PUT`    | `/api/v1/me/second-factor/totp/enrollment` | Prove and install the proposed secret |
| `DELETE` | `/api/v1/me/second-factor/totp`            | Remove the active TOTP credential     |

Capabilities report `totpEnrollment`, account state reports `totpEnabled`, and
pending status lists `totp` while the account has an active TOTP credential.
`totp` stays listed during a cool-down or key failure; verification then returns
`429` or `503`. TOTP verification on an account with no TOTP credential returns
`404 factor_not_found`, creates nothing, and counts no failure. No response
returns a credential ID, secret, ciphertext, key ID, nonce, last-used step, or
provisioning URI outside enrollment start.

TOTP adds no resume schema, public route, renderer, PDF, MCP tool, OAuth scope,
or export field.

## TOTP profile and code verification

The service implements RFC 6238 with HMAC-SHA-1, a 20-byte random secret, six
decimal digits, a 30-second period, and `T0=0`. HMAC-SHA-1 is allowed only for
this interoperability profile.

Input is exactly six ASCII digits. Spaces, hyphens, Unicode digits, signs, or
another length fail as `400 request_invalid` before cryptographic or database
work.

Verification computes the previous, current, and next steps that are
nonnegative, compares all three six-byte candidates in constant time, and picks
the greatest match. It accepts the match only when that step is greater than the
stored `last_used_step`. The transaction locks the user and credential, rechecks
the epoch, and advances the step. Concurrent use of one code across login and
reauthentication has one winner. A replay is `401 verification_failed` and one
pending failure.

The clock is injected. A time before the Unix epoch fails as
`503 authentication_unavailable`. Dependency, decryption, and clock failures
count no failure.

Enrollment proof has no stored step, so it accepts the greatest matching
nonnegative step and stores it as the initial `last_used_step`. The server never
logs or emits a code, secret, candidate, matched step, or HMAC input or output.

## Provisioning data

Enrollment start draws a 20-byte secret from the server entropy source. Its
canonical form is 32 uppercase unpadded RFC 4648 Base32 characters. The JSON
`secret` shows it as eight groups of four separated by one space. The browser
never sends the secret back.

The issuer is the lowercase ASCII host of `PUBLIC_ORIGIN` without a port:
`aboutme.vn` in production, and local HTTPS uses `localhost`. Startup rejects an
enabled TOTP enrollment flag when that host is an IP literal or not a canonical
ASCII DNS name; an internationalized host must use its A-label. The URI is, with
RFC 3986 percent encoding:

```text
otpauth://totp/<escaped-issuer>:<escaped-email>?secret=<base32>&issuer=<escaped-issuer>&algorithm=SHA1&digits=6&period=30
```

The issuer and the canonical account email are encoded as separate label parts
around one literal colon. Query parameters keep the order shown. A space encodes
as `%20`, never `+`.

The web renders the QR code locally and holds the URI and secret only in
component memory. Dialog close, navigation, account-state change, completion,
logout, or unmount clears both. Neither enters a URL, browser storage,
analytics, logs, traces, screenshots, mail, or export. The open dialog allows
copying the secret.

## Enrollment and replacement API

Both calls need a live current-epoch cookie session, the session CSRF token,
exact Origin, exact JSON media type, strict JSON, and the recent proof that the
[management rules](second-factor-authentication.md#enrollment-and-management)
require.

`POST /api/v1/me/second-factor/totp/enrollment` takes exactly `{}` and returns
`200`:

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

`enrollmentId` is 32 random bytes as unpadded base64url; PostgreSQL stores its
SHA-256 digest. The ten-minute enrollment binds account, session, epoch, sealed
secret, issuer, timestamps, and format. Under the user lock, start deletes the
account's existing enrollment row, expired or not, and inserts the new one. It
changes no active credential, recovery code, enforcement, epoch, session, grant,
or mail, so abandoning a replacement cannot lock the user out.

`PUT /api/v1/me/second-factor/totp/enrollment` takes exactly:

```json
{
  "enrollmentId": "43-character-unpadded-base64url-token",
  "code": "123456"
}
```

Unknown, malformed, foreign, expired, deleted, wrong-session, or wrong-epoch
enrollment IDs return `400 enrollment_invalid`. An invalid or replayed code
returns `401 verification_failed`. Enrollment proof uses no pending row and
never touches the active credential's failure budget. Start, completion, and
removal share the factor-management rate bucket with passkey management.
Completion also uses the shared code-attempt limits.

Successful completion takes the lock order below, rechecks every proof, deletes
the enrollment, and inserts or replaces the credential. When no policy exists,
it generates a random 32-byte `webauthn_user_handle` and inserts the policy,
each candidate inside its own savepoint because a unique violation aborts the
transaction. It tries at most three candidates; three collisions return
`503 authentication_unavailable` and change nothing. Later passkey enrollment
reuses the stored handle. Completion then applies the epoch, revocation, and
session-replacement rules of the
[assurance boundary](second-factor-authentication.md#assurance-boundary).

Completion returns `200`:

```json
{
  "data": {
    "totpEnabled": true,
    "recoveryCodes": ["amr_00000-00000-00000-00000-00000-0"]
  }
}
```

`recoveryCodes` holds ten codes and appears only when this completion created
the first active factor. It is omitted after a passkey already enabled
enforcement and on replacement, and is never null or empty.

While `TOTP_ENROLLMENT_ENABLED` is false, start returns the uniform
`404 not_found` before any session or database work. Completion deletes a
matching enrollment, installs nothing, and returns the same `404`.

## Pending verification API

`POST /api/v1/auth/second-factor/totp/verify` takes exactly `{"code":"123456"}`
under the pending cookie, pending CSRF token, exact Origin, and the shared check
order. A valid code returns `204`, consumes the pending row, advances the step
in the same transaction, and clears the cookie. An invalid or replayed code
returns `401 verification_failed` and counts toward the shared five-failure
pending limit.

## Per-account TOTP failure budget

Every valid primary login creates a new pending row, so the credential row also
carries an IP-independent budget: `failed_attempts` (0 through 1,000), nullable
`cooldown_until`, and nullable `last_failed_at`.

Verification locks the credential in lock order. While `cooldown_until` is later
than the transaction time, it returns `429 rate_limited` with `Retry-After` set
to the remaining whole seconds (at most 86,400). It decrypts nothing, counts no
pending failure, and changes no row. Otherwise each invalid or replayed code
sets `failed_attempts` to the lesser of its value plus one and 1,000, and sets
`last_failed_at`. When the new value is a multiple of five, `cooldown_until`
becomes the transaction time plus 15 minutes times
`2^min(failed_attempts/5 - 1, 7)`, capped at 24 hours, and exhaustion mail is
enqueued subject to its cap. At the ceiling every further failure sets a 24-hour
cool-down. A valid code resets all three fields only when `last_failed_at` is
null or at least 24 hours old. Replacement and removal always reset them.

A password holder therefore gets at most 35 guesses in the first day and 5 a day
after, from any number of client IPs. The cool-down covers TOTP only; passkey
and recovery verification never read these fields. A password reset changes
neither field, so a mailbox holder cannot reset the budget.

Every `second_factor_attempts_exhausted` job, from any method, is enqueued only
when `second_factor_policies.attempt_mail_at` is null or at least one hour old,
and the same transaction sets it to the transaction time, under the policy lock.
A suppressed mail never suppresses the state change.

## Removal, recovery, and races

`DELETE /api/v1/me/second-factor/totp` has no body and uses the same session,
CSRF, Origin, and recent-proof boundary as other factor management. Success is
`204`; a missing credential is `404 factor_not_found`.

Removal deletes any TOTP enrollment and decides final versus non-final from the
shared active-factor count. While another factor remains, the policy and
recovery codes stay. When TOTP is the final factor, removal deletes the policy
and every recovery digest and clears factor proof on the replacement session.
Every removal advances the epoch, revokes other sessions and agent authority,
rotates the current session, and enqueues mail in one transaction.

Every TOTP path uses one lock order: user, current session when present, factor
policy, TOTP credential, TOTP enrollment, then pending authentication when
applicable. OAuth grants, codes, tokens, and mail follow as the shared design
requires. Concurrent start, completion, replacement, removal, code or recovery
use, session rotation, and deletion have one winner and leave no partial state.

## PostgreSQL shape and bounds

Migration `00005_totp_second_factor.sql` adds:

- `totp_credentials`: application-generated UUIDv7 `id`, unique `user_id`, key
  ID, 12-byte nonce, 36-byte ciphertext, format version 1, nonnegative
  `last_used_step`, `failed_attempts` from 0 through 1,000, nullable
  `cooldown_until` and `last_failed_at`, `created_at`, and `updated_at`;
- `totp_enrollments`: application-generated UUIDv7 `id`, unique 32-byte token
  digest, unique `user_id`, `session_id`, epoch, issuer of 1 to 253 canonical
  ASCII bytes, key ID, 12-byte nonce, 36-byte ciphertext, format version 1, and
  creation and expiry times;
- nullable `second_factor_policies.attempt_mail_at`.

The application generates both IDs because each is bound into its ciphertext
before insert. Replacement and re-encryption update the credential row in place,
keeping its ID and creation time. Both user keys cascade on delete. The
enrollment session key also cascades, but only on session deletion; revocation
leaves the row, so completion's lock and liveness check on the current session,
which must equal the bound one, is the guard. Both key-ID columns are indexed.
Enrollment cleanup uses `(expires_at,id)` and deletes at most 200 expired rows
per admitted start. Checks enforce every byte length, the 26-byte `tk1_` key-ID
shape, format version 1, expiry after creation, a nonnegative step, and the
failure range. Portable export excludes both tables.

The migration also replaces `auth_email_jobs_kind_check` and
`auth_email_jobs_scope_check` to admit `totp_added`, `totp_replaced`, and
`totp_removed` with required user scope, no registration or reset scope, and the
existing token-digest rule. `aboutme_app` gets `SELECT`, `INSERT`, `UPDATE`, and
`DELETE` on both tables. The down section raises an exception while any
`totp_credentials` row exists; production never runs a down migration.

## Security mail and locales

`totp_added`, `totp_replaced`, and `totp_removed` use encrypted payload version
2 with only `to` and `occurredAt`. A first-factor completion sends
`second_factor_enabled` and a final removal `second_factor_disabled` instead.
Templates are fixed Vietnamese then English. They name an authenticator app, the
action, and the UTC time, and advise a password change and session revocation.
They contain no code, secret, URI, QR image, credential or enrollment ID, key
ID, nonce, ciphertext, IP, user agent, account ID, or link.

The factor mutation and outbox insert commit together. Starting, expiring,
superseding, or abandoning an enrollment sends no mail.

Every screen, error, label, title, accessible name, and live announcement has
Vietnamese and English text. Protocol names, codes, issuer, email, and
provisioning data do not change with locale.

## Migration, mixed versions, and loss

Migration 00005 is additive. It created no credential or enrollment and changed
no existing row. The only automatic loss is ephemeral: a new enrollment deletes
the prior incomplete one, and cleanup removes expired ones. Active credentials
and recovery codes change only on verified completion or deliberate removal.

An older server cannot count TOTP as an active factor, so production runs v0.4.7
(numeric 4007) or later whenever TOTP enrollment can succeed. `deploy.sh` and
`fence.sh` enforce this floor as the
[release fence](passkey-release-fence.md#authenticator-app-key-re-encryption)
defines. An older browser shows a refresh prompt for the unknown `totp` method,
calls no route for it, and grants nothing.
