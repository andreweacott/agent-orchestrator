// Package main is the CLI entry point.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/anthropics/claude-code-orchestrator/internal/client"
	"github.com/anthropics/claude-code-orchestrator/internal/model"
)

// must panics if err is non-nil. Used for initialization errors.
func must(err error) {
	if err != nil {
		panic(fmt.Errorf("initialization error: %w", err))
	}
}

// TaskFile represents the YAML task file format.
type TaskFile struct {
	ID           string           `yaml:"id"`
	Title        string           `yaml:"title"`
	Description  string           `yaml:"description,omitempty"`
	Repositories []RepositorySpec `yaml:"repositories"`
	Verifiers    []VerifierSpec   `yaml:"verifiers,omitempty"`
	Timeout      string           `yaml:"timeout,omitempty"` // e.g., "30m"

	// Optional
	TicketURL       string `yaml:"ticket_url,omitempty"`
	SlackChannel    string `yaml:"slack_channel,omitempty"`
	RequireApproval *bool  `yaml:"require_approval,omitempty"`
}

// RepositorySpec is the YAML format for repository configuration.
type RepositorySpec struct {
	URL    string   `yaml:"url"`
	Branch string   `yaml:"branch,omitempty"`
	Setup  []string `yaml:"setup,omitempty"`
}

// VerifierSpec is the YAML format for verifier configuration.
type VerifierSpec struct {
	Name    string   `yaml:"name"`
	Command []string `yaml:"command"`
}

var rootCmd = &cobra.Command{
	Use:   "claude-orchestrator",
	Short: "Claude Code Orchestrator CLI",
	Long:  "CLI for interacting with the Claude Code bug fix orchestration system",
}

// SIMP-001: Removed deprecated 'start' command - use 'run' instead

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

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run a task from file or flags",
	Long:  "Run a transformation task from a YAML file or command-line flags",
	RunE:  runRun,
}

func init() {
	// Run command flags (matches design doc interface)
	runCmd.Flags().StringP("file", "f", "", "Path to task YAML file")
	runCmd.Flags().StringArray("repo", []string{}, "Repository URL (can be repeated)")
	runCmd.Flags().StringP("prompt", "p", "", "Task prompt/description")
	runCmd.Flags().StringArray("verifier", []string{}, "Verifier in format 'name:command' (can be repeated)")
	runCmd.Flags().String("branch", "main", "Branch to use for all repositories")
	runCmd.Flags().Bool("no-approval", false, "Skip human approval step")
	runCmd.Flags().Int("timeout", 30, "Timeout in minutes")

	// Status command flags
	statusCmd.Flags().String("workflow-id", "", "Workflow ID (required)")
	must(statusCmd.MarkFlagRequired("workflow-id"))

	// Result command flags
	resultCmd.Flags().String("workflow-id", "", "Workflow ID (required)")
	must(resultCmd.MarkFlagRequired("workflow-id"))

	// Approve command flags
	approveCmd.Flags().String("workflow-id", "", "Workflow ID (required)")
	must(approveCmd.MarkFlagRequired("workflow-id"))

	// Reject command flags
	rejectCmd.Flags().String("workflow-id", "", "Workflow ID (required)")
	must(rejectCmd.MarkFlagRequired("workflow-id"))

	// Cancel command flags
	cancelCmd.Flags().String("workflow-id", "", "Workflow ID (required)")
	must(cancelCmd.MarkFlagRequired("workflow-id"))

	// List command flags
	listCmd.Flags().String("status", "", "Filter by status (Running, Completed, Failed, Canceled, Terminated)")

	// Add commands
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(resultCmd)
	rootCmd.AddCommand(approveCmd)
	rootCmd.AddCommand(rejectCmd)
	rootCmd.AddCommand(cancelCmd)
	rootCmd.AddCommand(listCmd)
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

func runRun(cmd *cobra.Command, args []string) error {
	filePath, _ := cmd.Flags().GetString("file")

	var task model.BugFixTask

	if filePath != "" {
		// Load from YAML file
		taskFromFile, err := loadTaskFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to load task file: %w", err)
		}
		task = *taskFromFile
	} else {
		// Build from flags
		repos, _ := cmd.Flags().GetStringArray("repo")
		prompt, _ := cmd.Flags().GetString("prompt")
		verifiers, _ := cmd.Flags().GetStringArray("verifier")
		branch, _ := cmd.Flags().GetString("branch")
		noApproval, _ := cmd.Flags().GetBool("no-approval")
		timeout, _ := cmd.Flags().GetInt("timeout")

		if len(repos) == 0 {
			return fmt.Errorf("at least one --repo is required (or use --file)")
		}
		if prompt == "" {
			return fmt.Errorf("--prompt is required (or use --file)")
		}

		// Generate task ID
		taskID := fmt.Sprintf("task-%d", os.Getpid())

		// Parse repositories
		var repositories []model.Repository
		for _, url := range repos {
			repositories = append(repositories, model.NewRepository(url, branch, ""))
		}

		// Parse verifiers
		parsedVerifiers := parseVerifiers(verifiers)

		task = model.BugFixTask{
			TaskID:          taskID,
			Title:           prompt,
			Description:     prompt,
			Repositories:    repositories,
			Verifiers:       parsedVerifiers,
			RequireApproval: !noApproval,
			TimeoutMinutes:  timeout,
		}
	}

	fmt.Printf("Starting task...\n")
	fmt.Printf("  Task ID: %s\n", task.TaskID)
	fmt.Printf("  Title: %s\n", task.Title)
	fmt.Printf("  Repositories: %d\n", len(task.Repositories))
	fmt.Printf("  Verifiers: %d\n", len(task.Verifiers))
	fmt.Printf("  Require approval: %v\n\n", task.RequireApproval)

	workflowID, err := client.StartBugFix(context.Background(), task)
	if err != nil {
		return fmt.Errorf("failed to start workflow: %w", err)
	}

	fmt.Printf("Workflow started: %s\n", workflowID)
	fmt.Printf("View at: http://localhost:8233/namespaces/default/workflows/%s\n", workflowID)

	return nil
}

