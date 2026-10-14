variable "state_passphrase" {
  type        = string
  sensitive   = true
  nullable    = false
  description = "State and plan encryption passphrase from TF_VAR_state_passphrase."
}

variable "zone_id" {
  type        = string
  nullable    = false
  description = "Cloudflare zone ID for aboutme.vn."
  validation {
    condition     = can(regex("^[0-9a-f]{32}$", var.zone_id))
    error_message = "Supply the 32-character Cloudflare zone ID for aboutme.vn."
  }
}

variable "host_ipv4" {
  type        = string
  nullable    = false
  description = "Host floating IPv4 address."
  validation {
    condition     = can(regex("^[0-9]{1,3}(\\.[0-9]{1,3}){3}$", var.host_ipv4)) && can(cidrnetmask("${var.host_ipv4}/32"))
    error_message = "Supply an IPv4 address without a prefix or port."
  }
}

variable "dkim_values" {
  type        = object({ dkim = string, etrans = string, google_dkim = string })
  nullable    = false
  sensitive   = true
  description = "Exact Cloudflare TXT content for the three DKIM records, including TXT quoting."
  validation {
    condition     = alltrue([for value in values(var.dkim_values) : try(length(trimspace(value)) > 0, false)])
    error_message = "All three DKIM TXT values must be nonempty."
  }
}

variable "google_verification_name" {
  type        = string
  nullable    = false
  description = "Full Google verification record name under aboutme.vn."
  validation {
    condition     = can(regex("^[a-zA-Z0-9_-]+\\.aboutme\\.vn$", var.google_verification_name))
    error_message = "Supply a verification label followed by .aboutme.vn."
  }
}

variable "google_verification_target" {
  type        = string
  nullable    = false
  description = "Google verification CNAME target without a trailing dot."
  validation {
    condition     = can(regex("^([a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?\\.)+[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?$", var.google_verification_target))
    error_message = "Supply a nonempty DNS hostname without a trailing dot."
  }
}

variable "record_ttls" {
  type        = object({ caa = number, mx = number, spf = number, dmarc = number, dkim = number, etrans = number, google_dkim = number, google_verification = number })
  nullable    = false
  description = "Existing TTLs for non-address records; 1 means automatic."
  validation {
    condition     = alltrue([for ttl in values(var.record_ttls) : try(ttl == floor(ttl) && (ttl == 1 || (ttl >= 60 && ttl <= 86400)), false)])
    error_message = "Each TTL must be 1 (automatic) or an integer from 60 to 86400."
  }
}

variable "mx_priority" {
  type        = number
  nullable    = false
  description = "Existing Google Workspace MX priority."
  validation {
    condition     = var.mx_priority == floor(var.mx_priority) && var.mx_priority >= 0 && var.mx_priority <= 65535
    error_message = "MX priority must be an integer from 0 to 65535."
  }
}

variable "record_ids" {
  type        = map(string)
  default     = {}
  nullable    = false
  description = "Existing Cloudflare record IDs for one-time imports; empty after adoption."
  validation {
    condition = alltrue([
      for key, id in var.record_ids : contains(["apex", "www", "caa", "mx", "spf", "dmarc", "dkim", "etrans", "google_dkim", "google_verification"], key) && can(regex("^[0-9a-f]{32}$", id))
    ])
    error_message = "Use known inventory keys and 32-character Cloudflare record IDs."
  }
}
