#!/bin/bash
set -e
export PATH="$PATH:/usr/local/go/bin"
# ----------------------------
# Environment & Validation Functions
# ----------------------------

#--- Github Settings to pull the code (can be overridden via env)
GITHUB_USER="${GITHUB_USER:-}"  # Set via env or leave empty
GITHUB_TOKEN="${GITHUB_TOKEN:-}"  # Set via env or leave empty

#--- harbor settings (can be overridden via env)
EXPOSED_HARBOR_IP="${EXPOSED_HARBOR_IP:-127.0.0.1}"
EXPOSED_HARBOR_PORT="${EXPOSED_HARBOR_PORT:-8081}"

#--- branch details (can be overridden via env)
DEV_REPO_BRANCH="${DEV_REPO_BRANCH:-dev-sprint-6}"
WFM_IP="${WFM_IP:-127.0.0.1}"
WFM_PORT="${WFM_PORT:-8082}"

# variables for observability stack
NAMESPACE_OBSERVABILITY="observability"
PROMTAIL_RELEASE="promtail"
OTEL_RELEASE="otel-collector"

validate_pre_required_vars() {
  local required_vars=("GITHUB_USER" "GITHUB_TOKEN" "DEV_REPO_BRANCH" "WFM_IP" "WFM_PORT")
  for var in "${required_vars[@]}"; do
    if [ -z "${!var}" ]; then
      echo "Error: Required environment variable $var is not set"
      exit 1
    fi
  done
}

validate_start_required_vars() {
  local required_vars=("WFM_IP" "WFM_PORT")
  for var in "${required_vars[@]}"; do
    if [ -z "${!var}" ]; then
      echo "Error: Required environment variable $var is not set"
      exit 1
    fi
  done
}

# ----------------------------
# Go Installation Functions
# ----------------------------
install_basic_utilities() {
  # apt update && apt install curl -y
  sudo apt update -y
  sudo apt install -y curl git
  echo "Installation complete: curl and git are installed."

  INSTALL_HELM_V3_15_1=true
  HELM_VERSION="3.15.1"
  HELM_TAR="helm-v${HELM_VERSION}-linux-amd64.tar.gz"
  HELM_BIN_DIR="/usr/local/bin"

  apt update && apt install -y curl dos2unix build-essential gcc libc6-dev
  install_helm
}

# Helm install/uninstall
install_helm() {
  cd $HOME
  if [ "${INSTALL_HELM_V3_15_1}" == "true" ]; then
    echo "Helm Setup"
    if command -v helm >/dev/null 2>&1 && [[ "$(helm version --short | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')" == "${HELM_VERSION}" ]]; then
        echo "Helm version ${HELM_VERSION} is already installed. Skipping."
    else
        echo "Downloading Helm version ${HELM_VERSION}..."
        if ! wget -q "https://get.helm.sh/${HELM_TAR}" ; then
            echo "Failed to download Helm."
            exit 1
        fi
        echo "Extracting Helm..."
        if ! tar -xzf "${HELM_TAR}" ; then
            echo "Failed to extract Helm tarball."
            exit 1
        fi
        echo "Moving Helm to ${HELM_BIN_DIR}..."
        if ! sudo mv "linux-amd64/helm" "${HELM_BIN_DIR}/" ; then
            echo "Failed to move Helm."
            exit 1
        fi
        echo "Helm binary moved successfully."
        echo "Cleaning up..."
        rm "${HELM_TAR}"
        rm -rf linux-amd64/
    fi
  fi
}

install_go() {
  if which go >/dev/null 2>&1; then
    echo 'Go already installed, skipping installation';
    go version;
  else
    echo 'Go not found, installing...';
    # rm -rf /usr/local/go /usr/bin/go
    wget "https://go.dev/dl/go1.23.2.linux-amd64.tar.gz" -O go.tar.gz;
    tar -C /usr/local -xzf go.tar.gz;
    rm go.tar.gz
    export PATH="$PATH:/usr/local/go/bin";
    which go;
    go version;
  fi
}

# ----------------------------
# Repository Functions
# ----------------------------
clone_dev_repo() {
  echo "Cloning dev-repo on ($VM2_HOST)..."
  cd $HOME
  sudo rm -rf dev-repo
  git clone "https://${GITHUB_USER}:${GITHUB_TOKEN}@github.com/margo/dev-repo.git"
  cd dev-repo
  git checkout ${DEV_REPO_BRANCH}
  cd ..
}

