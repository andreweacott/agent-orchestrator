# Claude Code + Temporal Orchestration Prototype

## Executive Summary

Build a prototype system that orchestrates Claude Code agents running in sandboxed containers, coordinated by Temporal workflows. This enables autonomous bug fixing across multiple repositories with human-in-the-loop approval.

**Implementation Language:** Go
**Target Timeline:** 2-3 weeks for working prototype

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              PHASE 1: LOCAL                                  │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   Slack/CLI Trigger                                                         │
│         │                                                                   │
│         ▼                                                                   │
│   ┌─────────────────────────────────────────────────────────────────────┐  │
│   │              Temporal Workflow (Go SDK)                             │  │
│   │                                                                      │  │
│   │   BugFixWorkflow                                                     │  │
│   │   ├── Activity: ProvisionSandbox()                                  │  │
│   │   ├── Activity: CloneRepositories()                                 │  │
│   │   ├── Activity: RunClaudeCode()                                     │  │
│   │   ├── Signal: wait_for_human_approval()  ◄── HITL                   │  │
│   │   ├── Activity: CreatePullRequest()                                 │  │
│   │   └── Activity: CleanupSandbox()                                    │  │
│   │                                                                      │  │
│   └─────────────────────────────────────────────────────────────────────┘  │
│                              │                                              │
│                              ▼                                              │
│   ┌─────────────────────────────────────────────────────────────────────┐  │
│   │                    Docker Container (Sandbox)                        │  │
│   │                                                                      │  │
│   │   /workspace/                                                        │  │
│   │   ├── repo-a/                                                        │  │
│   │   ├── repo-b/                                                        │  │
│   │   └── AGENTS.md                                                      │  │
│   │                                                                      │  │
│   │   Claude Code CLI                                                    │  │
│   │   └── claude -p "..." --dangerously-skip-permissions                │  │
│   │                                                                      │  │
│   └─────────────────────────────────────────────────────────────────────┘  │
│                                                                             │
│   Local Services:                                                           │
│   ├── Temporal Dev Server (localhost:7233)                                 │
│   ├── Temporal Web UI (localhost:8233)                                     │
│   └── Docker Engine                                                         │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Phase 1: Local Development Setup (Days 1-2)

### 1.1 Prerequisites

```bash
# macOS
brew install temporal
brew install go
brew install --cask docker

# Verify installations
temporal --version
go version        # Need 1.22+
docker --version
```

### 1.2 Project Structure

```
claude-code-orchestrator-go/
├── go.mod
├── go.sum
├── Makefile
├── README.md
├── docker/
│   └── Dockerfile.sandbox
├── cmd/
│   ├── worker/
│   │   └── main.go
│   └── cli/
│       └── main.go
├── internal/
│   ├── model/
│   │   ├── task.go
│   │   └── task_test.go
│   ├── workflow/
│   │   ├── bugfix.go
│   │   └── bugfix_test.go
│   ├── activity/
│   │   ├── sandbox.go
│   │   ├── claudecode.go
│   │   ├── github.go
│   │   └── slack.go
│   ├── docker/
│   │   └── client.go
│   └── client/
│       └── starter.go
└── scripts/
    └── integration_test.sh
```

### 1.3 Go Module Setup

```bash
# Create project
mkdir claude-code-orchestrator-go && cd claude-code-orchestrator-go

# Initialize Go module
go mod init github.com/your-org/claude-code-orchestrator

# Install dependencies
go get go.temporal.io/sdk@latest
go get github.com/docker/docker@latest
go get github.com/google/go-github/v62@latest
go get github.com/slack-go/slack@latest
go get github.com/spf13/cobra@latest
go get github.com/stretchr/testify@latest
```

**go.mod:**

```go
module github.com/your-org/claude-code-orchestrator

go 1.22

require (
    go.temporal.io/sdk v1.29.1
    github.com/docker/docker v25.0.6+incompatible
    github.com/google/go-github/v62 v62.0.0
    github.com/slack-go/slack v0.12.5
    github.com/spf13/cobra v1.8.1
    github.com/stretchr/testify v1.9.0
    golang.org/x/oauth2 v0.21.0
)
```

