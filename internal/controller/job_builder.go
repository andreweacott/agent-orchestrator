package controller

import (
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetliftv1alpha1 "github.com/tinkerloft/fleetlift/api/v1alpha1"
)

const (
	// DefaultMemoryLimit is the default memory limit for sandbox pods
	DefaultMemoryLimit = "4Gi"
	// DefaultCPULimit is the default CPU limit for sandbox pods
	DefaultCPULimit = "2"
	// ContainerName is the name of the sandbox container
	ContainerName = "sandbox"
	// SandboxUserID is the non-root user ID for sandbox containers
	SandboxUserID = 65532
	// SandboxGroupID is the group ID for sandbox containers
	SandboxGroupID = 65532
)

// BuildJob creates a Job spec from a SandboxRequest
func BuildJob(sr *fleetliftv1alpha1.SandboxRequest, namespace string) *batchv1.Job {
	// Build labels
	labels := map[string]string{
		"app.kubernetes.io/name":       "fleetlift-sandbox",
		"app.kubernetes.io/instance":   sr.Name,
		"app.kubernetes.io/component":  "sandbox",
		"app.kubernetes.io/managed-by": "fleetlift-controller",
		"fleetlift.io/task-id":         sr.Spec.TaskID,
	}

	// Build container spec
	container := buildContainer(sr)

	// Build pod spec
	podSpec := buildPodSpec(sr, container)

	// Build job spec
	var backoffLimit int32 = 0
	var ttlSeconds *int32
	if sr.Spec.TTLSecondsAfterFinished != nil {
		ttlSeconds = sr.Spec.TTLSecondsAfterFinished
	} else {
		// Default TTL: 1 hour
		defaultTTL := int32(3600)
		ttlSeconds = &defaultTTL
	}

	// Sanitize taskID for K8s name compatibility (max 63 chars total, minus "sandbox-" prefix = 55)
	sanitizedTaskID := SanitizeK8sName(sr.Spec.TaskID, 55)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("sandbox-%s", sanitizedTaskID),
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: ttlSeconds,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: podSpec,
			},
		},
	}

	return job
}

// buildContainer creates the container spec for the sandbox
func buildContainer(sr *fleetliftv1alpha1.SandboxRequest) corev1.Container {
	// Build environment variables
	env := make([]corev1.EnvVar, 0, len(sr.Spec.Env))
	for k, v := range sr.Spec.Env {
		env = append(env, corev1.EnvVar{
			Name:  k,
			Value: v,
		})
	}

	// Add credential environment variables
	for _, cred := range sr.Spec.Credentials {
		if cred.EnvVar != "" {
			env = append(env, corev1.EnvVar{
				Name: cred.EnvVar,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: cred.Name,
						},
						Key: cred.SecretKey,
					},
				},
			})
		}
	}

	// Build resource requirements
	resources := buildResources(sr)

	container := corev1.Container{
		Name:  ContainerName,
		Image: sr.Spec.Image,
		// Keep container running with tail -f so we can exec into it
		Command: []string{"sh", "-c", "touch /tmp/claude-code.log && tail -f /tmp/claude-code.log"},
		Env:     env,
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: boolPtr(false),
			ReadOnlyRootFilesystem:   boolPtr(false), // Need writable filesystem for workspace
			RunAsNonRoot:             boolPtr(true),
			RunAsUser:                int64Ptr(SandboxUserID),
			RunAsGroup:               int64Ptr(SandboxGroupID),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
		},
		Resources: resources,
	}

	// Set working directory if specified
	if sr.Spec.WorkingDir != "" {
		container.WorkingDir = sr.Spec.WorkingDir
	}

	// Add volume mounts for credentials with mount paths
	volumeMounts := []corev1.VolumeMount{}
	for _, cred := range sr.Spec.Credentials {
		if cred.MountPath != "" {
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      fmt.Sprintf("cred-%s", cred.Name),
				MountPath: cred.MountPath,
				ReadOnly:  true,
			})
		}
	}
	if len(volumeMounts) > 0 {
		container.VolumeMounts = volumeMounts
	}

	return container
}

