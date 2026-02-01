package activity

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/google/go-github/v62/github"
	"go.temporal.io/sdk/activity"
	"golang.org/x/oauth2"

	"github.com/andreweacott/agent-orchestrator/internal/model"
	"github.com/andreweacott/agent-orchestrator/internal/sandbox"
)

// GitHubActivities contains activities for GitHub operations.
type GitHubActivities struct {
	Provider sandbox.Provider
}

// NewGitHubActivities creates a new GitHubActivities instance.
func NewGitHubActivities(provider sandbox.Provider) *GitHubActivities {
	return &GitHubActivities{Provider: provider}
}

// extractOwnerRepo extracts owner and repo name from a GitHub URL.
// e.g., "https://github.com/owner/repo.git" -> ("owner", "repo")
func extractOwnerRepo(url string) (string, string) {
	url = strings.TrimSuffix(url, "/")
	url = strings.TrimSuffix(url, ".git")
	parts := strings.Split(url, "/")
	if len(parts) < 2 {
		return "", ""
	}
	return parts[len(parts)-2], parts[len(parts)-1]
}

// CreatePullRequestInput contains all inputs for creating a pull request.
type CreatePullRequestInput struct {
	ContainerID             string
	Repo                    model.Repository
	TaskID                  string
	Title                   string
	Description             string
	PRConfig                *model.PullRequestConfig
	UseTransformationLayout bool // If true, repo is at /workspace/targets/{name}
}

