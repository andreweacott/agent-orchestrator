// Package docker provides a Docker-based sandbox provider.
package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/andreweacott/agent-orchestrator/internal/sandbox"
)

// Provider implements sandbox.Provider using Docker containers.
type Provider struct {
	client *client.Client
}

// NewProvider creates a new Docker sandbox provider.
func NewProvider() (*Provider, error) {
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

	return &Provider{client: c}, nil
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "docker"
}

// Provision creates a new Docker container for sandbox execution.
func (p *Provider) Provision(ctx context.Context, opts sandbox.ProvisionOptions) (*sandbox.Sandbox, error) {
	containerConfig := &container.Config{
		Image:     opts.Image,
		Tty:       true,
		OpenStdin: true,
		// Use /tmp for the log file since /var/log may not be writable by non-root users
		Cmd: []string{"sh", "-c", "touch /tmp/claude-code.log && tail -f /tmp/claude-code.log"},
		Env: envMapToSlice(opts.Env),
	}

	hostConfig := &container.HostConfig{
		Resources: container.Resources{
			Memory:    opts.Resources.MemoryBytes,
			CPUPeriod: 100000,
			CPUQuota:  opts.Resources.CPUQuota,
		},
		SecurityOpt: []string{"no-new-privileges:true"},
	}

	// Apply network mode from environment
	networkMode := os.Getenv("SANDBOX_NETWORK_MODE")
	if networkMode == "" {
		networkMode = "bridge"
	}
	hostConfig.NetworkMode = container.NetworkMode(networkMode)

	containerName := fmt.Sprintf("claude-sandbox-%s", opts.TaskID)

	resp, err := p.client.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, containerName)
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	if err := p.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		// Try to clean up the created container
		_ = p.client.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	return &sandbox.Sandbox{
		ID:         resp.ID,
		Provider:   "docker",
		WorkingDir: opts.WorkingDir,
	}, nil
}

// Exec executes a command in a Docker container.
func (p *Provider) Exec(ctx context.Context, id string, cmd sandbox.ExecCommand) (*sandbox.ExecResult, error) {
	execConfig := types.ExecConfig{
		Cmd:          cmd.Command,
		AttachStdout: true,
		AttachStderr: true,
		User:         cmd.User,
		WorkingDir:   cmd.WorkingDir,
		Env:          envMapToSlice(cmd.Env),
	}

	execID, err := p.client.ContainerExecCreate(ctx, id, execConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create exec: %w", err)
	}

	resp, err := p.client.ContainerExecAttach(ctx, execID.ID, types.ExecStartCheck{})
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
	inspect, err := p.client.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect exec: %w", err)
	}

	result := &sandbox.ExecResult{
		ExitCode: inspect.ExitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}

	// Log the execution to the container log file
	timestamp := time.Now().Format(time.RFC3339)
	logEntry := fmt.Sprintf("[%s] Command: %v\nExit Code: %d\nStdout:\n%s\nStderr:\n%s\n%s\n",
		timestamp, execConfig.Cmd, result.ExitCode, result.Stdout, result.Stderr, "---")

	// Write to log (ignore errors to not fail the main execution)
	_ = p.writeToLog(ctx, id, logEntry)

	return result, nil
}

// writeToLog appends a message to the container's log file.
func (p *Provider) writeToLog(ctx context.Context, id string, message string) error {
	execConfig := types.ExecConfig{
		Cmd:          []string{"sh", "-c", "cat >> /tmp/claude-code.log"},
		AttachStdin:  true,
		AttachStdout: false,
		AttachStderr: false,
	}

	execID, err := p.client.ContainerExecCreate(ctx, id, execConfig)
	if err != nil {
		return err
	}

	resp, err := p.client.ContainerExecAttach(ctx, execID.ID, types.ExecStartCheck{})
	if err != nil {
		return err
	}
	defer resp.Close()

	_, err = resp.Conn.Write([]byte(message))
	if err != nil {
		return err
	}

	return resp.Conn.Close()
}

// ExecShell executes a shell command string in a Docker container.
// This is a convenience method that wraps the command in bash -c.
func (p *Provider) ExecShell(ctx context.Context, id string, command string, user string) (*sandbox.ExecResult, error) {
	return p.Exec(ctx, id, sandbox.ExecCommand{
		Command: []string{"bash", "-c", command},
		User:    user,
	})
}

// CopyFrom copies a file from a Docker container.
func (p *Provider) CopyFrom(ctx context.Context, id string, srcPath string) (io.ReadCloser, error) {
	reader, _, err := p.client.CopyFromContainer(ctx, id, srcPath)
	if err != nil {
		return nil, fmt.Errorf("failed to copy from container: %w", err)
	}
	return reader, nil
}

