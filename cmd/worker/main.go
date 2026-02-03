// Package main is the worker entry point.
package main

import (
	"flag"
	"log"
	"os"

	temporalactivity "go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/tinkerloft/fleetlift/internal/activity"
	internalclient "github.com/tinkerloft/fleetlift/internal/client"
	"github.com/tinkerloft/fleetlift/internal/sandbox"
	"github.com/tinkerloft/fleetlift/internal/sandbox/docker"
	"github.com/tinkerloft/fleetlift/internal/sandbox/kubernetes"
	"github.com/tinkerloft/fleetlift/internal/workflow"
)

func main() {
	// Parse command-line flags
	debugNoCleanup := flag.Bool("debug-no-cleanup", false, "Skip container cleanup on failure (for debugging)")
	flag.Parse()

	// Set environment variable for activities to check
	if *debugNoCleanup {
		if err := os.Setenv("DEBUG_NO_CLEANUP", "true"); err != nil {
			log.Printf("Warning: failed to set DEBUG_NO_CLEANUP: %v", err)
		}
		log.Println("DEBUG MODE: Container cleanup disabled - containers will persist after workflow completion")
	}
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

	// Create sandbox provider using factory
	factory := sandbox.NewProviderFactory(
		func() (sandbox.Provider, error) { return docker.NewProvider() },
		func() (sandbox.Provider, error) { return kubernetes.NewProvider() },
	)

	providerType := sandbox.DetectProviderType()
	sandboxProvider, err := factory.CreateWithType(providerType)
	if err != nil {
		log.Fatalf("Failed to create sandbox provider: %v", err)
	}
	log.Printf("Using sandbox provider: %s", sandboxProvider.Name())

	// Create activities
	sandboxActivities := activity.NewSandboxActivities(sandboxProvider)
	claudeActivities := activity.NewClaudeCodeActivities(sandboxProvider)
	deterministicActivities := activity.NewDeterministicActivities(sandboxProvider)
	githubActivities := activity.NewGitHubActivities(sandboxProvider)
	slackActivities := activity.NewSlackActivities()
	reportActivities := activity.NewReportActivities(sandboxProvider)
	steeringActivities := activity.NewSteeringActivities(sandboxProvider)

	// Create worker
	w := worker.New(c, internalclient.TaskQueue, worker.Options{})

	// Register workflows
	w.RegisterWorkflow(workflow.Transform)
	w.RegisterWorkflow(workflow.TransformGroup)

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
	w.RegisterActivityWithOptions(steeringActivities.GetDiff, temporalactivity.RegisterOptions{Name: activity.ActivityGetDiff})
	w.RegisterActivityWithOptions(steeringActivities.GetVerifierOutput, temporalactivity.RegisterOptions{Name: activity.ActivityGetVerifierOutput})

	log.Println("Worker started. Press Ctrl+C to stop.")

	// Run worker - Temporal's InterruptCh handles graceful shutdown
	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatalf("Worker failed: %v", err)
	}

	log.Println("Worker stopped")
}
