# Implementation Plan

Incremental implementation phases for the code transformation and discovery platform.

> **Last Updated**: 2026-02-02 (Phase 9.2 HITL Iterative Steering complete)
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

- [x] `fleetlift run --repo <url> --prompt <prompt>` *(both `start` and `run` commands)*
- [x] `fleetlift run --file task.yaml` *(--file flag with YAML parsing)*
- [x] `fleetlift status <task-id>` *(implemented as `status --workflow-id`)*
- [x] YAML task file parsing *(TaskFile struct with loadTaskFile function)*

### Deliverable

Run an agentic task locally:
```bash
fleetlift run \
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
- [x] Flexible execution patterns via groups *(unified groups-based model, max_parallel field)*
- [x] Aggregate results across repos

### 2.3 Task Result Improvements

- [x] Track files modified *(in ClaudeCodeResult.FilesModified)*
- [x] Include PR URLs in result *(in TransformResult.PullRequests)*
- [x] Better error reporting

### 2.4 CLI Enhancements

- [x] `--repo` flag accepts multiple values *(comma-separated in `--repos`)*
- [x] Output formatting (JSON, table) *(`--output` flag with json/table options)*
- [x] `fleetlift list` command

### Deliverable

```bash
fleetlift run \
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
fleetlift run \
  --repo https://github.com/org/service.git \
  --image openrewrite/rewrite:latest \
  --args "rewrite:run" \
  --args "-Drewrite.activeRecipes=org.openrewrite.java.logging.log4j.Log4j1ToLog4j2" \
  --verifier "build:mvn compile"
```

---

## Phase 4: Report Mode (Discovery)

**Goal**: Support distributed code analysis and discovery across repositories without creating PRs.

> **Design Rationale**: Report mode enables discovery—analyzing repositories to collect
> structured data. This is essential for security audits, dependency inventories, and
> pre-migration analysis.

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

- [x] `fleetlift run --mode report --prompt "..."` - run discovery
- [x] `fleetlift reports <workflow-id>` - view collected reports
- [x] `fleetlift reports <workflow-id> --output json` - export reports
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
fleetlift run --file auth-audit.yaml

# View full reports (markdown)
fleetlift reports auth-audit-xyz789
# Displays each repository's full report

# Export structured data (frontmatter) for aggregation
fleetlift reports auth-audit-xyz789 --format json > results.json
# [{"repository": "service-a", "frontmatter": {"auth_library": "oauth2", ...}}, ...]
```

---

## Phase 4b: forEach Multi-Target Discovery

**Goal**: Enable iterating over multiple targets within a single repository for fine-grained discovery.

> **Design Rationale**: forEach enables analyzing multiple components within a repo (e.g., each
> microservice in a monorepo, each API endpoint, each config file) with separate reports per target.
> This is useful for detailed audits where a single repo contains many analyzable units.

### 4b.1 Data Model

- [x] Add `for_each[]` field to Task for defining iteration targets *(already in model.Task)*
- [x] Add `ForEachExecution` struct to track per-target results *(model.ForEachExecution)*
- [x] Add `ForEachResults` to `RepositoryResult` *(RepositoryResult.ForEachResults)*

### 4b.2 Template Substitution

- [x] Parse `{{.Name}}` and `{{.Context}}` variables in prompt *(substitutePromptTemplate function)*
- [x] Inject target context into each execution *(buildPromptForTarget function)*
- [x] Error handling for invalid template syntax

### 4b.3 Workflow Changes

- [x] Loop over `for_each` targets within each repository *(in Transform workflow report mode section)*
- [x] Execute agent once per target with substituted prompt
- [x] Collect report per target (namespaced as `REPORT-{target.name}.md`)
- [x] Aggregate `ForEachExecution` results in `RepositoryResult`
- [x] Continue on partial failures (record error, process next target)

### 4b.4 CLI Support

- [x] Display per-target reports in `fleetlift reports` command
- [x] Support `--target <name>` filter for viewing specific target reports
- [x] JSON export includes target-level grouping

### 4b.5 Config Validation

- [x] Validate target names contain only `[a-zA-Z0-9_-]` (safe for filenames)
- [x] Error if `for_each` used with `mode: transform`

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
fleetlift run --file api-audit.yaml

# View all target reports
fleetlift reports <workflow-id>
# Repository: api-gateway
#   Target: users-api
#     Frontmatter: {auth: "jwt", rate_limit: true, ...}
#   Target: orders-api
#     Frontmatter: {auth: "jwt", rate_limit: false, ...}
#   Target: payments-api
#     Frontmatter: {auth: "jwt", rate_limit: true, ...}

