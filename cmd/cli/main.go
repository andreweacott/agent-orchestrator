// Package main is the CLI entry point.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/tinkerloft/fleetlift/internal/client"
	"github.com/tinkerloft/fleetlift/internal/config"
	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/state"
)

// OutputFormat specifies the output format for CLI commands.
type OutputFormat string

const (
	OutputFormatTable OutputFormat = "table"
	OutputFormatJSON  OutputFormat = "json"
)

var rootCmd = &cobra.Command{
	Use:   "fleetlift",
	Short: "Fleetlift - Fleet-wide code transformations with AI",
	Long:  "CLI for running durable code transformations and discovery tasks across repositories",
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

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run a task from file or flags",
	Long:  "Run a transformation task from a YAML file or command-line flags",
	RunE:  runRun,
}

var reportsCmd = &cobra.Command{
	Use:   "reports [workflow-id]",
	Short: "View reports from completed workflow",
	Long:  "Display reports collected from a report-mode workflow. If workflow-id is not provided, uses the last run.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runReports,
}

var diffCmd = &cobra.Command{
	Use:   "diff",
	Short: "View changes made by workflow",
	Long:  "Display git diffs for files modified by a workflow awaiting approval",
	RunE:  runDiff,
}

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View verifier output",
	Long:  "Display verifier execution output for a workflow awaiting approval",
	RunE:  runLogs,
}

var steerCmd = &cobra.Command{
	Use:   "steer",
	Short: "Send steering prompt to workflow",
	Long:  "Send a follow-up prompt to guide Claude Code through refinements",
	RunE:  runSteer,
}

var continueCmd = &cobra.Command{
	Use:   "continue",
	Short: "Continue a paused workflow",
	Long:  "Resume execution of a workflow paused due to failure threshold",
	RunE:  runContinue,
}

var retryCmd = &cobra.Command{
	Use:   "retry",
	Short: "Retry failed groups from a completed workflow",
	Long:  "Start a new workflow with only the groups that failed in a previous run",
	RunE:  runRetry,
}

