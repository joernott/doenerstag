# doenerstag build entry point.
#
# Targets that need the Node toolchain are stubbed until sprint 4 (task 4.7).
# See docs/08_technologies.md for the full target list and docs/14_implementation_plan.md
# for what is implemented so far.

SHELL := /bin/sh

BINARY      := doenerstag
CMD         := ./cmd/doenerstag
DIST        := dist

# Version reported by `doenerstag --version` and stamped into the binary.
# Derived from git; falls back to 0.0.0-dev outside a repository.
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.0.0-dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE  ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

VERSION_PKG := github.com/joernott/doenerstag/internal/version
LDFLAGS     := -X $(VERSION_PKG).version=$(VERSION) \
               -X $(VERSION_PKG).commit=$(COMMIT) \
               -X $(VERSION_PKG).buildDate=$(BUILD_DATE)

GO          ?= go
GOFLAGS     ?=

# Coverage is kept per platform. Windows skips the configuration file
# permission check and has no SIGHUP log reopen, so a single merged number
# would average two different runs and hide which lines are unexercised on
# which platform.
GOOS_NAME     := $(shell $(GO) env GOOS)
COVERAGE_OUT  := $(GOOS_NAME)-coverage.out
COVERAGE_HTML := $(GOOS_NAME)-coverage.html

.DEFAULT_GOAL := build

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

# --- dependencies ------------------------------------------------------------

.PHONY: deps
deps: ## Download Go and frontend dependencies
	$(GO) mod download
	@$(MAKE) --no-print-directory deps-frontend

.PHONY: deps-frontend
deps-frontend:
	@echo "deps-frontend: not implemented until task 4.7 (npm ci in frontend/)"

# --- build -------------------------------------------------------------------

.PHONY: build
build: ## Build a development binary; assets are served from --static-dir
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD)

.PHONY: release
release: frontend ## Build a release binary with the frontend embedded
	$(GO) build $(GOFLAGS) -tags embedstatic -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) $(CMD)

.PHONY: frontend
frontend: ## Build the frontend into static/
	@echo "frontend: not implemented until task 4.7 (esbuild + Tailwind into static/)"

.PHONY: dev
dev: ## Frontend watch mode plus a development binary
	@echo "dev: not implemented until task 4.7"

# --- quality -----------------------------------------------------------------

.PHONY: test
test: ## Run the Go tests
	$(GO) test $(GOFLAGS) ./...

.PHONY: test-race
test-race: ## Run the Go tests with the race detector
	$(GO) test $(GOFLAGS) -race ./...

.PHONY: cover
cover: ## Run the tests and write <os>-coverage.out and <os>-coverage.html
	$(GO) test $(GOFLAGS) -coverprofile=$(COVERAGE_OUT) ./...
	$(GO) tool cover -html=$(COVERAGE_OUT) -o $(COVERAGE_HTML)
	@$(GO) tool cover -func=$(COVERAGE_OUT) | tail -1
	@echo "wrote $(COVERAGE_OUT) and $(COVERAGE_HTML)"

.PHONY: lint
lint: fmt-check vet ## Run every linter
	@# An `a && b || c` chain here would swallow b's exit status: a linter that
	@# ran and found problems would take the || branch and report success.
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "lint: golangci-lint not installed, skipping (see docs/12_testing.md)"; \
	fi

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: fmt
fmt: ## Format the Go sources in place
	$(GO) fmt ./...

.PHONY: fmt-check
fmt-check: ## Fail if any Go source is not gofmt-clean
	@unformatted="$$(gofmt -l . 2>/dev/null)"; \
	if [ -n "$$unformatted" ]; then \
		echo "not gofmt-clean:"; echo "$$unformatted"; exit 1; \
	fi

.PHONY: vuln
vuln: ## Check dependencies for known vulnerabilities
	@if command -v govulncheck >/dev/null 2>&1; then \
		govulncheck ./...; \
	else \
		echo "vuln: govulncheck not installed, skipping"; \
	fi

.PHONY: tidy
tidy: ## Tidy go.mod and go.sum
	$(GO) mod tidy

# --- housekeeping ------------------------------------------------------------

.PHONY: clean
clean: ## Remove build and test artefacts
	rm -f $(BINARY) $(BINARY).exe
	rm -f coverage.out coverage.html *-coverage.out *-coverage.html
	rm -rf $(DIST)

.PHONY: version
version: ## Print the version this build would stamp in
	@echo "$(VERSION) ($(COMMIT)) $(BUILD_DATE)"
