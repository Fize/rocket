# Topology Spread Guide

[中文文档](topology_spread_zh.md)

## Overview

In a multi-cluster setup, spreading replicas across clusters reduces blast radius: a single cluster outage only impacts a portion of replicas.

Rocket supports replica spreading at the **cluster** level via:

- **Scheduling strategy `Spread`**: distribute replicas across multiple clusters using score weights.
- (Optional) **`TopologySpread` score plugin**: bias scores so that topology domains with fewer replicas are preferred.

## What Rocket Does (and Does Not) Do

- Rocket **does not** support `spec.schedule.scheduleType` or `spec.schedule.topologySpreadConstraints` style fields.
- Spreading is implemented by the scheduler and configured via **Application annotations** (strategy/constraints) plus **scheduler score plugins**.
- `TopologySpread` is a **score plugin** (best-effort). It does not implement `maxSkew` / `whenUnsatisfiable` semantics.

## Prerequisites

1. Label clusters with a topology key (for example zone or region):

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

2. Set the total replica count on the Application (`spec.replicas`).

## Current Limitations

- Rocket currently does **not** expose a per-Application switch to enable the `Spread` strategy.
- The `Spread` strategy and the `TopologySpread` score plugin are scheduler-internal configuration. Enabling/changing them requires a custom manager configuration/build.

If you need multi-zone distribution today, a practical workaround is to split workloads into multiple Applications and use `spec.clusterAffinity` to target clusters by zone labels.

## How Topology Spread Scoring Works

If the scheduler enables the `TopologySpread` score plugin, it will:

- Read a topology label value from each cluster (by a configured `topologyKey`, default `topology.kubernetes.io/zone`).
- Track the current planned replica distribution per topology value.
- Prefer clusters in topology domains with **fewer** replicas.

Note: the default manager configuration may not enable this plugin. Currently the Helm chart does not expose scheduler plugin configuration; enabling/changing it requires a custom manager configuration/build.

## Related Documentation

- [Scheduler Design](scheduler.md)
- [API Reference](api.md)
