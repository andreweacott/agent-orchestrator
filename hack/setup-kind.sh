#!/bin/bash
# Setup a kind cluster for fleetlift development and testing
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
CLUSTER_NAME="${CLUSTER_NAME:-fleetlift}"

echo "==> Setting up kind cluster: ${CLUSTER_NAME}"

# Check if kind is installed
if ! command -v kind &> /dev/null; then
    echo "Error: kind is not installed. Install from https://kind.sigs.k8s.io/"
    exit 1
fi

# Check if kubectl is installed
if ! command -v kubectl &> /dev/null; then
    echo "Error: kubectl is not installed"
    exit 1
fi

# Delete existing cluster if it exists
if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
    echo "==> Deleting existing cluster..."
    kind delete cluster --name "${CLUSTER_NAME}"
fi

# Create cluster
echo "==> Creating kind cluster..."
kind create cluster --name "${CLUSTER_NAME}" --config "${SCRIPT_DIR}/kind-config.yaml"

# Wait for cluster to be ready
echo "==> Waiting for cluster to be ready..."
kubectl wait --for=condition=Ready nodes --all --timeout=120s

# Install CRDs
echo "==> Installing CRDs..."
kubectl apply -f "${PROJECT_ROOT}/config/crd/bases/"

# Create namespaces and RBAC
echo "==> Creating namespaces and RBAC..."
kubectl apply -f "${PROJECT_ROOT}/config/rbac/sandbox_serviceaccount.yaml"
kubectl apply -f "${PROJECT_ROOT}/config/rbac/worker_role.yaml"
kubectl apply -f "${PROJECT_ROOT}/config/rbac/controller_role.yaml"

# Build and load controller image
echo "==> Building controller image..."
docker build -t fleetlift-controller:latest -f "${PROJECT_ROOT}/docker/Dockerfile.controller" "${PROJECT_ROOT}"
kind load docker-image fleetlift-controller:latest --name "${CLUSTER_NAME}"

# Build and load sandbox image (if it exists)
if [[ -f "${PROJECT_ROOT}/docker/Dockerfile.sandbox" ]]; then
    echo "==> Building sandbox image..."
    docker build -t claude-code-sandbox:latest -f "${PROJECT_ROOT}/docker/Dockerfile.sandbox" "${PROJECT_ROOT}/docker"
    kind load docker-image claude-code-sandbox:latest --name "${CLUSTER_NAME}"
fi

# Deploy controller
echo "==> Deploying controller..."
kubectl apply -f "${PROJECT_ROOT}/config/controller/deployment.yaml"

# Wait for controller to be ready
echo "==> Waiting for controller to be ready..."
kubectl wait --for=condition=Available deployment/fleetlift-controller \
    -n fleetlift-system --timeout=120s

echo ""
echo "==> Kind cluster '${CLUSTER_NAME}' is ready!"
echo ""
echo "Useful commands:"
echo "  kubectl get sandboxrequests -A       # List sandbox requests"
echo "  kubectl get pods -n sandbox-isolated # List sandbox pods"
echo "  kubectl logs -n fleetlift-system deployment/fleetlift-controller -f"
echo ""
echo "To run E2E tests:"
echo "  E2E_TEST=true go test -v ./test/e2e/..."
