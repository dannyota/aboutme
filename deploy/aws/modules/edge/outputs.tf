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
