# Grouped Execution with Failure Handling

## Overview

Fleetlift handles fleet-wide operations through **grouped execution** at the Task level, eliminating the need for a separate Campaign concept. This provides a simpler, more flexible model for orchestrating changes across many repositories.

## Key Features

### 1. Repository Groups

Organize repositories into named groups that share a sandbox:

```yaml
groups:
  - name: backend-services
    repositories:
      - url: https://github.com/org/auth.git
      - url: https://github.com/org/users.git
  - name: frontend-services
    repositories:
      - url: https://github.com/org/web-app.git
```

### 2. Parallel Execution

Control concurrency with `max_parallel`:

```yaml
max_parallel: 5  # Execute up to 5 groups concurrently
```

### 3. Failure Thresholds

Automatically pause or abort when failures exceed a threshold:

```yaml
failure:
  threshold_percent: 20  # Pause if >20% of groups fail
  action: pause          # Options: "pause" or "abort"
```

## How It Works

### Execution Flow

1. **Groups launch in parallel** (up to `max_parallel`)
2. **Track progress incrementally** as each group completes
3. **Check failure threshold** after each completion
4. **Pause if threshold exceeded** (when `action: pause`)
5. **Wait for human decision**: continue, skip remaining, or cancel

### Pause Behavior

When the failure threshold is exceeded:

```bash
# Check status
fleetlift status --workflow-id <id>
# Status: PAUSED
# Reason: Failure threshold exceeded (25% > 20%)
# Failed groups: team-b, team-d

# Option 1: Continue with remaining groups
fleetlift continue --workflow-id <id>

# Option 2: Skip remaining groups and finish
fleetlift continue --workflow-id <id> --skip-remaining

# Option 3: Cancel the entire workflow
fleetlift cancel --workflow-id <id>
```

### Abort Behavior

When `action: abort`, the workflow:
- Immediately stops launching new groups
- Marks remaining groups as "skipped"
- Allows in-flight groups to complete
- Returns results with partial completion

## Retry Failed Groups

After a workflow completes, retry only the failed groups:

```bash
fleetlift retry \
  --file task.yaml \
  --workflow-id <original-id> \
  --failed-only
```

This:
1. Gets the result from the original workflow
2. Extracts which groups failed
3. Filters the task to only those groups
4. Starts a new workflow with the filtered task
5. Tracks the retry lineage via `original_workflow_id`

## Complete Example

```yaml
version: 1
id: fleet-slog-migration
title: "Migrate to slog across all services"

# Organize by team for better context
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
      - url: https://github.com/org/sms-service.git

execution:
  agentic:
    prompt: |
      Migrate from log.Printf to slog:
      - Replace log.Printf with slog.Info/Warn/Error
      - Add structured context fields
      - Initialize logger in main()
    verifiers:
      - name: build
        command: ["go", "build", "./..."]
      - name: test
        command: ["go", "test", "./..."]

# Execute 3 groups at a time
max_parallel: 3

# Pause if more than 15% of groups fail
failure:
  threshold_percent: 15
  action: pause

timeout: 30m
require_approval: true

pull_request:
  branch_prefix: "auto/slog-migration"
  title: "Migrate to structured logging (slog)"
  labels: ["automated", "logging"]
```

## Usage

```bash
# Start the task
fleetlift run -f fleet-slog-migration.yaml

# Monitor progress (shows group-level detail)
fleetlift status --workflow-id transform-fleet-slog-migration
# Progress: 8/12 groups complete
# Failed: 1 (8%)
# Groups:
#   [✓] platform-team (2 repos)
#   [✗] payments-team: verifier failed
#   [~] notifications-team (running)
#   [ ] ...

# If paused on threshold
fleetlift continue --workflow-id transform-fleet-slog-migration

# Retry failed groups after completion
fleetlift retry \
  --file fleet-slog-migration.yaml \
  --workflow-id transform-fleet-slog-migration \
  --failed-only
```

## Comparison to Campaign Concept

Previously, we planned a separate `Campaign` type for fleet-wide operations. The current implementation is simpler and more flexible:

| Aspect | Campaign (Planned) | Grouped Execution (Implemented) |
|--------|-------------------|----------------------------------|
| **Concept** | Separate workflow type | Enhanced Task with groups |
| **Schema** | campaign.yaml | Same task.yaml schema |
| **CLI** | Separate commands | Same task commands |
| **Complexity** | Higher (two workflow types) | Lower (one workflow type) |
| **Flexibility** | Fixed batching model | Flexible group organization |
| **Retry** | Campaign-level retry | Group-level retry |

## Query API

The workflow exposes real-time progress via queries:

```go
// Get execution progress
progress, _ := client.GetExecutionProgress(ctx, workflowID)

// Returns:
type ExecutionProgress struct {
    TotalGroups      int      // Total number of groups
    CompletedGroups  int      // Groups that finished
    FailedGroups     int      // Groups that failed
    FailurePercent   float64  // Current failure rate
    IsPaused         bool     // Currently paused on threshold
    PausedReason     string   // Why it's paused
    FailedGroupNames []string // Names of failed groups
}
```

This enables building dashboards and monitoring tools on top of the workflow.

## Benefits

1. **Simpler Model** - One workflow type (Task) handles everything
2. **Flexible Grouping** - Organize repos however makes sense
3. **Incremental Pause** - Pause as soon as threshold is hit
4. **Granular Retry** - Retry specific failed groups
5. **Real-time Progress** - Query API provides live updates
6. **Backward Compatible** - Single-group tasks work unchanged
