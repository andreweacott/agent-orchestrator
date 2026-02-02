# Troubleshooting

Common issues and solutions when using the Claude Code Orchestrator.

## Workflow Issues

### "Workflow is running but nothing happens"

**Symptoms:**
- Workflow shows "Running" status
- No progress in Temporal UI
- Claude Code activity never starts

**Solutions:**

1. **Check if worker is running:**
   ```bash
   ps aux | grep bin/worker
   # Should show a running process
   ```

2. **Verify API key is set:**
   ```bash
   # Worker needs ANTHROPIC_API_KEY for agentic mode
   export ANTHROPIC_API_KEY=sk-ant-...
   make run-worker
   ```

3. **Check worker logs for errors:**
   ```bash
   # Look for configuration warnings or errors
   make run-worker
   ```

4. **Verify Docker is running (for sandbox):**
   ```bash
   docker ps
   # Should list running containers
   ```

5. **Check Temporal UI:**
   ```bash
   open http://localhost:8233
   # Navigate to workflow > History
   # Look for errors or pending activities
   ```

### "Status shows 'running' for completed workflow"

**Problem:**
The `status` command only works for **running** workflows. Use `result` for completed ones.

**Solution:**
```bash
# Wrong (for completed workflow)
./bin/fleetlift status --workflow-id transform-my-task

# Correct
./bin/fleetlift result --workflow-id transform-my-task
```

### "Workflow waiting for approval indefinitely"

**Symptoms:**
- Report mode task stuck at approval step
- Transform mode needs approval but you didn't realize

**Solutions:**

1. **For report mode, disable approval:**
   ```yaml
   mode: report
   require_approval: false  # Add this
   ```

2. **Approve the workflow:**
   ```bash
   ./bin/fleetlift approve --workflow-id transform-<task-id>
   ```

3. **Check approval status in Temporal UI:**
   - Navigate to workflow
   - Look for "Waiting for signal: approval-decision"

### "Activity not registered" error

**Symptoms:**
- Error: "activity type not registered"
- Workflow fails immediately

**Solutions:**

1. **Restart the worker:**
   ```bash
   # Kill old worker
   pkill -f bin/worker

   # Start fresh worker
   make run-worker
   ```

2. **Verify worker binary is up to date:**
   ```bash
   # Rebuild
   make build

   # Restart worker
   make run-worker
   ```

3. **Check activity registration in worker logs:**
   - Should see: "Registered activity: ActivityName"

## Docker / Sandbox Issues

### "Cannot connect to Docker daemon"

**Symptoms:**
- Error: "Cannot connect to the Docker daemon"
- Sandbox creation fails

**Solutions:**

1. **Start Docker Desktop:**
   ```bash
   # macOS
   open -a Docker

   # Verify
   docker ps
   ```

2. **Check Docker socket permissions:**
   ```bash
   ls -la /var/run/docker.sock
   # Should be readable by your user
   ```

### "Sandbox container exits immediately"

**Symptoms:**
- Container starts but exits with code 1
- No output from Claude Code

**Solutions:**

1. **Check sandbox image exists:**
   ```bash
   docker images | grep claude-sandbox
   ```

2. **Build sandbox image:**
   ```bash
   make sandbox-build
   ```

3. **Test sandbox locally:**
   ```bash
   docker run -it claude-sandbox:latest /bin/bash
   ```

4. **Check logs in Temporal UI:**
   - Activity details show container output

## Claude Code Issues

### "Claude Code exceeds iteration limit"

**Symptoms:**
- Error: "max_iterations exceeded"
- Workflow fails after many iterations

**Solutions:**

1. **Increase iteration limit:**
   ```yaml
   execution:
     agentic:
       limits:
         max_iterations: 100  # Increase from default 50
   ```

2. **Make prompt more specific:**
   - Provide exact file paths
   - Give clear success criteria
   - Add examples of desired output

3. **Check for infinite loops:**
   - Review Claude Code conversation in Temporal UI
   - Look for repeated failed attempts

### "Verifier fails unexpectedly"

**Symptoms:**
- Claude Code completes successfully
- Verifier command fails
- No PR created

**Solutions:**

1. **Check verifier command:**
   ```yaml
   verifiers:
     - name: test
       command: [go, test, ./...]  # Verify command is correct
   ```

2. **Test verifier locally:**
   ```bash
   cd /path/to/repo
   go test ./...
   # Debug any failures
   ```

3. **Add setup commands:**
   ```yaml
   repositories:
     - url: https://github.com/org/repo.git
       setup:
         - go mod download  # Install dependencies first
   ```

4. **View verifier output:**
   - Check Temporal UI > Activity > Output
   - Shows stdout/stderr from verifier

## API / Authentication Issues

### "Invalid API key" errors

**Symptoms:**
- Error: "invalid API key"
- Claude Code activity fails immediately

**Solutions:**

1. **Verify API key format:**
   ```bash
   echo $ANTHROPIC_API_KEY
   # Should start with sk-ant-
   ```

2. **Re-export API key:**
   ```bash
   export ANTHROPIC_API_KEY=sk-ant-your-key-here
   make run-worker
   ```

3. **Check key has not expired:**
   - Log into Anthropic console
   - Verify key is active

### "GitHub authentication failed"

**Symptoms:**
- Error creating PR: "authentication required"
- Git push fails

**Solutions:**

1. **Set GitHub token:**
   ```bash
   export GITHUB_TOKEN=ghp_...
   make run-worker
   ```

