package provider

import (
	"context"
	"net/url"
	"os"
	"strings"

	"github.com/google/go-github/v66/github"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure the implementation satisfies the provider.Provider interface.
var _ provider.Provider = (*ghflowProvider)(nil)

// ghflowProvider is the provider implementation.
type ghflowProvider struct {
	// version is set to the provider version on release, "dev" otherwise.
	version string
}

// ghflowProviderModel maps provider schema data to a Go type.
type ghflowProviderModel struct {
	Token   types.String `tfsdk:"token"`
	BaseURL types.String `tfsdk:"base_url"`
}

// New returns a function that constructs the provider, wired with its version.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &ghflowProvider{version: version}
	}
}

func (p *ghflowProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "ghflow"
	resp.Version = p.version
}

func (p *ghflowProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Interact with GitHub to commit files, open pull requests, and merge them as managed resources. " +
			"Authenticates with a personal access token (PAT).",
		Attributes: map[string]schema.Attribute{
			"token": schema.StringAttribute{
				MarkdownDescription: "GitHub personal access token. May also be set with the `GITHUB_TOKEN` environment variable.",
				Optional:            true,
				Sensitive:           true,
			},
			"base_url": schema.StringAttribute{
				MarkdownDescription: "Base URL of the GitHub API, for GitHub Enterprise Server. " +
					"Example: `https://ghe.example.com/`. May also be set with the `GITHUB_BASE_URL` environment variable. " +
					"Defaults to the public GitHub API.",
				Optional: true,
			},
		},
	}
}

func (p *ghflowProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config ghflowProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if config.Token.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			pathRoot("token"),
			"Unknown GitHub API token",
			"The provider cannot create the GitHub client because the token is an unknown value. "+
				"Set the value statically or via the GITHUB_TOKEN environment variable.",
		)
		return
	}

	token := os.Getenv("GITHUB_TOKEN")
	if !config.Token.IsNull() {
		token = config.Token.ValueString()
	}

	baseURL := os.Getenv("GITHUB_BASE_URL")
	if !config.BaseURL.IsNull() {
		baseURL = config.BaseURL.ValueString()
	}

	if token == "" {
		resp.Diagnostics.AddAttributeError(
			pathRoot("token"),
			"Missing GitHub API token",
			"The provider requires a GitHub token. Set the `token` attribute or the GITHUB_TOKEN environment variable.",
		)
		return
	}

	client := github.NewClient(nil).WithAuthToken(token)

	if baseURL != "" {
		// WithEnterpriseURLs normalizes the API/upload URLs for GitHub Enterprise.
		if !strings.HasSuffix(baseURL, "/") {
			baseURL += "/"
		}
		if _, err := url.Parse(baseURL); err != nil {
			resp.Diagnostics.AddAttributeError(
				pathRoot("base_url"),
				"Invalid base_url",
				"Could not parse base_url: "+err.Error(),
			)
			return
		}
		enterpriseClient, err := client.WithEnterpriseURLs(baseURL, baseURL)
		if err != nil {
			resp.Diagnostics.AddAttributeError(
				pathRoot("base_url"),
				"Invalid GitHub Enterprise base_url",
				err.Error(),
			)
			return
		}
		client = enterpriseClient
	}

	// Make the configured client available to resources via Configure.
	resp.ResourceData = client
	resp.DataSourceData = client
}

func (p *ghflowProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewCommitResource,
		NewPullRequestResource,
		NewPRMergeResource,
	}
}

func (p *ghflowProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}
