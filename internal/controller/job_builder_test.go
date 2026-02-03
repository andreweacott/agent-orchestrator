package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetliftv1alpha1 "github.com/tinkerloft/fleetlift/api/v1alpha1"
)

func TestBuildJob(t *testing.T) {
	tests := []struct {
		name      string
		sr        *fleetliftv1alpha1.SandboxRequest
		namespace string
		verify    func(t *testing.T, job interface{})
	}{
		{
			name: "minimal spec with defaults",
			sr: &fleetliftv1alpha1.SandboxRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-sandbox",
					Namespace: "fleetlift-system",
				},
				Spec: fleetliftv1alpha1.SandboxRequestSpec{
					TaskID: "task-123",
					Image:  "claude-code-sandbox:latest",
				},
			},
			namespace: "sandbox-isolated",
			verify: func(t *testing.T, jobI interface{}) {
				job := jobI.(*struct {
					name          string
					namespace     string
					ttl           int32
					runAsUser     int64
					runAsNonRoot  bool
					memoryLimit   string
					cpuLimit      string
					memoryRequest string
					cpuRequest    string
				})
				assert.Equal(t, "sandbox-task-123", job.name)
				assert.Equal(t, "sandbox-isolated", job.namespace)
				assert.Equal(t, int32(3600), job.ttl)
				assert.Equal(t, int64(SandboxUserID), job.runAsUser)
				assert.True(t, job.runAsNonRoot)
				assert.Equal(t, DefaultMemoryLimit, job.memoryLimit)
				assert.Equal(t, DefaultCPULimit, job.cpuLimit)
				assert.Equal(t, "1Gi", job.memoryRequest)
				assert.Equal(t, "500m", job.cpuRequest)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := BuildJob(tt.sr, tt.namespace)

			// Verify common fields (taskID is sanitized for K8s naming)
			expectedName := "sandbox-" + SanitizeK8sName(tt.sr.Spec.TaskID, 55)
			assert.Equal(t, expectedName, job.Name)
			assert.Equal(t, tt.namespace, job.Namespace)
			assert.Equal(t, int32(0), *job.Spec.BackoffLimit)

			// Verify labels
			assert.Equal(t, "fleetlift-sandbox", job.Labels["app.kubernetes.io/name"])
			assert.Equal(t, tt.sr.Name, job.Labels["app.kubernetes.io/instance"])
			assert.Equal(t, tt.sr.Spec.TaskID, job.Labels["fleetlift.io/task-id"])
		})
	}
}

func TestBuildJob_DefaultTTL(t *testing.T) {
	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID: "task-1",
			Image:  "test:latest",
		},
	}

	job := BuildJob(sr, "test-ns")

	require.NotNil(t, job.Spec.TTLSecondsAfterFinished)
	assert.Equal(t, int32(3600), *job.Spec.TTLSecondsAfterFinished)
}

func TestBuildJob_CustomTTL(t *testing.T) {
	customTTL := int32(7200)
	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID:                  "task-1",
			Image:                   "test:latest",
			TTLSecondsAfterFinished: &customTTL,
		},
	}

	job := BuildJob(sr, "test-ns")

	require.NotNil(t, job.Spec.TTLSecondsAfterFinished)
	assert.Equal(t, int32(7200), *job.Spec.TTLSecondsAfterFinished)
}

func TestBuildJob_SecurityContext(t *testing.T) {
	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID: "task-1",
			Image:  "test:latest",
		},
	}

	job := BuildJob(sr, "test-ns")

	// Pod security context
	podSecCtx := job.Spec.Template.Spec.SecurityContext
	require.NotNil(t, podSecCtx)
	assert.True(t, *podSecCtx.RunAsNonRoot)
	assert.Equal(t, int64(SandboxUserID), *podSecCtx.RunAsUser)
	assert.Equal(t, int64(SandboxGroupID), *podSecCtx.RunAsGroup)
	assert.Equal(t, int64(SandboxGroupID), *podSecCtx.FSGroup)
	assert.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, podSecCtx.SeccompProfile.Type)

	// Container security context
	require.Len(t, job.Spec.Template.Spec.Containers, 1)
	container := job.Spec.Template.Spec.Containers[0]
	require.NotNil(t, container.SecurityContext)
	assert.False(t, *container.SecurityContext.AllowPrivilegeEscalation)
	assert.True(t, *container.SecurityContext.RunAsNonRoot)
	assert.Equal(t, int64(SandboxUserID), *container.SecurityContext.RunAsUser)
	assert.Equal(t, int64(SandboxGroupID), *container.SecurityContext.RunAsGroup)
	require.NotNil(t, container.SecurityContext.Capabilities)
	assert.Contains(t, container.SecurityContext.Capabilities.Drop, corev1.Capability("ALL"))
}

