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

variable "password_registration_enabled" {
  type        = bool
  description = "PASSWORD_REGISTRATION_ENABLED for the server"
}

variable "passkey_enrollment_enabled" {
  type        = bool
  description = "PASSKEY_ENROLLMENT_ENABLED for the server"
}

variable "preview_card_enabled" {
  type        = bool
  description = "PREVIEW_CARD_ENABLED for the server"
}

variable "totp_enrollment_enabled" {
  type        = bool
  description = "TOTP_ENROLLMENT_ENABLED for the server"
}

variable "sign_in_to_view_enabled" {
  type        = bool
  description = "SIGN_IN_TO_VIEW_ENABLED for the server"
}

variable "totp_active_key_slot" {
  type        = string
  description = "Which totp/key-<slot> parameter supplies TOTP_ACTIVE_KEY: \"a\" or \"b\""
  validation {
    condition     = contains(["a", "b"], var.totp_active_key_slot)
    error_message = "totp_active_key_slot must be \"a\" or \"b\"."
  }
}

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

variable "provider_login_enabled" {
  type        = string
  description = "PROVIDER_LOGIN_ENABLED for the server: \"\" (off), \"google\", or \"google,linkedin\""
  validation {
    condition     = contains(["", "google", "google,linkedin"], var.provider_login_enabled)
    error_message = "provider_login_enabled must be \"\", \"google\", or \"google,linkedin\"; no other provider has production credentials wired."
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
