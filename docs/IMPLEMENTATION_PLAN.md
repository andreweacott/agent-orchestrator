# Implementation Plan

Incremental implementation phases for the code transformation and discovery platform.

> **Last Updated**: 2026-02-01 (Phase 4 Report Mode complete; Phase 4b added for forEach)
>
> **Note**: Implementation uses Task/Campaign terminology aligned with the design documents.
>
> **Vision**: Managed Turbolift with two execution backends (Docker images for deterministic
> transforms, Claude Code for agentic transforms) and two modes (transform for PRs, report for
> discovery). See DESIGN.md for full architecture and OVERVIEW.md for use cases.

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

---

## Phase 4: Report Mode (Discovery)

**Goal**: Support distributed code analysis and discovery across repositories without creating PRs.

> **Design Rationale**: Report mode enables discovery campaigns—analyzing repositories to collect
> structured data before making changes. This is essential for security audits, dependency
> inventories, and two-phase transform patterns (discover → transform).

### 4.1 Core Report Mode

- [x] Add `mode` field to Task: `transform` (default) or `report`
- [x] Skip PR creation workflow when `mode: report`
- [x] Capture agent stdout as structured output
- [x] Store report in Task result
- [x] Update workflow to branch based on mode

### 4.2 Report Output Collection

- [x] `CollectReport` activity - read `/workspace/REPORT.md` from sandbox
- [x] Parse frontmatter (YAML between `---` delimiters) from markdown
- [x] Store both structured frontmatter and prose body in result
- [x] Append output instructions to prompt for report mode tasks

### 4.3 Frontmatter Schema Validation

- [x] Parse `output.schema` (JSON Schema) from Task spec
- [x] Validate frontmatter against schema
- [x] Report validation errors in result
- [x] Support common types: object, array, string, number, boolean, enum

### 4.4 forEach: Multi-Target Discovery

> **Moved to Phase 4b** - See dedicated phase below for forEach implementation.

### 4.5 CLI Support

- [x] `orchestrator run --mode report --prompt "..."` - run discovery
- [x] `orchestrator reports <workflow-id>` - view collected reports
- [x] `orchestrator reports <workflow-id> --output json` - export reports
- [x] Update `status` command to show reports for report-mode tasks

### Deliverable

```yaml
# discovery-task.yaml
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

      Write your report to /workspace/REPORT.md with:
      1. YAML frontmatter containing structured data (auth_library, score, issues)
      2. Detailed markdown analysis with findings and recommendations

    output:
      schema:  # Validates the frontmatter
        type: object
        properties:
          auth_library: { type: string }
          score: { type: integer }
          issues: { type: array }
```

Agent writes `/workspace/REPORT.md`:
```markdown
---
auth_library: custom
score: 4
issues:
  - severity: high
    location: config/secrets.yaml:12
---

# Authentication Audit: service-b

## Summary
This service uses a custom auth implementation...

## Findings
### 1. Hardcoded Credentials (High)
Found API key in config/secrets.yaml...
```

```bash
# Run discovery
orchestrator run --file auth-audit.yaml

# View full reports (markdown)
orchestrator reports auth-audit-xyz789
# Displays each repository's full report

# Export structured data (frontmatter) for aggregation
orchestrator reports auth-audit-xyz789 --format json > results.json
# [{"repository": "service-a", "frontmatter": {"auth_library": "oauth2", ...}}, ...]
```

---

## Phase 4b: forEach Multi-Target Discovery

**Goal**: Enable iterating over multiple targets within a single repository for fine-grained discovery.

> **Design Rationale**: forEach enables analyzing multiple components within a repo (e.g., each
> microservice in a monorepo, each API endpoint, each config file) with separate reports per target.
> This is useful for detailed audits where a single repo contains many analyzable units.

### 4b.1 Data Model

- [ ] Add `for_each[]` field to Task for defining iteration targets
- [ ] Add `ForEachExecution` struct to track per-target results *(model exists, not yet used)*
- [ ] Add `ForEachResults` to `RepositoryResult` *(field exists, not yet populated)*

### 4b.2 Template Substitution

