# 调度器设计

[English](scheduler.md)

## 概述

Rocket 调度器采用插件化架构，类似于 Kubernetes 调度框架，支持灵活的集群选择策略。调度器负责为 Application 选择最合适的目标集群。

## 调度流程

![Scheduling Flow](images/scheduler_flow.png)

## 调度阶段

### 1. 过滤阶段 (Filter)

过滤阶段排除不满足条件的集群，只保留符合要求的候选集群。

**内置过滤插件：**

| 插件 | 功能 | 说明 |
|------|------|------|
| **Health** | 健康检查 | 排除状态为 NotReady 或连接断开的集群 |
| **Affinity** | 亲和性匹配 | 根据 `clusterAffinity.requiredDuringSchedulingIgnoredDuringExecution` 筛选 |
| **TaintToleration** | 污点容忍 | 检查集群污点是否被应用的容忍规则接受 |
| **Capacity** | 资源检查 | 检查集群剩余资源是否满足工作负载需求 |
| **VolumeRestriction** | 存储限制 | 检查集群是否支持所需的存储类型 |

### 2. 评分阶段 (Score)

评分阶段对通过过滤的候选集群进行打分，每个插件返回 0-100 的分数，最终分数为各插件加权求和后归一化。

**内置评分插件：**

| 插件 | 功能 | 评分策略 |
|------|------|----------|
| **Affinity** | 亲和性偏好 | 根据 `preferredDuringSchedulingIgnoredDuringExecution` 计算权重分 |
| **Resource** | 资源利用率 | 支持两种策略：<br>- `LeastAllocated`: 优先选择资源空闲的集群（负载均衡）<br>- `MostAllocated`: 优先填满已有负载的集群（成本优化） |
| **TopologySpread** | 拓扑分布 | 可选评分插件（默认未启用）。优先选择副本数较少的拓扑域，实现均匀分布 |

### 3. 选择阶段 (Select)

选择阶段根据配置的策略，从候选集群中选择最终目标。

| 策略 | 说明 | 适用场景 |
|------|------|----------|
| **SingleCluster** | 选择分数最高的单个集群 | 简单部署、单集群应用 |
| **Spread** | 将副本按分数权重分散到多个集群 | 高可用、容灾部署 |

## 调度算法详解

### 评分算法

#### 1. 资源评分算法 (Resource Plugin)

资源评分插件支持两种策略：

**LeastAllocated（最少分配，默认策略）**

优先选择资源空闲的集群，实现负载均衡：

```
CPU得分 = (可分配量 - 已分配量) × 100 / 可分配量
内存得分 = (可分配量 - 已分配量) × 100 / 可分配量
最终得分 = (CPU得分 + 内存得分) / 2
```

**MostAllocated（最多分配）**

优先填满已有负载的集群，实现成本优化（装箱策略）：

```
CPU得分 = 已分配量 × 100 / 可分配量
内存得分 = 已分配量 × 100 / 可分配量
最终得分 = (CPU得分 + 内存得分) / 2
```

#### 2. 亲和性评分算法 (Affinity Plugin)

亲和性评分根据 `preferredDuringSchedulingIgnoredDuringExecution` 中定义的偏好规则计算：

```
得分 = Σ(匹配的偏好规则权重)
归一化得分 = 得分 × 100 / 最高得分
```

例如，如果定义了以下偏好：
- `tier=high-performance` 权重 100
- `has-gpu=true` 权重 50

集群 A 匹配两个规则，得分为 150；集群 B 仅匹配第一个规则，得分为 100。归一化后 A 为 100 分，B 为 67 分。

#### 3. 拓扑分布评分算法 (TopologySpread Plugin)

拓扑分布插件优先选择副本数较少的拓扑域，实现跨区域均匀分布：

```
原始得分 = 该拓扑域当前副本数
归一化得分 = (最大副本数 - 当前副本数) × 100 / 最大副本数
```

副本数越少的拓扑域得分越高，从而实现均匀分布。

#### 4. 最终得分计算

所有评分插件的结果通过加权求和后归一化到 0-100 范围：

```
加权得分 = Σ(插件得分 × 插件权重)
最终归一化 = (加权得分 - 最小值) × 100 / (最大值 - 最小值)
```

如果所有集群得分相同，统一设置为 50 分（中性值）。

### 选择算法

#### SingleCluster 策略

选择最终得分最高的单个集群：

```
遍历所有候选集群
选择得分最高的集群（得分相同时按名称字典序选择）
将全部副本分配到该集群
```

#### Spread 策略

将副本按权重分散到多个集群，权重使用集群最终归一化得分。

