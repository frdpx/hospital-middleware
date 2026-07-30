.PHONY: help tidy fmt vet lint build run test test-cover up down logs ps rebuild

BINARY := bin/api
PKG    := ./...

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

tidy: ## Sync go.mod/go.sum with imports
	go mod tidy

fmt: ## Format all Go source
	gofmt -s -w .

vet: ## Run go vet
	go vet $(PKG)

build: ## Compile the API binary
	go build -o $(BINARY) ./cmd/api

run: ## Run the API against a locally reachable Postgres
	go run ./cmd/api

test: ## Run all unit tests
	go test $(PKG) -count=1

test-cover: ## Run tests and print per-package coverage
	go test $(PKG) -count=1 -coverprofile=coverage.out
	go tool cover -func=coverage.out | tail -20

check: fmt vet test ## Format, vet and test — run this before committing

up: ## Start nginx + api + postgres
	docker compose up -d --build

down: ## Stop the stack and remove volumes
	docker compose down -v

logs: ## Tail all service logs
	docker compose logs -f

ps: ## Show stack status
	docker compose ps
