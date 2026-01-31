// Package client provides Temporal client utilities.
package client

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"

	"github.com/andreweacott/agent-orchestrator/internal/model"
	"github.com/andreweacott/agent-orchestrator/internal/workflow"
)

// TaskQueue is the default task queue for transform workflows.
const TaskQueue = "claude-code-tasks"

// WorkflowTimeoutBuffer is minutes added to task timeout for setup/cleanup.
const WorkflowTimeoutBuffer = 30

// validWorkflowStatuses defines allowed Temporal workflow execution statuses.
var validWorkflowStatuses = map[string]bool{
	"Running":    true,
	"Completed":  true,
	"Failed":     true,
	"Canceled":   true,
	"Terminated": true,
	"TimedOut":   true,
}

// Client wraps the Temporal client to reduce connection churn.
type Client struct {
	temporal client.Client
}

// NewClient creates a new Temporal client wrapper.
func NewClient() (*Client, error) {
	temporalAddr := os.Getenv("TEMPORAL_ADDRESS")
	if temporalAddr == "" {
		temporalAddr = "localhost:7233"
	}

	c, err := client.Dial(client.Options{
		HostPort: temporalAddr,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Temporal: %w", err)
	}

	return &Client{temporal: c}, nil
}

// Close closes the underlying Temporal client connection.
func (c *Client) Close() {
	c.temporal.Close()
}

// StartTransform starts a new transform workflow.
func (c *Client) StartTransform(ctx context.Context, task model.TransformTask) (string, error) {
	workflowID := fmt.Sprintf("transform-%s", task.TaskID)

	// Calculate workflow timeout: task timeout + buffer for setup/cleanup
	workflowTimeout := time.Duration(task.TimeoutMinutes+WorkflowTimeoutBuffer) * time.Minute

	options := client.StartWorkflowOptions{
		ID:                       workflowID,
		TaskQueue:                TaskQueue,
		WorkflowExecutionTimeout: workflowTimeout,
	}

	we, err := c.temporal.ExecuteWorkflow(ctx, options, workflow.Transform, task)
	if err != nil {
		return "", fmt.Errorf("failed to start workflow: %w", err)
	}

	return we.GetID(), nil
}

// GetWorkflowStatus queries the status of a workflow.
func (c *Client) GetWorkflowStatus(ctx context.Context, workflowID string) (model.TaskStatus, error) {
	resp, err := c.temporal.QueryWorkflow(ctx, workflowID, "", workflow.QueryStatus)
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
func (c *Client) GetWorkflowResult(ctx context.Context, workflowID string) (*model.TransformResult, error) {
	run := c.temporal.GetWorkflow(ctx, workflowID, "")

	var result model.TransformResult
	if err := run.Get(ctx, &result); err != nil {
		return nil, fmt.Errorf("failed to get workflow result: %w", err)
	}

	return &result, nil
}

// ApproveWorkflow sends an approval signal to a workflow.
func (c *Client) ApproveWorkflow(ctx context.Context, workflowID string) error {
	return c.temporal.SignalWorkflow(ctx, workflowID, "", workflow.SignalApprove, nil)
}

// RejectWorkflow sends a rejection signal to a workflow.
func (c *Client) RejectWorkflow(ctx context.Context, workflowID string) error {
	return c.temporal.SignalWorkflow(ctx, workflowID, "", workflow.SignalReject, nil)
}

// CancelWorkflow sends a cancellation signal to a workflow.
func (c *Client) CancelWorkflow(ctx context.Context, workflowID string) error {
	return c.temporal.SignalWorkflow(ctx, workflowID, "", workflow.SignalCancel, nil)
}

// WorkflowInfo contains summary information about a workflow.
type WorkflowInfo struct {
	WorkflowID string
	RunID      string
	Status     string
	StartTime  string
}

// ListWorkflows lists workflows matching the given status filter with pagination.
// If limit is 0, all matching workflows are returned.
func (c *Client) ListWorkflows(ctx context.Context, statusFilter string, limit int) ([]WorkflowInfo, error) {
	query := `WorkflowType = "Transform"`
	if statusFilter != "" {
		if !validWorkflowStatuses[statusFilter] {
			return nil, fmt.Errorf("invalid status filter: %q (valid: Running, Completed, Failed, Canceled, Terminated, TimedOut)", statusFilter)
		}
		query += fmt.Sprintf(` AND ExecutionStatus = "%s"`, statusFilter)
	}

	var workflows []WorkflowInfo
	var nextPageToken []byte

	for {
		resp, err := c.temporal.ListWorkflow(ctx, &workflowservice.ListWorkflowExecutionsRequest{
			Query:         query,
			NextPageToken: nextPageToken,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list workflows: %w", err)
		}

		for _, wf := range resp.Executions {
			if limit > 0 && len(workflows) >= limit {
				break
			}
			workflows = append(workflows, WorkflowInfo{
				WorkflowID: wf.Execution.WorkflowId,
				RunID:      wf.Execution.RunId,
				Status:     wf.Status.String(),
				StartTime:  wf.StartTime.AsTime().Format("2006-01-02 15:04:05"),
			})
		}

		nextPageToken = resp.NextPageToken
		if len(nextPageToken) == 0 || (limit > 0 && len(workflows) >= limit) {
			break
		}
	}

	return workflows, nil
}

// Standalone functions for backwards compatibility with existing CLI code.
// These create a client per call, which is less efficient but simpler for one-off operations.
//
// Deprecated: For multiple operations, prefer creating a Client with NewClient()
// and reusing it to reduce connection overhead.

// StartTransform starts a new transform workflow (standalone version).
func StartTransform(ctx context.Context, task model.TransformTask) (string, error) {
	c, err := NewClient()
	if err != nil {
		return "", err
	}
	defer c.Close()
	return c.StartTransform(ctx, task)
}

// GetWorkflowStatus queries the status of a workflow (standalone version).
func GetWorkflowStatus(ctx context.Context, workflowID string) (model.TaskStatus, error) {
	c, err := NewClient()
	if err != nil {
		return "", err
	}
	defer c.Close()
	return c.GetWorkflowStatus(ctx, workflowID)
}

// GetWorkflowResult waits for and returns the workflow result (standalone version).
func GetWorkflowResult(ctx context.Context, workflowID string) (*model.TransformResult, error) {
	c, err := NewClient()
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.GetWorkflowResult(ctx, workflowID)
}

// ApproveWorkflow sends an approval signal to a workflow (standalone version).
func ApproveWorkflow(ctx context.Context, workflowID string) error {
	c, err := NewClient()
	if err != nil {
		return err
	}
	defer c.Close()
	return c.ApproveWorkflow(ctx, workflowID)
}

// RejectWorkflow sends a rejection signal to a workflow (standalone version).
func RejectWorkflow(ctx context.Context, workflowID string) error {
	c, err := NewClient()
	if err != nil {
		return err
	}
	defer c.Close()
	return c.RejectWorkflow(ctx, workflowID)
}

// CancelWorkflow sends a cancellation signal to a workflow (standalone version).
func CancelWorkflow(ctx context.Context, workflowID string) error {
	c, err := NewClient()
	if err != nil {
		return err
	}
	defer c.Close()
	return c.CancelWorkflow(ctx, workflowID)
}

// ListWorkflows lists workflows matching the given status filter (standalone version).
func ListWorkflows(ctx context.Context, statusFilter string) ([]WorkflowInfo, error) {
	c, err := NewClient()
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.ListWorkflows(ctx, statusFilter, 0)
}
