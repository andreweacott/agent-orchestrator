// Package activity contains Temporal activity implementations.
package activity

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"go.temporal.io/sdk/activity"

	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/sandbox"
)

// Package-level compiled regex patterns for performance.
var (
	diffAddPattern = regexp.MustCompile(`^\+[^+]`)
	diffDelPattern = regexp.MustCompile(`^-[^-]`)
)

// SteeringActivities contains activities for HITL steering operations.
type SteeringActivities struct {
	Provider sandbox.Provider
}

// NewSteeringActivities creates a new SteeringActivities instance.
func NewSteeringActivities(provider sandbox.Provider) *SteeringActivities {
	return &SteeringActivities{Provider: provider}
}

// GetDiffInput defines the input for GetDiff activity.
type GetDiffInput struct {
	ContainerID             string
	Repos                   []model.Repository
	UseTransformationLayout bool
	MaxLines                int // 0 = default 1000
}

// GetVerifierOutputInput defines the input for GetVerifierOutput activity.
type GetVerifierOutputInput struct {
	ContainerID             string
	Repos                   []model.Repository
	Verifiers               []model.Verifier
	UseTransformationLayout bool
}

// GetDiff returns git diffs for modified files in repositories.
func (a *SteeringActivities) GetDiff(ctx context.Context, input GetDiffInput) ([]model.DiffOutput, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Getting diffs", "repos", len(input.Repos))

	maxLines := input.MaxLines
	if maxLines <= 0 {
		maxLines = 1000
	}

	// Determine base path
	basePath := "/workspace"
	if input.UseTransformationLayout {
		basePath = "/workspace/targets"
	}

	var diffs []model.DiffOutput

	for _, repo := range input.Repos {
		repoPath := fmt.Sprintf("%s/%s", basePath, repo.Name)
		logger.Info("Getting diff for repository", "repo", repo.Name, "path", repoPath)

		diffOutput := model.DiffOutput{
			Repository: repo.Name,
			Files:      []model.FileDiff{},
		}

		// Get list of changed files with status
		statusCmd := fmt.Sprintf("cd %s && git status --porcelain", repoPath)
		result, err := a.Provider.ExecShell(ctx, input.ContainerID, statusCmd, AgentUser)
		if err != nil {
			logger.Warn("Failed to get git status", "repo", repo.Name, "error", err)
			diffs = append(diffs, diffOutput)
			continue
		}

		if result.ExitCode != 0 || strings.TrimSpace(result.Stdout) == "" {
			// No changes or error
			diffs = append(diffs, diffOutput)
			continue
		}

		// Parse git status output
		changedFiles := parseGitStatus(result.Stdout)
		if len(changedFiles) == 0 {
			diffs = append(diffs, diffOutput)
			continue
		}

		// Get detailed diff for each file
		totalLines := 0
		truncated := false

		for _, file := range changedFiles {
			if truncated {
				break
			}

			fileDiff := model.FileDiff{
				Path:   file.path,
				Status: file.status,
			}

			// Get diff for this file - use shell quoting to prevent command injection
			escapedPath := ShellQuote(file.path)
			var diffCmd string
			if file.status == "added" || file.status == "untracked" {
				// For new files, show the entire content
				diffCmd = fmt.Sprintf("cd %s && git diff --no-color -- %s 2>/dev/null || cat %s", repoPath, escapedPath, escapedPath)
			} else if file.status == "deleted" {
				// For deleted files, show what was removed
				diffCmd = fmt.Sprintf("cd %s && git diff --no-color -- %s", repoPath, escapedPath)
			} else {
				// For modified files
				diffCmd = fmt.Sprintf("cd %s && git diff --no-color -- %s", repoPath, escapedPath)
			}

			diffResult, err := a.Provider.ExecShell(ctx, input.ContainerID, diffCmd, AgentUser)
			if err != nil {
				logger.Warn("Failed to get diff for file", "file", file.path, "error", err)
				continue
			}

			diffContent := diffResult.Stdout
			diffLines := strings.Count(diffContent, "\n")

			// Check if we'd exceed max lines
			if totalLines+diffLines > maxLines {
				// Truncate this diff
				lines := strings.Split(diffContent, "\n")
				remaining := maxLines - totalLines
				if remaining > 0 {
					diffContent = strings.Join(lines[:remaining], "\n") + "\n... (truncated)"
					diffLines = remaining // Update to actual line count
				} else {
					diffContent = "... (truncated)"
					diffLines = 1 // The truncation message counts as one line
				}
				truncated = true
			}

			fileDiff.Diff = diffContent
			fileDiff.Additions, fileDiff.Deletions = countAdditionsDeletions(diffContent)
			totalLines += diffLines

			diffOutput.Files = append(diffOutput.Files, fileDiff)
		}

		// Build summary
		var additions, deletions int
		for _, f := range diffOutput.Files {
			additions += f.Additions
			deletions += f.Deletions
		}
		diffOutput.Summary = fmt.Sprintf("%d files changed, +%d, -%d", len(diffOutput.Files), additions, deletions)
		diffOutput.TotalLines = totalLines
		diffOutput.Truncated = truncated

		diffs = append(diffs, diffOutput)
		activity.RecordHeartbeat(ctx, fmt.Sprintf("Got diff for %s", repo.Name))
	}

	return diffs, nil
}

