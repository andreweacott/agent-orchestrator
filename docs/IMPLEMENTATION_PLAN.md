# Implementation Plan

Incremental implementation phases for the code transformation platform.

> **Last Updated**: 2026-02-01 (Design Review - Phases Revised)
>
> **Note**: Implementation uses `TransformTask`/`Transform` workflow naming, aligned with the
> generic transformation model in the design document.
>
> **Vision**: Managed Turbolift with two execution backends (Docker images for deterministic
> transforms, Claude Code for agentic transforms). See DESIGN.md for full architecture.

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

## Phase 4: CodeTransform CRD & Controller

**Goal**: Kubernetes-native interface for defining transformations.

> **Design Rationale**: The CRD provides a declarative, GitOps-friendly interface. Users can
> `kubectl apply -f transform.yaml` and the controller handles the rest. This aligns with
> the "managed Turbolift" vision—transformations are resources, not CLI invocations.

### 4.1 CRD Definition

- [ ] Define `CodeTransform` CRD schema (OpenAPI v3)
- [ ] `spec.mode` - execution mode: `transform` (default) or `report`
- [ ] `spec.repositories[]` - target repos with branch and setup commands
- [ ] `spec.forEach[]` - iterate over multiple targets within a repo (for report mode)
- [ ] `spec.transform.image` - deterministic transform (Docker image + args)
- [ ] `spec.transform.agent` - agentic transform (prompt + verifiers)
- [ ] `spec.transform.agent.outputSchema` - JSON schema for report mode output validation
- [ ] `spec.resources` - CPU/memory limits for sandbox
- [ ] `spec.timeout`, `spec.requireApproval` - execution settings
- [ ] `spec.pullRequest` - PR title, branch prefix, labels (transform mode only)
- [ ] `status` subresource - phase, workflowID, repository results, PR URLs or reports

### 4.2 Controller Implementation

- [ ] Scaffold controller using controller-runtime (kubebuilder)
- [ ] Watch `CodeTransform` resources
- [ ] On create: start Temporal workflow, record workflowID in status
- [ ] On update: handle cancellation signals
- [ ] On delete: cancel workflow, cleanup
- [ ] Periodic reconcile: sync status from Temporal to CRD

### 4.3 Temporal Integration

- [ ] `CodeTransformReconciler` creates workflows via Temporal client
- [ ] Query workflow status and map to CRD status phases
- [ ] Handle Temporal workflow completion/failure events

### 4.4 CLI Integration

- [ ] `orchestrator run --file transform.yaml` creates CRD (if in-cluster)
- [ ] `orchestrator run --file transform.yaml --local` bypasses CRD (direct Temporal)
- [ ] `orchestrator get <name>` shows CRD status
- [ ] Support both in-cluster and out-of-cluster operation

### Deliverable

```yaml
apiVersion: codetransform.io/v1alpha1
kind: CodeTransform
metadata:
  name: upgrade-logging
spec:
  repositories:
    - url: https://github.com/org/service-a.git
    - url: https://github.com/org/service-b.git
  transform:
    agent:
      prompt: |
        Migrate from log.Printf to slog package.
      verifiers:
        - name: build
          command: ["go", "build", "./..."]
        - name: test
          command: ["go", "test", "./..."]
  resources:
    limits:
      memory: "4Gi"
      cpu: "2"
  timeout: 30m
  requireApproval: true
  pullRequest:
    branchPrefix: "auto/slog-migration"
    title: "Migrate to structured logging"
```

```bash
# Apply transformation
kubectl apply -f upgrade-logging.yaml

# Check status
kubectl get codetransform upgrade-logging -o yaml

# Or via CLI
orchestrator get upgrade-logging
```

### 4.5 Configuration (Moved from original Phase 4)

- [ ] Load operator config from ConfigMap or file
- [ ] Default sandbox image, resources, namespace
- [ ] Temporal connection settings
- [ ] GitHub/Slack credentials references

---

## Phase 5: Kubernetes Jobs Sandbox Provider

