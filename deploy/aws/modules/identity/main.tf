# Log group, instance role, per-task execution roles and task roles.

locals {
  param = "arn:aws:ssm:ap-southeast-1:${var.account_id}:parameter/aboutme/prod"
  ecs_trust = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Action    = "sts:AssumeRole"
      Principal = { Service = "ecs-tasks.amazonaws.com" }
      Condition = { StringEquals = { "aws:SourceAccount" = var.account_id } }
    }]
  })
  # Parameters each execution role may read, and nothing else. maintenance
  # runs only the caddy container, so its role needs exactly the origin TLS
  # parameters: it cannot reuse the app role, which also needs the server
  # container's secrets.
  exec_params = {
    app         = ["db/app-password", "auth-email/active-key-id", "auth-email/active-key", "password-rate-hmac-key", "tls/origin-key", "tls/origin-cert", "tls/origin-pull-ca", "oauth/google-client-id", "oauth/google-client-secret", "totp/key-a", "totp/key-b"]
    web         = []
    maintenance = ["tls/origin-key", "tls/origin-cert", "tls/origin-pull-ca"]
    migrate     = ["db/migrator-password"]
    jobs        = ["db/app-password"]
    db-admin    = ["db/migrator-password", "db/app-password"]
  }
  media_statements = [
    # HeadObject reports a missing key as 404 only with ListBucket. The
    # bucket holds only resumes/ objects, so no prefix condition is needed.
    {
      Effect   = "Allow"
      Action   = ["s3:ListBucket"]
      Resource = var.media_bucket_arn
    },
    {
      Effect   = "Allow"
      Action   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"]
      Resource = "${var.media_bucket_arn}/resumes/*"
    },
  ]
}

resource "aws_cloudwatch_log_group" "main" {
  name              = "/aboutme/prod"
  retention_in_days = 180
}

resource "aws_iam_role" "instance" {
  name = "${var.name}-instance"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Action    = "sts:AssumeRole"
      Principal = { Service = "ec2.amazonaws.com" }
    }]
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

# AmazonSSMManagedInstanceCore allows ssm:GetParameter* on every parameter,
# and host-network containers can reach the instance role. This keeps the
# instance role away from production secrets; only task execution roles read
# them.
resource "aws_iam_role_policy" "instance_deny_secrets" {
  name = "deny-production-secrets"
  role = aws_iam_role.instance.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Deny"
        Action   = ["ssm:GetParameter", "ssm:GetParameters", "ssm:GetParametersByPath", "ssm:GetParameterHistory"]
        Resource = ["${local.param}/*", "${local.param}"]
      },
      {
        Effect   = "Deny"
        Action   = ["secretsmanager:GetSecretValue"]
        Resource = var.db_master_secret_arn
      },
    ]
  })
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
  name     = "task-start"
  role     = aws_iam_role.exec[each.key].id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = concat(
      [{
        Effect   = "Allow"
        Action   = ["logs:CreateLogStream", "logs:PutLogEvents"]
        Resource = "${aws_cloudwatch_log_group.main.arn}:*"
      }],
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

resource "aws_iam_role_policy" "app" {
  name = "media-and-mail"
  role = aws_iam_role.app.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = concat(local.media_statements, [{
      Effect    = "Allow"
      Action    = ["ses:SendEmail"]
      Resource  = "*"
      Condition = { StringEquals = { "ses:FromAddress" = var.ses_from_address } }
    }])
  })
}

resource "aws_iam_role" "jobs" {
  name               = "${var.name}-jobs-task"
  assume_role_policy = local.ecs_trust
}

resource "aws_iam_role_policy" "jobs" {
  name   = "media"
  role   = aws_iam_role.jobs.id
  policy = jsonencode({ Version = "2012-10-17", Statement = local.media_statements })
}

# The release-snapshot-sweep job lists the instance's snapshots and deletes
# only the ones deploy.sh tagged, plus the one release snapshot taken before
# tagging began. It cannot delete automated backups or any other snapshot.
resource "aws_iam_role_policy" "jobs_release_snapshots" {
  name = "release-snapshots"
  role = aws_iam_role.jobs.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = "rds:DescribeDBSnapshots"
        Resource = [
          "arn:aws:rds:ap-southeast-1:${var.account_id}:db:${var.name}",
          "arn:aws:rds:ap-southeast-1:${var.account_id}:snapshot:*",
        ]
      },
      {
        Effect    = "Allow"
        Action    = "rds:DeleteDBSnapshot"
        Resource  = "arn:aws:rds:ap-southeast-1:${var.account_id}:snapshot:${var.name}-v*"
        Condition = { StringEquals = { "aws:ResourceTag/aboutme:created-by" = "deploy.sh" } }
      },
      {
        Effect   = "Allow"
        Action   = "rds:DeleteDBSnapshot"
        Resource = "arn:aws:rds:ap-southeast-1:${var.account_id}:snapshot:${var.name}-v0-1-1-202609171438"
      },
    ]
  })
}

