output "instance_id" {
  value = aws_instance.host.id
}

output "public_ip" {
  value = aws_eip.host.public_ip
}

output "cluster_name" {
  value = aws_ecs_cluster.main.name
}

output "cluster_arn" {
  value = aws_ecs_cluster.main.arn
}
