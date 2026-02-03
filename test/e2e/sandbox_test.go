// Package e2e contains end-to-end tests for the Kubernetes sandbox provider.
// These tests require a running Kubernetes cluster with the fleetlift controller deployed.
// Run with: E2E_TEST=true go test -v ./test/e2e/...
package e2e

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetliftv1alpha1 "github.com/tinkerloft/fleetlift/api/v1alpha1"
	"github.com/tinkerloft/fleetlift/internal/sandbox"
	"github.com/tinkerloft/fleetlift/internal/sandbox/kubernetes"
)

func init() {
	// Register types with scheme
	_ = fleetliftv1alpha1.AddToScheme(scheme.Scheme)
	_ = corev1.AddToScheme(scheme.Scheme)
}

func skipIfNotE2E(t *testing.T) {
	if os.Getenv("E2E_TEST") != "true" {
		t.Skip("Skipping E2E test. Set E2E_TEST=true to run.")
	}
}

func getKubeClient(t *testing.T) client.Client {
	t.Helper()

	// Use kubeconfig from environment or default location
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, err := os.UserHomeDir()
		require.NoError(t, err)
		kubeconfig = home + "/.kube/config"
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	require.NoError(t, err, "Failed to build kubeconfig")

	k8sClient, err := client.New(config, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err, "Failed to create kubernetes client")

	return k8sClient
}

func TestSandboxRequestLifecycle(t *testing.T) {
	skipIfNotE2E(t)

	k8sClient := getKubeClient(t)
	ctx := context.Background()

	// Create a SandboxRequest
	taskID := "e2e-test-" + time.Now().Format("20060102-150405")
	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sandbox-" + taskID,
			Namespace: "fleetlift-system",
		},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID: taskID,
			Image:  "alpine:latest",
			Env: map[string]string{
				"TEST_VAR": "hello",
			},
		},
	}

	// Create
	t.Log("Creating SandboxRequest...")
	err := k8sClient.Create(ctx, sr)
	require.NoError(t, err, "Failed to create SandboxRequest")

	// Cleanup on test end
	defer func() {
		t.Log("Cleaning up SandboxRequest...")
		_ = k8sClient.Delete(ctx, sr)
	}()

	// Wait for Running phase
	t.Log("Waiting for sandbox to be running...")
	err = waitForPhase(ctx, k8sClient, sr.Name, sr.Namespace, fleetliftv1alpha1.SandboxPhaseRunning, 2*time.Minute)
	require.NoError(t, err, "Sandbox did not reach Running phase")

	// Verify status
	err = k8sClient.Get(ctx, types.NamespacedName{Name: sr.Name, Namespace: sr.Namespace}, sr)
	require.NoError(t, err)
	assert.NotEmpty(t, sr.Status.PodName, "PodName should be set")
	assert.NotEmpty(t, sr.Status.JobName, "JobName should be set")

	t.Log("SandboxRequest lifecycle test passed!")
}

func TestSandboxExec(t *testing.T) {
	skipIfNotE2E(t)

	k8sClient := getKubeClient(t)
	ctx := context.Background()

	// Create a SandboxRequest
	taskID := "e2e-exec-" + time.Now().Format("20060102-150405")
	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sandbox-" + taskID,
			Namespace: "fleetlift-system",
		},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID: taskID,
			Image:  "alpine:latest",
		},
	}

	// Create and wait for running
	err := k8sClient.Create(ctx, sr)
	require.NoError(t, err)
	defer func() {
		_ = k8sClient.Delete(ctx, sr)
	}()

	err = waitForPhase(ctx, k8sClient, sr.Name, sr.Namespace, fleetliftv1alpha1.SandboxPhaseRunning, 2*time.Minute)
	require.NoError(t, err)

	// Add an exec request
	t.Log("Adding exec request...")
	err = k8sClient.Get(ctx, types.NamespacedName{Name: sr.Name, Namespace: sr.Namespace}, sr)
	require.NoError(t, err)

	execID := "exec-1"
	sr.Spec.ExecRequests = append(sr.Spec.ExecRequests, fleetliftv1alpha1.ExecRequest{
		ID:             execID,
		Command:        []string{"echo", "hello world"},
		TimeoutSeconds: 30,
	})

	err = k8sClient.Update(ctx, sr)
	require.NoError(t, err)

	// Wait for exec result
	t.Log("Waiting for exec result...")
	err = waitForExecResult(ctx, k8sClient, sr.Name, sr.Namespace, execID, 30*time.Second)
	require.NoError(t, err)

	// Verify result
	err = k8sClient.Get(ctx, types.NamespacedName{Name: sr.Name, Namespace: sr.Namespace}, sr)
	require.NoError(t, err)

	require.Len(t, sr.Status.ExecResults, 1)
	result := sr.Status.ExecResults[0]
	assert.Equal(t, execID, result.ID)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "hello world")

	t.Log("Exec test passed!")
}