### 1.4 Start Local Temporal Server

```bash
# Terminal 1: Start Temporal dev server
temporal server start-dev \
  --ui-port 8233 \
  --db-filename temporal.db

# Access Web UI at http://localhost:8233
```

---

## Phase 2: Sandbox Container (Days 2-3)

### 2.1 Dockerfile

**docker/Dockerfile.sandbox:**

```dockerfile
FROM ubuntu:24.04

ARG DEBIAN_FRONTEND=noninteractive

# Install base tools
RUN apt-get update && apt-get install -y \
    git curl wget sudo ca-certificates gnupg \
    ripgrep fd-find jq tree htop \
    build-essential \
    && rm -rf /var/lib/apt/lists/*

# Install Node.js 20 LTS
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash - \
    && apt-get install -y nodejs \
    && rm -rf /var/lib/apt/lists/*

# Install Python 3.12 (default on Ubuntu 24.04)
RUN apt-get update && apt-get install -y \
    python3 python3-venv python3-pip \
    && rm -rf /var/lib/apt/lists/*

# Install Java 21 (optional)
RUN apt-get update && apt-get install -y openjdk-21-jdk maven \
    && rm -rf /var/lib/apt/lists/*

# Install Claude Code CLI
RUN npm install -g @anthropic-ai/claude-code

# Create non-root user for security
RUN useradd -m -s /bin/bash agent \
    && echo "agent ALL=(ALL) NOPASSWD:ALL" >> /etc/sudoers

# Create workspace directory
RUN mkdir -p /workspace /output \
    && chown -R agent:agent /workspace /output

# Switch to non-root user
USER agent
WORKDIR /workspace

# Environment variables
ENV HOME=/home/agent
ENV ANTHROPIC_API_KEY=""
ENV GITHUB_TOKEN=""

# Default command
CMD ["tail", "-f", "/dev/null"]
```

### 2.2 Build and Test Container

```bash
# Build the sandbox image
docker build -t claude-code-sandbox:latest -f docker/Dockerfile.sandbox docker/

# Test interactively
docker run -it --rm \
  -e ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY" \
  claude-code-sandbox:latest \
  bash

# Inside container, verify Claude Code works
claude --version
claude -p "Say hello" --print
```

---

## Phase 3: Data Models (Day 3)

### 3.1 Task Models

**internal/model/task.go:**

```go
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
    URL    string `json:"url"`
    Branch string `json:"branch"`
    Name   string `json:"name"`
}

// NewRepository creates a Repository with auto-derived name if not provided.
func NewRepository(url, branch, name string) Repository {
    if branch == "" {
        branch = "main"
    }
    if name == "" {
        name = extractRepoName(url)
    }
    return Repository{URL: url, Branch: branch, Name: name}
}

func extractRepoName(url string) string {
    url = strings.TrimSuffix(url, "/")
    parts := strings.Split(url, "/")
    if len(parts) == 0 {
        return ""
    }
    name := parts[len(parts)-1]
    return strings.TrimSuffix(name, ".git")
}

// BugFixTask is the input for the BugFixWorkflow.
type BugFixTask struct {
    TaskID          string       `json:"task_id"`
    Title           string       `json:"title"`
    Description     string       `json:"description"`
    Repositories    []Repository `json:"repositories"`
    TicketURL       *string      `json:"ticket_url,omitempty"`
    SlackChannel    *string      `json:"slack_channel,omitempty"`
    Requester       *string      `json:"requester,omitempty"`
    TimeoutMinutes  int          `json:"timeout_minutes"`
    RequireApproval bool         `json:"require_approval"`
    AutoMerge       bool         `json:"auto_merge"`
}

// SandboxInfo contains information about a provisioned sandbox.
type SandboxInfo struct {
    ContainerID   string    `json:"container_id"`
    WorkspacePath string    `json:"workspace_path"`
    CreatedAt     time.Time `json:"created_at"`
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
```

---

## Phase 4: Activities (Days 4-6)

### 4.1 Sandbox Management Activity

**internal/activity/sandbox.go:**

