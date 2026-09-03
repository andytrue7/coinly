.PHONY: help lint test test-integration proto proto-breaking build up down migrate-up migrate-create seed ci

GOLANGCI_LINT_IMAGE := golangci/golangci-lint:v2.13.2
BUF_IMAGE := bufbuild/buf:1.72.0

# No .proto files exist until the first package is added (identity/v1, in
# Phase 1 step 3); proto/proto-breaking no-op gracefully until then, same
# pattern as lint/test on an empty go.work. `buf generate` uses remote
# plugins (buf.build/protocolbuffers/go, buf.build/grpc/go) so the Docker
# image only needs the buf CLI itself — no protoc-gen-go install, local or
# in-image.
PROTO_FILES := $(shell find proto -name '*.proto' 2>/dev/null)

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "%-18s %s\n", $$1, $$2}'

# go.work has no `use` entries until the first module is added (pkg/money,
# in Phase 1 step 2); lint/test no-op gracefully until then. Once modules
# exist, lint/test iterate each workspace module directory individually and
# run `./...` from inside it, rather than `./...` from the repo root: the
# repo root itself has no go.mod (only services/modules under it do), and
# both `go test` and `golangci-lint` refuse a `./...` pattern whose
# directory prefix isn't itself inside a `use`d module — confirmed by
# running each against this workspace directly.
# `all` lists every module in the build graph, workspace members and their
# transitive dependencies alike; `{{if .Main}}` filters to just the
# workspace's own modules — a distinction that didn't matter while pkg had
# zero third-party deps, but broke once gen/go's grpc/protobuf deps entered
# the graph (lint tried to run inside the module cache and errored).
MODULE_DIRS := $(shell go list -m -f '{{if .Main}}{{.Dir}}{{end}}' all 2>/dev/null)

lint: ## Run golangci-lint (pinned, via Docker) across all modules
	@if [ -z "$(MODULE_DIRS)" ]; then \
		echo "No Go modules in go.work yet — skipping lint."; \
	else \
		for d in $(MODULE_DIRS); do \
			rel=$${d#$$PWD/}; \
			echo "==> lint $$rel"; \
			docker run --rm -v "$$PWD":/workspace -w "/workspace/$$rel" $(GOLANGCI_LINT_IMAGE) golangci-lint run ./... || exit 1; \
		done; \
	fi

test: ## Run unit tests across all modules
	@if [ -z "$(MODULE_DIRS)" ]; then \
		echo "No Go modules in go.work yet — skipping tests."; \
	else \
		for d in $(MODULE_DIRS); do \
			echo "==> test $$d"; \
			(cd "$$d" && go test -race -cover ./...) || exit 1; \
		done; \
	fi

test-integration: ## Run testcontainers integration tests (TODO: added in Phase 1 step 9)
	@echo "TODO: not implemented yet"

proto: ## Lint and generate code from proto definitions via buf (Docker)
	@if [ -z "$(PROTO_FILES)" ]; then \
		echo "No .proto files yet — skipping proto lint/generate."; \
	else \
		echo "==> buf lint"; \
		docker run --rm -v "$$PWD":/workspace -w /workspace/proto $(BUF_IMAGE) lint || exit 1; \
		echo "==> buf generate"; \
		docker run --rm -v "$$PWD":/workspace -w /workspace/proto $(BUF_IMAGE) generate || exit 1; \
	fi

proto-breaking: ## Run buf breaking-change check against main (Docker)
	@if [ -z "$(PROTO_FILES)" ]; then \
		echo "No .proto files yet — skipping breaking check."; \
	elif [ -z "$$(git ls-tree -r main --name-only -- proto 2>/dev/null | grep '\.proto$$')" ]; then \
		echo "main has no .proto files yet — nothing to check breaking changes against."; \
	else \
		docker run --rm -v "$$PWD":/workspace -w /workspace/proto $(BUF_IMAGE) breaking --against '../.git#branch=main,subdir=proto' || exit 1; \
	fi

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

ci: lint test proto proto-breaking ## Run the checks CI runs
