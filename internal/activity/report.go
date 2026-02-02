// Package activity contains Temporal activity implementations.
package activity

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/adrg/frontmatter"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"go.temporal.io/sdk/activity"
	"gopkg.in/yaml.v3"

	"github.com/tinkerloft/fleetlift/internal/model"
	"github.com/tinkerloft/fleetlift/internal/sandbox"
)

// targetNamePattern validates target names to prevent path traversal.
var targetNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ReportActivities contains activities for report mode operations.
type ReportActivities struct {
	Provider sandbox.Provider
}

// NewReportActivities creates a new ReportActivities instance.
func NewReportActivities(provider sandbox.Provider) *ReportActivities {
	return &ReportActivities{Provider: provider}
}

// CollectReportInput contains inputs for collecting a report.
type CollectReportInput struct {
	ContainerID         string
	RepoName            string
	TargetName          string // If set, reads REPORT-{TargetName}.md instead of REPORT.md
	UseTransformationLayout bool   // If true, looks in /workspace/targets/{repoName} instead of /workspace/{repoName}
}

// ValidateSchemaInput contains inputs for schema validation.
type ValidateSchemaInput struct {
	Frontmatter map[string]any
	Schema      string // JSON Schema as string
}

// CollectReport reads and parses the report file from the sandbox.
// The report is expected at /workspace/{repoName}/REPORT.md
// or /workspace/{repoName}/REPORT-{targetName}.md if TargetName is set.
func (a *ReportActivities) CollectReport(ctx context.Context, input CollectReportInput) (*model.ReportOutput, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Collecting report", "repo", input.RepoName, "target", input.TargetName)

	// Validate target name to prevent path traversal (defense in depth)
	if input.TargetName != "" && !targetNamePattern.MatchString(input.TargetName) {
		return &model.ReportOutput{
			Error: fmt.Sprintf("invalid target name '%s': must contain only alphanumeric, underscore, or hyphen", input.TargetName),
		}, nil
	}

	// Determine base path based on layout mode
	basePath := "/workspace"
	if input.UseTransformationLayout {
		basePath = "/workspace/targets"
	}

	// Report path is inside the repository directory
	var reportPath string
	if input.TargetName != "" {
		reportPath = fmt.Sprintf("%s/%s/REPORT-%s.md", basePath, input.RepoName, input.TargetName)
	} else {
		reportPath = fmt.Sprintf("%s/%s/REPORT.md", basePath, input.RepoName)
	}

	// Read the report file from the container
	reader, err := a.Provider.CopyFrom(ctx, input.ContainerID, reportPath)
	if err != nil {
		// File missing or read error - return structured error instead of failing activity
		// This allows the workflow to continue and aggregate partial results
		logger.Warn("Failed to read REPORT.md", "error", err)
		return &model.ReportOutput{
			Error: fmt.Sprintf("failed to read REPORT.md: %v (agent may not have created the file)", err),
		}, nil
	}
	defer func() {
		if cerr := reader.Close(); cerr != nil {
			logger.Warn("Failed to close reader", "error", cerr)
		}
	}()

	// Docker CopyFrom returns a tar archive - extract the file content
	tarReader := tar.NewReader(reader)
	_, err = tarReader.Next()
	if err != nil {
		logger.Warn("Failed to read tar header from REPORT.md", "error", err)
		return &model.ReportOutput{
			Error: fmt.Sprintf("failed to read tar header from REPORT.md: %v", err),
		}, nil
	}

	content, err := io.ReadAll(tarReader)
	if err != nil {
		logger.Warn("Failed to read REPORT.md content", "error", err)
		return &model.ReportOutput{
			Error: fmt.Sprintf("failed to read REPORT.md content: %v", err),
		}, nil
	}

	raw := string(content)

	// Parse the frontmatter using adrg/frontmatter library with yaml.v3
	// (yaml.v3 produces map[string]any natively, unlike yaml.v2's map[interface{}]interface{})
	var fm map[string]any
	yamlFormat := frontmatter.NewFormat("---", "---", yaml.Unmarshal)
	body, parseErr := frontmatter.Parse(bytes.NewReader(content), &fm, yamlFormat)
	if parseErr != nil {
		logger.Warn("Failed to parse frontmatter", "error", parseErr)
		return &model.ReportOutput{
			Raw:   raw,
			Error: parseErr.Error(),
		}, nil
	}

	logger.Info("Report collected successfully", "repo", input.RepoName, "hasFrontmatter", fm != nil)

	return &model.ReportOutput{
		Frontmatter: fm,
		Body:        strings.TrimSpace(string(body)),
		Raw:         raw,
	}, nil
}

// ValidateSchema validates frontmatter against a JSON Schema.
func (a *ReportActivities) ValidateSchema(_ context.Context, input ValidateSchemaInput) ([]string, error) {
	if input.Schema == "" {
		return nil, nil
	}

	if input.Frontmatter == nil {
		return []string{"frontmatter is required but was not provided"}, nil
	}

	// Compile the schema
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("schema.json", strings.NewReader(input.Schema)); err != nil {
		return nil, fmt.Errorf("failed to add schema resource: %w", err)
	}

	schema, err := compiler.Compile("schema.json")
	if err != nil {
		return nil, fmt.Errorf("failed to compile schema: %w", err)
	}

	// Validate the frontmatter
	if err := schema.Validate(input.Frontmatter); err != nil {
		validationErr, ok := err.(*jsonschema.ValidationError)
		if !ok {
			return []string{err.Error()}, nil
		}

		// Extract validation errors with field paths
		var errors []string
		extractValidationErrors(validationErr, &errors)
		return errors, nil
	}

	return nil, nil
}

// extractValidationErrors recursively extracts validation error messages.
func extractValidationErrors(err *jsonschema.ValidationError, errors *[]string) {
	if err.Message != "" {
		path := err.InstanceLocation
		if path == "" {
			path = "/"
		}
		*errors = append(*errors, fmt.Sprintf("%s: %s", path, err.Message))
	}
	for _, cause := range err.Causes {
		extractValidationErrors(cause, errors)
	}
}


