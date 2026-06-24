package provider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/go-github/v66/github"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource              = (*commitResource)(nil)
	_ resource.ResourceWithConfigure = (*commitResource)(nil)
)

type commitResource struct {
	client *github.Client
}

type commitResourceModel struct {
	ID            types.String `tfsdk:"id"`
	Owner         types.String `tfsdk:"owner"`
	Repository    types.String `tfsdk:"repository"`
	Branch        types.String `tfsdk:"branch"`
	FromBranch    types.String `tfsdk:"from_branch"`
	Path          types.String `tfsdk:"path"`
	Content       types.String `tfsdk:"content"`
	CommitMessage types.String `tfsdk:"commit_message"`
	AuthorName    types.String `tfsdk:"author_name"`
	AuthorEmail   types.String `tfsdk:"author_email"`
	CommitSHA     types.String `tfsdk:"commit_sha"`
	TreeSHA       types.String `tfsdk:"tree_sha"`
}

func NewCommitResource() resource.Resource {
	return &commitResource{}
}

func (r *commitResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_commit"
}

func (r *commitResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, resp)
}

func (r *commitResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Commits a single file to a branch in a GitHub repository.\n\n" +
			"This resource creates one commit per apply. Because git history is append-only, any change " +
			"to its inputs forces a new commit (replacement), and `destroy` only removes the resource from " +
			"state — it does not revert the commit.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Resource identifier, set to the created commit SHA.",
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
			"branch": schema.StringAttribute{
				MarkdownDescription: "Branch to commit to. If it does not exist and `from_branch` is set, it is created first.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"from_branch": schema.StringAttribute{
				MarkdownDescription: "If set and `branch` does not exist, create `branch` from this branch's HEAD before committing. " +
					"Typical pattern: commit to a new feature branch created from `main`.",
				Optional:      true,
				PlanModifiers: replace,
			},
			"path": schema.StringAttribute{
				MarkdownDescription: "Path of the file within the repository, e.g. `config/app.yaml`.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"content": schema.StringAttribute{
				MarkdownDescription: "UTF-8 file content to commit.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"commit_message": schema.StringAttribute{
				MarkdownDescription: "Commit message.",
				Required:            true,
				PlanModifiers:       replace,
			},
			"author_name": schema.StringAttribute{
				MarkdownDescription: "Commit author name. Defaults to the authenticated user if omitted.",
				Optional:            true,
				PlanModifiers:       replace,
			},
			"author_email": schema.StringAttribute{
				MarkdownDescription: "Commit author email. Defaults to the authenticated user if omitted.",
				Optional:            true,
				PlanModifiers:       replace,
			},
			"commit_sha": schema.StringAttribute{
				MarkdownDescription: "SHA of the created commit.",
				Computed:            true,
			},
			"tree_sha": schema.StringAttribute{
				MarkdownDescription: "SHA of the git tree created for the commit.",
				Computed:            true,
			},
		},
	}
}

