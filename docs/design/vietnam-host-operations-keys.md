# Vietnam host key operations

This page holds the secret custody and key rotation contracts of the
[host operations](vietnam-host-operations.md) design.

## Secret custody and maintenance

Keys are generated on-host as 32 random bytes encoded in canonical 43-character
base64url. Root owns `/etc/aboutme/secrets/` (0700), active files (0400),
`staging/<id>/` (0700), and `archive/<derived-id>` (0400). Only root may read
staging or archives. They are never directory-mounted into containers, included
by generic Podman sync, or passed to the application before activation. Reject
symlinks and unsafe modes before reads/writes. A request adopts its existing
staged key after a crash; it never generates a second key for the same intent.

Inject selected named Podman `type=env` secrets from files/stdin. Secret values
must never appear in command arguments, unit Environment lines, exported shell
variables, traces, diagnostics, environment dumps, journal, or core dumps. They
necessarily exist in the receiving process environment and memory; that process
and host root can read them. Disable core dumps for the runner and its
containers. Containers use no host PID namespace, privilege, Podman socket,
archive mount, or broad host mount; drop capabilities, set no-new-privileges,
read-only rootfs, 256 MiB memory and equal memory-swap limit, and no restart.
Only the socket and bounded tmpfs are mounted. Use `--log-driver=none` and
capture stdout privately before validation. Do not reuse job-run.sh wholesale.

