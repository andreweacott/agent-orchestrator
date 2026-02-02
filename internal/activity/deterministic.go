// Package activity contains Temporal activity implementations.
package activity

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"go.temporal.io/sdk/activity"

	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/sandbox"
)

// shellQuote properly quotes a string for safe use in shell commands.
func shellQuote(s string) string {
	escaped := strings.ReplaceAll(s, "'", "'\"'\"'")
	return "'" + escaped + "'"
}

// isValidEnvKey validates environment variable key format.
func isValidEnvKey(key string) bool {
	if len(key) == 0 {
		return false
	}
	for i, c := range key {
		isUpper := c >= 'A' && c <= 'Z'
		isLower := c >= 'a' && c <= 'z'
		isDigit := c >= '0' && c <= '9'
		isUnderscore := c == '_'

		if i == 0 {
			if !isUpper && !isLower && !isUnderscore {
				return false
			}
		} else {
			if !isUpper && !isLower && !isDigit && !isUnderscore {
				return false
			}
		}
	}
	return true
}

// DeterministicActivities contains activities for running deterministic transformations.
type DeterministicActivities struct {
	Provider sandbox.Provider
}

// NewDeterministicActivities creates a new DeterministicActivities instance.
func NewDeterministicActivities(provider sandbox.Provider) *DeterministicActivities {
	return &DeterministicActivities{Provider: provider}
}

// ExecuteDeterministic runs a deterministic transformation using a Docker image.
// It executes the transformation container with the workspace mounted and captures
// the output, exit code, and modified files.
func (a *DeterministicActivities) ExecuteDeterministic(
	ctx context.Context,
	sandboxInfo model.SandboxInfo,
	image string,
	args []string,
	env map[string]string,
	repos []model.Repository,
) (*model.DeterministicResult, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Starting deterministic transformation",
		"image", image,
		"args", args,
		"repos", len(repos))

	// Validate environment variable keys upfront (fail fast instead of silent skip)
	for key := range env {
		if !isValidEnvKey(key) {
			errMsg := fmt.Sprintf("invalid environment variable key: %q (must match [A-Za-z_][A-Za-z0-9_]*)", key)
			return &model.DeterministicResult{
				Success:  false,
				ExitCode: -1,
				Error:    &errMsg,
			}, nil
		}
	}

	// Build the docker run command to execute within the sandbox
	// The transformation container mounts the same workspace directory
	dockerCmd := buildDockerRunCommand(image, args, env)

	logger.Info("Executing docker run command", "command", dockerCmd)
	activity.RecordHeartbeat(ctx, "Running transformation container")

	// Execute docker run inside the sandbox container
	// Note: Infrastructure errors (network, docker daemon) return actual errors for Temporal retry
	// Non-zero exit codes are non-retriable application failures, returned via result struct
	result, err := a.Provider.ExecShell(ctx, sandboxInfo.ContainerID, dockerCmd, AgentUser)
	if err != nil {
		// Return actual error for retriable infrastructure failures (Temporal will retry)
		return nil, fmt.Errorf("failed to execute transformation: %w", err)
	}

	output := result.Stdout
	if result.Stderr != "" {
		output += "\n" + result.Stderr
	}

	// Check if transformation succeeded
	if result.ExitCode != 0 {
		logger.Warn("Transformation failed", "exitCode", result.ExitCode)
		errMsg := fmt.Sprintf("transformation exited with code %d", result.ExitCode)
		return &model.DeterministicResult{
			Success:  false,
			ExitCode: result.ExitCode,
			Output:   output,
			Error:    &errMsg,
		}, nil
	}

	activity.RecordHeartbeat(ctx, "Detecting modified files")

	// Detect modified files across all repositories
	filesModified, err := a.detectModifiedFiles(ctx, sandboxInfo.ContainerID, repos)
	if err != nil {
		logger.Warn("Failed to detect modified files", "error", err)
		// Continue anyway - we'll just have an empty list
	}

	logger.Info("Transformation completed",
		"exitCode", result.ExitCode,
		"filesModified", len(filesModified))

	return &model.DeterministicResult{
		Success:       true,
		ExitCode:      result.ExitCode,
		Output:        output,
		FilesModified: filesModified,
	}, nil
}

// buildDockerRunCommand constructs the docker run command string.
// The command runs the transformation image with the workspace mounted at the same path.
// Security hardening is applied to minimize container escape risk.
func buildDockerRunCommand(image string, args []string, env map[string]string) string {
	var parts []string
	parts = append(parts, "docker run --rm")

	// Security hardening: network isolation (prevents data exfiltration)
	parts = append(parts, "--network none")

	// Security hardening: drop all capabilities (minimize privileges)
	parts = append(parts, "--cap-drop=ALL")

	// Security hardening: read-only root filesystem
	parts = append(parts, "--read-only")

	// Security hardening: prevent privilege escalation
	parts = append(parts, "--security-opt=no-new-privileges:true")

	// Writable /tmp for tools that need temporary storage (noexec prevents code execution)
	parts = append(parts, "--tmpfs /tmp:rw,noexec,nosuid,size=512m")

	// Mount workspace (this is the only writable persistent storage)
	parts = append(parts, "-v /workspace:/workspace")

	// Set working directory
	parts = append(parts, "-w /workspace")

	// Add environment variables (sorted for deterministic output)
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := env[key]
		parts = append(parts, fmt.Sprintf("-e %s=%s", key, shellQuote(value)))
	}

	// Add image name (always quoted for safety)
	parts = append(parts, shellQuote(image))

	// Add arguments (always quoted for safety)
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}

	return strings.Join(parts, " ")
}

// detectModifiedFiles finds all modified files across repositories using git status.
func (a *DeterministicActivities) detectModifiedFiles(
	ctx context.Context,
	containerID string,
	repos []model.Repository,
) ([]string, error) {
	logger := activity.GetLogger(ctx)
	var allModified []string

	for _, repo := range repos {
		repoPath := fmt.Sprintf("/workspace/%s", repo.Name)

		// Use git status --porcelain to get modified files
		// Format: XY filename
		// where X is index status, Y is worktree status
		cmd := fmt.Sprintf("cd %s && git status --porcelain", repoPath)
		result, err := a.Provider.ExecShell(ctx, containerID, cmd, AgentUser)
		if err != nil {
			logger.Warn("Failed to get git status for repository",
				"repo", repo.Name,
				"error", err)
			continue
		}

		if result.ExitCode != 0 {
			logger.Warn("Git status failed for repository",
				"repo", repo.Name,
				"exitCode", result.ExitCode,
				"stderr", result.Stderr)
			continue
		}

		if result.Stdout == "" {
			continue
		}

		// Parse git status output
		lines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
		for _, line := range lines {
			if len(line) < 3 {
				continue
			}
			// Extract filename (skip the two-character status and space)
			filename := strings.TrimSpace(line[3:])
			if filename != "" {
				// Prefix with repo name for multi-repo support
				allModified = append(allModified, fmt.Sprintf("%s/%s", repo.Name, filename))
			}
		}
	}

	return allModified, nil
}
