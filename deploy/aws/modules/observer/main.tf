# The deployment observer on AWS (docs/design/deployment-transparency/
# README.md; ADR 0057): a Lambda function outside the host that reads which
# image digests the production cluster runs, verifies them, and writes
# deployment.json to a private bucket that only CloudFront reads. The ECR
# repository holds a byte-for-byte copy of the attested GHCR image
# (observer.sh). Until var.image_digest is set, only the repository, the
# bucket, and the roles exist.

locals {
  function_name = "${var.name}-observer"
  function_arn  = "arn:aws:lambda:ap-southeast-1:${var.account_id}:function:${local.function_name}"
  cluster_arn   = "arn:aws:ecs:ap-southeast-1:${var.account_id}:cluster/${var.name}"
  log_group     = "/aws/lambda/${local.function_name}"
  # CloudFront appends the viewer path to the S3 origin, so the object key
  # is the public path without its leading slash.
  document_key = ".well-known/deployment.json"
  enabled      = var.image_digest != ""
  cluster_condition = {
    ArnEquals = { "ecs:cluster" = local.cluster_arn }
  }
}

# ---- Image repository ----

resource "aws_ecr_repository" "observer" {
  name                 = "${var.name}-observer"
  image_tag_mutability = "IMMUTABLE"

  encryption_configuration {
    encryption_type = "AES256"
  }
}

# Lambda pulls with its service principal, scoped to this one function; the
# function's own role holds no ECR permission.
resource "aws_ecr_repository_policy" "observer" {
  repository = aws_ecr_repository.observer.name
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid       = "LambdaPull"
      Effect    = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
      Action    = ["ecr:BatchGetImage", "ecr:GetDownloadUrlForLayer"]
      Condition = {
        StringEquals = { "aws:SourceAccount" = var.account_id }
        ArnLike      = { "aws:SourceArn" = local.function_arn }
      }
    }]
  })
}

# Every copy is tagged with its release and kept, so a rollback target never
# expires; only an untagged leftover from an interrupted copy is removed.
resource "aws_ecr_lifecycle_policy" "observer" {
  repository = aws_ecr_repository.observer.name
  policy = jsonencode({
    rules = [{
      rulePriority = 1
      description  = "Expire untagged images after a day"
      selection    = { tagStatus = "untagged", countType = "sinceImagePushed", countUnit = "days", countNumber = 1 }
      action       = { type = "expire" }
    }]
  })
}

# ---- Transparency bucket ----

resource "aws_s3_bucket" "transparency" {
  bucket = var.bucket_name
}

resource "aws_s3_bucket_public_access_block" "transparency" {
  bucket                  = aws_s3_bucket.transparency.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_ownership_controls" "transparency" {
  bucket = aws_s3_bucket.transparency.id
  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "transparency" {
  bucket = aws_s3_bucket.transparency.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# Versioning stays off: the document is a snapshot and keeps no history.
resource "aws_s3_bucket_policy" "transparency" {
  bucket = aws_s3_bucket.transparency.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "CloudFrontReadsTheDocument"
        Effect    = "Allow"
        Principal = { Service = "cloudfront.amazonaws.com" }
        Action    = "s3:GetObject"
        Resource  = "${aws_s3_bucket.transparency.arn}/${local.document_key}"
        Condition = { StringEquals = { "AWS:SourceArn" = var.distribution_arn } }
      },
      {
        Sid       = "OnlyTheObserverWrites"
        Effect    = "Deny"
        Principal = "*"
        Action    = ["s3:PutObject", "s3:DeleteObject", "s3:ReplicateObject", "s3:ReplicateDelete"]
        Resource  = ["${aws_s3_bucket.transparency.arn}/${local.document_key}", "${aws_s3_bucket.transparency.arn}/verified/*"]
        Condition = { ArnNotEquals = { "aws:PrincipalArn" = aws_iam_role.observer.arn } }
      },
      {
        Sid       = "DenyInsecureTransport"
        Effect    = "Deny"
        Principal = "*"
        Action    = "s3:*"
        Resource  = [aws_s3_bucket.transparency.arn, "${aws_s3_bucket.transparency.arn}/*"]
        Condition = { Bool = { "aws:SecureTransport" = "false" } }
      },
    ]
  })
  depends_on = [aws_s3_bucket_public_access_block.transparency]
}

# ---- Observer role ----

resource "aws_iam_role" "observer" {
  name = local.function_name
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Action    = "sts:AssumeRole"
      Principal = { Service = "lambda.amazonaws.com" }
    }]
  })
}

