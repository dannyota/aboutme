# One Bottlerocket ECS host with a fixed Elastic IP and its ECS services.
# The AMI and user data are ignored after creation: Bottlerocket updates in
# place during the maintenance window.

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

  # Hop limit 1 keeps bridge-network containers away from instance metadata.
  metadata_options {
    http_endpoint               = "enabled"
    http_tokens                 = "required"
    http_put_response_hop_limit = 1
  }

  # OS volume.
  root_block_device {
    volume_type = "gp3"
    volume_size = 2
    encrypted   = true
  }

  # Container data volume.
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

    # Chromium's sandbox needs user namespaces; Bottlerocket disables them.
    [settings.kernel.sysctl]
    "user.max_user_namespaces" = "16384"
  EOT

  # The data volume is set at creation only; AWS reports computed fields and
  # default tags on it that would otherwise force a replacement.
  lifecycle {
    ignore_changes = [ami, user_data, ebs_block_device]
  }

  tags = { Name = var.name }
}

resource "aws_eip" "host" {
  domain = "vpc"
  tags   = { Name = var.name }
}

resource "aws_eip_association" "host" {
  instance_id   = aws_instance.host.id
  allocation_id = aws_eip.host.id
}

# Both services start at zero tasks; deploy.sh sets the task definition and
# the count, so OpenTofu ignores both afterwards. Maximum 100% with minimum 0%
# lets the single host-port task stop before its replacement starts.
resource "aws_ecs_service" "service" {
  for_each                           = var.task_definition_arns
  name                               = "${var.name}-${each.key}"
  cluster                            = aws_ecs_cluster.main.id
  task_definition                    = each.value
  desired_count                      = 0
  launch_type                        = "EC2"
  deployment_minimum_healthy_percent = 0
  deployment_maximum_percent         = 100

  lifecycle {
    ignore_changes = [task_definition, desired_count]
  }
}
