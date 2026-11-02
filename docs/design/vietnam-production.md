# Vietnam production

Production at `https://aboutme.vn` runs on GreenNode in Vietnam, with Bizfly
Email Transaction, under [ADR 0026](../adr/0026-vietnam-hosted-production.md).
The [production runbook](../runbooks/production.md) owns operator procedures.

Status: serving production. Cutover completed on 2026-10-11 with release v0.7.3.
**Unconfirmed** marks a provider fact that the repository does not prove.

## Architecture

The request path is browser or MCP client, the host's floating IP, Caddy on 443
(TLS, Coraza, CrowdSec bouncer), then Go on `127.0.0.1:8080` or Nuxt on
`127.0.0.1:3000`. Go reaches PostgreSQL on the same host by Unix socket, media
in vStorage, and Bizfly Email Transaction by SMTP. pgBackRest ships backups and
WAL to a second vStorage bucket. Logs stay in journald on the host; a host timer
mails alarms through Bizfly.

| Concern            | Production                                                          |
| ------------------ | ------------------------------------------------------------------- |
| Edge, TLS, cache   | Caddy on the host: Let's Encrypt, Coraza, CrowdSec; no edge service |
| DNS                | Cloudflare Free plan, DNS-only records, DNSSEC                      |
| Compute            | One amd64 vServer, Podman containers under systemd                  |
| Database           | PostgreSQL 18 on the vServer, pgBackRest to vStorage                |
| Media              | Private vStorage bucket, static key for one service account         |
| Secrets            | Root-only host files, age-encrypted copy in `aboutme-infra`         |
| Release fence      | Root-owned host file plus a start-time check                        |
| Jobs               | systemd timers running one-shot containers                          |
| Logs and alarms    | journald, host mail alert units, vMonitor; GitHub backstop          |
| Transactional mail | Bizfly Email Transaction over SMTP                                  |
| Support mailbox    | Google Workspace                                                    |
| Infrastructure     | OpenTofu `deploy/vn/`, `vngcloud` CLI, vStorage state; DNS root     |

Compute and network run in HCM03 and vStorage in HCM04, both Ho Chi Minh City
(owner, 2026-10-10); vStorage exists only in HCM04 and HAN02.

One serving replica ([ADR 0025](../adr/0025-replica-scaling.md)), the migrator
and roles ([ADR 0005](../adr/0005-database-migrations.md)), the public
revocation gate ([ADR 0010](../adr/0010-public-artifact-revocation.md)), private
media ([ADR 0009](../adr/0009-private-media-delivery.md)), and the HTTP, SSE,
and MCP contracts apply. Public images stay on GitHub Container Registry; they
hold no personal data.

## Edge

Nothing stands in front of the host (owner, 2026-10-10). The apex and `www`
resolve to the floating IP; Caddy terminates TLS with Let's Encrypt certificates
by ACME HTTP-01, and serves hashed `/_nuxt/*` assets with Nuxt's immutable cache
headers. The security group admits TCP 80 and 443 from anywhere and TCP 22922
from all sources (owner, 2026-10-10). Caddy takes the client address from the
socket, strips every forwarding header, and sends one `X-Real-IP` to Go; Go
keeps trusting only loopback. The CloudFront listener with
`CloudFront-Viewer-Address` remains in the code for the AWS path. Production
uses `EDGES=direct`.

Three host-level controls replace AWS WAF:

- Go's rate limits stay the application control.
- CrowdSec's engine, Caddy bouncer, and nftables SSH bouncer ban attacks; Caddy
  returns 403. Its [HTTP privacy contract](crowdsec-http-privacy.md) uses a
  bounded RAM-only feed with conditional rejected-target samples and disables
  community sharing. The owner approved the feed and PostgreSQL amendment on
  2026-10-18; its contract owns live retention, backups, the storage switch, and
  recovery.
