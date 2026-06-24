package provider

import (
	"context"
	"net/http"

	"github.com/google/go-github/v66/github"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
)

var (
	_ resource.Resource              = (*prMergeResource)(nil)
	_ resource.ResourceWithConfigure = (*prMergeResource)(nil)
)

type prMergeResource struct {
	client *github.Client
}

type prMergeResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Owner           types.String `tfsdk:"owner"`
	Repository      types.String `tfsdk:"repository"`
	Number          types.Int64  `tfsdk:"number"`
	MergeMethod     types.String `tfsdk:"merge_method"`
	CommitTitle     types.String `tfsdk:"commit_title"`
	CommitMessage   types.String `tfsdk:"commit_message"`
	RequiredHeadSHA types.String `tfsdk:"required_head_sha"`
	Merged          types.Bool   `tfsdk:"merged"`
	MergeCommitSHA  types.String `tfsdk:"merge_commit_sha"`
}

func NewPRMergeResource() resource.Resource {
	return &prMergeResource{}
}

func (r *prMergeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pr_merge"
}

func (r *prMergeResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, resp)
}

func (r *prMergeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Merges a pull request. This is a one-shot, irreversible action: all inputs force " +
			"replacement, and `destroy` only removes the resource from state — it cannot un-merge a PR.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Resource identifier, set to the merge commit SHA.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"owner": schema.StringAttribute{
				MarkdownDescription: "Owner of the repository (user or organization).",
				Required:            true,
				PlanModifiers:       replace,
			},
			"repository": schema.StringAttribute{
				MarkdownDescription: "Name of the repository.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"number": schema.Int64Attribute{
				MarkdownDescription: "Number of the pull request to merge.",
				Required:            true,
				PlanModifiers:       []planmodifier.Int64{int64RequiresReplace()},
			},
			"merge_method": schema.StringAttribute{
				MarkdownDescription: "Merge method: `merge`, `squash`, or `rebase`. Defaults to `merge`.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("merge"),
				PlanModifiers:       replace,
				Validators: []validator.String{
					stringvalidator.OneOf("merge", "squash", "rebase"),
				},
			},
			"commit_title": schema.StringAttribute{
				MarkdownDescription: "Title for the merge commit (ignored for `rebase`).",
				Optional:            true,
				PlanModifiers:       replace,
			},
			"commit_message": schema.StringAttribute{
				MarkdownDescription: "Extra detail appended to the merge commit message (ignored for `rebase`).",
				Optional:            true,
				PlanModifiers:       replace,
			},
			"required_head_sha": schema.StringAttribute{
				MarkdownDescription: "If set, the merge only succeeds when the PR head matches this SHA (passed as the " +
					"GitHub `sha` parameter). Guards against merging a branch that moved since plan time.",
				Optional:      true,
				PlanModifiers: replace,
			},
			"merged": schema.BoolAttribute{
				MarkdownDescription: "Whether the merge succeeded.",
				Computed:            true,
			},
			"merge_commit_sha": schema.StringAttribute{
				MarkdownDescription: "SHA of the resulting merge commit.",
				Computed:            true,
			},
		},
	}
}

func (r *prMergeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan prMergeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	opts := &github.PullRequestOptions{
		MergeMethod: plan.MergeMethod.ValueString(),
	}
	if !plan.CommitTitle.IsNull() {
		opts.CommitTitle = plan.CommitTitle.ValueString()
	}
	if !plan.RequiredHeadSHA.IsNull() {
		opts.SHA = plan.RequiredHeadSHA.ValueString()
	}

	commitMessage := ""
	if !plan.CommitMessage.IsNull() {
		commitMessage = plan.CommitMessage.ValueString()
	}

	result, _, err := r.client.PullRequests.Merge(
		ctx,
		plan.Owner.ValueString(),
		plan.Repository.ValueString(),
		int(plan.Number.ValueInt64()),
		commitMessage,
		opts,
	)
	if err != nil {
		resp.Diagnostics.AddError("Failed to merge pull request", err.Error())
		return
	}

	plan.Merged = types.BoolValue(result.GetMerged())
	plan.MergeCommitSHA = types.StringValue(result.GetSHA())
	plan.ID = types.StringValue(result.GetSHA())

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *prMergeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state prMergeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	pr, httpResp, err := r.client.PullRequests.Get(ctx, state.Owner.ValueString(), state.Repository.ValueString(), int(state.Number.ValueInt64()))
	if err != nil {
		if httpResp != nil && httpResp.StatusCode == http.StatusNotFound {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read pull request", err.Error())
		return
	}

	state.Merged = types.BoolValue(pr.GetMerged())
	if pr.GetMergeCommitSHA() != "" {
		state.MergeCommitSHA = types.StringValue(pr.GetMergeCommitSHA())
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *prMergeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// All inputs are RequiresReplace, so Update should never run.
	var plan prMergeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *prMergeResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
	// A merge cannot be undone. Deleting this resource only drops it from state.
}
