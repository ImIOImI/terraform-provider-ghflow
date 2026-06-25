# terraform-provider-ghflow

A provider that drives the GitHub **commit → pull request → merge** flow as managed resources, plus a
`ghflow_ci_status` data source that waits for CI and can gate a merge on green checks.

It is built on the [Terraform Plugin Framework][framework] and the plugin protocol (v6) shared by both
`tofu` and `terraform` — a single build runs under either CLI and publishes to either registry. This
repository contains the provider, its resources and data source, runnable [examples](./examples), and
generated [docs](./docs).

## Requirements

- [OpenTofu](https://opentofu.org/docs/intro/install/) >= 1.6 _or_ [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0
- [Go](https://golang.org/doc/install) >= 1.24 (to build the provider)
- A GitHub token (PAT). A fine-grained token needs **Contents: Read & write** and **Pull requests: Read & write**
  on the target repositories.

## Building the Provider

1. Clone the repository.
2. Enter the repository directory.
3. Build the provider using the Go `install` command:

```shell
go install
```

Or use the Makefile: `make build` produces `./terraform-provider-ghflow`.

## Adding Dependencies

This provider uses [Go modules](https://go.dev/wiki/Modules). To add a new dependency
`github.com/author/dependency`:

```shell
go get github.com/author/dependency
go mod tidy
```

Then commit the changes to `go.mod` and `go.sum`.

## Using the Provider

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
```

### Authentication

| Setting | Provider attribute | Environment variable |
|---------|--------------------|----------------------|
| Token (PAT) | `token` | `GITHUB_TOKEN` |
| API base URL (GitHub Enterprise) | `base_url` | `GITHUB_BASE_URL` |

### Resources

| Resource | Purpose |
|----------|---------|
| `ghflow_commit` | Commit a single file to a branch (optionally creating the branch from another). |
| `ghflow_pull_request` | Open a pull request; update title/body/base in place; optionally close on destroy. |
| `ghflow_pr_merge` | Merge a pull request (`merge` / `squash` / `rebase`), optionally pinned to a head SHA. |

The `ghflow_*` prefix intentionally avoids colliding with the official [`integrations/github`][igh] provider's
`github_*` resources, so the two can coexist in one configuration.

#### Lifecycle semantics — read this

These resources model imperative git/GitHub actions, which do not map cleanly onto desired-state semantics.
The provider makes the following explicit, documented choices:

| Resource | Change to inputs | `destroy` |
|----------|------------------|-----------|
| `ghflow_commit` | Forces a **new commit** (replacement). | Removes from state only — git history is append-only, the commit is **not** reverted. |
| `ghflow_pull_request` | `title`/`body`/`base_ref` update in place; `owner`/`repository`/`head_ref` replace. | Closes the PR (unless `close_on_destroy = false`); never deletes it (the API can't). |
| `ghflow_pr_merge` | Forces a **new merge** (replacement). | Removes from state only — a merge **cannot** be undone. |

`required_head_sha` on `ghflow_pr_merge` passes GitHub's `sha` parameter, so the merge fails loudly if the
branch advanced between plan and apply rather than silently merging unexpected commits.

### Data source: `ghflow_ci_status`

Waits for CI on a ref and reports whether all checks are green. It considers **both** check runs (GitHub
Actions / Check API) and the combined commit status, mirroring branch protection.

- **Always waits** (up to `timeout`, default `30m`) for checks to reach a terminal state, polling every `poll_interval` (default `10s`).
- By default every check gates the result. Narrow it with `required_checks` (allowlist) or `ignore_checks`
  (denylist). A required check that never appears keeps the result pending until `timeout`.
- Point `ref` at a **computed** value (e.g. `ghflow_commit.commit_sha`) so the wait happens at **apply** time.

**Failure behavior depends on `error_on_failure`:**

| `error_on_failure` | When CI is red or times out |
|--------------------|------------------------------|
| `true` (default) | The read **raises an error** and fails the plan/apply (the gate). It does *not* return `success = false`. |
| `false` | The read **succeeds**; inspect `success` / `state` / `failed_checks` yourself. A warning is still emitted. |

Set `pull_request_url` (typically `ghflow_pull_request.html_url`) to include the PR link in that error, in the
warning, and in a `tflog` line — so a failed run points straight at the PR to review manually:

```
Error: CI is not green

  CI on ImIOImI/demo-repo@9577b4d… is "failure". Failed checks: [build].
  Open the pull request for manual review: https://github.com/ImIOImI/demo-repo/pull/42
```

> **`destroy` note:** because this data source errors when CI is red, a `terraform destroy` re-reads it during
> its plan phase and will fail if the ref is no longer green. That is intended gating behavior — keep it in mind
> when placing it alongside resources you intend to tear down (or set `error_on_failure = false`).

### Example: commit → PR → wait for green CI → merge

```hcl
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

# Waits at apply time (ref is computed) and fails the apply if CI isn't green.
data "ghflow_ci_status" "gate" {
  owner            = "ImIOImI"
  repository       = "demo-repo"
  ref              = ghflow_commit.config.commit_sha
  timeout          = "30m"
  required_checks  = ["build", "test"]                   # optional: only gate on these
  pull_request_url = ghflow_pull_request.config.html_url # surfaced in the error/log on failure
}

resource "ghflow_pr_merge" "config" {
  owner             = "ImIOImI"
  repository        = "demo-repo"
  number            = ghflow_pull_request.config.number
  merge_method      = "squash"
  required_head_sha = ghflow_pull_request.config.head_sha
  depends_on        = [data.ghflow_ci_status.gate] # don't merge until the gate passes
}
```

See [`examples/`](./examples) for runnable configurations.

## Developing the Provider

If you wish to work on the provider, you'll first need [Go](http://www.golang.org) installed (see
[Requirements](#requirements)). To compile the provider, run `go install` (or `make build`).

To build and use a local copy without publishing, configure `dev_overrides`:

```shell
make dev-install   # builds the binary and prints the dev_overrides block for ~/.tofurc
```

With `dev_overrides` configured, run `tofu plan`/`apply` directly against a checkout — no `init` needed.

To generate or update the registry docs (requires [`tfplugindocs`][tfplugindocs]):

```shell
make generate
```

Run the checks and tests:

```shell
make lint        # gofmt + go vet
make test        # unit tests
make test-e2e    # end-to-end tests (see note below)
```

*Note:* `make test-e2e` runs [terratest][terratest] against **real** GitHub. It builds the provider, wires it
via `dev_overrides`, ensures a persistent **private** test repo exists (`terraform-provider-ghflow-e2e`,
override with `GHFLOW_TEST_REPO`), runs the full `commit → PR → merge` flow plus the `ghflow_ci_status` paths
(green / red gate / wait→timeout), and asserts against the GitHub API. It requires `GITHUB_TOKEN` (with the
`repo` scope) and `tofu` (override the binary with `GHFLOW_TF_BINARY`). The test repo is reused and not deleted.

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
[tfplugindocs]: https://github.com/hashicorp/terraform-plugin-docs
[terratest]: https://terratest.gruntwork.io/
[otregistry]: https://github.com/opentofu/registry
