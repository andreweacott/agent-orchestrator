package activity

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/google/go-github/v62/github"
	"go.temporal.io/sdk/activity"
	"golang.org/x/oauth2"

	"github.com/anthropics/claude-code-orchestrator/internal/docker"
	"github.com/anthropics/claude-code-orchestrator/internal/model"
)

// GitHubActivities contains activities for GitHub operations.
type GitHubActivities struct {
	DockerClient *docker.Client
}

// NewGitHubActivities creates a new GitHubActivities instance.
func NewGitHubActivities(client *docker.Client) *GitHubActivities {
	return &GitHubActivities{DockerClient: client}
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

// CreatePullRequest creates a pull request for changes in a repository.
func (a *GitHubActivities) CreatePullRequest(ctx context.Context, containerID string, repo model.Repository, taskID, title, description string) (*model.PullRequest, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Creating PR", "repo", repo.Name)

	// Check if there are changes
	statusCmd := fmt.Sprintf("cd /workspace/%s && git status --porcelain", repo.Name)
	statusResult, err := a.DockerClient.ExecShellCommand(ctx, containerID, statusCmd, "agent")
	if err != nil {
		return nil, fmt.Errorf("failed to check git status: %w", err)
	}

	if strings.TrimSpace(statusResult.Stdout) == "" {
		logger.Info("No changes in repo, skipping PR", "repo", repo.Name)
		return nil, nil
	}

	branchName := fmt.Sprintf("fix/claude-%s", taskID)

	// Configure git
	gitConfigCmds := []string{
		`git config --global user.email "claude-agent@example.com"`,
		`git config --global user.name "Claude Agent"`,
	}

	for _, cmd := range gitConfigCmds {
		result, err := a.DockerClient.ExecShellCommand(ctx, containerID, cmd, "agent")
		if err != nil || result.ExitCode != 0 {
			return nil, fmt.Errorf("failed to configure git: %s", result.Stderr)
		}
	}

	// Create branch and commit
	gitCommands := []string{
		fmt.Sprintf("cd /workspace/%s && git checkout -b %s", repo.Name, branchName),
		fmt.Sprintf("cd /workspace/%s && git add -A", repo.Name),
		fmt.Sprintf("cd /workspace/%s && git commit -m '%s'", repo.Name, title),
	}

	for _, cmd := range gitCommands {
		result, err := a.DockerClient.ExecShellCommand(ctx, containerID, cmd, "agent")
		if err != nil {
			return nil, fmt.Errorf("git command failed: %w", err)
		}
		if result.ExitCode != 0 {
			return nil, fmt.Errorf("git command failed: %s", result.Stderr)
		}
	}

	// Get GitHub token
	githubToken := os.Getenv("GITHUB_TOKEN")
	if githubToken == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN not set")
	}

	// Extract owner/repo from URL
	owner, repoName := extractOwnerRepo(repo.URL)
	if owner == "" || repoName == "" {
		return nil, fmt.Errorf("failed to extract owner/repo from URL: %s", repo.URL)
	}

	// Push with token
	pushURL := fmt.Sprintf("https://%s@github.com/%s/%s.git", githubToken, owner, repoName)
	pushCmd := fmt.Sprintf("cd /workspace/%s && git push %s %s", repo.Name, pushURL, branchName)
	pushResult, err := a.DockerClient.ExecShellCommand(ctx, containerID, pushCmd, "agent")
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

	pr, _, err := client.PullRequests.Create(ctx, owner, repoName, &github.NewPullRequest{
		Title: &title,
		Body:  &description,
		Head:  &branchName,
		Base:  &repo.Branch,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create PR: %w", err)
	}

	return &model.PullRequest{
		RepoName:   repo.Name,
		PRURL:      pr.GetHTMLURL(),
		PRNumber:   pr.GetNumber(),
		BranchName: branchName,
		Title:      title,
	}, nil
}
