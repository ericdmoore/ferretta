.DEFAULT_GOAL := help

export CGO_ENABLED := 0
export GOTOOLCHAIN := go$(shell cat .go-version)

PKG ?= ./...
TEST ?= .
ARGS ?= --help
VERSION ?= dev

.PHONY: help check fmt lint test test-network test-network-app test-network-models test-network-model-tools coverage coverage-html build build-all package run install-hooks coverage-update

help: ## Show common development commands (also the default for make)
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  make %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@printf '\nExamples:\n  make test PKG=./internal/intent TEST=TestParse\n  make run ARGS="intent parse" < comment.md\n'

check: ## Run the complete local/CI checks, including the coverage ratchet
	@sh scripts/check.sh

fmt: ## Format Go code with the pinned toolchain
	@git ls-files --cached --others --exclude-standard '*.go' | xargs "$$(go env GOROOT)/bin/gofmt" -w

lint: ## Run go vet with the pinned toolchain
	@go vet ./...

test: ## Run fresh offline tests; optionally set PKG and TEST
	@go test -count=1 $(PKG) -run '$(TEST)'

test-network: ## Run live GitHub tests; requires FERRETTA_TEST_REPO and FERRETTA_TEST_PR
	@go test -count=1 -tags=network -run '^TestNetworkComments$$' ./internal/github

test-network-app: ## Verify configured GitHub App against FERRETTA_TEST_REPO/PR (read-only)
	@go test -count=1 -tags=network -run '^TestNetworkApp$$' ./internal/github

test-network-models: ## Read model inventory only; requires FERRETTA_TEST_MODEL_API/ENDPOINT/MODEL
	@go test -count=1 -tags=network -run '^TestNetworkModelInventory$$' ./internal/review

test-network-model-tools: ## Explicitly run two inference turns; requires FERRETTA_TEST_MODEL_POLICY
	@go test -count=1 -tags=network -run '^TestNetworkModelTools$$' ./internal/review

coverage: ## Run the full offline suite and print coverage without changing the baseline
	@go test -count=1 -coverprofile=.coverage.out ./...
	@go tool cover -func=.coverage.out

coverage-html: coverage ## Generate a browsable coverage report at bin/coverage.html
	@mkdir -p bin
	@go tool cover -html=.coverage.out -o bin/coverage.html
	@printf 'Coverage report: bin/coverage.html\n'

build: ## Build the native CLI at bin/ferretta
	@go build -o bin/ferretta ./cmd/ferretta

build-all: ## Build Linux/macOS binaries for amd64/arm64 in bin/
	@set -e; for platform in darwin linux; do \
		for architecture in amd64 arm64; do \
			GOOS=$$platform GOARCH=$$architecture go build -o bin/ferretta-$$platform-$$architecture ./cmd/ferretta; \
		done; \
	done

run: build ## Build and run the CLI; set ARGS (defaults to --help)
	@./bin/ferretta $(ARGS)

package: build-all ## Package all binaries and checksums in dist/; set VERSION=vX.Y.Z
	@sh scripts/package.sh '$(VERSION)'

coverage-update: ## Run all checks and retain improved coverage in the baseline
	@UPDATE_COVERAGE=1 sh scripts/check.sh

install-hooks: ## Enable the local pre-commit checks for this checkout
	@git config core.hooksPath .githooks

.PHONY: tools-site site site-check site-serve test-installer

tools-site: ## Install the pinned pure-Go Hugo compiler into bin/tools (network required)
	@mkdir -p bin/tools
	@GOBIN="$(CURDIR)/bin/tools" go install github.com/gohugoio/hugo@v$$(cat .hugo-version)

site: ## Build the Hugo site into bin/site (run tools-site first)
	@sh scripts/site.sh build

site-check: site ## Build and check the site's local links and installer copy
	@go test -count=1 -tags=site ./internal/sitetest

site-serve: ## Preview Hugo at localhost:1313 (run tools-site first)
	@sh scripts/site.sh serve

test-installer: ## Exercise the installer with offline release and command fixtures
	@sh -n install.sh
	@go test -count=1 ./internal/installtest