# Export as JSON for aggregation
fleetlift reports <workflow-id> --output json
```

---

## Phase 4c: Transformation Repository Support

**Goal**: Enable separation of "recipe" (transformation repo) from "targets" for reusable skills and tools.

> **Design Rationale**: Transformation repositories allow centralized skills, tools, and configuration
> to be applied across multiple target repositories. This is essential for endpoint classification,
> security audits, and multi-repo analysis where the "how" (skills) should be separate from the
> "what" (target repos).

### 4c.1 Data Model

- [x] Add `Transformation *Repository` field to Task (the "recipe" repo)
- [x] Add `Targets []Repository` field (repos to analyze/transform)
- [x] Add `UsesTransformationRepo()` helper method
- [x] Add `GetEffectiveRepositories()` to return targets or repositories based on mode

### 4c.2 Config Loader

- [x] Parse `transformation` and `targets` fields from YAML
- [x] Validation: error if both `transformation` and `repositories` are used
- [x] Validation: error if `targets` without `transformation`
- [x] Backward compatible: `repositories` alone still works

### 4c.3 Workspace Layout

- [x] Transformation mode: clone transformation repo to `/workspace/`, targets to `/workspace/targets/`
- [x] Legacy mode: clone repos directly to `/workspace/{name}`
- [x] Run transformation repo setup commands first
- [x] Run target repo setup commands in `/workspace/targets/{name}`

### 4c.4 Activity Updates

- [x] `CloneRepositories` - support transformation layout with new input struct
- [x] `RunVerifiers` - use correct base path based on layout
- [x] `CreatePullRequest` - use correct repo path based on layout
- [x] `CollectReport` - use correct report path based on layout

### 4c.5 Workflow Updates

- [x] Pass `UseTransformationLayout` flag to all relevant activities
- [x] Use `GetEffectiveRepositories()` for iteration
- [x] Update `generateAgentsMD()` to show transformation info
- [x] Update `buildPrompt()` to show correct paths

### Deliverable

```yaml
# transformation-task.yaml
version: 1
id: endpoint-classification
title: "Classify endpoints for removal"
mode: report

# Transformation repo with skills and tools
transformation:
  url: https://github.com/org/classification-tools.git
  branch: main
  setup:
    - npm install

# Target repos to analyze
targets:
  - url: https://github.com/org/api-server.git
    name: server
  - url: https://github.com/org/web-client.git
    name: client

for_each:
  - name: users-endpoint
    context: |
      Endpoint: GET /api/v1/users
      Location: targets/server/src/handlers/users.go:45

execution:
  agentic:
    prompt: |
      Use the endpoint-classification skill to analyze {{.Name}}.
      {{.Context}}

      Search for callers in /workspace/targets/
```

```bash
# Run with transformation repo
fleetlift run --file transformation-task.yaml

# Workspace layout:
# /workspace/
# ├── .claude/skills/     # From transformation repo
# ├── CLAUDE.md           # From transformation repo
# └── targets/
#     ├── server/         # Target repos
#     └── client/
```

---

## Phase 5: Grouped Execution with Failure Thresholds - COMPLETE

**Goal**: Enable fleet-wide operations with failure handling and retry capabilities.

> **Implementation Note**: Instead of implementing a separate Campaign concept, we enhanced
> Task execution with grouped execution, failure thresholds, and retry capabilities. This
> provides the same benefits with a simpler, more flexible model.

### 5.1 Data Model - COMPLETE

- [x] Add `FailureConfig` to Task for threshold and action
- [x] Add `GroupResult` to track per-group execution status
- [x] Add `ExecutionProgress` for real-time progress queries
- [x] Add `ContinueSignalPayload` for resume signals
- [x] Add `Groups` and `OriginalWorkflowID` to TaskResult

### 5.2 Grouped Execution with Failure Detection - COMPLETE

- [x] Track results incrementally as groups complete
- [x] Calculate failure percentage after each group
- [x] Pause execution when threshold exceeded (action: pause)
- [x] Abort execution when threshold exceeded (action: abort)
- [x] Allow in-flight groups to complete during pause/abort
- [x] Register query handler for execution progress

### 5.3 Pause/Continue/Retry - COMPLETE

- [x] Signal handler for continue with skip_remaining option
- [x] Wait for human decision during pause (24hr timeout)
- [x] Support cancellation during pause
- [x] CLI: `continue` command with --skip-remaining flag
- [x] CLI: `retry` command with --failed-only flag
- [x] Enhanced `status` command to show group progress

### 5.4 Config Loading - COMPLETE

- [x] Parse `failure.threshold_percent` and `failure.action` from YAML
- [x] Validate threshold is 0-100
- [x] Validate action is "pause" or "abort"
- [x] Helper methods on Task: `GetFailureThresholdPercent()`, `ShouldPauseOnFailure()`

### Deliverable

```yaml
version: 1
id: fleet-slog-migration
title: "Migrate to slog across all services"

