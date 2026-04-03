# Addon Extension Design

[中文](addon_zh.md)

## Overview

Rocket uses an **Addon (plugin) mechanism** to implement functional extensions, allowing third-party components to be seamlessly integrated into the multi-cluster management platform. This document introduces the core architecture, implementation principles, and usage of Addons.

## Architecture Overview

![Addon Architecture](images/addon_architecture.drawio.png)

## Core Concepts

### 1. Addon Interface Definition

Addon is the standard interface for extending Rocket functionality. Each Addon needs to implement the following methods:

```go
type Addon interface {
    // Name returns the unique identifier of the Addon
    Name() string
    
    // ManagerController returns the Manager-side controller implementation
    // Returns nil if the Addon only runs on the Agent side
    ManagerController(mgr ctrl.Manager) (AddonController, error)
    
    // AgentController returns the Agent-side controller implementation
    // Returns nil if the Addon only runs on the Manager side
    AgentController(mgr ctrl.Manager) (AddonController, error)
    
    // Manifests returns the generic CRDs or resources required by the Addon
    Manifests() []runtime.Object
}
```

### 2. AddonController Interface

AddonController defines the reconciliation logic for Addons:

```go
type AddonController interface {
    // Reconcile handles the lifecycle of the Addon
    // Including installation, upgrade, configuration updates, uninstallation, etc.
    Reconcile(ctx context.Context, config AddonConfig) error
}

type AddonConfig struct {
    ClusterName string            // Target cluster name
    Config      map[string]string // Addon configuration
    Client      client.Client     // Kubernetes client
}
```

### 3. Dual-Side Controller Pattern

Rocket adopts a **dual-side controller pattern**, separating Addon deployment into Manager-side and Agent-side:

| Side | Responsibility | Use Cases |
|------|----------------|-----------|
| **Manager Side** | Deploy core components in Hub cluster | Broker, control plane, configuration management |
| **Agent Side** | Deploy workloads in member clusters | Operator, data plane, local agents |

Advantages of this design:
- ✅ Supports Hub-Spoke architecture
- ✅ Clear responsibilities, easier maintenance
- ✅ Independent deployment and upgrade
- ✅ Supports Edge mode (reverse tunnel)

## Global Registration Mechanism

### 1. Registry Implementation

Rocket uses a global registry to manage all Addons:

```go
// internal/addon/manager.go
var globalRegistry = &defaultRegistry{
    registry: make(map[string]Addon),
}

func Register(addon Addon) {
    globalRegistry.Register(addon)
}

func Get(name string) Addon {
    return globalRegistry.Get(name)
}

func List() []Addon {
    return globalRegistry.List()
}
```

### 2. Automatic Registration

Uses Go's `init()` function for automatic registration:

```go
// internal/addon/mcs/mcs.go
func init() {
    addon.Register(&MCSAddon{})  // Automatically registered at program startup
}
```

### 3. Controller Initialization

When the Manager starts, AddonReconciler automatically initializes all registered Addons:

```go
func (r *AddonReconciler) SetupWithManager(mgr ctrl.Manager) error {
    r.Controllers = make(map[string]addon.AddonController)
    
    // Iterate through all registered Addons
    for _, a := range r.getRegistry().List() {
        c, err := a.ManagerController(mgr)
        if err != nil {
            return err
        }
        if c != nil {
            r.Controllers[a.Name()] = c
        }
    }
    
    return ctrl.NewControllerManagedBy(mgr).
        For(&storagev1alpha1.ManagedCluster{}).
        Complete(r)
}
```

## Configuration Passing Mechanism

### Configuration Flow

```
1. User enables Addon in ManagedCluster.Spec.Addons:
   ManagedCluster {
       Spec: {
           Addons: [{
               Name: "mcs-lighthouse",
               Enabled: true,
               Config: {
                   "brokerChartVersion": "0.23.0-m0",
                   "submarinerChartVersion": "0.23.0-m0",
               }
           }]
       }
   }

2. Manager-side AddonReconciler watches changes:
   ├─ Calls ManagerController.Reconcile()
   ├─ Deploys core components (Broker)
   ├─ Retrieves connection info (token, ca)
   └─ Updates ManagedCluster.Spec.Addons[].Config:
       {
           "brokerURL": "https://manager-api:6443",
           "brokerToken": "eyJhbG...",
           "brokerCA": "LS0tLS...",
           "brokerNamespace": "submariner-k8s-broker"
       }

3. Agent-side syncs configuration via WebSocket tunnel:
   ├─ Watches ManagedCluster updates
   ├─ Reads latest Config
   └─ Calls AgentController.Reconcile()
       └─ Deploys workload components (Operator)
```

### Configuration Write-Back Example