```go
package activity

import (
    "context"
    "fmt"
    "os"

    "github.com/docker/docker/api/types/container"
    "go.temporal.io/sdk/activity"

    "github.com/your-org/claude-code-orchestrator/internal/docker"
    "github.com/your-org/claude-code-orchestrator/internal/model"
)

type SandboxActivities struct {
    DockerClient *docker.Client
}

func (a *SandboxActivities) ProvisionSandbox(ctx context.Context, taskID string) (*model.SandboxInfo, error) {
    logger := activity.GetLogger(ctx)
    logger.Info("Provisioning sandbox", "taskID", taskID)

    sandboxImage := getEnvOrDefault("SANDBOX_IMAGE", "claude-code-sandbox:latest")

    containerConfig := &container.Config{
        Image:     sandboxImage,
        Tty:       true,
        OpenStdin: true,
        Cmd:       []string{"tail", "-f", "/dev/null"},
        Env: []string{
            fmt.Sprintf("ANTHROPIC_API_KEY=%s", os.Getenv("ANTHROPIC_API_KEY")),
            fmt.Sprintf("GITHUB_TOKEN=%s", os.Getenv("GITHUB_TOKEN")),
            fmt.Sprintf("TASK_ID=%s", taskID),
        },
    }

    hostConfig := &container.HostConfig{
        Resources: container.Resources{
            Memory:    4 * 1024 * 1024 * 1024, // 4GB
            CPUPeriod: 100000,
            CPUQuota:  200000, // 2 CPUs
        },
        SecurityOpt: []string{"no-new-privileges:true"},
    }

    containerID, err := a.DockerClient.CreateAndStartContainer(ctx, containerConfig, hostConfig,
        fmt.Sprintf("claude-sandbox-%s", taskID))
    if err != nil {
        return nil, fmt.Errorf("failed to provision sandbox: %w", err)
    }

    return &model.SandboxInfo{
        ContainerID:   containerID,
        WorkspacePath: "/workspace",
    }, nil
}

func (a *SandboxActivities) CloneRepositories(ctx context.Context, sandbox model.SandboxInfo,
    repos []model.Repository, agentsMD string) ([]string, error) {

    logger := activity.GetLogger(ctx)
    var clonedPaths []string

    for _, repo := range repos {
        logger.Info("Cloning repository", "url", repo.URL, "name", repo.Name)

        cmd := fmt.Sprintf("git clone --depth 1 --branch %s %s /workspace/%s",
            repo.Branch, repo.URL, repo.Name)
        result, err := a.DockerClient.ExecShellCommand(ctx, sandbox.ContainerID, cmd, "agent")
        if err != nil || result.ExitCode != 0 {
            return nil, fmt.Errorf("failed to clone %s: %s", repo.URL, result.Stderr)
        }

        clonedPaths = append(clonedPaths, fmt.Sprintf("/workspace/%s", repo.Name))
        activity.RecordHeartbeat(ctx, fmt.Sprintf("Cloned %s", repo.Name))
    }

    return clonedPaths, nil
}

func (a *SandboxActivities) CleanupSandbox(ctx context.Context, containerID string) error {
    return a.DockerClient.StopAndRemoveContainer(ctx, containerID, 10)
}
```

### 4.2 Claude Code Execution Activity

**internal/activity/claudecode.go:**

