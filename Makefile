# TaskForge Makefile
# Build and development automation

.PHONY: build test benchmark clean docker docker-build docker-run lint fmt vet deps help

# Variables
APP_NAME := taskforge
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GO_VERSION := $(shell go version | awk '{print $$3}')

# Build flags
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -X main.goVersion=$(GO_VERSION)"

# Default target
all: test build

help: ## Show this help message
	@echo 'Usage: make <target>'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Build targets
build: ## Build the application
	@echo "Building $(APP_NAME)..."
	go build $(LDFLAGS) -o bin/$(APP_NAME) ./examples/basic/

build-linux: ## Build for Linux (useful for Docker)
	@echo "Building $(APP_NAME) for Linux..."
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/$(APP_NAME)-linux ./examples/basic/

# Development targets
deps: ## Download dependencies
	@echo "Downloading dependencies..."
	go mod download
	go mod tidy

fmt: ## Format Go code
	@echo "Formatting code..."
	go fmt ./...

vet: ## Run go vet
	@echo "Running go vet..."
	go vet ./...

lint: ## Run golangci-lint
	@echo "Running linter..."
	golangci-lint run

# Testing targets
test: ## Run tests
	@echo "Running tests..."
	go test -v -race ./...

test-coverage: ## Run tests with coverage
	@echo "Running tests with coverage..."
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

benchmark: ## Run benchmarks
	@echo "Running benchmarks..."
	go test -bench=. -benchmem ./...

benchmark-cpu: ## Run CPU benchmarks with profiling
	@echo "Running CPU benchmark with profiling..."
	go test -bench=. -benchmem -cpuprofile=cpu.prof ./...
	@echo "CPU profile saved: cpu.prof"

benchmark-mem: ## Run memory benchmarks with profiling
	@echo "Running memory benchmark with profiling..."
	go test -bench=. -benchmem -memprofile=mem.prof ./...
	@echo "Memory profile saved: mem.prof"

# Docker targets
docker-build: ## Build Docker image
	@echo "Building Docker image..."
	docker build -t $(APP_NAME):$(VERSION) -t $(APP_NAME):latest .

docker-run: ## Run Docker container
	@echo "Running Docker container..."
	docker run --rm -p 8080:8080 --name $(APP_NAME) $(APP_NAME):latest

docker-compose-up: ## Start with docker-compose
	@echo "Starting with docker-compose..."
	docker-compose up -d

docker-compose-down: ## Stop docker-compose services
	@echo "Stopping docker-compose services..."
	docker-compose down

docker-compose-logs: ## Show docker-compose logs
	docker-compose logs -f

# Utility targets
clean: ## Clean build artifacts
	@echo "Cleaning..."
	rm -rf bin/
	rm -f coverage.out coverage.html
	rm -f cpu.prof mem.prof
	go clean -testcache

run: ## Run the application locally
	@echo "Running $(APP_NAME)..."
	go run ./examples/basic/

run-with-env: ## Run with environment variables
	@echo "Running $(APP_NAME) with custom configuration..."
	TASKFORGE_WORKERS=8 TASKFORGE_QUEUE_SIZE=2000 go run ./examples/basic/

# Release targets
release-check: ## Check if ready for release
	@echo "Checking release readiness..."
	@if [ -z "$(VERSION)" ]; then echo "No version tag found"; exit 1; fi
	@echo "Version: $(VERSION)"
	@echo "Build time: $(BUILD_TIME)"
	@echo "Go version: $(GO_VERSION)"

release-build: ## Build release binaries for multiple platforms
	@echo "Building release binaries..."
	mkdir -p dist
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/$(APP_NAME)-linux-amd64 ./examples/basic/
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/$(APP_NAME)-linux-arm64 ./examples/basic/
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o dist/$(APP_NAME)-darwin-amd64 ./examples/basic/
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o dist/$(APP_NAME)-darwin-arm64 ./examples/basic/
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o dist/$(APP_NAME)-windows-amd64.exe ./examples/basic/

# CI targets (used by CI/CD pipeline)
ci-test: deps fmt vet lint test ## Run all CI checks

ci-benchmark: benchmark ## Run benchmarks for CI

ci-coverage: test-coverage ## Generate coverage for CI

# Development workflow
dev: deps fmt vet test ## Full development check

install-tools: ## Install development tools
	@echo "Installing development tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Show project status
status: ## Show project status
	@echo "Project Status:"
	@echo "  Version: $(VERSION)"
	@echo "  Go Version: $(GO_VERSION)"
	@echo "  Build Time: $(BUILD_TIME)"
	@echo ""
	@echo "Git Status:"
	@git status --short
	@echo ""
	@echo "Dependencies:"
	@go list -m all | head -10