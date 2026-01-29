// Package main is the CLI entry point.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/anthropics/claude-code-orchestrator/internal/client"
	"github.com/anthropics/claude-code-orchestrator/internal/model"
)

var rootCmd = &cobra.Command{
	Use:   "claude-orchestrator",
	Short: "Claude Code Orchestrator CLI",
	Long:  "CLI for interacting with the Claude Code bug fix orchestration system",
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start a bug fix workflow",
	Long:  "Start a new bug fix workflow with Claude Code",
	RunE:  runStart,
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Get workflow status",
	Long:  "Query the current status of a workflow",
	RunE:  runStatus,
}

var resultCmd = &cobra.Command{
	Use:   "result",
	Short: "Get workflow result",
	Long:  "Wait for and get the final result of a workflow",
	RunE:  runResult,
}

var approveCmd = &cobra.Command{
	Use:   "approve",
	Short: "Approve workflow changes",
	Long:  "Send an approval signal to a workflow awaiting approval",
	RunE:  runApprove,
}

var rejectCmd = &cobra.Command{
	Use:   "reject",
	Short: "Reject workflow changes",
	Long:  "Send a rejection signal to a workflow awaiting approval",
	RunE:  runReject,
}

var cancelCmd = &cobra.Command{
	Use:   "cancel",
	Short: "Cancel a workflow",
	Long:  "Send a cancellation signal to a running workflow",
	RunE:  runCancel,
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List workflows",
	Long:  "List all bug fix workflows",
	RunE:  runList,
}

