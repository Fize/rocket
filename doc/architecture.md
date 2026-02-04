# Rocket Architecture

[中文文档](architecture_zh.md)

## Overview

Rocket is a cloud-native multi-cluster application management platform that adopts a **Hub-Spoke** architecture model to manage multiple Kubernetes clusters. This document details Rocket's architecture design, core components, data flow, and key implementation details.

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                    Hub Cluster                                           │
│                                                                                          │
│   ┌──────────────────────────────────────────────────────────────────────────────────┐  │
│   │                              Rocket Manager                                        │  │
│   │                                                                                    │  │
│   │  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐                  │  │
│   │  │    Scheduler    │  │   Dispatcher    │  │ StatusReconciler│                  │  │
│   │  │                 │  │                 │  │                 │                  │  │
│   │  │  ┌───────────┐  │  │ - Generate      │  │ - Watch cluster │                  │  │
│   │  │  │  Filter   │  │  │   workloads     │  │   workloads     │                  │  │
│   │  │  │  Plugins  │  │  │ - Apply         │  │ - Aggregate     │                  │  │
│   │  │  ├───────────┤  │  │   overrides     │  │   status        │                  │  │
│   │  │  │  Score    │  │  │ - Distribute    │  │ - Update        │                  │  │
│   │  │  │  Plugins  │  │  │   to clusters   │  │   Application   │                  │  │
│   │  │  ├───────────┤  │  │                 │  │                 │                  │  │
│   │  │  │  Select   │  │  └─────────────────┘  └─────────────────┘                  │  │
│   │  │  └───────────┘  │                                                            │  │
│   │  └─────────────────┘                                                            │  │
│   │                                                                                    │  │
│   │  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐                  │  │
│   │  │ ClusterReconciler│  │  ClientManager  │  │  TunnelServer   │                  │  │
│   │  │                 │  │                 │  │                 │                  │  │
│   │  │ - Watch         │  │ - Build clients │  │ - WebSocket     │                  │  │
│   │  │   ManagedCluster│  │ - Cache clients │  │   listener      │                  │  │
│   │  │ - Update status │  │ - Route by mode │  │ - Session mgmt  │                  │  │
│   │  │ - Health check  │  │   (Hub/Edge)    │  │ - Auth verify   │                  │  │
│   │  └─────────────────┘  └─────────────────┘  └─────────────────┘                  │  │
│   │                                                                                    │  │
│   └──────────────────────────────────────────────────────────────────────────────────┘  │
│                                                                                          │
│   ┌──────────────────────────────────────────────────────────────────────────────────┐  │
│   │                              Custom Resources                                      │  │
│   │                                                                                    │  │
│   │  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐                  │  │
│   │  │   Application   │  │ ManagedCluster  │  │    Workspace    │                  │  │
│   │  │                 │  │                 │  │                 │                  │  │
│   │  │ - Scheduling    │  │ - Connection    │  │ - Namespace     │                  │  │
│   │  │   policy        │  │   config        │  │   management    │                  │  │
│   │  │ - Workload      │  │ - Credentials   │  │ - Resource      │                  │  │
│   │  │   template      │  │ - Labels/Tags   │  │   quotas        │                  │  │
│   │  │ - Affinity      │  │ - Status        │  │                 │                  │  │
│   │  └─────────────────┘  └─────────────────┘  └─────────────────┘                  │  │
│   │                                                                                    │  │
│   └──────────────────────────────────────────────────────────────────────────────────┘  │
│                                                                                          │
└─────────────────────────────────────────────────────────────────────────────────────────┘
                    │                                              │
                    │ Hub Mode                       Edge Mode     │
                    │ (Direct API)                   (Tunnel)      │
                    ▼                                              ▼
