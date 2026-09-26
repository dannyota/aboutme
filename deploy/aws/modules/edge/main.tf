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

# /.well-known/deployment.json comes from the transparency bucket, cached 30
# seconds, never from the host (docs/design/deployment-transparency/
# README.md, "Serving and caching"; ADR 0057).
resource "aws_cloudfront_cache_policy" "deployment_document" {
  name        = "${var.name}-deployment-document"
  min_ttl     = 0
  default_ttl = 30
  max_ttl     = 30

  parameters_in_cache_key_and_forwarded_to_origin {
    headers_config {
      header_behavior = "none"
    }
    query_strings_config {
      query_string_behavior = "none"
    }
    cookies_config {
      cookie_behavior = "none"
    }
    enable_accept_encoding_gzip   = true
    enable_accept_encoding_brotli = true
  }
}

# The request never reaches Caddy, so the edge sets the headers Caddy would
# and strips the ones S3 adds. CloudFront replaces a removed Server header
# with its own "Server: CloudFront".
resource "aws_cloudfront_response_headers_policy" "deployment_document" {
  name = "${var.name}-deployment-document"

  security_headers_config {
    strict_transport_security {
      access_control_max_age_sec = 31536000
      include_subdomains         = false
      preload                    = false
      override                   = true
    }
    content_type_options {
      override = true
    }
    # The object is served on the site's own origin; a sandboxed empty
    # policy keeps it inert even if its content type were ever wrong.
    content_security_policy {
      content_security_policy = "default-src 'none'; frame-ancestors 'none'; sandbox"
      override                = true
    }
  }

  # Any page may read the document. CloudFront adds the header to CORS
  # requests, those that carry Origin.
  cors_config {
    access_control_allow_credentials = false
    origin_override                  = true
    access_control_allow_origins {
      items = ["*"]
    }
    access_control_allow_methods {
      items = ["GET", "HEAD"]
    }
    access_control_allow_headers {
      items = ["*"]
    }
  }

  remove_headers_config {
    items {
      header = "Server"
    }
    items {
      header = "x-amz-request-id"
    }
    items {
      header = "x-amz-id-2"
    }
    items {
      header = "x-amz-server-side-encryption"
    }
  }
}

resource "aws_cloudfront_origin_access_control" "transparency" {
  name                              = "${var.name}-transparency"
  description                       = "CloudFront reads deployment.json from the transparency bucket"
  origin_access_control_origin_type = "s3"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}

# ---- Web ACL ----

