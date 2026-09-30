# Sign in to view

Status: **v0.6.26 floor, numeric 6026**. This runbook covers the production
flag-off deploy, floor activation, flag enablement, key rotation, and what
rollback means for sign in to view. It extends
[the production runbook](production.md), which owns the shared deploy, rollback,
and release-fence mechanics.
[Sign in to view](../design/viewer-analytics/sign-in-to-view.md) and
[delivery](../design/viewer-analytics/delivery.md) own the design;
[ADR 0022](../adr/0022-viewer-privacy-and-counting.md) owns the decision.

## Bootstrap

The v0.6.26 server requires `VIEW_PASS_KEY` at config load, whether or not the
flag is on. `deploy.sh` refuses a v0.6.26 or later candidate whose server
container lacks that secret, before any change, so an ordinary deploy alone does
not ship this release. Run these in order:

1. Run the base secrets script with no arguments:

   ```sh
   bash deploy/aws/scripts/secrets.sh
   ```

   It prints `kept` for every parameter that exists and creates only the missing
   ones, so it leaves the database, email, and rate-limit keys alone. It creates
   `/aboutme/prod/view-pass-key` as a SecureString holding 32 random bytes in
   unpadded base64url, written through a private tmpfs file so the value never
   reaches a command line or a log.

2. Apply the reviewed OpenTofu change. It adds the `VIEW_PASS_KEY` secret to the
   server container of the app task definition and `view-pass-key` to the
   execution role's app parameter list
   ([production runbook](production.md#secrets)).

## Flag-off deploy and floor activation

1. Deploy v0.6.26 with `sign_in_to_view_enabled = false` (the release's own
   default), only after the two Bootstrap steps:

   ```sh
   bash deploy/aws/scripts/deploy.sh v0.6.26
   ```

   No resume can be gated yet: the switch in the publish dialog stays hidden, so
   this step needs no proof beyond the
   [Healthy state](production.md#healthy-state) checks.

2. Raise the fence to numeric 6026:

   ```sh
   bash deploy/aws/scripts/deploy.sh --activate v0.6.26
   ```

   This requires `aboutme-prod-app` to already be the exact stable v0.6.26
   candidate and only raises the stored minimum; it makes no image, ECS,
   snapshot, or notification change. A lower or malformed fence state, an
   operation already in progress, or a running app that is not the exact
   candidate fails it before the raise.

## Enable sign in to view and redeploy

Only after the raise succeeds, apply the reviewed OpenTofu change that sets
`sign_in_to_view_enabled = true`, then redeploy the same tag:

```sh
bash deploy/aws/scripts/deploy.sh v0.6.26
```

Enabling the flag before the raise, or redeploying a different tag after it, is
not the supported order. `deploy.sh` refuses to register an app revision with
`SIGN_IN_TO_VIEW_ENABLED=true` while the fence is missing or below 6026, the
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

## Key rotation

`secrets.sh` never overwrites `view-pass-key`. To rotate it, write a fresh value
with `Overwrite`, then redeploy the running tag so its task picks up the new
value. The loader accepts only the canonical 43-character unpadded base64url
encoding of 32 random bytes and refuses to start on anything else, so generate
it exactly this way, the same as `secrets.sh` does. The request body goes
through a private tmpfs file, never a command argument, so the value stays out
of shell history and logs:

```sh
umask 077
input=$(mktemp -p "${XDG_RUNTIME_DIR:?XDG_RUNTIME_DIR must point at a per-user tmpfs}")
openssl rand 32 | basenc --base64url | tr -d '=\n' |
  jq -Rn --arg n /aboutme/prod/view-pass-key \
    '{Name: $n, Type: "SecureString", Value: input, Overwrite: true}' >"$input"
aws ssm put-parameter --region ap-southeast-1 --cli-input-json "file://$input" >/dev/null
rm -f "$input"
```

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
