#!/usr/bin/env bash
# Kruise-Rollout E2E Test Script
#
# This script manages a multi-cluster Kind environment for testing kruise-rollout.
# It creates a Hub cluster and two Edge clusters for cross-cluster rollout testing.
#
# Usage:
#   ./hack/kruise-rollout-e2e.sh create   - Create clusters and install CRDs
#   ./hack/kruise-rollout-e2e.sh delete   - Delete all clusters
#   ./hack/kruise-rollout-e2e.sh test     - Run kruise-rollout E2E tests
#   ./hack/kruise-rollout-e2e.sh status   - Show cluster status

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Cluster names
HUB_CLUSTER=rocket-hub
EDGE_CLUSTER_1=rocket-edge-1
EDGE_CLUSTER_2=rocket-edge-2

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
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

log_step() {
    echo -e "${BLUE}[STEP]${NC} $1"
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

create_hub_config() {
    cat > /tmp/rocket-hub-kind.yaml << 'EOF'
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: rocket-hub
nodes:
  - role: control-plane
EOF
}

create_edge_config() {
    local name=$1
    cat > /tmp/${name}-kind.yaml << EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: ${name}
nodes:
  - role: control-plane
EOF
}

create() {
    check_prerequisites
    
    log_step "Creating multi-cluster Kind environment for kruise-rollout E2E tests..."
    
    # Create Hub cluster
    log_info "Creating Hub cluster ${HUB_CLUSTER}..."
    if kind get clusters 2>/dev/null | grep -q "^${HUB_CLUSTER}$"; then
        log_warn "Hub cluster ${HUB_CLUSTER} already exists, skipping creation"
    else
        create_hub_config
        kind create cluster --config /tmp/rocket-hub-kind.yaml
    fi
    
    # Create Edge cluster 1
    log_info "Creating Edge cluster ${EDGE_CLUSTER_1}..."
    if kind get clusters 2>/dev/null | grep -q "^${EDGE_CLUSTER_1}$"; then
        log_warn "Edge cluster ${EDGE_CLUSTER_1} already exists, skipping creation"
    else
        create_edge_config ${EDGE_CLUSTER_1}
        kind create cluster --config /tmp/${EDGE_CLUSTER_1}-kind.yaml
    fi
    
    # Create Edge cluster 2
    log_info "Creating Edge cluster ${EDGE_CLUSTER_2}..."
    if kind get clusters 2>/dev/null | grep -q "^${EDGE_CLUSTER_2}$"; then
        log_warn "Edge cluster ${EDGE_CLUSTER_2} already exists, skipping creation"
    else
        create_edge_config ${EDGE_CLUSTER_2}
        kind create cluster --config /tmp/${EDGE_CLUSTER_2}-kind.yaml
    fi
    
    # Wait for all clusters to be ready
    log_info "Waiting for all clusters to be ready..."
    for cluster in ${HUB_CLUSTER} ${EDGE_CLUSTER_1} ${EDGE_CLUSTER_2}; do
        kubectl --context "kind-${cluster}" wait --for=condition=Ready nodes --all --timeout=120s
    done
    
    # Apply CRDs to Hub cluster
    log_step "Applying CRDs to Hub cluster..."
    kubectl --context "kind-${HUB_CLUSTER}" apply --server-side -f "${PROJECT_ROOT}/config/crd/bases"
    
    # Wait for CRDs to be established
    log_info "Waiting for CRDs to be established..."
    kubectl --context "kind-${HUB_CLUSTER}" wait --for=condition=Established crd/applications.apps.rocket.io --timeout=60s || true
    kubectl --context "kind-${HUB_CLUSTER}" wait --for=condition=Established crd/managedclusters.storage.rocket.io --timeout=60s || true
    kubectl --context "kind-${HUB_CLUSTER}" wait --for=condition=Established crd/rollouts.rollouts.kruise.io --timeout=60s || true
    
    # Create rocket-system namespace on all clusters
    log_info "Creating rocket-system namespace on all clusters..."
    for cluster in ${HUB_CLUSTER} ${EDGE_CLUSTER_1} ${EDGE_CLUSTER_2}; do
        kubectl --context "kind-${cluster}" create namespace rocket-system --dry-run=client -o yaml | \
            kubectl --context "kind-${cluster}" apply -f -
    done
    
    # Install kruise-rollout CRDs on Edge clusters (for testing without full helm install)
    log_step "Installing kruise-rollout CRDs on Edge clusters..."
    for cluster in ${EDGE_CLUSTER_1} ${EDGE_CLUSTER_2}; do
        # Install OpenKruise CRDs (simplified for e2e testing)
        log_info "Installing kruise-rollout CRDs on ${cluster}..."
        kubectl --context "kind-${cluster}" apply -f https://raw.githubusercontent.com/openkruise/kruise-rollout/master/config/crd/bases/rollouts.kruise.io_rollouts.yaml 2>/dev/null || \
            log_warn "Failed to install kruise-rollout CRDs on ${cluster} (may already exist)"
    done
    
    log_info ""
    log_info "========================================="
    log_info "E2E environment setup complete!"
    log_info "========================================="
    log_info "Hub Cluster:   kind-${HUB_CLUSTER}"
    log_info "Edge Cluster 1: kind-${EDGE_CLUSTER_1}"
    log_info "Edge Cluster 2: kind-${EDGE_CLUSTER_2}"
    log_info ""
    log_info "To run tests:"
    log_info "  export KUBECONFIG=\"${HOME}/.kube/config\""
    log_info "  kubectl config use-context kind-${HUB_CLUSTER}"
    log_info "  go test -v -tags=e2e ./test/e2e/... -run TestKruiseRolloutE2E -timeout=30m"
    log_info ""
}

delete() {
    log_step "Deleting all Kind clusters..."
    
    for cluster in ${HUB_CLUSTER} ${EDGE_CLUSTER_1} ${EDGE_CLUSTER_2}; do
        if kind get clusters 2>/dev/null | grep -q "^${cluster}$"; then
            log_info "Deleting cluster ${cluster}..."
            kind delete cluster --name "${cluster}"
        else
            log_warn "Cluster ${cluster} does not exist."
        fi
    done
    
    log_info "All clusters deleted."
}

test_run() {
    log_step "Running kruise-rollout E2E tests..."
    
    # Ensure we're using the hub cluster context
    kubectl config use-context "kind-${HUB_CLUSTER}" 2>/dev/null || {
        log_error "Failed to switch to kind-${HUB_CLUSTER} context. Did you run 'create' first?"
        exit 1
    }
    
    cd "${PROJECT_ROOT}"
    
    # Export KUBECONFIG for the tests
    export KUBECONFIG="${HOME}/.kube/config"
    
    # Run kruise-rollout specific tests
    log_info "Running kruise-rollout E2E tests..."
    go test -v -tags=e2e ./test/e2e/... -run TestKruiseRolloutE2E -count=1 -timeout=30m
    
    log_info "E2E tests completed!"
}

quick_test() {
    log_step "Running quick kruise-rollout verification..."
    
    # Ensure we're using the hub cluster context
    kubectl config use-context "kind-${HUB_CLUSTER}" 2>/dev/null || {
        log_error "Failed to switch to kind-${HUB_CLUSTER} context. Did you run 'create' first?"
        exit 1
    }
    
    cd "${PROJECT_ROOT}"
    export KUBECONFIG="${HOME}/.kube/config"
    
    # Run a single quick test
    log_info "Running quick kruise-rollout addon install test..."
    go test -v -tags=e2e ./test/e2e/... -run TestKruiseRolloutE2E/KruiseRolloutAddonInstall -count=1 -timeout=10m
}

status() {
    log_info "Cluster status:"
    echo ""
    
    for cluster in ${HUB_CLUSTER} ${EDGE_CLUSTER_1} ${EDGE_CLUSTER_2}; do
        if kind get clusters 2>/dev/null | grep -q "^${cluster}$"; then
            echo -e "${GREEN}✓${NC} kind-${cluster}"
            kubectl --context "kind-${cluster}" get nodes -o wide 2>/dev/null || echo "  (unable to get nodes)"
            echo ""
        else
            echo -e "${RED}✗${NC} kind-${cluster} (not created)"
        fi
    done
    
    # Show CRDs on Hub
    if kind get clusters 2>/dev/null | grep -q "^${HUB_CLUSTER}$"; then
        echo "Rocket CRDs on Hub:"
        kubectl --context "kind-${HUB_CLUSTER}" get crd 2>/dev/null | grep rocket || echo "  (no Rocket CRDs found)"
    fi
}

usage() {
    echo "Kruise-Rollout E2E Test Environment Script"
    echo ""
    echo "Usage: $0 <command>"
    echo ""
    echo "Commands:"
    echo "  create      Create Kind clusters and install CRDs"
    echo "  delete      Delete all Kind clusters"
    echo "  test        Run kruise-rollout E2E tests"
    echo "  quick-test  Run quick verification test"
    echo "  status      Show cluster status"
    echo ""
    echo "Examples:"
    echo "  $0 create      # Create the test environment"
    echo "  $0 test        # Run all kruise-rollout E2E tests"
    echo "  $0 quick-test  # Run quick verification"
    echo "  $0 delete      # Clean up"
}

case "${1:-}" in
    create) create ;;
    delete) delete ;;
    test) test_run ;;
    quick-test) quick_test ;;
    status) status ;;
    *) usage ; exit 1 ;;
esac
