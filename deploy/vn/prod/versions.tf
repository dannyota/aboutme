# Implements docs/design/vietnam-production.md, "Infrastructure code and state".
terraform {
  required_version = "= 1.12.6"
  required_providers {
    vngcloud = { source = "vngcloud/vngcloud", version = "1.3.21" }
  }

  # vStorage in HCM04 speaks the S3 API. bucket, shared_credentials_files, and
  # use_lockfile come from the ignored backend.hcl.
  backend "s3" {
    key    = "prod/terraform.tfstate"
    region = "HCM04"
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

# The provider reads the service account from the CLIENT_ID and CLIENT_SECRET
# environment variables. No credential is a variable or a file in this root.
provider "vngcloud" {
  token_url        = var.token_url
  vserver_base_url = var.vserver_base_url
}
