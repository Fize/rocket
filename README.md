# Rocket

[中文文档](README_zh.md)

[![Go Report Card](https://goreportcard.com/badge/github.com/fize/rocket)](https://goreportcard.com/report/github.com/fize/rocket)
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
  - Built-in **Submariner Addon**: Cross-cluster service discovery and networking
  - Multiple network modes: IPsec tunnel, WireGuard, VXLAN, flat network
  - Automated ServiceExport/ServiceImport management

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
git clone https://github.com/fize/rocket.git
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

## 🔌 Built-in Addons

### Submariner - Cross-Cluster Service Discovery

Rocket includes a built-in **Submariner Addon** (mcs-lighthouse) for cross-cluster service discovery and networking.

#### Enable Cross-Cluster Service Discovery

```yaml
apiVersion: storage.rocket.io/v1alpha1
kind: ManagedCluster
metadata:
  name: cluster-1
  labels:
    environment: production
spec:
  connectionMode: Hub
  apiServer: https://cluster-1.example.com:6443
  addons:
    - name: mcs-lighthouse
      enabled: true
      config:
        submarinerChartVersion: "0.23.0-m0"
```

#### Export Service to Other Clusters

```yaml
# Export service in member cluster
apiVersion: multicluster.x-k8s.io/v1alpha1
kind: ServiceExport
metadata:
  name: my-service
  namespace: default
```

#### Access Cross-Cluster Service

```bash
# Access using clusterset.local domain
kubectl run test --image=busybox --rm -it -- \
  wget my-service.default.svc.clusterset.local
```

#### Network Modes

Submariner supports multiple network modes:

| Mode | Use Case | Configuration |
|------|----------|---------------|
| **IPsec Tunnel** | Network isolation (default) | No additional config needed |
| **Flat Network** | Pod CIDR already routed across clusters | Set `natEnabled: false` |
| **VXLAN** | VPC Peering environment | Set `cableDriver: vxlan` |

> ⚠️ **Important**: Flat network mode requires users to configure underlying network routing to ensure Pod CIDRs are routable across all clusters. See [Addon Design](docs/addon.md#submariner-usage-guide) for details.

#### Limitations

1. **Network Requirements**: All clusters must communicate with Hub cluster
2. **Resource Requirements**: ~500m CPU and 512Mi memory per cluster
3. **Version Compatibility**: Same Submariner version across all clusters recommended
4. **Cluster ID**: Each cluster must have a unique `clusterId`

> ⚠️ **Important Notice**: Rocket provides only basic capabilities for cross-cluster service discovery and networking. For complex network scenarios (such as flat network routing configuration, cross-cloud network connectivity, hybrid cloud architectures, etc.), users are responsible for planning and maintaining the underlying network infrastructure based on their actual environment. Rocket does not handle or participate in the operations and maintenance of underlying network routing configuration, security policies, network device management, etc.

For more details, see [Addon Design](docs/addon.md)

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
