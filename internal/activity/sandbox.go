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

	"go.temporal.io/sdk/activity"

	"github.com/andreweacott/agent-orchestrator/internal/model"
	"github.com/andreweacott/agent-orchestrator/internal/sandbox"
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
	Provider sandbox.Provider
}

// NewSandboxActivities creates a new SandboxActivities instance.
func NewSandboxActivities(provider sandbox.Provider) *SandboxActivities {
	return &SandboxActivities{Provider: provider}
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

	// BUG-002: Use robust resource parsing
	memLimitBytes, err := parseMemory(memoryLimit)
	if err != nil {
		logger.Warn("Failed to parse memory limit, using default", "value", memoryLimit, "error", err)
	}

	cpuQuota, err := parseCPU(cpuLimit)
	if err != nil {
		logger.Warn("Failed to parse CPU limit, using default", "value", cpuLimit, "error", err)
	}

	opts := sandbox.ProvisionOptions{
		TaskID:     taskID,
		Image:      sandboxImage,
		WorkingDir: WorkspacePath,
		Env: map[string]string{
			"ANTHROPIC_API_KEY": os.Getenv("ANTHROPIC_API_KEY"),
			"GITHUB_TOKEN":      os.Getenv("GITHUB_TOKEN"),
			"TASK_ID":           taskID,
		},
		Resources: sandbox.ResourceLimits{
			MemoryBytes: memLimitBytes,
			CPUQuota:    cpuQuota,
		},
	}

	sb, err := a.Provider.Provision(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to provision sandbox: %w", err)
	}

	logger.Info("Container created", "containerID", shortContainerID(sb.ID), "taskID", taskID)

	return &model.SandboxInfo{
		ContainerID:   sb.ID,
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
	// Use umask 077 to ensure the credentials file is never world-readable (SEC-002 enhancement)
	cmd := fmt.Sprintf(`git config --global credential.helper store && (
umask 077 && cat > ~/.git-credentials << 'CRED_EOF'
https://x-access-token:%s@github.com
CRED_EOF
)`, token)

	result, err := a.Provider.ExecShell(ctx, containerID, cmd, AgentUser)
	if err != nil {
		return fmt.Errorf("failed to configure git credentials: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("failed to configure git credentials: %s", result.Stderr)
	}
	return nil
}

// CloneRepositoriesInput defines the input for CloneRepositories activity.
type CloneRepositoriesInput struct {
	SandboxInfo model.SandboxInfo

	// Transformation repo - when set, cloned to /workspace/ root
	// Targets are then cloned to /workspace/targets/
	Transformation *model.Repository

	// Target repos - cloned to /workspace/targets/{name} when Transformation is set
	Targets []model.Repository

	// Legacy repos - cloned to /workspace/{name} when Transformation is not set
	Repositories []model.Repository

	// AGENTS.md content to write to workspace root
	AgentsMD string
}

// CloneRepositories clones repositories into the sandbox and runs setup commands.
// Supports two modes:
// 1. Transformation mode: Clone transformation repo to /workspace, targets to /workspace/targets/
// 2. Legacy mode: Clone repos directly to /workspace/{name}
func (a *SandboxActivities) CloneRepositories(ctx context.Context, input CloneRepositoriesInput) ([]string, error) {
	logger := activity.GetLogger(ctx)

	// SEC-002: Configure git credentials once at start (token not exposed in clone commands)
	githubToken := os.Getenv("GITHUB_TOKEN")
	if err := a.configureGitCredentials(ctx, input.SandboxInfo.ContainerID, githubToken); err != nil {
		return nil, fmt.Errorf("failed to configure git credentials: %w", err)
	}

	// BUG-004: Configurable clone depth (default 50 for reasonable history)
	cloneDepth := getEnvOrDefault("SANDBOX_GIT_CLONE_DEPTH", DefaultCloneDepth)

	var clonedPaths []string

	if input.Transformation != nil {
		// Transformation mode: clone transformation repo to /workspace, targets to /workspace/targets/
		paths, err := a.cloneTransformationMode(ctx, input, cloneDepth)
		if err != nil {
			return nil, err
		}
		clonedPaths = paths
	} else {
		// Legacy mode: clone repos to /workspace/{name}
		logger.Info("Cloning repositories (legacy mode)", "count", len(input.Repositories))
		paths, err := a.cloneRepos(ctx, input.SandboxInfo.ContainerID, input.Repositories, "/workspace", cloneDepth)
		if err != nil {
			return nil, err
		}
		clonedPaths = paths
	}

	// Create AGENTS.md in workspace root
	logger.Info("Creating AGENTS.md")

	// Use heredoc to write file content
	writeCmd := fmt.Sprintf("cat > /workspace/AGENTS.md << 'AGENTS_EOF'\n%s\nAGENTS_EOF", input.AgentsMD)
	result, err := a.Provider.ExecShell(ctx, input.SandboxInfo.ContainerID, writeCmd, AgentUser)
	if err != nil {
		logger.Warn("Failed to create AGENTS.md", "error", err)
	} else if result.ExitCode != 0 {
		logger.Warn("Failed to create AGENTS.md", "stderr", result.Stderr)
	}

	return clonedPaths, nil
}

// cloneTransformationMode handles the transformation repo layout:
// - Clone transformation repo contents to /workspace (the recipe)
// - Clone target repos to /workspace/targets/{name}
func (a *SandboxActivities) cloneTransformationMode(ctx context.Context, input CloneRepositoriesInput, cloneDepth string) ([]string, error) {
	logger := activity.GetLogger(ctx)
	transformation := input.Transformation

	logger.Info("Cloning transformation repository", "url", transformation.URL, "branch", transformation.Branch)

	// Validate transformation repo
	if err := validateGitRef(transformation.Branch, "branch"); err != nil {
		return nil, fmt.Errorf("invalid transformation branch: %w", err)
	}
	if err := validateGitURL(transformation.URL); err != nil {
		return nil, fmt.Errorf("invalid transformation URL: %w", err)
	}

	// Clone transformation repo to a temp location first, then move contents to /workspace
	// This ensures .claude/, CLAUDE.md, bin/, etc. are at the workspace root
	tmpDir := "/tmp/transformation-clone"

	// Build clone command
	var cloneCmd string
	if cloneDepth == "0" || cloneDepth == "" {
		cloneCmd = fmt.Sprintf("git clone --branch %s %s %s", transformation.Branch, transformation.URL, tmpDir)
	} else {
		cloneCmd = fmt.Sprintf("git clone --depth %s --branch %s %s %s", cloneDepth, transformation.Branch, transformation.URL, tmpDir)
	}

	result, err := a.Provider.ExecShell(ctx, input.SandboxInfo.ContainerID, cloneCmd, AgentUser)
	if err != nil {
		return nil, fmt.Errorf("failed to clone transformation repo: %w", err)
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("failed to clone transformation repo: %s", result.Stderr)
	}

	// Move transformation repo contents (including hidden files) to /workspace
	// Using cp -a to preserve all attributes and handle hidden files, then remove temp
	moveCmd := fmt.Sprintf("cp -a %s/. /workspace/ && rm -rf %s", tmpDir, tmpDir)
	result, err = a.Provider.ExecShell(ctx, input.SandboxInfo.ContainerID, moveCmd, AgentUser)
	if err != nil {
		return nil, fmt.Errorf("failed to move transformation contents: %w", err)
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("failed to move transformation contents: %s", result.Stderr)
	}

	activity.RecordHeartbeat(ctx, "Cloned transformation repo")

	// Run transformation repo setup commands (from /workspace)
	if len(transformation.Setup) > 0 {
		logger.Info("Running transformation setup commands", "count", len(transformation.Setup))
		for i, setupCmd := range transformation.Setup {
			logger.Info("Running transformation setup command", "command", setupCmd)
			fullCmd := fmt.Sprintf("cd /workspace && %s", setupCmd)
			result, err := a.Provider.ExecShell(ctx, input.SandboxInfo.ContainerID, fullCmd, AgentUser)
			if err != nil {
				return nil, fmt.Errorf("failed to execute transformation setup command %d: %w", i+1, err)
			}
			if result.ExitCode != 0 {
				return nil, fmt.Errorf("transformation setup command %d failed: %s", i+1, result.Stderr)
			}
			activity.RecordHeartbeat(ctx, fmt.Sprintf("Transformation setup %d/%d", i+1, len(transformation.Setup)))
		}
	}

	var clonedPaths []string

	// Clone target repos to /workspace/targets/ if any
	if len(input.Targets) > 0 {
		// Create targets directory
		mkdirCmd := "mkdir -p /workspace/targets"
		result, err = a.Provider.ExecShell(ctx, input.SandboxInfo.ContainerID, mkdirCmd, AgentUser)
		if err != nil {
			return nil, fmt.Errorf("failed to create targets directory: %w", err)
		}
		if result.ExitCode != 0 {
			return nil, fmt.Errorf("failed to create targets directory: %s", result.Stderr)
		}

		logger.Info("Cloning target repositories", "count", len(input.Targets))
		targetPaths, err := a.cloneRepos(ctx, input.SandboxInfo.ContainerID, input.Targets, "/workspace/targets", cloneDepth)
		if err != nil {
			return nil, fmt.Errorf("failed to clone target repos: %w", err)
		}
		clonedPaths = append(clonedPaths, targetPaths...)
	}

	return clonedPaths, nil
}

// cloneRepos clones multiple repositories to the specified base path.
func (a *SandboxActivities) cloneRepos(ctx context.Context, containerID string, repos []model.Repository, basePath string, cloneDepth string) ([]string, error) {
	logger := activity.GetLogger(ctx)
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

		repoPath := fmt.Sprintf("%s/%s", basePath, repo.Name)
		logger.Info("Cloning repository", "url", repo.URL, "name", repo.Name, "branch", repo.Branch, "path", repoPath)

		// SEC-002: Clone without token in URL - git will use stored credentials
		var cmd string
		if cloneDepth == "0" || cloneDepth == "" {
			// Full clone
			cmd = fmt.Sprintf("git clone --branch %s %s %s", repo.Branch, repo.URL, repoPath)
		} else {
			// Shallow clone with configurable depth
			cmd = fmt.Sprintf("git clone --depth %s --branch %s %s %s", cloneDepth, repo.Branch, repo.URL, repoPath)
		}

		result, err := a.Provider.ExecShell(ctx, containerID, cmd, AgentUser)
		if err != nil {
			return nil, fmt.Errorf("failed to execute clone command: %w", err)
		}
		if result.ExitCode != 0 {
			return nil, fmt.Errorf("failed to clone %s: %s", repo.URL, result.Stderr)
		}

		clonedPaths = append(clonedPaths, repoPath)

		// Record heartbeat
		activity.RecordHeartbeat(ctx, fmt.Sprintf("Cloned %s", repo.Name))

		// Run setup commands if defined
		if len(repo.Setup) > 0 {
			logger.Info("Running setup commands", "repo", repo.Name, "count", len(repo.Setup))
			for i, setupCmd := range repo.Setup {
				logger.Info("Running setup command", "repo", repo.Name, "command", setupCmd)
				fullCmd := fmt.Sprintf("cd %s && %s", repoPath, setupCmd)
				result, err := a.Provider.ExecShell(ctx, containerID, fullCmd, AgentUser)
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

	return clonedPaths, nil
}

// CleanupSandbox stops and removes the sandbox container.
func (a *SandboxActivities) CleanupSandbox(ctx context.Context, containerID string) error {
	logger := activity.GetLogger(ctx)
	logger.Info("Cleaning up container", "containerID", shortContainerID(containerID))

	if err := a.Provider.Cleanup(ctx, containerID); err != nil {
		logger.Error("Error cleaning up container", "error", err)
		return err
	}

	logger.Info("Container removed", "containerID", shortContainerID(containerID))
	return nil
}

// RunVerifiers executes verification commands in each repository and returns results.
// RunVerifiersInput contains inputs for running verifiers.
type RunVerifiersInput struct {
	SandboxInfo             model.SandboxInfo
	Repos                   []model.Repository
	Verifiers               []model.Verifier
	UseTransformationLayout bool // If true, repos are at /workspace/targets/{name}
}

// RunVerifiers executes verification commands in each repository and returns results.
func (a *SandboxActivities) RunVerifiers(ctx context.Context, input RunVerifiersInput) (*model.VerifiersResult, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Running verifiers", "count", len(input.Verifiers), "repos", len(input.Repos))

	if len(input.Verifiers) == 0 {
		return &model.VerifiersResult{AllPassed: true, Results: []model.VerifierResult{}}, nil
	}

	// Determine base path
	basePath := "/workspace"
	if input.UseTransformationLayout {
		basePath = "/workspace/targets"
	}

	var results []model.VerifierResult
	allPassed := true

	for _, repo := range input.Repos {
		repoPath := fmt.Sprintf("%s/%s", basePath, repo.Name)

		for _, verifier := range input.Verifiers {
			logger.Info("Running verifier", "name", verifier.Name, "repo", repo.Name)

			// Build command string from verifier.Command slice
			cmdStr := strings.Join(verifier.Command, " ")
			fullCmd := fmt.Sprintf("cd %s && %s", repoPath, cmdStr)

			result, err := a.Provider.ExecShell(ctx, input.SandboxInfo.ContainerID, fullCmd, AgentUser)

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
