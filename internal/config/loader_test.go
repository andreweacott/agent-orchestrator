package config

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/model"
)

func TestLoadTask_Valid(t *testing.T) {
	yaml := `
version: 1
id: test-task
title: Test Task
repositories:
  - url: https://github.com/org/repo.git
    branch: main
execution:
  agentic:
    prompt: Fix the bug
`
	task, err := LoadTask([]byte(yaml))
	require.NoError(t, err)
	assert.Equal(t, 1, task.Version)
	assert.Equal(t, "test-task", task.ID)
	assert.Equal(t, "Test Task", task.Title)
	assert.Len(t, task.Repositories, 1)
	assert.Equal(t, "https://github.com/org/repo.git", task.Repositories[0].URL)
	assert.NotNil(t, task.Execution.Agentic)
	assert.Equal(t, "Fix the bug", task.Execution.Agentic.Prompt)
}

func TestLoadTask_MissingVersion(t *testing.T) {
	yaml := `
id: test-task
title: Test Task
repositories:
  - url: https://github.com/org/repo.git
execution:
  agentic:
    prompt: Fix the bug
`
	_, err := LoadTask([]byte(yaml))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version field is required")
}

func TestLoadTask_UnsupportedVersion(t *testing.T) {
	yaml := `
version: 99
id: test-task
title: Test Task
repositories:
  - url: https://github.com/org/repo.git
execution:
  agentic:
    prompt: Fix the bug
`
	_, err := LoadTask([]byte(yaml))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported schema version: 99")
}

func TestLoadTask_MissingID(t *testing.T) {
	yaml := `
version: 1
title: Test Task
repositories:
  - url: https://github.com/org/repo.git
execution:
  agentic:
    prompt: Fix the bug
`
	_, err := LoadTask([]byte(yaml))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "id field is required")
}

func TestLoadTask_MissingTitle(t *testing.T) {
	yaml := `
version: 1
id: test-task
repositories:
  - url: https://github.com/org/repo.git
execution:
  agentic:
    prompt: Fix the bug
`
	_, err := LoadTask([]byte(yaml))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "title field is required")
}

func TestLoadTask_MissingRepositories(t *testing.T) {
	yaml := `
version: 1
id: test-task
title: Test Task
execution:
  agentic:
    prompt: Fix the bug
`
	_, err := LoadTask([]byte(yaml))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one repository is required")
}

func TestLoadTask_MissingExecution(t *testing.T) {
	yaml := `
version: 1
id: test-task
title: Test Task
repositories:
  - url: https://github.com/org/repo.git
`
	_, err := LoadTask([]byte(yaml))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "execution must specify either agentic or deterministic")
}

func TestLoadTask_BothExecutionTypes(t *testing.T) {
	yaml := `
version: 1
id: test-task
title: Test Task
repositories:
  - url: https://github.com/org/repo.git
execution:
  agentic:
    prompt: Fix the bug
  deterministic:
    image: test:latest
`
	_, err := LoadTask([]byte(yaml))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "execution cannot specify both agentic and deterministic")
}

func TestLoadTask_AgenticMissingPrompt(t *testing.T) {
	yaml := `
version: 1
id: test-task
title: Test Task
repositories:
  - url: https://github.com/org/repo.git
execution:
  agentic:
    verifiers:
      - name: build
        command: ["go", "build"]
`
	_, err := LoadTask([]byte(yaml))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agentic execution requires prompt")
}

func TestLoadTask_DeterministicMissingImage(t *testing.T) {
	yaml := `
version: 1
id: test-task
title: Test Task
repositories:
  - url: https://github.com/org/repo.git
execution:
  deterministic:
    args: ["--fix"]
`
	_, err := LoadTask([]byte(yaml))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deterministic execution requires image")
}

