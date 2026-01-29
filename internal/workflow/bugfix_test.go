package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/testsuite"

	"github.com/anthropics/claude-code-orchestrator/internal/model"
)

// MockActivities holds mock implementations of activities
type MockActivities struct {
	mock.Mock
}

func (m *MockActivities) ProvisionSandbox(ctx context.Context, taskID string) (*model.SandboxInfo, error) {
	args := m.Called(ctx, taskID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.SandboxInfo), args.Error(1)
}

func (m *MockActivities) CloneRepositories(ctx context.Context, sandbox model.SandboxInfo, repos []model.Repository, agentsMD string) ([]string, error) {
	args := m.Called(ctx, sandbox, repos, agentsMD)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockActivities) CleanupSandbox(ctx context.Context, containerID string) error {
	args := m.Called(ctx, containerID)
	return args.Error(0)
}

func (m *MockActivities) RunClaudeCode(ctx context.Context, containerID, prompt string, timeoutSeconds int) (*model.ClaudeCodeResult, error) {
	args := m.Called(ctx, containerID, prompt, timeoutSeconds)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.ClaudeCodeResult), args.Error(1)
}

func (m *MockActivities) GetClaudeOutput(ctx context.Context, containerID, repoName string) (map[string]string, error) {
	args := m.Called(ctx, containerID, repoName)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]string), args.Error(1)
}

func (m *MockActivities) CreatePullRequest(ctx context.Context, containerID string, repo model.Repository, taskID, title, description string) (*model.PullRequest, error) {
	args := m.Called(ctx, containerID, repo, taskID, title, description)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.PullRequest), args.Error(1)
}

func (m *MockActivities) NotifySlack(ctx context.Context, channel, message string, threadTS *string) (*string, error) {
	args := m.Called(ctx, channel, message, threadTS)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*string), args.Error(1)
}

type BugFixWorkflowTestSuite struct {
	suite.Suite
	testsuite.WorkflowTestSuite
	env            *testsuite.TestWorkflowEnvironment
	mockActivities *MockActivities
}

func (s *BugFixWorkflowTestSuite) SetupTest() {
	s.env = s.NewTestWorkflowEnvironment()
	s.mockActivities = &MockActivities{}

	// Register the mock activities
	s.env.RegisterActivity(s.mockActivities.ProvisionSandbox)
	s.env.RegisterActivity(s.mockActivities.CloneRepositories)
	s.env.RegisterActivity(s.mockActivities.CleanupSandbox)
	s.env.RegisterActivity(s.mockActivities.RunClaudeCode)
	s.env.RegisterActivity(s.mockActivities.GetClaudeOutput)
	s.env.RegisterActivity(s.mockActivities.CreatePullRequest)
	s.env.RegisterActivity(s.mockActivities.NotifySlack)
}

func (s *BugFixWorkflowTestSuite) TearDownTest() {
	s.env.AssertExpectations(s.T())
}

func (s *BugFixWorkflowTestSuite) TestBugFixWorkflowSuccess() {
	// Setup mock expectations
	s.mockActivities.On("ProvisionSandbox", mock.Anything, "test-123").
		Return(&model.SandboxInfo{
			ContainerID:   "container-abc",
			WorkspacePath: "/workspace",
			CreatedAt:     time.Now(),
		}, nil)

	s.mockActivities.On("CloneRepositories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return([]string{"/workspace/test-repo"}, nil)

	s.mockActivities.On("RunClaudeCode", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&model.ClaudeCodeResult{
			Success:       true,
			Output:        "Fixed the bug",
			FilesModified: []string{"src/main.go"},
		}, nil)

	s.mockActivities.On("CreatePullRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&model.PullRequest{
			RepoName:   "test-repo",
			PRURL:      "https://github.com/org/test-repo/pull/1",
			PRNumber:   1,
			BranchName: "fix/claude-test-123",
			Title:      "fix: Test bug",
		}, nil)

	s.mockActivities.On("CleanupSandbox", mock.Anything, "container-abc").
		Return(nil)

	task := model.BugFixTask{
		TaskID:      "test-123",
		Title:       "Test bug fix",
		Description: "Fix the test bug",
		Repositories: []model.Repository{
			{URL: "https://github.com/org/test-repo.git", Branch: "main", Name: "test-repo"},
		},
		RequireApproval: false,
		TimeoutMinutes:  30,
	}

	s.env.ExecuteWorkflow(BugFix, task)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.BugFixResult
	s.NoError(s.env.GetWorkflowResult(&result))

	s.Equal(model.TaskStatusCompleted, result.Status)
	s.Len(result.PullRequests, 1)
	s.Equal(1, result.PullRequests[0].PRNumber)
}

