# Example 3: Code Transformation with PR Creation

Apply security fixes across multiple repositories and create pull requests using Claude Code.

## Overview

This example demonstrates **transform mode** with **agentic execution**, where:
- Claude Code makes code changes autonomously
- Verifiers validate the changes (tests, linting)
- Human approval is required before creating PRs
- Pull requests are created automatically after approval

## Use Case

You've discovered a JWT token validation vulnerability across multiple services. You want Claude Code to fix it consistently, run tests, and create PRs for review.

## Task File

**`examples/task-agentic.yaml`**

```yaml
version: 1
id: fix-auth-vulnerability
title: "Fix authentication token validation"
mode: transform

repositories:
  - url: https://github.com/org/service-a.git
  - url: https://github.com/org/service-b.git

execution:
  agentic:
    prompt: |
      Fix the JWT token validation vulnerability in this repository.

      The current code doesn't validate token expiration. Update the
      authentication middleware to check token.ExpiresAt.

      Run tests to verify the fix works.

    verifiers:
      - name: test
        command: [go, test, ./...]
      - name: lint
        command: [golangci-lint, run]

pull_request:
  title: "Security: Fix JWT token expiration validation"
  body: |
    ## Summary
    Fixed authentication vulnerability where expired tokens were accepted.

    ## Changes
    - Updated auth middleware to validate token.ExpiresAt
    - Added test coverage for expired tokens
  labels: [security, automated]

timeout: 30m
require_approval: true
```

## Running the Example

```bash
# Start the transformation workflow
./bin/fleetlift run -f examples/task-agentic.yaml

# Monitor progress in Temporal UI
open http://localhost:8233

# Claude Code will:
# 1. Clone repositories
# 2. Make code changes
# 3. Run verifiers (tests, lint)
# 4. Wait for approval...

# Review changes in Temporal UI, then approve
./bin/fleetlift approve --workflow-id transform-fix-auth-vulnerability

# PRs are created automatically after approval
./bin/fleetlift result --workflow-id transform-fix-auth-vulnerability
```

## Workflow Steps

1. **Clone Repository** - Checks out the code
2. **Run Claude Code** - Makes changes based on the prompt
3. **Run Verifiers** - Executes test and lint commands
4. **Wait for Approval** - Pauses for human review (if `require_approval: true`)
5. **Create PR** - Opens pull request with specified title/body/labels

## Verifiers

Verifiers ensure quality before creating PRs:

```yaml
verifiers:
  - name: test
    command: [go, test, ./...]          # Must pass
  - name: lint
    command: [golangci-lint, run]       # Must pass
  - name: build
    command: [go, build, ./...]         # Must compile
```

If any verifier fails, the workflow stops and reports the error. No PR is created.

## Approval Flow

### With Approval (`require_approval: true`)

```
Code Changes → Verifiers Pass → Wait for Human → Approve → Create PR
```

Use this for:
- Security-sensitive changes
- Complex refactorings
- First-time automation

### Without Approval (`require_approval: false`)

```
Code Changes → Verifiers Pass → Create PR (automatic)
```

Use this for:
- Trusted, well-tested changes
- Minor updates (dependency bumps, formatting)
- After validating the approach on a test repo

## Pull Request Configuration

```yaml
pull_request:
  branch_prefix: "fix/"              # Branch name: fix/claude-{timestamp}
  title: "Security: Fix JWT validation"
  body: |
    Detailed description with markdown formatting
  labels: [security, automated]      # GitHub labels
  reviewers: [alice, bob]           # Request reviews
```

## Key Features Demonstrated

- **Agentic Execution**: Claude Code makes intelligent decisions
- **Verifiers**: Automated quality gates
- **Human-in-the-Loop**: Approval before PR creation
- **Multi-Repository**: Same fix across multiple repos
- **PR Automation**: Consistent PR format with labels

## Parallel vs Sequential

```yaml
# Sequential (default) - one PR at a time
repositories:
  - url: https://github.com/org/repo-a.git
  - url: https://github.com/org/repo-b.git

# Parallel - repos processed independently in separate sandboxes
repositories:
  - url: https://github.com/org/repo-a.git
  - url: https://github.com/org/repo-b.git
max_parallel: 5  # Auto-generates one group per repo
```

## Best Practices

1. **Start with approval enabled** - Validate the approach first
2. **Use verifiers** - Catch issues before PR creation
3. **Test on one repo first** - Verify the prompt works
4. **Clear prompts** - Be specific about what to change
5. **Run existing tests** - Ensure changes don't break functionality

## Troubleshooting

**Verifiers fail:**
- Check the command paths are correct
- Ensure dependencies are installed (use `setup:` commands)
- Review error messages in Temporal UI

**Changes not as expected:**
- Refine the prompt with more specific instructions
- Add examples of desired output
- Specify file paths or patterns to focus on

## Next Steps

- Start with [Deterministic Transform](04-deterministic-transform.md) for predictable changes
- Use [Security Audit](01-security-audit.md) to discover issues before fixing
- Explore [Transformation Repository](05-transformation-repository.md) for reusable fixes

## Related Examples

- [Deterministic Transformation](04-deterministic-transform.md) - Docker-based reproducible changes
- [Security Audit](01-security-audit.md) - Discover issues before fixing
