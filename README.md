# Rocket

[中文文档](README_zh.md)

[![Go Report Card](https://goreportcard.com/badge/github.com/hex-techs/rocket)](https://goreportcard.com/report/github.com/hex-techs/rocket)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

**Rocket** is a cloud-native multi-cluster application management platform designed to simplify application distribution, scheduling, and management across multiple Kubernetes clusters.

## ✨ Features

- **🌐 Multi-Cluster Management**: Manage dozens of Kubernetes clusters from a single control plane
- **📦 Unified Application Distribution**: Write once, deploy everywhere with standard K8s workloads
- **🎯 Intelligent Scheduling**: Advanced placement engine with Spread, BinPacking, and Affinity support
- **🔄 Dual Connection Mode**: Support both Hub (pull) and Edge (push) cluster connectivity
- **🎛️ Policy-Based Overrides**: Customize configurations per cluster without duplicating YAMLs
- **📊 Global Status Aggregation**: Real-time visibility into application health across all clusters
- **🔌 Extensible Addon System**: Plugin architecture for MCS, monitoring, and custom extensions

## 🏗️ Architecture

Rocket adopts a Hub-Spoke architecture to manage multi-cluster environments efficiently.

![Architecture](docs/images/architecture.png)

### Components

| Component | Description |
|-----------|-------------|
| **Manager** | Central control plane running on Hub cluster. Manages Application and Cluster CRDs. |
| **Scheduler** | Multi-cluster placement engine with plugin-based Filter/Score architecture. |
| **Dispatcher** | Generates and distributes native K8s resources to target clusters. |
| **Tunnel Server** | WebSocket-based reverse tunnel for Edge cluster connectivity. |
| **Agent** | Runs on Edge clusters, maintains tunnel connection and executes workloads. |

### Connection Modes

| Mode | Direction | Use Case |
|------|-----------|----------|
| **Hub** | Manager → Cluster | Clusters accessible from Hub (same VPC, VPN) |
| **Edge** | Agent → Manager | Clusters behind NAT/firewall, no inbound access |

## 🚀 Quick Start

### Prerequisites

- Go 1.22+
- Docker
- Kind (for local testing)
- kubectl

### Installation

```bash
# Clone the repository
git clone https://github.com/hex-techs/rocket.git
cd rocket

# Build binaries
make build

# Install CRDs to your cluster
kubectl apply -f config/crd/bases/
```

### Deploy Manager

```bash
# Using Helm
helm install rocket-manager charts/manager -n rocket-system --create-namespace

# Or deploy manually
kubectl apply -f config/manager/
```

### Register a Cluster (Hub Mode)

```yaml
apiVersion: storage.rocket.io/v1alpha1
kind: ManagedCluster
metadata:
  name: production-east
  labels:
    env: production
    region: us-east
spec:
  connectionMode: Hub
  apiServer: https://prod-east.example.com:6443
  secretRef:
    name: prod-east-credentials
```

### Deploy an Application

```yaml
apiVersion: apps.rocket.io/v1alpha1
kind: Application
metadata:
  name: nginx-app
  namespace: default
spec:
  replicas: 6
  workload:
    apiVersion: apps/v1
    kind: Deployment
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - name: nginx
        image: nginx:1.25
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
  clusterAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      nodeSelectorTerms:
      - matchExpressions:
        - key: env
          operator: In
          values: ["production"]
```

## 📖 Documentation

| Document | Description |
|----------|-------------|
| [Architecture](docs/architecture.md) | Detailed system architecture and design |
| [Scheduler Design](docs/scheduler.md) | Multi-cluster scheduling framework |
| [Topology Spread](docs/topology_spread.md) | Cross-zone/region workload distribution |
| [Edge Cluster](docs/edge.md) | Tunnel-based Edge cluster management |
| [API Reference](docs/api.md) | CRD specifications and examples |

## 🧪 Testing

### Unit Tests

```bash
make test
```

### E2E Tests

```bash
# Full E2E suite with Kind
make e2e-kind

# Or step by step
make e2e-kind-create  # Create Kind cluster
make e2e-kind-test    # Run tests
make e2e-kind-delete  # Cleanup
```

## 🤝 Contributing

Contributions are welcome! Please read our [Contributing Guide](CONTRIBUTING.md) for details.

## 📄 License

Rocket is licensed under the Apache License 2.0. See [LICENSE](LICENSE) for details.
