// Package main is the worker entry point.
package main

import (
	"log"
	"os"

	temporalactivity "go.temporal.io/sdk/activity"
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

	// Register activities with explicit names to match workflow constants
	w.RegisterActivityWithOptions(sandboxActivities.ProvisionSandbox, temporalactivity.RegisterOptions{Name: activity.ActivityProvisionSandbox})
	w.RegisterActivityWithOptions(sandboxActivities.CloneRepositories, temporalactivity.RegisterOptions{Name: activity.ActivityCloneRepositories})
	w.RegisterActivityWithOptions(sandboxActivities.CleanupSandbox, temporalactivity.RegisterOptions{Name: activity.ActivityCleanupSandbox})
	w.RegisterActivityWithOptions(sandboxActivities.RunVerifiers, temporalactivity.RegisterOptions{Name: activity.ActivityRunVerifiers})
	w.RegisterActivityWithOptions(claudeActivities.RunClaudeCode, temporalactivity.RegisterOptions{Name: activity.ActivityRunClaudeCode})
	w.RegisterActivityWithOptions(claudeActivities.GetClaudeOutput, temporalactivity.RegisterOptions{Name: activity.ActivityGetClaudeOutput})
	w.RegisterActivityWithOptions(deterministicActivities.ExecuteDeterministic, temporalactivity.RegisterOptions{Name: activity.ActivityExecuteDeterministic})
	w.RegisterActivityWithOptions(githubActivities.CreatePullRequest, temporalactivity.RegisterOptions{Name: activity.ActivityCreatePullRequest})
	w.RegisterActivityWithOptions(slackActivities.NotifySlack, temporalactivity.RegisterOptions{Name: activity.ActivityNotifySlack})
	w.RegisterActivityWithOptions(reportActivities.CollectReport, temporalactivity.RegisterOptions{Name: activity.ActivityCollectReport})
	w.RegisterActivityWithOptions(reportActivities.ValidateSchema, temporalactivity.RegisterOptions{Name: activity.ActivityValidateSchema})

	log.Println("Worker started. Press Ctrl+C to stop.")

	// Run worker - Temporal's InterruptCh handles graceful shutdown
	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatalf("Worker failed: %v", err)
	}

	log.Println("Worker stopped")
}
