# Infrastructure tasks

Every task here writes to AWS or Cloudflare. Show the owner the `tofu plan`
summary and get a go-ahead before each `tofu apply`. AWS credentials come from
`aws login`. OpenTofu manages AWS only; Cloudflare changes go through the
owner's authenticated Cloudflare MCP connection, and the origin-pull certificate
is uploaded once in the Cloudflare dashboard. No credential is written to the
repository.

Common check for every task:

```sh
tofu -chdir=deploy/aws/prod fmt -check -recursive
tofu -chdir=deploy/aws/prod validate
```

State encryption reads `state_kms_key_arn` before the plan is loaded, so
`tofu apply` needs `-var-file=prod.tfvars` even when it applies a saved plan.

If a resource attribute named below differs in the locked provider version,
follow the provider documentation for that version and note the change in the
task report.

Every task below is complete. Each entry keeps its goal, the files it owns, its
interfaces, and its recorded result; the full steps are in Git history at commit
`a37d863`.

## Task 8

### State bootstrap and production root skeleton

**Files:**

- Create: `deploy/aws/bootstrap/main.tf`
- Create: `deploy/aws/prod/{versions,variables,main,outputs}.tf`
- Create: `deploy/aws/prod/backend.hcl.example`,
  `deploy/aws/prod/prod.tfvars.example`
- Modify: `.gitignore` (integration owner)

**Interfaces:** Produces the S3 state bucket, the state KMS key, and a `prod`
root that later tasks extend with `module` blocks.

`deploy/aws/bootstrap` holds local state on the owner's laptop only and
provisions an S3 bucket (versioned, SSE-KMS, TLS-only policy, public access
blocked) and a KMS key for state encryption. `deploy/aws/prod` is the S3-backed,
KMS-encrypted root, pinned to OpenTofu 1.12.6 and the AWS provider (`~> 6.0`,
plus a `us_east_1` alias); its variables are `account_id`, `state_kms_key_arn`,
`media_bucket_name`, `ses_from_address` (default is the service's SES sender
address), `ses_configuration_set`, and the three image references. `backend.hcl`
and `prod.tfvars` are gitignored; their `.example` files list every key. The
integration owner added a public `tofu` CI job that installs OpenTofu 1.12.6 and
runs `tofu fmt -check` and `tofu init -backend=false && tofu validate` with no
secrets.

Result (2026-09-16): the bucket and key exist, the `prod` root initializes and
its state object is OpenTofu ciphertext. OpenTofu has no Cloudflare provider;
see Task 11. The bucket keeps 20 noncurrent state versions for 90 days.

## Task 9

### Network, RDS and S3

**Files:**

- Create: `deploy/aws/modules/network/{main,variables,outputs}.tf`
- Create: `deploy/aws/modules/data/{main,variables,outputs}.tf`
- Modify: `deploy/aws/prod/main.tf`, `deploy/aws/prod/outputs.tf`

**Interfaces:** Produces `module.network.vpc_id`, `public_subnet_id`,
`host_security_group_id`, `db_security_group_id`; `module.data.db_endpoint`,
`db_master_secret_arn`, `media_bucket_arn`, `media_bucket_name`.

The network module builds a `/16` VPC with one public subnet (internet gateway)
and two private subnets, a host security group that allows 443 only from
Cloudflare's published IPv4 ranges, and a database security group that allows
5432 only from the host security group. The data module builds a private,
encrypted RDS PostgreSQL instance (`db.t4g.micro`, 20 GB gp3, 30-day backups,
deletion protection, `rds.force_ssl=1`, `log_statement=none`, master password
managed in Secrets Manager) and the unversioned, encrypted media S3 bucket with
public access blocked, bucket-owner-enforced ownership, an
incomplete-multipart-upload lifecycle rule and a TLS-only bucket policy. The
root fetches Cloudflare's IPv4 ranges from the public `/client/v4/ips` endpoint
through the `http` provider and passes them to the network module.

Result (2026-09-17): network, RDS and bucket exist and `tofu plan` reports no
changes. RDS 18.6 is private, encrypted, keeps 30-day backups, has deletion
protection and runs in `ap-southeast-1a`; `rds.force_ssl` is 1. The first apply
placed the subnets in Local Zones, so the zone lookup now filters
`zone-type = availability-zone`. `rds.force_ssl` carries
`apply_method = "pending-reboot"` to match what RDS records. The Cloudflare
ranges come from the public `/client/v4/ips` endpoint through the `http`
provider.

## Task 10

### Secrets, IAM roles and ECS task definitions

**Files:**

