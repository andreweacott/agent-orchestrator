# Claude Code Orchestrator

A durable, scalable platform for automating code transformations and discovery across repositories using Claude Code and Temporal workflows.

## What is This?

This project enables you to run automated code changes and analysis tasks across multiple repositories with enterprise-grade reliability. Think of it as "Turbolift with AI capabilities" - it coordinates complex, multi-step transformations using Claude Code while Temporal ensures durability and recoverability.

### Key Features

- **Dual Execution Modes**
  - **Agentic**: Uses Claude Code CLI for AI-powered transformations
  - **Deterministic**: Runs custom Docker images for reproducible changes

- **Two Operation Modes**
  - **Transform Mode**: Makes code changes and creates pull requests
  - **Report Mode**: Analyzes repositories and collects structured data (discovery)

- **Enterprise Reliability**
  - Durable execution via Temporal (survives failures, network issues)
  - Human-in-the-loop approval gates
  - Parallel or sequential PR creation
  - Automatic retry and error handling

- **Advanced Discovery Patterns**
  - **forEach**: Analyze multiple targets within a single repository
  - **Transformation Repositories**: Reusable skills and tools across projects
  - **Schema Validation**: Structured output with JSON Schema enforcement

### Use Cases

**Code Transformations:**
- Apply security patches across microservices
- Migrate APIs or dependencies fleet-wide
- Refactor patterns across multiple repositories
- Enforce coding standards automatically

**Discovery & Analysis:**
- Security audits across all services
- Dependency inventory and version tracking
- Technical debt assessment before migrations
- Compliance scanning and reporting

## Architecture

```
┌─────────────┐         ┌──────────────┐         ┌─────────────┐
│   CLI/API   │────────▶│   Temporal   │────────▶│   Workers   │
└─────────────┘         └──────────────┘         └─────────────┘
                              │                         │
                              │                         ▼
                              │                  ┌─────────────┐
                              │                  │   Docker    │
                              │                  │  Sandbox    │
                              │                  └─────────────┘
                              │                         │
                              ▼                         ▼
                        ┌──────────────┐         ┌─────────────┐
                        │   Results    │         │ Claude Code │
                        │   Storage    │         │     CLI     │
                        └──────────────┘         └─────────────┘
```

## Quick Start

### Prerequisites

- **Go 1.21+**
- **Docker** (running) - for sandbox containers
- **Temporal CLI** - `brew install temporal`
- **API Keys**:
  ```bash
  export ANTHROPIC_API_KEY=sk-ant-...  # Required for Claude Code
  export GITHUB_TOKEN=ghp_...          # Optional, for creating PRs
  ```

### Installation

```bash
# Clone the repository
git clone https://github.com/your-org/claude-code-orchestrator.git
cd claude-code-orchestrator

# Build binaries
make build

# Run tests
make test
```

### Running Your First Workflow

**Terminal 1: Start Temporal**
```bash
make temporal-dev
```

**Terminal 2: Start Worker**
```bash
export ANTHROPIC_API_KEY=sk-ant-...
make run-worker
```

**Terminal 3: Run Smoke Test**
```bash
# Run a discovery workflow
./bin/cli run -f examples/smoke-test-discovery.yaml

# Monitor progress in Temporal UI
open http://localhost:8233

# Check result (wait ~1-2 minutes)
./bin/cli result --workflow-id transform-smoke-test-discovery

# View the collected report
./bin/cli reports transform-smoke-test-discovery -o json
```

## Example Workflows

### Example 1: Security Audit (Report Mode)

Analyze authentication implementation across repositories and collect structured findings.

**Task File: `examples/task-report.yaml`**
```yaml
version: 1
id: security-audit-2024
title: "Authentication Security Audit"
mode: report

repositories:
  - url: https://github.com/org/service-a.git
  - url: https://github.com/org/service-b.git

execution:
  agentic:
    prompt: |
      Analyze this repository's authentication implementation.
      Write findings to /workspace/REPORT.md with YAML frontmatter.

    output:
      schema:
        type: object
        required: [auth_library, score]
        properties:
          auth_library: {type: string}
          score: {type: integer, minimum: 1, maximum: 10}
          issues:
            type: array
            items:
              type: object
              properties:
                severity: {type: string, enum: [low, medium, high, critical]}
                description: {type: string}

timeout: 15m
require_approval: false
```