┌───────────────────────────────────┐        ┌───────────────────────────────────┐
│       Member Cluster (Hub)         │        │       Member Cluster (Edge)        │
│                                    │        │                                    │
│  ┌──────────────────────────────┐ │        │  ┌──────────────────────────────┐ │
│  │    Kubeconfig / SA Token     │ │        │  │        Rocket Agent          │ │
│  │                              │ │        │  │                              │ │
│  │  Manager accesses cluster    │ │        │  │  - TunnelClient: connects    │ │
│  │  API Server via kubeconfig   │ │        │  │    to Hub WebSocket endpoint │ │
│  │  or ServiceAccount Token     │ │        │  │  - Heartbeat: periodic       │ │
│  │                              │ │        │  │    health reporting          │ │
│  └──────────────────────────────┘ │        │  │  - LocalExecutor: executes   │ │
│                                    │        │  │    Manager operations        │ │
│  Native K8s Resources:            │        │  └──────────────────────────────┘ │
│  - Deployment / StatefulSet       │        │                                    │
│  - Job / CronJob                  │        │  Native K8s Resources:            │
│  - Service / Ingress              │        │  - Deployment / StatefulSet       │
│  - ConfigMap / Secret             │        │  - Job / CronJob                  │
│                                    │        │  - Service / Ingress              │
└───────────────────────────────────┘        └───────────────────────────────────┘
```

## Core Components

### 1. ApplicationReconciler

ApplicationReconciler is Rocket's core controller, responsible for managing the complete lifecycle of Application CRs.

**Key Responsibilities:**
- Watch Application CR create, update, and delete events
- Call Scheduler to select target clusters
- Call Dispatcher to distribute workloads to target clusters
- Coordinate StatusReconciler to aggregate cluster statuses

**Scheduling Flow:**

```
Application Created/Updated
          │
          ▼
┌─────────────────────┐
│  Validate Spec      │
│  - Check workload   │
│  - Validate affinity│
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐
│  Schedule           │
│  - Filter clusters  │
│  - Score clusters   │
│  - Select targets   │
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐
│  Dispatch           │
│  - Generate YAML    │
│  - Apply overrides  │
│  - Create workloads │
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐
│  Update Status      │
│  - Record clusters  │
│  - Set conditions   │
└─────────────────────┘
```

### 2. Scheduler

The Scheduler adopts a plugin-based architecture similar to Kubernetes Scheduler Framework.

**Scheduling Phases:**

| Phase | Description | Plugin Examples |
|-------|-------------|-----------------|
| **PreFilter** | Pre-filtering, prepare scheduling context | ClusterAffinity |
| **Filter** | Filter out clusters that don't meet requirements | ClusterState, ResourceFit |
| **PostFilter** | Post-filter processing | - |
| **PreScore** | Pre-scoring preparation | TopologySpread |
| **Score** | Score candidate clusters | MostAllocated, LeastAllocated |
| **NormalizeScore** | Normalize scores (0-100) | - |
| **Select** | Final cluster selection | Spread, BinPacking |

**Data Structures:**

```go
// ClusterInfo contains cluster scheduling information
type ClusterInfo struct {
    Name           string
    Labels         map[string]string
    Allocatable    corev1.ResourceList  // Allocatable resources
    Requested      corev1.ResourceList  // Requested resources
    State          ClusterState         // Ready/NotReady
    ConnectionMode ConnectionMode       // Hub/Edge
}

// SchedulingContext holds the scheduling context
type SchedulingContext struct {
    Application    *appsv1alpha1.Application
    Clusters       []*ClusterInfo
    FilteredOut    map[string]string   // Filtered clusters and reasons
    Scores         map[string]int64    // Cluster scores
}
```

### 3. ClientManager

ClientManager is one of the most complex components in Rocket, responsible for managing Kubernetes client connections to all member clusters.

**Key Features:**
- Supports both Hub and Edge connection modes
- Creates and caches clients on-demand
- Handles connection failures with graceful retry

**Implementation:**

```go
// ClientManager interface
type ClientManager interface {
    // GetClient returns the client for specified cluster
    GetClient(clusterName string) (client.Client, error)
    
    // GetRESTConfig returns REST config (for dynamic clients)
    GetRESTConfig(clusterName string) (*rest.Config, error)
}

// Internal implementation selects strategy based on ConnectionMode:

func (m *clientManager) GetClient(name string) (client.Client, error) {
    cluster := m.getCluster(name)
    
    switch cluster.Spec.ConnectionMode {
    case ConnectionModeHub:
        // Hub mode: direct connection via kubeconfig/token
        return m.buildDirectClient(cluster)
        
    case ConnectionModeEdge:
        // Edge mode: proxy requests through Tunnel
        return m.buildTunnelClient(cluster)
    }
}
```

**Hub Mode Connection:**

```
Manager ──────────────────────────────► Member Cluster API Server
         HTTPS (kubeconfig/token)
```

**Edge Mode Connection:**

```
Manager ◄───────────────────────────── Agent
         WebSocket (Tunnel)
              │
              │ Requests forwarded through Tunnel
              ▼
         Member Cluster API Server
