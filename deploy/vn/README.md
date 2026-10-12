# Vietnam production

Code for production at `https://aboutme.vn` on one GreenNode vServer in Ho Chi
Minh City, under [ADR 0027](../../docs/adr/0027-vietnam-hosted-production.md)
and the [Vietnam production design](../../docs/design/vietnam-production.md).
`deploy/aws/` stays undeployed for a possible AWS ECS or Kubernetes path.

Production serves from HCM03-1C. Cutover completed on 2026-10-11 with release
v0.7.3, and AWS was removed. Cloudflare provides DNS-only records; the owner
edits them by hand. Facts marked **Unconfirmed** need an account check.

## Layout

| Path                         | What                                                                          |
| ---------------------------- | ----------------------------------------------------------------------------- |
| `prod/`                      | OpenTofu root: network, security group, server with floating IP, data volume  |
| `host/cloud-init.yaml.tftpl` | First boot only: the admin user and SSH settings                              |
| `host/data-volume.sh`        | Formats (on request) and mounts the encrypted data volume and its bind mounts |
| `host/install.sh`            | Idempotent host install: pinned packages, config, units, scripts              |
| `host/etc/`                  | Files copied to the host's `/etc`                                             |
| `host/quadlet/`              | Quadlet templates that `deploy-host.sh` renders per release                   |
| `host/env/`                  | Non-secret container settings; `*.example` files are copied once and kept     |
| `host/keys/`                 | The apt signing keys `install.sh` trusts, checked against pinned fingerprints |
| `edge/`                      | CrowdSec configuration and the vWAF escalation checklist                      |
| `probe/`                     | Host fact checks                                                              |
| `scripts/`                   | Deploy, fence, secrets, buckets, quotes, restore drill, cutover               |

## Local files that are never committed

Add these to `.git/info/exclude`, not to `.gitignore`:

```text
deploy/vn/prod/backend.hcl
deploy/vn/prod/prod.tfvars
deploy/vn/prod/.terraform/
deploy/vn/prod/*.tfstate
deploy/vn/prod/*.tfstate.*
deploy/vn/prod/*.tfplan
```

The vStorage state key lives in `~/.config/aboutme/vn/`, outside the checkout.
`~/src/aboutme-infra` keeps `backend.hcl`, `prod.tfvars`, and the age copies of
the state passphrase and host secrets.

## Credentials on the laptop

- The `vngcloud` CLI (v0.66.0 or later) with the owner's setup account in the
  environment (`set -a; . <env file>; set +a` in the command that needs it).
  `VNG_PROFILE` names a `~/.vngcloud` profile instead. Agents add `--read-only`
  unless a step writes.
- The OpenTofu provider authenticates as the `aboutme-tofu` IAM service account
  (`vServerFullAccess`) through `CLIENT_ID` and `CLIENT_SECRET`, and state
  encryption reads `TF_VAR_state_passphrase`. All three live in the main
  checkout's ignored `.dev/credentials/greennode.env`, mode 0600, with an age
  copy in `aboutme-infra`; load the file only in the command that runs `tofu`.

## SSH access

SSH listens on TCP 22922 from any source (owner, 2026-10-10). Access uses only
an `ssh-ed25519` admin key, with root login disabled and
`AllowUsers aboutme-admin`. CrowdSec bans SSH attacks. The admin key is the
public half of the owner's commit-signing key.

`prod/variables.tf` sets `ssh_port` to 22922 and `ssh_allowlist` to
`["0.0.0.0/0"]`. Laptop scripts use `SSH_PORT`, default 22922. Set `SSH_PORT` to
the same value as `ssh_port` when using another port.

## Build order

1. Confirm each provider fact below on the account.
2. `scripts/buckets.sh --storage-project-id <id>`: the media, backup, and state
   buckets, each with its own service account, bucket policy, and key. Each
   bucket is created with default encryption (SSE-S3, AES256), and the script
   refuses an existing bucket without it, unless the owner sets
   `BUCKET_ENCRYPTION=off`. Encrypted buckets refuse server-side copy (501).
3. In `prod/`: `tofu init -backend-config=backend.hcl`, then
   `tofu plan -var-file=prod.tfvars -out=prod.tfplan`.
