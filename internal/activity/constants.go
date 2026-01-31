// Package activity contains Temporal activity implementations.
package activity

import (
	"fmt"
	"log"
	"os"
	"strings"
)

// Activity name constants to prevent typos and improve maintainability (SIMP-003)
const (
	// Sandbox activities
	ActivityProvisionSandbox  = "ProvisionSandbox"
	ActivityCloneRepositories = "CloneRepositories"
	ActivityRunVerifiers      = "RunVerifiers"
	ActivityCleanupSandbox    = "CleanupSandbox"

	// Claude Code activities
	ActivityRunClaudeCode   = "RunClaudeCode"
	ActivityGetClaudeOutput = "GetClaudeOutput"

	// Deterministic transformation activities
	ActivityExecuteDeterministic = "ExecuteDeterministic"

	// GitHub activities
	ActivityCreatePullRequest = "CreatePullRequest"

	// Slack activities
	ActivityNotifySlack = "NotifySlack"
)

// Default configuration values (SIMP-004)
const (
	DefaultTimeoutMinutes  = 30
	DefaultApprovalTimeout = "24h"
	DefaultCloneDepth      = "50"
	DefaultBranch          = "main"
	DefaultMemoryLimit     = "4g"
	DefaultCPULimit        = "2"
	DefaultNetworkMode     = "bridge"

	// Git configuration defaults - intentionally use noreply.localhost to make it
	// obvious when defaults are being used. Production deployments MUST set
	// GIT_USER_EMAIL and GIT_USER_NAME environment variables.
	DefaultGitEmail = "claude-agent@noreply.localhost"
	DefaultGitName  = "Claude Code Agent"

	BranchPrefix  = "fix/claude-"
	WorkspacePath = "/workspace"
	AgentUser     = "agent"
)

// ConfigValidationMode controls how configuration validation behaves.
type ConfigValidationMode int

const (
	// ConfigModeWarn logs warnings for missing configuration but allows startup.
	ConfigModeWarn ConfigValidationMode = iota
	// ConfigModeRequire returns an error if required configuration is missing.
	ConfigModeRequire
)

// ConfigIssue represents a configuration problem found during validation.
type ConfigIssue struct {
	Name        string // Environment variable or config name
	Description string // What the issue is
	Required    bool   // Whether this is required for production
}

// ValidateConfig checks that required configuration is present.
// Returns a list of configuration issues found.
func ValidateConfig() []ConfigIssue {
	var issues []ConfigIssue

	// Git identity configuration
	if os.Getenv("GIT_USER_EMAIL") == "" {
		issues = append(issues, ConfigIssue{
			Name:        "GIT_USER_EMAIL",
			Description: "Git commits will use default email 'claude-agent@noreply.localhost'",
			Required:    true,
		})
	}
	if os.Getenv("GIT_USER_NAME") == "" {
		issues = append(issues, ConfigIssue{
			Name:        "GIT_USER_NAME",
			Description: "Git commits will use default name 'Claude Code Agent'",
			Required:    false, // Name is less critical than email
		})
	}

	// GitHub token
	if os.Getenv("GITHUB_TOKEN") == "" {
		issues = append(issues, ConfigIssue{
			Name:        "GITHUB_TOKEN",
			Description: "Required for creating pull requests",
			Required:    true,
		})
	}

	// Anthropic API key
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		issues = append(issues, ConfigIssue{
			Name:        "ANTHROPIC_API_KEY",
			Description: "Required for Claude Code execution",
			Required:    true,
		})
	}

	return issues
}

// CheckConfig validates configuration and handles issues according to the mode.
// In ConfigModeWarn, it logs warnings and returns nil.
// In ConfigModeRequire, it returns an error if any required config is missing.
func CheckConfig(mode ConfigValidationMode) error {
	issues := ValidateConfig()
	if len(issues) == 0 {
		return nil
	}

	var requiredMissing []string
	for _, issue := range issues {
		if issue.Required {
			requiredMissing = append(requiredMissing, issue.Name)
		}
		log.Printf("CONFIG WARNING: %s not set - %s", issue.Name, issue.Description)
	}

	if mode == ConfigModeRequire && len(requiredMissing) > 0 {
		return fmt.Errorf("required configuration missing: %s (set REQUIRE_CONFIG=false to run with warnings only)",
			strings.Join(requiredMissing, ", "))
	}

	return nil
}
