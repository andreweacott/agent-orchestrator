package controller

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	fleetliftv1alpha1 "github.com/tinkerloft/fleetlift/api/v1alpha1"
	"github.com/tinkerloft/fleetlift/internal/activity"
)

// ExecHandler handles command execution in sandbox pods
type ExecHandler struct {
	config    *rest.Config
	clientset *kubernetes.Clientset
}

// NewExecHandler creates a new ExecHandler
func NewExecHandler(config *rest.Config) (*ExecHandler, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}
	return &ExecHandler{
		config:    config,
		clientset: clientset,
	}, nil
}

// buildEffectiveCommand wraps the command with cd if WorkingDir is specified.
// Note: User field is not handled at exec time since the container runs as a fixed
// non-root user (65532). The User field is informational only.
// Note: Per-command Env field is not supported. Kubernetes exec API does not support
// per-command environment variables. Environment is set at container creation time.
func buildEffectiveCommand(req *fleetliftv1alpha1.ExecRequest) []string {
	if req.WorkingDir == "" {
		return req.Command
	}

	// Build a quoted version of the original command
	quotedArgs := make([]string, len(req.Command))
	for i, arg := range req.Command {
		quotedArgs[i] = activity.ShellQuote(arg)
	}
	cmdStr := strings.Join(quotedArgs, " ")

	// Wrap with cd to the working directory
	return []string{"sh", "-c", fmt.Sprintf("cd %s && %s", activity.ShellQuote(req.WorkingDir), cmdStr)}
}

// Execute runs a command in a pod and returns the result
func (h *ExecHandler) Execute(ctx context.Context, namespace, podName string, req *fleetliftv1alpha1.ExecRequest) *fleetliftv1alpha1.ExecResult {
	result := &fleetliftv1alpha1.ExecResult{
		ID: req.ID,
	}

	// Set timeout
	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = time.Duration(fleetliftv1alpha1.DefaultExecTimeoutSeconds) * time.Second
	}
	if timeout > time.Duration(fleetliftv1alpha1.MaxExecTimeoutSeconds)*time.Second {
		timeout = time.Duration(fleetliftv1alpha1.MaxExecTimeoutSeconds) * time.Second
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Build effective command (handles WorkingDir)
	command := buildEffectiveCommand(req)

	// Build exec request
	execReq := h.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec")

	execReq.VersionedParams(&corev1.PodExecOptions{
		Container: ContainerName,
		Command:   command,
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}, scheme.ParameterCodec)

	// Create executor
	exec, err := remotecommand.NewSPDYExecutor(h.config, "POST", execReq.URL())
	if err != nil {
		result.Error = fmt.Sprintf("failed to create executor: %v", err)
		result.ExitCode = -1
		now := metav1.Now()
		result.CompletedAt = &now
		return result
	}

	// Execute command
	var stdout, stderr bytes.Buffer
	err = exec.StreamWithContext(execCtx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	now := metav1.Now()
	result.CompletedAt = &now

	// Handle result
	if err != nil {
		// Check if it's an exit code error
		if exitErr, ok := err.(interface{ ExitStatus() int }); ok {
			result.ExitCode = exitErr.ExitStatus()
		} else {
			result.ExitCode = -1
			result.Error = err.Error()
		}
	} else {
		result.ExitCode = 0
	}

	// Truncate output if needed
	result.Stdout = truncateOutput(stdout.String())
	result.Stderr = truncateOutput(stderr.String())

	return result
}

// truncateOutput truncates output to the maximum allowed size
func truncateOutput(s string) string {
	if len(s) > fleetliftv1alpha1.MaxExecOutputSize {
		return s[:fleetliftv1alpha1.MaxExecOutputSize] + "\n... [truncated]"
	}
	return s
}