# ----------------------------
# Configuration Functions
# ----------------------------
update_agent_sbi_url() {
  echo 'Updating wfm.sbiUrl in agent config...'
  sed -i "s|sbiUrl:.*|sbiUrl: http://$WFM_IP:$WFM_PORT/v1alpha2/margo/sbi/v1|" "$HOME/dev-repo/poc/device/agent/config/config.yaml"
}

update_agent_kubepath() {
  echo 'Updating kubeconfigPath in agent config...'
  sed -i "s|kubeconfigPath:.*|kubeconfigPath: $HOME/.kube/config|" "$HOME/dev-repo/poc/device/agent/config/config.yaml"
}

update_agent_capabilities_path() {
  echo 'Updating capabilities.readFromFile in agent config...'
  sed -i "s|readFromFile:.*|readFromFile: $HOME/dev-repo/poc/device/agent/config/capabilities.json|" "$HOME/dev-repo/poc/device/agent/config/config.yaml"
}

update_agent_config() {
  update_agent_sbi_url
  update_agent_capabilities_path
  update_agent_kubepath
  echo 'Config updates completed.'
}

# ----------------------------
# K3s Installation Functions
# ----------------------------
check_k3s_installed() {
  if command -v k3s >/dev/null 2>&1; then
    echo 'k3s already installed, skipping installation.'
    k3s --version
    return 0
  else
    return 1
  fi
}

install_k3s_dependencies() {
  echo 'Installing k3s dependencies...'
  sudo apt update && sudo apt upgrade -y
  sudo apt install -y curl
}

install_k3s() {
  if ! check_k3s_installed; then
    echo 'Installing k3s...'
    install_k3s_dependencies
    curl -sfL https://get.k3s.io | sh -
  fi
}

verify_k3s_status() {
  echo 'Verifying k3s status...'
  sudo systemctl status k3s --no-pager || true
  sudo k3s kubectl get nodes || true
}

setup_kubeconfig() {
  echo 'Setting up kubeconfig...'
  mkdir -p "$HOME/.kube"
  sudo cp /etc/rancher/k3s/k3s.yaml "$HOME/.kube/config"
  sudo chown $(id -u):$(id -g) "$HOME/.kube/config"
  export KUBECONFIG="$HOME/.kube/config"
  echo 'Kubeconfig setup complete.'
  kubectl get nodes || true
}

setup_k3s() {
  install_k3s
  verify_k3s_status
  setup_kubeconfig
}

# ----------------------------
# Device Agent Build Functions
# ----------------------------
build_device_agent() {
  cd "$HOME/dev-repo"
  echo 'Building device-agent...'
 # go build -o device-agent
  docker-compose -f docker-compose.yml build
}

# ----------------------------
# Device Agent Service Functions
# ----------------------------
start_device_agent_service() {
  echo 'Starting device-agent...'
  cd "$HOME/dev-repo"
  #nohup sudo ./poc/device/agent/device-agent --config poc/device/agent/config/config.yaml > "$HOME/device-agent.log" 2>&1 &
  #echo $! > "$HOME/device-agent.pid"
  docker-compose -f docker-c
  docker-compose -f docker-compose.yml logs -f > "$HOME/device-agent.log" 2>&1 &
}

verify_device_agent_running() {
  ps -eo user,pid,ppid,tty,time,cmd | grep '[d]evice-agent'
  sleep 10
  tail -n 50 "$HOME/device-agent.log"
}

stop_device_agent_service() {
  echo "Stopping device-agent..."
  cd "$HOME/dev-repo"
  docker-compose -f poc/device/agent/docker-compose.yml down
  #if [ -f "$HOME/device-agent.pid" ]; then
  #  local pid=$(cat "$HOME/device-agent.pid")
  #  if kill "$pid" 2>/dev/null; then
  #    echo "device-agent stopped (PID: $pid)"
  #  else
  #    echo "Failed to stop device-agent with PID: $pid"
  #  fi
  #  rm -f "$HOME/device-agent.pid"
  #else
  #  echo 'No PID file found. Attempting to find and kill device-agent processes...'
  #  pkill -f "device-agent" && echo "device-agent processes killed" || echo "No device-agent processes found"
  #fi
}