func TestKubernetesProvider(t *testing.T) {
	skipIfNotE2E(t)

	// Create provider with external kubeconfig
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, err := os.UserHomeDir()
		require.NoError(t, err)
		kubeconfig = home + "/.kube/config"
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	require.NoError(t, err)

	k8sClient, err := client.New(config, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)

	cfg := kubernetes.ProviderConfig{
		Namespace:        "fleetlift-system",
		SandboxNamespace: "sandbox-isolated",
		PollInterval:     500 * time.Millisecond,
		ProvisionTimeout: 3 * time.Minute,
	}

	p := kubernetes.NewProviderWithClient(k8sClient, cfg)

	ctx := context.Background()
	taskID := "provider-test-" + time.Now().Format("20060102-150405")

	// Test Provision
	t.Log("Testing Provision...")
	sb, err := p.Provision(ctx, sandbox.ProvisionOptions{
		TaskID:     taskID,
		Image:      "alpine:latest",
		WorkingDir: "/workspace",
		Env: map[string]string{
			"TEST": "value",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "kubernetes", sb.Provider)

	// Cleanup
	defer func() {
		t.Log("Cleaning up...")
		_ = p.Cleanup(ctx, sb.ID)
	}()

	// Test Status
	t.Log("Testing Status...")
	status, err := p.Status(ctx, sb.ID)
	require.NoError(t, err)
	assert.Equal(t, sandbox.SandboxPhaseRunning, status.Phase)

	// Test Exec
	t.Log("Testing Exec...")
	result, err := p.Exec(ctx, sb.ID, sandbox.ExecCommand{
		Command: []string{"echo", "hello from provider"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "hello from provider")

	// Test ExecShell
	t.Log("Testing ExecShell...")
	result, err = p.ExecShell(ctx, sb.ID, "pwd && ls -la", "")
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)

	t.Log("Provider test passed!")
}

func waitForPhase(ctx context.Context, k8sClient client.Client, name, namespace string, phase fleetliftv1alpha1.SandboxPhase, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		sr := &fleetliftv1alpha1.SandboxRequest{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, sr); err != nil {
			return err
		}
		if sr.Status.Phase == phase {
			return nil
		}
		if sr.Status.Phase == fleetliftv1alpha1.SandboxPhaseFailed {
			return &phaseError{message: sr.Status.Message}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return &timeoutError{expected: string(phase)}
}

func waitForExecResult(ctx context.Context, k8sClient client.Client, name, namespace, execID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		sr := &fleetliftv1alpha1.SandboxRequest{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, sr); err != nil {
			return err
		}
		for _, result := range sr.Status.ExecResults {
			if result.ID == execID {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return &timeoutError{expected: "exec result " + execID}
}

type phaseError struct {
	message string
}

func (e *phaseError) Error() string {
	return "sandbox failed: " + e.message
}

type timeoutError struct {
	expected string
}

func (e *timeoutError) Error() string {
	return "timeout waiting for: " + e.expected
}
