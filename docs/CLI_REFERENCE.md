# CLI Reference

Complete command-line reference for the Claude Code Orchestrator.

## Starting Workflows

### From YAML File

```bash
./bin/orchestrator run -f task.yaml
```

### From Flags (Transform Mode)

```bash
./bin/orchestrator run \
  --repo https://github.com/org/repo.git \
  --prompt "Fix the bug in auth.go" \
  --no-approval
```

### Report Mode (Discovery)

```bash
./bin/orchestrator run -f examples/task-report.yaml
```

## Monitoring Workflows

### List Workflows

```bash
# List all workflows
./bin/orchestrator list

# Filter by status
./bin/orchestrator list --status Running
./bin/orchestrator list --status Completed
./bin/orchestrator list --status Failed
```

### Check Status (Running Workflows)

```bash
# Only works while workflow is running
./bin/orchestrator status --workflow-id transform-<task-id>
```

### Get Result (Completed Workflows)

```bash
# Works for completed workflows
./bin/orchestrator result --workflow-id transform-<task-id>
```

## Report Commands

### View Reports (Report Mode Only)

```bash
# Table format (body truncated for readability)
./bin/orchestrator reports transform-<task-id>

# Full JSON output
./bin/orchestrator reports transform-<task-id> -o json

# Save to file
./bin/orchestrator reports transform-<task-id> -o json > report.json

# Only frontmatter (structured data)
./bin/orchestrator reports transform-<task-id> --frontmatter-only -o json

# Filter to specific target (forEach mode)
./bin/orchestrator reports transform-<task-id> --target users-api
```

## Workflow Control

### Approve Changes

```bash
# Approve when require_approval: true
./bin/orchestrator approve --workflow-id transform-<task-id>
```

### Reject Changes

```bash
# Reject and stop workflow
./bin/orchestrator reject --workflow-id transform-<task-id>
```

### Cancel Workflow

```bash
# Cancel running workflow
./bin/orchestrator cancel --workflow-id transform-<task-id>
```

## Important Notes

### Workflow ID Prefix

Workflow IDs are automatically prefixed with `transform-`:

```yaml
# In task file
id: smoke-test

# Actual workflow ID
transform-smoke-test
```

Use `transform-<task-id>` in all CLI commands.

### Status vs Result

- **`status`**: Only works for **running** workflows
- **`result`**: Only works for **completed** workflows

```bash
# While running
./bin/orchestrator status --workflow-id transform-my-task

# After completion
./bin/orchestrator result --workflow-id transform-my-task
```

### Temporal UI

View detailed workflow execution:
```bash
open http://localhost:8233
```

Navigate to: `Namespaces > default > Workflows > transform-<task-id>`

## Examples

### Complete Workflow Lifecycle

```bash
# 1. Start workflow
./bin/orchestrator run -f examples/task-agentic.yaml

# 2. Monitor progress
./bin/orchestrator list --status Running

# 3. Check status while running
./bin/orchestrator status --workflow-id transform-example-agentic

# 4. View in UI
open http://localhost:8233

# 5. Approve (if require_approval: true)
./bin/orchestrator approve --workflow-id transform-example-agentic

# 6. Get final result
./bin/orchestrator result --workflow-id transform-example-agentic
```

### Report Mode Workflow

```bash
# 1. Start discovery
./bin/orchestrator run -f examples/task-report.yaml

# 2. Wait for completion (or check status)
./bin/orchestrator status --workflow-id transform-example-report

# 3. View reports
./bin/orchestrator reports transform-example-report

# 4. Export to JSON
./bin/orchestrator reports transform-example-report -o json > results.json

# 5. Process structured data
cat results.json | jq '.[] | .frontmatter'
```

### forEach Workflow

```bash
# 1. Start forEach analysis
./bin/orchestrator run -f examples/task-foreach.yaml

# 2. Get all results
./bin/orchestrator reports transform-example-foreach -o json

# 3. Filter to specific target
./bin/orchestrator reports transform-example-foreach --target users-api -o json
```

## Global Flags

```
--temporal-address string   Temporal server address (default "localhost:7233")
--namespace string          Temporal namespace (default "default")
--help                      Show help
--version                   Show version
```

## Environment Variables

```bash
# Required for agentic mode
export ANTHROPIC_API_KEY=sk-ant-...

# Optional, for creating PRs
export GITHUB_TOKEN=ghp_...

# Temporal server (if not localhost)
export TEMPORAL_ADDRESS=temporal.example.com:7233

# Temporal namespace (if not default)
export TEMPORAL_NAMESPACE=production
```

## Exit Codes

- `0` - Success
- `1` - General error
- `2` - Invalid arguments
- `3` - Workflow not found
- `4` - Workflow failed
- `5` - Approval timeout/rejected

## Tips

### Using jq with Reports

```bash
# Extract all security scores
./bin/orchestrator reports transform-security-audit -o json | jq '.[] | .frontmatter.score'

# Find high-severity issues
./bin/orchestrator reports transform-audit -o json | \
  jq '.[] | .frontmatter.issues[] | select(.severity == "high")'

# Group by repository
./bin/orchestrator reports transform-audit -o json | jq 'group_by(.repository)'
```

### Debugging Workflows

```bash
# Get detailed execution history
open "http://localhost:8233/namespaces/default/workflows/transform-<task-id>"

# Check worker logs
make run-worker  # Worker outputs detailed logs

# View activity inputs/outputs in Temporal UI
# Navigate to: Workflow > History > Activity details
```

### Batch Operations

```bash
# List all failed workflows
./bin/orchestrator list --status Failed

# Cancel all running workflows (careful!)
./bin/orchestrator list --status Running | \
  awk '{print $1}' | \
  xargs -I {} ./bin/orchestrator cancel --workflow-id {}
```

## See Also

- [Task File Reference](TASK_FILE_REFERENCE.md) - YAML specification
- [Examples](examples/) - Example workflows
- [Troubleshooting](TROUBLESHOOTING.md) - Common issues
