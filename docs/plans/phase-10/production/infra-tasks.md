# Infrastructure tasks

Every task here writes to AWS or Cloudflare. Show the owner the `tofu plan`
summary and get a go-ahead before each `tofu apply`. Credentials come from
`aws login` and a `CLOUDFLARE_API_TOKEN` in the owner's shell; nothing is
written to the repository.

Common check for every task:

```sh
tofu -chdir=deploy/aws/prod fmt -check -recursive
tofu -chdir=deploy/aws/prod validate
```

If a resource attribute named below differs in the locked provider version,
follow the provider documentation for that version and note the change in the
task report.

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

- [x] **Step 1: Ignore private files**

Add to `.gitignore`:

```gitignore
deploy/aws/**/.terraform/
deploy/aws/**/*.tfstate
deploy/aws/**/*.tfstate.*
deploy/aws/**/*.tfplan
deploy/aws/prod/backend.hcl
deploy/aws/prod/prod.tfvars
```

Lock files (`.terraform.lock.hcl`) are committed.

- [x] **Step 2: Write the bootstrap root**

`deploy/aws/bootstrap/main.tf` uses local state, kept only on the owner's
laptop:

```hcl
terraform {
  required_version = "= 1.12.6"
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 6.0" }
  }
}

variable "account_id" { type = string }

provider "aws" {
  region              = "ap-southeast-1"
  allowed_account_ids = [var.account_id]
  default_tags { tags = { Project = "aboutme", Environment = "prod" } }
}

resource "aws_kms_key" "state" {
  description         = "aboutme-prod OpenTofu state encryption"
  enable_key_rotation = true
}

resource "aws_kms_alias" "state" {
  name          = "alias/aboutme-prod-state"
  target_key_id = aws_kms_key.state.key_id
}

resource "aws_s3_bucket" "state" {
  bucket = "aboutme-prod-tfstate-${var.account_id}"
}

resource "aws_s3_bucket_versioning" "state" {
  bucket = aws_s3_bucket.state.id
  versioning_configuration { status = "Enabled" }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "state" {
  bucket = aws_s3_bucket.state.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm     = "aws:kms"
      kms_master_key_id = aws_kms_key.state.arn
    }
    bucket_key_enabled = true
  }
}

resource "aws_s3_bucket_public_access_block" "state" {
  bucket                  = aws_s3_bucket.state.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_policy" "state" {
  bucket = aws_s3_bucket.state.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid       = "DenyInsecureTransport"
      Effect    = "Deny"
      Principal = "*"
      Action    = "s3:*"
      Resource  = [aws_s3_bucket.state.arn, "${aws_s3_bucket.state.arn}/*"]
      Condition = { Bool = { "aws:SecureTransport" = "false" } }
    }]
  })
}

output "state_bucket" { value = aws_s3_bucket.state.bucket }
output "state_kms_key_arn" { value = aws_kms_key.state.arn }
```

- [x] **Step 3: Write the production root skeleton**

`deploy/aws/prod/versions.tf`:

```hcl
terraform {
  required_version = "= 1.12.6"
  required_providers {
    aws        = { source = "hashicorp/aws", version = "~> 6.0" }
    cloudflare = { source = "cloudflare/cloudflare", version = "~> 5.0" }
  }
  backend "s3" {
    key          = "prod/terraform.tfstate"
    region       = "ap-southeast-1"
    encrypt      = true
    use_lockfile = true
  }
  encryption {
    key_provider "aws_kms" "state" {
      kms_key_id = var.state_kms_key_arn
      region     = "ap-southeast-1"
      key_spec   = "AES_256"
    }
    method "aes_gcm" "state" {
      keys = key_provider.aws_kms.state
    }
    state {
      method   = method.aes_gcm.state
      enforced = true
    }
    plan {
      method   = method.aes_gcm.state
      enforced = true
    }
  }
}

provider "aws" {
  region              = "ap-southeast-1"
  allowed_account_ids = [var.account_id]
  default_tags { tags = { Project = "aboutme", Environment = "prod" } }
}

provider "aws" {
  alias               = "us_east_1"
  region              = "us-east-1"
  allowed_account_ids = [var.account_id]
  default_tags { tags = { Project = "aboutme", Environment = "prod" } }
}

provider "cloudflare" {}
```

`deploy/aws/prod/variables.tf`:

```hcl
variable "account_id" { type = string }
variable "state_kms_key_arn" { type = string }
variable "cloudflare_zone_id" { type = string }
variable "alarm_email" { type = string }
variable "media_bucket_name" { type = string }
variable "ses_from_address" {
  type    = string
  default = "danny@aboutme.vn"
}
variable "ses_configuration_set" {
  type    = string
  default = "aboutme-auth"
}
variable "image_server" {
  type        = string
  description = "Initial server image reference with digest"
}
variable "image_web" { type = string }
variable "image_caddy" { type = string }
```

`deploy/aws/prod/main.tf` starts with `locals { name = "aboutme-prod" }`.
`outputs.tf` starts empty. The two `.example` files list every key with an empty
or placeholder value:

```hcl
# backend.hcl.example
bucket = "aboutme-prod-tfstate-<account id>"
```

```hcl
# prod.tfvars.example
account_id         = ""
state_kms_key_arn  = ""
cloudflare_zone_id = ""
alarm_email        = ""
media_bucket_name  = ""
image_server       = "ghcr.io/dannyota/aboutme-server@sha256:..."
image_web          = "ghcr.io/dannyota/aboutme-web@sha256:..."
image_caddy        = "ghcr.io/dannyota/aboutme-caddy@sha256:..."
```

- [x] **Step 4: Apply the bootstrap and initialize the root**

