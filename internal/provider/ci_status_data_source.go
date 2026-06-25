package provider

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/go-github/v66/github"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*ciStatusDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*ciStatusDataSource)(nil)
)

const (
	defaultCITimeout      = 30 * time.Minute
	defaultCIPollInterval = 10 * time.Second
)

type ciStatusDataSource struct {
	client *github.Client
}

type ciStatusDataSourceModel struct {
	Owner          types.String `tfsdk:"owner"`
	Repository     types.String `tfsdk:"repository"`
	Ref            types.String `tfsdk:"ref"`
	Timeout        types.String `tfsdk:"timeout"`
	PollInterval   types.String `tfsdk:"poll_interval"`
	RequiredChecks types.List   `tfsdk:"required_checks"`
	IgnoreChecks   types.List   `tfsdk:"ignore_checks"`
	ErrorOnFailure types.Bool   `tfsdk:"error_on_failure"`

	ID            types.String `tfsdk:"id"`
	Success       types.Bool   `tfsdk:"success"`
	State         types.String `tfsdk:"state"`
	TotalCount    types.Int64  `tfsdk:"total_count"`
	FailedChecks  types.List   `tfsdk:"failed_checks"`
	PendingChecks types.List   `tfsdk:"pending_checks"`
	Checks        types.List   `tfsdk:"checks"`
}

// ciCheckModel is one row of the `checks` output.
type ciCheckModel struct {
	Name       types.String `tfsdk:"name"`
	Kind       types.String `tfsdk:"kind"`
	Status     types.String `tfsdk:"status"`
	Conclusion types.String `tfsdk:"conclusion"`
}

func ciCheckObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"name":       types.StringType,
		"kind":       types.StringType,
		"status":     types.StringType,
		"conclusion": types.StringType,
	}}
}

func NewCIStatusDataSource() datasource.DataSource {
	return &ciStatusDataSource{}
}

func (d *ciStatusDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ci_status"
}

func (d *ciStatusDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*github.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type", "Expected *github.Client. This is a bug in the provider.")
		return
	}
	d.client = client
}

func (d *ciStatusDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Waits for GitHub CI on a ref to finish, then reports whether all checks are green. " +
			"Considers both check runs (GitHub Actions / Check API) and the combined commit status. " +
			"Point `ref` at a computed value (e.g. a commit SHA) to defer the wait to apply time and gate a merge on green CI.",
		Attributes: map[string]schema.Attribute{
			"owner": schema.StringAttribute{
				MarkdownDescription: "Owner of the repository (user or organization).",
				Required:            true,
			},
			"repository": schema.StringAttribute{
				MarkdownDescription: "Name of the repository.",
				Required:            true,
			},
			"ref": schema.StringAttribute{
				MarkdownDescription: "Ref to check: a commit SHA, branch name, or tag.",
				Required:            true,
			},
			"timeout": schema.StringAttribute{
				MarkdownDescription: "Maximum time to wait for checks to complete, as a Go duration (e.g. `30m`, `1h`). Defaults to `30m`.",
				Optional:            true,
			},
			"poll_interval": schema.StringAttribute{
				MarkdownDescription: "How often to poll GitHub while waiting, as a Go duration (e.g. `10s`). Defaults to `10s`.",
				Optional:            true,
			},
			"required_checks": schema.ListAttribute{
				MarkdownDescription: "Allowlist of check/status names. If set, only these gate the verdict (others are reported but ignored). " +
					"A required check that never appears keeps the result pending until `timeout`.",
				Optional:    true,
				ElementType: types.StringType,
			},
			"ignore_checks": schema.ListAttribute{
				MarkdownDescription: "Denylist of check/status names to exclude from the verdict entirely.",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"error_on_failure": schema.BoolAttribute{
				MarkdownDescription: "If true (default), a failed or timed-out result raises an error (failing the plan/apply). " +
					"If false, the data source returns normally and you inspect `success`/`state` yourself.",
				Optional: true,
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "Identifier of this read (the ref).",
				Computed:            true,
			},
			"success": schema.BoolAttribute{
				MarkdownDescription: "True if every gating check completed green.",
				Computed:            true,
			},
			"state": schema.StringAttribute{
				MarkdownDescription: "Overall state: `success`, `failure`, or `pending` (only when `error_on_failure` is false and the wait timed out).",
				Computed:            true,
			},
			"total_count": schema.Int64Attribute{
				MarkdownDescription: "Number of gating checks considered.",
				Computed:            true,
			},
			"failed_checks": schema.ListAttribute{
				MarkdownDescription: "Names of gating checks that did not conclude green.",
				Computed:            true,
				ElementType:         types.StringType,
			},
			"pending_checks": schema.ListAttribute{
				MarkdownDescription: "Names of gating checks still pending when the wait ended.",
				Computed:            true,
				ElementType:         types.StringType,
			},
			"checks": schema.ListNestedAttribute{
				MarkdownDescription: "All gating checks with their status.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name":       schema.StringAttribute{Computed: true, MarkdownDescription: "Check run name or status context."},
						"kind":       schema.StringAttribute{Computed: true, MarkdownDescription: "`check_run` or `commit_status`."},
						"status":     schema.StringAttribute{Computed: true, MarkdownDescription: "`queued`, `in_progress`, or `completed`."},
						"conclusion": schema.StringAttribute{Computed: true, MarkdownDescription: "Terminal conclusion/state, e.g. `success`, `failure`, `skipped`."},
					},
				},
			},
		},
	}
}