**Goal**: Run sandboxes on Kubernetes using Jobs for production workloads.

> **Design Rationale**: Plain Kubernetes Jobs are simpler than custom CRDs or Agent Sandbox,
> and sufficient for ephemeral transformation workloads. The Temporal worker creates Jobs,
> execs into them, and deletes them when done.

### 5.1 Kubernetes Sandbox Provider

- [ ] Implement `Provider` interface for K8s using client-go
- [ ] `Provision()` - create Job with pod template
- [ ] `WaitReady()` - wait for pod Running state
- [ ] `Exec()` - exec into pod via K8s API (like kubectl exec)
- [ ] `Cleanup()` - delete Job (or rely on TTL)

### 5.2 Job Specification

- [ ] Generate Job spec from `CodeTransform` resource settings
- [ ] Configure resources (CPU, memory) from spec
- [ ] Set `runtimeClassName` for gVisor if specified
- [ ] Apply node selectors and tolerations
- [ ] Mount secrets for GitHub token, API keys
- [ ] Set `ttlSecondsAfterFinished` for automatic cleanup

### 5.3 Provider Selection

- [ ] Factory function based on `SANDBOX_PROVIDER` env var
- [ ] Auto-detect: Docker socket → Docker, ServiceAccount → Kubernetes
- [ ] Fallback chain: Kubernetes → Docker → error

### 5.4 Namespace and Multi-tenancy

- [ ] Configurable sandbox namespace (default: `sandbox-isolated`)
- [ ] Support namespace-per-team for isolation
- [ ] ResourceQuota enforcement per namespace

### 5.5 Local K8s Testing

- [ ] kind cluster setup script
- [ ] Integration tests against real cluster
- [ ] CI pipeline with kind

### Deliverable

```yaml
# Operator config
sandbox:
  provider: kubernetes
  namespace: sandbox-isolated
  image: your-org/claude-sandbox:latest
  serviceAccount: sandbox-runner
  nodeSelector:
    workload-type: sandbox
  runtimeClassName: gvisor  # optional
  resources:
    defaultLimits:
      memory: "4Gi"
      cpu: "2"
```

```bash
# Worker creates this Job:
kubectl get jobs -n sandbox-isolated
NAME                      COMPLETIONS   DURATION   AGE
transform-task-abc123     0/1           2m         2m

# And execs into the running pod
kubectl exec -n sandbox-isolated transform-task-abc123-xyz -- git clone ...
```

---

## Phase 6: Observability

**Goal**: Metrics, logging, and dashboards for operational visibility.

> **Design Rationale**: Observability comes before security hardening because you need
> visibility to tune and debug the system. Without metrics, you're flying blind.

### 6.1 Prometheus Metrics

- [ ] Instrument controller and worker with prometheus client
- [ ] `codetransform_tasks_total` (counter) - by status, transform_type
- [ ] `codetransform_task_duration_seconds` (histogram) - end-to-end duration
- [ ] `codetransform_sandbox_provision_seconds` (histogram) - sandbox startup time
- [ ] `codetransform_verifier_duration_seconds` (histogram) - by verifier name
- [ ] `codetransform_pr_created_total` (counter) - successful PRs
- [ ] `codetransform_api_tokens_used` (counter) - Claude API token consumption

### 6.2 Structured Logging

- [ ] Use slog or zap for structured JSON logs
- [ ] Include task_id, workflow_id, repository in all log entries
- [ ] Log lifecycle events: task started, sandbox provisioned, transform complete, PR created
- [ ] Separate log streams for controller vs worker vs sandbox

### 6.3 Grafana Dashboard

- [ ] Task throughput and success rate
- [ ] Duration percentiles (p50, p95, p99)
- [ ] Active tasks and queue depth
- [ ] Sandbox provisioning latency
- [ ] Error rate by error type

### 6.4 Alerting Rules

- [ ] High task failure rate (>10% over 1h)
- [ ] Task stuck in Running >2x timeout
- [ ] Sandbox provisioning failures
- [ ] Worker pod restarts

### Deliverable

