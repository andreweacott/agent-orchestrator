package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/testsuite"

	"github.com/andreweacott/agent-orchestrator/internal/model"
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

func (m *MockActivities) ExecuteDeterministic(ctx context.Context, sandbox model.SandboxInfo, image string, args []string, env map[string]string, repos []model.Repository) (*model.DeterministicResult, error) {
	a := m.Called(ctx, sandbox, image, args, env, repos)
	if a.Get(0) == nil {
		return nil, a.Error(1)
	}
	return a.Get(0).(*model.DeterministicResult), a.Error(1)
}

func (m *MockActivities) RunVerifiers(ctx context.Context, sandbox model.SandboxInfo, repos []model.Repository, verifiers []model.Verifier) (*model.VerifiersResult, error) {
	a := m.Called(ctx, sandbox, repos, verifiers)
	if a.Get(0) == nil {
		return nil, a.Error(1)
	}
	return a.Get(0).(*model.VerifiersResult), a.Error(1)
}

type TransformWorkflowTestSuite struct {
	suite.Suite
	testsuite.WorkflowTestSuite
	env            *testsuite.TestWorkflowEnvironment
	mockActivities *MockActivities
}

func (s *TransformWorkflowTestSuite) SetupTest() {
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
	s.env.RegisterActivity(s.mockActivities.ExecuteDeterministic)
	s.env.RegisterActivity(s.mockActivities.RunVerifiers)
}

func (s *TransformWorkflowTestSuite) TearDownTest() {
	s.env.AssertExpectations(s.T())
}

func (s *TransformWorkflowTestSuite) TestTransformWorkflowSuccess() {
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

	task := model.TransformTask{
		TaskID:      "test-123",
		Title:       "Test bug fix",
		Description: "Fix the test bug",
		Repositories: []model.Repository{
			{URL: "https://github.com/org/test-repo.git", Branch: "main", Name: "test-repo"},
		},
		RequireApproval: false,
		TimeoutMinutes:  30,
	}

	s.env.ExecuteWorkflow(Transform, task)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.TransformResult
	s.NoError(s.env.GetWorkflowResult(&result))

	s.Equal(model.TaskStatusCompleted, result.Status)
	s.Len(result.PullRequests, 1)
	s.Equal(1, result.PullRequests[0].PRNumber)
}

func (s *TransformWorkflowTestSuite) TestTransformWorkflowWithApproval() {
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

	task := model.TransformTask{
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

	s.env.ExecuteWorkflow(Transform, task)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.TransformResult
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.TaskStatusCompleted, result.Status)
}

func (s *TransformWorkflowTestSuite) TestTransformWorkflowRejection() {
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

	task := model.TransformTask{
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

	s.env.ExecuteWorkflow(Transform, task)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.TransformResult
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.TaskStatusCancelled, result.Status)
}

func (s *TransformWorkflowTestSuite) TestTransformWorkflowFailure() {
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

	task := model.TransformTask{
		TaskID:      "test-fail",
		Title:       "Test failure",
		Description: "This will fail",
		Repositories: []model.Repository{
			{URL: "https://github.com/org/test-repo.git", Branch: "main", Name: "test-repo"},
		},
		RequireApproval: false,
		TimeoutMinutes:  30,
	}

	s.env.ExecuteWorkflow(Transform, task)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.TransformResult
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal(model.TaskStatusFailed, result.Status)
	s.NotNil(result.Error)
}

func (s *TransformWorkflowTestSuite) TestDeterministicTransformationSuccess() {
	// Setup mock expectations for deterministic transformation
	s.mockActivities.On("ProvisionSandbox", mock.Anything, "test-det-001").
		Return(&model.SandboxInfo{
			ContainerID:   "container-det-001",
			WorkspacePath: "/workspace",
			CreatedAt:     time.Now(),
		}, nil)

	s.mockActivities.On("CloneRepositories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return([]string{"/workspace/test-repo"}, nil)

	s.mockActivities.On("ExecuteDeterministic", mock.Anything, mock.Anything, "my-transform:latest", []string{"--fix"}, map[string]string{"DEBUG": "1"}, mock.Anything).
		Return(&model.DeterministicResult{
			Success:       true,
			ExitCode:      0,
			Output:        "Applied 3 transformations",
			FilesModified: []string{"test-repo/src/main.go", "test-repo/src/utils.go"},
		}, nil)

	s.mockActivities.On("CreatePullRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&model.PullRequest{
			RepoName:   "test-repo",
			PRURL:      "https://github.com/org/test-repo/pull/10",
			PRNumber:   10,
			BranchName: "fix/claude-test-det-001",
			Title:      "fix: Deterministic transform",
		}, nil)

	s.mockActivities.On("CleanupSandbox", mock.Anything, "container-det-001").
		Return(nil)

	task := model.TransformTask{
		TaskID:      "test-det-001",
		Title:       "Deterministic transform",
		Description: "Apply automated fix",
		Repositories: []model.Repository{
			{URL: "https://github.com/org/test-repo.git", Branch: "main", Name: "test-repo"},
		},
		RequireApproval: false,
		TimeoutMinutes:  30,
		TransformMode:   model.TransformModeDeterministic,
		TransformImage:  "my-transform:latest",
		TransformArgs:   []string{"--fix"},
		TransformEnv:    map[string]string{"DEBUG": "1"},
	}

	s.env.ExecuteWorkflow(Transform, task)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.TransformResult
	s.NoError(s.env.GetWorkflowResult(&result))

	s.Equal(model.TaskStatusCompleted, result.Status)
	s.Len(result.PullRequests, 1)
	s.Equal(10, result.PullRequests[0].PRNumber)
}

