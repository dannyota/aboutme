variable "name" {
  type = string
}

variable "account_id" {
  type = string
}

variable "db_endpoint" {
  type = string
}

variable "db_master_secret_arn" {
  type = string
}

variable "media_bucket_name" {
  type = string
}

variable "ses_from_address" {
  type = string
}

variable "ses_configuration_set" {
  type = string
}

variable "exec_role_arns" {
  type = map(string)
}

variable "app_task_role_arn" {
  type = string
}

variable "jobs_task_role_arn" {
  type = string
}

variable "log_group_name" {
  type = string
}

variable "cloudflare_ipv4_cidrs" {
  type = list(string)
}

variable "image_server" {
  type = string
}

variable "image_web" {
  type = string
}

variable "image_caddy" {
  type = string
}