**Run it:**
```bash
./bin/cli run -f examples/task-report.yaml
./bin/cli reports transform-security-audit-2024 -o json > audit-results.json
```

**Output:**
```json
{
  "repository": "service-a",
  "frontmatter": {
    "auth_library": "jwt",
    "score": 7,
    "issues": [
      {"severity": "medium", "description": "Token expiration not checked"}
    ]
  },
  "body": "# Security Audit Report\n\n..."
}
```

### Example 2: Multi-Target Discovery (forEach Mode)

Analyze each API endpoint separately within a single repository.

**Task File: `examples/task-foreach.yaml`**
```yaml
version: 1
id: api-endpoint-audit
title: "API Endpoint Security Audit"
mode: report

repositories:
  - url: https://github.com/org/api-service.git

for_each:
  - name: users-api
    context: "Handles user authentication and profile management"
  - name: orders-api
    context: "Handles order creation and retrieval"
  - name: payments-api
    context: "Handles payment processing"

execution:
  agentic:
    prompt: |
      Analyze the {{.Name}} endpoint.
      Context: {{.Context}}

      Focus on authentication, input validation, and rate limiting.
      Write findings to the appropriate REPORT file.

    output:
      schema:
        type: object
        required: [endpoint_name, security_score]
        properties:
          endpoint_name: {type: string}
          security_score: {type: integer, minimum: 1, maximum: 10}
          vulnerabilities: {type: array}

timeout: 45m
require_approval: false
```

**Run it:**
```bash
./bin/cli run -f examples/task-foreach.yaml

# View all results
./bin/cli reports transform-api-endpoint-audit -o json

# Filter to specific target
./bin/cli reports transform-api-endpoint-audit --target users-api
```

**Output Structure:**
```json
{
  "repository": "api-service",
  "for_each_results": [
    {
      "target": {"name": "users-api", "context": "..."},
      "report": {
        "frontmatter": {"endpoint_name": "users-api", "security_score": 8},
        "body": "..."
      }
    },
    {
      "target": {"name": "orders-api", "context": "..."},
      "report": {...}
    }
  ]
}
```

### Example 3: Code Transformation with PR Creation

Apply a security fix across multiple repositories and create pull requests.

**Task File: `examples/task-agentic.yaml`**
```yaml
version: 1
id: fix-auth-vulnerability
title: "Fix authentication token validation"
mode: transform

repositories:
  - url: https://github.com/org/service-a.git
  - url: https://github.com/org/service-b.git

execution:
  agentic:
    prompt: |
      Fix the JWT token validation vulnerability in this repository.

      The current code doesn't validate token expiration. Update the
      authentication middleware to check token.ExpiresAt.

      Run tests to verify the fix works.

    verifiers:
      - name: test
        command: [go, test, ./...]
      - name: lint
        command: [golangci-lint, run]

pull_request:
  title: "Security: Fix JWT token expiration validation"
  body: |
    ## Summary
    Fixed authentication vulnerability where expired tokens were accepted.

    ## Changes
    - Updated auth middleware to validate token.ExpiresAt
    - Added test coverage for expired tokens
  labels: [security, automated]

timeout: 30m
require_approval: true  # Wait for human approval before creating PRs
```

**Run it:**
```bash
./bin/cli run -f examples/task-agentic.yaml

# Claude Code will make changes and run verifiers
# Then wait for approval...

# Review changes in Temporal UI, then approve
./bin/cli approve --workflow-id transform-fix-auth-vulnerability

# PRs are created automatically
./bin/cli result --workflow-id transform-fix-auth-vulnerability
```

### Example 4: Deterministic Transformation

Run a custom Docker image for reproducible transformations.

**Task File: `examples/task-deterministic.yaml`**
```yaml
version: 1
id: update-dependencies
title: "Update Go dependencies to latest patch versions"
mode: transform

repositories:
  - url: https://github.com/org/service-a.git

execution:
  deterministic:
    image: your-registry/go-dep-updater:latest
    args: [--update-patch]
    env:
      LOG_LEVEL: info

    verifiers:
      - name: build
        command: [go, build, ./...]

timeout: 20m
```

**Run it:**
```bash
./bin/cli run -f examples/task-deterministic.yaml
```

### Example 5: Transformation Repository Pattern

Share reusable skills and tools across multiple target repositories.