```go
package activity

import (
    "context"
    "fmt"
    "regexp"
    "strings"

    "go.temporal.io/sdk/activity"

    "github.com/your-org/claude-code-orchestrator/internal/docker"
    "github.com/your-org/claude-code-orchestrator/internal/model"
)

type ClaudeCodeActivities struct {
    DockerClient *docker.Client
}

var clarificationPatterns = []*regexp.Regexp{
    regexp.MustCompile(`(?i)need more information`),
    regexp.MustCompile(`(?i)could you clarify`),
    regexp.MustCompile(`(?i)please specify`),
}

func (a *ClaudeCodeActivities) RunClaudeCode(ctx context.Context, containerID, prompt string,
    timeoutSeconds int) (*model.ClaudeCodeResult, error) {

    logger := activity.GetLogger(ctx)
    logger.Info("Starting Claude Code execution")

    escapedPrompt := strings.ReplaceAll(prompt, `"`, `\"`)
    escapedPrompt = strings.ReplaceAll(escapedPrompt, `$`, `\$`)

    command := fmt.Sprintf(`cd /workspace && claude -p "%s" --dangerously-skip-permissions --allowedTools Read,Write,Edit,Bash --output-format json 2>&1`,
        escapedPrompt)

    result, err := a.DockerClient.ExecShellCommand(ctx, containerID, command, "agent")
    if err != nil {
        errStr := err.Error()
        return &model.ClaudeCodeResult{Success: false, Error: &errStr}, nil
    }

    // Check for clarification needed
    needsClarification := false
    var clarificationQuestion *string
    for _, pattern := range clarificationPatterns {
        if pattern.MatchString(result.Stdout) {
            needsClarification = true
            clarificationQuestion = &result.Stdout
            break
        }
    }

    return &model.ClaudeCodeResult{
        Success:               result.ExitCode == 0,
        Output:                result.Stdout,
        NeedsClarification:    needsClarification,
        ClarificationQuestion: clarificationQuestion,
    }, nil
}
```

### 4.3 GitHub Operations Activity

**internal/activity/github.go:**

```go
package activity

import (
    "context"
    "fmt"
    "os"

    "github.com/google/go-github/v62/github"
    "golang.org/x/oauth2"
    "go.temporal.io/sdk/activity"

    "github.com/your-org/claude-code-orchestrator/internal/docker"
    "github.com/your-org/claude-code-orchestrator/internal/model"
)

type GitHubActivities struct {
    DockerClient *docker.Client
}

func (a *GitHubActivities) CreatePullRequest(ctx context.Context, containerID string,
    repo model.Repository, taskID, title, description string) (*model.PullRequest, error) {

    logger := activity.GetLogger(ctx)
    logger.Info("Creating PR", "repo", repo.Name)

    branchName := fmt.Sprintf("fix/claude-%s", taskID)
    githubToken := os.Getenv("GITHUB_TOKEN")

    // Git operations: checkout, add, commit, push
    // ... (implementation details)

    // Create PR via GitHub API
    ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: githubToken})
    tc := oauth2.NewClient(ctx, ts)
    client := github.NewClient(tc)

    owner, repoName := extractOwnerRepo(repo.URL)
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
```

---

## Phase 5: Workflow Definition (Days 6-8)

### 5.1 Bug Fix Workflow

**internal/workflow/bugfix.go:**

```go
package workflow

import (
    "fmt"
    "time"

    "go.temporal.io/sdk/temporal"
    "go.temporal.io/sdk/workflow"

    "github.com/your-org/claude-code-orchestrator/internal/model"
)

const (
    SignalApprove = "approve"
    SignalReject  = "reject"
    SignalCancel  = "cancel"
    QueryStatus   = "get_status"
    QueryResult   = "get_claude_result"
)

