.PHONY: build test clean fleetlift-worker fleetlift sandbox-image all temporal-dev temporal-up temporal-down temporal-logs sandbox-build generate manifests controller-image fleetlift-controller

# Build all binaries
all: build

# Build binaries
build:
	go build -o bin/fleetlift-worker ./cmd/worker
	go build -o bin/fleetlift ./cmd/cli

# Build worker only
fleetlift-worker:
	go build -o bin/fleetlift-worker ./cmd/worker

# Build CLI only
fleetlift:
	go build -o bin/fleetlift ./cmd/cli

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
run-worker: fleetlift-worker
	./bin/fleetlift-worker

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

# Generate code (deepcopy, etc.)
generate:
	controller-gen object paths="./api/..."

# Generate CRD manifests
manifests:
	controller-gen crd paths="./api/..." output:crd:artifacts:config=config/crd/bases

# Build controller binary
fleetlift-controller:
	go build -o bin/fleetlift-controller ./cmd/controller

# Build controller Docker image
controller-image:
	docker build -f docker/Dockerfile.controller -t fleetlift-controller:latest .
