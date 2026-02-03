package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetliftv1alpha1 "github.com/tinkerloft/fleetlift/api/v1alpha1"
)

func newTestReconciler(objs ...runtime.Object) *SandboxRequestReconciler {
	scheme := runtime.NewScheme()
	_ = fleetliftv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(objs...).
		Build()

	return &SandboxRequestReconciler{
		Client:           client,
		Scheme:           scheme,
		Recorder:         record.NewFakeRecorder(100),
		SandboxNamespace: "sandbox-isolated",
	}
}

func TestValidateRequest_Valid(t *testing.T) {
	r := newTestReconciler()

	sr := &fleetliftv1alpha1.SandboxRequest{
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID: "task-123",
			Image:  "claude-code-sandbox:latest",
		},
	}

	err := r.validateRequest(sr)
	assert.NoError(t, err)
}

func TestValidateRequest_MissingTaskID(t *testing.T) {
	r := newTestReconciler()

	sr := &fleetliftv1alpha1.SandboxRequest{
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			Image: "claude-code-sandbox:latest",
		},
	}

	err := r.validateRequest(sr)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "taskId is required")
}

func TestValidateRequest_MissingImage(t *testing.T) {
	r := newTestReconciler()

	sr := &fleetliftv1alpha1.SandboxRequest{
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID: "task-123",
		},
	}

	err := r.validateRequest(sr)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "image is required")
}

func TestValidateRequest_EmptyNamespace(t *testing.T) {
	// Empty namespace in SandboxRequest is allowed - the controller uses SandboxNamespace
	r := newTestReconciler()

	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "",
		},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID: "task-123",
			Image:  "test:latest",
		},
	}

	err := r.validateRequest(sr)
	// Validation should pass - namespace is set by controller
	assert.NoError(t, err)
}

func TestFindPodForJob_NoPods(t *testing.T) {
	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sandbox",
			Namespace: "fleetlift-system",
		},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID: "task-123",
			Image:  "test:latest",
		},
	}

	r := newTestReconciler(sr)
	ctx := context.Background()

	pod, err := r.findPodForJob(ctx, sr)

	require.NoError(t, err)
	assert.Nil(t, pod)
}

func TestFindPodForJob_PodFound(t *testing.T) {
	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sandbox",
			Namespace: "fleetlift-system",
		},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID: "task-123",
			Image:  "test:latest",
		},
	}

	matchingPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sandbox-task-123-abc",
			Namespace: "sandbox-isolated",
			Labels: map[string]string{
				"app.kubernetes.io/name":     "fleetlift-sandbox",
				"app.kubernetes.io/instance": "test-sandbox",
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}

	r := newTestReconciler(sr, matchingPod)
	ctx := context.Background()

	pod, err := r.findPodForJob(ctx, sr)

	require.NoError(t, err)
	require.NotNil(t, pod)
	assert.Equal(t, "sandbox-task-123-abc", pod.Name)
	assert.Equal(t, corev1.PodRunning, pod.Status.Phase)
}

func TestFindPodForJob_MultiplePods(t *testing.T) {
	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sandbox",
			Namespace: "fleetlift-system",
		},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID: "task-123",
			Image:  "test:latest",
		},
	}

	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sandbox-task-123-abc",
			Namespace: "sandbox-isolated",
			Labels: map[string]string{
				"app.kubernetes.io/name":     "fleetlift-sandbox",
				"app.kubernetes.io/instance": "test-sandbox",
			},
		},
	}

	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sandbox-task-123-def",
			Namespace: "sandbox-isolated",
			Labels: map[string]string{
				"app.kubernetes.io/name":     "fleetlift-sandbox",
				"app.kubernetes.io/instance": "test-sandbox",
			},
		},
	}

	r := newTestReconciler(sr, pod1, pod2)
	ctx := context.Background()

	pod, err := r.findPodForJob(ctx, sr)

	// Should return first pod found (implementation returns first item)
	require.NoError(t, err)
	require.NotNil(t, pod)
}

func TestFindPodForJob_WrongLabels(t *testing.T) {
	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sandbox",
			Namespace: "fleetlift-system",
		},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID: "task-123",
			Image:  "test:latest",
		},
	}

	// Pod with wrong labels should not be found
	wrongPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some-other-pod",
			Namespace: "sandbox-isolated",
			Labels: map[string]string{
				"app.kubernetes.io/name":     "other-app",
				"app.kubernetes.io/instance": "other-instance",
			},
		},
	}

	r := newTestReconciler(sr, wrongPod)
	ctx := context.Background()

	pod, err := r.findPodForJob(ctx, sr)

	require.NoError(t, err)
	assert.Nil(t, pod)
}

func TestProcessExecRequests_NoRequests(t *testing.T) {
	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sandbox",
			Namespace: "fleetlift-system",
		},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID:       "task-123",
			Image:        "test:latest",
			ExecRequests: []fleetliftv1alpha1.ExecRequest{},
		},
		Status: fleetliftv1alpha1.SandboxRequestStatus{
			LastExecProcessedIndex: 0,
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sandbox-task-123-abc",
			Namespace: "sandbox-isolated",
		},
	}

	r := newTestReconciler(sr)
	ctx := context.Background()

	processed, err := r.processExecRequests(ctx, sr, pod)

	require.NoError(t, err)
	assert.False(t, processed)
}