# Exactly the design's policy: two ECS reads on the production cluster, one
# document write, the verification cache, and its own logs.
resource "aws_iam_role_policy" "observer" {
  name = "observe-and-publish"
  role = aws_iam_role.observer.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "ListServingTasks"
        Effect    = "Allow"
        Action    = "ecs:ListTasks"
        Resource  = "*"
        Condition = local.cluster_condition
      },
      {
        Sid       = "DescribeServingTasks"
        Effect    = "Allow"
        Action    = "ecs:DescribeTasks"
        Resource  = "arn:aws:ecs:ap-southeast-1:${var.account_id}:task/${var.name}/*"
        Condition = local.cluster_condition
      },
      {
        Sid      = "PublishDocument"
        Effect   = "Allow"
        Action   = "s3:PutObject"
        Resource = "${aws_s3_bucket.transparency.arn}/${local.document_key}"
      },
      {
        Sid      = "VerificationCache"
        Effect   = "Allow"
        Action   = ["s3:GetObject", "s3:PutObject"]
        Resource = "${aws_s3_bucket.transparency.arn}/verified/*"
      },
      {
        Sid      = "Logs"
        Effect   = "Allow"
        Action   = ["logs:CreateLogStream", "logs:PutLogEvents"]
        Resource = "arn:aws:logs:ap-southeast-1:${var.account_id}:log-group:${local.log_group}:*"
      },
    ]
  })
}

resource "aws_cloudwatch_log_group" "observer" {
  name              = local.log_group
  retention_in_days = 30
}

# ---- Function, schedule, alarm ----

resource "aws_lambda_function" "observer" {
  count         = local.enabled ? 1 : 0
  function_name = local.function_name
  role          = aws_iam_role.observer.arn
  package_type  = "Image"
  image_uri     = "${aws_ecr_repository.observer.repository_url}@${var.image_digest}"
  architectures = ["arm64"]
  memory_size   = 128
  timeout       = 30
  # A run ends within 30 s, starts every 60 s, and is never retried (the
  # event invoke config below), so runs do not overlap. Reserved concurrency
  # needs an account quota above the minimum unreserved pool; see
  # var.reserved_concurrency.
  reserved_concurrent_executions = var.reserved_concurrency

  # Names only, none of them secret, and none reaches the document.
  environment {
    variables = {
      OBSERVER_PLATFORM            = "ecs"
      OBSERVER_CLUSTER             = var.name
      OBSERVER_APP_SERVICE         = "${var.name}-app"
      OBSERVER_WEB_SERVICE         = "${var.name}-web"
      OBSERVER_MAINTENANCE_SERVICE = "${var.name}-maintenance"
      OBSERVER_BUCKET              = aws_s3_bucket.transparency.id
    }
  }

  logging_config {
    log_format = "Text"
    log_group  = aws_cloudwatch_log_group.observer.name
  }

  # observer.sh points the function at each verified copy by digest, as
  # deploy.sh does for task definitions.
  lifecycle {
    ignore_changes = [image_uri]
  }

  depends_on = [aws_iam_role_policy.observer, aws_ecr_repository_policy.observer]
}

# Scheduler invokes asynchronously; Lambda would otherwise retry a failed run
# twice and keep its event for hours. The next scheduled run replaces it.
resource "aws_lambda_function_event_invoke_config" "observer" {
  count                        = local.enabled ? 1 : 0
  function_name                = aws_lambda_function.observer[0].function_name
  maximum_retry_attempts       = 0
  maximum_event_age_in_seconds = 60
}

resource "aws_iam_role" "scheduler" {
  name = "${var.name}-observer-scheduler"
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
  name = "invoke-observer"
  role = aws_iam_role.scheduler.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = "lambda:InvokeFunction"
      Resource = local.function_arn
    }]
  })
}

resource "aws_scheduler_schedule" "observer" {
  count                        = local.enabled ? 1 : 0
  name                         = local.function_name
  schedule_expression          = "rate(1 minute)"
  schedule_expression_timezone = "UTC"

  flexible_time_window {
    mode = "OFF"
  }

  target {
    arn      = aws_lambda_function.observer[0].arn
    role_arn = aws_iam_role.scheduler.arn

    # A missed run is replaced by the next one a minute later.
    retry_policy {
      maximum_retry_attempts = 0
    }
  }
}

# A failed run writes nothing, so the last document ages into stale; this
# mails the owner (five or more errors in ten minutes).
resource "aws_cloudwatch_metric_alarm" "errors" {
  count               = local.enabled ? 1 : 0
  alarm_name          = "${local.function_name}-errors"
  namespace           = "AWS/Lambda"
  metric_name         = "Errors"
  statistic           = "Sum"
  dimensions          = { FunctionName = local.function_name }
  comparison_operator = "GreaterThanOrEqualToThreshold"
  threshold           = 5
  period              = 600
  evaluation_periods  = 1
  treat_missing_data  = "notBreaching"
  alarm_actions       = [var.alerts_topic_arn]
}

# No runs at all (a disabled schedule, a broken scheduler role) leave no
# errors to count, so a separate alarm fires when invocations stop.
resource "aws_cloudwatch_metric_alarm" "stopped" {
  count               = local.enabled ? 1 : 0
  alarm_name          = "${local.function_name}-stopped"
  namespace           = "AWS/Lambda"
  metric_name         = "Invocations"
  statistic           = "Sum"
  dimensions          = { FunctionName = local.function_name }
  comparison_operator = "LessThanThreshold"
  threshold           = 1
  period              = 600
  evaluation_periods  = 1
  treat_missing_data  = "breaching"
  alarm_actions       = [var.alerts_topic_arn]
}
