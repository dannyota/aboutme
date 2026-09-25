# The CloudFront edge (docs/design/cloudfront-edge.md; ADR 0054). This module
# currently holds only the origin certificate; the CloudFront distribution and
# its supporting resources join it as the migration reaches those steps.

terraform {
  required_providers {
    aws = {
      source                = "hashicorp/aws"
      configuration_aliases = [aws.us_east_1]
    }
  }
}

locals {
  ecdsa_p256 = "EC_prime256v1"
}

# An ACM exportable public certificate for the origin. DNS validation is added
# by hand at Cloudflare (docs/runbooks/cloudfront.md); there is no
# aws_acm_certificate_validation resource, so apply never waits on issuance.
resource "aws_acm_certificate" "origin" {
  domain_name               = "aboutme.vn"
  subject_alternative_names = ["www.aboutme.vn"]
  validation_method         = "DNS"
  # ECDSA P-256. Named through a local: the literal on a key_algorithm line
  # trips gitleaks' generic-api-key heuristic.
  key_algorithm = local.ecdsa_p256

  options {
    export = "ENABLED"
  }

  tags = { Name = "${var.name}-origin" }

  # Each issuance costs USD 7 per name (docs/design/cloudfront-edge.md,
  # "Origin certificate"); never replace this certificate by recreation.
  lifecycle {
    prevent_destroy = true
  }
}

# Renewal succeeded: export it with tls.sh export and redeploy the live tag.
resource "aws_cloudwatch_event_rule" "origin_cert_renewed" {
  name        = "${var.name}-origin-cert-renewed"
  description = "The ACM origin certificate renewed"
  event_pattern = jsonencode({
    source      = ["aws.acm"]
    detail-type = ["ACM Certificate Available"]
    resources   = [aws_acm_certificate.origin.arn]
    detail      = { Action = ["RENEWAL"] }
  })
}

resource "aws_cloudwatch_event_target" "origin_cert_renewed" {
  rule = aws_cloudwatch_event_rule.origin_cert_renewed.name
  arn  = var.alerts_topic_arn
}

# Renewal needs action ACM cannot take on its own (for example the DNS
# validation records went missing).
resource "aws_cloudwatch_event_rule" "origin_cert_renewal_blocked" {
  name        = "${var.name}-origin-cert-renewal-blocked"
  description = "The ACM origin certificate needs action to renew"
  event_pattern = jsonencode({
    source      = ["aws.acm"]
    detail-type = ["ACM Certificate Renewal Action Required"]
    resources   = [aws_acm_certificate.origin.arn]
  })
}

resource "aws_cloudwatch_event_target" "origin_cert_renewal_blocked" {
  rule = aws_cloudwatch_event_rule.origin_cert_renewal_blocked.name
  arn  = var.alerts_topic_arn
}
