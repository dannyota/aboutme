output "schedule_group" {
  value = aws_scheduler_schedule_group.jobs.name
}

output "alerts_topic_arn" {
  value = aws_sns_topic.alerts.arn
}

output "alerts_topic_arn_us_east_1" {
  value = aws_sns_topic.alerts_us_east_1.arn
}
