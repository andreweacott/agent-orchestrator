# Code Transformation Platform - Technical Design

## Overview

A platform for automated code transformations and discovery across repositories, supporting:

- **Deployment**: Local (Docker) or production (Kubernetes)
- **Scope**: Single repository or fleet-wide via Campaigns
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

### What We Build vs. What We Adopt

| Build (Custom) | Adopt (Standard) |
|----------------|------------------|
| Task and Campaign schemas | Temporal (workflow orchestration) |
| Task data model | Kubernetes Jobs (sandbox execution) |
| Workflow logic | Claude Code (agentic execution) |
| CLI interface | OpenRewrite/Scalafix (deterministic transforms) |
| GitHub PR integration | Docker/containerd (container runtime) |
| Verifier prompt generation | gVisor/Kata (optional isolation) |
| Configuration loading | |
| Campaign orchestration | |

**Principle**: Build the orchestration glue, adopt standards for infrastructure.

---

## Core Concepts

### Task

The atomic unit of work. A Task operates on one or more repositories and either:
- **Transforms** code and creates PRs (`mode: transform`)
- **Reports** structured data without PRs (`mode: report`)

### Campaign

Orchestrates multiple Tasks across many repositories:
- Submits Tasks in configurable batches
- Monitors progress and aggregates results
- Pauses on failure thresholds for human decision
- Enables two-phase patterns (discover → transform)

### Modes

| Mode | Output | Approval | Use Case |
|------|--------|----------|----------|
| `transform` | Pull requests | Yes | Migrations, upgrades, fixes |
| `report` | Structured JSON | No | Audits, inventories, discovery |

### Execution Types

| Type | Implementation | Verification |
|------|----------------|--------------|
| **Deterministic** | Docker image (OpenRewrite, custom script) | Exit code + verifiers |
| **Agentic** | Claude Code CLI with prompt | Agent iterates until verifiers pass |

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Architecture                                    │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   Interface Layer                                                           │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │   CLI                    │   (Optional) K8s Operator                │   │
│   │   orchestrator run       │   Watches YAML, submits to Temporal      │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│       │                                        │                             │
│       └────────────────────┬───────────────────┘                             │
│                            ▼                                                 │
│   Orchestration Layer                                                       │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                         Temporal                                     │   │
│   │                                                                      │   │
│   │   - Durable task execution                                          │   │
│   │   - Retry policies                                                   │   │
│   │   - Human-in-the-loop signals (approve/reject/steer)                │   │
│   │   - Campaign batch orchestration                                    │   │
│   │                                                                      │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│       │                                                                      │
│       ▼                                                                      │
│   Execution Layer                                                           │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                      Sandbox Provider                                │   │
│   │                                                                      │   │
│   │   Local:      Docker containers                                     │   │
│   │   Production: Kubernetes Jobs                                       │   │
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
│       │                                                                      │
│       ▼                                                                      │
│   Integration Layer                                                         │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │   GitHub (PR creation)  │  Slack (notifications)  │  Storage (reports)  │
│   └─────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Interface Hierarchy

1. **CLI** (`orchestrator run --file task.yaml`) - Direct submission to Temporal
2. **Temporal API** - Programmatic workflow submission
3. **(Optional) K8s Operator** - Watches YAML resources, submits to Temporal

The K8s operator is an optional convenience layer. The CLI and Temporal API are the primary interfaces.

### Standards Used

