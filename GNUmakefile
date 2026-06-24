BINARY  := terraform-provider-ghflow
VERSION ?= dev

default: build

.PHONY: build
build:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY)

.PHONY: install
install:
	go install -ldflags "-X main.version=$(VERSION)"

.PHONY: test
test:
	go test ./... -v

.PHONY: vet
vet:
	go vet ./...

.PHONY: fmt
fmt:
	gofmt -w .

.PHONY: lint
lint:
	gofmt -l .
	go vet ./...

# Generate registry docs from schema + examples (requires tfplugindocs).
# go install github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@latest
.PHONY: docs
docs:
	tfplugindocs generate --provider-name ghflow

# Build and place the binary for local dev_overrides testing.
.PHONY: dev-install
dev-install: build
	@echo "Add this to ~/.tofurc (or ~/.terraformrc):"
	@echo ""
	@echo 'provider_installation {'
	@echo '  dev_overrides {'
	@echo '    "registry.opentofu.org/ImIOImI/ghflow" = "$(CURDIR)"'
	@echo '  }'
	@echo '  direct {}'
	@echo '}'
