# terraform-provider-ghflow

An OpenTofu/Terraform provider that drives the GitHub **commit → pull request → merge** flow as managed
resources. Built with the [Terraform Plugin Framework][framework] and the same plugin protocol (v6) used by
both `tofu` and `terraform` — a single build runs under either CLI and publishes to either registry.

> **Why "terraform-" in the name?** The provider plugin SDKs (`terraform-plugin-framework`, …) are still
> HashiCorp-published and MPL-2.0 licensed; the BSL relicense affected Terraform *core*, not these libraries.
> OpenTofu has no provider SDK of its own and recommends these exact libraries. The `terraform-provider-`
> binary prefix is a hard registry requirement for **both** registries, so the name is correct for OpenTofu.

## Resources

| Resource | Purpose |
|----------|---------|
| `ghflow_commit` | Commit a single file to a branch (optionally creating the branch from another). |
| `ghflow_pull_request` | Open a pull request; update title/body/base in place; optionally close on destroy. |
| `ghflow_pr_merge` | Merge a pull request (`merge` / `squash` / `rebase`), optionally pinned to a head SHA. |

The `ghflow_*` prefix intentionally avoids colliding with the official [`integrations/github`][igh] provider's
`github_*` resources, so the two can coexist in one configuration.

## Data sources

| Data source | Purpose |
|-------------|---------|
| `ghflow_ci_status` | Waits for CI on a ref to finish and reports whether all checks are green. Considers both check runs and the combined commit status. Use it to gate a merge. |

`ghflow_ci_status` **always waits** (up to `timeout`) for checks to reach a terminal state. By default every
check gates the result; narrow it with `required_checks` (allowlist) or `ignore_checks` (denylist). If anything
is red and `error_on_failure` is true (the default), the read errors — which fails the plan/apply. Point `ref`
at a computed value (e.g. `ghflow_commit.commit_sha`) so the wait happens at **apply** time.

```hcl
# Gate a merge on green CI: wait for checks on the PR head, then merge.
data "ghflow_ci_status" "gate" {
  owner           = "ImIOImI"
  repository      = "demo-repo"
  ref             = ghflow_commit.config.commit_sha # computed -> waits at apply
  timeout         = "30m"
  required_checks = ["build", "test"] # optional: only gate on these
}

resource "ghflow_pr_merge" "config" {
  owner             = "ImIOImI"
  repository        = "demo-repo"
  number            = ghflow_pull_request.config.number
  required_head_sha = ghflow_pull_request.config.head_sha
  # depends_on forces the merge to wait until the CI gate read succeeds.
  depends_on = [data.ghflow_ci_status.gate]
}
```

## Usage

```hcl
terraform {
  required_providers {
    ghflow = {
      source = "registry.opentofu.org/ImIOImI/ghflow"
    }
  }
}

provider "ghflow" {
  token = var.github_token # or set GITHUB_TOKEN
}

resource "ghflow_commit" "config" {
  owner          = "ImIOImI"
  repository     = "demo-repo"
  branch         = "automation/update-config"
  from_branch    = "main"
  path           = "config/app.yaml"
  content        = file("${path.module}/app.yaml")
  commit_message = "chore: update app config"
}

resource "ghflow_pull_request" "config" {
  owner      = "ImIOImI"
  repository = "demo-repo"
  title      = "chore: update app config"
  head_ref   = ghflow_commit.config.branch
  base_ref   = "main"
}

resource "ghflow_pr_merge" "config" {
  owner             = "ImIOImI"
  repository        = "demo-repo"
  number            = ghflow_pull_request.config.number
  merge_method      = "squash"
  required_head_sha = ghflow_pull_request.config.head_sha
}
```

See [`examples/resources/full-flow`](./examples/resources/full-flow) for a runnable example.

## Authentication

| Setting | Provider attribute | Environment variable |
|---------|--------------------|----------------------|
| Token (PAT) | `token` | `GITHUB_TOKEN` |
| API base URL (GHES) | `base_url` | `GITHUB_BASE_URL` |

A fine-grained PAT needs **Contents: Read & write** and **Pull requests: Read & write** on the target repos.

## Lifecycle semantics — read this

These resources model imperative git/GitHub actions, which do not map cleanly onto desired-state semantics.
The provider makes the following explicit, documented choices:

| Resource | Change to inputs | `destroy` |
|----------|------------------|-----------|
| `ghflow_commit` | Forces a **new commit** (replacement). | Removes from state only — git history is append-only, the commit is **not** reverted. |
| `ghflow_pull_request` | `title`/`body`/`base_ref` update in place; `owner`/`repository`/`head_ref` replace. | Closes the PR (unless `close_on_destroy = false`); never deletes it (the API can't). |
| `ghflow_pr_merge` | Forces a **new merge** (replacement). | Removes from state only — a merge **cannot** be undone. |

`required_head_sha` on `ghflow_pr_merge` passes GitHub's `sha` parameter, so the merge fails loudly if the
branch advanced between plan and apply rather than silently merging unexpected commits.

## Local development

```bash
make build        # build ./terraform-provider-ghflow
make test         # unit tests
make dev-install  # build + print the dev_overrides block for ~/.tofurc
```

With `dev_overrides` configured, run `tofu plan` directly against a checkout — no `init` needed.

## Publishing to the OpenTofu Registry

1. **Generate a GPG signing key** and add the public key to your GitHub account. Export the private key and
   add repo secrets `GPG_PRIVATE_KEY` and `PASSPHRASE`.
2. **Tag a release**: `git tag v0.1.0 && git push origin v0.1.0`. The [`release`](.github/workflows/release.yml)
   workflow runs GoReleaser, producing signed archives + `SHA256SUMS` + manifest on a GitHub Release.
3. **Submit to the registry**: open a PR to [`opentofu/registry`][otregistry] adding the provider under your
   namespace (`ImIOImI/ghflow`) with the GPG public key. The registry indexes future tags automatically.
   (To also list on the Terraform Registry, connect the repo at registry.terraform.io — same release artifacts.)

[framework]: https://developer.hashicorp.com/terraform/plugin/framework
[igh]: https://registry.terraform.io/providers/integrations/github/latest/docs
[otregistry]: https://github.com/opentofu/registry
