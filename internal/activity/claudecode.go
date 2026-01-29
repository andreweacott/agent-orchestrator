package activity

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.temporal.io/sdk/activity"

	"github.com/anthropics/claude-code-orchestrator/internal/docker"
	"github.com/anthropics/claude-code-orchestrator/internal/model"
)

// ClaudeCodeActivities contains activities for executing Claude Code.
type ClaudeCodeActivities struct {
	DockerClient *docker.Client
}

// NewClaudeCodeActivities creates a new ClaudeCodeActivities instance.
func NewClaudeCodeActivities(client *docker.Client) *ClaudeCodeActivities {
	return &ClaudeCodeActivities{DockerClient: client}
}

// Patterns that indicate Claude needs clarification
var clarificationPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)need more information`),
	regexp.MustCompile(`(?i)could you clarify`),
	regexp.MustCompile(`(?i)please specify`),
	regexp.MustCompile(`(?i)which.*should I`),
	regexp.MustCompile(`(?i)I'm not sure`),
	regexp.MustCompile(`(?i)can you provide`),
	regexp.MustCompile(`(?i)unclear`),
}

// RunClaudeCode executes Claude Code in the sandbox container.
func (a *ClaudeCodeActivities) RunClaudeCode(ctx context.Context, containerID, prompt string, timeoutSeconds int) (*model.ClaudeCodeResult, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Starting Claude Code execution")

	// Escape the prompt for shell
	escapedPrompt := strings.ReplaceAll(prompt, `"`, `\"`)
	escapedPrompt = strings.ReplaceAll(escapedPrompt, `$`, `\$`)

	// Build the Claude Code command
	command := fmt.Sprintf(`cd /workspace && claude -p "%s" --dangerously-skip-permissions --allowedTools Read,Write,Edit,Bash --output-format json 2>&1`,
		escapedPrompt)

	result, err := a.DockerClient.ExecShellCommand(ctx, containerID, command, "agent")
	if err != nil {
		logger.Error("Claude Code execution failed", "error", err)
		errorStr := err.Error()
		return &model.ClaudeCodeResult{
			Success: false,
			Output:  "",
			Error:   &errorStr,
		}, nil
	}

	logger.Info("Claude Code exited", "exitCode", result.ExitCode)

	// Get list of modified files across all repos
	modifiedFiles := a.getModifiedFiles(ctx, containerID)

	// Check if Claude needs clarification
	needsClarification := false
	var clarificationQuestion *string

	output := result.Stdout
	if result.Stderr != "" {
		output = result.Stdout + "\n" + result.Stderr
	}

	for _, pattern := range clarificationPatterns {
		if pattern.MatchString(output) {
			needsClarification = true
			clarificationQuestion = &output
			break
		}
	}

	var errorMsg *string
	if result.ExitCode != 0 {
		errStr := output
		errorMsg = &errStr
	}

	return &model.ClaudeCodeResult{
		Success:               result.ExitCode == 0,
		Output:                output,
		FilesModified:         modifiedFiles,
		Error:                 errorMsg,
		NeedsClarification:    needsClarification,
		ClarificationQuestion: clarificationQuestion,
	}, nil
}

// getModifiedFiles returns a list of modified files across all repos in the workspace.
func (a *ClaudeCodeActivities) getModifiedFiles(ctx context.Context, containerID string) []string {
	// Find all git repos and get their status
	cmd := `cd /workspace && find . -name '.git' -type d -exec dirname {} \; | while read repo; do cd /workspace/$repo 2>/dev/null && git status --porcelain | sed "s|^|$repo/|" 2>/dev/null; done`

	result, err := a.DockerClient.ExecShellCommand(ctx, containerID, cmd, "agent")
	if err != nil {
		return nil
	}

	var files []string
	for _, line := range strings.Split(result.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "fatal:") {
			// Parse git status output: "XY path" -> extract path
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				files = append(files, parts[len(parts)-1])
			}
		}
	}

	return files
}

// GetClaudeOutput gets the git diff and status for a repository.
func (a *ClaudeCodeActivities) GetClaudeOutput(ctx context.Context, containerID, repoName string) (map[string]string, error) {
	// Get diff
	diffCmd := fmt.Sprintf("cd /workspace/%s && git diff", repoName)
	diffResult, err := a.DockerClient.ExecShellCommand(ctx, containerID, diffCmd, "agent")
	if err != nil {
		return nil, fmt.Errorf("failed to get diff: %w", err)
	}

	// Get status
	statusCmd := fmt.Sprintf("cd /workspace/%s && git status --short", repoName)
	statusResult, err := a.DockerClient.ExecShellCommand(ctx, containerID, statusCmd, "agent")
	if err != nil {
		return nil, fmt.Errorf("failed to get status: %w", err)
	}

	return map[string]string{
		"repo":   repoName,
		"diff":   diffResult.Stdout,
		"status": statusResult.Stdout,
	}, nil
}
