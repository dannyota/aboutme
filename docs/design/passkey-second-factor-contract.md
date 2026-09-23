# Passkey second-factor contract

Status: Approved for v0.4.2 under
[ADR 0048](../adr/0048-passkey-second-factor-authentication.md).

## Release surface

V0.4.2 adds passkeys and recovery codes only. It adds
`passkeyEnrollment: boolean` to `GET /api/v1/capabilities`. The field is true
only when passkey enrollment is enabled. The response has no `totpEnrollment`
field.

Registration options and completion both recheck the enrollment flag. If the
flag is false, options create nothing. Completion consumes a matching ceremony,
stores no credential, and returns the uniform `404 not_found`. Assertion,
recovery, removal, regeneration, and state routes ignore the enrollment flag.

`GET /api/v1/me/second-factor` has exactly these data fields:

```json
{
  "data": {
    "enabled": true,
    "passkeys": [
      {
        "id": "01900000-0000-7000-8000-000000000001",
        "createdAt": "2026-09-20T09:00:00Z",
        "lastUsedAt": null
      }
    ],
    "recoveryCodesRemaining": 10
  }
}
```

`passkeys` uses `(created_at, id)` order. `enabled` is derived from the active
factor count in the same read-only snapshot as `passkeys` and
`recoveryCodesRemaining`, never read from stored state on its own. The invariant
behind it is that the factor-policy row exists exactly while an active factor
exists, so all three fields describe one consistent instant. An unenrolled
account returns false, an empty array, and zero. V0.4.2 does not register any
TOTP verification, enrollment, replacement, or removal route. The state response
has no `totpEnabled` field, and pending method values never include `totp`.
V0.4.5 adds those fields and routes as an additive contract change.

Every response in this file uses `Cache-Control: no-store, no-transform`. JSON
follows the standard `{data:...}` or `{error:{code,message}}` envelope. One-time
plaintext appears only in the response that creates it.

## Primary authentication

`POST /api/v1/auth/password/login` keeps `email` and `password` and adds an
optional `next` string. The server validates `next` with the provider-login
return-path parser before storing it. Missing or invalid input becomes
`/app/resumes`.

Successful primary authentication for an unenrolled account keeps the existing
`204` response and session cookie. An enrolled account receives no session and
returns:

```json
{ "data": { "secondFactorRequired": true } }
```

The status is `202`. The server sets `__Host-auth-pending` and does not return a
redirect target. The web navigates to `/login/second-factor`, whose status read
returns the validated path.

`POST /api/v1/auth/password/reauth` behaves the same for an enrolled account,
except the pending row has purpose `reauth`, binds the concrete live session,
and fixes its return path to `/app/settings/sessions`. It does not update either
verification timestamp before factor completion. An unenrolled account keeps the
existing `204` response.

A successful provider callback for an enrolled login clears `__Host-oauth-tx`,
sets the pending cookie, creates no session, and redirects to
`/login/second-factor` with `302`. The pending row copies the validated return
path from the consumed provider transaction. For enrolled `purpose=reauth`, it
binds the current live session, fixes the return path to
`/app/settings/sessions`, leaves both timestamps unchanged, and redirects to the
factor page. A callback that cannot create the pending row uses `auth_failed` in
the existing purpose error redirect and creates no authority.

Provider rejection and expired provider transactions keep their existing
redirects: login goes to `/login?error=<code>`, and reauthentication goes to
`/app/settings/sessions?error=<code>`. If pending status later returns `401`,
the web clears its local flow. It redirects to settings with
`error=authentication_required` when `/me` still succeeds, otherwise to the
login page with that error.

After valid primary authentication, a new pending cookie consumes the pending
row named by the browser's previous valid pending cookie. It does not consume
pending rows held by other browsers. An unenrolled successful login clears a
present pending cookie. Primary-authentication failure neither creates nor
replaces a pending row.