```sh
tofu -chdir=deploy/aws/bootstrap init
tofu -chdir=deploy/aws/bootstrap apply -var account_id=<id>
cp deploy/aws/prod/backend.hcl.example deploy/aws/prod/backend.hcl   # fill in
cp deploy/aws/prod/prod.tfvars.example deploy/aws/prod/prod.tfvars   # fill in
tofu -chdir=deploy/aws/prod init -backend-config=backend.hcl
tofu -chdir=deploy/aws/prod plan -var-file=prod.tfvars
```

Expected: `No changes.` The state object in S3 is ciphertext
(`aws s3 cp s3://.../prod/terraform.tfstate - | head -c 80` shows the OpenTofu
encrypted envelope, not JSON resources).

- [x] **Step 5: Add the public CI check**

Ask the integration owner to add a `tofu` job to `ci.yml` that installs OpenTofu
1.12.6 and runs `tofu fmt -check -recursive deploy/aws` and
`tofu -chdir=deploy/aws/prod init -backend=false && tofu -chdir=deploy/aws/prod validate`.
The job receives no secrets.

Result (2026-09-16): the bucket and key exist, the `prod` root initializes and
its state object is OpenTofu ciphertext. The Cloudflare provider block moves to
Task 9, where its first data source appears, so the skeleton plans without a
Cloudflare token. The bucket keeps 20 noncurrent state versions for 90 days.

## Task 9

### Network, RDS and S3

**Files:**

- Create: `deploy/aws/modules/network/{main,variables,outputs}.tf`
- Create: `deploy/aws/modules/data/{main,variables,outputs}.tf`
- Modify: `deploy/aws/prod/main.tf`, `deploy/aws/prod/outputs.tf`

**Interfaces:** Produces `module.network.vpc_id`, `public_subnet_id`,
`host_security_group_id`, `db_security_group_id`; `module.data.db_endpoint`,
`db_master_secret_arn`, `media_bucket_arn`, `media_bucket_name`.

- [ ] **Step 1: Write the network module**

```hcl
# modules/network/main.tf
variable "name" { type = string }
variable "cloudflare_ipv4_cidrs" { type = list(string) }

data "aws_availability_zones" "available" { state = "available" }

resource "aws_vpc" "main" {
  cidr_block           = "10.20.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = var.name }
}

resource "aws_internet_gateway" "main" { vpc_id = aws_vpc.main.id }

resource "aws_subnet" "public" {
  vpc_id            = aws_vpc.main.id
  cidr_block        = "10.20.0.0/24"
  availability_zone = data.aws_availability_zones.available.names[0]
  tags              = { Name = "${var.name}-public" }
}

resource "aws_subnet" "private" {
  count             = 2
  vpc_id            = aws_vpc.main.id
  cidr_block        = "10.20.${10 + count.index}.0/24"
  availability_zone = data.aws_availability_zones.available.names[count.index]
  tags              = { Name = "${var.name}-private-${count.index}" }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.main.id
  }
}

resource "aws_route_table_association" "public" {
  subnet_id      = aws_subnet.public.id
  route_table_id = aws_route_table.public.id
}

resource "aws_security_group" "host" {
  name        = "${var.name}-host"
  description = "aboutme-prod host - HTTPS from Cloudflare only"
  vpc_id      = aws_vpc.main.id
}

resource "aws_vpc_security_group_ingress_rule" "cloudflare" {
  for_each          = toset(var.cloudflare_ipv4_cidrs)
  security_group_id = aws_security_group.host.id
  cidr_ipv4         = each.value
  ip_protocol       = "tcp"
  from_port         = 443
  to_port           = 443
}

resource "aws_vpc_security_group_egress_rule" "host_all" {
  security_group_id = aws_security_group.host.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}

resource "aws_security_group" "db" {
  name        = "${var.name}-db"
  description = "aboutme-prod database - PostgreSQL from the host only"
  vpc_id      = aws_vpc.main.id
}

resource "aws_vpc_security_group_ingress_rule" "db_from_host" {
  security_group_id            = aws_security_group.db.id
  referenced_security_group_id = aws_security_group.host.id
  ip_protocol                  = "tcp"
  from_port                    = 5432
  to_port                      = 5432
}
```

Outputs: `vpc_id`, `public_subnet_id`, `private_subnet_ids`,
`host_security_group_id`, `db_security_group_id`.

- [ ] **Step 2: Write the data module**

