# Passkey and pending-authentication contract

This contract fixes the wire shapes for primary login with a second factor, the
shared pending routes, passkeys, recovery codes, and the shared security-mail
events ([ADR 0048](../adr/0048-passkey-second-factor-authentication.md)). The
[second-factor design](second-factor-authentication.md) owns the rules. The
[authenticator-app contract](totp-second-factor-contract.md) adds the TOTP
routes. [Numeric budgets](budgets.md) owns every size and rate.

Every response here sends `Cache-Control: no-store, no-transform` and uses the
standard `{data:...}` or `{error:{code,message}}` envelope. One-time plaintext
appears only in the response that creates it.

## Release surface

`GET /api/v1/capabilities` reports `passkeyEnrollment: boolean` and
`totpEnrollment: boolean`, each true only while that enrollment flag is on.
While `passkeyEnrollment` is false, registration options return the uniform
`404 not_found` before any work. Registration completion consumes a matching
ceremony, stores no credential, and returns the same `404`. Assertion, recovery,
removal, regeneration, and state routes ignore the flag.

`GET /api/v1/me/second-factor` returns exactly:

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
    "totpEnabled": false,
    "recoveryCodesRemaining": 10
  }
}
```

`passkeys` uses `(created_at, id)` order. `enabled` comes from the active-factor
count in the same read-only snapshot as the other fields, never from stored
state alone. An unenrolled account returns false, an empty array, false, and
zero. The web treats an absent or malformed `totpEnabled` or `totpEnrollment` as
false.

## Primary authentication

`POST /api/v1/auth/password/login` takes `email`, `password`, and an optional
`next`. The server validates `next` with the provider-login return-path parser;
missing or invalid input becomes `/app/resumes`.

An unenrolled account keeps the `204` response and session cookie. An enrolled
account gets no session and a `202`:

```json
{ "data": { "secondFactorRequired": true } }
```

The server sets `__Host-auth-pending` and returns no redirect target. The web
navigates to `/login/second-factor`, whose status read returns the validated
path.

`POST /api/v1/auth/password/reauth` behaves the same for an enrolled account,
except the pending row has purpose `reauth`, binds the live session, and fixes
its return path to `/app/settings/sessions`. It changes neither proof time
before factor completion. An unenrolled account keeps `204`.

A successful provider callback for an enrolled login clears `__Host-oauth-tx`,
sets the pending cookie, creates no session, and redirects with `302` to
`/login/second-factor`. The pending row copies the return path from the consumed
provider transaction. For enrolled `purpose=reauth`, it binds the live session,
fixes the return path to `/app/settings/sessions`, and leaves both proof times
unchanged. A callback that cannot create the pending row uses `auth_failed` in
the existing purpose error redirect.

Provider rejection and expired transactions keep their redirects:
`/login?error=<code>` for login and `/app/settings/sessions?error=<code>` for
reauthentication. When pending status later returns `401`, the web clears its
flow and redirects to settings with `error=authentication_required` if `/me`
still succeeds, otherwise to the login page with that error.

After valid primary authentication, a new pending cookie consumes the row named
by the same browser's previous valid pending cookie, not rows held by other
browsers. A successful unenrolled login clears a present pending cookie. A
failed primary authentication neither creates nor replaces a pending row.

`sessions.reauthenticated_at` is the primary-proof time and
`sessions.second_factor_verified_at` the factor-proof time. Unenrolled login and
reauthentication leave factor proof null.

## Pending status and completion

`GET /api/v1/auth/second-factor` authenticates only the pending cookie. A
`reauth` row also requires its bound session cookie to stay live, current-epoch,
and owned by the same account. Success is `200`:

```json
{
  "data": {
    "purpose": "login",
    "methods": ["passkey", "totp", "recovery"],
    "expiresAt": "2026-09-20T09:05:00Z",
    "returnPath": "/app/resumes",
    "csrfToken": "43-character-unpadded-base64url-value"
  }
}
```

`methods` uses the fixed order `passkey`, `totp`, `recovery` and omits a method
the account lacks: `passkey` needs an active passkey, `totp` an active TOTP
credential, and `recovery` an unused code. An enrolled account with no method is
corrupt state and returns `503 authentication_unavailable`. Times are UTC
RFC 3339. The CSRF token is the row's 32-byte secret as 43-character unpadded
base64url. A `401 authentication_required` from status or completion clears the
pending cookie. No route accepts an arbitrary method name.

Every pending `POST` requires `Content-Type: application/json`, exact Origin,
`X-CSRF-Token`, and the pending cookie; `reauth` also requires the bound session
cookie. Checks run in this order: media type, body size, strict JSON, pending
authentication, Origin and CSRF, rate admission, then method work.

| Condition                                                            | Status and error code                 |
| -------------------------------------------------------------------- | ------------------------------------- |
| Unsupported media                                                    | `415 media_type_unsupported`          |
| Body over its route limit                                            | `413 body_too_large`                  |
| Duplicate, unknown, missing, malformed, or noncanonical field        | `400 request_invalid`                 |
| Absent, expired, consumed, wrong-epoch, or wrong-session pending row | `401 authentication_required`         |
| Wrong Origin or pending CSRF                                         | `403 csrf_rejected`                   |
| Unknown, expired, consumed, foreign, or wrong-purpose ceremony       | `400 challenge_invalid`               |
| A method the account lacks                                           | `404 factor_not_found`                |
| Well-formed but invalid assertion, code, or recovery code            | `401 verification_failed`             |
| Rate rejection                                                       | `429 rate_limited` plus `Retry-After` |
| Dependency or corrupt method state                                   | `503 authentication_unavailable`      |

A `404 factor_not_found` creates no ceremony and leaves the pending row and its
failure count unchanged. The fifth failed completion returns
`401 verification_failed`, consumes the pending row, records the exhaustion mail
subject to the per-account cap, and clears the cookie. Later use returns
`401 authentication_required`. Success returns `204`, consumes the row, and
clears the cookie. Login completion sets the session cookie; reauth completion
never issues a different session.

Creating a pending row or WebAuthn ceremony first makes one best-effort cleanup
call per table, deleting at most 200 expired rows in `(expires_at, id)` order by
database time. A cleanup failure logs one fixed warning and fails neither the
request nor readiness, because every read checks expiry. Creation is
rate-limited, so each admitted request can delete more rows than it adds.

Pending passkey verification uses the assertion completion body. Pending
recovery verification takes exactly `{"code":"<recovery-code>"}`.

## WebAuthn JSON

Binary WebAuthn values are canonical unpadded base64url strings. Null is
accepted only for assertion `userHandle`. Omitted fields must not be sent as
null. Objects reject duplicate and unknown fields. Credential `id` must be the
canonical encoding of the same bytes as `rawId`.

`POST /api/v1/me/second-factor/passkeys/options` returns registration options
and `POST /api/v1/auth/second-factor/passkey/options` returns assertion options.
Both take exactly `{}` and return `200`:

```json
{ "data": { "ceremonyId": "43-character-token", "publicKey": {} } }
```

`ceremonyId` is an independent 32-byte random value. A registration `publicKey`
holds exactly:

- `challenge`: 32 random bytes;
- `rp`: `{name:"aboutme",id:<canonical host>}`;
- `user`:
  `{id:<32-byte handle>,name:<canonical email>,displayName:<account name>}`;
- `pubKeyCredParams`:
  `[{type:"public-key",alg:-7},{type:"public-key",alg:-257}]`;
- `timeout`: `300000`;
- `excludeCredentials`: every active descriptor;
- `authenticatorSelection`:
  `{residentKey:"required",requireResidentKey:true,userVerification:"required"}`;
- `attestation`: `"none"`.

The server asks for `none` but does not require it. With no FIDO metadata
service configured, go-webauthn verifies whichever format the
`attestationObject` declares without checking its certificate chain, and accepts
any self-consistent statement. No attestation format is stored.

An assertion `publicKey` holds exactly `challenge`, `timeout: 300000`, `rpId`,
`allowCredentials`, and `userVerification: "required"`. `allowCredentials` lists
every active passkey and is never empty; an account with no passkey gets
`404 factor_not_found` instead.

A descriptor has `type: "public-key"` and `id`. `transports` is omitted when no
hint is stored, never null, and otherwise is a unique array in the order `usb`,
`nfc`, `ble`, `smart-card`, `hybrid`, `internal`. No options object contains
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

Assertion completion uses the same outer fields with this `response`:

```json
{
  "clientDataJSON": "base64url",
  "authenticatorData": "base64url",
  "signature": "base64url",
  "userHandle": null
}
```

`transports` is required on registration and may be empty. `userHandle` is
required and is null or at most 64 decoded bytes; an empty string decodes like
null and never matches the stored handle. `clientExtensionResults` must be an
empty object.

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

The real array holds ten codes. `recoveryCodes` is present only when this
completion created the factor policy, and never null or empty. Recovery
regeneration returns `200 {"data":{"recoveryCodes":[...]}}` with ten new codes.
Passkey removal returns `204`; an invalid, missing, foreign, or removed ID
returns `404 factor_not_found`.

Registration options and completion both return `409 passkey_limit_reached` when
the account already holds the maximum number of passkeys. Completion rechecks
the count at commit, so two concurrent ceremonies cannot exceed it. Completion
returns `400 verification_failed` when WebAuthn verification fails (ceremony
type, origin, challenge, RP ID hash, algorithm, presence and verification flags,
backup-flag consistency, or signature) or the credential ID already exists on
any account. The claimed ceremony stays consumed in every failure.

Every successful add, removal, and regeneration delivers the replacement
`__Host-session` cookie under the proof-time rules of the
[assurance boundary](second-factor-authentication.md#assurance-boundary). The
web then refreshes `/me` for the new CSRF token before enabling another session
mutation. Management mutations use the session CSRF token; pending mutations use
the pending token.

The API returns codes only as JSON. The web builds the optional download from
the array without another request and picks the heading from the interface
locale. English bytes are:

```text
aboutme recovery codes