**Task File: `examples/task-transformation.yaml`**
```yaml
version: 1
id: migrate-logging
title: "Migrate logging to structured format"
mode: transform

# Transformation repo contains skills, tools, CLAUDE.md
transformation:
  url: https://github.com/org/migration-toolkit.git
  branch: main
  setup:
    - npm install -g @migration/cli

# Target repos to transform
targets:
  - url: https://github.com/org/service-a.git
  - url: https://github.com/org/service-b.git
  - url: https://github.com/org/service-c.git

execution:
  agentic:
    prompt: |
      Use the logging-migration skill from the transformation repository
      to migrate this service to structured logging.

      The transformation repo is at /workspace/
      The target repo is at /workspace/targets/{{.RepoName}}

timeout: 40m
parallel: true  # Create PRs in parallel
```

**Workspace Layout:**
```
/workspace/                    # Transformation repo
  /workspace/.skills/          # Auto-discovered skills
  /workspace/CLAUDE.md         # Instructions for Claude
  /workspace/targets/
    /workspace/targets/service-a/   # Target repo 1
    /workspace/targets/service-b/   # Target repo 2
    /workspace/targets/service-c/   # Target repo 3
```

## CLI Commands

### Starting Workflows

```bash
# From YAML file
./bin/cli run -f task.yaml

# From flags (transform mode)
./bin/cli run \
  --repo https://github.com/org/repo.git \
  --prompt "Fix the bug in auth.go" \
  --no-approval

# Report mode (discovery)
./bin/cli run -f examples/task-report.yaml
```

### Monitoring Workflows

```bash
# List all workflows
./bin/cli list
./bin/cli list --status Running

# Check running workflow (only works while running)
./bin/cli status --workflow-id transform-<task-id>

# Get final result (works for completed workflows)
./bin/cli result --workflow-id transform-<task-id>

# View reports (report mode only)
./bin/cli reports transform-<task-id>                    # Table format (truncated body)
./bin/cli reports transform-<task-id> -o json            # Full JSON output
./bin/cli reports transform-<task-id> -o json > report.json  # Save to file
./bin/cli reports transform-<task-id> --frontmatter-only -o json  # Just structured data
```

### Workflow Control

```bash
# Approve changes (when require_approval: true)
./bin/cli approve --workflow-id transform-<task-id>

# Reject changes
./bin/cli reject --workflow-id transform-<task-id>

# Cancel workflow
./bin/cli cancel --workflow-id transform-<task-id>
```

## Important Notes

- **Workflow IDs**: Automatically prefixed with `transform-` (e.g., `smoke-test` becomes `transform-smoke-test`)
- **Status vs Result**: Use `status` for running workflows, `result` for completed ones
- **Temporal UI**: View workflows at http://localhost:8233
- **Approval**: For report mode tasks, set `require_approval: false` to skip manual approval

## Task File Reference

### Required Fields

```yaml
version: 1                    # Schema version (always 1)
id: unique-task-id           # Unique identifier for this task
title: "Task description"    # Human-readable title
```

### Mode Selection

```yaml
mode: transform              # Creates PRs (default)
# OR
mode: report                 # Collects structured data, no PRs
```

### Repository Configuration

**Legacy pattern (simple):**
```yaml
repositories:
  - url: https://github.com/org/repo.git
    branch: main             # Optional, defaults to "main"
    name: repo               # Optional, derived from URL
    setup:                   # Optional, commands to run after clone
      - npm install
      - go mod download
```

**Transformation repository pattern:**
```yaml
transformation:
  url: https://github.com/org/toolkit.git
  branch: main
  setup: [npm install]

targets:
  - url: https://github.com/org/service-a.git
  - url: https://github.com/org/service-b.git
```

### Execution Configuration

**Agentic (Claude Code):**
```yaml
execution:
  agentic:
    prompt: "Your instructions for Claude Code"
    verifiers:                # Optional validation commands
      - name: test
        command: [go, test, ./...]
    limits:                   # Optional resource limits
      max_iterations: 50
      max_tokens: 200000
    output:                   # For report mode
      schema: {...}           # JSON Schema for validation
```

**Deterministic (Docker):**
```yaml
execution:
  deterministic:
    image: your-image:tag
    args: [--flag, value]
    env:
      KEY: value
    verifiers: [...]
```

### Discovery Features