```hcl
# modules/data/main.tf
variable "name" { type = string }
variable "private_subnet_ids" { type = list(string) }
variable "db_security_group_id" { type = string }
variable "media_bucket_name" { type = string }

resource "aws_db_subnet_group" "main" {
  name       = var.name
  subnet_ids = var.private_subnet_ids
}

resource "aws_db_parameter_group" "main" {
  name   = var.name
  family = "postgres18"
  parameter {
    name  = "rds.force_ssl"
    value = "1"
  }
  parameter {
    name  = "log_statement"
    value = "none"
  }
}

resource "aws_db_instance" "main" {
  identifier                  = var.name
  engine                      = "postgres"
  engine_version              = "18.6"
  instance_class              = "db.t4g.micro"
  allocated_storage           = 20
  storage_type                = "gp3"
  storage_encrypted           = true
  db_name                     = "aboutme"
  username                    = "aboutme"
  manage_master_user_password = true
  db_subnet_group_name        = aws_db_subnet_group.main.name
  vpc_security_group_ids      = [var.db_security_group_id]
  parameter_group_name        = aws_db_parameter_group.main.name
  publicly_accessible         = false
  multi_az                    = false
  backup_retention_period     = 30
  copy_tags_to_snapshot       = true
  deletion_protection         = true
  skip_final_snapshot         = false
  final_snapshot_identifier   = "${var.name}-final"
  auto_minor_version_upgrade  = true
  ca_cert_identifier          = "rds-ca-rsa2048-g1"
  apply_immediately           = false
}

resource "aws_s3_bucket" "media" { bucket = var.media_bucket_name }

resource "aws_s3_bucket_public_access_block" "media" {
  bucket                  = aws_s3_bucket.media.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_ownership_controls" "media" {
  bucket = aws_s3_bucket.media.id
  rule { object_ownership = "BucketOwnerEnforced" }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "media" {
  bucket = aws_s3_bucket.media.id
  rule {
    apply_server_side_encryption_by_default { sse_algorithm = "AES256" }
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "media" {
  bucket = aws_s3_bucket.media.id
  rule {
    id     = "abort-incomplete-uploads"
    status = "Enabled"
    filter {}
    abort_incomplete_multipart_upload { days_after_initiation = 1 }
  }
}

resource "aws_s3_bucket_policy" "media" {
  bucket = aws_s3_bucket.media.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid       = "DenyInsecureTransport"
      Effect    = "Deny"
      Principal = "*"
      Action    = "s3:*"
      Resource  = [aws_s3_bucket.media.arn, "${aws_s3_bucket.media.arn}/*"]
      Condition = { Bool = { "aws:SecureTransport" = "false" } }
    }]
  })
}
```

The media bucket has no versioning, as the deployment design requires. Outputs:
`db_endpoint = aws_db_instance.main.address`,
`db_instance_id = aws_db_instance.main.identifier`,
`db_master_secret_arn = aws_db_instance.main.master_user_secret[0].secret_arn`,
`media_bucket_arn`, `media_bucket_name`.

- [ ] **Step 3: Wire both into the root**

```hcl
data "cloudflare_ip_ranges" "current" {}

module "network" {
  source                = "../modules/network"
  name                  = local.name
  cloudflare_ipv4_cidrs = data.cloudflare_ip_ranges.current.ipv4_cidrs
}

module "data" {
  source               = "../modules/data"
  name                 = local.name
  private_subnet_ids   = module.network.private_subnet_ids
  db_security_group_id = module.network.db_security_group_id
  media_bucket_name    = var.media_bucket_name
}
```

- [ ] **Step 4: Plan, review, apply**

```sh
tofu -chdir=deploy/aws/prod plan -var-file=prod.tfvars -out=prod.tfplan
tofu -chdir=deploy/aws/prod apply prod.tfplan
```

Expected: the VPC, subnets, security groups, RDS instance and bucket exist. RDS
creation takes about ten minutes. `aws rds describe-db-instances` shows
`PubliclyAccessible: false` and `BackupRetentionPeriod: 30`.

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
- Task definition families: `aboutme-prod-app`, `-web`, `-migrate`,
  `-db-bootstrap`, `-db-provision`, `-db-set-login`, `-jobs`.
- Container names used by Task 13: `caddy`, `server`, `web`, `migrate`, `jobs`,
  `admin`.

Secret values are generated by `secrets.sh`, not by OpenTofu. Two keys must be
exactly 32 bytes in unpadded base64url, which the OpenTofu random provider does
not produce, and one mechanism for every value is simpler to review. OpenTofu
only references parameter names.

- [ ] **Step 1: Write `secrets.sh`**

```bash
#!/usr/bin/env bash
# Creates missing production secrets in SSM. Never overwrites and never prints
# a value.
set -euo pipefail
region=ap-southeast-1
prefix=/aboutme/prod

exists() {
  aws ssm get-parameters --region "$region" --names "$1" \
    --query 'length(Parameters)' --output text | grep -qx 1
}

put() { # name type value-command
  local name=$prefix/$1 type=$2
  if exists "$name"; then
    echo "kept $name"
    return
  fi
  "${@:3}" | jq -Rn --arg n "$name" --arg t "$type" \
    '{Name: $n, Type: $t, Value: input}' \
    | aws ssm put-parameter --region "$region" --cli-input-json file:///dev/stdin >/dev/null
  echo "created $name"
}

hex48() { openssl rand -hex 24; }
key32() { openssl rand 32 | basenc --base64url | tr -d '=\n'; echo; }
keyid() { echo k1; }

put db/migrator-password SecureString hex48
put db/app-password SecureString hex48
put auth-email/active-key-id String keyid
put auth-email/active-key SecureString key32
put password-rate-hmac-key SecureString key32
```

`get-parameters` without `--with-decryption` returns only metadata here; the
script never reads a value back.

- [ ] **Step 2: Write `tls.sh`**

```bash
#!/usr/bin/env bash
# Generates the origin key and CSR, and the origin-pull client certificate.
# Private keys go to SSM or Cloudflare and are then deleted locally.
set -euo pipefail
: "${CLOUDFLARE_API_TOKEN:?}" "${CLOUDFLARE_ZONE_ID:?}"
region=ap-southeast-1
root=$(git rev-parse --show-toplevel)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
umask 077

openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes \
  -subj /CN=aboutme.vn -keyout "$work/origin.key" \
  -out "$root/deploy/aws/prod/origin.csr" 2>/dev/null
jq -Rs '{Name: "/aboutme/prod/tls/origin-key", Type: "SecureString", Value: ., Overwrite: true}' \
  "$work/origin.key" | aws ssm put-parameter --region "$region" --cli-input-json file:///dev/stdin >/dev/null

openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 3650 \
  -subj /CN=aboutme-origin-pull-ca -keyout "$work/ca.key" -out "$work/ca.pem" 2>/dev/null
openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes \
  -subj /CN=aboutme-origin-pull -keyout "$work/pull.key" -out "$work/pull.csr" 2>/dev/null
openssl x509 -req -in "$work/pull.csr" -CA "$work/ca.pem" -CAkey "$work/ca.key" \
  -CAcreateserial -days 3650 -out "$work/pull.pem" 2>/dev/null

jq -n --rawfile c "$work/pull.pem" --rawfile k "$work/pull.key" '{certificate: $c, private_key: $k}' \
  | curl -fsS -o /dev/null -X POST \
      -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" -H 'Content-Type: application/json' \
      --data-binary @- \
      "https://api.cloudflare.com/client/v4/zones/$CLOUDFLARE_ZONE_ID/origin_tls_client_auth"
jq -Rs '{Name: "/aboutme/prod/tls/origin-pull-ca", Type: "String", Value: ., Overwrite: true}' \
  "$work/ca.pem" | aws ssm put-parameter --region "$region" --cli-input-json file:///dev/stdin >/dev/null
echo "origin CSR written; keys stored; CA key discarded"
```