func (s *BugFixWorkflowTestSuite) TestBugFixWorkflowWithApproval() {
	// Setup mock expectations
	s.mockActivities.On("ProvisionSandbox", mock.Anything, "test-456").
		Return(&model.SandboxInfo{
			ContainerID:   "container-def",
			WorkspacePath: "/workspace",
			CreatedAt:     time.Now(),
		}, nil)

	s.mockActivities.On("CloneRepositories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return([]string{"/workspace/test-repo"}, nil)

	s.mockActivities.On("RunClaudeCode", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&model.ClaudeCodeResult{
			Success:       true,
			Output:        "Fixed the bug",
			FilesModified: []string{"src/main.go"},
		}, nil)

	s.mockActivities.On("CreatePullRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&model.PullRequest{
			RepoName:   "test-repo",
			PRURL:      "https://github.com/org/test-repo/pull/2",
			PRNumber:   2,
			BranchName: "fix/claude-test-456",
			Title:      "fix: Test with approval",
		}, nil)

	s.mockActivities.On("CleanupSandbox", mock.Anything, "container-def").
		Return(nil)

	task := model.BugFixTask{
		TaskID:      "test-456",
		Title:       "Test with approval",
		Description: "Need approval",
		Repositories: []model.Repository{
			{URL: "https://github.com/org/test-repo.git", Branch: "main", Name: "test-repo"},
		},
		RequireApproval: true,
		TimeoutMinutes:  30,
	}

	// Register a callback to send approval after a delay
	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(SignalApprove, nil)
	}, 5*time.Second)

	s.env.ExecuteWorkflow(BugFix, task)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.BugFixResult
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.TaskStatusCompleted, result.Status)
}

func (s *BugFixWorkflowTestSuite) TestBugFixWorkflowRejection() {
	// Setup mock expectations
	s.mockActivities.On("ProvisionSandbox", mock.Anything, "test-789").
		Return(&model.SandboxInfo{
			ContainerID:   "container-ghi",
			WorkspacePath: "/workspace",
			CreatedAt:     time.Now(),
		}, nil)

	s.mockActivities.On("CloneRepositories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return([]string{"/workspace/test-repo"}, nil)

	s.mockActivities.On("RunClaudeCode", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&model.ClaudeCodeResult{
			Success:       true,
			Output:        "Made some changes",
			FilesModified: []string{"src/main.go"},
		}, nil)

	s.mockActivities.On("CleanupSandbox", mock.Anything, "container-ghi").
		Return(nil)

	task := model.BugFixTask{
		TaskID:      "test-789",
		Title:       "Test rejection",
		Description: "This will be rejected",
		Repositories: []model.Repository{
			{URL: "https://github.com/org/test-repo.git", Branch: "main", Name: "test-repo"},
		},
		RequireApproval: true,
		TimeoutMinutes:  30,
	}

	// Register a callback to send rejection after a delay
	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow(SignalReject, nil)
	}, 5*time.Second)

	s.env.ExecuteWorkflow(BugFix, task)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.BugFixResult
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.TaskStatusCancelled, result.Status)
}

func (s *BugFixWorkflowTestSuite) TestBugFixWorkflowFailure() {
	// Setup mock expectations - Claude Code fails
	s.mockActivities.On("ProvisionSandbox", mock.Anything, "test-fail").
		Return(&model.SandboxInfo{
			ContainerID:   "container-fail",
			WorkspacePath: "/workspace",
			CreatedAt:     time.Now(),
		}, nil)

	s.mockActivities.On("CloneRepositories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return([]string{"/workspace/test-repo"}, nil)

	errorMsg := "Claude Code execution failed"
	s.mockActivities.On("RunClaudeCode", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&model.ClaudeCodeResult{
			Success: false,
			Output:  "",
			Error:   &errorMsg,
		}, nil)

	s.mockActivities.On("CleanupSandbox", mock.Anything, "container-fail").
		Return(nil)

	task := model.BugFixTask{
		TaskID:      "test-fail",
		Title:       "Test failure",
		Description: "This will fail",
		Repositories: []model.Repository{
			{URL: "https://github.com/org/test-repo.git", Branch: "main", Name: "test-repo"},
		},
		RequireApproval: false,
		TimeoutMinutes:  30,
	}

	s.env.ExecuteWorkflow(BugFix, task)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.BugFixResult
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.TaskStatusFailed, result.Status)
	s.NotNil(result.Error)
}

func TestBugFixWorkflowTestSuite(t *testing.T) {
	suite.Run(t, new(BugFixWorkflowTestSuite))
}
