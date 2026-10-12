output "server_id" {
  value = vngcloud_vserver_server.main.id
}

output "floating_ip" {
  value = try(vngcloud_vserver_server.main.internal_interfaces[0]["floating_ip"], null)
}

output "data_volume_id" {
  value = vngcloud_vserver_volume.data.id
}

output "security_group_id" {
  value = vngcloud_vserver_secgroup.main.id
}

output "drill_security_group_ids" {
  description = "Detached drill group IDs for reconciliation before attachment"
  value = var.drill_network == null ? null : {
    bootstrap = vngcloud_vserver_secgroup.drill["bootstrap"].id
    restore   = vngcloud_vserver_secgroup.drill["restore"].id
  }
}

output "drill_security_group_rules" {
  description = "Exact managed drill rule tuples for reconciliation before attachment"
  value       = var.drill_network == null ? null : values(local.drill_rules)
}
