// Package docker provides Docker client utilities.
package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// Client wraps the Docker client with helper methods.
type Client struct {
	*client.Client
}

// NewClient creates a Docker client, auto-detecting the socket location on macOS.
func NewClient() (*Client, error) {
	var opts []client.Opt

	// Check if DOCKER_HOST is already set
	if dockerHost := os.Getenv("DOCKER_HOST"); dockerHost != "" {
		opts = append(opts, client.FromEnv, client.WithAPIVersionNegotiation())
	} else {
		// On macOS, Docker Desktop may use ~/.docker/run/docker.sock
		homeDir, err := os.UserHomeDir()
		if err == nil {
			macOSSocket := filepath.Join(homeDir, ".docker", "run", "docker.sock")
			if _, err := os.Stat(macOSSocket); err == nil {
				opts = append(opts, client.WithHost("unix://"+macOSSocket))
			}
		}
		opts = append(opts, client.WithAPIVersionNegotiation())
	}

	// If no specific host was set, fall back to defaults
	if len(opts) == 1 {
		opts = append([]client.Opt{client.FromEnv}, opts...)
	}

	c, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	return &Client{Client: c}, nil
}

// ExecResult contains the result of executing a command in a container.
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// ExecCommand executes a command in a container and returns the result.
func (c *Client) ExecCommand(ctx context.Context, containerID string, cmd []string, user string) (*ExecResult, error) {
	execConfig := types.ExecConfig{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
		User:         user,
	}

	execID, err := c.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create exec: %w", err)
	}

	resp, err := c.ContainerExecAttach(ctx, execID.ID, types.ExecStartCheck{})
	if err != nil {
		return nil, fmt.Errorf("failed to attach exec: %w", err)
	}
	defer resp.Close()

	// Read output
	var stdout, stderr bytes.Buffer
	_, err = stdcopy.StdCopy(&stdout, &stderr, resp.Reader)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to read exec output: %w", err)
	}

	// Get exit code
	inspect, err := c.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect exec: %w", err)
	}

	return &ExecResult{
		ExitCode: inspect.ExitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}, nil
}

// ExecShellCommand executes a shell command in a container.
func (c *Client) ExecShellCommand(ctx context.Context, containerID, command, user string) (*ExecResult, error) {
	return c.ExecCommand(ctx, containerID, []string{"bash", "-c", command}, user)
}

// IsContainerRunning checks if a container is running.
func (c *Client) IsContainerRunning(ctx context.Context, containerID string) (bool, error) {
	inspect, err := c.ContainerInspect(ctx, containerID)
	if err != nil {
		if client.IsErrNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return inspect.State.Running, nil
}

// StopAndRemoveContainer stops and removes a container.
func (c *Client) StopAndRemoveContainer(ctx context.Context, containerID string, timeoutSeconds int) error {
	// Try to stop the container
	timeout := timeoutSeconds
	if err := c.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout}); err != nil {
		// Ignore "not found" errors
		if !client.IsErrNotFound(err) {
			// Log but continue to removal
			fmt.Printf("Warning: failed to stop container %s: %v\n", containerID[:12], err)
		}
	}

	// Remove the container
	if err := c.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true}); err != nil {
		if client.IsErrNotFound(err) {
			return nil // Already removed
		}
		return fmt.Errorf("failed to remove container: %w", err)
	}

	return nil
}

// CreateAndStartContainer creates and starts a container with the given configuration.
func (c *Client) CreateAndStartContainer(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, name string) (string, error) {
	resp, err := c.ContainerCreate(ctx, config, hostConfig, nil, nil, name)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	if err := c.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		// Try to clean up the created container
		_ = c.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		return "", fmt.Errorf("failed to start container: %w", err)
	}

	return resp.ID, nil
}

// PullImageIfNeeded pulls an image if it doesn't exist locally.
func (c *Client) PullImageIfNeeded(ctx context.Context, imageName string) error {
	_, _, err := c.ImageInspectWithRaw(ctx, imageName)
	if err == nil {
		return nil // Image exists
	}

	if !client.IsErrNotFound(err) {
		return fmt.Errorf("failed to inspect image: %w", err)
	}

	// Pull the image
	reader, err := c.ImagePull(ctx, imageName, types.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}
	defer reader.Close()

	// Wait for pull to complete
	_, err = io.Copy(io.Discard, reader)
	if err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}

	return nil
}