```go
// Manager-side writes Broker connection info back to CRD
func (c *ManagerController) Reconcile(ctx context.Context, config addon.AddonConfig) error {
    // 1. Deploy Broker
    if err := c.ensureBroker(ctx, config); err != nil {
        return err
    }
    
    // 2. Get Broker connection info
    brokerInfo, err := c.getBrokerInfo(ctx, config.Config)
    if err != nil {
        return err
    }
    
    // 3. Update ManagedCluster.Spec.Addons[].Config
    cluster := &storagev1alpha1.ManagedCluster{}
    if err := config.Client.Get(ctx, types.NamespacedName{Name: config.ClusterName}, cluster); err != nil {
        return err
    }
    
    for i, addon := range cluster.Spec.Addons {
        if addon.Name == AddonName {
            cluster.Spec.Addons[i].Config["brokerURL"] = brokerInfo["brokerURL"]
            cluster.Spec.Addons[i].Config["brokerToken"] = brokerInfo["brokerToken"]
            cluster.Spec.Addons[i].Config["brokerCA"] = brokerInfo["brokerCA"]
            break
        }
    }
    
    return config.Client.Update(ctx, cluster)
}
```

## Submariner Integration Implementation

### Overall Architecture

Rocket uses the Addon mechanism to integrate Submariner for cross-cluster networking and service discovery:

![Submariner Flow](images/submariner_flow.drawio.png)

### 1. Manager-side Implementation

Manager-side is responsible for deploying Submariner Broker:

```go
type ManagerController struct {
    mgrClient  client.Client
    helmClient helm.HelmClient
}

func (c *ManagerController) Reconcile(ctx context.Context, config addon.AddonConfig) error {
    // 1. Detect if Broker configuration changed
    shouldUpdate := c.shouldUpdateBroker(config.Config)
    
    // 2. Deploy/upgrade Broker (via Helm)
    if shouldUpdate {
        chartURL, err := resolveChartURL(chartURLConfig{
            RepoURL:      config.Config[ConfigBrokerChartRepoURL],
            ChartName:    "submariner-k8s-broker",
            ChartVersion: config.Config[ConfigBrokerChartVersion],
        })
        
        values := map[string]interface{}{
            "submariner": map[string]interface{}{
                "serviceDiscovery": true,
            },
        }
        
        helmClient.InstallOrUpgrade("submariner-k8s-broker", chartURL, values)
    }
    
    // 3. Get Broker Secret
    secret := &corev1.Secret{}
    c.mgrClient.Get(ctx, types.NamespacedName{
        Name:      "submariner-k8s-broker-client-token",
        Namespace: "submariner-k8s-broker",
    }, secret)
    
    token := string(secret.Data["token"])
    ca := base64.StdEncoding.EncodeToString(secret.Data["ca.crt"])
    
    // 4. Update ManagedCluster configuration
    // (See "Configuration Write-Back Example" above)
}
```

### 2. Agent-side Implementation

Agent-side is responsible for deploying Submariner Operator:

```go
type AgentController struct {
    helmClient helm.HelmClient
}

func (c *AgentController) Reconcile(ctx context.Context, config addon.AddonConfig) error {
    // 1. Read Broker connection info from Config
    brokerURL := config.Config["brokerURL"]
    brokerToken := config.Config["brokerToken"]
    brokerCA := config.Config["brokerCA"]
    
    // 2. Deploy Submariner Operator (via Helm)
    chartURL, err := resolveChartURL(chartURLConfig{
        RepoURL:      config.Config[ConfigSubmarinerChartRepoURL],
        ChartName:    "submariner-operator",
        ChartVersion: config.Config[ConfigSubmarinerChartVersion],
    })
    
    values := map[string]interface{}{
        "broker": map[string]interface{}{
            "server":    brokerURL,
            "token":     brokerToken,
            "namespace": "submariner-k8s-broker",
            "ca":        brokerCA,
        },
        "submariner": map[string]interface{}{
            "clusterId":        config.ClusterName,
            "natEnabled":       false,
            "serviceDiscovery": true,
        },
    }
    
    helmClient.InstallOrUpgrade("submariner", chartURL, values)
}
```

### 3. Configuration Change Detection

Supports detecting configuration changes and triggering upgrades:

```go
// Broker configuration change detection
func (c *ManagerController) shouldUpdateBroker(cfg map[string]string) bool {
    if c.lastBrokerConfig == nil {
        return true  // First-time installation
    }
    
    // Compare key configurations
    keys := []string{
        ConfigBrokerChartURL,
        ConfigBrokerChartVersion,
        ConfigBrokerValuesConfigMap,
        // ...
    }
    
    for _, key := range keys {
        if c.lastBrokerConfig[key] != cfg[key] {
            return true
        }
    }
    
    return false
}

// Submariner configuration change detection (similar)
func (c *AgentController) hasSubmarinerConfigChanged(cfg map[string]string) bool {
    // Detect if Chart version, Broker Token, etc. changed
}
```

