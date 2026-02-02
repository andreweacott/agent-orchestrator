// Package main is the CLI entry point.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/artisanlabs/fleetlift/internal/client"
	"github.com/artisanlabs/fleetlift/internal/config"
	"github.com/artisanlabs/fleetlift/internal/model"
)

// OutputFormat specifies the output format for CLI commands.
type OutputFormat string

const (
	OutputFormatTable OutputFormat = "table"
	OutputFormatJSON  OutputFormat = "json"
)

// must panics if err is non-nil. Used for initialization errors.
func must(err error) {
	if err != nil {
		panic(fmt.Errorf("initialization error: %w", err))
	}
}

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
	Use:   "reports <workflow-id>",
	Short: "View reports from completed workflow",
	Long:  "Display reports collected from a report-mode workflow",
	Args:  cobra.ExactArgs(1),
	RunE:  runReports,
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
	statusCmd.Flags().String("workflow-id", "", "Workflow ID (required)")
	statusCmd.Flags().StringP("output", "o", "table", "Output format (table, json)")
	must(statusCmd.MarkFlagRequired("workflow-id"))

	// Result command flags
	resultCmd.Flags().String("workflow-id", "", "Workflow ID (required)")
	resultCmd.Flags().StringP("output", "o", "table", "Output format (table, json)")
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
	listCmd.Flags().StringP("output", "o", "table", "Output format (table, json)")

	// Reports command flags
	reportsCmd.Flags().StringP("output", "o", "table", "Output format (table, json)")
	reportsCmd.Flags().Bool("frontmatter-only", false, "Show only frontmatter data")
	reportsCmd.Flags().String("target", "", "Filter to specific target (forEach mode)")

	// Add commands
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(resultCmd)
	rootCmd.AddCommand(approveCmd)
	rootCmd.AddCommand(rejectCmd)
	rootCmd.AddCommand(cancelCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(reportsCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	workflowID, _ := cmd.Flags().GetString("workflow-id")
	output, _ := cmd.Flags().GetString("output")

	status, err := client.GetWorkflowStatus(context.Background(), workflowID)
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	if output == "json" {
		result := map[string]string{
			"workflow_id": workflowID,
			"status":      string(status),
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

	return nil
}

func runResult(cmd *cobra.Command, args []string) error {
	workflowID, _ := cmd.Flags().GetString("workflow-id")
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
	output, _ := cmd.Flags().GetString("output")

	workflows, err := client.ListWorkflows(context.Background(), statusFilter)
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

	fmt.Printf("%-40s %-15s %s\n", "WORKFLOW ID", "STATUS", "START TIME")
	fmt.Println(strings.Repeat("-", 80))

	for _, wf := range workflows {
		fmt.Printf("%-40s %-15s %s\n", wf.WorkflowID, wf.Status, wf.StartTime)
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
	workflowID := args[0]
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

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
