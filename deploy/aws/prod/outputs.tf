output "cloudflare_ipv4" {
  value = local.cloudflare_ipv4
}

output "db_endpoint" {
  value = module.data.db_endpoint
}

output "media_bucket_name" {
  value = module.data.media_bucket_name
}
