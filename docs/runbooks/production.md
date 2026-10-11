# Production

Production serves `https://aboutme.vn` on one GreenNode host in Vietnam under
[ADR 0027](../adr/0027-vietnam-hosted-production.md). The
[Vietnam production design](../design/vietnam-production.md) owns the topology.
Cutover completed on 2026-10-11 with release v0.7.3. The AWS production stack
and its state bootstrap were destroyed that day. Nothing from `deploy/aws/` is
deployed; the code remains for a possible AWS ECS or Kubernetes path.

## Access and state

SSH uses `aboutme-admin` on TCP 22922, with the owner's `ssh-ed25519` key and
`sudo`. Root and password login are disabled. CrowdSec bans SSH attacks. The
GreenNode web console provides break-glass access.

Set `DEPLOY_HOST` to the host's floating IP for laptop scripts. `SSH_PORT`
defaults to 22922. OpenTofu lives in `deploy/vn/prod`; ignored `backend.hcl` and
`prod.tfvars` hold backend and environment settings. State is encrypted on
GreenNode vStorage. Follow the [host guide](../../deploy/vn/README.md) for
credentials, quotes, and host installation.

Read the installed release and unit state on the host:

```sh
sudo /usr/local/lib/aboutme/deploy-host.sh - - release-json
sudo systemctl status aboutme-caddy aboutme-server aboutme-web
```

`/var/lib/aboutme/release.json` records the installed tag and image digests.
Unit state and `/readyz` establish whether that release serves.

## Cloudflare DNS

Cloudflare provides DNS-only records on the Free plan. Apex and `www` A records
point to the floating IP with TTL 300. Records are edited by hand; OpenTofu
ownership remains open. Follow the [DNS runbook](dns.md) for the record
inventory and Cloudflare DNSSEC. The owner adds Cloudflare's DS record at the
`.vn` registrar.

Caddy terminates TLS on 443 with Let's Encrypt, using ACME HTTP-01 on port 80.
DNS must point at the host before the edge starts so validation can reach it.
Production uses `EDGES=direct`: Caddy takes the socket address, strips
forwarding headers, and emits one `X-Real-IP` to Go on loopback. Coraza and
CrowdSec run on the host. The [CloudFront runbook](cloudfront.md) is an AWS
reference.

## Secrets

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
- `PASSWORD_REGISTRATION_ENABLED=false` disables new email registration. Login,
  password reset, pending verification, and provider sign-up keep working. Set
  `true` and redeploy to restore registration.
