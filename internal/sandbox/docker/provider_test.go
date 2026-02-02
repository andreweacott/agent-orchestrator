package docker

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/artisanlabs/fleetlift/internal/sandbox"
)

func TestNewProvider(t *testing.T) {
	// This test requires Docker to be available
	provider, err := NewProvider()
	if err != nil {
		t.Skip("Docker not available, skipping test")
	}
	require.NotNil(t, provider)

	// Verify we can ping Docker
	_, err = provider.GetClient().Ping(context.Background())
	assert.NoError(t, err)
}

func TestExecResult(t *testing.T) {
	result := &sandbox.ExecResult{
		ExitCode: 0,
		Stdout:   "hello world",
		Stderr:   "",
	}

	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "hello world", result.Stdout)
	assert.Empty(t, result.Stderr)
	assert.True(t, result.IsSuccess())
}

// Integration tests that require Docker

func TestProvision(t *testing.T) {
	provider, err := NewProvider()
	if err != nil {
		t.Skip("Docker not available, skipping test")
	}

	ctx := context.Background()

	// Pull a small test image
	err = provider.PullImageIfNeeded(ctx, "alpine:latest")
	if err != nil {
		t.Skip("Cannot pull alpine image, skipping test")
	}

	// Provision sandbox
	sb, err := provider.Provision(ctx, sandbox.ProvisionOptions{
		TaskID:     "test-" + t.Name(),
		Image:      "alpine:latest",
		WorkingDir: "/workspace",
		Resources: sandbox.ResourceLimits{
			MemoryBytes: 512 * 1024 * 1024,
			CPUQuota:    100000,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, sb)
	require.NotEmpty(t, sb.ID)

	// Cleanup
	defer func() {
		_ = provider.Cleanup(ctx, sb.ID)
	}()

	// Verify container is running
	running, err := provider.IsContainerRunning(ctx, sb.ID)
	require.NoError(t, err)
	assert.True(t, running)
}

func TestExec(t *testing.T) {
	provider, err := NewProvider()
	if err != nil {
		t.Skip("Docker not available, skipping test")
	}

	ctx := context.Background()

	// Pull and start container
	err = provider.PullImageIfNeeded(ctx, "alpine:latest")
	if err != nil {
		t.Skip("Cannot pull alpine image, skipping test")
	}

	// Create container directly for this test
	client := provider.GetClient()
	resp, err := client.ContainerCreate(ctx,
		&container.Config{
			Image: "alpine:latest",
			Cmd:   []string{"sleep", "30"},
		},
		&container.HostConfig{},
		nil, nil,
		"test-exec-"+t.Name(),
	)
	require.NoError(t, err)

	err = client.ContainerStart(ctx, resp.ID, container.StartOptions{})
	require.NoError(t, err)

	defer func() {
		_ = provider.Cleanup(ctx, resp.ID)
	}()

	// Execute command
	result, err := provider.Exec(ctx, resp.ID, sandbox.ExecCommand{
		Command: []string{"echo", "hello"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "hello")
}

func TestExecShell(t *testing.T) {
	provider, err := NewProvider()
	if err != nil {
		t.Skip("Docker not available, skipping test")
	}

	ctx := context.Background()

	// Pull and start container - use ubuntu which has bash
	err = provider.PullImageIfNeeded(ctx, "ubuntu:22.04")
	if err != nil {
		t.Skip("Cannot pull ubuntu image, skipping test")
	}

	// Create container directly
	client := provider.GetClient()
	resp, err := client.ContainerCreate(ctx,
		&container.Config{
			Image: "ubuntu:22.04",
			Cmd:   []string{"sleep", "30"},
		},
		&container.HostConfig{},
		nil, nil,
		"test-shell-"+t.Name(),
	)
	require.NoError(t, err)

	err = client.ContainerStart(ctx, resp.ID, container.StartOptions{})
	require.NoError(t, err)

	defer func() {
		_ = provider.Cleanup(ctx, resp.ID)
	}()

	// Execute shell command with pipe
	result, err := provider.ExecShell(ctx, resp.ID, "echo hello | tr 'h' 'H'", "")
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "Hello")
}

func TestIsContainerRunning_NotFound(t *testing.T) {
	provider, err := NewProvider()
	if err != nil {
		t.Skip("Docker not available, skipping test")
	}

	running, err := provider.IsContainerRunning(context.Background(), "nonexistent-container-id")
	require.NoError(t, err)
	assert.False(t, running)
}

func TestCleanup_NotFound(t *testing.T) {
	provider, err := NewProvider()
	if err != nil {
		t.Skip("Docker not available, skipping test")
	}

	// Should not error on non-existent container
	err = provider.Cleanup(context.Background(), "nonexistent-container-id")
	assert.NoError(t, err)
}