- [ ] Parse `{{.name}}` and `{{.context}}` variables in prompt
- [ ] Inject target context into each execution
- [ ] Support nested variable paths: `{{.target.path}}`

### 4b.3 Workflow Changes

- [ ] Loop over `for_each` targets within each repository
- [ ] Execute agent once per target with substituted prompt
- [ ] Collect report per target (namespaced as `REPORT-{target.name}.md` or single file)
- [ ] Aggregate `ForEachExecution` results in `RepositoryResult`

### 4b.4 CLI Support

- [ ] Display per-target reports in `orchestrator reports` command
- [ ] Support `--target <name>` filter for viewing specific target reports
- [ ] JSON export includes target-level grouping

### Deliverable

```yaml
# forEach discovery example
version: 1
id: api-endpoint-audit
title: "Audit all API endpoints"
mode: report

repositories:
  - url: https://github.com/org/api-gateway.git

for_each:
  - name: users-api
    context: "The /users endpoint in src/routes/users.go"
  - name: orders-api
    context: "The /orders endpoint in src/routes/orders.go"
  - name: payments-api
    context: "The /payments endpoint in src/routes/payments.go"

execution:
  agentic:
    prompt: |
      Analyze {{.name}}: {{.context}}

      Check for: authentication, rate limiting, input validation.
      Write your findings to /workspace/REPORT.md
```

```bash
# Run forEach discovery
orchestrator run --file api-audit.yaml

# View all target reports
orchestrator reports <workflow-id>
# Repository: api-gateway
#   Target: users-api
#     Frontmatter: {auth: "jwt", rate_limit: true, ...}
#   Target: orders-api
#     Frontmatter: {auth: "jwt", rate_limit: false, ...}
#   Target: payments-api
#     Frontmatter: {auth: "jwt", rate_limit: true, ...}

# Export as JSON for aggregation
orchestrator reports <workflow-id> --output json
```

---

## Phase 5: Kubernetes Jobs Sandbox Provider

**Goal**: Run sandboxes on Kubernetes using Jobs for production workloads.

> **Design Rationale**: Plain Kubernetes Jobs are simpler than custom CRDs and
> sufficient for ephemeral transformation workloads. The Temporal worker creates Jobs,
> execs into them, and deletes them when done.

### 5.1 Kubernetes Sandbox Provider

- [ ] Implement `Provider` interface for K8s using client-go
- [ ] `Provision()` - create Job with pod template
- [ ] `WaitReady()` - wait for pod Running state
- [ ] `Exec()` - exec into pod via K8s API (like kubectl exec)
- [ ] `Cleanup()` - delete Job (or rely on TTL)

### 5.2 Job Specification

- [ ] Generate Job spec from Task settings
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
# config.yaml
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
task-abc123               0/1           2m         2m
```

---

## Phase 6: Campaign Orchestration

**Goal**: Orchestrate multiple Tasks across many repositories with batch execution and failure handling.

> **Design Rationale**: Campaigns are essential for fleet-wide operations. They enable
> batch execution with parallelism, failure thresholds for human escalation, and
> two-phase patterns (discover → transform).

### 6.1 Campaign Data Model

- [ ] Define `Campaign` struct with repository selection and task template
- [ ] Define `CampaignResult` with aggregated statistics
- [ ] YAML schema for campaign.yaml

### 6.2 Campaign Workflow

- [ ] Implement `CampaignWorkflow` as parent workflow
- [ ] Submit Tasks as child workflows
- [ ] Batch execution with configurable size and parallelism
- [ ] Progress monitoring and result aggregation

### 6.3 Failure Handling

- [ ] Track failure rate across Tasks
- [ ] Pause when failure threshold exceeded
- [ ] Signal handlers for human decisions (abort/continue/retry)
- [ ] Re-queue failed Tasks on retry decision

### 6.4 Two-Phase Campaigns

- [ ] Support `phases` configuration with discover and transform phases
- [ ] Filter repositories based on discovery results
- [ ] Pass discovery data to transform phase

### 6.5 CLI Support

- [ ] `orchestrator campaign run --file campaign.yaml`
- [ ] `orchestrator campaign status <id>`
- [ ] `orchestrator campaign abort <id>`
- [ ] `orchestrator campaign continue <id>`
- [ ] `orchestrator campaign retry <id>`

### Deliverable

```yaml
# campaign.yaml
version: 1
id: slog-migration-campaign
title: "Migrate all Go services to slog"

