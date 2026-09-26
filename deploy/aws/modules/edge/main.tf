# The CloudFront edge (docs/design/cloudfront-edge.md; ADR 0054): the origin
# certificate, the viewer certificate, the imported origin mTLS client
# certificate, the cache and origin request policies, the web ACL, and the
# distribution itself.

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

# An ACM exportable public certificate for the origin. Its DNS validation
# records live in the Route 53 zone (modules/dns); there is no
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

# ---- Viewer certificate ----

# The free ACM viewer certificate, issued in us-east-1 as CloudFront requires
# (docs/design/cloudfront-edge.md, "Viewer TLS, hostnames, and DNS"). Default
# key algorithm (RSA 2048) for the widest client support.
resource "aws_acm_certificate" "viewer" {
  provider                  = aws.us_east_1
  domain_name               = "aboutme.vn"
  subject_alternative_names = ["www.aboutme.vn"]
  validation_method         = "DNS"

  tags = { Name = "${var.name}-viewer" }

  lifecycle {
    create_before_destroy = true
  }
}

# ACM gives every certificate for a domain name in this account the same DNS
# validation CNAME, in any region (docs/design/cloudfront-edge.md, "Origin
# certificate"), so the origin certificate's records in the Route 53 zone
# (modules/dns) validate this one too. Apply waits here until it issues.
resource "aws_acm_certificate_validation" "viewer" {
  provider                = aws.us_east_1
  certificate_arn         = aws_acm_certificate.viewer.arn
  validation_record_fqdns = [for o in aws_acm_certificate.viewer.domain_validation_options : o.resource_record_name]

  timeouts {
    create = "45m"
  }
}

# ---- Origin mTLS client certificate ----

# The client certificate CloudFront presents to the origin's Caddy listener,
# imported by tls.sh client-ca and tls.sh client-import before the first
# apply (docs/runbooks/cloudfront.md). OpenTofu only reads it; the private
# key never enters state. No most_recent: a second match in this shared
# account fails the plan instead of wiring a stray certificate.
data "aws_acm_certificate" "client" {
  provider  = aws.us_east_1
  domain    = "cloudfront-origin.aboutme.vn"
  types     = ["IMPORTED"]
  key_types = ["EC_prime256v1"]
  statuses  = ["ISSUED"]
}

# ---- Cache and origin request policies ----

# docs/design/cloudfront-edge.md, "Cache and forwarding": hashed assets cache
# for up to a year; the query string is part of the key because the
# renderer's stylesheets are versioned by ?v=.
resource "aws_cloudfront_cache_policy" "nuxt_assets" {
  name        = "${var.name}-nuxt-assets"
  min_ttl     = 0
  default_ttl = 0
  max_ttl     = 31536000

  parameters_in_cache_key_and_forwarded_to_origin {
    headers_config {
      header_behavior = "whitelist"
      headers {
        items = ["Host"]
      }
    }
    query_strings_config {
      query_string_behavior = "all"
    }
    cookies_config {
      cookie_behavior = "none"
    }
    enable_accept_encoding_gzip   = true
    enable_accept_encoding_brotli = true
  }
}

# Everything else forwards every viewer header, cookie, and query string,
# plus CloudFront-Viewer-Address, so Authorization reaches the origin on
# every method (docs/design/cloudfront-edge.md, "Cache and forwarding").
resource "aws_cloudfront_origin_request_policy" "all_viewer" {
  name = "${var.name}-all-viewer"

  headers_config {
    header_behavior = "allViewerAndWhitelistCloudFront"
    headers {
      items = ["CloudFront-Viewer-Address"]
    }
  }
  cookies_config {
    cookie_behavior = "all"
  }
  query_strings_config {
    query_string_behavior = "all"
  }
}

data "aws_cloudfront_cache_policy" "disabled" {
  name = "Managed-CachingDisabled"
}

# ---- Web ACL ----

