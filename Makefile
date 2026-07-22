GO      ?= go
BINARY  := bin/pairflix
MODULE  := github.com/yohang/pairflix

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X $(MODULE)/internal/cli.version=$(VERSION) \
	-X $(MODULE)/internal/cli.commit=$(COMMIT) \
	-X $(MODULE)/internal/cli.date=$(DATE)

.PHONY: all
all: lint test build

.PHONY: build
build: ## Build the pairflix binary into bin/
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/pairflix

.PHONY: test
test: ## Run tests with race detector and coverage
	$(GO) test -race -shuffle=on -coverprofile=coverage.out ./...

.PHONY: cover
cover: test ## Open HTML coverage report
	$(GO) tool cover -html=coverage.out -o coverage.html

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run

.PHONY: fmt
fmt: ## Format code (gofumpt + goimports via golangci-lint)
	golangci-lint fmt

.PHONY: vuln
vuln: ## Check for known vulnerabilities
	govulncheck ./...

.PHONY: tidy
tidy: ## Tidy go.mod/go.sum
	$(GO) mod tidy

.PHONY: tools
tools: ## Install development tools
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	$(GO) install golang.org/x/vuln/cmd/govulncheck@latest
	$(GO) install github.com/evilmartians/lefthook@latest
	$(GO) install github.com/goreleaser/goreleaser/v2@latest

.PHONY: hooks
hooks: ## Install git hooks via lefthook
	lefthook install

.PHONY: clean
clean: ## Remove build and test artifacts
	rm -rf bin dist coverage.out coverage.html

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-10s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
