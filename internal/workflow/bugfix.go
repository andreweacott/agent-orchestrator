// Package workflow contains Temporal workflow definitions.
package workflow

import (
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/anthropics/claude-code-orchestrator/internal/activity"
	"github.com/anthropics/claude-code-orchestrator/internal/model"
)

// Signal and query names
const (
	SignalApprove = "approve"
	SignalReject  = "reject"
	SignalCancel  = "cancel"
	QueryStatus   = "get_status"
	QueryResult   = "get_claude_result"
)

// BugFix is the main workflow for fixing bugs using Claude Code.
func BugFix(ctx workflow.Context, task model.BugFixTask) (*model.BugFixResult, error) {
	logger := workflow.GetLogger(ctx)
	startTime := workflow.Now(ctx)

	// Workflow state
	var (
		status                = model.TaskStatusPending
		sandbox               *model.SandboxInfo
		claudeResult          *model.ClaudeCodeResult
		approved              *bool
		cancellationRequested bool
	)

	// Register query handlers
	if err := workflow.SetQueryHandler(ctx, QueryStatus, func() (model.TaskStatus, error) {
		return status, nil
	}); err != nil {
		return nil, fmt.Errorf("failed to register status query: %w", err)
	}

	if err := workflow.SetQueryHandler(ctx, QueryResult, func() (*model.ClaudeCodeResult, error) {
		return claudeResult, nil
	}); err != nil {
		return nil, fmt.Errorf("failed to register result query: %w", err)
	}

	// Set up signal channels
	approveChannel := workflow.GetSignalChannel(ctx, SignalApprove)
	rejectChannel := workflow.GetSignalChannel(ctx, SignalReject)
	cancelChannel := workflow.GetSignalChannel(ctx, SignalCancel)

	// BUG-003 Fix: Use done channel to signal goroutine termination
	doneChannel := workflow.NewChannel(ctx)
	var signalHandlerDone bool

	// Handle signals asynchronously
	workflow.Go(ctx, func(ctx workflow.Context) {
		for !signalHandlerDone {
			selector := workflow.NewSelector(ctx)

			selector.AddReceive(doneChannel, func(c workflow.ReceiveChannel, more bool) {
				c.Receive(ctx, nil)
				signalHandlerDone = true
			})

			selector.AddReceive(approveChannel, func(c workflow.ReceiveChannel, more bool) {
				c.Receive(ctx, nil)
				logger.Info("Received approval signal")
				val := true
				approved = &val
			})

			selector.AddReceive(rejectChannel, func(c workflow.ReceiveChannel, more bool) {
				c.Receive(ctx, nil)
				logger.Info("Received rejection signal")
				val := false
				approved = &val
			})

			selector.AddReceive(cancelChannel, func(c workflow.ReceiveChannel, more bool) {
				c.Receive(ctx, nil)
				logger.Info("Received cancellation signal")
				cancellationRequested = true
			})

			selector.Select(ctx)
		}
	})

	// Helper to signal done to the signal handler goroutine (BUG-003)
	signalDone := func() {
		doneChannel.Send(ctx, struct{}{})
	}

	// Helper to create failed result
	failedResult := func(errMsg string) *model.BugFixResult {
		signalDone()
		duration := workflow.Now(ctx).Sub(startTime).Seconds()
		return &model.BugFixResult{
			TaskID:          task.TaskID,
			Status:          model.TaskStatusFailed,
			Error:           &errMsg,
			DurationSeconds: &duration,
		}
	}

	// Helper to create cancelled result
	cancelledResult := func() *model.BugFixResult {
		signalDone()
		duration := workflow.Now(ctx).Sub(startTime).Seconds()
		errMsg := "Workflow cancelled"
		return &model.BugFixResult{
			TaskID:          task.TaskID,
			Status:          model.TaskStatusCancelled,
			Error:           &errMsg,
			DurationSeconds: &duration,
		}
	}

	// Retry policy for activities
	retryPolicy := &temporal.RetryPolicy{
		InitialInterval:    time.Second,
		MaximumInterval:    time.Minute,
		BackoffCoefficient: 2.0,
		MaximumAttempts:    3,
	}

	// Default activity options
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy:         retryPolicy,
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Ensure cleanup runs even if workflow fails
	defer func() {
		if sandbox != nil {
			// Use disconnected context for cleanup
			cleanupCtx, _ := workflow.NewDisconnectedContext(ctx)
			cleanupOptions := workflow.ActivityOptions{
				StartToCloseTimeout: 2 * time.Minute,
				RetryPolicy:         retryPolicy,
			}
			cleanupCtx = workflow.WithActivityOptions(cleanupCtx, cleanupOptions)

			var cleanupErr error
			_ = workflow.ExecuteActivity(cleanupCtx, activity.ActivityCleanupSandbox, sandbox.ContainerID).Get(cleanupCtx, &cleanupErr)
			if cleanupErr != nil {
				logger.Error("Cleanup failed", "error", cleanupErr)
			}
		}
	}()

	// 1. Provision sandbox
	status = model.TaskStatusProvisioning
	logger.Info("Starting bug fix workflow", "taskID", task.TaskID)

	if err := workflow.ExecuteActivity(ctx, activity.ActivityProvisionSandbox, task.TaskID).Get(ctx, &sandbox); err != nil {
		return failedResult(fmt.Sprintf("Failed to provision sandbox: %v", err)), nil
	}

	// 2. Clone repositories
	status = model.TaskStatusCloning
	agentsMD := generateAgentsMD(task)

	cloneOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		HeartbeatTimeout:    2 * time.Minute,
		RetryPolicy:         retryPolicy,
	}
	cloneCtx := workflow.WithActivityOptions(ctx, cloneOptions)

	var clonedPaths []string
	if err := workflow.ExecuteActivity(cloneCtx, activity.ActivityCloneRepositories, *sandbox, task.Repositories, agentsMD).Get(cloneCtx, &clonedPaths); err != nil {
		return failedResult(fmt.Sprintf("Failed to clone repositories: %v", err)), nil
	}

	// 3. Run Claude Code
	status = model.TaskStatusRunning
	prompt := buildPrompt(task)

	claudeOptions := workflow.ActivityOptions{
		StartToCloseTimeout: time.Duration(task.TimeoutMinutes+5) * time.Minute,
		HeartbeatTimeout:    5 * time.Minute,
		RetryPolicy:         retryPolicy,
	}
	claudeCtx := workflow.WithActivityOptions(ctx, claudeOptions)

	if err := workflow.ExecuteActivity(claudeCtx, activity.ActivityRunClaudeCode, sandbox.ContainerID, prompt, task.TimeoutMinutes*60).Get(claudeCtx, &claudeResult); err != nil {
		return failedResult(fmt.Sprintf("Failed to run Claude Code: %v", err)), nil
	}

	if !claudeResult.Success {
		errMsg := "Claude Code execution failed"
		if claudeResult.Error != nil {
			errMsg = *claudeResult.Error
		}
		return failedResult(errMsg), nil
	}

	// 4. Handle clarification if needed
	if claudeResult.NeedsClarification {
		if task.SlackChannel != nil {
			msg := fmt.Sprintf("Claude needs clarification for %s:\n\n%s",
				task.TaskID, *claudeResult.ClarificationQuestion)
			_ = workflow.ExecuteActivity(ctx, activity.ActivityNotifySlack, *task.SlackChannel, msg, (*string)(nil)).Get(ctx, nil)
		}

		status = model.TaskStatusAwaitingApproval

		// Wait for approval or cancellation
		ok, err := workflow.AwaitWithTimeout(ctx, 24*time.Hour, func() bool {
			return approved != nil || cancellationRequested
		})
		if err != nil || !ok {
			return failedResult("Timeout waiting for clarification response"), nil
		}
		if cancellationRequested {
			return cancelledResult(), nil
		}
	}

	// 5. Human approval for changes (if required)
	if task.RequireApproval && len(claudeResult.FilesModified) > 0 {
		status = model.TaskStatusAwaitingApproval

		if task.SlackChannel != nil {
			// Get changes summary
			summary := getChangesSummary(ctx, sandbox.ContainerID, task.Repositories)
			msg := fmt.Sprintf("Claude completed %s. Changes:\n```\n%s\n```\nReply with 'approve' or 'reject'",
				task.TaskID, summary)
			_ = workflow.ExecuteActivity(ctx, activity.ActivityNotifySlack, *task.SlackChannel, msg, (*string)(nil)).Get(ctx, nil)
		}

		// Wait for approval with 24hr timeout
		ok, err := workflow.AwaitWithTimeout(ctx, 24*time.Hour, func() bool {
			return approved != nil || cancellationRequested
		})
		if err != nil {
			return failedResult(fmt.Sprintf("Error waiting for approval: %v", err)), nil
		}
		if !ok {
			signalDone()
			errMsg := "Approval timeout (24 hours)"
			duration := workflow.Now(ctx).Sub(startTime).Seconds()
			return &model.BugFixResult{
				TaskID:          task.TaskID,
				Status:          model.TaskStatusCancelled,
				Error:           &errMsg,
				DurationSeconds: &duration,
			}, nil
		}
		if cancellationRequested {
			return cancelledResult(), nil
		}
		if approved != nil && !*approved {
			signalDone()
			errMsg := "Changes rejected"
			duration := workflow.Now(ctx).Sub(startTime).Seconds()
			return &model.BugFixResult{
				TaskID:          task.TaskID,
				Status:          model.TaskStatusCancelled,
				Error:           &errMsg,
				DurationSeconds: &duration,
			}, nil
		}
	}

	// 6. Run verifiers as final gate
	if len(task.Verifiers) > 0 && len(claudeResult.FilesModified) > 0 {
		logger.Info("Running verifiers as final gate")

		verifierOptions := workflow.ActivityOptions{
			StartToCloseTimeout: 10 * time.Minute,
			HeartbeatTimeout:    2 * time.Minute,
			RetryPolicy:         retryPolicy,
		}
		verifierCtx := workflow.WithActivityOptions(ctx, verifierOptions)

		var verifiersResult *model.VerifiersResult
		if err := workflow.ExecuteActivity(verifierCtx, activity.ActivityRunVerifiers, *sandbox, task.Repositories, task.Verifiers).Get(verifierCtx, &verifiersResult); err != nil {
			return failedResult(fmt.Sprintf("Failed to run verifiers: %v", err)), nil
		}

		if !verifiersResult.AllPassed {
			var failedVerifiers []string
			for _, r := range verifiersResult.Results {
				if !r.Success {
					failedVerifiers = append(failedVerifiers, r.Name)
				}
			}
			return failedResult(fmt.Sprintf("Verifiers failed: %s", strings.Join(failedVerifiers, ", "))), nil
		}

		logger.Info("All verifiers passed")
	}

	// 7. Create pull requests
	status = model.TaskStatusCreatingPRs

	var pullRequests []model.PullRequest
	for _, repo := range task.Repositories {
		prDesc := buildPRDescription(task, claudeResult)

		var pr *model.PullRequest
		if err := workflow.ExecuteActivity(ctx, activity.ActivityCreatePullRequest,
			sandbox.ContainerID, repo, task.TaskID,
			fmt.Sprintf("fix: %s", task.Title), prDesc).Get(ctx, &pr); err != nil {
			return failedResult(fmt.Sprintf("Failed to create PR: %v", err)), nil
		}

		if pr != nil {
			pullRequests = append(pullRequests, *pr)
		}
	}

	// 8. Notify completion
	if task.SlackChannel != nil && len(pullRequests) > 0 {
		var prLinks []string
		for _, pr := range pullRequests {
			prLinks = append(prLinks, fmt.Sprintf("- %s", pr.PRURL))
		}
		msg := fmt.Sprintf("Pull requests created for %s:\n%s",
			task.TaskID, strings.Join(prLinks, "\n"))
		_ = workflow.ExecuteActivity(ctx, activity.ActivityNotifySlack, *task.SlackChannel, msg, (*string)(nil)).Get(ctx, nil)
	}

	status = model.TaskStatusCompleted
	signalDone()
	duration := workflow.Now(ctx).Sub(startTime).Seconds()

	return &model.BugFixResult{
		TaskID:          task.TaskID,
		Status:          model.TaskStatusCompleted,
		PullRequests:    pullRequests,
		DurationSeconds: &duration,
	}, nil
}

