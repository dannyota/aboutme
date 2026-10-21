locals {
  name = "aboutme-cs-rehearsal-${var.run_id}"
}
resource "vngcloud_vserver_secgroup" "main" {
  project_id  = var.project_id
  name        = local.name
  description = local.name
}
resource "vngcloud_vserver_secgrouprule" "http" {
  project_id        = var.project_id
  direction         = "ingress"
  ethertype         = "IPv4"
  port_range_min    = 80
  port_range_max    = 80
  protocol          = "TCP"
  remote_ip_prefix  = "0.0.0.0/0"
  security_group_id = vngcloud_vserver_secgroup.main.id
  description       = "${local.name}-acme"
}
resource "vngcloud_vserver_secgrouprule" "https" {
  for_each          = var.operator_cidrs
  project_id        = var.project_id
  direction         = "ingress"
  ethertype         = "IPv4"
  port_range_min    = 443
  port_range_max    = 443
  protocol          = "TCP"
  remote_ip_prefix  = each.value
  security_group_id = vngcloud_vserver_secgroup.main.id
  description       = "${local.name}-https"
}
resource "vngcloud_vserver_secgrouprule" "ssh" {
  for_each          = var.operator_cidrs
  project_id        = var.project_id
  direction         = "ingress"
  ethertype         = "IPv4"
  port_range_min    = 22922
  port_range_max    = 22922
  protocol          = "TCP"
  remote_ip_prefix  = each.value
  security_group_id = vngcloud_vserver_secgroup.main.id
  description       = "${local.name}-ssh"
}
resource "vngcloud_vserver_server" "main" {
  project_id                = var.project_id
  name                      = local.name
  zone_id                   = "HCM03-1C"
  encryption_volume         = true
  root_disk_encryption_type = "aes-xts-plain64_256"
  flavor_id                 = "flav-530ea5cb-6fac-4264-bcad-9e0e0a5ba3fc"
  image_id                  = "img-34440a82-92fb-40bc-b79c-b1a2b49b93de"
  network_id                = var.network_id
  subnet_id                 = var.subnet_id
  root_disk_size            = 30
  root_disk_type_id         = "vtype-e782f8e1-0569-11f0-a0a4-ec2a72332f83"
  security_group            = [vngcloud_vserver_secgroup.main.id]
  attach_floating           = true
  user_data                 = base64encode(var.cloud_init)
  user_data_base64_encode   = true
}
resource "vngcloud_vserver_volume" "data" {
  project_id      = var.project_id
  name            = "${local.name}-data"
  size            = 20
  volume_type_id  = "vtype-e782f8e1-0569-11f0-a0a4-ec2a72332f83"
  encryption_type = "aes-xts-plain64_256"
  zone_id         = "HCM03-1C"
}
resource "vngcloud_vserver_volume_attach" "data" {
  project_id = var.project_id
  volume_id  = vngcloud_vserver_volume.data.id
  server_id  = vngcloud_vserver_server.main.id
}
resource "cloudflare_dns_record" "rehearsal" {
  zone_id = var.zone_id
  name    = "cs-rehearsal-${var.run_id}.aboutme.vn"
  type    = "A"
  content = vngcloud_vserver_server.main.internal_interfaces[0]["floating_ip"]
  ttl     = 300
  proxied = false
}
output "server_id" { value = vngcloud_vserver_server.main.id }
output "floating_ip" { value = vngcloud_vserver_server.main.internal_interfaces[0]["floating_ip"] }
output "data_volume_id" { value = vngcloud_vserver_volume.data.id }
output "security_group_id" { value = vngcloud_vserver_secgroup.main.id }
output "dns_record_id" { value = cloudflare_dns_record.rehearsal.id }