func TestLoadTask_Deterministic(t *testing.T) {
	yaml := `
version: 1
id: test-det
title: Deterministic Task
repositories:
  - url: https://github.com/org/repo.git
execution:
  deterministic:
    image: my-transform:latest
    args: ["--fix"]
    env:
      DEBUG: "1"
    verifiers:
      - name: test
        command: ["go", "test"]
`
	task, err := LoadTask([]byte(yaml))
	require.NoError(t, err)
	assert.Nil(t, task.Execution.Agentic)
	assert.NotNil(t, task.Execution.Deterministic)
	assert.Equal(t, "my-transform:latest", task.Execution.Deterministic.Image)
	assert.Equal(t, []string{"--fix"}, task.Execution.Deterministic.Args)
	assert.Equal(t, "1", task.Execution.Deterministic.Env["DEBUG"])
	assert.Len(t, task.Execution.Deterministic.Verifiers, 1)
}

func TestLoadTask_DefaultValues(t *testing.T) {
	yaml := `
version: 1
id: test-task
title: Test Task
repositories:
  - url: https://github.com/org/repo.git
execution:
  agentic:
    prompt: Fix the bug
`
	task, err := LoadTask([]byte(yaml))
	require.NoError(t, err)

	// Check defaults
	assert.True(t, task.RequireApproval)
	assert.Equal(t, "main", task.Repositories[0].Branch)
	assert.Equal(t, model.TaskModeTransform, task.GetMode())
	assert.Equal(t, 30, task.GetTimeoutMinutes())
}

func TestLoadTask_WithPullRequestConfig(t *testing.T) {
	yaml := `
version: 1
id: test-task
title: Test Task
repositories:
  - url: https://github.com/org/repo.git
execution:
  agentic:
    prompt: Fix the bug
pull_request:
  branch_prefix: "auto/fix"
  title: "Automated Fix"
  labels: ["automated"]
  reviewers: ["alice", "bob"]
`
	task, err := LoadTask([]byte(yaml))
	require.NoError(t, err)
	require.NotNil(t, task.PullRequest)
	assert.Equal(t, "auto/fix", task.PullRequest.BranchPrefix)
	assert.Equal(t, "Automated Fix", task.PullRequest.Title)
	assert.Equal(t, []string{"automated"}, task.PullRequest.Labels)
	assert.Equal(t, []string{"alice", "bob"}, task.PullRequest.Reviewers)
}

func TestLoadTask_WithAgentLimits(t *testing.T) {
	yaml := `
version: 1
id: test-task
title: Test Task
repositories:
  - url: https://github.com/org/repo.git
execution:
  agentic:
    prompt: Fix the bug
    limits:
      max_tokens: 100000
      max_iterations: 50
      max_verifier_retries: 5
`
	task, err := LoadTask([]byte(yaml))
	require.NoError(t, err)
	require.NotNil(t, task.Execution.Agentic)
	require.NotNil(t, task.Execution.Agentic.Limits)
	assert.Equal(t, 100000, task.Execution.Agentic.Limits.MaxTokens)
	assert.Equal(t, 50, task.Execution.Agentic.Limits.MaxIterations)
	assert.Equal(t, 5, task.Execution.Agentic.Limits.MaxVerifierRetries)
}

func TestLoadTask_WithAgentLimitsBackwardCompat(t *testing.T) {
	// Test backward compatibility with old field names
	yaml := `
version: 1
id: test-task
title: Test Task
repositories:
  - url: https://github.com/org/repo.git
execution:
  agentic:
    prompt: Fix the bug
    limits:
      max_tokens: 100000
      max_turns: 50
      max_file_reads: 3
`
	task, err := LoadTask([]byte(yaml))
	require.NoError(t, err)
	require.NotNil(t, task.Execution.Agentic)
	require.NotNil(t, task.Execution.Agentic.Limits)
	assert.Equal(t, 100000, task.Execution.Agentic.Limits.MaxTokens)
	// Old max_turns maps to new MaxIterations
	assert.Equal(t, 50, task.Execution.Agentic.Limits.MaxIterations)
	// Old max_file_reads maps to new MaxVerifierRetries
	assert.Equal(t, 3, task.Execution.Agentic.Limits.MaxVerifierRetries)
}