func init() {
	// Run command flags (matches design doc interface)
	runCmd.Flags().StringP("file", "f", "", "Path to task YAML file")
	runCmd.Flags().StringArray("repo", []string{}, "Repository URL (can be repeated)")
	runCmd.Flags().StringP("prompt", "p", "", "Task prompt/description")
	runCmd.Flags().StringArray("verifier", []string{}, "Verifier in format 'name:command' (can be repeated)")
	runCmd.Flags().String("branch", "main", "Branch to use for all repositories")
	runCmd.Flags().Bool("no-approval", false, "Skip human approval step")
	runCmd.Flags().String("timeout", "30m", "Timeout duration (e.g., '30m', '1h')")
	runCmd.Flags().Bool("parallel", false, "Execute PR creation in parallel for multi-repo tasks")
	runCmd.Flags().StringP("output", "o", "table", "Output format (table, json)")

	// Deterministic transformation flags
	runCmd.Flags().String("image", "", "Docker image for deterministic transformation")
	runCmd.Flags().StringArray("args", []string{}, "Arguments for transformation container")
	runCmd.Flags().StringArray("env", []string{}, "Environment variables (KEY=VALUE format)")

	// Mode flag
	runCmd.Flags().String("mode", "transform", "Task mode: transform or report")

	// Status command flags
	statusCmd.Flags().String("workflow-id", "", "Workflow ID (defaults to last run)")
	statusCmd.Flags().StringP("output", "o", "table", "Output format (table, json)")

	// Result command flags
	resultCmd.Flags().String("workflow-id", "", "Workflow ID (defaults to last run)")
	resultCmd.Flags().StringP("output", "o", "table", "Output format (table, json)")

	// Approve command flags
	approveCmd.Flags().String("workflow-id", "", "Workflow ID (defaults to last run)")

	// Reject command flags
	rejectCmd.Flags().String("workflow-id", "", "Workflow ID (defaults to last run)")

	// Cancel command flags
	cancelCmd.Flags().String("workflow-id", "", "Workflow ID (defaults to last run)")

	// List command flags
	listCmd.Flags().String("status", "", "Filter by status (Running, Completed, Failed, Canceled, Terminated)")
	listCmd.Flags().IntP("limit", "n", 10, "Maximum number of workflows to show (0 for unlimited)")
	listCmd.Flags().StringP("output", "o", "table", "Output format (table, json)")

	// Reports command flags
	reportsCmd.Flags().StringP("output", "o", "table", "Output format (table, json)")
	reportsCmd.Flags().Bool("frontmatter-only", false, "Show only frontmatter data")
	reportsCmd.Flags().String("target", "", "Filter to specific target (forEach mode)")

	// Diff command flags
	diffCmd.Flags().String("workflow-id", "", "Workflow ID (defaults to last run)")
	diffCmd.Flags().StringP("output", "o", "table", "Output format (table, json)")
	diffCmd.Flags().Bool("full", false, "Show full diff content (default: summary only)")
	diffCmd.Flags().String("file", "", "Filter to specific file path")

	// Logs command flags
	logsCmd.Flags().String("workflow-id", "", "Workflow ID (defaults to last run)")
	logsCmd.Flags().StringP("output", "o", "table", "Output format (table, json)")
	logsCmd.Flags().String("verifier", "", "Filter to specific verifier name")

	// Steer command flags
	steerCmd.Flags().String("workflow-id", "", "Workflow ID (defaults to last run)")
	steerCmd.Flags().StringP("prompt", "p", "", "Steering prompt (required)")
	_ = steerCmd.MarkFlagRequired("prompt")

	// Continue command flags
	continueCmd.Flags().String("workflow-id", "", "Workflow ID (defaults to last run)")
	continueCmd.Flags().Bool("skip-remaining", false, "Skip remaining groups after resuming")

	// Retry command flags
	retryCmd.Flags().StringP("file", "f", "", "Path to task YAML file (required)")
	retryCmd.Flags().String("workflow-id", "", "Workflow ID to retry from (defaults to last run)")
	retryCmd.Flags().Bool("failed-only", true, "Retry only failed groups (default: true)")
	_ = retryCmd.MarkFlagRequired("file")

	// Add commands
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(resultCmd)
	rootCmd.AddCommand(approveCmd)
	rootCmd.AddCommand(rejectCmd)
	rootCmd.AddCommand(cancelCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(reportsCmd)
	rootCmd.AddCommand(diffCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(steerCmd)
	rootCmd.AddCommand(continueCmd)
	rootCmd.AddCommand(retryCmd)
}

// getWorkflowID returns the workflow ID from the flag or falls back to the last workflow.
func getWorkflowID(cmd *cobra.Command) (string, error) {
	workflowID, _ := cmd.Flags().GetString("workflow-id")
	if workflowID == "" {
		var err error
		workflowID, err = state.GetLastWorkflow()
		if err != nil {
			return "", fmt.Errorf("no workflow specified and no last workflow found: %w", err)
		}
	}
	return workflowID, nil
}

func runStatus(cmd *cobra.Command, args []string) error {
	workflowID, err := getWorkflowID(cmd)
	if err != nil {
		return err
	}
	output, _ := cmd.Flags().GetString("output")

	status, err := client.GetWorkflowStatus(context.Background(), workflowID)
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	// Try to get execution progress for multi-group workflows
	progress, progressErr := client.GetExecutionProgress(context.Background(), workflowID)

	if output == "json" {
		result := map[string]interface{}{
			"workflow_id": workflowID,
			"status":      string(status),
		}
		if progressErr == nil && progress != nil {
			result["progress"] = progress
		}
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal output: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("Workflow: %s\n", workflowID)
	fmt.Printf("Status: %s\n", status)

	// Display execution progress if available
	if progressErr == nil && progress != nil && progress.TotalGroups > 0 {
		fmt.Printf("\nProgress: %d/%d groups complete\n", progress.CompletedGroups, progress.TotalGroups)
		if progress.FailedGroups > 0 {
			fmt.Printf("Failed: %d (%.1f%%)\n", progress.FailedGroups, progress.FailurePercent)
		}

		if progress.IsPaused {
			fmt.Printf("\nStatus: PAUSED\n")
			fmt.Printf("Reason: %s\n", progress.PausedReason)
			fmt.Printf("\nUse 'fleetlift continue' to resume or 'fleetlift cancel' to abort\n")
		}

		if len(progress.FailedGroupNames) > 0 {
			fmt.Printf("\nFailed groups:\n")
			for _, name := range progress.FailedGroupNames {
				fmt.Printf("  - %s\n", name)
			}
		}
	}

	return nil
}

func runResult(cmd *cobra.Command, args []string) error {
	workflowID, err := getWorkflowID(cmd)
	if err != nil {
		return err
	}
	output, _ := cmd.Flags().GetString("output")

	if output != "json" {
		fmt.Printf("Waiting for workflow %s to complete...\n", workflowID)
	}

	result, err := client.GetWorkflowResult(context.Background(), workflowID)
	if err != nil {
		return fmt.Errorf("failed to get result: %w", err)
	}

	if output == "json" {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal output: %w", err)
		}
		fmt.Println(string(data))
		return nil
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

	// Show mode-specific information
	if result.Mode == model.TaskModeReport {
		// Report mode: show report count
		var reportCount, errorCount int
		for _, repo := range result.Repositories {
			if repo.Report != nil {
				reportCount++
			}
			if repo.Error != nil || (repo.Report != nil && repo.Report.Error != "") {
				errorCount++
			}
		}
		fmt.Printf("  Mode: report\n")
		fmt.Printf("  Reports collected: %d\n", reportCount)
		if errorCount > 0 {
			fmt.Printf("  Errors: %d\n", errorCount)
		}
		fmt.Printf("\n  Use 'fleetlift reports %s' to view report details.\n", workflowID)
	} else {
		// Transform mode: show PRs
		var hasPRs bool
		for _, repo := range result.Repositories {
			if repo.PullRequest != nil {
				hasPRs = true
				break
			}
		}
		if hasPRs {
			fmt.Printf("  Pull Requests:\n")
			for _, repo := range result.Repositories {
				if repo.PullRequest != nil {
					pr := repo.PullRequest
					fmt.Printf("    - %s (#%d): %s\n", pr.RepoName, pr.PRNumber, pr.PRURL)
				}
			}
		}
	}

	return nil
}

func runApprove(cmd *cobra.Command, args []string) error {
	workflowID, err := getWorkflowID(cmd)
	if err != nil {
		return err
	}

	if err := client.ApproveWorkflow(context.Background(), workflowID); err != nil {
		return fmt.Errorf("failed to approve: %w", err)
	}

	fmt.Printf("Approved: %s\n", workflowID)
	return nil
}

func runReject(cmd *cobra.Command, args []string) error {
	workflowID, err := getWorkflowID(cmd)
	if err != nil {
		return err
	}

	if err := client.RejectWorkflow(context.Background(), workflowID); err != nil {
		return fmt.Errorf("failed to reject: %w", err)
	}

	fmt.Printf("Rejected: %s\n", workflowID)
	return nil
}

func runCancel(cmd *cobra.Command, args []string) error {
	workflowID, err := getWorkflowID(cmd)
	if err != nil {
		return err
	}

	if err := client.CancelWorkflow(context.Background(), workflowID); err != nil {
		return fmt.Errorf("failed to cancel: %w", err)
	}

	fmt.Printf("Cancelled: %s\n", workflowID)
	return nil
}

func runList(cmd *cobra.Command, args []string) error {
	statusFilter, _ := cmd.Flags().GetString("status")
	limit, _ := cmd.Flags().GetInt("limit")
	output, _ := cmd.Flags().GetString("output")

	workflows, err := client.ListWorkflows(context.Background(), statusFilter, limit)
	if err != nil {
		return fmt.Errorf("failed to list workflows: %w", err)
	}

	if output == "json" {
		data, err := json.MarshalIndent(workflows, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal output: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	if len(workflows) == 0 {
		fmt.Println("No workflows found")
		return nil
	}

	fmt.Printf("%-50s %-15s %s\n", "WORKFLOW ID", "STATUS", "START TIME")
	fmt.Println(strings.Repeat("-", 90))

	for _, wf := range workflows {
		fmt.Printf("%-50s %-15s %s\n", wf.WorkflowID, wf.Status, wf.StartTime)
	}

	return nil
}

func runRun(cmd *cobra.Command, args []string) error {
	filePath, _ := cmd.Flags().GetString("file")
	output, _ := cmd.Flags().GetString("output")
	parallel, _ := cmd.Flags().GetBool("parallel")

	var task *model.Task

	mode, _ := cmd.Flags().GetString("mode")

	if filePath != "" {
		// Load from YAML file using versioned loader
		var err error
		task, err = config.LoadTaskFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to load task file: %w", err)
		}
		// CLI flags can override file settings
		if parallel {
			// Convert repositories to individual groups for parallel execution
			if len(task.Repositories) > 1 && len(task.Groups) == 0 {
				for _, repo := range task.Repositories {
					task.Groups = append(task.Groups, model.RepositoryGroup{
						Name:         repo.Name,
						Repositories: []model.Repository{repo},
					})
				}
				task.Repositories = nil // Clear to avoid duplication
			}
			if task.MaxParallel == 0 {
				task.MaxParallel = 5
			}
		}
		if mode != "" && mode != "transform" {
			task.Mode = model.TaskMode(mode)
		}
	} else {
		// Build from flags
		repos, _ := cmd.Flags().GetStringArray("repo")
		prompt, _ := cmd.Flags().GetString("prompt")
		verifiers, _ := cmd.Flags().GetStringArray("verifier")
		branch, _ := cmd.Flags().GetString("branch")
		noApproval, _ := cmd.Flags().GetBool("no-approval")
		timeout, _ := cmd.Flags().GetString("timeout")

		// Deterministic transformation flags
		image, _ := cmd.Flags().GetString("image")
		transformArgs, _ := cmd.Flags().GetStringArray("args")
		envVars, _ := cmd.Flags().GetStringArray("env")

		if len(repos) == 0 {
			return fmt.Errorf("at least one --repo is required (or use --file)")
		}

		// Validate: either --prompt (agentic) or --image (deterministic) required
		if image == "" && prompt == "" {
			return fmt.Errorf("--prompt required (or use --image for deterministic transformation)")
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

		// Parse environment variables
		parsedEnv := parseEnvVars(envVars)

		// Build title
		title := prompt
		if image != "" && title == "" {
			title = fmt.Sprintf("Deterministic transformation: %s", image)
		}

		// Determine task mode
		taskMode := model.TaskModeTransform
		if mode == "report" {
			taskMode = model.TaskModeReport
		}

		// Build task
		task = &model.Task{
			Version:         model.SchemaVersion,
			ID:              taskID,
			Title:           title,
			Mode:            taskMode,
			Repositories:    repositories,
			Timeout:         timeout,
			RequireApproval: !noApproval,
			MaxParallel:     5,
		}

		// If parallel flag is set, convert repos to individual groups
		if parallel && len(repositories) > 1 {
			for _, repo := range repositories {
				task.Groups = append(task.Groups, model.RepositoryGroup{
					Name:         repo.Name,
					Repositories: []model.Repository{repo},
				})
			}
			task.Repositories = nil // Clear to avoid duplication
		}

		// Set execution configuration
		if image != "" {
			task.Execution.Deterministic = &model.DeterministicExecution{
				Image:     image,
				Args:      transformArgs,
				Env:       parsedEnv,
				Verifiers: parsedVerifiers,
			}
		} else {
			task.Execution.Agentic = &model.AgenticExecution{
				Prompt:    prompt,
				Verifiers: parsedVerifiers,
			}
		}
	}

	workflowID, err := client.StartTransform(context.Background(), *task)
	if err != nil {
		return fmt.Errorf("failed to start workflow: %w", err)
	}

	// Save workflow ID for later status/reports commands
	if saveErr := state.SaveLastWorkflow(workflowID); saveErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save last workflow: %v\n", saveErr)
	}

	executionType := task.Execution.GetExecutionType()

	if output == "json" {
		result := map[string]interface{}{
			"task_id":          task.ID,
			"title":            task.Title,
			"workflow_id":      workflowID,
			"mode":             string(task.GetMode()),
			"groups":           len(task.GetExecutionGroups()),
			"verifiers":        len(task.Execution.GetVerifiers()),
			"require_approval": task.RequireApproval,
			"max_parallel":     task.GetMaxParallel(),
			"execution_type":   string(executionType),
			"url":              fmt.Sprintf("http://localhost:8233/namespaces/default/workflows/%s", workflowID),
		}
		if executionType == model.ExecutionTypeDeterministic {
			result["image"] = task.Execution.Deterministic.Image
		}
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal output: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("Starting task...\n")
	fmt.Printf("  Task ID: %s\n", task.ID)
	fmt.Printf("  Title: %s\n", task.Title)
	fmt.Printf("  Mode: %s\n", task.GetMode())
	fmt.Printf("  Execution Type: %s\n", executionType)
	if executionType == model.ExecutionTypeDeterministic {
		fmt.Printf("  Image: %s\n", task.Execution.Deterministic.Image)
	}
	groups := task.GetExecutionGroups()
	fmt.Printf("  Groups: %d", len(groups))
	if len(groups) > 1 {
		fmt.Printf(" (max parallel: %d)", task.GetMaxParallel())
	}
	fmt.Printf("\n")
	fmt.Printf("  Verifiers: %d\n", len(task.Execution.GetVerifiers()))
	fmt.Printf("  Require approval: %v\n\n", task.RequireApproval)

	fmt.Printf("Workflow started: %s\n", workflowID)
	fmt.Printf("View at: http://localhost:8233/namespaces/default/workflows/%s\n", workflowID)

	return nil
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

// parseEnvVars parses environment variable strings in "KEY=VALUE" format.
func parseEnvVars(envStrs []string) map[string]string {
	env := make(map[string]string)
	for _, e := range envStrs {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			env[parts[0]] = parts[1]
		}
	}
	return env
}

func runReports(cmd *cobra.Command, args []string) error {
	var workflowID string
	if len(args) > 0 {
		workflowID = args[0]
	} else {
		var err error
		workflowID, err = state.GetLastWorkflow()
		if err != nil {
			return fmt.Errorf("no workflow specified and no last workflow found: %w", err)
		}
	}
	output, _ := cmd.Flags().GetString("output")
	frontmatterOnly, _ := cmd.Flags().GetBool("frontmatter-only")
	targetFilter, _ := cmd.Flags().GetString("target")

	result, err := client.GetWorkflowResult(context.Background(), workflowID)
	if err != nil {
		return fmt.Errorf("failed to get workflow result: %w", err)
	}

	// Check if this is a report-mode task
	if result.Mode != model.TaskModeReport {
		return fmt.Errorf("workflow %s is not a report-mode task (mode: %s)", workflowID, result.Mode)
	}

	if output == "json" {
		var outputData interface{}
		if frontmatterOnly {
			// Extract just frontmatter from each repo/target
			frontmatters := make(map[string]interface{})
			for _, repo := range result.Repositories {
				if len(repo.ForEachResults) > 0 {
					// forEach mode: organize by target
					targetFrontmatters := make(map[string]map[string]any)
					for _, fe := range repo.ForEachResults {
						if targetFilter != "" && fe.Target.Name != targetFilter {
							continue
						}
						if fe.Report != nil && fe.Report.Frontmatter != nil {
							targetFrontmatters[fe.Target.Name] = fe.Report.Frontmatter
						}
					}
					frontmatters[repo.Repository] = targetFrontmatters
				} else {
					// Single report mode
					if repo.Report != nil && repo.Report.Frontmatter != nil {
						frontmatters[repo.Repository] = repo.Report.Frontmatter
					}
				}
			}
			outputData = frontmatters
		} else {
			// Filter by target if specified
			if targetFilter != "" {
				filteredRepos := make([]model.RepositoryResult, 0, len(result.Repositories))
				for _, repo := range result.Repositories {
					if len(repo.ForEachResults) > 0 {
						filteredResults := make([]model.ForEachExecution, 0)
						for _, fe := range repo.ForEachResults {
							if fe.Target.Name == targetFilter {
								filteredResults = append(filteredResults, fe)
							}
						}
						if len(filteredResults) > 0 {
							filteredRepo := repo
							filteredRepo.ForEachResults = filteredResults
							filteredRepos = append(filteredRepos, filteredRepo)
						}
					}
				}
				outputData = filteredRepos
			} else {
				outputData = result.Repositories
			}
		}
		data, err := json.MarshalIndent(outputData, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal output: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	// Table output
	fmt.Printf("Reports for workflow: %s\n", workflowID)
	fmt.Printf("Status: %s\n\n", result.Status)

	for _, repo := range result.Repositories {
		fmt.Printf("Repository: %s\n", repo.Repository)
		fmt.Printf("  Status: %s\n", repo.Status)

		if repo.Error != nil {
			fmt.Printf("  Error: %s\n", *repo.Error)
			fmt.Println()
			continue
		}

		// Check if this is forEach mode
		if len(repo.ForEachResults) > 0 {
			fmt.Printf("  Targets: %d\n\n", len(repo.ForEachResults))

			for _, fe := range repo.ForEachResults {
				// Filter by target if specified
				if targetFilter != "" && fe.Target.Name != targetFilter {
					continue
				}

				fmt.Printf("  Target: %s\n", fe.Target.Name)
				if fe.Target.Context != "" {
					fmt.Printf("    Context: %s\n", fe.Target.Context)
				}

				if fe.Error != nil {
					fmt.Printf("    Error: %s\n", *fe.Error)
					fmt.Println()
					continue
				}

				if fe.Report == nil {
					fmt.Println("    No report collected")
					fmt.Println()
					continue
				}

				displayReport(fe.Report, "    ", frontmatterOnly)
				fmt.Println()
			}
		} else {
			// Single report mode (existing behavior)
			if repo.Report == nil {
				fmt.Println("  No report collected")
				fmt.Println()
				continue
			}

			displayReport(repo.Report, "  ", frontmatterOnly)
			fmt.Println()
		}
	}

	return nil
}

// displayReport displays a single report's content with the given indent prefix.
func displayReport(report *model.ReportOutput, indent string, frontmatterOnly bool) {
	if report.Error != "" {
		fmt.Printf("%sParse Error: %s\n", indent, report.Error)
	}

	if len(report.ValidationErrors) > 0 {
		fmt.Printf("%sValidation Errors:\n", indent)
		for _, verr := range report.ValidationErrors {
			fmt.Printf("%s  - %s\n", indent, verr)
		}
	}

	if report.Frontmatter != nil {
		fmt.Printf("%sFrontmatter:\n", indent)
		for k, v := range report.Frontmatter {
			fmt.Printf("%s  %s: %v\n", indent, k, v)
		}
	}

	if !frontmatterOnly && report.Body != "" {
		fmt.Printf("%sBody:\n", indent)
		body := report.Body
		// Use rune slicing to avoid splitting multi-byte UTF-8 characters
		bodyRunes := []rune(body)
		if len(bodyRunes) > 200 {
			body = string(bodyRunes[:200]) + "..."
		}
		fmt.Printf("%s  %s\n", indent, strings.ReplaceAll(body, "\n", "\n"+indent+"  "))
	}
}

func runDiff(cmd *cobra.Command, args []string) error {
	workflowID, err := getWorkflowID(cmd)
	if err != nil {
		return err
	}
	output, _ := cmd.Flags().GetString("output")
	fullDiff, _ := cmd.Flags().GetBool("full")
	fileFilter, _ := cmd.Flags().GetString("file")

	diffs, err := client.GetWorkflowDiff(context.Background(), workflowID)
	if err != nil {
		return fmt.Errorf("failed to get diff: %w", err)
	}

	// Filter by file if specified
	if fileFilter != "" {
		var filteredDiffs []model.DiffOutput
		for _, d := range diffs {
			var filteredFiles []model.FileDiff
			for _, f := range d.Files {
				if strings.Contains(f.Path, fileFilter) {
					filteredFiles = append(filteredFiles, f)
				}
			}
			if len(filteredFiles) > 0 {
				filtered := d
				filtered.Files = filteredFiles
				filteredDiffs = append(filteredDiffs, filtered)
			}
		}
		diffs = filteredDiffs
	}

	if output == "json" {
		data, err := json.MarshalIndent(diffs, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal output: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	if len(diffs) == 0 {
		fmt.Println("No changes detected or workflow not in approval state")
		return nil
	}

	fmt.Printf("Diff for workflow: %s\n\n", workflowID)

	for _, diff := range diffs {
		fmt.Printf("Repository: %s\n", diff.Repository)
		if len(diff.Files) == 0 {
			fmt.Println("  No changes")
			fmt.Println()
			continue
		}

		fmt.Printf("  Summary: %s\n", diff.Summary)
		if diff.Truncated {
			fmt.Println("  (output truncated)")
		}
		fmt.Println()

		for _, f := range diff.Files {
			fmt.Printf("  %s (%s, +%d/-%d)\n", f.Path, f.Status, f.Additions, f.Deletions)
			if fullDiff && f.Diff != "" {
				// Indent the diff output
				lines := strings.Split(f.Diff, "\n")
				for _, line := range lines {
					if line != "" {
						fmt.Printf("    %s\n", line)
					}
				}
				fmt.Println()
			}
		}
		fmt.Println()
	}

	return nil
}

func runLogs(cmd *cobra.Command, args []string) error {
	workflowID, err := getWorkflowID(cmd)
	if err != nil {
		return err
	}
	output, _ := cmd.Flags().GetString("output")
	verifierFilter, _ := cmd.Flags().GetString("verifier")

	logs, err := client.GetWorkflowVerifierLogs(context.Background(), workflowID)
	if err != nil {
		return fmt.Errorf("failed to get verifier logs: %w", err)
	}

	// Filter by verifier if specified
	if verifierFilter != "" {
		var filtered []model.VerifierOutput
		for _, l := range logs {
			if strings.Contains(l.Verifier, verifierFilter) {
				filtered = append(filtered, l)
			}
		}
		logs = filtered
	}

	if output == "json" {
		data, err := json.MarshalIndent(logs, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal output: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	if len(logs) == 0 {
		fmt.Println("No verifier output available or workflow not in approval state")
		return nil
	}

	fmt.Printf("Verifier logs for workflow: %s\n\n", workflowID)

	for _, log := range logs {
		status := "PASS"
		if !log.Success {
			status = "FAIL"
		}
		fmt.Printf("[%s] %s (exit code: %d)\n", status, log.Verifier, log.ExitCode)

		if log.Stdout != "" {
			fmt.Println("  stdout:")
			lines := strings.Split(log.Stdout, "\n")
			for _, line := range lines {
				if line != "" {
					fmt.Printf("    %s\n", line)
				}
			}
		}

		if log.Stderr != "" {
			fmt.Println("  stderr:")
			lines := strings.Split(log.Stderr, "\n")
			for _, line := range lines {
				if line != "" {
					fmt.Printf("    %s\n", line)
				}
			}
		}
		fmt.Println()
	}

	return nil
}

func runSteer(cmd *cobra.Command, args []string) error {
	workflowID, err := getWorkflowID(cmd)
	if err != nil {
		return err
	}
	prompt, _ := cmd.Flags().GetString("prompt")

	if prompt == "" {
		return fmt.Errorf("--prompt is required")
	}

	if err := client.SteerWorkflow(context.Background(), workflowID, prompt); err != nil {
		return fmt.Errorf("failed to send steering signal: %w", err)
	}

	fmt.Printf("Steering signal sent to: %s\n", workflowID)
	fmt.Println("Use 'fleetlift status' to monitor progress.")
	return nil
}

func runContinue(cmd *cobra.Command, args []string) error {
	workflowID, err := getWorkflowID(cmd)
	if err != nil {
		return err
	}
	skipRemaining, _ := cmd.Flags().GetBool("skip-remaining")

	if err := client.ContinueWorkflow(context.Background(), workflowID, skipRemaining); err != nil {
		return fmt.Errorf("failed to send continue signal: %w", err)
	}

	fmt.Printf("Continue signal sent to: %s\n", workflowID)
	if skipRemaining {
		fmt.Println("Remaining groups will be skipped.")
	}
	fmt.Println("Use 'fleetlift status' to monitor progress.")
	return nil
}

func runRetry(cmd *cobra.Command, args []string) error {
	filePath, _ := cmd.Flags().GetString("file")
	workflowID, err := getWorkflowID(cmd)
	if err != nil {
		return err
	}
	failedOnly, _ := cmd.Flags().GetBool("failed-only")

	// Get the completed workflow result
	result, err := client.GetWorkflowResult(context.Background(), workflowID)
	if err != nil {
		return fmt.Errorf("failed to get workflow result: %w", err)
	}

	// Load task from file
	task, err := config.LoadTaskFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to load task file: %w", err)
	}

	if failedOnly {
		// Extract failed group names from result
		var failedGroupNames []string
		if len(result.Groups) > 0 {
			// Use Groups field if available
			for _, gr := range result.Groups {
				if gr.Status == "failed" {
					failedGroupNames = append(failedGroupNames, gr.GroupName)
				}
			}
		} else {
			// Fall back to checking repository results
			// Map repos to groups from the original task
			repoToGroup := make(map[string]string)
			for _, group := range task.Groups {
				for _, repo := range group.Repositories {
					repoToGroup[repo.Name] = group.Name
				}
			}

			failedGroups := make(map[string]bool)
			for _, repoResult := range result.Repositories {
				if repoResult.Status == "failed" {
					if groupName, ok := repoToGroup[repoResult.Repository]; ok {
						failedGroups[groupName] = true
					}
				}
			}

			for name := range failedGroups {
				failedGroupNames = append(failedGroupNames, name)
			}
		}

		if len(failedGroupNames) == 0 {
			fmt.Println("No failed groups found in the workflow result.")
			return nil
		}

		// Filter task.Groups to only include failed groups
		var filteredGroups []model.RepositoryGroup
		for _, group := range task.Groups {
			for _, failedName := range failedGroupNames {
				if group.Name == failedName {
					filteredGroups = append(filteredGroups, group)
					break
				}
			}
		}

		if len(filteredGroups) == 0 {
			return fmt.Errorf("failed groups not found in task file: %v", failedGroupNames)
		}

		task.Groups = filteredGroups
		task.Repositories = nil // Clear to avoid conflicts

		fmt.Printf("Retrying %d failed groups: %v\n", len(filteredGroups), failedGroupNames)
	}

	// Start new workflow with filtered task
	newWorkflowID, err := client.StartTransform(context.Background(), *task)
	if err != nil {
		return fmt.Errorf("failed to start retry workflow: %w", err)
	}

	// Save workflow ID for later status commands
	if saveErr := state.SaveLastWorkflow(newWorkflowID); saveErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save last workflow: %v\n", saveErr)
	}

	fmt.Printf("Retry workflow started: %s\n", newWorkflowID)
	fmt.Printf("Original workflow: %s\n", workflowID)
	fmt.Printf("View at: http://localhost:8233/namespaces/default/workflows/%s\n", newWorkflowID)

	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