**副本分配算法（汉密尔顿法）**：

```
1. 确定可用集群数量（受 maxClusters 限制）
2. 计算每个集群的权重占比
3. 按权重比例分配副本（向下取整）
4. 使用最大余数法分配剩余副本
   - 按小数部分从大到小排序
   - 每个集群依次分配 1 个副本直到分完
```

**扩容时的副本稳定性**：

扩容（总副本数增加）时遵循"不减少"原则：

```
1. 保持每个现有集群的副本数不变（作为最小值）
2. 仅将新增的副本按权重分配到各集群
3. 现有集群可以获得更多副本，但不会减少
```

例如：
- 当前分布：cluster1=3, cluster2=2（共5副本）
- 扩容到 8 副本
- 新增 3 副本按权重分配
- 结果：cluster1=4, cluster2=3, cluster3=1（假设 cluster3 得分也较高）

**StatefulSet 的瀑布填充算法**：

StatefulSet 使用特殊的瀑布填充（Waterfill）算法，保证有状态应用的序号连续性：

```
1. 按集群名称排序，保持稳定顺序
2. 扩容时：从最后一个有副本的集群开始，依次向后填充
3. 缩容时：从最后一个有副本的集群开始，依次向前移除
```

这确保了 StatefulSet Pod 的序号在多集群间保持有序。

## 调度配置

### 资源策略配置

调度器支持通过命令行参数配置资源调度策略：

```bash
rocket-manager --scheduler-resource-strategy=MostAllocated
```

支持的策略：
- `LeastAllocated` (默认): 将 Pod 分配给空闲资源最多的集群（打散）。
- `MostAllocated`: 将 Pod 分配给空闲资源最少的集群（堆叠/Bin-packing）。

### 调度策略 (Spread vs SingleCluster)

调度器默认使用 `Spread` 策略（将副本打散部署到多个可用集群）。你可以通过 `apps.rocket.io/scheduler-strategy` 注解为特定的 Application 指定使用 `SingleCluster` 策略（强制所有副本部署到单个最优集群）。

#### 1. Spread 策略 (默认)

**设计理念**: 稳定性优先，渐进式趋向理想分布。

| 操作 | 策略 | 是否迁移 | 新集群获得副本？ |
|------|------|----------|------------------|
| 扩容 | 按**缺口 (Deficit)** 分配 | 无迁移，仅新增 | ✅ 优先获得 |
| 缩容 | 按**超额 (Surplus)** 减少 | 无迁移，仅删除 | ❌ 需等扩容 |

##### 扩容算法（基于缺口）

1. **锁定存量**: 保留现有分布作为最低保障。
2. **计算缺口**: 每个集群的 `缺口 = max(0, 理想值 - 当前值)`。
3. **分配新副本**: 按缺口比例分配新增的副本。
4. **处理余数**: 使用最大余数法分配零头。

**示例（扩容，含新集群）**:
- 当前状态: Total=5, C1=3, C2=2, C3=0
- 扩容到: Total=10
- 新权重: C1=10%, C2=20%, C3=70%
- 理想值 (10个): C1=1, C2=2, C3=7
- 缺口: C1=0 (超额), C2=0 (刚好), C3=7
- **结果**: 5 个新副本全部分给 C3 → **C1=3, C2=2, C3=5**

##### 缩容算法（基于超额）

1. **计算超额**: 每个集群的 `超额 = max(0, 当前值 - 理想值)`。
2. **从超额集群减少**: 按超额比例从"吃撑了"的集群删除副本。
3. **无迁移**: 只删除 Pod，不会将 Pod 迁移到其他集群。

**示例（缩容）**:
- 当前状态: Total=10, C1=6, C2=4
- 缩容到: Total=8
- 权重: C1=50%, C2=50%
- 理想值 (8个): C1=4, C2=4
- 超额: C1=2, C2=0
- **结果**: 2 个减少量全部从 C1 扣除 → **C1=4, C2=4**

**注意**: 缩容时新集群**不会**获得任何副本，需要等待下次扩容机会。

##### 初始部署

使用标准的 **最大余数法 (Hamilton Method)** 按权重分配:
- 部署 `replicas: 10`。
- 可用集群:
    *   集群 A (得分: 100) -> 权重约 0.66
    *   集群 B (得分: 50)  -> 权重约 0.33
- **计算过程**:
    *   集群 A 理论值: 10 * 0.66 = 6.6
    *   集群 B 理论值: 10 * 0.33 = 3.3
- **最终结果**: 集群 A 分配 7 个，集群 B 分配 3 个。