func TestLoadTask_RepositoryDefaults(t *testing.T) {
	yaml := `
version: 1
id: test-task
title: Test Task
repositories:
  - url: https://github.com/org/my-repo.git
execution:
  agentic:
    prompt: Fix the bug
`
	task, err := LoadTask([]byte(yaml))
	require.NoError(t, err)

	// Auto-derived values
	assert.Equal(t, "main", task.Repositories[0].Branch)
	assert.Equal(t, "my-repo", task.Repositories[0].Name)
}

func TestLoadTask_ExplicitRepositoryValues(t *testing.T) {
	yaml := `
version: 1
id: test-task
title: Test Task
repositories:
  - url: https://github.com/org/my-repo.git
    branch: develop
    name: custom-name
    setup:
      - npm install
      - npm run build
execution:
  agentic:
    prompt: Fix the bug
`
	task, err := LoadTask([]byte(yaml))
	require.NoError(t, err)

	assert.Equal(t, "develop", task.Repositories[0].Branch)
	assert.Equal(t, "custom-name", task.Repositories[0].Name)
	assert.Equal(t, []string{"npm install", "npm run build"}, task.Repositories[0].Setup)
}

func TestLoadTask_ForEachValid(t *testing.T) {
	yamlContent := `
version: 1
id: test-foreach
title: ForEach Test
mode: report
repositories:
  - url: https://github.com/org/repo.git
for_each:
  - name: users-api
    context: Handles user authentication
  - name: orders-api
    context: Handles order processing
execution:
  agentic:
    prompt: Analyze endpoint
`
	task, err := LoadTask([]byte(yamlContent))
	require.NoError(t, err)

	assert.Len(t, task.ForEach, 2)
	assert.Equal(t, "users-api", task.ForEach[0].Name)
	assert.Equal(t, "Handles user authentication", task.ForEach[0].Context)
	assert.Equal(t, "orders-api", task.ForEach[1].Name)
	assert.Equal(t, "Handles order processing", task.ForEach[1].Context)
}

