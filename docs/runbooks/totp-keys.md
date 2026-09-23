# Authenticator-app (TOTP) keys

Status: **v0.4.7 floor, numeric 4007**. This runbook covers production TOTP key
bootstrap, rotation, the release-fence floor, the
`aboutme-prod-totp-unavailable` alarm, and the two scripted production proofs.
It extends [the production runbook](production.md), which owns the shared
deploy, rollback, and release-fence mechanics.
[Key management](../design/totp-key-management.md) and
[the release-fence contract](../design/passkey-release-fence.md#authenticator-app-key-re-encryption)
own the design; [ADR 0049](../adr/0049-totp-second-factor-authentication.md)
owns the decision.

## Bootstrap

The first production apply needs only `secrets.sh totp-key a`. Key slot `b` is
created later, at [rotation](#key-rotation) step 1; nothing reads it before
that, so an unused slot is never provisioned ahead of need.

## Flag-off deploy and floor activation

1. Deploy v0.4.7 with `totp_enrollment_enabled = false`:

   ```sh
   bash deploy/aws/scripts/deploy.sh v0.4.7
   ```

2. Run the first production proof (see [Production proofs](#production-proofs),
   mode `totp-prod-flag-off`): health, existing sign-in without a second factor,
   and disabled enrollment.
3. Raise the fence to numeric 4007:

   ```sh
   bash deploy/aws/scripts/deploy.sh --activate v0.4.7
   ```

   This requires `aboutme-prod-app` to already be the exact stable v0.4.7
   candidate and only raises the stored minimum; it makes no image, ECS,
   snapshot, or notification change. A lower or malformed fence state, an
   operation already in progress, or a running app that is not the exact
   candidate fails it before the raise.

## Enable TOTP enrollment and redeploy

Only after the raise succeeds, apply the reviewed OpenTofu change that sets
`totp_enrollment_enabled = true`, then redeploy the same tag:

```sh
bash deploy/aws/scripts/deploy.sh v0.4.7
```

Enabling the flag before the raise, or redeploying a different tag after it, is
not the supported order. `deploy.sh` refuses to register an app revision with
`TOTP_ENROLLMENT_ENABLED=true` while the fence is missing or below 4007, the
same way it refuses passkey enrollment below 4002. If the raise succeeds but the
apply or redeploy then fails, the fence stays raised; fix forward with the same
or a newer capable tag.

Run the second production proof (mode `totp-prod-enabled`): first enrollment,
pending login, replacement, shared recovery, passkey coexistence, both locales,
and removal.

## Key rotation

Rotation starts only when the running app task definition names no previous slot
and a re-encryption run reports zero rows off the active key. The steps below
assume active slot `a`; swap the letters otherwise. Each redeploy runs the
running tag through a serialized `deploy.sh` operation.

1. `secrets.sh totp-key b`. No task names slot `b`, so a task restart at any
   point is unaffected.
2. Set `totp_previous_key_slot = "b"`, apply, redeploy the running tag.
3. Set `totp_active_key_slot = "b"` and `totp_previous_key_slot = "a"`, apply,
   redeploy. During rollout, step 2 tasks still write under `a`, which the new
   tasks read as previous, and read rows the new tasks write under `b`.
4. Run the one-shot re-encryption until it reports zero credential and zero
   enrollment rows off the active key:

   ```sh
   bash deploy/aws/scripts/deploy.sh --totp-key-reencrypt v0.4.7
   ```

   This acquires the operation lock with kind `totp_reencrypt`, requires the
   stored fence minimum to be at least 4007 and the given tag to equal the
   running app's exact release, checkpoints, runs only
   `aboutme-prod-totp-reencrypt`, waits for it to stop, and releases the exact
   lock it holds. It changes no service, schedule, alarm, or rule. Counts and
   internal row IDs are in the task's own CloudWatch log stream; the command
   never prints a key, secret, nonce, or ciphertext. If the task is still
   running after about 40 minutes of waiting, the command exits nonzero and
   keeps the lock held on purpose. Clear it only through the manual lock-clear
   procedure in [the production runbook](production.md), after the
   `deploy-totp-reencrypt` task has stopped.

5. At least ten minutes after the step 3 deploy completes, so every enrollment
   an older task sealed has expired or been rewritten, run step 4 again. Repeat
   until it reports zero.
6. Set `totp_previous_key_slot = ""`, apply, redeploy.
7. `secrets.sh totp-key a` to replace the retired key with an unused random
   value.

Steps 1 and 7 write only a parameter that no running task names. Steps 2, 3, and
6 change keys only through a new task definition, so a restart between writes
starts a revision whose key set is complete. Restoring a revision from before
step 3 after step 7 fails closed for TOTP only.

A decrypt failure during rotation leaves the affected row unchanged, emits the
`totp_unavailable` signal below, and fails the run: rotation stops at step 4
with the previous key still configured and nothing else broken. The account
owner can clear the blocking row by proving a passkey or recovery code and
replacing or removing TOTP; no operator deletes or rewrites a credential
([ADR 0028](../adr/0028-no-operator-surface.md)).

## The totp-unavailable alarm

`aboutme-prod-totp-unavailable` fires when the app log group's
`totp_unavailable` metric filter sums to at least one in five minutes. A stored
key ID outside the ring or an AES-GCM authentication failure fails closed for
that TOTP row only; verification, enrollment completion, and re-encryption
return `503 authentication_unavailable`. Passkeys, recovery codes, accounts
without TOTP, and `/readyz` are unaffected, so this alarm never overlaps with
the site-down alarm.

On alert:

1. Read the closed `reason` in the `totp_unavailable` log line:
   `unknown_key_id`, `key_id_overflow`, or `decrypt_failed`.
2. `unknown_key_id` or `key_id_overflow` means a row carries a key ID outside
   `TOTP_ACTIVE_KEY`/`TOTP_PREVIOUS_KEY`, or a third ID exists. Compare the
   running task's active and previous slots against what rotation last set; an
   interrupted rotation (a skipped or reordered step) is the likely cause. Do
   not change a key slot to "fix" this without confirming which slot the
   affected row was actually sealed under; an incorrect change fails more rows
   closed.
3. `decrypt_failed` names the record kind and internal row ID. It confirms an
   authentication mismatch, not a transient error. Do not retry the operation
   automatically. See [key rotation](#key-rotation) for the account-owner
   recovery path.
4. No fallback exists: never widen the key ring, restore plaintext, or accept an
   unchecked code to clear the alarm. Confirm the sum returns to zero only
   through the affected rows being fixed or removed.

## Production proofs

GitHub CI cannot observe the deployed fence, the enrollment flag, or the
production origin. The top manager runs each proof as the scripted Playwright
proof QA authors, using the pinned browser image from
`deploy/dev-https-browser/Dockerfile`, not a host install. No one drives the
browser by hand.

- After the flag-off deploy and before activation, run mode
  `totp-prod-flag-off`: health, existing sign-in without a second factor, and
  disabled enrollment.
- After activation and the flag-on redeploy, run mode `totp-prod-enabled`, using
  only the release's named fictional account: first enrollment, pending login,
  replacement, shared recovery, passkey coexistence, both locales, and removal.

Each proof reads the fictional account from the owner-only ignored file
`.dev/v0.4.7/production-input/account.env`, mounted read-only, computes TOTP
codes and reads the setup secret and recovery codes into process memory only,
and prints only fixed step names and outcomes; no assertion prints a secret,
URI, code, or password.

Run each mode with the wrapper, directly and without another lock around it:

```sh
scripts/totp-production-proof.sh totp-prod-enabled
```

The wrapper refuses to start below 8 GiB `MemAvailable`, while another local
check holds the shared lock, or while a development stack is active. It takes
the lock without blocking and holds it until it exits, and no child process
inherits the lock. It requires the account file with mode 0600 and never reads
it. It builds the pinned image once, stages the exact production spec files into
an owner-only temporary directory, runs `deploy/dev-https-browser/run.sh` under
a 60-minute timeout, and removes the staging directory on every exit. Evidence
goes to `.dev/v0.4.7/production-evidence`, which must start empty.

The container runs with a 2 GiB hard memory limit, no swap, and two CPUs
(`--memory=2g --memory-swap=2g --cpus=2`), an isolated browser profile on the
container tmpfs, and no local product stack. On success, failure, a blocker, or
browser exit, the `finally` path removes only that account's proof factors,
recovery plaintext, and proof sessions, deletes the tmpfs output directory, then
logs out. If cleanup cannot finish, the next action is the same command with
mode `totp-prod-cleanup`. Missing memory floor, lock, container limit, image, or
account file blocks the proof. Never install a host runner, use an uncapped
browser, or use the owner's own account.

## Forward fix

A rollback runs through the same fence-aware `deploy.sh` and is rejected below
the fence minimum, the same as a forward deploy. Once TOTP enrollment has ever
been possible in production, rolling back below 4007 is a forward fix or
privileged administration, not a supported rollback; see
[the release fence](production.md#passkey-release-fence). A migration applied by
a v0.4.7 release makes rollback unsafe regardless of the TOTP floor; fix forward
with a new release, or restore the database from the deploy's snapshot.
