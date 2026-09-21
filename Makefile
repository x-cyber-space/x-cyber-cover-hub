BINARY  := x-cyber-cover-hub
BIN_DIR := bin
PKG     := ./cmd/server
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

LDFLAGS := -s -w -X main.version=$(VERSION)

# golangci-lint is normally installed into $(go env GOPATH)/bin, which is not
# always on PATH. Resolve it explicitly so `make lint` works either way.
GOLANGCI_LINT ?= $(shell command -v golangci-lint 2>/dev/null || echo "$(shell go env GOPATH)/bin/golangci-lint")
GORELEASER    ?= $(shell command -v goreleaser 2>/dev/null || echo "$(shell go env GOPATH)/bin/goreleaser")

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the binary into bin/ (static, no CGO)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(PKG)

.PHONY: install
install: ## Install the binary into $(go env GOPATH)/bin
	CGO_ENABLED=0 go install -trimpath -ldflags "$(LDFLAGS)" $(PKG)

.PHONY: test
test: ## Run the offline test suite with the race detector
	go test -short -race -count=1 ./...

.PHONY: test-all
test-all: ## Run every test, including the live artwork smoke test (needs network)
	go test -race -count=1 ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: fmt
fmt: ## Format the source
	@gofmt -w $$(git ls-files '*.go')

.PHONY: fmt-check
fmt-check: ## Fail if anything is not gofmt-clean
	@files="$$(git ls-files '*.go')"; \
	if [ -z "$$files" ]; then echo "no tracked Go files found"; exit 1; fi; \
	out="$$(gofmt -l $$files)"; \
	if [ -n "$$out" ]; then echo "not gofmt-clean:"; echo "$$out"; exit 1; fi; \
	echo "gofmt: clean"

.PHONY: lint
lint: ## Run golangci-lint
	@test -x "$(GOLANGCI_LINT)" || { \
		echo "golangci-lint not found at $(GOLANGCI_LINT)"; \
		echo "install it with: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest"; \
		exit 1; }
	$(GOLANGCI_LINT) run

.PHONY: check
check: fmt-check vet test lint ## Run every check CI runs

# The subset needing no extra tooling installed. The release workflow uses this
# because golangci-lint is not preinstalled on GitHub runners, and CI runs lint
# in a dedicated job that installs it properly.
.PHONY: check-core
check-core: fmt-check vet test ## Run every check that needs no extra tooling

.PHONY: run
run: build ## Build and run locally
	./$(BIN_DIR)/$(BINARY)

.PHONY: tidy
tidy: ## Tidy the module graph
	go mod tidy

.PHONY: docker
docker: ## Build the container image
	docker build --build-arg VERSION=$(VERSION) -t $(BINARY):$(VERSION) .

.PHONY: release-check
release-check: ## Validate .goreleaser.yml
	@test -x "$(GORELEASER)" || { \
		echo "goreleaser not found at $(GORELEASER)"; \
		echo "install it with: go install github.com/goreleaser/goreleaser/v2@latest"; \
		exit 1; }
	$(GORELEASER) check

.PHONY: clean
clean: ## Remove build artifacts, runtime data and local caches
	rm -rf $(BIN_DIR) dist data .workcache .lintcache .evalcache