repositories:
  explicit:
    - url: https://github.com/org/service-a.git
    - url: https://github.com/org/service-b.git
    # ... 50 services

task_template:
  mode: transform
  execution:
    agentic:
      prompt: "Migrate from log.Printf to slog..."
      verifiers:
        - name: build
          command: ["go", "build", "./..."]
  timeout: 30m
  require_approval: true
  pull_request:
    branch_prefix: "auto/slog-migration"
    title: "Migrate to structured logging"

batch:
  size: 10
  parallelism: 5

failure:
  threshold_percent: 10
  action: pause
```

```bash
# Run campaign
orchestrator campaign run --file campaign.yaml
# Campaign started: slog-migration-campaign-abc123

# Check progress
orchestrator campaign status slog-migration-campaign-abc123
# Status: Running
# Progress: 23/50 tasks complete
# Failed: 2 (4%)
# PRs created: 21

# Handle pause on threshold
orchestrator campaign continue slog-migration-campaign-abc123
```

---

## Phase 7: Observability

**Goal**: Metrics, logging, and dashboards for operational visibility.

### 7.1 Prometheus Metrics

- [ ] Instrument workflow and activities with prometheus client
- [ ] `codetransform_tasks_total` (counter) - by status, mode, transform_type
- [ ] `codetransform_task_duration_seconds` (histogram) - end-to-end duration
- [ ] `codetransform_sandbox_provision_seconds` (histogram) - sandbox startup time
- [ ] `codetransform_verifier_duration_seconds` (histogram) - by verifier name
- [ ] `codetransform_pr_created_total` (counter) - successful PRs
- [ ] `codetransform_reports_collected_total` (counter) - report mode
- [ ] `codetransform_api_tokens_used` (counter) - Claude API token consumption

### 7.2 Structured Logging

- [ ] Use slog for structured JSON logs
- [ ] Include task_id, workflow_id, repository in all log entries
- [ ] Log lifecycle events: task started, sandbox provisioned, transform complete
- [ ] Separate log streams for worker vs sandbox

### 7.3 Grafana Dashboard

- [ ] Task throughput and success rate
- [ ] Duration percentiles (p50, p95, p99)
- [ ] Active tasks and queue depth
- [ ] Sandbox provisioning latency
- [ ] Error rate by error type
- [ ] Campaign progress visualization

### 7.4 Alerting Rules

- [ ] High task failure rate (>10% over 1h)
- [ ] Task stuck in Running >2x timeout
- [ ] Sandbox provisioning failures
- [ ] Worker pod restarts
- [ ] Campaign paused on threshold

### Deliverable

```yaml
# ServiceMonitor for Prometheus Operator
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: codetransform-worker
spec:
  selector:
    matchLabels:
      app: codetransform-worker
  endpoints:
  - port: metrics
    interval: 30s
