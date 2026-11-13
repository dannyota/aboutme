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

  static_records = {
    caa = {
      name    = local.zone
      type    = "CAA"
      records = ["0 issue \"amazon.com\"", "0 issue \"letsencrypt.org\""]
    }
    mx = {
      name    = local.zone
      type    = "MX"
      records = ["1 smtp.google.com"]
    }
    spf = {
      name    = local.zone
      type    = "TXT"
      records = ["v=spf1 include:_spf.bizflycloud.vn include:_spf.google.com ~all"]
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
    # One TXT string holds at most 255 characters; the provider sends "a""b" as two strings of one record (Route 53 TXT records).
    google_dkim = {
      name    = "google._domainkey.${local.zone}"
      type    = "TXT"
      records = [join("\"\"", regexall(".{1,255}", var.google_workspace.dkim_txt))]
    }
    bizfly_dkim = {
      name    = "dkim._domainkey.${local.zone}"
      type    = "TXT"
      records = [join("\"\"", regexall(".{1,255}", var.bizfly_dkim_txt))]
    }
    bizfly_dkim_etrans = {
      name    = "etrans._domainkey.${local.zone}"
      type    = "TXT"
      records = [join("\"\"", regexall(".{1,255}", var.bizfly_dkim_txt))]
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

# Route 53 creates the NS record with a two-day TTL; one hour keeps a registry change quick to apply or undo (Route 53 NS records).
resource "aws_route53_record" "ns" {
  zone_id         = aws_route53_zone.zone.zone_id
  name            = local.zone
  type            = "NS"
  ttl             = 3600
  records         = aws_route53_zone.zone.name_servers
  allow_overwrite = true
}

resource "aws_route53_record" "edge" {
  for_each = {
    for pair in setproduct([local.zone, "www.${local.zone}"], var.apex_on_vn_host ? ["A"] : ["A", "AAAA", "HTTPS"]) :
    "${pair[0]} ${pair[1]}" => { name = pair[0], type = pair[1] }
  }

  zone_id = aws_route53_zone.zone.zone_id
  name    = each.value.name
  type    = each.value.type
  ttl     = var.apex_on_vn_host ? 60 : null
  records = var.apex_on_vn_host ? [var.vn_host_ipv4] : null

  dynamic "alias" {
    for_each = var.apex_on_vn_host ? [] : [1]
    content {
      name    = var.distribution_domain_name
      zone_id = var.distribution_hosted_zone_id
      # CloudFront aliases do not support target health (Route 53 alias records).
      evaluate_target_health = false
    }
  }

  lifecycle {
    precondition {
      condition     = !var.apex_on_vn_host || var.vn_host_ipv4 != null
      error_message = "apex_on_vn_host needs vn_host_ipv4."
    }
  }
}

resource "aws_route53_record" "vn_rehearsal" {
  for_each = var.vn_host_ipv4 == null || var.apex_on_vn_host ? toset([]) : toset(["vn-rehearsal.${local.zone}", "www.vn-rehearsal.${local.zone}"])

  zone_id = aws_route53_zone.zone.zone_id
  name    = each.value
  type    = "A"
  ttl     = 60
  records = [var.vn_host_ipv4]
}

resource "aws_route53_record" "static" {
  for_each = local.static_records

  zone_id = aws_route53_zone.zone.zone_id
  name    = each.value.name
  type    = each.value.type
  ttl     = local.ttl
  records = each.value.records
}

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

resource "aws_route53_record" "acm_validation" {
  for_each = var.certificate_validation_records

  zone_id = aws_route53_zone.zone.zone_id
  name    = each.value.name
  type    = each.value.type
  ttl     = local.ttl
  records = [each.value.value]
}

# Route 53 requires the key-signing key's KMS key to be ECC_NIST_P256 in us-east-1 (Route 53 DNSSEC).
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
        # As in the Route 53 key policy example, only the signing statement takes the source conditions (Route 53 DNSSEC key policy).
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

resource "aws_route53_hosted_zone_dnssec" "zone" {
  hosted_zone_id = aws_route53_key_signing_key.ksk.hosted_zone_id
  signing_status = "SIGNING"

  lifecycle {
    prevent_destroy = true
  }
}

# Route 53 publishes these metrics every four hours, in us-east-1 only (Route 53 guide, "Monitoring hosted zones using Amazon CloudWatch").
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