## Usage Examples

### 1. Enable Addon

Enable Addon in `ManagedCluster`:

```yaml
apiVersion: storage.rocket.io/v1alpha1
kind: ManagedCluster
metadata:
  name: cluster-1
spec:
  connectionMode: Hub
  addons:
    - name: mcs-lighthouse
      enabled: true
      config:
        brokerChartVersion: "0.23.0-m0"
        submarinerChartVersion: "0.23.0-m0"
```

### 2. Customize Helm Values

Customize Helm Values via ConfigMap:

```yaml
apiVersion: storage.rocket.io/v1alpha1
kind: ManagedCluster
metadata:
  name: cluster-1
spec:
  addons:
    - name: mcs-lighthouse
      enabled: true
      config:
        brokerChartVersion: "0.23.0-m0"
        brokerValuesConfigMap: "my-broker-values"
        brokerValuesNamespace: "default"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-broker-values
  namespace: default
data:
  values.yaml: |
    submariner:
      serviceDiscovery: true
      broker:
        globalnet: true
```

### 3. View Status

View Addon configuration and status:

```bash
kubectl get managedcluster cluster-1 -o yaml

# View Addon configuration (written back)
spec:
  addons:
    - name: mcs-lighthouse
      enabled: true
      config:
        brokerChartVersion: "0.23.0-m0"
        brokerURL: "https://10.0.0.1:6443"
        brokerToken: "eyJhbG..."
        brokerCA: "LS0tLS..."
        brokerNamespace: "submariner-k8s-broker"
```

## Design Highlights

| Design Point | Description | Advantages |
|--------------|-------------|------------|
| **Global Registry** | Auto-registration via init(), no hardcoding | Extensible, follows Open-Closed Principle |
| **Dual-Side Controller** | Manager + Agent separated deployment | Supports Hub/Edge architecture |
| **Configuration Write-Back** | Manager writes connection info to CRD | Agent syncs via CRD, decoupled communication |
| **Helm Integration** | Deploy components via Helm | Version-controlled, supports upgrade/rollback |
| **Change Detection** | lastConfig cache + key comparison | Avoids repeated installation, supports upgrade |
| **Custom Values** | Supports ConfigMap/Secret injection | High flexibility, meets diverse requirements |
| **Idempotent Design** | InstallOrUpgrade handles automatically | Multiple calls won't cause errors |

## Best Practices

### 1. Addon Naming Convention

- Use lowercase letters and hyphens
- Format: `<function>-<type>`
- Examples: `mcs-lighthouse`, `istio-mesh`, `monitoring-prometheus`

### 2. Configuration Management

- Store sensitive information in Secrets
- Use ConfigMaps for non-sensitive configuration
- Support environment variables to override defaults

### 3. Error Handling

- Distinguish between temporary and permanent errors
- Return error for temporary errors to trigger retry
- Update status and stop retry for permanent errors

### 4. Version Compatibility

- Record supported version ranges in Addon
- Provide version upgrade paths
- Support downgrade and rollback

## Built-in Addon List

| Addon Name | Function | Manager Side | Agent Side |
|-----------|----------|--------------|-----------|
| **mcs-lighthouse** | Cross-cluster service discovery | Broker | Submariner Operator |
| **kruise-rollout** | Cross-cluster progressive rollout | Status coordination | kruise-rollout installation |
| **victoriametrics** | Multi-cluster monitoring | VictoriaMetrics Single | vmagent |

## Submariner Usage Guide

### Network Modes

Submariner supports multiple network modes. Choose the appropriate mode based on your infrastructure:

| Mode | Network Requirements | Use Cases | Configuration |
|------|---------------------|-----------|---------------|
| **IPsec Tunnel** | Network isolation between clusters | Public cloud, cross-datacenter | Default mode, auto-established encrypted tunnel |
| **WireGuard Tunnel** | Network isolation between clusters | High-performance scenarios | Set `cableDriver: wireguard` |
| **VXLAN Tunnel** | Clusters reachable | VPC Peering, on-premises network | Set `cableDriver: vxlan` |
| **Flat Network** | Pod CIDR routed across clusters | Pre-configured flat network | Set `natEnabled: false` |

### Flat Network Configuration

If your clusters already have cross-cluster routing configured (Pod CIDRs are routable across all clusters), you can use flat network mode without establishing tunnels:

#### Prerequisites

1. **Pod CIDR Routing Configured**: All clusters' Pod CIDRs have routing configured in the underlying network, Pod IPs are directly reachable across clusters
2. **Service CIDR Reachable**: ClusterIP Service's ClusterIP is routable across clusters (optional, depends on requirements)
3. **No NAT Required**: No network address translation needed for inter-cluster communication