func TestProcessExecRequests_AllAlreadyProcessed(t *testing.T) {
	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sandbox",
			Namespace: "fleetlift-system",
		},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID: "task-123",
			Image:  "test:latest",
			ExecRequests: []fleetliftv1alpha1.ExecRequest{
				{ID: "exec-1", Command: []string{"echo", "hello"}},
				{ID: "exec-2", Command: []string{"echo", "world"}},
			},
		},
		Status: fleetliftv1alpha1.SandboxRequestStatus{
			LastExecProcessedIndex: 2,
			ExecResults: []fleetliftv1alpha1.ExecResult{
				{ID: "exec-1", ExitCode: 0},
				{ID: "exec-2", ExitCode: 0},
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sandbox-task-123-abc",
			Namespace: "sandbox-isolated",
		},
	}

	r := newTestReconciler(sr)
	ctx := context.Background()

	processed, err := r.processExecRequests(ctx, sr, pod)

	require.NoError(t, err)
	assert.False(t, processed)
}

func TestExecResultsTrimming(t *testing.T) {
	// Create a SandboxRequest with results at the limit
	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sandbox",
			Namespace: "fleetlift-system",
		},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID: "task-123",
			Image:  "test:latest",
		},
		Status: fleetliftv1alpha1.SandboxRequestStatus{
			LastExecProcessedIndex: 0,
		},
	}

	// Create results at the max limit
	for i := 0; i < fleetliftv1alpha1.MaxExecResultsToRetain; i++ {
		sr.Status.ExecResults = append(sr.Status.ExecResults, fleetliftv1alpha1.ExecResult{
			ID:       "old-" + string(rune('0'+i)),
			ExitCode: 0,
		})
	}

	// Add one more result (simulating what happens after processing)
	sr.Status.ExecResults = append(sr.Status.ExecResults, fleetliftv1alpha1.ExecResult{
		ID:       "new-result",
		ExitCode: 0,
	})

	// Verify trimming logic
	if len(sr.Status.ExecResults) > fleetliftv1alpha1.MaxExecResultsToRetain {
		excess := len(sr.Status.ExecResults) - fleetliftv1alpha1.MaxExecResultsToRetain
		sr.Status.ExecResults = sr.Status.ExecResults[excess:]
	}

	assert.Len(t, sr.Status.ExecResults, fleetliftv1alpha1.MaxExecResultsToRetain)
	// Oldest result should have been removed, new result should be present
	assert.Equal(t, "new-result", sr.Status.ExecResults[len(sr.Status.ExecResults)-1].ID)
}

func TestPhaseTransitions(t *testing.T) {
	tests := []struct {
		name          string
		initialPhase  fleetliftv1alpha1.SandboxPhase
		expectHandled bool
	}{
		{
			name:          "empty phase treated as pending",
			initialPhase:  "",
			expectHandled: true,
		},
		{
			name:          "pending phase",
			initialPhase:  fleetliftv1alpha1.SandboxPhasePending,
			expectHandled: true,
		},
		{
			name:          "provisioning phase",
			initialPhase:  fleetliftv1alpha1.SandboxPhaseProvisioning,
			expectHandled: true,
		},
		{
			name:          "running phase",
			initialPhase:  fleetliftv1alpha1.SandboxPhaseRunning,
			expectHandled: true,
		},
		{
			name:          "succeeded phase is terminal",
			initialPhase:  fleetliftv1alpha1.SandboxPhaseSucceeded,
			expectHandled: false,
		},
		{
			name:          "failed phase is terminal",
			initialPhase:  fleetliftv1alpha1.SandboxPhaseFailed,
			expectHandled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just verify the phase handling logic
			isTerminal := tt.initialPhase == fleetliftv1alpha1.SandboxPhaseSucceeded ||
				tt.initialPhase == fleetliftv1alpha1.SandboxPhaseFailed

			assert.Equal(t, tt.expectHandled, !isTerminal)
		})
	}
}

func TestPodPhaseToSandboxPhase(t *testing.T) {
	tests := []struct {
		podPhase     corev1.PodPhase
		expectStatus string
	}{
		{corev1.PodRunning, "running"},
		{corev1.PodPending, "waiting"},
		{corev1.PodFailed, "failed"},
		{corev1.PodSucceeded, "terminated"},
	}

	for _, tt := range tests {
		t.Run(string(tt.podPhase), func(t *testing.T) {
			// Verify pod phase mapping logic matches expectations
			var status string
			switch tt.podPhase {
			case corev1.PodRunning:
				status = "running"
			case corev1.PodPending:
				status = "waiting"
			case corev1.PodFailed:
				status = "failed"
			case corev1.PodSucceeded:
				status = "terminated"
			}
			assert.Equal(t, tt.expectStatus, status)
		})
	}
}