func TestBuildJob_CustomResources(t *testing.T) {
	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID: "task-1",
			Image:  "test:latest",
			Resources: fleetliftv1alpha1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("8Gi"),
					corev1.ResourceCPU:    resource.MustParse("4"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("2Gi"),
					corev1.ResourceCPU:    resource.MustParse("1"),
				},
			},
		},
	}

	job := BuildJob(sr, "test-ns")

	container := job.Spec.Template.Spec.Containers[0]
	assert.Equal(t, resource.MustParse("8Gi"), container.Resources.Limits[corev1.ResourceMemory])
	assert.Equal(t, resource.MustParse("4"), container.Resources.Limits[corev1.ResourceCPU])
	assert.Equal(t, resource.MustParse("2Gi"), container.Resources.Requests[corev1.ResourceMemory])
	assert.Equal(t, resource.MustParse("1"), container.Resources.Requests[corev1.ResourceCPU])
}

func TestBuildJob_DefaultResources(t *testing.T) {
	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID: "task-1",
			Image:  "test:latest",
		},
	}

	job := BuildJob(sr, "test-ns")

	container := job.Spec.Template.Spec.Containers[0]
	assert.Equal(t, resource.MustParse(DefaultMemoryLimit), container.Resources.Limits[corev1.ResourceMemory])
	assert.Equal(t, resource.MustParse(DefaultCPULimit), container.Resources.Limits[corev1.ResourceCPU])
	assert.Equal(t, resource.MustParse("1Gi"), container.Resources.Requests[corev1.ResourceMemory])
	assert.Equal(t, resource.MustParse("500m"), container.Resources.Requests[corev1.ResourceCPU])
}

func TestBuildJob_CredentialsAsEnvVars(t *testing.T) {
	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID: "task-1",
			Image:  "test:latest",
			Credentials: []fleetliftv1alpha1.CredentialRef{
				{
					Name:      "github-token-secret",
					EnvVar:    "GITHUB_TOKEN",
					SecretKey: "token",
				},
				{
					Name:      "anthropic-secret",
					EnvVar:    "ANTHROPIC_API_KEY",
					SecretKey: "api-key",
				},
			},
		},
	}

	job := BuildJob(sr, "test-ns")

	container := job.Spec.Template.Spec.Containers[0]

	// Find the credential env vars
	var ghEnv, anthropicEnv *corev1.EnvVar
	for i := range container.Env {
		if container.Env[i].Name == "GITHUB_TOKEN" {
			ghEnv = &container.Env[i]
		}
		if container.Env[i].Name == "ANTHROPIC_API_KEY" {
			anthropicEnv = &container.Env[i]
		}
	}

	require.NotNil(t, ghEnv)
	require.NotNil(t, ghEnv.ValueFrom)
	require.NotNil(t, ghEnv.ValueFrom.SecretKeyRef)
	assert.Equal(t, "github-token-secret", ghEnv.ValueFrom.SecretKeyRef.Name)
	assert.Equal(t, "token", ghEnv.ValueFrom.SecretKeyRef.Key)

	require.NotNil(t, anthropicEnv)
	require.NotNil(t, anthropicEnv.ValueFrom)
	require.NotNil(t, anthropicEnv.ValueFrom.SecretKeyRef)
	assert.Equal(t, "anthropic-secret", anthropicEnv.ValueFrom.SecretKeyRef.Name)
	assert.Equal(t, "api-key", anthropicEnv.ValueFrom.SecretKeyRef.Key)
}

