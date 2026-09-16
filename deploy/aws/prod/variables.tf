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

variable "ses_from_address" {
  type    = string
  default = "danny@aboutme.vn"
}

variable "ses_configuration_set" {
  type    = string
  default = "aboutme-auth"
}

# Initial task definition images. deploy.sh registers later revisions.
variable "image_server" {
  type = string
}

variable "image_web" {
  type = string
}

variable "image_caddy" {
  type = string
}

variable "alarm_email" {
  type        = string
  description = "Where alarm mail goes; kept in the ignored prod.tfvars"
}

variable "site_alarm_enabled" {
  type    = bool
  default = false
}