// loadTaskFile loads a task from a YAML file.
func loadTaskFile(path string) (*model.BugFixTask, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var tf TaskFile
	if err := yaml.Unmarshal(data, &tf); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Convert to BugFixTask
	var repos []model.Repository
	for _, r := range tf.Repositories {
		branch := r.Branch
		if branch == "" {
			branch = "main"
		}
		repos = append(repos, model.Repository{
			URL:    r.URL,
			Branch: branch,
			Name:   model.NewRepository(r.URL, branch, "").Name,
			Setup:  r.Setup,
		})
	}

	var verifiers []model.Verifier
	for _, v := range tf.Verifiers {
		verifiers = append(verifiers, model.Verifier{
			Name:    v.Name,
			Command: v.Command,
		})
	}

	// Parse timeout (default 30m)
	timeout := 30
	if tf.Timeout != "" {
		// Use time.ParseDuration for flexible parsing (e.g., "30m", "1h", "1h30m")
		if d, err := time.ParseDuration(tf.Timeout); err == nil {
			timeout = int(d.Minutes())
		}
	}

	requireApproval := true
	if tf.RequireApproval != nil {
		requireApproval = *tf.RequireApproval
	}

	task := &model.BugFixTask{
		TaskID:          tf.ID,
		Title:           tf.Title,
		Description:     tf.Description,
		Repositories:    repos,
		Verifiers:       verifiers,
		RequireApproval: requireApproval,
		TimeoutMinutes:  timeout,
	}

	if tf.TicketURL != "" {
		task.TicketURL = &tf.TicketURL
	}
	if tf.SlackChannel != "" {
		task.SlackChannel = &tf.SlackChannel
	}

	return task, nil
}

// parseVerifiers parses verifier strings in "name:command" format.
func parseVerifiers(verifierStrs []string) []model.Verifier {
	var verifiers []model.Verifier
	for _, v := range verifierStrs {
		parts := strings.SplitN(v, ":", 2)
		if len(parts) == 2 {
			verifiers = append(verifiers, model.Verifier{
				Name:    parts[0],
				Command: strings.Fields(parts[1]),
			})
		}
	}
	return verifiers
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
