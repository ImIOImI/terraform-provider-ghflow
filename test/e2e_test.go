//go:build e2e

// Package test contains an end-to-end test for the ghflow provider.
//
// It builds the provider locally, points OpenTofu at it via dev_overrides,
// creates a throwaway private GitHub repository, runs the full
// commit -> pull_request -> merge flow with `tofu apply`, asserts the result
// against the GitHub API, and deletes the repository on teardown.
//
// Run it with:
//
//	cd test && GITHUB_TOKEN=... go test -tags e2e -v -timeout 30m ./...
//
// Requirements:
//   - GITHUB_TOKEN with `repo` and `delete_repo` scopes (classic PAT) or a
//     fine-grained token allowed to create and delete repos plus write
//     contents and pull requests.
//   - `tofu` (or `terraform`) on PATH. Override with GHFLOW_TF_BINARY.
//   - A Go toolchain (the test runs `go build` on the provider).
package test

import (
	"context"
	"fmt"
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

const providerSource = "registry.opentofu.org/ImIOImI/ghflow"

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

	// Build the provider into a temp dir and point OpenTofu at it.
	pluginDir := t.TempDir()
	buildProvider(t, pluginDir)
	cliConfig := writeDevOverrides(t, t.TempDir(), pluginDir)

	// Create an ephemeral private repo, initialized so `main` exists.
	repoName := fmt.Sprintf("ghflow-e2e-%d", time.Now().UnixNano())
	_, _, err := client.Repositories.Create(ctx, "", &github.Repository{
		Name:        github.String(repoName),
		Private:     github.Bool(true),
		AutoInit:    github.Bool(true),
		Description: github.String("ephemeral ghflow e2e test repo - safe to delete"),
	})
	require.NoError(t, err, "failed to create test repo")
	t.Cleanup(func() {
		if _, err := client.Repositories.Delete(ctx, owner, repoName); err != nil {
			t.Logf("WARNING: failed to delete test repo %s/%s: %v (delete it manually)", owner, repoName, err)
		} else {
			t.Logf("deleted test repo %s/%s", owner, repoName)
		}
	})

	waitForBranch(t, ctx, client, owner, repoName, "main")

	tfOpts := &terraform.Options{
		TerraformDir:    "fixtures/e2e",
		TerraformBinary: tfBinary(),
		Vars: map[string]interface{}{
			"github_token": token,
			"owner":        owner,
			"repository":   repoName,
		},
		EnvVars: map[string]string{
			// dev_overrides means no `init` is needed; apply resolves the
			// provider straight from the locally built binary.
			"TF_CLI_CONFIG_FILE": cliConfig,
		},
		NoColor: true,
	}

	// destroy is mostly a no-op for these resources (a merge can't be undone),
	// but run it to exercise the Delete paths; the repo is deleted in Cleanup.
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

	// The committed file should now exist on the base branch after the merge.
	fileContent, _, _, err := client.Repositories.GetContents(ctx, owner, repoName, "ghflow-e2e.txt",
		&github.RepositoryContentGetOptions{Ref: "main"})
	require.NoError(t, err, "committed file should exist on main after merge")
	decoded, err := fileContent.GetContent()
	require.NoError(t, err)
	require.Contains(t, decoded, "created by ghflow e2e test")
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
