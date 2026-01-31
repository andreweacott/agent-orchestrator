package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractRepoName(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"https://github.com/org/repo.git", "repo"},
		{"https://github.com/org/repo", "repo"},
		{"https://github.com/org/my-repo.git", "my-repo"},
		{"git@github.com:org/repo.git", "repo"},
		{"https://github.com/org/repo/", "repo"},
	}

	for _, tc := range tests {
		t.Run(tc.url, func(t *testing.T) {
			result := extractRepoName(tc.url)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestNewRepository(t *testing.T) {
	// Test with all values provided
	repo := NewRepository("https://github.com/org/repo.git", "develop", "custom-name")
	assert.Equal(t, "https://github.com/org/repo.git", repo.URL)
	assert.Equal(t, "develop", repo.Branch)
	assert.Equal(t, "custom-name", repo.Name)

	// Test with defaults
	repo = NewRepository("https://github.com/org/my-repo.git", "", "")
	assert.Equal(t, "main", repo.Branch)
	assert.Equal(t, "my-repo", repo.Name)
}

func TestNewBugFixTask(t *testing.T) {
	repos := []Repository{
		NewRepository("https://github.com/org/repo.git", "", ""),
	}

	task := NewBugFixTask("task-123", "Fix bug", "Description", repos)

	assert.Equal(t, "task-123", task.TaskID)
	assert.Equal(t, "Fix bug", task.Title)
	assert.Equal(t, "Description", task.Description)
	assert.Len(t, task.Repositories, 1)
	assert.Equal(t, 30, task.TimeoutMinutes)
	assert.True(t, task.RequireApproval)
	// SIMP-002: AutoMerge field was removed as unused
	assert.Nil(t, task.TicketURL)
	assert.Nil(t, task.SlackChannel)
}

func TestBugFixTaskJSON(t *testing.T) {
	slackChannel := "#dev"
	task := BugFixTask{
		TaskID:      "task-123",
		Title:       "Fix bug",
		Description: "Fix the bug in login",
		Repositories: []Repository{
			{URL: "https://github.com/org/repo.git", Branch: "main", Name: "repo"},
		},
		SlackChannel:    &slackChannel,
		TimeoutMinutes:  30,
		RequireApproval: true,
	}

	// Marshal to JSON
	data, err := json.Marshal(task)
	require.NoError(t, err)

	// Unmarshal back
	var decoded BugFixTask
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, task.TaskID, decoded.TaskID)
	assert.Equal(t, task.Title, decoded.Title)
	assert.NotNil(t, decoded.SlackChannel)
	assert.Equal(t, "#dev", *decoded.SlackChannel)
}

func TestBugFixResult(t *testing.T) {
	result := NewBugFixResult("task-123", TaskStatusCompleted)
	assert.Equal(t, "task-123", result.TaskID)
	assert.Equal(t, TaskStatusCompleted, result.Status)
	assert.Empty(t, result.PullRequests)
	assert.Nil(t, result.Error)

	// Test with error
	result = result.WithError("something went wrong")
	assert.NotNil(t, result.Error)
	assert.Equal(t, "something went wrong", *result.Error)

	// Test with duration
	result = result.WithDuration(123.45)
	assert.NotNil(t, result.DurationSeconds)
	assert.Equal(t, 123.45, *result.DurationSeconds)

	// Test with pull requests
	prs := []PullRequest{
		{RepoName: "repo", PRURL: "https://github.com/org/repo/pull/1", PRNumber: 1, BranchName: "fix/bug", Title: "Fix bug"},
	}
	result = result.WithPullRequests(prs)
	assert.Len(t, result.PullRequests, 1)
}

func TestTaskStatus(t *testing.T) {
	// Test that status values are correct strings
	assert.Equal(t, "pending", string(TaskStatusPending))
	assert.Equal(t, "awaiting_approval", string(TaskStatusAwaitingApproval))
	assert.Equal(t, "completed", string(TaskStatusCompleted))
	assert.Equal(t, "failed", string(TaskStatusFailed))
}

func TestClaudeCodeResult(t *testing.T) {
	result := ClaudeCodeResult{
		Success:       true,
		Output:        "Fixed the bug",
		FilesModified: []string{"src/main.go", "src/util.go"},
	}

	assert.True(t, result.Success)
	assert.Equal(t, "Fixed the bug", result.Output)
	assert.Len(t, result.FilesModified, 2)
	assert.Nil(t, result.Error)
	assert.False(t, result.NeedsClarification)

	// Test with clarification
	question := "Which file should I modify?"
	result = ClaudeCodeResult{
		Success:               false,
		Output:                "I need more information",
		NeedsClarification:    true,
		ClarificationQuestion: &question,
	}

	assert.False(t, result.Success)
	assert.True(t, result.NeedsClarification)
	assert.NotNil(t, result.ClarificationQuestion)
}

func TestSandboxInfo(t *testing.T) {
	sandbox := NewSandboxInfo("container-abc123")

	assert.Equal(t, "container-abc123", sandbox.ContainerID)
	assert.Equal(t, "/workspace", sandbox.WorkspacePath)
	assert.False(t, sandbox.CreatedAt.IsZero())
}

func TestPullRequest(t *testing.T) {
	pr := PullRequest{
		RepoName:   "my-repo",
		PRURL:      "https://github.com/org/my-repo/pull/42",
		PRNumber:   42,
		BranchName: "fix/claude-task-123",
		Title:      "fix: resolve login issue",
	}

	assert.Equal(t, "my-repo", pr.RepoName)
	assert.Equal(t, 42, pr.PRNumber)
	assert.Contains(t, pr.PRURL, "pull/42")
}
