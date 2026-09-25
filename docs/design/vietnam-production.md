# Vietnam production

Production at `https://aboutme.vn` moves from AWS Singapore and Amazon
CloudFront to providers that store and process personal data in Vietnam, under
[ADR 0051](../adr/0051-vietnam-hosted-production.md). This page is the target
design and the migration order. Until cutover, the
[single-host design](single-host-production.md) describes what runs. The ADR
holds the legal reason. A data protection impact assessment (DPIA) is still
required and is not covered here.

Status: proposed. **Owner approval** marks a choice the owner must make before
the work that depends on it starts. **Unconfirmed** marks a provider fact from
public docs or research; devops verifies each by testing on the account before
cutover ([facts](#provider-facts-to-confirm)).

## Target architecture

The request path is browser or MCP client, vCDN (PoPs in Vietnam), the host's
floating IP, Caddy on 443, then Go on `127.0.0.1:8080` or Nuxt on
`127.0.0.1:3000`. Go reaches PostgreSQL on the same host by Unix socket, media
in vStorage, and Bizfly Email Transaction by SMTP. pgBackRest ships backups and
WAL to a second vStorage bucket. Agents send logs and metrics to vMonitor.

| Concern            | Today (AWS, CloudFront)                     | Vietnam production                                          |
| ------------------ | ------------------------------------------- | ----------------------------------------------------------- |
| Edge, TLS, cache   | CloudFront, public origin cert, origin mTLS | vCDN Web Accelerator; origin allowlist plus secret header   |
| DNS                | Cloudflare, DNS-only CNAMEs to CloudFront   | vDNS                                                        |
| Compute            | EC2 `t4g.small`, Bottlerocket ECS           | One amd64 vServer, Podman containers under systemd          |
| Database           | RDS PostgreSQL 18, 30-day PITR              | PostgreSQL 18 on the vServer, pgBackRest to vStorage        |
| Media              | Private S3, task role                       | Private vStorage bucket, static key for one service account |
| Secrets            | SSM Parameter Store                         | Root-only host files, age-encrypted copy in `aboutme-infra` |
| Release fence      | DynamoDB item                               | Root-owned host file plus a start-time check                |
| Jobs               | EventBridge Scheduler, ECS tasks            | systemd timers running one-shot containers                  |
| Logs and alarms    | CloudWatch, SNS, Route 53 health check      | vMonitor logs, metrics, alarms, synthetic HTTP checks       |
| Transactional mail | SES `ap-southeast-1`                        | Bizfly Email Transaction over SMTP                          |
| Support mailbox    | Google Workspace                            | Bizfly Business Email                                       |
| Google sign-in     | On                                          | Off (`PROVIDER_LOGIN_ENABLED=false`)                        |
| Infrastructure     | OpenTofu `deploy/aws/`, S3 state, KMS       | OpenTofu `deploy/vn/`, vStorage state, passphrase           |

All GreenNode resources live in one region. **Owner approval:** HCM03 (Ho Chi
Minh City), where vServer and vStorage both have current docs.

Unchanged: one serving replica
([ADR 0036](../adr/0036-single-replica-launch-and-pipeline-migrations.md)), the
migrator and roles
([ADR 0038](../adr/0038-single-baseline-and-plain-migrator.md)), the public
revocation gate ([ADR 0022](../adr/0022-public-artifact-revocation.md)), private
media ([ADR 0019](../adr/0019-private-media-delivery.md)), and every HTTP, SSE,
and MCP contract. Public images stay on GitHub Container Registry; they hold no
personal data.

## Edge

vCDN Web Accelerator fronts the apex and `www`. Following ADR 0022, it caches
only hashed `/_nuxt/*` assets; every other path passes through with no edge
storage. **Unconfirmed:** Web Accelerator caches HTML and API by default, so a
rule must bypass everything else, and vCDN must neither cache nor replace 5xx
responses (the maintenance page is a 503).

vCDN has no equivalent of CloudFront origin mTLS, so two layers lock the origin:

1. The security group admits TCP 443 only from vCDN's published origin ranges
   and TCP 22 only from the owner's allowlist. `deploy.sh` stops when vCDN's
   live range list differs from the deployed one. **Unconfirmed:** vCDN
   publishes its ranges and announces changes.
2. vCDN adds the request header `Aboutme-Edge-Key` with a 32-byte random value.
   Caddy answers 403 unless it carries the current or previous value, then
   strips it before proxying. Two values allow rotation without downtime.
   **Unconfirmed:** vCDN can add a fixed origin request header.

Viewer TLS terminates at vCDN. **Owner approval:** a vCDN-managed certificate
for the apex and `www` with automatic renewal if offered, otherwise a Let's
Encrypt certificate that devops renews and uploads. vCDN reaches the origin over
HTTPS with `Host: aboutme.vn`. If vCDN verifies the origin certificate
(**Unconfirmed**), Caddy gets a public one by ACME HTTP-01, with vCDN passing
`/.well-known/acme-challenge/*` uncached. If not, a private CA certificate is
enough, and the two layers above carry origin trust.

Caddy trusts the vCDN client-IP header only from vCDN ranges
(`trusted_proxies_strict`), strips every other forwarding header, and sends one
`X-Real-IP` to Go. Go keeps trusting only loopback and never parses
`X-Forwarded-For`. The vCDN listener ignores `CloudFront-Viewer-Address`. If
vCDN sends only `X-Forwarded-For`, Caddy takes the rightmost address outside its
trusted ranges. **Unconfirmed:** the header name, and whether vCDN overwrites a
viewer-supplied value.

**Unconfirmed, required before cutover:** vCDN streams responses unbuffered; its
idle timeout is at least 60 seconds, above the 25-second SSE heartbeat; it
passes `Authorization`, cookies, query strings, `ETag`, `If-None-Match`, and
every method unchanged; and it accepts request bodies of at least 2,162,688
bytes (the photo limit in [budgets](budgets.md)). If buffering or the timeout
cannot be changed, the heartbeat changes with a budget update.

The free tier has L3 and L4 DDoS protection but no L7 protection. Go's rate
limits stay the application control. **Owner approval:** launch without vWAF and
add it if abuse appears.

## DNS and mail

vDNS hosts the `aboutme.vn` zone, with the apex pointing at vCDN. **Unconfirmed,
blocker for the NS move:** vDNS supports an apex CNAME, ALIAS, or flattening. If
not, the choices are apex A records to stable vCDN addresses, if vCDN has them,
or Cloudflare as a DNS-only host. The [cutover](#5-cutover) switches records at
Cloudflare first and moves NS later, so it does not wait on vDNS.

- MX: Bizfly Business Email, after Google Workspace mail has moved.
- Root SPF: the two Bizfly includes only; `~all` until DMARC reports are clean,
  then `-all`.
- DKIM: one selector per Bizfly service, 2048-bit where offered.
- DMARC: `p=none` with reports to the support mailbox for two weeks, then
  `p=quarantine`.
- SES MAIL FROM and SES DKIM records go once AWS stops sending as `aboutme.vn`.

Go gets an SMTP sender (`AUTH_EMAIL_MODE=smtp`): implicit TLS on 465 or STARTTLS
on 587 with certificate verification, `PLAIN` authentication, one message per
connection. As with SES, a 2xx after `DATA` is accepted, a 5xx is permanent, and
4xx, timeouts, and transport errors are temporary. Logs carry the reply code
only. **Owner approval:** SMTP rather than the Bizfly HTTP API, because SMTP is
a standard contract that a local stub can test. **Unconfirmed:** Bizfly's bounce
and complaint reporting, suppression list, and sending limits.

## Host

One amd64 vServer runs Ubuntu 24.04 LTS (**Owner approval**). **Owner
approval:** 2 vCPU and 4 GiB, since PostgreSQL now shares the host: 512 MiB for
Go and Chromium (unchanged cap), about 300 MiB for Nuxt and Caddy, 1 GiB for
PostgreSQL, and the rest for the OS and page cache. OpenTofu owns the server,
the root disk, an encrypted SSD data volume for PostgreSQL, and the floating IP.
Release images add `linux/amd64` beside `linux/arm64`, because GreenNode offers
no ARM vServer (**Unconfirmed**).

Rootful Podman runs every container from systemd Quadlet units:

| Unit                  | Network            | Runs                                           |
| --------------------- | ------------------ | ---------------------------------------------- |
| `aboutme-caddy`       | host               | Caddy on 443; always                           |
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
- `aboutme-caddy` and `aboutme-maintenance` conflict in systemd, and the deploy
  script proves one stopped before it starts the other.
- One-shot containers reach PostgreSQL only through the mounted socket; only
  media jobs get outbound network, for vStorage.
- Chromium keeps its sandbox and its blocked proxy; the probe checks the user
  namespaces it needs.

Access is key-only SSH from the allowlist as a non-root admin with `sudo`, and
the GreenNode web console for break-glass. **Owner approval:** a hardware-backed
key (`ed25519-sk`). Security updates install daily; a monthly maintenance window
reboots.

## Secrets and host identity

There is no instance role or parameter store. Each secret is a root-owned 0400
file under `/etc/aboutme/secrets/`, loaded as a Podman secret and given only to
the units that need it, with the split in the
[single-host design](single-host-production.md#secrets-and-identity). New
values:

| Value                                   | Units                        |
| --------------------------------------- | ---------------------------- |
| vStorage media key (service account)    | `aboutme-server`, media jobs |
| vStorage backup key and pgBackRest pass | pgBackRest on the host       |
| Bizfly SMTP user and password           | `aboutme-server`             |
| Edge secret, current and previous       | `aboutme-caddy`              |

`deploy/vn/scripts/secrets.sh` runs over SSH. It generates each value on the
host with `openssl`, never overwrites or prints one, and writes a copy encrypted
to the owner's age recipient; the laptop pulls only that ciphertext into
`aboutme-infra`. Values already in SSM (TOTP key ring, auth-mail keys,
password-rate key) move once at cutover through an owner-run pipe that prints
nothing, and rotate when AWS real data is deleted.

Each vStorage service account has a policy for one bucket: media (get, put,
list, delete), backups, or state. The state key, the OpenTofu service account,
and the Bizfly API key stay on the laptop.

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
addressing. **Owner approval:** a second repository in the Hanoi region against
a regional failure; it stays in Vietnam and costs little.

Before cutover and then every quarter, devops restores the latest backup and a
point-in-time target from the previous day to a temporary vServer. It checks the
goose version, row counts per table, the catalog grant test, and a store smoke
as `aboutme_app`, records the recovery time, and deletes the server. The first
drill is the launch restore evidence.

## Release fence

The fence moves to the host, the only place production images start.
`/var/lib/aboutme/fence.json` is root-owned and holds the DynamoDB item's
fields. `deploy/vn/scripts/fence.sh` changes it on the host under `flock`, with
the conditions in the [fence design](passkey-release-fence.md). Every unit that
runs the server or web image has an `ExecStartPre` that refuses a
`DEPLOY_RELEASE_NUMBER` below the file's minimum, so a manual `systemctl start`
cannot start an old image either. A rebuilt host starts at the highest floor in
the fence design (4007). Root on the host is a privileged bypass, as the
AWS-login principal is today. **Owner approval:** a host file rather than a
PostgreSQL table, because a point-in-time restore would roll a table back and a
start-time check should not need the database.

## Scheduled jobs

systemd timers start `aboutme-job@<name>` with the server image:
`idempotency-expiry-sweep` and `media-deletion-sweep` hourly,
`privacy-retention-sweep` daily, and `media-orphan-sweep` weekly. `Persistent=`
runs a missed job after a reboot, and `OnFailure=` writes a fixed marker line
for a log alarm. `release-snapshot-sweep` does not run, because pgBackRest
retention expires release backups; the command stays for the AWS test
environment.

## Logs, metrics, and alarms

The vMonitor log agent ships journald output to a log project in the same
region, and the metric agent reports host metrics. Caddy logs still drop client
addresses and request headers. Alarms email the support mailbox.
**Unconfirmed:** 180-day log retention, and log and metric storage in Vietnam.

| Signal   | Mechanism                                                        |
| -------- | ---------------------------------------------------------------- |
| App down | vMonitor synthetic HTTPS check on `/readyz` through vCDN         |
| Units    | Log alarm on a unit failure or restart loop                      |
| Host     | CPU, memory, root and data volume disk                           |
| Database | Archive failure, backup age, connections, data volume free space |
| Jobs     | Log alarm on the `OnFailure=` marker                             |
| TOTP     | Log alarm on `totp_unavailable`                                  |
| Mail     | Bizfly bounce and complaint reporting (**Unconfirmed**)          |
| Spend    | GreenNode and Bizfly balance alerts, outside the repository      |

## Infrastructure code and state

The `vngcloud/vngcloud` OpenTofu provider (1.3.21, **Unconfirmed** at build
time) covers servers, volumes, security groups, and floating IPs, but not
buckets, vCDN, vDNS, or vMonitor. A script creates buckets with the S3 API.
vCDN, vDNS, and vMonitor settings are applied by API scripts where an API
exists, or by console steps in the runbook; a change to one updates the runbook
in the same change.

State uses the S3 backend on a vStorage bucket with path-style addressing and
the AWS-only checks skipped. OpenTofu encrypts state client-side with the
`pbkdf2` key provider; the passphrase lives in the ignored credentials file and,
age-encrypted, in `aboutme-infra`. **Unconfirmed:** conditional writes for
OpenTofu's lock file; without them, one operator applies at a time by rule.
Public CI runs `tofu fmt -check` and `tofu validate` for `deploy/vn/` without
credentials.

## Media

Go already supports an S3-compatible endpoint with static keys and path-style
addressing. Media uses a private, unversioned vStorage bucket. **Unconfirmed,
cutover blocker:** vStorage answers a `PUT` with `If-None-Match: *` on an
existing key with 412, which the create-only write needs. If it does not,
collision safety rests on the random key alone, and that needs its own design
decision.

## Deploy

`deploy/vn/scripts/deploy.sh <tag>` runs from the laptop over SSH, in today's
order:

1. Resolve the tag to digests with an `amd64` manifest. Require the tag on
   `main` with green CI.
2. Take the fence lock on the host. A lower tag or a held lock stops here.
3. Pull images by digest while the old release serves.
4. Run a pgBackRest incremental backup annotated with the tag.
5. Stop the job timers and the app-down check. Start maintenance beside Caddy
   (both bind 443 with `SO_REUSEPORT`), then stop Caddy and the server.
6. Require the marked 503 through vCDN.
7. Run `db-setup` with `--first-deploy`; otherwise run `migrate` and require
   exit 0.
8. Restart web at the new digest, start the server and Caddy beside maintenance,
   wait for `/readyz`, then stop maintenance and prove it stopped.
9. Start the timers and resume the app-down check.
10. Smoke through vCDN: health, TLS, and security headers. From the host, a
    request without the edge secret gets 403. Release the lock.

Failure handling, `--rollback`, and the maintenance page keep the rules in the
[single-host design](single-host-production.md#release-and-deploy). A failed or
uncertain migration leaves maintenance up; recovery is a forward fix or a
point-in-time restore.

## `deploy/vn/` layout

```text
deploy/vn/
  README.md
  bootstrap/   OpenTofu root: service accounts that exist before state
  prod/        OpenTofu root: vServer, volumes, security groups, floating IP
  host/        cloud-init, sysctl, Quadlet units, timers, postgresql.conf,
               pg_hba.conf, pgbackrest.conf, log and metric agent config
  edge/        vCDN, vDNS, and vMonitor settings and the scripts that apply them
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
3. Caddy edge selection: the `EDGES` list gains `vcdn`, a listener with its own
   trust and client address rule, and the site host comes from the environment
   so a rehearsal hostname works.
4. A notice asking accounts with a Google identity and no password to add one
   while Google sign-in still works. Password reset is a no-op for such an
   account today, so one that misses the notice cannot sign in after cutover;
   its data stays. **Owner approval:** let reset create the first password for
   an account with none, since the reset email proves the same control as
   registration.
5. Privacy notice and terms naming GreenNode and Bizfly in Vietnam. The text is
   true only after cutover, so its first deploy is the cutover deploy.

### 3. Build infrastructure

Devops applies `deploy/vn/prod`, creates buckets, installs the host, secrets,
PostgreSQL, pgBackRest, agents, and alarms, and runs the probe. Bizfly
verification records go into Cloudflare DNS beside the existing ones.

### 4. Rehearsal

Under a temporary hostname such as `vn-rehearsal.aboutme.vn` (a DNS-only
Cloudflare record to vCDN), with fictional data only: first deploy, a normal
deploy, rollback, a restore drill, every alarm once, mail to a test mailbox, SSE
and MCP through vCDN, forged-header and direct-IP probes, and a timed dry run of
the cutover script.

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
   smoke it through vCDN with the production host.
6. Switch the apex and `www` at Cloudflare to DNS-only CNAMEs to vCDN (TTL 60
   seconds), and MX and SPF to Bizfly. Visitors on old DNS still get the AWS
   maintenance page.
7. After 48 hours with no AWS traffic, move NS to vDNS with identical records.

The auth-mail keys move with the database, so pending outbox mail sends from
Vietnam. Sessions, passkeys (RP ID `aboutme.vn`), TOTP, and agent grants keep
working.

### 6. AWS real-data deletion

Once the cutover is verified, delete: the RDS instance without a final snapshot,
every manual snapshot and retained automated backup, every media object and the
bucket, the CloudWatch log groups, the SES suppression list and feedback queue,
and the Google Workspace mailbox once its mail has moved. Rotate the TOTP key
ring and auth-mail keys on the host, then delete their SSM copies. Record each
deletion with its date in `aboutme-infra`. Then rebuild the AWS stack empty as
the test environment.

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

Have I Been Pwned, MCP clients a user connects, and GitHub stay outside this
move; [ADR 0051](../adr/0051-vietnam-hosted-production.md) records why.

## Provider facts to confirm

Devops tests each on the account; the runbook records results. Data location and
subprocessors come from the provider's published terms.

| ID  | Provider           | Question                                                                                                                       |
| --- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------ |
| Q1  | GreenNode vCDN     | Can a rule cache only `/_nuxt/*` and pass every other path with no edge storage? Are 5xx responses passed uncached, unchanged? |
| Q2  | GreenNode vCDN     | Is there a published list of origin-facing IP ranges, and how are changes announced?                                           |
| Q3  | GreenNode vCDN     | Can vCDN add a fixed request header to every origin request?                                                                   |
| Q4  | GreenNode vCDN     | Which header carries the viewer IP, and does vCDN overwrite a viewer-supplied value?                                           |
| Q5  | GreenNode vCDN     | Are responses streamed unbuffered? What is the idle timeout for a long-lived response (Server-Sent Events)?                    |
| Q6  | GreenNode vCDN     | Are `Authorization`, cookies, query strings, all methods, `ETag`, and `If-None-Match` passed unchanged? What is the body cap?  |
| Q7  | GreenNode vCDN     | Does vCDN verify the origin certificate? Does it offer a managed apex certificate with automatic renewal?                      |
| Q8  | GreenNode vCDN     | Where are viewer request logs stored, and for how long?                                                                        |
| Q9  | GreenNode vCDN     | What L7 protection applies without vWAF?                                                                                       |
| Q10 | GreenNode vDNS     | Does the apex support CNAME, ALIAS, or flattening? DNSSEC? A records API?                                                      |
| Q11 | GreenNode vStorage | Does `PUT` with `If-None-Match: *` return 412 when the key exists?                                                             |
| Q12 | GreenNode vStorage | Are pgBackRest and the OpenTofu S3 backend supported with path-style addressing and conditional writes?                        |
| Q13 | GreenNode vStorage | Where is each region's data stored, including replicas?                                                                        |
| Q14 | GreenNode compute  | Is an ARM vServer offered? Is Ubuntu 24.04 available? Which volume types are encrypted?                                        |
| Q15 | GreenNode vMonitor | Which log retention periods exist, and where are logs and metrics stored?                                                      |
| Q16 | GreenNode vDB      | Are PostgreSQL 18 and point-in-time recovery planned, and when?                                                                |
| Q17 | GreenNode          | Is there a data processing agreement? Does any subprocessor store customer data outside Vietnam?                               |
| Q18 | Bizfly             | What are the SMTP host, ports, and TLS modes for Email Transaction?                                                            |
| Q19 | Bizfly             | How are bounces and complaints reported? Is there a suppression list? What are the rate limits?                                |
| Q20 | Bizfly             | Which DKIM key lengths and selectors do Email Transaction and Business Email use?                                              |
| Q21 | Bizfly             | Can Business Email import a Google Workspace mailbox?                                                                          |
| Q22 | Bizfly             | Where is data for both services stored? Is there a data processing agreement?                                                  |
