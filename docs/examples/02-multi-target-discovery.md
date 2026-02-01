# Example 2: Multi-Target Discovery (forEach Mode)

Analyze multiple API endpoints separately within a single repository using forEach iteration.

## Overview

This example demonstrates **forEach mode**, where:
- One repository is analyzed multiple times
- Each iteration focuses on a different target (endpoint, component, etc.)
- Each target produces its own report file
- Template substitution (`{{.Name}}`, `{{.Context}}`) customizes the prompt

## Use Case

You have an API service with multiple endpoints. You want to analyze each endpoint separately for security issues, producing individual reports for each one.

## Task File

**`examples/task-foreach.yaml`**

```yaml
version: 1
id: api-endpoint-audit
title: "API Endpoint Security Audit"
mode: report

repositories:
  - url: https://github.com/org/api-service.git

for_each:
  - name: users-api
    context: "Handles user authentication and profile management"
  - name: orders-api
    context: "Handles order creation and retrieval"
  - name: payments-api
    context: "Handles payment processing"

execution:
  agentic:
    prompt: |
      Analyze the {{.Name}} endpoint.
      Context: {{.Context}}

      Focus on authentication, input validation, and rate limiting.
      Write findings to the appropriate REPORT file.

    output:
      schema:
        type: object
        required: [endpoint_name, security_score]
        properties:
          endpoint_name: {type: string}
          security_score: {type: integer, minimum: 1, maximum: 10}
          vulnerabilities: {type: array}

timeout: 45m  # 15 minutes per target (3 targets)
require_approval: false
```

## Running the Example

```bash
# Run the forEach analysis
./bin/orchestrator run -f examples/task-foreach.yaml

# View all results
./bin/orchestrator reports transform-api-endpoint-audit -o json

# Filter to specific target
./bin/orchestrator reports transform-api-endpoint-audit --target users-api
```

## Output Structure

```json
{
  "repository": "api-service",
  "for_each_results": [
    {
      "target": {
        "name": "users-api",
        "context": "Handles user authentication and profile management"
      },
      "report": {
        "frontmatter": {
          "endpoint_name": "users-api",
          "security_score": 8,
          "vulnerabilities": [...]
        },
        "body": "# Users API Security Analysis\n..."
      }
    },
    {
      "target": {
        "name": "orders-api",
        "context": "Handles order creation and retrieval"
      },
      "report": {
        "frontmatter": {
          "endpoint_name": "orders-api",
          "security_score": 6,
          "vulnerabilities": [...]
        },
        "body": "..."
      }
    }
  ]
}
```

## Report Files Created

When using forEach, Claude Code creates separate report files:

```
/workspace/api-service/
├── REPORT-users-api.md      # First target
├── REPORT-orders-api.md     # Second target
└── REPORT-payments-api.md   # Third target
```

## Template Substitution

The prompt template supports:
- `{{.Name}}` - The target name (e.g., "users-api")
- `{{.Context}}` - The target context field
- Any custom fields you add to the target object

## Key Features Demonstrated

- **forEach Iteration**: Multiple executions per repository
- **Template Substitution**: Dynamic prompts per target
- **Separate Reports**: Each target gets its own REPORT file
- **Structured Context**: Provide guidance for each specific target
- **Time Budgeting**: Timeout covers all iterations

## Best Practices

1. **Keep targets focused** - Each should be a distinct unit of analysis
2. **Provide context** - Help Claude Code understand what to look for
3. **Budget time appropriately** - Multiply per-target time by number of targets
4. **Use descriptive names** - Makes filtering results easier

## Next Steps

- Combine with [Transformation Repository](05-transformation-repository.md) for shared analysis tools
- Use results to prioritize which endpoints need refactoring
- Apply fixes with [Code Transformation](03-code-transformation.md) mode

## Related Examples

- [Security Audit](01-security-audit.md) - Single analysis per repository
- [Transformation Repository](05-transformation-repository.md) - forEach with shared tools
