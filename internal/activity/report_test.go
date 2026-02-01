package activity

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/andreweacott/agent-orchestrator/internal/model"
	"github.com/andreweacott/agent-orchestrator/internal/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

// mockProvider implements sandbox.Provider for testing.
type mockProvider struct {
	copyFromFunc func(ctx context.Context, id, path string) (io.ReadCloser, error)
}

func (m *mockProvider) Provision(_ context.Context, _ sandbox.ProvisionOptions) (*sandbox.Sandbox, error) {
	return nil, errors.New("not implemented")
}

func (m *mockProvider) Exec(_ context.Context, _ string, _ sandbox.ExecCommand) (*sandbox.ExecResult, error) {
	return nil, errors.New("not implemented")
}

func (m *mockProvider) ExecShell(_ context.Context, _, _, _ string) (*sandbox.ExecResult, error) {
	return nil, errors.New("not implemented")
}

func (m *mockProvider) CopyTo(_ context.Context, _ string, _ io.Reader, _ string) error {
	return errors.New("not implemented")
}

func (m *mockProvider) CopyFrom(ctx context.Context, id, srcPath string) (io.ReadCloser, error) {
	if m.copyFromFunc != nil {
		return m.copyFromFunc(ctx, id, srcPath)
	}
	return nil, errors.New("not implemented")
}

func (m *mockProvider) Status(_ context.Context, _ string) (*sandbox.SandboxStatus, error) {
	return nil, errors.New("not implemented")
}

func (m *mockProvider) Cleanup(_ context.Context, _ string) error {
	return errors.New("not implemented")
}

func (m *mockProvider) Name() string {
	return "mock"
}

// createTarArchive creates a tar archive containing a single file with the given content.
func createTarArchive(filename string, content []byte) io.ReadCloser {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	hdr := &tar.Header{
		Name: filename,
		Mode: 0644,
		Size: int64(len(content)),
	}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write(content)
	_ = tw.Close()

	return io.NopCloser(&buf)
}

func TestCollectReport(t *testing.T) {
	tests := []struct {
		name             string
		repoName         string
		copyFromFunc     func(ctx context.Context, id, path string) (io.ReadCloser, error)
		wantError        string
		wantFrontmatter  map[string]any
		wantBodyContains string
	}{
		{
			name:     "successful report collection with frontmatter",
			repoName: "test-repo",
			copyFromFunc: func(_ context.Context, _, path string) (io.ReadCloser, error) {
				// Verify the correct path is being requested
				if path != "/workspace/test-repo/REPORT.md" {
					return nil, errors.New("unexpected path: " + path)
				}
				content := []byte(`---
title: Test Report
status: complete
---

# Analysis

This is the report body.`)
				return createTarArchive("REPORT.md", content), nil
			},
			wantFrontmatter: map[string]any{
				"title":  "Test Report",
				"status": "complete",
			},
			wantBodyContains: "This is the report body",
		},
		{
			name:     "successful report collection without frontmatter",
			repoName: "my-service",
			copyFromFunc: func(_ context.Context, _, _ string) (io.ReadCloser, error) {
				content := []byte(`# Just a plain markdown report

No frontmatter here.`)
				return createTarArchive("REPORT.md", content), nil
			},
			wantFrontmatter:  nil,
			wantBodyContains: "Just a plain markdown report",
		},
		{
			name:     "file not found returns structured error",
			repoName: "missing-repo",
			copyFromFunc: func(_ context.Context, _, _ string) (io.ReadCloser, error) {
				return nil, errors.New("no such file or directory")
			},
			wantError: "failed to read REPORT.md",
		},
		{
			name:     "empty tar archive returns structured error",
			repoName: "empty-repo",
			copyFromFunc: func(_ context.Context, _, _ string) (io.ReadCloser, error) {
				// Return an empty tar archive (no files)
				var buf bytes.Buffer
				tw := tar.NewWriter(&buf)
				_ = tw.Close()
				return io.NopCloser(&buf), nil
			},
			wantError: "failed to read tar header",
		},
		{
			name:     "report with nested frontmatter data",
			repoName: "security-service",
			copyFromFunc: func(_ context.Context, _, _ string) (io.ReadCloser, error) {
				content := []byte(`---
metadata:
  author: Claude
  version: 1.0
issues:
  - severity: high
    location: main.go:42
  - severity: low
    location: util.go:10
---

# Security Audit Report`)
				return createTarArchive("REPORT.md", content), nil
			},
			wantFrontmatter: map[string]any{
				"metadata": map[string]any{
					"author":  "Claude",
					"version": 1.0,
				},
				"issues": []any{
					map[string]any{"severity": "high", "location": "main.go:42"},
					map[string]any{"severity": "low", "location": "util.go:10"},
				},
			},
			wantBodyContains: "Security Audit Report",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use Temporal test environment to get proper activity context
			testSuite := &testsuite.WorkflowTestSuite{}
			env := testSuite.NewTestActivityEnvironment()

			provider := &mockProvider{copyFromFunc: tt.copyFromFunc}
			activities := NewReportActivities(provider)
			env.RegisterActivity(activities.CollectReport)

			repoName := tt.repoName
			if repoName == "" {
				repoName = "default-repo"
			}

			input := CollectReportInput{
				ContainerID: "test-container",
				RepoName:    repoName,
			}

			result, err := env.ExecuteActivity(activities.CollectReport, input)
			require.NoError(t, err)

			var report *model.ReportOutput
			require.NoError(t, result.Get(&report))
			require.NotNil(t, report)

			if tt.wantError != "" {
				assert.Contains(t, report.Error, tt.wantError)
				return
			}

			assert.Empty(t, report.Error)
			assert.Equal(t, tt.wantFrontmatter, report.Frontmatter)
			if tt.wantBodyContains != "" {
				assert.Contains(t, report.Body, tt.wantBodyContains)
			}
		})
	}
}

