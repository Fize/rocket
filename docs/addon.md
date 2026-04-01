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
