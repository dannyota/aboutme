output "vpc_id" {
  value = aws_vpc.main.id
}

output "public_subnet_id" {
  value = aws_subnet.public.id
}

output "private_subnet_ids" {
  value = aws_subnet.private[*].id
}

output "host_security_group_id" {
  value = aws_security_group.host.id
}

output "cloudfront_origin_security_group_id" {
  value = aws_security_group.cloudfront_origin.id
}

output "db_security_group_id" {
  value = aws_security_group.db.id
}
