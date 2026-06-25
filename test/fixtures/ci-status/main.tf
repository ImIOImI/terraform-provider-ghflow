terraform {
  required_providers {
    ghflow = {
      source = "registry.opentofu.org/ImIOImI/ghflow"
    }
  }
}

provider "ghflow" {
  token = var.github_token
}

variable "github_token" {
  type      = string
  sensitive = true
}

variable "owner" {
  type = string
}

variable "repository" {
  type = string
}

variable "ref" {
  type = string
}

variable "error_on_failure" {
  type    = bool
  default = true
}

variable "required_checks" {
  type    = list(string)
  default = []
}

variable "timeout" {
  type    = string
  default = "60s"
}

variable "poll_interval" {
  type    = string
  default = "3s"
}

variable "pull_request_url" {
  type    = string
  default = ""
}

data "ghflow_ci_status" "this" {
  owner            = var.owner
  repository       = var.repository
  ref              = var.ref
  error_on_failure = var.error_on_failure
  required_checks  = var.required_checks
  timeout          = var.timeout
  poll_interval    = var.poll_interval
  pull_request_url = var.pull_request_url
}

output "success" {
  value = data.ghflow_ci_status.this.success
}

output "state" {
  value = data.ghflow_ci_status.this.state
}

output "total_count" {
  value = data.ghflow_ci_status.this.total_count
}

output "failed_checks" {
  value = data.ghflow_ci_status.this.failed_checks
}
