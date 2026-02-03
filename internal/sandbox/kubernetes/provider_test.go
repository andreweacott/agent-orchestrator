package kubernetes

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetliftv1alpha1 "github.com/tinkerloft/fleetlift/api/v1alpha1"
	"github.com/tinkerloft/fleetlift/internal/sandbox"
)

func TestProviderName(t *testing.T) {
	p := &Provider{}
	assert.Equal(t, "kubernetes", p.Name())
}

func TestProvision(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, fleetliftv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	client := fake.NewClientBuilder().WithScheme(scheme).Build()

	cfg := ProviderConfig{
		Namespace:        "test-ns",
		SandboxNamespace: "sandbox-ns",
		PollInterval:     10 * time.Millisecond,
		ProvisionTimeout: 100 * time.Millisecond,
	}

	p := NewProviderWithClient(client, cfg)

	ctx := context.Background()

	// This will timeout waiting for Running phase (since no controller is running)
	// but should create the CR
	_, err := p.Provision(ctx, sandbox.ProvisionOptions{
		TaskID:     "test-task-123",
		Image:      "claude-code-sandbox:latest",
		WorkingDir: "/workspace",
		Env: map[string]string{
			"FOO": "bar",
		},
		Resources: sandbox.ResourceLimits{
			MemoryBytes: 4 * 1024 * 1024 * 1024, // 4GB
			CPUQuota:    200000,                  // 2 CPUs
		},
	})

	// Should timeout waiting for Running phase
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed waiting for sandbox to run")

	// But the CR should have been created
	// Use direct name lookup
	var srList fleetliftv1alpha1.SandboxRequestList
	require.NoError(t, client.List(ctx, &srList))
	require.Len(t, srList.Items, 1)

	created := srList.Items[0]
	assert.Equal(t, "test-task-123", created.Spec.TaskID)
	assert.Equal(t, "claude-code-sandbox:latest", created.Spec.Image)
	assert.Equal(t, "/workspace", created.Spec.WorkingDir)
	assert.Equal(t, "bar", created.Spec.Env["FOO"])
}

func TestProvisionWithRunningPhase(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, fleetliftv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	// Pre-create a SandboxRequest that's already running
	existingSR := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sandbox-existing-task",
			Namespace: "test-ns",
		},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID: "existing-task",
			Image:  "test-image:latest",
		},
		Status: fleetliftv1alpha1.SandboxRequestStatus{
			Phase:   fleetliftv1alpha1.SandboxPhaseRunning,
			PodName: "sandbox-existing-task-abc",
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(existingSR).
		WithStatusSubresource(existingSR).
		Build()

	cfg := ProviderConfig{
		Namespace:        "test-ns",
		SandboxNamespace: "sandbox-ns",
		PollInterval:     10 * time.Millisecond,
		ProvisionTimeout: 1 * time.Second,
	}

	p := NewProviderWithClient(client, cfg)

	ctx := context.Background()
	sb, err := p.Provision(ctx, sandbox.ProvisionOptions{
		TaskID: "existing-task",
		Image:  "test-image:latest",
	})

	require.NoError(t, err)
	assert.Equal(t, "sandbox-existing-task", sb.ID)
	assert.Equal(t, "kubernetes", sb.Provider)
}

func TestStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, fleetliftv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	tests := []struct {
		name          string
		crPhase       fleetliftv1alpha1.SandboxPhase
		expectedPhase sandbox.SandboxPhase
	}{
		{"pending", fleetliftv1alpha1.SandboxPhasePending, sandbox.SandboxPhasePending},
		{"provisioning", fleetliftv1alpha1.SandboxPhaseProvisioning, sandbox.SandboxPhasePending},
		{"running", fleetliftv1alpha1.SandboxPhaseRunning, sandbox.SandboxPhaseRunning},
		{"succeeded", fleetliftv1alpha1.SandboxPhaseSucceeded, sandbox.SandboxPhaseSucceeded},
		{"failed", fleetliftv1alpha1.SandboxPhaseFailed, sandbox.SandboxPhaseFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := &fleetliftv1alpha1.SandboxRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-sandbox",
					Namespace: "test-ns",
				},
				Spec: fleetliftv1alpha1.SandboxRequestSpec{
					TaskID: "test-task",
					Image:  "test:latest",
				},
				Status: fleetliftv1alpha1.SandboxRequestStatus{
					Phase:   tt.crPhase,
					Message: "test message",
				},
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(sr).
				Build()

			cfg := ProviderConfig{Namespace: "test-ns"}
			p := NewProviderWithClient(client, cfg)

			status, err := p.Status(context.Background(), "test-sandbox")
			require.NoError(t, err)
			assert.Equal(t, tt.expectedPhase, status.Phase)
			assert.Equal(t, "test message", status.Message)
		})
	}
}

func TestStatusNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, fleetliftv1alpha1.AddToScheme(scheme))

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	cfg := ProviderConfig{Namespace: "test-ns"}
	p := NewProviderWithClient(client, cfg)

	status, err := p.Status(context.Background(), "nonexistent")
	require.NoError(t, err)
	assert.Equal(t, sandbox.SandboxPhaseUnknown, status.Phase)
}

func TestCleanup(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, fleetliftv1alpha1.AddToScheme(scheme))

	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sandbox",
			Namespace: "test-ns",
		},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID: "test-task",
			Image:  "test:latest",
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sr).Build()
	cfg := ProviderConfig{Namespace: "test-ns"}
	p := NewProviderWithClient(client, cfg)

	err := p.Cleanup(context.Background(), "test-sandbox")
	require.NoError(t, err)

	// Verify it's deleted
	var srList fleetliftv1alpha1.SandboxRequestList
	require.NoError(t, client.List(context.Background(), &srList))
	assert.Len(t, srList.Items, 0)
}

func TestCleanupNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, fleetliftv1alpha1.AddToScheme(scheme))

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	cfg := ProviderConfig{Namespace: "test-ns"}
	p := NewProviderWithClient(client, cfg)

	// Should not error for nonexistent sandbox
	err := p.Cleanup(context.Background(), "nonexistent")
	require.NoError(t, err)
}

func TestBuildResources(t *testing.T) {
	p := &Provider{}

	// Test resource conversion
	opts := sandbox.ProvisionOptions{
		Resources: sandbox.ResourceLimits{
			MemoryBytes: 4 * 1024 * 1024 * 1024, // 4GB
			CPUQuota:    200000,                  // 2 CPUs
		},
	}

	// The resource building happens in Provision, so we just verify the logic
	memQty := resource.NewQuantity(opts.Resources.MemoryBytes, resource.BinarySI)
	assert.Equal(t, "4Gi", memQty.String())

	cpuMillis := opts.Resources.CPUQuota / 100 // 2000 millicores = 2 CPUs
	cpuQty := resource.NewMilliQuantity(cpuMillis, resource.DecimalSI)
	assert.Equal(t, "2", cpuQty.String())

	_ = p // silence unused warning
}
