# doctopus developer Makefile
BINARY      := doctopus
PKG         := ./...
BIN_DIR     := bin
GOFLAGS     ?=

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the doctopus binary into ./bin
	@mkdir -p $(BIN_DIR)
	go build $(GOFLAGS) -o $(BIN_DIR)/$(BINARY) ./cmd/doctopus

.PHONY: install
install: ## Install doctopus into GOBIN
	go install $(GOFLAGS) ./cmd/doctopus

.PHONY: test
test: ## Run unit + smoke tests
	go test $(GOFLAGS) -race -count=1 $(PKG)

.PHONY: test-integration
test-integration: ## Run integration / golden tests
	go test $(GOFLAGS) -race -count=1 -tags=integration $(PKG)

.PHONY: cover
cover: ## Run tests with coverage report (includes integration/golden code)
	go test $(GOFLAGS) -tags=integration -race -covermode=atomic -coverprofile=coverage.out $(PKG)
	go tool cover -func=coverage.out | tail -1

.PHONY: vet
vet: ## Run go vet
	go vet $(PKG)

.PHONY: lint
lint: ## Run golangci-lint (must be installed)
	golangci-lint run

.PHONY: tidy
tidy: ## Tidy and verify modules
	go mod tidy
	go mod verify

.PHONY: fmt
fmt: ## Format the codebase
	gofmt -s -w .

.PHONY: check
check: fmt vet lint test ## Run the full local verification suite

.PHONY: clean
clean: ## Remove build and coverage artifacts
	rm -rf $(BIN_DIR) coverage.out out
