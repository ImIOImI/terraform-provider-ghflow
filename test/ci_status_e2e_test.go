//go:build e2e

package test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/go-github/v66/github"
	"github.com/gruntwork-io/terratest/modules/terraform"
	"github.com/stretchr/testify/require"
)

// TestGhflowCIStatus exercises the ghflow_ci_status data source by posting
// commit statuses via the GitHub API (the persistent test repo has no real CI)
// and asserting how the data source reads them: the green path, the red gate
// (apply fails), and the wait -> timeout path for a never-arriving check.
func TestGhflowCIStatus(t *testing.T) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		t.Skip("GITHUB_TOKEN not set; skipping e2e test")
	}

	ctx := context.Background()
	client := github.NewClient(nil).WithAuthToken(token)

	owner := os.Getenv("GHFLOW_TEST_OWNER")
	if owner == "" {
		u, _, err := client.Users.Get(ctx, "")
		require.NoError(t, err)
		owner = u.GetLogin()
	}
	repoName := os.Getenv("GHFLOW_TEST_REPO")
	if repoName == "" {
		repoName = defaultTestRepo
	}

	pluginDir := t.TempDir()
	buildProvider(t, pluginDir)
	cliConfig := writeDevOverrides(t, t.TempDir(), pluginDir)

	ensurePrivateRepo(t, ctx, client, owner, repoName)
	waitForBranch(t, ctx, client, owner, repoName, "main")

	// Status target: current main HEAD.
	mainRef, _, err := client.Git.GetRef(ctx, owner, repoName, "refs/heads/main")
	require.NoError(t, err)
	sha := mainRef.GetObject().GetSHA()

	const checkContext = "ghflow-e2e/ci"

	baseOpts := func(vars map[string]interface{}) *terraform.Options {
		v := map[string]interface{}{
			"github_token":  token,
			"owner":         owner,
			"repository":    repoName,
			"ref":           sha,
			"timeout":       "60s",
			"poll_interval": "2s",
		}
		for k, val := range vars {
			v[k] = val
		}
		return &terraform.Options{
			TerraformDir:    "fixtures/ci-status",
			TerraformBinary: tfBinary(),
			Vars:            v,
			EnvVars:         map[string]string{"TF_CLI_CONFIG_FILE": cliConfig},
			NoColor:         true,
		}
	}

	// --- Phase 1: green status -> success ---
	createStatus(t, ctx, client, owner, repoName, sha, checkContext, "success")
	waitForCombinedState(t, ctx, client, owner, repoName, sha, checkContext, "success")

	// No terraform.Destroy: this fixture has only a data source (nothing to
	// destroy), and a deferred destroy would re-read the data source after we
	// flip the status to red below, failing on the very gate we're testing.
	greenOpts := baseOpts(nil)
	terraform.Apply(t, greenOpts)
	require.Equal(t, "true", terraform.Output(t, greenOpts, "success"))
	require.Equal(t, "success", terraform.Output(t, greenOpts, "state"))

	// --- Phase 2: failing status, error_on_failure=false -> reports failure ---
	createStatus(t, ctx, client, owner, repoName, sha, checkContext, "failure")
	waitForCombinedState(t, ctx, client, owner, repoName, sha, checkContext, "failure")

	const fakePRURL = "https://github.com/ImIOImI/terraform-provider-ghflow-e2e/pull/4242"

	redReadOpts := baseOpts(map[string]interface{}{
		"error_on_failure": false,
		"pull_request_url": fakePRURL,
	})
	readOut := terraform.Apply(t, redReadOpts)
	require.Equal(t, "false", terraform.Output(t, redReadOpts, "success"))
	require.Equal(t, "failure", terraform.Output(t, redReadOpts, "state"))
	require.Contains(t, terraform.Output(t, redReadOpts, "failed_checks"), checkContext)
	// The PR URL is emitted as a warning so it's visible in normal output.
	require.Contains(t, readOut, fakePRURL, "warning should include the PR URL for manual review")

	// --- Phase 3: failing status, error_on_failure=true -> the gate fails apply ---
	redGateOpts := baseOpts(map[string]interface{}{
		"error_on_failure": true,
		"pull_request_url": fakePRURL,
	})
	_, err = terraform.ApplyE(t, redGateOpts)
	require.Error(t, err, "apply should fail when CI is red and error_on_failure=true")
	require.Contains(t, err.Error(), "not green")
	require.Contains(t, err.Error(), fakePRURL, "error should include the PR URL for manual review")

	// --- Phase 4: a required check that never appears -> wait then timeout=pending ---
	pendingOpts := baseOpts(map[string]interface{}{
		"error_on_failure": false,
		"required_checks":  []string{"never-runs"},
		"timeout":          "5s",
		"poll_interval":    "1s",
	})
	start := time.Now()
	terraform.Apply(t, pendingOpts)
	require.GreaterOrEqual(t, time.Since(start).Seconds(), 4.0, "should have waited for the timeout")
	require.Equal(t, "pending", terraform.Output(t, pendingOpts, "state"))
	require.Equal(t, "false", terraform.Output(t, pendingOpts, "success"))
}

func createStatus(t *testing.T, ctx context.Context, client *github.Client, owner, repo, sha, statusContext, state string) {
	t.Helper()
	_, _, err := client.Repositories.CreateStatus(ctx, owner, repo, sha, &github.RepoStatus{
		State:       github.String(state),
		Context:     github.String(statusContext),
		Description: github.String("ghflow e2e simulated CI"),
	})
	require.NoError(t, err, "failed to create commit status")
}

// waitForCombinedState polls until the named context shows wantState, so the
// data source read isn't racing GitHub's status propagation.
func waitForCombinedState(t *testing.T, ctx context.Context, client *github.Client, owner, repo, sha, statusContext, wantState string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		combined, _, err := client.Repositories.GetCombinedStatus(ctx, owner, repo, sha, &github.ListOptions{PerPage: 100})
		require.NoError(t, err)
		for _, s := range combined.Statuses {
			if s.GetContext() == statusContext && s.GetState() == wantState {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("status %q never reached state %q on %s", statusContext, wantState, sha)
		}
		time.Sleep(time.Second)
	}
}