func BugFix(ctx workflow.Context, task model.BugFixTask) (*model.BugFixResult, error) {
    logger := workflow.GetLogger(ctx)
    startTime := workflow.Now(ctx)

    var (
        status       = model.TaskStatusPending
        sandbox      *model.SandboxInfo
        claudeResult *model.ClaudeCodeResult
        approved     *bool
        cancelled    bool
    )

    // Register query handlers
    workflow.SetQueryHandler(ctx, QueryStatus, func() (model.TaskStatus, error) {
        return status, nil
    })

    // Set up signal channels
    approveChannel := workflow.GetSignalChannel(ctx, SignalApprove)
    rejectChannel := workflow.GetSignalChannel(ctx, SignalReject)
    cancelChannel := workflow.GetSignalChannel(ctx, SignalCancel)

    // Handle signals asynchronously
    workflow.Go(ctx, func(ctx workflow.Context) {
        for {
            selector := workflow.NewSelector(ctx)
            selector.AddReceive(approveChannel, func(c workflow.ReceiveChannel, more bool) {
                c.Receive(ctx, nil)
                val := true
                approved = &val
            })
            selector.AddReceive(rejectChannel, func(c workflow.ReceiveChannel, more bool) {
                c.Receive(ctx, nil)
                val := false
                approved = &val
            })
            selector.AddReceive(cancelChannel, func(c workflow.ReceiveChannel, more bool) {
                c.Receive(ctx, nil)
                cancelled = true
            })
            selector.Select(ctx)
        }
    })

    // Retry policy
    retryPolicy := &temporal.RetryPolicy{
        InitialInterval:    time.Second,
        MaximumInterval:    time.Minute,
        MaximumAttempts:    3,
    }

    activityOptions := workflow.ActivityOptions{
        StartToCloseTimeout: 5 * time.Minute,
        RetryPolicy:         retryPolicy,
    }
    ctx = workflow.WithActivityOptions(ctx, activityOptions)

    // Ensure cleanup
    defer func() {
        if sandbox != nil {
            cleanupCtx, _ := workflow.NewDisconnectedContext(ctx)
            cleanupCtx = workflow.WithActivityOptions(cleanupCtx, workflow.ActivityOptions{
                StartToCloseTimeout: 2 * time.Minute,
            })
            workflow.ExecuteActivity(cleanupCtx, "CleanupSandbox", sandbox.ContainerID).Get(cleanupCtx, nil)
        }
    }()

    // 1. Provision sandbox
    status = model.TaskStatusProvisioning
    if err := workflow.ExecuteActivity(ctx, "ProvisionSandbox", task.TaskID).Get(ctx, &sandbox); err != nil {
        return failedResult(task.TaskID, startTime, err.Error()), nil
    }

    // 2. Clone repositories
    status = model.TaskStatusCloning
    agentsMD := generateAgentsMD(task)
    var clonedPaths []string
    if err := workflow.ExecuteActivity(ctx, "CloneRepositories", *sandbox, task.Repositories, agentsMD).Get(ctx, &clonedPaths); err != nil {
        return failedResult(task.TaskID, startTime, err.Error()), nil
    }

    // 3. Run Claude Code
    status = model.TaskStatusRunning
    prompt := buildPrompt(task)
    if err := workflow.ExecuteActivity(ctx, "RunClaudeCode", sandbox.ContainerID, prompt, task.TimeoutMinutes*60).Get(ctx, &claudeResult); err != nil {
        return failedResult(task.TaskID, startTime, err.Error()), nil
    }

    // 4. Wait for approval if required
    if task.RequireApproval && claudeResult.Success {
        status = model.TaskStatusAwaitingApproval
        workflow.AwaitWithTimeout(ctx, 24*time.Hour, func() bool {
            return approved != nil || cancelled
        })
        if cancelled || (approved != nil && !*approved) {
            return cancelledResult(task.TaskID, startTime), nil
        }
    }

    // 5. Create pull requests
    status = model.TaskStatusCreatingPRs
    var pullRequests []model.PullRequest
    for _, repo := range task.Repositories {
        var pr *model.PullRequest
        if err := workflow.ExecuteActivity(ctx, "CreatePullRequest",
            sandbox.ContainerID, repo, task.TaskID,
            fmt.Sprintf("fix: %s", task.Title), task.Description).Get(ctx, &pr); err != nil {
            return failedResult(task.TaskID, startTime, err.Error()), nil
        }
        if pr != nil {
            pullRequests = append(pullRequests, *pr)
        }
    }

    status = model.TaskStatusCompleted
    duration := workflow.Now(ctx).Sub(startTime).Seconds()
    return &model.BugFixResult{
        TaskID:          task.TaskID,
        Status:          model.TaskStatusCompleted,
        PullRequests:    pullRequests,
        DurationSeconds: &duration,
    }, nil
}
```

---

## Phase 6: Worker and CLI (Days 8-9)

### 6.1 Worker

**cmd/worker/main.go:**

```go
package main

import (
    "log"
    "os"

    "go.temporal.io/sdk/client"
    "go.temporal.io/sdk/worker"

    "github.com/your-org/claude-code-orchestrator/internal/activity"
    "github.com/your-org/claude-code-orchestrator/internal/docker"
    "github.com/your-org/claude-code-orchestrator/internal/workflow"
)

