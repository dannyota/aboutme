# Vietnam production

Production at `https://aboutme.vn` moves from AWS Singapore and Amazon
CloudFront to providers that store and process personal data in Vietnam, under
[ADR 0027](../adr/0027-vietnam-hosted-production.md). This page is the target
design and the migration order. Until cutover, the
[single-host design](single-host-production.md) describes what runs. The ADR
holds the legal reason.

Status: proposed. **Unconfirmed** marks a provider fact from public docs or
research; devops verifies each by testing on the account before cutover
([facts](#provider-facts-to-confirm)).

## Target architecture

The request path is browser or MCP client, the host's floating IP, Caddy on 443
(TLS, Coraza, CrowdSec bouncer), then Go on `127.0.0.1:8080` or Nuxt on
`127.0.0.1:3000`. Go reaches PostgreSQL on the same host by Unix socket, media
in vStorage, and Bizfly Email Transaction by SMTP. pgBackRest ships backups and
WAL to a second vStorage bucket. Logs stay in journald on the host; a host timer
mails alarms through Bizfly.

| Concern            | Today (AWS, CloudFront)                     | Vietnam production                                               |
| ------------------ | ------------------------------------------- | ---------------------------------------------------------------- |
| Edge, TLS, cache   | CloudFront, AWS WAF, origin mTLS, ACM certs | Caddy on the host: TLS, Coraza WAF, CrowdSec; no edge service    |
| DNS                | Route 53 with DNSSEC, alias to CloudFront   | Route 53 with DNSSEC, address records to the host, unchanged     |
| Compute            | EC2 `t4g.small`, Bottlerocket ECS           | One amd64 vServer, Podman containers under systemd               |
| Database           | RDS PostgreSQL 18, 30-day PITR              | PostgreSQL 18 on the vServer, pgBackRest to vStorage             |
| Media              | Private S3, task role                       | Private vStorage bucket, static key for one service account      |
| Secrets            | SSM Parameter Store                         | Root-only host files, age-encrypted copy in `aboutme-infra`      |
| Release fence      | DynamoDB item                               | Root-owned host file plus a start-time check                     |
| Jobs               | EventBridge Scheduler, ECS tasks            | systemd timers running one-shot containers                       |
| Logs and alarms    | CloudWatch, SNS, Route 53 health check      | journald on the host, alert timer by mail, Route 53 health check |
| Transactional mail | SES `ap-southeast-1`                        | Bizfly Email Transaction over SMTP                               |
| Support mailbox    | Google Workspace                            | Google Workspace, unchanged                                      |
| Infrastructure     | OpenTofu `deploy/aws/`, S3 state, KMS       | OpenTofu `deploy/vn/` plus `vngcloud` CLI, vStorage state        |

Compute and network run in HCM03 and vStorage in HCM04, both Ho Chi Minh City
(owner, 2026-10-10); vStorage exists only in HCM04 and HAN02.

Unchanged: one serving replica ([ADR 0026](../adr/0026-replica-scaling.md)), the
migrator and roles ([ADR 0005](../adr/0005-database-migrations.md)), the public
revocation gate ([ADR 0010](../adr/0010-public-artifact-revocation.md)), private
media ([ADR 0009](../adr/0009-private-media-delivery.md)), and every HTTP, SSE,
and MCP contract. Public images stay on GitHub Container Registry; they hold no
personal data.

## Edge

Nothing stands in front of the host (owner, 2026-10-10). The apex and `www`
resolve to the floating IP; Caddy terminates TLS with Let's Encrypt certificates
by ACME HTTP-01, as `aboutme-caddy` does today, and serves hashed `/_nuxt/*`
assets with Nuxt's immutable cache headers. The security group admits TCP 80 and
443 from anywhere and TCP 22922 from all sources (owner, 2026-10-10). Caddy
takes the client address from the socket, strips every forwarding header, and
sends one `X-Real-IP` to Go; Go keeps trusting only loopback. The CloudFront
listener with its edge key and `CloudFront-Viewer-Address` stays for the AWS
test environment under the `EDGES` list, which gains `direct`.

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
  callback. Only its fixed missing-key macro warning,
  `key not found in collection, returning the original text`, emits
  `msg=waf_engine_diagnostic`. It has only `ts`, `level`, `logger`, and `msg`,
  no `rule_id` or raw fields; the watcher excludes it from rule counts and
  unknown alerts. Logs carry no raw fields or personal data and never reach disk
  or journald. Files cap at 5 MiB plus two rolls; one-day expiry applies only to
  rolled files, so a low-volume active file persists until rotation or reboot.
  After two weeks, devops reviews matches, excludes false positives on resume
  writes, and enables blocking.

None of these absorbs an HTTP flood larger than the host. GreenNode vWAF is the
escalation: an application in the root portal with the floating IP as upstream,
live after `www` becomes a CNAME to `<code>.waf.greennode.vn` and the apex an A
record to vWAF's address, since Route 53 aliases only AWS targets. The runbook
holds the steps. **Unconfirmed:** network-level DDoS protection for a vServer
floating IP (Q1), and IPv6 on the floating IP (Q2).

## DNS and mail

Route 53 keeps the `aboutme.vn` zone, signed, under the existing OpenTofu
module. The apex and `www` become A records (and AAAA if the floating IP has
IPv6) to the host: TTL 60 through cutover, then 300. The CloudFront aliases, the
ACM validation records go after cutover; the health check stays as the external
app-down check; it resolves `aboutme.vn`, so it follows the DNS switch to the
host. An authoritative name server answers resolvers and sees no person, so
Route 53 stays outside the data-residency boundary at about USD 2 a month.

The Google Workspace mailbox stays: a person who writes to support sends their
own mail. MX, root SPF, Google DKIM, and DMARC are unchanged. Bizfly Email
Transaction gets its own DKIM selector and, if offered, a return-path subdomain
like SES's `bounce.aboutme.vn`; otherwise root SPF gains the Bizfly include. SES
records go once AWS stops sending as `aboutme.vn`.

Go gets an SMTP sender (`AUTH_EMAIL_MODE=smtp`): implicit TLS or STARTTLS on the
configured port (Bizfly: `smtp.bizflycloud.vn`, 465 or 587) with certificate
verification, `PLAIN` authentication, one message per connection. As with SES, a
2xx after `DATA` is accepted, a 5xx is permanent, and 4xx, timeouts, and
transport errors are temporary. Logs carry the reply code only. Go uses SMTP
rather than the Bizfly HTTP API (owner, 2026-10-10), because SMTP is a standard
contract that a local stub can test. **Unconfirmed:** Bizfly's bounce and
complaint reporting, suppression list, and sending limits.

## Host

One amd64 vServer runs Ubuntu 24.04 LTS with 2 vCPU and 4 GiB (owner,
2026-10-10). Its [memory and storage budget](vietnam-host-memory.md) caps each
service, serializes jobs, gives Caddy logs a 64 MiB tmpfs, and makes measured
rehearsal peaks a cutover gate. OpenTofu owns a 30 GB encrypted root disk, a 20
GB encrypted SSD data volume, and the floating IP. Both disks use GreenNode
encryption (aes-xts-plain64 256), chosen at server create (owner, 2026-10-10).
Only the OS, public images, and caches use root. Secrets, PostgreSQL, CrowdSec
state, and journald use the data volume. Either disk grows online at its 70
percent alarm. Release images add `linux/amd64` beside `linux/arm64`, because
GreenNode offers no ARM vServer (no ARM flavor on the account, 2026-10-10).

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

The trust boundaries match today's host:

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
loaded as a Podman secret and given only to the units that need it, with the
split in the
[single-host design](single-host-production.md#secrets-and-identity). New
values:

| Value                                   | Units                        |
| --------------------------------------- | ---------------------------- |
| vStorage media key (service account)    | `aboutme-server`, media jobs |
| vStorage backup key and pgBackRest pass | pgBackRest on the host       |
| Bizfly SMTP user and password           | `aboutme-server`             |
| CrowdSec bouncer API key                | `aboutme-caddy`              |

`deploy/vn/scripts/secrets.sh` runs over SSH. It generates each value on the
host with `openssl`, never overwrites or prints one, and writes a copy encrypted
to the owner's age recipient; the laptop pulls only that ciphertext into
`aboutme-infra`. Values already in SSM (TOTP key ring, auth-mail keys,
password-rate key) move once at cutover through an owner-run pipe that prints
nothing, and rotate when AWS real data is deleted.

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
An alarm fires on an archive failure or a newest backup older than 26 hours.
**Unconfirmed:** vStorage works as a pgBackRest S3 repository with path-style
addressing. One repository in HCM04 (owner, 2026-10-10): the only other vStorage
region is Hanoi, so a regional failure means waiting for HCM04 to return.

Before cutover and then every quarter, devops restores the latest backup and a
point-in-time target from the previous day to a temporary vServer. It checks the
goose version, row counts per table, the catalog grant test, and a store smoke
as `aboutme_app`, records the recovery time, and deletes the server. The first
drill is the launch restore evidence.

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
runs a missed job after a reboot, and `OnFailure=` writes a fixed marker line
for a log alarm. `release-snapshot-sweep` does not run, because pgBackRest
retention expires release backups; the command stays for the AWS test
environment.

## Logs, metrics, and alarms

There is no monitoring service (owner, 2026-10-10: vMonitor removed for cost).
journald keeps 30 days on the data volume, capped at 2 GB. Ordinary Caddy logs
drop addresses, URIs, and request headers. Its RAM-only feed and logs follow
[HTTP privacy contract](crowdsec-http-privacy.md). `aboutme-watch.timer` runs
`watch.sh` every five minutes and `OnFailure=` on every unit runs `alert.sh`;
both mail the support mailbox through Bizfly SMTP with `msmtp`, one message per
condition per day, from the sender the app uses. The Route 53 health check on
`/readyz` stays as the only check from outside the host; it probes a public
endpoint and carries no personal data.

| Signal   | Mechanism                                                      |
| -------- | -------------------------------------------------------------- |
| App down | Route 53 health check on `/readyz`, alarm by SNS mail          |
| Units    | `OnFailure=` mail; `watch.sh` lists failed units               |
| Host     | `watch.sh`: root and data volume above 70%, load, memory       |
| Database | `watch.sh`: archive failure, newest backup older than 26 hours |
| Jobs     | `OnFailure=` mail from each `aboutme-job@` unit                |
| Edge     | `watch.sh`: CrowdSec bans and Coraza rule counts per hour      |
| TOTP     | `watch.sh`: `totp_unavailable` in the server journal           |
| Mail     | Bizfly bounce and complaint reporting (**Unconfirmed**)        |
| Spend    | GreenNode and Bizfly balance alerts, outside the repository    |

## Infrastructure code and state

The `vngcloud/vngcloud` OpenTofu provider (1.3.21, **Unconfirmed** at build
time) covers servers, volumes, security groups, and floating IPs. The `vngcloud`
CLI (github.com/dannyota/vngcloud, an IAM user with a 0600 credentials file)
covers IAM, service accounts, buckets and their keys and policies, and billing
budgets; its calls live in `deploy/vn/scripts/`. The vWAF escalation is a
runbook checklist for the root portal.

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

`deploy/vn/scripts/deploy.sh <tag>` runs from the laptop over SSH, in today's
order:

1. Resolve the tag to digests with an `amd64` manifest. Require the tag on
   `main` with green CI.
2. Take the fence lock on the host. A lower tag or a held lock stops here.
3. Pull images by digest while the old release serves.
4. Run a pgBackRest incremental backup annotated with the tag.
5. Stop the job timers and the app-down check. Start maintenance beside Caddy
   (both bind 80 and 443 with `SO_REUSEPORT`), then stop Caddy and the server.
6. Require the marked 503 from outside the host.
7. Run `db-setup` with `--first-deploy`; otherwise run `migrate` and require
   exit 0.
8. Restart web at the new digest, start the server and Caddy beside maintenance,
   wait for `/readyz` on Go at `127.0.0.1:8080` from the host, not only from
   outside, so the result does not depend on which Caddy takes the connection.
   Then stop maintenance and prove it stopped.
9. Start the timers and resume the app-down check.
10. Smoke from outside: health, TLS, security headers, and a 403 for a
    CrowdSec-banned test address. Release the lock.

Failure handling, `--rollback`, and the maintenance page keep the rules in the
[single-host design](single-host-production.md#release-and-deploy). A failed or
uncertain migration leaves maintenance up; recovery is a forward fix or a
point-in-time restore.

## `deploy/vn/` layout

```text
deploy/vn/
  README.md
  prod/        OpenTofu root: vServer, volumes, security groups, floating IP
  host/        cloud-init, sysctl, Quadlet units, timers, postgresql.conf,
               pg_hba.conf, pgbackrest.conf, log and metric agent config
  edge/        CrowdSec and Coraza config, vWAF escalation checklist
  probe/       host fact checks (Podman networking, Chromium sandbox)
  scripts/     deploy.sh, fence.sh, secrets.sh, buckets.sh, restore-drill.sh,
               cutover.sh
```

`deploy/aws/` stays for the test environment. `backend.hcl`, `prod.tfvars`, and
state stay ignored.

## Migration

### 1. Verify on the account

The owner opens both accounts. Devops tests each
[provider fact](#provider-facts-to-confirm) on them and records the result. Work
that depends on a fact waits for its test.

### 2. App preparation releases

Each ships alone on AWS before cutover:

1. Images for `linux/amd64` and `linux/arm64`.
2. The SMTP mail sender.
3. Caddy edge selection: the `EDGES` list gains `direct`, a listener that trusts
   no forwarding header; the Coraza and CrowdSec modules in the Caddy image; and
   the site host from the environment so a rehearsal hostname works.
4. Privacy notice and terms naming the Vietnam providers and CrowdSec fields and
   retention, held until Route 53 points at the verified Vietnam host.

### 3. Build infrastructure

Devops applies `deploy/vn/prod`, creates buckets, installs the host, secrets,
PostgreSQL, pgBackRest, agents, and alarms, and runs the probe. Bizfly
verification records go into the live zone beside the existing ones.

### 4. Rehearsal

Under a temporary hostname such as `vn-rehearsal.aboutme.vn` (an A record to the
host), with fictional data only: first deploy, a normal deploy, rollback, a
restore drill, every alarm once, mail to a test mailbox, SSE and MCP from
outside, forged-header probes, and a timed cutover dry run. A normal deploy and
second cutover are refused while locked, including during signal cleanup. A
retained-state reboot refuses serving and jobs before Podman; maintenance and
matching `reset-db` `db-setup` remain available.

### 5. Cutover

In one announced maintenance window:

1. Put AWS in maintenance with app and timers stopped. AWS never serves writes
   again.
2. `pg_dump -Fc` from RDS as `aboutme_migrator` through an SSM port forward,
   age-encrypted on the laptop and copied to the host. `pg_restore` as
   `postgres` into the database `db-setup` prepared, keeping owners and grants.
   Delete the laptop copy after step 3.
3. Verify the goose version, row counts, and an ordered hash of each table on
   both sides, then the catalog grant test.
4. `rclone copy` the media, then `rclone check --download` for a byte-for-byte
   match and equal object counts.
5. Run `migrate` if the cutover release adds migrations, start the app, and
   smoke it from outside with the production host.
6. Switch the apex and `www` at Route 53 to the floating IP (TTL 60 seconds).
   Visitors on cached answers still get the AWS maintenance page.
7. After 48 hours with no AWS traffic, delete the CloudFront distribution and
   its certificates and validation records. Keep the Route 53 health check.

The auth-mail keys move with the database, so pending outbox mail sends from
Vietnam. Sessions, passkeys (RP ID `aboutme.vn`), TOTP, and agent grants keep
working.

### 6. AWS real-data deletion

Once the cutover is verified, delete: the RDS instance without a final snapshot,
every manual snapshot and retained automated backup, every media object and the
bucket, the CloudWatch log groups, and the SES suppression list and feedback
queue. Rotate the TOTP key ring and auth-mail keys on the host, then delete
their SSM copies. Record each deletion with its date in `aboutme-infra`. Then
rebuild the AWS stack empty as the test environment.

## Rollback

Until cutover step 6 sends traffic, rollback is stopping the VN app and ending
AWS maintenance. Once Vietnam accepts writes, there is no rollback window
(owner, 2026-09-24): moving data back would be a new transfer abroad, so
recovery is a forward fix or a restore in Vietnam, and AWS real data is deleted
as soon as the cutover is verified.

## Test environment

The AWS stack holds fictional data only: no real accounts and no production
copy. It gets no domain of its own (owner, 2026-09-24), and `aboutme.vn` keeps
no AWS records. Its mail goes only to test addresses.

## Provider facts to confirm

Devops tests each on the account; the runbook records results. Data location and
subprocessors come from the provider's published terms.

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