#### 2. SingleCluster 策略
**工作原理**:
1.  **过滤 (Filter)**: 排除不满足硬性条件（资源不足、Taints 不匹配等）的集群。
2.  **打分 (Score)**: 根据配置的插件（资源使用率、亲和性等）对剩余集群进行打分。
3.  **选择 (Select)**: 选择得分**最高**的那个集群。
4.  **分配**: 将 **所有** 副本都调度到该集群。

**示例**:
- 部署一个 `replicas: 5` 的应用。
- 集群 A (得分: 80), 集群 B (得分: 60)。
- **结果**: 所有 5 个副本都被调度到集群 A。

#### 配置示例：强制使用 SingleCluster

```yaml
apiVersion: apps.rocket.io/v1alpha1
kind: Application
metadata:
  name: my-app
  annotations:
    apps.rocket.io/scheduler-strategy: SingleCluster
spec:
  # ...
```

### 集群亲和性

集群亲和性用于指定应用应该调度到哪些集群。

**必须满足的条件 (Required)**

```yaml
spec:
  clusterAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      nodeSelectorTerms:
      - matchExpressions:
        - key: env
          operator: In
          values: ["production"]
        - key: region
          operator: In
          values: ["us-east", "us-west"]
```

支持的操作符：

| 操作符 | 说明 |
|--------|------|
| `In` | 标签值在指定列表中 |
| `NotIn` | 标签值不在指定列表中 |
| `Exists` | 标签存在 |
| `DoesNotExist` | 标签不存在 |
| `Gt` | 标签值大于指定值 |
| `Lt` | 标签值小于指定值 |

**优先满足的条件 (Preferred)**

```yaml
spec:
  clusterAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
    - weight: 100
      preference:
        matchExpressions:
        - key: tier
          operator: In
          values: ["high-performance"]
    - weight: 50
      preference:
        matchExpressions:
        - key: has-gpu
          operator: In
          values: ["true"]
```

### 集群污点和容忍

集群可以设置污点来排斥某些工作负载：

```yaml
# ManagedCluster 上的污点
spec:
  taints:
  - key: "dedicated"
    value: "gpu"
    effect: NoSchedule
```

应用可以通过容忍来接受有污点的集群：

```yaml
# Application 上的容忍
spec:
  clusterTolerations:
  - key: "dedicated"
    operator: "Equal"
    value: "gpu"
    effect: "NoSchedule"
```

污点效果：

| 效果 | 说明 |
|------|------|
| `NoSchedule` | 不调度到该集群（除非有匹配的容忍） |
| `PreferNoSchedule` | 尽量不调度到该集群 |
| `NoExecute` | 不调度，且会驱逐已存在的工作负载 |

## 调度示例

### 生产环境部署

```yaml
apiVersion: apps.rocket.io/v1alpha1
kind: Application
metadata:
  name: production-app
spec:
  replicas: 6
  workload:
    apiVersion: apps/v1
    kind: Deployment
  template:
    # ...
  clusterAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      nodeSelectorTerms:
      - matchExpressions:
        - key: env
          operator: In
          values: ["production"]
```

### GPU 工作负载

```yaml
apiVersion: apps.rocket.io/v1alpha1
kind: Application
metadata:
  name: ml-training
spec:
  workload:
    apiVersion: batch/v1
    kind: Job
  template:
    # ...
  clusterAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      nodeSelectorTerms:
      - matchExpressions:
        - key: has-gpu
          operator: In
          values: ["true"]
  clusterTolerations:
  - key: "dedicated"
    value: "gpu"
    effect: "NoSchedule"
```

## 调度失败排查

当应用无法调度时，可以通过以下方式排查：

1. **查看应用状态**
   ```bash
   kubectl get application my-app -o yaml
   ```
   检查 `status.conditions` 中的调度相关条件。

2. **检查集群状态**
   ```bash
   kubectl get managedcluster -o wide
   ```
  确认 `status.state` 与 `status.conditions` 反映 Ready/连接正常。

3. **验证集群标签**
   ```bash
   kubectl get managedcluster -o custom-columns=\
   NAME:.metadata.name,LABELS:.metadata.labels
   ```
   确认集群标签与亲和性规则匹配。

4. **检查集群资源**
   ```bash
   kubectl get managedcluster my-cluster -o yaml
   ```
   查看 `status.resourceSummary` 确认资源充足。

## 相关文档

- [架构设计](architecture_zh.md) - 系统整体架构
- [拓扑分布指南](topology_spread_zh.md) - 跨区域高可用部署详解
- [API 参考](api_zh.md) - Application 完整配置说明
