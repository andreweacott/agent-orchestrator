# Phase 6: Kubernetes Sandbox Provider with Controller

## Overview

This document describes the architecture and implementation of the Kubernetes sandbox provider for fleetlift. The design uses a **controller pattern** where workers create `SandboxRequest` custom resources with minimal permissions, and a dedicated controller reconciles them into Jobs with elevated permissions.

**Key Benefits:**
- Least-privilege access for worker pods
- Kubernetes-native observability and auditing
- Policy enforcement via controller
- Clean separation of concerns

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                          fleetlift-system namespace                     │
│  ┌──────────────────┐           ┌────────────────────────────────────┐  │
│  │  Worker Pod      │           │  Controller Pod                    │  │
│  │  ────────────    │  creates  │  ──────────────                    │  │
│  │  - Temporal      │──────────▶│  - Watches SandboxRequest CRs      │  │
│  │    activities    │           │  - Creates Jobs in sandbox ns      │  │
│  │  - Creates CRs   │◀──────────│  - Processes exec requests         │  │
│  │  - Polls status  │  updates  │  - Updates CR status               │  │
│  └──────────────────┘   status  └────────────────────────────────────┘  │
│         │                                      │                        │
│         │ SandboxRequest CR                    │ creates Job            │
│         ▼                                      ▼                        │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │                     SandboxRequest CRD                            │  │
│  │  apiVersion: fleetlift.io/v1alpha1                                │  │
│  │  kind: SandboxRequest                                             │  │
│  │  spec:                                                            │  │
│  │    taskId, image, env, resources, credentials, execRequests[]     │  │
│  │  status:                                                          │  │
│  │    phase, podName, jobName, execResults[], lastExecProcessedIndex │  │
│  └──────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────┘
                                       │
                                       │ creates
                                       ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                       sandbox-isolated namespace                         │
│  ┌────────────────────────────────────────────────────────────────────┐ │
│  │  Sandbox Pod (from Job)                                            │ │
│  │  ──────────────────────                                            │ │
│  │  - Runs sandbox container (claude-code-sandbox image)              │ │
│  │  - No K8s API access (automountServiceAccountToken: false)         │ │
│  │  - Controller execs into pod to run commands                       │ │
│  │  - Restricted security context                                     │ │
│  └────────────────────────────────────────────────────────────────────┘ │
│                                                                          │
│  Security:                                                               │
│  - Pod Security Standards: restricted                                    │
│  - ResourceQuota: limits total namespace resources                       │
│  - LimitRange: limits per-container resources                            │
│  - Optional: gVisor runtime for additional isolation                     │
└──────────────────────────────────────────────────────────────────────────┘
```

## CRD Schema

### SandboxRequest

```yaml
apiVersion: fleetlift.io/v1alpha1
kind: SandboxRequest
metadata:
  name: sandbox-{taskId}
  namespace: fleetlift-system
spec:
  taskId: "abc123"                     # Unique task identifier
  image: "claude-code-sandbox:latest"  # Container image
  workingDir: "/workspace"             # Working directory (optional)
  env:                                 # Environment variables
    GITHUB_TOKEN: "..."
    ANTHROPIC_API_KEY: "..."
  resources:                           # Resource limits
    limits:
      memory: "4Gi"
      cpu: "2"
    requests:
      memory: "1Gi"
      cpu: "500m"
  runtimeClassName: "gvisor"           # Optional: gVisor for extra isolation
  nodeSelector:                        # Optional: node selection
    workload-type: sandbox
  credentials:                         # Secret references
    - name: github-token
      envVar: GITHUB_TOKEN
      secretKey: token
  execRequests:                        # Worker appends exec requests here
    - id: "uuid-1"
      command: ["git", "clone", "..."]
      workingDir: "/workspace"
      timeoutSeconds: 300
  ttlSecondsAfterFinished: 3600        # Auto-cleanup after completion