<code 1>
...
<code 10>
```

Vietnamese replaces the first line with `Mã khôi phục aboutme`. The file is
UTF-8 without a byte-order mark, uses LF, and ends with one LF. Its type is
`text/plain;charset=utf-8` and its name `aboutme-recovery-codes.txt`. The web
revokes the object URL in a `finally` path. Closing the dialog or finishing the
download clears the codes and blob; neither enters browser persistence.

## Transactional mail events

These closed `auth_email_jobs.kind` values cover passkeys and shared events:

- `second_factor_enabled` when a completion creates the factor policy;
- `passkey_added` when enforcement already exists;
- `passkey_removed` when another active factor remains;
- `second_factor_disabled` when the removed factor was the final one;
- `recovery_codes_regenerated`;
- `recovery_code_used`;
- `second_factor_attempts_exhausted` on the fifth failed completion or a TOTP
  cool-down start, at most one per account per hour.

Each job has `user_id`, no registration or reset scope, no token digest, and an
expiry 24 hours after the event. The encrypted strict JSON payload is:

```json
{
  "version": 2,
  "to": "canonical@example.com",
  "occurredAt": "2026-09-20T09:00:00Z",
  "remainingRecoveryCodes": 9
}
```

`remainingRecoveryCodes` appears only for `recovery_code_used`. `occurredAt` is
the transaction clock in whole UTC seconds. Verify, reset, and password-change
jobs stay payload version 1; the worker accepts both. Templates are fixed
bilingual copy, Vietnamese first, so there is no locale field. No event carries
a link, factor material, passkey ID, IP, user agent, or account ID.

The mutation or terminal failed-attempt update and its outbox insert share one
transaction; an insert or encryption failure aborts the state change.

## Counter security events

`authentication_security_events` stores `id`, the fixed kind
`passkey_counter_non_increasing`, `user_id`, the internal `passkey_id`,
`stored_counter` and `received_counter` (unsigned 32-bit values in `bigint`),
and `occurred_at`. The user key cascades on delete; `passkey_id` stays an opaque
value after removal. The table holds no credential ID, address, IP, user agent,
challenge, or assertion bytes.

A non-increasing nonzero counter consumes the ceremony, increments the pending
failure count, leaves the stored counter unchanged, and inserts one event in the
same transaction, then returns `401 verification_failed`. A failed insert rolls
all of it back and returns `503 authentication_unavailable`. The daily privacy
retention command deletes events older than 180 days. Logs name only the event
ID and kind.

## PostgreSQL shape

Migration `00004_passkey_second_factor.sql` adds nonnegative `bigint` epochs:
`users.auth_epoch`, `sessions.auth_epoch`, `oauth_grants.auth_epoch`, and
required `oauth_authorization_codes.grant_id` and `.auth_epoch`, plus nullable
`sessions.second_factor_verified_at`. Its relations are:

- `second_factor_policies`: `user_id` primary key, unique 32-byte
  `webauthn_user_handle`, `enabled_at`, and nullable `attempt_mail_at` (added by
  migration 00005);
- `webauthn_credentials`: UUIDv7 `id`, `user_id`, globally unique 16 to 1,023
  byte `credential_id`, 1 to 2,048 byte `public_key`, `sign_count`,
  `backup_eligible`, `backup_state`, closed canonical `transports`,
  `created_at`, and nullable `last_used_at`;
- `second_factor_recovery_codes`: UUIDv7 `id`, `user_id`, unique 32-byte
  `code_digest`, and `created_at`; use deletes the row;
- `pending_authentications`: UUIDv7 `id`, unique 32-byte `token_digest`, 32-byte
  `csrf_secret`, `user_id`, purpose `login` or `reauth`, `auth_epoch`, nullable
  `session_id`, `primary_verified_at`, `return_path`, `failed_attempts`,
  `created_at`, `expires_at`, and nullable `consumed_at`;
- `webauthn_ceremonies`: UUIDv7 `id`, unique 32-byte `token_digest`, 32-byte
  `challenge_digest`, `user_id`, purpose `registration` or `assertion`,
  `auth_epoch`, nullable session and pending bindings, nullable 32-byte
  `proposed_user_handle`, `created_at`, `expires_at`, and nullable
  `consumed_at`;
- `authentication_security_events` with an `(occurred_at,id)` retention index.

Every user foreign key cascades on delete. Pending and ceremony tables have
`(expires_at,id)` cleanup indexes; credential listing uses
`(user_id,created_at,id)`; recovery lookup uses `(user_id,code_digest)`. Checks
enforce byte lengths, closed purposes, expiry after creation, failed attempts
from zero through five, `backup_state` only when eligible, and counters from
zero through 4,294,967,295. Login pending rows have no session and reauth rows
require one. Registration ceremonies require a session and no pending row;
assertion ceremonies require a pending row and no session.

The migration deleted outstanding authorization codes (60-second lifetime)
before making `grant_id` and `auth_epoch` required; nothing else was deleted. An
insert trigger still fills an omitted code `grant_id` from the unique live
`(user_id, client_id)` grant and copies the user's epoch. Current code supplies
both fields.
