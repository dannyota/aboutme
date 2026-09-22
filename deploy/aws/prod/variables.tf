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

variable "ses_from_name" {
  type    = string
  default = "Danny from aboutme"
}

variable "ses_configuration_set" {
  type    = string
  default = "aboutme-auth"
}

variable "password_registration_enabled" {
  type        = bool
  default     = true
  description = "false turns off email-and-password sign-up, for example while SES is in the sandbox"
}

variable "provider_login_enabled" {
  type        = string
  default     = ""
  description = "\"google\" turns on Google login once its SSM parameters exist; \"\" keeps it off"
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

# The canonical same-account IAM ARN behind the owner's `aws login` session:
# an IAM user or role ARN, never an STS assumed-role session ARN. OpenTofu
# trusts only this ARN as aboutme-prod-operator's assume-role principal. Kept
# in the ignored prod.tfvars.
variable "operator_principal_arn" {
  type        = string
  description = "Same-account IAM user or role ARN trusted to assume aboutme-prod-operator"
  validation {
    condition     = can(regex("^arn:aws:iam::${var.account_id}:(role|user)/[A-Za-z0-9+=,.@_/-]+$", var.operator_principal_arn))
    error_message = "operator_principal_arn must be an IAM role or user ARN in this same account, not an assumed-role session ARN."
  }
}

# Off until a healthy v0.4.2 or later release raises the production
# minimum-release fence; see docs/design/passkey-release-fence.md.
variable "passkey_enrollment_enabled" {
  type        = bool
  default     = false
  description = "PASSKEY_ENROLLMENT_ENABLED for the server"
}
