# Production

Status: **serving production**. The live release is the `DEPLOY_RELEASE_TAG`
value in the `aboutme-prod-app` task definition. This runbook covers the
single-host production environment at `https://aboutme.vn`. The
[single-host design](../design/single-host-production.md) explains why it is
shaped this way.

## Access

- AWS: `aws login` into the owner's account, region `ap-southeast-1`. This
  identity applies OpenTofu directly. `deploy.sh` never mutates ECS, Scheduler,
  a release snapshot, or the release fence with it directly: it assumes
  `aboutme-prod-operator`, which may only read the fence and assume
  `aboutme-prod-deploy`, which performs every deployment mutation. See
  [Passkey release fence](#passkey-release-fence).
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
| Web Analytics (RUM)        | Off; the privacy policy promises no analytics or tracking scripts                                                 |
| Email Obfuscation          | Off; it rewrote validated public HTML and injected a script                                                       |
| Rocket Loader, Auto Minify | Off; both would rewrite validated public HTML                                                                     |
| Automatic HTTPS Rewrites   | Off; it rewrote links inside validated public HTML, and Always Use HTTPS already covers the site                  |
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

## Email sign-up

Email-and-password sign-up is on and sends a verification email through SES,
which has production access. To turn sign-up off, for example during an SES
incident, set `password_registration_enabled = false` in `prod.tfvars`, run
`tofu apply`, and redeploy the live tag with `deploy.sh <tag>`. The register
route then returns 404 and the web hides the sign-up form. Google sign-up,
login, password reset, and verification of pending registrations keep working.
Turn it back on the same way with `true`.

## Deploy

Public pages link the resume stylesheets and hydration script with a build-time
content hash (`?v=`), so a release reaches browsers despite the one-year
immutable cache. A font change must rename the fixed-name `.woff2` under
`/_nuxt/fonts/`, changing both stylesheet hashes too.

A release is a `v*` tag on `main` with green `ci.yml` (not `workflow_dispatch`).
`release-images.yml` publishes and Trivy-scans
`ghcr.io/dannyota/aboutme-{server,web,caddy}` (public), failing on a fixable
HIGH/CRITICAL finding. `security-scan.yml` also runs `govulncheck`/`npm audit`
weekly on `main`/latest images; cron editor alone is alerted; 60d idle drops it.

Before the first deploy after adding maintenance mode, apply the reviewed
OpenTofu change. It creates `aboutme-prod-maintenance` at desired count zero.
The deploy script registers the release's Caddy image before it starts that
service, so the initial task definition image never serves traffic.

```sh
bash deploy/aws/scripts/deploy.sh <tag>                 # normal release
bash deploy/aws/scripts/deploy.sh <tag> --first-deploy  # first release only
```

The script verifies the caller, assumes the operator then deploy role, reads the
release fence, and rejects a target below its minimum before any AWS mutation.
It then checks the tag and CI, resolves image digests, compares Cloudflare
ranges, checks that every secret the new revisions reference exists (by name,
never reading a value), snapshots RDS, registers task definition revisions,
disables the `aboutme-prod-site-down` alarm actions and the
`aboutme-prod-task-stopped` rule when they were enabled, disables the job
schedules, and stops `app`. It then swaps in the `maintenance` service: the same
Caddy image, in a mode that serves `deploy/caddy/production/maintenance.html` at
503 for every path, including `/readyz` and `/api/*`. With the maintenance page
up, it runs the database steps, starts `web`, swaps `maintenance` back out,
starts `app`, and re-enables the schedules. It then smoke-tests through
Cloudflare, using the base caller's own credentials for `ec2:DescribeAddresses`
and `cloudwatch:GetMetricStatistics` (in `us-east-1`), both outside the deploy
role's IAM list. It warms `DEPLOY_WARM_PAGE` (default `/danny`; must stay a
published resume page) and the homepage, since first requests after a restart
can be slow on the Cloudflare-to-origin path in a way `/healthz` misses, until
each answers fast (`DEPLOY_WARM_FAST` seconds) or `DEPLOY_WARM_ATTEMPTS` run
out. The script requires the maintenance page's 503 response through Cloudflare
before it starts a database task. `app` and `maintenance` both bind host port
443, so only one runs at once. After maintenance stops, Cloudflare can return
521 until `app` accepts traffic. Do not promise a bounded downtime window.

The deploy records the prior state of the site-down alarm actions and
task-stopped rule before it changes either source. On success, it re-enables the
task-stopped rule, then waits (bounded by `DEPLOY_ALARM_WAIT` seconds, default
900, polled every `DEPLOY_ALARM_POLL` seconds) for the alarm to read `OK` with
enough healthy Route 53 minutes since the site came back up, before re-enabling
its actions: CloudWatch evaluates the alarm on a delay, so `OK` alone does not
prove the outage finished evaluating, and re-enabling early can send a false
alert. A warm-up or smoke failure after the app started waits the same way, up
to `DEPLOY_ALARM_WAIT` seconds; Ctrl-C during that wait skips it and re-enables
the actions at once. A timeout still re-enables the actions and fails, naming
the alarm's last state and healthy-minute count. An error before the handoff
finishes, or a HUP, INT, or TERM exit before that wait starts, restores both
sources at once when they were enabled. Database, capacity, Scheduler, and host
recovery alarms stay active throughout. SIGKILL, host loss, and shell death skip
cleanup. Before retrying after one of those failures, use the recorded
`deployment notification states` line from the deploy output. If the recorded
task-stopped rule was `ENABLED`, run:

```sh
aws events enable-rule --region ap-southeast-1 --name aboutme-prod-task-stopped
```

If the recorded site-down actions were `True`, run:

```sh
aws cloudwatch enable-alarm-actions --region us-east-1 \
  --alarm-names aboutme-prod-site-down
```

Leave either source unchanged when its recorded state was `DISABLED` or `False`.
If the deploy output is unavailable, do not change either source until the
operator establishes its prior state from operational evidence.

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

A failure before the migration task starts turns the maintenance page back off
and restores the previous `app` revision and the job schedules' earlier state,
then exits non-zero. Once the script requests `migrate`, it leaves the
maintenance page up and keeps `app` and the job schedules stopped. The request
can succeed even when the client loses its response, and Goose can commit an
earlier migration before a later migration fails. Fix forward with a new
release, or restore the snapshot the deploy took. On `--first-deploy`, a failure
retries the app stop and starts maintenance only after ECS confirms the port is
free. It leaves every job schedule disabled, because `db-setup` may not have
created usable application state.

If the script reports that a database task may still be running, it leaves the
maintenance page, the app, and the schedules exactly as they are: check that
task with `aws ecs describe-tasks --cluster aboutme-prod --tasks <arn>`, and
when it has stopped, rerun the deploy. Enabled schedules always point at the
released `jobs` revision.

## Rollback

```sh
bash deploy/aws/scripts/deploy.sh --rollback <previous-tag>
```

It redeploys earlier server, web, and app Caddy images without a snapshot or
migration, through the same maintenance-page window as a normal deploy. The
maintenance service keeps its currently registered Caddy image so rollbacks to
tags from before maintenance mode still serve the page. A rollback is safe only
when the failed release applied no migration. After a migration, fix forward
with a new release, or restore the database from the snapshot the failed deploy
took.