Before activation, encrypt a stable snapshot of rotation keys and their staging
and archives in the closed
[recovery archive](vietnam-host-operations-recovery.md#recovery-archive) format
on-host to the configured age recipient. The identity stays off-host. New
`secrets-host.sh operation-copy <id>` takes no caller-selected recipient/path;
it emits only ciphertext. Root-owned 0600 `/etc/aboutme/operations.json` pins
the recipient as the sole `ageRecipient` field, copied from
`host/env/operations.json.example` only when missing. No identity is stored
there. The wrapper uses the owner's configured `ABOUTME_INFRA_DIR` outside the
checkout; no remote output path is accepted. The wrapper saves it to the owner's
configured `aboutme-infra` directory, mode 0600, fsyncs file and directory, and
sends `recovery-copy <id> <sha256>` to the runner. Hash the exported ciphertext,
not plaintext. The receipt must match the saved export hash and staged key ID.
Lost replies reuse that same export; export failure or missing receipt blocks
activation. Only an authenticated operator can attest off-host durability.

Before changing an active file, Podman secret, or flags, write
`/srv/data/host-operations/key-change.json` with request ID, saved release, and
stage. Fence checks reject every serving, migration, and job start while it
exists, including after reboot or if malformed. Missing data mounts fail these
checks closed. Maintenance may start. Only the owning runner may render saved
units under the marker; it may not start them. Generic secret import/replacement
and sync for these keys refuse outside this protocol, except missing-key
bootstrap before any serving release exists.

Stop timers, drain jobs, acquire the workload lock, start maintenance, stop
serving Caddy and Go, and prove units, containers, and their DB backends
stopped. Require an external marked 503 before active writes. Staging and
recovery export precede maintenance. New `deploy-host.sh <id> <tag> reload-live`
reconciles the saved digests, flags and secret metadata without migration,
backup, observer change, or nested deploy.sh. It renders under the owner
checkpoint, fsyncs `starting`, removes the marker only after all
files/secrets/units agree, then starts web/Go/Caddy and checks local readiness.
Finish by stopping maintenance, restoring timers and proving serving externally.
Failed starts retain the fence and maintenance; ordinary resume uses the same
keys and release, never old secrets. An incompatible image or defective runner
uses quiescent repair, without requiring successful startup or safe suspension.

Release the workload lock while awaiting a proof or ten-minute wait, retaining
the fence. Reacquire and revalidate before effects. The key marker blocks starts
only while configuration is inconsistent; a retained rotation lock alone must
allow the reconciled release to serve for sign-in proofs and owner recovery.
Protect key file changes and exports with that same protocol so a concurrent
import cannot invalidate a receipt. Backups retain their existing timers.

## TOTP rotation and recovery

[TOTP key management](totp-key-management.md) owns cryptography and batching. A
plan requires no previous slot and zero off-active rows, including expired
enrollments. Apply rechecks this, chooses the other slot, archives its retired
contents before reuse, stages the new key, and obtains the recovery receipt. The
`prepared` proof enrolls a temporary factor under the old active key and proves
sign-in before switching. Missing proof inputs block before maintenance.

Under maintenance, install the staged key in the other slot, sync that named
Podman secret, atomically set new active/old previous in flags, and reload.
Persist switch boot ID and monotonic time after the new configuration is active;
uncertain timing restarts the ten-minute wait. Users see maintenance 503 during
the switch; afterwards existing codes work through either ring entry. Factor
secrets, recovery codes, sessions, and authentication epochs are not changed by
re-encryption. Normal concurrent enrollments and lazy rotation use active.

Run existing `totp-key-reencrypt`: at most 200 rows per batch, 10,000 rows and
30 minutes per run. Limit reached, skipped rows, or decrypt failure can leave
work. Retain both keys. A failed run's counts prove nothing; `resume` explicitly
runs the next bounded pass. Unknown effects first require quiescence. Two
successful status snapshots must report zero off-active rows, with the second at
least ten monotonic minutes after switch; a reboot restarts that wait. Require
the `serving` sign-in proof before retirement.

`retire` takes locks, rechecks zero, archives the previous key, obtains a fresh
recovery-copy receipt including the archive, then uses maintenance to remove
only the previous-slot mapping and reload. Remove that slot's Podman secret only
after old containers stop. Require the `retired` proof. Do not infer zero from a
failed query, health check, skip count, or elapsed enrollment lifetime.

| Interrupted point       | `resume` / `abort` behavior                                                                                                                                                             |
| ----------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Before active writes    | Resume same staging/proof; abort discards staging and restores saved serving/timers if stopped. Retained ciphertext remains encrypted.                                                  |
| During switch or reload | Reconcile forward to staged active plus old previous. Abort is refused until that ring serves safely. Never return to the old-only ring.                                                |
| During re-encryption    | Prove stopped, retain both keys, continue bounded batches on resume. Abort may close as `suspended` after ring/readiness checks; it releases the fence but keeps request and both keys. |
| Suspended               | Owner can repair a failing factor through passkey/recovery. Resume reacquires fence at the same release/ring; drift refuses. A new rotation is refused until this request retires.      |
| During retirement       | Reconcile new active and archived previous, repeat zero/readiness/proof checks. Never discard either file on an uncertain result.                                                       |
| Completed               | Resume/retire return saved outcome without writes; verify may repeat reads.                                                                                                             |

A suspended request may permit an image fix after quiescence and safe serving. A
normal deploy must preserve both ring mappings and derived IDs and meet all
floors. Automatic resume refuses release drift. `rebind` requires exact
`resume-totp:<id>:<new-tag>:<active-key-id>:<previous-key-id>` confirmation,
fresh eligibility/provenance and installed-hash checks, and a healthy live
release in the wrapper's tested rotation-contract allowlist. Under locks, prove
old attempts stopped and the live ring matches the saved ring. Atomically save
the new release record, invalidate proof nonces and zero snapshots, and restart
the ten-minute wait. No key, flag, floor or row changes. Duplicate identical
rebind succeeds; other drift refuses. Resume then runs the same bounded
re-encryption and fresh proofs. An image with an incompatible command contract
needs a forward fix that preserves this contract before rebind. A decrypt
failure leaves the row unavailable with `503 authentication_unavailable`;
passkeys, recovery, and `/readyz` continue. Only the account owner can
replace/remove the factor. Never reset credentials as an operator
([ADR 0017](../adr/0017-second-factor-authentication.md)).

Keep derived-ID archives and encrypted copies for every dependent backup/WAL
restore target, at least 30 days. These releases do not implement key pruning;
retain archives until a reviewed inventory-based disposal procedure exists.
Restore follows the recovery archive's ring selection and validation rules,
including staged keys after partial activation, with at most two in runtime.
Unknown IDs or unavailable keys block TOTP recovery, never authorize rewriting
factors. A DB restore never restores the release floor or authorizes an older
image.

## View-pass rotation and recovery

Stage a key, recovery receipt and `prepared` old-pass witness as for TOTP. Under
maintenance persist intent, replace `view-pass-key` atomically, replace its
named Podman secret, and recreate the saved release. Go loads the key at
startup; replacing a Podman secret alone does not update a running process.
Resume reconciles to that staged key. Before replacement abort may discard it;
after replacement abort can only finish safe serving with the new key and record
`unverified`. Never restore the old key, including during image rollback or DB
restore. Preserve resume epochs, publish state, flags, and release floor.

Every viewer must sign in again. Account sessions are unaffected. Verify the
same unexpired old pass rejects every protected route while a fresh pass admits
those routes for the same resume ID and epoch, as specified in
[proofs](vietnam-host-operations-proofs.md). An expired/lost witness or changed
epoch is inconclusive: restore readiness, close `unverified`, then make a new
plan/witness and rotate again. Do not claim revocation proof. Recovery manifests
mark every exported view key retired for restore; restore always generates a
fresh view key instead of loading any historical copy.
