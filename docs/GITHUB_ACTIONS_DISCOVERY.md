# Lightweight Discovery with GitHub Actions

For one-off discovery tasks that need durability but don't require the full platform, GitHub Actions provides a simple alternative.

## When to Use This Approach

| Scenario | Use GitHub Actions | Use Full Platform |
|----------|-------------------|-------------------|
| One-off research/audit | ✅ | ❌ Overkill |
| Single repo, many targets | ✅ | ❌ Overkill |
| Recurring scheduled audits | ⚠️ Consider platform | ✅ |
| Multi-repo with HITL approval | ❌ | ✅ |

## Basic Discovery Workflow

```yaml
# .github/workflows/discover.yml
name: Discovery Research

on:
  workflow_dispatch:
    inputs:
      targets_file:
        description: 'Path to targets list (one per line)'
        required: true
        default: 'targets.txt'
      prompt_file:
        description: 'Path to prompt/skill file'
        required: true
        default: 'prompts/research.md'
      output_schema:
        description: 'Expected JSON schema (for validation)'
        required: false

jobs:
  discover:
    runs-on: ubuntu-latest
    timeout-minutes: 360  # 6 hours max

    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Setup Node.js
        uses: actions/setup-node@v4
        with:
          node-version: '20'

      - name: Install Claude CLI
        run: npm install -g @anthropic-ai/claude-code

      - name: Run discovery
        env:
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
        run: |
          mkdir -p reports

          prompt=$(cat "${{ inputs.prompt_file }}")
          total=$(wc -l < "${{ inputs.targets_file }}")
          current=0

          while IFS= read -r target || [[ -n "$target" ]]; do
            current=$((current + 1))
            echo "[$current/$total] Analyzing: $target"

            # Create safe filename
            name=$(echo "$target" | tr '/' '_' | tr ' ' '_')

            # Run Claude with the prompt
            claude --print \
              --prompt "$prompt

Target: $target

Output your analysis as JSON." \
              > "reports/${name}.json" 2>&1 || {
                echo "{\"target\": \"$target\", \"error\": \"Analysis failed\"}" > "reports/${name}.json"
              }

          done < "${{ inputs.targets_file }}"

      - name: Aggregate reports
        run: |
          echo "Aggregating $(ls reports/*.json | wc -l) reports..."

          # Merge all JSON reports into one array
          jq -s '.' reports/*.json > aggregated-report.json

          # Generate summary
          jq '{
            total: length,
            successful: [.[] | select(.error == null)] | length,
            failed: [.[] | select(.error != null)] | length,
            reports: .
          }' aggregated-report.json > final-report.json

          echo "=== Summary ==="
          jq '{total, successful, failed}' final-report.json

      - name: Upload reports
        uses: actions/upload-artifact@v4
        with:
          name: discovery-reports-${{ github.run_id }}
          path: |
            reports/
            final-report.json
          retention-days: 30
```

## Example: Unused Endpoint Research

### 1. Create targets file

```text
# targets.txt
/api/v1/users/legacy
/api/v1/orders/deprecated
/api/v1/payments/old-webhook
/api/v2/inventory/bulk-import
# ... 100 endpoints
```

### 2. Create research prompt

```markdown
# prompts/unused-endpoint-research.md

You are analyzing whether an API endpoint is unused and can be safely removed.

## Research Process

1. **Check metrics**: Look for Datadog/Prometheus queries showing request counts
2. **Search for references**: Find callers in this repo and related repos
3. **Review git history**: When was this last modified? By whom?
4. **Assess dependencies**: What would break if removed?

## Output Schema

Return JSON with this structure:
{
  "endpoint": "/api/v1/...",
  "is_unused": true|false,
  "confidence": "high"|"medium"|"low",
  "evidence": [
    "No requests in last 90 days (Datadog)",
    "No references found in frontend repo"
  ],
  "removal_effort": "trivial"|"moderate"|"significant",
  "blockers": ["List any blockers"],
  "recommendation": "remove"|"keep"|"investigate"
}
```

### 3. Run the workflow

```bash
# Trigger via GitHub CLI
gh workflow run discover.yml \
  -f targets_file=targets.txt \
  -f prompt_file=prompts/unused-endpoint-research.md

# Watch progress
gh run watch

# Download results when complete
gh run download <run-id>
```