```yaml
# ServiceMonitor for Prometheus Operator
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: codetransform-controller
spec:
  selector:
    matchLabels:
      app: codetransform-controller
  endpoints:
  - port: metrics
    interval: 30s
```

---

## Phase 7: Security Hardening

**Goal**: Production-grade security, RBAC, and operational resilience.

### 7.1 Network Policies

- [ ] Sandbox egress policy: allow HTTPS to GitHub, package registries, AI APIs
- [ ] Deny all ingress to sandbox pods
- [ ] Worker-to-sandbox communication via K8s exec API only
- [ ] Document required egress destinations for common ecosystems (npm, PyPI, Maven)

### 7.2 RBAC

- [ ] Controller ServiceAccount: create/delete Jobs, update CRD status
- [ ] Worker ServiceAccount: create Jobs, exec into pods, read secrets
- [ ] Sandbox ServiceAccount: no K8s API access (empty RBAC)
- [ ] Namespace-scoped roles for multi-tenant deployments

### 7.3 Secret Management

- [ ] IRSA for AWS credentials (ECR pull, Secrets Manager)
- [ ] External Secrets Operator integration for GitHub tokens, API keys
- [ ] Secret rotation without pod restart
- [ ] Audit which tasks accessed which secrets

### 7.4 Audit Logging

- [ ] Enable K8s audit logging for sandbox namespace
- [ ] Log all exec operations with task context
- [ ] Integration with SIEM (CloudWatch, Splunk, etc.)

### 7.5 Scaling & Reliability

- [ ] HPA for workers based on Temporal task queue depth
- [ ] Cluster Autoscaler configuration for sandbox node pool
- [ ] Spot instance node group with fallback to on-demand
- [ ] Pod Disruption Budgets for controller and workers
- [ ] Graceful shutdown: drain active tasks before termination

### 7.6 Deployment Artifacts

- [ ] Helm chart with configurable values
- [ ] Terraform module for EKS cluster setup
- [ ] Kustomize overlays for dev/staging/prod
- [ ] Runbook: common failure modes and remediation

### Deliverable

```bash
# Deploy with Helm
helm install codetransform ./charts/codetransform \
  --namespace codetransform-system \
  --set temporal.address=temporal.internal:7233 \
  --set sandbox.namespace=sandbox-isolated \
  --set sandbox.runtimeClassName=gvisor
```

---

## Phase 8: Agent Sandbox Integration (Optional)

**Goal**: Use kubernetes-sigs/agent-sandbox for warm pools and faster provisioning.

> **Design Rationale**: This phase is OPTIONAL. Plain Kubernetes Jobs (Phase 5) are sufficient
> for most workloads. Only implement this if you need sub-second sandbox provisioning or
> want to leverage the Agent Sandbox ecosystem.

### 8.1 Agent Sandbox Provider

- [ ] Implement provider using SandboxClaim CRD
- [ ] Reference SandboxTemplate by name from CodeTransform spec
- [ ] Acquire sandbox from SandboxWarmPool instead of creating Job

### 8.2 Sandbox Templates

- [ ] Define SandboxTemplate CRDs for common stacks (Go, Node, Python, Java)
- [ ] Configure gVisor runtime class
- [ ] Set resource limits matching CodeTransform defaults

### 8.3 Warm Pools

- [ ] Configure SandboxWarmPool CRD with min/max sizes
- [ ] Metrics for pool utilization and wait time
- [ ] Auto-tune pool size based on demand patterns

### 8.4 Fallback to Jobs

- [ ] If warm pool exhausted, fall back to creating a Job
- [ ] Configurable: prefer warm pool vs always use Jobs

### Deliverable

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

---

## Phase 9: Advanced Features

**Goal**: Enhanced capabilities based on usage patterns.

### 9.1 Human-in-the-Loop (Basic) - COMPLETE

- [x] Temporal signals for approval *(approve/reject/cancel signals implemented)*
- [x] Slack integration for notifications *(NotifySlack activity)*
- [x] Approval timeout handling *(24-hour timeout with AwaitWithTimeout)*

