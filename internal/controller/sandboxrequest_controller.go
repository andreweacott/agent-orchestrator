// Package controller implements the Kubernetes controller for SandboxRequest resources.
package controller

import (
	"context"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	fleetliftv1alpha1 "github.com/tinkerloft/fleetlift/api/v1alpha1"
)

const (
	// ProvisioningTimeout is the maximum time allowed for a sandbox to start
	ProvisioningTimeout = 10 * time.Minute
)

// SandboxRequestReconciler reconciles a SandboxRequest object
type SandboxRequestReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	Recorder         record.EventRecorder
	SandboxNamespace string
	ExecHandler      *ExecHandler
}

// +kubebuilder:rbac:groups=fleetlift.io,resources=sandboxrequests,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=fleetlift.io,resources=sandboxrequests/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=fleetlift.io,resources=sandboxrequests/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=create
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile handles the reconciliation loop for SandboxRequest resources
func (r *SandboxRequestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the SandboxRequest
	var sandboxReq fleetliftv1alpha1.SandboxRequest
	if err := r.Get(ctx, req.NamespacedName, &sandboxReq); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "unable to fetch SandboxRequest")
		return ctrl.Result{}, err
	}

	// Handle based on current phase
	switch sandboxReq.Status.Phase {
	case "", fleetliftv1alpha1.SandboxPhasePending:
		return r.handlePending(ctx, &sandboxReq)
	case fleetliftv1alpha1.SandboxPhaseProvisioning:
		return r.handleProvisioning(ctx, &sandboxReq)
	case fleetliftv1alpha1.SandboxPhaseRunning:
		return r.handleRunning(ctx, &sandboxReq)
	case fleetliftv1alpha1.SandboxPhaseSucceeded, fleetliftv1alpha1.SandboxPhaseFailed:
		// Terminal states - no action needed
		return ctrl.Result{}, nil
	default:
		logger.Info("unknown phase", "phase", sandboxReq.Status.Phase)
		return ctrl.Result{}, nil
	}
}

// handlePending creates the Job for a new SandboxRequest
func (r *SandboxRequestReconciler) handlePending(ctx context.Context, sandboxReq *fleetliftv1alpha1.SandboxRequest) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("handling pending sandbox request", "taskId", sandboxReq.Spec.TaskID)

	// Validate the request
	if err := r.validateRequest(sandboxReq); err != nil {
		return r.updateStatusFailed(ctx, sandboxReq, fmt.Sprintf("validation failed: %v", err))
	}

	// Build and create the Job
	job := BuildJob(sandboxReq, r.SandboxNamespace)

	// Set owner reference for garbage collection
	if err := ctrl.SetControllerReference(sandboxReq, job, r.Scheme); err != nil {
		logger.Error(err, "unable to set owner reference")
		return ctrl.Result{}, err
	}

	if err := r.Create(ctx, job); err != nil {
		if apierrors.IsAlreadyExists(err) {
			// Job already exists, move to provisioning
			logger.Info("job already exists, moving to provisioning")
		} else {
			logger.Error(err, "unable to create job")
			return r.updateStatusFailed(ctx, sandboxReq, fmt.Sprintf("failed to create job: %v", err))
		}
	}

	r.Recorder.Event(sandboxReq, corev1.EventTypeNormal, "JobCreated", "Created sandbox job")

	// Update status to Provisioning
	sandboxReq.Status.Phase = fleetliftv1alpha1.SandboxPhaseProvisioning
	sandboxReq.Status.JobName = job.Name
	sandboxReq.Status.Message = "Job created, waiting for pod to start"
	if err := r.Status().Update(ctx, sandboxReq); err != nil {
		logger.Error(err, "unable to update status")
		return ctrl.Result{}, err
	}

	// Requeue to check provisioning status
	return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
}