// generateAgentsMD creates the AGENTS.md content for the workspace.
func generateAgentsMD(task model.BugFixTask) string {
	var sb strings.Builder

	sb.WriteString("# Agent Instructions\n\n")
	sb.WriteString(fmt.Sprintf("## Task: %s\n\n", task.Title))
	sb.WriteString(fmt.Sprintf("**Task ID:** %s\n\n", task.TaskID))

	if task.Description != "" {
		sb.WriteString(fmt.Sprintf("**Description:**\n%s\n\n", task.Description))
	}

	if task.TicketURL != nil {
		sb.WriteString(fmt.Sprintf("**Related Ticket:** %s\n\n", *task.TicketURL))
	}

	sb.WriteString("## Repositories\n\n")
	for _, repo := range task.Repositories {
		sb.WriteString(fmt.Sprintf("- `%s` (branch: %s)\n", repo.Name, repo.Branch))
	}

	sb.WriteString("\n## Guidelines\n\n")
	sb.WriteString("1. Focus on the specific task described above\n")
	sb.WriteString("2. Make minimal, targeted changes\n")
	sb.WriteString("3. Follow existing code style and patterns\n")
	sb.WriteString("4. Add tests if applicable\n")
	sb.WriteString("5. Do not modify unrelated files\n")

	return sb.String()
}