### 9.2 Human-in-the-Loop (Iterative Steering)

**Goal**: Enable rich, iterative human-agent collaboration instead of binary approve/reject.

> **This is the key differentiator.** Basic HITL (approve/reject) is table stakes. Iterative
> steering—where humans can guide the agent through multiple rounds—is what makes an agentic
> platform valuable. Prioritize this over Agent Sandbox integration.

#### 9.2.1 View Changes

- [ ] `GetDiff` activity - return full git diffs for all modified files
- [ ] `GetVerifierOutput` activity - return detailed test/build output
- [ ] CLI: `orchestrator diff --workflow-id <id>` - view changes in terminal
- [ ] CLI: `orchestrator logs --workflow-id <id>` - view verifier output
- [ ] Slack: Include diff snippets or link to full diff viewer

#### 9.2.2 Steering Prompts

- [ ] Add `steer` signal with prompt payload to workflow
- [ ] Workflow loops back to Claude Code with steering prompt
- [ ] Preserve conversation context across steering iterations
- [ ] CLI: `orchestrator steer --workflow-id <id> --prompt "try X instead"`
- [ ] Track iteration count and history

#### 9.2.3 Partial Approval

- [ ] Allow approving specific files while requesting changes to others
- [ ] `orchestrator approve --workflow-id <id> --files "src/main.go,src/util.go"`
- [ ] `orchestrator steer --workflow-id <id> --files "src/test.go" --prompt "add edge case tests"`

#### 9.2.4 Interactive Review UI

- [ ] Web UI for reviewing diffs (syntax highlighted)
- [ ] Side-by-side diff view
- [ ] Inline commenting on changes
- [ ] One-click approve/reject/steer buttons
- [ ] View agent's reasoning and conversation history

#### 9.2.5 Interaction Flow

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

### 9.3 Scheduled Tasks

- [ ] Temporal schedules for recurring tasks
- [ ] Cron-like syntax support
- [ ] Example: weekly dependency update transforms

### 9.4 Cost Tracking

- [ ] Track API token usage per task (Claude API)
- [ ] Compute cost attribution (sandbox CPU/memory hours)
- [ ] Per-team/namespace cost rollup
- [ ] Budget alerts and quotas

### 9.5 Web UI (Optional)

- [ ] Task submission form
- [ ] Status dashboard
- [ ] Result viewing
- [ ] Diff viewer (integrates with 9.2.4)

### 9.6 Report Mode (Discovery)

**Goal**: Support distributed code analysis and discovery across many repositories without creating PRs.

> **Use Case**: Before running a migration campaign, discover which repositories are affected and
> collect structured data to inform the transformation strategy. Also useful for security audits,
> dependency inventories, and compliance assessments.

#### 9.6.1 Core Report Mode

- [ ] Add `mode` field to CRD: `transform` (default) or `report`
- [ ] Skip PR creation workflow when `mode: report`
- [ ] Capture agent stdout as structured output
- [ ] Validate output against `outputSchema` if provided
- [ ] Store report in CRD status (for small reports)

#### 9.6.2 Output Schema Validation

- [ ] Parse `spec.transform.agent.outputSchema` (JSON Schema)
- [ ] Validate agent output against schema
- [ ] Report validation errors in status
- [ ] Support common types: object, array, string, number, boolean, enum

#### 9.6.3 forEach: Multi-Target Discovery

- [ ] Add `spec.forEach[]` field for iterating within a repo
- [ ] Each target gets its own sandbox execution
- [ ] Template variable substitution in prompt: `{{.context}}`, `{{.name}}`
- [ ] Aggregate reports by target in status

#### 9.6.4 Report Storage

- [ ] Inline in CRD status (default, for small reports)
- [ ] ConfigMap storage for medium reports
- [ ] S3/GCS backend for large-scale discovery (100+ repos)
- [ ] Operator config: `reportStorage.backend`

#### 9.6.5 CLI Support

