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

variable "base_branch" {
  type    = string
  default = "main"
}

# Unique per run so reruns against the persistent repo never collide.
variable "branch" {
  type = string
}

# Unique per run so the committed file always differs from what's on main,
# guaranteeing a non-empty diff that can actually be merged.
variable "marker" {
  type = string
}

resource "ghflow_commit" "test" {
  owner          = var.owner
  repository     = var.repository
  branch         = var.branch
  from_branch    = var.base_branch
  path           = "ghflow-e2e.txt"
  content        = "created by ghflow e2e test\nrun: ${var.marker}\n"
  commit_message = "test: ghflow e2e commit (${var.marker})"
}

resource "ghflow_pull_request" "test" {
  owner      = var.owner
  repository = var.repository
  title      = "ghflow e2e PR (${var.marker})"
  body       = "Automated end-to-end test PR."
  head_ref   = ghflow_commit.test.branch
  base_ref   = var.base_branch
}

resource "ghflow_pr_merge" "test" {
  owner             = var.owner
  repository        = var.repository
  number            = ghflow_pull_request.test.number
  merge_method      = "squash"
  required_head_sha = ghflow_pull_request.test.head_sha
}

output "pr_number" {
  value = ghflow_pull_request.test.number
}

output "merged" {
  value = ghflow_pr_merge.test.merged
}

output "merge_commit_sha" {
  value = ghflow_pr_merge.test.merge_commit_sha
}
