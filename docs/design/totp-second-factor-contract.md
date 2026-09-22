# Authenticator-app second-factor contract

Status: Proposed for v0.4.3. The owner approved all seven product choices below
on 2026-09-22.

This contract adds time-based one-time password (TOTP) verification to the
v0.4.2 pending, recovery, session, and authentication-epoch boundary. It does
not weaken or replace passkey enforcement.

## Owner decisions

The owner already selected optional authenticator-app codes after passkeys and
the same single-use recovery model. The owner approved these product choices on
2026-09-22:

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
7. Approved: Raise the production minimum-release fence to v0.4.3 before TOTP
   enrollment is enabled. Once TOTP enrollment can succeed, a supported rollback
   below v0.4.3 is forbidden.

## Release surface

V0.4.3 preserves every v0.4.2 passkey, pending, recovery, error, cookie, CSRF,
cache, and epoch rule. `GET /api/v1/capabilities` adds
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
`totp`, then `recovery`, omitting unavailable methods. An enrolled account with
no available method returns `503 authentication_unavailable`.

Cache policy is route-specific on every success and error:

- `GET /api/v1/capabilities`, password login and reauthentication, and every
  v0.4.2 pending, passkey, recovery, and factor-state route keep the exact
  `Cache-Control: no-store` header.
- New TOTP verification and removal also use exact `Cache-Control: no-store`.
- TOTP enrollment `POST` and `PUT` use exact
  `Cache-Control: no-store, no-transform` because their success can contain a
  provisioning secret or recovery codes.

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

Enrollment proof uses the same rules and stores the matched step as the initial
`last_used_step`, so the setup code cannot complete a login. The server never
logs or emits a code, secret, candidate, matched step, HMAC input, or HMAC
output.

## Provisioning data

Enrollment start generates one 20-byte secret from the server entropy source.
Its canonical secret is 32 uppercase, unpadded RFC 4648 Base32 characters. The
JSON `secret` field groups those characters into eight groups of four separated
by one ASCII space. Input never accepts this secret back from the browser.

The issuer is the lowercase ASCII hostname from `PUBLIC_ORIGIN`, without a port.
Production therefore uses `aboutme.vn`; local HTTPS uses `localhost`. The label
is `<issuer>:<canonical account email>`. The server creates this exact
provisioning shape with RFC 3986 percent encoding:

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

Under the user lock, start consumes any prior live enrollment. It changes no
active credential, recovery code, enforcement, epoch, session, grant, or mail,
so abandoning replacement cannot lock out the user.

`PUT /api/v1/me/second-factor/totp/enrollment` accepts exactly:

```json
{
  "enrollmentId": "43-character-unpadded-base64url-token",
  "code": "123456"
}
```

Unknown, malformed, foreign, expired, consumed, wrong-session, or wrong-epoch
enrollment IDs collapse to `400 enrollment_invalid`. A well-shaped invalid or
replayed code returns `401 verification_failed`. Enrollment proof skips the
pending-attempt counter. It uses the management limit of 10 per hour and shared
code limits of 10 per 15 minutes per `(account, client IP)`, plus 30 per minute
per client IP.

Successful completion uses the canonical lock order below, rechecks every proof,
consumes the enrollment, and inserts or replaces the active credential. When no
policy exists, it generates a cryptographically random 32-byte
`webauthn_user_handle` and inserts the policy with it. A unique collision gets
at most two fresh retries, for three candidates total. Three collisions return
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
Completion consumes a matching enrollment, installs nothing, and returns the
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
attempt consumes the pending row, enqueues the existing exhaustion mail, and
clears the cookie. Existing account and client-IP second-factor rate limits
apply across methods.

## Removal, recovery, and races

`DELETE /api/v1/me/second-factor/totp` uses no request body. It requires the
same current session, CSRF, Origin, and recent-proof boundary as other factor
management. Success is `204`; a missing credential is `404 factor_not_found`.

Removal consumes any live TOTP enrollment. When a passkey remains, removal keeps
the policy and recovery-code set. When TOTP is the final active factor, removal
deletes the policy and every recovery digest and clears factor proof on the
replacement current session. Both paths advance the epoch, revoke other sessions
and connected-agent authority, rotate the current session, and enqueue mail
atomically.

The credential being replaced or removed may have supplied the recent factor
proof. A user who lost TOTP but retains a passkey or recovery code may use it to
authorize replacement or removal. Password reset, provider login, relinking,
email, support, and operators never bypass factor enforcement. Losing every
active factor and recovery code remains permanent application-level account
loss.

