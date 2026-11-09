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

The [secrets and flags runbook](secrets.md) owns secret files, imports and
rotations, feature flags in `/etc/aboutme/flags.env`, and the mail sender.

## Deploy

The [production deploy runbook](deploy.md) owns release eligibility, the
deployer's steps, failed deploys, rollback, and the passkey release fence.

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

See [failed deploy](deploy.md#failed-deploy).

## Rollback

See [rollback](deploy.md#rollback).

## Passkey release fence

See [passkey release fence](deploy.md#passkey-release-fence).

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

Each watcher run journals hourly totals without sending mail:

```sh
sudo journalctl -u aboutme-watch --since -1h -o cat
```

`coraza counts in the last hour: matches=N unparsed=N diagnostics=N` is the only
output, including zeros. Malformed logs or unknown record kinds still send
`coraza-log-invalid` mail; inspect the WAF log format and reader on the host.

Coraza counts rule-match records, not distinct requests. Inspect current and
rotated serving and maintenance metadata logs on tmpfs at
`/run/aboutme/caddy-log/waf/` for timestamps, levels, message types, diagnostic
kinds, and rule IDs. These records do not prove response status or successful
access. Check the configured Coraza mode before treating a match as blocked.

### Host Telegram setup and rotation

Keep application SMTP and VNG's bot. The
[operator reference](../design/telegram-host-alerts.md#operator-reference)
covers the existing bot/group, reviewed installation, custody, and cleanup. On
the host:

```sh
sudo /usr/local/lib/aboutme/alert-transport.sh stage
```

On the trusted laptop, with `DEPLOY_HOST` exported:

```sh
bash deploy/vn/scripts/telegram-setup.sh
bash deploy/vn/scripts/secrets.sh age-copy "$AGE_RECIPIENT" "$ENCRYPTED_BACKUP"
```

On the host, require exit 0 and owner receipt in the intended group of
`[aboutme-prod] host alert delivery test` before confirmation:

```sh
sudo /usr/local/lib/aboutme/alert-telegram.sh --test </dev/null
sudo /usr/local/lib/aboutme/alert-transport.sh confirm-receipt --received
```

For rotation, coordinate token revocation with other senders and update only
local `TELEGRAM_BOT`; destination changes need owner approval. On the laptop:

```sh
bash deploy/vn/scripts/telegram-setup.sh --replace
```

Refresh encrypted custody as above, using a fresh output path. Keep Telegram
selected; do not run `stage`. On the host, require fresh owner receipt:

```sh
sudo /usr/local/lib/aboutme/alert-telegram.sh --test </dev/null
sudo /usr/local/lib/aboutme/alert-transport.sh confirm-receipt --received
```

For rollback, restore reviewed `/etc/aboutme/smtp.env` including `ALERT_TO`,
SMTP secrets, and `msmtp` first. On the host, switch and prove mail receipt:

```sh
sudo /usr/local/lib/aboutme/alert-transport.sh stage
sudo /usr/local/lib/aboutme/alert.sh "rollback-$(date -u +%s)" \
  'host alert rollback test' </dev/null
```

## Vulnerability scans

The [vulnerability scan runbook](vulnerability-scans.md) covers the weekly
security scan and explicit release dispatch.

## Reboot

Use the GreenNode API to reboot the server. Chromium's two-second version probe
timed out at boot; startup allows one timeout retry in a four-second probe
budget, plus joined teardown. Inspect the journal and fence before restarting
`aboutme-server`, then check public readiness.

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