- Coraza 3.8.1 in Caddy (`coraza-caddy` with OWASP CRS v4) detects for two weeks
  before blocking. Raw audit logging stays off because it can record body data.
  Serving writes `/var/log/caddy/waf/serving-match.log`; maintenance writes
  `/var/log/caddy/waf/maintenance-match.log`; both are on tmpfs. Rule callbacks
  emit `msg=waf_rule_match` with numeric `rule_id`; other unknown shapes emit
  `msg=waf_rule_match_unparsed`. Coraza shares its engine logger with the rule
  callback. An exact allowlist of fixed engine and connector diagnostics,
  including the missing-key macro warning, body processing errors, and the
  connector's interruption summary, emits `msg=waf_engine_diagnostic`.
  Diagnostics have only `ts`, `level`, `logger`, and `msg`, no `rule_id` or raw
  fields; the watcher excludes them from rule counts. WAF records carry no raw
  fields or personal data and stay on tmpfs. Only aggregate match and unparsed
  counts reach the journal. Files cap at 5 MiB plus two rolls; one-day expiry
  applies only to rolled files, so a low-volume active file persists until
  rotation or reboot. After two weeks, devops reviews matches, excludes false
  positives on resume writes, and enables blocking.

None of these absorbs an HTTP flood larger than the host. GreenNode vWAF is the
escalation: an application in the root portal with the floating IP as upstream,
live after `www` becomes a CNAME to `<code>.waf.greennode.vn` and the apex an A
record to vWAF's address. The
[escalation checklist](../../deploy/vn/edge/vwaf-escalation.md) holds the steps.
**Unconfirmed:** network-level DDoS protection for a vServer floating IP (Q1),
and IPv6 on the floating IP (Q2).

## DNS and mail

Cloudflare is authoritative for `aboutme.vn` on the Free plan. Every record is
DNS-only. Apex and `www` are A records to the host's floating IP with TTL 300.
Cloudflare signs the zone with DNSSEC algorithm 13; the owner adds its DS record
at the `.vn` registrar. The separate OpenTofu root `deploy/vn/dns/` owns the
records; DNSSEC stays manual. The [DNS runbook](../runbooks/dns.md) owns the
record inventory and signing checks.

Google Workspace receives replies at `danny@aboutme.vn`. Bizfly Email
Transaction sends application mail. The [email runbook](../runbooks/email.md)
owns mail DNS and SMTP configuration.

Go uses SMTP (`AUTH_EMAIL_MODE=smtp`), chosen by the owner on 2026-10-10 because
a local stub can test its standard contract. The
[email runbook](../runbooks/email.md) owns TLS, credentials, sending, and
failure behavior. Bizfly permits only its account address as sender and refused
a second domain sender. Auth mail therefore uses
`Danny from aboutme.vn <danny@aboutme.vn>` (owner, 2026-10-11), with replies to
Google Workspace.

## Host

One amd64 vServer runs Ubuntu 24.04 LTS with 2 vCPU and 4 GiB (owner,
2026-10-10). Its [memory and storage budget](vietnam-host-memory.md) caps each
service, serializes jobs, and gives Caddy logs a 64 MiB tmpfs. OpenTofu owns a
30 GB encrypted root disk, a 20 GB encrypted SSD data volume, and the floating
IP. Both disks use GreenNode encryption (aes-xts-plain64 256), chosen at server
create (owner, 2026-10-10). Only the OS, public images, and caches use root.
Secrets, PostgreSQL, CrowdSec state, and journald use the data volume. Either
disk grows online at its 70 percent alarm. Release images add `linux/amd64`
beside `linux/arm64`, because GreenNode offers no ARM vServer (no ARM flavor on
the account, 2026-10-10).

Rootful Podman runs every container from systemd Quadlet units:

| Unit                  | Network            | Runs                                           |
| --------------------- | ------------------ | ---------------------------------------------- |
| `aboutme-caddy`       | host               | Caddy on 80, 443; always                       |
| `aboutme-server`      | host               | Go on `127.0.0.1:8080`; always                 |
| `aboutme-web`         | bridge `aboutme`   | Nuxt, published on `127.0.0.1:3000`; always    |
| `aboutme-maintenance` | host               | Maintenance Caddy; deploy and recovery only    |
| `aboutme-migrate`     | none, socket mount | One-shot `migrate apply` as `aboutme_migrator` |
| `aboutme-db-setup`    | none, socket mount | First deploy only                              |
| `aboutme-job@<name>`  | socket mount       | One-shot privacy and media jobs                |

