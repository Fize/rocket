# Edge Cluster Management

[中文文档](edge_zh.md)

## Overview

Rocket supports two cluster connection modes:

| Mode | Connection Direction | Use Case |
|------|---------------------|----------|
| **Hub** | Manager → Cluster | Data center clusters with accessible API servers |
| **Edge** | Cluster → Manager | Edge clusters behind NAT/firewall |

This document focuses on Edge mode's tunnel connection mechanism, the Rocket Agent, and operation guide.

## Architecture

![Edge Architecture](images/edge_arch.png)

## Tunnel Protocol

### Connection Flow

```
1. Agent Initialization
   ├── Read ManagedCluster configuration
   ├── Get Hub Manager address
   └── Prepare authentication credentials

2. Establish Connection
   ├── Create WebSocket connection to Manager /connect endpoint
   ├── Send authentication headers:
   │   ├── Authorization: Bearer <bootstrap-token>
   │   ├── X-Rocket-Cluster-Name: <cluster-name>
   │   └── X-Remotedialer-ID: <cluster-name>
   └── Wait for connection confirmation

3. Maintain Connection
   ├── Send heartbeat every 30 seconds
   ├── Monitor connection status
   └── Auto reconnect on failure (exponential backoff)

4. Handle Requests
   ├── Receive request from Manager through tunnel
   ├── Forward to local API server
   └── Return response through tunnel
```

### Authentication Methods

#### Bootstrap Token Authentication

Used for initial cluster registration:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: bootstrap-token-<token-id>
  namespace: kube-system
type: bootstrap.kubernetes.io/token
data:
  token-id: <base64-encoded>
  token-secret: <base64-encoded>
  usage-bootstrap-authentication: dHJ1ZQ==  # "true"
```

The token format is `<token-id>.<token-secret>`.

#### ServiceAccount Token (Recommended for Production)

After initial registration, Agent uses ServiceAccount token:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: rocket-agent
  namespace: rocket-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: rocket-agent
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin  # Or custom role with required permissions
subjects:
- kind: ServiceAccount
  name: rocket-agent
  namespace: rocket-system
```

## Agent Deployment

### Prerequisites

1. Hub cluster with Rocket Manager running
2. Network connectivity from Edge cluster to Manager's WebSocket port
3. Bootstrap token or ServiceAccount configured

### Installation via Helm

```bash
# Add Rocket Helm repository
helm repo add rocket https://hex-techs.github.io/rocket
helm repo update

# Create namespace
kubectl create namespace rocket-system

# Install Agent
helm install rocket-agent rocket/agent \
  --namespace rocket-system \
  --set manager.endpoint="wss://manager.example.com:8443" \
  --set cluster.name="edge-cluster-01" \
  --set auth.bootstrapToken="<bootstrap-token>"
```

### Helm Values

```yaml
# charts/agent/values.yaml
manager:
  # Manager WebSocket endpoint
  endpoint: "wss://manager.example.com:8443"

cluster:
  # Unique cluster name
  name: "edge-cluster-01"
  # Labels for scheduling
  labels:
    env: production
    region: edge-site-1

auth:
  # Bootstrap token for initial registration
  bootstrapToken: ""
  # Or use existing secret
  existingSecret: ""

agent:
  image:
    repository: ghcr.io/hex-techs/rocket-agent
    tag: latest
  resources:
    requests:
      cpu: 100m
      memory: 128Mi
    limits:
      cpu: 500m
      memory: 512Mi
```

### Manual Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: rocket-agent
  namespace: rocket-system