The CSR is public and is committed. The CA key is deleted with the temporary
directory, so a new client certificate needs a new CA.

- [ ] **Step 3: Write the identity module**

Roles and policies:

```hcl
variable "name" { type = string }
variable "account_id" { type = string }
variable "media_bucket_arn" { type = string }
variable "db_master_secret_arn" { type = string }
variable "ses_from_address" { type = string }

locals {
  param = "arn:aws:ssm:ap-southeast-1:${var.account_id}:parameter/aboutme/prod"
  log   = "arn:aws:logs:ap-southeast-1:${var.account_id}:log-group:/aboutme/prod:*"
  ecs_trust = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Action    = "sts:AssumeRole"
      Principal = { Service = "ecs-tasks.amazonaws.com" }
      Condition = { StringEquals = { "aws:SourceAccount" = var.account_id } }
    }]
  })
  exec_params = {
    app      = ["db/app-password", "auth-email/active-key-id", "auth-email/active-key", "password-rate-hmac-key", "tls/origin-key", "tls/origin-cert", "tls/origin-pull-ca"]
    web      = []
    migrate  = ["db/migrator-password"]
    jobs     = ["db/app-password"]
    db-admin = ["db/migrator-password", "db/app-password"]
  }
}

resource "aws_cloudwatch_log_group" "main" {
  name              = "/aboutme/prod"
  retention_in_days = 180
}

resource "aws_iam_role" "instance" {
  name = "${var.name}-instance"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{ Effect = "Allow", Action = "sts:AssumeRole",
    Principal = { Service = "ec2.amazonaws.com" } }]
  })
}

resource "aws_iam_role_policy_attachment" "instance" {
  for_each = toset([
    "arn:aws:iam::aws:policy/service-role/AmazonEC2ContainerServiceforEC2Role",
    "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore",
  ])
  role       = aws_iam_role.instance.name
  policy_arn = each.value
}

resource "aws_iam_instance_profile" "instance" {
  name = "${var.name}-instance"
  role = aws_iam_role.instance.name
}

resource "aws_iam_role" "exec" {
  for_each           = local.exec_params
  name               = "${var.name}-${each.key}-exec"
  assume_role_policy = local.ecs_trust
}

resource "aws_iam_role_policy" "exec" {
  for_each = local.exec_params
  role     = aws_iam_role.exec[each.key].id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = concat(
      [{ Effect = "Allow", Action = ["logs:CreateLogStream", "logs:PutLogEvents"], Resource = local.log }],
      length(each.value) == 0 ? [] : [{
        Effect   = "Allow"
        Action   = ["ssm:GetParameters"]
        Resource = [for p in each.value : "${local.param}/${p}"]
      }],
      each.key != "db-admin" ? [] : [{
        Effect   = "Allow"
        Action   = ["secretsmanager:GetSecretValue"]
        Resource = var.db_master_secret_arn
      }],
    )
  })
}

resource "aws_iam_role" "app" {
  name               = "${var.name}-app-task"
  assume_role_policy = local.ecs_trust
}

resource "aws_iam_role" "jobs" {
  name               = "${var.name}-jobs-task"
  assume_role_policy = local.ecs_trust
}

locals {
  media_statements = [
    { Effect = "Allow", Action = ["s3:ListBucket"], Resource = var.media_bucket_arn,
    Condition = { StringLike = { "s3:prefix" = ["resumes/*"] } } },
    { Effect = "Allow", Action = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"],
    Resource = "${var.media_bucket_arn}/resumes/*" },
  ]
}

resource "aws_iam_role_policy" "app" {
  role = aws_iam_role.app.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = concat(local.media_statements, [{
      Effect    = "Allow"
      Action    = ["ses:SendEmail", "ses:SendRawEmail"]
      Resource  = "*"
      Condition = { StringEquals = { "ses:FromAddress" = var.ses_from_address } }
    }])
  })
}

resource "aws_iam_role_policy" "jobs" {
  role   = aws_iam_role.jobs.id
  policy = jsonencode({ Version = "2012-10-17", Statement = local.media_statements })
}
```

Outputs: `instance_profile_name`, `exec_role_arns` (map), `app_task_role_arn`,
`jobs_task_role_arn`, `log_group_name`. If a task later fails with a KMS error
reading a `SecureString`, add `kms:Decrypt` on the `aws/ssm` key to that
execution role.

- [ ] **Step 4: Write the tasks module**

The module takes the images, DB endpoint, bucket name, role ARNs, account ID and
Cloudflare ranges, and renders the container definitions below. In the code,
write each `ssm("x")` as `"${local.param}/x"`; HCL has no user functions. Every
container uses this log configuration, with its own name as the prefix:

