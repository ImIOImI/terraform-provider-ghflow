# Running ghflow in GitHub Actions

[`ghflow-pipeline.yml`](./ghflow-pipeline.yml) is an example workflow for driving the provider from CI. Copy it
into a **consumer** repo's `.github/workflows/`.

## Do not use the default `GITHUB_TOKEN`

Authenticating the provider with the automatic `GITHUB_TOKEN` breaks this flow in **two separate ways**:

1. **It can't open the PR (by default).** Creating a pull request with `GITHUB_TOKEN` is blocked unless
   *Settings → Actions → General → "Allow GitHub Actions to create and approve pull requests"* is enabled — and
   it is **off by default**. With it off, `ghflow_pull_request` fails with a `403`. (Pushing the commit in
   `ghflow_commit` works fine with `contents: write`; it's specifically PR creation/approval that's blocked.)
2. **It won't trigger your CI.** Even with PR creation enabled, events created by `GITHUB_TOKEN` do **not** start
   new workflow runs — GitHub's anti-recursion rule, with only `workflow_dispatch`/`repository_dispatch` exempt.
   So the checks `ghflow_ci_status` waits on never run, and the gate waits the full `timeout`.

Use a GitHub App token or a PAT instead — both can create PRs and both trigger downstream CI.

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
