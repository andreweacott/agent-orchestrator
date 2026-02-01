// Package model contains data models for the Claude Code orchestrator.
package model

import (
	"encoding/json"
	"strings"
	"time"
)

// SchemaVersion is the current supported schema version.
const SchemaVersion = 1

// DefaultMaxParallel is the default concurrency limit for grouped execution.
const DefaultMaxParallel = 5

// TaskStatus represents the status of a task.
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

// ExecutionType specifies whether execution is agentic or deterministic.
type ExecutionType string

const (
	ExecutionTypeAgentic       ExecutionType = "agentic"       // Claude Code (default)
	ExecutionTypeDeterministic ExecutionType = "deterministic" // Docker image execution
)

// TaskMode specifies what the task produces.
type TaskMode string

const (
	TaskModeTransform TaskMode = "transform" // Creates PRs (default)
	TaskModeReport    TaskMode = "report"    // Collects structured output, no PRs
)

// Execution strategies are now unified into the groups model.
// Previously we had combined/parallel/grouped strategies, but these can all be
// expressed through different group configurations:
// - Combined: One group with all repos
// - Parallel: N groups, each with one repo
// - Grouped: M groups with varying repo counts

// Repository represents a repository to clone into the sandbox.
type Repository struct {
	URL    string   `json:"url" yaml:"url"`                           // e.g., "https://github.com/org/repo.git"
	Branch string   `json:"branch,omitempty" yaml:"branch,omitempty"` // Default: "main"
	Name   string   `json:"name,omitempty" yaml:"name,omitempty"`     // Directory name, derived from URL if not set
	Setup  []string `json:"setup,omitempty" yaml:"setup,omitempty"`   // Commands to run after clone (e.g., "go mod download")
}