func TestBuildJob_CredentialsWithMountPaths(t *testing.T) {
	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID: "task-1",
			Image:  "test:latest",
			Credentials: []fleetliftv1alpha1.CredentialRef{
				{
					Name:      "ssh-key-secret",
					MountPath: "/root/.ssh",
				},
				{
					Name:      "config-secret",
					MountPath: "/etc/myconfig",
				},
			},
		},
	}

	job := BuildJob(sr, "test-ns")

	container := job.Spec.Template.Spec.Containers[0]
	podSpec := job.Spec.Template.Spec

	// Verify volume mounts on container
	require.Len(t, container.VolumeMounts, 2)

	var sshMount, configMount *corev1.VolumeMount
	for i := range container.VolumeMounts {
		if container.VolumeMounts[i].MountPath == "/root/.ssh" {
			sshMount = &container.VolumeMounts[i]
		}
		if container.VolumeMounts[i].MountPath == "/etc/myconfig" {
			configMount = &container.VolumeMounts[i]
		}
	}

	require.NotNil(t, sshMount)
	assert.Equal(t, "cred-ssh-key-secret", sshMount.Name)
	assert.True(t, sshMount.ReadOnly)

	require.NotNil(t, configMount)
	assert.Equal(t, "cred-config-secret", configMount.Name)
	assert.True(t, configMount.ReadOnly)

	// Verify volumes on pod spec
	require.Len(t, podSpec.Volumes, 2)

	var sshVol, configVol *corev1.Volume
	for i := range podSpec.Volumes {
		if podSpec.Volumes[i].Name == "cred-ssh-key-secret" {
			sshVol = &podSpec.Volumes[i]
		}
		if podSpec.Volumes[i].Name == "cred-config-secret" {
			configVol = &podSpec.Volumes[i]
		}
	}

	require.NotNil(t, sshVol)
	require.NotNil(t, sshVol.Secret)
	assert.Equal(t, "ssh-key-secret", sshVol.Secret.SecretName)

	require.NotNil(t, configVol)
	require.NotNil(t, configVol.Secret)
	assert.Equal(t, "config-secret", configVol.Secret.SecretName)
}

func TestBuildJob_RuntimeClassName(t *testing.T) {
	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID:           "task-1",
			Image:            "test:latest",
			RuntimeClassName: "gvisor",
		},
	}

	job := BuildJob(sr, "test-ns")

	require.NotNil(t, job.Spec.Template.Spec.RuntimeClassName)
	assert.Equal(t, "gvisor", *job.Spec.Template.Spec.RuntimeClassName)
}

func TestBuildJob_NodeSelector(t *testing.T) {
	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID: "task-1",
			Image:  "test:latest",
			NodeSelector: map[string]string{
				"workload-type":       "sandbox",
				"kubernetes.io/arch":  "amd64",
			},
		},
	}

	job := BuildJob(sr, "test-ns")

	assert.Len(t, job.Spec.Template.Spec.NodeSelector, 2)
	assert.Equal(t, "sandbox", job.Spec.Template.Spec.NodeSelector["workload-type"])
	assert.Equal(t, "amd64", job.Spec.Template.Spec.NodeSelector["kubernetes.io/arch"])
}

func TestBuildJob_WorkingDir(t *testing.T) {
	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID:     "task-1",
			Image:      "test:latest",
			WorkingDir: "/workspace/myproject",
		},
	}

	job := BuildJob(sr, "test-ns")

	container := job.Spec.Template.Spec.Containers[0]
	assert.Equal(t, "/workspace/myproject", container.WorkingDir)
}

func TestBuildJob_EnvVars(t *testing.T) {
	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID: "task-1",
			Image:  "test:latest",
			Env: map[string]string{
				"FOO": "bar",
				"BAZ": "qux",
			},
		},
	}

	job := BuildJob(sr, "test-ns")

	container := job.Spec.Template.Spec.Containers[0]

	envMap := make(map[string]string)
	for _, env := range container.Env {
		envMap[env.Name] = env.Value
	}

	assert.Equal(t, "bar", envMap["FOO"])
	assert.Equal(t, "qux", envMap["BAZ"])
}