Every TOTP path uses one lock order: user, current session when present, factor
policy, TOTP credential, TOTP enrollment, and pending authentication last when
applicable. OAuth grants, codes, tokens, and mail follow as the shared design
requires. Concurrent start, completion, replacement, removal, code or recovery
use, session rotation, and deletion have one valid winner and no partial state.

## Encryption and key rotation

TOTP secrets must be recoverable for verification. PostgreSQL stores only
AES-256-GCM ciphertext, a random 96-bit nonce, a key identifier, and format
version. Format version 1 ciphertext is exactly 36 bytes for the 20-byte secret
and 16-byte tag.

Associated data concatenates ASCII `aboutme.totp.v1`, zero, record kind
`credential` or `enrollment`, zero, raw 16-byte account UUID, zero, raw 16-byte
row UUID, zero, ASCII format-version decimal, zero, and ASCII key ID. Moving
ciphertext between any of those bindings fails authentication.

Runtime configuration provides exactly one active 32-byte key and at most one
previous 32-byte key. Key values use canonical 43-character unpadded base64url.
Key identifiers are 1 to 64 printable ASCII bytes, distinct, and never secret.
The exact variables are `TOTP_ACTIVE_KEY_ID`, `TOTP_ACTIVE_KEY`,
`TOTP_PREVIOUS_KEY_ID`, and `TOTP_PREVIOUS_KEY`. The previous pair is either
both absent or both present. Production stores them under `totp/active-key-id`,
`totp/active-key`, `totp/previous-key-id`, and `totp/previous-key` in the
existing task-secret parameter prefix. Key values use protected parameters. Only
the app ECS execution role may read those exact parameter ARNs. Scheduled jobs
receive none. The supported one-shot path is
`/usr/local/bin/server totp-key-reencrypt` in a dedicated ECS task definition
that uses the app execution and task roles and the same exact injected
parameters. New credentials, new enrollments, and replacement writes use the
active key. Verification under the previous key lazily re-encrypts the
credential with a fresh nonce and the active key in the same transaction.

Rotation uses these ordered steps:

1. Install a new active key and retain the old active key as previous.
2. Deploy v0.4.3 or later and confirm readiness with both identifiers.
3. Run the bounded re-encryption command until it reports zero credential and
   live-enrollment rows on the previous identifier. Each transaction locks and
   rewrites at most 200 rows. One command processes at most 10,000 rows in 30
   minutes.
4. Wait ten minutes after the last old-key enrollment was created, then run the
   bounded count check again.
5. Remove the previous key only after the check returns zero.

The re-encryption command uses the same authenticated decrypt and encrypt paths,
fresh nonces, row locks, and a key-ID compare before update. It reports counts
and row IDs only. It never prints keys, secrets, nonces, ciphertext, email, or
account IDs. A decrypt failure leaves the row unchanged and fails the run.

Startup fails on a missing or malformed ring. Its bounded query reads at most
three distinct key IDs across active credentials and unexpired enrollments; an
unknown or third ID fails startup. It never scans or decrypts every credential.

An AES-GCM authentication failure returns `503 authentication_unavailable` and
atomically latches that server instance unhealthy with only the first failing
record kind and internal row ID. Further failures add no latch entries. Each
readiness probe may lock and validate that one row once. Successful decryption
or confirmed deletion clears the latch; failure keeps it unhealthy. Restart
clears the process latch only after the bounded key-ID startup check. Unrepaired
ciphertext relatches on its next use. Readiness exposes only a fixed TOTP
unavailable outcome, never row, account, key, or ciphertext data.

No failure falls back to plaintext, a default key, unchecked code, or
recovery-only enforcement. Passkey and recovery verification do not decrypt TOTP
state.

Keys use the existing runtime secret path. Values never enter source, OpenTofu
state, plans, command arguments, logs, metrics, documentation, CI artifacts, or
conversation output.

## PostgreSQL shape and bounds

Migration `00005_totp_second_factor.sql` adds these relations:

- `totp_credentials`: UUIDv7 `id`, unique `user_id`, key identifier, 12-byte
  nonce, 36-byte ciphertext, format version 1, nonnegative `last_used_step`,
  `created_at`, and `updated_at`;
- `totp_enrollments`: UUIDv7 `id`, unique 32-byte token digest, `user_id`,
  concrete `session_id`, authentication epoch, issuer, key identifier, 12-byte
  nonce, 36-byte ciphertext, format version 1, creation and expiry times, and
  nullable `consumed_at`.

Replacement updates the existing credential row, preserves its ID and creation
time, and advances `updated_at`. Both user foreign keys use `ON DELETE CASCADE`.
The enrollment session foreign key also cascades, so revoking its bound session
invalidates setup. Credential and enrollment key identifiers have indexes.
Enrollment cleanup uses `(expires_at,id)`. A partial unique index permits at
most one unconsumed enrollment per account; start consumes the old row before
inserting the new one. Issuer is 1 to 253 canonical ASCII hostname bytes.

