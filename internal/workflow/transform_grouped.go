// Package workflow contains Temporal workflow definitions.
package workflow

import (
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/tinkerloft/fleetlift/internal/model"
)

// executeGroupedStrategy executes the grouped execution strategy.
// It spawns child workflows for each group with concurrency limiting.
func executeGroupedStrategy(ctx workflow.Context, task model.Task, startTime time.Time, signalDone func()) *model.TaskResult {
	logger := workflow.GetLogger(ctx)

	// Build shared data for child workflows
	agentsMD := generateAgentsMD(task)
	prDesc := buildPRDescriptionWithFiles(task, nil) // Files will be per-group
	prTitle := fmt.Sprintf("fix: %s", task.Title)

	// State for tracking execution progress
	var (
		continueRequested bool
		skipRemaining     bool
		executionProgress = model.ExecutionProgress{
			TotalGroups:      len(task.Groups),
			CompletedGroups:  0,
			FailedGroups:     0,
			FailurePercent:   0,
			IsPaused:         false,
			FailedGroupNames: []string{},
		}
		groupResults          = make(map[string]model.GroupResult)
		allRepoResults        []model.RepositoryResult
		cancellationRequested bool
	)

	// Register query handler for execution progress
	if err := workflow.SetQueryHandler(ctx, QueryExecutionProgress, func() (*model.ExecutionProgress, error) {
		return &executionProgress, nil
	}); err != nil {
		logger.Warn("Failed to register execution progress query handler", "error", err)
	}

	// Set up signal channels
	cancelChannel := workflow.GetSignalChannel(ctx, SignalCancel)
	continueChannel := workflow.GetSignalChannel(ctx, SignalContinue)

	// Handle cancel signal asynchronously
	workflow.Go(ctx, func(ctx workflow.Context) {
		cancelChannel.Receive(ctx, nil)
		cancellationRequested = true
		logger.Info("Received cancellation signal")
	})

	// If approval is required, wait for it at the parent level before spawning children
	if task.RequireApproval {
		approved := false
		cancelled := false

		approveChannel := workflow.GetSignalChannel(ctx, SignalApprove)
		rejectChannel := workflow.GetSignalChannel(ctx, SignalReject)

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
	type groupResultWithName struct {
		groupName   string
		repoResults []model.RepositoryResult
		err         error
	}
	resultChannel := workflow.NewChannel(ctx)

	// Track groups that were actually launched
	launchedGroups := make(map[string]bool)

	// Launch child workflows with concurrency control
	for _, group := range task.Groups {
		group := group // capture loop variable

		// Check if we should skip remaining groups
		if skipRemaining {
			logger.Info("Skipping group due to skip_remaining flag", "group", group.Name)
			groupResults[group.Name] = model.GroupResult{
				GroupName: group.Name,
				Status:    "skipped",
			}
			continue
		}

		// Mark this group as launched
		launchedGroups[group.Name] = true

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
				resultChannel.Send(gCtx, groupResultWithName{
					groupName:   group.Name,
					repoResults: repoResults,
					err:         err,
				})
			} else if result != nil {
				resultChannel.Send(gCtx, groupResultWithName{
					groupName:   group.Name,
					repoResults: result.Repositories,
					err:         nil,
				})
			}
		})
	}

	// Collect results only for launched groups
	for range launchedGroups {
		var result groupResultWithName
		resultChannel.Receive(ctx, &result)

		allRepoResults = append(allRepoResults, result.repoResults...)
		executionProgress.CompletedGroups++

		// Determine group status
		groupStatus := "success"
		var groupError *string

		// First check if the child workflow itself failed
		if result.err != nil {
			groupStatus = "failed"
			errMsg := result.err.Error()
			groupError = &errMsg
		}

		// Then check if any repos in the group failed (this may override the error message)
		for _, r := range result.repoResults {
			if r.Status == "failed" {
				groupStatus = "failed"
				if r.Error != nil {
					groupError = r.Error
				}
				break
			}
		}

		// Only count and record the failure once
		if groupStatus == "failed" {
			executionProgress.FailedGroups++
			executionProgress.FailedGroupNames = append(executionProgress.FailedGroupNames, result.groupName)
		}

		groupResults[result.groupName] = model.GroupResult{
			GroupName:    result.groupName,
			Status:       groupStatus,
			Repositories: result.repoResults,
			Error:        groupError,
		}

		// Update failure percentage
		if executionProgress.CompletedGroups > 0 {
			executionProgress.FailurePercent = (float64(executionProgress.FailedGroups) / float64(executionProgress.CompletedGroups)) * 100
		}

		logger.Info("Group completed", "group", result.groupName, "status", groupStatus,
			"completed", executionProgress.CompletedGroups, "failed", executionProgress.FailedGroups,
			"failurePercent", executionProgress.FailurePercent)

		// Check if we should pause on failure threshold
		if task.ShouldPauseOnFailure(executionProgress.CompletedGroups, executionProgress.FailedGroups) {
			action := task.GetFailureAction()
			threshold := task.GetFailureThresholdPercent()

			if action == "abort" {
				logger.Warn("Aborting due to failure threshold", "threshold", threshold, "failurePercent", executionProgress.FailurePercent)
				skipRemaining = true
				// Mark remaining groups as skipped
				for _, group := range task.Groups {
					if _, exists := groupResults[group.Name]; !exists {
						groupResults[group.Name] = model.GroupResult{
							GroupName: group.Name,
							Status:    "skipped",
						}
					}
				}
				break
			}

			if action == "pause" && !executionProgress.IsPaused {
				executionProgress.IsPaused = true
				executionProgress.PausedReason = fmt.Sprintf("Failure threshold exceeded (%.1f%% > %d%%)", executionProgress.FailurePercent, threshold)

				logger.Warn("Pausing due to failure threshold", "threshold", threshold, "failurePercent", executionProgress.FailurePercent)

				// Wait for continue signal or cancellation
				continueRequested = false
				selector := workflow.NewSelector(ctx)
				selector.AddReceive(continueChannel, func(c workflow.ReceiveChannel, more bool) {
					var payload model.ContinueSignalPayload
					c.Receive(ctx, &payload)
					continueRequested = true
					skipRemaining = payload.SkipRemaining
					logger.Info("Received continue signal", "skipRemaining", payload.SkipRemaining)
				})
				selector.AddReceive(cancelChannel, func(c workflow.ReceiveChannel, more bool) {
					c.Receive(ctx, nil)
					cancellationRequested = true
					logger.Info("Received cancellation during pause")
				})

				ok, _ := workflow.AwaitWithTimeout(ctx, 24*time.Hour, func() bool {
					return continueRequested || cancellationRequested
				})

				if cancellationRequested || !ok {
					signalDone()
					duration := workflow.Now(ctx).Sub(startTime).Seconds()
					errMsg := "Workflow cancelled during pause"
					if !ok {
						errMsg = "Continue timeout (24 hours)"
					}

					// Build group results array
					var finalGroupResults []model.GroupResult
					for _, group := range task.Groups {
						if gr, exists := groupResults[group.Name]; exists {
							finalGroupResults = append(finalGroupResults, gr)
						} else {
							finalGroupResults = append(finalGroupResults, model.GroupResult{
								GroupName: group.Name,
								Status:    "pending",
							})
						}
					}

					return &model.TaskResult{
						TaskID:          task.ID,
						Status:          model.TaskStatusCancelled,
						Mode:            task.GetMode(),
						Repositories:    allRepoResults,
						Groups:          finalGroupResults,
						Error:           &errMsg,
						DurationSeconds: &duration,
					}
				}

				executionProgress.IsPaused = false
				executionProgress.PausedReason = ""
				logger.Info("Resuming execution after pause")

				if skipRemaining {
					logger.Info("Skipping remaining groups per continue signal")
					// Mark remaining groups as skipped
					for _, group := range task.Groups {
						if _, exists := groupResults[group.Name]; !exists {
							groupResults[group.Name] = model.GroupResult{
								GroupName: group.Name,
								Status:    "skipped",
							}
						}
					}
					break
				}
			}
		}
	}

	// Build final group results array in order
	var finalGroupResults []model.GroupResult
	for _, group := range task.Groups {
		if gr, exists := groupResults[group.Name]; exists {
			finalGroupResults = append(finalGroupResults, gr)
		}
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
			Groups:          finalGroupResults,
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
		Groups:          finalGroupResults,
		DurationSeconds: &duration,
	}
}