`sessions.reauthenticated_at` remains the primary-proof timestamp.
`sessions.second_factor_verified_at` is the factor-proof timestamp. Login
completion for an enrolled account creates a session with both set to the
completion time. Reauthentication completion updates both on the bound session
to the completion time. Unenrolled login and reauthentication leave
`second_factor_verified_at` null.

## Pending status and completion

`GET /api/v1/auth/second-factor` authenticates only the pending cookie. A reauth
row also requires its bound session cookie to remain live, current-epoch, and
owned by the same account. A `200` success is:

```json
{
  "data": {
    "purpose": "login",
    "methods": ["passkey", "recovery"],
    "expiresAt": "2026-09-20T09:05:00Z",
    "returnPath": "/app/resumes",
    "csrfToken": "43-character-unpadded-base64url-value"
  }
}
```

`purpose` is `login` or `reauth`. `methods` has fixed order `passkey`, then
`recovery`. Passkey is present when the account has an active passkey; recovery
is present only while a code remains. An enrolled v0.4.2 account with no
available method is corrupt state and returns `503 authentication_unavailable`.
Times are UTC RFC 3339 strings. The CSRF token is the pending row's 32-byte
secret encoded as 43-character unpadded base64url.

A `401 authentication_required` status or completion response clears the pending
cookie. A request to a TOTP path reaches the existing uniform `404 not_found`;
no pending route accepts an arbitrary method name.

Every pending POST requires `Content-Type: application/json`, exact Origin,
`X-CSRF-Token`, and the pending cookie. Reauth also requires the bound session
cookie. The checks run in this order: media type, bounded body, strict JSON,
pending authentication, Origin and CSRF, rate admission, then method work.

| Condition                                                            | Status and error code                 |
| -------------------------------------------------------------------- | ------------------------------------- |
| Unsupported media                                                    | `415 media_type_unsupported`          |
| Body over its route limit                                            | `413 body_too_large`                  |
| Duplicate, unknown, missing, malformed, or noncanonical field        | `400 request_invalid`                 |
| Absent, expired, consumed, wrong-epoch, or wrong-session pending row | `401 authentication_required`         |
| Wrong Origin or pending CSRF                                         | `403 csrf_rejected`                   |
| Unknown, expired, consumed, foreign, or wrong-purpose ceremony       | `400 challenge_invalid`               |
| Assertion options for an account with no active passkey              | `404 factor_not_found`                |
| Well-formed but invalid assertion or recovery code                   | `401 verification_failed`             |
| Rate rejection                                                       | `429 rate_limited` plus `Retry-After` |
| Dependency or corrupt method state                                   | `503 authentication_unavailable`      |

The fifth failed completion returns `401 verification_failed`, atomically
consumes the pending row, records its notification, and clears the cookie. From
v0.4.5, that notification obeys a per-account cap of one attempt mail per hour;
a suppressed mail never suppresses the state change. Later use returns
`401 authentication_required`. Every successful completion returns `204`,
consumes the pending row, and clears the cookie. Login completion sets the
session cookie; reauth completion never issues a different session.

Creating a pending row or WebAuthn ceremony first makes one best-effort cleanup
call for each table. Each call deletes at most 200 expired rows in
`(expires_at, id)` order using database time. Failure writes one fixed warning
and does not fail the request or readiness because every read still checks
expiry. Creation is rate-limited, so each admitted request can delete more rows
than it adds. No traffic adds no expiring rows and therefore needs no timer.

## WebAuthn JSON

All binary WebAuthn values use canonical unpadded base64url strings. Null is
accepted only for assertion `userHandle`. Fields described as omitted must not
be sent as null. Objects reject duplicate and unknown fields. Credential `id`
must be the canonical encoding of the same bytes as `rawId`.

`POST /api/v1/me/second-factor/passkeys/options` returns `200` registration
options. `POST /api/v1/auth/second-factor/passkey/options` returns `200`
assertion options. Both accept exactly `{}` as their JSON body. Both responses
have this outer shape:

```json
{ "data": { "ceremonyId": "43-character-token", "publicKey": {} } }
```

`ceremonyId` is an independent 32-byte random value. A registration `publicKey`
contains exactly:

- `challenge`: 32 random bytes;
- `rp`: `{name:"aboutme",id:<canonical host>}`;
- `user`:
  `{id:<32-byte handle>,name:<canonical email>,displayName:<account name>}`;
- `pubKeyCredParams`:
  `[{type:"public-key",alg:-7},{type:"public-key",alg:-257}]`;
- `timeout`: `300000` milliseconds;
- `excludeCredentials`: every active descriptor;
- `authenticatorSelection`:
  `{residentKey:"required",requireResidentKey:true,userVerification:"required"}`;
  and
- `attestation`: `"none"`.

The server asks for `attestation: "none"` but does not require it: with no FIDO
metadata service configured, go-webauthn cryptographically verifies whichever
format `attestationObject` declares (`packed`, `tpm`, `android-key`,
`android-safetynet`, `apple`, `fido-u2f`, `compound`) without checking its
certificate chain against a trusted root, and accepts any self-consistent
statement. A non-`none` format is therefore verified and accepted, never
rejected or normalized to `none`. No attestation format or type is stored.

An assertion `publicKey` contains exactly `challenge`, `timeout: 300000`,
`rpId`, `allowCredentials`, and `userVerification: "required"`.
`allowCredentials` contains every active passkey and is never empty. When the
pending account has no active passkey, for example when another factor type
enforces it, assertion options return `404 factor_not_found`. That response
creates no ceremony and leaves the pending row and its failure count unchanged.

A credential descriptor has required `type: "public-key"` and `id` fields.
`transports` is omitted when no hint is stored, never null, and otherwise is a
unique array in the fixed order `usb`, `nfc`, `ble`, `smart-card`, `hybrid`,
`internal`. Transport never changes authorization. No options object contains
`extensions` or `hints`.

Registration completion accepts exactly:

```json
{
  "ceremonyId": "43-character-token",
  "credential": {
    "id": "base64url",
    "rawId": "base64url",
    "type": "public-key",
    "response": {
      "clientDataJSON": "base64url",
      "attestationObject": "base64url",
      "transports": ["internal"]
    },
    "clientExtensionResults": {}
  }
}
```

Assertion completion accepts the same outer credential fields with this response
instead:

```json
{
  "clientDataJSON": "base64url",
  "authenticatorData": "base64url",
  "signature": "base64url",
  "userHandle": null
}
```

`transports` is required on registration and may be empty. `userHandle` is
required and may be null or at most 64 decoded bytes. An empty `userHandle`
string decodes exactly like `null`: no handle to compare, so it never matches
the stored one. `clientExtensionResults` is required and must be an empty object
because v0.4.2 requests no extensions. The field-size and body limits come from
[Numeric budgets](budgets.md).

## Management responses and recovery download

First passkey completion returns `201`:

```json
{
  "data": {
    "passkey": {
      "id": "01900000-0000-7000-8000-000000000001",
      "createdAt": "2026-09-20T09:00:00Z",
      "lastUsedAt": null
    },
    "recoveryCodes": ["amr_00000-00000-00000-00000-00000-0"]
  }
}
```

The real array has ten codes. Completion decides first versus later from factor
policy existence, not from passkey count. Completion omits `recoveryCodes` when
the policy already exists and never sends null or an empty array. Passkey
removal returns `204`; an invalid, missing, foreign, or already removed internal
ID returns `404 factor_not_found`. Recovery regeneration returns
`200 {"data":{"recoveryCodes":[...]}}` with ten new codes.

