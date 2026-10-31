# Authenticator-app (TOTP) keys

Production TOTP uses release floor **v0.4.7, numeric 4007**. The
[production runbook](production.md) owns host deploys, flags, and the release
fence. [Key management](../design/totp-key-management.md) owns the key-ring
contract. The [AWS key reference](../design/aws-totp-key-management.md) owns ECS
injection, rotation, and monitoring;
[ADR 0017](../adr/0017-second-factor-authentication.md) owns the choice.

## Bootstrap and enrollment

`DEPLOY_HOST=<host> bash deploy/vn/scripts/secrets.sh generate` creates the
missing active key in slot `a`. The host deployer reads `TOTP_ACTIVE_KEY_SLOT`
and `TOTP_PREVIOUS_KEY_SLOT` from `/etc/aboutme/flags.env` when rendering units.
Do not provision an unused previous slot ahead of need.

For first enablement, follow the
[release-fence order](production.md#passkey-release-fence): deploy a capable tag
with `TOTP_ENROLLMENT_ENABLED=false`, run the flag-off proof, activate the
floor, then set `TOTP_ENROLLMENT_ENABLED=true` and redeploy the same tag. Run
the enabled proof after that deploy. A failed redeploy leaves the floor raised;
fix forward with a capable tag.

## Key rotation

Status: **open host procedure**. The
[AWS ECS re-encryption procedure](../design/aws-totp-key-management.md#rotation)
does not apply to production. Neither the Vietnam deployer nor `host/job-run.sh`
provides a reviewed TOTP re-encryption path. Do not use AWS commands or
improvise a host command to rotate keys with stored credentials.

A reviewed host procedure must preserve these safeguards:

- Start only when no previous slot is configured and no credential or enrollment
  row is sealed under another key. Never overwrite a key still needed by a row.
- Make the new key readable before making it active. Keep the old key readable
  while re-encryption runs under the operation lock at the exact live release.
- Require zero credential and enrollment rows off the active key. Repeat the
  check at least ten minutes after switching the active key, so older pending
  enrollments have expired or been rewritten, before retiring the previous key.
- Stop on a decrypt failure with the previous key still configured. The account
  owner can prove a passkey or recovery code and replace or remove TOTP. An
  operator must not delete or rewrite a credential
  ([ADR 0003](../adr/0003-public-namespace-and-no-operator-surface.md)).
- A restore that needs a retired key fails closed for TOTP. Keep required keys
  available for the recovery window.

## TOTP unavailable

The host watcher checks `totp_unavailable` in the server journal. A key ID
outside `TOTP_ACTIVE_KEY`/`TOTP_PREVIOUS_KEY`, or an AES-GCM authentication
failure, returns `503 authentication_unavailable` for that TOTP row. Passkeys,
recovery codes, accounts without TOTP, and `/readyz` remain available.

On alert:

1. Read the closed `reason`: `unknown_key_id`, `key_id_overflow`, or
   `decrypt_failed`. Read the server journal on the host; never copy raw row
   data or secrets into reports.
2. For an unknown or overflowing key ID, compare the installed active and
   previous slots with the key-ring configuration. Confirm which key sealed the
   affected row before changing a slot.
3. A decrypt failure names the record kind and internal row ID. Do not retry
   automatically. Use the account-owner recovery path above.
4. Never widen the key ring, restore plaintext, or accept an unchecked code.
   Confirm the signal clears after the affected rows are fixed or removed.

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
  replacement (without the old-secret rejection), shared recovery, one pending
  row proving passkey coexistence across both locales and completed with TOTP,
  and removal. The hosted proof (`totp.spec.ts`) covers replay, concurrency,
  step skew, and old-secret rejection; production has only one account, so
  `totp-prod-enabled` stays within its per-account pending budget
  (`secondFactorAttemptAccountLimit`,
  apps/server/internal/auth/second_factor_handlers.go) by spending at most 8
  admitted attempts.

Each proof reads the fictional account from the owner-only ignored file
`.dev/v0.4.7/production-input/account.env`, mounted read-only, computes TOTP
codes and reads the setup secret and recovery codes into process memory only,
and prints only fixed step names and outcomes; no assertion prints a secret,
URI, code, or password.

Run each mode with the wrapper, directly and without another lock around it:

```sh
scripts/totp-production-proof.sh totp-prod-enabled
```

The wrapper uses the [local memory watchdog](../../instructions/resources.md)
and refuses while another local check holds the shared lock or a development
stack is active. It takes the lock without blocking and holds it until it exits,
and no child process inherits the lock. It requires the account file with mode
0600 and never reads it. It builds the pinned image once, stages the exact
production spec files into an owner-only temporary directory, runs
`deploy/dev-https-browser/run.sh` under a 60-minute timeout, and removes the
staging directory on every exit. Evidence goes to
`.dev/v0.4.7/production-evidence`, which must start empty.

The container runs with a 2 GiB hard memory limit, no swap, and two CPUs
(`--memory=2g --memory-swap=2g --cpus=2`), an isolated browser profile on the
container tmpfs, and no local product stack. On success, failure, a blocker, or
browser exit, the `finally` path removes only that account's proof factors,
recovery plaintext, and proof sessions, deletes the tmpfs output directory, then
logs out. If cleanup cannot finish, the next action is the same command with
mode `totp-prod-cleanup`. Missing watchdog, lock, container limit, image, or
account file blocks the proof. Never install a host runner, use an uncapped
browser, or use the owner's own account.

## Forward fix

A rollback runs through the same fence-aware `deploy.sh` and is rejected below
the fence minimum, the same as a forward deploy. Once TOTP enrollment has ever
been possible in production, rolling back below 4007 is a forward fix or
privileged administration, not a supported rollback; see
[the release fence](production.md#passkey-release-fence). A migration applied by
a v0.4.7 release makes rollback unsafe regardless of the TOTP floor; fix forward
with a new release, or restore the database from the host's pgBackRest backups.