// buildPrompt creates the prompt for Claude Code.
func buildPrompt(task model.BugFixTask) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Task: %s\n\n", task.Title))

	if task.Description != "" {
		sb.WriteString(fmt.Sprintf("Description:\n%s\n\n", task.Description))
	}

	if task.TicketURL != nil {
		sb.WriteString(fmt.Sprintf("Related ticket: %s\n\n", *task.TicketURL))
	}

	sb.WriteString("Repositories to work on:\n")
	for _, repo := range task.Repositories {
		sb.WriteString(fmt.Sprintf("- %s (in /workspace/%s)\n", repo.Name, repo.Name))
	}

	sb.WriteString("\nPlease analyze the codebase and implement the necessary fix. ")
	sb.WriteString("Follow the existing code style and patterns. ")
	sb.WriteString("Make minimal, targeted changes to address the issue.")

	// Append verifier instructions if verifiers are defined
	if len(task.Verifiers) > 0 {
		sb.WriteString("\n\n## Verification\n\n")
		sb.WriteString("After making changes, verify your work by running these commands:\n\n")
		for _, v := range task.Verifiers {
			sb.WriteString(fmt.Sprintf("- **%s**: `%s`\n", v.Name, strings.Join(v.Command, " ")))
		}
		sb.WriteString("\nFix any errors before completing the task. All verifiers must pass.")
	}

	return sb.String()
}

