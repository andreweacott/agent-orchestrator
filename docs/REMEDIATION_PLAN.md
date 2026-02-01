# Remediation Plan

This document outlines the issues identified during code review of the working directory changes and provides a detailed remediation plan for each issue.

## Summary

| Priority | Issue | Effort | Risk |
|----------|-------|--------|------|
| P1 (High) | Missing Approval Flow in Child Workflows | Medium | High |
| P1 (High) | Backward Compatibility Issue with GetExecutionGroups | Low | Medium |
| P1 (High) | Semaphore Goroutine Leak in Grouped Strategy | Low | Medium |
| P2 (Medium) | Missing Test Coverage for transform_group.go | Medium | Low |
| P2 (Medium) | Validation for groups+repositories Conflict | Low | Low |
| P2 (Medium) | Semaphore Release Race Condition | Low | Low |
| P3 (Low) | Hardcoded MaxParallel Default | Trivial | None |
| P3 (Low) | Container Log File Migration | Low | Low |

---

## P1: High Priority Issues

### Issue 1: Missing Approval Flow in Child Workflows

**Location**: `internal/workflow/transform_group.go:35-312`

**Problem**: The `TransformGroup` child workflow does not implement the approval flow. The `Approved` field is passed in `GroupTransformInput` but never checked. When `require_approval: true` is set with grouped execution, PRs will be created without human review.

**Impact**: Safety mechanism for reviewing changes before PR creation is bypassed in grouped execution mode.

**Remediation Steps**:

1. **Modify `executeGroupedStrategy` in `internal/workflow/transform.go`**:
   - Add approval handling BEFORE spawning child workflows
   - Wait for approval signal at the parent level
   - Only proceed to launch child workflows after approval is received

2. **Implementation**:

```go
// In executeGroupedStrategy, add approval handling before spawning child workflows
func executeGroupedStrategy(ctx workflow.Context, task model.Task, startTime time.Time, signalDone func()) *model.TaskResult {
    logger := workflow.GetLogger(ctx)

    // If approval is required, wait for it at the parent level before spawning children
    if task.RequireApproval {
        approved := false
        cancelled := false

        approveChannel := workflow.GetSignalChannel(ctx, SignalApprove)
        rejectChannel := workflow.GetSignalChannel(ctx, SignalReject)
        cancelChannel := workflow.GetSignalChannel(ctx, SignalCancel)

        selector := workflow.NewSelector(ctx)
        selector.AddReceive(approveChannel, func(c workflow.ReceiveChannel, more bool) {
            c.Receive(ctx, nil)
            approved = true
        })
        selector.AddReceive(rejectChannel, func(c workflow.ReceiveChannel, more bool) {
            c.Receive(ctx, nil)
            cancelled = true
        })
        selector.AddReceive(cancelChannel, func(c workflow.ReceiveChannel, more bool) {
            c.Receive(ctx, nil)
            cancelled = true
        })

        logger.Info("Waiting for approval before grouped execution")

        // Wait for approval with timeout
        ok, _ := workflow.AwaitWithTimeout(ctx, 24*time.Hour, func() bool {
            return approved || cancelled
        })

        if !ok || cancelled || !approved {
            signalDone()
            return &model.TaskResult{
                Status:    "rejected",
                StartTime: startTime,
                EndTime:   workflow.Now(ctx),
            }
        }

        logger.Info("Approval received, proceeding with grouped execution")
    }

    // Continue with existing child workflow spawning logic...
}
```

3. **Update child workflow input**:
   - Set `Approved: true` when spawning child workflows (since approval was already obtained at parent level)

**Verification**:
- Add test case for grouped execution with `require_approval: true`
- Verify PRs are not created until approval signal is received
- Verify rejection properly cancels all pending child workflows

---

### Issue 2: Backward Compatibility Issue with GetExecutionGroups

**Location**: `internal/model/task.go:319-344`

**Problem**: `GetExecutionGroups()` creates one group per repository by default, changing the old default behavior from sequential (all repos in one sandbox) to parallel (each repo in its own sandbox).

**Impact**: Breaking change for users relying on default sequential behavior.

**Remediation Steps**:

1. **Modify `GetExecutionGroups` in `internal/model/task.go`**:

```go
// GetExecutionGroups returns the groups to execute.
// If Groups is explicitly set, returns it.
// Otherwise, returns a single group with all repositories (combined strategy).
// Use explicit groups: definition to enable parallel/grouped execution.
func (t Task) GetExecutionGroups() []RepositoryGroup {
    // If groups are explicitly defined, use them
    if len(t.Groups) > 0 {
        return t.Groups
    }

    // Backward compatibility: single group with all repos (combined/sequential)
    effectiveRepos := t.GetEffectiveRepositories()
    if len(effectiveRepos) == 0 {
        return nil
    }

    // Create one group containing all repositories
    // This maintains backward compatibility with the old sequential behavior
    return []RepositoryGroup{{
        Name:         "default",
        Repositories: effectiveRepos,
    }}
}
```

2. **Update documentation** in `docs/TASK_FILE_REFERENCE.md`:
   - Clarify that without explicit `groups:`, all repositories are processed in a single sandbox
   - Add examples showing how to enable parallel execution with groups

**Verification**:
- Test that tasks without `groups:` defined process all repos in one sandbox
- Test that existing task files continue to work as expected
- Verify `max_parallel` behavior is consistent with new default

---

### Issue 3: Semaphore Goroutine Leak in Grouped Strategy

**Location**: `internal/workflow/transform.go:855-861`

**Problem**: The semaphore pre-fill goroutine sends `maxParallel` items to the channel, but if fewer groups exist than `maxParallel`, some sends will never complete.

**Impact**: Goroutine leak that could accumulate over many workflow executions.

**Remediation Steps**:

1. **Calculate effective parallel count**:

```go
// In executeGroupedStrategy
numGroups := len(task.Groups)
effectiveParallel := maxParallel
if numGroups < effectiveParallel {
    effectiveParallel = numGroups
}

// Pre-fill semaphore with only the tokens we'll actually use
semaphore := workflow.NewChannel(ctx)

workflow.Go(ctx, func(ctx workflow.Context) {
    for i := 0; i < effectiveParallel; i++ {
        semaphore.Send(ctx, struct{}{})
    }
})
```

2. **Consider alternative approaches**:
   - Use Temporal's `workflow.Go` with select for bounded concurrency
   - Batch child workflow launches instead of using semaphore

**Verification**:
- Test with `max_parallel: 10` but only 2 groups
- Verify no goroutine leaks after workflow completion
- Add logging to confirm semaphore token usage

---

## P2: Medium Priority Issues

### Issue 4: Missing Test Coverage for transform_group.go

**Location**: `internal/workflow/transform_group.go` (new file)

**Problem**: The new `TransformGroup` workflow and `buildPromptForGroup` function have no test coverage.

**Impact**: Reduced confidence in new grouped execution feature.

**Remediation Steps**:

1. **Create `internal/workflow/transform_group_test.go`**:

```go
package workflow

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "go.temporal.io/sdk/testsuite"

    "orchestrator/internal/model"
)

func TestBuildPromptForGroup(t *testing.T) {
    tests := []struct {
        name     string
        task     model.Task
        group    model.RepositoryGroup
        expected string
    }{
        {
            name: "single repo group",
            task: model.Task{
                Description: "Fix security issues",
            },
            group: model.RepositoryGroup{
                Name: "backend",
                Repositories: []model.Repository{
                    {Name: "api-server", URL: "github.com/org/api-server"},
                },
            },
            expected: "...",
        },
        // Add more test cases
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := buildPromptForGroup(tt.task, tt.group)
            assert.Contains(t, result, tt.task.Description)
        })
    }
}

func TestTransformGroupWorkflow(t *testing.T) {
    testSuite := &testsuite.WorkflowTestSuite{}
    env := testSuite.NewTestWorkflowEnvironment()

    // Register activities
    env.RegisterActivity(/* activities */)

    // Set up test input
    input := GroupTransformInput{
        Task: model.Task{
            Description: "Test task",
        },
        Group: model.RepositoryGroup{
            Name: "test-group",
            Repositories: []model.Repository{
                {Name: "test-repo", URL: "github.com/org/test-repo"},
            },
        },
        AgentsMD: "# Agents",
        PRTitle:  "Test PR",
        PRDesc:   "Test description",
        Approved: true,
    }

    // Execute workflow
    env.ExecuteWorkflow(TransformGroup, input)

    require.True(t, env.IsWorkflowCompleted())
    require.NoError(t, env.GetWorkflowError())

    var result model.GroupResult
    require.NoError(t, env.GetWorkflowResult(&result))
    assert.Equal(t, "test-group", result.GroupName)
}
```