spec:
  replicas: 1
  selector:
    matchLabels:
      app: rocket-agent
  template:
    metadata:
      labels:
        app: rocket-agent
    spec:
      serviceAccountName: rocket-agent
      containers:
      - name: agent
        image: ghcr.io/hex-techs/rocket-agent:latest
        args:
        - --manager-endpoint=wss://manager.example.com:8443
        - --cluster-name=edge-cluster-01
        - --bootstrap-token=$(BOOTSTRAP_TOKEN)
        env:
        - name: BOOTSTRAP_TOKEN
          valueFrom:
            secretKeyRef:
              name: rocket-agent-token
              key: token
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
```

## ManagedCluster Configuration

### Registering an Edge Cluster

```yaml
apiVersion: storage.rocket.io/v1alpha1
kind: ManagedCluster
metadata:
  name: edge-cluster-01
  labels:
    env: production
    topology.kubernetes.io/region: edge-site-1
spec:
  # Set connection mode to Edge
  connectionMode: Edge
  
  # Optional: Cluster taints
  taints:
  - key: "location"
    value: "edge"
    effect: PreferNoSchedule
```

### Status Monitoring

```bash
# Check cluster status
kubectl get managedcluster edge-cluster-01 -o yaml

# Status output
status:
  connectionStatus: Connected
  ready: true
  lastHeartbeatTime: "2024-01-01T12:00:00Z"
  version:
    gitVersion: v1.28.0
  allocatable:
    cpu: "8"
    memory: "32Gi"
  conditions:
  - type: Ready
    status: "True"
  - type: AgentConnected
    status: "True"
```

## Troubleshooting

### Connection Issues

#### Agent Cannot Connect to Manager

1. **Check network connectivity**
   ```bash
   # From Edge cluster node
   curl -v https://manager.example.com:8443/healthz
   ```

2. **Verify bootstrap token**
   ```bash
   # On Hub cluster
   kubectl get secret -n kube-system | grep bootstrap-token
   ```

3. **Check Agent logs**
   ```bash
   kubectl logs -n rocket-system -l app=rocket-agent -f
   ```

#### Connection Unstable

1. **Check heartbeat timeout settings**
   - Default: 30 seconds interval, 90 seconds timeout
   - Increase timeout in high-latency networks

2. **Monitor network latency**
   ```bash
   # Check round-trip time
   ping manager.example.com
   ```

### Authentication Failures

#### Invalid Bootstrap Token

```
Error: authentication failed: invalid bootstrap token
```

**Solution:**
1. Verify token format: `<token-id>.<token-secret>`
2. Check token exists in Hub cluster:
   ```bash
   kubectl get secret -n kube-system bootstrap-token-<token-id>
   ```

#### Permission Denied

```
Error: forbidden: User "system:serviceaccount:..." cannot list resource
```

**Solution:**
1. Verify ClusterRoleBinding exists
2. Check ServiceAccount has required permissions

### Debugging Commands

```bash
# Check Agent status
kubectl get pods -n rocket-system -l app=rocket-agent

# View Agent logs
kubectl logs -n rocket-system deployment/rocket-agent -f

# Check ManagedCluster status
kubectl get managedcluster -o wide

# Describe cluster for details
kubectl describe managedcluster edge-cluster-01

# Check events
kubectl get events -n rocket-system --sort-by='.lastTimestamp'
```

## Security Considerations

### Network Security

1. **TLS Encryption**: All tunnel traffic is encrypted via TLS
2. **Certificate Verification**: Agent verifies Manager's certificate
3. **Firewall Rules**: Only outbound connections required from Edge

### Authentication Security

1. **Token Rotation**: Regularly rotate bootstrap tokens
2. **Minimal Permissions**: Use ServiceAccount with least required permissions
3. **Audit Logging**: Enable audit logs for tunnel connections

### Recommended Production Settings

```yaml
# Agent deployment with security hardening
spec:
  template:
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 1000
        fsGroup: 1000
      containers:
      - name: agent
        securityContext:
          allowPrivilegeEscalation: false
          readOnlyRootFilesystem: true
          capabilities:
            drop:
            - ALL
```

## Related Documentation

- [Architecture Overview](architecture.md) - System architecture
- [API Reference](api.md) - ManagedCluster CRD specification