// CopyTo copies data into a Docker container.
func (p *Provider) CopyTo(ctx context.Context, id string, src io.Reader, destPath string) error {
	// Read all data from src
	data, err := io.ReadAll(src)
	if err != nil {
		return fmt.Errorf("failed to read source data: %w", err)
	}

	// Create a tar archive with the file
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	header := &tar.Header{
		Name: path.Base(destPath),
		Mode: 0644,
		Size: int64(len(data)),
	}

	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write tar header: %w", err)
	}

	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("failed to write tar data: %w", err)
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("failed to close tar writer: %w", err)
	}

	// Copy to container
	err = p.client.CopyToContainer(ctx, id, path.Dir(destPath), &buf, types.CopyToContainerOptions{})
	if err != nil {
		return fmt.Errorf("failed to copy to container: %w", err)
	}

	return nil
}

// Status returns the current status of a Docker container.
func (p *Provider) Status(ctx context.Context, id string) (*sandbox.SandboxStatus, error) {
	inspect, err := p.client.ContainerInspect(ctx, id)
	if err != nil {
		if client.IsErrNotFound(err) {
			return &sandbox.SandboxStatus{
				Phase:   sandbox.SandboxPhaseUnknown,
				Message: "container not found",
			}, nil
		}
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	var phase sandbox.SandboxPhase
	var message string

	switch {
	case inspect.State.Running:
		phase = sandbox.SandboxPhaseRunning
		message = "container is running"
	case inspect.State.Paused:
		phase = sandbox.SandboxPhasePending
		message = "container is paused"
	case inspect.State.Restarting:
		phase = sandbox.SandboxPhasePending
		message = "container is restarting"
	case inspect.State.Dead:
		phase = sandbox.SandboxPhaseFailed
		message = fmt.Sprintf("container is dead: %s", inspect.State.Error)
	case inspect.State.ExitCode == 0:
		phase = sandbox.SandboxPhaseSucceeded
		message = "container exited successfully"
	case inspect.State.ExitCode != 0:
		phase = sandbox.SandboxPhaseFailed
		message = fmt.Sprintf("container exited with code %d", inspect.State.ExitCode)
	default:
		phase = sandbox.SandboxPhaseUnknown
		message = inspect.State.Status
	}

	return &sandbox.SandboxStatus{
		Phase:   phase,
		Message: message,
	}, nil
}

// Cleanup stops and removes a Docker container.
func (p *Provider) Cleanup(ctx context.Context, id string) error {
	timeout := 10
	if err := p.client.ContainerStop(ctx, id, container.StopOptions{Timeout: &timeout}); err != nil {
		if !client.IsErrNotFound(err) {
			// Log but continue to removal
			fmt.Printf("Warning: failed to stop container %s: %v\n", shortID(id), err)
		}
	}

	if err := p.client.ContainerRemove(ctx, id, container.RemoveOptions{Force: true}); err != nil {
		if client.IsErrNotFound(err) {
			return nil // Already removed
		}
		return fmt.Errorf("failed to remove container: %w", err)
	}

	return nil
}

// IsContainerRunning checks if a container is running.
func (p *Provider) IsContainerRunning(ctx context.Context, id string) (bool, error) {
	inspect, err := p.client.ContainerInspect(ctx, id)
	if err != nil {
		if client.IsErrNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return inspect.State.Running, nil
}

// PullImageIfNeeded pulls an image if it doesn't exist locally.
func (p *Provider) PullImageIfNeeded(ctx context.Context, imageName string) error {
	_, _, err := p.client.ImageInspectWithRaw(ctx, imageName)
	if err == nil {
		return nil // Image exists
	}

	if !client.IsErrNotFound(err) {
		return fmt.Errorf("failed to inspect image: %w", err)
	}

	// Pull the image
	reader, err := p.client.ImagePull(ctx, imageName, types.ImagePullOptions{})
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

// GetClient returns the underlying Docker client for advanced operations.
// This should be used sparingly; prefer the Provider interface methods.
func (p *Provider) GetClient() *client.Client {
	return p.client
}

// envMapToSlice converts a map of env vars to a slice of KEY=VALUE strings.
func envMapToSlice(env map[string]string) []string {
	if env == nil {
		return nil
	}
	result := make([]string, 0, len(env))
	for k, v := range env {
		result = append(result, fmt.Sprintf("%s=%s", k, v))
	}
	return result
}

// shortID safely truncates container ID for logging.
func shortID(id string) string {
	if id == "" {
		return "<empty>"
	}
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