#### Configuration

Configure in ManagedCluster:

```yaml
apiVersion: storage.rocket.io/v1alpha1
kind: ManagedCluster
metadata:
  name: cluster-1
spec:
  connectionMode: Hub
  addons:
    - name: mcs-lighthouse
      enabled: true
      config:
        submarinerChartVersion: "0.23.0-m0"
        # Customize values via ConfigMap
        submarinerValuesConfigMap: "submariner-values"
        submarinerValuesNamespace: "default"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: submariner-values
  namespace: default
data:
  values.yaml: |
    submariner:
      clusterId: cluster-1
      natEnabled: false          # Disable NAT
      serviceDiscovery: true      # Enable service discovery
      # No cableDriver needed, or set to empty
```

#### How It Works

In flat network mode:

1. **Service Discovery Layer (Lighthouse)** is still required for:
   - ServiceExport/ServiceImport synchronization
   - DNS resolution (`*.clusterset.local`)
   - EndpointSlice propagation

2. **Data Plane Layer (Gateway Engine)** is not needed:
   - No IPsec/WireGuard/VXLAN tunnels established
   - Traffic routes directly through underlying network

3. **Route Agent** may be needed, depending on network configuration:
   - If node routing tables need updates, Route Agent is still required
   - If routing is already configured, Route Agent can be disabled

#### Network Routing Configuration Example

Flat network requires configuring routes on underlying network devices or cloud platforms, for example:

```bash
# Cluster A (Pod CIDR: 10.244.0.0/16)
# Add route on Cluster B nodes
ip route add 10.244.0.0/16 via <cluster-a-gateway-ip>

# Cluster B (Pod CIDR: 10.245.0.0/16)
# Add route on Cluster A nodes
ip route add 10.245.0.0/16 via <cluster-b-gateway-ip>
```

Or configure in cloud platform VPC route tables:

| Destination CIDR | Next Hop Type | Next Hop |
|-----------------|---------------|----------|
| 10.244.0.0/16 | Peering Connection | cluster-a-vpc |
| 10.245.0.0/16 | Peering Connection | cluster-b-vpc |

### Limitations

#### 1. Network Connectivity Requirements

- **Required**: All member clusters can communicate with Hub cluster (can access Hub API Server)
- **Required**: Hub cluster can access Broker API Server
- **Conditional**: Whether member clusters need to communicate with each other depends on network mode

#### 2. Resource Requirements

| Component | CPU | Memory | Notes |
|-----------|-----|--------|-------|
| Broker | 100m | 128Mi | Runs on Hub cluster |
| Operator | 100m | 128Mi | Each member cluster |
| Lighthouse Agent | 50m | 64Mi | Each member cluster |
| Lighthouse CoreDNS | 50m | 64Mi | Each member cluster |
| Gateway Engine | 200m | 256Mi | Tunnel mode only |
| Route Agent | 50m | 64Mi | Each node, only when needed |

#### 3. Port Requirements

Tunnel mode requires opening the following ports:

| Port | Protocol | Purpose | Notes |
|------|----------|---------|-------|
| 4500/UDP | IPsec | IPsec NAT-T | Default IPsec mode |
| 51871/UDP | WireGuard | WireGuard | WireGuard mode |
| 4800/UDP | VXLAN | VXLAN | VXLAN mode |

Flat network mode requires no additional ports.

#### 4. Cluster ID Uniqueness

Each cluster must have a unique `clusterId` to distinguish services from different clusters:

```yaml
# ❌ Wrong: Multiple clusters use the same ID
spec:
  addons:
    - name: mcs-lighthouse
      config:
        clusterId: "default"  # Same for all clusters

# ✅ Correct: Each cluster uses a unique ID
spec:
  addons:
    - name: mcs-lighthouse
      config:
        clusterId: "cluster-east-1"  # Unique identifier
```

#### 5. Version Compatibility

- Broker and Agent versions should be consistent
- Submariner version built into Rocket: `0.23.0-m0`
- Supports overriding version via configuration (ensure compatibility)

### FAQ

#### Q1: How to determine if flat network mode is needed?

**A**: Consider flat network if your environment meets any of these conditions:
- All clusters in the same VPC/VNet with Pod CIDRs routed
- Using VPC Peering with cross-VPC routing configured
- On-premises datacenter with network devices configured for cross-cluster routing
- Inter-cluster network connected through other means (e.g., SD-WAN)

#### Q2: Is Broker still needed in flat network mode?

**A**: **Yes, Broker is still required**. Broker is used for:
- Storing cluster metadata
- Synchronizing ServiceExport/ServiceImport
- Central API Server for Lighthouse Agent connections

Flat network only affects the data plane, not the control plane.

#### Q3: How to verify cross-cluster network reachability?