cleanup_device_agent() {
  echo "Cleaning up device-agent files..."
  [ -f "$HOME/device-agent.log" ] && rm -f "$HOME/device-agent.log" && echo "Removed device-agent.log"
  [ -f "$HOME/dev-repo/poc/device/agent/device-agent" ] && rm -f "$HOME/dev-repo/poc/device/agent/device-agent" && echo "Removed device-agent binary"
}

add_container_registry_mirror_to_k3s() {
  echo "Configuring container registry mirror for k3s..."
  
  # Ask for container registry URL or default to https://registry-1.docker.io
  read -p "Enter container registry URL [https://registry-1.docker.io]: " registry_url
  registry_url=${registry_url:-"https://registry-1.docker.io"}
  
  # Ask for registry username, no default
  read -p "Enter registry username: " registry_user
  if [ -z "$registry_user" ]; then
    echo "❌ Registry username is required"
    return 1
  fi
  
  # Ask for registry password, no default (hidden input)
  read -s -p "Enter registry password: " registry_password
  echo  # New line after hidden input
  if [ -z "$registry_password" ]; then
    echo "❌ Registry password is required"
    return 1
  fi
  
  # Create k3s directory if it doesn't exist
  sudo mkdir -p /var/lib/rancher/k3s
  
  # Backup existing registries.yml if it exists
  if [ -f /var/lib/rancher/k3s/registries.yml ]; then
    sudo cp /var/lib/rancher/k3s/registries.yml /var/lib/rancher/k3s/registries.yml.backup.$(date +%s)
    echo "✅ Backed up existing registries.yml"
  fi
  
  # Add docker registry mirror and credentials in /var/lib/rancher/k3s
  cat <<EOF | sudo tee /var/lib/rancher/k3s/registries.yml
mirrors:
  "$EXPOSED_HARBOR_IP:$EXPOSED_HARBOR_PORT":
    endpoint:
      - "$registry_url"

configs:
  "$EXPOSED_HARBOR_IP:$EXPOSED_HARBOR_PORT":
    auth:
      username: "$registry_user"
      password: "$registry_password"
    tls:
    insecure_skip_verify: true
EOF

  cat <<EOF | sudo tee /var/lib/rancher/k3s/registries.yaml
mirrors:
  "$EXPOSED_HARBOR_IP:$EXPOSED_HARBOR_PORT":
    endpoint:
      - "$registry_url"

configs:
  "$EXPOSED_HARBOR_IP:$EXPOSED_HARBOR_PORT":
    auth:
      username: "$registry_user"
      password: "$registry_password"
    tls:
    insecure_skip_verify: true
EOF

# Add docker registry mirror and credentials in /etc/rancher/k3s
cat <<EOF | sudo tee /etc/rancher/k3s/registries.yaml
mirrors:
  "$EXPOSED_HARBOR_IP:$EXPOSED_HARBOR_PORT":
    endpoint:
      - "$registry_url"

configs:
  "$EXPOSED_HARBOR_IP:$EXPOSED_HARBOR_PORT":
    auth:
      username: "$registry_user"
      password: "$registry_password"
EOF

cat <<EOF | sudo tee /etc/rancher/k3s/registries.yml
mirrors:
  "$EXPOSED_HARBOR_IP:$EXPOSED_HARBOR_PORT":
    endpoint:
      - "$registry_url"

configs:
  "$EXPOSED_HARBOR_IP:$EXPOSED_HARBOR_PORT":
    auth:
      username: "$registry_user"
      password: "$registry_password"
EOF

  echo "✅ Created k3s registries configuration"
  
  # Restart k3s to apply changes
  echo "Restarting k3s to apply registry changes..."
  if sudo systemctl restart k3s; then
    echo "✅ k3s restarted successfully"
    
    # Wait for k3s to be ready
    echo "Waiting for k3s to be ready..."
    for i in {1..30}; do
      if sudo systemctl is-active --quiet k3s; then
        echo "✅ k3s is active and running"
        break
      else
        echo "Waiting for k3s... ($i/30)"
        sleep 2
      fi
    done
    
    # Verify k3s is working
    if sudo k3s kubectl get nodes >/dev/null 2>&1; then
      echo "✅ k3s cluster is responding"
    else
      echo "⚠️ k3s cluster may not be fully ready yet"
    fi
  else
    echo "❌ Failed to restart k3s"
    return 1
  fi
  
  echo "✅ Container registry mirror configuration completed"
}



