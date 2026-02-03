package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SandboxRequestSpec defines the desired state of a sandbox
type SandboxRequestSpec struct {
	// TaskID is the unique identifier for this sandbox task
	TaskID string `json:"taskId"`

	// Image is the container image to use for the sandbox
	Image string `json:"image"`

	// WorkingDir is the working directory inside the container
	// +optional
	WorkingDir string `json:"workingDir,omitempty"`

	// Env contains environment variables to set in the sandbox
	// +optional
	Env map[string]string `json:"env,omitempty"`

	// Resources defines resource limits for the sandbox
	// +optional
	Resources ResourceRequirements `json:"resources,omitempty"`

	// RuntimeClassName specifies the RuntimeClass (e.g., gvisor)
	// +optional
	RuntimeClassName string `json:"runtimeClassName,omitempty"`

	// NodeSelector for pod scheduling
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Credentials references secrets to mount in the sandbox
	// +optional
	Credentials []CredentialRef `json:"credentials,omitempty"`

	// ExecRequests contains exec requests from the worker
	// Worker appends new requests, controller processes them
	// +optional
	ExecRequests []ExecRequest `json:"execRequests,omitempty"`

	// TTLSecondsAfterFinished specifies how long to keep the sandbox after completion
	// +optional
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty"`
}

// ResourceRequirements defines resource limits for the sandbox
type ResourceRequirements struct {
	// Limits describes the maximum amount of compute resources allowed
	// +optional
	Limits corev1.ResourceList `json:"limits,omitempty"`

	// Requests describes the minimum amount of compute resources required
	// +optional
	Requests corev1.ResourceList `json:"requests,omitempty"`
}

// CredentialRef references a secret to mount in the sandbox
type CredentialRef struct {
	// Name of the secret
	Name string `json:"name"`

	// MountPath is the path where the secret should be mounted
	// If not specified, secret is injected as environment variables
	// +optional
	MountPath string `json:"mountPath,omitempty"`

	// EnvVar is the environment variable name to use (for single-key secrets)
	// +optional
	EnvVar string `json:"envVar,omitempty"`

	// SecretKey is the specific key from the secret to use
	// +optional
	SecretKey string `json:"secretKey,omitempty"`
}

// ExecRequest represents a command execution request from the worker
type ExecRequest struct {
	// ID is a unique identifier for this exec request (UUID)
	ID string `json:"id"`

	// Command is the command and arguments to execute
	Command []string `json:"command"`

	// WorkingDir is the working directory for the command
	// +optional
	WorkingDir string `json:"workingDir,omitempty"`

	// User to run the command as
	// +optional
	User string `json:"user,omitempty"`

	// Env contains additional environment variables for this command
	// +optional
	Env map[string]string `json:"env,omitempty"`

	// TimeoutSeconds is the maximum time allowed for the command (default 300, max 1800)
	// +optional
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`
}

// SandboxRequestStatus defines the observed state of a SandboxRequest
type SandboxRequestStatus struct {
	// Phase represents the current lifecycle phase of the sandbox
	// +optional
	Phase SandboxPhase `json:"phase,omitempty"`

	// JobName is the name of the Job created for this sandbox
	// +optional
	JobName string `json:"jobName,omitempty"`

	// PodName is the name of the Pod running the sandbox
	// +optional
	PodName string `json:"podName,omitempty"`

	// StartTime is when the sandbox started running
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime is when the sandbox finished
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Message provides additional status information
	// +optional
	Message string `json:"message,omitempty"`

	// ExecResults contains results of processed exec requests
	// +optional
	ExecResults []ExecResult `json:"execResults,omitempty"`

	// LastExecProcessedIndex is the index of the last processed exec request
	// Controller uses this to track which requests have been handled
	// +optional
	LastExecProcessedIndex int `json:"lastExecProcessedIndex,omitempty"`

	// Conditions represent the latest available observations of the sandbox's state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// SandboxPhase represents the current lifecycle phase of a sandbox
// +kubebuilder:validation:Enum=Pending;Provisioning;Running;Succeeded;Failed
type SandboxPhase string

const (
	// SandboxPhasePending means the sandbox request has been accepted but not yet processed
	SandboxPhasePending SandboxPhase = "Pending"

	// SandboxPhaseProvisioning means the Job/Pod is being created
	SandboxPhaseProvisioning SandboxPhase = "Provisioning"

	// SandboxPhaseRunning means the sandbox pod is running and ready for exec requests
	SandboxPhaseRunning SandboxPhase = "Running"

	// SandboxPhaseSucceeded means the sandbox completed successfully
	SandboxPhaseSucceeded SandboxPhase = "Succeeded"

	// SandboxPhaseFailed means the sandbox failed
	SandboxPhaseFailed SandboxPhase = "Failed"
)

// ExecResult contains the result of an exec request
type ExecResult struct {
	// ID matches the ExecRequest ID
	ID string `json:"id"`

	// ExitCode is the command's exit code
	ExitCode int `json:"exitCode"`

	// Stdout contains the command's standard output (truncated to 1MB)
	// +optional
	Stdout string `json:"stdout,omitempty"`

	// Stderr contains the command's standard error (truncated to 1MB)
	// +optional
	Stderr string `json:"stderr,omitempty"`

	// Error contains any error message from the exec operation
	// +optional
	Error string `json:"error,omitempty"`

	// CompletedAt is when the exec completed
	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Pod",type=string,JSONPath=`.status.podName`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// SandboxRequest is the Schema for the sandboxrequests API
type SandboxRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SandboxRequestSpec   `json:"spec,omitempty"`
	Status SandboxRequestStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SandboxRequestList contains a list of SandboxRequest
type SandboxRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SandboxRequest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SandboxRequest{}, &SandboxRequestList{})
}

// Constants for exec request handling
const (
	// MaxExecOutputSize limits individual exec output to prevent etcd bloat.
	// etcd has a default 1.5MB object limit; we cap at 1MB per result to leave
	// room for metadata and multiple results. Output exceeding this limit is
	// truncated with a "... [truncated]" suffix.
	MaxExecOutputSize = 1024 * 1024

	// DefaultExecTimeoutSeconds is the default timeout for exec requests
	DefaultExecTimeoutSeconds = 300

	// MaxExecTimeoutSeconds is the maximum allowed timeout for exec requests
	MaxExecTimeoutSeconds = 1800

	// MaxExecResultsToRetain prevents unbounded growth of exec results in the CR.
	// Combined with output trimming in processExecRequests(), this ensures the CR
	// stays well under etcd's 1.5MB limit. Oldest results are removed when this
	// limit is exceeded. With 1MB max per result and typical results being much
	// smaller, 100 results provides good history while staying safe.
	MaxExecResultsToRetain = 100
)

// Condition types for SandboxRequest
const (
	// ConditionTypeReady indicates the sandbox is ready to accept exec requests
	ConditionTypeReady = "Ready"

	// ConditionTypeJobCreated indicates the Job has been created
	ConditionTypeJobCreated = "JobCreated"

	// ConditionTypePodRunning indicates the Pod is running
	ConditionTypePodRunning = "PodRunning"
)