- Passkey, TOTP, and sign-in-to-view flags require their release-fence floors.
  See [Passkey release fence](#passkey-release-fence).

The app sends through Bizfly SMTP as `Danny from aboutme.vn <danny@aboutme.vn>`.
Replies reach Google Workspace. The [email runbook](email.md) owns configuration
and delivery checks.

## Deploy

Use the current reviewed checkout's deployer. Publication and deployment share
`scripts/release-eligibility.sh`: the tag must be strict `vMAJOR.MINOR.PATCH`,
resolve to a commit on `main` fetched from `dannyota/aboutme`, and have a
completed, successful latest matching `ci.yml` push run on `main` for that exact
commit. A branch or manual run never counts. A newer pending, cancelled, or
failed matching run blocks an older success. Missing evidence or a fetch/API
error also blocks.

The image workflow runs the helper from a separate checkout of
`dannyota/aboutme` at `refs/heads/main` and binds the tag to the workflow's
event commit. A read-only job checks eligibility before publishing jobs start.
Every write-capable job repeats the check before other work, so a partial rerun
must pass fresh eligibility. Candidate checkouts supply build inputs only. The
workflow publishes arm64 and amd64 images; the Vietnam deploy requires amd64 and
pins image digests. GitHub holds no cloud credentials.

Before SSH or registry access, the deployer also requires its checkout to be on
`main`, clean (including untracked files), and equal to freshly fetched
`origin/main`. Use that checkout for rollbacks and activation too; the target
tag may be older. Ignored local configuration does not make the checkout dirty.

```sh
DEPLOY_HOST=<host> bash deploy/vn/scripts/deploy.sh <tag>
```

The deployer checks tag-bound build provenance and requires the installed
`fence.sh`, `deploy-host.sh`, and `deploy-safety.sh` to match the checkout. If
they differ, install the reviewed host bundle before retrying.

The deployer then:

1. Takes the host operation lock and rejects a tag below the release floor.
2. Pulls images while the old release serves and checks every referenced secret
   by name.
3. Takes a pgBackRest incremental backup annotated with the tag.
4. Stops job timers and active jobs. Starts maintenance beside Caddy with shared
   ports and certificate storage, then stops serving Caddy and Go.
5. Requires the marked 503 maintenance page from outside the host.
6. Renders the new units, runs migrations, and requires exit 0. `--first-deploy`
   runs `db-setup` before migrations.
7. Restarts Nuxt, starts Go, and waits for `/readyz` at `127.0.0.1:8080` on the
   host. Starts Caddy, then stops maintenance and proves it stopped.
8. Starts the four job timers. Checks public health, readiness, TLS, HSTS,
   `nosniff`, and a CrowdSec 403 for the operator's test ban. Releases the lock.

The external uptime workflow keeps running during maintenance. Host alarms and
backup timers also remain active. A deploy can therefore send a site-down mail.

Public-page stylesheets and hydration scripts use a build-time content hash.
Rename a changed fixed-name font under `/_nuxt/fonts/` so browsers fetch it.

## Failed deploy

Before a migration request, a failed deploy restores the previous release and
job timers through maintenance. A failed first deploy keeps maintenance up. Once
a migration may have run, the deployer keeps maintenance up with the app and
jobs stopped. Recover with a forward fix or point-in-time restore. A failed
recovery retains the operation lock. Inspect units and the host journal before
retrying.

## Rollback

```sh
DEPLOY_HOST=<host> bash deploy/vn/scripts/deploy.sh --rollback <previous-tag>
```

Rollback uses the same lock and maintenance path, with no backup or migration.
It keeps the current maintenance image. Rollback is safe only when the failed
release applied no migration, the tag meets the release floor, and the older
server can read every stored document version. No tool lowers newer document
rows for an older server. Fix forward when those conditions do not hold.

## Passkey release fence

`/var/lib/aboutme/fence.json` holds the monotonic minimum release and a
nonexpiring operation lock outside PostgreSQL. `fence.sh` writes it under
`flock`, with an atomic rename. A crash leaves the lock closed. Every supported
deploy, rollback, and activation takes the lock; host image starts check the
floor before Podman runs.

Activation requires the exact candidate server to be running:

```sh
DEPLOY_HOST=<host> bash deploy/vn/scripts/deploy.sh --activate <tag>
```

Deploy a capable release with its feature flag off, prove it healthy, activate
the floor, then enable the flag in `/etc/aboutme/flags.env` and redeploy the
same tag. The floors are v0.4.2 for passkeys, v0.4.7 for TOTP, and v0.6.26 for
sign in to view. A raised floor stays raised if a later step fails.

Before clearing a stale lock, prove that no laptop deploy, host deploy process,
database command, or media writer from that operation can still run. Inspect
release state, unit state, the fence, and any mutation marker. On the host:

```sh
sudo /usr/local/lib/aboutme/fence.sh clear '<reason>'
```

The clear records its reason and never lowers the floor. It refuses a retained
cutover mutation marker. A retained cutover lock or marker also blocks serving
and job starts across reboot; maintenance remains available. Inspect and recover
the exact mutation before clearing its lock. Do not delete fence files to
restart the app.

## Backups and restore

PostgreSQL 18 runs on the encrypted data volume and listens only on
`/run/postgresql`. pgBackRest archives WAL to an encrypted private GreenNode
vStorage repository in HCM04. It keeps 30 days by time, runs a full backup
weekly, a differential daily, and backup verification weekly. Deploys add
incremental backups. `archive_timeout=60s` bounds normal WAL shipping delay.

RDS was deleted without a final snapshot on 2026-10-11. Production data lives
only on the Vietnam host and its pgBackRest backups in GreenNode vStorage. There
is no AWS recovery copy.

The quarterly [restore drill](../../deploy/vn/README.md#restore-drill) restores
the latest backup and a point-in-time target to a temporary encrypted server.
Outbound networking for that server remains open; do not claim a completed drill
from backup configuration alone.

## Alarms

[`.github/workflows/uptime.yml`](../../.github/workflows/uptime.yml) checks
`https://aboutme.vn/readyz` every five minutes. It tries three times, 20 seconds
apart, and passes only on HTTP 200. A failed run makes GitHub email the account
that last changed the cron line.

GitHub documents that scheduled runs can be delayed under load, five minutes is
the shortest interval, and scheduled workflows in a public repository are
disabled after 60 days without repository activity. See
[GitHub's schedule documentation](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#schedule).
A delayed or disabled workflow does not prove the site healthy. Check the
workflow's enabled state and run history after an idle period.

Host alarms use `aboutme-watch.timer`, `aboutme-watch.service`, and
`aboutme-alert@.service`. The watcher runs every five minutes and checks:

- Root and data volume use above 70%, failed units, and PostgreSQL on its
  socket.
- WAL archive failure, unreadable backups, and newest backup age at 26 hours.
- CrowdSec retention timer and lease, CrowdSec alert counts, and Coraza match
  counts and malformed records in the last hour.
- `totp_unavailable` in the server journal.

Unit failures also trigger mail through `OnFailure=`. `alert.sh` sends through
Bizfly SMTP with `msmtp`, at most once per condition per UTC day. A send failure
removes the stamp so a later call can retry. Mail carries names and counts,
never raw journal lines or personal data. Read details on the host:

```sh
sudo journalctl -u aboutme-watch -u aboutme-server --since -30min
sudo systemctl list-timers 'aboutme-*'
```

## Vulnerability scans

[The security scan](../../.github/workflows/security-scan.yml) runs weekly. Go
checks cover the server, generated schema module, observer, and Caddy build
module. npm audits cover the root, web, schema, and browser-proof dependencies.
Dependency checks use the workflow checkout: the default branch on scheduled
runs, or the selected branch on manual runs.

Image scans cover both arm64 and amd64 for server, web, Caddy, and observer. The
scan selects GitHub's latest published release and reads its `digests.txt` asset
by asset ID. Every image scan uses the listed platform digest. The observer's
arm64 digest belongs to the plain version tag; its amd64 digest belongs to
`<tag>-amd64`. The job summary records the selected app and observer releases
and each scanned digest. JSON reports and release digest lists remain in the
workflow artifact for seven days.

To check a rollback release or an observer retained from an older release,
dispatch with explicit versions:

```sh
gh workflow run security-scan.yml --ref main \
  -f release_tag=<app-release> -f observer_release_tag=<observer-release>
```

Omit `release_tag` to select the latest published release. Omit
`observer_release_tag` to use the selected app release for both observer
platforms. Both inputs require a published `vMAJOR.MINOR.PATCH` release and its
digest asset. A reachable tag alone does not qualify.

Missing or duplicate platform digests, invalid reports, scan-engine errors, and
fixable HIGH or CRITICAL image findings fail the scan. Other targets still run.
Observer publication is optional, so a release missing either observer digest
fails coverage unless an explicit observer release supplies both.

These are release scans, not deployed-version checks. The workflow does not read
the host's installed release or retained observer version. Use the host's
release record and observer unit to choose those versions before dispatching.

## Reboot

Use the GreenNode API to reboot the server. An in-guest `systemctl reboot` hung
once. Readiness stayed failed once after a hard reset until `aboutme-server`
restarted. The cause remains open. If the same failure occurs, inspect the
journal and fence before restarting the server unit, then check public readiness
again.

## Healthy state

Healthy state has these properties:

- `aboutme-caddy`, `aboutme-server`, and `aboutme-web` are active;
  `aboutme-maintenance` is inactive.
- Public `/healthz` and `/readyz` return 200, and `www` redirects to the apex.
- The four job timers, backup timers, host watcher, and CrowdSec retention timer
  are enabled and active.
- The fence has no operation lock or mutation marker between operations.
- The uptime workflow is enabled and has recent successful runs.
