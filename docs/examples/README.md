# Example Workflows

This directory contains detailed examples demonstrating different features and use cases of the Claude Code Orchestrator.

## Examples

### Report Mode (Discovery)

1. **[Security Audit](01-security-audit.md)** - Analyze authentication implementation across repositories and collect structured findings
2. **[Multi-Target Discovery](02-multi-target-discovery.md)** - Use forEach to analyze multiple API endpoints within a single repository

### Transform Mode (Code Changes)

3. **[Code Transformation with PR Creation](03-code-transformation.md)** - Apply security fixes and create pull requests using Claude Code
4. **[Deterministic Transformation](04-deterministic-transform.md)** - Run reproducible changes using custom Docker images

### Advanced Patterns

5. **[Transformation Repository Pattern](05-transformation-repository.md)** - Share reusable skills and tools across multiple target repositories

## Quick Reference

| Example | Mode | Pattern | Claude Code | Docker | forEach |
|---------|------|---------|-------------|--------|---------|
| Security Audit | Report | Single repo | ✓ | | |
| Multi-Target Discovery | Report | forEach | ✓ | | ✓ |
| Code Transformation | Transform | Single repo | ✓ | | |
| Deterministic Transform | Transform | Single repo | | ✓ | |
| Transformation Repository | Report | Multi-repo | ✓ | | ✓ |

## Getting Started

If you're new to the orchestrator, we recommend:

1. Start with the **Quick Start** in the main [README](../../README.md)
2. Try the smoke test: `./bin/fleetlift run -f examples/smoke-test-discovery.yaml`
3. Review the **[Security Audit](01-security-audit.md)** example for report mode
4. Try the **[Code Transformation](03-code-transformation.md)** example for transform mode

## Need Help?

- [CLI Reference](../CLI_REFERENCE.md) - Complete CLI documentation
- [Task File Reference](../TASK_FILE_REFERENCE.md) - Full YAML specification
- [Troubleshooting](../TROUBLESHOOTING.md) - Common issues and solutions
