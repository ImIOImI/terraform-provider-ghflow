package main

import (
	"context"
	"flag"
	"log"

	"github.com/ImIOImI/terraform-provider-ghflow/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

// version is set at build time via -ldflags by GoReleaser.
var version = "dev"

// Provider registry address. Works identically for the OpenTofu Registry
// (registry.opentofu.org) and the Terraform Registry — same plugin protocol.
const providerAddress = "registry.opentofu.org/ImIOImI/ghflow"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: providerAddress,
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