# ---- Minimum-release fence: operator and deploy roles ----
#
# docs/design/passkey-release-fence.md fixes this boundary. The AWS-login
# principal (operator_principal_arn) assumes aboutme-prod-operator, which may
# only describe and strongly read the fence and assume aboutme-prod-deploy.
# Only aboutme-prod-deploy may write the fence or perform the bounded
# deployment mutations. App, web, jobs, maintenance, migration, execution,
# host and scheduler roles are untouched here and so keep no fence access.
locals {
  cluster_arn = "arn:aws:ecs:ap-southeast-1:${var.account_id}:cluster/${var.name}"
  service_arns = [
    for svc in ["app", "web", "maintenance"] :
    "arn:aws:ecs:ap-southeast-1:${var.account_id}:service/${var.name}/${var.name}-${svc}"
  ]
  # totp-reencrypt uses the same execution and task roles as app, so it adds
  # no pass-role scope; see docs/design/passkey-release-fence.md,
  # "Authenticator-app key re-encryption".
  run_task_family_arns = [
    for fam in ["migrate", "db-setup", "jobs", "totp-reencrypt"] :
    "arn:aws:ecs:ap-southeast-1:${var.account_id}:task-definition/${var.name}-${fam}:*"
  ]
  deploy_pass_role_arns = concat(
    [for k, r in aws_iam_role.exec : r.arn],
    [aws_iam_role.app.arn, aws_iam_role.jobs.arn],
  )
  schedule_arns         = "arn:aws:scheduler:ap-southeast-1:${var.account_id}:schedule/${var.name}-jobs/*"
  site_alarm_arn        = "arn:aws:cloudwatch:us-east-1:${var.account_id}:alarm:${var.name}-site-down"
  task_stopped_rule_arn = "arn:aws:events:ap-southeast-1:${var.account_id}:rule/${var.name}-task-stopped"
  release_snapshot_arns = "arn:aws:rds:ap-southeast-1:${var.account_id}:snapshot:${var.name}-v*"
  db_arn                = "arn:aws:rds:ap-southeast-1:${var.account_id}:db:${var.name}"
  # aws_scheduler_schedule.job's target role, in the sibling ops module; hand
  # built the same way every other cross-resource ARN in this file is, since
  # ops exports no output for it.
  scheduler_role_arn = "arn:aws:iam::${var.account_id}:role/${var.name}-scheduler"
  # This partition key is the fence table's only item; LeadingKeys below pins
  # every item-level fence action to it.
  fence_leading_key = { "ForAllValues:StringEquals" = { "dynamodb:LeadingKeys" = ["application"] } }

  # The explicit deny the release-fence contract requires on the operator
  # role: it denies every action except the fence read and the assumption of
  # the deploy role, so a future broadening of the operator role's
  # permissions still cannot bypass the deploy role.
  fence_mutation_deny = [
    {
      Effect    = "Deny"
      NotAction = ["dynamodb:DescribeTable", "dynamodb:GetItem", "sts:AssumeRole"]
      Resource  = "*"
    },
  ]
}

resource "aws_iam_role" "operator" {
  name = "${var.name}-operator"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Action    = "sts:AssumeRole"
      Principal = { AWS = var.operator_principal_arn }
      Condition = { StringEquals = { "aws:PrincipalAccount" = var.account_id } }
    }]
  })
}

resource "aws_iam_role_policy" "operator_fence_read_and_assume" {
  name = "fence-read-and-assume-deploy"
  role = aws_iam_role.operator.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = "dynamodb:DescribeTable"
        Resource = var.release_fence_table_arn
      },
      {
        Effect    = "Allow"
        Action    = "dynamodb:GetItem"
        Resource  = var.release_fence_table_arn
        Condition = local.fence_leading_key
      },
      {
        Effect   = "Allow"
        Action   = "sts:AssumeRole"
        Resource = aws_iam_role.deploy.arn
      },
    ]
  })
}

resource "aws_iam_role_policy" "operator_deny_direct_mutation" {
  name   = "deny-direct-deployment-mutations"
  role   = aws_iam_role.operator.id
  policy = jsonencode({ Version = "2012-10-17", Statement = local.fence_mutation_deny })
}

resource "aws_iam_role" "deploy" {
  name = "${var.name}-deploy"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Action    = "sts:AssumeRole"
      Principal = { AWS = aws_iam_role.operator.arn }
      Condition = { StringEquals = { "aws:PrincipalAccount" = var.account_id } }
    }]
  })
}