2. **Verify token has correct scopes:**
   - Needs: `repo` (full control)
   - Create at: https://github.com/settings/tokens

3. **Check repository permissions:**
   - Token owner must have write access
   - For org repos, enable SSO if required

## Configuration Issues

### "Task file validation failed"

**Symptoms:**
- CLI rejects task file before starting
- Error messages about missing fields

**Solutions:**

1. **Check required fields:**
   ```yaml
   version: 1           # Required
   id: my-task         # Required
   title: "My task"    # Required
   ```

2. **Validate YAML syntax:**
   ```bash
   # Use yamllint or similar
   yamllint task.yaml
   ```

3. **Check field types:**
   ```yaml
   timeout: "30m"      # Wrong (string)
   timeout: 30m        # Correct (duration)
   ```

### "Invalid mode" error

**Problem:**
Mode must be exactly `transform` or `report`.

**Solution:**
```yaml
mode: transform  # Correct
# NOT: transforms, transformation, reporting, discover
```

## Performance Issues

### "Workflow takes too long"

**Symptoms:**
- Exceeds timeout
- Individual repos take 20+ minutes

**Solutions:**

1. **Increase timeout:**
   ```yaml
   timeout: 60m  # Increase from 30m
   ```

2. **Use deterministic mode for simple changes:**
   ```yaml
   execution:
     deterministic:  # Faster than agentic
       image: your-tool:latest
   ```

3. **Enable parallel execution:**
   ```yaml
   parallel: true  # Process repos simultaneously
   ```

4. **Simplify the prompt:**
   - Break into smaller tasks
   - Focus on specific changes

### "Too many API calls / rate limited"

**Symptoms:**
- Error: "rate limit exceeded"
- Slow Claude Code responses

**Solutions:**

1. **Reduce parallelism:**
   ```yaml
   parallel: false  # Sequential execution
   ```

2. **Add delays between repos:**
   - Use sequential mode
   - Temporal handles retries automatically

3. **Contact Anthropic for rate limit increase**

## Temporal Issues

### "Connection refused to Temporal server"

**Symptoms:**
- Error: "connection refused localhost:7233"
- CLI can't reach Temporal

**Solutions:**

1. **Start Temporal dev server:**
   ```bash
   make temporal-dev
   ```

2. **Verify Temporal is running:**
   ```bash
   temporal server info
   # Should show server details
   ```

3. **Check port is not in use:**
   ```bash
   lsof -i :7233
   # Should show Temporal process
   ```

### "Workflow execution history too large"

**Symptoms:**
- Error: "history size limit exceeded"
- Very long-running workflows fail

**Solutions:**

1. **Break into smaller tasks:**
   - Instead of 50 repos, do 10 at a time

2. **Use continue-as-new pattern:**
   - For very large batches
   - Advanced: requires code changes

3. **Clean up old workflows:**
   ```bash
   # Use Temporal CLI to clean old executions
   temporal workflow delete --query "StartTime < '2024-01-01'"
   ```

## Report Mode Issues

### "No reports found"

**Symptoms:**
- `./bin/fleetlift reports` returns empty
- Workflow completed but no data

**Solutions:**

1. **Verify mode is 'report':**
   ```yaml
   mode: report  # Required for reports command
   ```

2. **Check Claude Code created REPORT.md:**
   - View activity output in Temporal UI
   - Look for /workspace/REPORT.md

3. **Verify output schema validation passed:**
   - Check for validation errors in logs

### "JSON Schema validation failed"

**Symptoms:**
- Error: "frontmatter does not match schema"
- Report mode workflow fails

**Solutions:**

1. **Check schema matches frontmatter:**
   ```yaml
   schema:
     type: object
     required: [field1]  # Field1 must exist in YAML frontmatter
   ```

2. **Make prompt more explicit:**
   ```yaml
   prompt: |
     Write REPORT.md with YAML frontmatter containing exactly:
     - field1: string
     - field2: integer
   ```

3. **Test schema locally:**
   - Use JSON Schema validator
   - Verify example frontmatter passes

## Getting More Help

### View Detailed Execution

```bash
# Open Temporal UI
open http://localhost:8233

# Navigate to:
# Namespaces > default > Workflows > transform-<task-id>

# Review:
# - Event History (every step)
# - Activity inputs/outputs
# - Error messages
# - Timeline
```

### Enable Debug Logging

```bash
# Start worker with verbose logging
LOG_LEVEL=debug make run-worker
```

### Check Worker Health

```bash
# Worker should show:
# - Connected to Temporal
# - Registered activities
# - Polling for tasks

make run-worker
# Look for: "Worker started successfully"
```

### Common Log Messages

**Good signs:**
```
INFO: Connected to Temporal server
INFO: Registered activity: CloneRepositoryActivity
INFO: Worker started, polling for tasks
```

**Warning signs:**
```
WARN: Failed to connect to Docker
ERROR: Activity registration failed
ERROR: Invalid API key
```

## Still Stuck?

1. **Check example files:**
   - Use provided examples as templates
   - Verify they work before customizing

2. **Review documentation:**
   - [CLI Reference](CLI_REFERENCE.md)
   - [Task File Reference](TASK_FILE_REFERENCE.md)
   - [Examples](examples/)

3. **File an issue:**
   - https://github.com/tinkerloft/fleetlift/issues
   - Include: task file, error logs, Temporal workflow ID

4. **Check Temporal docs:**
   - https://docs.temporal.io
   - For Temporal-specific issues
