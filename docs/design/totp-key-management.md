# Authenticator-app key management

This design fixes how TOTP secrets are sealed, how the key ring is configured
and rotated, and how a key failure stays confined to TOTP. The
[authenticator-app contract](totp-second-factor-contract.md) owns every other
TOTP rule, and the [TOTP key runbook](../runbooks/totp-keys.md) holds the
Vietnam operator procedures. The [AWS reference](aws-totp-key-management.md)
owns ECS injection, task execution, and AWS monitoring.

## Sealing

TOTP secrets must be recoverable for verification. PostgreSQL stores only
AES-256-GCM ciphertext, a random 96-bit nonce, a key identifier, and format
version. Format version 1 ciphertext is exactly 36 bytes for the 20-byte secret
and 16-byte tag.

Associated data concatenates ASCII `aboutme.totp.v1`, zero, record kind
`credential` or `enrollment`, zero, raw 16-byte account UUID, zero, raw 16-byte
row UUID, zero, ASCII format-version decimal, zero, and ASCII key ID. Moving
ciphertext between any of those bindings fails authentication.

The application generates each credential and enrollment UUIDv7 with its
injected generator, seals with that ID, and inserts that exact ID. Completion
opens the enrollment ciphertext under its enrollment binding, then seals the
secret again with a fresh nonce under the active key and the credential binding:
a new ID on first install, or the existing ID on replacement. Enrollment
ciphertext is never copied into a credential. Lazy and proactive re-encryption
keep the row ID and use a fresh nonce.

## Key ring

Runtime configuration provides `TOTP_ACTIVE_KEY` and optional
`TOTP_PREVIOUS_KEY`, each 32 bytes in canonical 43-character unpadded base64url.
A key ID is derived, never configured: ASCII `tk1_` followed by the canonical
unpadded base64url of the first 16 bytes of SHA-256 over ASCII
`aboutme.totp.key-id.v1`, one zero byte, and the 32 key bytes. It is 26 bytes
and binds each stored ID to one key value. Startup fails when the active key is
missing or malformed, the previous value is present but malformed, or both keys
derive the same ID.

See [AWS key injection](aws-totp-key-management.md#key-ring) for the ECS
reference.

## Rotation

See [AWS rotation](aws-totp-key-management.md#rotation) for the ECS reference.

Vietnam key rotation remains an
[open host procedure](../runbooks/totp-keys.md#key-rotation).

New credentials, enrollments, and replacements use the active key. Verification
under the previous key re-encrypts the credential under the active key in the
same transaction.

The re-encryption command uses the same authenticated decrypt and encrypt paths
and a key-ID compare before update. It takes no user lock. Each transaction
handles one record kind, credentials first and then enrollments, and locks at
most 200 rows off the active key in `id` order with `FOR UPDATE SKIP LOCKED`. A
skipped row belongs to a live factor transaction and waits for a later batch, so
the command never waits on a lock that a user, session, or policy lock precedes.
It rewrites unexpired enrollments and deletes expired ones without decrypting.
One run processes at most 10,000 rows in 30 minutes. It reports counts and row
IDs only, never keys, secrets, nonces, ciphertext, email, or account IDs. A run
with nothing to rewrite only counts. Batches skip rows another transaction
holds, so the reported off-key totals come from an unlocked `count(*)` of every
credential and enrollment row off the active key, taken after the batches.

A decrypt failure leaves the row unchanged, emits the signal below, and fails
the run, so rotation stops with the previous key still configured and nothing
else broken. The owner of that account can replace or remove TOTP after proving
a passkey or recovery code, because neither action decrypts the row. That
removes the blocking row. No operator deletes or rewrites a credential
([ADR 0003](../adr/0003-public-namespace-and-no-operator-surface.md)).

## Key failures

See [AWS monitoring](aws-totp-key-management.md#key-failures) for the ECS
reference.

A stored key ID outside the ring or an AES-GCM authentication failure fails
closed for that TOTP row only. Verification, enrollment completion, and
re-encryption return `503 authentication_unavailable` and count no failure.
Removal and state reads need no decryption and keep working. Passkeys, recovery
codes, accounts without TOTP, and `/readyz` are unaffected.
`internal/publicstate/readiness.go` has no TOTP input.

The server logs the fixed JSON message `totp_unavailable` with a closed `reason`
of `unknown_key_id`, `key_id_overflow`, or `decrypt_failed`. Only
`decrypt_failed` adds the record kind and internal row ID. Each reason logs at
most once a minute per process. At startup and every five minutes, a bounded
query reads at most three distinct key IDs across credentials and unexpired
enrollments without decrypting. An ID outside the ring logs `unknown_key_id`; a
third ID logs `key_id_overflow`.

No failure falls back to plaintext, a default key, unchecked code, or
recovery-only enforcement. Passkey and recovery verification do not decrypt TOTP
state. Key values never enter source, OpenTofu state, plans, command arguments,
logs, metrics, documentation, CI artifacts, or conversation output.