# ----------------------------
# Main Orchestration Functions
# ----------------------------
install_prerequisites() {
  echo "Installing prerequisites: k3s and others ..."
  validate_pre_required_vars
  install_go
  clone_dev_repo
  setup_k3s
}

start_device_agent() {
  echo "Building and starting device-agent ..."
  validate_start_required_vars
  update_agent_config
  
  build_device_agent
  start_device_agent_service
  verify_device_agent_running
  
  echo 'device-agent started'
}

stop_device_agent() {
  echo "Stopping device-agent on VM2 ($VM2_HOST)..."
  stop_device_agent_service
  echo "Device Agent stopped"
}

uninstall_prerequisites() {
  cleanup_device_agent
}

show_status() {
  echo "Device Agent Status:"
  echo "==================="
  
  if [ -f "$HOME/device-agent.pid" ]; then
    local pid=$(cat "$HOME/device-agent.pid")
    if ps -p "$pid" > /dev/null 2>&1; then
      echo "✅ Device Agent is running (PID: $pid)"
      ps -p "$pid" -o pid,ppid,cmd --no-headers
    else
      echo "❌ Device Agent PID file exists but process is not running"
    fi
  else
    echo "❌ Device Agent PID file not found"
  fi
  
  # Check for any device-agent processes
  local processes=$(ps aux | grep '[d]evice-agent' | wc -l)
  if [ "$processes" -gt 0 ]; then
    echo "Found $processes device-agent process(es):"
    ps aux | grep '[d]evice-agent'
  fi
  
  # Show recent logs if available
  if [ -f "$HOME/device-agent.log" ]; then
    echo ""
    echo "Recent logs (last 10 lines):"
    tail -n 10 "$HOME/device-agent.log"
  fi
}


function install_promtail() {
  echo "📦 Installing Promtail to push logs to Loki at $WFM_IP..."

  cat <<EOF > promtail-values.yaml
config:
  server:
    http_listen_port: 9080
    grpc_listen_port: 0

  positions:
    filename: /tmp/positions.yaml

  clients:
    - url: http://${WFM_IP}:32100/loki/api/v1/push

  scrape_configs:
    - job_name: pod-logs
      static_configs:
        - targets:
            - localhost
          labels:
            job: podlogs
            __path__: /var/log/pods/*/*/*.log
EOF

  helm repo add grafana https://grafana.github.io/helm-charts
  helm repo update

  helm install $PROMTAIL_RELEASE grafana/promtail -f promtail-values.yaml --namespace $NAMESPACE_OBSERVABILITY

  echo "✅ Promtail installed and configured to push logs to Loki"
}
function install_otel_collector() {
  echo "📡 Installing OTEL Collector to send metrics and traces to WFM node..."

  cat <<EOF > otel-values.yaml
mode: deployment
image:
  repository: otel/opentelemetry-collector-contrib

extraEnvs:
  - name: KUBE_NODE_NAME
    valueFrom:
      fieldRef:
        fieldPath: spec.nodeName

config:
  receivers:
    otlp:
      protocols:
        http:
          endpoint: 0.0.0.0:4318
        grpc:
          endpoint: 0.0.0.0:4317

    hostmetrics:
      collection_interval: 30s
      scrapers:
        cpu:
        memory:
        disk:
        filesystem:
        load:
        network:
        processes:
        paging:

    kubeletstats:
      collection_interval: 30s
      auth_type: "serviceAccount"
      endpoint: "https://\${KUBE_NODE_NAME}:10250"
      insecure_skip_verify: true
      metric_groups:
        - container
        - pod
        - node

  exporters:
    otlp:
      endpoint: ${WFM_IP}:30417
      tls:
        insecure: true

    prometheus:
      endpoint: "0.0.0.0:8899"

    debug:
      verbosity: detailed

  processors:
    batch: {}

  service:
    pipelines:
      traces:
        receivers: [otlp]
        processors: [batch]
        exporters: [otlp, debug]

      metrics:
        receivers: [otlp, hostmetrics, kubeletstats]
        processors: [batch]
        exporters: [prometheus, debug]
EOF

  helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts
  helm repo update

  helm install $OTEL_RELEASE open-telemetry/opentelemetry-collector -f otel-values.yaml --namespace $NAMESPACE_OBSERVABILITY

  echo "🔧 Patching OTEL Collector service to expose Prometheus metrics on NodePort 30999..."
  sudo kubectl patch svc otel-collector-opentelemetry-collector \
    -n $NAMESPACE_OBSERVABILITY \
    --type='json' \
    -p='[
      {
        "op": "add",
        "path": "/spec/ports/-",
        "value": {
          "name": "prometheus-metrics",
          "port": 8889,
          "protocol": "TCP",
          "targetPort": 8889,
          "nodePort": 30999
        }
      },
      {
        "op": "replace",
        "path": "/spec/type",
        "value": "NodePort"
      }
    ]'

  echo "✅ OTEL Collector installed and Prometheus metrics exposed at NodePort 30999"
}

