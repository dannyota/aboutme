# The public Route 53 hosted zone for aboutme.vn (docs/runbooks/dns.md): the
# CloudFront aliases, the mail and ACM validation records, CAA, and DNSSEC
# signing. Alias answers let Route 53 pick the CloudFront edge nearest the
# viewer at the apex, which a flattened CNAME cannot.

terraform {
  required_providers {
    aws = {
      source                = "hashicorp/aws"
      configuration_aliases = [aws.us_east_1]
    }
  }
}

locals {
  zone = "aboutme.vn"
  ttl  = 300

  # Records with fixed values. The mail records are Google Workspace's and
  # the SES custom MAIL FROM's (docs/runbooks/email.md).
  static_records = {
    caa = {
      name    = local.zone
      type    = "CAA"
      records = ["0 issue \"amazon.com\""]
    }
    mx = {
      name    = local.zone
      type    = "MX"
      records = ["1 smtp.google.com"]
    }
    spf = {
      name    = local.zone
      type    = "TXT"
      records = ["v=spf1 include:_spf.google.com ~all"]
    }
    dmarc = {
      name    = "_dmarc.${local.zone}"
      type    = "TXT"
      records = ["v=DMARC1; p=none; rua=mailto:danny@aboutme.vn"]
    }
    bounce_mx = {
      name    = "bounce.${local.zone}"
      type    = "MX"
      records = ["10 feedback-smtp.ap-southeast-1.amazonses.com"]
    }
    bounce_spf = {
      name    = "bounce.${local.zone}"
      type    = "TXT"
      records = ["v=spf1 include:amazonses.com ~all"]
    }
    google_verification = {
      name    = "${var.google_workspace.verification_label}.${local.zone}"
      type    = "CNAME"
      records = [var.google_workspace.verification_target]
    }
    # One TXT string holds at most 255 characters; the provider sends
    # "a""b" as two strings of one record, which resolvers join.
    google_dkim = {
      name    = "google._domainkey.${local.zone}"
      type    = "TXT"
      records = [join("\"\"", regexall(".{1,255}", var.google_workspace.dkim_txt))]
    }
  }
}

resource "aws_route53_zone" "zone" {
  name    = local.zone
  comment = "${var.name} public zone"

  # A new zone gets new name servers, and a new KSK would no longer match the
  # DS record at the .vn registry: both take the domain offline.
  lifecycle {
    prevent_destroy = true
  }
}

# Route 53 creates the zone's NS record with a two-day TTL. One hour keeps a
# name server change at the registry quick to take effect or to undo.
resource "aws_route53_record" "ns" {
  zone_id         = aws_route53_zone.zone.zone_id
  name            = local.zone
  type            = "NS"
  ttl             = 3600
  records         = aws_route53_zone.zone.name_servers
  allow_overwrite = true
}

# Alias A and AAAA answers carry the distribution's addresses for the
# resolver's (or the client subnet's) location; the HTTPS alias advertises
# HTTP/2 and HTTP/3 before the connection. Queries for all three are free.
resource "aws_route53_record" "edge" {
  for_each = {
    for pair in setproduct([local.zone, "www.${local.zone}"], ["A", "AAAA", "HTTPS"]) :
    "${pair[0]} ${pair[1]}" => { name = pair[0], type = pair[1] }
  }

  zone_id = aws_route53_zone.zone.zone_id
  name    = each.value.name
  type    = each.value.type

  alias {
    name    = var.distribution_domain_name
    zone_id = var.distribution_hosted_zone_id
    # CloudFront aliases do not support target health.
    evaluate_target_health = false
  }
}

resource "aws_route53_record" "static" {
  for_each = local.static_records

  zone_id = aws_route53_zone.zone.zone_id
  name    = each.value.name
  type    = each.value.type
  ttl     = local.ttl
  records = each.value.records
}

# The SES identity belongs to the aboutme-email CloudFormation stack; its
# Easy DKIM tokens are read here so no generated token is committed.
data "aws_sesv2_email_identity" "domain" {
  email_identity = local.zone
}

resource "aws_route53_record" "ses_dkim" {
  for_each = toset(data.aws_sesv2_email_identity.domain.dkim_signing_attributes[0].tokens)

  zone_id = aws_route53_zone.zone.zone_id
  name    = "${each.value}._domainkey.${local.zone}"
  type    = "CNAME"
  ttl     = local.ttl
  records = ["${each.value}.dkim.amazonses.com"]
}

