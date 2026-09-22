output "instance_profile_name" {
  value = aws_iam_instance_profile.instance.name
}

output "exec_role_arns" {
  value = { for k, r in aws_iam_role.exec : k => r.arn }
}

output "app_task_role_arn" {
  value = aws_iam_role.app.arn
}

output "jobs_task_role_arn" {
  value = aws_iam_role.jobs.arn
}

output "log_group_name" {
  value = aws_cloudwatch_log_group.main.name
}

output "operator_role_arn" {
  value = aws_iam_role.operator.arn
}

output "deploy_role_arn" {
  value = aws_iam_role.deploy.arn
}
