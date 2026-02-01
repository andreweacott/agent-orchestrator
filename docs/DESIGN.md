# Code Transformation Platform - Design

## Overview

A platform for automated code transformations and discovery across repositories, supporting:

- **Deployment**: Local (Docker) or production (Kubernetes)
- **Scope**: Single repo or multi-repo
- **Execution**: Deterministic (Docker images) or agentic (AI prompts)
- **Mode**: Transform (create PRs) or Report (collect structured data)

### Vision: Managed Turbolift

Think of this as **managed [Turbolift](https://github.com/Skyscanner/turbolift)** with two execution backends:

| Turbolift | This Platform |
|-----------|---------------|
| CLI on your laptop | Managed service (Temporal + K8s) |
| Script/command | Docker image OR agent prompt |
| Dies if laptop closes | Durable execution, survives failures |
| No approval flow | Human-in-the-loop before PR |
| Stateless | Status tracking, audit trail |
| Single user | Multi-tenant capable |

The platform adds the "managed" layer that Turbolift deliberately doesn't have, while supporting both deterministic transforms (like Turbolift scripts) and agentic transforms (AI-driven code changes).

## Design Principles

1. **Standards over custom** - Prefer existing open source solutions where available
2. **Pluggable by default** - Abstract infrastructure behind interfaces
3. **Local-first development** - Everything works on a laptop
4. **Incremental complexity** - Start simple, add features as needed

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Architecture                                    │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   CLI / API                                                                 │
│       │                                                                      │
│       ▼                                                                      │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                         Temporal                                     │   │
│   │                                                                      │   │
│   │   - Durable task execution                                          │   │
│   │   - Retry policies                                                   │   │
│   │   - Human-in-the-loop signals                                       │   │
│   │                                                                      │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│       │                                                                      │
│       ▼                                                                      │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                      Sandbox Provider                                │   │
│   │                                                                      │   │
│   │   Local:      Docker containers                                     │   │
│   │   Production: Raw Kubernetes pods / jobs                            │   │
│   │                                                                      │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│       │                                                                      │
│       ▼                                                                      │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                   Transformation Execution                           │   │
│   │                                                                      │   │
│   │   Deterministic: Docker image (OpenRewrite, Scalafix, custom)       │   │
│   │   Agentic:       Claude Code CLI + verifiers via Bash               │   │
│   │                                                                      │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Standards Used

| Concern | Solution | Why |
|---------|----------|-----|
| Workflow Orchestration | [Temporal](https://temporal.io/) | Durable execution, signals/queries, battle-tested |
| Sandbox Lifecycle (K8s) | Kubernetes Jobs | Purpose-built for AI agents, warm pools, gVisor |
| Sandbox Lifecycle (Local) | Docker | Simple, universal |
| Deterministic Transforms | [OpenRewrite](https://docs.openrewrite.org/), [Scalafix](https://scalacenter.github.io/scalafix/) | Mature AST-based tools |
| AI Agent | Claude Code CLI | Agentic loop, context management, tool use |

---

## Core Data Model

### CodeTransform CRD (Kubernetes-Native Interface)

The primary interface for defining transformations is a Kubernetes Custom Resource:

```yaml
apiVersion: codetransform.io/v1alpha1
kind: CodeTransform
metadata:
  name: upgrade-to-slog
  namespace: transforms
spec:
  # Execution mode: "transform" (default) or "report"
  # - transform: Make code changes and create PRs
  # - report: Analyze code and collect structured output (no PRs)
  mode: transform

  # Target repositories
  repositories:
    - url: https://github.com/org/service-a.git
      branch: main
    - url: https://github.com/org/service-b.git

  # For iterating over multiple targets within a repo (e.g., API endpoints)
  # forEach:
  #   - name: users-api
  #     context: "Analyze src/api/users/"
  #   - name: orders-api
  #     context: "Analyze src/api/orders/"

  # Transformation definition (one of):
  transform:
    # Option 1: Deterministic - Docker image execution
    image:
      ref: ghcr.io/moderneinc/mod:latest
      args: ["mod", "run", "--recipe", "UpgradeLog4j"]
      env:
        LOG_LEVEL: info

    # Option 2: Agentic - AI-driven transformation
    # agent:
    #   prompt: |
    #     Migrate from log.Printf to slog package.
    #     Use structured logging with context fields.
    #   verifiers:
    #     - name: build
    #       command: ["go", "build", "./..."]
    #     - name: test
    #       command: ["go", "test", "./..."]
    #   limits:
    #     maxIterations: 10
    #     maxTokens: 100000
    #     maxVerifierRetries: 3
    #
    #   # For report mode: validate/parse agent output as structured data
    #   outputSchema:
    #     type: object
    #     properties:
    #       finding: { type: string }
    #       severity: { type: string, enum: ["low", "medium", "high"] }
    #       details: { type: object }

  # Credentials (reference K8s secrets)
  credentials:
    github:
      secretRef:
        name: github-token
        key: token
    anthropic:
      secretRef:
        name: claude-credentials
        key: api-key

  # Execution settings
  resources:
    limits:
      memory: "4Gi"
      cpu: "2"
  timeout: 30m
  requireApproval: true

  # Sandbox settings
  sandbox:
    namespace: sandbox-isolated      # Where pods run
    runtimeClassName: gvisor         # Optional: enhanced isolation
    nodeSelector:
      workload-type: sandbox

  # PR settings
  pullRequest:
    branchPrefix: "auto/slog-migration"
    title: "Migrate to structured logging (slog)"
    labels: ["automated", "logging"]

status:
  phase: Running  # Pending | Running | AwaitingApproval | Completed | Failed
  workflowID: "transform-xyz123"
  repositories:
    - name: service-a
      status: Completed
      pullRequest:        # For transform mode
        url: https://github.com/org/service-a/pull/123
        number: 123
      report: null        # For report mode: structured output from agent
    - name: service-b
      status: Running
  # Aggregated reports (for report mode with forEach)
  reports: []
  startedAt: "2026-01-31T10:00:00Z"
  completedAt: null
```

### Internal Go Types

The CRD maps to internal Go types used by the Temporal workflow:

```go
type Task struct {
    ID          string             `json:"id"`
    Title       string             `json:"title"`
    Description string             `json:"description,omitempty"`
    Repositories   []RepositoryTarget `json:"repositories"`
    Transformation Transformation     `json:"transformation"`
    Timeout        time.Duration      `json:"timeout,omitempty"`
}

type RepositoryTarget struct {
    URL    string   `json:"url"`              // Git URL
    Branch string   `json:"branch,omitempty"` // Default: "main"
    Setup  []string `json:"setup,omitempty"`  // Commands after clone
}
```

### Transformation

Either deterministic or agentic.

```go
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
    Prompt    string     `json:"prompt"`
    Verifiers []Verifier `json:"verifiers,omitempty"`
}

type Verifier struct {
    Name    string   `json:"name"`    // e.g., "build", "test"
    Command []string `json:"command"` // e.g., ["go", "test", "./..."]
}
```

### Task Result

```go
type TaskResult struct {
    TaskID       string             `json:"taskId"`
    Status       TaskStatus         `json:"status"`
    Repositories []RepositoryResult `json:"repositories"`
    StartedAt    time.Time          `json:"startedAt"`
    CompletedAt  *time.Time         `json:"completedAt,omitempty"`
    Error        *string            `json:"error,omitempty"`
}

type RepositoryResult struct {
    Repository    string           `json:"repository"`
    Status        string           `json:"status"` // "success" | "failed" | "skipped"
    FilesModified []string         `json:"filesModified,omitempty"`
    PullRequest   *PullRequestInfo `json:"pullRequest,omitempty"`
    Output        string           `json:"output,omitempty"`
    Error         *string          `json:"error,omitempty"`
}

type PullRequestInfo struct {
    URL    string `json:"url"`
    Number int    `json:"number"`
    Branch string `json:"branch"`
}
```

---

## Sandbox Provider Interface

Abstracts container runtime for local and production.

```go
type Provider interface {
    Provision(ctx context.Context, opts ProvisionOptions) (*Sandbox, error)
    Exec(ctx context.Context, id string, cmd ExecCommand) (*ExecResult, error)
    CopyTo(ctx context.Context, id string, src io.Reader, destPath string) error
    CopyFrom(ctx context.Context, id string, srcPath string) (io.ReadCloser, error)
    Status(ctx context.Context, id string) (*SandboxStatus, error)
    Cleanup(ctx context.Context, id string) error
    Name() string
}

type ProvisionOptions struct {
    TaskID     string
    Image      string
    WorkingDir string
    Env        map[string]string
    Resources  ResourceLimits
    Timeout    time.Duration
    Labels     map[string]string

    // Kubernetes-specific (ignored by Docker)
    Namespace      string
    ServiceAccount string
    NodeSelector   map[string]string
    RuntimeClass   string // gvisor, kata
}

type Sandbox struct {
    ID         string
    Provider   string // "docker" | "kubernetes" | "agent-sandbox"
    WorkingDir string
    Status     SandboxPhase
}

type ExecCommand struct {
    Command    []string
    WorkingDir string
    Env        map[string]string
    Stdin      io.Reader
    Timeout    time.Duration
}

type ExecResult struct {
    ExitCode int
    Stdout   string
    Stderr   string
}
```

### Provider Selection

```go
func NewProvider() (Provider, error) {
    switch os.Getenv("SANDBOX_PROVIDER") {
    case "kubernetes":
        return kubernetes.NewProvider()
    case "agent-sandbox":
        return agentsandbox.NewProvider()
    default:
        return docker.NewProvider()
    }
}
```

---

## Execution Flow

```
1. Task Received
   │
   ▼
2. For each repository:
   │
   ├──▶ 3. Provision Sandbox
   │       │
   │       ▼
   │    4. Clone Repository
   │       │
   │       ▼
   │    5. Run Setup (if defined)
   │       │
   │       ▼
   │    6. Execute Transformation
   │       │
   │       ├── Deterministic: Run Docker image
   │       │
   │       └── Agentic: Run Claude Code
   │           │
   │           ├──▶ Agent makes changes
   │           │       │
   │           │       ▼
   │           │    Agent runs verifiers via Bash
   │           │       │
   │           │       ├── Pass: Continue
   │           │       └── Fail: Agent fixes and retries
   │           │
   │           └── Until: all verifiers pass
   │       │
   │       ▼
   │    7. Create PR
   │       │
   │       ▼
   └──▶ 8. Cleanup Sandbox

9. Report Results
```

---

## Verifiers

Verifiers are commands the agent runs via Bash to validate changes. The orchestrator appends them to the prompt:

```
After making changes, verify your work by running these commands:
- build: go build ./...
- test: go test ./...
- lint: golangci-lint run

Fix any errors before completing the task.
```

The agent runs these directly using its Bash tool, sees output, and iterates until all pass.

The orchestrator also runs verifiers as a **final gate** before creating the PR.

---

## Temporal Workflow

```go
func ExecuteTask(ctx workflow.Context, task model.Task) (*model.TaskResult, error) {
    result := &model.TaskResult{
        TaskID:    task.ID,
        Status:    model.TaskStatusRunning,
        StartedAt: workflow.Now(ctx),
    }

    for _, repo := range task.Repositories {
        repoResult := processRepository(ctx, task, repo)
        result.Repositories = append(result.Repositories, repoResult)
    }

    result.Status = determineOverallStatus(result.Repositories)
    now := workflow.Now(ctx)
    result.CompletedAt = &now

    return result, nil
}

func processRepository(ctx workflow.Context, task model.Task, repo model.RepositoryTarget) model.RepositoryResult {
    // 1. Provision sandbox
    sandbox, err := workflow.ExecuteActivity(ctx, "ProvisionSandbox", task.ID).Get(ctx, nil)
    if err != nil {
        return failedResult(repo, err)
    }
    defer cleanupSandbox(ctx, sandbox)

    // 2. Clone repository
    err = workflow.ExecuteActivity(ctx, "CloneRepository", sandbox, repo).Get(ctx, nil)
    if err != nil {
        return failedResult(repo, err)
    }

    // 3. Run setup
    if len(repo.Setup) > 0 {
        err = workflow.ExecuteActivity(ctx, "RunSetup", sandbox, repo.Setup).Get(ctx, nil)
        if err != nil {
            return failedResult(repo, err)
        }
    }

    // 4. Execute transformation
    var output string
    if task.Transformation.Deterministic != nil {
        output, err = executeDeterministic(ctx, sandbox, task.Transformation.Deterministic)
    } else {
        output, err = executeAgentic(ctx, sandbox, task.Transformation.Agentic)
    }
    if err != nil {
        return failedResult(repo, err)
    }

    // 5. Create PR
    pr, err := workflow.ExecuteActivity(ctx, "CreatePullRequest", sandbox, repo, task).Get(ctx, nil)
    if err != nil {
        return failedResult(repo, err)
    }

    return model.RepositoryResult{
        Repository:  repo.URL,
        Status:      "success",
        Output:      output,
        PullRequest: pr,
    }
}
```

---

## CLI Interface

```bash
# Deterministic transformation
orchestrator run \
  --repo https://github.com/org/service.git \
  --image internal/log4j-upgrader:latest \
  --args "--target-version=2.21.1"

# Agentic transformation
orchestrator run \
  --repo https://github.com/org/service.git \
  --prompt "Add input validation to all API endpoints" \
  --verifier "build:go build ./..." \
  --verifier "test:go test ./..."

# Multi-repo
orchestrator run \
  --repo https://github.com/org/service-a.git \
  --repo https://github.com/org/service-b.git \
  --prompt "Update deprecated API calls"

# From file
orchestrator run --file task.yaml

# Status
orchestrator status <task-id>
orchestrator list
```

### Task File Format

```yaml
id: upgrade-logging
title: "Upgrade to structured logging"

repositories:
  - url: https://github.com/org/service-a.git
    setup: ["go mod download"]
  - url: https://github.com/org/service-b.git
    setup: ["go mod download"]

transformation:
  agentic:
    prompt: |
      Migrate from log.Printf to slog package.
      - Replace all log.Printf calls with slog equivalents
      - Add context fields where appropriate
      - Ensure log levels are correct

    verifiers:
      - name: build
        command: ["go", "build", "./..."]
      - name: test
        command: ["go", "test", "./..."]

timeout: 30m
```

---

## Configuration

### Local (Docker)

```yaml
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
sandbox:
  provider: agent-sandbox  # or "kubernetes"
  namespace: orchestrator-sandboxes
  image: 123456789.dkr.ecr.us-west-2.amazonaws.com/claude-sandbox:latest
  warmPool: claude-warm-pool
  runtimeClass: gvisor
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

## Kubernetes Production Architecture

### Recommended: Plain Kubernetes Jobs

For most use cases, plain Kubernetes Jobs provide sufficient functionality without additional complexity:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           EKS Cluster                                        │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                    Control Plane Namespace                           │    │
│  │                                                                      │    │
│  │   ┌─────────────┐    ┌─────────────────────┐    ┌────────────────┐  │    │
│  │   │  Temporal   │    │  Worker Pods        │    │  Controller    │  │    │
│  │   │  Server     │◄──►│  (HPA Scaled)       │    │  (CRD watch)   │  │    │
│  │   │  (or Cloud) │    │                     │    │                │  │    │
│  │   └─────────────┘    └─────────────────────┘    └────────────────┘  │    │
│  │                                                                      │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                    Sandbox Node Pool                                 │    │
│  │                                                                      │    │
│  │   ┌───────────┐  ┌───────────┐  ┌───────────┐  ┌───────────┐       │    │
│  │   │ Job/Pod   │  │ Job/Pod   │  │ Job/Pod   │  │ Job/Pod   │       │    │
│  │   │ (task-1)  │  │ (task-2)  │  │ (task-3)  │  │ (task-4)  │       │    │
│  │   └───────────┘  └───────────┘  └───────────┘  └───────────┘       │    │
│  │                                                                      │    │
│  │   Labels: node-type=sandbox, spot=true                              │    │
│  │   RuntimeClass: gvisor (optional)                                   │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Job-Based Sandbox Execution

The Temporal worker creates a Kubernetes Job for each transformation:

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: transform-task-abc123
  namespace: sandbox-isolated
  labels:
    codetransform.io/task-id: abc123
    codetransform.io/type: agentic
spec:
  ttlSecondsAfterFinished: 3600
  backoffLimit: 0  # No retries - Temporal handles retry logic
  template:
    spec:
      runtimeClassName: gvisor  # Optional: kernel-level isolation
      serviceAccountName: sandbox-runner
      nodeSelector:
        workload-type: sandbox
      containers:
      - name: sandbox
        image: your-org/claude-sandbox:latest
        resources:
          limits:
            memory: "4Gi"
            cpu: "2"
        env:
        - name: ANTHROPIC_API_KEY
          valueFrom:
            secretKeyRef:
              name: claude-credentials
              key: api-key
      restartPolicy: Never
```

The worker then:
1. Creates the Job via client-go
2. Waits for the pod to be Running
3. Execs commands into it (clone, transform, verify)
4. Deletes the Job when complete (or lets TTL clean it up)

### Optional: Agent Sandbox Integration

For advanced use cases requiring warm pools or faster provisioning, [kubernetes-sigs/agent-sandbox](https://github.com/kubernetes-sigs/agent-sandbox) can be used:

```yaml
apiVersion: agents.x-k8s.io/v1alpha1
kind: SandboxTemplate
metadata:
  name: go-standard
spec:
  runtimeClassName: gvisor
  containers:
    - name: sandbox
      image: claude-sandbox-go:1.22
      resources:
        limits:
          memory: 4Gi
          cpu: "2"
---
apiVersion: agents.x-k8s.io/v1alpha1
kind: SandboxWarmPool
metadata:
  name: go-warm-pool
spec:
  templateRef:
    name: go-standard
  minSize: 2
  maxSize: 10
```

**When to use Agent Sandbox:**
- You need sub-second sandbox provisioning (warm pools)
- You want standardized sandbox lifecycle management across multiple platforms
- You have complex multi-container sandbox requirements

**When plain Jobs are sufficient:**
- Transformation tasks take minutes (cold start overhead is negligible)
- You don't need warm pools
- You want simpler operational overhead

### Security

- **RBAC**: Workers can create Jobs and exec into sandbox pods; sandboxes have no K8s API access
- **Network Policies**: Sandboxes allow egress to GitHub, package registries (npm, PyPI, Maven Central), and AI APIs
- **gVisor/Kata**: Optional kernel-level isolation for defense-in-depth
- **IRSA**: IAM roles for service accounts (ECR pull, secrets access)
- **Namespace Isolation**: Each team/tenant can have dedicated sandbox namespace with ResourceQuotas

### Network Policy Example

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: sandbox-egress
  namespace: sandbox-isolated
spec:
  podSelector:
    matchLabels:
      codetransform.io/sandbox: "true"
  policyTypes:
  - Egress
  egress:
  - to:
    - ipBlock:
        cidr: 0.0.0.0/0
    ports:
    - protocol: TCP
      port: 443  # HTTPS only
```

### Scaling

- **Workers**: HPA based on Temporal task queue depth
- **Sandboxes**: Cluster Autoscaler scales node pool based on pending pods
- **Spot Instances**: Cost-effective for ephemeral sandbox workloads

---

## Directory Structure

```
orchestrator/
├── cmd/
│   ├── worker/              # Temporal worker
│   │   └── main.go
│   └── cli/                 # CLI tool
│       └── main.go
├── internal/
│   ├── model/               # Data models
│   │   ├── task.go
│   │   └── result.go
│   ├── workflow/            # Temporal workflows
│   │   ├── execute.go
│   │   └── execute_test.go
│   ├── activity/            # Temporal activities
│   │   ├── sandbox.go
│   │   ├── transform.go
│   │   ├── git.go
│   │   └── github.go
│   ├── sandbox/             # Sandbox abstraction
│   │   ├── provider.go      # Interface
│   │   ├── docker/          # Docker implementation
│   │   ├── kubernetes/      # K8s implementation
│   │   └── agentsandbox/    # Agent Sandbox implementation
│   └── config/              # Configuration loading
├── config/
│   ├── local.yaml
│   └── production.yaml
└── docker/
    └── Dockerfile.sandbox
```

---

## What We Build vs. What We Adopt

| Build (Custom) | Adopt (Standard) |
|----------------|------------------|
| `CodeTransform` CRD and controller | Temporal (workflow orchestration) |
| Task data model | Kubernetes Jobs (sandbox execution) |
| Workflow logic | Claude Code (agentic execution) |
| CLI interface | OpenRewrite/Scalafix (deterministic transforms) |
| GitHub PR integration | Docker/containerd (container runtime) |
| Verifier prompt generation | gVisor/Kata (optional isolation) |
| Configuration loading | Agent Sandbox (optional, for warm pools) |
| Status synchronization (CRD ↔ Temporal) | |

**Principle**: Build the orchestration glue, adopt standards for infrastructure.

### Key Architectural Decisions

1. **CRD as primary interface**: Users define transforms as Kubernetes resources (`kubectl apply -f transform.yaml`), enabling GitOps workflows and K8s-native tooling.

2. **Temporal for durability**: The CRD controller triggers Temporal workflows. Temporal handles retries, timeouts, and human-in-the-loop signals. Status is synced back to the CRD.

3. **Jobs over Agent Sandbox**: Plain Kubernetes Jobs are simpler and sufficient for most use cases. Agent Sandbox is an optional optimization for warm pools.

4. **Two transform modes**: Deterministic (Docker images like OpenRewrite) and Agentic (Claude Code with prompts) share the same orchestration infrastructure but have different execution paths.

5. **Agent-agnostic design**: The platform's value is in the orchestration layer (durability, HITL, multi-repo coordination, PR creation), not the specific AI agent. Claude Code is the current implementation, but the agent is a swappable component:

   ```
   Platform (durable)          Agent (swappable)
   ─────────────────           ─────────────────
   • Temporal workflows        • Claude Code CLI (today)
   • Human-in-the-loop         • Other agents (future): Codex, Aider, custom
   • Multi-repo coordination   • Interface: exec(prompt) → code changes
   • PR creation               • Verifiers validate output regardless of agent
   • Cost/rate limiting
   • Audit trail
   ```

   This design de-risks the "is the agent good enough?" question—the platform provides value for deterministic transforms today, and agentic capabilities improve over time without architectural changes.

---

## Campaign Orchestration

A **CodeTransform** is the atomic unit—it operates on a specific set of repositories. For large-scale rollouts (e.g., "upgrade logging across 200 services") or distributed discovery (e.g., "audit auth patterns across 100 services"), a higher-level **Campaign** orchestrator manages batches:

### Campaign Types

| Type | Mode | Output | Use Case |
|------|------|--------|----------|
| **Transform Campaign** | `transform` | PRs created | Migrations, upgrades, refactoring |
| **Discovery Campaign** | `report` | Aggregated reports | Audits, inventories, assessments |
| **Two-Phase Campaign** | Both | Reports → PRs | Discover issues, then fix them |

```
┌─────────────────────────────────────────────────────────────────┐
│                     Campaign Orchestrator                        │
│                                                                  │
│   - Submits CodeTransform CRDs in batches                       │
│   - Monitors progress across all transforms                     │
│   - Pauses on failure threshold (e.g., >10% failed)             │
│   - Asks human: abort / continue / retry failed                 │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│   CodeTransform    CodeTransform    CodeTransform    ...        │
│   (service-a)      (service-b)      (service-c)                 │
└─────────────────────────────────────────────────────────────────┘
```

### Batch Failure Semantics

When running transforms across many repositories:

| Scenario | Behavior |
|----------|----------|
| All succeed | Campaign completes, PRs created |
| Some fail (<threshold) | Continue, report failures at end |
| Many fail (≥threshold) | **Pause and ask human**: abort / continue / retry failed |
| Critical failure | Halt immediately, notify human |

The failure threshold is configurable per campaign (default: 10%).

### Campaign vs CodeTransform Separation

| Concern | CodeTransform (CRD) | Campaign (Orchestrator) |
|---------|---------------------|-------------------------|
| Repository list | Explicit in spec | Manages which repos to include |
| Mode | `transform` or `report` | Can mix modes (discover → transform) |
| Execution | Single Temporal workflow | Submits multiple CodeTransforms |
| Failure handling | Per-repo retry via Temporal | Batch-level pause on threshold |
| Human approval | Per-transform approve/reject | Batch-level abort/continue |
| Output | PR URLs or report data | Aggregated results/reports |

> **Note**: Campaign orchestration is a future capability. Initially, users submit CodeTransform CRDs
> directly with explicit repository lists.

---

## Discovery Mode (Report Mode)

While the platform is primarily designed for code transformations, the same infrastructure supports
**distributed discovery and analysis** across repositories. Instead of making changes and creating PRs,
report mode collects structured data from each repository.

### Use Cases

| Use Case | Description |
|----------|-------------|
| **Security audit** | Analyze authentication patterns across 100 services |
| **Dependency inventory** | Catalog all Log4j versions across the org |
| **API assessment** | Evaluate 100 endpoints in a monorepo for compliance |
| **Technical debt survey** | Identify deprecated patterns before a migration |
| **Pre-migration analysis** | Gather data to inform a Campaign's transform strategy |

### Report Mode CRD

```yaml
apiVersion: codetransform.io/v1alpha1
kind: CodeTransform
metadata:
  name: auth-security-audit
spec:
  mode: report  # Key difference: no PRs created

  repositories:
    - url: https://github.com/org/service-a.git
    - url: https://github.com/org/service-b.git
    # ... up to 100 repos

  transform:
    agent:
      prompt: |
        Analyze this repository's authentication implementation:
        - What auth library is used?
        - Are there any hardcoded credentials?
        - Is token rotation implemented?
        - Rate limiting on auth endpoints?

        Output your findings as JSON to stdout in the specified schema.

      outputSchema:
        type: object
        required: ["auth_library", "issues", "score"]
        properties:
          auth_library:
            type: string
          issues:
            type: array
            items:
              type: object
              properties:
                severity: { type: string, enum: ["low", "medium", "high", "critical"] }
                description: { type: string }
                location: { type: string }
          score:
            type: integer
            minimum: 1
            maximum: 10

  timeout: 15m
  # No pullRequest section - report mode doesn't create PRs

status:
  phase: Completed
  repositories:
    - name: service-a
      status: Completed
      report:
        auth_library: "oauth2-proxy"
        issues: []
        score: 9
    - name: service-b
      status: Completed
      report:
        auth_library: "custom"
        issues:
          - severity: "high"
            description: "Hardcoded API key in config.yaml"
            location: "config/config.yaml:42"
        score: 3
```

### forEach: Multiple Targets in One Repository

For analyzing multiple components within a single large repository (e.g., a monorepo with 100 API endpoints):

```yaml
apiVersion: codetransform.io/v1alpha1
kind: CodeTransform
metadata:
  name: api-endpoint-audit
spec:
  mode: report

  repositories:
    - url: https://github.com/org/monolith.git

  # Iterate over targets within the repo
  forEach:
    - name: users-api
      context: "Focus on src/api/users/"
    - name: orders-api
      context: "Focus on src/api/orders/"
    - name: payments-api
      context: "Focus on src/api/payments/"
    # ... 100 endpoints

  transform:
    agent:
      prompt: |
        {{.context}}

        Assess this API endpoint for:
        - Input validation completeness
        - Error handling patterns
        - Rate limiting implementation
        - Logging coverage

        Output your assessment as JSON.

      outputSchema:
        type: object
        properties:
          endpoint: { type: string }
          input_validation: { type: string, enum: ["none", "partial", "complete"] }
          error_handling: { type: string, enum: ["poor", "adequate", "good"] }
          has_rate_limiting: { type: boolean }
          logging_score: { type: integer, minimum: 1, maximum: 5 }

status:
  phase: Completed
  reports:
    - target: users-api
      output:
        endpoint: "/api/users"
        input_validation: "complete"
        error_handling: "good"
        has_rate_limiting: true
        logging_score: 4
    - target: orders-api
      output:
        endpoint: "/api/orders"
        input_validation: "partial"
        error_handling: "adequate"
        has_rate_limiting: false
        logging_score: 2
```

### Discovery Campaigns

A Discovery Campaign aggregates reports across many CodeTransforms:

```
┌─────────────────────────────────────────────────────────────────┐
│                   Discovery Campaign                             │
│                                                                  │
│   Phase 1: Submit CodeTransforms (mode: report)                 │
│   Phase 2: Collect all reports from CRD status                  │
│   Phase 3: Aggregate into summary report                        │
│   Phase 4: (Optional) Trigger follow-up transforms              │
│                                                                  │
│   Output:                                                        │
│   - Individual reports per repo/target                          │
│   - Aggregated statistics                                       │
│   - Prioritized action items                                    │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│  CodeTransform     CodeTransform     CodeTransform    ...       │
│  (mode: report)    (mode: report)    (mode: report)             │
│  (service-a)       (service-b)       (service-c)                │
└─────────────────────────────────────────────────────────────────┘
```

### Two-Phase Pattern: Discover → Transform

Discovery campaigns can feed into transformation campaigns:

```
┌─────────────────────────────────────────────────────────────────┐
│   Phase 1: Discovery                                             │
│   "Assess Log4j usage across all Java services"                 │
│                                                                  │
│   Output: 47 services using Log4j 1.x, 23 using 2.x, 30 clean  │
└─────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────┐
│   Phase 2: Transform (targeted)                                  │
│   "Upgrade the 47 services using Log4j 1.x to 2.x"              │
│                                                                  │
│   Submits CodeTransforms only for the 47 affected services      │
└─────────────────────────────────────────────────────────────────┘
```

### Report Storage

For large-scale discovery (100+ repos), reports may be too large for CRD status:

| Storage Option | Use Case |
|----------------|----------|
| **CRD status** | Small reports (<1KB each), <50 repos |
| **ConfigMap** | Medium reports, <100 repos |
| **S3/GCS** | Large reports, archival, 100+ repos |
| **PVC** | Offline processing, custom aggregation |

The operator config specifies the storage backend:

```yaml
reportStorage:
  backend: s3  # "status" | "configmap" | "s3" | "pvc"
  s3:
    bucket: codetransform-reports
    prefix: discovery/
```

---

## Credential Handling

Credentials are stored as Kubernetes Secrets and referenced in the CodeTransform spec:

```yaml
apiVersion: codetransform.io/v1alpha1
kind: CodeTransform
metadata:
  name: upgrade-logging
spec:
  # ... repositories, transform, etc.

  credentials:
    github:
      secretRef:
        name: github-token
        key: token
    anthropic:
      secretRef:
        name: claude-credentials
        key: api-key
```

### Credential Flow

1. **Controller** reads secret references from CodeTransform spec
2. **Worker** mounts secrets into sandbox pod (or passes via env)
3. **Sandbox** uses credentials for git operations and API calls
4. **Cleanup** ensures credentials are not persisted in sandbox

### Security Considerations

- Secrets are namespace-scoped; CodeTransform can only reference secrets in its namespace
- Sandbox pods use a dedicated ServiceAccount with minimal RBAC
- Audit logging tracks which transforms accessed which secrets
- Consider [External Secrets Operator](https://external-secrets.io/) for centralized secret management

---

## Agent Failure Handling

Agentic transforms can fail in ways that deterministic transforms cannot. The platform must handle:

### Failure Modes

| Failure Mode | Detection | Response |
|--------------|-----------|----------|
| **Claude stuck in loop** | Iteration count > max | Terminate, report partial output |
| **Token budget exceeded** | Token counter > limit | Terminate, report what was accomplished |
| **Verifiers keep failing** | Retry count > max | Stop iteration, ask human for guidance |
| **Claude refuses (safety)** | Specific error patterns | Report refusal reason, allow human override |
| **Timeout** | Wall clock > spec.timeout | Terminate sandbox, report timeout |

### Iteration Limits

```yaml
apiVersion: codetransform.io/v1alpha1
kind: CodeTransform
spec:
  transform:
    agent:
      prompt: "..."
      limits:
        maxIterations: 10        # Max Claude invocations
        maxTokens: 100000        # Total input+output tokens
        maxVerifierRetries: 3    # Retries after verifier failure
```

### Graceful Degradation

When an agentic transform fails:

1. **Preserve partial work**: Commit changes to a WIP branch even if incomplete
2. **Capture diagnostics**: Save Claude's conversation history, verifier output
3. **Enable recovery**: Allow human to:
   - Review partial changes and steer ("try a different approach")
   - Approve partial changes ("good enough, create PR")
   - Abort and discard

### Stuck Detection

The workflow monitors for progress:

```go
// If no new file changes in N iterations, consider stuck
if iterationsSinceLastChange > 3 {
    return StuckError("No progress after 3 iterations")
}
```

---

## Rate Limiting

The platform must respect external API limits and control costs.

### GitHub API Limits

| Limit | Value | Mitigation |
|-------|-------|------------|
| REST API | 5,000 req/hr per token | Queue requests, use conditional requests |
| Git operations | Varies by plan | Batch clones, use shallow clones |
| PR creation | No hard limit | Self-imposed limit per campaign |

### Claude API Limits

| Concern | Mitigation |
|---------|------------|
| Tokens per minute | Configurable delay between transforms |
| Cost per transform | Token budget per CodeTransform (see above) |
| Runaway costs | Campaign-level budget with hard stop |

### Implementation

```yaml
# Operator config
rateLimits:
  github:
    requestsPerHour: 4000      # Leave headroom
    parallelClones: 5          # Max concurrent git operations
  claude:
    tokensPerMinute: 100000    # Anthropic tier limit
    maxCostPerTransform: 5.00  # USD, terminate if exceeded
    maxCostPerCampaign: 500.00 # USD, pause campaign if exceeded
```

### Cost Attribution

- Track token usage per CodeTransform
- Attribute to team/namespace for chargeback
- Surface in observability metrics (`codetransform_cost_usd` gauge)
