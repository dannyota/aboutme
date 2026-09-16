output "cloudflare_ipv4" {
  value = local.cloudflare_ipv4
}

output "db_endpoint" {
  value = module.data.db_endpoint
}

output "media_bucket_name" {
  value = module.data.media_bucket_name
}

output "host_instance_id" {
  value = module.host.instance_id
}

output "host_public_ip" {
  value = module.host.public_ip
}
