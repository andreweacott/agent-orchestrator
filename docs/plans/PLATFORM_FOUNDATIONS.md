# Platform Foundations

## Overview

This document defines the core abstractions for a code transformation platform that supports:

- **Deployment flexibility**: Local (Docker) or production (Kubernetes)
- **Task scope**: Single repo, multi-repo, or fleet-wide
- **Execution modes**: Deterministic (Docker images) or agentic (AI prompts)

The goal is to establish a minimal, solid foundation that can evolve to support advanced features (campaigns, automerge, monitoring) later.

---

## Design Principles

1. **Standards over custom** - Use existing open source solutions where available
2. **Pluggable by default** - Abstract infrastructure behind interfaces
3. **Local-first development** - Everything works on a laptop before production
4. **Incremental complexity** - Start simple, add features as needed

---

## Standards-Based Building Blocks

| Concern | Standard Solution | Why |
|---------|-------------------|-----|
| **Sandbox Lifecycle** | [Agent Sandbox](https://github.com/kubernetes-sigs/agent-sandbox) (K8s SIG) | Purpose-built for AI agent execution, gVisor/Kata isolation, warm pools |
| **Workflow Orchestration** | [Temporal](https://temporal.io/) | Durable execution, signals/queries for HITL, battle-tested |
| **Tool Abstraction** | [MCP](https://modelcontextprotocol.io/) (Model Context Protocol) | Standard protocol for LLM tool use, verifiers as MCP tools |
| **Deterministic Transforms** | [OpenRewrite](https://docs.openrewrite.org/), [Scalafix](https://scalacenter.github.io/scalafix/) | Mature AST-based refactoring tools |
| **Container Runtime** | Docker (local), containerd/gVisor (K8s) | Industry standards |
| **AI Agent** | Claude Code CLI | Handles agentic loop, context management, tool use |

### Architecture with Standards

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                    Standards-Based Architecture                              │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   CLI / API                                                                 │
│       │                                                                      │
│       ▼                                                                      │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                    Temporal (Workflow)                               │   │
│   │                                                                      │   │
│   │   - Durable task execution                                          │   │
│   │   - Retry policies                                                   │   │
│   │   - Human-in-the-loop signals                                       │   │
│   │                                                                      │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│       │                                                                      │
│       ▼                                                                      │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                    Sandbox Provider                                  │   │
│   │                                                                      │   │
│   │   Local:      Docker containers                                     │   │
│   │   Production: Agent Sandbox (K8s SIG)                               │   │
│   │               - SandboxWarmPool for fast start                      │   │
│   │               - gVisor/Kata for isolation                           │   │
│   │                                                                      │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│       │                                                                      │
│       ▼                                                                      │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                    Transformation Execution                          │   │
│   │                                                                      │   │
│   │   Deterministic:                                                    │   │
│   │   - Docker image with OpenRewrite/Scalafix/custom tool             │   │
│   │                                                                      │   │
│   │   Agentic:                                                          │   │
│   │   - Claude Code CLI                                                 │   │
│   │   - MCP-based verifiers (build, test, lint)                        │   │
│   │                                                                      │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Core Abstractions

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Core Abstractions                                    │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   ┌─────────────┐      ┌─────────────┐      ┌─────────────┐                │
│   │    Task     │─────▶│Transformation│─────▶│   Sandbox   │                │
│   │             │      │             │      │             │                │
│   │ What to do  │      │ How to do it│      │ Where to    │                │
│   │ and where   │      │             │      │ execute     │                │
│   └─────────────┘      └─────────────┘      └─────────────┘                │
│         │                    │                    │                         │
│         │                    │                    │                         │
│         ▼                    ▼                    ▼                         │
│   ┌─────────────┐      ┌─────────────┐      ┌─────────────┐                │
│   │ Repository  │      │Deterministic│      │   Docker    │                │
│   │ (1 or many) │      │     OR      │      │     OR      │                │
│   │             │      │  Agentic    │      │ Kubernetes  │                │
│   └─────────────┘      └─────────────┘      └─────────────┘                │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 1. Task

A **Task** represents a unit of work - what needs to be done and where.

```go
// A Task defines what to do and where
type Task struct {
    ID          string
    Title       string
    Description string

    // Target repositories (1 or many)
    Repositories []RepositoryRef

    // How to make the change
    Transformation Transformation

    // Execution settings
    Timeout time.Duration

    // Metadata
    Requester string
    Labels    map[string]string
}

// RepositoryRef identifies a target repository
type RepositoryRef struct {
    URL    string  // Git URL (can be used directly for ad-hoc)
    Name   string  // OR reference to pre-registered Repository
    Branch string  // Target branch (default: main)
}
```

### 2. Transformation

A **Transformation** defines how to make the change - either deterministic or agentic.

```go
// Transformation defines how to make changes
type Transformation struct {
    // Exactly one of these must be set
    Deterministic *DeterministicTransform
    Agentic       *AgenticTransform
}

// DeterministicTransform uses a Docker image to apply changes
type DeterministicTransform struct {
    // Docker image containing the transformation tool
    Image string

    // Command to run (optional, uses image default if empty)
    Command []string

    // Arguments passed to the command
    Args []string

    // Environment variables
    Env map[string]string
}

// AgenticTransform uses an AI agent to make changes
type AgenticTransform struct {
    // Natural language prompt describing what to do
    Prompt string

    // Optional: structured instructions for the agent
    Instructions string

    // Optional: verifiers to run for feedback (build, test, lint)
    Verifiers []Verifier

    // Agent settings
    MaxTurns int  // Max agentic iterations (default: 10)
}

// Verifier provides feedback to the agent during execution.
// Verifiers are exposed as MCP tools to the agent.
type Verifier struct {
    Name        string   // e.g., "build", "test", "lint"
    Command     []string // Command to run
    Description string   // Description for MCP tool schema
    // Output is fed back to the agent on failure
}

// Verifiers are registered as MCP tools, allowing the agent to invoke them
// directly during execution. This follows the Model Context Protocol standard.
// Example MCP tool schema generated from verifier:
//
//   {
//     "name": "build",
//     "description": "Run the build to check for compilation errors",
//     "inputSchema": { "type": "object", "properties": {} }
//   }
```

### 3. Sandbox

A **Sandbox** is the isolated execution environment - abstracted via the Provider interface.

**Local development**: Docker containers
**Production**: [Agent Sandbox](https://github.com/kubernetes-sigs/agent-sandbox) (Kubernetes SIG project)

```go
// Provider abstracts sandbox lifecycle
// - Docker provider for local development
// - Agent Sandbox provider for Kubernetes (uses SandboxClaim CRD)
type Provider interface {
    Provision(ctx context.Context, opts ProvisionOptions) (*Sandbox, error)
    Exec(ctx context.Context, id string, cmd ExecCommand) (*ExecResult, error)
    Cleanup(ctx context.Context, id string) error
}

// Sandbox represents a running execution environment
type Sandbox struct {
    ID         string
    Provider   string       // "docker" | "agent-sandbox"
    WorkingDir string       // e.g., "/workspace"
    Status     SandboxPhase
}
```

**Agent Sandbox benefits (production):**
- `SandboxWarmPool`: Pre-warmed pods for <1s cold start
- `gVisor`/`Kata Containers`: Kernel-level isolation for untrusted code
- `SandboxTemplate`: Reusable sandbox configurations
- Python SDK and Go client for programmatic access

### 4. Repository (Optional)

A **Repository** provides pre-registered configuration for known repos. This is optional - tasks can target repos directly by URL for ad-hoc work.

```go
// Repository defines configuration for a known repository
type Repository struct {
    Name   string
    URL    string
    Branch string

    // Default sandbox settings for this repo
    SandboxProfile string

    // Setup commands to run after clone
    SetupCommands []string

    // Validation commands to run after transformation
    ValidationCommands []string
}
```

---

## Execution Flow

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Execution Flow                                     │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   1. Task Received                                                          │
│      │                                                                       │
│      ▼                                                                       │
│   2. For each repository in task.Repositories:                              │
│      │                                                                       │
│      ├──▶ 3. Provision Sandbox                                              │
│      │       │                                                               │
│      │       ▼                                                               │
│      │    4. Clone Repository                                               │
│      │       │                                                               │
│      │       ▼                                                               │
│      │    5. Run Setup (if defined)                                         │
│      │       │                                                               │
│      │       ▼                                                               │
│      │    6. Execute Transformation                                         │
│      │       │                                                               │
│      │       ├── Deterministic: Run Docker image                            │
│      │       │                                                               │
│      │       └── Agentic: Run AI agent with verifier loop                   │
│      │           │                                                           │
│      │           ├──▶ Agent makes changes                                   │
│      │           │       │                                                   │
│      │           │       ▼                                                   │
│      │           │    Run verifiers (build, test)                           │
│      │           │       │                                                   │
│      │           │       ├── Pass: Continue                                 │
│      │           │       └── Fail: Feed error back to agent, retry          │
│      │           │                                                           │
│      │           └── Until: success OR max turns                            │
│      │       │                                                               │
│      │       ▼                                                               │
│      │    7. Run Validation (if defined)                                    │
│      │       │                                                               │
│      │       ▼                                                               │
│      │    8. Create PR (or stage changes)                                   │
│      │       │                                                               │
│      │       ▼                                                               │
│      └──▶ 9. Cleanup Sandbox                                                │
│                                                                              │
│   10. Report Results                                                         │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Dual Execution Mode

The key architectural insight: **same orchestration, different execution**.

### Deterministic Execution

For well-understood, repeatable transformations:

```yaml
task:
  title: "Upgrade log4j to 2.21.1"
  repositories:
    - url: "https://github.com/org/service-a.git"
    - url: "https://github.com/org/service-b.git"
  transformation:
    deterministic:
      image: "internal/log4j-upgrader:latest"
      args: ["--target-version", "2.21.1"]
```

**Use cases:**
- Dependency version bumps
- AST-based refactoring (OpenRewrite, Scalafix)
- Configuration migrations
- Code formatting / linting fixes

### Agentic Execution

For complex, context-dependent changes:

```yaml
task:
  title: "Add rate limiting to API endpoints"
  repositories:
    - url: "https://github.com/org/api-gateway.git"
  transformation:
    agentic:
      prompt: |
        Add rate limiting to all public API endpoints in this service.

        Requirements:
        - Use the existing RateLimiter class from pkg/middleware
        - Default limit: 100 requests per minute per client
        - Add configuration via environment variables
        - Update tests to cover rate limiting behavior

      verifiers:
        - name: build
          command: ["go", "build", "./..."]
        - name: test
          command: ["go", "test", "./..."]
        - name: lint
          command: ["golangci-lint", "run"]

      maxTurns: 15
```

**Use cases:**
- Bug fixes requiring investigation
- Feature implementation
- Complex refactoring with judgment calls
- Cross-cutting changes requiring understanding

---

## MCP-Based Verifiers

Verifiers are exposed to the agent as [MCP (Model Context Protocol)](https://modelcontextprotocol.io/) tools. This allows the agent to invoke build, test, and lint tools directly during execution.

```yaml
# Verifiers defined in task become MCP tools
transformation:
  agentic:
    prompt: "Add input validation..."
    verifiers:
      - name: build
        command: ["go", "build", "./..."]
        description: "Compile the Go code to check for errors"

      - name: test
        command: ["go", "test", "./..."]
        description: "Run unit tests"

      - name: lint
        command: ["golangci-lint", "run"]
        description: "Run linter to check code quality"
```

The orchestrator starts an MCP server in the sandbox that exposes these as tools:

```json
{
  "tools": [
    {
      "name": "build",
      "description": "Compile the Go code to check for errors",
      "inputSchema": { "type": "object", "properties": {} }
    },
    {
      "name": "test",
      "description": "Run unit tests",
      "inputSchema": { "type": "object", "properties": {} }
    },
    {
      "name": "lint",
      "description": "Run linter to check code quality",
      "inputSchema": { "type": "object", "properties": {} }
    }
  ]
}
```

The agent can then invoke these tools during its execution, getting immediate feedback without waiting for external CI.

---

## Verifier Loop (Agentic Mode)

Verifiers provide fast feedback during agentic execution via MCP:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                    Verifier Loop (MCP-Based)                                 │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                    Sandbox Environment                               │   │
│   │                                                                      │   │
│   │   ┌─────────────────┐         ┌─────────────────────────────────┐   │   │
│   │   │   Claude Code   │◀──MCP──▶│      MCP Verifier Server        │   │   │
│   │   │     (Agent)     │         │                                  │   │   │
│   │   │                 │         │  Tools:                          │   │   │
│   │   │  - Read/Write   │         │  - build()  → go build ./...    │   │   │
│   │   │  - Edit         │         │  - test()   → go test ./...     │   │   │
│   │   │  - Bash         │         │  - lint()   → golangci-lint     │   │   │
│   │   │  - [MCP tools]  │         │                                  │   │   │
│   │   └─────────────────┘         └─────────────────────────────────┘   │   │
│   │                                                                      │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
│   Flow:                                                                     │
│                                                                              │
│   1. Agent receives prompt + MCP tools (verifiers)                          │
│        │                                                                     │
│        ▼                                                                     │
│   2. Agent analyzes code and makes changes                                  │
│        │                                                                     │
│        ▼                                                                     │
│   3. Agent invokes verifier via MCP: tool_use("build")                      │
│        │                                                                     │
│        ├── build ──▶ ✓ Pass                                                 │
│        ├── test  ──▶ ✗ Fail: "TestRateLimit failed: expected 429"          │
│        └── lint  ──▶ ✓ Pass                                                 │
│        │                                                                     │
│        ▼                                                                     │
│   4. Agent sees failure in tool result, fixes code                          │
│        │                                                                     │
│        ▼                                                                     │
│   5. Agent invokes verifiers again via MCP                                  │
│        │                                                                     │
│        └── All pass ──▶ Agent completes task                                │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Data Model

### Task (Runtime)

```go
package model

type Task struct {
    ID          string            `json:"id"`
    Title       string            `json:"title"`
    Description string            `json:"description"`

    Repositories   []RepositoryRef   `json:"repositories"`
    Transformation Transformation    `json:"transformation"`

    Timeout   time.Duration     `json:"timeout"`
    Requester string            `json:"requester,omitempty"`
    Labels    map[string]string `json:"labels,omitempty"`
}

type RepositoryRef struct {
    URL    string `json:"url,omitempty"`    // Direct URL (ad-hoc)
    Name   string `json:"name,omitempty"`   // Reference to Repository config
    Branch string `json:"branch,omitempty"` // Default: "main"
}

type Transformation struct {
    Deterministic *DeterministicTransform `json:"deterministic,omitempty"`
    Agentic       *AgenticTransform       `json:"agentic,omitempty"`
}

type DeterministicTransform struct {
    Image   string            `json:"image"`
    Command []string          `json:"command,omitempty"`
    Args    []string          `json:"args,omitempty"`
    Env     map[string]string `json:"env,omitempty"`
}

type AgenticTransform struct {
    Prompt       string     `json:"prompt"`
    Instructions string     `json:"instructions,omitempty"`
    Verifiers    []Verifier `json:"verifiers,omitempty"`
    MaxTurns     int        `json:"maxTurns,omitempty"` // Default: 10
}

type Verifier struct {
    Name    string   `json:"name"`
    Command []string `json:"command"`
}
```

### Task Result

```go
type TaskResult struct {
    TaskID  string     `json:"taskId"`
    Status  TaskStatus `json:"status"`

    // Results per repository
    Repositories []RepositoryResult `json:"repositories"`

    // Timing
    StartedAt   time.Time  `json:"startedAt"`
    CompletedAt *time.Time `json:"completedAt,omitempty"`
    Duration    *float64   `json:"durationSeconds,omitempty"`

    // Error (if failed)
    Error *string `json:"error,omitempty"`
}

type RepositoryResult struct {
    Repository string `json:"repository"`
    Status     string `json:"status"` // "success" | "failed" | "skipped"

    // Changes made
    FilesModified []string `json:"filesModified,omitempty"`

    // PR created (if applicable)
    PullRequest *PullRequestInfo `json:"pullRequest,omitempty"`

    // Transformation output
    Output string  `json:"output,omitempty"`
    Error  *string `json:"error,omitempty"`
}

type PullRequestInfo struct {
    URL    string `json:"url"`
    Number int    `json:"number"`
    Branch string `json:"branch"`
}
```

### Repository (Configuration)

```go
type Repository struct {
    Name   string `json:"name"`
    URL    string `json:"url"`
    Branch string `json:"branch,omitempty"` // Default: "main"

    // Sandbox configuration
    SandboxProfile string `json:"sandboxProfile,omitempty"`

    // Lifecycle hooks
    SetupCommands      []string `json:"setupCommands,omitempty"`
    ValidationCommands []string `json:"validationCommands,omitempty"`

    // PR settings
    PRBranchPrefix string   `json:"prBranchPrefix,omitempty"` // Default: "auto/"
    PRLabels       []string `json:"prLabels,omitempty"`
}
```

### Sandbox Profile (Configuration)

```go
type SandboxProfile struct {
    Name  string `json:"name"`
    Image string `json:"image"`

    Resources ResourceLimits `json:"resources"`
    Timeout   time.Duration  `json:"timeout"`

    // Pre-installed tools / setup
    SetupScript string `json:"setupScript,omitempty"`
}

type ResourceLimits struct {
    MemoryMB int64   `json:"memoryMB"`
    CPUCores float64 `json:"cpuCores"`
}
```

---

## Temporal Workflow

The workflow orchestrates task execution:

```go
func ExecuteTask(ctx workflow.Context, task model.Task) (*model.TaskResult, error) {
    result := &model.TaskResult{
        TaskID:    task.ID,
        Status:    model.TaskStatusRunning,
        StartedAt: workflow.Now(ctx),
    }

    // Process each repository
    for _, repoRef := range task.Repositories {
        repoResult := processRepository(ctx, task, repoRef)
        result.Repositories = append(result.Repositories, repoResult)
    }

    // Determine overall status
    result.Status = determineOverallStatus(result.Repositories)
    now := workflow.Now(ctx)
    result.CompletedAt = &now

    return result, nil
}

func processRepository(ctx workflow.Context, task model.Task, repoRef model.RepositoryRef) model.RepositoryResult {
    // 1. Provision sandbox
    sandbox, err := workflow.ExecuteActivity(ctx, "ProvisionSandbox", task.ID).Get(ctx, nil)
    if err != nil {
        return failedResult(repoRef, err)
    }
    defer cleanupSandbox(ctx, sandbox)

    // 2. Clone repository
    err = workflow.ExecuteActivity(ctx, "CloneRepository", sandbox, repoRef).Get(ctx, nil)
    if err != nil {
        return failedResult(repoRef, err)
    }

    // 3. Run setup (if configured)
    // ...

    // 4. Execute transformation
    var output string
    if task.Transformation.Deterministic != nil {
        output, err = executeDeterministic(ctx, sandbox, task.Transformation.Deterministic)
    } else {
        output, err = executeAgentic(ctx, sandbox, task.Transformation.Agentic)
    }
    if err != nil {
        return failedResult(repoRef, err)
    }

    // 5. Create PR
    pr, err := workflow.ExecuteActivity(ctx, "CreatePullRequest", sandbox, repoRef, task).Get(ctx, nil)
    if err != nil {
        return failedResult(repoRef, err)
    }

    return model.RepositoryResult{
        Repository:  repoRef.URL,
        Status:      "success",
        Output:      output,
        PullRequest: pr,
    }
}
```

### Deterministic Execution Activity

```go
func (a *Activities) ExecuteDeterministic(ctx context.Context, sandbox *Sandbox, transform *DeterministicTransform) (string, error) {
    // Pull the transformation image
    if err := a.sandbox.PullImage(ctx, transform.Image); err != nil {
        return "", err
    }

    // Run the transformation container
    // Mount the workspace from the sandbox
    result, err := a.sandbox.Exec(ctx, sandbox.ID, ExecCommand{
        Image:   transform.Image,
        Command: transform.Command,
        Args:    transform.Args,
        Env:     transform.Env,
    })
    if err != nil {
        return "", err
    }

    if result.ExitCode != 0 {
        return result.Stdout, fmt.Errorf("transformation failed: %s", result.Stderr)
    }

    return result.Stdout, nil
}
```

### Agentic Execution Activity

```go
func (a *Activities) ExecuteAgentic(ctx context.Context, sandbox *Sandbox, transform *AgenticTransform) (string, error) {
    maxTurns := transform.MaxTurns
    if maxTurns == 0 {
        maxTurns = 10
    }

    // Build the prompt with verifier instructions
    prompt := buildAgentPrompt(transform)

    for turn := 0; turn < maxTurns; turn++ {
        activity.RecordHeartbeat(ctx, fmt.Sprintf("Turn %d/%d", turn+1, maxTurns))

        // Run Claude Code
        result, err := a.runClaudeCode(ctx, sandbox, prompt)
        if err != nil {
            return "", err
        }

        // Run verifiers
        allPassed := true
        var feedback strings.Builder

        for _, verifier := range transform.Verifiers {
            vResult, err := a.sandbox.Exec(ctx, sandbox.ID, ExecCommand{
                Command: verifier.Command,
            })
            if err != nil || vResult.ExitCode != 0 {
                allPassed = false
                feedback.WriteString(fmt.Sprintf("\n[%s] FAILED:\n%s\n", verifier.Name, vResult.Stderr))
            }
        }

        if allPassed {
            return result.Output, nil
        }

        // Feed failure back to agent for next turn
        prompt = fmt.Sprintf("The following verifiers failed. Please fix the issues:\n%s", feedback.String())
    }

    return "", fmt.Errorf("max turns (%d) exceeded without passing verifiers", maxTurns)
}
```

---

## CLI Interface

```bash
# Ad-hoc task with deterministic transformation
orchestrator run \
  --repo https://github.com/org/service.git \
  --image internal/log4j-upgrader:latest \
  --args "--target-version=2.21.1"

# Ad-hoc task with agentic transformation
orchestrator run \
  --repo https://github.com/org/service.git \
  --prompt "Add input validation to all API endpoints" \
  --verifier "build:go build ./..." \
  --verifier "test:go test ./..."

# Multi-repo task
orchestrator run \
  --repo https://github.com/org/service-a.git \
  --repo https://github.com/org/service-b.git \
  --prompt "Update deprecated API calls to use v2 endpoints"

# Task from file
orchestrator run --file task.yaml

# Check status
orchestrator status <task-id>

# List tasks
orchestrator list
```

### Task File Format

```yaml
# task.yaml
id: upgrade-logging-library
title: "Upgrade logging library to structured logging"
repositories:
  - url: https://github.com/org/service-a.git
  - url: https://github.com/org/service-b.git
  - url: https://github.com/org/service-c.git

transformation:
  agentic:
    prompt: |
      Migrate this service from the legacy logging library to use
      structured logging with the new `slog` package.

      Requirements:
      - Replace all log.Printf calls with slog equivalents
      - Add context fields where appropriate
      - Ensure log levels are correct (info, warn, error)
      - Update any log configuration

    verifiers:
      - name: build
        command: ["go", "build", "./..."]
      - name: test
        command: ["go", "test", "./..."]

    maxTurns: 10

timeout: 30m
```

---

## Configuration

### Local Development (Docker)

```yaml
# config.yaml
sandbox:
  provider: docker
  image: claude-sandbox:latest
  resources:
    memoryMB: 4096
    cpuCores: 2

temporal:
  address: localhost:7233
  namespace: default
  taskQueue: orchestrator-tasks

github:
  tokenEnvVar: GITHUB_TOKEN

claude:
  apiKeyEnvVar: ANTHROPIC_API_KEY
```

### Production (Kubernetes)

```yaml
# config.yaml
sandbox:
  provider: kubernetes
  namespace: orchestrator-sandboxes
  image: 123456789.dkr.ecr.us-west-2.amazonaws.com/claude-sandbox:latest
  resources:
    memoryMB: 4096
    cpuCores: 2
  nodeSelector:
    workload-type: sandbox

temporal:
  address: temporal.internal:7233
  namespace: orchestrator
  taskQueue: orchestrator-tasks

github:
  tokenSecretRef:
    name: github-credentials
    key: token

claude:
  apiKeySecretRef:
    name: claude-credentials
    key: api-key
```

---

## Directory Structure

```
orchestrator/
├── cmd/
│   ├── worker/           # Temporal worker
│   │   └── main.go
│   └── cli/              # CLI tool
│       └── main.go
├── internal/
│   ├── model/            # Data models
│   │   ├── task.go
│   │   ├── result.go
│   │   └── config.go
│   ├── workflow/         # Temporal workflows
│   │   ├── execute.go
│   │   └── execute_test.go
│   ├── activity/         # Temporal activities
│   │   ├── sandbox.go
│   │   ├── transform.go
│   │   ├── git.go
│   │   └── github.go
│   ├── sandbox/          # Sandbox abstraction
│   │   ├── provider.go   # Interface
│   │   ├── docker/       # Docker implementation
│   │   └── kubernetes/   # K8s implementation
│   └── agent/            # Agentic execution
│       ├── claude.go     # Claude Code integration
│       └── verifier.go   # Verifier loop
├── config/
│   ├── local.yaml
│   └── production.yaml
└── docker/
    └── Dockerfile.sandbox
```

---

## Evolution Path

This foundation supports future enhancements while maintaining standards-based approach:

| Foundation | Future Enhancement | Standard Solution |
|------------|-------------------|-------------------|
| Task with multiple repos | Campaign CRD for fleet targeting | K8s CRDs + operators |
| Manual PR creation | Automerge with safety checks | GitHub Actions / merge queues |
| MCP verifiers in task | Shared Verifier library | MCP tool registry |
| Ad-hoc repo refs | Repository registry with policies | K8s CRDs |
| Simple status | Monitoring and alerting | OpenTelemetry + Prometheus |
| CLI-driven | Slack bot, web UI | Backstage plugin |
| Single execution | Scheduled/recurring tasks | K8s CronJobs / Temporal schedules |
| Docker sandboxes | Production isolation | Agent Sandbox + gVisor |

The key is that the **core abstractions remain stable** while features layer on top using **standards-based solutions**.

---

## What We Build vs. What We Adopt

| Build (Custom) | Adopt (Standard) |
|----------------|------------------|
| Task/Transformation model | Temporal (workflow) |
| Workflow orchestration logic | Agent Sandbox (K8s sandbox lifecycle) |
| CLI interface | MCP (verifier protocol) |
| GitHub PR integration | Claude Code (agentic execution) |
| Configuration loading | OpenRewrite/Scalafix (deterministic transforms) |
| | Docker/containerd (container runtime) |
| | gVisor/Kata (isolation) |

**Principle**: Build the orchestration glue, adopt standards for infrastructure.
