.PHONY: help lint test test-integration proto proto-breaking build up down migrate-up migrate-create seed ci

GOLANGCI_LINT_IMAGE := golangci/golangci-lint:v2.13.2

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "%-18s %s\n", $$1, $$2}'

# go.work has no `use` entries until the first module is added (pkg/money,
# in Phase 1 step 2). `go test`/`golangci-lint` error on an empty workspace,
# so lint/test no-op gracefully until then; both run for real automatically
# once a module exists.

lint: ## Run golangci-lint (pinned, via Docker) across all modules
	@if ! grep -q '^use' go.work 2>/dev/null; then \
		echo "No Go modules in go.work yet — skipping lint."; \
	else \
		docker run --rm -v "$$PWD":/workspace -w /workspace $(GOLANGCI_LINT_IMAGE) golangci-lint run ./...; \
	fi

test: ## Run unit tests across all modules
	@if ! grep -q '^use' go.work 2>/dev/null; then \
		echo "No Go modules in go.work yet — skipping tests."; \
	else \
		go test -race -cover ./...; \
	fi

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
