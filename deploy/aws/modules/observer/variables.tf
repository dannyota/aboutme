variable "name" {
  type = string
}

variable "account_id" {
  type = string
}

variable "bucket_name" {
  type        = string
  description = "Private bucket that holds deployment.json and the verification cache"
}

variable "distribution_arn" {
  type        = string
  description = "The CloudFront distribution that may read deployment.json"
}

variable "alerts_topic_arn" {
  type = string
}

variable "image_digest" {
  type        = string
  default     = ""
  description = "sha256 digest of the observer image observer.sh copied into ECR; \"\" creates no function, schedule, or alarm yet"
  validation {
    condition     = var.image_digest == "" || can(regex("^sha256:[0-9a-f]{64}$", var.image_digest))
    error_message = "image_digest must be empty or sha256:<64 hex>."
  }
}

variable "reserved_concurrency" {
  type        = number
  default     = null
  description = "Reserved concurrency for the function; null leaves it unreserved, which an account at the minimum Lambda concurrency quota requires"
}