func (s *TransformWorkflowTestSuite) TestDeterministicTransformationNoChanges() {
	// Setup mock expectations - transformation makes no changes
	s.mockActivities.On("ProvisionSandbox", mock.Anything, "test-det-002").
		Return(&model.SandboxInfo{
			ContainerID:   "container-det-002",
			WorkspacePath: "/workspace",
			CreatedAt:     time.Now(),
		}, nil)

	s.mockActivities.On("CloneRepositories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return([]string{"/workspace/test-repo"}, nil)

	s.mockActivities.On("ExecuteDeterministic", mock.Anything, mock.Anything, "my-transform:latest", mock.Anything, mock.Anything, mock.Anything).
		Return(&model.DeterministicResult{
			Success:       true,
			ExitCode:      0,
			Output:        "No changes needed",
			FilesModified: []string{}, // No files modified
		}, nil)

	s.mockActivities.On("CleanupSandbox", mock.Anything, "container-det-002").
		Return(nil)

	task := model.TransformTask{
		TaskID:      "test-det-002",
		Title:       "No-op transform",
		Description: "Nothing to change",
		Repositories: []model.Repository{
			{URL: "https://github.com/org/test-repo.git", Branch: "main", Name: "test-repo"},
		},
		RequireApproval: false,
		TimeoutMinutes:  30,
		TransformMode:   model.TransformModeDeterministic,
		TransformImage:  "my-transform:latest",
	}

	s.env.ExecuteWorkflow(Transform, task)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.TransformResult
	s.NoError(s.env.GetWorkflowResult(&result))

	// Should complete successfully but with no PRs
	s.Equal(model.TaskStatusCompleted, result.Status)
	s.Len(result.PullRequests, 0)
}

func (s *TransformWorkflowTestSuite) TestDeterministicTransformationFailure() {
	// Setup mock expectations - transformation fails with non-zero exit
	s.mockActivities.On("ProvisionSandbox", mock.Anything, "test-det-003").
		Return(&model.SandboxInfo{
			ContainerID:   "container-det-003",
			WorkspacePath: "/workspace",
			CreatedAt:     time.Now(),
		}, nil)

	s.mockActivities.On("CloneRepositories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return([]string{"/workspace/test-repo"}, nil)

	errMsg := "transformation exited with code 1"
	s.mockActivities.On("ExecuteDeterministic", mock.Anything, mock.Anything, "failing-transform:latest", mock.Anything, mock.Anything, mock.Anything).
		Return(&model.DeterministicResult{
			Success:  false,
			ExitCode: 1,
			Output:   "Error: invalid input",
			Error:    &errMsg,
		}, nil)

	s.mockActivities.On("CleanupSandbox", mock.Anything, "container-det-003").
		Return(nil)

	task := model.TransformTask{
		TaskID:      "test-det-003",
		Title:       "Failing transform",
		Description: "This will fail",
		Repositories: []model.Repository{
			{URL: "https://github.com/org/test-repo.git", Branch: "main", Name: "test-repo"},
		},
		RequireApproval: false,
		TimeoutMinutes:  30,
		TransformMode:   model.TransformModeDeterministic,
		TransformImage:  "failing-transform:latest",
	}

	s.env.ExecuteWorkflow(Transform, task)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result model.TransformResult
	s.NoError(s.env.GetWorkflowResult(&result))

	s.Equal(model.TaskStatusFailed, result.Status)
	s.NotNil(result.Error)
	s.Contains(*result.Error, "transformation exited with code 1")
}

func TestTransformWorkflowTestSuite(t *testing.T) {
	suite.Run(t, new(TransformWorkflowTestSuite))
}
