##### [Back To Main](../README.md)
# WFM, Device-Agent and Observability stack Setup Guide

This directory contains scripts to set up a complete WFM environment with Symphony API, device agents, and observability stack.

## 📋 Prerequisites

- [ Ubuntu/Debian-based VM requirements](../docs/deploy.md#vm-requirements)
- Internet connectivity.
- GitHub account with access to [MARGO Development Repository](https://github.com/margo/dev-repo) and [Forked Symphony Repository](https://github.com/margo/symphony) under margo organization.
- GitHub personal access token (GITHUB_TOKEN).
- GitHub username (GITHUB_USER).
- Create GitHub personal access token using the path `Settings -> Developer settings -> Personal access tokens`. Generate a Token(classic). This GITHUB_TOKEN and GITHUB_USER will be exported as environment variables while running scripts(wfm.sh , device-agent.sh , wfm-cli.sh)


## 🏗️ Architecture Overview

The setup consists of three main components:

1. **WFM Node** (`wfm.sh`) - Main workfleet management server (symphony). For Code First Sandbox purpose this script is deploying 
    1. **WFM (Symphony)**: Workload Fleet Manager
    2. **Gogs**: Stores Margo application/workload artefacts (Application description manifest stored in margo.yaml file and related resources)
    3. **Harbor**: Stores docker images and helm charts as OCI compliant artefacts
    4. **Keycloak**: Earlier it was used while device onboarding to get client-id, this was removed as WFM is generating client-id while authenticating the device with server-side TLS as part of Initial Trust Establishment. WFM Server verifies client certificate and assigns client-id.
    5. **Observability stack**: Ideally observability stack should be hosted on separate VM. In this Code First Sandbox, stack is hosted on WFM and observability data sent to otel collector on device agent. OTEL collector forwards the data to Observability stack(WFM VM). Stack includes jaeger for workload traces, prometheus for workload metrices, grafana and loki for workoad logging.


2. **WFM/Easy CLI** (`wfm-cli.sh`) - Interactive command-line interface for WFM. Used for application package and deployment instance LCM operations

3. **Device Agent Node** (`device-agent.sh`) - Device management agent. Also hosts OTEL collector and promtail components. Promtail is an agent which ships the contents of workload logs to a Grafana Loki instance as OpenTelemetry doesn't have an evolved logging support as compared to metrics and traces.

## 🚀 Quick Start

### Step 1: Environment Variables Setup

Before running any scripts, export the required environment variables:

```bash
# Required for all setups
export GITHUB_USER=<your-github-username>
export GITHUB_TOKEN=<your-github-personal-access-token>

# WFM Node specific (replace with actual IP addresses)
export EXPOSED_HARBOR_IP=<wfm-machine-ip>
export EXPOSED_GOGS_IP=<wfm-machine-ip>
export EXPOSED_KEYCLOAK_IP=<wfm-machine-ip>
export EXPOSED_SYMPHONY_IP=<wfm-machine-ip>
export DEVICE_NODE_IP=<device-agent-machine-ip>

# Branch configuration (change branch as per your need)
export SYMPHONY_BRANCH=main    
export DEV_REPO_BRANCH=main    

# Device Agent script specific
export WFM_IP=<wfm-machine-ip>
export WFM_PORT=8082
export EXPOSED_HARBOR_IP=<wfm-machine-ip>
```

```bash
Examples: 
For wfm.sh script
export GITHUB_USER=<your-github-username>
export GITHUB_TOKEN=<your-github-personal-access-token>  
export EXPOSED_HARBOR_IP=10.139.9.90
export EXPOSED_GOGS_IP=10.139.9.90
export EXPOSED_KEYCLOAK_IP=10.139.9.90
export EXPOSED_SYMPHONY_IP=10.139.9.90
export DEVICE_NODE_IP=10.139.9.151
export SYMPHONY_BRANCH=main
export DEV_REPO_BRANCH=main
sudo -E bash wfm.sh
_________________________________________________________

For device-agent.sh script
export GITHUB_USER=<your-github-username>
export GITHUB_TOKEN=<your-github-personal-access-token>
export DEV_REPO_BRANCH=main
export WFM_IP=10.139.9.90
export WFM_PORT=8082
export EXPOSED_HARBOR_IP=10.139.9.90
export EXPOSED_HARBOR_PORT=8081
sudo -E bash device-agent.sh
```

### Step 2: WFM Node Setup
Execute the WFM setup script on your main server:
```bash
sudo -E bash wfm.sh
```
**Interactive Menu Options:**
1. **PreRequisites: Setup** - Install all dependencies and services. This includes docker, docker compose, redis, rust, go, helm, git, jq, symphony, gogs, harbor, k3s etc.    
2. **PreRequisites: Cleanup** - Remove all installed components.
3. **Symphony: Start** - Start the Symphony API server.
4. **Symphony: Stop** - Stop the Symphony API server.
5. **ObservabilityStack: Start** - Install Jaeger, Prometheus, Grafana, Loki.
6. **ObservabilityStack: Stop** - Uninstall observability components

### Step 3: Device Agent Setup
Execute the device agent script on your device node:

```bash
sudo -E bash device-agent.sh
```

**Interactive Menu Options:**
1. **Install-prerequisites** - Install all dependencies and services.
2. **Uninstall-prerequisites** - Uninstall all dependencies and services.
3. **Device-agent-Start(docker-compose-device)** - Start device agent as docker container.
4. **Device-agent-Stop(docker-compose-device)**  - Stop device agent container.
5. **Device-agent-Start(k3s-device)** - Start device agent as k3s pod.
6. **Device-agent-Stop(k3s-device)**  - stop device agent pod
7. **Device-agent-Status** - check device docker container or pod is running
8. **OTEL-collector-promtail-installation** - Install Opentelemetry collector and Promtail
9. **OTEL-collector-promtail-uninstallation** -  Uninstall Opentelemetry collector and Promtail
10. **cleanup-residual** - Remove residual files.
11. **create_device_rsa_certs** - Create device RSA certificates required for server trust establishment.
12. **create_device_ecdsa_certs** - Create device ECDSA certificates required for server trust establishment.

### Step 4: Using Easy/WFM CLI

Interact with the WFM using the Easy CLI:

```bash
sudo -E bash bash wfm-cli.sh
```

**CLI Features:**
- 📦 List and manage app packages
- 🖥️ List and manage devices
- 🚀 Deploy and manage instances
- 🗑️ Delete packages and deployments

## 🔧 Detailed Setup Instructions

### WFM Node Components

The WFM setup installs and configures:
- **Harbor** - Container registry (Port: 8081)
- **Gogs** - Git repository server (Port: 8084)
- **Symphony API** - Main WFM server (Port: 8082)
- **K3s** - Lightweight Kubernetes
- **Observability Stack** - Jaeger, Prometheus, Grafana, Loki

### Device Agent Components

The device agent setup includes:
- **K3s** - Kubernetes device OR **Docker** - Docker compose device
- **Device Agent** - Connects to WFM server
- **OTEL Collector** - Metrics and traces collection
- **Promtail** - Log forwarding to Loki

## 🌐 Service Access Points

After successful setup, access services at:

| Service | URL | Credentials |
|---------|-----|-------------|
| Symphony API | `http://<WFM_IP>:8082` | - |
| Harbor Registry | `http://<WFM_IP>:8081` | admin/Harbor12345 |
| Gogs | `http://<WFM_IP>:8084` | gogsadmin/admin123 |
| Grafana | `http://<WFM_IP>:32000` | admin/admin |
| Prometheus | `http://<WFM_IP>:30900` | - |
| Jaeger | `http://<WFM_IP>:32500` | - |

## 🔍 Troubleshooting

### Common Issues

**Q1: Package manager lock errors**
```
Waiting for cache lock: Could not get lock /var/lib/dpkg/lock-frontend
```
**Solution:** Wait for system updates to complete, or manually stop unattended upgrades:
```bash
sudo systemctl stop unattended-upgrades
sudo killall unattended-upgrade-shutdown
```

**Q2: Docker daemon not starting**
**Solution:** Check Docker service status and restart:
```bash
sudo systemctl status docker
sudo systemctl restart docker
```

**Q3: K3s cluster not ready**
**Solution:** Verify K3s installation and restart if needed:
```bash
sudo systemctl status k3s
sudo systemctl restart k3s
```

**Q4: Symphony API not accessible**
**Solution:** Check if the API is running and ports are open:
```bash
ps aux | grep symphony-api
sudo netstat -tlnp | grep 8082
```
**Q5: Redis not ready**
**Solution:** Verify redis installation and restart if needed:
```bash
sudo systemctl status redis
sudo systemctl restart redis
```

### Log Locations

- Symphony API: docker logs -f symphony-container-name
- Device Agent: docker logs -f device-agent-container-name (For docker-compose device) 
  OR kubectl logs -f device-agent-pod-name (For k3s device)
- K3s: `sudo journalctl -u k3s`
- Docker: `sudo journalctl -u docker`

## 🧹 Cleanup

To completely remove the installation:

**WFM Node:**
```bash
sudo -E bash wfm.sh
# Select option 2: PreRequisites: Cleanup
```

**Device Agent:**
```bash
sudo -E bash device-agent.sh
# 4) Device-agent-Stop(docker-compose-device)
# 6) Device-agent-Stop(k3s-device)
#10) cleanup-residual
```


### Port Requirements

Ensure the following ports are available:

**WFM Node:**
- 8081 (Harbor)
- 8082 (Symphony API)
- 8084 (Gogs)
- 30900 (Prometheus)
- 32000 (Grafana)
- 32500 (Jaeger)

**Device Node:**
- 6443 (K3s API)
- 30999 (OTEL Collector metrics)

## 🤝 Support

For issues and questions:
1. Check the troubleshooting section above
2. Review log files for error details
3. Ensure all environment variables are properly set
4. Verify network connectivity between nodes