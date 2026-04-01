# Addon 扩展设计

[English](addon.md)

## 概述

Rocket 采用 **Addon（插件）机制** 实现功能扩展,支持将第三方组件无缝集成到多集群管理平台中。本文档介绍 Addon 的核心架构、实现原理和使用方法。

## 架构总览

![Addon Architecture](images/addon_architecture.drawio.png)

## 核心概念

### 1. Addon 接口定义

Addon 是 Rocket 扩展功能的标准接口,每个 Addon 需要实现以下方法:

```go
type Addon interface {
    // Name 返回 Addon 的唯一标识符
    Name() string
    
    // ManagerController 返回 Manager 端控制器实现
    // 如果 Addon 仅在 Agent 端运行,返回 nil
    ManagerController(mgr ctrl.Manager) (AddonController, error)
    
    // AgentController 返回 Agent 端控制器实现
    // 如果 Addon 仅在 Manager 端运行,返回 nil
    AgentController(mgr ctrl.Manager) (AddonController, error)
    
    // Manifests 返回 Addon 所需的通用 CRD 或资源
    Manifests() []runtime.Object
}
```

### 2. AddonController 接口

AddonController 定义了 Addon 的协调逻辑:

```go
type AddonController interface {
    // Reconcile 处理 Addon 的生命周期
    // 包括安装、升级、配置更新、卸载等
    Reconcile(ctx context.Context, config AddonConfig) error
}

type AddonConfig struct {
    ClusterName string            // 目标集群名称
    Config      map[string]string // Addon 配置
    Client      client.Client     // Kubernetes 客户端
}
```

### 3. 双侧控制器模式

Rocket 采用 **双侧控制器模式**,将 Addon 的部署分为 Manager 端和 Agent 端:

| 端 | 职责 | 适用场景 |
|----|------|---------|
| **Manager 端** | 在 Hub 集群部署核心组件 | Broker、控制平面、配置管理 |
| **Agent 端** | 在成员集群部署工作负载 | Operator、数据平面、本地代理 |

这种设计的优势:
- ✅ 支持 Hub-Spoke 架构
- ✅ 职责清晰,便于维护
- ✅ 可独立部署和升级
- ✅ 支持 Edge 模式(反向隧道)

## 全局注册机制

### 1. 注册表实现

Rocket 使用全局注册表管理所有 Addon:

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

### 2. 自动注册

使用 Go 的 `init()` 函数实现自动注册:

```go
// internal/addon/mcs/mcs.go
func init() {
    addon.Register(&MCSAddon{})  // 程序启动时自动注册
}
```

### 3. 控制器初始化

Manager 启动时,AddonReconciler 会自动初始化所有已注册的 Addon:

```go
func (r *AddonReconciler) SetupWithManager(mgr ctrl.Manager) error {
    r.Controllers = make(map[string]addon.AddonController)
    
    // 遍历所有已注册的 Addon
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

## 配置传递机制

### 配置流程

```
1. 用户在 ManagedCluster.Spec.Addons 中启用 Addon:
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

2. Manager 端 AddonReconciler 监听变更:
   ├─ 调用 ManagerController.Reconcile()
   ├─ 部署核心组件(Broker)
   ├─ 获取连接信息(token, ca)
   └─ 更新 ManagedCluster.Spec.Addons[].Config:
       {
           "brokerURL": "https://manager-api:6443",
           "brokerToken": "eyJhbG...",
           "brokerCA": "LS0tLS...",
           "brokerNamespace": "submariner-k8s-broker"
       }

3. Agent 端通过 WebSocket 隧道同步配置:
   ├─ 监听 ManagedCluster 更新
   ├─ 读取最新的 Config
   └─ 调用 AgentController.Reconcile()
       └─ 部署工作负载组件(Operator)
