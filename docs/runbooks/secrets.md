# Production secrets and flags

This runbook holds the secret and flag steps of the
[production runbook](production.md).

Secrets are root-owned 0400 files under `/etc/aboutme/secrets/` on the encrypted
data volume. Containers receive only their named Podman secrets. The laptop
keeps an age-encrypted copy in `aboutme-infra`. Never print values.

```sh
DEPLOY_HOST=<host> bash deploy/vn/scripts/secrets.sh list
```

`secrets.sh generate` creates missing values. `secrets.sh import <name>` reads a
value from standard input and syncs Podman. Supported rotations use `--replace`;
feed a private file or an approved runtime pipe, never a literal value in a
command. See the [host guide](../../deploy/vn/README.md#build-order) for media
and backup key imports.

The owner edits `/etc/aboutme/flags.env` on the host. `host/install.sh` copies
the example once and preserves that file. Redeploy the live tag after a flag or
secret change so the rendered units and running containers use it.

- `PROVIDER_LOGIN_ENABLED=false` disables provider login. `google` enables
  Google; `google,linkedin` enables both. Import the corresponding
  `<provider>-client-id` and `<provider>-client-secret` before enabling them.
  Keep provider callback URLs under `https://aboutme.vn/api/v1/auth/`.
- [GitHub evidence](../design/github-evidence.md) uses its own GitHub OAuth
  application, registered with the callback
  `https://aboutme.vn/api/v1/github-evidence/callback`. To turn it on, import
  its client id as `github-evidence-client-id` and its client secret as
  `github-evidence-client-secret`, then redeploy. Deploys give the server both
  secrets or neither, and refuse one without the other before the site changes.
  To turn it off, delete both files from `/etc/aboutme/secrets/` and both Podman
  secrets on the host, then redeploy; no script does this. A file left behind
  lets `secrets.sh` recreate its Podman secret.
- `PASSWORD_REGISTRATION_ENABLED=false` disables new email registration. Login,
  password reset, pending verification, and provider sign-up keep working. Set
  `true` and redeploy to restore registration.
- Passkey, TOTP, and sign-in-to-view flags require their release-fence floors.
  See [Passkey release fence](deploy.md#passkey-release-fence).

The app sends through Bizfly SMTP as `Danny from aboutme.vn <danny@aboutme.vn>`.
Replies reach Google Workspace. The [email runbook](email.md) owns configuration
and delivery checks.
