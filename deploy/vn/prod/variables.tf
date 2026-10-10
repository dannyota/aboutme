variable "project_id" {
  type        = string
  description = "vServer project ID (pro-...)"
}

variable "zone_id" {
  type        = string
  description = "Availability zone in region hcm-3"
  default     = "HCM03-1A"
}

variable "flavor_id" {
  type        = string
  description = "Server flavor (s2-general-2x4)"
  default     = "flav-cbb11ae8-f4b7-4e25-96a6-c30bcfcccece"
}

variable "image_id" {
  type        = string
  description = "Server image (Ubuntu 24.04 UEFI)"
  default     = "img-34440a82-92fb-40bc-b79c-b1a2b49b93de"
}

variable "volume_type_id" {
  type        = string
  description = "Volume type for the root and data disks (SSD 3000 IOPS)"
  default     = "vtype-61c3fc5b-f4e9-45b4-8957-8aa7b6029018"
}

variable "root_disk_gb" {
  type        = number
  description = "Root disk size in GB"
  default     = 30
}

variable "data_disk_gb" {
  type        = number
  description = "Data volume size in GB"
  default     = 20
}

variable "data_volume_encryption_type" {
  type        = string
  description = "Encryption key type of the data volume. This is the single switch for data volume encryption."
  default     = "aes-xts-plain64_256"

  validation {
    condition     = contains(["aes-xts-plain64_256", "aes-xts-plain64_128"], var.data_volume_encryption_type)
    error_message = "data_volume_encryption_type must be aes-xts-plain64_256 or aes-xts-plain64_128."
  }
}

variable "network_cidr" {
  type        = string
  description = "CIDR of the vServer network (/16)"
  default     = "10.76.0.0/16"
}

variable "subnet_cidr" {
  type        = string
  description = "CIDR of the host subnet"
  default     = "10.76.1.0/24"
}

variable "ssh_allowlist" {
  type        = list(string)
  description = "CIDRs allowed to reach TCP 22. Never 0.0.0.0/0."

  validation {
    condition     = length(var.ssh_allowlist) > 0
    error_message = "ssh_allowlist must not be empty."
  }
  validation {
    condition     = alltrue([for c in var.ssh_allowlist : can(cidrhost(c, 0))])
    error_message = "Every ssh_allowlist entry must be a CIDR."
  }
  validation {
    condition     = alltrue([for c in var.ssh_allowlist : c != "0.0.0.0/0"])
    error_message = "ssh_allowlist must not contain 0.0.0.0/0."
  }
}

variable "admin_ssh_public_key" {
  type        = string
  description = "Hardware-backed admin public key (docs/design/vietnam-production.md, Host)"

  validation {
    condition     = startswith(var.admin_ssh_public_key, "sk-ssh-ed25519@openssh.com ")
    error_message = "admin_ssh_public_key must be an sk-ssh-ed25519@openssh.com key."
  }
}

variable "state_passphrase" {
  type        = string
  sensitive   = true
  description = "Passphrase that encrypts state and plans. Set through TF_VAR_state_passphrase."

  validation {
    condition     = length(var.state_passphrase) >= 16
    error_message = "state_passphrase must be at least 16 characters."
  }
}

# The two URLs below are the provider's documented defaults. Whether they still
# answer after the GreenNode domain move is unconfirmed.
variable "token_url" {
  type        = string
  description = "IAM token endpoint"
  default     = "https://iamapis.vngcloud.vn/accounts-api/v2/auth/token"
}

variable "vserver_base_url" {
  type        = string
  description = "vServer API base URL for region hcm-3"
  default     = "https://hcm-3.api.vngcloud.vn/vserver/vserver-gateway"
}
