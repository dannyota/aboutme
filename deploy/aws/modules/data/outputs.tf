output "db_endpoint" {
  value = aws_db_instance.main.address
}

output "db_instance_id" {
  value = aws_db_instance.main.identifier
}

output "db_master_secret_arn" {
  value = aws_db_instance.main.master_user_secret[0].secret_arn
}

output "media_bucket_arn" {
  value = aws_s3_bucket.media.arn
}

output "media_bucket_name" {
  value = aws_s3_bucket.media.bucket
}
