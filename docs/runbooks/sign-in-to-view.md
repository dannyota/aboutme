# Sign in to view

Status: **v0.6.26 floor, numeric 6026**. This runbook covers the production
flag-off deploy, floor activation, flag enablement, key rotation, and what
rollback means for sign in to view. It extends
[the production runbook](production.md), which owns the shared deploy, rollback,
and release-fence mechanics.
[Sign in to view](../design/viewer-analytics/sign-in-to-view.md) and
[delivery](../design/viewer-analytics/delivery.md) own the design;
[ADR 0022](../adr/0022-viewer-privacy-and-counting.md) owns the decision.

## Bootstrap and enablement

The server requires `VIEW_PASS_KEY` even with the flag off. On the Vietnam host,
`DEPLOY_HOST=<host> bash deploy/vn/scripts/secrets.sh generate` creates the
missing `view-pass-key`. The server receives it as a named Podman secret.

Follow the [release-fence order](production.md#passkey-release-fence): deploy a
capable tag with `SIGN_IN_TO_VIEW_ENABLED=false`, prove it healthy, activate the
v0.6.26 floor, then set `SIGN_IN_TO_VIEW_ENABLED=true` in
`/etc/aboutme/flags.env` and redeploy the same tag. The deployer refuses an
enabled flag below numeric 6026. A failed redeploy leaves the floor raised;
fix forward with a capable tag.

Run the sign-in-to-view live checks
([delivery](../design/viewer-analytics/delivery.md#live-checks), item 7) on a
fictional published resume: turn its `signInToView` switch on, confirm every
gated route in
[Gated routes](../design/viewer-analytics/sign-in-to-view.md#gated-routes) is
gated, sign in with Google, confirm the resume shows and the join invite appears
after scrolling, confirm no row or log line names the viewer, then turn the
switch off and confirm the resume serves publicly again at once.

## Key rotation

Status: **open host procedure**. The AWS SSM rotation procedure no longer
applies to production. The host secrets script accepts replacement values, but
there is no reviewed sign-in-to-view key-rotation procedure covering secret
replacement, running containers, failure recovery, and verification. Do not
improvise a rotation from the generic import command.

The key must encode 32 random bytes as canonical 43-character unpadded
base64url. Keep plaintext out of shell history, command arguments, logs, and
reports. A reviewed procedure must redeploy the live tag to load the new key
and confirm that existing passes are rejected.

Rotation ends every pass at once: every viewer of every `sign_in` resume must
sign in again, whatever epoch their pass carried. This is routine maintenance,
not only an incident response.

## Rollback

The release ships with the flag off, so a rollback below v0.6.26 is safe until
the fence is raised. Once raised, `deploy.sh --rollback` below v0.6.26 is
refused, the same way a rollback below the passkey or TOTP floors is refused
([the release fence](production.md#passkey-release-fence)): an older image would
serve `sign_in` resumes publicly, which is unsafe regardless of whether a
migration also blocks it. Rolling back below the release is a forward fix or
privileged administration, not a supported rollback.