// handleProvisioning waits for the pod to be running
func (r *SandboxRequestReconciler) handleProvisioning(ctx context.Context, sandboxReq *fleetliftv1alpha1.SandboxRequest) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Check for provisioning timeout
	if sandboxReq.CreationTimestamp.Add(ProvisioningTimeout).Before(time.Now()) {
		logger.Info("provisioning timeout exceeded", "creationTime", sandboxReq.CreationTimestamp)
		return r.updateStatusFailed(ctx, sandboxReq, "provisioning timeout exceeded")
	}

	// Find the pod created by the job
	pod, err := r.findPodForJob(ctx, sandboxReq)
	if err != nil {
		logger.Error(err, "error finding pod")
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}

	if pod == nil {
		logger.Info("pod not found yet, waiting")
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}

	// Check pod phase
	switch pod.Status.Phase {
	case corev1.PodRunning:
		// Pod is running, transition to Running phase
		sandboxReq.Status.Phase = fleetliftv1alpha1.SandboxPhaseRunning
		sandboxReq.Status.PodName = pod.Name
		sandboxReq.Status.Message = "Pod is running"
		now := metav1.Now()
		sandboxReq.Status.StartTime = &now

		if err := r.Status().Update(ctx, sandboxReq); err != nil {
			logger.Error(err, "unable to update status")
			return ctrl.Result{}, err
		}

		r.Recorder.Event(sandboxReq, corev1.EventTypeNormal, "PodRunning", "Sandbox pod is now running")
		return ctrl.Result{RequeueAfter: 500 * time.Millisecond}, nil

	case corev1.PodPending:
		// Still waiting for pod to start
		sandboxReq.Status.Message = "Waiting for pod to start"
		if err := r.Status().Update(ctx, sandboxReq); err != nil {
			logger.Error(err, "unable to update status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil

	case corev1.PodFailed:
		return r.updateStatusFailed(ctx, sandboxReq, fmt.Sprintf("pod failed: %s", pod.Status.Message))

	case corev1.PodSucceeded:
		// Pod completed before we could use it (shouldn't happen with tail -f)
		return r.updateStatusFailed(ctx, sandboxReq, "pod terminated unexpectedly")

	default:
		logger.Info("unexpected pod phase", "phase", pod.Status.Phase)
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}
}

// handleRunning processes exec requests while the sandbox is running
func (r *SandboxRequestReconciler) handleRunning(ctx context.Context, sandboxReq *fleetliftv1alpha1.SandboxRequest) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Check if pod is still running
	pod, err := r.findPodForJob(ctx, sandboxReq)
	if err != nil {
		logger.Error(err, "error finding pod")
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	if pod == nil || pod.Status.Phase != corev1.PodRunning {
		// Pod is no longer running
		if pod != nil && pod.Status.Phase == corev1.PodSucceeded {
			sandboxReq.Status.Phase = fleetliftv1alpha1.SandboxPhaseSucceeded
			sandboxReq.Status.Message = "Sandbox completed"
			now := metav1.Now()
			sandboxReq.Status.CompletionTime = &now
		} else {
			sandboxReq.Status.Phase = fleetliftv1alpha1.SandboxPhaseFailed
			sandboxReq.Status.Message = "Pod is no longer running"
			now := metav1.Now()
			sandboxReq.Status.CompletionTime = &now
		}
		if err := r.Status().Update(ctx, sandboxReq); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Process any pending exec requests
	processed, err := r.processExecRequests(ctx, sandboxReq, pod)
	if err != nil {
		logger.Error(err, "error processing exec requests")
		// Continue running despite exec errors
	}

	// Requeue to check for more exec requests
	// Use shorter interval if we just processed something
	requeueAfter := 500 * time.Millisecond
	if !processed {
		requeueAfter = 1 * time.Second
	}
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// processExecRequests processes pending exec requests from the spec
func (r *SandboxRequestReconciler) processExecRequests(ctx context.Context, sandboxReq *fleetliftv1alpha1.SandboxRequest, pod *corev1.Pod) (bool, error) {
	logger := log.FromContext(ctx)

	// Get list of unprocessed exec requests
	lastProcessed := sandboxReq.Status.LastExecProcessedIndex
	if lastProcessed >= len(sandboxReq.Spec.ExecRequests) {
		// No new requests
		return false, nil
	}

	// Process requests sequentially
	processed := false
	for i := lastProcessed; i < len(sandboxReq.Spec.ExecRequests); i++ {
		// Check for context cancellation between exec requests
		select {
		case <-ctx.Done():
			logger.Info("context cancelled, stopping exec processing")
			// Update status with what we've processed so far before returning
			if processed {
				if err := r.Status().Update(ctx, sandboxReq); err != nil {
					logger.Error(err, "failed to update status before context cancellation")
				}
			}
			return processed, ctx.Err()
		default:
		}

		req := sandboxReq.Spec.ExecRequests[i]
		logger.Info("processing exec request", "id", req.ID, "command", req.Command)

		// Execute the command
		result := r.ExecHandler.Execute(ctx, pod.Namespace, pod.Name, &req)

		// Append result to status
		sandboxReq.Status.ExecResults = append(sandboxReq.Status.ExecResults, *result)
		sandboxReq.Status.LastExecProcessedIndex = i + 1
		processed = true
	}

	// Trim exec results if exceeding limit to prevent CR size issues
	if len(sandboxReq.Status.ExecResults) > fleetliftv1alpha1.MaxExecResultsToRetain {
		excess := len(sandboxReq.Status.ExecResults) - fleetliftv1alpha1.MaxExecResultsToRetain
		sandboxReq.Status.ExecResults = sandboxReq.Status.ExecResults[excess:]
		logger.Info("trimmed exec results", "removed", excess, "remaining", len(sandboxReq.Status.ExecResults))
	}

	// Update status
	if processed {
		if err := r.Status().Update(ctx, sandboxReq); err != nil {
			return processed, err
		}
	}

	return processed, nil
}

// validateRequest validates the SandboxRequest spec
func (r *SandboxRequestReconciler) validateRequest(sandboxReq *fleetliftv1alpha1.SandboxRequest) error {
	if sandboxReq.Spec.TaskID == "" {
		return fmt.Errorf("taskId is required")
	}
	if sandboxReq.Spec.Image == "" {
		return fmt.Errorf("image is required")
	}
	return nil
}

// findPodForJob finds the pod created by the sandbox job
func (r *SandboxRequestReconciler) findPodForJob(ctx context.Context, sandboxReq *fleetliftv1alpha1.SandboxRequest) (*corev1.Pod, error) {
	var podList corev1.PodList
	if err := r.List(ctx, &podList, client.InNamespace(r.SandboxNamespace), client.MatchingLabels{
		"app.kubernetes.io/name":     "fleetlift-sandbox",
		"app.kubernetes.io/instance": sandboxReq.Name,
	}); err != nil {
		return nil, err
	}

	if len(podList.Items) == 0 {
		return nil, nil
	}

	// Return the first (and should be only) pod
	return &podList.Items[0], nil
}

// updateStatusFailed updates the status to failed with an error message
func (r *SandboxRequestReconciler) updateStatusFailed(ctx context.Context, sandboxReq *fleetliftv1alpha1.SandboxRequest, message string) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	sandboxReq.Status.Phase = fleetliftv1alpha1.SandboxPhaseFailed
	sandboxReq.Status.Message = message
	now := metav1.Now()
	sandboxReq.Status.CompletionTime = &now

	if err := r.Status().Update(ctx, sandboxReq); err != nil {
		logger.Error(err, "unable to update status to failed")
		return ctrl.Result{}, err
	}

	r.Recorder.Event(sandboxReq, corev1.EventTypeWarning, "Failed", message)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *SandboxRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Initialize exec handler with rest config
	execHandler, err := NewExecHandler(mgr.GetConfig())
	if err != nil {
		return fmt.Errorf("failed to create exec handler: %w", err)
	}
	r.ExecHandler = execHandler

	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetliftv1alpha1.SandboxRequest{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
