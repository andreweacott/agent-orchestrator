// Package activity contains Temporal activity implementations.
package activity

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"go.temporal.io/sdk/activity"

	"github.com/anthropics/claude-code-orchestrator/internal/docker"
	"github.com/anthropics/claude-code-orchestrator/internal/model"
)

// gitRefPattern validates git ref names (branches, tags, repo names)
// SEC-004: Prevents command injection via malicious ref names
var gitRefPattern = regexp.MustCompile(`^[a-zA-Z0-9._/-]+$`)

// validateGitRef validates that a string is safe for use as a git ref
func validateGitRef(ref, fieldName string) error {
	if ref == "" {
		return fmt.Errorf("%s cannot be empty", fieldName)
	}
	if !gitRefPattern.MatchString(ref) {
		return fmt.Errorf("invalid %s: %q contains invalid characters", fieldName, ref)
	}
	return nil
}

// validateGitURL validates that a URL is a safe git URL (SEC-005)
func validateGitURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("URL cannot be empty")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "git" && u.Scheme != "ssh" {
		return fmt.Errorf("unsupported URL scheme: %s", u.Scheme)
	}
	return nil
}

// shortContainerID safely truncates container ID for logging
func shortContainerID(id string) string {
	if id == "" {
		return "<empty>"
	}
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

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

// parseMemory parses memory strings like "4g", "4G", "4096m", "4Gi" (BUG-002 fix)
func parseMemory(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 4 * 1024 * 1024 * 1024, nil // Default 4GB
	}

	var multiplier int64 = 1
	var numStr string

	switch {
	case strings.HasSuffix(s, "gi"):
		multiplier = 1024 * 1024 * 1024
		numStr = strings.TrimSuffix(s, "gi")
	case strings.HasSuffix(s, "g"):
		multiplier = 1024 * 1024 * 1024
		numStr = strings.TrimSuffix(s, "g")
	case strings.HasSuffix(s, "mi"):
		multiplier = 1024 * 1024
		numStr = strings.TrimSuffix(s, "mi")
	case strings.HasSuffix(s, "m"):
		multiplier = 1024 * 1024
		numStr = strings.TrimSuffix(s, "m")
	case strings.HasSuffix(s, "ki"):
		multiplier = 1024
		numStr = strings.TrimSuffix(s, "ki")
	case strings.HasSuffix(s, "k"):
		multiplier = 1024
		numStr = strings.TrimSuffix(s, "k")
	default:
		numStr = s
	}

	val, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 4 * 1024 * 1024 * 1024, fmt.Errorf("invalid memory value: %s", s)
	}

	return int64(val * float64(multiplier)), nil
}

// parseCPU parses CPU strings like "2", "2.5", "500m" (millicores) (BUG-002 fix)
func parseCPU(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 200000, nil // Default 2 CPUs
	}

	if strings.HasSuffix(s, "m") {
		// Millicores: 1000m = 1 CPU
		millis, err := strconv.ParseInt(strings.TrimSuffix(s, "m"), 10, 64)
		if err != nil {
			return 200000, fmt.Errorf("invalid cpu value: %s", s)
		}
		return millis * 100, nil // Convert to quota (100000 = 1 CPU)
	}

	cpus, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 200000, fmt.Errorf("invalid cpu value: %s", s)
	}
	return int64(cpus * 100000), nil
}

