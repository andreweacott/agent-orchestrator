// Package client provides Temporal client utilities.
package client

import (
	"context"
	"fmt"
	"os"

	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"

	"github.com/anthropics/claude-code-orchestrator/internal/model"
	"github.com/anthropics/claude-code-orchestrator/internal/workflow"
)

// TaskQueue is the default task queue for bug fix workflows.
const TaskQueue = "claude-code-tasks"

// GetClient creates a new Temporal client.
func GetClient(ctx context.Context) (client.Client, error) {
	temporalAddr := os.Getenv("TEMPORAL_ADDRESS")
	if temporalAddr == "" {
		temporalAddr = "localhost:7233"
	}

	return client.Dial(client.Options{
		HostPort: temporalAddr,
	})
}

// StartBugFix starts a new bug fix workflow.
func StartBugFix(ctx context.Context, task model.BugFixTask) (string, error) {
	c, err := GetClient(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to connect to Temporal: %w", err)
	}
	defer c.Close()

	workflowID := fmt.Sprintf("bug-fix-%s", task.TaskID)

	options := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: TaskQueue,
	}

	we, err := c.ExecuteWorkflow(ctx, options, workflow.BugFix, task)
	if err != nil {
		return "", fmt.Errorf("failed to start workflow: %w", err)
	}

	return we.GetID(), nil
}

// GetWorkflowStatus queries the status of a workflow.
func GetWorkflowStatus(ctx context.Context, workflowID string) (model.TaskStatus, error) {
	c, err := GetClient(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to connect to Temporal: %w", err)
	}
	defer c.Close()

	resp, err := c.QueryWorkflow(ctx, workflowID, "", workflow.QueryStatus)
	if err != nil {
		return "", fmt.Errorf("failed to query workflow: %w", err)
	}

	var status model.TaskStatus
	if err := resp.Get(&status); err != nil {
		return "", fmt.Errorf("failed to decode status: %w", err)
	}

	return status, nil
}

// GetWorkflowResult waits for and returns the workflow result.
func GetWorkflowResult(ctx context.Context, workflowID string) (*model.BugFixResult, error) {
	c, err := GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Temporal: %w", err)
	}
	defer c.Close()

	run := c.GetWorkflow(ctx, workflowID, "")

	var result model.BugFixResult
	if err := run.Get(ctx, &result); err != nil {
		return nil, fmt.Errorf("failed to get workflow result: %w", err)
	}

	return &result, nil
}

// ApproveWorkflow sends an approval signal to a workflow.
func ApproveWorkflow(ctx context.Context, workflowID string) error {
	c, err := GetClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Temporal: %w", err)
	}
	defer c.Close()

	return c.SignalWorkflow(ctx, workflowID, "", workflow.SignalApprove, nil)
}

// RejectWorkflow sends a rejection signal to a workflow.
func RejectWorkflow(ctx context.Context, workflowID string) error {
	c, err := GetClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Temporal: %w", err)
	}
	defer c.Close()

	return c.SignalWorkflow(ctx, workflowID, "", workflow.SignalReject, nil)
}

// CancelWorkflow sends a cancellation signal to a workflow.
func CancelWorkflow(ctx context.Context, workflowID string) error {
	c, err := GetClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Temporal: %w", err)
	}
	defer c.Close()

	return c.SignalWorkflow(ctx, workflowID, "", workflow.SignalCancel, nil)
}

// WorkflowInfo contains summary information about a workflow.
type WorkflowInfo struct {
	WorkflowID string
	RunID      string
	Status     string
	StartTime  string
}

// ListWorkflows lists workflows matching the given status filter.
func ListWorkflows(ctx context.Context, statusFilter string) ([]WorkflowInfo, error) {
	c, err := GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Temporal: %w", err)
	}
	defer c.Close()

	query := `WorkflowType = "BugFix"`
	if statusFilter != "" {
		query += fmt.Sprintf(` AND ExecutionStatus = "%s"`, statusFilter)
	}

	var workflows []WorkflowInfo

	resp, err := c.ListWorkflow(ctx, &workflowservice.ListWorkflowExecutionsRequest{
		Query: query,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list workflows: %w", err)
	}

	for _, wf := range resp.Executions {
		workflows = append(workflows, WorkflowInfo{
			WorkflowID: wf.Execution.WorkflowId,
			RunID:      wf.Execution.RunId,
			Status:     wf.Status.String(),
			StartTime:  wf.StartTime.AsTime().Format("2006-01-02 15:04:05"),
		})
	}

	return workflows, nil
}
