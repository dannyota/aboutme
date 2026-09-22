# Authenticator-app key management

Status: Proposed for v0.4.3 with the
[authenticator-app contract](totp-second-factor-contract.md), which owns every
other TOTP rule.

This document fixes how TOTP secrets are sealed, how the key ring is configured
and rotated, and how a key failure stays confined to TOTP.

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

Production stores two protected parameters, `totp/key-a` and `totp/key-b`, in
the existing task-secret prefix. Nonsecret OpenTofu variables choose the slots:
`totp_active_key_slot` is `a` or `b` and defaults to `a`;
`totp_previous_key_slot` is empty, `a`, or `b`, defaults to empty, and must
differ from the active slot. The app and re-encryption task definitions inject
`TOTP_ACTIVE_KEY` from the active slot. They inject `TOTP_PREVIOUS_KEY` only
when the previous slot is set, so an absent previous key names no parameter and
ECS never fails on a missing one. Only the app execution role may read the two
exact slot ARNs. Scheduled jobs receive none. `secrets.sh totp-key <a|b>` writes
a fresh random value without printing it and refuses a slot that the running app
task definition names.

The one-shot path is `/usr/local/bin/server totp-key-reencrypt` in task family
`aboutme-prod-totp-reencrypt`, which uses the app execution and task roles and
the same injection. The
[release-fence contract](passkey-release-fence.md#authenticator-app-key-re-encryption)
defines its `totp_reencrypt` operation. New credentials, enrollments, and
replacements use the active key. Verification under the previous key re-encrypts
the credential under the active key in the same transaction.

## Rotation

Rotation starts only when the running app task definition names no previous slot
and a re-encryption run reports zero rows off the active key. The steps below
assume active slot `a`; swap the letters otherwise. Each redeploy runs the
running tag through a serialized `deploy.sh` operation.

1. Run `secrets.sh totp-key b`. No task names slot `b`, so a task restart at any
   point is unaffected.
2. Set `totp_previous_key_slot = "b"`, apply the reviewed OpenTofu plan, and
   redeploy. Tasks can now read `b` but never write it.
3. Set `totp_active_key_slot = "b"` and `totp_previous_key_slot = "a"`, apply,
   and redeploy. During rollout, step 2 tasks still write under `a`, which the
   new tasks read as previous, and read rows the new tasks write under `b`.
4. Run `deploy.sh --totp-key-reencrypt <tag>` until it reports zero credential
   rows and zero unconsumed, unexpired enrollment rows off the active key.
5. At least ten minutes after the step 3 deploy completes, so every enrollment
   an older task sealed has expired or been rewritten, run step 4 again. Repeat
   until it reports zero.
6. Set `totp_previous_key_slot = ""`, apply, and redeploy.
7. Run `secrets.sh totp-key a` to replace the retired key with an unused random
   value.

Steps 1 and 7 write only a parameter that no running task names. Steps 2, 3, and
6 change keys only through a new task definition, so a restart between writes
starts a revision whose key set is complete. Restoring a revision from before
step 3 after step 7 fails closed for TOTP only.

The re-encryption command uses the same authenticated decrypt and encrypt paths,
row locks, and a key-ID compare before update. Each transaction locks and
rewrites at most 200 rows, and one run processes at most 10,000 rows in 30
minutes. It reports counts and row IDs only, never keys, secrets, nonces,
ciphertext, email, or account IDs. A run with nothing to rewrite only counts. A
decrypt failure leaves the row unchanged, emits the signal below, and fails the
run.

## Key failures

A stored key ID outside the ring or an AES-GCM authentication failure fails
closed for that TOTP row only. Verification, enrollment completion, and
re-encryption return `503 authentication_unavailable` and count no failure.
Removal and state reads need no decryption and keep working. Passkeys, recovery
codes, accounts without TOTP, and `/readyz` are unaffected, so a Route 53 health
alarm never fires for a TOTP key problem. `internal/publicstate/readiness.go`
has no TOTP input.

The server logs the fixed JSON message `totp_unavailable` with a closed `reason`
of `unknown_key_id`, `key_id_overflow`, or `decrypt_failed`. Only
`decrypt_failed` adds the record kind and internal row ID. Each reason logs at
most once a minute per process. At startup and every five minutes, a bounded
query reads at most three distinct key IDs across credentials and unexpired
enrollments without decrypting. An ID outside the ring logs `unknown_key_id`; a
third ID logs `key_id_overflow`. A CloudWatch metric filter on the app log group
matches `{ $.msg = "totp_unavailable" }`. Alarm `aboutme-prod-totp-unavailable`
notifies the existing alert topic when the sum is at least one in five minutes.

No failure falls back to plaintext, a default key, unchecked code, or
recovery-only enforcement. Passkey and recovery verification do not decrypt TOTP
state. Key values never enter source, OpenTofu state, plans, command arguments,
logs, metrics, documentation, CI artifacts, or conversation output.