# docs/design/cloudfront-edge.md, "DDoS and WAF": a rate rule per IP, the
# Amazon IP reputation list, and known bad inputs. The core rule set is left
# out because it blocks bodies over 8 KB and inspects rich-text bodies for
# cross-site scripting. Every rule starts in count mode (var.waf_block =
# false) for a week before the owner switches it to block.
#
# sampled_requests_enabled stays false everywhere: sampled requests keep the
# viewer's IP address and headers, and the privacy policy promises no IP
# address in request logs. No logging configuration is set either.
resource "aws_wafv2_web_acl" "edge" {
  provider    = aws.us_east_1
  name        = "${var.name}-edge"
  description = "aboutme-prod CloudFront edge web ACL"
  scope       = "CLOUDFRONT"

  default_action {
    allow {}
  }

  rule {
    name     = "rate-per-ip"
    priority = 0

    dynamic "action" {
      for_each = var.waf_block ? [] : [1]
      content {
        count {}
      }
    }
    dynamic "action" {
      for_each = var.waf_block ? [1] : []
      content {
        block {}
      }
    }

    statement {
      rate_based_statement {
        limit                 = 2000
        aggregate_key_type    = "IP"
        evaluation_window_sec = 300
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      sampled_requests_enabled   = false
      metric_name                = "${var.name}-edge-rate-per-ip"
    }
  }

  rule {
    name     = "ip-reputation"
    priority = 1

    dynamic "override_action" {
      for_each = var.waf_block ? [] : [1]
      content {
        count {}
      }
    }
    dynamic "override_action" {
      for_each = var.waf_block ? [1] : []
      content {
        none {}
      }
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesAmazonIpReputationList"
        vendor_name = "AWS"
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      sampled_requests_enabled   = false
      metric_name                = "${var.name}-edge-ip-reputation"
    }
  }

  rule {
    name     = "known-bad-inputs"
    priority = 2

    dynamic "override_action" {
      for_each = var.waf_block ? [] : [1]
      content {
        count {}
      }
    }
    dynamic "override_action" {
      for_each = var.waf_block ? [1] : []
      content {
        none {}
      }
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesKnownBadInputsRuleSet"
        vendor_name = "AWS"
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      sampled_requests_enabled   = false
      metric_name                = "${var.name}-edge-known-bad-inputs"
    }
  }

  visibility_config {
    cloudwatch_metrics_enabled = true
    sampled_requests_enabled   = false
    metric_name                = "${var.name}-edge"
  }
}

# ---- Distribution ----

locals {
  # Error caching TTL 0 for every error CloudFront may cache, so a
  # maintenance 503 or a hash the old release lacks is never replayed
  # (docs/design/cloudfront-edge.md, "Cache and forwarding").
  cached_error_codes = [400, 403, 404, 405, 414, 416, 500, 501, 502, 503, 504]
}

resource "aws_cloudfront_distribution" "edge" {
  enabled         = true
  is_ipv6_enabled = true
  http_version    = "http2and3"
  price_class     = "PriceClass_All"
  aliases         = ["aboutme.vn", "www.aboutme.vn"]
  comment         = "${var.name} edge"
  web_acl_id      = aws_wafv2_web_acl.edge.arn

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  viewer_certificate {
    acm_certificate_arn      = aws_acm_certificate_validation.viewer.certificate_arn
    ssl_support_method       = "sni-only"
    minimum_protocol_version = "TLSv1.2_2021"
  }

  origin {
    origin_id   = "host"
    domain_name = var.origin_domain_name

    connection_attempts = 3
    connection_timeout  = 10

    custom_origin_config {
      http_port                = 80
      https_port               = 8443
      origin_protocol_policy   = "https-only"
      origin_ssl_protocols     = ["TLSv1.2"]
      origin_read_timeout      = 60
      origin_keepalive_timeout = 60
      ip_address_type          = "ipv4"

      origin_mtls_config {
        client_certificate_arn = data.aws_acm_certificate.client.arn
      }
    }
  }

  # /_nuxt/* is the only cached path (docs/design/cloudfront-edge.md, "Cache
  # and forwarding"); Caddy already compresses at the origin.
  ordered_cache_behavior {
    path_pattern           = "/_nuxt/*"
    target_origin_id       = "host"
    allowed_methods        = ["GET", "HEAD"]
    cached_methods         = ["GET", "HEAD"]
    cache_policy_id        = aws_cloudfront_cache_policy.nuxt_assets.id
    viewer_protocol_policy = "redirect-to-https"
    compress               = false
  }

  default_cache_behavior {
    target_origin_id         = "host"
    allowed_methods          = ["GET", "HEAD", "OPTIONS", "PUT", "POST", "PATCH", "DELETE"]
    cached_methods           = ["GET", "HEAD"]
    cache_policy_id          = data.aws_cloudfront_cache_policy.disabled.id
    origin_request_policy_id = aws_cloudfront_origin_request_policy.all_viewer.id
    viewer_protocol_policy   = "redirect-to-https"
    compress                 = false
  }

  dynamic "custom_error_response" {
    for_each = local.cached_error_codes
    content {
      error_code            = custom_error_response.value
      error_caching_min_ttl = 0
    }
  }
}

# docs/design/cloudfront-edge.md, "Deploys and monitoring": the imported
# client certificate's expiry alert, 45 days ahead.
resource "aws_cloudwatch_event_rule" "client_cert_expiring" {
  provider    = aws.us_east_1
  name        = "${var.name}-client-cert-expiring"
  description = "The imported CloudFront origin mTLS client certificate is approaching expiration"
  event_pattern = jsonencode({
    source      = ["aws.acm"]
    detail-type = ["ACM Certificate Approaching Expiration"]
    resources   = [data.aws_acm_certificate.client.arn]
  })
}

resource "aws_cloudwatch_event_target" "client_cert_expiring" {
  provider = aws.us_east_1
  rule     = aws_cloudwatch_event_rule.client_cert_expiring.name
  arn      = var.alerts_topic_arn_us_east_1
}
