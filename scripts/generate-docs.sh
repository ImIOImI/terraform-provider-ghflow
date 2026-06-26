#!/usr/bin/env bash
# Generate registry docs with tfplugindocs using OpenTofu (no Terraform needed).
#
# tfplugindocs normally shells out to `terraform` to export the provider schema.
# In an OpenTofu-only environment that fails, so this script exports the schema
# with `tofu` via dev_overrides and feeds it to tfplugindocs --providers-schema.
#
# Requires: go, tofu, and tfplugindocs on PATH
#   go install github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@latest
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

SOURCE="registry.opentofu.org/ImIOImI/ghflow"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/plugins" "$TMP/cfg"

go build -o "$TMP/plugins/terraform-provider-ghflow" .

cat > "$TMP/dev.tofurc" <<EOF
provider_installation {
  dev_overrides {
    "$SOURCE" = "$TMP/plugins"
  }
  direct {}
}
EOF

cat > "$TMP/cfg/main.tf" <<EOF
terraform {
  required_providers {
    ghflow = { source = "$SOURCE" }
  }
}
EOF

# Export the schema with tofu. tfplugindocs looks up the provider under the
# registry.terraform.io/hashicorp/<name> address, so rewrite the key to match;
# this is cosmetic and does not affect the rendered docs (titles use --provider-name).
TF_CLI_CONFIG_FILE="$TMP/dev.tofurc" tofu -chdir="$TMP/cfg" providers schema -json \
  | sed 's#registry.opentofu.org/imioimi/ghflow#registry.terraform.io/hashicorp/ghflow#' \
  > "$TMP/schema.json"

tfplugindocs generate \
  --provider-name ghflow \
  --rendered-provider-name ghflow \
  --providers-schema "$TMP/schema.json"

echo "Docs generated under ./docs"
