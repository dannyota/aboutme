output "app_task_definition_arn" {
  value = aws_ecs_task_definition.app.arn
}

output "web_task_definition_arn" {
  value = aws_ecs_task_definition.web.arn
}

output "maintenance_task_definition_arn" {
  value = aws_ecs_task_definition.maintenance.arn
}

output "jobs_task_family_arn" {
  value = aws_ecs_task_definition.jobs.arn_without_revision
}
