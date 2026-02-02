// Package workflow contains Temporal workflow definitions.
package workflow

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/tinkerloft/fleetlift/internal/activity"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// Signal and query names
const (
	SignalApprove = "approve"
	SignalReject  = "reject"
	SignalCancel  = "cancel"
	QueryStatus   = "get_status"
	QueryResult   = "get_claude_result"
)

// Transform is the main workflow for code transformations.
// It supports both agentic (Claude Code) and deterministic (Docker) transformations.
func Transform(ctx workflow.Context, task model.Task) (*model.TaskResult, error) {
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
	failedResult := func(errMsg string) *model.TaskResult {
		signalDone()
		duration := workflow.Now(ctx).Sub(startTime).Seconds()
		return &model.TaskResult{
			TaskID:          task.ID,
			Status:          model.TaskStatusFailed,
			Error:           &errMsg,
			DurationSeconds: &duration,
		}
	}

	// Helper to create cancelled result
	cancelledResult := func() *model.TaskResult {
		signalDone()
		duration := workflow.Now(ctx).Sub(startTime).Seconds()
		errMsg := "Workflow cancelled"
		return &model.TaskResult{
			TaskID:          task.ID,
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

	// Get execution type and timeout
	executionType := task.Execution.GetExecutionType()
	timeoutMinutes := task.GetTimeoutMinutes()

	// Get effective repositories and execution groups
	effectiveRepos := task.GetEffectiveRepositories()
	executionGroups := task.GetExecutionGroups()

	// Branch based on number of groups for parallel execution
	// Both transform and report modes support grouped execution when multiple groups are defined
	if len(executionGroups) > 1 {
		logger.Info("Using multi-group execution", "groups", len(executionGroups), "maxParallel", task.GetMaxParallel(), "mode", task.GetMode())
		return executeGroupedStrategy(ctx, task, startTime, signalDone), nil
	}

	// Single-group execution: one sandbox with all repos (combined strategy)
	logger.Info("Using single-sandbox execution", "repos", len(effectiveRepos))

	// 1. Provision sandbox
	status = model.TaskStatusProvisioning
	logger.Info("Starting transform workflow", "taskID", task.ID)

	if err := workflow.ExecuteActivity(ctx, activity.ActivityProvisionSandbox, task.ID).Get(ctx, &sandbox); err != nil {
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

	// Build clone input based on transformation vs legacy mode
	// Use effectiveRepos to include repos from groups when applicable
	cloneInput := activity.CloneRepositoriesInput{
		SandboxInfo:    *sandbox,
		AgentsMD:       agentsMD,
		Transformation: task.Transformation,
		Targets:        task.Targets,
		Repositories:   effectiveRepos,
	}

	var clonedPaths []string
	if err := workflow.ExecuteActivity(cloneCtx, activity.ActivityCloneRepositories, cloneInput).Get(cloneCtx, &clonedPaths); err != nil {
		return failedResult(fmt.Sprintf("Failed to clone repositories: %v", err)), nil
	}

	// 3. Run transformation (Claude Code OR Deterministic)
	status = model.TaskStatusRunning

	var filesModified []string
	verifiers := task.Execution.GetVerifiers()

	// Validate deterministic mode has required image
	if executionType == model.ExecutionTypeDeterministic {
		if task.Execution.Deterministic == nil || task.Execution.Deterministic.Image == "" {
			return failedResult("Deterministic execution requires image to be set"), nil
		}
	}

	if executionType == model.ExecutionTypeDeterministic {
		// Run deterministic transformation
		logger.Info("Running deterministic transformation", "image", task.Execution.Deterministic.Image)

		deterministicOptions := workflow.ActivityOptions{
			StartToCloseTimeout: time.Duration(timeoutMinutes+5) * time.Minute,
			HeartbeatTimeout:    5 * time.Minute,
			RetryPolicy:         retryPolicy,
		}
		deterministicCtx := workflow.WithActivityOptions(ctx, deterministicOptions)

		var deterministicResult *model.DeterministicResult
		err := workflow.ExecuteActivity(deterministicCtx, activity.ActivityExecuteDeterministic,
			*sandbox, task.Execution.Deterministic.Image, task.Execution.Deterministic.Args,
			task.Execution.Deterministic.Env, effectiveRepos).Get(deterministicCtx, &deterministicResult)

		if err != nil {
			return failedResult(fmt.Sprintf("Failed to run deterministic transformation: %v", err)), nil
		}

		if !deterministicResult.Success {
			errMsg := "Deterministic transformation failed"
			if deterministicResult.Error != nil {
				errMsg = *deterministicResult.Error
			}
			return failedResult(errMsg), nil
		}

		filesModified = deterministicResult.FilesModified

		// Skip PR if no changes
		if len(filesModified) == 0 {
			logger.Info("No files modified by deterministic transformation")
			signalDone()
			duration := workflow.Now(ctx).Sub(startTime).Seconds()
			// Build repository results with no PRs
			var repoResults []model.RepositoryResult
			for _, repo := range effectiveRepos {
				repoResults = append(repoResults, model.RepositoryResult{
					Repository: repo.Name,
					Status:     "success",
				})
			}
			return &model.TaskResult{
				TaskID:          task.ID,
				Status:          model.TaskStatusCompleted,
				Repositories:    repoResults,
				DurationSeconds: &duration,
			}, nil
		}

		logger.Info("Deterministic transformation completed", "filesModified", len(filesModified))

		// Note: Skip human approval for deterministic mode (transforms are pre-vetted)
	} else {
		// Run Claude Code (agentic mode - default)
		if task.Execution.Agentic == nil || task.Execution.Agentic.Prompt == "" {
			return failedResult("Agentic execution requires prompt to be set"), nil
		}

		// For forEach mode in report tasks, skip the single Claude Code run here.
		// Claude Code will be run per-target in the report collection section.
		isForEachMode := task.GetMode() == model.TaskModeReport && len(task.ForEach) > 0

		if !isForEachMode {
			prompt := buildPrompt(task)

			claudeOptions := workflow.ActivityOptions{
				StartToCloseTimeout: time.Duration(timeoutMinutes+5) * time.Minute,
				HeartbeatTimeout:    5 * time.Minute,
				RetryPolicy:         retryPolicy,
			}
			claudeCtx := workflow.WithActivityOptions(ctx, claudeOptions)

			if err := workflow.ExecuteActivity(claudeCtx, activity.ActivityRunClaudeCode, sandbox.ContainerID, prompt, timeoutMinutes*60).Get(claudeCtx, &claudeResult); err != nil {
				return failedResult(fmt.Sprintf("Failed to run Claude Code: %v", err)), nil
			}

			if !claudeResult.Success {
				errMsg := "Claude Code execution failed"
				if claudeResult.Error != nil {
					errMsg = *claudeResult.Error
				}
				return failedResult(errMsg), nil
			}

			filesModified = claudeResult.FilesModified

			// 4. Handle clarification if needed (agentic mode only)
			if claudeResult.NeedsClarification {
				if task.SlackChannel != nil {
					msg := fmt.Sprintf("Claude needs clarification for %s:\n\n%s",
						task.ID, *claudeResult.ClarificationQuestion)
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

			// 5. Human approval for changes (agentic mode only, if required)
			if task.RequireApproval && len(filesModified) > 0 {
				status = model.TaskStatusAwaitingApproval

				if task.SlackChannel != nil {
					// Get changes summary
					summary := getChangesSummary(ctx, sandbox.ContainerID, effectiveRepos)
					msg := fmt.Sprintf("Claude completed %s. Changes:\n```\n%s\n```\nReply with 'approve' or 'reject'",
						task.ID, summary)
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
					return &model.TaskResult{
						TaskID:          task.ID,
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
					return &model.TaskResult{
						TaskID:          task.ID,
						Status:          model.TaskStatusCancelled,
						Error:           &errMsg,
						DurationSeconds: &duration,
					}, nil
				}
			}
		}
	}

	// 6. Run verifiers as final gate
	if len(verifiers) > 0 && len(filesModified) > 0 {
		logger.Info("Running verifiers as final gate")

		verifierOptions := workflow.ActivityOptions{
			StartToCloseTimeout: 10 * time.Minute,
			HeartbeatTimeout:    2 * time.Minute,
			RetryPolicy:         retryPolicy,
		}
		verifierCtx := workflow.WithActivityOptions(ctx, verifierOptions)

		var verifiersResult *model.VerifiersResult
		verifierInput := activity.RunVerifiersInput{
			SandboxInfo:             *sandbox,
			Repos:                   effectiveRepos,
			Verifiers:               verifiers,
			UseTransformationLayout: task.UsesTransformationRepo(),
		}
		if err := workflow.ExecuteActivity(verifierCtx, activity.ActivityRunVerifiers, verifierInput).Get(verifierCtx, &verifiersResult); err != nil {
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

	// 7. Handle based on mode
	if task.GetMode() == model.TaskModeReport {
		// Report mode: collect reports, skip PR creation
		logger.Info("Collecting reports", "repos", len(effectiveRepos))

		var repoResults []model.RepositoryResult

		for _, repo := range effectiveRepos {
			repoResult := model.RepositoryResult{
				Repository: repo.Name,
				Status:     "success",
			}

			if len(task.ForEach) > 0 {
				// forEach mode: execute N times, once per target
			// Note: Each target gets the full task timeout. Total execution time
			// may be up to len(ForEach) * timeout for sequential execution.
				logger.Info("Running forEach mode", "repo", repo.Name, "targets", len(task.ForEach))

				// Create activity context for Claude Code calls in forEach mode
				claudeOptions := workflow.ActivityOptions{
					StartToCloseTimeout: time.Duration(timeoutMinutes+5) * time.Minute,
					HeartbeatTimeout:    5 * time.Minute,
					RetryPolicy:         retryPolicy,
				}
				forEachClaudeCtx := workflow.WithActivityOptions(ctx, claudeOptions)

				var forEachResults []model.ForEachExecution

				for _, target := range task.ForEach {
					forEachExec := model.ForEachExecution{
						Target: target,
					}

					// Build target-specific report path
					repoPath := getRepoPath(task, repo)
					reportPath := fmt.Sprintf("%s/REPORT-%s.md", repoPath, target.Name)

					// Build prompt with template substitution
					targetPrompt, err := buildPromptForTarget(task, target, reportPath)
					if err != nil {
						errStr := fmt.Sprintf("failed to build prompt for target %s: %v", target.Name, err)
						forEachExec.Error = &errStr
						logger.Warn("Template error for target", "target", target.Name, "error", err)
						forEachResults = append(forEachResults, forEachExec)
						continue
					}

					// Run Claude Code with substituted prompt
					logger.Info("Running Claude Code for target", "target", target.Name)
					var targetResult *model.ClaudeCodeResult
					err = workflow.ExecuteActivity(forEachClaudeCtx, activity.ActivityRunClaudeCode,
						sandbox.ContainerID, targetPrompt, timeoutMinutes*60).Get(forEachClaudeCtx, &targetResult)

					if err != nil {
						errStr := fmt.Sprintf("Claude Code failed for target %s: %v", target.Name, err)
						forEachExec.Error = &errStr
						logger.Warn("Claude Code failed for target", "target", target.Name, "error", err)
						forEachResults = append(forEachResults, forEachExec)
						continue
					}

					if !targetResult.Success {
						errStr := fmt.Sprintf("Claude Code execution failed for target %s", target.Name)
						if targetResult.Error != nil {
							errStr = *targetResult.Error
						}
						forEachExec.Error = &errStr
						logger.Warn("Claude Code execution failed for target", "target", target.Name)
						forEachResults = append(forEachResults, forEachExec)
						continue
					}

					// Collect report for this target
					collectInput := activity.CollectReportInput{
						ContainerID:             sandbox.ContainerID,
						RepoName:                repo.Name,
						TargetName:              target.Name,
						UseTransformationLayout: task.UsesTransformationRepo(),
					}

					var report *model.ReportOutput
					err = workflow.ExecuteActivity(ctx, activity.ActivityCollectReport, collectInput).Get(ctx, &report)

					if err != nil {
						errStr := fmt.Sprintf("failed to collect report for target %s: %v", target.Name, err)
						forEachExec.Error = &errStr
						logger.Warn("Failed to collect report for target", "target", target.Name, "error", err)
						forEachResults = append(forEachResults, forEachExec)
						continue
					}

					forEachExec.Report = report

					// Validate schema if specified
					if hasSchema(task) && report != nil && report.Frontmatter != nil {
						schemaInput := activity.ValidateSchemaInput{
							Frontmatter: report.Frontmatter,
							Schema:      string(task.Execution.Agentic.Output.Schema),
						}

						var validationErrors []string
						err := workflow.ExecuteActivity(ctx, activity.ActivityValidateSchema, schemaInput).Get(ctx, &validationErrors)
						if err != nil {
							logger.Warn("Schema validation activity failed", "target", target.Name, "error", err)
						} else if len(validationErrors) > 0 {
							forEachExec.Report.ValidationErrors = validationErrors
							logger.Info("Schema validation errors", "target", target.Name, "errors", validationErrors)
						}
					}

					forEachResults = append(forEachResults, forEachExec)
					logger.Info("Completed target", "target", target.Name)
				}

				repoResult.ForEachResults = forEachResults
			} else {
				// Single report mode (existing behavior)
				collectInput := activity.CollectReportInput{
					ContainerID:             sandbox.ContainerID,
					RepoName:                repo.Name,
					UseTransformationLayout: task.UsesTransformationRepo(),
				}

				var report *model.ReportOutput
				err := workflow.ExecuteActivity(ctx, activity.ActivityCollectReport, collectInput).Get(ctx, &report)

				if err != nil {
					errStr := err.Error()
					repoResult.Status = "failed"
					repoResult.Error = &errStr
					logger.Warn("Failed to collect report", "repo", repo.Name, "error", err)
				} else {
					repoResult.Report = report

					// Validate schema if specified
					if hasSchema(task) && report != nil && report.Frontmatter != nil {
						schemaInput := activity.ValidateSchemaInput{
							Frontmatter: report.Frontmatter,
							Schema:      string(task.Execution.Agentic.Output.Schema),
						}

						var validationErrors []string
						err := workflow.ExecuteActivity(ctx, activity.ActivityValidateSchema, schemaInput).Get(ctx, &validationErrors)
						if err != nil {
							logger.Warn("Schema validation activity failed", "repo", repo.Name, "error", err)
						} else if len(validationErrors) > 0 {
							repoResult.Report.ValidationErrors = validationErrors
							logger.Info("Schema validation errors", "repo", repo.Name, "errors", validationErrors)
						}
					}
				}
			}

			repoResults = append(repoResults, repoResult)
		}

		status = model.TaskStatusCompleted
		signalDone()
		duration := workflow.Now(ctx).Sub(startTime).Seconds()

		logger.Info("Report mode task completed", "repos", len(repoResults))

		return &model.TaskResult{
			TaskID:          task.ID,
			Status:          model.TaskStatusCompleted,
			Mode:            model.TaskModeReport,
			Repositories:    repoResults,
			DurationSeconds: &duration,
		}, nil
	}

	// Transform mode: create pull requests
	status = model.TaskStatusCreatingPRs

	var pullRequests []model.PullRequest
	prDesc := buildPRDescriptionWithFiles(task, filesModified)
	prTitle := fmt.Sprintf("fix: %s", task.Title)

	// Sequential PR creation (parallel strategy uses child workflows)
	for _, repo := range effectiveRepos {
		input := activity.CreatePullRequestInput{
			ContainerID:             sandbox.ContainerID,
			Repo:                    repo,
			TaskID:                  task.ID,
			Title:                   prTitle,
			Description:             prDesc,
			PRConfig:                task.PullRequest,
			UseTransformationLayout: task.UsesTransformationRepo(),
		}
		var pr *model.PullRequest
		if err := workflow.ExecuteActivity(ctx, activity.ActivityCreatePullRequest, input).Get(ctx, &pr); err != nil {
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
			task.ID, strings.Join(prLinks, "\n"))
		_ = workflow.ExecuteActivity(ctx, activity.ActivityNotifySlack, *task.SlackChannel, msg, (*string)(nil)).Get(ctx, nil)
	}

	status = model.TaskStatusCompleted
	signalDone()
	duration := workflow.Now(ctx).Sub(startTime).Seconds()

	// Build repository results with PRs
	var repoResults []model.RepositoryResult
	prByRepo := make(map[string]*model.PullRequest)
	for i := range pullRequests {
		prByRepo[pullRequests[i].RepoName] = &pullRequests[i]
	}
	for _, repo := range effectiveRepos {
		repoResult := model.RepositoryResult{
			Repository:    repo.Name,
			Status:        "success",
			FilesModified: filesModified,
		}
		if pr, ok := prByRepo[repo.Name]; ok {
			repoResult.PullRequest = pr
		}
		repoResults = append(repoResults, repoResult)
	}

	return &model.TaskResult{
		TaskID:          task.ID,
		Status:          model.TaskStatusCompleted,
		Mode:            task.GetMode(),
		Repositories:    repoResults,
		DurationSeconds: &duration,
	}, nil
}

// generateAgentsMD creates the AGENTS.md content for the workspace.
func generateAgentsMD(task model.Task) string {
	var sb strings.Builder

	sb.WriteString("# Agent Instructions\n\n")
	sb.WriteString(fmt.Sprintf("## Task: %s\n\n", task.Title))
	sb.WriteString(fmt.Sprintf("**Task ID:** %s\n\n", task.ID))

	if task.Execution.Agentic != nil && task.Execution.Agentic.Prompt != "" {
		sb.WriteString(fmt.Sprintf("**Prompt:**\n%s\n\n", task.Execution.Agentic.Prompt))
	}

	if task.TicketURL != nil {
		sb.WriteString(fmt.Sprintf("**Related Ticket:** %s\n\n", *task.TicketURL))
	}

	// Include transformation repo info if set
	if task.UsesTransformationRepo() {
		sb.WriteString("## Transformation Repository\n\n")
		sb.WriteString("This workspace uses a transformation repository with skills and tools.\n")
		sb.WriteString(fmt.Sprintf("- Transformation: `%s` (branch: %s)\n\n", task.Transformation.Name, task.Transformation.Branch))
	}

	sb.WriteString("## Repositories\n\n")
	effectiveRepos := task.GetEffectiveRepositories()
	for _, repo := range effectiveRepos {
		if task.UsesTransformationRepo() {
			sb.WriteString(fmt.Sprintf("- `%s` (branch: %s) - located at `/workspace/targets/%s`\n", repo.Name, repo.Branch, repo.Name))
		} else {
			sb.WriteString(fmt.Sprintf("- `%s` (branch: %s)\n", repo.Name, repo.Branch))
		}
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
func buildPrompt(task model.Task) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Task: %s\n\n", task.Title))

	if task.Execution.Agentic != nil && task.Execution.Agentic.Prompt != "" {
		sb.WriteString(fmt.Sprintf("Instructions:\n%s\n\n", task.Execution.Agentic.Prompt))
	}

	if task.TicketURL != nil {
		sb.WriteString(fmt.Sprintf("Related ticket: %s\n\n", *task.TicketURL))
	}

	sb.WriteString("Repositories to work on:\n")
	effectiveRepos := task.GetEffectiveRepositories()
	for _, repo := range effectiveRepos {
		repoPath := getRepoPath(task, repo)
		sb.WriteString(fmt.Sprintf("- %s (in %s)\n", repo.Name, repoPath))
	}

	sb.WriteString("\nPlease analyze the codebase and implement the necessary fix. ")
	sb.WriteString("Follow the existing code style and patterns. ")
	sb.WriteString("Make minimal, targeted changes to address the issue.")

	// Append verifier instructions if verifiers are defined
	verifiers := task.Execution.GetVerifiers()
	if len(verifiers) > 0 {
		sb.WriteString("\n\n## Verification\n\n")
		sb.WriteString("After making changes, verify your work by running these commands:\n\n")
		for _, v := range verifiers {
			sb.WriteString(fmt.Sprintf("- **%s**: `%s`\n", v.Name, strings.Join(v.Command, " ")))
		}
		sb.WriteString("\nFix any errors before completing the task. All verifiers must pass.")
	}

	// Append report mode instructions if in report mode
	if task.GetMode() == model.TaskModeReport {
		sb.WriteString("\n\n## Output Requirements\n\n")
		if len(effectiveRepos) == 1 {
			repoPath := getRepoPath(task, effectiveRepos[0])
			sb.WriteString(fmt.Sprintf("Write your report to `%s/REPORT.md` with YAML frontmatter:\n\n", repoPath))
		} else {
			sb.WriteString("For each repository, write a report to the appropriate REPORT.md with YAML frontmatter:\n\n")
			for _, repo := range effectiveRepos {
				repoPath := getRepoPath(task, repo)
				sb.WriteString(fmt.Sprintf("- `%s/REPORT.md`\n", repoPath))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("```markdown\n---\nkey: value\nanother_key: value\n---\n\n# Report\n\nYour analysis and findings here.\n```\n\n")
		sb.WriteString("The frontmatter section (between `---` delimiters) should contain structured data.\n")
		sb.WriteString("The body section (after the closing `---`) should contain your detailed analysis.\n")

		if hasSchema(task) {
			sb.WriteString("\nThe frontmatter must conform to this JSON Schema:\n\n```json\n")
			sb.WriteString(string(task.Execution.Agentic.Output.Schema))
			sb.WriteString("\n```\n")
		}
	}

	return sb.String()
}

// getRepoPath returns the path to a repository within the workspace.
// When using transformation mode, repos are in /workspace/targets/{name}.
// Otherwise, repos are in /workspace/{name}.
func getRepoPath(task model.Task, repo model.Repository) string {
	if task.UsesTransformationRepo() {
		return fmt.Sprintf("/workspace/targets/%s", repo.Name)
	}
	return fmt.Sprintf("/workspace/%s", repo.Name)
}

// hasSchema checks if the task has a JSON Schema defined for report validation.
func hasSchema(task model.Task) bool {
	return task.Execution.Agentic != nil &&
		task.Execution.Agentic.Output != nil &&
		len(task.Execution.Agentic.Output.Schema) > 0
}

// buildPRDescriptionWithFiles creates the PR description with a list of modified files.
func buildPRDescriptionWithFiles(task model.Task, filesModified []string) string {
	var sb strings.Builder

	sb.WriteString("## Summary\n\n")
	sb.WriteString(fmt.Sprintf("Automated fix for: %s\n\n", task.Title))

	if task.Execution.Agentic != nil && task.Execution.Agentic.Prompt != "" {
		sb.WriteString(fmt.Sprintf("**Original prompt:**\n%s\n\n", task.Execution.Agentic.Prompt))
	}

	if task.TicketURL != nil {
		sb.WriteString(fmt.Sprintf("**Related ticket:** %s\n\n", *task.TicketURL))
	}

	// Add transformation mode info
	if task.Execution.GetExecutionType() == model.ExecutionTypeDeterministic {
		sb.WriteString(fmt.Sprintf("**Transformation:** Deterministic (%s)\n\n", task.Execution.Deterministic.Image))
	}

	sb.WriteString("## Changes\n\n")
	if len(filesModified) > 0 {
		sb.WriteString("Modified files:\n")
		for _, f := range filesModified {
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

// executeGroupedStrategy executes the grouped execution strategy.
// It spawns child workflows for each group with concurrency limiting.
func executeGroupedStrategy(ctx workflow.Context, task model.Task, startTime time.Time, signalDone func()) *model.TaskResult {
	logger := workflow.GetLogger(ctx)

	// Build shared data for child workflows
	agentsMD := generateAgentsMD(task)
	prDesc := buildPRDescriptionWithFiles(task, nil) // Files will be per-group
	prTitle := fmt.Sprintf("fix: %s", task.Title)

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

		// Wait for approval with 24hr timeout
		ok, _ := workflow.AwaitWithTimeout(ctx, 24*time.Hour, func() bool {
			return approved || cancelled
		})

		if !ok || cancelled || !approved {
			signalDone()
			duration := workflow.Now(ctx).Sub(startTime).Seconds()
			errMsg := "Changes rejected or approval timeout"
			if !ok {
				errMsg = "Approval timeout (24 hours)"
			}
			return &model.TaskResult{
				TaskID:          task.ID,
				Status:          model.TaskStatusCancelled,
				Mode:            task.GetMode(),
				Error:           &errMsg,
				DurationSeconds: &duration,
			}
		}

		logger.Info("Approval received, proceeding with grouped execution")
	}

	// Semaphore for concurrency limiting
	maxParallel := task.GetMaxParallel()
	numGroups := len(task.Groups)

	// Calculate effective parallel count to avoid goroutine leak
	// when there are fewer groups than maxParallel
	effectiveParallel := maxParallel
	if numGroups < effectiveParallel {
		effectiveParallel = numGroups
	}

	semaphore := workflow.NewChannel(ctx)

	// Pre-fill semaphore with only the tokens we'll actually use
	workflow.Go(ctx, func(ctx workflow.Context) {
		for i := 0; i < effectiveParallel; i++ {
			semaphore.Send(ctx, struct{}{})
		}
	})

	// Channel to collect results
	resultChannel := workflow.NewChannel(ctx)

	// Launch child workflows with concurrency control
	for _, group := range task.Groups {
		group := group // capture loop variable
		workflow.Go(ctx, func(gCtx workflow.Context) {
			// Acquire semaphore
			semaphore.Receive(gCtx, nil)

			// Use disconnected context for cleanup to ensure semaphore
			// is released even if workflow is cancelled
			defer func() {
				cleanupCtx, _ := workflow.NewDisconnectedContext(gCtx)
				semaphore.Send(cleanupCtx, struct{}{})
			}()

			logger.Info("Starting child workflow for group", "group", group.Name)

			// Start child workflow
			childID := fmt.Sprintf("%s-%s", task.ID, group.Name)
			childOptions := workflow.ChildWorkflowOptions{
				WorkflowID: childID,
			}
			childCtx := workflow.WithChildOptions(gCtx, childOptions)

			input := GroupTransformInput{
				Task:     task,
				Group:    group,
				AgentsMD: agentsMD,
				PRTitle:  prTitle,
				PRDesc:   prDesc,
				Approved: true, // Approval already obtained at parent level (or not required)
			}

			var result *GroupTransformResult
			err := workflow.ExecuteChildWorkflow(childCtx, TransformGroup, input).Get(childCtx, &result)

			if err != nil {
				// Workflow execution error
				logger.Error("Child workflow failed", "group", group.Name, "error", err)
				errMsg := err.Error()
				// Create failed results for all repos in the group
				var repoResults []model.RepositoryResult
				for _, repo := range group.Repositories {
					repoResults = append(repoResults, model.RepositoryResult{
						Repository: repo.Name,
						Status:     "failed",
						Error:      &errMsg,
					})
				}
				resultChannel.Send(gCtx, repoResults)
			} else if result != nil {
				resultChannel.Send(gCtx, result.Repositories)
			}
		})
	}

	// Collect all results
	var allRepoResults []model.RepositoryResult
	for i := 0; i < len(task.Groups); i++ {
		var groupResults []model.RepositoryResult
		resultChannel.Receive(ctx, &groupResults)
		allRepoResults = append(allRepoResults, groupResults...)
	}

	// Check for failures
	var failedRepos []string
	for _, r := range allRepoResults {
		if r.Status == "failed" {
			failedRepos = append(failedRepos, r.Repository)
		}
	}

	signalDone()
	duration := workflow.Now(ctx).Sub(startTime).Seconds()

	if len(failedRepos) > 0 {
		errMsg := fmt.Sprintf("Failed repositories: %s", strings.Join(failedRepos, ", "))
		return &model.TaskResult{
			TaskID:          task.ID,
			Status:          model.TaskStatusFailed,
			Mode:            task.GetMode(),
			Repositories:    allRepoResults,
			Error:           &errMsg,
			DurationSeconds: &duration,
		}
	}

	logger.Info("Grouped strategy completed successfully", "groups", len(task.Groups), "totalRepos", len(allRepoResults))

	return &model.TaskResult{
		TaskID:          task.ID,
		Status:          model.TaskStatusCompleted,
		Mode:            task.GetMode(),
		Repositories:    allRepoResults,
		DurationSeconds: &duration,
	}
}

// substitutePromptTemplate substitutes {{.Name}} and {{.Context}} variables in the prompt.
func substitutePromptTemplate(prompt string, target model.ForEachTarget) (string, error) {
	tmpl, err := template.New("prompt").Parse(prompt)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, target); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// buildPromptForTarget creates the prompt for a specific forEach target.
// It substitutes template variables and appends report output instructions.
func buildPromptForTarget(task model.Task, target model.ForEachTarget, reportPath string) (string, error) {
	if task.Execution.Agentic == nil || task.Execution.Agentic.Prompt == "" {
		return "", fmt.Errorf("agentic execution requires prompt to be set")
	}

	// Substitute template variables in the original prompt
	substitutedPrompt, err := substitutePromptTemplate(task.Execution.Agentic.Prompt, target)
	if err != nil {
		return "", err
	}

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Task: %s\n\n", task.Title))
	sb.WriteString(fmt.Sprintf("Target: %s\n\n", target.Name))
	sb.WriteString(fmt.Sprintf("Instructions:\n%s\n\n", substitutedPrompt))

	if task.TicketURL != nil {
		sb.WriteString(fmt.Sprintf("Related ticket: %s\n\n", *task.TicketURL))
	}

	sb.WriteString("Repositories to work on:\n")
	effectiveRepos := task.GetEffectiveRepositories()
	for _, repo := range effectiveRepos {
		repoPath := getRepoPath(task, repo)
		sb.WriteString(fmt.Sprintf("- %s (in %s)\n", repo.Name, repoPath))
	}

	sb.WriteString("\nPlease analyze the codebase and complete the task. ")
	sb.WriteString("Follow the existing code style and patterns. ")
	sb.WriteString("Focus specifically on the target described above.")

	// Append verifier instructions if verifiers are defined
	verifiers := task.Execution.GetVerifiers()
	if len(verifiers) > 0 {
		sb.WriteString("\n\n## Verification\n\n")
		sb.WriteString("After making changes, verify your work by running these commands:\n\n")
		for _, v := range verifiers {
			sb.WriteString(fmt.Sprintf("- **%s**: `%s`\n", v.Name, strings.Join(v.Command, " ")))
		}
		sb.WriteString("\nFix any errors before completing the task. All verifiers must pass.")
	}

	// Append report mode instructions with target-specific path
	sb.WriteString("\n\n## Output Requirements\n\n")
	sb.WriteString(fmt.Sprintf("Write your report to `%s` with YAML frontmatter:\n\n", reportPath))
	sb.WriteString("```markdown\n---\nkey: value\nanother_key: value\n---\n\n# Report\n\nYour analysis and findings here.\n```\n\n")
	sb.WriteString("The frontmatter section (between `---` delimiters) should contain structured data.\n")
	sb.WriteString("The body section (after the closing `---`) should contain your detailed analysis.\n")

	if hasSchema(task) {
		sb.WriteString("\nThe frontmatter must conform to this JSON Schema:\n\n```json\n")
		sb.WriteString(string(task.Execution.Agentic.Output.Schema))
		sb.WriteString("\n```\n")
	}

	return sb.String(), nil
}