```

### 配置回写示例

```go
// Manager 端将 Broker 连接信息回写到 CRD
func (c *ManagerController) Reconcile(ctx context.Context, config addon.AddonConfig) error {
    // 1. 部署 Broker
    if err := c.ensureBroker(ctx, config); err != nil {
        return err
    }
    
    // 2. 获取 Broker 连接信息
    brokerInfo, err := c.getBrokerInfo(ctx, config.Config)
    if err != nil {
        return err
    }
    
    // 3. 更新 ManagedCluster.Spec.Addons[].Config
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

## Submariner 接入实现

### 整体架构

Rocket 使用 Addon 机制集成 Submariner,实现跨集群网络和服务发现:

![Submariner Flow](images/submariner_flow.drawio.png)

### 1. Manager 端实现

Manager 端负责部署 Submariner Broker:

```go
type ManagerController struct {
    mgrClient  client.Client
    helmClient helm.HelmClient
}

func (c *ManagerController) Reconcile(ctx context.Context, config addon.AddonConfig) error {
    // 1. 检测 Broker 配置是否变更
    shouldUpdate := c.shouldUpdateBroker(config.Config)
    
    // 2. 部署/升级 Broker (通过 Helm)
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
    
    // 3. 获取 Broker Secret
    secret := &corev1.Secret{}
    c.mgrClient.Get(ctx, types.NamespacedName{
        Name:      "submariner-k8s-broker-client-token",
        Namespace: "submariner-k8s-broker",
    }, secret)
    
    token := string(secret.Data["token"])
    ca := base64.StdEncoding.EncodeToString(secret.Data["ca.crt"])
    
    // 4. 更新 ManagedCluster 配置
    // (见上文"配置回写示例")
}
```

### 2. Agent 端实现

Agent 端负责部署 Submariner Operator:

```go
type AgentController struct {
    helmClient helm.HelmClient
}

func (c *AgentController) Reconcile(ctx context.Context, config addon.AddonConfig) error {
    // 1. 从 Config 读取 Broker 连接信息
    brokerURL := config.Config["brokerURL"]
    brokerToken := config.Config["brokerToken"]
    brokerCA := config.Config["brokerCA"]
    
    // 2. 部署 Submariner Operator (通过 Helm)
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

### 3. 配置变更检测

支持检测配置变更并触发升级:

```go
// Broker 配置变更检测
func (c *ManagerController) shouldUpdateBroker(cfg map[string]string) bool {
    if c.lastBrokerConfig == nil {
        return true  // 首次安装
    }
    
    // 比较关键配置
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

// Submariner 配置变更检测 (类似)
func (c *AgentController) hasSubmarinerConfigChanged(cfg map[string]string) bool {
    // 检测 Chart 版本、Broker Token 等是否变更
}
```

## 使用示例

### 1. 启用 Addon

在 `ManagedCluster` 中启用 Addon:

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

### 2. 自定义 Helm Values

通过 ConfigMap 自定义 Helm Values:

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

### 3. 查看状态

查看 Addon 配置和状态:

```bash
kubectl get managedcluster cluster-1 -o yaml

# 查看 Addon 配置(已回写)
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

## 设计亮点

| 设计点 | 说明 | 优势 |
|--------|------|------|
| **全局注册表** | init() 自动注册,无硬编码 | 扩展性强,符合开闭原则 |
| **双侧控制器** | Manager + Agent 分离部署 | 支持 Hub/Edge 架构 |
| **配置回写** | Manager 将连接信息写入 CRD | Agent 通过 CRD 同步,解耦通信 |
| **Helm 集成** | 通过 Helm 部署组件 | 版本可控,支持升级回滚 |
| **变更检测** | lastConfig 缓存 + key 比较 | 避免重复安装,支持升级 |
| **自定义 Values** | 支持 ConfigMap/Secret 注入 | 灵活性高,满足差异化需求 |
| **幂等性设计** | InstallOrUpgrade 自动处理 | 多次调用不会出错 |

## 最佳实践

### 1. Addon 命名规范

- 使用小写字母和连字符
- 格式: `<功能>-<类型>`
- 示例: `mcs-lighthouse`, `istio-mesh`, `monitoring-prometheus`

### 2. 配置管理

- 将敏感信息存储在 Secret 中
- 使用 ConfigMap 存储非敏感配置
- 支持环境变量覆盖默认值

### 3. 错误处理

- 区分临时错误和永久错误
- 临时错误返回 error 触发重试
- 永久错误更新状态并停止重试

### 4. 版本兼容性

- 在 Addon 中记录支持的版本范围
- 提供版本升级路径
- 支持降级回滚

## 内置 Addon 列表

| Addon 名称 | 功能 | Manager 端 | Agent 端 |
|-----------|------|-----------|---------|
| **mcs-lighthouse** | 跨集群服务发现 | Broker | Submariner Operator |

## 开发自定义 Addon

### 1. 实现 Addon 接口

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
        // CRD 定义
    }
}
```

### 2. 实现控制器

```go
type MyManagerController struct {
    client client.Client
}

func (c *MyManagerController) Reconcile(ctx context.Context, config addon.AddonConfig) error {
    // 实现协调逻辑
    // 1. 检查是否已安装
    // 2. 部署组件
    // 3. 更新配置
    return nil
}
```

### 3. 注册 Addon

在 `main.go` 中导入 Addon 包:

```go
import (
    _ "github.com/hex-techs/rocket/internal/addon/mcs"
    _ "github.com/your-org/rocket-addons/my-addon"  // 第三方 Addon
)
```

## 故障排查

### 1. Addon 未生效

**症状**: 启用 Addon 后无反应

**排查步骤**:
```bash
# 1. 检查 ManagedCluster 状态
kubectl get managedcluster <name> -o yaml

# 2. 查看 Manager 日志
kubectl logs -n rocket-system deployment/rocket-manager

# 3. 检查 Addon 是否注册
# 在 Manager 日志中搜索 "Addon registered"
```

### 2. Helm 安装失败

**症状**: Addon 报错 "failed to install via Helm"

**排查步骤**:
```bash
# 1. 检查 Helm Chart 是否存在
helm search repo submariner

# 2. 手动测试 Helm 安装
helm install test-submariner submariner-operator \
  --namespace submariner-operator \
  --set broker.server=<broker-url>

# 3. 检查集群资源
kubectl get all -n submariner-k8s-broker
```

### 3. 配置未同步

**症状**: Agent 端未收到更新后的配置

**排查步骤**:
```bash
# 1. 检查 WebSocket 连接
kubectl logs -n rocket-system deployment/rocket-agent | grep "WebSocket"

# 2. 查看 ManagedCluster 配置
kubectl get managedcluster <name> -o jsonpath='{.spec.addons}'

# 3. 检查 Agent 日志
kubectl logs -n rocket-system deployment/rocket-agent
```

## 相关文档

- [架构设计](architecture_zh.md) - Rocket 整体架构
- [Edge 集群管理](edge_zh.md) - WebSocket 隧道实现
- [API 参考](api_zh.md) - ManagedCluster CRD 规范