4. Still in `prod/`, run
   `../scripts/quote.sh --plan prod.tfplan --max-price <vnd>`, then
   `tofu apply prod.tfplan`. The price gate quotes the saved plan. Apply only
   that same file after the gate passes.
5. On the host: `host/data-volume.sh <device> --format` once, then
   `host/install.sh <bundle>` with the bundle copied by `rsync`.
6. `scripts/secrets.sh generate`, `import-s3 media`, `import-s3 backups`,
   `import smtp-username`, and `import smtp-password`.
7. `deploy-host.sh - <tag> db-bootstrap` on the host, then `probe/probe.sh`.
8. `scripts/budget.sh --limit-vnd <amount>`, then fill `ALERT_TO` and the SMTP
   settings in `/etc/aboutme/smtp.env`.
9. Point DNS at the floating IP before starting Caddy, so Let's Encrypt can
   validate HTTP-01. Follow the [DNS runbook](../../docs/runbooks/dns.md).
10. `DEPLOY_HOST=<ip> scripts/deploy.sh <tag> --first-deploy`.

Installation creates the root reporter's observation directory and the
observer-owned public directory, enables `aboutme-deployment-document.timer`,
and installs the observer Quadlet template. A release with
`aboutme-observer:<tag>-amd64` starts or restarts `aboutme-observer.service`
after checking its provenance and single `linux/amd64` manifest. The plain
observer version tag remains arm64 for Lambda. The observer stays up during
maintenance. Deploys and rollbacks without that image keep the installed
observer unit. See the [observer runbook](../../docs/runbooks/observer.md).

To rotate a media or backup key, create its new local key file, then run
`scripts/secrets.sh import-s3 media --replace` or
`scripts/secrets.sh import-s3 backups --replace`. The script deletes the local
file only after the host import and Podman sync succeed. A refused or failed
replacement keeps the local file for recovery.

## Data volume encryption

