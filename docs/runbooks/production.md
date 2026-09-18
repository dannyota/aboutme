# Production

Status: **infrastructure built; not yet serving** (2026-09-17). This runbook
covers the single-host production environment at `https://aboutme.vn`. The
[single-host design](../design/single-host-production.md) explains why it is
shaped this way.

## Access

- AWS: `aws login` into the owner's account, region `ap-southeast-1`.
- OpenTofu: `deploy/aws/prod` with the ignored `backend.hcl` and `prod.tfvars`.
  Every `tofu plan` and `tofu apply` takes `-var-file=prod.tfvars`, because
  state encryption reads the key ARN before a saved plan loads.
- Cloudflare: the owner's authenticated Cloudflare MCP connection. There is no
  Cloudflare API token and no Cloudflare provider in OpenTofu.

```sh
tofu -chdir=deploy/aws/prod plan -var-file=prod.tfvars
```

A clean environment prints `No changes.`

## Cloudflare settings

These live only in Cloudflare. Change them through the MCP connection or the
dashboard, and update this table in the same change.

| Setting                    | Value                                                                                                             |
| -------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| `aboutme.vn`               | `A` to the host Elastic IP (`tofu output host_public_ip`), proxied                                                |
| `www.aboutme.vn`           | `CNAME` to `aboutme.vn`, proxied; the origin redirects it to the apex                                             |
| SSL/TLS mode               | Full (strict)                                                                                                     |
| Always Use HTTPS           | On                                                                                                                |
| Minimum TLS version        | 1.2; TLS 1.3 on                                                                                                   |
| HSTS                       | On, max-age 31536000, no subdomains, no preload, nosniff                                                          |
| Bot Fight Mode             | Off                                                                                                               |
| Cache rule                 | `not starts_with(http.request.uri.path, "/_nuxt/")` → bypass cache                                                |
| Origin CA certificate      | ECC, `aboutme.vn` and `www.aboutme.vn`, expires 2041-09-12; stored at `/aboutme/prod/tls/origin-cert`             |
| Authenticated Origin Pulls | On; zone-level certificate from `tls.sh pull`, active, expires 2036-09-13. Caddy requires it from its first start |

Mail records (MX, TXT, DKIM and the SES `bounce` records) belong to the
[email runbook](email.md); do not change them here.

Before a deploy, `deploy.sh` compares Cloudflare's published IPv4 ranges with
the ranges in the running task definition. If they differ, run `tofu apply` to
refresh the security group and the Caddy trust list first.

## Secrets

Values live in SSM Parameter Store under `/aboutme/prod/`. Never print them.

- `deploy/aws/scripts/secrets.sh` creates missing database passwords and keys
  and never overwrites one.
- `deploy/aws/scripts/tls.sh origin` creates a new origin key (to SSM) and
  `deploy/aws/prod/origin.csr`. A new key needs a new Origin CA certificate from
  that request, stored at `/aboutme/prod/tls/origin-cert`.
- `deploy/aws/scripts/tls.sh pull` creates a new origin-pull CA (certificate to
  SSM, key discarded) and a leaf client certificate in
  `$XDG_RUNTIME_DIR/aboutme-origin-pull`. Upload it in the dashboard: SSL/TLS →
  Origin Server → Authenticated Origin Pulls → Zone-level → Upload certificate,
  pasting each file with `wl-copy < <file>`. Then run
  `wl-copy --clear && tls.sh forget-pull`. The deploy after a new CA picks up
  the new trust pool.
- The RDS master password is managed by RDS in Secrets Manager. Only the
  one-shot `db-setup` task can read it.

List names only:

```sh
aws ssm get-parameters-by-path --path /aboutme/prod --recursive --query 'Parameters[].Name'
```

## Google sign-in

Google is the only provider production can enable. The `provider_login_enabled`
variable sets `PROVIDER_LOGIN_ENABLED`: `""` (the default) keeps provider login
off, and `"google"` turns on Google. With `"google"`, the app task reads its
credentials from two SecureString parameters:

- `/aboutme/prod/oauth/google-client-id`
- `/aboutme/prod/oauth/google-client-secret`

The server refuses to start when Google is enabled and either value is missing,
and `deploy.sh` refuses to begin a deploy while any task secret is missing.

Enable Google as its own step, after a release that contains the provider list
parser (commit `b8460d9` or later) and a web build that reads the capabilities
`providers` list is live and healthy. Never combine the switch with that
release: a rollback from it would give an older release `google`, which it
refuses at startup.

1. In the Google Cloud Console, create an OAuth client of type "Web application"
   with the authorized redirect URI
   `https://aboutme.vn/api/v1/auth/google/callback`. The app requests the
   `openid`, `email`, and `profile` scopes. Put the consent screen in production
   so that sign-in is not limited to test users.
2. Store each value through a private tmpfs file, never a command argument.
   `mktemp` creates the file readable only by you. Copy each value in the
   console, then write it to the file and clear the clipboard; the server trims
   the trailing newline.

   ```sh
   f=$(mktemp -p "$XDG_RUNTIME_DIR")
   wl-paste >"$f" && wl-copy --clear # after copying the client ID
   aws ssm put-parameter --region ap-southeast-1 --type SecureString \
     --name /aboutme/prod/oauth/google-client-id --value "file://$f"
   wl-paste >"$f" && wl-copy --clear # after copying the client secret
   aws ssm put-parameter --region ap-southeast-1 --type SecureString \
     --name /aboutme/prod/oauth/google-client-secret --value "file://$f"
   rm -f "$f"
   ```

