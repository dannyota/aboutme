output "schedule_group" {
  value = aws_scheduler_schedule_group.jobs.name
}

output "alerts_topic_arn" {
  value = aws_sns_topic.alerts.arn
}
