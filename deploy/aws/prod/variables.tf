variable "account_id" {
  type        = string
  description = "AWS account ID"
}

variable "state_kms_key_arn" {
  type        = string
  description = "KMS key that encrypts state and plans, from deploy/aws/bootstrap"
}
