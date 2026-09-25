variable "name" {
  type = string
}

variable "public_subnet_id" {
  type = string
}

variable "security_group_ids" {
  type = list(string)
}

variable "instance_profile_name" {
  type = string
}

variable "task_definition_arns" {
  type        = map(string)
  description = "Initial task definition per service name suffix"
}