func TestLoadTask_ForEachInvalidTargetName(t *testing.T) {
	tests := []struct {
		name        string
		targetName  string
		wantErr     string
	}{
		{
			name:       "empty name",
			targetName: "",
			wantErr:    "for_each target name is required",
		},
		{
			name:       "spaces in name",
			targetName: "users api",
			wantErr:    "contains invalid characters",
		},
		{
			name:       "special characters",
			targetName: "users/api",
			wantErr:    "contains invalid characters",
		},
		{
			name:       "dots in name",
			targetName: "users.api",
			wantErr:    "contains invalid characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yaml := fmt.Sprintf(`
version: 1
id: test-foreach
title: ForEach Test
mode: report
repositories:
  - url: https://github.com/org/repo.git
for_each:
  - name: "%s"
    context: Some context
execution:
  agentic:
    prompt: Analyze target
`, tt.targetName)
			_, err := LoadTask([]byte(yaml))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestLoadTask_ForEachValidTargetNames(t *testing.T) {
	validNames := []string{
		"users-api",
		"orders_api",
		"PaymentsAPI",
		"api123",
		"a",
		"API-v2_test",
	}

	for _, name := range validNames {
		t.Run(name, func(t *testing.T) {
			yaml := fmt.Sprintf(`
version: 1
id: test-foreach
title: ForEach Test
mode: report
repositories:
  - url: https://github.com/org/repo.git
for_each:
  - name: "%s"
    context: Some context
execution:
  agentic:
    prompt: Analyze target
`, name)
			task, err := LoadTask([]byte(yaml))
			require.NoError(t, err)
			assert.Equal(t, name, task.ForEach[0].Name)
		})
	}
}

func TestLoadTask_ForEachWithTransformModeError(t *testing.T) {
	yaml := `
version: 1
id: test-foreach
title: ForEach Test
mode: transform
repositories:
  - url: https://github.com/org/repo.git
for_each:
  - name: users-api
    context: Some context
execution:
  agentic:
    prompt: Fix the bug
`
	_, err := LoadTask([]byte(yaml))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "for_each can only be used with mode: report")
}

func TestLoadTask_TransformationWithTargets(t *testing.T) {
	yamlContent := `
version: 1
id: test-transformation
title: Transformation Test
mode: report

transformation:
  url: https://github.com/org/classification-tools.git
  branch: main
  setup:
    - npm install

targets:
  - url: https://github.com/org/api-server.git
    name: server
  - url: https://github.com/org/web-client.git
    name: client

for_each:
  - name: users-endpoint
    context: |
      Endpoint: GET /api/v1/users

execution:
  agentic:
    prompt: Analyze {{.Name}}
`
	task, err := LoadTask([]byte(yamlContent))
	require.NoError(t, err)

	// Check transformation repo
	require.NotNil(t, task.Transformation)
	assert.Equal(t, "https://github.com/org/classification-tools.git", task.Transformation.URL)
	assert.Equal(t, "main", task.Transformation.Branch)
	assert.Equal(t, []string{"npm install"}, task.Transformation.Setup)
	assert.Equal(t, "classification-tools", task.Transformation.Name) // auto-derived

	// Check targets
	assert.Len(t, task.Targets, 2)
	assert.Equal(t, "server", task.Targets[0].Name)
	assert.Equal(t, "client", task.Targets[1].Name)

	// Repositories should be empty when using transformation mode
	assert.Empty(t, task.Repositories)

	// Check helper methods
	assert.True(t, task.UsesTransformationRepo())
	effectiveRepos := task.GetEffectiveRepositories()
	assert.Len(t, effectiveRepos, 2)
	assert.Equal(t, "server", effectiveRepos[0].Name)
}

func TestLoadTask_TransformationWithoutTargets(t *testing.T) {
	// Transformation repo without targets is valid (self-contained recipe)
	yamlContent := `
version: 1
id: test-transformation-only
title: Self-Contained Transformation
mode: report

transformation:
  url: https://github.com/org/analysis-tools.git
  branch: main

execution:
  agentic:
    prompt: Run the analysis
`
	task, err := LoadTask([]byte(yamlContent))
	require.NoError(t, err)

	require.NotNil(t, task.Transformation)
	assert.Empty(t, task.Targets)
	assert.Empty(t, task.Repositories)
	assert.True(t, task.UsesTransformationRepo())
	assert.Empty(t, task.GetEffectiveRepositories())
}

func TestLoadTask_TransformationWithRepositoriesError(t *testing.T) {
	// Cannot use both transformation and repositories
	yamlContent := `
version: 1
id: test-invalid
title: Invalid Mix
mode: report

transformation:
  url: https://github.com/org/tools.git

repositories:
  - url: https://github.com/org/repo.git

execution:
  agentic:
    prompt: Do something
`
	_, err := LoadTask([]byte(yamlContent))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot use both 'transformation' and 'repositories'")
}

func TestLoadTask_TargetsWithoutTransformationError(t *testing.T) {
	// Targets require transformation to be set
	yamlContent := `
version: 1
id: test-invalid
title: Invalid Targets
mode: report

targets:
  - url: https://github.com/org/repo.git

execution:
  agentic:
    prompt: Do something
`
	_, err := LoadTask([]byte(yamlContent))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "'targets' requires 'transformation' to be set")
}

func TestLoadTask_LegacyRepositoriesStillWorks(t *testing.T) {
	// Ensure backward compatibility
	yamlContent := `
version: 1
id: test-legacy
title: Legacy Task
repositories:
  - url: https://github.com/org/repo.git
    branch: develop
    name: my-repo
execution:
  agentic:
    prompt: Fix the bug
`
	task, err := LoadTask([]byte(yamlContent))
	require.NoError(t, err)

	assert.Nil(t, task.Transformation)
	assert.Empty(t, task.Targets)
	assert.Len(t, task.Repositories, 1)
	assert.Equal(t, "my-repo", task.Repositories[0].Name)

	// Helper method checks
	assert.False(t, task.UsesTransformationRepo())
	effectiveRepos := task.GetEffectiveRepositories()
	assert.Len(t, effectiveRepos, 1)
	assert.Equal(t, "my-repo", effectiveRepos[0].Name)
}