- Create: `deploy/aws/scripts/secrets.sh`
- Create: `deploy/aws/scripts/tls.sh`
- Create: `deploy/aws/modules/identity/{main,variables,outputs}.tf`
- Create: `deploy/aws/modules/tasks/{main,variables,outputs}.tf`
- Modify: `deploy/aws/prod/main.tf`
- Modify: `docs/design/single-host-production.md` (secret generation paragraph)

**Interfaces:**

- SSM parameters (all `SecureString` unless noted) under `/aboutme/prod/`:
  `db/migrator-password`, `db/app-password`, `auth-email/active-key-id`
  (`String`), `auth-email/active-key`, `password-rate-hmac-key`,
  `tls/origin-key`, `tls/origin-cert` (`String`, written by Task 11),
  `tls/origin-pull-ca` (`String`).
- Task definition families: `aboutme-prod-app`, `-web`, `-migrate`, `-db-setup`,
  `-jobs`.
- Container names used by Task 13: `caddy`, `server`, `web`, `migrate`, `jobs`,
  `admin`.

Secret values are generated by `secrets.sh`, not by OpenTofu, because two keys
must be exactly 32 bytes in unpadded base64url, which the OpenTofu random
provider does not produce, and one mechanism for every value is simpler to
review. OpenTofu only references parameter names. `secrets.sh` creates each
missing password and key with `openssl` and never overwrites or prints one.
`tls.sh origin` writes a new origin key straight to SSM and a public CSR to
`deploy/aws/prod/origin.csr`; `tls.sh pull` writes a new origin-pull CA
certificate to SSM (its key is discarded) and a client certificate under
`$XDG_RUNTIME_DIR`, a per-user tmpfs, for the owner to upload in the Cloudflare
dashboard; `tls.sh forget-pull` deletes that directory.

The identity module creates the EC2 instance role (ECS and SSM managed
policies), one execution role per task family scoped to only the SSM parameters
and, for the `db-admin` family, the RDS master secret that family needs, and two
task roles: `app` (media bucket access under `resumes/*` plus
`ses:SendEmail`/`SendRawEmail` restricted to the configured from-address) and
`jobs` (media bucket access only). Every role logs to `/aboutme/prod` (180-day
retention).

The tasks module renders the task definition families (five since
[ADR 0038](../../../adr/0038-single-baseline-and-plain-migrator.md) collapsed
first-deploy setup into one task). `app` runs on the host network with two
containers: `server` (port 8080, health check on `/healthz`, environment
includes `PRINT_LISTEN_ADDR`, `MCP_ENABLED=true`, media/SES/build-digest
settings) and `caddy` (port 443, tmpfs `/run/caddy`, `CLOUDFLARE_RANGES`,
depends on `server` being healthy). `web`, `migrate` and the first-deploy family
(`db-setup`, container name `admin`) run bridge-network, single-container, no
task role; `db-setup` uses the `db-admin` execution role and reads `PGPASSWORD`
from the RDS master secret. `jobs` runs the `server` binary under the `jobs`
task role; the scheduler (Task 12) supplies its command.

Result (2026-09-17): five secrets, the origin key, the origin-pull CA and the
Origin CA certificate are in SSM; 26 IAM, log and task definition resources
exist, and `tofu plan` reports no changes. IAM simulation confirmed each
execution role reads only its own parameters, only `db-admin` reads the master
secret, and the task roles are limited to `resumes/` objects and, for `app`,
mail. Differences from the design: the AWS CLI cannot read `file:///dev/stdin`,
so both scripts write each request to a private tmpfs file and delete it;
`ListBucket` has no prefix condition, because `HeadObject` reports a missing key
as 404 only with it; the Origin CA certificate was issued here through the
Cloudflare MCP; the task definitions start with `:unreleased` image tags until
the first deploy. The first client certificate lacked leaf extensions, so
`tls.sh` has `origin`, `pull` and `forget-pull` steps and marks the client
certificate `CA:FALSE` for client authentication. The owner uploaded it on
2026-09-17, and Cloudflare shows it active.

## Task 11

### Host, ECS services and Cloudflare edge

**Files:**

- Create: `deploy/aws/modules/host/{main,variables,outputs}.tf`
- Create: `docs/runbooks/production.md` (Cloudflare settings section)
- Modify: `deploy/aws/prod/main.tf`

**Interfaces:** Produces `module.host.instance_id`, `public_ip`, `cluster_name`,
service names `aboutme-prod-app` and `aboutme-prod-web`.

