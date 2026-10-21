variable "run_id" {
  type = string
  validation {
    condition     = can(regex("^[0-9a-f]{16}$", var.run_id))
    error_message = "A fresh run identity is required."
  }
}
variable "project_id" { type = string }
variable "network_id" { type = string }
variable "subnet_id" { type = string }
variable "zone_id" { type = string }
variable "operator_cidrs" {
  type = set(string)
  validation {
    condition     = length(var.operator_cidrs) > 0 && alltrue([for c in var.operator_cidrs : can(cidrnetmask(c)) && endswith(c, "/32")])
    error_message = "Operator addresses must be IPv4 host CIDRs."
  }
}
variable "cloud_init" {
  type      = string
  sensitive = true
}
variable "state_passphrase" {
  type      = string
  sensitive = true
  validation {
    condition     = length(var.state_passphrase) >= 16
    error_message = "An encrypted state passphrase is required."
  }
}
