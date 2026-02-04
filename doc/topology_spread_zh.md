# 拓扑分布指南

[English](topology_spread.md)

## 概述

在多集群环境中，将副本分散到不同集群可以降低爆炸半径：单个集群故障只会影响部分副本。

Rocket 在**集群层面**支持副本分散，主要依赖：

- **`Spread` 调度策略**：根据集群评分权重将副本分配到多个集群。
-（可选）**`TopologySpread` 评分插件**：让拓扑域（如 zone/region）中副本更少的一侧获得更高分，从而倾向均衡分布。

## Rocket 做什么 / 不做什么

- Rocket **不支持** `spec.schedule.scheduleType`、`spec.schedule.topologySpreadConstraints` 这类字段。
- Rocket 的分散能力由调度器实现，通过 **Application 注解**（策略/约束）和 **调度器评分插件**共同生效。
- `TopologySpread` 是**评分插件（尽力而为）**，不提供 `maxSkew`、`whenUnsatisfiable` 之类的强约束语义。

## 前置条件

1. 为集群打上用于拓扑分组的标签（例如 zone 或 region）：

```yaml
apiVersion: storage.rocket.io/v1alpha1
kind: ManagedCluster
metadata:
  name: us-east-1a
  labels:
    topology.kubernetes.io/zone: us-east-1a
---
apiVersion: storage.rocket.io/v1alpha1
kind: ManagedCluster
metadata:
  name: us-east-1b
  labels:
    topology.kubernetes.io/zone: us-east-1b
```

2. 在 Application 上设置总副本数（`spec.replicas`）。

## 当前限制

- Rocket 目前**不提供**通过单个 Application 级别开关来启用 `Spread` 策略。
- `Spread` 策略以及 `TopologySpread` 打分插件属于调度器内部配置；启用/调整需要自定义 manager 配置/构建。

如果你需要跨可用区分布副本，一个可行的替代方案是把工作负载拆成多个 Application，并通过 `spec.clusterAffinity` 结合集群的可用区标签进行定向调度。

## TopologySpread 评分的工作方式

当调度器启用 `TopologySpread` 评分插件时，它会：

- 按配置的 `topologyKey`（默认 `topology.kubernetes.io/zone`）读取每个集群的标签值。
- 跟踪当前计划中的“拓扑域 -> 副本数”分布。
- 让副本更少的拓扑域得到更高分，从而倾向均衡。

注意：默认的 manager 配置可能未启用该插件。目前 Helm chart 未暴露调度器插件配置；启用/调整需要自定义 manager 配置/构建。

## 相关文档

- [调度器设计](scheduler_zh.md)
- [API 参考](api_zh.md)