resource "aws_iam_role_policy" "deploy_fence" {
  name = "fence-lock-and-raise"
  role = aws_iam_role.deploy.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = "dynamodb:DescribeTable"
        Resource = var.release_fence_table_arn
      },
      {
        Effect    = "Allow"
        Action    = ["dynamodb:GetItem", "dynamodb:UpdateItem"]
        Resource  = var.release_fence_table_arn
        Condition = local.fence_leading_key
      },
    ]
  })
}

resource "aws_iam_role_policy" "deploy_ecs" {
  name = "deployment-ecs"
  role = aws_iam_role.deploy.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        # ECS does not support resource-level permissions on these read
        # actions; they return no secret material.
        Effect   = "Allow"
        Action   = ["ecs:DescribeClusters", "ecs:DescribeServices", "ecs:DescribeTasks", "ecs:ListTasks", "ecs:DescribeTaskDefinition"]
        Resource = "*"
      },
      {
        # RegisterTaskDefinition has no resource-level permission either;
        # iam:PassRole below is the actual boundary on what it can start.
        Effect   = "Allow"
        Action   = "ecs:RegisterTaskDefinition"
        Resource = "*"
      },
      {
        Effect   = "Allow"
        Action   = "ecs:UpdateService"
        Resource = local.service_arns
      },
      {
        Effect   = "Allow"
        Action   = "ecs:RunTask"
        Resource = local.run_task_family_arns
        Condition = {
          ArnEquals = { "ecs:cluster" = local.cluster_arn }
        }
      },
      {
        Effect    = "Allow"
        Action    = "iam:PassRole"
        Resource  = local.deploy_pass_role_arns
        Condition = { StringEquals = { "iam:PassedToService" = "ecs-tasks.amazonaws.com" } }
      },
    ]
  })
}

resource "aws_iam_role_policy" "deploy_scheduler" {
  name = "deployment-schedules"
  role = aws_iam_role.deploy.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["scheduler:GetSchedule", "scheduler:UpdateSchedule"]
        Resource = local.schedule_arns
      },
      {
        # ListSchedules supports no resource-level scoping.
        Effect   = "Allow"
        Action   = "scheduler:ListSchedules"
        Resource = "*"
      },
      {
        # UpdateSchedule passes the target role to Scheduler; scope that pass
        # to exactly the role aws_scheduler_schedule.job already targets.
        Effect    = "Allow"
        Action    = "iam:PassRole"
        Resource  = local.scheduler_role_arn
        Condition = { StringEquals = { "iam:PassedToService" = "scheduler.amazonaws.com" } }
      },
    ]
  })
}

resource "aws_iam_role_policy" "deploy_snapshots" {
  name = "deployment-snapshots"
  role = aws_iam_role.deploy.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = "rds:DescribeDBSnapshots"
        Resource = [local.db_arn, "arn:aws:rds:ap-southeast-1:${var.account_id}:snapshot:*"]
      },
      {
        Effect   = "Allow"
        Action   = "rds:CreateDBSnapshot"
        Resource = [local.db_arn, local.release_snapshot_arns]
      },
      {
        Effect   = "Allow"
        Action   = "rds:AddTagsToResource"
        Resource = local.release_snapshot_arns
      },
    ]
  })
}

resource "aws_iam_role_policy" "deploy_secret_existence" {
  name = "deployment-secret-existence"
  role = aws_iam_role.deploy.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        # DescribeParameters returns metadata only, never a parameter value,
        # and AWS does not support scoping it to a resource ARN.
        Effect   = "Allow"
        Action   = "ssm:DescribeParameters"
        Resource = "*"
      },
      {
        Effect   = "Allow"
        Action   = "secretsmanager:DescribeSecret"
        Resource = var.db_master_secret_arn
      },
    ]
  })
}

resource "aws_iam_role_policy" "deploy_alarm_and_rule_suppression" {
  name = "deployment-alarm-and-rule-suppression"
  role = aws_iam_role.deploy.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        # DescribeAlarms lists and filters across alarms by name; AWS grants
        # it no resource-level scoping, unlike the two actions below.
        Effect   = "Allow"
        Action   = "cloudwatch:DescribeAlarms"
        Resource = "*"
      },
      {
        Effect   = "Allow"
        Action   = ["cloudwatch:DisableAlarmActions", "cloudwatch:EnableAlarmActions"]
        Resource = local.site_alarm_arn
      },
      {
        Effect   = "Allow"
        Action   = ["events:DescribeRule", "events:DisableRule", "events:EnableRule"]
        Resource = local.task_stopped_rule_arn
      },
    ]
  })
}
