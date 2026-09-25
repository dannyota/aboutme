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

module "identity" {
  source                  = "../modules/identity"
  name                    = local.name
  account_id              = var.account_id
  media_bucket_arn        = module.data.media_bucket_arn
  db_master_secret_arn    = module.data.db_master_secret_arn
  ses_from_address        = var.ses_from_address
  release_fence_table_arn = module.data.release_fence_table_arn
  operator_principal_arn  = var.operator_principal_arn
}

module "tasks" {
  source                        = "../modules/tasks"
  name                          = local.name
  account_id                    = var.account_id
  db_endpoint                   = module.data.db_endpoint
  db_master_secret_arn          = module.data.db_master_secret_arn
  media_bucket_name             = module.data.media_bucket_name
  ses_from_address              = var.ses_from_address
  ses_from_name                 = var.ses_from_name
  ses_configuration_set         = var.ses_configuration_set
  provider_login_enabled        = var.provider_login_enabled
  password_registration_enabled = var.password_registration_enabled
  passkey_enrollment_enabled    = var.passkey_enrollment_enabled
  totp_enrollment_enabled       = var.totp_enrollment_enabled
  totp_active_key_slot          = var.totp_active_key_slot
  totp_previous_key_slot        = var.totp_previous_key_slot
  exec_role_arns                = module.identity.exec_role_arns
  app_task_role_arn             = module.identity.app_task_role_arn
  jobs_task_role_arn            = module.identity.jobs_task_role_arn
  log_group_name                = module.identity.log_group_name
  cloudflare_ipv4_cidrs         = local.cloudflare_ipv4
  edges                         = var.edges
  image_server                  = var.image_server
  image_web                     = var.image_web
  image_caddy                   = var.image_caddy
}

module "host" {
  source                = "../modules/host"
  name                  = local.name
  public_subnet_id      = module.network.public_subnet_id
  security_group_ids    = [module.network.host_security_group_id, module.network.cloudfront_origin_security_group_id]
  instance_profile_name = module.identity.instance_profile_name
  task_definition_arns = {
    app         = module.tasks.app_task_definition_arn
    web         = module.tasks.web_task_definition_arn
    maintenance = module.tasks.maintenance_task_definition_arn
  }
}

module "ops" {
  source = "../modules/ops"
  providers = {
    aws           = aws
    aws.us_east_1 = aws.us_east_1
  }
  name                 = local.name
  account_id           = var.account_id
  alarm_email          = var.alarm_email
  cluster_arn          = module.host.cluster_arn
  cluster_name         = module.host.cluster_name
  instance_id          = module.host.instance_id
  db_instance_id       = module.data.db_instance_id
  jobs_task_family_arn = module.tasks.jobs_task_family_arn
  jobs_exec_role_arn   = module.identity.exec_role_arns["jobs"]
  jobs_task_role_arn   = module.identity.jobs_task_role_arn
  site_alarm_enabled   = var.site_alarm_enabled
}

module "edge" {
  source = "../modules/edge"
  providers = {
    aws           = aws
    aws.us_east_1 = aws.us_east_1
  }
  name                       = local.name
  alerts_topic_arn           = module.ops.alerts_topic_arn
  origin_domain_name         = module.host.public_dns
  alerts_topic_arn_us_east_1 = module.ops.alerts_topic_arn_us_east_1
  waf_block                  = var.waf_block
}
