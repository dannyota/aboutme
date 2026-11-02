# Production

Production serves `https://aboutme.vn` on one GreenNode host in Vietnam under
[ADR 0026](../adr/0026-vietnam-hosted-production.md). The
[Vietnam production design](../design/vietnam-production.md) owns the topology.
Cutover completed on 2026-10-11 with v0.7.3. Nothing from `deploy/aws/` is
deployed.

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
Unit state and `/readyz` establish whether that release serves. Root-only host
operation state and audit live on the mounted data volume under
`/srv/data/host-operations/`; never copy personal audit into CI or the journal.

## Cloudflare DNS

Cloudflare provides DNS-only records on the Free plan. Apex and `www` A records
point to the floating IP with TTL 300. The separate OpenTofu root
`deploy/vn/dns/` owns the records. Follow the [DNS runbook](dns.md) for the
record inventory and Cloudflare DNSSEC. The owner adds Cloudflare's DS record at
the `.vn` registrar.

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

Every ordinary host installation runs the idempotent CrowdSec PostgreSQL
migration. Follow the [CrowdSec PostgreSQL runbook](crowdsec-postgresql.md) for
verification, failure recovery, and explicit reset after a cluster restore.

Use the current reviewed checkout's deployer. Publication and deployment share
`scripts/release-eligibility.sh`: the tag must be strict `vMAJOR.MINOR.PATCH`,
resolve to a commit on `main` fetched from `dannyota/aboutme`, and have a
completed, successful latest matching `ci.yml` push run on `main` for that exact
commit. A branch or manual run never counts. A newer pending, cancelled, or
failed matching run blocks an older success. Missing evidence or a fetch/API
error also blocks.

The accepted
[tooling and display changes](../design/local-tooling-simplification.md) leave
this exact-commit release rule unchanged. After integration, full CI also
requires `server-runtime-smoke` startup/idle evidence. It does not replace
release image checks, production smoke checks, or host memory observation.
Native macOS partial proofs cannot authorize tagging or deployment.

The image workflow runs the helper from a separate checkout of
`dannyota/aboutme` at `refs/heads/main` and binds the tag to the workflow's
event commit. A read-only job checks eligibility before publishing jobs start.
Every write-capable job repeats the check before other work, so a partial rerun
must pass fresh eligibility. Candidate checkouts supply build inputs only. The
workflow publishes arm64 and amd64 images; the Vietnam deploy requires amd64 and
pins image digests. GitHub holds no cloud credential other than the vMonitor
quota-renewal service account
([quota renewal design](../design/vmonitor-quota-renewal.md)).

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

vMonitor and the uptime backstop keep running during maintenance. Host alarms
and backup timers also remain active. A deploy can therefore send a site-down
notification.

Public-page stylesheets and hydration scripts use a build-time content hash.
Rename a changed fixed-name font under `/_nuxt/fonts/` so browsers fetch it.

## Host operations

Follow the [CrowdSec privacy notice runbook](crowdsec-privacy-notice.md) for
first adoption, initial reconciliation, notification, and activation. The
existing production campaign is complete; do not resend it or recapture its
cohort for the storage transition.

Account deletion first ships in v0.7.9. Publish the reviewed image, install its
reviewed bundle, then deploy it before use. Follow the
[account-deletion runbook](account-delete.md) for confirmation, closed output,
unknown-outcome recovery, public proof, and physical cleanup. Operations use
pinned SSH as `aboutme-admin`; their target and confirmation arrive only through
stdin to a fixed root entry point. Saved status never starts work.

The request service survives SSH loss and never restarts automatically. Resume
proves its old unit, containers, and matching database backends stopped before
reconciliation. An unresolved write retains the operation fence and capacity.
Known, quiescent deletion releases them before public proof and the media sweep.
The daily `aboutme-operation-retention.timer` erases expired personal evidence
at 180 days; abort can close an evidence-expired request only after quiescence.
Ordinary installation refuses unresolved requests and preserves saved state. Key
rotations and their repair/archive procedures ship separately.

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

Before taking fence ownership, take `/srv/data/host-operations/.bundle.lock`,
then the fence lock, and release the bundle lock after recording ownership.

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

