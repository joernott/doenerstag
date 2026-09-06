# doenerstag build entry point.
#
# The frontend targets shell out to npm in frontend/, so `make release` needs
# the Node toolchain installed; `make build` does not, because a development
# binary serves its assets from --static-dir rather than carrying them.
# See docs/08_technologies.md for the full target list.

SHELL := /bin/sh

BINARY      := doenerstag
CMD         := ./cmd/doenerstag
DIST        := dist

# Version reported by `doenerstag --version` and stamped into the binary.
#
# It must be major.minor.patch, because install records it in app_version and
# update compares against it. --always is deliberately absent: in a repository
# with no tags it returns the bare commit hash, which built a binary whose
# install verb failed after it had already created the database. Falling back
# to 0.0.0-dev is right for an untagged build, and the hash is not lost -- it
# is stamped separately as COMMIT.
VERSION     ?= $(shell v=$$(git describe --tags --match 'v[0-9]*' --dirty 2>/dev/null | sed 's/^v//'); echo $${v:-0.0.0-dev})
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
deps-frontend: ## Install the frontend toolchain
	cd frontend && npm ci

# --- build -------------------------------------------------------------------

.PHONY: build
build: ## Build a development binary; assets are served from --static-dir
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD)

.PHONY: release
release: frontend ## Build a release binary with the frontend embedded
	$(GO) build $(GOFLAGS) -tags embedstatic -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) $(CMD)

.PHONY: frontend
frontend: ## Build the frontend into static/
	cd frontend && npm run build

.PHONY: dev
dev: build ## Frontend watch mode plus a development binary
	cd frontend && npm run watch

# --- quality -----------------------------------------------------------------

.PHONY: test
test: test-frontend ## Run the Go and frontend tests
	$(GO) test $(GOFLAGS) ./...

# Skipped rather than failed when the toolchain is absent, the same way lint
# handles a missing golangci-lint: a machine that can build a development
# binary is not required to have Node, and CI has both.
.PHONY: test-frontend
test-frontend: ## Type-check and unit-test the frontend
	@if [ -d frontend/node_modules ]; then \
		cd frontend && npm run typecheck && npm test; \
	else \
		echo "test-frontend: frontend/node_modules missing, run make deps-frontend (skipping)"; \
	fi

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
lint: fmt-check vet lint-frontend ## Run every linter
	@# An `a && b || c` chain here would swallow b's exit status: a linter that
	@# ran and found problems would take the || branch and report success.
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "lint: golangci-lint not installed, skipping (see docs/12_testing.md)"; \
	fi

.PHONY: e2e
e2e: ## Run the Playwright suite against a running server
	@# The server is not started here: it needs a database, and where that is
	@# depends on the machine. Point the suite at one with DOENER_E2E_URL,
	@# which defaults to https://localhost:8443.
	cd frontend && npx playwright test

.PHONY: lint-frontend
lint-frontend: ## Type-check and lint the frontend
	@if [ -d frontend/node_modules ]; then \
		cd frontend && npm run typecheck && npm run lint; \
	else \
		echo "lint-frontend: frontend/node_modules missing, run make deps-frontend (skipping)"; \
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