// buildPRDescription creates the PR description.
func buildPRDescription(task model.BugFixTask, result *model.ClaudeCodeResult) string {
	var sb strings.Builder

	sb.WriteString("## Summary\n\n")
	sb.WriteString(fmt.Sprintf("Automated fix for: %s\n\n", task.Title))

	if task.Description != "" {
		sb.WriteString(fmt.Sprintf("**Original issue:**\n%s\n\n", task.Description))
	}

	if task.TicketURL != nil {
		sb.WriteString(fmt.Sprintf("**Related ticket:** %s\n\n", *task.TicketURL))
	}

	sb.WriteString("## Changes\n\n")
	if result != nil && len(result.FilesModified) > 0 {
		sb.WriteString("Modified files:\n")
		for _, f := range result.FilesModified {
			sb.WriteString(fmt.Sprintf("- `%s`\n", f))
		}
	}

	sb.WriteString("\n---\n")
	sb.WriteString("*This PR was automatically generated by Claude Code Orchestrator*\n")

	return sb.String()
}

// getChangesSummary gets a summary of changes for notification.
func getChangesSummary(ctx workflow.Context, containerID string, repos []model.Repository) string {
	var summaries []string

	for _, repo := range repos {
		var output map[string]string
		err := workflow.ExecuteActivity(ctx, activity.ActivityGetClaudeOutput, containerID, repo.Name).Get(ctx, &output)
		if err != nil {
			continue
		}

		if status, ok := output["status"]; ok && strings.TrimSpace(status) != "" {
			summaries = append(summaries, fmt.Sprintf("%s:\n%s", repo.Name, status))
		}
	}

	if len(summaries) == 0 {
		return "No changes detected"
	}

	return strings.Join(summaries, "\n\n")
}
