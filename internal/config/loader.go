// Package config provides configuration loading utilities.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"

	"github.com/andreweacott/agent-orchestrator/internal/model"
)

// validTargetNameRegex validates forEach target names.
// Target names must contain only alphanumeric characters, underscores, and hyphens.
var validTargetNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// SupportedVersions lists all schema versions supported by this loader.
var SupportedVersions = []int{1}

// versionHeader is used to extract just the version from YAML.
type versionHeader struct {
	Version *int `yaml:"version"`
}

// LoadTask loads a Task from YAML data with schema version validation.
func LoadTask(data []byte) (*model.Task, error) {
	// 1. Parse version field first
	var header versionHeader
	if err := yaml.Unmarshal(data, &header); err != nil {
		return nil, fmt.Errorf("failed to parse task: %w", err)
	}

	// 2. Validate version is present
	if header.Version == nil {
		return nil, errors.New("version field is required")
	}

	// 3. Route to version-specific loader
	switch *header.Version {
	case 1:
		return loadTaskV1(data)
	default:
		return nil, fmt.Errorf("unsupported schema version: %d (supported: %v)", *header.Version, SupportedVersions)
	}
}

// LoadTaskFile loads a Task from a YAML file path.
func LoadTaskFile(path string) (*model.Task, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	return LoadTask(data)
}

// taskV1 is the internal representation for schema version 1.
type taskV1 struct {
	Version         int              `yaml:"version"`
	ID              string           `yaml:"id"`
	Title           string           `yaml:"title"`
	Description     string           `yaml:"description,omitempty"`
	Mode            string           `yaml:"mode,omitempty"`
	Repositories    []repositoryV1   `yaml:"repositories"`
	ForEach         []forEachV1      `yaml:"for_each,omitempty"`
	Execution       executionV1      `yaml:"execution"`
	TicketURL       string           `yaml:"ticket_url,omitempty"`
	SlackChannel    string           `yaml:"slack_channel,omitempty"`
	Requester       string           `yaml:"requester,omitempty"`
	Timeout         string           `yaml:"timeout,omitempty"`
	RequireApproval *bool            `yaml:"require_approval,omitempty"`
	Parallel        bool             `yaml:"parallel,omitempty"`
	PullRequest     *pullRequestV1   `yaml:"pull_request,omitempty"`
	Sandbox         *sandboxV1       `yaml:"sandbox,omitempty"`
	Credentials     *credentialsV1   `yaml:"credentials,omitempty"`
}

type forEachV1 struct {
	Name    string `yaml:"name"`
	Context string `yaml:"context"`
}

type sandboxV1 struct {
	Namespace    string            `yaml:"namespace,omitempty"`
	RuntimeClass string            `yaml:"runtime_class,omitempty"`
	NodeSelector map[string]string `yaml:"node_selector,omitempty"`
	Resources    *resourcesV1      `yaml:"resources,omitempty"`
}

type resourcesV1 struct {
	Limits   resourceSpecV1 `yaml:"limits,omitempty"`
	Requests resourceSpecV1 `yaml:"requests,omitempty"`
}

type resourceSpecV1 struct {
	Memory string `yaml:"memory,omitempty"`
	CPU    string `yaml:"cpu,omitempty"`
}

type credentialsV1 struct {
	GitHub    *secretRefV1 `yaml:"github,omitempty"`
	Anthropic *secretRefV1 `yaml:"anthropic,omitempty"`
}

type secretRefV1 struct {
	SecretRef secretRefSpecV1 `yaml:"secret_ref"`
}

type secretRefSpecV1 struct {
	Name string `yaml:"name"`
	Key  string `yaml:"key"`
}

type repositoryV1 struct {
	URL    string   `yaml:"url"`
	Branch string   `yaml:"branch,omitempty"`
	Name   string   `yaml:"name,omitempty"`
	Setup  []string `yaml:"setup,omitempty"`
}

type executionV1 struct {
	Agentic       *agenticV1       `yaml:"agentic,omitempty"`
	Deterministic *deterministicV1 `yaml:"deterministic,omitempty"`
}

type agenticV1 struct {
	Prompt    string       `yaml:"prompt"`
	Verifiers []verifierV1 `yaml:"verifiers,omitempty"`
	Limits    *limitsV1    `yaml:"limits,omitempty"`
	Output    *outputV1    `yaml:"output,omitempty"`
}

type outputV1 struct {
	Schema yaml.Node `yaml:"schema,omitempty"`
}