A rollback also runs through the fence-aware `deploy.sh`, never a copy from the
target tag, and is rejected the same way a forward deploy is when the target is
below the release fence minimum. Once passkey enrollment has ever been possible
in production, rolling back below that minimum is a forward fix or privileged
administration, not a supported rollback; see
[Passkey release fence](#passkey-release-fence).

A rollback cannot cross a document schema release. Every resume write persists
the current document version, and an older release fails closed on a version it
does not know. After a release that raises the document version, such as
document v4 (ADR 0044), any resume saved since the deploy is unreadable and
unwritable by the older release. Fix forward instead. A rollback past such a
release first needs every newer row lowered to the older version, and no tool
does that yet.

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

## Passkey release fence

[The release-fence contract](../design/passkey-release-fence.md) keeps a durable
minimum release in DynamoDB table `aboutme-prod-release-fence` (item id
`application`) and one nonexpiring operation lock on the same item. `deploy.sh`
assumes `aboutme-prod-operator`, which may only read the fence and assume
`aboutme-prod-deploy`, which holds every ECS, Scheduler, snapshot, fence, and
alarm-suppression permission the script needs. A normal deploy,
`--first-deploy`, and `--rollback` all read the fence, reject a target below its
minimum before any mutation, and hold the lock for the whole run, rechecking it
before every task registration, service, one-shot, schedule, alarm, and rule
mutation that starts an image. The maintenance page is the one exception: it
carries no release or enrollment logic and stays available as a recovery
fallback even when a checkpoint elsewhere fails. The lock releases only once
notifications are restored, using the exact operation ID this run acquired.

The same fence and lock gate TOTP enrollment at a second, higher floor (numeric
4007). [The TOTP keys runbook](totp-keys.md) covers the flag-off deploy, floor
activation, flag enablement, key rotation, the `aboutme-prod-totp-unavailable`
alarm, and the production proofs for that release.

The one-time bootstrap, before the first fence-aware deploy: set
`operator_principal_arn` in the ignored `prod.tfvars` to the owner's `aws login`
identity, run `sync.sh` in the `aboutme-infra` repository to back up that
change, then `tofu apply`.

Accepted residual risk: the deploy role's `ecs:RegisterTaskDefinition` is
unscoped and its `ecs:RunTask` covers every revision of the migrate, db-setup,
and jobs families, so it can register and run those families with an arbitrary
image or command. `iam:PassRole` still limits which task and execution roles
such a task can assume. This is accepted because the deploy role is reachable
only through the operator role, which the AWS-login principal alone may assume.

### Activation

Enrollment stays off in production until a healthy release at or above v0.4.2
runs with `PASSKEY_ENROLLMENT_ENABLED=false` and passes the first production
proof below. Only then:

```sh
bash deploy/aws/scripts/deploy.sh --activate <tag>
```

This requires `aboutme-prod-app` to already be stable and running exactly the
candidate release, acquires the lock, raises the fence to the tag's release with
one idempotent conditional update, and releases the lock. It makes no image,
ECS, snapshot, or notification change. A lower or malformed fence state, an
operation already in progress, or a running app that is not the exact stable
candidate, fails it before the raise. Only after it succeeds does a reviewed
`tofu apply` set `passkey_enrollment_enabled = true` in `prod.tfvars`; then
redeploy the same tag with a normal `deploy.sh <tag>` run. Enabling the flag
before the raise, or redeploying a different tag after it, is not the supported
order. If the raise succeeds but the apply or the redeploy then fails, the fence
stays raised; fix forward with the same or a newer capable tag, never a flag-off
image that predates the raise.

### Recovery

A script failure or signal restores alarm actions and the task-stopped rule,
then releases the lock it holds, the same as a successful run. Only an unhandled
process death (SIGKILL, host loss) skips this and leaves the lock closed. Before
clearing it by hand, using the AWS-login principal's own credentials, which
OpenTofu and account administration already trust as a privileged bypass:

1. Confirm no `deploy.sh` process is running.
2. Confirm no database task and no release snapshot from a prior run is still in
   flight:

   ```sh
   aws ecs list-tasks --region ap-southeast-1 --cluster aboutme-prod --started-by deploy-migrate
   aws ecs list-tasks --region ap-southeast-1 --cluster aboutme-prod --started-by deploy-db-setup
   aws ecs list-tasks --region ap-southeast-1 --cluster aboutme-prod --started-by deploy-totp-reencrypt
   aws rds describe-db-snapshots --region ap-southeast-1 --db-instance-identifier aboutme-prod \
     --query 'DBSnapshots[?Status!=`available`]'
   ```

   Every `list-tasks` call must return no task ARNs, and the snapshot query must
   return an empty list, before continuing.

3. Inspect `aboutme-prod-app`, `aboutme-prod-web`, `aboutme-prod-maintenance`,
   the job schedules, the `aboutme-prod-site-down` alarm, and the
   `aboutme-prod-task-stopped` rule against [Healthy state](#healthy-state).
4. Strongly read the fence item and record its `operation_id` and
   `operation_kind` with the reason for the clear:

   ```sh
   aws dynamodb get-item --region ap-southeast-1 --table-name aboutme-prod-release-fence \
     --key '{"id":{"S":"application"}}' --consistent-read
   ```

5. Remove only the operation attributes, conditioned on the exact `operation_id`
   read in step 4, so the clear cannot remove a different operation that started
   in between:

   ```sh
   aws dynamodb update-item --region ap-southeast-1 \
     --table-name aboutme-prod-release-fence --key '{"id":{"S":"application"}}' \
     --update-expression 'REMOVE operation_id, operation_kind, operation_started_at, operation_checked_at' \
     --condition-expression 'operation_id = :o' \
     --expression-attribute-values '{":o":{"S":"<operation_id from step 4>"}}'
   ```

This clear never lowers `minimum_release`. An old `deploy.sh` from a tag before
this contract, or any script run with the AWS-login or administrator credentials
directly, bypasses the fence entirely; prove from account and factor state that
a bypass target is compatible with what already ran, and record the reason,
before using one.

### Production proofs

GitHub CI cannot observe the deployed fence, the enrollment flag, live identity
providers, or the production origin. Two manual browser proofs cover what CI
cannot, each with the owner's standing v0.4.x authorization, a dedicated
fictional account, one browser profile, and the shared local-check lock and
memory floor:

- After the flag-off deploy and before activation: sign in with password and
  every enabled provider without a second factor, confirm enrollment stays
  hidden, and check existing sessions, connected agents, and health.
- After activation and the flag-on redeploy, using only the release's named
  fictional account: enroll a passkey, confirm pending login isolation,
  recovery, epoch revocation on a factor change, both locales, and remove the
  account's last factor.

Each proof starts no local product stack. On success, failure, or an unexpected
exit, remove the fictional account's proof factors, recovery plaintext, and
sessions before signing out; if cleanup cannot finish in the run, repeat it
alone before any further release action.

## Healthy state

- ECS services `aboutme-prod-app` and `aboutme-prod-web` each run one task;
  `aboutme-prod-maintenance` runs zero. `deploy.sh` is the only thing that
  changes any of their desired counts; OpenTofu ignores that drift.
- `https://aboutme.vn/healthz` and `/readyz` return 200 through Cloudflare, and
  `https://www.aboutme.vn/` redirects to the apex.
- A request straight to the Elastic IP gets no response.
- The five job schedules in group `aboutme-prod-jobs` are enabled.
- The `aboutme-prod-site-down` alarm (us-east-1) has actions enabled
  (`site_alarm_enabled = true` in `prod.tfvars`).
- `aboutme-prod-release-fence`'s `application` item has no `operation_id`
  between deploys.
