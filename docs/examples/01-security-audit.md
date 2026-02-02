# Example 1: Security Audit (Report Mode)

Analyze authentication implementation across repositories and collect structured findings.

## Overview

This example demonstrates **report mode**, where the orchestrator:
- Analyzes code without making changes
- Collects structured data with JSON Schema validation
- Produces reports with YAML frontmatter + markdown body
- Does not create pull requests

## Use Case

You want to audit authentication implementation across multiple services before planning a migration. You need structured data (which auth library, security score, issues) plus detailed analysis.

## Task File

**`examples/task-report.yaml`**

```yaml
version: 1
id: security-audit-2024
title: "Authentication Security Audit"
mode: report

repositories:
  - url: https://github.com/org/service-a.git
  - url: https://github.com/org/service-b.git

execution:
  agentic:
    prompt: |
      Analyze this repository's authentication implementation.
      Write findings to /workspace/REPORT.md with YAML frontmatter.

    output:
      schema:
        type: object
        required: [auth_library, score]
        properties:
          auth_library: {type: string}
          score: {type: integer, minimum: 1, maximum: 10}
          issues:
            type: array
            items:
              type: object
              properties:
                severity: {type: string, enum: [low, medium, high, critical]}
                description: {type: string}

timeout: 15m
require_approval: false
```

## Running the Example

```bash
# Run the audit
./bin/fleetlift run -f examples/task-report.yaml

# View results in table format
./bin/fleetlift reports transform-security-audit-2024

# Get full JSON output
./bin/fleetlift reports transform-security-audit-2024 -o json > audit-results.json

# Get only the structured data (frontmatter)
./bin/fleetlift reports transform-security-audit-2024 --frontmatter-only -o json
```

## Output Structure

```json
{
  "repository": "service-a",
  "frontmatter": {
    "auth_library": "jwt",
    "score": 7,
    "issues": [
      {
        "severity": "medium",
        "description": "Token expiration not checked in middleware"
      }
    ]
  },
  "body": "# Security Audit Report\n\n## Overview\n..."
}
```

## Key Features Demonstrated

- **Report Mode**: No PRs created, only data collection
- **JSON Schema Validation**: Ensures consistent output structure
- **YAML Frontmatter**: Structured data for programmatic processing
- **Multi-Repository**: Runs the same analysis across multiple repos
- **No Approval Required**: Set `require_approval: false` for automated runs

## Next Steps

- Use the collected data to prioritize migration work
- Feed results into dashboards or tracking systems
- Combine with [forEach mode](02-multi-target-discovery.md) for more granular analysis

## Related Examples

- [Multi-Target Discovery](02-multi-target-discovery.md) - Analyze multiple endpoints within one repo
- [Code Transformation](03-code-transformation.md) - Apply fixes based on audit findings