## Parallel Execution

For faster processing, use a matrix strategy:

```yaml
jobs:
  # Split targets into chunks
  prepare:
    runs-on: ubuntu-latest
    outputs:
      chunks: ${{ steps.split.outputs.chunks }}
    steps:
      - uses: actions/checkout@v4
      - id: split
        run: |
          # Split into chunks of 10
          split -l 10 targets.txt chunk_
          chunks=$(ls chunk_* | jq -R -s -c 'split("\n") | map(select(. != ""))')
          echo "chunks=$chunks" >> $GITHUB_OUTPUT

  # Process each chunk in parallel
  discover:
    needs: prepare
    runs-on: ubuntu-latest
    strategy:
      matrix:
        chunk: ${{ fromJson(needs.prepare.outputs.chunks) }}
      max-parallel: 5  # Respect rate limits
    steps:
      - uses: actions/checkout@v4
      - name: Process chunk
        env:
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
        run: |
          # ... same as above, but process ${{ matrix.chunk }}

      - uses: actions/upload-artifact@v4
        with:
          name: reports-${{ matrix.chunk }}
          path: reports/

  # Aggregate all chunks
  aggregate:
    needs: discover
    runs-on: ubuntu-latest
    steps:
      - uses: actions/download-artifact@v4
        with:
          pattern: reports-*
          merge-multiple: true
          path: all-reports

      - name: Merge reports
        run: |
          jq -s '.' all-reports/*.json > final-report.json

      - uses: actions/upload-artifact@v4
        with:
          name: final-report
          path: final-report.json
```

## With Claude Code Skills

If you have a Claude Code skill file (`.claude/skills/unused-endpoint-research.md`):

```yaml
- name: Run discovery with skill
  env:
    ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
  run: |
    while IFS= read -r target; do
      name=$(echo "$target" | tr '/' '_')

      # Use the skill directly
      claude --print \
        --skill unused-endpoint-research \
        --prompt "Analyze endpoint: $target" \
        > "reports/${name}.json"

    done < targets.txt
```

## Scheduled Discovery

For recurring audits:

```yaml
on:
  schedule:
    - cron: '0 0 * * 0'  # Weekly on Sunday
  workflow_dispatch:  # Allow manual trigger

jobs:
  discover:
    # ... same as above

  notify:
    needs: discover
    runs-on: ubuntu-latest
    steps:
      - uses: actions/download-artifact@v4
        with:
          name: discovery-reports-${{ github.run_id }}

      - name: Post to Slack
        env:
          SLACK_WEBHOOK: ${{ secrets.SLACK_WEBHOOK }}
        run: |
          summary=$(jq -r '"Found \(.total) endpoints: \(.successful) analyzed, \(.failed) failed"' final-report.json)
          curl -X POST "$SLACK_WEBHOOK" \
            -H 'Content-type: application/json' \
            -d "{\"text\": \"Weekly endpoint audit complete: $summary\"}"
```

## Limitations

| Limitation | Workaround |
|------------|------------|
| 6-hour max runtime | Split into multiple workflows |
| No persistent state | Use artifacts + workflow chaining |
| No HITL mid-workflow | Use workflow approvals between jobs |
| 10GB artifact limit | Upload to S3 instead |
| Rate limits | Add delays, use max-parallel |

## Migrating to Full Platform

When you outgrow GitHub Actions:

1. **Same prompt** → becomes `spec.transform.agent.prompt`
2. **targets.txt** → becomes `spec.forEach[]` or `spec.repositories[]`
3. **Output schema** → becomes `spec.transform.agent.outputSchema`
4. **Artifacts** → becomes CRD status or S3 storage
5. **Matrix jobs** → becomes parallel Temporal activities

```yaml
# GitHub Actions version
targets_file: targets.txt
prompt_file: prompts/research.md

# Platform version
apiVersion: codetransform.io/v1alpha1
kind: CodeTransform
spec:
  mode: report
  forEach:
    - name: users-api
      context: "/api/v1/users/legacy"
    # ... generated from targets.txt
  transform:
    agent:
      prompt: |
        # ... contents of prompts/research.md
      outputSchema:
        # ... same schema
```

The migration path is straightforward—the platform adds durability, HITL, and multi-repo coordination on top of the same core concept.
