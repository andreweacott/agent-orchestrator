# Example 4: Deterministic Transformation

Run reproducible code transformations using custom Docker images (like OpenRewrite).

## Overview

This example demonstrates **deterministic execution**, where:
- A pre-built Docker image performs the transformation
- Changes are reproducible and predictable
- No AI decision-making (unlike agentic mode)
- Faster execution for well-defined changes

## Use Case

You need to upgrade Log4j 1.x to 2.x across multiple Java services. You have a proven OpenRewrite recipe that works reliably. You want fast, reproducible results.

## Task File

**`examples/task-deterministic.yaml`**

```yaml
version: 1
id: log4j-upgrade
title: "Upgrade Log4j 1.x to 2.x"
mode: transform

repositories:
  - url: https://github.com/org/java-service-a.git
  - url: https://github.com/org/java-service-b.git

execution:
  deterministic:
    image: openrewrite/rewrite:latest
    args:
      - "rewrite:run"
      - "-Drewrite.activeRecipes=org.openrewrite.java.logging.log4j.Log4j1ToLog4j2"
    env:
      MAVEN_OPTS: "-Xmx2g"

    verifiers:
      - name: build
        command: [mvn, compile]

timeout: 20m
require_approval: false

pull_request:
  branch_prefix: "security/log4j-upgrade"
  title: "Security: Upgrade Log4j 1.x to 2.x"
  labels: [security, automated, dependencies]
```

## Running the Example

```bash
# Run the deterministic transformation
./bin/orchestrator run -f examples/task-deterministic.yaml

# No approval needed - PRs created automatically after verifiers pass
./bin/orchestrator result --workflow-id transform-log4j-upgrade
```

## Docker Image Requirements

Your Docker image should:

1. **Accept the repository at `/workspace`**
   ```dockerfile
   WORKDIR /workspace
   ```

2. **Make changes in place**
   ```dockerfile
   CMD ["rewrite:run"]  # Modifies files in /workspace
   ```

3. **Exit with status code**
   - `0` = success
   - Non-zero = failure

## Example Docker Images

### OpenRewrite (Java)
```yaml
deterministic:
  image: openrewrite/rewrite:latest
  args: ["rewrite:run", "-Drewrite.activeRecipes=..."]
```

### Codemod (JavaScript)
```yaml
deterministic:
  image: your-registry/codemod-runner:latest
  args: ["--transform", "migrate-to-react-18"]
```

### Custom Script
```yaml
deterministic:
  image: your-registry/dependency-updater:latest
  args: ["--update-patch-versions"]
  env:
    UPDATE_MODE: "conservative"
```

## Verifiers with Deterministic Mode

Even with deterministic transforms, verifiers ensure quality:

```yaml
verifiers:
  - name: build
    command: [mvn, compile]      # Java
  - name: test
    command: [npm, test]         # JavaScript
  - name: typecheck
    command: [tsc, --noEmit]     # TypeScript
```

## When to Use Deterministic vs Agentic

### Use Deterministic When:
- ✅ You have a proven, tested transformation tool
- ✅ The change is well-defined and repeatable
- ✅ You need fast, consistent results
- ✅ The transformation is mechanical (rename, restructure, upgrade)

**Examples:**
- Dependency upgrades (OpenRewrite, Dependabot-style)
- Code formatting (Prettier, Black, gofmt)
- Import reorganization
- API migrations with codemods

### Use Agentic When:
- ✅ The change requires understanding context
- ✅ Each repository might need different approaches
- ✅ You need intelligent decision-making
- ✅ The task is exploratory or creative

**Examples:**
- Security vulnerability fixes
- Refactoring based on code patterns
- Adding features with tests
- Complex migrations

## Building a Custom Docker Image

```dockerfile
FROM maven:3.9-eclipse-temurin-17

# Install your transformation tool
RUN apt-get update && apt-get install -y jq

# Copy transformation scripts
COPY transform.sh /usr/local/bin/transform
RUN chmod +x /usr/local/bin/transform

WORKDIR /workspace

# Default command runs the transformation
ENTRYPOINT ["/usr/local/bin/transform"]
```

Then use it:

```yaml
execution:
  deterministic:
    image: your-registry/custom-transformer:v1.0
    args: ["--mode", "safe"]
```

## Key Features Demonstrated

- **Docker-Based Execution**: Reproducible environment
- **No AI Required**: Faster, more predictable
- **Custom Tools**: Use any transformation tool
- **Verifiers**: Still validate results
- **No Approval Needed**: Safe for proven transformations

## Approval Strategy

```yaml
# First run: Enable approval to verify
require_approval: true

# After validating on test repos: Disable for speed
require_approval: false
```

## Performance

Deterministic mode is typically faster than agentic:

- **Agentic**: 5-15 minutes per repo (AI reasoning + execution)
- **Deterministic**: 1-3 minutes per repo (just execution)

Perfect for large-scale changes across many repositories.

## Best Practices

1. **Test your Docker image locally first**
   ```bash
   docker run -v $(pwd):/workspace your-image:tag
   ```

2. **Use verifiers** - Even deterministic transforms can break things

3. **Version your images** - Use tags like `v1.2.3`, not `latest`

4. **Keep images small** - Faster to pull and start

5. **Log verbosely** - Helps debug when things fail

## Troubleshooting

**Docker image not found:**
- Verify the image exists and is accessible
- Check registry authentication if using private registry

**Transformation fails:**
- Run the Docker image locally to debug
- Check logs in Temporal UI for error messages
- Ensure the image has correct permissions

**No changes made:**
- Verify the transformation tool is configured correctly
- Check that args are passed properly
- Review tool documentation for correct usage

## Next Steps

- Build custom images for your specific transformations
- Combine with [forEach mode](02-multi-target-discovery.md) for granular control
- Use [Transformation Repository](05-transformation-repository.md) for shared tooling

## Related Examples

- [Code Transformation](03-code-transformation.md) - Agentic alternative
- [Transformation Repository](05-transformation-repository.md) - Shared tools pattern