func init() {
	// Start command flags
	startCmd.Flags().String("task-id", "", "Unique task identifier (required)")
	startCmd.Flags().String("title", "", "Bug title (required)")
	startCmd.Flags().String("description", "", "Bug description")
	startCmd.Flags().String("repos", "", "Comma-separated repository URLs (required)")
	startCmd.Flags().String("branch", "main", "Branch to use")
	startCmd.Flags().Bool("no-approval", false, "Skip human approval step")
	startCmd.Flags().String("slack-channel", "", "Slack channel for notifications")
	startCmd.Flags().String("ticket-url", "", "URL to related ticket")
	startCmd.Flags().Int("timeout", 30, "Timeout in minutes")
	startCmd.MarkFlagRequired("task-id")
	startCmd.MarkFlagRequired("title")
	startCmd.MarkFlagRequired("repos")

	// Status command flags
	statusCmd.Flags().String("workflow-id", "", "Workflow ID (required)")
	statusCmd.MarkFlagRequired("workflow-id")

	// Result command flags
	resultCmd.Flags().String("workflow-id", "", "Workflow ID (required)")
	resultCmd.MarkFlagRequired("workflow-id")

	// Approve command flags
	approveCmd.Flags().String("workflow-id", "", "Workflow ID (required)")
	approveCmd.MarkFlagRequired("workflow-id")

	// Reject command flags
	rejectCmd.Flags().String("workflow-id", "", "Workflow ID (required)")
	rejectCmd.MarkFlagRequired("workflow-id")

	// Cancel command flags
	cancelCmd.Flags().String("workflow-id", "", "Workflow ID (required)")
	cancelCmd.MarkFlagRequired("workflow-id")

	// List command flags
	listCmd.Flags().String("status", "", "Filter by status (Running, Completed, Failed, Canceled, Terminated)")

	// Add commands
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(resultCmd)
	rootCmd.AddCommand(approveCmd)
	rootCmd.AddCommand(rejectCmd)
	rootCmd.AddCommand(cancelCmd)
	rootCmd.AddCommand(listCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	taskID, _ := cmd.Flags().GetString("task-id")
	title, _ := cmd.Flags().GetString("title")
	description, _ := cmd.Flags().GetString("description")
	repos, _ := cmd.Flags().GetString("repos")
	branch, _ := cmd.Flags().GetString("branch")
	noApproval, _ := cmd.Flags().GetBool("no-approval")
	slackChannel, _ := cmd.Flags().GetString("slack-channel")
	ticketURL, _ := cmd.Flags().GetString("ticket-url")
	timeout, _ := cmd.Flags().GetInt("timeout")

	// Parse repositories
	var repositories []model.Repository
	for _, url := range strings.Split(repos, ",") {
		url = strings.TrimSpace(url)
		if url != "" {
			repositories = append(repositories, model.NewRepository(url, branch, ""))
		}
	}

	if len(repositories) == 0 {
		return fmt.Errorf("at least one repository URL is required")
	}

	// Build task
	task := model.BugFixTask{
		TaskID:          taskID,
		Title:           title,
		Description:     description,
		Repositories:    repositories,
		RequireApproval: !noApproval,
		TimeoutMinutes:  timeout,
	}

	if slackChannel != "" {
		task.SlackChannel = &slackChannel
	}
	if ticketURL != "" {
		task.TicketURL = &ticketURL
	}

	fmt.Printf("Starting bug fix workflow...\n")
	fmt.Printf("  Task ID: %s\n", task.TaskID)
	fmt.Printf("  Title: %s\n", task.Title)
	fmt.Printf("  Repositories: %s\n", repos)
	fmt.Printf("  Require approval: %v\n\n", task.RequireApproval)

	workflowID, err := client.StartBugFix(context.Background(), task)
	if err != nil {
		return fmt.Errorf("failed to start workflow: %w", err)
	}

	fmt.Printf("Workflow started: %s\n", workflowID)
	fmt.Printf("View at: http://localhost:8233/namespaces/default/workflows/%s\n", workflowID)

	return nil
}

func runStatus(cmd *cobra.Command, args []string) error {
	workflowID, _ := cmd.Flags().GetString("workflow-id")

	status, err := client.GetWorkflowStatus(context.Background(), workflowID)
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	fmt.Printf("Workflow: %s\n", workflowID)
	fmt.Printf("Status: %s\n", status)

	return nil
}

func runResult(cmd *cobra.Command, args []string) error {
	workflowID, _ := cmd.Flags().GetString("workflow-id")

	fmt.Printf("Waiting for workflow %s to complete...\n", workflowID)

	result, err := client.GetWorkflowResult(context.Background(), workflowID)
	if err != nil {
		return fmt.Errorf("failed to get result: %w", err)
	}

	fmt.Printf("\nWorkflow Result:\n")
	fmt.Printf("  Task ID: %s\n", result.TaskID)
	fmt.Printf("  Status: %s\n", result.Status)

	if result.Error != nil {
		fmt.Printf("  Error: %s\n", *result.Error)
	}

	if result.DurationSeconds != nil {
		fmt.Printf("  Duration: %.2f seconds\n", *result.DurationSeconds)
	}

	if len(result.PullRequests) > 0 {
		fmt.Printf("  Pull Requests:\n")
		for _, pr := range result.PullRequests {
			fmt.Printf("    - %s (#%d): %s\n", pr.RepoName, pr.PRNumber, pr.PRURL)
		}
	}

	return nil
}

func runApprove(cmd *cobra.Command, args []string) error {
	workflowID, _ := cmd.Flags().GetString("workflow-id")

	if err := client.ApproveWorkflow(context.Background(), workflowID); err != nil {
		return fmt.Errorf("failed to approve: %w", err)
	}

	fmt.Printf("Approved: %s\n", workflowID)
	return nil
}

func runReject(cmd *cobra.Command, args []string) error {
	workflowID, _ := cmd.Flags().GetString("workflow-id")

	if err := client.RejectWorkflow(context.Background(), workflowID); err != nil {
		return fmt.Errorf("failed to reject: %w", err)
	}

	fmt.Printf("Rejected: %s\n", workflowID)
	return nil
}

func runCancel(cmd *cobra.Command, args []string) error {
	workflowID, _ := cmd.Flags().GetString("workflow-id")

	if err := client.CancelWorkflow(context.Background(), workflowID); err != nil {
		return fmt.Errorf("failed to cancel: %w", err)
	}

	fmt.Printf("Cancelled: %s\n", workflowID)
	return nil
}

func runList(cmd *cobra.Command, args []string) error {
	statusFilter, _ := cmd.Flags().GetString("status")

	workflows, err := client.ListWorkflows(context.Background(), statusFilter)
	if err != nil {
		return fmt.Errorf("failed to list workflows: %w", err)
	}

	if len(workflows) == 0 {
		fmt.Println("No workflows found")
		return nil
	}

	fmt.Printf("%-40s %-15s %s\n", "WORKFLOW ID", "STATUS", "START TIME")
	fmt.Println(strings.Repeat("-", 80))

	for _, wf := range workflows {
		fmt.Printf("%-40s %-15s %s\n", wf.WorkflowID, wf.Status, wf.StartTime)
	}

	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