# ACM DNS validation, keyed by certificate domain. ACM gives the origin and
# viewer certificates the same records, so these keep both renewing
# automatically.
resource "aws_route53_record" "acm_validation" {
  for_each = var.certificate_validation_records

  zone_id = aws_route53_zone.zone.zone_id
  name    = each.value.name
  type    = each.value.type
  ttl     = local.ttl
  records = [each.value.value]
}

# ---- DNSSEC signing ----

# The key-signing key's KMS key: asymmetric ECC_NIST_P256 in us-east-1, as
# Route 53 requires. Route 53 uses it only for the zone below.
resource "aws_kms_key" "dnssec" {
  provider                 = aws.us_east_1
  description              = "${var.name} Route 53 DNSSEC key-signing key for ${local.zone}"
  customer_master_key_spec = "ECC_NIST_P256"
  key_usage                = "SIGN_VERIFY"
  deletion_window_in_days  = 30

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "AccountAdministration"
        Effect    = "Allow"
        Principal = { AWS = "arn:aws:iam::${var.account_id}:root" }
        Action    = "kms:*"
        Resource  = "*"
      },
      {
        Sid       = "Route53DnssecSigning"
        Effect    = "Allow"
        Principal = { Service = "dnssec-route53.amazonaws.com" }
        Action    = ["kms:DescribeKey", "kms:GetPublicKey", "kms:Sign"]
        Resource  = "*"
        Condition = {
          StringEquals = { "aws:SourceAccount" = var.account_id }
          ArnEquals    = { "aws:SourceArn" = aws_route53_zone.zone.arn }
        }
      },
      {
        Sid       = "Route53DnssecGrant"
        Effect    = "Allow"
        Principal = { Service = "dnssec-route53.amazonaws.com" }
        Action    = "kms:CreateGrant"
        Resource  = "*"
        # As in the Route 53 key policy example: only the signing statement
        # takes the source conditions.
        Condition = {
          Bool = { "kms:GrantIsForAWSResource" = "true" }
        }
      },
    ]
  })

  tags = { Name = "${var.name}-dnssec" }

  # Once the DS record is at the registry, losing this key makes every
  # validating resolver fail the whole domain.
  lifecycle {
    prevent_destroy = true
  }
}

resource "aws_route53_key_signing_key" "ksk" {
  hosted_zone_id             = aws_route53_zone.zone.id
  key_management_service_arn = aws_kms_key.dnssec.arn
  name                       = "aboutme_vn_ksk"
  status                     = "ACTIVE"

  # Once the DS record names this key, replacing or deactivating it fails
  # validation for the whole domain.
  lifecycle {
    prevent_destroy = true
  }
}

# Signing without a DS record at the registry is safe: resolvers treat the
# zone as unsigned until the DS exists (docs/runbooks/dns.md, "DNSSEC").
resource "aws_route53_hosted_zone_dnssec" "zone" {
  hosted_zone_id = aws_route53_key_signing_key.ksk.hosted_zone_id
  signing_status = "SIGNING"

  lifecycle {
    prevent_destroy = true
  }
}

# Route 53 publishes these metrics once every four hours, in us-east-1 only
# ("Monitoring hosted zones using Amazon CloudWatch" in the Route 53 guide).
# Either one means validating resolvers may soon fail the domain.
resource "aws_cloudwatch_metric_alarm" "dnssec" {
  for_each = {
    internal-failure = "DNSSECInternalFailure"
    ksk-action       = "DNSSECKeySigningKeysNeedingAction"
  }

  provider            = aws.us_east_1
  alarm_name          = "${var.name}-dnssec-${each.key}"
  alarm_description   = "Route 53 DNSSEC for ${local.zone}: ${each.value} above 0"
  namespace           = "AWS/Route53"
  metric_name         = each.value
  statistic           = "Sum"
  dimensions          = { HostedZoneId = aws_route53_zone.zone.zone_id }
  comparison_operator = "GreaterThanThreshold"
  threshold           = 0
  period              = 14400
  evaluation_periods  = 1
  treat_missing_data  = "notBreaching"
  alarm_actions       = [var.alerts_topic_arn_us_east_1]
}
