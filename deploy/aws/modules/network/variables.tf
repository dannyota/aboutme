variable "name" {
  type = string
}

variable "cloudflare_ipv4_cidrs" {
  type        = list(string)
  description = "Cloudflare edge ranges allowed to reach the host on 443"
}
