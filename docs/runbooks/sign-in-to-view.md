# Sign in to view

Status: **v0.6.22 floor, numeric 6022**. This runbook covers the production
flag-off deploy, floor activation, flag enablement, the LinkedIn view switch,
key rotation, and what rollback means for sign in to view. It extends
[the production runbook](production.md), which owns the shared deploy, rollback,
and release-fence mechanics.
[Sign in to view](../design/viewer-analytics/sign-in-to-view.md) and
[delivery](../design/viewer-analytics/delivery.md) own the design;
[ADR 0022](../adr/0022-viewer-privacy-and-counting.md) owns the decision.

## Bootstrap

The base `secrets.sh` run (no arguments) creates `view-pass-key` along with the
other base secrets; nothing extra is needed before the first apply.

## Flag-off deploy and floor activation

1. Deploy v0.6.22 with `sign_in_to_view_enabled = false` (the release's own
   default, so an ordinary deploy already ships this):

   ```sh
   bash deploy/aws/scripts/deploy.sh v0.6.22
   ```

   No resume can be gated yet: the switch in the publish dialog stays hidden, so
   this step needs no proof beyond the
   [Healthy state](production.md#healthy-state) checks.

2. Raise the fence to numeric 6022:

   ```sh
   bash deploy/aws/scripts/deploy.sh --activate v0.6.22
   ```

   This requires `aboutme-prod-app` to already be the exact stable v0.6.22
   candidate and only raises the stored minimum; it makes no image, ECS,
   snapshot, or notification change. A lower or malformed fence state, an
   operation already in progress, or a running app that is not the exact
   candidate fails it before the raise.

## Enable sign in to view and redeploy

Only after the raise succeeds, apply the reviewed OpenTofu change that sets
`sign_in_to_view_enabled = true`, then redeploy the same tag:

```sh
bash deploy/aws/scripts/deploy.sh v0.6.22
```

Enabling the flag before the raise, or redeploying a different tag after it, is
not the supported order. `deploy.sh` refuses to register an app revision with
`SIGN_IN_TO_VIEW_ENABLED=true` while the fence is missing or below 6022, the
same way it refuses passkey enrollment below 4002 and TOTP enrollment
below 4007. If the raise succeeds but the apply or redeploy then fails, the
fence stays raised; fix forward with the same or a newer capable tag.

Run the sign-in-to-view live checks
([delivery](../design/viewer-analytics/delivery.md#live-checks), item 7) on a
fictional published resume: turn its `signInToView` switch on, confirm every
gated route in
[Gated routes](../design/viewer-analytics/sign-in-to-view.md#gated-routes) is
gated, sign in with Google, confirm the resume shows and the join invite appears
after scrolling, confirm no row or log line names the viewer, then turn the
switch off and confirm the resume serves publicly again at once.

## LinkedIn view switch

`sign_in_to_view_linkedin_enabled` stays `false` until a live check confirms
LinkedIn accepts an authorize request with scope `openid` alone: with the switch
off, the gate offers no LinkedIn button and the LinkedIn `view` start redirects
to the resume with no transaction, so leaving it off after the release above is
always safe. Confirm the scope acceptance directly against LinkedIn's
authorization endpoint, apply the reviewed OpenTofu change setting the switch
`true`, redeploy the same tag, then repeat the LinkedIn half of the live check
above.

## Key rotation

`secrets.sh` never overwrites `view-pass-key`. To rotate it, write a fresh
32-byte value with `--overwrite` on `put-parameter`, the same as
[rotating a provider credential](production.md#provider-sign-in), then redeploy
the running tag so its task picks up the new value. Rotation ends every pass at
once: every viewer of every `sign_in` resume must sign in again, whatever epoch
their pass carried. This is routine maintenance, not only an incident response.

## Rollback

The release ships with the flag off, so a rollback below v0.6.22 is safe until
the fence is raised. Once raised, `deploy.sh --rollback` below v0.6.22 is
refused, the same way a rollback below the passkey or TOTP floors is refused
([the release fence](production.md#passkey-release-fence)): an older image would
serve `sign_in` resumes publicly, which is unsafe regardless of whether a
migration also blocks it. Rolling back below the release is a forward fix or
privileged administration, not a supported rollback.
