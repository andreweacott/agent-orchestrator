// Package activity contains Temporal activity implementations.
package activity

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

	BranchPrefix  = "fix/claude-"
	WorkspacePath = "/workspace"
	AgentUser     = "agent"
)
