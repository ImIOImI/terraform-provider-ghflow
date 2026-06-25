# End-to-end example: commit a file to a new branch, open a PR, then merge it.
#
# These three resources chain via references, so OpenTofu/Terraform orders them
# correctly: commit -> pull_request -> pr_merge.

locals {
  owner = "ImIOImI"
  repo  = "demo-repo"
}

# 1. Commit a file to a feature branch (created from main if it doesn't exist).
resource "ghflow_commit" "config" {
  owner          = local.owner
  repository     = local.repo
  branch         = "automation/update-config"
  from_branch    = "main"
  path           = "config/app.yaml"
  content        = file("${path.module}/app.yaml")
  commit_message = "chore: update app config via ghflow"
  author_name    = "ghflow-bot"
  author_email   = "automation@example.com"
}

# 2. Open a PR from the feature branch into main.
resource "ghflow_pull_request" "config" {
  owner      = local.owner
  repository = local.repo
  title      = "chore: update app config"
  body       = "Automated config update.\n\nCommit: ${ghflow_commit.config.commit_sha}"
  head_ref   = ghflow_commit.config.branch
  base_ref   = "main"

  # Leave the PR open if the resource is destroyed (default closes it).
  close_on_destroy = true
}

# 3. Merge the PR. required_head_sha guards against the branch moving since plan.
resource "ghflow_pr_merge" "config" {
  owner             = local.owner
  repository        = local.repo
  number            = ghflow_pull_request.config.number
  merge_method      = "squash"
  commit_title      = "chore: update app config (#${ghflow_pull_request.config.number})"
  required_head_sha = ghflow_pull_request.config.head_sha
}

output "merge_commit_sha" {
  value = ghflow_pr_merge.config.merge_commit_sha
}
