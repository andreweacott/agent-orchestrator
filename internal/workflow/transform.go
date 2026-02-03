// Package workflow contains Temporal workflow definitions.
package workflow

import (
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/tinkerloft/fleetlift/internal/activity"
	"github.com/tinkerloft/fleetlift/internal/model"
)

// Signal and query names
const (
	SignalApprove  = "approve"
	SignalReject   = "reject"
	SignalCancel   = "cancel"
	SignalSteer    = "steer"
	SignalContinue = "continue"

	QueryStatus            = "get_status"
	QueryResult            = "get_claude_result"
	QueryDiff              = "get_diff"
	QueryVerifierLogs      = "get_verifier_logs"
	QuerySteeringState     = "get_steering_state"
	QueryExecutionProgress = "get_execution_progress"
)

// DefaultMaxSteeringIterations is the default limit for steering iterations.
const DefaultMaxSteeringIterations = 5

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

		// Steering state
		steerRequested       bool
		steeringPrompt       string
		steeringState        = model.SteeringState{MaxIterations: DefaultMaxSteeringIterations}
		cachedDiffs          []model.DiffOutput
		cachedVerifierOutput []model.VerifierOutput
	)

	// Apply task-level max steering iterations if set
	if task.MaxSteeringIterations > 0 {
		steeringState.MaxIterations = task.MaxSteeringIterations
	}

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

	if err := workflow.SetQueryHandler(ctx, QueryDiff, func() ([]model.DiffOutput, error) {
		return cachedDiffs, nil
	}); err != nil {
		return nil, fmt.Errorf("failed to register diff query: %w", err)
	}

	if err := workflow.SetQueryHandler(ctx, QueryVerifierLogs, func() ([]model.VerifierOutput, error) {
		return cachedVerifierOutput, nil
	}); err != nil {
		return nil, fmt.Errorf("failed to register verifier logs query: %w", err)
	}

	if err := workflow.SetQueryHandler(ctx, QuerySteeringState, func() (*model.SteeringState, error) {
		return &steeringState, nil
	}); err != nil {
		return nil, fmt.Errorf("failed to register steering state query: %w", err)
	}

	// Set up signal channels
	approveChannel := workflow.GetSignalChannel(ctx, SignalApprove)
	rejectChannel := workflow.GetSignalChannel(ctx, SignalReject)
	cancelChannel := workflow.GetSignalChannel(ctx, SignalCancel)
	steerChannel := workflow.GetSignalChannel(ctx, SignalSteer)

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

			selector.AddReceive(steerChannel, func(c workflow.ReceiveChannel, more bool) {
				var payload model.SteeringSignalPayload
				c.Receive(ctx, &payload)
				logger.Info("Received steering signal", "prompt", payload.Prompt)
				steerRequested = true
				steeringPrompt = payload.Prompt
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

			// 5. Human approval/steering loop for changes (agentic mode only, if required)
			if task.RequireApproval && len(filesModified) > 0 {
				// Cache diff and verifier output for queries
				diffInput := activity.GetDiffInput{
					ContainerID:             sandbox.ContainerID,
					Repos:                   effectiveRepos,
					UseTransformationLayout: task.UsesTransformationRepo(),
				}
				if err := workflow.ExecuteActivity(ctx, activity.ActivityGetDiff, diffInput).Get(ctx, &cachedDiffs); err != nil {
					logger.Warn("Failed to cache diff, queries may return stale data", "error", err)
				}

				if len(verifiers) > 0 {
					verifierInput := activity.GetVerifierOutputInput{
						ContainerID:             sandbox.ContainerID,
						Repos:                   effectiveRepos,
						Verifiers:               verifiers,
						UseTransformationLayout: task.UsesTransformationRepo(),
					}
					if err := workflow.ExecuteActivity(ctx, activity.ActivityGetVerifierOutput, verifierInput).Get(ctx, &cachedVerifierOutput); err != nil {
						logger.Warn("Failed to cache verifier output, queries may return stale data", "error", err)
					}
				}

				// Build diff summary for notification
				diffSummary := buildDiffSummary(cachedDiffs)

				status = model.TaskStatusAwaitingApproval

				if task.SlackChannel != nil {
					msg := fmt.Sprintf("Claude completed %s.\n\n%s\n\nReply: `approve`, `reject`, or `steer \"<prompt>\"`",
						task.ID, diffSummary)
					_ = workflow.ExecuteActivity(ctx, activity.ActivityNotifySlack, *task.SlackChannel, msg, (*string)(nil)).Get(ctx, nil)
				}

				// Steering loop
			steeringLoop:
				for {
					// Reset per-iteration state
					steerRequested = false
					steeringPrompt = ""

					// Wait for signal with 24hr timeout
					ok, err := workflow.AwaitWithTimeout(ctx, 24*time.Hour, func() bool {
						return approved != nil || cancellationRequested || steerRequested
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

					if approved != nil && *approved {
						// Approved - break out of steering loop
						break steeringLoop
					}

					if steerRequested {
						// Check iteration limit
						if steeringState.CurrentIteration >= steeringState.MaxIterations {
							logger.Warn("Max steering iterations reached", "max", steeringState.MaxIterations)
							if task.SlackChannel != nil {
								msg := fmt.Sprintf("Max steering iterations (%d) reached for %s. Please `approve` or `reject`.",
									steeringState.MaxIterations, task.ID)
								_ = workflow.ExecuteActivity(ctx, activity.ActivityNotifySlack, *task.SlackChannel, msg, (*string)(nil)).Get(ctx, nil)
							}
							continue
						}

						// Increment iteration
						steeringState.CurrentIteration++
						status = model.TaskStatusRunning
						logger.Info("Processing steering request", "iteration", steeringState.CurrentIteration, "prompt", steeringPrompt)

						// Build steering prompt with context
						basePrompt := buildPrompt(task)
						previousOutput := ""
						if claudeResult != nil {
							previousOutput = claudeResult.Output
						}
						fullSteeringPrompt := activity.BuildSteeringPrompt(basePrompt, steeringPrompt, steeringState.CurrentIteration, previousOutput)

						// Run Claude Code with steering prompt
						claudeOptions := workflow.ActivityOptions{
							StartToCloseTimeout: time.Duration(timeoutMinutes+5) * time.Minute,
							HeartbeatTimeout:    5 * time.Minute,
							RetryPolicy:         retryPolicy,
						}
						claudeCtx := workflow.WithActivityOptions(ctx, claudeOptions)

						var steerResult *model.ClaudeCodeResult
						if err := workflow.ExecuteActivity(claudeCtx, activity.ActivityRunClaudeCode, sandbox.ContainerID, fullSteeringPrompt, timeoutMinutes*60).Get(claudeCtx, &steerResult); err != nil {
							logger.Error("Steering Claude Code execution failed", "error", err)
							// Continue to allow retry or approval of current state
						} else {
							claudeResult = steerResult
							filesModified = steerResult.FilesModified
						}

						// Record steering iteration
						iteration := model.SteeringIteration{
							IterationNumber: steeringState.CurrentIteration,
							Prompt:          steeringPrompt,
							Timestamp:       workflow.Now(ctx),
							FilesModified:   filesModified,
						}
						if claudeResult != nil {
							iteration.Output = claudeResult.Output
						}
						steeringState.History = append(steeringState.History, iteration)

						// Re-cache diff
						if err := workflow.ExecuteActivity(ctx, activity.ActivityGetDiff, diffInput).Get(ctx, &cachedDiffs); err != nil {
							logger.Warn("Failed to re-cache diff after steering", "iteration", steeringState.CurrentIteration, "error", err)
						}

						// Re-run verifiers if defined
						if len(verifiers) > 0 {
							verifierInput := activity.GetVerifierOutputInput{
								ContainerID:             sandbox.ContainerID,
								Repos:                   effectiveRepos,
								Verifiers:               verifiers,
								UseTransformationLayout: task.UsesTransformationRepo(),
							}
							if err := workflow.ExecuteActivity(ctx, activity.ActivityGetVerifierOutput, verifierInput).Get(ctx, &cachedVerifierOutput); err != nil {
								logger.Warn("Failed to re-cache verifier output after steering", "iteration", steeringState.CurrentIteration, "error", err)
							}
						}

						// Notify with updated changes
						diffSummary = buildDiffSummary(cachedDiffs)
						status = model.TaskStatusAwaitingApproval

						if task.SlackChannel != nil {
							msg := fmt.Sprintf("Steering iteration %d complete for %s.\n\n%s\n\nReply: `approve`, `reject`, or `steer \"<prompt>\"`",
								steeringState.CurrentIteration, task.ID, diffSummary)
							_ = workflow.ExecuteActivity(ctx, activity.ActivityNotifySlack, *task.SlackChannel, msg, (*string)(nil)).Get(ctx, nil)
						}
					}
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
