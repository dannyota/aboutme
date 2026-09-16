locals {
  name            = "aboutme-prod"
  cloudflare_ipv4 = sort(jsondecode(data.http.cloudflare_ips.response_body).result.ipv4_cidrs)
}

# Cloudflare publishes its edge ranges without authentication.
data "http" "cloudflare_ips" {
  url = "https://api.cloudflare.com/client/v4/ips"

  lifecycle {
    postcondition {
      condition     = jsondecode(self.response_body).success && length(jsondecode(self.response_body).result.ipv4_cidrs) > 0
      error_message = "Cloudflare did not return its IPv4 ranges."
    }
  }
}

module "network" {
  source                = "../modules/network"
  name                  = local.name
  cloudflare_ipv4_cidrs = local.cloudflare_ipv4
}

module "data" {
  source               = "../modules/data"
  name                 = local.name
  private_subnet_ids   = module.network.private_subnet_ids
  db_security_group_id = module.network.db_security_group_id
  media_bucket_name    = var.media_bucket_name
}
