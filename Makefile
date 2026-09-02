.PHONY: help lint test test-integration proto proto-breaking build up down migrate-up migrate-create seed ci

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "%-18s %s\n", $$1, $$2}'

lint: ## Run golangci-lint across all modules (TODO: no modules yet)
	@echo "TODO: no Go modules in go.work yet"

test: ## Run unit tests across all modules (TODO: no modules yet)
	@echo "TODO: no Go modules in go.work yet"

test-integration: ## Run testcontainers integration tests (TODO: added in Phase 1 step 9)
	@echo "TODO: not implemented yet"

proto: ## Generate code from proto definitions via buf (TODO: added in Phase 1 step 3)
	@echo "TODO: not implemented yet"

proto-breaking: ## Run buf breaking-change check (TODO: added in Phase 1 step 3)
	@echo "TODO: not implemented yet"

build: ## Build all service binaries (TODO: no services yet)
	@echo "TODO: no services yet"

up: ## Start the local dev stack via docker compose (TODO: added in Phase 1 step 10)
	@echo "TODO: not implemented yet"

down: ## Stop the local dev stack (TODO: added in Phase 1 step 10)
	@echo "TODO: not implemented yet"

migrate-up: ## Apply pending goose migrations (TODO: added with wallet Postgres adapter)
	@echo "TODO: not implemented yet"

migrate-create: ## Create a new goose migration (TODO: added with wallet Postgres adapter)
	@echo "TODO: not implemented yet"

seed: ## Seed demo data (TODO: added in Phase 1 step 10)
	@echo "TODO: not implemented yet"

ci: lint test ## Run the checks CI runs