The server is created with encryption enabled and a 30 GB encrypted root disk.
The 20 GB encrypted data volume is created separately and attached. Both disks
use `disk_encryption_type` (default `aes-xts-plain64_256`). A server created
with plain disks refuses an encrypted volume with a 400 `BadRequest`
(`cannot attach encryption volume`; vngcloud wiki Compute-Servers.md, "Encrypted
disks", a live result).

Encrypting any disk at server create adds one `CES` line of 30% of the flavor
price, 170,280 VND a month for `s2-general-2x4`. Encrypting the root disk too
adds no further charge. The `vngcloud` CLI creates an encrypted volume from
v0.59.0 (`volume create-volume --encryption-type-id`);
`scripts/restore-drill.sh` refuses an older CLI.

## Logs and alarms

There is no log or monitoring service. The journal on the data volume is the log
store: 30 days, at most 2 GB (`host/etc/systemd/journald.conf.d/`). Every
aboutme unit, and the PostgreSQL, CrowdSec, and SSH services, names
`aboutme-alert@.service` in `OnFailure=`, and `aboutme-watch.timer` runs
`host/bin/watch.sh` every five minutes: root and data volume use above 70%,
failed units, PostgreSQL on its socket, WAL archive failures, the newest backup
at least 26 hours old, the CrowdSec alert count and Coraza match count in the
last hour, and `totp_unavailable` in the server journal. `host/bin/alert.sh`
mails the support mailbox through Bizfly SMTP with msmtp, at most once per
condition per UTC day, with names and counts only. The external app-down check
is the GitHub uptime workflow. The
[production runbook](../../docs/runbooks/production.md#alarms) owns its
schedule, notification recipient, and limits.

## Restore drill

The production OpenTofu root can create two persistent, detached security groups
for a restore drill. Set the nullable `drill_network` object in the ignored
`prod.tfvars` with production's private IPv4 `/32`, nonempty resolver and
time-service IPv4 `/32` sets, and reviewed public package and HCM04 IPv4 CIDR
sets. The default is `null`, which creates no drill groups. The configuration
has no live endpoint defaults.

The optional groups are named `aboutme-drill-bootstrap` and
`aboutme-drill-restore`. OpenTofu does not attach either group to a server or
allocate a drill resource. The provider adds unmanaged default egress to each
new group, so treat both groups as untrusted. Remove the default egress and
audit the exact rules and attachments before any attachment. The OpenTofu
configuration and mocked plan tests do not perform that reconciliation and do
not make a live drill safe to run.

`scripts/drill-journal.sh` is a standalone transport for bounded JSON records
under `restore-drill/v1/`. Source it, then call
`drill_journal_put_create <record-path> <file>` or
`drill_journal_get <record-path> <new-file>`. It uses the existing
`~/.config/aboutme/vn/tfstate-s3-credentials` file without printing its values.
Set `DRILL_STATE_CREDENTIALS_FILE` to an alternate reviewed state credential
file when the operator configuration is elsewhere. The caller owns record
fields, chain validation, fencing, and secret-free content. The transport has no
delete operation and is not a distributed lock.

The journal uses reviewed `vngcloud` v0.78.0 native object GET and create-only
PUT with a fixed HCM04 endpoint, isolated configuration, and bounded transfers.
Its credential file must be a regular non-symlink file owned by the operator,
mode 0600, containing only the static `[default]` key pair. The standalone proof
uses the reviewed AWS CLI 2.37.9 runtime and selects its legacy error format.
Its migration waits for native HEAD, DELETE, and bucket-versioning operations
with S3 credentials; the AWS CLI empty-message conditional-conflict failure
still blocks that proof.

Before using the journal, run `scripts/drill-state-proof.sh` from an attended
operator machine. The command creates one unique nonsecret object under
`restore-drill/probes/`, proves that a second conditional write is refused,
reads back the original bytes, and deletes only that object. It refuses a
version-enabled or version-suspended bucket because deletion could retain an
older object version. Its final absence check covers the current object key; it
does not claim physical erasure by the storage provider.

During the short proof, no other process may mutate its printed, unpredictable
probe key and no administrator may change bucket versioning. Unrelated state and
journal writes may continue. Cleanup reads the probe again and deletes it only
when its bytes match a payload written successfully by that proof. An unreadable
or foreign value remains at the printed recovery key for attended resolution.

`scripts/restore-drill.sh` creates a temporary encrypted server and data volume,
reaches its private IP through the production host as an SSH jump host, installs
PostgreSQL and pgBackRest, then restores the latest backup and a point-in-time
target. It checks versions, row counts, grants, and an `aboutme_app` read. The
exit trap deletes the server and volume unless `--keep` was requested.

The server has no floating IP and no NAT, so it has no outbound internet for
packages or vStorage. Outbound networking remains open. GreenNode Public NAT
adds a `0.0.0.0/0` route for the whole VPC, which sends every VM's outbound
traffic through it. Do not add a Public NAT to the production VPC for the drill.

## Cutover prerequisites

`scripts/cutover.sh` is a reference for an AWS-to-Vietnam data move. Before
`preflight`, the Vietnam host must already serve maintenance with its server
stopped. Preflight stops job timers and active jobs and checks that state; it
does not start maintenance. AWS must also serve maintenance with its app and
schedules stopped. The AWS stack is not deployed, so the script cannot run
against the current infrastructure.

## Reboot

Use the GreenNode API to reboot. An in-guest `systemctl reboot` hung once.
Readiness stayed failed once after a hard reset until `aboutme-server`
restarted. The cause remains open. The
[production runbook](../../docs/runbooks/production.md#reboot) owns the host
checks.

## Provider facts

| Fact                                                                                                                        | Status                                                                                                                                                                                                                            |
| --------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Provider `vngcloud/vngcloud` 1.3.21 (1.3.22 adds only VKS logging)                                                          | Read from the registry and source tag                                                                                                                                                                                             |
| Resource names: `vngcloud_vserver_server`, `_volume`, `_volume_attach`, `_secgroup`, `_secgrouprule`, `_network`, `_subnet` | [docs/resources](https://github.com/vngcloud/terraform-provider-vngcloud/tree/v1.3.21/docs/resources)                                                                                                                             |
| Authentication: `client_id` and `client_secret` (env `CLIENT_ID`, `CLIENT_SECRET`)                                          | [docs/index.md](https://github.com/vngcloud/terraform-provider-vngcloud/blob/v1.3.21/docs/index.md)                                                                                                                               |
| Default endpoints `*.vngcloud.vn` still answer after the GreenNode domain move                                              | Yes: the provider's default token and vServer URLs worked for plan and apply (2026-10-10)                                                                                                                                         |
| `vngcloud_vserver_volume.encryption_type` creates an encrypted volume                                                       | Yes: the data volume reads `EncryptionType` `aes-xts-plain64_256`                                                                                                                                                                 |
| The UEFI Ubuntu image passes as `image_id`                                                                                  | Yes                                                                                                                                                                                                                               |
| User data with an SSH key ID at server creation                                                                             | Refused by GreenNode. `restore-drill.sh` omits `--ssh-key-id`; cloud-init supplies the admin key                                                                                                                                  |
| The floating IP is created with the server (`attach_floating`) and deleted on detach                                        | Created with the server; replacing the server twice kept the same address. The server keeps `prevent_destroy`                                                                                                                     |
| The new security group's default egress allows outbound traffic                                                             | Yes: cloud-init installed packages over it                                                                                                                                                                                        |
| vStorage as the OpenTofu S3 backend and pgBackRest repository, path-style                                                   | Lock file works (412 on a contended lock), so `versions.tf` sets `use_lockfile`; pgBackRest uses the backup bucket; restore-drill outbound networking remains open                                                                |
| An encrypted volume attaches to a server created with plain disks                                                           | No: 400 `cannot attach encryption volume` (wiki Compute-Servers.md)                                                                                                                                                               |
| GreenNode's Ubuntu 24.04 image ships its own `sshd_config`                                                                  | Yes: `Port 234`, no `Include` of `sshd_config.d`, password login on, and `ssh.service` without socket activation. cloud-init replaces the file before sshd starts, and the image's `ssh-keygen.service` fails on DSA without harm |
| A security group description takes any text                                                                                 | No: only letters, digits, `_`, `.`, `@`, `-`, and spaces                                                                                                                                                                          |
| `user_data` alone passes cloud-init                                                                                         | No: the provider also needs `user_data_base64_encode`; the root sends base64 with the flag set                                                                                                                                    |
| The console log is readable when SSH fails                                                                                  | Yes: `GET .../v2/{project}/servers/{server}/console-log`; cloud-init writes an SSH check to the serial console                                                                                                                    |

## Provider questions

Answers to the design's provider questions, checked on 2026-10-10 against the
account and first-party pages. "Not published" means no first-party page says
so.

| ID  | Answer                                                                                                                                                                                     | Source                                                                               |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ |
| Q1  | Free Layer 3 and 4 DDoS protection for every customer, no Layer 7; capacity not published                                                                                                  | [DDoS FAQ](https://docs.vngcloud.vn/vng-cloud-document/faq/ddos)                     |
| Q2  | IPv6 on a floating IP: not published                                                                                                                                                       | [Floating IP](https://docs.greennode.ai/vserver/compute-hcm03-1a/vpc/floating-ip)    |
| Q3  | Yes: 412 `PreconditionFailed` on an existing key, 200 on a new one. `If-Match` on `PUT` fails with the quoted ETag; `DELETE` ignores `If-Match`                                            | Account test (signed `curl`)                                                         |
| Q4  | Path style, 12 MB multipart, `ListObjectsV2` with a delimiter, `DeleteObjects`, and default CRC32 checksums work; the OpenTofu 1.12.6 lock file works                                      | Account test (AWS CLI 2.37, OpenTofu)                                                |
| Q5  | HCM04 is in Ho Chi Minh City; GreenNode is a Vietnamese provider, no further questions                                                                                                     | Owner, 2026-10-10                                                                    |
| Q6  | No ARM flavor; zone HCM03-1A needs a provider request, HCM03-1C is enabled; encryption types `aes-xts-plain64_128` and `_256`; encryption at server create adds a 170,280 VND monthly line | Account (`compute list-flavors`, `portal list-zones`, `compute quote-create-server`) |
| Q7  | vDB PostgreSQL: 12 to 15 single, 15 to 17 cluster; no PostgreSQL 18 or plan published                                                                                                      | [vDB](https://docs.greennode.ai/vdb)                                                 |
| Q8  | GreenNode is a Vietnamese provider; no DPA request, no further questions                                                                                                                   | Owner, 2026-10-10                                                                    |
| Q9  | `smtp.bizflycloud.vn`, 465 implicit TLS or 587 STARTTLS                                                                                                                                    | Account test                                                                         |
| Q10 | No webhook or suppression API; logs show each delivery and its SMTP reply; 100,000 an hour, 1,000,000 a day                                                                                | Account API (`openapi.json`, `domains/{id}/quota`, `logs`)                           |
| Q11 | 2048-bit RSA, signed as `s=etrans` (portal shows `dkim`); envelope sender is the From address; no return-path setting; one domain on the PAYG pack                                         | Account test                                                                         |
| Q12 | Bizfly is a Vietnamese provider; no DPA request, no further questions                                                                                                                      | Owner, 2026-10-10                                                                    |

## Host packages and what each fetch verifies

| Package                                           | Pinned version         | Verified against                                                       |
| ------------------------------------------------- | ---------------------- | ---------------------------------------------------------------------- |
| `postgresql-18`, `postgresql-client-18`, `libpq5` | `18.6-1.pgdg24.04+2`   | PGDG key `host/keys/pgdg.asc`, fingerprint `B97B0AFC…ACCC4CF8`         |
| `postgresql-common`                               | `293.pgdg24.04+1`      | the same PGDG key                                                      |
| `pgbackrest`                                      | `2.59.3-1.pgdg24.04+1` | the same PGDG key                                                      |
| `crowdsec`                                        | `1.8.1`                | `host/keys/crowdsec.asc`, fingerprint `6A89E3C2…6E93CD0C`              |
| `crowdsec-firewall-bouncer-nftables`              | `0.0.36`               | the same CrowdSec key                                                  |
| `age`, `msmtp`                                    | Ubuntu archive         | Ubuntu archive signature; security updates through unattended-upgrades |

CrowdSec publishes no key fingerprint; the committed key is the one
packagecloud.io served on 2026-10-10. apt holds every pinned package, and
unattended-upgrades installs only Ubuntu security updates.

## Contracts with other lanes

These contracts match the server and Caddy images:

- Caddy image (`EDGES=direct`): `DIRECT_HOST`, `DIRECT_WWW_HOST`,
  `CROWDSEC_API_URL` (`http://127.0.0.1:8095`), `CROWDSEC_API_KEY` (Podman
  secret `crowdsec-bouncer-key`), Caddy storage at `/data`, and metadata-only
  WAF match logs at `/var/log/caddy/waf/serving-match.log` and
  `/var/log/caddy/waf/maintenance-match.log`, mounted from tmpfs
  `/run/aboutme/caddy-log`. Each stream has at most two rolled files. The image
  must start without `ORIGIN_CERT` and `ORIGIN_KEY` when `EDGES=direct`, and
  binds 80 and 443 as uid 10001 (the host sets
  `net.ipv4.ip_unprivileged_port_start=80`).
- Server SMTP sender: `AUTH_EMAIL_MODE=smtp`, `SMTP_HOST`, `SMTP_PORT`,
  `SMTP_TLS`, `SES_FROM_ADDRESS`, `SES_FROM_NAME`, and the Podman secrets
  `smtp-username` and `smtp-password` as `SMTP_USERNAME` and `SMTP_PASSWORD`.
- App release images: a `linux/amd64` entry in each image index; `deploy.sh`
  refuses a tag without one and pins that platform image's digest, which its
  provenance and SBOM attestations name. The optional observer uses a separate
  single-platform `<tag>-amd64` image.

## Paid resources and quotes

Read-only quotes on 2026-10-10 in zone `HCM03-1C`, VND a month, 10% VAT included
(owner, 2026-10-10). The host and storage total 1,047,880. Run
`scripts/quote.sh` again before creating paid resources.

| Resource                                       | Created by                                  | Quote                        |
| ---------------------------------------------- | ------------------------------------------- | ---------------------------- |
| vServer `s2-general-2x4` with 30 GB root SSD   | `tofu apply`                                | 663,600                      |
| 20 GB SSD data volume                          | `tofu apply`                                | 64,000                       |
| Disk encryption chosen at server create (CES)  | `tofu apply`                                | 170,280                      |
| Floating IP                                    | `tofu apply`                                | 120,000                      |
| vStorage Gold project, 30 GB                   | `storage create-project`, bought 2026-10-10 | 30,000                       |
| Restore drill server and volume, while it runs | `scripts/restore-drill.sh`                  | the server's price, prorated |

The vStorage project has monthly auto-renew enabled since 2026-10-10. The next
renewal is 2026-11-09. The owner checks the renewal and available balance so
media, backups, and infrastructure state remain available.

The budget is free. Alerts use the Bizfly SMTP account the server already sends
with.
