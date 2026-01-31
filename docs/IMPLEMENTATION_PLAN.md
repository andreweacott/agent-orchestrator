# Implementation Plan

Incremental implementation phases for the code transformation platform.

> **Last Updated**: 2026-01-31 (Phase 3 Complete)
>
> **Note**: Implementation uses `TransformTask`/`Transform` workflow naming, aligned with the
> generic transformation model in the design document.

---

## Phase 1: Local MVP

**Goal**: Single-repo agentic transformation running locally with Docker.

### 1.1 Project Setup

- [x] Initialize Go module
- [x] Set up directory structure
- [x] Add Makefile with common targets
- [x] Create Dockerfile.sandbox (base image with git, Claude Code CLI)

### 1.2 Data Model

- [x] Define `Task` struct *(implemented as `TransformTask`)*
- [x] Define `RepositoryTarget` struct *(implemented as `Repository` with Setup field)*
- [x] Define `Transformation` (Agentic only for now) *(prompt embedded in TransformTask)*
- [x] Define `Verifier` struct *(with VerifierResult, VerifiersResult)*
- [x] Define `TaskResult` and `RepositoryResult` *(implemented as `TransformResult`, `ClaudeCodeResult`)*

### 1.3 Docker Sandbox Provider

- [x] Implement `Provider` interface *(implemented as `docker.Client`)*
- [x] `Provision()` - create container with mounted workspace
- [x] `Exec()` - run commands in container
- [x] `Cleanup()` - stop and remove container
- [x] Unit tests with mock Docker client *(integration tests in client_test.go)*

### 1.4 Temporal Workflow (Basic)

- [x] Set up local Temporal server (docker-compose) *(docker-compose.yaml with Postgres)*
- [x] Implement `ExecuteTask` workflow *(implemented as `Transform` workflow)*
- [x] Implement activities:
  - [x] `ProvisionSandbox`
  - [x] `CloneRepository` *(implemented as `CloneRepositories` with setup)*
  - [x] `RunSetup` *(integrated into CloneRepositories)*
  - [x] `ExecuteAgentic` (run Claude Code) *(implemented as `RunClaudeCode`)*
  - [x] `CleanupSandbox`
  - [x] `RunVerifiers` *(new activity)*
- [x] Workflow tests with Temporal test framework *(basic tests in bugfix_test.go)*

### 1.5 Claude Code Integration

- [x] Build prompt from `AgenticTransform` *(buildPrompt function)*
- [x] Append verifier instructions to prompt
- [x] Execute Claude Code CLI in sandbox
- [x] Capture output
- [x] Run verifiers as final gate *(RunVerifiers activity called before PR creation)*

### 1.6 CLI (Minimal)

- [x] `orchestrator run --repo <url> --prompt <prompt>` *(both `start` and `run` commands)*
- [x] `orchestrator run --file task.yaml` *(--file flag with YAML parsing)*
- [x] `orchestrator status <task-id>` *(implemented as `status --workflow-id`)*
- [x] YAML task file parsing *(TaskFile struct with loadTaskFile function)*

### Deliverable

Run an agentic task locally:
```bash
orchestrator run \
  --repo https://github.com/example/test-repo.git \
  --prompt "Add input validation" \
  --verifier "build:go build ./..." \
  --verifier "test:go test ./..."
```

---

## Phase 2: PR Creation & Multi-Repo

**Goal**: Create pull requests, support multiple repositories per task.

### 2.1 GitHub Integration

- [x] GitHub client setup (go-github or gh CLI) *(uses gh CLI via shell)*
- [x] `CreatePullRequest` activity
  - [x] Create branch
  - [x] Push changes
  - [x] Open PR with title/body
- [x] Handle authentication (token from env)

### 2.2 Multi-Repository Support

- [x] Update workflow to iterate over repositories
- [x] Parallel vs sequential execution option *(--parallel flag, Parallel field in task)*
- [x] Aggregate results across repos

### 2.3 Task Result Improvements

- [x] Track files modified *(in ClaudeCodeResult.FilesModified)*
- [x] Include PR URLs in result *(in TransformResult.PullRequests)*
- [x] Better error reporting

### 2.4 CLI Enhancements

- [x] `--repo` flag accepts multiple values *(comma-separated in `--repos`)*
- [x] Output formatting (JSON, table) *(`--output` flag with json/table options)*
- [x] `orchestrator list` command

### Deliverable

```bash
orchestrator run \
  --repo https://github.com/org/service-a.git \
  --repo https://github.com/org/service-b.git \
  --prompt "Update API v1 to v2" \
  --verifier "test:npm test"
# Creates PRs in both repos
```

---

## Phase 3: Deterministic Transformations

**Goal**: Support Docker-based deterministic transformations.

### 3.1 Deterministic Transform Execution

