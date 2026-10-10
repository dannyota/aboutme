# Network. docs/design/vietnam-production.md, "Host".
resource "vngcloud_vserver_network" "main" {
  project_id = var.project_id
  name       = "aboutme-prod"
  cidr       = var.network_cidr
  zone_id    = var.zone_id
}

resource "vngcloud_vserver_subnet" "main" {
  project_id = var.project_id
  name       = "aboutme-prod"
  cidr       = var.subnet_cidr
  network_id = vngcloud_vserver_network.main.id
  zone_id    = var.zone_id
}

# Firewall. docs/design/vietnam-production.md, "Edge" (80 and 443 public) and
# "Host" (SSH from the allowlist only).
# Egress is left to the provider default. The default egress rule is
# unconfirmed, so the probe checks outbound access before the first deploy.
resource "vngcloud_vserver_secgroup" "main" {
  project_id  = var.project_id
  name        = "aboutme-prod"
  description = "aboutme-prod host: web from anywhere, SSH from the allowlist"
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
  description       = "aboutme-prod-http"
}

resource "vngcloud_vserver_secgrouprule" "https" {
  project_id        = var.project_id
  direction         = "ingress"
  ethertype         = "IPv4"
  port_range_min    = 443
  port_range_max    = 443
  protocol          = "TCP"
  remote_ip_prefix  = "0.0.0.0/0"
  security_group_id = vngcloud_vserver_secgroup.main.id
  description       = "aboutme-prod-https"
}

resource "vngcloud_vserver_secgrouprule" "ssh" {
  for_each          = toset(var.ssh_allowlist)
  project_id        = var.project_id
  direction         = "ingress"
  ethertype         = "IPv4"
  port_range_min    = 22
  port_range_max    = 22
  protocol          = "TCP"
  remote_ip_prefix  = each.value
  security_group_id = vngcloud_vserver_secgroup.main.id
  description       = "aboutme-prod-ssh"
}

# The host. docs/design/vietnam-production.md, "Host". Both disks are encrypted
# because a server with plain disks refuses an encrypted volume.
resource "vngcloud_vserver_server" "main" {
  project_id                = var.project_id
  name                      = "aboutme-prod"
  zone_id                   = var.zone_id
  encryption_volume         = true
  root_disk_encryption_type = var.disk_encryption_type
  flavor_id                 = var.flavor_id
  image_id                  = var.image_id
  network_id                = vngcloud_vserver_network.main.id
  subnet_id                 = vngcloud_vserver_subnet.main.id
  root_disk_size            = var.root_disk_gb
  root_disk_type_id         = var.volume_type_id
  security_group            = [vngcloud_vserver_secgroup.main.id]
  attach_floating           = true

  # No ssh_key: the admin key arrives through cloud-init.
  user_data = templatefile("${path.module}/../host/cloud-init.yaml.tftpl", {
    admin_ssh_public_key = var.admin_ssh_public_key
  })

  lifecycle {
    prevent_destroy = true
    # Replacing the server deletes its floating IP, so cloud-init stays
    # first-boot only and host/install.sh applies every later change.
    ignore_changes = [user_data, image_id]
  }
}

# Data volume. docs/design/vietnam-production.md, "Host".
resource "vngcloud_vserver_volume" "data" {
  project_id      = var.project_id
  name            = "aboutme-prod-data"
  size            = var.data_disk_gb
  volume_type_id  = var.volume_type_id
  encryption_type = var.disk_encryption_type
  zone_id         = var.zone_id

  lifecycle {
    prevent_destroy = true
  }
}

resource "vngcloud_vserver_volume_attach" "data" {
  project_id = var.project_id
  volume_id  = vngcloud_vserver_volume.data.id
  server_id  = vngcloud_vserver_server.main.id
}