const TaskQueue = "claude-code-tasks"

func main() {
    temporalAddr := os.Getenv("TEMPORAL_ADDRESS")
    if temporalAddr == "" {
        temporalAddr = "localhost:7233"
    }

    c, err := client.Dial(client.Options{HostPort: temporalAddr})
    if err != nil {
        log.Fatalf("Failed to connect to Temporal: %v", err)
    }
    defer c.Close()

    dockerClient, err := docker.NewClient()
    if err != nil {
        log.Fatalf("Failed to create Docker client: %v", err)
    }

    sandboxActivities := activity.NewSandboxActivities(dockerClient)
    claudeActivities := activity.NewClaudeCodeActivities(dockerClient)
    githubActivities := activity.NewGitHubActivities(dockerClient)
    slackActivities := activity.NewSlackActivities()

    w := worker.New(c, TaskQueue, worker.Options{})

    w.RegisterWorkflow(workflow.BugFix)
    w.RegisterActivity(sandboxActivities.ProvisionSandbox)
    w.RegisterActivity(sandboxActivities.CloneRepositories)
    w.RegisterActivity(sandboxActivities.CleanupSandbox)
    w.RegisterActivity(claudeActivities.RunClaudeCode)
    w.RegisterActivity(claudeActivities.GetClaudeOutput)
    w.RegisterActivity(githubActivities.CreatePullRequest)
    w.RegisterActivity(slackActivities.NotifySlack)

    log.Printf("Starting worker on task queue: %s", TaskQueue)
    if err := w.Run(worker.InterruptCh()); err != nil {
        log.Fatalf("Worker failed: %v", err)
    }
}
```

### 6.2 CLI

**cmd/cli/main.go:**

```go
package main

import (
    "context"
    "fmt"
    "os"
    "strings"

    "github.com/spf13/cobra"

    "github.com/your-org/claude-code-orchestrator/internal/client"
    "github.com/your-org/claude-code-orchestrator/internal/model"
)

var rootCmd = &cobra.Command{
    Use:   "claude-orchestrator",
    Short: "Claude Code Orchestrator CLI",
}

var startCmd = &cobra.Command{
    Use:   "start",
    Short: "Start a bug fix workflow",
    RunE: func(cmd *cobra.Command, args []string) error {
        taskID, _ := cmd.Flags().GetString("task-id")
        title, _ := cmd.Flags().GetString("title")
        repos, _ := cmd.Flags().GetString("repos")
        noApproval, _ := cmd.Flags().GetBool("no-approval")

        var repositories []model.Repository
        for _, url := range strings.Split(repos, ",") {
            repositories = append(repositories, model.NewRepository(strings.TrimSpace(url), "", ""))
        }

        task := model.BugFixTask{
            TaskID:          taskID,
            Title:           title,
            Repositories:    repositories,
            RequireApproval: !noApproval,
            TimeoutMinutes:  30,
        }

        workflowID, err := client.StartBugFix(context.Background(), task)
        if err != nil {
            return err
        }

        fmt.Printf("Workflow started: %s\n", workflowID)
        fmt.Printf("View at: http://localhost:8233/namespaces/default/workflows/%s\n", workflowID)
        return nil
    },
}

func init() {
    startCmd.Flags().String("task-id", "", "Unique task identifier (required)")
    startCmd.Flags().String("title", "", "Bug title (required)")
    startCmd.Flags().String("repos", "", "Comma-separated repository URLs (required)")
    startCmd.Flags().Bool("no-approval", false, "Skip approval step")
    startCmd.MarkFlagRequired("task-id")
    startCmd.MarkFlagRequired("title")
    startCmd.MarkFlagRequired("repos")

    rootCmd.AddCommand(startCmd)
    // Add approve, reject, status, list commands...
}

func main() {
    if err := rootCmd.Execute(); err != nil {
        os.Exit(1)
    }
}
```

---

## Phase 7: Testing (Days 9-10)

### 7.1 Workflow Tests

**internal/workflow/bugfix_test.go:**

```go
package workflow

