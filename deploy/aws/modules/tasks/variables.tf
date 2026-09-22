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

variable "ses_from_name" {
  type = string
}

variable "ses_configuration_set" {
  type = string
}

# Email-and-password sign-up. Off keeps pending registrations verifiable and
# every other password route working.
variable "password_registration_enabled" {
  type        = bool
  description = "PASSWORD_REGISTRATION_ENABLED for the server"
}

# Off until a healthy capable release raises the production minimum-release
# fence. See docs/design/passkey-release-fence.md.
variable "passkey_enrollment_enabled" {
  type        = bool
  description = "PASSKEY_ENROLLMENT_ENABLED for the server"
}

# Only Google's credentials are wired, so only Google may be enabled here.
variable "provider_login_enabled" {
  type        = string
  description = "PROVIDER_LOGIN_ENABLED for the server: \"\" (off) or \"google\""
  validation {
    condition     = contains(["", "google"], var.provider_login_enabled)
    error_message = "provider_login_enabled must be \"\" or \"google\"; no other provider has production credentials wired."
  }
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
