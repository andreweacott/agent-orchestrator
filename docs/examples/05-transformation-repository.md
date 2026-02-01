# Example 5: Transformation Repository Pattern

Share reusable skills, tools, and instructions across multiple target repositories.

## Overview

This example demonstrates the **transformation repository pattern**, where:
- One "transformation" repository contains the recipe (skills, tools, docs)
- Multiple "target" repositories are analyzed or transformed
- Claude Code auto-discovers skills from the transformation repo
- All targets share the same tooling and instructions

## Use Case

You're migrating logging across 10+ services. You've built a migration toolkit with:
- Claude Code skills for common patterns
- Analysis scripts and tools
- A `CLAUDE.md` with detailed instructions

You want to apply this toolkit to all target repositories without duplicating it.

## Workspace Layout

```
/workspace/                      # Root (transformation repo)
├── .claude/
│   └── skills/                  # Auto-discovered by Claude Code
│       ├── analyze-logging/
│       └── migrate-logger/
├── CLAUDE.md                    # Instructions for Claude Code
├── bin/
│   └── check-migration.sh       # Helper scripts
├── package.json                 # Transformation repo dependencies
└── targets/                     # Target repos cloned here
    ├── service-a/               # First target
    ├── service-b/               # Second target
    └── service-c/               # Third target
```

Claude Code runs from `/workspace`, sees skills and instructions, and works on targets in `targets/`.

## Task File

**`examples/task-transformation.yaml`**

```yaml
version: 1
id: migrate-logging
title: "Migrate logging to structured format"
mode: transform

# Transformation repository - contains the "recipe"
transformation:
  url: https://github.com/org/migration-toolkit.git
  branch: main
  setup:
    - npm install -g @migration/cli

# Target repositories - what gets transformed
targets:
  - url: https://github.com/org/service-a.git
  - url: https://github.com/org/service-b.git
  - url: https://github.com/org/service-c.git

execution:
  agentic:
    prompt: |
      Use the logging-migration skill from the transformation repository
      to migrate this service to structured logging.

      The transformation repo is at /workspace/
      The target repo is at /workspace/targets/{{.RepoName}}

timeout: 40m
max_parallel: 5  # Process repos concurrently (auto-generates one group per repo)
```

## Transformation Repository Structure

### Example: `migration-toolkit` repo

```
migration-toolkit/
├── .claude/
│   └── skills/
│       ├── analyze-logging/
│       │   └── skill.md         # Auto-discovered skill
│       └── migrate-logger/
│           └── skill.md
├── CLAUDE.md                    # Instructions
├── bin/
│   ├── check-migration.sh
│   └── validate-logs.sh
├── templates/
│   └── logger-config.yml
└── package.json
```

### CLAUDE.md Example

```markdown
# Logging Migration Instructions

This transformation migrates services from unstructured to structured logging.

## Process

1. Run `/analyze-logging` to understand current logging usage
2. Run `/migrate-logger` to convert log statements
3. Run tests to verify migration
4. Run `bin/check-migration.sh` to validate

## Patterns to Convert

- `console.log(...)` → `logger.info({ ... })`
- `console.error(...)` → `logger.error({ ... })`

## Important

- Preserve all log messages
- Add context fields for structured logging
- Follow the format in `templates/logger-config.yml`
```

## Running the Example

```bash
# Run the transformation
./bin/orchestrator run -f examples/task-transformation.yaml

# Claude Code will:
# 1. Clone transformation repo to /workspace/
# 2. Run setup commands (npm install)
# 3. Clone each target to /workspace/targets/
# 4. For each target:
#    - Auto-discover skills from transformation repo
#    - Read CLAUDE.md instructions
#    - Apply migration
#    - Create PR

# Monitor progress
./bin/orchestrator list --status Running

# Get results
./bin/orchestrator result --workflow-id transform-migrate-logging
```

## Combining with forEach

You can use forEach to analyze multiple aspects of each target:

```yaml
transformation:
  url: https://github.com/org/classification-tools.git

targets:
  - url: https://github.com/org/api-server.git
  - url: https://github.com/org/web-client.git

for_each:
  - name: users-endpoint
    context: "Endpoint: GET /api/v1/users"
  - name: orders-endpoint
    context: "Endpoint: POST /api/v1/orders"

execution:
  agentic:
    prompt: |
      Use the endpoint-classification skill to analyze {{.Name}}.
      Search for callers across all targets in /workspace/targets/
```

This creates separate analyses for each endpoint across all target repositories.

## Key Features Demonstrated

- **Reusable Tooling**: One transformation repo, many targets
- **Skill Discovery**: Claude Code finds skills automatically
- **Shared Instructions**: CLAUDE.md applies to all targets
- **Setup Commands**: Install dependencies once
- **Parallel Execution**: Transform multiple repos simultaneously

## Benefits

### Without Transformation Repo (Duplication)
```yaml
# Must duplicate this prompt for every project
prompt: |
  Convert logging like this:
  console.log("message") → logger.info({ msg: "message" })
  [100 more lines of instructions]
```

### With Transformation Repo (Reuse)
```yaml
# Simple, reusable
prompt: |
  Use the logging-migration skill to migrate this service.
```

The complexity lives in the transformation repo, which is:
- Version controlled
- Testable
- Reusable
- Maintainable

## Setup Commands

```yaml
transformation:
  url: https://github.com/org/toolkit.git
  setup:
    # Install CLI tools
    - npm install -g @company/migration-cli

    # Install dependencies
    - npm install

    # Build tools
    - make build

    # Run initialization
    - ./bin/init.sh
```

These run once after cloning the transformation repo.

## Best Practices

1. **Version your transformation repo**
   ```yaml
   transformation:
     url: https://github.com/org/toolkit.git
     branch: v1.2.0  # Use tagged versions
   ```

2. **Document in CLAUDE.md**
   - Clear instructions for Claude Code
   - Examples of patterns to find/replace
   - Testing requirements

3. **Use skills for complex tasks**
   - Break migrations into steps
   - Make skills reusable

4. **Test transformation repo separately**
   - Run it on test repositories first
   - Validate skills work as expected

5. **Keep targets read-only**
   - Transformation repo provides tools
   - Targets are just modified, not mixed

## Report Mode with Transformation Repo

Works great for discovery too:

```yaml
mode: report
transformation:
  url: https://github.com/org/analysis-tools.git

targets:
  - url: https://github.com/org/service-a.git
  - url: https://github.com/org/service-b.git

execution:
  agentic:
    prompt: |
      Use the security-audit skill to analyze this service.
    output:
      schema: {...}
```

Collect consistent reports across all targets using shared analysis tools.

## Troubleshooting

**Skills not found:**
- Verify skills are in `.claude/skills/` directory
- Check skill.md files are properly formatted
- Review Claude Code logs in Temporal UI

**Setup fails:**
- Test setup commands locally first
- Check paths and dependencies
- Use absolute paths where possible

**Targets not found:**
- Targets are cloned to `/workspace/targets/{repo-name}/`
- Use `{{.RepoName}}` template variable for dynamic paths

## Next Steps

- Build a transformation repository for your common migrations
- Combine with [forEach](02-multi-target-discovery.md) for granular analysis
- Use [Report Mode](01-security-audit.md) to audit before transforming

## Related Examples

- [Multi-Target Discovery](02-multi-target-discovery.md) - forEach pattern
- [Code Transformation](03-code-transformation.md) - Basic transform mode
- [Deterministic Transform](04-deterministic-transform.md) - Docker-based alternative