The host enforces these trust boundaries:

- Only Caddy and Go share the host loopback, and Go trusts only `127.0.0.1`.
- Nuxt has its own network namespace and reaches only Go's print listener on the
  `aboutme` bridge gateway, where the one-use capability applies.
- `aboutme-caddy` and `aboutme-maintenance` do not conflict. A deploy runs them
  side by side on ports 80 and 443 with `SO_REUSEPORT`, which Linux allows only
  for sockets of one effective user, so both run as the same user. They share
  one Caddy storage directory. Either process then answers an ACME HTTP-01
  challenge for the other's order and they do not race to issue; with separate
  storage, each would order its own certificate and a challenge could reach the
  process that did not order it, so issuance fails or hits rate limits.
- One-shot containers reach PostgreSQL only through the mounted socket; only
  media jobs get outbound network, for vStorage.
- Chromium keeps its sandbox and its blocked proxy; the probe checks the user
  namespaces it needs.

Access is key-only SSH on TCP 22922 from all sources as `aboutme-admin` with
`sudo`; root login is disabled and `AllowUsers` permits only `aboutme-admin`.
The key is the owner's `ssh-ed25519` commit-signing key. CrowdSec bans SSH
attacks (owner, 2026-10-10). The GreenNode web console provides break-glass
access. Security updates install daily; a monthly maintenance window reboots.

## Secrets and host identity

There is no instance role or parameter store. Each secret is a root-owned 0400
file under `/etc/aboutme/secrets/`, a directory on the encrypted data volume,
loaded as a Podman secret and given only to the units that need it. The host
templates name each unit's secret set. Runtime values include:

| Value                                   | Units                                     |
| --------------------------------------- | ----------------------------------------- |
| vStorage media key (service account)    | `aboutme-server`, all four scheduled jobs |
| vStorage backup key and pgBackRest pass | pgBackRest on the host                    |
| Bizfly SMTP user and password           | `aboutme-server`                          |
| CrowdSec bouncer API key                | `aboutme-caddy`, `aboutme-maintenance`    |

`deploy/vn/scripts/secrets.sh` runs over SSH. It generates missing application
keys on the host with `openssl` and imports provider credentials from standard
input. Supported rotations require `--replace`. The script never prints a value.
`age-copy` exports a copy encrypted to the owner's age recipient; the laptop
keeps only that ciphertext in `aboutme-infra`.

All four scheduled jobs receive the media keys because server configuration
requires them when `MEDIA_ENDPOINT` is set. Only the two media jobs have
outbound network access; the database jobs run with `--network none`.

Each vStorage service account has a policy for one bucket: media (get, put,
list, delete), backups, or state. The state key, the OpenTofu provider's
credentials, and the Bizfly API key stay on the laptop.

## PostgreSQL

Managed vDB offers PostgreSQL 17 at most, 2 to 14 days of backups, and no
point-in-time recovery (**Unconfirmed**). The schema needs PostgreSQL 18's
built-in `uuidv7()`, and the recovery contract is 30 days, so production runs
PostgreSQL 18 from the PGDG packages, pinned to the minor version.

- Data lives on the encrypted data volume. `listen_addresses = ''`, so the only
  listener is the Unix socket, and no connection needs TLS. `pg_hba.conf` allows
  `scram-sha-256` for `aboutme`, `aboutme_migrator`, and `aboutme_app`, and
  `peer` for `postgres`. `DATABASE_URL` uses `host=/run/postgresql`.
- The `aboutme` role owns the database with `CREATEROLE`, like the RDS master,
  and runs `db-setup`. Only pgBackRest and restores use the `postgres`
  superuser, apart from provisioning the separate CrowdSec database and role.

The approved CrowdSec architecture uses a separate `crowdsec` database and
least-privilege role in this cluster, with Unix-socket peer authentication. Its
[storage and privacy contract](crowdsec-http-privacy.md) owns the grants,
retention gate, backup inclusion, release order, and fresh state after restore.
Approval does not establish that production cutover has completed.

