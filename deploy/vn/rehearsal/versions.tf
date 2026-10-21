terraform {
  required_version = "= 1.12.6"
  required_providers {
    vngcloud   = { source = "vngcloud/vngcloud", version = "1.3.21" }
    cloudflare = { source = "cloudflare/cloudflare", version = "= 5.27.0" }
  }
  backend "local" {}
  encryption {
    key_provider "pbkdf2" "state" {
      passphrase = var.state_passphrase
    }
    method "aes_gcm" "state" {
      keys = key_provider.pbkdf2.state
    }
    state {
      method   = method.aes_gcm.state
      enforced = true
    }
    plan {
      method   = method.aes_gcm.state
      enforced = true
    }
  }
}
provider "vngcloud" {
  token_url        = "https://iamapis.vngcloud.vn/accounts-api/v2/auth/token"
  vserver_base_url = "https://hcm-3.api.vngcloud.vn/vserver/vserver-gateway"
}
provider "cloudflare" {}
