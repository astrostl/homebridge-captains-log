# Homebridge Captain's Log Build Script

BINARY_NAME=hb-clog
GO_VERSION=1.24.5

# Tool versions (pinned for reproducible builds)
REVIVE_VERSION=v1.11.0
GOCYCLO_VERSION=v0.6.0
GOIMPORTS_VERSION=v0.28.0
STATICCHECK_VERSION=2025.1.1
GOSEC_VERSION=v2.22.5
INEFFASSIGN_VERSION=v0.1.0
MISSPELL_VERSION=v0.7.0
GOVULNCHECK_VERSION=v1.1.3
DEADCODE_VERSION=latest

.PHONY: build test fmt fmts vet mod verify vulncheck clean lint cyclo imports staticcheck gosec ineffassign misspell deadcode depscan security quality check all install deps help

# Individual targets
build: ## Build the binary
	go build -o $(BINARY_NAME)

test: ## Run tests
	go test ./...

fmt: ## Format code
	go fmt ./...

fmts: ## Simplify code formatting
	gofmt -s -w .

vet: ## Run static analysis
	go vet ./...

mod: ## Tidy dependencies
	go mod tidy

verify: ## Verify module dependencies
	go mod verify

vulncheck: ## Check for known vulnerabilities
	@command -v $$(go env GOPATH)/bin/govulncheck >/dev/null 2>&1 || go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	$$(go env GOPATH)/bin/govulncheck ./...

lint: ## Run revive linter with configuration
	@command -v $$(go env GOPATH)/bin/revive >/dev/null 2>&1 || go install github.com/mgechev/revive@$(REVIVE_VERSION)
	$$(go env GOPATH)/bin/revive -config .revive.toml -set_exit_status ./...

cyclo: ## Check cyclomatic complexity (threshold 15)
	@command -v $$(go env GOPATH)/bin/gocyclo >/dev/null 2>&1 || go install github.com/fzipp/gocyclo/cmd/gocyclo@$(GOCYCLO_VERSION)
	$$(go env GOPATH)/bin/gocyclo -over 15 .

imports: ## Check import formatting
	@command -v $$(go env GOPATH)/bin/goimports >/dev/null 2>&1 || go install golang.org/x/tools/cmd/goimports@$(GOIMPORTS_VERSION)
	$$(go env GOPATH)/bin/goimports -d .

staticcheck: ## Run enhanced static analysis
	@command -v $$(go env GOPATH)/bin/staticcheck >/dev/null 2>&1 || go install honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)
	$$(go env GOPATH)/bin/staticcheck ./...

gosec: ## Run security vulnerability scanner with configuration
	@command -v $$(go env GOPATH)/bin/gosec >/dev/null 2>&1 || go install github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION)
	$$(go env GOPATH)/bin/gosec -conf .gosec.json -fmt sarif -out gosec-report.sarif ./...
	$$(go env GOPATH)/bin/gosec -conf .gosec.json ./...

ineffassign: ## Detect ineffectual assignments
	@command -v $$(go env GOPATH)/bin/ineffassign >/dev/null 2>&1 || go install github.com/gordonklaus/ineffassign@$(INEFFASSIGN_VERSION)
	$$(go env GOPATH)/bin/ineffassign ./...

misspell: ## Check for common spelling errors
	@command -v $$(go env GOPATH)/bin/misspell >/dev/null 2>&1 || go install github.com/golangci/misspell/cmd/misspell@$(MISSPELL_VERSION)
	$$(go env GOPATH)/bin/misspell -error .

deadcode: ## Detect unused (dead) code
	@command -v $$(go env GOPATH)/bin/deadcode >/dev/null 2>&1 || go install golang.org/x/tools/cmd/deadcode@$(DEADCODE_VERSION)
	$$(go env GOPATH)/bin/deadcode ./...

depscan: ## Enhanced dependency vulnerability scanning
	@echo "Running comprehensive dependency security scan..."
	@command -v $$(go env GOPATH)/bin/govulncheck >/dev/null 2>&1 || go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	$$(go env GOPATH)/bin/govulncheck -json ./... > govulncheck-report.json || true
	$$(go env GOPATH)/bin/govulncheck ./...
	@echo "Checking for dependency updates with security implications..."
	go list -u -m all | grep -E "\[.*\]" || echo "All dependencies are up to date"
	@echo "Generating module dependency graph for security review..."
	go mod graph > deps-graph.txt

security: gosec depscan ## Comprehensive security scan
	@echo "Security scan complete. Check gosec-report.sarif and govulncheck-report.json for detailed results."


clean: ## Remove build artifacts
	rm -f $(BINARY_NAME)
	rm -rf .codeql-db codeql-results.sarif
	rm -f gosec-report.sarif govulncheck-report.json deps-graph.txt

# Suite targets
quality: fmt fmts vet verify vulncheck lint cyclo imports staticcheck security ineffassign misspell deadcode ## Run comprehensive quality checks including security
check: fmt vet test ## Run basic quality checks (fmt, vet, test)

all: quality test build ## Full build pipeline (quality + test + build)

install: build ## Install binary to GOPATH/bin
	go install

deps: ## Install all development tools
	go install github.com/mgechev/revive@$(REVIVE_VERSION)
	go install github.com/fzipp/gocyclo/cmd/gocyclo@$(GOCYCLO_VERSION)
	go install golang.org/x/tools/cmd/goimports@$(GOIMPORTS_VERSION)
	go install honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)
	go install github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION)
	go install github.com/gordonklaus/ineffassign@$(INEFFASSIGN_VERSION)
	go install github.com/golangci/misspell/cmd/misspell@$(MISSPELL_VERSION)
	go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	go install golang.org/x/tools/cmd/deadcode@$(DEADCODE_VERSION)

# Utility targets
help: ## Show this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'

# Default target
.DEFAULT_GOAL := all