| Concern | Solution | Why |
|---------|----------|-----|
| Workflow Orchestration | [Temporal](https://temporal.io/) | Durable execution, signals/queries, battle-tested |
| Sandbox (Local) | Docker | Simple, universal |
| Sandbox (Production) | Kubernetes Jobs | Built-in lifecycle, scaling, isolation |
| Deterministic Transforms | [OpenRewrite](https://docs.openrewrite.org/), [Scalafix](https://scalacenter.github.io/scalafix/) | Mature AST-based tools |
| AI Agent | Claude Code CLI | Agentic loop, context management, tool use |

---

## Data Model

### Schema Versioning

All Task and Campaign YAML files require a `version` field. This enables:

- **Forward compatibility**: Older CLI versions reject unknown schema versions
- **Migration tooling**: `orchestrator migrate --file task.yaml` upgrades schemas
- **Clear deprecation**: Announce version sunset with migration period

**Version Format**: Integer (e.g., `1`, `2`)

| Version | Status | Notes |
|---------|--------|-------|
| `1` | Current | Initial stable schema |

**Breaking Changes** (require version bump):
- Removing or renaming required fields
- Changing field types
- Changing default behavior

**Non-Breaking Changes** (same version):
- Adding optional fields with defaults
- Adding new enum values
- Relaxing validation constraints

**Loader Behavior**:

```go
func LoadTask(data []byte) (*Task, error) {
    // 1. Parse version field first
    var header struct {
        Version *int `yaml:"version"`
    }
    if err := yaml.Unmarshal(data, &header); err != nil {
        return nil, err
    }

    // 2. Validate version
    if header.Version == nil {
        return nil, errors.New("version field is required")
    }
    switch *header.Version {
    case 1:
        return loadTaskV1(data)
    default:
        return nil, fmt.Errorf("unsupported schema version: %d (supported: 1)", *header.Version)
    }
}
```

### Task Schema (task.yaml)

The primary interface for defining transformations:

```yaml
# task.yaml - Complete schema
version: 1                    # Schema version (required, integer)
id: string                    # Unique identifier
title: string                 # Human-readable title
description: string           # Optional longer description

mode: transform | report      # Default: transform

repositories:
  - url: string               # Git URL (required)
    branch: string            # Default: main
    name: string              # Directory name, derived from URL if not set
    setup:                    # Commands to run after clone
      - string

# For report mode: iterate over targets within a repo
for_each:                     # Optional
  - name: string              # Target identifier
    context: string           # Passed to prompt as {{.context}}

execution:
  # Option 1: Agentic (Claude Code)
  agentic:
    prompt: string            # The prompt for the agent
    verifiers:
      - name: string          # e.g., "build", "test"
        command: [string]     # e.g., ["go", "build", "./..."]
    limits:
      max_iterations: int     # Max agent invocations (default: 10)
      max_tokens: int         # Total token budget (default: 100000)
      max_verifier_retries: int # Retries after verifier failure (default: 3)
    output:                   # For report mode
      schema:                 # JSON schema to validate frontmatter
        type: object
        properties: {}

  # Option 2: Deterministic (Docker image)
  deterministic:
    image: string             # Docker image ref
    command: [string]         # Override entrypoint
    args: [string]            # Arguments
    env:                      # Environment variables
      KEY: value
    verifiers:                # Optional post-execution verification
      - name: string
        command: [string]

# Execution settings
timeout: duration             # e.g., "30m" - Total wall-clock time for entire task
                              # Includes: sandbox provisioning, clone, execution, verification
                              # Does NOT include: approval wait time (separate timeout)
                              # For multi-repo tasks: applies to entire task, not per-repo
require_approval: boolean     # Default: true for agentic, false for deterministic

# PR settings (transform mode only)
pull_request:
  branch_prefix: string       # e.g., "auto/slog-migration"
  title: string               # PR title
  body: string                # PR body template
  labels: [string]            # Labels to apply
  reviewers: [string]         # Reviewers to request

# Sandbox settings (production)
sandbox:
  namespace: string           # K8s namespace for sandbox Jobs
  runtime_class: string       # Optional: gvisor, kata
  node_selector:
    key: value
  resources:
    limits:
      memory: string          # e.g., "4Gi"
      cpu: string             # e.g., "2"

# Credentials (production)
credentials:
  github:
    secret_ref:
      name: string
      key: string
  anthropic:
    secret_ref:
      name: string
      key: string
```

#### Task Examples

**Agentic Transform:**
```yaml
version: 1
id: slog-migration
title: "Migrate to structured logging"
mode: transform

repositories:
  - url: https://github.com/org/service-a.git
    setup: ["go mod download"]
  - url: https://github.com/org/service-b.git
    setup: ["go mod download"]

execution:
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
require_approval: true

pull_request:
  branch_prefix: "auto/slog-migration"
  title: "Migrate to structured logging (slog)"
  labels: ["automated", "logging"]
```

**Deterministic Transform:**
```yaml
version: 1
id: log4j-upgrade
title: "Upgrade Log4j 1.x to 2.x"
mode: transform

repositories:
  - url: https://github.com/org/java-service.git

execution:
  deterministic:
    image: openrewrite/rewrite:latest
    args:
      - "rewrite:run"
      - "-Drewrite.activeRecipes=org.openrewrite.java.logging.log4j.Log4j1ToLog4j2"

    verifiers:
      - name: build
        command: ["mvn", "compile"]

timeout: 20m
require_approval: false  # Pre-vetted deterministic transform

pull_request:
  branch_prefix: "security/log4j-upgrade"
  title: "Upgrade Log4j 1.x to 2.x"
```

**Report Mode:**
```yaml
version: 1
id: auth-audit
title: "Authentication security audit"
mode: report

repositories:
  - url: https://github.com/org/service-a.git
  - url: https://github.com/org/service-b.git

execution:
  agentic:
    prompt: |
      Analyze this repository's authentication implementation.

      Output your findings as a markdown document with:
      1. YAML frontmatter containing structured data (auth_library, score, issues)
      2. Detailed prose analysis with findings, rationale, and recommendations

    output:
      schema:  # Validates the frontmatter
        type: object
        required: ["auth_library", "score"]
        properties:
          auth_library:
            type: string
          score:
            type: integer
            minimum: 1
            maximum: 10
          issues:
            type: array
            items:
              type: object
              properties:
                severity:
                  type: string
                  enum: ["low", "medium", "high", "critical"]
                location:
                  type: string

timeout: 15m
```

**Example agent output (frontmatter + markdown):**
```markdown
---
auth_library: custom
score: 3
issues:
  - severity: high
    location: config/config.yaml:42
  - severity: medium
    location: pkg/auth/jwt.go:89
---

# Authentication Audit: service-b

## Summary

This service uses a **custom authentication implementation** with significant security concerns.
Overall security score: **3/10**

## Critical Findings

### 1. Hardcoded API Key (High Severity)

Found hardcoded API key in `config/config.yaml` at line 42:

\`\`\`yaml
api_key: "sk-1234567890abcdef"
\`\`\`

**Why this matters**: Hardcoded credentials in source control can be extracted by anyone
with repository access and are difficult to rotate after a breach.

**Recommendation**: Use environment variables or a secrets manager like HashiCorp Vault.

### 2. No Token Expiration (Medium Severity)

JWT tokens in `pkg/auth/jwt.go` are issued without an expiration claim...

## Recommendations

1. **Immediate**: Rotate the exposed API key
2. **Short-term**: Implement token expiration with 24-hour TTL
3. **Medium-term**: Add rate limiting to authentication endpoints
```

**Report Mode with forEach:**
```yaml
version: 1
id: api-endpoint-audit
title: "API endpoint assessment"
mode: report

repositories:
  - url: https://github.com/org/monolith.git

for_each:
  - name: users-api
    context: "Focus on src/api/users/"
  - name: orders-api
    context: "Focus on src/api/orders/"
  - name: payments-api
    context: "Focus on src/api/payments/"

execution:
  agentic:
    prompt: |
      {{.context}}

      Assess this API endpoint for:
      - Input validation completeness
      - Error handling patterns
      - Rate limiting implementation

      Output as markdown with YAML frontmatter for structured data.

    output:
      schema:
        type: object
        properties:
          endpoint:
            type: string
          input_validation:
            type: string
            enum: ["none", "partial", "complete"]
          has_rate_limiting:
            type: boolean
          issue_count:
            type: integer

timeout: 10m
```

### Campaign Schema (campaign.yaml)

Orchestrates multiple Tasks across repositories:

```yaml
# campaign.yaml - Complete schema
version: 1                    # Schema version (required, integer)
id: string                    # Unique identifier
title: string                 # Human-readable title
description: string           # Optional longer description

# Repository selection
repositories:
  # Option 1: Explicit list
  explicit:
    - url: string
      branch: string
      setup: [string]

  # Option 2: Query-based (future)
  query:
    org: string               # GitHub org
    topics: [string]          # Filter by topics
    language: string          # Filter by language
    exclude: [string]         # Repos to exclude

# Task template - applied to each repository
task_template:
  mode: transform | report
  execution:
    agentic:
      prompt: string
      verifiers: []
    # or deterministic: {}
  timeout: duration
  require_approval: boolean
  pull_request: {}

# Batch configuration
batch:
  size: int                   # Tasks per batch (default: 10)
  parallelism: int            # Concurrent Tasks within batch (default: 5)
  delay_between: duration     # Delay between batches (default: 0)

# Failure handling
failure:
  threshold_percent: int      # Pause if failure rate exceeds (default: 10)
  threshold_count: int        # Or pause after N failures
  action: pause | abort       # What to do on threshold (default: pause)

# Two-phase configuration
phases:
  - name: discover
    mode: report
    task_template: {}
  - name: transform
    mode: transform
    # Repositories filtered to those with findings from discover phase
    filter_from_phase: discover
    filter_condition: ".uses_deprecated_api == true"
    task_template: {}
```

#### Campaign Examples

**Transform Campaign:**
```yaml
version: 1
id: slog-migration-campaign
title: "Migrate all Go services to slog"

repositories:
  explicit:
    - url: https://github.com/org/service-a.git
    - url: https://github.com/org/service-b.git
    # ... 50 more services

task_template:
  mode: transform
  execution:
    agentic:
      prompt: "Migrate from log.Printf to slog package..."
      verifiers:
        - name: build
          command: ["go", "build", "./..."]
        - name: test
          command: ["go", "test", "./..."]
  timeout: 30m
  require_approval: true
  pull_request:
    branch_prefix: "auto/slog-migration"
    title: "Migrate to structured logging (slog)"

batch:
  size: 10
  parallelism: 5

failure:
  threshold_percent: 10
  action: pause
```

**Discovery Campaign:**
```yaml
version: 1
id: log4j-discovery
title: "Discover Log4j usage across all Java services"

repositories:
  query:
    org: my-org
    language: java

task_template:
  mode: report
  execution:
    agentic:
      prompt: |
        Analyze Log4j usage:
        - Which version is used?
        - How many files import Log4j?
        - Is Log4j in dependencies?

      output:
        schema:
          type: object
          properties:
            log4j_version:
              type: string
            file_count:
              type: integer
            in_dependencies:
              type: boolean
  timeout: 10m

batch:
  size: 20
  parallelism: 10
```

**Two-Phase Campaign:**
```yaml
version: 1
id: api-v2-migration
title: "Discover and migrate deprecated v1 API"

phases:
  - name: discover
    mode: report
    task_template:
      execution:
        agentic:
          prompt: "Find usage of deprecated v1 API client..."
          output:
            schema:
              type: object
              properties:
                uses_v1_api:
                  type: boolean
                usage_locations:
                  type: array

  - name: transform
    mode: transform
    filter_from_phase: discover
    filter_condition: ".uses_v1_api == true"
    task_template:
      execution:
        agentic:
          prompt: "Migrate from v1 API client to v2..."
          verifiers:
            - name: build
              command: ["go", "build", "./..."]
      pull_request:
        branch_prefix: "auto/api-v2-migration"
        title: "Migrate to v2 API client"

batch:
  size: 10
  parallelism: 5

failure:
  threshold_percent: 15
  action: pause
```

### Go Types

The schemas map to internal Go types used by Temporal workflows:

```go
// Task is the input for the Task workflow
type Task struct {
    Version      int                `json:"version"`        // Schema version, e.g., 1
    ID           string             `json:"id"`
    Title        string             `json:"title"`
    Description  string             `json:"description,omitempty"`
    Mode         TaskMode           `json:"mode"`           // "transform" or "report"
    Repositories []RepositoryTarget `json:"repositories"`
    ForEach      []ForEachTarget    `json:"for_each,omitempty"`
    Execution    Execution          `json:"execution"`
    Timeout      time.Duration      `json:"timeout,omitempty"`
    RequireApproval bool            `json:"require_approval"`
    PullRequest  *PullRequestConfig `json:"pull_request,omitempty"`
    Sandbox      *SandboxConfig     `json:"sandbox,omitempty"`
}

type TaskMode string
const (
    TaskModeTransform TaskMode = "transform"
    TaskModeReport    TaskMode = "report"
)

type RepositoryTarget struct {
    URL    string   `json:"url"`
    Branch string   `json:"branch,omitempty"` // Default: "main"
    Name   string   `json:"name,omitempty"`   // Derived from URL if not set
    Setup  []string `json:"setup,omitempty"`
}

type ForEachTarget struct {
    Name    string `json:"name"`
    Context string `json:"context"`
}

type Execution struct {
    Agentic       *AgenticExecution       `json:"agentic,omitempty"`
    Deterministic *DeterministicExecution `json:"deterministic,omitempty"`
}

type AgenticExecution struct {
    Prompt    string        `json:"prompt"`
    Verifiers []Verifier    `json:"verifiers,omitempty"`
    Limits    *AgentLimits  `json:"limits,omitempty"`
    Output    *OutputConfig `json:"output,omitempty"` // For report mode
}

// OutputConfig defines how report mode output is captured and validated
type OutputConfig struct {
    Schema json.RawMessage `json:"schema,omitempty"` // JSON Schema for frontmatter validation
}

// ReportOutput represents the parsed output from a report-mode task
type ReportOutput struct {
    Frontmatter map[string]any `json:"frontmatter"` // Structured data (validated against schema)
    Body        string         `json:"body"`        // Markdown prose
    Raw         string         `json:"raw"`         // Original unparsed output
}

type DeterministicExecution struct {
    Image     string            `json:"image"`
    Command   []string          `json:"command,omitempty"`
    Args      []string          `json:"args,omitempty"`
    Env       map[string]string `json:"env,omitempty"`
    Verifiers []Verifier        `json:"verifiers,omitempty"`
}

type Verifier struct {
    Name    string   `json:"name"`
    Command []string `json:"command"`
}

type AgentLimits struct {
    MaxIterations      int `json:"max_iterations"`
    MaxTokens          int `json:"max_tokens"`
    MaxVerifierRetries int `json:"max_verifier_retries"`
}
```

### Result Types

```go
type TaskResult struct {
    TaskID       string             `json:"task_id"`
    Status       TaskStatus         `json:"status"`
    Mode         TaskMode           `json:"mode"`
    Repositories []RepositoryResult `json:"repositories"`
    StartedAt    time.Time          `json:"started_at"`
    CompletedAt  *time.Time         `json:"completed_at,omitempty"`
    Error        *string            `json:"error,omitempty"`
}

type TaskStatus string
const (
    TaskStatusPending          TaskStatus = "pending"
    TaskStatusRunning          TaskStatus = "running"
    TaskStatusAwaitingApproval TaskStatus = "awaiting_approval"
    TaskStatusCompleted        TaskStatus = "completed"
    TaskStatusFailed           TaskStatus = "failed"
    TaskStatusCancelled        TaskStatus = "cancelled"
)

type RepositoryResult struct {
    Repository    string           `json:"repository"`
    Status        string           `json:"status"` // "success" | "failed" | "skipped"
    FilesModified []string         `json:"files_modified,omitempty"`
    PullRequest   *PullRequestInfo `json:"pull_request,omitempty"` // Transform mode
    Report        *ReportOutput    `json:"report,omitempty"`        // Report mode
    Error         *string          `json:"error,omitempty"`
}

type PullRequestInfo struct {
    URL    string `json:"url"`
    Number int    `json:"number"`
    Branch string `json:"branch"`
}

// Campaign result aggregates Task results
type CampaignResult struct {
    CampaignID   string       `json:"campaign_id"`
    Status       string       `json:"status"`
    TaskResults  []TaskResult `json:"task_results"`
    Summary      CampaignSummary `json:"summary"`
    StartedAt    time.Time    `json:"started_at"`
    CompletedAt  *time.Time   `json:"completed_at,omitempty"`
}

type CampaignSummary struct {
    TotalTasks     int `json:"total_tasks"`
    CompletedTasks int `json:"completed_tasks"`
    FailedTasks    int `json:"failed_tasks"`
    SkippedTasks   int `json:"skipped_tasks"`
    PRsCreated     int `json:"prs_created,omitempty"`     // Transform mode
    ReportsCollected int `json:"reports_collected,omitempty"` // Report mode
}
```

---

## Sandbox Provider Interface

Abstracts container runtime for local and production:

```go
type Provider interface {
    // Provision creates a new sandbox
    Provision(ctx context.Context, opts ProvisionOptions) (*Sandbox, error)

    // Exec runs a command in the sandbox
    Exec(ctx context.Context, id string, cmd ExecCommand) (*ExecResult, error)

    // CopyTo copies data into the sandbox
    CopyTo(ctx context.Context, id string, src io.Reader, destPath string) error

    // CopyFrom copies data from the sandbox
    CopyFrom(ctx context.Context, id string, srcPath string) (io.ReadCloser, error)

    // Status returns the current sandbox status
    Status(ctx context.Context, id string) (*SandboxStatus, error)

    // Cleanup destroys the sandbox
    Cleanup(ctx context.Context, id string) error

    // Name returns the provider name
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
    Provider   string // "docker" | "kubernetes"
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

### Docker Provider (Local Development)

For local development and testing:

```go
func NewDockerProvider() (Provider, error) {
    client, err := docker.NewClientWithOpts(docker.FromEnv)
    if err != nil {
        return nil, err
    }
    return &dockerProvider{client: client}, nil
}
```

Provisions containers with:
- Bind-mounted workspace directory
- Environment variables for credentials
- Configurable resource limits
- Automatic cleanup on completion

### Kubernetes Jobs Provider (Production)

For production workloads, the provider creates Kubernetes Jobs:

```go
func NewKubernetesProvider(clientset *kubernetes.Clientset) Provider {
    return &k8sProvider{clientset: clientset}
}
```

**Job Specification:**

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: task-{{.TaskID}}
  namespace: {{.Namespace}}
  labels:
    app.kubernetes.io/name: code-transform-sandbox
    app.kubernetes.io/component: sandbox
    codetransform.io/task-id: {{.TaskID}}
spec:
  ttlSecondsAfterFinished: 3600
  backoffLimit: 0  # No retries - Temporal handles retry logic
  template:
    spec:
      runtimeClassName: {{.RuntimeClass}}  # Optional: gvisor
      serviceAccountName: sandbox-runner
      nodeSelector:
        {{range $k, $v := .NodeSelector}}
        {{$k}}: {{$v}}
        {{end}}
      containers:
      - name: sandbox
        image: {{.Image}}
        resources:
          limits:
            memory: {{.Resources.Memory}}
            cpu: {{.Resources.CPU}}
        env:
        - name: ANTHROPIC_API_KEY
          valueFrom:
            secretKeyRef:
              name: claude-credentials
              key: api-key
        - name: GITHUB_TOKEN
          valueFrom:
            secretKeyRef:
              name: github-credentials
              key: token
      restartPolicy: Never
```

**Provider Selection:**

```go
func NewProvider() (Provider, error) {
    switch os.Getenv("SANDBOX_PROVIDER") {
    case "kubernetes":
        return kubernetes.NewProvider()
    default:
        return docker.NewProvider()
    }
}
```

### Security (Production)

**RBAC:**
- Worker ServiceAccount: create Jobs, exec into pods, read secrets
- Sandbox ServiceAccount: no K8s API access (empty RBAC)

**Network Policy:**

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: sandbox-egress
  namespace: sandbox-isolated
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/component: sandbox
  policyTypes:
  - Egress
  - Ingress
  ingress: []  # Deny all ingress
  egress:
  - to:
    - ipBlock:
        cidr: 0.0.0.0/0
    ports:
    - protocol: TCP
      port: 443  # HTTPS only
```

**Resource Limits:**
```yaml
resources:
  limits:
    memory: "4Gi"
    cpu: "2"
  requests:
    memory: "2Gi"
    cpu: "1"
```

---

## Temporal Workflows

### Task Workflow

The Task workflow executes a single Task across one or more repositories:

```
┌─────────────────────────────────────────────────────────────────┐
│                       Task Workflow                              │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   1. Provision Sandbox                                          │
│         │                                                        │
│         ▼                                                        │
│   2. For each repository:                                        │
│         │                                                        │
│         ├──▶ Clone Repository                                   │
│         │         │                                              │
│         │         ▼                                              │
│         │    Run Setup Commands                                  │
│         │         │                                              │
│         │         ▼                                              │
│         │    Execute Transformation                              │
│         │    (deterministic or agentic)                          │
│         │         │                                              │
│         │         ▼                                              │
│         │    Run Verifiers (final gate)                          │
│         │         │                                              │
│         └──▶ Collect Result                                     │
│         │                                                        │
│         ▼                                                        │
│   3. Mode == transform?                                          │
│         │                                                        │
│         ├── Yes: Await Approval ──▶ Create PRs                  │
│         │                                                        │
│         └── No (report): Collect Reports                        │
│         │                                                        │
│         ▼                                                        │
│   4. Cleanup Sandbox                                             │
│         │                                                        │
│         ▼                                                        │
│   5. Return TaskResult                                           │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

**Workflow Pseudocode:**

```go
func TaskWorkflow(ctx workflow.Context, task Task) (*TaskResult, error) {
    result := &TaskResult{
        TaskID:    task.ID,
        Mode:      task.Mode,
        Status:    TaskStatusRunning,
        StartedAt: workflow.Now(ctx),
    }

    // 1. Provision sandbox
    sandbox, err := workflow.ExecuteActivity(ctx, ProvisionSandbox, task).Get(ctx, nil)
    if err != nil {
        return failedResult(task.ID, err), nil
    }
    defer workflow.ExecuteActivity(ctx, CleanupSandbox, sandbox)

    // 2. Process each repository
    for _, repo := range task.Repositories {
        repoResult := processRepository(ctx, task, sandbox, repo)
        result.Repositories = append(result.Repositories, repoResult)
    }

    // 3. Mode-specific handling
    if task.Mode == TaskModeTransform && task.RequireApproval {
        // Wait for human approval
        result.Status = TaskStatusAwaitingApproval
        approved := awaitApproval(ctx, task.ID)
        if !approved {
            result.Status = TaskStatusCancelled
            return result, nil
        }

        // Create PRs
        for i, repoResult := range result.Repositories {
            if repoResult.Status == "success" {
                pr := workflow.ExecuteActivity(ctx, CreatePullRequest, sandbox, task, repoResult).Get(ctx, nil)
                result.Repositories[i].PullRequest = pr
            }
        }
    }

    result.Status = determineOverallStatus(result.Repositories)
    now := workflow.Now(ctx)
    result.CompletedAt = &now

    return result, nil
}
```

**Activities:**

| Activity | Description |
|----------|-------------|
| `ProvisionSandbox` | Create Docker container or K8s Job |
| `CloneRepository` | Git clone with branch and depth options |
| `RunSetup` | Execute setup commands (go mod download, npm install) |
| `ExecuteAgentic` | Run Claude Code CLI with prompt |
| `ExecuteDeterministic` | Run Docker image transformation |
| `RunVerifiers` | Execute verifier commands as final gate |
| `CreatePullRequest` | Create branch, push, open PR via GitHub API |
| `CleanupSandbox` | Destroy container/Job |

**Signal Handling:**

```go
// Approval signals
workflow.GetSignalChannel(ctx, "approve").Receive(ctx, nil)
workflow.GetSignalChannel(ctx, "reject").Receive(ctx, nil)
workflow.GetSignalChannel(ctx, "cancel").Receive(ctx, nil)

// Steering signal (iterative HITL)
var steerPrompt string
workflow.GetSignalChannel(ctx, "steer").Receive(ctx, &steerPrompt)
```

### Campaign Workflow

The Campaign workflow orchestrates multiple Tasks:

```
┌─────────────────────────────────────────────────────────────────┐
│                     Campaign Workflow                            │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   1. Initialize                                                  │
│         │                                                        │
│         ▼                                                        │
│   2. For each phase (or single phase):                          │
│         │                                                        │
│         ├──▶ For each batch:                                    │
│         │         │                                              │
│         │         ├── Submit Tasks (parallel within batch)      │
│         │         │         │                                    │
│         │         │         ▼                                    │
│         │         │    Monitor Progress                          │
│         │         │         │                                    │
│         │         │         ▼                                    │
│         │         │    Check Failure Threshold                   │
│         │         │         │                                    │
│         │         │         ├── Under threshold: Continue       │
│         │         │         │                                    │
│         │         │         └── Over threshold: Pause            │
│         │         │                   │                          │
│         │         │                   ▼                          │
│         │         │              Await Human Decision            │
│         │         │              (abort/continue/retry)          │
│         │         │                                              │
│         │         └── Collect batch results                     │
│         │                                                        │
│         └──▶ Aggregate phase results                            │
│         │                                                        │
│         ▼                                                        │
│   3. (Two-phase) Filter repos for transform phase               │
│         │                                                        │
│         ▼                                                        │
│   4. Return CampaignResult                                       │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

**Workflow Pseudocode:**

```go
func CampaignWorkflow(ctx workflow.Context, campaign Campaign) (*CampaignResult, error) {
    result := &CampaignResult{
        CampaignID: campaign.ID,
        Status:     "running",
        StartedAt:  workflow.Now(ctx),
    }

    repos := campaign.Repositories
    batchSize := campaign.Batch.Size

    for i := 0; i < len(repos); i += batchSize {
        batch := repos[i:min(i+batchSize, len(repos))]

        // Submit Tasks for batch (as child workflows)
        var futures []workflow.Future
        for _, repo := range batch {
            task := createTaskFromTemplate(campaign.TaskTemplate, repo)
            future := workflow.ExecuteChildWorkflow(ctx, TaskWorkflow, task)
            futures = append(futures, future)
        }

        // Wait for batch completion
        for _, future := range futures {
            var taskResult TaskResult
            future.Get(ctx, &taskResult)
            result.TaskResults = append(result.TaskResults, taskResult)
        }

        // Check failure threshold
        failureRate := calculateFailureRate(result.TaskResults)
        if failureRate > campaign.Failure.ThresholdPercent {
            result.Status = "paused"

            // Wait for human decision
            decision := awaitDecision(ctx)
            switch decision {
            case "abort":
                result.Status = "aborted"
                return result, nil
            case "retry":
                // Re-queue failed Tasks
                i -= batchSize
            case "continue":
                // Continue to next batch
            }
        }
    }

    result.Status = "completed"
    result.Summary = calculateSummary(result.TaskResults)
    now := workflow.Now(ctx)
    result.CompletedAt = &now

    return result, nil
}
```

**Batch Failure Semantics:**

| Scenario | Behavior |
|----------|----------|
| All succeed | Campaign completes, PRs created |
| Some fail (<threshold) | Continue, report failures at end |
| Many fail (≥threshold) | Pause and ask human: abort / continue / retry |
| Critical failure | Halt immediately, notify human |

---

## Report Output Collection

For report mode, the orchestrator must collect the agent's output artifact from the sandbox.

### Collection Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                    Report Output Collection                      │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   1. Agent executes in sandbox                                  │
│         │                                                        │
│         ▼                                                        │
│   2. Agent writes report to well-known path                     │
│      /workspace/REPORT.md                                       │
│         │                                                        │
│         ▼                                                        │
│   3. Orchestrator reads file from sandbox                       │
│      sandbox.CopyFrom("/workspace/REPORT.md")                   │
│         │                                                        │
│         ▼                                                        │
│   4. Parse frontmatter from markdown                            │
│      Split on "---" delimiters, parse YAML                      │
│         │                                                        │
│         ▼                                                        │
│   5. Validate frontmatter against schema (if provided)          │
│         │                                                        │
│         ▼                                                        │
│   6. Store in RepositoryResult.Report                           │
│      {frontmatter: {...}, body: "...", raw: "..."}              │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Prompt Convention

For report mode, the orchestrator appends instructions to the prompt:

```
Write your report to /workspace/REPORT.md with:
- YAML frontmatter between --- delimiters containing structured data
- Markdown body with detailed analysis

Example format:
---
key: value
items:
  - item1
  - item2
---

# Report Title

Your detailed analysis here...
```

### Collection Activity

```go
func CollectReport(ctx context.Context, sandbox Sandbox, schema json.RawMessage) (*ReportOutput, error) {
    // 1. Read report file from sandbox
    content, err := sandbox.CopyFrom(ctx, "/workspace/REPORT.md")
    if err != nil {
        return nil, fmt.Errorf("failed to read report: %w", err)
    }

    // 2. Parse frontmatter
    report, err := ParseFrontmatter(content)
    if err != nil {
        return nil, fmt.Errorf("failed to parse frontmatter: %w", err)
    }

    // 3. Validate against schema (if provided)
    if schema != nil {
        if err := ValidateJSON(report.Frontmatter, schema); err != nil {
            return nil, fmt.Errorf("frontmatter validation failed: %w", err)
        }
    }

    return report, nil
}

func ParseFrontmatter(content string) (*ReportOutput, error) {
    // Split content on "---" delimiters
    // First block after opening "---" is YAML frontmatter
    // Rest is markdown body

    parts := strings.SplitN(content, "---", 3)
    if len(parts) < 3 {
        // No frontmatter, treat entire content as body
        return &ReportOutput{
            Frontmatter: nil,
            Body:        content,
            Raw:         content,
        }, nil
    }

    var frontmatter map[string]any
    if err := yaml.Unmarshal([]byte(parts[1]), &frontmatter); err != nil {
        return nil, err
    }

    return &ReportOutput{
        Frontmatter: frontmatter,
        Body:        strings.TrimSpace(parts[2]),
        Raw:         content,
    }, nil
}
```

### Kubernetes Job Collection

For K8s Jobs, the file is read via the exec API before the pod terminates:

```go
func (p *k8sProvider) CopyFrom(ctx context.Context, sandboxID, path string) (string, error) {
    // Get pod name from Job
    pod, err := p.getPodForJob(ctx, sandboxID)
    if err != nil {
        return "", err
    }

    // Exec "cat" to read file content
    result, err := p.Exec(ctx, sandboxID, ExecCommand{
        Command: []string{"cat", path},
    })
    if err != nil {
        return "", err
    }

    return result.Stdout, nil
}
```

### Alternative: Structured Output via Stdout

For simpler cases, the agent can output directly to stdout and the orchestrator captures it:

```yaml
execution:
  agentic:
    prompt: "Analyze and output your report..."
    output:
      capture: stdout  # Alternative to file-based collection
      schema: {...}
```

The default (`capture: file`) writes to `/workspace/REPORT.md`. The `capture: stdout` option captures the agent's final output instead.

---

## Verifiers

Verifiers validate transformations before PR creation. For agentic transforms, they serve two purposes:

1. **Agent guidance**: Appended to the prompt so the agent runs them and iterates
2. **Final gate**: Run by the orchestrator as a hard check before PR

### Prompt Injection Pattern

The orchestrator appends verifier instructions to the prompt:

```
After making changes, verify your work by running these commands:
- build: go build ./...
- test: go test ./...
- lint: golangci-lint run

Fix any errors before completing the task.
```

The agent runs these using its Bash tool, sees output, and iterates until all pass.

### Final Gate Validation

Even after the agent reports success, the orchestrator runs verifiers as a final check:

```go
func RunVerifiers(ctx context.Context, sandbox Sandbox, verifiers []Verifier) (*VerifiersResult, error) {
    result := &VerifiersResult{AllPassed: true}

    for _, v := range verifiers {
        execResult, err := sandbox.Exec(ctx, ExecCommand{Command: v.Command})
        if err != nil || execResult.ExitCode != 0 {
            result.AllPassed = false
        }
        result.Results = append(result.Results, VerifierResult{
            Name:     v.Name,
            Success:  execResult.ExitCode == 0,
            ExitCode: execResult.ExitCode,
            Output:   execResult.Stdout + execResult.Stderr,
        })
    }

    return result, nil
}
```

---

## Failure Handling

### Agentic Failure Modes

| Failure Mode | Detection | Response |
|--------------|-----------|----------|
| **Agent stuck in loop** | Iteration count > `max_iterations` | Terminate, preserve partial work |
| **Token budget exceeded** | Token counter > `max_tokens` | Terminate, report what was accomplished |
| **Verifiers keep failing** | Retry count > `max_verifier_retries` | Stop iteration, ask human for guidance |
| **Agent refuses (safety)** | Specific error patterns | Report refusal reason |
| **Timeout** | Wall clock > `timeout` | Terminate sandbox, report timeout |

### Graceful Degradation

When a transform fails:

1. **Preserve partial work**: Commit changes to a WIP branch
2. **Capture diagnostics**: Save conversation history, verifier output
3. **Enable recovery**: Allow human to:
   - Steer ("try a different approach")
   - Approve partial changes ("good enough")
   - Abort and discard

### Stuck Detection

```go
// If no new file changes in N iterations, consider stuck
if iterationsSinceLastChange > 3 {
    return StuckError("No progress after 3 iterations")
}
```

### Report Mode Failure Handling

Report mode has unique failure scenarios since there's no PR creation or verifier loop:

| Failure Mode | Detection | Response |
|--------------|-----------|----------|
| **Report file missing** | `/workspace/REPORT.md` doesn't exist | Fail task, include agent stdout as diagnostic |
| **Frontmatter parse error** | YAML between `---` delimiters is invalid | Fail task, return raw content for debugging |
| **Schema validation failed** | Frontmatter doesn't match `output.schema` | Fail task, return validation errors and raw content |
| **Empty report** | File exists but is empty or trivial | Warning (not failure), flag for human review |
| **Timeout** | Wall clock > `timeout` | Terminate sandbox, return partial output if available |

**Recovery Flow:**

```go
func CollectReport(ctx context.Context, sandbox Sandbox, schema json.RawMessage) (*ReportOutput, error) {
    // 1. Attempt to read report file
    content, err := sandbox.CopyFrom(ctx, "/workspace/REPORT.md")
    if err != nil {
        // File missing - capture agent stdout as fallback diagnostic
        return &ReportOutput{
            Raw:   "",
            Error: fmt.Sprintf("Report file not found: %v", err),
        }, ErrReportMissing
    }

    // 2. Parse frontmatter
    report, err := ParseFrontmatter(content)
    if err != nil {
        // Return raw content so human can see what agent produced
        return &ReportOutput{
            Raw:   content,
            Error: fmt.Sprintf("Frontmatter parse error: %v", err),
        }, ErrFrontmatterInvalid
    }

    // 3. Validate against schema (if provided)
    if schema != nil {
        if validationErrs := ValidateJSON(report.Frontmatter, schema); len(validationErrs) > 0 {
            report.ValidationErrors = validationErrs
            return report, ErrSchemaValidation
        }
    }

    return report, nil
}
```

**Error Types:**

```go
var (
    ErrReportMissing      = errors.New("report file not found")
    ErrFrontmatterInvalid = errors.New("frontmatter parse failed")
    ErrSchemaValidation   = errors.New("frontmatter schema validation failed")
)

// ReportOutput extended for error cases
type ReportOutput struct {
    Frontmatter      map[string]any `json:"frontmatter,omitempty"`
    Body             string         `json:"body,omitempty"`
    Raw              string         `json:"raw"`
    Error            string         `json:"error,omitempty"`
    ValidationErrors []string       `json:"validation_errors,omitempty"`
}
```

**Campaign Behavior:**

When a report-mode task fails within a Campaign:
- The failure counts toward the Campaign's failure threshold
- The raw output (if any) is preserved in the result for debugging
- Human can decide to: skip the repository, retry with steering prompt, or abort

---

## Rate Limiting & Cost Control

### GitHub API Limits

| Limit | Value | Mitigation |
|-------|-------|------------|
| REST API | 5,000 req/hr per token | Queue requests, use conditional requests |
| Git operations | Varies by plan | Batch clones, use shallow clones |
| PR creation | No hard limit | Self-imposed limit per Campaign |

### Claude API Limits

| Concern | Mitigation |
|---------|------------|
| Tokens per minute | Configurable delay between transforms |
| Cost per transform | Token budget per Task (`max_tokens`) |
| Runaway costs | Campaign-level budget with hard stop |

### Configuration

```yaml
rate_limits:
  github:
    requests_per_hour: 4000      # Leave headroom
    parallel_clones: 5           # Max concurrent git operations
  claude:
    tokens_per_minute: 100000    # Anthropic tier limit
    max_cost_per_task: 5.00      # USD, terminate if exceeded
    max_cost_per_campaign: 500.00 # USD, pause Campaign if exceeded
```

### Cost Attribution

- Track token usage per Task
- Attribute to team/namespace for chargeback
- Surface in observability metrics (`codetransform_cost_usd` gauge)

---

## Credential Handling

### Local (Docker)

Credentials are passed via environment variables:

```bash
export GITHUB_TOKEN=ghp_xxxx
export ANTHROPIC_API_KEY=sk-ant-xxxx

orchestrator run --file task.yaml
```

### Production (Kubernetes)

Credentials are stored as Kubernetes Secrets and referenced in the Task:

```yaml
credentials:
  github:
    secret_ref:
      name: github-credentials
      key: token
  anthropic:
    secret_ref:
      name: claude-credentials
      key: api-key
```

**Credential Flow:**
1. Worker reads secret references from Task
2. Worker mounts secrets into sandbox pod as environment variables
3. Sandbox uses credentials for git operations and API calls
4. Cleanup ensures credentials are not persisted

**Security Considerations:**
- Secrets are namespace-scoped
- Sandbox pods use dedicated ServiceAccount with minimal RBAC
- Audit logging tracks credential access
- Consider [External Secrets Operator](https://external-secrets.io/) for centralized management

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
│   │   ├── task.go          # Task, Campaign types
│   │   └── result.go        # Result types
│   ├── workflow/            # Temporal workflows
│   │   ├── task.go          # Task workflow
│   │   ├── campaign.go      # Campaign workflow
│   │   └── signals.go       # Signal handlers
│   ├── activity/            # Temporal activities
│   │   ├── sandbox.go       # Provision, Cleanup
│   │   ├── transform.go     # Execute transformations
│   │   ├── git.go           # Clone, branch, push
│   │   └── github.go        # PR creation
│   ├── sandbox/             # Sandbox abstraction
│   │   ├── provider.go      # Interface
│   │   ├── docker/          # Docker implementation
│   │   └── kubernetes/      # K8s Jobs implementation
│   └── config/              # Configuration loading
├── config/
│   ├── local.yaml
│   └── production.yaml
├── docker/
│   └── Dockerfile.sandbox
└── docs/
    ├── OVERVIEW.md          # User-facing documentation
    ├── DESIGN.md            # This document
    └── IMPLEMENTATION_PLAN.md
```

---

## Key Architectural Decisions

1. **CLI as primary interface**: Users submit Tasks via CLI or YAML files. The optional K8s operator is a convenience layer, not the primary interface.

2. **Temporal for durability**: Temporal handles retries, timeouts, and human-in-the-loop signals. All state is durable and recoverable.

3. **Jobs over custom controllers**: Plain Kubernetes Jobs are simpler than custom CRDs or operators. The Temporal worker creates Jobs and manages their lifecycle.

4. **Two transform modes**: Deterministic (Docker images) and Agentic (Claude Code) share the same orchestration but have different execution paths.

5. **Agent-agnostic design**: The platform's value is in the orchestration layer. Claude Code is the current agent, but the interface (`exec(prompt) → code changes`) allows for other agents:

   ```
   Platform (durable)          Agent (swappable)
   ─────────────────           ─────────────────
   • Temporal workflows        • Claude Code CLI (today)
   • Human-in-the-loop         • Other agents (future)
   • Multi-repo coordination   • Interface: exec(prompt) → changes
   • PR creation               • Verifiers validate regardless of agent
   • Cost/rate limiting
   • Audit trail
   ```

6. **Campaign as first-class**: Campaigns orchestrate Tasks at scale with batch execution, failure thresholds, and two-phase patterns. This is core functionality, not a future capability.

7. **Report mode integrated**: Report mode (discovery, audits) is a core mode alongside transform mode, not an afterthought.
