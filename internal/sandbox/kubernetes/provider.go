// Package kubernetes provides a Kubernetes-based sandbox provider using the controller pattern.
package kubernetes

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetliftv1alpha1 "github.com/tinkerloft/fleetlift/api/v1alpha1"
	"github.com/tinkerloft/fleetlift/internal/activity"
	"github.com/tinkerloft/fleetlift/internal/controller"
	"github.com/tinkerloft/fleetlift/internal/sandbox"
)

const (
	// DefaultPollInterval is the default interval for polling CR status
	DefaultPollInterval = 500 * time.Millisecond
	// DefaultProvisionTimeout is the default timeout for sandbox provisioning
	DefaultProvisionTimeout = 5 * time.Minute
	// DefaultExecPollInterval is the interval for polling exec results
	DefaultExecPollInterval = 200 * time.Millisecond
	// MaxExecUpdateRetries is the maximum number of retries for exec CR updates
	MaxExecUpdateRetries = 5
)

// ProviderConfig contains configuration for the Kubernetes provider
type ProviderConfig struct {
	// Namespace is the namespace where SandboxRequest CRs are created
	Namespace string
	// SandboxNamespace is the namespace where sandbox pods run (may be different)
	SandboxNamespace string
	// DefaultImage is the default container image for sandboxes
	DefaultImage string
	// PollInterval is the interval for polling CR status
	PollInterval time.Duration
	// ProvisionTimeout is the timeout for sandbox provisioning
	ProvisionTimeout time.Duration
}

// DefaultConfig returns a ProviderConfig with default values
func DefaultConfig() ProviderConfig {
	namespace := os.Getenv("SANDBOX_REQUEST_NAMESPACE")
	if namespace == "" {
		namespace = "fleetlift-system"
	}
	sandboxNs := os.Getenv("SANDBOX_NAMESPACE")
	if sandboxNs == "" {
		sandboxNs = "sandbox-isolated"
	}
	return ProviderConfig{
		Namespace:        namespace,
		SandboxNamespace: sandboxNs,
		PollInterval:     DefaultPollInterval,
		ProvisionTimeout: DefaultProvisionTimeout,
	}
}

// Provider implements sandbox.Provider using Kubernetes custom resources
type Provider struct {
	client client.Client
	config ProviderConfig
}

// NewProvider creates a new Kubernetes sandbox provider
func NewProvider() (*Provider, error) {
	return NewProviderWithConfig(DefaultConfig())
}

// NewProviderWithConfig creates a new Kubernetes sandbox provider with custom config
func NewProviderWithConfig(cfg ProviderConfig) (*Provider, error) {
	// Get in-cluster config
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
	}

	// Create controller-runtime client
	scheme, err := fleetliftv1alpha1.SchemeBuilder.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build scheme: %w", err)
	}
	// Add core types to scheme
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add core types to scheme: %w", err)
	}

	k8sClient, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return &Provider{
		client: k8sClient,
		config: cfg,
	}, nil
}

// NewProviderWithClient creates a provider with a pre-configured client (for testing)
func NewProviderWithClient(k8sClient client.Client, cfg ProviderConfig) *Provider {
	return &Provider{
		client: k8sClient,
		config: cfg,
	}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "kubernetes"
}

