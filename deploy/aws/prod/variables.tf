variable "account_id" {
  type        = string
  description = "AWS account ID"
}

variable "state_kms_key_arn" {
  type        = string
  description = "KMS key that encrypts state and plans, from deploy/aws/bootstrap"
}

variable "media_bucket_name" {
  type        = string
  description = "Private media bucket name"
}