```hcl
locals {
  param = "arn:aws:ssm:ap-southeast-1:${var.account_id}:parameter/aboutme/prod"
  logs = { for c in ["server", "caddy", "web", "migrate", "admin", "jobs"] : c => {
    logDriver = "awslogs"
    options = {
      awslogs-group         = "/aboutme/prod"
      awslogs-region        = "ap-southeast-1"
      awslogs-stream-prefix = c
    }
  } }
}
```

```hcl
locals {
  db_url = "postgres://%s@${var.db_endpoint}:5432/%s?sslmode=verify-full&sslrootcert=/etc/ssl/rds/global-bundle.pem"
  server_env = [
    { name = "ENV", value = "prod" },
    { name = "PORT", value = "8080" },
    { name = "LISTEN_HOST", value = "127.0.0.1" },
    { name = "TRUSTED_PROXY_CIDRS", value = "127.0.0.1/32" },
    { name = "PUBLIC_ORIGIN", value = "https://aboutme.vn" },
    { name = "PUBLIC_RENDER_ORIGIN", value = "http://127.0.0.1:3000" },
    { name = "PRINT_LISTEN_ADDR", value = "172.17.0.1:8081" },
    { name = "MCP_ENABLED", value = "true" },
    { name = "DATABASE_URL", value = format(local.db_url, "aboutme_app", "aboutme") },
    { name = "MEDIA_BACKEND", value = "s3" },
    { name = "MEDIA_BUCKET", value = var.media_bucket_name },
    { name = "MEDIA_REGION", value = "ap-southeast-1" },
    { name = "AUTH_EMAIL_MODE", value = "ses" },
    { name = "AWS_REGION", value = "ap-southeast-1" },
    { name = "SES_FROM_ADDRESS", value = var.ses_from_address },
    { name = "SES_CONFIGURATION_SET", value = var.ses_configuration_set },
    { name = "APP_BUILD_DIGEST", value = var.image_server },
    { name = "PUBLIC_RENDERER_BUILD_DIGEST", value = var.image_web },
  ]
}

resource "aws_ecs_task_definition" "app" {
  family             = "${var.name}-app"
  network_mode       = "host"
  execution_role_arn = var.exec_role_arns["app"]
  task_role_arn      = var.app_task_role_arn
  requires_compatibilities = ["EC2"]
  container_definitions = jsonencode([
    {
      name              = "server"
      image             = var.image_server
      memory            = 512
      cpu               = 512
      essential         = true
      environment       = local.server_env
      secrets = [
        { name = "PGPASSWORD", valueFrom = ssm("db/app-password") },
        { name = "AUTH_EMAIL_ACTIVE_KEY_ID", valueFrom = ssm("auth-email/active-key-id") },
        { name = "AUTH_EMAIL_ACTIVE_KEY", valueFrom = ssm("auth-email/active-key") },
        { name = "PASSWORD_RATE_HMAC_KEY", valueFrom = ssm("password-rate-hmac-key") },
      ]
      linuxParameters = { initProcessEnabled = true, sharedMemorySize = 128 }
      healthCheck = {
        command     = ["CMD", "wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8080/healthz"]
        interval    = 10
        timeout     = 3
        retries     = 3
        startPeriod = 20
      }
      logConfiguration = local.logs["server"]
    },
    {
      name      = "caddy"
      image     = var.image_caddy
      memory    = 128
      cpu       = 128
      essential = true
      portMappings = [{ containerPort = 443, hostPort = 443, protocol = "tcp" }]
      environment = [{ name = "CLOUDFLARE_RANGES", value = join(" ", var.cloudflare_ipv4_cidrs) }]
      secrets = [
        { name = "ORIGIN_KEY", valueFrom = ssm("tls/origin-key") },
        { name = "ORIGIN_CERT", valueFrom = ssm("tls/origin-cert") },
        { name = "ORIGIN_PULL_CA", valueFrom = ssm("tls/origin-pull-ca") },
      ]
      linuxParameters = { tmpfs = [{ containerPath = "/run/caddy", size = 1 }] }
      dependsOn        = [{ containerName = "server", condition = "HEALTHY" }]
      logConfiguration = local.logs["caddy"]
    },
  ])
}
```

`aws_ecs_task_definition.web`: `network_mode = "bridge"`, container `web`, image
`var.image_web`, memory 256, cpu 256, port mapping container 3000 to host 3000,
environment `HOST=0.0.0.0`, `PORT=3000`, `NUXT_PUBLIC_API_BASE=/api/v1`,
`NUXT_PRINT_ORIGIN=http://172.17.0.1:8081`, health check
`wget -q -O /dev/null http://127.0.0.1:3000/`, no task role.

`aws_ecs_task_definition.migrate`: bridge, container `migrate`, image
`var.image_server`, `entryPoint = ["/usr/local/bin/migrate"]`, memory 256,
environment `MIGRATION_IDENTITY=direct` and
`DATABASE_URL=format(local.db_url, "aboutme_migrator", "aboutme")`, secret
`PGPASSWORD` from `db/migrator-password`, no task role.

The three first-deploy families share execution role `db-admin` and no task
role. Container name `admin`, image `var.image_server`, memory 256, bridge. Each
reads `PGPASSWORD` from `"${var.db_master_secret_arn}:password::"`:

