.DEFAULT_GOAL := help
.PHONY: help build test vet lint clean preview up destroy tidy hooks demo demo-clean

STACK ?= dev

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build: ## Build all packages
	go build ./...

test: ## Run all tests
	go test ./...

vet: ## Run go vet
	go vet ./...

lint: vet ## Alias for vet

clean: ## Clean build artifacts
	go clean ./...

tidy: ## Tidy go.mod dependencies
	go mod tidy

preview: ## Preview infrastructure changes (dry-run)
	FORGE_STACK=$(STACK) go run ./cmd/forge preview

up: ## Deploy infrastructure
	FORGE_STACK=$(STACK) go run ./cmd/forge up

destroy: ## Tear down infrastructure
	FORGE_STACK=$(STACK) go run ./cmd/forge destroy

hooks: ## Install git pre-commit hooks
	./scripts/install-hooks.sh

demo: ## Run the local cross-cloud federation proof (Apple container; DEMO_RUNTIME=docker for Docker)
	./demo/run.sh

demo-clean: ## Tear down demo containers, network, and generated artifacts
	-container rm -f spire-gcp-server spire-aws-server spire-gcp-agent spire-aws-agent forge-serve 2>/dev/null
	-container network rm forge-demo 2>/dev/null
	-docker compose -f demo/docker-compose.yml down 2>/dev/null
	rm -rf demo/generated demo/certs
