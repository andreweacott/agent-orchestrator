package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"

	fleetliftv1alpha1 "github.com/tinkerloft/fleetlift/api/v1alpha1"
)

func TestBuildEffectiveCommand_NoWorkingDir(t *testing.T) {
	req := &fleetliftv1alpha1.ExecRequest{
		ID:      "test-1",
		Command: []string{"git", "status"},
	}

	cmd := buildEffectiveCommand(req)

	assert.Equal(t, []string{"git", "status"}, cmd)
}

func TestBuildEffectiveCommand_WithWorkingDir(t *testing.T) {
	req := &fleetliftv1alpha1.ExecRequest{
		ID:         "test-1",
		Command:    []string{"git", "status"},
		WorkingDir: "/workspace/myrepo",
	}

	cmd := buildEffectiveCommand(req)

	assert.Equal(t, []string{"sh", "-c", "cd '/workspace/myrepo' && 'git' 'status'"}, cmd)
}

func TestBuildEffectiveCommand_WithSpecialCharsInWorkingDir(t *testing.T) {
	req := &fleetliftv1alpha1.ExecRequest{
		ID:         "test-1",
		Command:    []string{"ls", "-la"},
		WorkingDir: "/workspace/my project's dir",
	}

	cmd := buildEffectiveCommand(req)

	// Single quotes in path should be properly escaped
	assert.Equal(t, []string{"sh", "-c", "cd '/workspace/my project'\"'\"'s dir' && 'ls' '-la'"}, cmd)
}

func TestBuildEffectiveCommand_WithSpecialCharsInCommand(t *testing.T) {
	req := &fleetliftv1alpha1.ExecRequest{
		ID:         "test-1",
		Command:    []string{"echo", "hello world"},
		WorkingDir: "/workspace",
	}

	cmd := buildEffectiveCommand(req)

	assert.Equal(t, []string{"sh", "-c", "cd '/workspace' && 'echo' 'hello world'"}, cmd)
}

func TestBuildEffectiveCommand_SingleCommand(t *testing.T) {
	req := &fleetliftv1alpha1.ExecRequest{
		ID:         "test-1",
		Command:    []string{"pwd"},
		WorkingDir: "/workspace",
	}

	cmd := buildEffectiveCommand(req)

	assert.Equal(t, []string{"sh", "-c", "cd '/workspace' && 'pwd'"}, cmd)
}

func TestBuildEffectiveCommand_UserFieldIgnored(t *testing.T) {
	// User field is informational only since container runs as fixed non-root user
	req := &fleetliftv1alpha1.ExecRequest{
		ID:      "test-1",
		Command: []string{"whoami"},
		User:    "root",
	}

	cmd := buildEffectiveCommand(req)

	// User should be ignored, command unchanged
	assert.Equal(t, []string{"whoami"}, cmd)
}

func TestTruncateOutput_Empty(t *testing.T) {
	result := truncateOutput("")
	assert.Equal(t, "", result)
}

func TestTruncateOutput_UnderLimit(t *testing.T) {
	input := "hello world"
	result := truncateOutput(input)
	assert.Equal(t, "hello world", result)
}

func TestTruncateOutput_ExactLimit(t *testing.T) {
	// Create string exactly at the limit
	input := make([]byte, fleetliftv1alpha1.MaxExecOutputSize)
	for i := range input {
		input[i] = 'x'
	}

	result := truncateOutput(string(input))

	// Should not be truncated
	assert.Equal(t, string(input), result)
	assert.Len(t, result, fleetliftv1alpha1.MaxExecOutputSize)
}

func TestTruncateOutput_OverLimit(t *testing.T) {
	// Create string over the limit
	input := make([]byte, fleetliftv1alpha1.MaxExecOutputSize+100)
	for i := range input {
		input[i] = 'x'
	}

	result := truncateOutput(string(input))

	// Should be truncated with suffix
	assert.True(t, len(result) > fleetliftv1alpha1.MaxExecOutputSize)
	assert.True(t, len(result) < len(input))
	assert.Contains(t, result, "... [truncated]")
	// First part should be preserved
	assert.Equal(t, string(input[:fleetliftv1alpha1.MaxExecOutputSize]), result[:fleetliftv1alpha1.MaxExecOutputSize])
}

func TestTruncateOutput_OneByteTooMuch(t *testing.T) {
	// Create string exactly one byte over the limit
	input := make([]byte, fleetliftv1alpha1.MaxExecOutputSize+1)
	for i := range input {
		input[i] = 'y'
	}

	result := truncateOutput(string(input))

	assert.Contains(t, result, "... [truncated]")
}

func TestBuildEffectiveCommand_EmptyWorkingDir(t *testing.T) {
	req := &fleetliftv1alpha1.ExecRequest{
		ID:         "test-1",
		Command:    []string{"echo", "hello"},
		WorkingDir: "",
	}

	cmd := buildEffectiveCommand(req)

	// Empty working dir should return original command unchanged
	assert.Equal(t, []string{"echo", "hello"}, cmd)
}
