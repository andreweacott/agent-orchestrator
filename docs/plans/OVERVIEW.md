# Code Transformation and Discovery Platform

## The Problem

Fleet-wide code changes are hard. Understanding what exists across repositories is harder. When you need to upgrade a dependency, migrate an API, or fix a security vulnerability across dozens or hundreds of repositories, manual approaches don't scale.

Existing tools like [Turbolift](https://github.com/Skyscanner/turbolift) help with the mechanics of cloning repos and opening PRs, but they lack:

- **Durability**: If your laptop sleeps or the script dies, you start over
- **Approval workflows**: Changes go straight to PRs without human review
- **Agentic execution**: Complex, context-dependent changes require AI assistance
- **Discovery at scale**: Understanding what exists across repositories before changing it

## The Solution

A managed platform for code transformation **and discovery** at scale:

- **Durable execution** via Temporal—survives failures, restarts, and network issues
- **Human-in-the-loop approval** before any PR is created
- **Two execution backends**: deterministic (Docker images) and agentic (AI-driven)
- **Two modes**: transform (create PRs) and report (collect structured data)

Think of it as **managed Turbolift** with AI capabilities:

| Turbolift | This Platform |
|-----------|---------------|
| CLI on your laptop | Managed service (Temporal + K8s) |
| Script/command | Docker image OR AI agent |
| Dies if laptop closes | Durable execution, survives failures |
| No approval flow | Human-in-the-loop before PR |
| Stateless | Status tracking, audit trail |

---

## Key Concepts

### Task

The atomic unit of work. A Task operates on one or more repositories and either:

- **Transforms** code and creates PRs, or
- **Reports** structured data (no PRs created)

A Task specifies:
- Target repositories
- What to do (prompt for agentic, Docker image for deterministic)
- How to verify success (build, test, lint commands)
- PR metadata (title, branch prefix, labels) for transform mode

### Campaign

Orchestrates multiple Tasks across many repositories with:

- **Batch execution**: Submit Tasks in parallel with configurable concurrency
- **Failure thresholds**: Pause if too many Tasks fail (e.g., >10%)
- **Human escalation**: Ask a human to decide—abort, continue, or retry
- **Result aggregation**: Collect outputs across all Tasks

### Execution Modes

| Mode | Deterministic | Agentic |
|------|--------------|---------|
| **What** | Docker image (OpenRewrite, custom scripts) | AI agent (Claude Code) |
| **When** | Known, repeatable transformations | Complex, context-dependent changes |
| **Verification** | Exit code + verifier commands | Verifier commands (agent iterates until passing) |
| **Examples** | Log4j upgrade, dependency bumps | API migration, refactoring with judgment |

**When to use deterministic:**
- You have an existing tool (OpenRewrite recipe, Scalafix rule, custom script)
- The transformation is well-defined and repeatable
- You want predictable, auditable changes

**When to use agentic:**
- The change requires understanding context and making judgment calls
- The transformation varies by codebase (different frameworks, patterns)
- You'd normally assign this to an engineer

### Transform vs Report Mode

| | Transform Mode | Report Mode |
|---|----------------|-------------|
| **Output** | Pull requests | Structured data (JSON) |
| **Use when** | Making code changes | Analyzing code, gathering information |
| **Human approval** | Before PR creation | Not applicable |
| **Examples** | Migrations, upgrades, fixes | Audits, inventories, assessments |

---

## Use Cases

### Code Transformations

| Use Case | Execution | Description |
|----------|-----------|-------------|
| **Dependency upgrades** | Deterministic | Log4j 1.x → 2.x using OpenRewrite |
| **API migrations** | Agentic | Migrate from deprecated API to new version |
| **Security fixes** | Either | Add input validation, fix auth issues |
| **Refactoring** | Agentic | Migrate logging patterns, error handling |

### Code Discovery (Report Mode)

| Use Case | Description |
|----------|-------------|
| **Security audits** | Analyze authentication patterns across services |
| **Dependency inventories** | Catalog all Log4j versions in the org |
| **Technical debt assessment** | Identify deprecated patterns before migration |
| **Pre-migration analysis** | Discover what needs to change before transforming |

### Transformation Repository (Reusable Skills)

| Use Case | Description |
|----------|-------------|
| **Endpoint classification** | Use a classification skill repo to analyze API endpoints across multiple services |
| **Security scanning** | Reusable security audit skills applied to different codebases |
| **Migration tooling** | Centralized migration recipes applied to target repositories |
| **Multi-repo analysis** | Analyze relationships between services using a central skills repo |

---

## How It Works

### Single Task Flow

```
CLI → Temporal → Sandbox (Docker/K8s) → Execute → PR or Report
         ↑                                    ↓
         └──────── Human Approval ←──────────┘
```

1. **Submit**: User submits a Task via CLI or YAML file
2. **Provision**: Platform creates an isolated sandbox (Docker container or K8s Job)
3. **Clone**: Repository is cloned into the sandbox
4. **Execute**: Transformation runs (deterministic image or agentic prompt)
5. **Verify**: Verifier commands run to validate changes
6. **Approve**: Human reviews changes (for transform mode with approval required)
7. **Create PR**: On approval, platform creates the pull request
8. **Cleanup**: Sandbox is destroyed

### Campaign Flow

```
Campaign → Submit Tasks in batches → Monitor progress
              ↓                           ↓
         Parallel execution      Pause on failure threshold
              ↓                           ↓
         Collect results         Human decides: continue/abort/retry
```

1. **Define**: Specify repository selection and Task template
2. **Submit**: Campaign submits Tasks in configurable batch sizes
3. **Monitor**: Track progress across all Tasks
4. **Threshold**: If failures exceed threshold, pause and ask human
5. **Aggregate**: Collect results (PRs or reports) from all Tasks

---

## Deployment Modes

### Local (Docker)

Everything runs on your laptop:
- Temporal server via docker-compose
- Sandboxes as Docker containers
- Great for development and testing single repos

```bash
# Start Temporal
docker-compose up -d

# Run a transformation
orchestrator run \
  --repo https://github.com/org/service.git \
  --prompt "Add input validation to API endpoints" \
  --verifier "build:go build ./..." \
  --verifier "test:go test ./..."
```

### Production (Kubernetes)

Scales to hundreds of repositories:
- Temporal server (self-hosted or Temporal Cloud)
- Sandboxes as Kubernetes Jobs
- Optional: gVisor for enhanced isolation

```yaml
sandbox:
  provider: kubernetes
  namespace: sandbox-isolated
  image: your-org/claude-sandbox:latest
  nodeSelector:
    workload-type: sandbox
```

---

## CLI Quick Reference

| Command | Description |
|---------|-------------|
| `orchestrator run --file task.yaml` | Submit a Task from file |
| `orchestrator run --repo <url> --prompt <prompt>` | Submit an agentic Task |
| `orchestrator run --repo <url> --image <image>` | Submit a deterministic Task |
| `orchestrator status <workflow-id>` | Check Task status |
| `orchestrator approve <workflow-id>` | Approve changes and create PRs |
| `orchestrator reject <workflow-id>` | Reject changes |
| `orchestrator list` | List all Tasks |

### Common Flags

| Flag | Description |
|------|-------------|
| `--repos <url1,url2>` | Target multiple repositories |
| `--verifier "name:command"` | Add a verifier (can repeat) |
| `--parallel` | Auto-generate parallel groups (one repo per group) |
| `--no-approval` | Skip human approval (use with caution) |
| `--output json` | Output results as JSON |

---

## Examples

### Example 1: Migrate to Structured Logging (Agentic)

Transform 10 Go services from `log.Printf` to the `slog` package:

```yaml
# task.yaml
version: 1
id: slog-migration
title: "Migrate to structured logging (slog)"

repositories:
  - url: https://github.com/org/service-a.git
    setup: ["go mod download"]
  - url: https://github.com/org/service-b.git
    setup: ["go mod download"]
  # ... 8 more services

execution:
  agentic:
    prompt: |
      Migrate from log.Printf to the slog package:
      - Replace all log.Printf calls with slog equivalents
      - Add context fields where appropriate (request ID, user ID)
      - Use appropriate log levels (Info, Warn, Error)
      - Ensure logger is initialized in main()

    verifiers:
      - name: build
        command: ["go", "build", "./..."]
      - name: test
        command: ["go", "test", "./..."]
      - name: lint
        command: ["golangci-lint", "run"]

timeout: 30m
require_approval: true
max_parallel: 5  # Process repos concurrently

pull_request:
  branch_prefix: "auto/slog-migration"
  title: "Migrate to structured logging (slog)"
  labels: ["automated", "logging", "tech-debt"]
```

```bash
orchestrator run --file task.yaml
# Workflow started: slog-migration-abc123
# Status: Running (2/10 repositories complete)

orchestrator status slog-migration-abc123
# Status: AwaitingApproval
# Repositories:
#   service-a: ready (15 files modified)
#   service-b: ready (8 files modified)
#   ...

orchestrator approve slog-migration-abc123
# Creating PRs...
# PR created: https://github.com/org/service-a/pull/123
# PR created: https://github.com/org/service-b/pull/456
# ...
```

### Example 2: Security Audit (Report Mode)

Analyze authentication patterns across 50 repositories:

```yaml
# auth-audit.yaml
version: 1
id: auth-security-audit
title: "Authentication security audit"
mode: report

repositories:
  - url: https://github.com/org/service-a.git
  - url: https://github.com/org/service-b.git
  # ... 48 more services

execution:
  agentic:
    prompt: |
      Analyze this repository's authentication implementation.

      Write a report to /workspace/REPORT.md with:
      1. YAML frontmatter containing: auth_library, score (1-10), issues array
      2. Detailed markdown analysis with findings, rationale, and recommendations

    output:
      schema:  # Validates the frontmatter
        type: object
        required: ["auth_library", "score"]
        properties:
          auth_library:
            type: string
          score:
            type: integer
          issues:
            type: array

timeout: 15m
```

The agent produces a report like:

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

This service uses a **custom authentication implementation** with security concerns.

## Findings

### 1. Hardcoded API Key (High Severity)

Found in `config/config.yaml` at line 42...

**Recommendation**: Use environment variables or a secrets manager.

### 2. No Token Expiration (Medium Severity)

JWT tokens have no expiration claim...
```

```bash
orchestrator run --file auth-audit.yaml
# Workflow started: auth-security-audit-xyz789

orchestrator status auth-security-audit-xyz789
# Status: Completed
# Repositories: 50/50 complete

# View individual reports (full markdown)
orchestrator reports auth-security-audit-xyz789
# Displays each repository's report

# Export structured data (frontmatter only) for aggregation
orchestrator reports auth-security-audit-xyz789 --format json > audit-results.json
```

### Example 3: Log4j Upgrade (Deterministic)

Upgrade Log4j using OpenRewrite across all Java services:

```yaml
# log4j-upgrade.yaml
version: 1
id: log4j-upgrade
title: "Upgrade Log4j 1.x to 2.x"
mode: deterministic

repositories:
  - url: https://github.com/org/java-service-a.git
  - url: https://github.com/org/java-service-b.git
  # ... more Java services

execution:
  deterministic:
    image: openrewrite/rewrite:latest
    args:
      - "rewrite:run"
      - "-Drewrite.activeRecipes=org.openrewrite.java.logging.log4j.Log4j1ToLog4j2"

    verifiers:
      - name: build
        command: ["mvn", "compile"]
      - name: test
        command: ["mvn", "test"]

timeout: 20m
require_approval: false  # Pre-vetted deterministic transform

pull_request:
  branch_prefix: "security/log4j-upgrade"
  title: "Upgrade Log4j 1.x to 2.x (security)"
  labels: ["security", "automated", "dependencies"]
```

### Example 4: Transformation Repository (Reusable Skills)

Use a centralized skills repository to analyze endpoints across multiple target services:

```yaml
# endpoint-analysis.yaml
version: 1
id: endpoint-classification
title: "Classify API endpoints for removal"
mode: report

# Transformation repo - contains skills, tools, CLAUDE.md
transformation:
  url: https://github.com/org/classification-tools.git
  branch: main
  setup:
    - npm install

# Target repos to analyze (cloned to /workspace/targets/)
targets:
  - url: https://github.com/org/api-server.git
    name: server
  - url: https://github.com/org/web-client.git
    name: client
  - url: https://github.com/org/mobile-app.git
    name: mobile

for_each:
  - name: users-endpoint
    context: |
      Endpoint: GET /api/v1/users
      Location: targets/server/src/handlers/users.go:45
  - name: legacy-export
    context: |
      Endpoint: POST /api/v1/export
      Location: targets/server/src/handlers/legacy.go:120

execution:
  agentic:
    prompt: |
      Use the endpoint-classification skill to analyze {{.Name}}.

      {{.Context}}

      Search for callers across all targets:
      - /workspace/targets/server
      - /workspace/targets/client
      - /workspace/targets/mobile

      Classify whether this endpoint can be safely removed.

    output:
      schema:
        type: object
        properties:
          classification:
            type: string
            enum: ["remove", "keep", "deprecate", "investigate"]
          callers_found:
            type: integer
          reasoning:
            type: string

timeout: 30m
```

The workspace layout when using transformation mode:
```
/workspace/
├── .claude/                    # From transformation repo
│   └── skills/
│       └── endpoint-classification.md
├── CLAUDE.md                   # From transformation repo
└── targets/
    ├── server/                 # Target repos
    ├── client/
    └── mobile/
```

```bash
# Run endpoint classification
orchestrator run --file endpoint-analysis.yaml

# View reports per endpoint
orchestrator reports endpoint-classification-xyz
# Repository: (transformation)
#   Target: users-endpoint
#     classification: keep
#     callers_found: 12
#   Target: legacy-export
#     classification: remove
#     callers_found: 0
```

---

## Further Reading

- [DESIGN.md](DESIGN.md) - Technical architecture, data model, and implementation details
- [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md) - Development phases and status