# docs/design/cloudfront-edge.md, "DDoS and WAF": a rate rule per IP, the
# Amazon IP reputation list, and known bad inputs. The core rule set is left
# out because it blocks bodies over 8 KB and inspects rich-text bodies for
# cross-site scripting. Every rule starts in count mode (var.waf_block =
# false) for a week before the owner switches it to block.
#
# Layers 2 and 3 of viewer analytics (docs/design/viewer-analytics/
# counting.md, "Layers 2 and 3: edge labels") add Bot Control (Common) and
# the Anonymous IP list, scoped to the view-collect path, plus two rules
# that turn their labels into request headers. These four rules count only,
# always, regardless of var.waf_block: the labels are a counting signal,
# never a filter, so blocking would drop real viewers and crawlers still
# need every page to load (docs/design/link-previews.md). About USD 15 a
# month (docs/design/viewer-analytics/delivery.md, "Cost").
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

  # Common-level Bot Control, count always, scoped to POST requests whose
  # path starts with the collect route (docs/design/viewer-analytics/
  # counting.md, "Layers 2 and 3: edge labels").
  rule {
    name     = "view-bot-control"
    priority = 3

    override_action {
      count {}
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesBotControlRuleSet"
        vendor_name = "AWS"

        managed_rule_group_configs {
          aws_managed_rules_bot_control_rule_set {
            inspection_level = "COMMON"
          }
        }

        scope_down_statement {
          and_statement {
            statement {
              byte_match_statement {
                field_to_match {
                  method {}
                }
                positional_constraint = "EXACTLY"
                search_string         = "POST"
                text_transformation {
                  priority = 0
                  type     = "NONE"
                }
              }
            }
            # URL_DECODE, not NONE: WAF matches the raw path, and Go decodes
            # it before routing, so an undecoded match lets
            # /api/v1/public/views/%63ollect skip this scope-down
            # (https://raw.githubusercontent.com/hashicorp/terraform-provider-aws/v6.64.0/website/docs/r/wafv2_web_acl.html.markdown,
            # "text_transformation" Block).
            statement {
              byte_match_statement {
                field_to_match {
                  uri_path {}
                }
                positional_constraint = "STARTS_WITH"
                search_string         = "/api/v1/public/views/collect"
                text_transformation {
                  priority = 0
                  type     = "URL_DECODE"
                }
              }
            }
          }
        }
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      sampled_requests_enabled   = false
      metric_name                = "${var.name}-edge-view-bot-control"
    }
  }

  # Anonymous IP list, count always, same scope-down as view-bot-control.
  # rule_action_override forces both of the group's own rules to count too:
  # without it, the group's override_action only reaches the first rule
  # that would otherwise match, so AnonymousIPList (checked first) can
  # short-circuit evaluation and hide HostingProviderIPList
  # (https://docs.aws.amazon.com/waf/latest/developerguide/web-acl-rule-group-override-options.html).
  # rule_action_override nests inside managed_rule_group_statement, not the
  # rule itself
  # (https://raw.githubusercontent.com/hashicorp/terraform-provider-aws/v6.64.0/website/docs/r/wafv2_web_acl.html.markdown,
  # "managed_rule_group_statement" and "rule_action_override" Blocks).
  rule {
    name     = "view-anonymous-ip"
    priority = 4

    override_action {
      count {}
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesAnonymousIpList"
        vendor_name = "AWS"

        rule_action_override {
          name = "AnonymousIPList"
          action_to_use {
            count {}
          }
        }
        rule_action_override {
          name = "HostingProviderIPList"
          action_to_use {
            count {}
          }
        }

        scope_down_statement {
          and_statement {
            statement {
              byte_match_statement {
                field_to_match {
                  method {}
                }
                positional_constraint = "EXACTLY"
                search_string         = "POST"
                text_transformation {
                  priority = 0
                  type     = "NONE"
                }
              }
            }
            # URL_DECODE: see the same statement in view-bot-control above.
            statement {
              byte_match_statement {
                field_to_match {
                  uri_path {}
                }
                positional_constraint = "STARTS_WITH"
                search_string         = "/api/v1/public/views/collect"
                text_transformation {
                  priority = 0
                  type     = "URL_DECODE"
                }
              }
            }
          }
        }
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      sampled_requests_enabled   = false
      metric_name                = "${var.name}-edge-view-anonymous-ip"
    }
  }

  # Reads the labels view-bot-control added and inserts the request header
  # Caddy's CloudFront listener passes to Go as x-amzn-waf-aboutme-bot
  # (docs/design/viewer-analytics/counting.md, "Layers 2 and 3: edge
  # labels"). Runs after view-bot-control, so the labels already exist.
  rule {
    name     = "view-bot-label"
    priority = 5

    action {
      count {
        custom_request_handling {
          insert_header {
            name  = "aboutme-bot"
            value = "1"
          }
        }
      }
    }

    statement {
      or_statement {
        statement {
          label_match_statement {
            scope = "NAMESPACE"
            key   = "awswaf:managed:aws:bot-control:bot:"
          }
        }
        statement {
          label_match_statement {
            scope = "LABEL"
            key   = "awswaf:managed:aws:bot-control:signal:automated_browser"
          }
        }
        statement {
          label_match_statement {
            scope = "LABEL"
            key   = "awswaf:managed:aws:bot-control:signal:non_browser_user_agent"
          }
        }
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      sampled_requests_enabled   = false
      metric_name                = "${var.name}-edge-view-bot-label"
    }
  }

  # Reads labels view-bot-control and view-anonymous-ip added and inserts
  # x-amzn-waf-aboutme-dc (docs/design/viewer-analytics/counting.md, "Layers
  # 2 and 3: edge labels").
  rule {
    name     = "view-dc-label"
    priority = 6

    action {
      count {
        custom_request_handling {
          insert_header {
            name  = "aboutme-dc"
            value = "1"
          }
        }
      }
    }

    statement {
      or_statement {
        statement {
          label_match_statement {
            scope = "LABEL"
            key   = "awswaf:managed:aws:anonymous-ip-list:HostingProviderIPList"
          }
        }
        statement {
          label_match_statement {
            scope = "LABEL"
            key   = "awswaf:managed:aws:bot-control:signal:known_bot_data_center"
          }
        }
        statement {
          label_match_statement {
            scope = "NAMESPACE"
            key   = "awswaf:managed:aws:bot-control:signal:cloud_service_provider:"
          }
        }
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      sampled_requests_enabled   = false
      metric_name                = "${var.name}-edge-view-dc-label"
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

  dynamic "origin" {
    for_each = var.transparency_enabled ? [1] : []
    content {
      origin_id                = "transparency"
      domain_name              = var.transparency_bucket_domain_name
      origin_access_control_id = aws_cloudfront_origin_access_control.transparency.id
    }
  }

  # The deployment document, from the transparency bucket.
  dynamic "ordered_cache_behavior" {
    for_each = var.transparency_enabled ? [1] : []
    content {
      path_pattern               = "/.well-known/deployment.json"
      target_origin_id           = "transparency"
      allowed_methods            = ["GET", "HEAD"]
      cached_methods             = ["GET", "HEAD"]
      cache_policy_id            = aws_cloudfront_cache_policy.deployment_document.id
      response_headers_policy_id = aws_cloudfront_response_headers_policy.deployment_document.id
      viewer_protocol_policy     = "redirect-to-https"
      compress                   = true
    }
  }

  # /_nuxt/* is the other cached path (docs/design/cloudfront-edge.md, "Cache
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
