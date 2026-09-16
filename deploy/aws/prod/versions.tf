terraform {
  required_version = "= 1.12.6"
  required_providers {
    aws  = { source = "hashicorp/aws", version = "~> 6.0" }
    http = { source = "hashicorp/http", version = "~> 3.0" }
  }

  # Bucket name comes from the ignored backend.hcl.
  backend "s3" {
    key          = "prod/terraform.tfstate"
    region       = "ap-southeast-1"
    encrypt      = true
    use_lockfile = true
  }

  encryption {
    key_provider "aws_kms" "state" {
      kms_key_id = var.state_kms_key_arn
      region     = "ap-southeast-1"
      key_spec   = "AES_256"
    }
    method "aes_gcm" "state" {
      keys = key_provider.aws_kms.state
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

provider "aws" {
  region              = "ap-southeast-1"
  allowed_account_ids = [var.account_id]
  default_tags {
    tags = { Project = "aboutme", Environment = "prod" }
  }
}

provider "aws" {
  alias               = "us_east_1"
  region              = "us-east-1"
  allowed_account_ids = [var.account_id]
  default_tags {
    tags = { Project = "aboutme", Environment = "prod" }
  }
}
