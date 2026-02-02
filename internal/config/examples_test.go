package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/model"
)

func TestLoadExampleFiles(t *testing.T) {
	// Find examples directory (go test runs from the package directory)
	examplesDir := filepath.Join("..", "..", "examples")

	// Check if examples exist
	if _, err := os.Stat(examplesDir); os.IsNotExist(err) {
		t.Skip("Examples directory not found, skipping test")
	}

	tests := []struct {
		file          string
		expectedID    string
		expectedType  model.ExecutionType
		expectedTitle string
		expectedMode  model.TaskMode
	}{
		{
			file:          "task-agentic.yaml",
			expectedID:    "example-agentic",
			expectedType:  model.ExecutionTypeAgentic,
			expectedTitle: "Example agentic transform",
			expectedMode:  model.TaskModeTransform,
		},
		{
			file:          "task-deterministic.yaml",
			expectedID:    "example-deterministic",
			expectedType:  model.ExecutionTypeDeterministic,
			expectedTitle: "Upgrade Log4j 1.x to 2.x",
			expectedMode:  model.TaskModeTransform,
		},
		{
			file:          "task-report.yaml",
			expectedID:    "example-report",
			expectedType:  model.ExecutionTypeAgentic,
			expectedTitle: "Security audit",
			expectedMode:  model.TaskModeReport,
		},
	}

	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			path := filepath.Join(examplesDir, tc.file)

			task, err := LoadTaskFile(path)
			require.NoError(t, err, "Failed to load %s", tc.file)

			assert.Equal(t, tc.expectedID, task.ID)
			assert.Equal(t, tc.expectedTitle, task.Title)
			assert.Equal(t, tc.expectedType, task.Execution.GetExecutionType())
			assert.Equal(t, tc.expectedMode, task.GetMode())
			assert.Equal(t, 1, task.Version)
		})
	}
}
