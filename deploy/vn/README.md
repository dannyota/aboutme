# Vietnam production

Code for production at `https://aboutme.vn` on one GreenNode vServer in Ho Chi
Minh City, under [ADR 0027](../../docs/adr/0027-vietnam-hosted-production.md)
and the [Vietnam production design](../../docs/design/vietnam-production.md).
`deploy/aws/` stays for the AWS test environment.

Nothing here has been applied. Facts marked **Unconfirmed** come from provider
docs or source and wait for a test on the account (design, "Provider facts to
confirm").

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

- A `vngcloud` CLI profile named `aboutme` (`VNG_PROFILE`), set up with
  `vngcloud --profile aboutme configure`. Agents use only a `--read-only`
  profile.
- The OpenTofu provider authenticates as an IAM service account through
  `CLIENT_ID` and `CLIENT_SECRET` in the environment, never a file in this tree.
- `TF_VAR_state_passphrase` for state encryption, at least 16 characters.

## Build order

1. Confirm each provider fact below on the account.
2. `scripts/buckets.sh --storage-project-id <id>`: the media, backup, and state
   buckets, each with its own service account, bucket policy, and key. It
   refuses to create a bucket until the CLI can set bucket encryption, unless
   the owner sets `BUCKET_ENCRYPTION=off`.
3. `scripts/quote.sh --max-price <vnd>`: the price gate, since OpenTofu has no
   price ceiling.
4. In `prod/`: `tofu init -backend-config=backend.hcl`, then
   `tofu plan -var-file=prod.tfvars` and `tofu apply`.
5. On the host: `host/data-volume.sh <device> --format` once, then
   `host/install.sh <bundle>` with the bundle copied by `rsync`.
6. `scripts/secrets.sh generate`, `import-s3 media`, `import-s3 backups`,
   `import smtp-username`, and `import smtp-password`.
7. `deploy-host.sh - <tag> db-bootstrap` on the host, then `probe/probe.sh`.
8. `scripts/budget.sh --limit-vnd <amount>`, then fill `ALERT_TO` and the SMTP
   settings in `/etc/aboutme/smtp.env`.
9. `DEPLOY_HOST=<ip> scripts/deploy.sh <tag> --first-deploy`.

## Data volume encryption

The server is created with a plain 30 GB root disk and no data disk; the 20 GB
data volume is created separately with `data_volume_encryption_type` (default
`aes-xts-plain64_256`) and attached. Encrypting a disk at server create adds a
surcharge of 30% of the flavor price (vngcloud wiki, Compute-Servers.md,
"Encrypted disks"); a separately created encrypted volume costs the same as a
plain one. The `vngcloud` CLI creates an encrypted volume from v0.59.0
(`volume create-volume --encryption-type-id`); `scripts/restore-drill.sh`
refuses an older CLI, and the production apply waits for the provider check
below.

**Unconfirmed:** that an encrypted volume attaches to a server created with
plain disks (wiki Compute-Servers.md only states it for a server created with an
encrypted disk).

## Logs and alarms

There is no log or monitoring service. The journal on the data volume is the log
store: 30 days, at most 2 GB (`host/etc/systemd/journald.conf.d/`). Every
aboutme unit, and the PostgreSQL, CrowdSec, and SSH services, names
`aboutme-alert@.service` in `OnFailure=`, and `aboutme-watch.timer` runs
`host/bin/watch.sh` every five minutes: root and data volume use above 70%,
failed units, PostgreSQL on its socket, WAL archive failures, the newest backup
older than 26 hours, the CrowdSec alert count and Coraza match count in the last
hour, and `totp_unavailable` in the server journal. `host/bin/alert.sh` mails
the support mailbox through Bizfly SMTP with msmtp, at most once a day per
condition, with names and counts only. The external app-down check is the Route
53 health check, outside this tree.

## Provider facts

| Fact                                                                                                                        | Status                                                                                                                                                            |
| --------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Provider `vngcloud/vngcloud` 1.3.21 (1.3.22 adds only VKS logging)                                                          | Read from the registry and source tag                                                                                                                             |
| Resource names: `vngcloud_vserver_server`, `_volume`, `_volume_attach`, `_secgroup`, `_secgrouprule`, `_network`, `_subnet` | [docs/resources](https://github.com/vngcloud/terraform-provider-vngcloud/tree/v1.3.21/docs/resources)                                                             |
| Authentication: `client_id` and `client_secret` (env `CLIENT_ID`, `CLIENT_SECRET`)                                          | [docs/index.md](https://github.com/vngcloud/terraform-provider-vngcloud/blob/v1.3.21/docs/index.md)                                                               |
| Default endpoints `*.vngcloud.vn` still answer after the GreenNode domain move                                              | **Unconfirmed**; the CLI wiki says the old hosts redirect and drop the Authorization header                                                                       |
| `vngcloud_vserver_volume.encryption_type` creates an encrypted volume                                                       | **Unconfirmed**; the argument exists ([vserver_volume.md](https://github.com/vngcloud/terraform-provider-vngcloud/blob/v1.3.21/docs/resources/vserver_volume.md)) |
| The UEFI Ubuntu image passes as `image_id`                                                                                  | **Unconfirmed** ([vserver_server.md](https://github.com/vngcloud/terraform-provider-vngcloud/blob/v1.3.21/docs/resources/vserver_server.md))                      |
| A server without `ssh_key` is accepted (the admin key comes through cloud-init)                                             | **Unconfirmed**; the provider marks it optional, the CLI requires one                                                                                             |
| The floating IP is created with the server (`attach_floating`) and deleted on detach                                        | Read from source; there is no standalone floating IP resource, so the server has `prevent_destroy`                                                                |
| The new security group's default egress allows outbound traffic                                                             | **Unconfirmed**; `probe/probe.sh` checks it                                                                                                                       |
| vStorage as the OpenTofu S3 backend and pgBackRest repository, path-style                                                   | **Unconfirmed** (design Q4); `use_lockfile` stays off until tested                                                                                                |
| An encrypted volume attaches to a server created with plain disks                                                           | **Unconfirmed**                                                                                                                                                   |

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
- Release images: a `linux/amd64` entry in each image index; `deploy.sh` refuses
  a tag without one.

## Paid resources and quotes

Read-only quotes on 2026-10-10, VND a month. The server quote below used the old
20 GB root disk setting. Run `scripts/quote.sh` again with the current 30 GB
default before creating paid resources.

| Resource                                       | Created by                 | Quote                     |
| ---------------------------------------------- | -------------------------- | ------------------------- |
| vServer `s2-general-2x4` with 20 GB root SSD   | `tofu apply`               | 631,600                   |
| 20 GB encrypted SSD data volume                | `tofu apply`               | 64,000                    |
| Floating IP                                    | `tofu apply`               | 120,000                   |
| vStorage package                               | the console, by the owner  | not quoted                |
| Restore drill server and volume, while it runs | `scripts/restore-drill.sh` | 695,600 a month, prorated |

The budget is free. Alerts use the Bizfly SMTP account the server already sends
with.
