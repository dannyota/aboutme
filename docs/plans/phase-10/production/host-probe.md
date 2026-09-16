# Task 1: Bottlerocket host probe

**Goal:** prove the four unverified host facts in the
[single-host design](../../../design/single-host-production.md#host-and-networking)
on a real instance, then delete everything.

**Files:**

- Create: `deploy/aws/probe/main.tf`, `deploy/aws/probe/README.md`
- Result: `.dev/phase-10/host-probe-report.txt` (ignored)

**Interfaces:** Produces a PASS or FAIL line for each fact. Tasks 8–12 start
only when all four pass.

The probe costs a few cents and creates AWS resources. Get the owner's go-ahead
first. It uses local state in `deploy/aws/probe/` and the default VPC.

- [x] **Step 1: Write the probe root**

`deploy/aws/probe/main.tf`:

```hcl
terraform {
  required_version = "= 1.12.6"
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 6.0" }
  }
}

provider "aws" { region = "ap-southeast-1" }

data "aws_ssm_parameter" "ami" {
  name = "/aws/service/bottlerocket/aws-ecs-2/arm64/latest/image_id"
}

data "aws_vpc" "default" { default = true }

resource "aws_ecs_cluster" "probe" { name = "aboutme-probe" }

resource "aws_iam_role" "instance" {
  name = "aboutme-probe-instance"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{ Effect = "Allow", Action = "sts:AssumeRole",
    Principal = { Service = "ec2.amazonaws.com" } }]
  })
}

resource "aws_iam_role_policy_attachment" "ecs" {
  role       = aws_iam_role.instance.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonEC2ContainerServiceforEC2Role"
}

resource "aws_iam_role_policy_attachment" "ssm" {
  role       = aws_iam_role.instance.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "instance" {
  name = "aboutme-probe-instance"
  role = aws_iam_role.instance.name
}

resource "aws_iam_role" "task" {
  name = "aboutme-probe-task"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{ Effect = "Allow", Action = "sts:AssumeRole",
    Principal = { Service = "ecs-tasks.amazonaws.com" } }]
  })
}

resource "aws_security_group" "probe" {
  name   = "aboutme-probe"
  vpc_id = data.aws_vpc.default.id
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_instance" "probe" {
  ami                    = data.aws_ssm_parameter.ami.value
  instance_type          = "t4g.small"
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
  tags = { Name = "aboutme-probe" }
}

output "cluster" { value = aws_ecs_cluster.probe.name }
output "task_role_arn" { value = aws_iam_role.task.arn }
```

- [x] **Step 2: Apply**

```sh
cd deploy/aws/probe && tofu init && tofu apply
```

Expected: one instance registers in cluster `aboutme-probe` within five minutes
(`aws ecs list-container-instances --cluster aboutme-probe`).

- [x] **Step 3: Run the four checks as ECS tasks**

Register and run a host-mode task and a bridge-mode task with
`public.ecr.aws/docker/library/busybox:1.37` and the task role from the output:

1. Host-mode task role: the host task runs
   `wget -qO- http://169.254.170.2$AWS_CONTAINER_CREDENTIALS_RELATIVE_URI` and
   prints only whether the response contains `AccessKeyId`. PASS if it does.
2. Bridge to host gateway: the host task runs
   `nc -lk -s 172.17.0.1 -p 8081 -e echo ok`; the bridge task runs
   `nc 172.17.0.1 8081`. PASS if it prints `ok`.
3. Host to published bridge port: the bridge task maps container port 3000 to
   host port 3000 and runs `httpd -f -p 3000`; the host task runs
   `wget -qO- http://127.0.0.1:3000/`. PASS if the request connects.
4. IMDS block: the bridge task runs
   `wget -qO- --header 'X-aws-ec2-metadata-token-ttl-seconds: 60' --method PUT http://169.254.169.254/latest/api/token`.
   PASS if it times out.

Record each result, with no credential content, in
`.dev/phase-10/host-probe-report.txt`.

- [x] **Step 4: Destroy**

```sh
tofu destroy
```

Expected: `Destroy complete`. Confirm no `aboutme-probe` instance remains.

- [x] **Step 5: Decide**

All four PASS: continue to Task 8. Check 1 FAIL: stop and return the design for
review. Check 2 or 3 FAIL: amend the design to run `web` in `awsvpc` mode with
Cloud Map DNS before Task 10. Commit only `deploy/aws/probe/`; its state files
stay ignored.

Result (2026-09-16): all four checks passed on Bottlerocket 1.65.0. The first
run failed checks 2 and 3 only because the probe image lacked `httpd`; the fixed
run added a host-mode IMDS control for check 4.