pgBackRest archives WAL continuously to a private backup bucket with
`archive_timeout = 60s`, so about a minute of commits is at risk. It runs a full
backup weekly, a differential daily, and `pgbackrest verify` weekly, keeps 30
days by time, and encrypts every file client-side (`aes-256-cbc`) before upload.
An alarm fires on an archive failure or a newest backup at least 26 hours old.
The repository uses S3 path-style addressing. One repository in HCM04 (owner,
2026-10-10): the only other vStorage region is Hanoi, so a regional failure
means waiting for HCM04 to return.

The quarterly restore drill targets the latest backup and a point-in-time target
from the previous day on a temporary vServer. It checks the goose version, row
counts per table, the catalog grant test, and a store smoke as `aboutme_app`,
records the recovery time, and deletes the server. The drill has no outbound
internet because it has no floating IP or NAT. Outbound networking remains open;
a successful restore drill is not recorded here. See the
[host guide](../../deploy/vn/README.md#restore-drill).

## Client metadata authorization

`deploy/vn/host/env/server.env` defaults `OAUTH_CIMD_ENABLED=false`; the owner
may override it in `/etc/aboutme/flags.env`. CIMD is enabled in production, and
the owner completed the
[Claude Code proof](mcp-cimd-verification.md#production-enablement-proof).
Production refuses `OAUTH_CIMD_DEV_STUB_ADDR`, even with the feature disabled.

Apply the additive URL-client migration before serving the CIMD binary. Normal
rollback turns the feature flag off while retaining the binary, grants, and
tokens. `deploy-host.sh` checks the
[binary rollback boundary](mcp-cimd-authorization.md#older-clients-and-loss)
while the serving app is stopped, at render, restoration, and startup. A target
before v0.8.0 may start only with no URL-client rows or `MCP_ENABLED=false`.
Configuration and query failures refuse startup. Keep the migration and current
reviewed deployment helpers when selecting older images.

## Release fence

`fence.sh` owns root-only `/var/lib/aboutme/fence.json` under `flock`. Images
check the minimum before Podman; new hosts start at floor 4007. Cutover locks
the installed tag, checkpoints before mutation, and holds through quiescence;
`verify` locks. Its marker records ID, kind, state, and start time. Retained
cutover ownership or a marker blocks server, web, serving Caddy, migrate, and
jobs across reboot; maintenance can start. Only a valid active `reset-db` marker
matching the lock permits db-setup. Missing, symlinked, malformed, wrong-kind,
or mismatched state refuses it. The owner removes the marker after quiescence.
Uncertainty retains ownership. Manual clear refuses markers; privileged recovery
records its reason. Cleanup releases only its exact ID without a marker; retries
cannot replace an owner. Other operation kinds retain minimum-only checks unless
their contract adds gates.

The
[notice host contract](crowdsec-privacy-notification.md#send-authority-and-restore-fence)
owns mount checks, adoption, restore sequencing, and proof limits. Its
`notice_restore` takes the fence before the workload lock; retained ownership
and initializing adoption block app/ingestion starts, including db-setup, across
reboot. PostgreSQL recovery and maintenance retain their existing gates. Restore
releases only its own ownership, never reconciliation or send/activation gates.

## Scheduled jobs

systemd timers start `aboutme-job@<name>` with the server image:
`idempotency-expiry-sweep` and `media-deletion-sweep` hourly,
`privacy-retention-sweep` daily, and `media-orphan-sweep` weekly. `Persistent=`
runs a missed job after a reboot, and `OnFailure=` sends mail through the host
alert unit. `release-snapshot-sweep` does not run, because pgBackRest retention
expires release backups; the command stays for the AWS path.

## Logs, metrics, and alarms

journald keeps 30 days on the data volume, capped at 2 GB. Ordinary Caddy logs
drop addresses, URIs, and request headers. Its RAM-only feed and logs follow
[HTTP privacy contract](crowdsec-http-privacy.md). `aboutme-watch.timer` runs
`watch.sh` every five minutes and `OnFailure=` on every unit runs `alert.sh`;
both mail the support mailbox through Bizfly SMTP with `msmtp`, at most one
message per condition per UTC day, from the sender the app uses. GreenNode
vMonitor is the external readiness alarm: it checks `/readyz` every minute from
two public locations and alerts the owner on Telegram. The GitHub
[uptime workflow](../../.github/workflows/uptime.yml) is a slower backstop. The
[production runbook](../runbooks/production.md#alarms) owns check settings,
notification delivery, and monthly quota renewal.

The [quota renewal design](vmonitor-quota-renewal.md) owns renewal security and
failure boundaries. Dispatch is live; due-renewal proof and scheduling remain
pending. Its renew-only account is GitHub's approved cloud-credential exception.

| Signal   | Mechanism                                                        |
| -------- | ---------------------------------------------------------------- |
| App down | vMonitor on `/readyz`, Telegram; GitHub uptime backstop          |
| Units    | `OnFailure=` mail; `watch.sh` lists failed units                 |
| Host     | `watch.sh`: root and data volume above 70%                       |
| Database | `watch.sh`: archive failure, newest backup at least 26 hours old |
| Jobs     | `OnFailure=` mail from each `aboutme-job@` unit                  |
| Edge     | `watch.sh`: CrowdSec bans and invalid Coraza logs                |
| TOTP     | `watch.sh`: `totp_unavailable` in the server journal             |
| Mail     | Bizfly delivery logs; see the email runbook                      |
| Spend    | GreenNode and Bizfly balance alerts, outside the repository      |

Coraza match and unparsed counts do not trigger mail (owner, 2026-10-14). Each
watcher run writes one journal line with both counts for the preceding hour,
including zero counts. `coraza-log-invalid` still mails malformed logs or
unknown record kinds. Rule-match counts are callback records, not distinct
requests or blocked responses. The connector's interruption summary is an engine
diagnostic because an interruption can also come from a body limit.

### Proposed host Telegram alerts

The [Telegram host-alert design](telegram-host-alerts.md) proposes replacing
host SMTP alerts with Telegram only, preserving metadata limits and daily
suppression. Mail remains current until cutover; application SMTP stays
unchanged. The design owns transport, custody, authorization, failures,
verification, and rollout.

## Infrastructure code and state

The `vngcloud/vngcloud` OpenTofu provider (1.3.21) covers servers, volumes,
security groups, and floating IPs. The `vngcloud` CLI
(github.com/dannyota/vngcloud, an IAM user with a 0600 credentials file) covers
IAM, service accounts, buckets and their keys and policies, and billing budgets;
its calls live in `deploy/vn/scripts/`. The vWAF escalation is a runbook
checklist for the root portal.

State uses vStorage's S3 backend, path-style addressing, AWS-only checks
skipped, and OpenTofu `pbkdf2` encryption. The passphrase stays in ignored
credentials and age-encrypted in `aboutme-infra`. Locking rejects a second
operator's plan via create-only `PUT` (tested 2026-10-10). CI runs
`tofu fmt -check` and `tofu validate` for `deploy/vn/` without credentials.

## Media

Go already supports an S3-compatible endpoint with static keys and path-style
addressing. Media uses a private, unversioned vStorage bucket. The create-only
write sends `If-None-Match: *`, and vStorage answers it with 412 on an existing
key (tested 2026-10-10). vStorage ignores `If-Match` on `DELETE` and refuses a
`PUT` whose `If-Match` carries the quoted ETag, so no write relies on
`If-Match`.

## Deploy

The [production runbook](../runbooks/production.md#deploy) owns release gates,
deployment, rollback, and recovery; the [host guide](../../deploy/vn/README.md)
maps `deploy/vn/`. Backend settings, provider variables, and state stay ignored.

## AWS reference and recovery

Nothing from `deploy/aws/` is deployed.
[ADR 0026](../adr/0026-vietnam-hosted-production.md#history) records the stack
and SES teardown on 2026-10-11. Production data lives only on the Vietnam host
and its pgBackRest backups. Returning to AWS ECS or moving to Kubernetes needs a
reviewed infrastructure and data-transfer decision.

## Provider facts to confirm

The [host guide](../../deploy/vn/README.md#provider-questions) owns unresolved
provider questions and account results.
