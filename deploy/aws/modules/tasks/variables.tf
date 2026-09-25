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

# Off until a healthy v0.4.7 or later release raises the production
# minimum-release fence to numeric 4007. See
# docs/design/passkey-release-fence.md, "Authenticator-app key
# re-encryption".
variable "totp_enrollment_enabled" {
  type        = bool
  description = "TOTP_ENROLLMENT_ENABLED for the server"
}

# Chooses which protected totp/key-<slot> parameter supplies
# TOTP_ACTIVE_KEY. See docs/design/totp-key-management.md, "Key ring".
variable "totp_active_key_slot" {
  type        = string
  description = "Which totp/key-<slot> parameter supplies TOTP_ACTIVE_KEY: \"a\" or \"b\""
  validation {
    condition     = contains(["a", "b"], var.totp_active_key_slot)
    error_message = "totp_active_key_slot must be \"a\" or \"b\"."
  }
}

# Empty keeps TOTP_PREVIOUS_KEY unset, so the task definition names no
# parameter for it and ECS never fails on a missing one.
variable "totp_previous_key_slot" {
  type        = string
  description = "Which totp/key-<slot> parameter supplies TOTP_PREVIOUS_KEY: \"\", \"a\", or \"b\""
  validation {
    condition     = contains(["", "a", "b"], var.totp_previous_key_slot)
    error_message = "totp_previous_key_slot must be \"\", \"a\", or \"b\"."
  }
  validation {
    condition     = var.totp_previous_key_slot == "" || var.totp_previous_key_slot != var.totp_active_key_slot
    error_message = "totp_previous_key_slot must differ from totp_active_key_slot."
  }
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

variable "image_server" {
  type = string
}

variable "image_web" {
  type = string
}

variable "image_caddy" {
  type = string
}