type deterministicV1 struct {
	Image     string            `yaml:"image"`
	Command   []string          `yaml:"command,omitempty"`
	Args      []string          `yaml:"args,omitempty"`
	Env       map[string]string `yaml:"env,omitempty"`
	Verifiers []verifierV1      `yaml:"verifiers,omitempty"`
}

type verifierV1 struct {
	Name    string   `yaml:"name"`
	Command []string `yaml:"command"`
}

type limitsV1 struct {
	// New field names (per design doc)
	MaxIterations      int `yaml:"max_iterations,omitempty"`
	MaxTokens          int `yaml:"max_tokens,omitempty"`
	MaxVerifierRetries int `yaml:"max_verifier_retries,omitempty"`

	// Deprecated field names (for backward compatibility)
	MaxTurns     int `yaml:"max_turns,omitempty"`
	MaxFileReads int `yaml:"max_file_reads,omitempty"`
}

type pullRequestV1 struct {
	BranchPrefix string   `yaml:"branch_prefix,omitempty"`
	Title        string   `yaml:"title,omitempty"`
	Body         string   `yaml:"body,omitempty"`
	Labels       []string `yaml:"labels,omitempty"`
	Reviewers    []string `yaml:"reviewers,omitempty"`
}

// loadTaskV1 loads a version 1 task from YAML data.
func loadTaskV1(data []byte) (*model.Task, error) {
	var tv1 taskV1
	if err := yaml.Unmarshal(data, &tv1); err != nil {
		return nil, fmt.Errorf("failed to parse task v1: %w", err)
	}

	// Validate required fields
	if tv1.ID == "" {
		return nil, errors.New("id field is required")
	}
	if tv1.Title == "" {
		return nil, errors.New("title field is required")
	}
	if len(tv1.Repositories) == 0 {
		return nil, errors.New("at least one repository is required")
	}
	if tv1.Execution.Agentic == nil && tv1.Execution.Deterministic == nil {
		return nil, errors.New("execution must specify either agentic or deterministic")
	}
	if tv1.Execution.Agentic != nil && tv1.Execution.Deterministic != nil {
		return nil, errors.New("execution cannot specify both agentic and deterministic")
	}

	// Validate agentic execution
	if tv1.Execution.Agentic != nil && tv1.Execution.Agentic.Prompt == "" {
		return nil, errors.New("agentic execution requires prompt")
	}

	// Validate deterministic execution
	if tv1.Execution.Deterministic != nil && tv1.Execution.Deterministic.Image == "" {
		return nil, errors.New("deterministic execution requires image")
	}

	// Convert to model.Task
	task := &model.Task{
		Version:     tv1.Version,
		ID:          tv1.ID,
		Title:       tv1.Title,
		Description: tv1.Description,
		Mode:        model.TaskMode(tv1.Mode),
		Timeout:     tv1.Timeout,
		Parallel:    tv1.Parallel,
	}

	// Default require_approval to true if not specified
	if tv1.RequireApproval != nil {
		task.RequireApproval = *tv1.RequireApproval
	} else {
		task.RequireApproval = true
	}

	// Convert repositories
	for _, r := range tv1.Repositories {
		repo := model.Repository{
			URL:    r.URL,
			Branch: r.Branch,
			Name:   r.Name,
			Setup:  r.Setup,
		}
		if repo.Branch == "" {
			repo.Branch = "main"
		}
		if repo.Name == "" {
			repo.Name = model.NewRepository(r.URL, repo.Branch, "").Name
		}
		task.Repositories = append(task.Repositories, repo)
	}

	// Convert and validate ForEach
	for _, fe := range tv1.ForEach {
		// Validate target name contains only safe characters for filenames
		if fe.Name == "" {
			return nil, errors.New("for_each target name is required")
		}
		if !validTargetNameRegex.MatchString(fe.Name) {
			return nil, fmt.Errorf("for_each target name '%s' contains invalid characters (allowed: a-zA-Z0-9_-)", fe.Name)
		}
		task.ForEach = append(task.ForEach, model.ForEachTarget{
			Name:    fe.Name,
			Context: fe.Context,
		})
	}

	// Validate for_each is only used with report mode
	if len(task.ForEach) > 0 && tv1.Mode != "" && tv1.Mode != "report" {
		return nil, errors.New("for_each can only be used with mode: report")
	}

	// Convert execution
	if tv1.Execution.Agentic != nil {
		task.Execution.Agentic = &model.AgenticExecution{
			Prompt: tv1.Execution.Agentic.Prompt,
		}
		for _, v := range tv1.Execution.Agentic.Verifiers {
			task.Execution.Agentic.Verifiers = append(task.Execution.Agentic.Verifiers,
				model.Verifier{Name: v.Name, Command: v.Command})
		}
		if tv1.Execution.Agentic.Limits != nil {
			limits := &model.AgentLimits{
				MaxTokens: tv1.Execution.Agentic.Limits.MaxTokens,
			}
			// Use new field names, fall back to deprecated names for backward compat
			if tv1.Execution.Agentic.Limits.MaxIterations > 0 {
				limits.MaxIterations = tv1.Execution.Agentic.Limits.MaxIterations
			} else if tv1.Execution.Agentic.Limits.MaxTurns > 0 {
				limits.MaxIterations = tv1.Execution.Agentic.Limits.MaxTurns
			}
			if tv1.Execution.Agentic.Limits.MaxVerifierRetries > 0 {
				limits.MaxVerifierRetries = tv1.Execution.Agentic.Limits.MaxVerifierRetries
			} else if tv1.Execution.Agentic.Limits.MaxFileReads > 0 {
				limits.MaxVerifierRetries = tv1.Execution.Agentic.Limits.MaxFileReads
			}
			task.Execution.Agentic.Limits = limits
		}
		// Convert output config for report mode
		if tv1.Execution.Agentic.Output != nil {
			schemaBytes, err := yaml.Marshal(&tv1.Execution.Agentic.Output.Schema)
			if err == nil && len(schemaBytes) > 0 {
				// Convert YAML to JSON for the schema
				var schemaData interface{}
				if err := yaml.Unmarshal(schemaBytes, &schemaData); err == nil {
					if jsonBytes, err := json.Marshal(schemaData); err == nil {
						task.Execution.Agentic.Output = &model.OutputConfig{
							Schema: jsonBytes,
						}
					}
				}
			}
		}
	}

	if tv1.Execution.Deterministic != nil {
		task.Execution.Deterministic = &model.DeterministicExecution{
			Image:   tv1.Execution.Deterministic.Image,
			Command: tv1.Execution.Deterministic.Command,
			Args:    tv1.Execution.Deterministic.Args,
			Env:     tv1.Execution.Deterministic.Env,
		}
		for _, v := range tv1.Execution.Deterministic.Verifiers {
			task.Execution.Deterministic.Verifiers = append(task.Execution.Deterministic.Verifiers,
				model.Verifier{Name: v.Name, Command: v.Command})
		}
	}

	// Convert optional fields
	if tv1.TicketURL != "" {
		task.TicketURL = &tv1.TicketURL
	}
	if tv1.SlackChannel != "" {
		task.SlackChannel = &tv1.SlackChannel
	}
	if tv1.Requester != "" {
		task.Requester = &tv1.Requester
	}

	// Convert PR config
	if tv1.PullRequest != nil {
		task.PullRequest = &model.PullRequestConfig{
			BranchPrefix: tv1.PullRequest.BranchPrefix,
			Title:        tv1.PullRequest.Title,
			Body:         tv1.PullRequest.Body,
			Labels:       tv1.PullRequest.Labels,
			Reviewers:    tv1.PullRequest.Reviewers,
		}
	}

	// Convert sandbox config
	if tv1.Sandbox != nil {
		task.Sandbox = &model.SandboxConfig{
			Namespace:    tv1.Sandbox.Namespace,
			RuntimeClass: tv1.Sandbox.RuntimeClass,
			NodeSelector: tv1.Sandbox.NodeSelector,
		}
		if tv1.Sandbox.Resources != nil {
			task.Sandbox.Resources = &model.ResourceConfig{
				Limits: model.ResourceSpec{
					Memory: tv1.Sandbox.Resources.Limits.Memory,
					CPU:    tv1.Sandbox.Resources.Limits.CPU,
				},
				Requests: model.ResourceSpec{
					Memory: tv1.Sandbox.Resources.Requests.Memory,
					CPU:    tv1.Sandbox.Resources.Requests.CPU,
				},
			}
		}
	}

	// Convert credentials config
	if tv1.Credentials != nil {
		task.Credentials = &model.CredentialsConfig{}
		if tv1.Credentials.GitHub != nil {
			task.Credentials.GitHub = &model.SecretRef{
				SecretRefSpec: model.SecretRefSpec{
					Name: tv1.Credentials.GitHub.SecretRef.Name,
					Key:  tv1.Credentials.GitHub.SecretRef.Key,
				},
			}
		}
		if tv1.Credentials.Anthropic != nil {
			task.Credentials.Anthropic = &model.SecretRef{
				SecretRefSpec: model.SecretRefSpec{
					Name: tv1.Credentials.Anthropic.SecretRef.Name,
					Key:  tv1.Credentials.Anthropic.SecretRef.Key,
				},
			}
		}
	}

	return task, nil
}