**A**: In flat network mode, test with:

```bash
# On Cluster A node
kubectl run test --image=busybox --rm -it -- ping <cluster-b-pod-ip>

# Test DNS resolution
kubectl run test --image=busybox --rm -it -- \
  nslookup nginx.default.svc.clusterset.local

# Test service access
kubectl run test --image=busybox --rm -it -- \
  wget -qO- nginx.default.svc.clusterset.local
```

#### Q4: What to do if cross-cluster service access fails?

**A**: Troubleshoot with these steps:

1. **Check service export**:
   ```bash
   kubectl get serviceexport -A
   kubectl describe serviceexport nginx
   ```

2. **Check service import**:
   ```bash
   kubectl get serviceimport -A
   kubectl describe serviceimport nginx
   ```

3. **Check DNS resolution**:
   ```bash
   kubectl logs -n submariner-operator <lighthouse-coredns-pod>
   ```

4. **Check network connectivity**:
   ```bash
   # View Pod IPs in EndpointSlice
   kubectl get endpointslice -o yaml
   
   # Test if Pod IP is reachable
   ping <remote-pod-ip>
   ```

5. **Check Lighthouse Agent logs**:
   ```bash
   kubectl logs -n submariner-operator <lighthouse-agent-pod>
   ```

> 💡 **Tip**: If network connectivity issues involve underlying network configuration (such as routing, firewalls, VPC Peering, etc.), users need to troubleshoot and configure them themselves. Rocket is only responsible for service discovery functionality and does not handle underlying network operations.

## Kruise-Rollout Usage Guide

### Overview

