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
  # Parameters each execution role may read, and nothing else.
  exec_params = {
    app      = ["db/app-password", "auth-email/active-key-id", "auth-email/active-key", "password-rate-hmac-key", "tls/origin-key", "tls/origin-cert", "tls/origin-pull-ca"]
    web      = []
    migrate  = ["db/migrator-password"]
    jobs     = ["db/app-password"]
    db-admin = ["db/migrator-password", "db/app-password"]
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
      Action    = ["ses:SendEmail", "ses:SendRawEmail"]
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