// Verifier represents a validation command to run after transformation.
type Verifier struct {
	Name    string   `json:"name" yaml:"name"`       // e.g., "build", "test", "lint"
	Command []string `json:"command" yaml:"command"` // e.g., ["go", "build", "./..."]
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

// AgentLimits defines resource limits for agentic execution.
type AgentLimits struct {
	MaxIterations      int `json:"max_iterations,omitempty" yaml:"max_iterations,omitempty"`
	MaxTokens          int `json:"max_tokens,omitempty" yaml:"max_tokens,omitempty"`
	MaxVerifierRetries int `json:"max_verifier_retries,omitempty" yaml:"max_verifier_retries,omitempty"`
}

// OutputConfig defines how report mode output is captured and validated.
type OutputConfig struct {
	Schema json.RawMessage `json:"schema,omitempty" yaml:"schema,omitempty"` // JSON Schema for frontmatter validation
}

// AgenticExecution contains settings for agentic (Claude Code) execution.
type AgenticExecution struct {
	Prompt    string        `json:"prompt" yaml:"prompt"`
	Verifiers []Verifier    `json:"verifiers,omitempty" yaml:"verifiers,omitempty"`
	Limits    *AgentLimits  `json:"limits,omitempty" yaml:"limits,omitempty"`
	Output    *OutputConfig `json:"output,omitempty" yaml:"output,omitempty"` // For report mode
}

// DeterministicExecution contains settings for deterministic (Docker) execution.
type DeterministicExecution struct {
	Image     string            `json:"image" yaml:"image"`
	Command   []string          `json:"command,omitempty" yaml:"command,omitempty"`
	Args      []string          `json:"args,omitempty" yaml:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	Verifiers []Verifier        `json:"verifiers,omitempty" yaml:"verifiers,omitempty"`
}

// Execution contains the execution configuration - either agentic or deterministic.
type Execution struct {
	Agentic       *AgenticExecution       `json:"agentic,omitempty" yaml:"agentic,omitempty"`
	Deterministic *DeterministicExecution `json:"deterministic,omitempty" yaml:"deterministic,omitempty"`
}

// GetExecutionType returns the execution type based on which field is set.
func (e Execution) GetExecutionType() ExecutionType {
	if e.Deterministic != nil {
		return ExecutionTypeDeterministic
	}
	return ExecutionTypeAgentic
}

// GetVerifiers returns verifiers from the appropriate execution type.
func (e Execution) GetVerifiers() []Verifier {
	if e.Deterministic != nil {
		return e.Deterministic.Verifiers
	}
	if e.Agentic != nil {
		return e.Agentic.Verifiers
	}
	return nil
}

// PullRequestConfig contains configurable PR settings.
type PullRequestConfig struct {
	BranchPrefix string   `json:"branch_prefix,omitempty" yaml:"branch_prefix,omitempty"`
	Title        string   `json:"title,omitempty" yaml:"title,omitempty"`
	Body         string   `json:"body,omitempty" yaml:"body,omitempty"`
	Labels       []string `json:"labels,omitempty" yaml:"labels,omitempty"`
	Reviewers    []string `json:"reviewers,omitempty" yaml:"reviewers,omitempty"`
}

// ForEachTarget represents a target for iteration within a repository (report mode).
type ForEachTarget struct {
	Name    string `json:"name" yaml:"name"`
	Context string `json:"context" yaml:"context"`
}

// ForEachExecution represents the result of a single forEach target execution.
type ForEachExecution struct {
	Target ForEachTarget `json:"target"`
	Report *ReportOutput `json:"report,omitempty"`
	Error  *string       `json:"error,omitempty"`
}

// RepositoryGroup represents a named group of repositories that share a sandbox.
type RepositoryGroup struct {
	Name         string       `json:"name" yaml:"name"`
	Repositories []Repository `json:"repositories" yaml:"repositories"`
}

// SandboxConfig contains Kubernetes sandbox settings for production.
type SandboxConfig struct {
	Namespace    string            `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	RuntimeClass string            `json:"runtime_class,omitempty" yaml:"runtime_class,omitempty"`
	NodeSelector map[string]string `json:"node_selector,omitempty" yaml:"node_selector,omitempty"`
	Resources    *ResourceConfig   `json:"resources,omitempty" yaml:"resources,omitempty"`
}

// ResourceConfig contains resource limits and requests.
type ResourceConfig struct {
	Limits   ResourceSpec `json:"limits,omitempty" yaml:"limits,omitempty"`
	Requests ResourceSpec `json:"requests,omitempty" yaml:"requests,omitempty"`
}

// ResourceSpec contains memory and CPU specifications.
type ResourceSpec struct {
	Memory string `json:"memory,omitempty" yaml:"memory,omitempty"`
	CPU    string `json:"cpu,omitempty" yaml:"cpu,omitempty"`
}

// CredentialsConfig contains references to Kubernetes secrets for credentials.
type CredentialsConfig struct {
	GitHub    *SecretRef `json:"github,omitempty" yaml:"github,omitempty"`
	Anthropic *SecretRef `json:"anthropic,omitempty" yaml:"anthropic,omitempty"`
}

// SecretRef references a Kubernetes secret.
type SecretRef struct {
	SecretRefSpec SecretRefSpec `json:"secret_ref" yaml:"secret_ref"`
}

// SecretRefSpec contains the name and key for a secret reference.
type SecretRefSpec struct {
	Name string `json:"name" yaml:"name"`
	Key  string `json:"key" yaml:"key"`
}

// Task is the input for the Transform workflow.
type Task struct {
	// Schema version (required)
	Version int `json:"version" yaml:"version"`

	// Task identification
	ID          string `json:"id" yaml:"id"`
	Title       string `json:"title" yaml:"title"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Task mode: transform (default) or report
	Mode TaskMode `json:"mode,omitempty" yaml:"mode,omitempty"`

	// Transformation repository - contains skills, tools, CLAUDE.md
	// Claude Code runs from this repo's root (/workspace)
	// When set, targets are cloned into /workspace/targets/
	Transformation *Repository `json:"transformation,omitempty" yaml:"transformation,omitempty"`

	// Target repositories - cloned into /workspace/targets/
	// Used when Transformation is set; mutually exclusive with Repositories for clarity
	Targets []Repository `json:"targets,omitempty" yaml:"targets,omitempty"`

	// Repositories to operate on (existing behavior when Transformation is not set)
	// Cloned directly into /workspace/{name}
	Repositories []Repository `json:"repositories,omitempty" yaml:"repositories,omitempty"`

	// For report mode: iterate over targets within a repo
	ForEach []ForEachTarget `json:"for_each,omitempty" yaml:"for_each,omitempty"`

	// Execution configuration (agentic or deterministic)
	Execution Execution `json:"execution" yaml:"execution"`

	// Optional context
	TicketURL    *string `json:"ticket_url,omitempty" yaml:"ticket_url,omitempty"`
	SlackChannel *string `json:"slack_channel,omitempty" yaml:"slack_channel,omitempty"`
	Requester    *string `json:"requester,omitempty" yaml:"requester,omitempty"`

	// Execution settings
	Timeout         string `json:"timeout,omitempty" yaml:"timeout,omitempty"` // e.g., "30m"
	RequireApproval bool   `json:"require_approval,omitempty" yaml:"require_approval,omitempty"`
	MaxParallel     int    `json:"max_parallel,omitempty" yaml:"max_parallel,omitempty"` // Concurrency limit across groups (default: 5)

	// Repository groups - defines how repos are organized into sandboxes
	// If empty, auto-generated from Repositories field (one group with all repos)
	Groups []RepositoryGroup `json:"groups,omitempty" yaml:"groups,omitempty"`

	// PR configuration (transform mode only)
	PullRequest *PullRequestConfig `json:"pull_request,omitempty" yaml:"pull_request,omitempty"`

	// Sandbox configuration (production K8s)
	Sandbox *SandboxConfig `json:"sandbox,omitempty" yaml:"sandbox,omitempty"`

	// Credentials configuration (production K8s)
	Credentials *CredentialsConfig `json:"credentials,omitempty" yaml:"credentials,omitempty"`
}

// GetMode returns the task mode, defaulting to transform.
func (t Task) GetMode() TaskMode {
	if t.Mode == "" {
		return TaskModeTransform
	}
	return t.Mode
}

// GetTimeoutMinutes returns the timeout in minutes, defaulting to 30.
func (t Task) GetTimeoutMinutes() int {
	if t.Timeout == "" {
		return 30
	}
	// Parse duration string
	d, err := time.ParseDuration(t.Timeout)
	if err != nil {
		return 30
	}
	return int(d.Minutes())
}

// UsesTransformationRepo returns true if the task uses a transformation repository.
func (t Task) UsesTransformationRepo() bool {
	return t.Transformation != nil
}

// GetEffectiveRepositories returns the repositories to operate on.
// When using transformation mode, returns Targets; otherwise returns Repositories.
func (t Task) GetEffectiveRepositories() []Repository {
	if t.UsesTransformationRepo() {
		return t.Targets
	}
	return t.Repositories
}

// GetMaxParallel returns the max parallel limit, defaulting to DefaultMaxParallel.
func (t Task) GetMaxParallel() int {
	if t.MaxParallel <= 0 {
		return DefaultMaxParallel
	}
	return t.MaxParallel
}

// GetExecutionGroups returns the groups to execute.
// If Groups is explicitly set, returns it.
// Otherwise, auto-generates groups from Repositories field for backward compatibility.
func (t Task) GetExecutionGroups() []RepositoryGroup {
	// If groups are explicitly defined, use them
	if len(t.Groups) > 0 {
		return t.Groups
	}

	// Backward compatibility: auto-generate groups from repositories
	effectiveRepos := t.GetEffectiveRepositories()
	if len(effectiveRepos) == 0 {
		return nil
	}

	// Create one group per repository for parallel execution by default
	// This maintains backward compatibility with the old "parallel" behavior when multiple repos were used
	var groups []RepositoryGroup
	for _, repo := range effectiveRepos {
		groups = append(groups, RepositoryGroup{
			Name:         repo.Name,
			Repositories: []Repository{repo},
		})
	}
	return groups
}

// GetAllRepositoriesFromGroups returns a flat list of all repositories from all groups.
func (t Task) GetAllRepositoriesFromGroups() []Repository {
	var allRepos []Repository
	for _, group := range t.GetExecutionGroups() {
		allRepos = append(allRepos, group.Repositories...)
	}
	return allRepos
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

// DeterministicResult is the result from running a deterministic transformation.
type DeterministicResult struct {
	Success       bool     `json:"success"`
	ExitCode      int      `json:"exit_code"`
	Output        string   `json:"output"`
	FilesModified []string `json:"files_modified"`
	Error         *string  `json:"error,omitempty"`
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

// ReportOutput represents the parsed output from a report-mode task.
type ReportOutput struct {
	Frontmatter      map[string]any `json:"frontmatter,omitempty"` // Structured data (validated against schema)
	Body             string         `json:"body,omitempty"`        // Markdown prose
	Raw              string         `json:"raw"`                   // Original unparsed output
	Error            string         `json:"error,omitempty"`       // Error message if parsing failed
	ValidationErrors []string       `json:"validation_errors,omitempty"`
}

// RepositoryResult represents the result for a single repository.
type RepositoryResult struct {
	Repository     string             `json:"repository"`
	Status         string             `json:"status"` // "success" | "failed" | "skipped"
	FilesModified  []string           `json:"files_modified,omitempty"`
	PullRequest    *PullRequest       `json:"pull_request,omitempty"`    // Transform mode
	Report         *ReportOutput      `json:"report,omitempty"`          // Report mode
	ForEachResults []ForEachExecution `json:"for_each_results,omitempty"` // Report mode with forEach
	Error          *string            `json:"error,omitempty"`
}

// TaskResult is the final result of the Transform workflow.
type TaskResult struct {
	TaskID          string             `json:"task_id"`
	Status          TaskStatus         `json:"status"`
	Mode            TaskMode           `json:"mode,omitempty"`
	Repositories    []RepositoryResult `json:"repositories,omitempty"`
	StartedAt       *time.Time         `json:"started_at,omitempty"`
	CompletedAt     *time.Time         `json:"completed_at,omitempty"`
	Error           *string            `json:"error,omitempty"`
	DurationSeconds *float64           `json:"duration_seconds,omitempty"`

	// Deprecated: Use Repositories[].PullRequest instead. Kept for backward compatibility.
	PullRequests []PullRequest `json:"pull_requests,omitempty"`
}

// NewTaskResult creates a TaskResult with the given status.
func NewTaskResult(taskID string, status TaskStatus) TaskResult {
	return TaskResult{
		TaskID:       taskID,
		Status:       status,
		PullRequests: []PullRequest{},
		Repositories: []RepositoryResult{},
	}
}

// WithMode returns a copy of the result with the specified mode.
func (r TaskResult) WithMode(mode TaskMode) TaskResult {
	r.Mode = mode
	return r
}

// WithStartedAt returns a copy of the result with the started timestamp.
func (r TaskResult) WithStartedAt(t time.Time) TaskResult {
	r.StartedAt = &t
	return r
}

// WithCompletedAt returns a copy of the result with the completed timestamp.
func (r TaskResult) WithCompletedAt(t time.Time) TaskResult {
	r.CompletedAt = &t
	return r
}

// WithRepositories returns a copy of the result with repository results.
func (r TaskResult) WithRepositories(repos []RepositoryResult) TaskResult {
	r.Repositories = repos
	return r
}

// WithError returns a copy of the result with an error message.
func (r TaskResult) WithError(err string) TaskResult {
	r.Error = &err
	return r
}

// WithDuration returns a copy of the result with a duration.
func (r TaskResult) WithDuration(seconds float64) TaskResult {
	r.DurationSeconds = &seconds
	return r
}

// WithPullRequests returns a copy of the result with pull requests.
func (r TaskResult) WithPullRequests(prs []PullRequest) TaskResult {
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