func TestValidateSchema(t *testing.T) {
	activities := &ReportActivities{}

	tests := []struct {
		name            string
		frontmatter     map[string]any
		schema          string
		wantErrors      bool
		wantErrorCount  int
	}{
		{
			name: "valid frontmatter against schema",
			frontmatter: map[string]any{
				"title":  "Test Report",
				"status": "complete",
			},
			schema: `{
				"type": "object",
				"required": ["title", "status"],
				"properties": {
					"title": {"type": "string"},
					"status": {"type": "string", "enum": ["pending", "complete", "failed"]}
				}
			}`,
			wantErrors: false,
		},
		{
			name: "missing required field",
			frontmatter: map[string]any{
				"title": "Test Report",
			},
			schema: `{
				"type": "object",
				"required": ["title", "status"],
				"properties": {
					"title": {"type": "string"},
					"status": {"type": "string"}
				}
			}`,
			wantErrors:     true,
			wantErrorCount: 1,
		},
		{
			name: "wrong type",
			frontmatter: map[string]any{
				"title": 123,
			},
			schema: `{
				"type": "object",
				"properties": {
					"title": {"type": "string"}
				}
			}`,
			wantErrors:     true,
			wantErrorCount: 1,
		},
		{
			name: "invalid enum value",
			frontmatter: map[string]any{
				"status": "invalid",
			},
			schema: `{
				"type": "object",
				"properties": {
					"status": {"type": "string", "enum": ["pending", "complete"]}
				}
			}`,
			wantErrors:     true,
			wantErrorCount: 1,
		},
		{
			name:        "nil frontmatter",
			frontmatter: nil,
			schema: `{
				"type": "object",
				"required": ["title"]
			}`,
			wantErrors:     true,
			wantErrorCount: 1,
		},
		{
			name: "empty schema",
			frontmatter: map[string]any{
				"anything": "goes",
			},
			schema:     "",
			wantErrors: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := ValidateSchemaInput{
				Frontmatter: tt.frontmatter,
				Schema:      tt.schema,
			}

			errors, err := activities.ValidateSchema(context.Background(), input)
			require.NoError(t, err)

			if tt.wantErrors {
				assert.NotEmpty(t, errors)
				if tt.wantErrorCount > 0 {
					assert.GreaterOrEqual(t, len(errors), tt.wantErrorCount)
				}
			} else {
				assert.Empty(t, errors)
			}
		})
	}
}
