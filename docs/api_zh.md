# API 参考

[English](api.md)

本文档提供 Rocket 所有 Custom Resource Definition (CRD) 的详细规范和使用示例。

## 目录

- [Application](#application)
- [ManagedCluster](#managedcluster)
- [Workspace](#workspace)

---

## Application

Application 是 Rocket 的核心资源，用于定义跨集群部署的应用程序。

### 完整规范

```yaml
apiVersion: apps.rocket.io/v1alpha1
kind: Application
metadata:
  name: my-app
  namespace: default
  labels:
    app: my-app
    team: backend
spec:
  # 期望的总副本数（所有集群合计）
  replicas: 6
  
  # 工作负载类型
  workload:
    apiVersion: apps/v1
    kind: Deployment  # 支持: Deployment, StatefulSet, Job, CronJob
  
  # 标签选择器
  selector:
    matchLabels:
      app: my-app
  
  # 集群亲和性（复用 Kubernetes NodeAffinity 结构）
  clusterAffinity:
    # 硬性要求
    requiredDuringSchedulingIgnoredDuringExecution:
      nodeSelectorTerms:
      - matchExpressions:
        - key: env
          operator: In
          values: ["production"]
        - key: region
          operator: NotIn
          values: ["cn-north"]
    # 软性偏好
    preferredDuringSchedulingIgnoredDuringExecution:
    - weight: 100
      preference:
        matchExpressions:
        - key: gpu
          operator: Exists
  
  # 集群容忍（复用 Kubernetes Toleration 结构）
  clusterTolerations:
  - key: "dedicated"
    operator: "Equal"
    value: "special"
    effect: "NoSchedule"
  
  # 工作负载模板 (RawExtension，支持任意 K8s 资源模板)
  template:
    metadata:
      labels:
        app: my-app
    spec:
      containers:
      - name: app
        image: nginx:1.25
        ports:
        - containerPort: 80
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
          limits:
            cpu: 500m
            memory: 512Mi
  
  # CronJob 专用：调度表达式
  # schedule: "0 * * * *"
  
  # Job/CronJob 属性
  # jobAttributes:
  #   completions: 1
  #   parallelism: 1
  #   backoffLimit: 6
  
  # 针对特定集群的配置覆盖
  overrides:
  - clusterSelector:
      matchLabels:
        region: cn-east
    # 覆盖镜像
    image: nginx:1.25-alpine
    # 覆盖环境变量
    env:
    - name: REGION
      value: "cn-east"
    # 覆盖资源
    resources:
      requests:
        cpu: 200m
        memory: 256Mi
  
  # 弹性策略（PDB）
  resiliency:
    minAvailable: 2
    # 或 maxUnavailable: 1

status:
  # 调度阶段
  schedulingPhase: Scheduled  # Pending | Scheduling | Scheduled | Descheduling | Failed
  
  # 健康阶段
  healthPhase: Healthy  # Healthy | Progressing | Degraded | Unknown
  
  # 全局副本统计
  globalReplicas: 6
  globalReadyReplicas: 6
  
  # 调度结果
  placement:
    topology:
    - name: cluster-a
      replicas: 3
    - name: cluster-b
      replicas: 3
  
  # 已应用的集群列表（用于清理）
  appliedClusters:
  - cluster-a
  - cluster-b
  
  # 各集群状态详情
  clustersStatus:
  - clusterName: cluster-a
    phase: Healthy  # Healthy | Progressing | Degraded | Unknown
    replicas: 3
    readyReplicas: 3
    availableReplicas: 3
  - clusterName: cluster-b
    phase: Progressing
    replicas: 3
    readyReplicas: 2
    availableReplicas: 2
    message: "Waiting for rollout to finish"
  
  # 状态条件
  conditions:
  - type: Scheduled
    status: "True"
    lastTransitionTime: "2024-01-15T10:00:00Z"
    reason: SchedulingComplete
    message: "Successfully scheduled to 2 clusters"
  
  # 观察到的 Generation
  observedGeneration: 1
```

### Spec 字段说明

| 字段 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `workload` | WorkloadGVK | 是 | 工作负载类型 (apiVersion + kind) |
| `replicas` | *int32 | 否 | 期望的总副本数 |
| `selector` | *LabelSelector | 否 | Pod 选择器 |
| `template` | RawExtension | 否 | Pod 模板（任意 K8s 资源模板） |
| `schedule` | string | 否 | CronJob 调度表达式 |
| `suspend` | *bool | 否 | 是否暂停执行 |
| `jobAttributes` | *JobAttributes | 否 | Job/CronJob 专用配置 |
| `clusterAffinity` | *NodeAffinity | 否 | 集群选择亲和性 |
| `clusterTolerations` | []Toleration | 否 | 集群容忍 |
| `overrides` | []PolicyOverride | 否 | 集群特定配置覆盖 |
| `resiliency` | *ResiliencyPolicy | 否 | 弹性策略 (PDB) |

### PolicyOverride 结构

| 字段 | 类型 | 描述 |
|------|------|------|
| `clusterSelector` | *LabelSelector | 匹配目标集群 |
| `image` | string | 覆盖容器镜像 |
| `env` | []EnvVar | 覆盖/追加环境变量 |
| `resources` | *ResourceRequirements | 覆盖资源配置 |
| `command` | []string | 覆盖入口命令 |
| `args` | []string | 覆盖命令参数 |

### JobAttributes 结构

| 字段 | 类型 | 描述 |
|------|------|------|
| `completions` | *int32 | 期望完成的 Pod 数 |
| `parallelism` | *int32 | 最大并行 Pod 数 |
| `backoffLimit` | *int32 | 失败重试次数 |
| `ttlSecondsAfterFinished` | *int32 | 完成后自动清理时间 |
| `successfulJobsHistoryLimit` | *int32 | 保留成功 Job 数量 (CronJob) |
| `failedJobsHistoryLimit` | *int32 | 保留失败 Job 数量 (CronJob) |

### 使用示例

#### 基础 Deployment

```yaml
apiVersion: apps.rocket.io/v1alpha1
kind: Application
metadata:
  name: nginx-basic
spec:
  replicas: 3
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
```

#### 跨区域高可用

```yaml
apiVersion: apps.rocket.io/v1alpha1
kind: Application
metadata:
  name: nginx-ha
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
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - name: nginx
        image: nginx:1.25
```

#### Job 类型应用

```yaml
apiVersion: apps.rocket.io/v1alpha1
kind: Application
metadata:
  name: batch-job
spec:
  workload:
    apiVersion: batch/v1
    kind: Job
  jobAttributes:
    completions: 1
    backoffLimit: 3
    ttlSecondsAfterFinished: 3600
  template:
    spec:
      restartPolicy: OnFailure
      containers:
      - name: job
        image: busybox
        command: ["echo", "Hello from Rocket!"]
```

#### CronJob 类型应用

```yaml
apiVersion: apps.rocket.io/v1alpha1
kind: Application
metadata:
  name: scheduled-job
spec:
  workload:
    apiVersion: batch/v1
    kind: CronJob
  schedule: "0 * * * *"  # 每小时执行
  jobAttributes:
    successfulJobsHistoryLimit: 3
    failedJobsHistoryLimit: 1
  template:
    spec:
      restartPolicy: OnFailure
      containers:
      - name: cleanup
        image: busybox
        command: ["sh", "-c", "echo 'Cleanup task'"]
```

---

## ManagedCluster

ManagedCluster 定义 Rocket 管理的成员集群。这是一个集群级别的资源（非命名空间）。

### 完整规范

```yaml
apiVersion: storage.rocket.io/v1alpha1
kind: ManagedCluster
metadata:
  name: production-east
  labels:
    env: production
    region: us-east
    zone: us-east-1a
    provider: aws
    gpu: "true"
spec:
  # 连接模式
  connectionMode: Hub  # Hub | Edge (默认 Hub)
  
  # API Server 地址
  apiServer: https://prod-east.example.com:6443
  
  # Hub 模式：凭证引用
  secretRef:
    name: prod-east-credentials
  
  # 集群污点（用于调度过滤）
  taints:
  - key: dedicated
    value: special
    effect: NoSchedule
  
  # Addon 配置
  addons:
  - name: metrics-server
    enabled: true

status:
  # 集群状态
  state: Ready  # Pending | Ready | Offline | Rejected
  
  # 集群 ID
  id: "abc123"
  
  # Kubernetes 版本
  kubernetesVersion: "v1.28.4"
  
  # Agent 版本（Edge 模式）
  agentVersion: "v0.1.0"
  
  # API Server URL（Agent 上报）
  apiServerURL: "https://kubernetes.default.svc:443"
  
  # 最后心跳时间（Edge 模式）
  lastKeepAliveTime: "2024-01-15T10:30:00Z"
  
  # 节点统计
  nodeSummary:
  - name: default
    totalNum: 10
    readyNum: 10
  
  # 资源统计
  resourceSummary:
  - name: default
    allocatable:
      cpu: "80"
      memory: "320Gi"
      pods: "1100"
    allocated:
      cpu: "40"
      memory: "160Gi"
    allocating:
      cpu: "5"
      memory: "10Gi"
  
  # Addon 状态
  addonStatus:
  - name: metrics-server
    state: Running
  
  # 状态条件
  conditions:
  - type: Ready
    status: "True"
    lastTransitionTime: "2024-01-15T08:00:00Z"
    reason: ClusterHealthy
    message: "Cluster is healthy and responding"
```

### Spec 字段说明

| 字段 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `connectionMode` | string | 否 | 连接模式: Hub 或 Edge，默认 Hub |
| `apiServer` | string | 否 | Kubernetes API Server 地址 |
| `secretRef` | *LocalObjectReference | Hub 模式需要 | 凭证 Secret 引用 |
| `taints` | []Taint | 否 | 集群污点 |
| `addons` | []ClusterAddon | 否 | Addon 配置 |

### 凭证 Secret 格式

Hub 模式需要提供凭证 Secret，支持以下字段：

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: prod-east-credentials
  namespace: rocket-system
type: Opaque
data:
  # CA 证书 (base64)
  caData: <base64-encoded-ca-cert>
  # 客户端证书 (base64) - 证书认证方式
  certData: <base64-encoded-client-cert>
  # 客户端私钥 (base64) - 证书认证方式
  keyData: <base64-encoded-client-key>
  # 或使用 Token 认证
  token: <base64-encoded-token>
```

### 连接模式

#### Hub 模式

Manager 直接连接集群，需要提供凭证：

```yaml
apiVersion: storage.rocket.io/v1alpha1
kind: ManagedCluster
metadata:
  name: hub-cluster
  labels:
    env: production
spec:
  connectionMode: Hub
  apiServer: https://api.hub-cluster.example.com:6443
  secretRef:
    name: hub-cluster-credentials
```

#### Edge 模式

Agent 主动连接 Manager，通过隧道通信：

```yaml
apiVersion: storage.rocket.io/v1alpha1
kind: ManagedCluster
metadata:
  name: edge-cluster
  labels:
    env: edge
spec:
  connectionMode: Edge
  # apiServer 可选，Agent 会自动上报
  # secretRef 可选，Edge 模式可通过 Tunnel 获取凭证
```

### 集群标签规范

建议使用以下标签便于调度：

| 标签 | 示例值 | 用途 |
|------|--------|------|
| `env` | production, staging, dev | 环境选择 |
| `region` | us-east, cn-north, eu-west | 地理区域 |
| `zone` / `topology.kubernetes.io/zone` | us-east-1a | 可用区 |
| `provider` | aws, azure, gcp, on-prem | 云提供商 |
| `gpu` | true, false | GPU 可用性 |

---

## Workspace

Workspace 用于逻辑隔离，在多个集群中统一创建和管理命名空间。这是一个集群级别的资源。

### 完整规范

```yaml
apiVersion: workspace.rocket.io/v1alpha1
kind: Workspace
metadata:
  name: team-backend
spec:
  # 命名空间名称（默认使用 Workspace 名称）
  name: backend-ns
  
  # 集群选择器
  clusterSelector:
    matchLabels:
      env: production
  
  # 资源约束
  resourceConstraints:
    # ResourceQuota
    quota:
      hard:
        requests.cpu: "100"
        requests.memory: "200Gi"
        limits.cpu: "200"
        limits.memory: "400Gi"
        pods: "1000"
    # LimitRange
    limitRange:
      limits:
      - type: Container
        default:
          cpu: 500m
          memory: 512Mi
        defaultRequest:
          cpu: 100m
          memory: 128Mi

status:
  # 已应用的集群
  appliedClusters:
  - cluster-a
  - cluster-b
  
  # 状态条件
  conditions:
  - type: Ready
    status: "True"
    lastTransitionTime: "2024-01-15T10:00:00Z"
    reason: NamespacesCreated
```

### Spec 字段说明

| 字段 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `name` | string | 否 | 命名空间名称，默认使用 Workspace 名称 |
| `clusterSelector` | *LabelSelector | 否 | 选择目标集群 |
| `resourceConstraints` | *WorkspaceConstraints | 否 | 资源约束配置 |

### 使用示例

#### 团队工作空间

```yaml
apiVersion: workspace.rocket.io/v1alpha1
kind: Workspace
metadata:
  name: team-frontend
spec:
  clusterSelector:
    matchLabels:
      env: production
  resourceConstraints:
    quota:
      hard:
        requests.cpu: "50"
        requests.memory: "100Gi"
        pods: "500"
```

---

## 状态码参考

### Application 状态

| SchedulingPhase | 描述 |
|-----------------|------|
| `Pending` | 等待调度 |
| `Scheduling` | 正在调度 |
| `Scheduled` | 调度完成 |
| `Descheduling` | 正在取消调度 |
| `Failed` | 调度失败 |

| HealthPhase | 描述 |
|-------------|------|
| `Healthy` | 所有副本就绪 |
| `Progressing` | 正在更新中 |
| `Degraded` | 部分副本不可用 |
| `Unknown` | 状态未知 |

| ClusterPhase | 描述 |
|--------------|------|
| `Healthy` | 该集群中应用健康 |
| `Progressing` | 该集群中应用正在更新 |
| `Degraded` | 该集群中应用部分不可用 |
| `Unknown` | 状态未知 |

### ManagedCluster 状态

| State | 描述 |
|-------|------|
| `Pending` | 等待批准/连接 |
| `Ready` | 集群健康可用 |
| `Offline` | 集群离线（心跳超时） |
| `Rejected` | 集群注册被拒绝 |

## 相关文档

- [架构设计](architecture.md) - 整体架构概述
- [调度器设计](scheduler.md) - 调度与策略说明
- [Topology Spread 指南](topology_spread_zh.md) - 多区域/多可用区分布说明
- [Edge 集群管理](edge.md) - Edge 模式配置
