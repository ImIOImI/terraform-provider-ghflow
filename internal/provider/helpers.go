package provider

import (
	"strconv"

	"github.com/google/go-github/v66/github"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
)

// ptr returns a pointer to v. Used instead of github.Ptr/github.String so the
// code does not depend on which pointer helpers a given go-github version ships.
func ptr[T any](v T) *T {
	return &v
}

// pathRoot is a thin wrapper around path.Root for readability.
func pathRoot(name string) path.Path {
	return path.Root(name)
}

// intToString renders an int as a decimal string (used for resource IDs).
func intToString(i int) string {
	return strconv.Itoa(i)
}

// int64RequiresReplace returns the RequiresReplace plan modifier for int64 attributes.
func int64RequiresReplace() planmodifier.Int64 {
	return int64planmodifier.RequiresReplace()
}

// clientFromProviderData extracts the configured *github.Client that the
// provider's Configure method stored in ProviderData. It reports a diagnostic
// (and returns nil) if the data is missing or of an unexpected type. Resources
// call this from their own Configure method.
func clientFromProviderData(providerData any, resp *resource.ConfigureResponse) *github.Client {
	if providerData == nil {
		// Provider Configure has not run yet (e.g. during validation). Not an error.
		return nil
	}
	client, ok := providerData.(*github.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data type",
			"Expected *github.Client from the provider. This is a bug in the provider.",
		)
		return nil
	}
	return client
}
