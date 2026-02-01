// Package workflow contains Temporal workflow definitions.
package workflow

import (
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/andreweacott/agent-orchestrator/internal/activity"
	"github.com/andreweacott/agent-orchestrator/internal/model"
)

// GroupTransformInput contains the input for processing a repository group.
type GroupTransformInput struct {
	Task      model.Task
	Group     model.RepositoryGroup
	AgentsMD  string
	PRTitle   string
	PRDesc    string
	Approved  bool // Pre-approval status (approved at parent workflow level)
}

// GroupTransformResult contains the result of processing a repository group.
type GroupTransformResult struct {
	GroupName    string
	Repositories []model.RepositoryResult
	Error        error
}

// TransformGroup is a child workflow that processes a group of repositories.
// It provisions one sandbox, clones all repos in the group, transforms them together, and creates PRs.
func TransformGroup(ctx workflow.Context, input GroupTransformInput) (*GroupTransformResult, error) {
	logger := workflow.GetLogger(ctx)
	startTime := workflow.Now(ctx)

	task := input.Task
	group := input.Group

	logger.Info("Starting group transform", "group", group.Name, "repos", len(group.Repositories), "taskID", task.ID)

	// Workflow state
	var (
		sandbox      *model.SandboxInfo
		claudeResult *model.ClaudeCodeResult
	)

	// Helper to create failed result
	failedResult := func(errMsg string) *GroupTransformResult {
		var repoResults []model.RepositoryResult
		for _, repo := range group.Repositories {
			repoResults = append(repoResults, model.RepositoryResult{
				Repository: repo.Name,
				Status:     "failed",
				Error:      &errMsg,
			})
		}
		return &GroupTransformResult{
			GroupName:    group.Name,
			Repositories: repoResults,
			Error:        fmt.Errorf("%s", errMsg),
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

	executionType := task.Execution.GetExecutionType()
	timeoutMinutes := task.GetTimeoutMinutes()

	// 1. Provision sandbox for this group
	logger.Info("Provisioning sandbox", "group", group.Name)
	sandboxID := fmt.Sprintf("%s-%s", task.ID, group.Name)
	if err := workflow.ExecuteActivity(ctx, activity.ActivityProvisionSandbox, sandboxID).Get(ctx, &sandbox); err != nil {
		return failedResult(fmt.Sprintf("Failed to provision sandbox: %v", err)), nil
	}

	// 2. Clone all repositories in the group
	logger.Info("Cloning repositories", "group", group.Name, "count", len(group.Repositories))
	cloneOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		HeartbeatTimeout:    2 * time.Minute,
		RetryPolicy:         retryPolicy,
	}
	cloneCtx := workflow.WithActivityOptions(ctx, cloneOptions)

	cloneInput := activity.CloneRepositoriesInput{
		SandboxInfo:    *sandbox,
		AgentsMD:       input.AgentsMD,
		Transformation: task.Transformation,
		Targets:        nil,
		Repositories:   group.Repositories,
	}

	var clonedPaths []string
	if err := workflow.ExecuteActivity(cloneCtx, activity.ActivityCloneRepositories, cloneInput).Get(cloneCtx, &clonedPaths); err != nil {
		return failedResult(fmt.Sprintf("Failed to clone repositories: %v", err)), nil
	}

	// 3. Run transformation
	logger.Info("Running transformation", "group", group.Name, "type", executionType)

	var filesModified []string
	verifiers := task.Execution.GetVerifiers()

	if executionType == model.ExecutionTypeDeterministic {
		// Deterministic transformation
		if task.Execution.Deterministic == nil || task.Execution.Deterministic.Image == "" {
			return failedResult("Deterministic execution requires image to be set"), nil
		}

		deterministicOptions := workflow.ActivityOptions{
			StartToCloseTimeout: time.Duration(timeoutMinutes+5) * time.Minute,
			HeartbeatTimeout:    5 * time.Minute,
			RetryPolicy:         retryPolicy,
		}
		deterministicCtx := workflow.WithActivityOptions(ctx, deterministicOptions)

		var deterministicResult *model.DeterministicResult
		err := workflow.ExecuteActivity(deterministicCtx, activity.ActivityExecuteDeterministic,
			*sandbox, task.Execution.Deterministic.Image, task.Execution.Deterministic.Args,
			task.Execution.Deterministic.Env, group.Repositories).Get(deterministicCtx, &deterministicResult)

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
			logger.Info("No files modified by deterministic transformation", "group", group.Name)
			var repoResults []model.RepositoryResult
			for _, repo := range group.Repositories {
				repoResults = append(repoResults, model.RepositoryResult{
					Repository: repo.Name,
					Status:     "success",
				})
			}
			return &GroupTransformResult{
				GroupName:    group.Name,
				Repositories: repoResults,
			}, nil
		}

		logger.Info("Deterministic transformation completed", "group", group.Name, "filesModified", len(filesModified))
	} else {
		// Agentic transformation (Claude Code)
		if task.Execution.Agentic == nil || task.Execution.Agentic.Prompt == "" {
			return failedResult("Agentic execution requires prompt to be set"), nil
		}

		// Build prompt for this group
		prompt := buildPromptForGroup(task, group)

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

		// Skip PR if no changes
		if len(filesModified) == 0 {
			logger.Info("No files modified by Claude Code", "group", group.Name)
			var repoResults []model.RepositoryResult
			for _, repo := range group.Repositories {
				repoResults = append(repoResults, model.RepositoryResult{
					Repository: repo.Name,
					Status:     "success",
				})
			}
			return &GroupTransformResult{
				GroupName:    group.Name,
				Repositories: repoResults,
			}, nil
		}

		logger.Info("Claude Code completed", "group", group.Name, "filesModified", len(filesModified))
	}

	// 4. Run verifiers
	if len(verifiers) > 0 && len(filesModified) > 0 {
		logger.Info("Running verifiers", "group", group.Name)

		verifierOptions := workflow.ActivityOptions{
			StartToCloseTimeout: 10 * time.Minute,
			HeartbeatTimeout:    2 * time.Minute,
			RetryPolicy:         retryPolicy,
		}
		verifierCtx := workflow.WithActivityOptions(ctx, verifierOptions)

		var verifiersResult *model.VerifiersResult
		verifierInput := activity.RunVerifiersInput{
			SandboxInfo:             *sandbox,
			Repos:                   group.Repositories,
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

		logger.Info("All verifiers passed", "group", group.Name)
	}

	// 5. Handle based on mode
	var repoResults []model.RepositoryResult

	if task.GetMode() == model.TaskModeReport {
		// Report mode: collect reports, skip PR creation
		logger.Info("Collecting reports", "group", group.Name, "repos", len(group.Repositories))

		for _, repo := range group.Repositories {
			repoResult := model.RepositoryResult{
				Repository: repo.Name,
				Status:     "success",
			}

			// Collect report for this repo
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
				if task.Execution.Agentic != nil &&
					task.Execution.Agentic.Output != nil &&
					task.Execution.Agentic.Output.Schema != nil &&
					report != nil && report.Frontmatter != nil {

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

			repoResults = append(repoResults, repoResult)
		}
	} else {
		// Transform mode: create pull requests
		logger.Info("Creating pull requests", "group", group.Name, "repos", len(group.Repositories))

		for _, repo := range group.Repositories {
			prInput := activity.CreatePullRequestInput{
				ContainerID:             sandbox.ContainerID,
				Repo:                    repo,
				TaskID:                  task.ID,
				Title:                   input.PRTitle,
				Description:             input.PRDesc,
				PRConfig:                task.PullRequest,
				UseTransformationLayout: task.UsesTransformationRepo(),
			}

			var pr *model.PullRequest
			if err := workflow.ExecuteActivity(ctx, activity.ActivityCreatePullRequest, prInput).Get(ctx, &pr); err != nil {
				errMsg := fmt.Sprintf("Failed to create PR: %v", err)
				repoResults = append(repoResults, model.RepositoryResult{
					Repository: repo.Name,
					Status:     "failed",
					Error:      &errMsg,
				})
				continue
			}

			repoResults = append(repoResults, model.RepositoryResult{
				Repository:    repo.Name,
				Status:        "success",
				FilesModified: filesModified,
				PullRequest:   pr,
			})
		}
	}

	duration := workflow.Now(ctx).Sub(startTime)
	logger.Info("Group workflow completed", "group", group.Name, "mode", task.GetMode(), "duration", duration)

	return &GroupTransformResult{
		GroupName:    group.Name,
		Repositories: repoResults,
	}, nil
}

// buildPromptForGroup creates a prompt for a group of repositories.
func buildPromptForGroup(task model.Task, group model.RepositoryGroup) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Task: %s\n\n", task.Title))
	sb.WriteString(fmt.Sprintf("Group: %s\n\n", group.Name))

	if task.Execution.Agentic != nil && task.Execution.Agentic.Prompt != "" {
		sb.WriteString(fmt.Sprintf("Instructions:\n%s\n\n", task.Execution.Agentic.Prompt))
	}

	if task.TicketURL != nil {
		sb.WriteString(fmt.Sprintf("Related ticket: %s\n\n", *task.TicketURL))
	}

	sb.WriteString("Repositories in this group:\n")
	for _, repo := range group.Repositories {
		repoPath := getRepoPath(task, repo)
		sb.WriteString(fmt.Sprintf("- %s (in %s)\n", repo.Name, repoPath))
	}
	sb.WriteString("\n")

	// Add mode-appropriate instructions
	if task.GetMode() == model.TaskModeReport {
		sb.WriteString("Complete the analysis and write the requested report. ")
		sb.WriteString("Once you have written the report file, your task is complete.")
	} else {
		sb.WriteString("Please analyze the codebase and implement the necessary fix. ")
		sb.WriteString("Follow the existing code style and patterns. ")
		sb.WriteString("Make minimal, targeted changes to address the issue. ")
		sb.WriteString("You have access to all repositories in this group, so you can make cross-repository changes if needed.")
	}

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

	return sb.String()
}