// checkRecord is the internal, merged representation of one check.
type checkRecord struct {
	name       string
	kind       string // "check_run" or "commit_status"
	status     string // queued | in_progress | completed
	conclusion string // success | failure | neutral | skipped | ...
	terminal   bool
	green      bool
}

func (d *ciStatusDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config ciStatusDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	timeout := defaultCITimeout
	if !config.Timeout.IsNull() {
		parsed, err := time.ParseDuration(config.Timeout.ValueString())
		if err != nil {
			resp.Diagnostics.AddAttributeError(pathRoot("timeout"), "Invalid timeout", err.Error())
			return
		}
		timeout = parsed
	}

	pollInterval := defaultCIPollInterval
	if !config.PollInterval.IsNull() {
		parsed, err := time.ParseDuration(config.PollInterval.ValueString())
		if err != nil {
			resp.Diagnostics.AddAttributeError(pathRoot("poll_interval"), "Invalid poll_interval", err.Error())
			return
		}
		pollInterval = parsed
	}

	errorOnFailure := true
	if !config.ErrorOnFailure.IsNull() {
		errorOnFailure = config.ErrorOnFailure.ValueBool()
	}

	var required, ignore []string
	resp.Diagnostics.Append(config.RequiredChecks.ElementsAs(ctx, &required, false)...)
	resp.Diagnostics.Append(config.IgnoreChecks.ElementsAs(ctx, &ignore, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	requiredSet := toSet(required)
	ignoreSet := toSet(ignore)

	owner := config.Owner.ValueString()
	repo := config.Repository.ValueString()
	ref := config.Ref.ValueString()

	deadline := time.Now().Add(timeout)
	var records []checkRecord
	var timedOut bool

	for {
		var err error
		records, err = d.gather(ctx, owner, repo, ref, requiredSet, ignoreSet)
		if err != nil {
			resp.Diagnostics.AddError("Failed to read CI status", err.Error())
			return
		}

		if allTerminal(records) {
			break
		}
		if time.Now().After(deadline) {
			timedOut = true
			break
		}

		select {
		case <-ctx.Done():
			resp.Diagnostics.AddError("Cancelled while waiting for CI", ctx.Err().Error())
			return
		case <-time.After(pollInterval):
		}
	}

	// Build outputs.
	var failed, pending []string
	checkModels := make([]ciCheckModel, 0, len(records))
	for _, r := range records {
		checkModels = append(checkModels, ciCheckModel{
			Name:       types.StringValue(r.name),
			Kind:       types.StringValue(r.kind),
			Status:     types.StringValue(r.status),
			Conclusion: types.StringValue(r.conclusion),
		})
		if !r.terminal {
			pending = append(pending, r.name)
		} else if !r.green {
			failed = append(failed, r.name)
		}
	}
	sort.Strings(failed)
	sort.Strings(pending)

	success := len(failed) == 0 && len(pending) == 0
	state := "success"
	switch {
	case len(pending) > 0:
		state = "pending"
	case len(failed) > 0:
		state = "failure"
	}

	config.ID = types.StringValue(ref)
	config.Success = types.BoolValue(success)
	config.State = types.StringValue(state)
	config.TotalCount = types.Int64Value(int64(len(records)))

	failedList, diags := types.ListValueFrom(ctx, types.StringType, failed)
	resp.Diagnostics.Append(diags...)
	pendingList, diags := types.ListValueFrom(ctx, types.StringType, pending)
	resp.Diagnostics.Append(diags...)
	checksList, diags := types.ListValueFrom(ctx, ciCheckObjectType(), checkModels)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	config.FailedChecks = failedList
	config.PendingChecks = pendingList
	config.Checks = checksList

	if errorOnFailure && !success {
		if timedOut {
			resp.Diagnostics.AddError(
				"Timed out waiting for CI",
				fmt.Sprintf("CI on %s/%s@%s did not complete within %s. Pending: %v; failed: %v.", owner, repo, ref, timeout, pending, failed),
			)
		} else {
			resp.Diagnostics.AddError(
				"CI is not green",
				fmt.Sprintf("CI on %s/%s@%s is %q. Failed checks: %v.", owner, repo, ref, state, failed),
			)
		}
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

// gather fetches and merges check runs and combined commit statuses for the ref,
// applying the ignore/required filters. Required-but-absent checks are added as
// pending so the caller keeps waiting for them.
func (d *ciStatusDataSource) gather(ctx context.Context, owner, repo, ref string, required, ignore map[string]bool) ([]checkRecord, error) {
	byName := map[string]checkRecord{}

	// Check runs (paginated).
	crOpts := &github.ListCheckRunsOptions{ListOptions: github.ListOptions{PerPage: 100}}
	for {
		result, httpResp, err := d.client.Checks.ListCheckRunsForRef(ctx, owner, repo, ref, crOpts)
		if err != nil {
			return nil, err
		}
		for _, cr := range result.CheckRuns {
			name := cr.GetName()
			status := cr.GetStatus()
			conclusion := cr.GetConclusion()
			byName[name] = checkRecord{
				name:       name,
				kind:       "check_run",
				status:     status,
				conclusion: conclusion,
				terminal:   status == "completed",
				green:      conclusionIsGreen(conclusion),
			}
		}
		if httpResp == nil || httpResp.NextPage == 0 {
			break
		}
		crOpts.Page = httpResp.NextPage
	}

	// Combined commit status (paginated over the per-context statuses).
	stOpts := &github.ListOptions{PerPage: 100}
	for {
		combined, httpResp, err := d.client.Repositories.GetCombinedStatus(ctx, owner, repo, ref, stOpts)
		if err != nil {
			return nil, err
		}
		for _, s := range combined.Statuses {
			name := s.GetContext()
			state := s.GetState() // success | pending | failure | error
			byName[name] = checkRecord{
				name:       name,
				kind:       "commit_status",
				status:     statusToRunStatus(state),
				conclusion: state,
				terminal:   state != "pending",
				green:      state == "success",
			}
		}
		if httpResp == nil || httpResp.NextPage == 0 {
			break
		}
		stOpts.Page = httpResp.NextPage
	}

	// Apply filters.
	records := make([]checkRecord, 0, len(byName))
	for name, r := range byName {
		if ignore[name] {
			continue
		}
		if len(required) > 0 && !required[name] {
			continue
		}
		records = append(records, r)
	}

	// A required check that has not appeared yet counts as pending.
	for name := range required {
		if _, seen := byName[name]; !seen && !ignore[name] {
			records = append(records, checkRecord{name: name, kind: "missing", status: "queued", conclusion: "", terminal: false, green: false})
		}
	}

	sort.Slice(records, func(i, j int) bool { return records[i].name < records[j].name })
	return records, nil
}

func conclusionIsGreen(conclusion string) bool {
	switch conclusion {
	case "success", "neutral", "skipped":
		return true
	default:
		return false
	}
}

func statusToRunStatus(state string) string {
	if state == "pending" {
		return "in_progress"
	}
	return "completed"
}

func allTerminal(records []checkRecord) bool {
	for _, r := range records {
		if !r.terminal {
			return false
		}
	}
	return true
}

func toSet(items []string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, i := range items {
		set[i] = true
	}
	return set
}
