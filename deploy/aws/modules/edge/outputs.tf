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

output "distribution_id" {
  value = aws_cloudfront_distribution.edge.id
}

output "distribution_domain_name" {
  value = aws_cloudfront_distribution.edge.domain_name
}

output "viewer_certificate_arn" {
  value = aws_acm_certificate_validation.viewer.certificate_arn
}

output "web_acl_arn" {
  value = aws_wafv2_web_acl.edge.arn
}
