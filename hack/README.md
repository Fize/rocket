# Hack Directory

This directory contains scripts and configurations for development and testing.

## Files

| File | Description |
|------|-------------|
| `e2e-kind.sh` | E2E test environment management script |
| `rocket-hub-kind.yaml` | Kind cluster configuration for Hub cluster |
| `boilerplate.go.txt` | Go file header template for code generation |

## E2E Testing

### Prerequisites

- [kind](https://kind.sigs.k8s.io) (Kubernetes in Docker)
- kubectl
- Go >= 1.21

### Quick Start

```bash
# Create Kind cluster and install CRDs
make e2e-kind-create

# Run E2E tests
make e2e-kind-test

# Or run full suite (create, test, delete)
make e2e-kind

# Cleanup
make e2e-kind-delete
```

### Manual Commands

```bash
# Create cluster
./hack/e2e-kind.sh create

# Run tests
./hack/e2e-kind.sh test

# Check status
./hack/e2e-kind.sh status

# Delete cluster
./hack/e2e-kind.sh delete
```

### E2E Test Coverage

The E2E tests cover the following functionality:

| Test Suite | Sub-tests | Coverage |
|------------|-----------|----------|
| **ClusterManagement** | HubClusterLifecycle | Hub cluster creation and status |
| | ClusterOfflineDetection | Cluster offline detection |
| **EdgeCluster** | EdgeClusterLifecycle | Edge cluster with tunnel connection |
| | EdgeClusterDeployment | Application deployment on Edge clusters |
| | EdgeClusterHeartbeat | Agent heartbeat mechanism |
| **ApplicationLifecycle** | DeploymentLifecycle | Deployment creation and distribution |
| | ApplicationDeletion | Application deletion and cleanup |
| **Scheduling** | BasicScheduling | ClusterAffinity-based scheduling |
| | WaterfillScheduling | Waterfill scheduling strategy |
| **PushModel** | PushModelDeployment | Push model deployment |
| | PushModelMultiCluster | Multi-cluster deployment |
| | PushModelJobWorkload | Job workload distribution |
| **Features** | JobSupport | Job workload type |
| | CronJobSupport | CronJob workload type |
| | ResiliencyPDB | PodDisruptionBudget policy |

**Total: 17 test cases covering core Rocket functionality**

### Test Environment

The E2E tests use:
- A single Kind cluster (`rocket-hub`) as the Hub
- In-process controllers and scheduler
- Mock tunnel server for Edge cluster testing
- Bootstrap token authentication for tunnel connections

### Troubleshooting

If tests fail, check:

1. Cluster is running: `kind get clusters`
2. CRDs are installed: `kubectl get crd | grep rocket`
3. KUBECONFIG is set correctly
4. Sufficient system resources (Docker memory/CPU)
