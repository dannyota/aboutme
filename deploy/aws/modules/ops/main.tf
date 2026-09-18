# Scheduled privacy jobs, alarms, the site health check and monthly OS
# updates. Budgets and anomaly monitoring are account-level and live outside
# this repository.

terraform {
  required_providers {
    aws = {
      source                = "hashicorp/aws"
      configuration_aliases = [aws.us_east_1]
    }
  }
}

locals {
  # Times are UTC. Vietnam is UTC+7.
  jobs = {
    idempotency-expiry-sweep = "cron(5 * * * ? *)"
    media-deletion-sweep     = "cron(15 * * * ? *)"
    privacy-retention-sweep  = "cron(30 19 * * ? *)"
    media-orphan-sweep       = "cron(45 18 ? * SUN *)"
    release-snapshot-sweep   = "cron(0 20 * * ? *)"
  }
}

# ---- Scheduled jobs ----

resource "aws_scheduler_schedule_group" "jobs" {
  name = "${var.name}-jobs"
}

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
  name = "run-jobs"
  role = aws_iam_role.scheduler.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = "ecs:RunTask"
        Resource = [var.jobs_task_family_arn, "${var.jobs_task_family_arn}:*"]
      },
      {
        Effect   = "Allow"
        Action   = "iam:PassRole"
        Resource = [var.jobs_exec_role_arn, var.jobs_task_role_arn]
      },
    ]
  })
}

# Schedules start disabled; deploy.sh enables them once a real image runs,
# disables them around each deploy, and points them at the released revision.
resource "aws_scheduler_schedule" "job" {
  for_each                     = local.jobs
  name                         = each.key
  group_name                   = aws_scheduler_schedule_group.jobs.name
  schedule_expression          = each.value
  schedule_expression_timezone = "UTC"
  state                        = "DISABLED"

  flexible_time_window {
    mode = "OFF"
  }

  target {
    arn      = var.cluster_arn
    role_arn = aws_iam_role.scheduler.arn
    input = jsonencode({
      containerOverrides = [{ name = "jobs", command = [each.key] }]
    })

    ecs_parameters {
      task_definition_arn = var.jobs_task_family_arn
      launch_type         = "EC2"
      task_count          = 1
    }

    # Scheduler retries only a failed task start, not a job that runs and
    # fails. release-snapshot-sweep retries so a start failure does not cost
    # a day of its 30-day margin.
    retry_policy {
      maximum_retry_attempts = each.key == "release-snapshot-sweep" ? 2 : 0
    }
  }

  # deploy.sh owns the state and pins the target to the released revision.
  lifecycle {
    ignore_changes = [state, target[0].ecs_parameters[0].task_definition_arn]
  }
}

# ---- Notifications ----

resource "aws_sns_topic" "alerts" {
  name = "${var.name}-alerts"
}

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

# Every stopped app, web or job task sends mail: a crash, a failed job, or a
# deploy. The event carries the stop reason and exit codes.
resource "aws_cloudwatch_event_rule" "task_stopped" {
  name        = "${var.name}-task-stopped"
  description = "aboutme-prod ECS task stopped"
  event_pattern = jsonencode({
    source      = ["aws.ecs"]
    detail-type = ["ECS Task State Change"]
    detail = {
      clusterArn = [var.cluster_arn]
      lastStatus = ["STOPPED"]
      group      = [{ prefix = "service:${var.name}-" }, "family:${var.name}-jobs"]
    }
  })
}

resource "aws_cloudwatch_event_target" "task_stopped" {
  rule = aws_cloudwatch_event_rule.task_stopped.name
  arn  = aws_sns_topic.alerts.arn
}

# ---- Alarms ----