Registration options and registration completion both return
`409 passkey_limit_reached` once an account already holds five active passkeys,
the [budgeted](budgets.md) maximum. Options refuses a sixth ceremony before any
device interaction; completion rechecks the count again at commit, so a race
between two concurrently started ceremonies cannot create a sixth credential.
Registration completion returns `400 verification_failed` when WebAuthn
verification of the submitted credential fails, covering ceremony type, exact
origin, challenge, RP ID hash, public-key algorithm, user presence and
verification flags, the backup-eligible/backup-state consistency check, and
signature validity, and also when the verified credential ID already exists for
any account. In every `verification_failed` case the claimed ceremony stays
consumed, so the same ceremony ID cannot be retried.

Passkey removal decides final versus non-final only from the shared count of
active factors of every type, taken under the user lock as
[Second-factor authentication](second-factor-authentication.md#enrollment-and-management)
defines. A handler never counts passkeys alone.

Every successful passkey add or removal and recovery regeneration delivers the
replacement `__Host-session` cookie. First enrollment preserves the prior
primary-proof time and sets factor proof to completion. Later add and
regeneration preserve both proof times. Removal preserves both while another
active factor of any type remains. Removal of the final active factor clears
factor proof. Every replacement preserves the prior session creation time,
absolute expiry, and device metadata. The web refreshes `/me` for the
replacement CSRF token before enabling another session mutation.

Pending passkey verification uses the assertion completion body. Pending
recovery verification uses exactly `{"code":"<recovery-code>"}`. Management
mutations use the session CSRF token from `/me`; pending mutations use the
pending token.

The API returns codes only as JSON. The web creates the optional download from
the array without another request. It selects the heading from the current
interface locale. English bytes are:

```text
aboutme recovery codes

<code 1>
...
<code 10>
```

Vietnamese bytes replace the first line with:

```text
Mã khôi phục aboutme
```

Both are UTF-8 without a byte-order mark, use LF, and end with one LF. The MIME
type is `text/plain;charset=utf-8`; the name is `aboutme-recovery-codes.txt`.
The web revokes the object URL in a `finally` path. Closing the dialog or
completing download clears the codes and blob from component state; neither
enters browser persistence.

## Transactional mail events

V0.4.2 adds these closed `auth_email_jobs.kind` values:

- `second_factor_enabled` when passkey completion creates the factor policy;
- `passkey_added` when enforcement already exists;
- `passkey_removed` when another active factor of any type remains;
- `second_factor_disabled` when the removed passkey was the final active factor;
- `recovery_codes_regenerated`;
- `recovery_code_used`; and
- `second_factor_attempts_exhausted` on the fifth failed completion, at most one
  per account per hour from v0.4.5.

Each uses `user_id`, no registration or reset scope, no token digest, and an
expiry exactly 24 hours after the event. The encrypted strict JSON payload is:

```json
{
  "version": 2,
  "to": "canonical@example.com",
  "occurredAt": "2026-09-20T09:00:00Z",
  "remainingRecoveryCodes": 9
}
```

`remainingRecoveryCodes` is required only for `recovery_code_used` and omitted
for every other kind. `occurredAt` is the transaction clock rounded to whole UTC
seconds. Existing verify, reset, and password-change jobs remain payload version
1; the worker accepts both closed versions. Every security template is fixed
bilingual copy, Vietnamese first and English second, so no locale field exists.
No security event contains a link, factor material, passkey identifier, IP, user
agent, or account ID in plaintext or ciphertext.

The security mutation or terminal failed-attempt update and its outbox insert
use one transaction. Insert or encryption failure aborts the state change.

## Counter security events

Migration 00004 adds `authentication_security_events`. It stores `id`, fixed
kind `passkey_counter_non_increasing`, `user_id`, internal `passkey_id`,
unsigned 32-bit `stored_counter` and `received_counter` values in SQL `bigint`,
and `occurred_at`. The user foreign key uses `ON DELETE CASCADE`; passkey ID
remains an opaque value after factor removal. The table stores no credential ID,
account address, IP, user agent, challenge, or assertion bytes.

A non-increasing nonzero counter claims and consumes the ceremony, increments
the pending failure count, leaves the credential counter unchanged, and inserts
one event in the same transaction. The committed response is
`401 verification_failed`. Insert failure rolls back all those writes and
returns `503 authentication_unavailable`; it never authenticates. The existing
daily privacy-retention command deletes events older than 180 days in its
bounded 1,000-row pages and 10,000-row run ceiling. Logs name only the event ID
and fixed kind.

## PostgreSQL shape

Migration 00004 uses `bigint` epochs with nonnegative checks. It adds
`users.auth_epoch`, `sessions.auth_epoch`, nullable
`sessions.second_factor_verified_at`, `oauth_grants.auth_epoch`, and required
`oauth_authorization_codes.grant_id` and `.auth_epoch`.

- `second_factor_policies`: `user_id` primary key, unique 32-byte
  `webauthn_user_handle`, and `enabled_at`;
- `webauthn_credentials`: UUIDv7 `id`, `user_id`, globally unique 16 to 1,023
  byte `credential_id`, 1 to 2,048 byte `public_key`, unsigned-32-bit
  `sign_count` in `bigint`, `backup_eligible`, `backup_state`, closed canonical
  `transports`, `created_at`, and nullable `last_used_at`;
- `second_factor_recovery_codes`: UUIDv7 `id`, `user_id`, unique 32-byte
  `code_digest`, and `created_at`; successful use deletes the row;
- `pending_authentications`: UUIDv7 `id`, unique 32-byte `token_digest`, 32-byte
  `csrf_secret`, `user_id`, purpose `login` or `reauth`, `auth_epoch`, nullable
  `session_id`, `primary_verified_at`, `return_path`, `failed_attempts`,
  `created_at`, `expires_at`, and nullable `consumed_at`;
- `webauthn_ceremonies`: UUIDv7 `id`, unique 32-byte `token_digest`, 32-byte
  `challenge_digest`, `user_id`, purpose `registration` or `assertion`,
  `auth_epoch`, nullable session and pending bindings, nullable 32-byte
  `proposed_user_handle`, `created_at`, `expires_at`, and nullable
  `consumed_at`; and
- `authentication_security_events`: the fields and bounds in Counter security
  events, with an `(occurred_at,id)` retention index.

All user foreign keys use `ON DELETE CASCADE`. Pending and ceremony tables have
`(expires_at,id)` cleanup indexes. Credential listing uses
`(user_id,created_at,id)`. Recovery lookup uses `(user_id,code_digest)`.
Database checks enforce byte lengths, closed purposes, expiry after creation,
failed attempts from zero through five, `backup_state` only when eligible, and
counters from zero through 4,294,967,295. Login pending rows have no session;
reauth rows require one. Registration ceremonies require a session and no
pending row; assertion ceremonies require a pending row and no session. No TOTP
table is present.

## Migration and mixed versions

Migration `00004_passkey_second_factor.sql` is the only v0.4.2 migration. It
deletes all outstanding OAuth authorization codes before adding required
`grant_id` and `auth_epoch` bindings. Existing users, sessions, and grants
receive epoch zero. The only data loss is authorization codes with at most 60
seconds of life; clients restart authorization. No grant, token, account,
session, resume, or mail job is deleted.

The migration's insert trigger fills an omitted code `grant_id` from the unique
live `(user_id,client_id)` grant and copies the user's epoch. This lets the old
code issuer run before enrollment despite both columns being `NOT NULL`. New
code supplies both fields. Mixed-version startup reports passkey enrollment
false, and no factor may exist before the fence excludes old session issuers.

## Production minimum-release fence

The [passkey release-fence contract](passkey-release-fence.md) owns the DynamoDB
item, serialized deployment operation, IAM roles, failure order, activation
sequence, and privileged bypass boundary.
