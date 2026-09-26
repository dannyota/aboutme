output "bucket_regional_domain_name" {
  value = aws_s3_bucket.transparency.bucket_regional_domain_name
}

output "repository_url" {
  value = aws_ecr_repository.observer.repository_url
}

output "function_name" {
  value = local.function_name
}