groups:
  - name: platform-team
    repositories:
      - url: https://github.com/org/auth-service.git
      - url: https://github.com/org/user-service.git
  - name: payments-team
    repositories:
      - url: https://github.com/org/payment-gateway.git
  - name: notifications-team
    repositories:
      - url: https://github.com/org/email-service.git

execution:
  agentic:
    prompt: "Migrate from log.Printf to slog..."
    verifiers:
      - name: build
        command: ["go", "build", "./..."]

max_parallel: 3

failure:
  threshold_percent: 20  # Pause if >20% of groups fail
  action: pause          # "pause" or "abort"

timeout: 30m
```

```bash
# Run task with grouped execution
fleetlift run -f fleet-slog-migration.yaml

# Check progress (shows group-level detail)
fleetlift status --workflow-id transform-fleet-slog-migration
# Progress: 8/12 groups complete
# Failed: 2 (16%)

# If paused on threshold
fleetlift continue --workflow-id transform-fleet-slog-migration

# Retry failed groups after completion
fleetlift retry \
  --file fleet-slog-migration.yaml \
  --workflow-id transform-fleet-slog-migration \
  --failed-only
```

---

## Phase 6: Kubernetes Sandbox Provider with Controller

**Goal**: Run sandboxes on Kubernetes using a controller pattern for production workloads.

> **Design Rationale**: The controller pattern provides better security than direct K8s API access
> from the worker. The worker creates `SandboxRequest` custom resources with minimal permissions,
> and a dedicated controller reconciles them into Jobs with elevated permissions. This enables
> least-privilege access, policy enforcement, and Kubernetes-native observability.

### 6.1 SandboxRequest CRD

- [ ] Define `SandboxRequest` CRD schema (api/v1alpha1/)
- [ ] Fields: taskId, image, resources, credentials, runtimeClassName, nodeSelector
- [ ] Status: phase, podName, jobName, execResults
- [ ] Generate CRD manifests with controller-gen

### 6.2 Sandbox Controller

- [ ] Scaffold controller with kubebuilder
- [ ] Implement reconciliation loop:
  - [ ] `Pending` → validate request, create Job → `Provisioning`
  - [ ] `Provisioning` → wait for pod Running → `Running`
  - [ ] `Running` → process exec requests, update results
  - [ ] `Succeeded/Failed` → cleanup (or rely on TTL)
- [ ] Policy enforcement (allowed images, max resources, require gVisor)
- [ ] Exec handling via K8s exec API

### 6.3 Kubernetes Sandbox Provider (Worker Side)

- [ ] Implement `Provider` interface using CR-based approach
- [ ] `Provision()` - create SandboxRequest CR, wait for Running phase
- [ ] `Exec()` - add exec request to CR, poll for result in status
- [ ] `CopyFrom()` - exec `cat` command to read files
- [ ] `Cleanup()` - delete SandboxRequest CR

### 6.4 Job Specification (Controller Side)

- [ ] Generate Job spec from SandboxRequest
- [ ] Configure resources (CPU, memory) from spec
- [ ] Set `runtimeClassName` for gVisor if specified
- [ ] Apply node selectors and tolerations
- [ ] Mount secrets for GitHub token, API keys
- [ ] Set ownerReference for garbage collection
- [ ] Set `ttlSecondsAfterFinished` for automatic cleanup

### 6.5 RBAC Configuration

- [ ] Worker Role: create/get/patch/delete SandboxRequest CRs only
- [ ] Controller Role: create/delete Jobs, exec into pods, read secrets
- [ ] Sandbox ServiceAccount: no K8s API access (empty RBAC)

### 6.6 Provider Selection

- [ ] Factory function based on `SANDBOX_PROVIDER` env var
- [ ] Auto-detect: Docker socket → Docker, ServiceAccount → Kubernetes
- [ ] Fallback chain: Kubernetes → Docker → error

### 6.7 Namespace and Multi-tenancy

- [ ] Configurable sandbox namespace (default: `sandbox-isolated`)
- [ ] Support namespace-per-team for isolation
- [ ] ResourceQuota enforcement per namespace

### 6.8 Local K8s Testing

- [ ] kind cluster setup script
- [ ] Deploy CRD and controller to kind
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
# Worker creates SandboxRequest CR:
kubectl get sandboxrequests -n sandbox-isolated
NAME              PHASE     POD                   AGE
task-abc123       Running   task-abc123-xyz       2m

# Controller creates and manages Job:
kubectl get jobs -n sandbox-isolated
NAME                      COMPLETIONS   DURATION   AGE
task-abc123               0/1           2m         2m
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

**Goal**: Production-grade security and operational resilience.

> **Note**: Core RBAC (worker, controller, sandbox service accounts) is implemented in Phase 6
> as part of the controller pattern. This phase focuses on additional security hardening.

### 8.1 Network Policies

- [ ] Sandbox egress policy: allow HTTPS to GitHub, package registries, AI APIs
- [ ] Deny all ingress to sandbox pods
- [ ] Worker-to-controller communication via CRs only (no direct pod exec)
- [ ] Document required egress destinations for common ecosystems

### 8.2 Advanced RBAC

- [ ] Namespace-scoped roles for multi-tenant deployments
- [ ] Admission webhooks for additional policy enforcement
- [ ] Pod Security Standards (restricted profile for sandboxes)

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

### 9.2 Human-in-the-Loop (Iterative Steering) - COMPLETE

**Goal**: Enable iterative human-agent collaboration instead of binary approve/reject.

> **This is a key differentiator.** Basic HITL (approve/reject) is table stakes. Iterative
> steering—where humans can guide the agent through multiple rounds—makes the platform valuable.

#### 9.2.1 View Changes

- [x] `GetDiff` activity - return full git diffs for all modified files
- [x] `GetVerifierOutput` activity - return detailed test/build output
- [x] CLI: `fleetlift diff --workflow-id <id>` - view changes in terminal
- [x] CLI: `fleetlift logs --workflow-id <id>` - view verifier output
- [x] Slack: Include diff summary in approval notifications

#### 9.2.2 Steering Prompts

- [x] Add `steer` signal with prompt payload to workflow
- [x] Workflow loops back to Claude Code with steering prompt
- [x] Preserve conversation context across steering iterations
- [x] CLI: `fleetlift steer --workflow-id <id> --prompt "try X instead"`
- [x] Track iteration count and history
- [x] Configurable max iterations (`max_steering_iterations` in task, default 5)
- [x] Query handlers for diff, verifier logs, and steering state (web-ready)

#### 9.2.3 Partial Approval

> **Explicitly out of scope** - Partial/granular file-level approval was excluded per requirements.
> Users can steer to request specific file changes, then approve all changes together.

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
| 4b | forEach Discovery | Multi-target iteration within repos | ✅ Complete |
| 4c | Transformation Repo | Reusable skills, recipe/targets separation | ✅ Complete |
| 5 | **Grouped Execution** | Failure thresholds, pause/continue, retry | ✅ Complete |
| 6 | **K8s Controller** | Sandbox controller + CRD for production | ⬜ Not started |
| 7 | **Observability** | Metrics, logging, dashboards | ⬜ Not started |
| 8 | **Security** | NetworkPolicy, secrets, scaling | ⬜ Not started |
| 9 | Advanced | HITL steering, scheduling, cost tracking | 🟡 ~40% (basic HITL + iterative steering) |

Each phase builds on the previous and delivers working functionality.

### Recommended Next Steps

**Production Infrastructure Track:**
1. **Phase 6** - Kubernetes sandbox controller (CRD + controller for least-privilege)
2. **Phase 7** - Observability (metrics, dashboards, alerting)
3. **Phase 8** - Security hardening (NetworkPolicy, secrets, scaling)

**Enhanced Features Track:**
1. **Phase 9.3** - Scheduled tasks (recurring transformations)
2. **Phase 9.4** - Cost tracking (API usage, compute attribution)
3. **Phase 9.5** - Web UI (optional dashboard)

> **Status Note**: Core platform capabilities are complete. All product features (agentic/deterministic
> transforms, report mode, grouped execution, failure handling, retry) are implemented. Next phase
> focuses on production infrastructure (Kubernetes) and operational features (observability, security).

### Key Changes from Previous Plan

| Previous | Current | Rationale |
|----------|---------|-----------|
| Phase 4: CRD & Controller | Phase 4: Report Mode | Report mode is core functionality; CRD is optional convenience layer |
| Phase 8: Agent Sandbox | Removed | Plain K8s Jobs are sufficient; Agent Sandbox adds complexity without clear benefit |
| Phase 9.6: Report Mode | Phase 4: Report Mode | Promoted to core phase—discovery is first-class, not an afterthought |
| Campaign as separate concept | Phase 5: Grouped Execution | Simpler model: failure handling at Task level with groups instead of separate Campaign type |
| Direct K8s API from worker | Phase 6: Controller pattern | Least-privilege: worker creates CRs, controller has elevated permissions |