| Family          | `entryPoint`                              | Environment                                                                            | Extra secrets                                                                          |
| --------------- | ----------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `-db-bootstrap` | `["/usr/local/bin/db-role-bootstrap"]`    | `CLUSTER_BOOTSTRAP_DATABASE_URL=format(local.db_url, "aboutme", "postgres")`           | none                                                                                   |
| `-db-provision` | `["/usr/local/bin/migrate", "provision"]` | `DATABASE_URL=format(local.db_url, "aboutme", "aboutme")`, `MIGRATION_IDENTITY=direct` | none                                                                                   |
| `-db-set-login` | `["/usr/local/bin/db-set-login"]`         | `DATABASE_URL=format(local.db_url, "aboutme", "aboutme")`                              | `MIGRATOR_PASSWORD` from `db/migrator-password`, `APP_PASSWORD` from `db/app-password` |

`aws_ecs_task_definition.jobs`: bridge, container `jobs`, image
`var.image_server`, memory 256, `entryPoint = ["/usr/local/bin/server"]`, task
role `jobs`, environment `DATABASE_URL` for `aboutme_app`, `MEDIA_BACKEND=s3`,
`MEDIA_BUCKET`, `MEDIA_REGION=ap-southeast-1`, secret `PGPASSWORD` from
`db/app-password`. The scheduler supplies the command.

- [ ] **Step 5: Update the design paragraph**

In `docs/design/single-host-production.md`, replace the paragraph that says
OpenTofu generates passwords as ephemeral values with:
"`deploy/aws/scripts/secrets.sh` generates each value with `openssl`, writes it
straight to SSM, and never overwrites or prints one. OpenTofu references
parameter names only." Run `make docs-lint`.

- [ ] **Step 6: Run the scripts, wire the modules, apply**

```sh
bash deploy/aws/scripts/secrets.sh
CLOUDFLARE_ZONE_ID=<zone> bash deploy/aws/scripts/tls.sh
tofu -chdir=deploy/aws/prod plan -var-file=prod.tfvars -out=prod.tfplan
tofu -chdir=deploy/aws/prod apply prod.tfplan
```

Expected: five `created` lines, the TLS script's final line, then seven task
definition families and the roles.
`aws ssm get-parameters-by-path --path /aboutme/prod --recursive --query 'Parameters[].Name'`
lists the names only.

## Task 11

### Host, ECS services and Cloudflare edge

**Files:**

- Create: `deploy/aws/modules/host/{main,variables,outputs}.tf`
- Create: `deploy/aws/modules/edge/{main,variables,outputs}.tf`
- Modify: `deploy/aws/prod/main.tf`

**Interfaces:** Produces `module.host.instance_id`, `public_ip`, `cluster_name`,
service names `aboutme-prod-app` and `aboutme-prod-web`.

- [ ] **Step 1: Inventory existing DNS read-only**

```sh
curl -fsS -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
  "https://api.cloudflare.com/client/v4/zones/$CLOUDFLARE_ZONE_ID/dns_records?per_page=100" \
  | jq -r '.result[] | [.type, .name, .proxied] | @tsv'
```

Keep every MX, TXT and SES record. If an apex `A`/`AAAA` or a `www` record
already exists, import it with `tofu import` into the resources below instead of
creating a duplicate.

- [ ] **Step 2: Write the host module**

```hcl
data "aws_ssm_parameter" "bottlerocket" {
  name = "/aws/service/bottlerocket/aws-ecs-2/arm64/latest/image_id"
}

resource "aws_ecs_cluster" "main" {
  name = var.name
  setting {
    name  = "containerInsights"
    value = "disabled"
  }
}

resource "aws_instance" "host" {
  ami                     = data.aws_ssm_parameter.bottlerocket.value
  instance_type           = "t4g.small"
  subnet_id               = var.public_subnet_id
  vpc_security_group_ids  = [var.host_security_group_id]
  iam_instance_profile    = var.instance_profile_name
  disable_api_termination = true
  metadata_options {
    http_tokens                 = "required"
    http_put_response_hop_limit = 1
  }
  root_block_device {
    volume_type = "gp3"
    volume_size = 4
    encrypted   = true
  }
  ebs_block_device {
    device_name = "/dev/xvdb"
    volume_type = "gp3"
    volume_size = 20
    encrypted   = true
  }
  user_data = <<-EOT
    [settings.ecs]
    cluster = "${aws_ecs_cluster.main.name}"
    [settings.host-containers.admin]
    enabled = false
    [settings.updates]
    ignore-waves = false
  EOT
  lifecycle { ignore_changes = [ami, user_data] }
  tags = { Name = var.name }
}

resource "aws_eip" "host" { domain = "vpc" }

resource "aws_eip_association" "host" {
  instance_id   = aws_instance.host.id
  allocation_id = aws_eip.host.id
}

resource "aws_ecs_service" "app" {
  name                               = "${var.name}-app"
  cluster                            = aws_ecs_cluster.main.id
  task_definition                    = var.app_task_definition_arn
  desired_count                      = 1
  launch_type                        = "EC2"
  deployment_minimum_healthy_percent = 0
  deployment_maximum_percent         = 100
  lifecycle { ignore_changes = [task_definition, desired_count] }
}

resource "aws_ecs_service" "web" {
  name                               = "${var.name}-web"
  cluster                            = aws_ecs_cluster.main.id
  task_definition                    = var.web_task_definition_arn
  desired_count                      = 1
  launch_type                        = "EC2"
  deployment_minimum_healthy_percent = 0
  deployment_maximum_percent         = 100
  lifecycle { ignore_changes = [task_definition, desired_count] }
}
```

The AMI is ignored after creation; Bottlerocket updates itself in place (Task
12). The root volume holds the OS and the second volume holds container data.

- [ ] **Step 3: Write the edge module**

