# AWS authenticator-app key management reference

Status: **undeployed reference**. Production runs on the Vietnam host under
[ADR 0026](../adr/0026-vietnam-hosted-production.md). These ECS procedures do
not authorize host operations. Vietnam key rotation remains an
[open host procedure](../runbooks/totp-keys.md#key-rotation).

The [application key design](totp-key-management.md) owns sealing, runtime key
validation, re-encryption behavior, and key failures. Paths and commands below
refer to `deploy/aws/` and require a reviewed AWS rebuild.

## Key ring

The AWS ECS reference stores two protected parameters, `totp/key-a` and
`totp/key-b`, in the existing task-secret prefix. Nonsecret OpenTofu variables
choose the slots: `totp_active_key_slot` is `a` or `b` and defaults to `a`;
`totp_previous_key_slot` is empty, `a`, or `b`, defaults to empty, and must
differ from the active slot. The app and re-encryption task definitions inject
`TOTP_ACTIVE_KEY` from the active slot. They inject `TOTP_PREVIOUS_KEY` only
when the previous slot is set, so an absent previous key names no parameter and
ECS never fails on a missing one. Only the app execution role may read the two
exact slot ARNs. Scheduled jobs receive none. `secrets.sh totp-key <a|b>` writes
a fresh random value without printing it and refuses a slot that the running app
task definition names.

## Rotation

The one-shot path is `/usr/local/bin/server totp-key-reencrypt` in task family
`aboutme-prod-totp-reencrypt`, which uses the app execution and task roles and
the same injection. The
[release-fence contract](passkey-release-fence.md#authenticator-app-key-re-encryption)
defines its `totp_reencrypt` operation.

The application's [rotation rules](totp-key-management.md#rotation) define lazy
re-encryption, batch bounds, output limits, and decrypt-failure handling.

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
   rows and zero enrollment rows of any age off the active key.
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

## Key failures

Under the [application failure rules](totp-key-management.md#key-failures), a
Route 53 health alarm never fires for a TOTP key problem.

A CloudWatch metric filter on the app log group matches
`{ $.msg = "totp_unavailable" }`. Alarm `aboutme-prod-totp-unavailable` notifies
the existing alert topic when the sum is at least one in five minutes.
