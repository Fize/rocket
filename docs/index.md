# Rocket Documentation

[中文文档](index_zh.md)

Welcome to the Rocket multi-cluster application management platform documentation.

## Documentation Index

| Document | Description | Audience |
|----------|-------------|----------|
| [Architecture](architecture.md) | System architecture, core components, data flow | Developers, Architects |
| [Scheduler Design](scheduler.md) | Multi-cluster scheduling framework, plugin mechanism | Developers |
| [Topology Spread](topology_spread.md) | Cross-region workload distribution plugin | Developers, Operators |
| [Edge Cluster](edge.md) | Tunnel connection, Bootstrap Token, Agent deployment | Operators, Developers |
| [API Reference](api.md) | CRD specifications, field descriptions, examples | All Users |

## Quick Navigation

### 🚀 Getting Started

If you're new to Rocket, we recommend reading in this order:

1. **[Architecture](architecture.md)** - Understand Rocket's design philosophy and components
2. **[API Reference](api.md)** - Learn how to define Application and ManagedCluster
3. **[Edge Cluster](edge.md)** - Understand how to onboard edge clusters (if needed)

### 🔧 Deep Dive

For understanding Rocket's implementation details:

1. **[Scheduler Design](scheduler.md)** - Understand the core multi-cluster scheduling algorithms
2. **[Topology Spread](topology_spread.md)** - Learn cross-region high availability deployment

### 📋 Common Scenarios

| Scenario | Recommended Document |
|----------|---------------------|
| Deploy first cross-cluster application | [API Reference - Application](api.md#application) |
| Register a new member cluster | [API Reference - ManagedCluster](api.md#managedcluster) |
| Onboard edge cluster behind NAT | [Edge Cluster](edge.md) |
| Configure cross-region HA | [Topology Spread](topology_spread.md) |
| Understand scheduling decisions | [Scheduler Design](scheduler.md) |

## Project Links

- [GitHub Repository](https://github.com/hex-techs/rocket)
- [Issue Tracker](https://github.com/hex-techs/rocket/issues)
- [Contributing Guide](https://github.com/hex-techs/rocket/blob/main/CONTRIBUTING.md)

## Contributing to Documentation

Found an error or want to add content? PRs are welcome! Documentation is written in Markdown format and located in the `doc/` directory.