- [ ] `orchestrator run --mode report --prompt "..."` - run discovery
- [ ] `orchestrator reports <name>` - view collected reports
- [ ] `orchestrator reports <name> --output json` - export reports
- [ ] `orchestrator reports <name> --aggregate` - show summary statistics

#### 9.6.6 Campaign Integration

- [ ] Discovery Campaign type (mode: report across many repos)
- [ ] Report aggregation at Campaign level
- [ ] Two-phase workflow: discover → review → transform
- [ ] Pass discovery output to transform phase

### Deliverable (Report Mode)

```yaml
# Discovery task
apiVersion: codetransform.io/v1alpha1
kind: CodeTransform
metadata:
  name: auth-audit
spec:
  mode: report
  repositories:
    - url: https://github.com/org/service-a.git
    - url: https://github.com/org/service-b.git
  transform:
    agent:
      prompt: |
        Analyze authentication patterns. Output JSON with:
        - auth_library: string
        - has_mfa: boolean
        - issues: array of {severity, description}
      outputSchema:
        type: object
        properties:
          auth_library: { type: string }
          has_mfa: { type: boolean }
          issues: { type: array }
  timeout: 15m
```

```bash
# Run discovery
orchestrator run --file auth-audit.yaml

# View reports
orchestrator reports auth-audit
# service-a: {"auth_library": "oauth2", "has_mfa": true, "issues": []}
# service-b: {"auth_library": "custom", "has_mfa": false, "issues": [...]}

# Export for further analysis
orchestrator reports auth-audit --output json > audit-results.json
```

---

## Summary

| Phase | Focus | Key Deliverable | Status |
|-------|-------|-----------------|--------|
| 1 | Local MVP | Single-repo agentic task with Docker | ✅ Complete |
| 2 | PR Creation | Multi-repo with GitHub PRs | ✅ Complete |
| 3 | Deterministic | Docker-based transformations | ✅ Complete |
| 4 | **CRD & Controller** | `CodeTransform` CRD, K8s-native interface | ⬜ Not started |
| 5 | **Kubernetes Jobs** | K8s sandbox provider using Jobs | ⬜ Not started |
| 6 | **Observability** | Metrics, logging, dashboards | ⬜ Not started |
| 7 | **Security** | RBAC, NetworkPolicy, secrets, scaling | ⬜ Not started |
| 8 | Agent Sandbox | Warm pools (OPTIONAL) | ⬜ Not started |
| 9 | Advanced | HITL steering, scheduling, cost tracking, **report mode** | 🟡 ~20% (basic HITL only) |

Each phase builds on the previous and delivers working functionality.

### Recommended Next Steps

**Parallel Track A (Infrastructure):**
1. **Phase 4** - CodeTransform CRD and controller (K8s-native interface)
2. **Phase 5** - Kubernetes Jobs sandbox provider
3. **Phase 6** - Observability (needed to tune production)

**Parallel Track B (Product Differentiation):**
1. **Phase 9.2** - Iterative HITL steering (key differentiator - can start immediately)
2. **Phase 9.6** - Report mode for distributed discovery (enables two-phase campaigns)

> **Priority Note**: Iterative HITL steering (Phase 9.2) is the most valuable feature for
> agentic mode usability. Report mode (Phase 9.6) enables discovery campaigns which are
> valuable for pre-migration analysis and security audits. Both have no dependencies on
> Phases 4-7 and can be developed in parallel using the existing Docker sandbox.

### Key Changes from Original Plan

| Original | Revised | Rationale |
|----------|---------|-----------|
| Phase 4: Config & Profiles | Phase 4: CRD & Controller | CRD is the primary interface; config folded in |
| Phase 5: K8s Provider (abstract) | Phase 5: K8s Jobs (concrete) | Jobs are simpler and sufficient |
| Phase 6: Agent Sandbox | Phase 8: Agent Sandbox (optional) | Defer unless warm pools needed |
| Phase 7: Prod Hardening (mixed) | Phase 6: Observability, Phase 7: Security | Split for clarity; observability first |
| Phase 8: Advanced | Phase 9: Advanced | Iterative steering emphasized as key differentiator |