- [x] `ExecuteDeterministic` activity *(new `internal/activity/deterministic.go`)*
- [x] Pull transformation image *(via docker run in sandbox)*
- [x] Mount workspace into transformation container *(bind mount at /workspace)*
- [x] Run transformation
- [x] Capture output/errors
- [x] Detect modified files via `git status --porcelain`

### 3.2 CLI Support

- [x] `--image` flag for deterministic mode
- [x] `--args` flag for transformation arguments
- [x] `--env` flag for environment variables
- [x] YAML task file support (`mode`, `image`, `args`, `env` fields)

### 3.3 Validation

- [x] Run verifiers after deterministic transform
- [x] Skip PR if no changes detected *(returns success with empty PR list)*
- [x] Skip human approval for deterministic mode *(pre-vetted transforms)*

### 3.4 Data Model

- [x] `TransformMode` type (`agentic`, `deterministic`)
- [x] `DeterministicResult` struct
- [x] New `TransformTask` fields: `TransformMode`, `TransformImage`, `TransformArgs`, `TransformEnv`

### Deliverable

```bash
orchestrator run \
  --repo https://github.com/org/service.git \
  --image openrewrite/rewrite:latest \
  --args "rewrite:run" \
  --args "-Drewrite.activeRecipes=org.openrewrite.java.logging.log4j.Log4j1ToLog4j2" \
  --verifier "build:mvn compile"
```

Or via YAML:
```yaml
id: openrewrite-migration
title: Migrate Log4j 1.x to 2.x
mode: deterministic
image: openrewrite/rewrite:latest
args:
  - "rewrite:run"
  - "-Drewrite.activeRecipes=org.openrewrite.java.logging.log4j.Log4j1ToLog4j2"
repositories:
  - url: https://github.com/org/service.git
verifiers:
  - name: build
    command: ["mvn", "compile"]
```

---

## Phase 4: Configuration & Profiles

**Goal**: Externalize configuration, support sandbox profiles.

### 4.1 Configuration Loading

- [ ] Load config from YAML file
- [ ] Environment variable overrides
- [ ] Config struct with defaults

### 4.2 Sandbox Profiles

- [ ] Define `SandboxProfile` struct
- [ ] Load profiles from config
- [ ] Select profile per task or use default
- [ ] Profile specifies: image, resources, timeout

### 4.3 Default Verifiers

- [ ] Allow profiles to define default verifiers
- [ ] Task verifiers override/extend profile defaults

### Deliverable

```yaml
# config.yaml
profiles:
  go:
    image: claude-sandbox-go:1.22
    resources:
      memoryMB: 4096
    defaultVerifiers:
      - name: build
        command: ["go", "build", "./..."]
      - name: test
        command: ["go", "test", "./..."]
```

---

## Phase 5: Kubernetes Provider

**Goal**: Run sandboxes on Kubernetes for production.

### 5.1 Kubernetes Sandbox Provider

- [ ] Implement `Provider` interface for K8s
- [ ] `Provision()` - create Pod
- [ ] `Exec()` - kubectl exec via API
- [ ] `Cleanup()` - delete Pod
- [ ] Wait for pod ready with timeout

### 5.2 Provider Selection

- [ ] Factory function based on config/env
- [ ] Auto-detect environment (Docker socket vs K8s service account)

### 5.3 K8s-Specific Options

- [ ] Namespace configuration
- [ ] Node selectors
- [ ] Resource limits from profile
- [ ] Service account for sandbox pods

### 5.4 Local K8s Testing

- [ ] kind/minikube setup instructions
- [ ] Integration tests with real cluster

### Deliverable

```yaml
# config.yaml
sandbox:
  provider: kubernetes
  namespace: orchestrator-sandboxes
  nodeSelector:
    workload-type: sandbox
```

---

## Phase 6: Agent Sandbox Integration

**Goal**: Use kubernetes-sigs/agent-sandbox for production isolation.

### 6.1 Agent Sandbox Provider

- [ ] Implement provider using SandboxClaim CRD
- [ ] Reference SandboxTemplate by name
- [ ] Use SandboxWarmPool for fast start

### 6.2 Sandbox Templates

- [ ] Define SandboxTemplate CRDs for each profile
- [ ] gVisor runtime class configuration
- [ ] Resource limits and timeouts

### 6.3 Warm Pools

- [ ] Configure SandboxWarmPool CRD
- [ ] Monitor pool utilization
- [ ] Tune pool size based on demand

### Deliverable

```yaml
# SandboxTemplate
apiVersion: sandbox.k8s.io/v1
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
```

---

## Phase 7: Production Hardening

**Goal**: Security, observability, and operational readiness.

### 7.1 Security

- [ ] Network policies for sandbox pods
- [ ] RBAC for workers and sandboxes
- [ ] Secret management (IRSA, external-secrets)
- [ ] Audit logging

### 7.2 Observability

