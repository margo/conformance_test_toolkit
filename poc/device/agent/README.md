# Device Agent

A Margo Compliant device agent for managing workloads on edge devices. The agent provides deployment, their monitoring, and lifecycle management of applications across Kubernetes and Docker Compose environments.

## Table of Contents

- [Overview](#overview)
- [Architecture](#architecture)
  - [Core Components](#core-components)
  - [Supported Runtimes](#supported-runtimes-to-deploy-the-agent)
- [Quick Start](#quick-start)
  - [Prerequisites](#prerequisites)
  - [Build and Run](#build-and-run)
  - [Docker Deployment](#docker-deployment)
- [Configuration](#configuration)
  - [Main Configuration](#main-configuration-configconfigyaml)
  - [Device Capabilities](#device-capabilities-configcapabilitiesjson)
- [Key Features](#key-features)
  - [Event-Driven Architecture](#event-driven-architecture)
  - [State Synchronization](#state-synchronization)
  - [Workload Management](#workload-management)
  - [Monitoring & Health Checks](#monitoring--health-checks)
  - [Persistence](#persistence)
- [API Integration](#api-integration)
- [Development](#development)
  - [Project Structure](#project-structure)
  - [Adding New Runtime Support](#adding-new-runtime-support)
  - [Testing](#testing)
- [Troubleshooting](#troubleshooting)
  - [Common Issues](#common-issues)
  - [Logs](#logs)
  - [Health Checks](#health-checks)
- [Contributing](#contributing)
  - [Code Style](#code-style)
- [License](#license)

## Overview

The Device Agent is part of the Margo platform and serves as the edge component that:
- Onboards devices with the orchestrator
- Reports device capabilities 
- Manages application workload deployments
- Monitors workload health and status
- Synchronizes state with the central orchestrator

## Architecture

The agent follows an event-driven architecture with the following core components:

### Core Components

- **Device Agent** (`agent.go`) - Main orchestrator managing all components
- **Database** (`database/`) - In-memory database with disk persistence for workload state
- **State Syncer** (`device/stateSync.go`) - Synchronizes desired vs actual state with orchestrator
- **Workload Manager** (`workload/manager.go`) - Handles deployment, updates, and removal of workloads
- **Workload Watcher** (`workload/watcher.go`) - Monitors workload health and status
- **Onboarding Manager** (`device/onboarding.go`) - Handles device registration with orchestrator
- **Capabilities Manager** (`device/capabilities.go`) - Reports device capabilities

### Supported Runtimes to Deploy the agent

- **Docker**

## Quick Start

### Prerequisites

- Go 1.24.3 or later
- Kubernetes cluster with kubectl configured (for Kubernetes runtime)
- Docker and Docker Compose (for Docker runtime)

### Build and Run

1. **Clone and build:**
```bash
git clone https://github.com/margo/dev-repo
cd dev-repo
git checkout dev-sprint-4 #<interested-branch-name>
go build ./poc/device/agent
```

2. **Configure the agent:**
```bash
cp -r ./poc/device/agent/config .
# Edit config/config.yaml with your settings
```

3. **Run the agent:**
```bash
./agent -config config/config.yaml
```

### Docker Deployment

```bash
# Build Docker image
docker build -t device-agent ./poc/device/agent

# Run container
docker run -d \
  -v ./config/config.yaml:/app/config/config.yaml \
  -v /root/.kube/config:/app/config/kubeconf.conf \
  -v /tmp/data:/app/data
  device-agent
```

## Configuration

### Main Configuration (`config/config.yaml`)

```yaml
deviceId: device-101
wfmSbiUrl: http://localhost:8082/v1alpha2/margo/sbi/v1
capabilitiesFile: /path/to/capabilities.json
runtimeInfo:
  type: KUBERNETES  # or DOCKER
  kubernetes:
    kubeconfigPath: /root/.kube/config
  # docker:
  #   host: localhost
  #   port: 2376
```

### Device Capabilities (`config/capabilities.json`)

As of now the device capabilities are read from this file. This is static.
Define your device's hardware capabilities like this. It is a predefined structure:

```json
{
  "apiVersion": "device.margo/v1",
  "kind": "DeviceCapability",
  "properties": {
    "id": "my-device-id",
    "vendor": "Device Vendor",
    "modelNumber": "MODEL-123",
    "serialNumber": "SN-456",
    "roles": ["standalone cluster"],
    "resources": {
      "memory": 64,
      "storage": 2000,
      "cpus": [{
        "architecture": "Intel x64",
        "cores": 24,
        "frequency": 6.2
      }]
    },
    "interfaces": [
      {
        "name": "Ethernet Interface",
        "type": "Ethernet",
        "properties": {
          "maxSpeed": "1 Gbps"
        }
      }
    ]
  }
}
```

## Key Features

### Event-Driven Architecture
- Components communicate via database events
- Non-blocking operations with proper context handling
- Graceful shutdown with timeout handling

### State Synchronization
- Periodic sync with orchestrator (configurable interval)
- Event-driven updates for immediate state changes
- Conflict resolution between desired and actual state

### Workload Management
- **Helm Deployments**: Full lifecycle management of Helm charts
- **Docker Compose**: Container orchestration via Docker Compose

### Monitoring & Health Checks
- Workload status monitoring
- Workload status reporting to orchestrator

### Persistence (Custom In-Memory database implementation)
- In-memory database with disk backup
- Automatic state restoration on restart
- Atomic write operations for data consistency

## API Integration

The agent communicates with the orchestrator via Margo compliant REST APIs. Please check the Margo docs.

## Development

### Project Structure

```
poc/device/agent/
├── main.go                 # Application entry point
├── agent.go               # Main agent orchestrator
├── config/                # Configuration files
├── database/              # In-memory database with events
├── device/                # Device management components
│   ├── capabilities.go    # Capabilities reporting
│   ├── onboarding.go     # Device onboarding
│   └── stateSync.go      # State synchronization
├── workload/              # Workload management
│   ├── manager.go         # Deployment manager
│   ├── watcher.go         # Health monitoring
│   ├── deployers/         # Runtime-specific deployers
│   └── monitoring/        # Monitoring implementations
└── types/                 # Shared types and interfaces
```

### Adding New Runtime Support

1. Implement `WorkloadDeployer` interface in `workload/deployers/`
2. Implement `WorkloadMonitor` interface in `workload/monitoring/`
3. Add runtime configuration to `types/config.go`
4. Register deployer and monitor in `agent.go`

### Testing

```bash
# Run tests
go test ./...

# Run with coverage
go test -cover ./...
```

## Troubleshooting

### Common Issues

1. **Connection refused to orchestrator**
   - Verify `wfmSbiUrl` in configuration
   - Check network connectivity
   - Ensure orchestrator is running

2. **Kubernetes deployment failures**
   - Verify kubeconfig path and permissions
   - Check Kubernetes cluster connectivity
   - Validate Helm chart repositories

3. **Docker deployment failures**
   - Ensure Docker daemon is running
   - Check Docker socket permissions
   - Verify Docker Compose version compatibility

### Logs

The agent uses structured logging with different levels:
- `DEBUG`: Detailed operation logs
- `INFO`: General operational information
- `WARN`: Warning conditions
- `ERROR`: Error conditions requiring attention

## Contributing
[TBD]

### Code Style

- Follow Go conventions and `gofmt`
- Use structured logging with context
- Include proper error handling

## License

[License information - TBD]