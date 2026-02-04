# Rocket API Reference

[中文文档](api_zh.md)

## CRD Overview

Rocket defines three core Custom Resource Definitions (CRDs):

| CRD | API Group | Description |
|-----|-----------|-------------|
| Application | apps.rocket.io/v1alpha1 | Defines workload templates and multi-cluster scheduling inputs |
| ManagedCluster | storage.rocket.io/v1alpha1 | Represents a managed member cluster |
| Workspace | workspace.rocket.io/v1alpha1 | Namespace provisioning + quota/limits across clusters |

---

## Application

Application defines a workload type (`spec.workload`) plus a PodTemplate-like `spec.template` to be applied to target clusters.

### Spec Fields (selected)

| Field | Type | Notes |
|------|------|------|
| `spec.workload` | object | Required. GVK of the workload (e.g. Deployment/StatefulSet/Job/CronJob) |
| `spec.template` | RawExtension | Required. Pod template content placed into the target workload at `spec.template` (CronJob uses `spec.jobTemplate.spec.template`) |
| `spec.replicas` | *int32 | Optional. For Job, mapped to `spec.parallelism` if not set in jobAttributes |
| `spec.selector` | LabelSelector | Optional. Used for workloads that require `spec.selector` (Deployment/StatefulSet/...) |
| `spec.schedule` | string | Optional. Only used for CronJob workload (`spec.schedule`) |
| `spec.jobAttributes` | object | Optional. Job/CronJob attributes (completions/parallelism/backoffLimit/ttlSecondsAfterFinished/...) |
| `spec.clusterAffinity` | NodeAffinity | Optional. Scheduling input against ManagedCluster labels |
| `spec.clusterTolerations` | []Toleration | Optional. Scheduling input against ManagedCluster taints |
| `spec.overrides` | []PolicyOverride | Optional. Overrides for selected clusters (image/env/resources/command/args) |
| `spec.resiliency` | object | Optional. Creates/updates a PodDisruptionBudget (minAvailable/maxUnavailable) |

### Example (Deployment)

```yaml
apiVersion: apps.rocket.io/v1alpha1
kind: Application
metadata:
  name: example-app
  namespace: default
spec:
  replicas: 6
  workload:
    apiVersion: apps/v1
    kind: Deployment
  clusterAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      nodeSelectorTerms:
      - matchExpressions:
        - key: env
          operator: In
          values: ["production"]
  clusterTolerations:
  - key: "dedicated"
    operator: "Equal"
    value: "gpu"
    effect: "NoSchedule"
  template:
    metadata:
      labels:
        app: example
    spec:
      containers:
      - name: app
        image: nginx:1.25
```

### Status Fields (selected)

| Field | Type | Description |
|------|------|-------------|
| `status.schedulingPhase` | string | Pending/Scheduling/Scheduled/Descheduling/Failed |
| `status.healthPhase` | string | Healthy/Progressing/Degraded/Unknown |
| `status.globalReplicas` | int32 | Total desired replicas |
| `status.globalReadyReplicas` | int32 | Total ready replicas across clusters |
| `status.placement.topology` | []object | Per-cluster replica plan (`name`/`replicas`) |
| `status.clustersStatus` | []object | Per-cluster observed status (replicas/ready/available/phase) |
| `status.conditions` | []Condition | Conditions set by controllers |

---

## ManagedCluster

ManagedCluster defines a member cluster and how Rocket connects to it.

### Example

```yaml
apiVersion: storage.rocket.io/v1alpha1
kind: ManagedCluster
metadata:
  name: cluster-a
  labels:
    env: production
    topology.kubernetes.io/zone: us-east-1a
spec:
  connectionMode: Hub  # Hub | Edge
  apiServer: https://prod-east.example.com:6443
  secretRef:
    name: cluster-a-credentials
  taints:
  - key: dedicated
    value: gpu
    effect: NoSchedule
  addons:
  - name: metrics-server
    enabled: true
status:
  state: Ready
  kubernetesVersion: "v1.28.4"
  lastKeepAliveTime: "2024-01-15T10:30:00Z"
  conditions:
  - type: Ready
    status: "True"
    lastTransitionTime: "2024-01-15T08:00:00Z"
```

### Credential Secret (Hub mode)

The referenced Secret should contain one of token auth or cert auth:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: cluster-a-credentials
  namespace: rocket-system
type: Opaque
data:
  caData: <base64>
  # cert-based auth
  certData: <base64>
  keyData: <base64>
  # or token auth
  token: <base64>
```

---

## Workspace

Workspace provisions a namespace across selected clusters and optionally applies quota/limit ranges.

### Example

```yaml
apiVersion: workspace.rocket.io/v1alpha1
kind: Workspace
metadata:
  name: team-backend
spec:
  # If omitted, defaults to metadata.name
  name: backend-ns
  clusterSelector:
    matchLabels:
      env: production
  resourceConstraints:
    quota:
      hard:
        requests.cpu: "100"
        requests.memory: "200Gi"
        limits.cpu: "200"
        limits.memory: "400Gi"
        pods: "1000"
    limitRange:
      limits:
      - type: Container
        default:
          cpu: "500m"
          memory: "512Mi"
        defaultRequest:
          cpu: "100m"
          memory: "128Mi"
status:
  appliedClusters:
  - cluster-a
  - cluster-b
  conditions:
  - type: Ready
    status: "True"
```

---

## Common Patterns

### Multi-region / multi-zone spread

Rocket uses cluster labels (e.g. `topology.kubernetes.io/zone`) as scheduling inputs, but it does not currently expose a per-Application configuration to enable replica spreading across zones.

See: [Topology Spread Guide](topology_spread.md)

### Canary release by overrides

```yaml
apiVersion: apps.rocket.io/v1alpha1
kind: Application
metadata:
  name: canary-app
spec:
  workload:
    apiVersion: apps/v1
    kind: Deployment
  replicas: 3
  template:
    metadata:
      labels:
        app: canary
    spec:
      containers:
      - name: app
        image: example/app:stable
  overrides:
  - clusterSelector:
      matchLabels:
        canary: "true"
    image: example/app:canary
```

---

## Related Documentation

- [Architecture Overview](architecture.md)
- [Scheduler Design](scheduler.md)
- [Topology Spread Guide](topology_spread.md)
