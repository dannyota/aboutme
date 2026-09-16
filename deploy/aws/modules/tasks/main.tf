# ECS task definitions. Images here are the initial revision only; deploy.sh
# registers later revisions by digest, and the services ignore that drift.

locals {
  param     = "arn:aws:ssm:ap-southeast-1:${var.account_id}:parameter/aboutme/prod"
  db_url    = "postgres://%s@${var.db_endpoint}:5432/%s?sslmode=verify-full&sslrootcert=/etc/ssl/rds/global-bundle.pem"
  master_pw = "${var.db_master_secret_arn}:password::"

  logs = { for c in ["server", "caddy", "web", "migrate", "admin", "jobs"] : c => {
    logDriver = "awslogs"
    options = {
      awslogs-group         = var.log_group_name
      awslogs-region        = "ap-southeast-1"
      awslogs-stream-prefix = c
    }
  } }

  media_env = [
    { name = "MEDIA_BACKEND", value = "s3" },
    { name = "MEDIA_BUCKET", value = var.media_bucket_name },
    { name = "MEDIA_REGION", value = "ap-southeast-1" },
  ]

  server_env = concat(local.media_env, [
    { name = "ENV", value = "prod" },
    { name = "PORT", value = "8080" },
    { name = "LISTEN_HOST", value = "127.0.0.1" },
    { name = "TRUSTED_PROXY_CIDRS", value = "127.0.0.1/32" },
    { name = "PUBLIC_ORIGIN", value = "https://aboutme.vn" },
    { name = "PUBLIC_RENDER_ORIGIN", value = "http://127.0.0.1:3000" },
    { name = "PRINT_LISTEN_ADDR", value = "172.17.0.1:8081" },
    { name = "MCP_ENABLED", value = "true" },
    { name = "DATABASE_URL", value = format(local.db_url, "aboutme_app", "aboutme") },
    { name = "AUTH_EMAIL_MODE", value = "ses" },
    { name = "AWS_REGION", value = "ap-southeast-1" },
    { name = "SES_FROM_ADDRESS", value = var.ses_from_address },
    { name = "SES_CONFIGURATION_SET", value = var.ses_configuration_set },
    { name = "APP_BUILD_DIGEST", value = var.image_server },
    { name = "PUBLIC_RENDERER_BUILD_DIGEST", value = var.image_web },
  ])

  # One-shot tasks that run as the RDS master user on the first deploy.
  admin_tasks = {
    db-bootstrap = {
      entry_point = ["/usr/local/bin/db-role-bootstrap"]
      environment = [{ name = "CLUSTER_BOOTSTRAP_DATABASE_URL", value = format(local.db_url, "aboutme", "postgres") }]
      secrets     = []
    }
    db-provision = {
      entry_point = ["/usr/local/bin/migrate", "provision"]
      environment = [
        { name = "DATABASE_URL", value = format(local.db_url, "aboutme", "aboutme") },
        { name = "MIGRATION_IDENTITY", value = "direct" },
      ]
      secrets = []
    }
    db-set-login = {
      entry_point = ["/usr/local/bin/db-set-login"]
      environment = [{ name = "DATABASE_URL", value = format(local.db_url, "aboutme", "aboutme") }]
      secrets = [
        { name = "MIGRATOR_PASSWORD", valueFrom = "${local.param}/db/migrator-password" },
        { name = "APP_PASSWORD", valueFrom = "${local.param}/db/app-password" },
      ]
    }
  }
}