```

### 4. TunnelServer

TunnelServer is based on [remotedialer](https://github.com/rancher/remotedialer), providing reverse tunnel connections for Edge clusters.

**Workflow:**

```
1. Agent connects to Manager's WebSocket endpoint on startup
   Agent ──────WebSocket──────► Manager:8443/connect

2. Manager verifies Agent identity (Bootstrap Token or SA Token)

3. After connection established, Agent maintains heartbeat
   Agent ────heartbeat (30s)────► Manager

4. When Manager needs to access Edge cluster, requests are forwarded through Tunnel
   Manager ────API Request────► Tunnel ────► Agent ────► Local API Server
```

**Authentication:**

```go
// Edge cluster uses Bootstrap Token for initial authentication
// Headers when Agent connects
headers := http.Header{}
headers.Set("Authorization", "Bearer "+bootstrapToken)
headers.Set("X-Rocket-Cluster-Name", clusterName)
headers.Set("X-Remotedialer-ID", clusterName)
```

### 5. StatusReconciler

StatusReconciler collects workload status from member clusters and aggregates updates to the Application CR.

**Aggregation Logic:**

```go
// Collect Deployment status from each cluster
for _, cluster := range targetClusters {
    client := clientManager.GetClient(cluster)
    
    deployment := &appsv1.Deployment{}
    client.Get(ctx, key, deployment)
    
    clusterStatuses = append(clusterStatuses, ClusterStatus{
        Cluster:     cluster,
        Ready:       deployment.Status.ReadyReplicas,
        Available:   deployment.Status.AvailableReplicas,
        Conditions:  deployment.Status.Conditions,
    })
}

// Aggregate health phase
app.Status.HealthPhase = calculateHealthPhase(clusterStatuses)
// Healthy: all clusters healthy
// Progressing: updates in progress
// Degraded: some clusters unhealthy
```

## Data Flow

### Application Creation Flow

```
User creates Application
         │
         ▼
┌─────────────────┐
│ API Server      │  Hub Cluster
│ (etcd storage)  │
└────────┬────────┘
         │ Watch
         ▼
┌─────────────────┐
│ Application     │
│ Reconciler      │
└────────┬────────┘
         │
    ┌────┴────┐
    │         │
    ▼         ▼
┌───────┐ ┌───────┐
│ Sched │ │ Disp  │
│ uler  │ │ atcher│
└───┬───┘ └───┬───┘
    │         │
    └────┬────┘
         │ Select clusters and distribute
         ▼
┌─────────────────┐     ┌─────────────────┐
│ Member Cluster A│     │ Member Cluster B│
│                 │     │                 │
│ ┌─────────────┐ │     │ ┌─────────────┐ │
│ │ Deployment  │ │     │ │ Deployment  │ │
│ │ replicas: 3 │ │     │ │ replicas: 3 │ │
│ └─────────────┘ │     │ └─────────────┘ │
└─────────────────┘     └─────────────────┘
```

## High Availability

### Manager HA

- Deployed as Kubernetes Deployment, supports multiple replicas
- Uses Leader Election to ensure only one active instance
- Client caching and connection pool management

```yaml
# charts/manager/templates/deployment.yaml
spec:
  replicas: 2
  template:
    spec:
      containers:
      - name: manager
        args:
        - --leader-elect=true
```

### Agent HA

- Supports automatic reconnection with exponential backoff
- Automatic connection rebuild on heartbeat timeout
- Local workloads unaffected by Tunnel disconnection

```go
func (a *Agent) Run() error {
    backoff := wait.Backoff{
        Steps:    math.MaxInt32,
        Duration: time.Second,
        Factor:   2.0,
        Cap:      5 * time.Minute,
    }
    
    return wait.ExponentialBackoff(backoff, func() (bool, error) {
        err := a.connectTunnel()
        if err != nil {
            log.Error(err, "tunnel connection failed, retrying...")
            return false, nil  // Continue retry
        }
        return true, nil
    })
}
```

## Related Documentation

- [Scheduler Design](scheduler.md) - Detailed scheduler architecture and plugin mechanism
- [Topology Spread Guide](topology_spread.md) - Cross-region/zone workload distribution
- [Edge Cluster Management](edge.md) - Detailed tunnel implementation
- [API Reference](api.md) - CRD specifications and examples