The clear records its reason and never lowers the floor. Account-deletion
release and clear also refuse an unresolved request, active unit/container, or
matching database backend. Missing data mounts and key-change markers block
image starts. Recover through the owning request; neither manual clear nor an
EXIT trap bypasses these checks. A retained operation or mutation marker can
block serving and job starts across reboot. Inspect and recover the exact
mutation before clearing its lock. Do not delete fence files to restart the app.

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
from backup configuration alone. The
[CrowdSec reset procedure](crowdsec-postgresql.md#reset-after-a-cluster-restore)
requires the operator to discard restored attack data, statistics, and
registrations before consumer restart. Whole-cluster backups include CrowdSec
under its accepted recovery and expiration policy.

## Alarms

GreenNode vMonitor check `aboutme-readyz` is the site-down alarm. It sends GET
`https://aboutme.vn/readyz` every minute from public locations `SYNTT-VN-HCM01`
and `SYNTT-VN-HAN01`, with a 10-second timeout. The default assertion fails on
4xx or 5xx responses. Three consecutive failures at either location trigger an
alarm. In-alarm, Up, and Undetermined notifications go to Telegram channel
`aboutme-alerts`, which targets the owner's private alert group.

To confirm delivery safely, create a temporary check against a URL that returns
404, using the same locations, failure threshold, and Telegram channel. Wait for
its In-alarm notification to reach the owner, record the receipt time, then
delete the temporary check. Do not interrupt production to test delivery.

The Basic Synthetic Test quota is free and lasts at most one month. The server
refuses auto-renew for free packages. Pro and Enterprise packages are disabled.
The email notification quota was deleted. Telegram needs no notification quota;
each send increments the synthetic usage counter's Telegram count.

| Quota                | Allowance | Expiry recorded 2026-11-01 (UTC) |
| -------------------- | --------- | -------------------------------- |
| Synthetic Test Basic | 10 checks | 2026-12-01T06:54:54Z             |

The [renewal workflow](../design/vmonitor-quota-renewal.md) supports dispatch;
due-renewal proof and scheduling remain pending. Renew each month in the last
seven days; the next window is 2026-11-24T06:54:54Z to 2026-12-01T06:54:54Z.
Follow the design's existing hold procedure before any manual renewal:

1. Open the vMonitor console, then **Quota & Usage**.
2. Select **Synthetic test** and renew the free Basic quota. The pinned
   `vngcloud` CLI in the renewal design is also available.
3. Confirm the quota is active and record its new expiry date.

A lapsed synthetic quota stops checks. If it lapses, recover Basic from trash at
0 VND, confirm it is active, and record the new expiry. Deleting the synthetic
quota deletes all checks, so recreate them from a saved configuration. The
current quota is active from 2026-11-01T06:54:54Z to 2026-12-01T06:54:54Z. The
previous quota expired on 2026-10-25 and was recovered on 2026-11-01.

[`.github/workflows/uptime.yml`](../../.github/workflows/uptime.yml) is a slower
backstop. It requests a five-minute schedule, tries `/readyz` three times, 20
seconds apart, and passes only on HTTP 200. A failed run makes GitHub email the
account that last changed the cron line. Scheduled runs have arrived 3 to 10
hours apart. GitHub can delay or drop scheduled runs and disables scheduled
workflows in public repositories after 60 days without repository activity.
Check its enabled state and run history after an idle period. See
[GitHub's schedule documentation](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#schedule).

Host alarms use `aboutme-watch.timer`, `aboutme-watch.service`, and
`aboutme-alert@.service`. The watcher runs every five minutes and checks:

- Root and data volume use above 70%, failed units, and PostgreSQL on its
  socket.
- WAL archive failure, unreadable backups, and newest backup age at 26 hours.
- CrowdSec retention/vacuum timers and lease, CrowdSec alert counts, and
  malformed Coraza logs or unknown record kinds in the last hour.
- `totp_unavailable` in the server journal.

Operation request and retention units also trigger `OnFailure=` mail. Their
journal output contains no target or raw command diagnostics; inspect closed
status and the root-only audit for an operation outcome. Media sweep failures
leave physical cleanup pending.

Unit failures also trigger mail through `OnFailure=`. `alert.sh` sends through
Bizfly SMTP with `msmtp`, at most once per condition per UTC day. A send failure
removes the stamp so a later call can retry. Mail carries names and counts,
never raw journal lines or personal data. Read details on the host:

```sh
sudo journalctl -u aboutme-watch -u aboutme-server --since -30min
sudo systemctl list-timers 'aboutme-*'
```

`coraza-matches` and `coraza-unparsed` do not send mail. Each watcher run writes
one journal line with match and unparsed counts for the preceding hour:

```sh
sudo journalctl -u aboutme-watch --since -1h -o cat
```

The line contains only `coraza counts in the last hour: matches=N unparsed=N`,
including zeros. A malformed log or unknown record kind still sends
`coraza-log-invalid` mail; inspect the WAF log format and reader on the host.

Coraza counts rule-match records, not distinct requests. Its metadata lives in
`/run/aboutme/caddy-log/waf/` on tmpfs, in current and rotated serving and
maintenance logs. Summarize timestamps, levels, message kinds, and rule IDs;
these records do not establish response status or successful access. Check the
configured Coraza mode before interpreting a match as a blocked request.

### Host Telegram setup and rotation draft

Proposed only. Use after owner acceptance and reviewed implementation of the
[host alert contract](../design/vietnam-production.md#proposed-host-telegram-alerts).
Commands require the proposed helper support.

1. Reuse the bot whose token is `TELEGRAM_BOT` in the ignored local `.env`. Do
   not create a bot. The owner adds it to the existing vMonitor `aboutme-alerts`
   supergroup with send permission before any test. vMonitor uses VNG's own bot;
   leave that bot and its notification configuration intact.
2. Install reviewed secret-helper support with SMTP still active. On the trusted
   owner machine, parse only the literal `TELEGRAM_BOT` value without sourcing
   `.env`; pipe it without a newline directly into
   `bash deploy/vn/scripts/secrets.sh import telegram-bot-token` stdin.
3. Pipe `vngcloud monitor list-channels --output json` into a filter selecting
   `.Items[]` with `Name == "aboutme-alerts"`. Require exactly one Telegram
   channel and a valid numeric supergroup `.Address`; pipe that address without
   a newline directly into
   `bash deploy/vn/scripts/secrets.sh import telegram-chat-id` stdin. Fail
   closed on missing, duplicate, malformed, or unexpected results.
4. Use existing `DEPLOY_HOST`. Both pipelines must suppress value-bearing errors
   and fail on any stage error. Never print, inspect, tee, or save values or raw
   responses; no temporary input files, shell tracing, or terminal recording.
   Check secret names and permissions only. Refresh the age-encrypted copy by
   pipe, with no plaintext intermediate. Telegram secrets never enter Podman.
5. Stage `/usr/local/lib/aboutme/alert-telegram.sh` beside SMTP `alert.sh`;
   `watch.sh` and `aboutme-alert@.service` must still call SMTP `alert.sh`. Run
   the staged sender with `--test` as the host administrator. Require exit 0 and
   owner confirmation of the fixed message in the intended supergroup. Record
   only UTC receipt time and success/failure, never values or screenshots.
6. Only after that receipt, switch both automatic callers to `alert-telegram.sh`
   and reload the systemd unit. Preserve daily stamps. Then remove host-alert
   `ALERT_TO` and the msmtp requirement; keep application SMTP credentials and
   settings. Failed staging or proof leaves SMTP active and cutover incomplete.
   For rollback after switching, restore both SMTP caller paths and reviewed
   mail configuration/package, reload systemd, and prove mail with a fresh
   synthetic key. Preserve other host safety changes and stamps; do not restart
   the app.

For rotation, revoke the existing bot's token in BotFather and update only
`TELEGRAM_BOT` in the ignored local `.env`. Repeat its pipe-only import with
`--replace`, refresh the encrypted copy by pipe, and repeat the staged sender's
`--test` with owner receipt. Coordinate other senders using this bot; sends may
fail during rotation. No app restart is needed. To replace the destination,
require owner approval of the vMonitor channel change, repeat the same channel
lookup and pipe-only import with `--replace`, and prove membership and receipt.
Never accept API-suggested destination changes. After restore, verify membership
and current credentials without displaying values; revoke exposed tokens and
repeat the same rotation path. Inspect only fixed journal outcomes and local
DNS/HTTPS reachability. Origin HTTP success does not prove alert delivery.

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
- The four job timers, backup timers, host watcher, and both CrowdSec
  maintenance timers are enabled and active, with normal scheduled passes
  observed.
- The fence has no operation lock or mutation marker between operations.
- vMonitor `aboutme-readyz` is healthy, the synthetic quota is active, and the
  uptime backstop is enabled with recent successful runs.
