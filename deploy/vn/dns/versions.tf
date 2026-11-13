terraform {
  required_version = "= 1.12.6"
  required_providers {
    cloudflare = { source = "cloudflare/cloudflare", version = "= 5.27.0" }
  }

  # vStorage in HCM04 answers a create-only PUT on an existing key with 412, so the S3 lock file works (GreenNode vStorage).
  backend "s3" {
    key          = "dns/terraform.tfstate"
    region       = "HCM04"
    use_lockfile = true
    endpoints = {
      s3 = "https://hcm04.vstorage.vngcloud.vn"
    }
    use_path_style              = true
    skip_credentials_validation = true
    skip_region_validation      = true
    skip_requesting_account_id  = true
    skip_metadata_api_check     = true
    skip_s3_checksum            = true
  }

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

provider "cloudflare" {}