```hcl
resource "cloudflare_dns_record" "apex" {
  zone_id = var.zone_id
  name    = "aboutme.vn"
  type    = "A"
  content = var.origin_ip
  proxied = true
  ttl     = 1
}

resource "cloudflare_dns_record" "www" {
  zone_id = var.zone_id
  name    = "www.aboutme.vn"
  type    = "CNAME"
  content = "aboutme.vn"
  proxied = true
  ttl     = 1
}

resource "cloudflare_zone_setting" "this" {
  for_each = {
    ssl              = "strict"
    always_use_https = "on"
    min_tls_version  = "1.2"
    tls_1_3          = "on"
  }
  zone_id    = var.zone_id
  setting_id = each.key
  value      = each.value
}

resource "cloudflare_zone_setting" "hsts" {
  zone_id    = var.zone_id
  setting_id = "security_header"
  value = {
    strict_transport_security = {
      enabled            = true
      max_age            = 31536000
      include_subdomains = false
      preload            = false
      nosniff            = true
    }
  }
}

resource "cloudflare_bot_management" "this" {
  zone_id    = var.zone_id
  fight_mode = false
}

resource "cloudflare_authenticated_origin_pulls_settings" "this" {
  zone_id = var.zone_id
  enabled = true
}

resource "cloudflare_ruleset" "cache" {
  zone_id = var.zone_id
  name    = "aboutme cache policy"
  kind    = "zone"
  phase   = "http_request_cache_settings"
  rules = [{
    description = "Bypass cache except hashed Nuxt assets"
    expression  = "not starts_with(http.request.uri.path, \"/_nuxt/\")"
    action      = "set_cache_settings"
    action_parameters = { cache = false }
    enabled     = true
  }]
}

resource "cloudflare_origin_ca_certificate" "origin" {
  csr                = file("${path.root}/origin.csr")
  hostnames          = ["aboutme.vn", "www.aboutme.vn"]
  request_type       = "origin-ecc"
  requested_validity = 5475
}

resource "aws_ssm_parameter" "origin_cert" {
  name  = "/aboutme/prod/tls/origin-cert"
  type  = "String"
  value = cloudflare_origin_ca_certificate.origin.certificate
}
```

`include_subdomains` stays false so Google Workspace and other subdomains are
not forced onto HSTS by this change.

- [ ] **Step 4: Wire, plan, apply**

Wire both modules in `prod/main.tf`, passing
`origin_ip = module.host.public_ip`. Apply the edge module with the host in the
same plan.

Expected: `aws ecs list-container-instances --cluster aboutme-prod` shows one
instance, and both services report `runningCount 0` or a failing task until Task
15 runs the first deploy; that is expected before migrations exist.
`curl -sS https://aboutme.vn/healthz` returns a Cloudflare 5xx page at this
point, and `curl -m 5 https://<elastic ip>/` times out.

## Task 12

### Jobs, alarms and budget

**Files:**

- Create: `deploy/aws/modules/ops/{main,variables,outputs}.tf`
- Modify: `deploy/aws/prod/main.tf`

**Interfaces:** Produces SNS topic `aboutme-prod-alerts`, four schedules, the
Route 53 health check, alarms, the budget and the maintenance window.

- [ ] **Step 1: Write the schedules**

```hcl
locals {
  jobs = {
    idempotency-expiry-sweep = "cron(5 * * * ? *)"
    media-deletion-sweep     = "cron(15 * * * ? *)"
    privacy-retention-sweep  = "cron(30 2 * * ? *)"
    media-orphan-sweep       = "cron(45 3 ? * SUN *)"
  }
}

resource "aws_scheduler_schedule_group" "jobs" { name = "${var.name}-jobs" }

resource "aws_iam_role" "scheduler" {
  name = "${var.name}-scheduler"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Action    = "sts:AssumeRole"
      Principal = { Service = "scheduler.amazonaws.com" }
      Condition = { StringEquals = { "aws:SourceAccount" = var.account_id } }
    }]
  })
}

resource "aws_iam_role_policy" "scheduler" {
  role = aws_iam_role.scheduler.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      { Effect = "Allow", Action = "ecs:RunTask",
        Resource = [
          "arn:aws:ecs:ap-southeast-1:${var.account_id}:task-definition/${var.name}-jobs",
          "arn:aws:ecs:ap-southeast-1:${var.account_id}:task-definition/${var.name}-jobs:*",
      ] },
      { Effect = "Allow", Action = "iam:PassRole",
      Resource = [var.jobs_exec_role_arn, var.jobs_task_role_arn] },
    ]
  })
}

resource "aws_scheduler_schedule" "job" {
  for_each                     = local.jobs
  name                         = each.key
  group_name                   = aws_scheduler_schedule_group.jobs.name
  schedule_expression          = each.value
  schedule_expression_timezone = "UTC"
  flexible_time_window { mode = "OFF" }
  target {
    arn      = var.cluster_arn
    role_arn = aws_iam_role.scheduler.arn
    ecs_parameters {
      task_definition_arn = var.jobs_task_family_arn
      launch_type         = "EC2"
      task_count          = 1
    }
    input = jsonencode({
      containerOverrides = [{ name = "jobs", command = [each.key] }]
    })
    retry_policy { maximum_retry_attempts = 0 }
  }
}
```

`jobs_task_family_arn` is the task definition ARN without its revision, so ECS
runs the latest active revision that Task 13 registers. Task 16 confirms this
with the first real run; if Scheduler rejects an unversioned ARN, `deploy.sh`
updates each schedule's target to the new revision while it re-enables them.
`deploy.sh` disables the group's schedules during a deploy.

- [ ] **Step 2: Write alarms, health check, budget and updates**

