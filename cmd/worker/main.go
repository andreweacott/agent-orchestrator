// Package main is the worker entry point.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/anthropics/claude-code-orchestrator/internal/activity"
	"github.com/anthropics/claude-code-orchestrator/internal/docker"
	"github.com/anthropics/claude-code-orchestrator/internal/workflow"
)

// TaskQueue is the task queue for bug fix workflows.
const TaskQueue = "claude-code-tasks"

func main() {
	// Get Temporal address
	temporalAddr := os.Getenv("TEMPORAL_ADDRESS")
	if temporalAddr == "" {
		temporalAddr = "localhost:7233"
	}

	// Connect to Temporal
	c, err := client.Dial(client.Options{
		HostPort: temporalAddr,
	})
	if err != nil {
		log.Fatalf("Failed to connect to Temporal: %v", err)
	}
	defer c.Close()

	log.Printf("Connected to Temporal at %s", temporalAddr)
	log.Printf("Task queue: %s", TaskQueue)

	// Create Docker client
	dockerClient, err := docker.NewClient()
	if err != nil {
		log.Fatalf("Failed to create Docker client: %v", err)
	}

	// Create activities
	sandboxActivities := activity.NewSandboxActivities(dockerClient)
	claudeActivities := activity.NewClaudeCodeActivities(dockerClient)
	githubActivities := activity.NewGitHubActivities(dockerClient)
	slackActivities := activity.NewSlackActivities()

	// Create worker
	w := worker.New(c, TaskQueue, worker.Options{})

	// Register workflow
	w.RegisterWorkflow(workflow.BugFix)

	// Register activities
	w.RegisterActivity(sandboxActivities.ProvisionSandbox)
	w.RegisterActivity(sandboxActivities.CloneRepositories)
	w.RegisterActivity(sandboxActivities.CleanupSandbox)
	w.RegisterActivity(claudeActivities.RunClaudeCode)
	w.RegisterActivity(claudeActivities.GetClaudeOutput)
	w.RegisterActivity(githubActivities.CreatePullRequest)
	w.RegisterActivity(slackActivities.NotifySlack)

	log.Println("Worker started. Press Ctrl+C to stop.")

	// Graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down...")
		cancel()
	}()

	// Run worker
	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatalf("Worker failed: %v", err)
	}

	<-ctx.Done()
}