resource "aws_ecs_task_definition" "app" {
  family                   = "${var.name}-app"
  network_mode             = "host"
  requires_compatibilities = ["EC2"]
  execution_role_arn       = var.exec_role_arns["app"]
  task_role_arn            = var.app_task_role_arn
  container_definitions = jsonencode([
    {
      name        = "server"
      image       = var.image_server
      memory      = 512
      cpu         = 512
      essential   = true
      environment = local.server_env
      secrets = [
        { name = "PGPASSWORD", valueFrom = "${local.param}/db/app-password" },
        { name = "AUTH_EMAIL_ACTIVE_KEY_ID", valueFrom = "${local.param}/auth-email/active-key-id" },
        { name = "AUTH_EMAIL_ACTIVE_KEY", valueFrom = "${local.param}/auth-email/active-key" },
        { name = "PASSWORD_RATE_HMAC_KEY", valueFrom = "${local.param}/password-rate-hmac-key" },
      ]
      # SYS_ADMIN lets Docker's seccomp profile allow the user namespaces that
      # Chromium's sandbox needs. The image runs as a non-root user, so the
      # process gains no effective capability.
      linuxParameters = {
        initProcessEnabled = true
        sharedMemorySize   = 128
        capabilities       = { add = ["SYS_ADMIN"], drop = [] }
      }
      healthCheck = {
        command     = ["CMD", "wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8080/healthz"]
        interval    = 10
        timeout     = 3
        retries     = 3
        startPeriod = 30
      }
      logConfiguration = local.logs["server"]
    },
    {
      name         = "caddy"
      image        = var.image_caddy
      memory       = 128
      cpu          = 128
      essential    = true
      portMappings = [{ containerPort = 443, hostPort = 443, protocol = "tcp" }]
      environment  = [{ name = "CLOUDFLARE_RANGES", value = join(" ", var.cloudflare_ipv4_cidrs) }]
      secrets = [
        { name = "ORIGIN_KEY", valueFrom = "${local.param}/tls/origin-key" },
        { name = "ORIGIN_CERT", valueFrom = "${local.param}/tls/origin-cert" },
        { name = "ORIGIN_PULL_CA", valueFrom = "${local.param}/tls/origin-pull-ca" },
      ]
      linuxParameters  = { tmpfs = [{ containerPath = "/run/caddy", size = 1 }] }
      dependsOn        = [{ containerName = "server", condition = "HEALTHY" }]
      logConfiguration = local.logs["caddy"]
    },
  ])
}

resource "aws_ecs_task_definition" "web" {
  family                   = "${var.name}-web"
  network_mode             = "bridge"
  requires_compatibilities = ["EC2"]
  execution_role_arn       = var.exec_role_arns["web"]
  container_definitions = jsonencode([{
    name         = "web"
    image        = var.image_web
    memory       = 256
    cpu          = 256
    essential    = true
    portMappings = [{ containerPort = 3000, hostPort = 3000, protocol = "tcp" }]
    environment = [
      { name = "HOST", value = "0.0.0.0" },
      { name = "PORT", value = "3000" },
      { name = "NUXT_PUBLIC_API_BASE", value = "/api/v1" },
      { name = "NUXT_PRINT_ORIGIN", value = "http://172.17.0.1:8081" },
    ]
    healthCheck = {
      command     = ["CMD", "wget", "-q", "-O", "/dev/null", "http://127.0.0.1:3000/"]
      interval    = 10
      timeout     = 3
      retries     = 3
      startPeriod = 30
    }
    logConfiguration = local.logs["web"]
  }])
}

resource "aws_ecs_task_definition" "migrate" {
  family                   = "${var.name}-migrate"
  network_mode             = "bridge"
  requires_compatibilities = ["EC2"]
  execution_role_arn       = var.exec_role_arns["migrate"]
  container_definitions = jsonencode([{
    name       = "migrate"
    image      = var.image_server
    memory     = 256
    essential  = true
    entryPoint = ["/usr/local/bin/migrate"]
    environment = [
      { name = "MIGRATION_IDENTITY", value = "direct" },
      { name = "DATABASE_URL", value = format(local.db_url, "aboutme_migrator", "aboutme") },
    ]
    secrets          = [{ name = "PGPASSWORD", valueFrom = "${local.param}/db/migrator-password" }]
    logConfiguration = local.logs["migrate"]
  }])
}

resource "aws_ecs_task_definition" "admin" {
  for_each                 = local.admin_tasks
  family                   = "${var.name}-${each.key}"
  network_mode             = "bridge"
  requires_compatibilities = ["EC2"]
  execution_role_arn       = var.exec_role_arns["db-admin"]
  container_definitions = jsonencode([{
    name             = "admin"
    image            = var.image_server
    memory           = 256
    essential        = true
    entryPoint       = each.value.entry_point
    environment      = each.value.environment
    secrets          = concat([{ name = "PGPASSWORD", valueFrom = local.master_pw }], each.value.secrets)
    logConfiguration = local.logs["admin"]
  }])
}

resource "aws_ecs_task_definition" "jobs" {
  family                   = "${var.name}-jobs"
  network_mode             = "bridge"
  requires_compatibilities = ["EC2"]
  execution_role_arn       = var.exec_role_arns["jobs"]
  task_role_arn            = var.jobs_task_role_arn
  container_definitions = jsonencode([{
    name       = "jobs"
    image      = var.image_server
    memory     = 256
    essential  = true
    entryPoint = ["/usr/local/bin/server"]
    command    = ["idempotency-expiry-sweep"]
    environment = concat(local.media_env, [
      { name = "DATABASE_URL", value = format(local.db_url, "aboutme_app", "aboutme") },
    ])
    secrets          = [{ name = "PGPASSWORD", valueFrom = "${local.param}/db/app-password" }]
    logConfiguration = local.logs["jobs"]
  }])
}
