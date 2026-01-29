.PHONY: build test clean worker cli sandbox-image all

# Build all binaries
all: build

# Build binaries
build:
	go build -o bin/worker ./cmd/worker
	go build -o bin/cli ./cmd/cli

# Build worker only
worker:
	go build -o bin/worker ./cmd/worker

# Build CLI only
cli:
	go build -o bin/cli ./cmd/cli

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

# Start Temporal dev server
temporal-dev:
	temporal server start-dev --ui-port 8233