// CreatePullRequest creates a pull request for changes in a repository.
func (a *GitHubActivities) CreatePullRequest(ctx context.Context, input CreatePullRequestInput) (*model.PullRequest, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Creating PR", "repo", input.Repo.Name)

	// Determine repo path based on layout
	repoPath := fmt.Sprintf("/workspace/%s", input.Repo.Name)
	if input.UseTransformationLayout {
		repoPath = fmt.Sprintf("/workspace/targets/%s", input.Repo.Name)
	}

	// Check if there are changes
	statusCmd := fmt.Sprintf("cd %s && git status --porcelain", repoPath)
	statusResult, err := a.Provider.ExecShell(ctx, input.ContainerID, statusCmd, AgentUser)
	if err != nil {
		return nil, fmt.Errorf("failed to check git status: %w", err)
	}

	if strings.TrimSpace(statusResult.Stdout) == "" {
		logger.Info("No changes in repo, skipping PR", "repo", input.Repo.Name)
		return nil, nil
	}

	// Determine branch name
	branchPrefix := BranchPrefix
	if input.PRConfig != nil && input.PRConfig.BranchPrefix != "" {
		branchPrefix = input.PRConfig.BranchPrefix
		// Ensure it ends with a separator if not already
		if !strings.HasSuffix(branchPrefix, "/") && !strings.HasSuffix(branchPrefix, "-") {
			branchPrefix += "-"
		}
	}
	branchName := fmt.Sprintf("%s%s", branchPrefix, input.TaskID)

	// Configure git with configurable identity
	gitEmail := getEnvOrDefault("GIT_USER_EMAIL", DefaultGitEmail)
	gitName := getEnvOrDefault("GIT_USER_NAME", DefaultGitName)
	gitConfigCmds := []string{
		fmt.Sprintf(`git config --global user.email "%s"`, gitEmail),
		fmt.Sprintf(`git config --global user.name "%s"`, gitName),
	}

	for _, cmd := range gitConfigCmds {
		result, err := a.Provider.ExecShell(ctx, input.ContainerID, cmd, AgentUser)
		if err != nil || result.ExitCode != 0 {
			return nil, fmt.Errorf("failed to configure git: %s", result.Stderr)
		}
	}

	// Create branch
	checkoutCmd := fmt.Sprintf("cd %s && git checkout -b %s", repoPath, branchName)
	result, err := a.Provider.ExecShell(ctx, input.ContainerID, checkoutCmd, AgentUser)
	if err != nil || result.ExitCode != 0 {
		return nil, fmt.Errorf("git checkout failed: %s", result.Stderr)
	}

	// Stage all changes
	addCmd := fmt.Sprintf("cd %s && git add -A", repoPath)
	result, err = a.Provider.ExecShell(ctx, input.ContainerID, addCmd, AgentUser)
	if err != nil || result.ExitCode != 0 {
		return nil, fmt.Errorf("git add failed: %s", result.Stderr)
	}

	// BUG-001 Fix: Use heredoc for commit message to handle special characters (quotes, etc.)
	commitCmd := fmt.Sprintf(`cd %s && git commit -m "$(cat <<'COMMIT_MSG_EOF'
%s
COMMIT_MSG_EOF
)"`, repoPath, input.Title)
	result, err = a.Provider.ExecShell(ctx, input.ContainerID, commitCmd, AgentUser)
	if err != nil || result.ExitCode != 0 {
		return nil, fmt.Errorf("git commit failed: %s", result.Stderr)
	}

	// Get GitHub token
	githubToken := os.Getenv("GITHUB_TOKEN")
	if githubToken == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN not set")
	}

	// Extract owner/repo from URL
	owner, repoName := extractOwnerRepo(input.Repo.URL)
	if owner == "" || repoName == "" {
		return nil, fmt.Errorf("failed to extract owner/repo from URL: %s", input.Repo.URL)
	}

	// SEC-002 Fix: Use environment variable expansion instead of embedding token in command
	// The GITHUB_TOKEN is already set in the container environment by ProvisionSandbox
	// This prevents the token from appearing in shell command strings and logs
	pushCmd := fmt.Sprintf(`cd %s && git push "https://x-access-token:${GITHUB_TOKEN}@github.com/%s/%s.git" %s`,
		repoPath, owner, repoName, branchName)
	pushResult, err := a.Provider.ExecShell(ctx, input.ContainerID, pushCmd, AgentUser)
	if err != nil {
		return nil, fmt.Errorf("git push failed: %w", err)
	}
	if pushResult.ExitCode != 0 {
		return nil, fmt.Errorf("git push failed: %s", pushResult.Stderr)
	}

	// Create PR via GitHub API
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: githubToken})
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	// Use PR config title/body if provided
	prTitle := input.Title
	prBody := input.Description
	if input.PRConfig != nil {
		if input.PRConfig.Title != "" {
			prTitle = input.PRConfig.Title
		}
		if input.PRConfig.Body != "" {
			prBody = input.PRConfig.Body
		}
	}

	pr, _, err := client.PullRequests.Create(ctx, owner, repoName, &github.NewPullRequest{
		Title: &prTitle,
		Body:  &prBody,
		Head:  &branchName,
		Base:  &input.Repo.Branch,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create PR: %w", err)
	}

	// Add labels if configured
	if input.PRConfig != nil && len(input.PRConfig.Labels) > 0 {
		_, _, err := client.Issues.AddLabelsToIssue(ctx, owner, repoName, pr.GetNumber(), input.PRConfig.Labels)
		if err != nil {
			logger.Warn("Failed to add labels to PR", "error", err)
		}
	}

	// Request reviewers if configured
	if input.PRConfig != nil && len(input.PRConfig.Reviewers) > 0 {
		_, _, err := client.PullRequests.RequestReviewers(ctx, owner, repoName, pr.GetNumber(), github.ReviewersRequest{
			Reviewers: input.PRConfig.Reviewers,
		})
		if err != nil {
			logger.Warn("Failed to request reviewers", "error", err)
		}
	}

	return &model.PullRequest{
		RepoName:   input.Repo.Name,
		PRURL:      pr.GetHTMLURL(),
		PRNumber:   pr.GetNumber(),
		BranchName: branchName,
		Title:      prTitle,
	}, nil
}

// CreatePullRequestLegacy is the legacy signature for backward compatibility.
// Deprecated: Use CreatePullRequest with CreatePullRequestInput instead.
func (a *GitHubActivities) CreatePullRequestLegacy(ctx context.Context, containerID string, repo model.Repository, taskID, title, description string) (*model.PullRequest, error) {
	return a.CreatePullRequest(ctx, CreatePullRequestInput{
		ContainerID: containerID,
		Repo:        repo,
		TaskID:      taskID,
		Title:       title,
		Description: description,
		PRConfig:    nil,
	})
}
