# DNS record ownership follows docs/adr/0026-vietnam-hosted-production.md.
terraform {
  required_version = "= 1.12.6"
  required_providers {
    cloudflare = { source = "cloudflare/cloudflare", version = "= 5.27.0" }
  }

  # vStorage in HCM04 speaks the S3 API and answers a create-only PUT on an
  # existing key with 412, so the lock file works. bucket and
  # shared_credentials_files come from the ignored backend.hcl.
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

  # The passphrase comes from TF_VAR_state_passphrase, never from a file.
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

# Authentication comes only from CLOUDFLARE_API_TOKEN.
provider "cloudflare" {}
