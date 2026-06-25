//go:build e2e

// Package test contains an end-to-end test for the ghflow provider.
//
// It builds the provider locally, points OpenTofu at it via dev_overrides,
// ensures a persistent PRIVATE GitHub repository exists (so token auth is
// exercised against a private repo), runs the full
// commit -> pull_request -> merge flow with `tofu apply`, and asserts the
// result against the GitHub API.
//
// The test repo is created once and reused; it is NOT deleted on teardown.
// Each run uses a unique branch and unique file content, so reruns never
// collide and always produce a mergeable (non-empty) diff.
//
// Run it with:
//
//	cd test && GITHUB_TOKEN=... go test -tags e2e -v -timeout 30m ./...
//
// Requirements:
//   - GITHUB_TOKEN with the `repo` scope (creates the private test repo on the
//     first run, then reads/writes contents and pull requests). No
//     `delete_repo` scope is needed.
//   - `tofu` (or `terraform`) on PATH. Override with GHFLOW_TF_BINARY.
//   - A Go toolchain (the test runs `go build` on the provider).
//
// Optional:
//   - GHFLOW_TEST_OWNER: repo owner (defaults to the authenticated user).
//   - GHFLOW_TEST_REPO:  test repo name (defaults to terraform-provider-ghflow-e2e).
package test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/google/go-github/v66/github"
	"github.com/gruntwork-io/terratest/modules/terraform"
	"github.com/stretchr/testify/require"
)

const (
	providerSource  = "registry.opentofu.org/ImIOImI/ghflow"
	defaultTestRepo = "terraform-provider-ghflow-e2e"
)

func TestGhflowEndToEnd(t *testing.T) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		t.Skip("GITHUB_TOKEN not set; skipping e2e test")
	}

	ctx := context.Background()
	client := github.NewClient(nil).WithAuthToken(token)

	owner := os.Getenv("GHFLOW_TEST_OWNER")
	if owner == "" {
		u, _, err := client.Users.Get(ctx, "")
		require.NoError(t, err, "could not resolve authenticated user; set GHFLOW_TEST_OWNER")
		owner = u.GetLogin()
	}

	repoName := os.Getenv("GHFLOW_TEST_REPO")
	if repoName == "" {
		repoName = defaultTestRepo
	}

	// Build the provider into a temp dir and point OpenTofu at it.
	pluginDir := t.TempDir()
	buildProvider(t, pluginDir)
	cliConfig := writeDevOverrides(t, t.TempDir(), pluginDir)

	// Ensure the persistent private test repo exists (create on first run).
	ensurePrivateRepo(t, ctx, client, owner, repoName)
	waitForBranch(t, ctx, client, owner, repoName, "main")

	// Unique per run: fresh branch + content marker so reruns always merge.
	marker := strconv.FormatInt(time.Now().UnixNano(), 10)
	branch := "ghflow-e2e-" + marker

	tfOpts := &terraform.Options{
		TerraformDir:    "fixtures/e2e",
		TerraformBinary: tfBinary(),
		Vars: map[string]interface{}{
			"github_token": token,
			"owner":        owner,
			"repository":   repoName,
			"branch":       branch,
			"marker":       marker,
		},
		EnvVars: map[string]string{
			// dev_overrides means no `init` is needed; apply resolves the
			// provider straight from the locally built binary.
			"TF_CLI_CONFIG_FILE": cliConfig,
		},
		NoColor: true,
	}

	// destroy is mostly a no-op for these resources (a merge can't be undone),
	// but run it to exercise the Delete paths. The repo and merged history stay.
	defer terraform.Destroy(t, tfOpts)
	terraform.Apply(t, tfOpts)

	// --- Assertions against the GitHub API (the real proof) ---

	prNumberStr := terraform.Output(t, tfOpts, "pr_number")
	prNumber, err := strconv.Atoi(prNumberStr)
	require.NoError(t, err)

	pr, _, err := client.PullRequests.Get(ctx, owner, repoName, prNumber)
	require.NoError(t, err)
	require.True(t, pr.GetMerged(), "pull request #%d should be merged", prNumber)

	require.Equal(t, "true", terraform.Output(t, tfOpts, "merged"), "ghflow_pr_merge.merged output")
	require.NotEmpty(t, terraform.Output(t, tfOpts, "merge_commit_sha"), "merge_commit_sha output")

	// The committed file should now exist on the base branch after the merge,
	// carrying this run's marker (proves it was this run's commit that merged).
	fileContent, _, _, err := client.Repositories.GetContents(ctx, owner, repoName, "ghflow-e2e.txt",
		&github.RepositoryContentGetOptions{Ref: "main"})
	require.NoError(t, err, "committed file should exist on main after merge")
	decoded, err := fileContent.GetContent()
	require.NoError(t, err)
	require.Contains(t, decoded, "created by ghflow e2e test")
	require.Contains(t, decoded, marker, "merged file should carry this run's marker")
}

// ensurePrivateRepo gets the test repo, creating it as a private, initialized
// repo if it does not already exist. It is never deleted.
func ensurePrivateRepo(t *testing.T, ctx context.Context, client *github.Client, owner, repo string) {
	t.Helper()

	got, resp, err := client.Repositories.Get(ctx, owner, repo)
	if err == nil {
		require.True(t, got.GetPrivate(), "test repo %s/%s exists but is not private", owner, repo)
		return
	}
	if resp == nil || resp.StatusCode != http.StatusNotFound {
		require.NoError(t, err, "unexpected error checking for test repo")
	}

	t.Logf("creating persistent private test repo %s/%s", owner, repo)
	_, _, err = client.Repositories.Create(ctx, "", &github.Repository{
		Name:        github.String(repo),
		Private:     github.Bool(true),
		AutoInit:    github.Bool(true),
		Description: github.String("Persistent private repo for ghflow e2e tests."),
	})
	require.NoError(t, err, "failed to create test repo")
}

// buildProvider compiles the provider into dir as a dev_overrides binary.
func buildProvider(t *testing.T, dir string) {
	t.Helper()

	repoRoot, err := filepath.Abs("..")
	require.NoError(t, err)

	binName := "terraform-provider-ghflow"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}

	cmd := exec.Command("go", "build", "-o", filepath.Join(dir, binName), ".")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "go build failed: %s", string(out))
}

// writeDevOverrides writes an OpenTofu CLI config in configDir that overrides
// the provider with the locally built binary in pluginDir. Returns its path.
func writeDevOverrides(t *testing.T, configDir, pluginDir string) string {
	t.Helper()

	content := fmt.Sprintf(`provider_installation {
  dev_overrides {
    %q = %q
  }
  direct {}
}
`, providerSource, pluginDir)

	path := filepath.Join(configDir, "dev.tofurc")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// tfBinary returns the OpenTofu/Terraform binary to use. Defaults to "tofu".
func tfBinary() string {
	if b := os.Getenv("GHFLOW_TF_BINARY"); b != "" {
		return b
	}
	return "tofu"
}

// waitForBranch polls until the named branch exists (AutoInit can lag briefly).
func waitForBranch(t *testing.T, ctx context.Context, client *github.Client, owner, repo, branch string) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for {
		_, _, err := client.Git.GetRef(ctx, owner, repo, "refs/heads/"+branch)
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("branch %q never became available in %s/%s: %v", branch, owner, repo, err)
		}
		time.Sleep(time.Second)
	}
}
