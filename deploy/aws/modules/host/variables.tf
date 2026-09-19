variable "name" {
  type = string
}

variable "public_subnet_id" {
  type = string
}

variable "host_security_group_id" {
  type = string
}

variable "instance_profile_name" {
  type = string
}

variable "task_definition_arns" {
  type        = map(string)
  description = "Initial task definition per service name suffix"
}
