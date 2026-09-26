output "origin_certificate_arn" {
  value = aws_acm_certificate.origin.arn
}

output "certificate_validation_records" {
  value = distinct([
    for o in aws_acm_certificate.origin.domain_validation_options : {
      name  = o.resource_record_name
      type  = o.resource_record_type
      value = o.resource_record_value
    }
  ])
}

# The same records keyed by certificate domain. The origin certificate is
# never recreated (prevent_destroy), so the keys are known at plan time.
output "certificate_validation_records_by_domain" {
  value = {
    for o in aws_acm_certificate.origin.domain_validation_options : o.domain_name => {
      name  = o.resource_record_name
      type  = o.resource_record_type
      value = o.resource_record_value
    }
  }
}

output "distribution_id" {
  value = aws_cloudfront_distribution.edge.id
}

output "distribution_arn" {
  value = aws_cloudfront_distribution.edge.arn
}

output "distribution_domain_name" {
  value = aws_cloudfront_distribution.edge.domain_name
}

output "distribution_hosted_zone_id" {
  value = aws_cloudfront_distribution.edge.hosted_zone_id
}

output "viewer_certificate_arn" {
  value = aws_acm_certificate_validation.viewer.certificate_arn
}

output "web_acl_arn" {
  value = aws_wafv2_web_acl.edge.arn
}
