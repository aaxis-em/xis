# xis Makefile. Run `make help` for the targets.

BINARY  := xis
PKG     := github.com/aaxis-em/xis
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X $(PKG)/internal/buildinfo.Version=$(VERSION) \
           -X $(PKG)/internal/buildinfo.Commit=$(COMMIT) \
           -X $(PKG)/internal/buildinfo.Date=$(DATE)
GOBIN   := $(or $(GOBIN),$(shell go env GOPATH)/bin)
GOLANGCI := $(shell command -v golangci-lint 2>/dev/null || echo $(GOBIN)/golangci-lint)

export CGO_ENABLED = 0

.PHONY: help build install test race lint vet docs-lint check e2e fixtures fixtures-update bench snapshot clean

help: ## Show the targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'

build: ## Build bin/xis
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/xis

install: ## Install xis into GOBIN
	go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/xis

test: ## Run the unit tests
	go test ./...

race: ## Run the tests with the race detector (needs cgo)
	CGO_ENABLED=1 go test -race -gcflags=all=-d=checkptr=0 ./...

vet: ## Run go vet
	go vet ./...

lint: vet ## Run golangci-lint
	$(GOLANGCI) run ./...

docs-lint: ## Check the documents against the writing rules (warns only)
	go run ./internal/tools/doclint README.md CONTRIBUTING.md docs

check: ## Run gofmt, lint, tests and a strict docs-lint (the gate before each commit)
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "run gofmt -w ."; exit 1; }
	$(MAKE) lint
	go test ./...
	go run ./internal/tools/doclint -strict README.md CONTRIBUTING.md docs

e2e: ## Run the end-to-end tests
	go test -run 'E2E' -count=1 ./...

bench: ## Run the hook latency benchmarks
	go test -run '^$$' -bench . -benchtime 50x ./internal/capture/ ./internal/snapshot/

fixtures: ## Check that the golden files match
	go test -count=1 ./internal/adapters/... ./internal/report/... ./internal/attribution/...

fixtures-update: ## Regenerate the golden files
	go test -count=1 ./internal/adapters/... ./internal/report/... ./internal/attribution/... -update

snapshot: ## Build a local release with GoReleaser
	goreleaser release --snapshot --clean -p 2

clean: ## Remove build output
	rm -rf bin dist