import (
    "testing"
    "time"

    "github.com/stretchr/testify/mock"
    "github.com/stretchr/testify/suite"
    "go.temporal.io/sdk/testsuite"

    "github.com/your-org/claude-code-orchestrator/internal/model"
)

type BugFixWorkflowTestSuite struct {
    suite.Suite
    testsuite.WorkflowTestSuite
    env *testsuite.TestWorkflowEnvironment
}

func (s *BugFixWorkflowTestSuite) SetupTest() {
    s.env = s.NewTestWorkflowEnvironment()
}

func (s *BugFixWorkflowTestSuite) TestBugFixWorkflowSuccess() {
    s.env.OnActivity("ProvisionSandbox", mock.Anything, "test-123").
        Return(&model.SandboxInfo{ContainerID: "container-abc"}, nil)

    s.env.OnActivity("CloneRepositories", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
        Return([]string{"/workspace/test-repo"}, nil)

    s.env.OnActivity("RunClaudeCode", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
        Return(&model.ClaudeCodeResult{Success: true, Output: "Fixed"}, nil)

    s.env.OnActivity("CreatePullRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
        Return(&model.PullRequest{RepoName: "test-repo", PRNumber: 1}, nil)

    s.env.OnActivity("CleanupSandbox", mock.Anything, "container-abc").Return(nil)

    task := model.BugFixTask{
        TaskID:          "test-123",
        Title:           "Test bug fix",
        Repositories:    []model.Repository{{URL: "https://github.com/org/test-repo.git", Branch: "main", Name: "test-repo"}},
        RequireApproval: false,
        TimeoutMinutes:  30,
    }

    s.env.ExecuteWorkflow(BugFix, task)
    s.True(s.env.IsWorkflowCompleted())
    s.NoError(s.env.GetWorkflowError())

    var result model.BugFixResult
    s.NoError(s.env.GetWorkflowResult(&result))
    s.Equal(model.TaskStatusCompleted, result.Status)
}

func TestBugFixWorkflowTestSuite(t *testing.T) {
    suite.Run(t, new(BugFixWorkflowTestSuite))
}
```

---

## Quick Start Commands

```bash
# 1. Clone and setup
git clone <your-repo>
cd claude-code-orchestrator-go

# 2. Install dependencies
go mod download

# 3. Build binaries
make build

# 4. Build sandbox image
make sandbox-image

# 5. Start Temporal (Terminal 1)
temporal server start-dev --ui-port 8233

# 6. Start Worker (Terminal 2)
export ANTHROPIC_API_KEY="sk-ant-..."
export GITHUB_TOKEN="ghp_..."
./bin/worker

# 7. Start a task (Terminal 3)
./bin/cli start \
    --task-id "my-first-task" \
    --title "Fix null pointer bug" \
    --repos "https://github.com/your-org/your-repo.git"

# 8. View in Temporal UI
open http://localhost:8233

# 9. Approve changes
./bin/cli approve --workflow-id bug-fix-my-first-task
```

---

## Timeline Summary

| Phase | Days | Deliverable |
|-------|------|-------------|
| 1. Local Setup | 1-2 | Temporal + Go environment |
| 2. Sandbox Container | 2-3 | Docker image with Claude Code |
| 3. Data Models | 3 | Go structs for tasks |
| 4. Activities | 4-6 | Sandbox, Claude, GitHub activities |
| 5. Workflow | 6-8 | BugFixWorkflow with signals |
| 6. Worker/CLI | 8-9 | Worker and Cobra CLI |
| 7. Testing | 9-10 | Unit + workflow tests |
| 8. Production | 10-12 | Temporal Cloud migration |

**Total: ~2 weeks for working prototype**

---

## Next Steps After Prototype

1. **Slack Bot Integration**: Build a Slack bot that creates tasks from messages
2. **Web Dashboard**: React UI for monitoring and approving tasks
3. **Multi-Repo PRs**: Coordinate PRs across repos with dependencies
4. **Campaign Mode**: Batch multiple similar tasks
5. **Cost Tracking**: Monitor Anthropic API usage per task
6. **Observability**: Add OpenTelemetry tracing
