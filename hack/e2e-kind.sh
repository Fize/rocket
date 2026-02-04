#!/usr/bin/env bash
# Rocket E2E Test Environment Script
#
# This script manages Kind multi-cluster environments for E2E testing.
# It sets up a hub cluster with all required CRDs and prepares the
# environment for running the Rocket E2E tests.
#
# Usage:
#   ./hack/e2e-kind.sh create   - Create clusters and install CRDs
#   ./hack/e2e-kind.sh delete   - Delete all clusters
#   ./hack/e2e-kind.sh test     - Run E2E tests
#   ./hack/e2e-kind.sh status   - Show cluster status

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Cluster names
HUB_CLUSTER=rocket-hub

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

check_prerequisites() {
    log_info "Checking prerequisites..."
    
    if ! command -v kind &> /dev/null; then
        log_error "kind is not installed. Please install it: https://kind.sigs.k8s.io/"
        exit 1
    fi
    
    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl is not installed. Please install it: https://kubernetes.io/docs/tasks/tools/"
        exit 1
    fi
    
    if ! command -v go &> /dev/null; then
        log_error "go is not installed. Please install it: https://golang.org/doc/install"
        exit 1
    fi
    
    log_info "All prerequisites are met."
}

create() {
    check_prerequisites
    
    log_info "Creating Kind cluster ${HUB_CLUSTER}..."
    
    if kind get clusters 2>/dev/null | grep -q "^${HUB_CLUSTER}$"; then
        log_warn "Cluster ${HUB_CLUSTER} already exists, skipping creation"
    else
        kind create cluster --name "${HUB_CLUSTER}" --config "${PROJECT_ROOT}/hack/rocket-hub-kind.yaml"
    fi
    
    # Wait for cluster to be ready
    log_info "Waiting for cluster to be ready..."
    kubectl --context "kind-${HUB_CLUSTER}" wait --for=condition=Ready nodes --all --timeout=120s
    
    # Apply CRDs
    log_info "Applying CRDs..."
    kubectl --context "kind-${HUB_CLUSTER}" apply --server-side -f "${PROJECT_ROOT}/config/crd/bases"
    
    # Wait for CRDs to be established
    log_info "Waiting for CRDs to be established..."
    kubectl --context "kind-${HUB_CLUSTER}" wait --for=condition=Established crd/applications.apps.rocket.io --timeout=60s || true
    kubectl --context "kind-${HUB_CLUSTER}" wait --for=condition=Established crd/managedclusters.storage.rocket.io --timeout=60s || true
    
    # Create rocket-system namespace
    kubectl --context "kind-${HUB_CLUSTER}" create namespace rocket-system --dry-run=client -o yaml | \
        kubectl --context "kind-${HUB_CLUSTER}" apply -f -
    
    log_info ""
    log_info "E2E environment setup complete!"
    log_info "Cluster: kind-${HUB_CLUSTER}"
    log_info ""
    log_info "To run tests:"
    log_info "  kubectl config use-context kind-${HUB_CLUSTER}"
    log_info "  make e2e-test"
}

delete() {
    log_info "Deleting Kind cluster ${HUB_CLUSTER}..."
    
    if kind get clusters 2>/dev/null | grep -q "^${HUB_CLUSTER}$"; then
        kind delete cluster --name "${HUB_CLUSTER}"
        log_info "Cluster ${HUB_CLUSTER} deleted."
    else
        log_warn "Cluster ${HUB_CLUSTER} does not exist."
    fi
}

test_run() {
    log_info "Running E2E tests..."
    
    # Ensure we're using the hub cluster context
    kubectl config use-context "kind-${HUB_CLUSTER}" 2>/dev/null || {
        log_error "Failed to switch to kind-${HUB_CLUSTER} context. Did you run 'create' first?"
        exit 1
    }
    
    cd "${PROJECT_ROOT}"
    
    # Export KUBECONFIG for the tests
    export KUBECONFIG="${HOME}/.kube/config"
    
    # Run tests
    log_info "Running E2E tests..."
    go test -v -tags=e2e ./test/e2e/... -count=1 -timeout=15m
    
    log_info "E2E tests completed!"
}

status() {
    log_info "Cluster status:"
    echo ""
    
    if kind get clusters 2>/dev/null | grep -q "^${HUB_CLUSTER}$"; then
        echo -e "${GREEN}✓${NC} kind-${HUB_CLUSTER}"
        kubectl --context "kind-${HUB_CLUSTER}" get nodes -o wide 2>/dev/null || echo "  (unable to get nodes)"
        echo ""
        echo "CRDs installed:"
        kubectl --context "kind-${HUB_CLUSTER}" get crd 2>/dev/null | grep rocket || echo "  (no Rocket CRDs found)"
    else
        echo -e "${RED}✗${NC} kind-${HUB_CLUSTER} (not created)"
    fi
}

usage() {
    echo "Rocket E2E Test Environment Script"
    echo ""
    echo "Usage: $0 <command>"
    echo ""
    echo "Commands:"
    echo "  create    Create Kind cluster and install CRDs"
    echo "  delete    Delete Kind cluster"
    echo "  test      Run E2E tests"
    echo "  status    Show cluster status"
    echo ""
    echo "Examples:"
    echo "  $0 create   # Create the test environment"
    echo "  $0 test     # Run E2E tests"
    echo "  $0 delete   # Clean up"
}

case "${1:-}" in
    create) create ;;
    delete) delete ;;
    test) test_run ;;
    status) status ;;
    *) usage ; exit 1 ;;
esac
