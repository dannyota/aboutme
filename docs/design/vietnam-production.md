# Vietnam production

Production at `https://aboutme.vn` runs on GreenNode in Vietnam, with Bizfly
Email Transaction, under [ADR 0027](../adr/0027-vietnam-hosted-production.md).
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

One serving replica ([ADR 0026](../adr/0026-replica-scaling.md)), the migrator
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
  five-field RAM-only feed, keeps encrypted attack records for at most 24 hours,
  and disables community sharing (owner, 2026-10-10).
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

Go uses an SMTP sender (`AUTH_EMAIL_MODE=smtp`): implicit TLS or STARTTLS on the
configured port (Bizfly: `smtp.bizflycloud.vn`, 465 or 587) with certificate
verification, `PLAIN` authentication, one message per connection. A 2xx after
`DATA` is accepted, a 5xx is permanent, and 4xx, timeouts, and transport errors
are temporary. Logs carry the reply code only. Go uses SMTP rather than the
Bizfly HTTP API (owner, 2026-10-10), because SMTP is a standard contract that a
local stub can test. Bizfly's SMTP account sends only as its own address, and
the account refused a second sender for the domain, so auth mail goes from
`Danny from aboutme.vn <danny@aboutme.vn>` (owner, 2026-10-11); replies reach
the Google Workspace mailbox.

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

If application keys are ever copied in from another platform again, rotate them
once that platform's copies are deleted, as `deploy/vn/scripts/secrets.sh`
requires. The cutover rotation is complete: on 2026-10-10 at about 20:46 UTC,
before deleting the AWS parameter copies, the host rotated the auth-mail key and
key ID, TOTP key A, password-rate HMAC key, and view-pass key. No TOTP
credentials or queued mail jobs existed. Sign-in was rechecked afterwards.

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
  superuser.

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

## Release fence

