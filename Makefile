.DEFAULT_GOAL := help
.PHONY: help build test vet lint clean preview up destroy tidy hooks

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