Kruise-Rollout Addon provides cross-cluster progressive rollout capabilities for Rocket, supporting three rollout strategies: Canary, Blue-Green, and A/B Test. This Addon is built on [OpenKruise Rollout](https://openkruise.io/docs/rollout/overview), with Rocket responsible for coordinating multi-cluster rollout processes.

### Architecture Design

![Kruise-Rollout Architecture](images/kruise_rollout_architecture.drawio.png)

### Enable Addon

Enable kruise-rollout on member clusters:

```yaml
apiVersion: storage.rocket.io/v1alpha1
kind: ManagedCluster
metadata:
  name: cluster-1
spec:
  connectionMode: Edge
  addons:
    - name: kruise-rollout
      enabled: true
      config:
        chartVersion: "0.5.0"  # Optional, specify chart version
```

### Rollout Strategies

#### 1. Canary

Canary rollout supports multi-batch progressive updates, with each batch updating a percentage of Pods.

**Basic Configuration**:

```yaml
apiVersion: apps.rocket.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  workload:
    apiVersion: apps/v1
    kind: Deployment
  replicas: 100
  rolloutStrategy:
    type: Canary
    canary:
      steps:
        - weight: 20      # First batch: 20%
          pause:
            duration: 60  # Pause for 60 seconds
        - weight: 50      # Second batch: 50%
          pause:
            duration: 60
        - weight: 100     # Third batch: 100%
```

**Cross-Cluster Batched Rollout**:

With `globalReplicaDistribution` configuration, Rocket automatically calculates the number of Pods to rollout in each cluster:

```yaml
apiVersion: apps.rocket.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  replicas: 100
  rolloutStrategy:
    type: Canary
    canary:
      steps:
        - weight: 20
      globalReplicaDistribution:
        mode: Equal  # or Weighted, Sequential
```

##### Distribution Modes

| Mode | Description | Use Cases |
|------|-------------|-----------|
| **Equal** | Distribute equally across all clusters | Clusters with similar sizes |
| **Weighted** | Distribute based on cluster weights | Clusters with different sizes |
| **Sequential** | Rollout one cluster at a time | Strict control over rollout order |

##### Example: Weighted Distribution

```yaml
globalReplicaDistribution:
  mode: Weighted
  clusterWeights:
    - clusterName: cluster-a
      weight: 30  # 30% of canary pods
    - clusterName: cluster-b
      weight: 70  # 70% of canary pods
```

##### Example: Sequential Rollout

```yaml
globalReplicaDistribution:
  mode: Sequential
---
# Use with ClusterOrder
clusterOrder:
  type: Sequential
  clusters:
    - cluster-canary   # Rollout to canary cluster first
    - cluster-prod-1   # Then production cluster 1
    - cluster-prod-2   # Finally production cluster 2
```

#### 2. Blue-Green

Blue-Green rollout creates a complete new version environment, and switches traffic after validation.

```yaml
apiVersion: apps.rocket.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  rolloutStrategy:
    type: BlueGreen
    blueGreen:
      activeService: my-app-active    # Current version service
      previewService: my-app-preview  # New version preview service
      autoPromotionEnabled: false     # Manual confirmation before switch
      scaleDownDelaySeconds: 600      # Wait 10 minutes before scaling down old version
```

#### 3. A/B Test

A/B Test uses different clusters as baseline and candidate versions for comparison testing.

```yaml
apiVersion: apps.rocket.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  rolloutStrategy:
    type: ABTest
    abTest:
      baselineCluster: cluster-stable      # Baseline version cluster
      candidateClusters:                   # Candidate version clusters
        - cluster-canary-1
        - cluster-canary-2
      trafficSplit: 30  # 30% traffic to candidate version
```

### Pod Count Calculation Logic

#### Data Source

Batched rollout is based on **desired replicas** (scheduler assignment), not actual running replicas:

```
Data Flow:
Application.Spec.Replicas (Global desired)
         ↓
Placement.Topology[].Replicas (Per-cluster desired) ← Used for batch calculation
         ↓
ClustersStatus[].ReadyReplicas (Actually running)
```

#### Calculation Examples

Assume configuration:
- Global 100 replicas, rollout 20%
- Cluster A expects 40 replicas
- Cluster B expects 60 replicas

**Equal Mode**:
```
Global canary = 100 × 20% = 20 pods
Cluster A canary = 20 / 2 = 10 pods
Cluster B canary = 20 / 2 = 10 pods
```

**Weighted Mode (A:30, B:70)**:
```
Cluster A canary = 20 × 30% = 6 pods
Cluster B canary = 20 × 70% = 14 pods
```

**Proportional Mode (default, no GlobalReplicaDistribution)**:
```
Cluster A canary = 20 × 40/100 = 8 pods (proportional to desired replicas)
Cluster B canary = 20 × 60/100 = 12 pods
```

### Traffic Routing

Rocket only handles Pod count batching. **Traffic routing is controlled by users**. Can be implemented via:

#### Istio Traffic Routing

```yaml
rolloutStrategy:
  type: Canary
  canary:
    steps:
      - weight: 20
    trafficRouting:
      istio:
        virtualService: my-app-vs
        destinationRule: my-app-dr
```

Users need to configure VirtualService traffic percentages in Lua or external systems.

#### NGINX Ingress Traffic Routing

```yaml
rolloutStrategy:
  type: Canary
  canary:
    steps:
      - weight: 20
    trafficRouting:
      nginx:
        ingress: my-app-ingress
        annotationPrefix: nginx.ingress.kubernetes.io
```

### Important Notes

#### 1. Rollout Controls Pod Count, Not Traffic Percentage

> ⚠️ **Important**: Rocket's batched rollout controls **Pod count**, not traffic percentage. Traffic routing is controlled by users through Lua scripts or external systems (such as Istio, Nginx Ingress).

This means:
- `weight: 20` means creating 20% new version Pods
- How traffic is distributed to these Pods is decided by users
- The platform has no traffic data and cannot perceive actual traffic distribution

#### 2. Calculation Based on Desired Replicas

Batched calculation uses `Placement.Topology[].Replicas` (desired value assigned by scheduler), not actual running replicas.

**Advantages**:
- Declarative design, stable rollout plan
- Not affected by runtime state fluctuations
- Follows Kubernetes design principles

**Scenario Description**:
- If cluster A expects 40 replicas, actually running 30, when rolling out 20%, calculation is still based on 40 replicas (8 canary pods)
- This ensures rollout plan consistency, avoiding strategy changes due to temporary failures

#### 3. kruise-rollout Deployment Location

kruise-rollout should be deployed on **clusters that need to run workloads with rollout strategies**:

| Cluster Type | Needs kruise-rollout | Description |
|--------------|---------------------|-------------|
| Hub (management plane only) | No | Only runs Rocket controller, no workloads |
| Hub (management + workloads) | **Yes** | Runs both Rocket controller and workloads |
| Edge (workload cluster) | **Yes** | Runs workloads |

**Configuration Examples**:

```yaml
# Scenario 1: Hub cluster only serves as management plane, no workloads
# No need to configure kruise-rollout addon
apiVersion: storage.rocket.io/v1alpha1
kind: ManagedCluster
metadata:
  name: hub-management-only
spec:
  connectionMode: Hub
  # No need to configure kruise-rollout

---
# Scenario 2: Hub cluster runs both management plane and workloads
# Need to enable kruise-rollout for workload rollout strategies
apiVersion: storage.rocket.io/v1alpha1
kind: ManagedCluster
metadata:
  name: hub-with-workloads
spec:
  connectionMode: Hub
  addons:
    - name: kruise-rollout
      enabled: true
      config:
        chartVersion: "0.5.0"

---
# Scenario 3: Edge cluster runs workloads
# Need to enable kruise-rollout
apiVersion: storage.rocket.io/v1alpha1
kind: ManagedCluster
metadata:
  name: edge-cluster-1
spec:
  connectionMode: Edge
  addons:
    - name: kruise-rollout
      enabled: true
```

> 💡 **Best Practice**: It's recommended to dedicate Hub cluster for management plane only, and deploy workloads to Edge clusters. If Hub cluster must run workloads, ensure to enable kruise-rollout addon for it.

#### 4. Sequential Rollout Limitations

When using `Sequential` mode or `ClusterOrder`:
- Next cluster only starts after previous cluster completes rollout
- Need to check previous cluster's Rollout status in Application Status
- If previous cluster rollout fails, subsequent clusters won't start

#### 5. Workload Compatibility

Supported workload types:
- Deployment
- StatefulSet
- CloneSet (OpenKruise)
- Advanced StatefulSet (OpenKruise)

### Configuration Options

| Config Key | Description | Default |
|------------|-------------|---------|
| `chartVersion` | kruise-rollout chart version | `0.5.0` |
| `chartRepoURL` | Helm Chart repository URL | Official repository |
| `chartName` | Chart name | `kruise-rollout` |
| `valuesConfigMap` | ConfigMap with custom Helm values | - |
| `valuesNamespace` | Namespace of the ConfigMap | `kruise-rollout` |

### Custom Helm Values

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: kruise-rollout-values
  namespace: kruise-rollout
data:
  values.yaml: |
    manager:
      replicas: 2
      resources:
        limits:
          cpu: 500m
          memory: 512Mi
---
apiVersion: storage.rocket.io/v1alpha1
kind: ManagedCluster
metadata:
  name: edge-cluster-1
spec:
  addons:
    - name: kruise-rollout
      enabled: true
      config:
        valuesConfigMap: "kruise-rollout-values"
```

### Verify Installation

```bash
# Check if kruise-rollout is installed successfully
kubectl get all -n kruise-rollout

# View Rollout CR
kubectl get rollout -A

# View rollout status
kubectl describe rollout my-app -n default
```

### Troubleshooting

#### Q1: Rollout CR not created?

**A**: Check the following:
1. Whether Application has `rolloutStrategy` configured
2. Whether cluster has kruise-rollout addon enabled
3. Check Manager logs: `kubectl logs -n rocket-system deployment/rocket-manager`

#### Q2: Rollout stuck?

**A**: Possible causes:
1. Pod image pull failure
2. Insufficient resources, Pod cannot be scheduled
3. Health check misconfigured
4. View Rollout status: `kubectl get rollout my-app -o yaml`

#### Q3: How to manually advance rollout?

**A**: Modify Application's `rolloutStrategy.canary.steps` or use kubectl plugin:

```bash
# View current status
kubectl kruise rollout status rollout/my-app

# Manually advance to next step
kubectl kruise rollout approve rollout/my-app
```

### Related Documentation

- [Application API Reference](api.md) - Complete RolloutStrategy definition
- [Architecture Design](architecture.md) - Rocket overall architecture
- [OpenKruise Rollout Documentation](https://openkruise.io/docs/rollout/overview)

---

## VictoriaMetrics Usage Guide

### Overview

VictoriaMetrics Addon deploys monitoring system for multi-cluster environments with Hub-Spoke architecture:

- **Hub side**: Deploys VictoriaMetrics single-node as centralized monitoring storage
- **Edge side**: Deploys vmagent to collect local metrics and push to Hub

### Architecture Design

![VictoriaMetrics Architecture](images/victoriametrics_architecture.drawio.png)

### Important Notes

⚠️ **VictoriaMetrics Single includes built-in agent capabilities**: The VictoriaMetrics single-node deployment includes vmagent-like functionality for data ingestion. Therefore, **no separate vmagent is deployed on Hub clusters** - only on Edge clusters.

**If using your own Prometheus or other VictoriaMetrics cluster**, you need to:
1. Deploy and configure vmagent manually on Hub cluster if needed
2. Configure vmagent's `remoteWrite` URL to point to your external storage
3. Ensure network connectivity between vmagent and target storage

### Basic Configuration

Enable VictoriaMetrics on Hub cluster:

```yaml
apiVersion: storage.rocket.io/v1alpha1
kind: ManagedCluster
metadata:
  name: hub-cluster
spec:
  connectionMode: Hub
  addons:
    - name: victoriametrics
      enabled: true
```

Enable vmagent on Edge cluster with Hub's VM URL:

```yaml
apiVersion: storage.rocket.io/v1alpha1
kind: ManagedCluster
metadata:
  name: edge-cluster
spec:
  connectionMode: Edge
  addons:
    - name: victoriametrics
      enabled: true
      config:
        victoriametricsURL: "http://victoria-metrics-victoria-metrics-single.victoriametrics.svc.cluster.local:8428"
```

### Configuration Options

| Config Key | Description | Default |
|-----------|-------------|---------|
| `vmChartVersion` | VictoriaMetrics chart version | `0.33.0` |
| `vmAgentChartVersion` | vmagent chart version | `0.34.0` |
| `vmStorageClass` | Persistent storage class for VictoriaMetrics | None (no persistence) |
| `vmStorageSize` | Persistent storage size | `16Gi` |
| `vmValuesConfigMap` | ConfigMap name for custom Helm values | - |
| `victoriametricsURL` | VictoriaMetrics URL (auto-populated on Hub) | Auto-detected |

### Storage Configuration

By default, VictoriaMetrics uses emptyDir (no persistent storage). For production use:

```yaml
apiVersion: storage.rocket.io/v1alpha1
kind: ManagedCluster
metadata:
  name: hub-cluster
spec:
  addons:
    - name: victoriametrics
      enabled: true
      config:
        vmStorageClass: "standard"
        vmStorageSize: "50Gi"
```

### Custom Helm Values

Customize via ConfigMap:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: vm-values
  namespace: default
data:
  values.yaml: |
    server:
      persistentVolume:
        enabled: true
        storageClassName: standard
        size: 50Gi
      resources:
        requests:
          cpu: 500m
          memory: 1Gi
---
apiVersion: storage.rocket.io/v1alpha1
kind: ManagedCluster
metadata:
  name: hub-cluster
spec:
  addons:
    - name: victoriametrics
      enabled: true
      config:
        vmValuesConfigMap: "vm-values"
        vmValuesNamespace: "default"
```

### Verify Installation

```bash
# Check VictoriaMetrics on Hub
kubectl get all -n victoriametrics

# Check vmagent on Edge
kubectl get all -n vm-agent

# Query metrics
kubectl port-forward -n victoriametrics svc/victoria-metrics-victoria-metrics-single 8428:8428
curl http://localhost:8428/api/v1/query?query=up
```

## Developing Custom Addons

### 1. Implement Addon Interface

```go
package myaddon

import (
    "github.com/hex-techs/rocket/internal/addon"
    ctrl "sigs.k8s.io/controller-runtime"
)

func init() {
    addon.Register(&MyAddon{})
}

type MyAddon struct{}

func (a *MyAddon) Name() string {
    return "my-addon"
}

func (a *MyAddon) ManagerController(mgr ctrl.Manager) (addon.AddonController, error) {
    return &MyManagerController{
        client: mgr.GetClient(),
    }, nil
}

func (a *MyAddon) AgentController(mgr ctrl.Manager) (addon.AddonController, error) {
    return &MyAgentController{}, nil
}

func (a *MyAddon) Manifests() []runtime.Object {
    return []runtime.Object{
        // CRD definitions
    }
}
```

### 2. Implement Controller

```go
type MyManagerController struct {
    client client.Client
}

func (c *MyManagerController) Reconcile(ctx context.Context, config addon.AddonConfig) error {
    // Implement reconciliation logic
    // 1. Check if already installed
    // 2. Deploy components
    // 3. Update configuration
    return nil
}
```

### 3. Register Addon

Import Addon package in `main.go`:

```go
import (
    _ "github.com/hex-techs/rocket/internal/addon/mcs"
    _ "github.com/your-org/rocket-addons/my-addon"  // Third-party Addon
)
```

## Troubleshooting

### 1. Addon Not Taking Effect

**Symptom**: No response after enabling Addon

**Troubleshooting Steps**:
```bash
# 1. Check ManagedCluster status
kubectl get managedcluster <name> -o yaml

# 2. View Manager logs
kubectl logs -n rocket-system deployment/rocket-manager

# 3. Check if Addon is registered
# Search for "Addon registered" in Manager logs
```

### 2. Helm Installation Failed

**Symptom**: Addon reports "failed to install via Helm"

**Troubleshooting Steps**:
```bash
# 1. Check if Helm Chart exists
helm search repo submariner

# 2. Test Helm installation manually
helm install test-submariner submariner-operator \
  --namespace submariner-operator \
  --set broker.server=<broker-url>

# 3. Check cluster resources
kubectl get all -n submariner-k8s-broker
```

### 3. Configuration Not Synced

**Symptom**: Agent-side didn't receive updated configuration

**Troubleshooting Steps**:
```bash
# 1. Check WebSocket connection
kubectl logs -n rocket-system deployment/rocket-agent | grep "WebSocket"

# 2. View ManagedCluster configuration
kubectl get managedcluster <name> -o jsonpath='{.spec.addons}'

# 3. Check Agent logs
kubectl logs -n rocket-system deployment/rocket-agent
```

## Related Documentation

- [Architecture Design](architecture.md) - Rocket overall architecture
- [Edge Cluster Management](edge.md) - WebSocket tunnel implementation
- [API Reference](api.md) - ManagedCluster CRD specification
