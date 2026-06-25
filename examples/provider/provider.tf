terraform {
  required_providers {
    ghflow = {
      source = "registry.opentofu.org/ImIOImI/ghflow"
    }
  }
}

provider "ghflow" {
  # token may be omitted and read from the GITHUB_TOKEN environment variable.
  token = var.github_token

  # base_url = "https://ghe.example.com/" # for GitHub Enterprise Server
}

variable "github_token" {
  type      = string
  sensitive = true
  default   = null
}