`deploy/vn/scripts/fence.sh` changes root-owned `/var/lib/aboutme/fence.json`
outside PostgreSQL under `flock` per the
[fence design](passkey-release-fence.md). Images check the minimum before
Podman; new hosts start at floor 4007. Each `cutover` subcommand locks the
installed tag, checkpoints before mutation, and holds through quiescence;
`verify` locks. Before mutation, the fence writes a marker with its ID, kind,
state, and start time. A retained cutover operation or any marker refuses
server, web, serving Caddy, migrate, and scheduled-job starts before Podman,
including after reboot. Maintenance can start. `db-setup` starts only for a
valid active `reset-db` marker sharing the cutover lock ID; missing, symlinked,
malformed, wrong-kind, or mismatched state refuses it. Non-cutover operations
keep the minimum-only rule. The owning operation removes its marker after safe
quiescence. Uncertainty keeps the marker and lock closed. Manual clear refuses a
marker; privileged recovery logs its reason and clears it after inspection.
Cleanup releases the exact ID only without a marker. A retry cannot replace its
owner.

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
two public locations and mails the owner. The GitHub
[uptime workflow](../../.github/workflows/uptime.yml) is a slower backstop. The
[production runbook](../runbooks/production.md#alarms) owns check settings,
notification delivery, and monthly quota renewal.

The vMonitor check and its email channel depend on two free GreenNode quotas
that last at most one month and cannot auto-renew. The
[quota renewal design](vmonitor-quota-renewal.md) proposes a scheduled GitHub
workflow that renews both with a GreenNode service account limited to those
renewals, refuses any price above 0 VND, and fails loudly when a quota has two
days or fewer left. That credential is the one exception to the rule that GitHub
holds no cloud credential (owner, 2026-10-14).

| Signal   | Mechanism                                                        |
| -------- | ---------------------------------------------------------------- |
| App down | vMonitor on `/readyz`, email; GitHub uptime backstop             |
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

## Infrastructure code and state

The `vngcloud/vngcloud` OpenTofu provider (1.3.21) covers servers, volumes,
security groups, and floating IPs. The `vngcloud` CLI
(github.com/dannyota/vngcloud, an IAM user with a 0600 credentials file) covers
IAM, service accounts, buckets and their keys and policies, and billing budgets;
its calls live in `deploy/vn/scripts/`. The vWAF escalation is a runbook
checklist for the root portal.

State uses the S3 backend on a vStorage bucket with path-style addressing and
the AWS-only checks skipped. OpenTofu encrypts state client-side with the
`pbkdf2` key provider; the passphrase lives in the ignored credentials file and,
age-encrypted, in `aboutme-infra`. OpenTofu's lock file is on: vStorage refuses
a create-only `PUT` on an existing key, so a second operator's plan fails with
"Error acquiring the state lock" (tested 2026-10-10). Public CI runs
`tofu fmt -check` and `tofu validate` for `deploy/vn/` without credentials.

## Media

Go already supports an S3-compatible endpoint with static keys and path-style
addressing. Media uses a private, unversioned vStorage bucket. The create-only
write sends `If-None-Match: *`, and vStorage answers it with 412 on an existing
key (tested 2026-10-10). vStorage ignores `If-Match` on `DELETE` and refuses a
`PUT` whose `If-Match` carries the quoted ETag, so no write relies on
`If-Match`.

## Deploy

`deploy/vn/scripts/deploy.sh <tag>` runs from the laptop over SSH:

1. Resolve the tag to digests with an `amd64` manifest. Require the tag on
   `main` with green CI.
2. Verify tag-bound build provenance and the installed host scripts. Take the
   fence lock on the host. A lower tag or a held lock stops here.
3. Pull images by digest while the old release serves. Check every referenced
   secret by name.
4. Run a pgBackRest incremental backup annotated with the tag.
5. Stop the job timers and active jobs. Start maintenance beside Caddy (both
   bind 80 and 443 with `SO_REUSEPORT`), then stop Caddy and the server.
6. Require the marked 503 from outside the host.
7. Render the new units. Run `db-setup` with `--first-deploy`, then `migrate`;
   normal deploys run `migrate` only. Require exit 0.
8. Restart web at the new digest, start the server, and wait for `/readyz` on Go
   at `127.0.0.1:8080` from the host. Start Caddy beside maintenance, then stop
   maintenance and prove it stopped. The readiness check does not depend on
   which Caddy takes an external connection.
9. Start the job timers. vMonitor and the uptime backstop run throughout
   deploys.
10. Smoke from outside: health, TLS, security headers, and a 403 for a
    CrowdSec-banned test address. Release the lock.

Before a migration request, a failed deploy restores the previous release. After
a migration may have run, maintenance stays up with the app and jobs stopped.
Recovery is a forward fix or a point-in-time restore. A failed recovery keeps
the operation lock. `--rollback` skips backup and migration and is safe only
when the failed release applied no migration and the older release can read
every stored document version.

## `deploy/vn/` layout

```text
deploy/vn/
  README.md
  prod/        OpenTofu root: vServer, volumes, security groups, floating IP
  host/        cloud-init, sysctl, Quadlet units, timers, postgresql.conf,
               pg_hba.conf, pgbackrest.conf, journal and alert config
  edge/        CrowdSec and Coraza config, vWAF escalation checklist
  probe/       host fact checks (Podman networking, Chromium sandbox)
  scripts/     deploy.sh, fence.sh, secrets.sh, buckets.sh, restore-drill.sh,
               cutover.sh
```

`backend.hcl`, `prod.tfvars`, and state stay ignored.

## AWS reference and recovery

The production stack in `deploy/aws/prod` and its state bootstrap were destroyed
on 2026-10-11. RDS was deleted without a final snapshot. Production data lives
only on the Vietnam host and its pgBackRest backups in GreenNode vStorage. The
`aboutme-email` CloudFormation stack, including its SES identity, configuration
set, feedback queue, and alarms, was deleted.

Nothing from `deploy/aws/` is deployed. The code stays so production could
return to AWS ECS or move to Kubernetes later. A return would need a new
reviewed infrastructure and data-transfer decision. Recovery uses the Vietnam
host or its backups.

## Provider facts to confirm

The [host guide](../../deploy/vn/README.md#provider-questions) records the
account results for these questions. Unresolved facts remain marked there.

| ID  | Provider           | Question                                                                                                    |
| --- | ------------------ | ----------------------------------------------------------------------------------------------------------- |
| Q1  | GreenNode compute  | Does a vServer floating IP get network-level DDoS protection, and to what capacity?                         |
| Q2  | GreenNode compute  | Is IPv6 offered on a floating IP?                                                                           |
| Q3  | GreenNode vStorage | Does `PUT` with `If-None-Match: *` return 412 when the key exists?                                          |
| Q4  | GreenNode vStorage | Are pgBackRest and the OpenTofu S3 backend supported with path-style addressing and conditional writes?     |
| Q5  | GreenNode vStorage | Where is each region's data stored, including replicas?                                                     |
| Q6  | GreenNode compute  | Is an ARM vServer offered? Which volume types are encrypted, at what price?                                 |
| Q7  | GreenNode vDB      | Are PostgreSQL 18 and point-in-time recovery planned, and when?                                             |
| Q8  | GreenNode          | Is there a data processing agreement? Does any subprocessor store customer data outside Vietnam?            |
| Q9  | Bizfly             | What are the SMTP host, ports, and TLS modes for Email Transaction?                                         |
| Q10 | Bizfly             | How are bounces and complaints reported? Is there a suppression list? What are the rate limits?             |
| Q11 | Bizfly             | Which DKIM key length and selector does Email Transaction use? Is a custom return-path subdomain supported? |
| Q12 | Bizfly             | Where is Email Transaction data stored? Is there a data processing agreement?                               |
