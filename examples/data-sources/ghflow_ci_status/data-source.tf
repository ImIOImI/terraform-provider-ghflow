# Wait for CI on a ref and fail the plan/apply if it isn't all green.
data "ghflow_ci_status" "head" {
  owner      = "ImIOImI"
  repository = "demo-repo"
  ref        = "main" # a SHA, branch, or tag

  # Always waits up to this long for checks to finish.
  timeout       = "30m"
  poll_interval = "10s"

  # Optional: only these checks gate the verdict (others are reported, ignored).
  # required_checks = ["build", "test"]

  # Optional: never block on these (e.g. a flaky non-required check).
  # ignore_checks = ["optional-lint"]

  # Set to false to read state without failing when CI is red/pending.
  error_on_failure = true
}

output "ci_is_green" {
  value = data.ghflow_ci_status.head.success
}

output "ci_state" {
  value = data.ghflow_ci_status.head.state # success | failure | pending
}
