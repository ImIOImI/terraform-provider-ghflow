package provider

import (
	"context"
	"net/http"

	"github.com/google/go-github/v66/github"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource              = (*pullRequestResource)(nil)
	_ resource.ResourceWithConfigure = (*pullRequestResource)(nil)
)

type pullRequestResource struct {
	client *github.Client
}

type pullRequestResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Owner          types.String `tfsdk:"owner"`
	Repository     types.String `tfsdk:"repository"`
	Title          types.String `tfsdk:"title"`
	Body           types.String `tfsdk:"body"`
	HeadRef        types.String `tfsdk:"head_ref"`
	BaseRef        types.String `tfsdk:"base_ref"`
	CloseOnDestroy types.Bool   `tfsdk:"close_on_destroy"`
	Number         types.Int64  `tfsdk:"number"`
	State          types.String `tfsdk:"state"`
	Merged         types.Bool   `tfsdk:"merged"`
	HeadSHA        types.String `tfsdk:"head_sha"`
	HTMLURL        types.String `tfsdk:"html_url"`
}

func NewPullRequestResource() resource.Resource {
	return &pullRequestResource{}
}

func (r *pullRequestResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pull_request"
}

func (r *pullRequestResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, resp)
}

func (r *pullRequestResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Opens a pull request in a GitHub repository. `title`, `body`, and `base_ref` can be " +
			"updated in place; changing `owner`, `repository`, or `head_ref` replaces the PR.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Resource identifier, set to the pull request number.",
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
			"title": schema.StringAttribute{
				MarkdownDescription: "Title of the pull request.",
				Required:            true,
			},
			"body": schema.StringAttribute{
				MarkdownDescription: "Body (description) of the pull request.",
				Optional:            true,
			},
			"head_ref": schema.StringAttribute{
				MarkdownDescription: "The branch containing the changes to merge. For a cross-repo PR use `owner:branch`.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"base_ref": schema.StringAttribute{
				MarkdownDescription: "The branch you want the changes pulled into, e.g. `main`.",
				Required:            true,
			},
			"close_on_destroy": schema.BoolAttribute{
				MarkdownDescription: "If true (default), `destroy` closes the pull request. If false, `destroy` only " +
					"removes the resource from state and leaves the PR open.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"number": schema.Int64Attribute{
				MarkdownDescription: "The pull request number.",
				Computed:            true,
			},
			"state": schema.StringAttribute{
				MarkdownDescription: "State of the pull request: `open` or `closed`.",
				Computed:            true,
			},
			"merged": schema.BoolAttribute{
				MarkdownDescription: "Whether the pull request has been merged.",
				Computed:            true,
			},
			"head_sha": schema.StringAttribute{
				MarkdownDescription: "SHA of the head commit of the pull request.",
				Computed:            true,
			},
			"html_url": schema.StringAttribute{
				MarkdownDescription: "Web URL of the pull request.",
				Computed:            true,
			},
		},
	}
}

func (r *pullRequestResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan pullRequestResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	newPR := &github.NewPullRequest{
		Title: ptr(plan.Title.ValueString()),
		Head:  ptr(plan.HeadRef.ValueString()),
		Base:  ptr(plan.BaseRef.ValueString()),
	}
	if !plan.Body.IsNull() {
		newPR.Body = ptr(plan.Body.ValueString())
	}

	pr, _, err := r.client.PullRequests.Create(ctx, plan.Owner.ValueString(), plan.Repository.ValueString(), newPR)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create pull request", err.Error())
		return
	}

	applyPRToModel(pr, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *pullRequestResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state pullRequestResourceModel
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

	applyPRToModel(pr, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *pullRequestResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan pullRequestResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	update := &github.PullRequest{
		Title: ptr(plan.Title.ValueString()),
		Base:  &github.PullRequestBranch{Ref: ptr(plan.BaseRef.ValueString())},
	}
	if !plan.Body.IsNull() {
		update.Body = ptr(plan.Body.ValueString())
	}

	pr, _, err := r.client.PullRequests.Edit(ctx, plan.Owner.ValueString(), plan.Repository.ValueString(), int(plan.Number.ValueInt64()), update)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update pull request", err.Error())
		return
	}

	applyPRToModel(pr, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *pullRequestResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state pullRequestResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// A pull request cannot be deleted via the API. Optionally close it.
	if !state.CloseOnDestroy.ValueBool() {
		return
	}

	// Closing an already-merged PR is a no-op error; skip in that case.
	if state.Merged.ValueBool() {
		return
	}

	_, _, err := r.client.PullRequests.Edit(ctx, state.Owner.ValueString(), state.Repository.ValueString(), int(state.Number.ValueInt64()), &github.PullRequest{
		State: ptr("closed"),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to close pull request", err.Error())
	}
}

// applyPRToModel copies fields from a GitHub PR into the resource model.
func applyPRToModel(pr *github.PullRequest, m *pullRequestResourceModel) {
	m.ID = types.StringValue(intToString(pr.GetNumber()))
	m.Number = types.Int64Value(int64(pr.GetNumber()))
	m.State = types.StringValue(pr.GetState())
	m.Merged = types.BoolValue(pr.GetMerged())
	m.HeadSHA = types.StringValue(pr.GetHead().GetSHA())
	m.HTMLURL = types.StringValue(pr.GetHTMLURL())
}