// ProvisionSandbox creates a Docker container for Claude Code execution.
func (a *SandboxActivities) ProvisionSandbox(ctx context.Context, taskID string) (*model.SandboxInfo, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Provisioning sandbox", "taskID", taskID)

	sandboxImage := getEnvOrDefault("SANDBOX_IMAGE", "claude-code-sandbox:latest")
	memoryLimit := getEnvOrDefault("SANDBOX_MEMORY_LIMIT", DefaultMemoryLimit)
	cpuLimit := getEnvOrDefault("SANDBOX_CPU_LIMIT", DefaultCPULimit)
	// SEC-003: Configurable network mode for sandbox isolation
	networkMode := getEnvOrDefault("SANDBOX_NETWORK_MODE", DefaultNetworkMode)

	// BUG-002: Use robust resource parsing
	memLimitBytes, err := parseMemory(memoryLimit)
	if err != nil {
		logger.Warn("Failed to parse memory limit, using default", "value", memoryLimit, "error", err)
	}

	cpuQuota, err := parseCPU(cpuLimit)
	if err != nil {
		logger.Warn("Failed to parse CPU limit, using default", "value", cpuLimit, "error", err)
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
		// SEC-003: Apply configurable network mode
		// Options: "bridge" (default), "none" (no network), "host", or custom network name
		NetworkMode: container.NetworkMode(networkMode),
	}

	containerName := fmt.Sprintf("claude-sandbox-%s", taskID)

	containerID, err := a.DockerClient.CreateAndStartContainer(ctx, containerConfig, hostConfig, containerName)
	if err != nil {
		return nil, fmt.Errorf("failed to provision sandbox: %w", err)
	}

	logger.Info("Container created", "containerID", shortContainerID(containerID), "taskID", taskID, "networkMode", networkMode)

	return &model.SandboxInfo{
		ContainerID:   containerID,
		WorkspacePath: WorkspacePath,
	}, nil
}

// configureGitCredentials sets up git credential helper to avoid token in command line (SEC-002 fix)
func (a *SandboxActivities) configureGitCredentials(ctx context.Context, containerID, token string) error {
	if token == "" {
		return nil
	}

	// Configure git to use credential helper with stored credentials
	// This avoids exposing the token in shell commands, process lists, and logs
	commands := []string{
		"git config --global credential.helper store",
		fmt.Sprintf("echo 'https://x-access-token:%s@github.com' > ~/.git-credentials", token),
		"chmod 600 ~/.git-credentials",
	}

	for _, cmd := range commands {
		result, err := a.DockerClient.ExecShellCommand(ctx, containerID, cmd, AgentUser)
		if err != nil {
			return fmt.Errorf("failed to configure git credentials: %w", err)
		}
		if result.ExitCode != 0 {
			return fmt.Errorf("failed to configure git credentials: %s", result.Stderr)
		}
	}
	return nil
}

// CloneRepositories clones repositories into the sandbox and runs setup commands.
func (a *SandboxActivities) CloneRepositories(ctx context.Context, sandbox model.SandboxInfo, repos []model.Repository, agentsMD string) ([]string, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Cloning repositories", "count", len(repos))

	// SEC-002: Configure git credentials once at start (token not exposed in clone commands)
	githubToken := os.Getenv("GITHUB_TOKEN")
	if err := a.configureGitCredentials(ctx, sandbox.ContainerID, githubToken); err != nil {
		return nil, fmt.Errorf("failed to configure git credentials: %w", err)
	}

	// BUG-004: Configurable clone depth (default 50 for reasonable history)
	cloneDepth := getEnvOrDefault("SANDBOX_GIT_CLONE_DEPTH", DefaultCloneDepth)

	var clonedPaths []string

	for _, repo := range repos {
		// SEC-004: Validate git refs to prevent command injection
		if err := validateGitRef(repo.Branch, "branch"); err != nil {
			return nil, fmt.Errorf("invalid branch for %s: %w", repo.Name, err)
		}
		if err := validateGitRef(repo.Name, "repo name"); err != nil {
			return nil, fmt.Errorf("invalid repo name: %w", err)
		}
		// SEC-005: Validate URL to ensure safe git operations
		if err := validateGitURL(repo.URL); err != nil {
			return nil, fmt.Errorf("invalid URL for %s: %w", repo.Name, err)
		}

		logger.Info("Cloning repository", "url", repo.URL, "name", repo.Name, "branch", repo.Branch)

		// SEC-002: Clone without token in URL - git will use stored credentials
		var cmd string
		if cloneDepth == "0" || cloneDepth == "" {
			// Full clone
			cmd = fmt.Sprintf("git clone --branch %s %s /workspace/%s", repo.Branch, repo.URL, repo.Name)
		} else {
			// Shallow clone with configurable depth
			cmd = fmt.Sprintf("git clone --depth %s --branch %s %s /workspace/%s", cloneDepth, repo.Branch, repo.URL, repo.Name)
		}

		result, err := a.DockerClient.ExecShellCommand(ctx, sandbox.ContainerID, cmd, AgentUser)
		if err != nil {
			return nil, fmt.Errorf("failed to execute clone command: %w", err)
		}
		if result.ExitCode != 0 {
			return nil, fmt.Errorf("failed to clone %s: %s", repo.URL, result.Stderr)
		}

		repoPath := fmt.Sprintf("/workspace/%s", repo.Name)
		clonedPaths = append(clonedPaths, repoPath)

		// Record heartbeat
		activity.RecordHeartbeat(ctx, fmt.Sprintf("Cloned %s", repo.Name))

		// Run setup commands if defined
		if len(repo.Setup) > 0 {
			logger.Info("Running setup commands", "repo", repo.Name, "count", len(repo.Setup))
			for i, setupCmd := range repo.Setup {
				logger.Info("Running setup command", "repo", repo.Name, "command", setupCmd)
				fullCmd := fmt.Sprintf("cd %s && %s", repoPath, setupCmd)
				result, err := a.DockerClient.ExecShellCommand(ctx, sandbox.ContainerID, fullCmd, AgentUser)
				if err != nil {
					return nil, fmt.Errorf("failed to execute setup command %d for %s: %w", i+1, repo.Name, err)
				}
				if result.ExitCode != 0 {
					return nil, fmt.Errorf("setup command %d failed for %s: %s", i+1, repo.Name, result.Stderr)
				}
				activity.RecordHeartbeat(ctx, fmt.Sprintf("Setup %d/%d for %s", i+1, len(repo.Setup), repo.Name))
			}
			logger.Info("Setup completed", "repo", repo.Name)
		}
	}

	// Create AGENTS.md in workspace root
	logger.Info("Creating AGENTS.md")

	// Use heredoc to write file content
	writeCmd := fmt.Sprintf("cat > /workspace/AGENTS.md << 'AGENTS_EOF'\n%s\nAGENTS_EOF", agentsMD)
	result, err := a.DockerClient.ExecShellCommand(ctx, sandbox.ContainerID, writeCmd, AgentUser)
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
	logger.Info("Cleaning up container", "containerID", shortContainerID(containerID))

	if err := a.DockerClient.StopAndRemoveContainer(ctx, containerID, 10); err != nil {
		logger.Error("Error cleaning up container", "error", err)
		return err
	}

	logger.Info("Container removed", "containerID", shortContainerID(containerID))
	return nil
}

