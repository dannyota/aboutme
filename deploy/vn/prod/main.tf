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
# "Host" (key-only SSH on the configured port).
# Egress is left to the provider default. The default egress rule is
# unconfirmed, so the probe checks outbound access before the first deploy.
resource "vngcloud_vserver_secgroup" "main" {
  project_id  = var.project_id
  name        = "aboutme-prod"
  description = "aboutme-prod host - public web and key-only SSH"
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
  port_range_min    = var.ssh_port
  port_range_max    = var.ssh_port
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

  # No ssh_key: the admin key arrives through cloud-init. The provider requires
  # user_data_base64_encode with user_data; the API takes the encoded form.
  user_data = base64encode(templatefile("${path.module}/../host/cloud-init.yaml.tftpl", {
    admin_ssh_public_key = var.admin_ssh_public_key
    ssh_port             = var.ssh_port
  }))
  user_data_base64_encode = true

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

locals {
  drill_groups = var.drill_network == null ? {} : {
    bootstrap = "aboutme-drill-bootstrap"
    restore   = "aboutme-drill-restore"
  }
  drill_production_private_cidr = var.drill_network == null ? "" : (var.drill_network.production_private_cidr == null ? "" : var.drill_network.production_private_cidr)
  drill_resolver_cidrs          = var.drill_network == null ? toset([]) : (var.drill_network.resolver_cidrs == null ? toset([]) : var.drill_network.resolver_cidrs)
  drill_time_service_cidrs      = var.drill_network == null ? toset([]) : (var.drill_network.time_service_cidrs == null ? toset([]) : var.drill_network.time_service_cidrs)
  drill_package_cidrs           = var.drill_network == null ? toset([]) : (var.drill_network.package_cidrs == null ? toset([]) : var.drill_network.package_cidrs)
  drill_hcm04_cidrs             = var.drill_network == null ? toset([]) : (var.drill_network.hcm04_cidrs == null ? toset([]) : var.drill_network.hcm04_cidrs)

  drill_rule_tuples = var.drill_network == null ? [] : concat(
    [
      for group in ["bootstrap", "restore"] : {
        group            = group
        direction        = "ingress"
        ethertype        = "IPv4"
        protocol         = "TCP"
        port_min         = 22922
        port_max         = 22922
        remote_ip_prefix = local.drill_production_private_cidr
      }
    ],
    flatten([
      for cidr in local.drill_resolver_cidrs : [
        for protocol in ["TCP", "UDP"] : {
          group            = "bootstrap"
          direction        = "egress"
          ethertype        = "IPv4"
          protocol         = protocol
          port_min         = 53
          port_max         = 53
          remote_ip_prefix = cidr
        }
      ]
    ]),
    [
      for cidr in local.drill_time_service_cidrs : {
        group            = "bootstrap"
        direction        = "egress"
        ethertype        = "IPv4"
        protocol         = "UDP"
        port_min         = 123
        port_max         = 123
        remote_ip_prefix = cidr
      }
    ],
    flatten([
      for cidr in local.drill_package_cidrs : [
        for port in [80, 443] : {
          group            = "bootstrap"
          direction        = "egress"
          ethertype        = "IPv4"
          protocol         = "TCP"
          port_min         = port
          port_max         = port
          remote_ip_prefix = cidr
        }
      ]
    ]),
    [
      for cidr in local.drill_hcm04_cidrs : {
        group            = "restore"
        direction        = "egress"
        ethertype        = "IPv4"
        protocol         = "TCP"
        port_min         = 443
        port_max         = 443
        remote_ip_prefix = cidr
      }
    ],
  )

  drill_rules = {
    for rule in local.drill_rule_tuples :
    join("|", [rule.group, rule.direction, rule.protocol, tostring(rule.port_min), rule.remote_ip_prefix]) => rule
  }
}

resource "vngcloud_vserver_secgroup" "drill" {
  for_each    = local.drill_groups
  project_id  = var.project_id
  name        = each.value
  description = "${each.value} detached and untrusted until default egress is removed"
}

resource "vngcloud_vserver_secgrouprule" "drill" {
  for_each          = local.drill_rules
  project_id        = var.project_id
  direction         = each.value.direction
  ethertype         = each.value.ethertype
  port_range_min    = each.value.port_min
  port_range_max    = each.value.port_max
  protocol          = each.value.protocol
  remote_ip_prefix  = each.value.remote_ip_prefix
  security_group_id = vngcloud_vserver_secgroup.drill[each.value.group].id
  description       = "aboutme-drill-${each.value.group}-${lower(each.value.protocol)}-${each.value.port_min}"
}