- [ ] Prometheus metrics
  - [ ] Task duration
  - [ ] Success/failure rates
  - [ ] Sandbox provisioning time
- [ ] Structured logging
- [ ] Grafana dashboard

### 7.3 Scaling

- [ ] HPA for workers based on queue depth
- [ ] Cluster autoscaler for sandbox nodes
- [ ] Spot instance configuration

### 7.4 Reliability

- [ ] Pod disruption budgets
- [ ] Graceful shutdown handling
- [ ] Retry policies tuning

### Deliverable

Production-ready deployment with:
- Helm chart
- Terraform for EKS
- Runbook documentation

---

## Phase 8: Advanced Features

**Goal**: Enhanced capabilities based on usage patterns.

### 8.1 Human-in-the-Loop (Basic)

- [x] Temporal signals for approval *(approve/reject/cancel signals implemented)*
- [x] Slack integration for notifications *(NotifySlack activity)*
- [x] Approval timeout handling *(24-hour timeout with AwaitWithTimeout)*

### 8.2 Human-in-the-Loop (Iterative Steering)

**Goal**: Enable rich, iterative human-agent collaboration instead of binary approve/reject.

#### 8.2.1 View Changes

- [ ] `GetDiff` activity - return full git diffs for all modified files
- [ ] `GetVerifierOutput` activity - return detailed test/build output
- [ ] CLI: `orchestrator diff --workflow-id <id>` - view changes in terminal
- [ ] CLI: `orchestrator logs --workflow-id <id>` - view verifier output
- [ ] Slack: Include diff snippets or link to full diff viewer

#### 8.2.2 Steering Prompts

- [ ] Add `steer` signal with prompt payload to workflow
- [ ] Workflow loops back to Claude Code with steering prompt
- [ ] Preserve conversation context across steering iterations
- [ ] CLI: `orchestrator steer --workflow-id <id> --prompt "try X instead"`
- [ ] Track iteration count and history

#### 8.2.3 Partial Approval

- [ ] Allow approving specific files while requesting changes to others
- [ ] `orchestrator approve --workflow-id <id> --files "src/main.go,src/util.go"`
- [ ] `orchestrator steer --workflow-id <id> --files "src/test.go" --prompt "add edge case tests"`

#### 8.2.4 Interactive Review UI

- [ ] Web UI for reviewing diffs (syntax highlighted)
- [ ] Side-by-side diff view
- [ ] Inline commenting on changes
- [ ] One-click approve/reject/steer buttons
- [ ] View agent's reasoning and conversation history

#### Interaction Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                    Iterative HITL Flow                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│   Agent works on task                                           │
│         │                                                       │
│         ▼                                                       │
│   Workflow pauses ──────────────────────────────────────────┐   │
│         │                                                   │   │
│         ▼                                                   │   │
│   Human reviews:                                            │   │
│   • View diffs (orchestrator diff)                          │   │
│   • View test output (orchestrator logs)                    │   │
│   • View agent reasoning                                    │   │
│         │                                                   │   │
│         ▼                                                   │   │
│   Human decides:                                            │   │
│         │                                                   │   │
│         ├── Approve ──────────► Create PR ──► Done          │   │
│         │                                                   │   │
│         ├── Reject ───────────► Cleanup ───► Done           │   │
│         │                                                   │   │
│         └── Steer("try X") ───► Agent continues ────────────┘   │
│                                 (with new prompt)               │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### 8.3 Scheduled Tasks

- [ ] Temporal schedules for recurring tasks
- [ ] Cron-like syntax support

### 8.4 Cost Tracking

- [ ] Track API token usage per task
- [ ] Compute cost attribution
- [ ] Budget alerts

### 8.5 Web UI (Optional)

- [ ] Task submission form
- [ ] Status dashboard
- [ ] Result viewing
- [ ] Diff viewer (integrates with 8.2.4)

---

## Summary

| Phase | Focus | Key Deliverable | Status |
|-------|-------|-----------------|--------|
| 1 | Local MVP | Single-repo agentic task with Docker | ✅ Complete |
| 2 | PR Creation | Multi-repo with GitHub PRs | ✅ Complete |
| 3 | Deterministic | Docker-based transformations | ✅ Complete |
| 4 | Configuration | Profiles and external config | ⬜ Not started |
| 5 | Kubernetes | K8s sandbox provider | ⬜ Not started |
| 6 | Agent Sandbox | Production isolation with warm pools | ⬜ Not started |
| 7 | Production | Security, observability, scaling | ⬜ Not started |
| 8 | Advanced | HITL (basic + iterative), scheduling, cost tracking | 🟡 ~20% (basic HITL only) |

Each phase builds on the previous and delivers working functionality.

### Recommended Next Steps

1. **Phase 4** - Add configuration file and sandbox profiles
2. **Phase 8.2** - Iterative HITL steering (high value for usability)
3. **Phase 5** - Kubernetes sandbox provider for production
