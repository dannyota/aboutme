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

output "origin_certificate_arn" {
  value = module.edge.origin_certificate_arn
}

output "certificate_validation_records" {
  value = module.edge.certificate_validation_records
}