// RunVerifiers executes verification commands in each repository and returns results.
func (a *SandboxActivities) RunVerifiers(ctx context.Context, sandbox model.SandboxInfo, repos []model.Repository, verifiers []model.Verifier) (*model.VerifiersResult, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Running verifiers", "count", len(verifiers), "repos", len(repos))

	if len(verifiers) == 0 {
		return &model.VerifiersResult{AllPassed: true, Results: []model.VerifierResult{}}, nil
	}

	var results []model.VerifierResult
	allPassed := true

	for _, repo := range repos {
		repoPath := fmt.Sprintf("/workspace/%s", repo.Name)

		for _, verifier := range verifiers {
			logger.Info("Running verifier", "name", verifier.Name, "repo", repo.Name)

			// Build command string from verifier.Command slice
			cmdStr := strings.Join(verifier.Command, " ")
			fullCmd := fmt.Sprintf("cd %s && %s", repoPath, cmdStr)

			result, err := a.DockerClient.ExecShellCommand(ctx, sandbox.ContainerID, fullCmd, AgentUser)

			var vResult model.VerifierResult
			vResult.Name = fmt.Sprintf("%s:%s", repo.Name, verifier.Name)

			if err != nil {
				vResult.Success = false
				vResult.ExitCode = -1
				vResult.Error = err.Error()
				allPassed = false
			} else {
				vResult.ExitCode = result.ExitCode
				vResult.Success = result.ExitCode == 0
				vResult.Output = result.Stdout
				if result.Stderr != "" {
					vResult.Output += "\n" + result.Stderr
				}
				if !vResult.Success {
					vResult.Error = fmt.Sprintf("exit code %d", result.ExitCode)
					allPassed = false
				}
			}

			results = append(results, vResult)
			logger.Info("Verifier completed", "name", vResult.Name, "success", vResult.Success)

			activity.RecordHeartbeat(ctx, fmt.Sprintf("Verifier %s: %v", vResult.Name, vResult.Success))
		}
	}

	return &model.VerifiersResult{
		AllPassed: allPassed,
		Results:   results,
	}, nil
}