func (r *commitResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan commitResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	owner := plan.Owner.ValueString()
	repoName := plan.Repository.ValueString()
	branch := plan.Branch.ValueString()
	branchRef := "refs/heads/" + branch

	// Resolve the branch HEAD, creating the branch from from_branch if needed.
	baseSHA, ok := r.resolveBranchHead(ctx, owner, repoName, branch, plan.FromBranch, resp)
	if !ok {
		return
	}

	// Get the base commit so we can root the new tree on its tree.
	baseCommit, _, err := r.client.Git.GetCommit(ctx, owner, repoName, baseSHA)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read base commit", err.Error())
		return
	}

	// Build a tree containing the single file. Content is inlined; the API
	// creates the blob implicitly.
	entry := &github.TreeEntry{
		Path:    ptr(plan.Path.ValueString()),
		Mode:    ptr("100644"),
		Type:    ptr("blob"),
		Content: ptr(plan.Content.ValueString()),
	}
	newTree, _, err := r.client.Git.CreateTree(ctx, owner, repoName, baseCommit.GetTree().GetSHA(), []*github.TreeEntry{entry})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create tree", err.Error())
		return
	}

	commit := &github.Commit{
		Message: ptr(plan.CommitMessage.ValueString()),
		Tree:    &github.Tree{SHA: newTree.SHA},
		Parents: []*github.Commit{{SHA: ptr(baseSHA)}},
	}
	if !plan.AuthorName.IsNull() || !plan.AuthorEmail.IsNull() {
		commit.Author = &github.CommitAuthor{
			Name:  ptr(plan.AuthorName.ValueString()),
			Email: ptr(plan.AuthorEmail.ValueString()),
		}
	}

	newCommit, _, err := r.client.Git.CreateCommit(ctx, owner, repoName, commit, nil)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create commit", err.Error())
		return
	}

	// Advance the branch ref to the new commit.
	_, _, err = r.client.Git.UpdateRef(ctx, owner, repoName, &github.Reference{
		Ref:    ptr(branchRef),
		Object: &github.GitObject{SHA: newCommit.SHA},
	}, false)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update branch ref", err.Error())
		return
	}

	plan.ID = types.StringValue(newCommit.GetSHA())
	plan.CommitSHA = types.StringValue(newCommit.GetSHA())
	plan.TreeSHA = types.StringValue(newTree.GetSHA())

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// resolveBranchHead returns the HEAD commit SHA of branch. If the branch does
// not exist and fromBranch is set, it creates the branch from fromBranch's HEAD
// and returns that SHA. Returns ok=false (and sets diagnostics) on error.
func (r *commitResource) resolveBranchHead(ctx context.Context, owner, repoName, branch string, fromBranch types.String, resp *resource.CreateResponse) (string, bool) {
	ref, httpResp, err := r.client.Git.GetRef(ctx, owner, repoName, "refs/heads/"+branch)
	if err == nil {
		return ref.GetObject().GetSHA(), true
	}

	// Only attempt creation on a genuine 404.
	if httpResp == nil || httpResp.StatusCode != http.StatusNotFound {
		resp.Diagnostics.AddError("Failed to read branch", fmt.Sprintf("branch %q: %s", branch, err.Error()))
		return "", false
	}

	if fromBranch.IsNull() || fromBranch.ValueString() == "" {
		resp.Diagnostics.AddError(
			"Branch does not exist",
			fmt.Sprintf("Branch %q does not exist. Set `from_branch` to have the provider create it.", branch),
		)
		return "", false
	}

	fromRef, _, err := r.client.Git.GetRef(ctx, owner, repoName, "refs/heads/"+fromBranch.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read from_branch", err.Error())
		return "", false
	}
	baseSHA := fromRef.GetObject().GetSHA()

	_, _, err = r.client.Git.CreateRef(ctx, owner, repoName, &github.Reference{
		Ref:    ptr("refs/heads/" + branch),
		Object: &github.GitObject{SHA: ptr(baseSHA)},
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create branch", err.Error())
		return "", false
	}
	return baseSHA, true
}

func (r *commitResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state commitResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Verify the commit still exists. If it was garbage-collected or the repo
	// is gone, remove the resource from state so it can be recreated.
	if state.CommitSHA.IsNull() || state.CommitSHA.ValueString() == "" {
		return
	}
	_, httpResp, err := r.client.Git.GetCommit(ctx, state.Owner.ValueString(), state.Repository.ValueString(), state.CommitSHA.ValueString())
	if err != nil {
		if httpResp != nil && httpResp.StatusCode == http.StatusNotFound {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read commit", err.Error())
		return
	}
	// Commit is immutable; nothing to reconcile.
}

func (r *commitResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// All mutable inputs are RequiresReplace, so Update is never expected to run.
	// Implemented defensively to satisfy the interface.
	var plan commitResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *commitResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
	// Git history is append-only — a commit cannot be un-made. Deleting this
	// resource only removes it from Terraform state; the commit remains.
}