// buildPodSpec creates the pod spec for the sandbox
func buildPodSpec(sr *fleetliftv1alpha1.SandboxRequest, container corev1.Container) corev1.PodSpec {
	podSpec := corev1.PodSpec{
		Containers:    []corev1.Container{container},
		RestartPolicy: corev1.RestartPolicyNever,
		// Disable service account token mount - sandboxes don't need K8s API access
		AutomountServiceAccountToken: boolPtr(false),
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot: boolPtr(true),
			RunAsUser:    int64Ptr(SandboxUserID),
			RunAsGroup:   int64Ptr(SandboxGroupID),
			FSGroup:      int64Ptr(SandboxGroupID),
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
	}

	// Set runtime class if specified (e.g., gvisor)
	if sr.Spec.RuntimeClassName != "" {
		podSpec.RuntimeClassName = &sr.Spec.RuntimeClassName
	}

	// Set node selector if specified
	if len(sr.Spec.NodeSelector) > 0 {
		podSpec.NodeSelector = sr.Spec.NodeSelector
	}

	// Add volumes for credential mounts
	volumes := []corev1.Volume{}
	for _, cred := range sr.Spec.Credentials {
		if cred.MountPath != "" {
			volumes = append(volumes, corev1.Volume{
				Name: fmt.Sprintf("cred-%s", cred.Name),
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: cred.Name,
					},
				},
			})
		}
	}
	if len(volumes) > 0 {
		podSpec.Volumes = volumes
	}

	return podSpec
}

// buildResources creates the resource requirements for the container
func buildResources(sr *fleetliftv1alpha1.SandboxRequest) corev1.ResourceRequirements {
	resources := corev1.ResourceRequirements{
		Limits:   make(corev1.ResourceList),
		Requests: make(corev1.ResourceList),
	}

	// Use spec limits if provided, otherwise use defaults
	if sr.Spec.Resources.Limits != nil {
		for k, v := range sr.Spec.Resources.Limits {
			resources.Limits[k] = v
		}
	}
	if _, ok := resources.Limits[corev1.ResourceMemory]; !ok {
		resources.Limits[corev1.ResourceMemory] = resource.MustParse(DefaultMemoryLimit)
	}
	if _, ok := resources.Limits[corev1.ResourceCPU]; !ok {
		resources.Limits[corev1.ResourceCPU] = resource.MustParse(DefaultCPULimit)
	}

	// Use spec requests if provided, otherwise use half of limits
	if sr.Spec.Resources.Requests != nil {
		for k, v := range sr.Spec.Resources.Requests {
			resources.Requests[k] = v
		}
	}
	if _, ok := resources.Requests[corev1.ResourceMemory]; !ok {
		// Default request to 1Gi
		resources.Requests[corev1.ResourceMemory] = resource.MustParse("1Gi")
	}
	if _, ok := resources.Requests[corev1.ResourceCPU]; !ok {
		// Default request to 500m
		resources.Requests[corev1.ResourceCPU] = resource.MustParse("500m")
	}

	return resources
}

func boolPtr(b bool) *bool {
	return &b
}

func int64Ptr(i int64) *int64 {
	return &i
}

// SanitizeK8sName converts a string to a valid Kubernetes object name.
// Kubernetes names must match: [a-z0-9]([-a-z0-9]*[a-z0-9])?
// This function:
// - Converts to lowercase
// - Replaces invalid characters with dashes
// - Trims leading/trailing dashes
// - Truncates to maxLen characters
func SanitizeK8sName(name string, maxLen int) string {
	// Convert to lowercase
	name = strings.ToLower(name)

	// Replace invalid characters with dashes
	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		} else {
			result.WriteRune('-')
		}
	}

	// Trim leading/trailing dashes
	sanitized := strings.Trim(result.String(), "-")

	// Collapse multiple consecutive dashes into one
	for strings.Contains(sanitized, "--") {
		sanitized = strings.ReplaceAll(sanitized, "--", "-")
	}

	// Truncate if needed
	if len(sanitized) > maxLen {
		sanitized = sanitized[:maxLen]
		// Trim trailing dash after truncation
		sanitized = strings.TrimRight(sanitized, "-")
	}

	return sanitized
}