2. **Add edge case tests**:
   - Empty repositories in group
   - Sandbox creation failure
   - Agent execution failure
   - PR creation failure

**Verification**:
- Run `go test ./internal/workflow/... -v`
- Achieve >80% coverage for new code

---

### Issue 5: Validation for groups+repositories Conflict

**Location**: `internal/config/loader.go:196-296`

**Problem**: No validation when both `groups` and `repositories` are specified. `groups` silently takes precedence.

**Remediation Steps**:

1. **Add validation in `LoadTaskFile`**:

```go
// In the validation section of LoadTaskFile
hasGroups := len(task.Groups) > 0
hasRepositories := len(task.Repositories) > 0

if hasGroups && hasRepositories {
    return nil, errors.New("cannot use both 'groups' and 'repositories' fields; " +
        "use 'groups' for grouped execution or 'repositories' for legacy mode")
}
```

2. **Update documentation** to clarify mutual exclusivity

**Verification**:
- Test that specifying both returns clear error
- Update example files if needed

---

### Issue 6: Semaphore Release Race Condition

**Location**: `internal/workflow/transform.go:869-875`

**Problem**: Deferred semaphore release may not execute properly on workflow cancellation.

**Remediation Steps**:

1. **Use disconnected context for cleanup**:

```go
workflow.Go(ctx, func(gCtx workflow.Context) {
    // Acquire semaphore
    semaphore.Receive(gCtx, nil)

    // Use disconnected context for guaranteed cleanup
    defer func() {
        cleanupCtx, _ := workflow.NewDisconnectedContext(gCtx)
        semaphore.Send(cleanupCtx, struct{}{})
    }()

    // ... rest of logic
})
```

**Verification**:
- Test cancellation during grouped execution
- Verify semaphore is properly released on cancel

---

## P3: Low Priority Issues

### Issue 7: Hardcoded MaxParallel Default

**Location**: `internal/model/task.go:311-316`, CLI flags

**Remediation**:
- Extract to constant: `const DefaultMaxParallel = 5`
- Reference constant in all locations

### Issue 8: Container Log File Migration

**Location**: `internal/sandbox/docker/provider.go:71`

**Remediation**:
- The current implementation already uses `touch` to create the file
- Consider adding a comment explaining the log file purpose
- Document in troubleshooting guide if issues arise with existing containers

---

## Implementation Order

1. **Phase 1: Critical Safety** (Do First)
   - Issue 1: Missing Approval Flow in Child Workflows

2. **Phase 2: Compatibility**
   - Issue 2: Backward Compatibility Issue
   - Issue 5: Validation for groups+repositories

3. **Phase 3: Correctness**
   - Issue 3: Semaphore Goroutine Leak
   - Issue 6: Semaphore Release Race Condition

4. **Phase 4: Quality**
   - Issue 4: Missing Test Coverage
   - Issue 7: Hardcoded MaxParallel
   - Issue 8: Container Log File Migration

---

## Verification Checklist

After implementing all remediations:

- [ ] `make lint` passes with no errors
- [ ] `go test ./...` passes with all tests green
- [ ] `go build ./...` compiles without errors
- [ ] Manual test: grouped execution with `require_approval: true` waits for approval
- [ ] Manual test: task without `groups:` processes all repos in single sandbox
- [ ] Manual test: `max_parallel` correctly limits concurrent groups
- [ ] Documentation reflects actual behavior

---

## Notes

- The overall architecture of the grouped execution model is sound
- The transition from `parallel: bool` to flexible groups is a good improvement
- Documentation updates were comprehensive and well-organized
- Consider adding integration tests for the full grouped execution flow