3. Set `provider_login_enabled = "google"` in `prod.tfvars`, then run
   `tofu apply`. The plan changes the app task definition.
4. Redeploy the live tag with `deploy.sh <tag>`. It builds the new revision from
   the task definition that `tofu apply` registered.
5. Check `https://aboutme.vn/api/v1/capabilities`. It reports
   `"providers":["google"]`, and `/api/v1/auth/github/start` returns 404.

To rotate a value, repeat step 2 with `--overwrite` on `put-parameter`. The
running app keeps the old value until its task next starts, so deploy after
rotating.

To turn Google off, set `provider_login_enabled = ""`, apply, and deploy. Leave
the parameters in place.

## Deploy

A release is a `v*` tag on `main` with green CI. The `release-images` workflow
publishes `ghcr.io/dannyota/aboutme-{server,web,caddy}` for that tag; the
packages must be public so the host can pull them.

```sh
bash deploy/aws/scripts/deploy.sh <tag>                 # normal release
bash deploy/aws/scripts/deploy.sh <tag> --first-deploy  # first release only
```

The script checks the tag and CI, resolves image digests, compares Cloudflare
ranges, checks that every secret the new revisions reference exists (by name,
never reading a value), snapshots RDS, registers task definition revisions,
disables the job schedules, stops `app`, runs the database steps, starts `web`
then `app`, re-enables the schedules and smoke-tests through Cloudflare. The
site is down between "site down" and "site up", usually one to three minutes.

Each release snapshot is tagged `aboutme:created-by=deploy.sh`. The script never
deletes a snapshot. The daily `release-snapshot-sweep` job (20:00 UTC) deletes
release snapshots of `aboutme-prod` more than 27 days old by
`SnapshotCreateTime`, so none reaches 30 days even after one missed run. It
deletes only manual, available snapshots whose names match
`aboutme-prod-v<tag>-<YYYYMMDDHHMM>` and that carry the tag, plus the untagged
`aboutme-prod-v0-1-1-202609171438`. Automated backups, the final snapshot, and
any other snapshot stay. The job's result reports counts; its log names each
deleted or failed snapshot and the AWS error code of a failed delete. Scheduler
retries a failed task start twice. Like every job schedule, it runs only after a
deploy enables it.

To keep a release snapshot longer, copy it without the tag and under a name the
job does not match, then delete the copy when it is no longer needed. A kept
copy is outside the 30-day backup promise in the privacy policy.

```sh
aws rds copy-db-snapshot --region ap-southeast-1 \
  --source-db-snapshot-identifier <release-snapshot> \
  --target-db-snapshot-identifier keep-<release-snapshot>
```

`copy-db-snapshot` copies no tags unless given `--copy-tags`.

A failure after "site down" but before migrations complete restores the previous
`app` revision and the job schedules' earlier state, then exits non-zero. If
migrations were already applied, the script leaves the app and schedules stopped
and says so: fix forward with a new release, or restore the snapshot the deploy
took.

Goose applies each migration in its own transaction. When a release carries
several migrations and `migrate` fails partway, the earlier ones stay applied
while the script still restores the previous app. After any migrate failure,
check the applied head in the migrate task's log before trusting the restored
app; if it moved, treat the deploy as migrated. On `--first-deploy` it leaves
`app` stopped, because there is no earlier release. If it reports that a
database task may still be running, it leaves the app and schedules stopped:
check that task with
`aws ecs describe-tasks --cluster aboutme-prod --tasks <arn>`, and when it has
stopped, rerun the deploy. Enabled schedules always point at the released `jobs`
revision.

## Rollback

```sh
bash deploy/aws/scripts/deploy.sh --rollback <previous-tag>
```

It redeploys earlier images without a snapshot or migration. It is safe only
when the failed release applied no migration. After a migration, fix forward
with a new release, or restore the database from the snapshot the failed deploy
took.

A rollback builds from the current task definition, so it keeps the current
settings. Before rolling back to a tag older than the provider list parser
(older than `b8460d9`) while Google is on, set `provider_login_enabled = ""` and
run `tofu apply`; that older release refuses to start with
`PROVIDER_LOGIN_ENABLED=google`.

A rollback also points the job schedules at the older `jobs` revision. Rolling
back to a release that predates the `release-snapshot-sweep` command leaves that
schedule failing daily with a usage error and deleting nothing until the next
release. Each day without a successful run uses one day of the 30-day margin, so
ship the fix-forward release within a day, or delete expired release snapshots
by hand.

## Healthy state

- ECS services `aboutme-prod-app` and `aboutme-prod-web` each run one task.
- `https://aboutme.vn/healthz` and `/readyz` return 200 through Cloudflare, and
  `https://www.aboutme.vn/` redirects to the apex.
- A request straight to the Elastic IP gets no response.
- The four job schedules in group `aboutme-prod-jobs` are enabled.
- The `aboutme-prod-site-down` alarm (us-east-1) has actions enabled
  (`site_alarm_enabled = true` in `prod.tfvars`).
