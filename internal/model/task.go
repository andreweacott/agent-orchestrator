// Package model contains data models for the Claude Code orchestrator.
package model

import (
	"strings"
	"time"
)

// TaskStatus represents the status of a bug fix task.
type TaskStatus string

const (
	TaskStatusPending          TaskStatus = "pending"
	TaskStatusProvisioning     TaskStatus = "provisioning"
	TaskStatusCloning          TaskStatus = "cloning"
	TaskStatusRunning          TaskStatus = "running"
	TaskStatusAwaitingApproval TaskStatus = "awaiting_approval"
	TaskStatusCreatingPRs      TaskStatus = "creating_prs"
	TaskStatusCompleted        TaskStatus = "completed"
	TaskStatusFailed           TaskStatus = "failed"
	TaskStatusCancelled        TaskStatus = "cancelled"
)

// Repository represents a repository to clone into the sandbox.
type Repository struct {
	URL    string   `json:"url"`             // e.g., "https://github.com/org/repo.git"
	Branch string   `json:"branch"`          // Default: "main"
	Name   string   `json:"name"`            // Directory name, derived from URL if not set
	Setup  []string `json:"setup,omitempty"` // Commands to run after clone (e.g., "go mod download")
}

// Verifier represents a validation command to run after transformation.
type Verifier struct {
	Name    string   `json:"name"`    // e.g., "build", "test", "lint"
	Command []string `json:"command"` // e.g., ["go", "build", "./..."]
}

// NewVerifier creates a Verifier from a name and command.
func NewVerifier(name string, command []string) Verifier {
	return Verifier{
		Name:    name,
		Command: command,
	}
}

// NewRepository creates a new Repository with auto-derived name if not provided.
func NewRepository(url, branch, name string) Repository {
	if branch == "" {
		branch = "main"
	}
	if name == "" {
		name = extractRepoName(url)
	}
	return Repository{
		URL:    url,
		Branch: branch,
		Name:   name,
	}
}

// extractRepoName extracts the repository name from a URL.
// e.g., "https://github.com/org/repo.git" -> "repo"
func extractRepoName(url string) string {
	url = strings.TrimSuffix(url, "/")
	parts := strings.Split(url, "/")
	if len(parts) == 0 {
		return ""
	}
	name := parts[len(parts)-1]
	name = strings.TrimSuffix(name, ".git")
	return name
}

// BugFixTask is the input for the BugFixWorkflow.
type BugFixTask struct {
	TaskID       string       `json:"task_id"`
	Title        string       `json:"title"`
	Description  string       `json:"description"`
	Repositories []Repository `json:"repositories"`
	Verifiers    []Verifier   `json:"verifiers,omitempty"` // Validation commands to run after transformation

	// Optional context
	TicketURL    *string `json:"ticket_url,omitempty"`    // e.g., Jira URL
	SlackChannel *string `json:"slack_channel,omitempty"` // For notifications
	Requester    *string `json:"requester,omitempty"`

	// Execution settings
	TimeoutMinutes  int  `json:"timeout_minutes"`
	RequireApproval bool `json:"require_approval"`
	// SIMP-002: Removed unused AutoMerge field
}

// NewBugFixTask creates a BugFixTask with default values.
func NewBugFixTask(taskID, title, description string, repos []Repository) BugFixTask {
	return BugFixTask{
		TaskID:          taskID,
		Title:           title,
		Description:     description,
		Repositories:    repos,
		TimeoutMinutes:  30,
		RequireApproval: true,
	}
}

// SandboxInfo contains information about a provisioned sandbox.
type SandboxInfo struct {
	ContainerID   string    `json:"container_id"`
	WorkspacePath string    `json:"workspace_path"`
	CreatedAt     time.Time `json:"created_at"`
}

// NewSandboxInfo creates a SandboxInfo with default values.
func NewSandboxInfo(containerID string) SandboxInfo {
	return SandboxInfo{
		ContainerID:   containerID,
		WorkspacePath: "/workspace",
		CreatedAt:     time.Now().UTC(),
	}
}

// ClaudeCodeResult is the result from running Claude Code.
type ClaudeCodeResult struct {
	Success               bool     `json:"success"`
	Output                string   `json:"output"`
	FilesModified         []string `json:"files_modified"`
	Error                 *string  `json:"error,omitempty"`
	NeedsClarification    bool     `json:"needs_clarification"`
	ClarificationQuestion *string  `json:"clarification_question,omitempty"`
}

// VerifierResult is the result of running a single verifier.
type VerifierResult struct {
	Name     string `json:"name"`
	Success  bool   `json:"success"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
	Error    string `json:"error,omitempty"`
}

// VerifiersResult is the aggregate result of running all verifiers.
type VerifiersResult struct {
	AllPassed bool             `json:"all_passed"`
	Results   []VerifierResult `json:"results"`
}

// PullRequest represents a created pull request.
type PullRequest struct {
	RepoName   string `json:"repo_name"`
	PRURL      string `json:"pr_url"`
	PRNumber   int    `json:"pr_number"`
	BranchName string `json:"branch_name"`
	Title      string `json:"title"`
}

// BugFixResult is the final result of the BugFixWorkflow.
type BugFixResult struct {
	TaskID          string        `json:"task_id"`
	Status          TaskStatus    `json:"status"`
	PullRequests    []PullRequest `json:"pull_requests"`
	Error           *string       `json:"error,omitempty"`
	DurationSeconds *float64      `json:"duration_seconds,omitempty"`
}

// NewBugFixResult creates a BugFixResult with the given status.
func NewBugFixResult(taskID string, status TaskStatus) BugFixResult {
	return BugFixResult{
		TaskID:       taskID,
		Status:       status,
		PullRequests: []PullRequest{},
	}
}

// WithError returns a copy of the result with an error message.
func (r BugFixResult) WithError(err string) BugFixResult {
	r.Error = &err
	return r
}

// WithDuration returns a copy of the result with a duration.
func (r BugFixResult) WithDuration(seconds float64) BugFixResult {
	r.DurationSeconds = &seconds
	return r
}

// WithPullRequests returns a copy of the result with pull requests.
func (r BugFixResult) WithPullRequests(prs []PullRequest) BugFixResult {
	r.PullRequests = prs
	return r
}

// Helper functions for creating pointer values
func StringPtr(s string) *string {
	return &s
}

func Float64Ptr(f float64) *float64 {
	return &f
}
