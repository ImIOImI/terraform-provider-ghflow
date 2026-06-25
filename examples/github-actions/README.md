# Running ghflow in GitHub Actions

[`ghflow-pipeline.yml`](./ghflow-pipeline.yml) is an example workflow for driving the provider from CI. Copy it
into a **consumer** repo's `.github/workflows/`.

## Do not use the default `GITHUB_TOKEN`

The automatic `GITHUB_TOKEN` **cannot trigger other workflows** — by design, events it creates (a push, a PR, a
merge) do not start new workflow runs. Because `ghflow_commit` pushes the commit that your CI is supposed to run
on, authenticating the provider with `GITHUB_TOKEN` means that CI never fires, and `ghflow_ci_status` waits the
full `timeout` for checks that will never appear. Use one of the two credentials below instead — both trigger
downstream workflows.

## Option A — GitHub App (recommended)

A GitHub App installation token triggers downstream CI, isn't bound to a user, rotates automatically, and has
higher rate limits.

1. Create a GitHub App and install it on the target repositories with the same permissions the provider needs
   (see the [Token permissions](../../README.md#token-permissions) table): Contents R/W, Pull requests R/W,
   Checks R, Commit statuses R.
2. Add the App ID as the repository/org **variable** `GHFLOW_APP_ID` and the App private key as the **secret**
   `GHFLOW_APP_PRIVATE_KEY`.
3. The workflow mints an installation token with [`actions/create-github-app-token`][cgat] and passes it to the
   provider via `GITHUB_TOKEN`.

## Option B — Personal Access Token

Simplest to set up; also triggers downstream CI. Store a fine-grained or classic PAT (see the
[Token permissions](../../README.md#token-permissions) table) as the secret `GHFLOW_PAT`, then swap the
`GITHUB_TOKEN` line in the workflow as noted in its comments. A PAT is user-bound and needs manual rotation.

## Branch protection

If the base branch requires status checks or reviews, the App/PAT must be allowed to **merge** — either by
satisfying those rules (the `ghflow_ci_status` gate helps here) or by being on the branch protection bypass list.

[cgat]: https://github.com/actions/create-github-app-token