```

---

## Phase 8: Security Hardening

**Goal**: Production-grade security, RBAC, and operational resilience.

### 8.1 Network Policies

- [ ] Sandbox egress policy: allow HTTPS to GitHub, package registries, AI APIs
- [ ] Deny all ingress to sandbox pods
- [ ] Worker-to-sandbox communication via K8s exec API only
- [ ] Document required egress destinations for common ecosystems

### 8.2 RBAC

- [ ] Worker ServiceAccount: create Jobs, exec into pods, read secrets
- [ ] Sandbox ServiceAccount: no K8s API access (empty RBAC)
- [ ] Namespace-scoped roles for multi-tenant deployments

### 8.3 Secret Management

- [ ] IRSA for AWS credentials (ECR pull, Secrets Manager)
- [ ] External Secrets Operator integration
- [ ] Secret rotation without pod restart
- [ ] Audit which tasks accessed which secrets

### 8.4 Audit Logging

- [ ] Enable K8s audit logging for sandbox namespace
- [ ] Log all exec operations with task context
- [ ] Integration with SIEM (CloudWatch, Splunk, etc.)

### 8.5 Scaling & Reliability

- [ ] HPA for workers based on Temporal task queue depth
- [ ] Cluster Autoscaler configuration for sandbox node pool
- [ ] Spot instance node group with fallback to on-demand
- [ ] Pod Disruption Budgets for workers
- [ ] Graceful shutdown: drain active tasks before termination

### 8.6 Deployment Artifacts

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

## Phase 9: Advanced Features

**Goal**: Enhanced capabilities for production usage.

### 9.1 Human-in-the-Loop (Basic) - COMPLETE

- [x] Temporal signals for approval *(approve/reject/cancel signals implemented)*
- [x] Slack integration for notifications *(NotifySlack activity)*
- [x] Approval timeout handling *(24-hour timeout with AwaitWithTimeout)*

### 9.2 Human-in-the-Loop (Iterative Steering)

**Goal**: Enable iterative human-agent collaboration instead of binary approve/reject.

> **This is a key differentiator.** Basic HITL (approve/reject) is table stakes. Iterative
> steering—where humans can guide the agent through multiple rounds—makes the platform valuable.

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
- [ ] Diff viewer (integrates with 9.2)

### 9.6 Report Storage Options

- [ ] Inline in result (default, for small reports)
- [ ] S3/GCS backend for large-scale discovery (100+ repos)
- [ ] Config: `report_storage.backend`

---

## Summary

| Phase | Focus | Key Deliverable | Status |
|-------|-------|-----------------|--------|
| 1 | Local MVP | Single-repo agentic task with Docker | ✅ Complete |
| 2 | PR Creation | Multi-repo with GitHub PRs | ✅ Complete |
| 3 | Deterministic | Docker-based transformations | ✅ Complete |
| 4 | Report Mode | Discovery and analysis (no PRs) | ✅ Complete |
| 4b | **forEach Discovery** | Multi-target iteration within repos | ⬜ Not started |
| 5 | **Kubernetes Jobs** | K8s sandbox provider | ⬜ Not started |
| 6 | **Campaign** | Batch orchestration, failure handling | ⬜ Not started |
| 7 | **Observability** | Metrics, logging, dashboards | ⬜ Not started |
| 8 | **Security** | RBAC, NetworkPolicy, secrets, scaling | ⬜ Not started |
| 9 | Advanced | HITL steering, scheduling, cost tracking | 🟡 ~20% (basic HITL) |

Each phase builds on the previous and delivers working functionality.

### Recommended Next Steps

**Parallel Track A (Product Capabilities):**
1. **Phase 4** - Report mode for discovery (enables two-phase campaigns)
2. **Phase 6** - Campaign orchestration (scales to fleet-wide operations)
3. **Phase 9.2** - Iterative HITL steering (key differentiator)

**Parallel Track B (Production Infrastructure):**
1. **Phase 5** - Kubernetes Jobs sandbox provider
2. **Phase 7** - Observability (needed to tune production)
3. **Phase 8** - Security hardening

> **Priority Note**: Report mode (Phase 4) and Campaign orchestration (Phase 6) are core
> platform capabilities. They enable the two-phase discover → transform pattern which is
> valuable for security audits, dependency inventories, and targeted migrations.
> Both can be developed in parallel using the existing Docker sandbox.

### Key Changes from Previous Plan

| Previous | Current | Rationale |
|----------|---------|-----------|
| Phase 4: CRD & Controller | Phase 4: Report Mode | Report mode is core functionality; CRD is optional convenience layer |
| Phase 8: Agent Sandbox | Removed | Plain K8s Jobs are sufficient; Agent Sandbox adds complexity without clear benefit |
| Phase 9.6: Report Mode | Phase 4: Report Mode | Promoted to core phase—discovery is first-class, not an afterthought |
| Campaign as "future" | Phase 6: Campaign | Campaign is core functionality for fleet-wide operations |