# Function to create observability namespace
create_observability_namespace() {
    echo "🔧 Checking observability namespace..."
    
    if sudo kubectl get namespace $NAMESPACE_OBSERVABILITY >/dev/null 2>&1; then
        echo "✅ Namespace '$NAMESPACE_OBSERVABILITY' already exists"
    else
        echo "🔧 Creating namespace '$NAMESPACE_OBSERVABILITY'..."
        sudo kubectl create namespace $NAMESPACE_OBSERVABILITY
        echo "✅ Namespace '$NAMESPACE_OBSERVABILITY' created successfully"
    fi
}


install_otel_collector_promtail() {
  echo "Installing OTEL Collector and Promtail..."
  cd "$HOME/dev-repo/pipeline/observability" || { echo '❌ observability dir missing'; exit 1; }
  create_observability_namespace
  install_promtail
  install_otel_collector
  echo "✅ OTEL Collector and Promtail installation completed."
}

uninstall_otel_collector_promtail() {
  echo "🧹 Uninstalling Promtail and OTEL Collector..."
  cd "$HOME/dev-repo/pipeline/observability" || { echo '❌ observability dir missing'; exit 1; }
   
   # Uninstall helm releases only if they exist
    for release in $PROMTAIL_RELEASE $OTEL_RELEASE; do
        if helm status $release -n "$NAMESPACE_OBSERVABILITY" >/dev/null 2>&1; then
            echo "🗑️ Uninstalling $release..."
            helm uninstall $release --namespace "$NAMESPACE_OBSERVABILITY"
        else
            echo "⏭️ $release not found, skipping..."
        fi
    done


  rm -f promtail-values.yaml otel-values.yaml
  echo "✅ Cleanup complete."

}

cleanup_residual() {
  rm -rf "$HOME/dev-repo"
  rm -rf "$HOME/symphony"
  rm -rf "$HOME/device-agent.log"
  rm -rf "$HOME/device-agent.pid"
}



show_menu() {
  echo "Choose an option:"
  echo "1) install-prerequisites"
  echo "2) uninstall-prerequisites"
  echo "3) device-agent-start"
  echo "4) device-agent-stop"
  echo "5) device-agent-status"
  echo "6) otel-collector-promtail-installation"
  echo "7) otel-collector-promtail-uninstallation"
  echo "8) add-container-registry-mirror-to-k3s"
  echo "9) cleanup-residual"
  read -rp "Enter choice [1-9]: " choice
  case $choice in
    1) install_prerequisites;;
    2) uninstall_prerequisites;;
    3) start_device_agent ;;
    4) stop_device_agent ;;
    5) show_status ;;
    6) install_otel_collector_promtail ;;
    7) uninstall_otel_collector_promtail ;;
    8) add_container_registry_mirror_to_k3s;;
    9) cleanup_residual;;
    *) echo "Invalid choice" ;;
  esac
}

# ----------------------------
# Main Script Execution
# ----------------------------
if [ -z "$1" ]; then
  show_menu
else
  case $1 in
    start) start_device_agent ;;
    stop) stop_device_agent ;;
    install_prerequisites) install_prerequisites;;
    uninstall_prerequisites) uninstall_prerequisites;;
    status) show_status ;;
    install_otel_collector_promtail) install_otel_collector_promtail ;;
    uninstall_otel_collector_promtail) uninstall_otel_collector_promtail ;;
    add_container_registry_mirror_to_k3s) add_container_registry_mirror_to_k3s ;;
    cleanup_residual) cleanup_residual ;;
    *) echo "Usage: $0 {start|stop|status|install_prerequisites|uninstall_prerequisites|install_otel_collector_promtail|uninstall_otel_collector_promtail|add_container_registry_mirror_to_k3s|cleanup_residual}" ;;
  esac
fi