func TestBuildJob_ContainerName(t *testing.T) {
	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID: "task-1",
			Image:  "test:latest",
		},
	}

	job := BuildJob(sr, "test-ns")

	require.Len(t, job.Spec.Template.Spec.Containers, 1)
	assert.Equal(t, ContainerName, job.Spec.Template.Spec.Containers[0].Name)
	assert.Equal(t, "test:latest", job.Spec.Template.Spec.Containers[0].Image)
}

func TestBuildJob_AutomountServiceAccountTokenDisabled(t *testing.T) {
	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID: "task-1",
			Image:  "test:latest",
		},
	}

	job := BuildJob(sr, "test-ns")

	require.NotNil(t, job.Spec.Template.Spec.AutomountServiceAccountToken)
	assert.False(t, *job.Spec.Template.Spec.AutomountServiceAccountToken)
}

func TestBuildJob_RestartPolicy(t *testing.T) {
	sr := &fleetliftv1alpha1.SandboxRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: fleetliftv1alpha1.SandboxRequestSpec{
			TaskID: "task-1",
			Image:  "test:latest",
		},
	}

	job := BuildJob(sr, "test-ns")

	assert.Equal(t, corev1.RestartPolicyNever, job.Spec.Template.Spec.RestartPolicy)
}

func TestSanitizeK8sName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "already valid lowercase",
			input:    "task-123",
			maxLen:   55,
			expected: "task-123",
		},
		{
			name:     "uppercase converted to lowercase",
			input:    "Task-ABC",
			maxLen:   55,
			expected: "task-abc",
		},
		{
			name:     "underscores replaced with dashes",
			input:    "task_123_abc",
			maxLen:   55,
			expected: "task-123-abc",
		},
		{
			name:     "special characters replaced",
			input:    "task.123@abc!def",
			maxLen:   55,
			expected: "task-123-abc-def",
		},
		{
			name:     "leading dashes trimmed",
			input:    "---task-123",
			maxLen:   55,
			expected: "task-123",
		},
		{
			name:     "trailing dashes trimmed",
			input:    "task-123---",
			maxLen:   55,
			expected: "task-123",
		},
		{
			name:     "consecutive dashes collapsed",
			input:    "task---123",
			maxLen:   55,
			expected: "task-123",
		},
		{
			name:     "truncated to max length",
			input:    "this-is-a-very-long-task-id-that-exceeds-the-maximum-allowed-length",
			maxLen:   20,
			expected: "this-is-a-very-long",
		},
		{
			name:     "truncation removes trailing dash",
			input:    "task-1234567890-abcd",
			maxLen:   15,
			expected: "task-1234567890",
		},
		{
			name:     "mixed invalid characters",
			input:    "Task_123.ABC@def!",
			maxLen:   55,
			expected: "task-123-abc-def",
		},
		{
			name:     "only numbers",
			input:    "12345",
			maxLen:   55,
			expected: "12345",
		},
		{
			name:     "uuid format preserved",
			input:    "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
			maxLen:   55,
			expected: "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeK8sName(tt.input, tt.maxLen)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildJob_TaskIDSanitization(t *testing.T) {
	tests := []struct {
		name         string
		taskID       string
		expectedName string
	}{
		{
			name:         "valid taskID unchanged",
			taskID:       "task-123",
			expectedName: "sandbox-task-123",
		},
		{
			name:         "uppercase taskID sanitized",
			taskID:       "Task-ABC",
			expectedName: "sandbox-task-abc",
		},
		{
			name:         "underscores in taskID sanitized",
			taskID:       "task_123_abc",
			expectedName: "sandbox-task-123-abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := &fleetliftv1alpha1.SandboxRequest{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Spec: fleetliftv1alpha1.SandboxRequestSpec{
					TaskID: tt.taskID,
					Image:  "test:latest",
				},
			}

			job := BuildJob(sr, "test-ns")
			assert.Equal(t, tt.expectedName, job.Name)
		})
	}
}
