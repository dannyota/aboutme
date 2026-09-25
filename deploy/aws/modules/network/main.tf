# VPC with one public subnet for the host and two private subnets for RDS.
# There is no NAT gateway; RDS needs no outbound access.

# Standard Availability Zones only; RDS and this design use no Local Zones.
data "aws_availability_zones" "available" {
  state = "available"
  filter {
    name   = "zone-type"
    values = ["availability-zone"]
  }
}

resource "aws_vpc" "main" {
  cidr_block           = "10.20.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = var.name }
}

resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id
  tags   = { Name = var.name }
}

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
  tags = { Name = "${var.name}-public" }
}

resource "aws_route_table_association" "public" {
  subnet_id      = aws_subnet.public.id
  route_table_id = aws_route_table.public.id
}

# The default security group allows nothing.
resource "aws_default_security_group" "main" {
  vpc_id = aws_vpc.main.id
}

resource "aws_security_group" "host" {
  name        = "${var.name}-host"
  description = "aboutme-prod host - HTTPS from Cloudflare only"
  vpc_id      = aws_vpc.main.id
  tags        = { Name = "${var.name}-host" }
}

resource "aws_vpc_security_group_ingress_rule" "cloudflare" {
  for_each          = toset(var.cloudflare_ipv4_cidrs)
  security_group_id = aws_security_group.host.id
  cidr_ipv4         = each.value
  ip_protocol       = "tcp"
  from_port         = 443
  to_port           = 443
  description       = "Cloudflare edge"
}

resource "aws_vpc_security_group_egress_rule" "host_all" {
  security_group_id = aws_security_group.host.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
  description       = "Image pulls, AWS APIs, mail"
}

# The CloudFront origin-facing prefix list weighs 55 rules against the
# default quota of 60 per group (docs/design/cloudfront-edge.md, "Origin
# access"), so it gets its own group rather than joining aws_security_group.host.
data "aws_ec2_managed_prefix_list" "cloudfront_origin" {
  name = "com.amazonaws.global.cloudfront.origin-facing"
}

resource "aws_security_group" "cloudfront_origin" {
  name        = "${var.name}-cloudfront-origin"
  description = "aboutme-prod host - HTTPS 8443 from CloudFront origin-facing only"
  vpc_id      = aws_vpc.main.id
  tags        = { Name = "${var.name}-cloudfront-origin" }
}

resource "aws_vpc_security_group_ingress_rule" "cloudfront_origin" {
  security_group_id = aws_security_group.cloudfront_origin.id
  prefix_list_id    = data.aws_ec2_managed_prefix_list.cloudfront_origin.id
  ip_protocol       = "tcp"
  from_port         = 8443
  to_port           = 8443
  description       = "CloudFront origin-facing"
}

resource "aws_security_group" "db" {
  name        = "${var.name}-db"
  description = "aboutme-prod database - PostgreSQL from the host only"
  vpc_id      = aws_vpc.main.id
  tags        = { Name = "${var.name}-db" }
}

resource "aws_vpc_security_group_ingress_rule" "db_from_host" {
  security_group_id            = aws_security_group.db.id
  referenced_security_group_id = aws_security_group.host.id
  ip_protocol                  = "tcp"
  from_port                    = 5432
  to_port                      = 5432
  description                  = "PostgreSQL from the host"
}