```hcl
resource "aws_sns_topic" "alerts" { name = "${var.name}-alerts" }

resource "aws_sns_topic_subscription" "email" {
  topic_arn = aws_sns_topic.alerts.arn
  protocol  = "email"
  endpoint  = var.alarm_email
}

resource "aws_sns_topic_policy" "alerts" {
  arn = aws_sns_topic.alerts.arn
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Action    = "sns:Publish"
      Resource  = aws_sns_topic.alerts.arn
      Principal = { Service = ["events.amazonaws.com", "cloudwatch.amazonaws.com"] }
      Condition = { StringEquals = { "aws:SourceAccount" = var.account_id } }
    }]
  })
}

resource "aws_cloudwatch_metric_alarm" "recover" {
  alarm_name          = "${var.name}-host-recover"
  namespace           = "AWS/EC2"
  metric_name         = "StatusCheckFailed_System"
  dimensions          = { InstanceId = var.instance_id }
  statistic           = "Maximum"
  period              = 60
  evaluation_periods  = 2
  threshold           = 1
  comparison_operator = "GreaterThanOrEqualToThreshold"
  alarm_actions       = ["arn:aws:automate:ap-southeast-1:ec2:recover", aws_sns_topic.alerts.arn]
}
```

Add `aws_cloudwatch_metric_alarm` resources, each notifying the topic, for:

| Alarm                   | Namespace and metric                                | Condition                  |
| ----------------------- | --------------------------------------------------- | -------------------------- |
| `host-instance-check`   | `AWS/EC2 StatusCheckFailed_Instance`                | ≥ 1 for 3 × 60 s           |
| `app-cpu`, `app-memory` | `AWS/ECS CPUUtilization`, `MemoryUtilization` (app) | > 85 % for 3 × 300 s       |
| `db-cpu`                | `AWS/RDS CPUUtilization`                            | > 80 % for 3 × 300 s       |
| `db-credits`            | `AWS/RDS CPUCreditBalance`                          | < 20 for 2 × 300 s         |
| `db-storage`            | `AWS/RDS FreeStorageSpace`                          | < 2147483648 for 1 × 300 s |
| `db-connections`        | `AWS/RDS DatabaseConnections`                       | > 80 for 2 × 300 s         |
| `ses-bounce`            | `AWS/SES Reputation.BounceRate`                     | > 0.05 for 1 × 3600 s      |
| `ses-complaint`         | `AWS/SES Reputation.ComplaintRate`                  | > 0.001 for 1 × 3600 s     |
| `jobs-target-errors`    | `AWS/Scheduler TargetErrorCount` (group)            | ≥ 1 for 1 × 300 s          |

Add an EventBridge rule on `ECS Task State Change` with `lastStatus = STOPPED`
and `group` prefix `family:aboutme-prod-jobs` or `service:aboutme-prod-`,
targeting the topic, so every job exit and every service task stop sends mail.

Route 53 metrics exist only in `us-east-1`, so the health check alarm and its
own SNS topic use `provider = aws.us_east_1`:

```hcl
resource "aws_route53_health_check" "site" {
  fqdn              = "aboutme.vn"
  port              = 443
  type              = "HTTPS"
  resource_path     = "/healthz"
  request_interval  = 30
  failure_threshold = 3
  measure_latency   = false
}
```

Budget and anomaly detection:

```hcl
resource "aws_budgets_budget" "monthly" {
  name         = "${var.name}-monthly"
  budget_type  = "COST"
  limit_amount = "60"
  limit_unit   = "USD"
  time_unit    = "MONTHLY"
  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 80
    threshold_type             = "PERCENTAGE"
    notification_type          = "ACTUAL"
    subscriber_email_addresses = [var.alarm_email]
  }
  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "FORECASTED"
    subscriber_email_addresses = [var.alarm_email]
  }
}

resource "aws_ce_anomaly_monitor" "services" {
  name              = "${var.name}-services"
  monitor_type      = "DIMENSIONAL"
  monitor_dimension = "SERVICE"
}

resource "aws_ce_anomaly_subscription" "email" {
  name             = "${var.name}-anomalies"
  frequency        = "DAILY"
  monitor_arn_list = [aws_ce_anomaly_monitor.services.arn]
  subscriber {
    type    = "EMAIL"
    address = var.alarm_email
  }
  threshold_expression {
    dimension {
      key           = "ANOMALY_TOTAL_IMPACT_ABSOLUTE"
      values        = ["10"]
      match_options = ["GREATER_THAN_OR_EQUAL"]
    }
  }
}
```

Monthly OS updates:

```hcl
resource "aws_ssm_maintenance_window" "updates" {
  name              = "${var.name}-updates"
  schedule          = "cron(0 20 ? * SUN#1 *)"
  duration          = 2
  cutoff            = 1
  schedule_timezone = "UTC"
}

resource "aws_ssm_maintenance_window_target" "host" {
  window_id     = aws_ssm_maintenance_window.updates.id
  resource_type = "INSTANCE"
  targets {
    key    = "InstanceIds"
    values = [var.instance_id]
  }
}

resource "aws_ssm_maintenance_window_task" "update" {
  window_id       = aws_ssm_maintenance_window.updates.id
  task_type       = "RUN_COMMAND"
  task_arn        = "AWS-RunShellScript"
  max_concurrency = "1"
  max_errors      = "1"
  targets {
    key    = "WindowTargetIds"
    values = [aws_ssm_maintenance_window_target.host.id]
  }
  task_invocation_parameters {
    run_command_parameters {
      parameter {
        name   = "commands"
        values = ["apiclient update check", "apiclient update apply --reboot"]
      }
    }
  }
}
```

Sunday 20:00 UTC is Monday 03:00 in Vietnam.

- [ ] **Step 3: Wire, plan, apply, confirm the subscription**

Expected: the owner receives and confirms two SNS subscription emails (one per
region). `aws scheduler list-schedules --group-name aboutme-prod-jobs` lists
four schedules.