Database checks enforce every byte length, printable key identifier, closed
format version, expiry after creation, and nonnegative step. Cleanup deletes at
most 200 expired or consumed enrollment rows per admitted start. Account
deletion removes all plaintext-derived state. Portable export excludes both
relations.

Migration 00005 also replaces `auth_email_jobs_kind_check` and
`auth_email_jobs_scope_check`. The kind constraint adds `totp_added`,
`totp_replaced`, and `totp_removed` to the complete v0.4.2 set. The scope
constraint requires `user_id`, forbids registration and reset scope, and keeps
the existing token-digest rule. Its down section deletes only those three job
kinds before restoring the exact v0.4.2 constraints. TOTP mail insertion stays
inside the factor mutation transaction.

- one active TOTP credential and one unconsumed enrollment per account;
- 20 secret bytes, 32 Base32 characters, and a 39-character grouped display;
- six digits, 30-second period, and previous/current/next-step verification;
- ten-minute enrollment lifetime and 32-byte enrollment token;
- 4,096-byte verification and management request bodies;
- 2,048-byte provisioning URI;
- 1 to 253 issuer bytes;
- three policy-handle candidates per first TOTP completion;
- 10 TOTP management actions per hour per `(account, client IP)`;
- 1 to 64 printable ASCII key ID bytes;
- 12 nonce bytes and 36 ciphertext bytes;
- 200-row cleanup and re-encryption batches; and
- 10,000 re-encryption rows and 30 minutes per run.

## Security mail and locales

V0.4.3 adds closed mail kinds `totp_added`, `totp_replaced`, and `totp_removed`.
First-factor completion uses `second_factor_enabled`. Final factor removal uses
`second_factor_disabled`. Existing recovery and attempt events stay unchanged.

The events use encrypted payload version 2 with only `to` and `occurredAt`.
Every template contains fixed Vietnamese first and English second. It names an
authenticator app, the action, and UTC time, then advises password change and
session revocation. It contains no code, secret, provisioning URI, QR image,
credential or enrollment ID, key ID, nonce, ciphertext, IP, user agent, account
ID, or state-changing link.

The factor mutation and outbox insert commit together. Insert or encryption
failure aborts the mutation. Starting, expiring, superseding, or abandoning an
enrollment does not send mail.

Every new screen, error, label, warning, title, accessible name, and live
announcement has Vietnamese and English text. Protocol names, codes, issuer,
account email, and provisioning data remain unchanged when locale changes.

## Migration, mixed versions, and loss

Migration 00005 is additive and forward-only. It creates no credential or
enrollment for existing accounts. It changes no v0.4.2 passkey, policy,
recovery, pending, session, grant, authorization-code, token, or mail row. It
deletes no data. Rollback leaves the empty or populated tables in place.

Before enrollment enablement, v0.4.2 and v0.4.3 application tasks may overlap
because no TOTP row can exist. The v0.4.3 web treats absent or malformed new
capability and state fields as false during that window. The flag stays false
until every running server and web asset supports TOTP.

After TOTP enrollment can succeed, an older server cannot provide every active
method or manage a TOTP-only account. The durable production floor must then be
v0.4.3, numeric release 4003. Older browser assets fail closed: primary login
still creates no session, an unknown `totp` method grants nothing, and the page
asks the user to refresh. Existing recovery may still work, but compatibility
does not depend on it.

Turning `TOTP_ENROLLMENT_ENABLED` off stops new setup and replacement. It never
lowers the floor, removes a credential, disables TOTP verification, changes
recovery, or permits primary-only login. After enablement, rollback means a
forward fix or an image at or above the v0.4.3 fence.

The only intentional loss is ephemeral: starting a new enrollment consumes the
prior incomplete enrollment, and cleanup removes expired or consumed enrollment
rows. Active credentials and recovery codes are unchanged until verified
completion or deliberate removal.

## Verification and release

GitHub CI owns every build, test, lint, database, migration, browser,
infrastructure, Semgrep, and gitleaks gate. It covers profile bounds, replay,
races, strict JSON, mail constraints, policy-handle collision, ciphertext and
readiness failure, rotation, recovery, epochs, flags, locales, and old clients.
Production deploys valid keys with enrollment off, proves compatibility, raises
the serialized floor to v0.4.3, enables enrollment, and proves the flow with the
authorized fictional account. Only the release plan's bounded browser exception
may inspect production; it starts no local stack and retains no secret evidence.
