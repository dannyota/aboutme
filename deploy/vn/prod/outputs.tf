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