**forEach (multi-target analysis):**
```yaml
mode: report
for_each:
  - name: target-1
    context: "Context about target 1"
  - name: target-2
    context: "Context about target 2"

execution:
  agentic:
    prompt: "Analyze {{.Name}}. Context: {{.Context}}"
```

### Pull Request Configuration

```yaml
pull_request:
  branch_prefix: fix/          # Default: "fix/claude-"
  title: "PR title"
  body: "PR description"
  labels: [automated, security]
  reviewers: [user1, user2]
```

### Other Options

```yaml
timeout: 30m                    # Workflow timeout
require_approval: true          # Wait for human approval (transform mode)
parallel: true                  # Create PRs in parallel (multi-repo)
ticket_url: https://jira/...    # Optional ticket reference
slack_channel: "#deployments"   # Optional Slack notifications
```

## Troubleshooting

### "Workflow is running but nothing happens"

Check if the worker is running and has the required API keys:
```bash
# Check worker logs for config warnings
# Worker needs ANTHROPIC_API_KEY for agentic mode
export ANTHROPIC_API_KEY=sk-ant-...
make run-worker
```

### "Status shows 'running' for completed workflow"

The `status` command only works for running workflows. Use `result` for completed ones:
```bash
./bin/cli result --workflow-id transform-<task-id>
```

### "Workflow waiting for approval indefinitely"

For report mode tasks, set `require_approval: false` to skip the approval step:
```yaml
mode: report
require_approval: false
```

Or manually approve:
```bash
./bin/cli approve --workflow-id transform-<task-id>
```

### "Activity not registered" error

Restart the worker to pick up the latest activity registrations:
```bash
# Kill old worker
pkill -f bin/worker

# Start fresh worker
make run-worker
```

### View detailed workflow execution

Use the Temporal UI for detailed debugging:
```bash
open http://localhost:8233/namespaces/default/workflows/transform-<task-id>
```

## Advanced Topics

### Custom Sandbox Configuration

For production deployments with Kubernetes:

```yaml
sandbox:
  namespace: claude-workers
  runtime_class: gvisor
  resources:
    limits:
      memory: 4Gi
      cpu: 2
    requests:
      memory: 2Gi
      cpu: 1
```

### Credentials Management

For production, use Kubernetes secrets:

```yaml
credentials:
  github:
    secret_ref:
      name: github-token
      key: token
  anthropic:
    secret_ref:
      name: anthropic-api-key
      key: api-key
```

### Parallel Execution

Enable parallel PR creation for faster multi-repo workflows:

```yaml
repositories:
  - url: https://github.com/org/repo-1.git
  - url: https://github.com/org/repo-2.git
  - url: https://github.com/org/repo-3.git

parallel: true  # Create all 3 PRs simultaneously
```

## Development

```bash
# Run tests
make test

# Run tests with coverage
make test-coverage

# Lint code
make lint

# Build binaries
make build

# Format code
make fmt

# Build sandbox Docker image
make sandbox-build
```

## Project Structure

```
.
├── cmd/
│   ├── cli/          # CLI entry point
│   └── worker/       # Worker entry point
├── internal/
│   ├── activity/     # Temporal activities
│   ├── workflow/     # Temporal workflows
│   ├── model/        # Data models
│   ├── sandbox/      # Sandbox providers (Docker/K8s)
│   ├── client/       # Temporal client utilities
│   └── config/       # Configuration loading
├── examples/         # Example task files
├── docs/            # Design documentation
│   ├── DESIGN.md
│   ├── IMPLEMENTATION_PLAN.md
│   └── OVERVIEW.md
└── CLAUDE.md        # Claude Code instructions
```

## Documentation

- **[Design Document](docs/DESIGN.md)** - Technical architecture and design decisions
- **[Implementation Plan](docs/IMPLEMENTATION_PLAN.md)** - Phased development roadmap
- **[Overview](docs/OVERVIEW.md)** - User-facing feature overview
- **[CLAUDE.md](CLAUDE.md)** - Instructions for Claude Code when working on this repo

## Contributing

See [CLAUDE.md](CLAUDE.md) for development conventions and requirements.

Before submitting changes:
1. Run `make lint` - all code must pass linter
2. Run `go test ./...` - all tests must pass
3. Run `go build ./...` - code must compile
4. Update `docs/IMPLEMENTATION_PLAN.md` when completing phases

## License

[Your license here]
