variable "project_id" {
  type        = string
  description = "vServer project ID (pro-...)"
}

variable "zone_id" {
  type        = string
  description = "Availability zone in region hcm-3"
  default     = "HCM03-1C"
}

variable "flavor_id" {
  type        = string
  description = "Server flavor (s2-general-2x4)"
  default     = "flav-530ea5cb-6fac-4264-bcad-9e0e0a5ba3fc"
}

variable "image_id" {
  type        = string
  description = "Server image (Ubuntu 24.04 UEFI)"
  default     = "img-34440a82-92fb-40bc-b79c-b1a2b49b93de"
}

variable "volume_type_id" {
  type        = string
  description = "Volume type for the root and data disks (SSD 3000 IOPS)"
  default     = "vtype-e782f8e1-0569-11f0-a0a4-ec2a72332f83"
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

variable "disk_encryption_type" {
  type        = string
  description = "The encryption type of the root disk and the data volume."
  default     = "aes-xts-plain64_256"

  validation {
    condition     = contains(["aes-xts-plain64_256", "aes-xts-plain64_128"], var.disk_encryption_type)
    error_message = "disk_encryption_type must be aes-xts-plain64_256 or aes-xts-plain64_128."
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

variable "ssh_port" {
  type        = number
  description = "SSH listen port"
  default     = 22922

  validation {
    condition     = var.ssh_port >= 1024 && var.ssh_port <= 32767 && var.ssh_port != 22 && floor(var.ssh_port) == var.ssh_port
    error_message = "ssh_port must be an integer from 1024 to 32767 and must not be 22."
  }
}

variable "ssh_allowlist" {
  type        = list(string)
  description = "CIDRs allowed to reach the SSH port"
  default     = ["0.0.0.0/0"]

  validation {
    condition     = length(var.ssh_allowlist) > 0
    error_message = "ssh_allowlist must not be empty."
  }
  validation {
    condition     = alltrue([for c in var.ssh_allowlist : can(cidrhost(c, 0))])
    error_message = "Every ssh_allowlist entry must be a CIDR."
  }
}

variable "drill_network" {
  type = object({
    production_private_cidr = string
    resolver_cidrs          = set(string)
    time_service_cidrs      = set(string)
    package_cidrs           = set(string)
    hcm04_cidrs             = set(string)
  })
  description = "Reviewed IPv4 destinations for optional detached restore-drill security groups"
  default     = null
  nullable    = true

  validation {
    condition = var.drill_network == null ? true : try(
      var.drill_network.production_private_cidr != null &&
      var.drill_network.resolver_cidrs != null &&
      var.drill_network.time_service_cidrs != null &&
      var.drill_network.package_cidrs != null &&
      var.drill_network.hcm04_cidrs != null,
      false,
    )
    error_message = "drill_network fields must not be null."
  }

  validation {
    condition = var.drill_network == null ? true : try(
      cidrnetmask(var.drill_network.production_private_cidr) != "" &&
      tonumber(split("/", var.drill_network.production_private_cidr)[1]) == 32 &&
      (
        cidrcontains("10.0.0.0/8", cidrhost(var.drill_network.production_private_cidr, 0)) ||
        cidrcontains("172.16.0.0/12", cidrhost(var.drill_network.production_private_cidr, 0)) ||
        cidrcontains("192.168.0.0/16", cidrhost(var.drill_network.production_private_cidr, 0))
      ),
      false,
    )
    error_message = "drill_network.production_private_cidr must be a private IPv4 /32."
  }

  validation {
    condition = var.drill_network == null ? true : try(
      length(var.drill_network.resolver_cidrs) > 0 &&
      length(var.drill_network.time_service_cidrs) > 0 &&
      length(var.drill_network.package_cidrs) > 0 &&
      length(var.drill_network.hcm04_cidrs) > 0,
      false,
    )
    error_message = "Every drill_network destination set must contain at least one CIDR."
  }

  validation {
    condition = var.drill_network == null ? true : try(
      alltrue([
        for cidr in setunion(var.drill_network.resolver_cidrs, var.drill_network.time_service_cidrs) : try(
          cidrnetmask(cidr) != "" && tonumber(split("/", cidr)[1]) == 32,
          false,
        )
      ]),
      false,
    )
    error_message = "Every resolver and time-service destination must be an IPv4 /32."
  }

  validation {
    condition = var.drill_network == null ? true : try(
      alltrue([
        for cidr in setunion(var.drill_network.package_cidrs, var.drill_network.hcm04_cidrs) : try(
          cidrnetmask(cidr) != "" && alltrue([
            for reserved in [
              "0.0.0.0/8",
              "10.0.0.0/8",
              "100.64.0.0/10",
              "127.0.0.0/8",
              "169.254.0.0/16",
              "172.16.0.0/12",
              "192.0.0.0/24",
              "192.0.2.0/24",
              "192.88.99.0/24",
              "192.168.0.0/16",
              "198.18.0.0/15",
              "198.51.100.0/24",
              "203.0.113.0/24",
              "224.0.0.0/4",
              "240.0.0.0/4",
              ] : !(
              cidrcontains(reserved, cidrhost(cidr, 0)) ||
              cidrcontains(cidr, cidrhost(reserved, 0))
            )
          ]),
          false,
        )
      ]),
      false,
    )
    error_message = "Package and HCM04 destinations must be public IPv4 CIDRs that do not overlap private or special-use ranges."
  }

  validation {
    condition = var.drill_network == null ? true : try(
      alltrue([
        for cidr in setunion(
          var.drill_network.resolver_cidrs,
          var.drill_network.time_service_cidrs,
          var.drill_network.package_cidrs,
          var.drill_network.hcm04_cidrs,
          ) : try(
          !cidrcontains(cidr, cidrhost(var.drill_network.production_private_cidr, 0)) &&
          !cidrcontains(cidr, "169.254.169.254"),
          false,
        )
      ]),
      false,
    )
    error_message = "Drill egress destinations must not include production or the metadata endpoint."
  }
}

variable "admin_ssh_public_key" {
  type        = string
  description = "Admin ssh-ed25519 public key (docs/design/vietnam-production.md, Host)"

  validation {
    condition     = startswith(var.admin_ssh_public_key, "ssh-ed25519 ")
    error_message = "admin_ssh_public_key must be an ssh-ed25519 key."
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
