// Package main is the worker entry point.
package main

import (
	"log"
	"os"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/andreweacott/agent-orchestrator/internal/activity"
	internalclient "github.com/andreweacott/agent-orchestrator/internal/client"
	"github.com/andreweacott/agent-orchestrator/internal/sandbox/docker"
	"github.com/andreweacott/agent-orchestrator/internal/workflow"
)

func main() {
	// Validate configuration at startup
	configMode := activity.ConfigModeWarn
	if os.Getenv("REQUIRE_CONFIG") == "true" {
		configMode = activity.ConfigModeRequire
	}
	if err := activity.CheckConfig(configMode); err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

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
	log.Printf("Task queue: %s", internalclient.TaskQueue)

	// Create Docker provider
	dockerProvider, err := docker.NewProvider()
	if err != nil {
		log.Fatalf("Failed to create Docker provider: %v", err)
	}

	// Create activities
	sandboxActivities := activity.NewSandboxActivities(dockerProvider)
	claudeActivities := activity.NewClaudeCodeActivities(dockerProvider)
	deterministicActivities := activity.NewDeterministicActivities(dockerProvider)
	githubActivities := activity.NewGitHubActivities(dockerProvider)
	slackActivities := activity.NewSlackActivities()
	reportActivities := activity.NewReportActivities(dockerProvider)

	// Create worker
	w := worker.New(c, internalclient.TaskQueue, worker.Options{})

	// Register workflow
	w.RegisterWorkflow(workflow.Transform)

	// Register activities
	w.RegisterActivity(sandboxActivities.ProvisionSandbox)
	w.RegisterActivity(sandboxActivities.CloneRepositories)
	w.RegisterActivity(sandboxActivities.CleanupSandbox)
	w.RegisterActivity(sandboxActivities.RunVerifiers)
	w.RegisterActivity(claudeActivities.RunClaudeCode)
	w.RegisterActivity(claudeActivities.GetClaudeOutput)
	w.RegisterActivity(deterministicActivities.ExecuteDeterministic)
	w.RegisterActivity(githubActivities.CreatePullRequest)
	w.RegisterActivity(slackActivities.NotifySlack)
	w.RegisterActivity(reportActivities.CollectReport)
	w.RegisterActivity(reportActivities.ValidateSchema)

	log.Println("Worker started. Press Ctrl+C to stop.")

	// Run worker - Temporal's InterruptCh handles graceful shutdown
	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatalf("Worker failed: %v", err)
	}

	log.Println("Worker stopped")
}
