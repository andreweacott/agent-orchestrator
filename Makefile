.PHONY: build test clean worker orchestrator sandbox-image all temporal-dev temporal-up temporal-down temporal-logs sandbox-build

# Build all binaries
all: build

# Build binaries
build:
	go build -o bin/worker ./cmd/worker
	go build -o bin/orchestrator ./cmd/cli

# Build worker only
worker:
	go build -o bin/worker ./cmd/worker

# Build CLI only
orchestrator:
	go build -o bin/orchestrator ./cmd/cli

# Run tests
test:
	go test -v ./...

# Run tests with coverage
test-coverage:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Clean build artifacts
clean:
	rm -rf bin/
	rm -f coverage.out coverage.html

# Build the sandbox Docker image
sandbox-image:
	docker build -f ../docker/Dockerfile.sandbox -t claude-code-sandbox:latest ../docker

# Run the worker (requires Temporal server)
run-worker: worker
	./bin/worker

# Format code
fmt:
	go fmt ./...

# Lint code
lint:
	golangci-lint run

# Download dependencies
deps:
	go mod download
	go mod tidy

# Start Temporal dev server (lightweight, in-memory)
temporal-dev:
	temporal server start-dev --ui-port 8233

# Start Temporal with docker-compose (persistent, production-like)
temporal-up:
	docker compose up -d

# Stop Temporal docker-compose
temporal-down:
	docker compose down

# View Temporal logs
temporal-logs:
	docker compose logs -f temporal

# Build sandbox image
sandbox-build:
	docker build -f docker/Dockerfile.sandbox -t claude-code-sandbox:latest docker/
