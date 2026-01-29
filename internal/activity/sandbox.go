// Package activity contains Temporal activity implementations.
package activity

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/docker/docker/api/types/container"
	"go.temporal.io/sdk/activity"

	"github.com/anthropics/claude-code-orchestrator/internal/docker"
	"github.com/anthropics/claude-code-orchestrator/internal/model"
)

// SandboxActivities contains activities for managing sandbox containers.
type SandboxActivities struct {
	DockerClient *docker.Client
}

// NewSandboxActivities creates a new SandboxActivities instance.
func NewSandboxActivities(client *docker.Client) *SandboxActivities {
	return &SandboxActivities{DockerClient: client}
}

// getEnvOrDefault returns the environment variable value or a default.
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// ProvisionSandbox creates a Docker container for Claude Code execution.
func (a *SandboxActivities) ProvisionSandbox(ctx context.Context, taskID string) (*model.SandboxInfo, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Provisioning sandbox", "taskID", taskID)

	sandboxImage := getEnvOrDefault("SANDBOX_IMAGE", "claude-code-sandbox:latest")
	memoryLimit := getEnvOrDefault("SANDBOX_MEMORY_LIMIT", "4g")
	cpuLimit := getEnvOrDefault("SANDBOX_CPU_LIMIT", "2")

	// Parse memory limit
	var memLimitBytes int64 = 4 * 1024 * 1024 * 1024 // Default 4GB
	if memoryLimit != "" {
		// Simple parsing: assume format like "4g" or "4096m"
		if strings.HasSuffix(memoryLimit, "g") {
			var gb int64
			fmt.Sscanf(memoryLimit, "%dg", &gb)
			memLimitBytes = gb * 1024 * 1024 * 1024
		} else if strings.HasSuffix(memoryLimit, "m") {
			var mb int64
			fmt.Sscanf(memoryLimit, "%dm", &mb)
			memLimitBytes = mb * 1024 * 1024
		}
	}

	// Parse CPU limit
	var cpuQuota int64 = 200000 // Default 2 CPUs
	if cpuLimit != "" {
		var cpus int64
		fmt.Sscanf(cpuLimit, "%d", &cpus)
		cpuQuota = cpus * 100000
	}

	containerConfig := &container.Config{
		Image:     sandboxImage,
		Tty:       true,
		OpenStdin: true,
		Cmd:       []string{"tail", "-f", "/dev/null"},
		Env: []string{
			fmt.Sprintf("ANTHROPIC_API_KEY=%s", os.Getenv("ANTHROPIC_API_KEY")),
			fmt.Sprintf("GITHUB_TOKEN=%s", os.Getenv("GITHUB_TOKEN")),
			fmt.Sprintf("TASK_ID=%s", taskID),
		},
	}

	hostConfig := &container.HostConfig{
		Resources: container.Resources{
			Memory:    memLimitBytes,
			CPUPeriod: 100000,
			CPUQuota:  cpuQuota,
		},
		SecurityOpt: []string{"no-new-privileges:true"},
	}

	containerName := fmt.Sprintf("claude-sandbox-%s", taskID)

	containerID, err := a.DockerClient.CreateAndStartContainer(ctx, containerConfig, hostConfig, containerName)
	if err != nil {
		return nil, fmt.Errorf("failed to provision sandbox: %w", err)
	}

	logger.Info("Container created", "containerID", containerID[:12], "taskID", taskID)

	return &model.SandboxInfo{
		ContainerID:   containerID,
		WorkspacePath: "/workspace",
	}, nil
}

// CloneRepositories clones repositories into the sandbox.
func (a *SandboxActivities) CloneRepositories(ctx context.Context, sandbox model.SandboxInfo, repos []model.Repository, agentsMD string) ([]string, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Cloning repositories", "count", len(repos))

	var clonedPaths []string
	githubToken := os.Getenv("GITHUB_TOKEN")

	for _, repo := range repos {
		logger.Info("Cloning repository", "url", repo.URL, "name", repo.Name, "branch", repo.Branch)

		// Build clone URL with token for private repos
		cloneURL := repo.URL
		if githubToken != "" && strings.Contains(repo.URL, "github.com") {
			cloneURL = strings.Replace(repo.URL, "https://github.com", fmt.Sprintf("https://%s@github.com", githubToken), 1)
		}

		// Clone the repository
		cmd := fmt.Sprintf("git clone --depth 1 --branch %s %s /workspace/%s", repo.Branch, cloneURL, repo.Name)
		result, err := a.DockerClient.ExecShellCommand(ctx, sandbox.ContainerID, cmd, "agent")
		if err != nil {
			return nil, fmt.Errorf("failed to execute clone command: %w", err)
		}
		if result.ExitCode != 0 {
			return nil, fmt.Errorf("failed to clone %s: %s", repo.URL, result.Stderr)
		}

		clonedPaths = append(clonedPaths, fmt.Sprintf("/workspace/%s", repo.Name))

		// Record heartbeat
		activity.RecordHeartbeat(ctx, fmt.Sprintf("Cloned %s", repo.Name))
	}

	// Create AGENTS.md in workspace root
	logger.Info("Creating AGENTS.md")

	// Use heredoc to write file content
	writeCmd := fmt.Sprintf("cat > /workspace/AGENTS.md << 'AGENTS_EOF'\n%s\nAGENTS_EOF", agentsMD)
	result, err := a.DockerClient.ExecShellCommand(ctx, sandbox.ContainerID, writeCmd, "agent")
	if err != nil {
		logger.Warn("Failed to create AGENTS.md", "error", err)
	} else if result.ExitCode != 0 {
		logger.Warn("Failed to create AGENTS.md", "stderr", result.Stderr)
	}

	return clonedPaths, nil
}

// CleanupSandbox stops and removes the sandbox container.
func (a *SandboxActivities) CleanupSandbox(ctx context.Context, containerID string) error {
	logger := activity.GetLogger(ctx)
	logger.Info("Cleaning up container", "containerID", containerID[:12])

	if err := a.DockerClient.StopAndRemoveContainer(ctx, containerID, 10); err != nil {
		logger.Error("Error cleaning up container", "error", err)
		return err
	}

	logger.Info("Container removed", "containerID", containerID[:12])
	return nil
}