// Provision creates a new sandbox by creating a SandboxRequest CR
func (p *Provider) Provision(ctx context.Context, opts sandbox.ProvisionOptions) (*sandbox.Sandbox, error) {
	// Generate CR name from task ID (sanitized for K8s naming requirements)
	sanitizedTaskID := controller.SanitizeK8sName(opts.TaskID, 55)
	crName := fmt.Sprintf("sandbox-%s", sanitizedTaskID)

	// Build resource requirements
	resources := fleetliftv1alpha1.ResourceRequirements{}
	if opts.Resources.MemoryBytes > 0 || opts.Resources.CPUQuota > 0 {
		resources.Limits = make(corev1.ResourceList)
		resources.Requests = make(corev1.ResourceList)

		if opts.Resources.MemoryBytes > 0 {
			resources.Limits[corev1.ResourceMemory] = *resource.NewQuantity(opts.Resources.MemoryBytes, resource.BinarySI)
			resources.Requests[corev1.ResourceMemory] = *resource.NewQuantity(opts.Resources.MemoryBytes/4, resource.BinarySI)
		}
		if opts.Resources.CPUQuota > 0 {
			// Convert from 1/100000 of CPU to millicores
			milliCores := opts.Resources.CPUQuota / 100
			resources.Limits[corev1.ResourceCPU] = *resource.NewMilliQuantity(milliCores, resource.DecimalSI)
			resources.Requests[corev1.ResourceCPU] = *resource.NewMilliQuantity(milliCores/4, resource.DecimalSI)
		}
	}

	// Create SandboxRequest CR
	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      crName,
			Namespace: p.config.Namespace,
		},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID:     opts.TaskID,
			Image:      opts.Image,
			WorkingDir: opts.WorkingDir,
			Env:        opts.Env,
			Resources:  resources,
		},
	}

	// Add credential references from environment
	sr.Spec.Credentials = p.buildCredentialRefs()

	if err := p.client.Create(ctx, sr); err != nil {
		if apierrors.IsAlreadyExists(err) {
			// CR already exists, fetch it
			if err := p.client.Get(ctx, types.NamespacedName{
				Name:      crName,
				Namespace: p.config.Namespace,
			}, sr); err != nil {
				return nil, fmt.Errorf("failed to get existing sandbox request: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to create sandbox request: %w", err)
		}
	}

	// Wait for sandbox to be running
	timeout := p.config.ProvisionTimeout
	if opts.Timeout > 0 {
		timeout = opts.Timeout
	}

	if err := p.waitForRunning(ctx, crName, timeout); err != nil {
		return nil, fmt.Errorf("failed waiting for sandbox to run: %w", err)
	}

	return &sandbox.Sandbox{
		ID:         crName,
		Provider:   "kubernetes",
		WorkingDir: opts.WorkingDir,
	}, nil
}

// waitForRunning waits for the SandboxRequest to reach Running phase
func (p *Provider) waitForRunning(ctx context.Context, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, p.config.PollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		sr := &fleetliftv1alpha1.SandboxRequest{}
		if err := p.client.Get(ctx, types.NamespacedName{
			Name:      name,
			Namespace: p.config.Namespace,
		}, sr); err != nil {
			return false, err
		}

		switch sr.Status.Phase {
		case fleetliftv1alpha1.SandboxPhaseRunning:
			return true, nil
		case fleetliftv1alpha1.SandboxPhaseFailed:
			return false, fmt.Errorf("sandbox failed: %s", sr.Status.Message)
		default:
			return false, nil
		}
	})
}

// Exec executes a command in the sandbox via the controller
func (p *Provider) Exec(ctx context.Context, id string, cmd sandbox.ExecCommand) (*sandbox.ExecResult, error) {
	// Generate unique ID for this exec request
	execID := uuid.New().String()

	// Calculate timeout
	timeoutSeconds := int32(fleetliftv1alpha1.DefaultExecTimeoutSeconds)
	if cmd.Timeout > 0 {
		timeoutSeconds = int32(cmd.Timeout.Seconds())
		if timeoutSeconds > fleetliftv1alpha1.MaxExecTimeoutSeconds {
			timeoutSeconds = fleetliftv1alpha1.MaxExecTimeoutSeconds
		}
	}

	// Build exec request
	// Note: Per-command environment variables (cmd.Env) are not supported.
	// Kubernetes exec API does not support per-command env vars natively.
	// Environment must be set at container creation time via SandboxRequestSpec.Env.
	execReq := fleetliftv1alpha1.ExecRequest{
		ID:             execID,
		Command:        cmd.Command,
		WorkingDir:     cmd.WorkingDir,
		User:           cmd.User,
		TimeoutSeconds: timeoutSeconds,
	}

	// Retry loop for conflict handling
	var lastErr error
	for retries := 0; retries < MaxExecUpdateRetries; retries++ {
		sr := &fleetliftv1alpha1.SandboxRequest{}
		if err := p.client.Get(ctx, types.NamespacedName{
			Name:      id,
			Namespace: p.config.Namespace,
		}, sr); err != nil {
			return nil, fmt.Errorf("failed to get sandbox request: %w", err)
		}

		// Check that sandbox is running
		if sr.Status.Phase != fleetliftv1alpha1.SandboxPhaseRunning {
			return nil, fmt.Errorf("sandbox is not running (phase: %s)", sr.Status.Phase)
		}

		// Append exec request to spec
		sr.Spec.ExecRequests = append(sr.Spec.ExecRequests, execReq)

		// Update the CR
		if err := p.client.Update(ctx, sr); err != nil {
			if apierrors.IsConflict(err) {
				lastErr = err
				time.Sleep(time.Duration(retries*50) * time.Millisecond)
				continue
			}
			return nil, fmt.Errorf("failed to update sandbox request with exec: %w", err)
		}
		lastErr = nil
		break
	}
	if lastErr != nil {
		return nil, fmt.Errorf("failed to update sandbox request after %d retries: %w", MaxExecUpdateRetries, lastErr)
	}

	// Poll for result
	timeout := time.Duration(timeoutSeconds)*time.Second + 30*time.Second // Add buffer
	result, err := p.waitForExecResult(ctx, id, execID, timeout)
	if err != nil {
		return nil, fmt.Errorf("failed waiting for exec result: %w", err)
	}

	return &sandbox.ExecResult{
		ExitCode: result.ExitCode,
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
	}, nil
}

