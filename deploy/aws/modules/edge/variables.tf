variable "name" {
  type = string
}

variable "alerts_topic_arn" {
  type = string
}

variable "origin_domain_name" {
  type        = string
  description = "The host's public DNS name; CloudFront's HTTPS origin"
}

variable "alerts_topic_arn_us_east_1" {
  type        = string
  description = "SNS topic in us-east-1 for alarms that only exist there"
}

variable "waf_block" {
  type        = bool
  description = "false: every WAF rule only counts; true: rules block"
}

variable "transparency_enabled" {
  type        = bool
  description = "Serve /.well-known/deployment.json from the transparency bucket"
}

variable "transparency_bucket_domain_name" {
  type        = string
  description = "The transparency bucket's regional domain name"
}
