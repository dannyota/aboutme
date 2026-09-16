terraform {
  required_version = "= 1.12.6"
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 6.0" }
  }
}

provider "aws" {
  region = "ap-southeast-1"
  default_tags {
    tags = { Project = "aboutme", Environment = "probe" }
  }
}

data "aws_caller_identity" "current" {}

data "aws_ssm_parameter" "ami" {
  name = "/aws/service/bottlerocket/aws-ecs-2/arm64/latest/image_id"
}

data "aws_vpc" "default" {
  default = true
}

data "aws_subnet" "default" {
  vpc_id            = data.aws_vpc.default.id
  availability_zone = "ap-southeast-1a"
  default_for_az    = true
}

resource "aws_ecs_cluster" "probe" {
  name = "aboutme-probe"
}

resource "aws_cloudwatch_log_group" "probe" {
  name              = "/aboutme/probe"
  retention_in_days = 1
}

resource "aws_iam_role" "instance" {
  name = "aboutme-probe-instance"
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
  name = "aboutme-probe-instance"
  role = aws_iam_role.instance.name
}

locals {
  ecs_trust = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Action    = "sts:AssumeRole"
      Principal = { Service = "ecs-tasks.amazonaws.com" }
      Condition = { StringEquals = { "aws:SourceAccount" = data.aws_caller_identity.current.account_id } }
    }]
  })
  logs = {
    logDriver = "awslogs"
    options = {
      awslogs-group         = aws_cloudwatch_log_group.probe.name
      awslogs-region        = "ap-southeast-1"
      awslogs-stream-prefix = "probe"
    }
  }
  image = "public.ecr.aws/docker/library/alpine:3.22"
}

# The task role has no permissions. The probe only checks that host-mode
# containers receive its credentials.
resource "aws_iam_role" "task" {
  name               = "aboutme-probe-task"
  assume_role_policy = local.ecs_trust
}

resource "aws_iam_role" "exec" {
  name               = "aboutme-probe-exec"
  assume_role_policy = local.ecs_trust
}

resource "aws_iam_role_policy" "exec" {
  role = aws_iam_role.exec.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["logs:CreateLogStream", "logs:PutLogEvents"]
      Resource = "${aws_cloudwatch_log_group.probe.arn}:*"
    }]
  })
}

resource "aws_security_group" "probe" {
  name        = "aboutme-probe"
  description = "aboutme probe - egress only"
  vpc_id      = data.aws_vpc.default.id
}

resource "aws_vpc_security_group_egress_rule" "all" {
  security_group_id = aws_security_group.probe.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}

resource "aws_instance" "probe" {
  ami                    = data.aws_ssm_parameter.ami.value
  instance_type          = "t4g.small"
  subnet_id              = data.aws_subnet.default.id
  iam_instance_profile   = aws_iam_instance_profile.instance.name
  vpc_security_group_ids = [aws_security_group.probe.id]
  metadata_options {
    http_tokens                 = "required"
    http_put_response_hop_limit = 1
  }
  user_data = <<-EOT
    [settings.ecs]
    cluster = "${aws_ecs_cluster.probe.name}"
    [settings.host-containers.admin]
    enabled = false
  EOT
  tags      = { Name = "aboutme-probe" }
}

# Check 1 and the listener for check 2.
resource "aws_ecs_task_definition" "host_server" {
  family                   = "aboutme-probe-host-server"
  network_mode             = "host"
  requires_compatibilities = ["EC2"]
  task_role_arn            = aws_iam_role.task.arn
  execution_role_arn       = aws_iam_role.exec.arn
  container_definitions = jsonencode([{
    name      = "host-server"
    image     = local.image
    memory    = 64
    essential = true
    command = ["sh", "-c", <<-EOT
      apk add --no-cache curl busybox-extras >/dev/null
      if [ -n "$AWS_CONTAINER_CREDENTIALS_RELATIVE_URI" ] &&
         curl -fsS -m 5 "http://169.254.170.2$AWS_CONTAINER_CREDENTIALS_RELATIVE_URI" | grep -q AccessKeyId; then
        echo "CHECK1 PASS host-mode task role"
      else
        echo "CHECK1 FAIL host-mode task role (uri $${AWS_CONTAINER_CREDENTIALS_RELATIVE_URI:+set})"
      fi
      mkdir -p /www && echo gateway-ok > /www/index.html
      exec httpd -f -p 172.17.0.1:8081 -h /www
    EOT
    ]
    logConfiguration = local.logs
  }])
}

# Checks 2 and 4, and the published port for check 3.
resource "aws_ecs_task_definition" "bridge" {
  family                   = "aboutme-probe-bridge"
  network_mode             = "bridge"
  requires_compatibilities = ["EC2"]
  execution_role_arn       = aws_iam_role.exec.arn
  container_definitions = jsonencode([{
    name         = "bridge"
    image        = local.image
    memory       = 64
    essential    = true
    portMappings = [{ containerPort = 3000, hostPort = 3000, protocol = "tcp" }]
    command = ["sh", "-c", <<-EOT
      apk add --no-cache curl busybox-extras >/dev/null
      mkdir -p /www && echo bridge-ok > /www/index.html
      httpd -p 3000 -h /www
      if curl -fsS -m 5 http://172.17.0.1:8081/ | grep -q gateway-ok; then
        echo "CHECK2 PASS bridge reaches host listener on 172.17.0.1"
      else
        echo "CHECK2 FAIL bridge reaches host listener on 172.17.0.1"
      fi
      if curl -sS -m 5 -X PUT -H 'X-aws-ec2-metadata-token-ttl-seconds: 60' \
           http://169.254.169.254/latest/api/token >/dev/null; then
        echo "CHECK4 FAIL bridge container received an IMDS token"
      else
        echo "CHECK4 PASS bridge container cannot get an IMDS token"
      fi
      sleep 900
    EOT
    ]
    logConfiguration = local.logs
  }])
}

# Check 3.
resource "aws_ecs_task_definition" "host_client" {
  family                   = "aboutme-probe-host-client"
  network_mode             = "host"
  requires_compatibilities = ["EC2"]
  execution_role_arn       = aws_iam_role.exec.arn
  container_definitions = jsonencode([{
    name      = "host-client"
    image     = local.image
    memory    = 64
    essential = true
    command = ["sh", "-c", <<-EOT
      apk add --no-cache curl busybox-extras >/dev/null
      token=$(curl -fsS -m 5 -X PUT -H 'X-aws-ec2-metadata-token-ttl-seconds: 60' http://169.254.169.254/latest/api/token || true)
      if [ -n "$token" ]; then
        echo "CHECK4 CONTROL host-mode container gets an IMDS token"
      else
        echo "CHECK4 CONTROL FAILED host-mode container got no IMDS token"
      fi
      if curl -fsS -m 5 http://127.0.0.1:3000/ | grep -q bridge-ok; then
        echo "CHECK3 PASS host reaches published bridge port on 127.0.0.1"
      else
        echo "CHECK3 FAIL host reaches published bridge port on 127.0.0.1"
      fi
    EOT
    ]
    logConfiguration = local.logs
  }])
}

output "cluster" {
  value = aws_ecs_cluster.probe.name
}