// waitForExecResult waits for an exec result to appear in the CR status
func (p *Provider) waitForExecResult(ctx context.Context, crName, execID string, timeout time.Duration) (*fleetliftv1alpha1.ExecResult, error) {
	var result *fleetliftv1alpha1.ExecResult

	err := wait.PollUntilContextTimeout(ctx, DefaultExecPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		sr := &fleetliftv1alpha1.SandboxRequest{}
		if err := p.client.Get(ctx, types.NamespacedName{
			Name:      crName,
			Namespace: p.config.Namespace,
		}, sr); err != nil {
			return false, err
		}

		// Check if sandbox is still running
		if sr.Status.Phase == fleetliftv1alpha1.SandboxPhaseFailed {
			return false, fmt.Errorf("sandbox failed: %s", sr.Status.Message)
		}

		// Look for our exec result
		for i := range sr.Status.ExecResults {
			if sr.Status.ExecResults[i].ID == execID {
				result = &sr.Status.ExecResults[i]
				return true, nil
			}
		}

		// Check if result was likely trimmed (exec was processed but result not found)
		// Find our exec request index by ID
		ourIndex := -1
		for i, req := range sr.Spec.ExecRequests {
			if req.ID == execID {
				ourIndex = i
				break
			}
		}
		// If our request was processed (index < LastExecProcessedIndex) but result not found,
		// it was likely trimmed due to MaxExecResultsToRetain
		if ourIndex >= 0 && sr.Status.LastExecProcessedIndex > ourIndex {
			return false, fmt.Errorf("exec result for %s was processed but not found (possibly trimmed due to result retention limit)", execID)
		}

		return false, nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

// ExecShell executes a shell command string
func (p *Provider) ExecShell(ctx context.Context, id string, command string, user string) (*sandbox.ExecResult, error) {
	return p.Exec(ctx, id, sandbox.ExecCommand{
		Command: []string{"bash", "-c", command},
		User:    user,
	})
}

// CopyFrom copies a file from the sandbox using exec
func (p *Provider) CopyFrom(ctx context.Context, id string, srcPath string) (io.ReadCloser, error) {
	// Use cat to read the file
	result, err := p.Exec(ctx, id, sandbox.ExecCommand{
		Command: []string{"cat", srcPath},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	if result.ExitCode != 0 {
		return nil, fmt.Errorf("cat failed with exit code %d: %s", result.ExitCode, result.Stderr)
	}

	return io.NopCloser(strings.NewReader(result.Stdout)), nil
}

// CopyTo copies data into the sandbox using exec
func (p *Provider) CopyTo(ctx context.Context, id string, src io.Reader, destPath string) error {
	// Read all data
	data, err := io.ReadAll(src)
	if err != nil {
		return fmt.Errorf("failed to read source data: %w", err)
	}

	// Base64 encode and use echo + base64 decode to write
	encoded := base64.StdEncoding.EncodeToString(data)

	// Write in chunks if data is large
	const maxChunkSize = 65536 // 64KB chunks for command line safety

	// Use shellQuote to prevent command injection via destPath
	quotedPath := activity.ShellQuote(destPath)

	if len(encoded) <= maxChunkSize {
		result, err := p.Exec(ctx, id, sandbox.ExecCommand{
			Command: []string{"sh", "-c", fmt.Sprintf("echo '%s' | base64 -d > %s", encoded, quotedPath)},
		})
		if err != nil {
			return fmt.Errorf("failed to write file: %w", err)
		}
		if result.ExitCode != 0 {
			return fmt.Errorf("write failed with exit code %d: %s", result.ExitCode, result.Stderr)
		}
	} else {
		// For large files, write in chunks
		quotedB64Path := activity.ShellQuote(destPath + ".b64")
		b64TempCreated := false

		// Cleanup temp file on error
		defer func() {
			if b64TempCreated {
				// Best-effort cleanup of temp file on error path
				// The normal success path removes it via the decode command
				_, _ = p.Exec(ctx, id, sandbox.ExecCommand{
					Command: []string{"rm", "-f", destPath + ".b64"},
				})
			}
		}()

		// First, create/truncate the file
		result, err := p.Exec(ctx, id, sandbox.ExecCommand{
			Command: []string{"sh", "-c", fmt.Sprintf(": > %s", quotedPath)},
		})
		if err != nil {
			return fmt.Errorf("failed to create file: %w", err)
		}
		if result.ExitCode != 0 {
			return fmt.Errorf("file creation failed: %s", result.Stderr)
		}

		// Write chunks
		for i := 0; i < len(encoded); i += maxChunkSize {
			end := i + maxChunkSize
			if end > len(encoded) {
				end = len(encoded)
			}
			chunk := encoded[i:end]

			result, err := p.Exec(ctx, id, sandbox.ExecCommand{
				Command: []string{"sh", "-c", fmt.Sprintf("echo -n '%s' >> %s", chunk, quotedB64Path)},
			})
			if err != nil {
				return fmt.Errorf("failed to write chunk: %w", err)
			}
			if result.ExitCode != 0 {
				return fmt.Errorf("chunk write failed: %s", result.Stderr)
			}
			b64TempCreated = true
		}

		// Decode the assembled file
		result, err = p.Exec(ctx, id, sandbox.ExecCommand{
			Command: []string{"sh", "-c", fmt.Sprintf("base64 -d %s > %s && rm %s", quotedB64Path, quotedPath, quotedB64Path)},
		})
		if err != nil {
			return fmt.Errorf("failed to decode file: %w", err)
		}
		if result.ExitCode != 0 {
			return fmt.Errorf("decode failed: %s", result.Stderr)
		}
		// Success - temp file was cleaned up by the decode command
		b64TempCreated = false
	}

	return nil
}

// Status returns the current sandbox status
func (p *Provider) Status(ctx context.Context, id string) (*sandbox.SandboxStatus, error) {
	sr := &fleetliftv1alpha1.SandboxRequest{}
	if err := p.client.Get(ctx, types.NamespacedName{
		Name:      id,
		Namespace: p.config.Namespace,
	}, sr); err != nil {
		if apierrors.IsNotFound(err) {
			return &sandbox.SandboxStatus{
				Phase:   sandbox.SandboxPhaseUnknown,
				Message: "sandbox request not found",
			}, nil
		}
		return nil, fmt.Errorf("failed to get sandbox request: %w", err)
	}

	// Map CR phase to sandbox phase
	var phase sandbox.SandboxPhase
	switch sr.Status.Phase {
	case fleetliftv1alpha1.SandboxPhasePending, fleetliftv1alpha1.SandboxPhaseProvisioning:
		phase = sandbox.SandboxPhasePending
	case fleetliftv1alpha1.SandboxPhaseRunning:
		phase = sandbox.SandboxPhaseRunning
	case fleetliftv1alpha1.SandboxPhaseSucceeded:
		phase = sandbox.SandboxPhaseSucceeded
	case fleetliftv1alpha1.SandboxPhaseFailed:
		phase = sandbox.SandboxPhaseFailed
	default:
		phase = sandbox.SandboxPhaseUnknown
	}

	return &sandbox.SandboxStatus{
		Phase:   phase,
		Message: sr.Status.Message,
	}, nil
}

// Cleanup deletes the SandboxRequest CR
func (p *Provider) Cleanup(ctx context.Context, id string) error {
	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      id,
			Namespace: p.config.Namespace,
		},
	}

	if err := p.client.Delete(ctx, sr); err != nil {
		if apierrors.IsNotFound(err) {
			return nil // Already deleted
		}
		return fmt.Errorf("failed to delete sandbox request: %w", err)
	}

	return nil
}

// buildCredentialRefs builds credential references from environment
func (p *Provider) buildCredentialRefs() []fleetliftv1alpha1.CredentialRef {
	var creds []fleetliftv1alpha1.CredentialRef

	// Add GitHub token if configured
	if secretName := os.Getenv("GITHUB_TOKEN_SECRET"); secretName != "" {
		creds = append(creds, fleetliftv1alpha1.CredentialRef{
			Name:      secretName,
			EnvVar:    "GITHUB_TOKEN",
			SecretKey: "token",
		})
	}

	// Add Anthropic API key if configured
	if secretName := os.Getenv("ANTHROPIC_API_KEY_SECRET"); secretName != "" {
		creds = append(creds, fleetliftv1alpha1.CredentialRef{
			Name:      secretName,
			EnvVar:    "ANTHROPIC_API_KEY",
			SecretKey: "api-key",
		})
	}

	return creds
}

