variable "name" {
  type = string
}

variable "account_id" {
  type = string
}

variable "alarm_email" {
  type = string
}

variable "cluster_arn" {
  type = string
}

variable "cluster_name" {
  type = string
}

variable "instance_id" {
  type = string
}

variable "db_instance_id" {
  type = string
}

variable "jobs_task_family_arn" {
  type = string
}

variable "jobs_exec_role_arn" {
  type = string
}

variable "jobs_task_role_arn" {
  type = string
}

variable "site_alarm_enabled" {
  type        = bool
  description = "Send site-down mail; off until the first deploy is healthy"
}