locals {
  alarms = {
    host-instance-check = {
      namespace = "AWS/EC2", metric = "StatusCheckFailed_Instance", stat = "Maximum"
      dims      = { InstanceId = var.instance_id }
      op        = "GreaterThanOrEqualToThreshold", threshold = 1, period = 60, periods = 3
    }
    app-cpu = {
      namespace = "AWS/ECS", metric = "CPUUtilization", stat = "Average"
      dims      = { ClusterName = var.cluster_name, ServiceName = "${var.name}-app" }
      op        = "GreaterThanThreshold", threshold = 85, period = 300, periods = 3
    }
    app-memory = {
      namespace = "AWS/ECS", metric = "MemoryUtilization", stat = "Average"
      dims      = { ClusterName = var.cluster_name, ServiceName = "${var.name}-app" }
      op        = "GreaterThanThreshold", threshold = 85, period = 300, periods = 3
    }
    db-cpu = {
      namespace = "AWS/RDS", metric = "CPUUtilization", stat = "Average"
      dims      = { DBInstanceIdentifier = var.db_instance_id }
      op        = "GreaterThanThreshold", threshold = 80, period = 300, periods = 3
    }
    db-credits = {
      namespace = "AWS/RDS", metric = "CPUCreditBalance", stat = "Minimum"
      dims      = { DBInstanceIdentifier = var.db_instance_id }
      op        = "LessThanThreshold", threshold = 20, period = 300, periods = 2
    }
    db-storage = {
      namespace = "AWS/RDS", metric = "FreeStorageSpace", stat = "Minimum"
      dims      = { DBInstanceIdentifier = var.db_instance_id }
      op        = "LessThanThreshold", threshold = 2147483648, period = 300, periods = 1
    }
    db-connections = {
      namespace = "AWS/RDS", metric = "DatabaseConnections", stat = "Maximum"
      dims      = { DBInstanceIdentifier = var.db_instance_id }
      op        = "GreaterThanThreshold", threshold = 60, period = 300, periods = 2
    }
    jobs-target-errors = {
      namespace = "AWS/Scheduler", metric = "TargetErrorCount", stat = "Sum"
      dims      = { ScheduleGroup = aws_scheduler_schedule_group.jobs.name }
      op        = "GreaterThanOrEqualToThreshold", threshold = 1, period = 300, periods = 1
    }
  }
}

resource "aws_cloudwatch_metric_alarm" "alarm" {
  for_each            = local.alarms
  alarm_name          = "${var.name}-${each.key}"
  namespace           = each.value.namespace
  metric_name         = each.value.metric
  statistic           = each.value.stat
  dimensions          = each.value.dims
  comparison_operator = each.value.op
  threshold           = each.value.threshold
  period              = each.value.period
  evaluation_periods  = each.value.periods
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_sns_topic.alerts.arn]
}

# A failed EC2 system check moves the host to healthy hardware.
resource "aws_cloudwatch_metric_alarm" "host_recover" {
  alarm_name          = "${var.name}-host-recover"
  namespace           = "AWS/EC2"
  metric_name         = "StatusCheckFailed_System"
  statistic           = "Maximum"
  dimensions          = { InstanceId = var.instance_id }
  comparison_operator = "GreaterThanOrEqualToThreshold"
  threshold           = 1
  period              = 60
  evaluation_periods  = 2
  alarm_actions       = ["arn:aws:automate:ap-southeast-1:ec2:recover", aws_sns_topic.alerts.arn]
}

# ---- Site health (Route 53 metrics exist only in us-east-1) ----

resource "aws_route53_health_check" "site" {
  fqdn              = "aboutme.vn"
  port              = 443
  type              = "HTTPS"
  resource_path     = "/healthz"
  request_interval  = 30
  failure_threshold = 3
  tags              = { Name = "${var.name}-site" }
}

resource "aws_sns_topic" "alerts_us_east_1" {
  provider = aws.us_east_1
  name     = "${var.name}-alerts"
}

resource "aws_sns_topic_subscription" "email_us_east_1" {
  provider  = aws.us_east_1
  topic_arn = aws_sns_topic.alerts_us_east_1.arn
  protocol  = "email"
  endpoint  = var.alarm_email
}

resource "aws_cloudwatch_metric_alarm" "site" {
  provider            = aws.us_east_1
  alarm_name          = "${var.name}-site-down"
  namespace           = "AWS/Route53"
  metric_name         = "HealthCheckStatus"
  statistic           = "Minimum"
  dimensions          = { HealthCheckId = aws_route53_health_check.site.id }
  comparison_operator = "LessThanThreshold"
  threshold           = 1
  period              = 60
  evaluation_periods  = 3
  treat_missing_data  = "breaching"
  actions_enabled     = var.site_alarm_enabled
  alarm_actions       = [aws_sns_topic.alerts_us_east_1.arn]
}

# ---- Monthly Bottlerocket update: first Sunday 20:00 UTC ----

resource "aws_ssm_maintenance_window" "updates" {
  name              = "${var.name}-updates"
  schedule          = "cron(0 20 ? * SUN#1 *)"
  schedule_timezone = "UTC"
  duration          = 2
  cutoff            = 1
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
