variable "name" {
  type = string
}

variable "account_id" {
  type = string
}

variable "media_bucket_arn" {
  type = string
}

variable "db_master_secret_arn" {
  type = string
}

variable "ses_from_address" {
  type = string
}

variable "release_fence_table_arn" {
  type        = string
  description = "DynamoDB minimum-release fence table ARN, from the data module"
}

variable "operator_principal_arn" {
  type        = string
  description = "Same-account canonical IAM ARN trusted to assume aboutme-prod-operator"
}
