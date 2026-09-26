variable "name" {
  type = string
}

variable "account_id" {
  type = string
}

variable "distribution_domain_name" {
  type        = string
  description = "The CloudFront distribution's domain name, the alias target"
}

variable "distribution_hosted_zone_id" {
  type        = string
  description = "CloudFront's alias hosted zone ID"
}

variable "certificate_validation_records" {
  type = map(object({
    name  = string
    type  = string
    value = string
  }))
  description = "ACM DNS validation records, keyed by certificate domain"
}

variable "alerts_topic_arn_us_east_1" {
  type        = string
  description = "SNS topic in us-east-1 for the DNSSEC alarms"
}

# Google Workspace's generated values stay in the ignored prod.tfvars, like
# every other generated token (docs/runbooks/email.md).
variable "google_workspace" {
  type = object({
    verification_label  = string
    verification_target = string
    dkim_txt            = string
  })
  description = "Google Workspace domain verification CNAME and google._domainkey TXT value"

  validation {
    condition     = can(regex("^[a-z0-9]+$", var.google_workspace.verification_label))
    error_message = "verification_label is one DNS label, such as the part before .aboutme.vn."
  }
  validation {
    condition     = can(regex("^[a-z0-9-]+\\.dv\\.googlehosted\\.com$", var.google_workspace.verification_target))
    error_message = "verification_target is a <token>.dv.googlehosted.com name without a trailing dot."
  }
  validation {
    condition     = can(regex("^v=DKIM1;[ ]?k=rsa;[ ]?p=[A-Za-z0-9+/]+=*$", var.google_workspace.dkim_txt))
    error_message = "dkim_txt is the whole TXT value, v=DKIM1;k=rsa;p=<key>, joined into one string without quotes."
  }
}