On 2026-09-16 the zone held only mail records (MX, TXT, DKIM CNAMEs and the SES
`bounce` records) and no apex `A` or `www` record; every existing record was
kept. The host module runs the latest Bottlerocket `aws-ecs-2` arm64 AMI on a
`t4g.small` in the public subnet with a 4 GB encrypted root volume and a 20 GB
encrypted data volume, IMDSv2 required with a hop limit of 1, termination
protection on, and an Elastic IP; the AMI and user data are ignored after
creation so Bottlerocket can update itself in place (Task 12). The ECS cluster
disables Container Insights. The `app` and `web` services run on the cluster
with `desired_count` and `task_definition` ignored after creation, because
`deploy.sh` (Task 13) owns both.

After the host had its Elastic IP, the Cloudflare edge for zone `aboutme.vn` was
configured through the MCP connection: the apex `A` and `www` `CNAME` records
(proxied), SSL/TLS mode Full (strict), Always Use HTTPS, minimum TLS 1.2 with
TLS 1.3 on, HSTS (no subdomains, so Google Workspace and other subdomains are
not forced onto it), Bot Fight Mode left off, a cache-bypass rule for everything
except `/_nuxt/`, an Origin CA certificate issued from
`deploy/aws/prod/origin.csr`, and zone-level Authenticated Origin Pulls turned
on before the first deploy because Caddy requires the client certificate from
its first start (review finding B1). Every setting is recorded in the Cloudflare
settings table in
[the production runbook](../../../runbooks/production.md#cloudflare-settings),
which is the table to update on a future change. The issued Origin CA
certificate, which is public, is stored as the `String` parameter
`/aboutme/prod/tls/origin-cert`.

Result (2026-09-17): the host runs Bottlerocket 1.65.0 in `ap-southeast-1a` with
IMDSv2 hop limit 1 and registered with the cluster. The Cloudflare records and
settings above are applied and read back, and `docs/runbooks/production.md`
records them. `https://aboutme.vn/` returns 521 and the Elastic IP does not
answer, as expected before a deploy. Differences: both services start at zero
tasks because no image exists yet, so Task 13's `deploy.sh` must set the `web`
count to one as well as `app`; the data volume is in `ignore_changes`, because
AWS reports computed fields and default tags on it that would force a host
replacement; Bot Fight Mode was already off. Zone-level origin pulls were turned
on on 2026-09-17, before the first deploy, after the review found that Caddy
requires the certificate from its first start.

## Task 12

### Jobs and alarms

**Files:**

- Create: `deploy/aws/modules/ops/{main,variables,outputs}.tf`
- Modify: `deploy/aws/prod/main.tf`

**Interfaces:** Produces SNS topic `aboutme-prod-alerts`, four schedules, the
Route 53 health check, alarms and the maintenance window.

EventBridge Scheduler runs four jobs in schedule group `aboutme-prod-jobs`
through a scheduler role scoped to `ecs:RunTask` on the `aboutme-prod-jobs` task
definition and `iam:PassRole` on its execution and task roles:
`idempotency-expiry-sweep` and `media-deletion-sweep` hourly,
`privacy-retention-sweep` daily at 02:30 UTC, and `media-orphan-sweep` weekly.
Each target references the task family without a revision, so it always runs the
latest active `jobs` revision that `deploy.sh` registers; `deploy.sh` disables
the group during a deploy and re-enables it after.

An SNS topic `aboutme-prod-alerts` (with an email subscription the owner
confirms) collects CloudWatch alarms: host status checks (one recovers the
instance automatically), app CPU and memory over 85%, RDS CPU over 80%, RDS
credit balance under 20, RDS free storage under 2 GiB, RDS connections over a
threshold, SES bounce and complaint rate, a Scheduler target-error alarm for the
jobs group, and an EventBridge rule on ECS task stops for the jobs family or
either service. A Route 53 HTTPS health check against `/healthz` and its alarm
run in `us-east-1`, because Route 53 metrics exist only there. Budgets, anomaly
monitoring and the alert address are account-level and stay out of this
repository; OpenTofu takes the alert address from the ignored `prod.tfvars`. A
monthly SSM maintenance window applies Bottlerocket updates on the first Sunday
of the month at 20:00 UTC (Monday 03:00 Vietnam).

Result (2026-09-17): 28 resources exist and `tofu plan` reports no changes. SSM
Run Command reaches the host and `apiclient update check` works, so the update
window is usable. Differences: the schedules start `DISABLED` and ignore later
state, so jobs never run the `:unreleased` image, and `deploy.sh` enables them;
the site-down alarm has `actions_enabled` from `site_alarm_enabled`, false until
the first healthy deploy (Task 15 turns it on); the weekly orphan sweep runs
Sunday 18:45 UTC, before the monthly update window; SES bounce and complaint
alarms are not added, because the email stack already has them; the connection
alarm threshold is 60. The owner still has to confirm the two SNS subscription
emails.