// fileStatus represents a file's git status.
type fileStatus struct {
	path   string
	status string // "modified", "added", "deleted", "untracked"
}

// parseGitStatus parses git status --porcelain output.
func parseGitStatus(output string) []fileStatus {
	var files []fileStatus
	lines := strings.Split(strings.TrimSpace(output), "\n")

	for _, line := range lines {
		if len(line) < 3 {
			continue
		}

		statusCode := line[:2]
		path := strings.TrimSpace(line[2:])

		// Handle renamed files (R  old -> new)
		if strings.Contains(path, " -> ") {
			parts := strings.Split(path, " -> ")
			if len(parts) == 2 {
				path = parts[1]
			}
		}

		var status string
		switch {
		case statusCode == "??" || statusCode == "A " || statusCode == " A":
			status = "added"
		case statusCode == "D " || statusCode == " D":
			status = "deleted"
		case statusCode == "M " || statusCode == " M" || statusCode == "MM":
			status = "modified"
		case statusCode[0] == 'R':
			status = "modified" // Treat renamed as modified
		default:
			status = "modified"
		}

		files = append(files, fileStatus{path: path, status: status})
	}

	return files
}

// countAdditionsDeletions counts + and - lines in a diff.
func countAdditionsDeletions(diff string) (additions, deletions int) {
	lines := strings.Split(diff, "\n")

	for _, line := range lines {
		if diffAddPattern.MatchString(line) {
			additions++
		} else if diffDelPattern.MatchString(line) {
			deletions++
		}
	}
	return
}

// GetVerifierOutput runs verifiers and returns detailed output.
func (a *SteeringActivities) GetVerifierOutput(ctx context.Context, input GetVerifierOutputInput) ([]model.VerifierOutput, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Getting verifier output", "verifiers", len(input.Verifiers), "repos", len(input.Repos))

	if len(input.Verifiers) == 0 {
		return []model.VerifierOutput{}, nil
	}

	// Determine base path
	basePath := "/workspace"
	if input.UseTransformationLayout {
		basePath = "/workspace/targets"
	}

	var outputs []model.VerifierOutput

	for _, repo := range input.Repos {
		repoPath := fmt.Sprintf("%s/%s", basePath, repo.Name)

		for _, verifier := range input.Verifiers {
			verifierName := fmt.Sprintf("%s:%s", repo.Name, verifier.Name)
			logger.Info("Running verifier", "name", verifierName)

			// Build command string from verifier.Command slice
			cmdStr := strings.Join(verifier.Command, " ")
			fullCmd := fmt.Sprintf("cd %s && %s", repoPath, cmdStr)

			result, err := a.Provider.ExecShell(ctx, input.ContainerID, fullCmd, AgentUser)

			output := model.VerifierOutput{
				Verifier: verifierName,
			}

			if err != nil {
				output.ExitCode = -1
				output.Stderr = err.Error()
				output.Success = false
			} else {
				output.ExitCode = result.ExitCode
				output.Stdout = result.Stdout
				output.Stderr = result.Stderr
				output.Success = result.ExitCode == 0
			}

			outputs = append(outputs, output)
			logger.Info("Verifier completed", "name", verifierName, "success", output.Success)
			activity.RecordHeartbeat(ctx, fmt.Sprintf("Verifier %s: %v", verifierName, output.Success))
		}
	}

	return outputs, nil
}

// BuildSteeringPrompt creates a prompt for Claude Code with steering context.
func BuildSteeringPrompt(basePrompt string, steeringPrompt string, iteration int, previousOutput string) string {
	var sb strings.Builder

	sb.WriteString("## Steering Iteration ")
	sb.WriteString(strconv.Itoa(iteration))
	sb.WriteString("\n\n")

	sb.WriteString("### Original Task\n")
	sb.WriteString(basePrompt)
	sb.WriteString("\n\n")

	sb.WriteString("### Feedback\n")
	sb.WriteString(steeringPrompt)
	sb.WriteString("\n\n")

	if previousOutput != "" {
		sb.WriteString("### Previous Changes Summary\n")
		sb.WriteString(previousOutput)
		sb.WriteString("\n\n")
	}

	sb.WriteString("Please address the feedback above and make the necessary changes. ")
	sb.WriteString("Build on your previous work rather than starting over.")

	return sb.String()
}