status:
  phase: Running                       # Pending|Provisioning|Running|Succeeded|Failed
  jobName: sandbox-abc123              # Created Job name
  podName: sandbox-abc123-xyz          # Running Pod name
  startTime: "2024-01-01T00:00:00Z"
  completionTime: null
  message: "Pod is running"
  execResults:                         # Controller writes results here
    - id: "uuid-1"
      exitCode: 0
      stdout: "Cloning into..."
      stderr: ""
      completedAt: "2024-01-01T00:00:10Z"
  lastExecProcessedIndex: 1            # Tracks processed requests
```

### Lifecycle Phases

```
┌─────────┐    validate     ┌──────────────┐   pod ready    ┌─────────┐
│ Pending │───────────────▶│ Provisioning │──────────────▶│ Running │
└─────────┘   create job    └──────────────┘                └─────────┘
                                   │                             │
                                   │ pod failed                  │ cleanup/
                                   ▼                             │ timeout
                            ┌──────────┐                         ▼
                            │  Failed  │◀───────────────── ┌───────────┐
                            └──────────┘                   │ Succeeded │
                                                           └───────────┘
```

## RBAC Model

### Worker Role (Minimal)

```yaml
# Worker only needs SandboxRequest CR operations
rules:
  - apiGroups: ["fleetlift.io"]
    resources: ["sandboxrequests"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["fleetlift.io"]
    resources: ["sandboxrequests/status"]
    verbs: ["get", "watch"]
```

### Controller Role (Elevated)

```yaml
# Controller needs Job/Pod management and exec
rules:
  - apiGroups: ["fleetlift.io"]
    resources: ["sandboxrequests", "sandboxrequests/status", "sandboxrequests/finalizers"]
    verbs: ["get", "list", "watch", "update", "patch"]
  - apiGroups: ["batch"]
    resources: ["jobs"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["pods/exec"]
    verbs: ["create"]
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list", "watch"]
```

### Sandbox Pod (Zero Permissions)

```yaml
# Sandbox pods have no K8s API access
spec:
  automountServiceAccountToken: false
  securityContext:
    runAsNonRoot: true
    seccompProfile:
      type: RuntimeDefault
  containers:
    - securityContext:
        allowPrivilegeEscalation: false
        capabilities:
          drop: ["ALL"]
```

## Exec Request/Response Pattern

### Flow

1. **Worker submits request**: Appends `ExecRequest` to `spec.execRequests[]`
2. **Controller processes**: Finds unprocessed requests (index > lastExecProcessedIndex)
3. **Controller executes**: Uses K8s exec API with timeout
4. **Controller writes result**: Appends to `status.execResults[]`, increments index
5. **Worker polls**: Matches result by UUID

### Example

```go
// Worker adds exec request
sr.Spec.ExecRequests = append(sr.Spec.ExecRequests, ExecRequest{
    ID:             "uuid-123",
    Command:        []string{"git", "clone", repo},
    TimeoutSeconds: 300,
})
client.Update(ctx, sr)

// Worker polls for result
for {
    client.Get(ctx, sr)
    for _, result := range sr.Status.ExecResults {
        if result.ID == "uuid-123" {
            return result
        }
    }
    time.Sleep(200ms)
}
```

### Output Truncation & etcd Size Management

- Stdout/stderr truncated to 1MB per result (`MaxExecOutputSize`)
- Maximum 100 exec results retained (`MaxExecResultsToRetain`); oldest are removed
- Full output available in pod logs if needed
- Truncation indicated with `... [truncated]` suffix

**etcd Considerations:**
- etcd has a default 1.5MB object size limit
- Our limits ensure CRs stay well under this threshold
- Monitor CR sizes in production: `kubectl get sandboxrequests -o json | jq '.items[] | {name: .metadata.name, size: (. | tostring | length)}'`
- Consider alerting if CR sizes exceed 1MB

## Security Considerations

### Network Isolation

```yaml
# NetworkPolicy for sandbox namespace (recommended)
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: sandbox-egress
  namespace: sandbox-isolated
spec:
  podSelector: {}
  policyTypes: ["Egress"]
  egress:
    # Allow HTTPS to GitHub, package registries, AI APIs
    - to:
        - ipBlock:
            cidr: 0.0.0.0/0
      ports:
        - protocol: TCP
          port: 443
    # Allow DNS
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: kube-system
      ports:
        - protocol: UDP
          port: 53
```

### gVisor Runtime (Optional)

For additional sandboxing, configure a gVisor RuntimeClass:

```yaml
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: gvisor
handler: runsc
```

Set `runtimeClassName: gvisor` in SandboxRequest spec.

## Provider Implementation

### Interface Mapping

| sandbox.Provider | Kubernetes Provider Implementation |
|------------------|-----------------------------------|
| `Provision()` | Create SandboxRequest CR, wait for Running phase |
| `Exec()` | Append to spec.execRequests, poll status.execResults |
| `ExecShell()` | Wrap in `bash -c`, call Exec() |
| `CopyFrom()` | Exec `cat` command |
| `CopyTo()` | Base64 encode, exec `echo \| base64 -d > file` |
| `Status()` | Get CR, convert phase |
| `Cleanup()` | Delete SandboxRequest CR |

### Configuration

```go
type ProviderConfig struct {
    Namespace        string        // CR namespace (fleetlift-system)
    SandboxNamespace string        // Pod namespace (sandbox-isolated)
    PollInterval     time.Duration // Status polling interval
    ProvisionTimeout time.Duration // Max wait for Running phase
}
```

Environment variables:
- `SANDBOX_PROVIDER=kubernetes` - Enable K8s provider
- `SANDBOX_REQUEST_NAMESPACE` - Override CR namespace
- `SANDBOX_NAMESPACE` - Override sandbox pod namespace

## Deployment

### Prerequisites

1. Kubernetes cluster (kind for local, EKS/GKE for production)
2. CRD installed: `kubectl apply -f config/crd/bases/`
3. RBAC configured: `kubectl apply -f config/rbac/`

### Local Development (kind)

```bash
# Setup kind cluster with controller
./hack/setup-kind.sh

# Run E2E tests
E2E_TEST=true go test -v ./test/e2e/...

# Teardown
./hack/teardown-kind.sh
```

### Production

```bash
# Build and push images
docker build -t your-registry/fleetlift-controller:latest -f docker/Dockerfile.controller .
docker push your-registry/fleetlift-controller:latest

# Deploy
kubectl apply -f config/crd/bases/
kubectl apply -f config/rbac/
kubectl apply -f config/controller/deployment.yaml
```

## Resource Requirements

### Controller

- CPU: 100m-500m
- Memory: 64Mi-256Mi
- Replicas: 1 (with leader election for HA)

### Per Sandbox Pod

- Default CPU: 500m-2 cores
- Default Memory: 1Gi-4Gi
- Configurable via SandboxRequest spec

### Namespace Quotas

```yaml
# sandbox-isolated namespace
ResourceQuota:
  pods: "20"
  requests.cpu: "20"
  requests.memory: "80Gi"
  limits.cpu: "40"
  limits.memory: "160Gi"
```

## Observability

### Metrics (Prometheus)

The controller exposes metrics on `:8080/metrics`:
- `controller_runtime_reconcile_total` - Reconciliation counts
- `controller_runtime_reconcile_errors_total` - Error counts
- `controller_runtime_reconcile_time_seconds` - Reconciliation latency

### Logging

Controller uses structured JSON logging via zap:
```json
{"level":"info","ts":"...","msg":"handling pending sandbox request","taskId":"abc123"}
{"level":"info","ts":"...","msg":"processing exec request","id":"uuid-1","command":["git","clone"]}
```

### Events

Kubernetes events on SandboxRequest resources:
- `JobCreated` - Job was created
- `PodRunning` - Pod is running
- `Failed` - Sandbox failed with message

## Future Enhancements

1. **Horizontal Pod Autoscaling** for controller based on queue depth
2. **Webhook admission** for additional policy enforcement
3. **Cost attribution** via labels and resource tracking
4. **Multi-tenancy** with namespace-per-team isolation
