#!/bin/bash
set -e

# ----------------------------
# Environment & Validation
# ----------------------------

#--- Github Settings to pull the code (can be overridden via env)
GITHUB_USER="${GITHUB_USER:-}"
GITHUB_TOKEN="${GITHUB_TOKEN:-}"

#--- branch details (can be overridden via env)
SYMPHONY_BRANCH="${SYMPHONY_BRANCH:-sup/app-registry-on-oci}"
DEV_REPO_BRANCH="${DEV_REPO_BRANCH:-sup/app-registry-on-oci}"

#--- harbor settings (can be overridden via env)
EXPOSED_HARBOR_IP="${EXPOSED_HARBOR_IP:-127.0.0.1}"
EXPOSED_HARBOR_PORT="${EXPOSED_HARBOR_PORT:-8081}"

#--- symphony settings (can be overridden via env)
EXPOSED_SYMPHONY_IP="${EXPOSED_SYMPHONY_IP:-127.0.0.1}"
EXPOSED_SYMPHONY_PORT="${EXPOSED_SYMPHONY_PORT:-8082}"

#--- device node IP (can be overridden via env) for prometheus to scrape metrics 
DEVICE_NODE_IP="${DEVICE_NODE_IP:-127.0.0.1}"


#--- Registry settings (can be overridden via env)
REGISTRY_URL="${REGISTRY_URL:-http://${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}}"
REGISTRY_USER="${REGISTRY_USER:-admin}"
REGISTRY_PASS="${REGISTRY_PASS:-Harbor12345}"

# OCI Registry organization/namespace
OCI_ORGANIZATION="${OCI_ORGANIZATION:-library}"

# variables for observability stack
NAMESPACE_OBSERVABILITY="observability"
JAEGER_RELEASE="jaeger"
PROM_RELEASE="prometheus"
GRAFANA_RELEASE="grafana"
LOKI_RELEASE="loki"

# the directory to generate and store ssl certs in
CERT_DIR="$HOME/symphony/api/certificates"


# ----------------------------
# Utility Functions
# ----------------------------

info() {
    echo "ℹ️  $1"
}

success() {
    echo "✅ $1"
}


# ----------------------------
# Installation Functions
# ----------------------------
install_basic_utilities() {
  INSTALL_HELM_V3_15_1=true
  HELM_VERSION="3.15.1"
  HELM_TAR="helm-v${HELM_VERSION}-linux-amd64.tar.gz"
  HELM_BIN_DIR="/usr/local/bin"

  apt update && apt install -y curl dos2unix build-essential gcc libc6-dev jq
  install_helm
}

install_redis() {
  echo "Installing Redis..."
  
  if command -v redis-server >/dev/null 2>&1; then
    echo "✅ Redis is already installed."
    redis-server --version
  else
    echo "🔄 Updating package list and installing Redis..."
    sudo apt update
    sudo apt install -y redis-server

    echo "🔧 Configuring Redis to start on boot..."
    sudo systemctl enable redis-server

    echo "🚀 Starting Redis service..."
    sudo systemctl start redis-server

    echo "✅ Redis installation completed."
    redis-server --version
  fi
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
  cd $HOME
  if which go >/dev/null 2>&1; then
    echo 'Go already installed, skipping installation';
    go version;
  else
    echo 'Go not found, installing...';
    rm -rf /usr/local/go /usr/bin/go
    wget "https://go.dev/dl/go1.24.4.linux-amd64.tar.gz" -O go.tar.gz;
    tar -C /usr/local -xzf go.tar.gz;
    rm go.tar.gz
    export PATH="$PATH:/usr/local/go/bin";
    source ~/.bashrc
    which go;
    go version;
  fi
}

install_docker_compose() {
  cd $HOME
  if ! command -v docker >/dev/null 2>&1; then
    echo 'Docker not found. Installing Docker...';
    apt-get remove -y docker docker-engine docker.io containerd runc || true;
    curl -fsSL "https://get.docker.com" -o get-docker.sh; sh get-docker.sh;
    usermod -aG docker $USER;
  else
    echo 'Docker already installed.';
  fi;

  # Docker Compose V2 is included with Docker by default now
  if ! docker compose version >/dev/null 2>&1; then
    echo 'Docker Compose plugin not available. Installing...';
    # This should rarely be needed with modern Docker installations
    curl -L "https://github.com/docker/compose/releases/download/v2.24.6/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose;
    chmod +x /usr/local/bin/docker-compose;
  else
    echo 'Docker Compose plugin already available.';
  fi
  
  # Start and enable Docker daemon
  systemctl start docker
  systemctl enable docker
  
  # Wait for Docker daemon to be active (max 30s)
  for i in $(seq 1 30); do
    if systemctl is-active --quiet docker; then
      echo 'Docker daemon is running.'
      break
    else
      echo 'Waiting for Docker daemon to start... ($i/30)'
      sleep 1
    fi
  done 
}


install_oras() {
  echo "Installing ORAS CLI..."
  
  if command -v oras >/dev/null 2>&1; then
    echo "✅ ORAS is already installed."
    oras version
    return 0
  fi
  
  cd /tmp
  ORAS_VERSION="1.1.0"
  wget "https://github.com/oras-project/oras/releases/download/v${ORAS_VERSION}/oras_${ORAS_VERSION}_linux_amd64.tar.gz"
  tar -xzf "oras_${ORAS_VERSION}_linux_amd64.tar.gz"
  sudo mv oras /usr/local/bin/
  rm "oras_${ORAS_VERSION}_linux_amd64.tar.gz"
  
  echo "✅ ORAS installed successfully"
  oras version
}


# ----------------------------
# Repository Functions
# ----------------------------
clone_symphony_repo() {
  cd "$HOME"
  echo 'Cloning symphony...'
  sudo rm -rf "$HOME/symphony"
  git clone "https://${GITHUB_USER}:${GITHUB_TOKEN}@github.com/margo/symphony.git" "$HOME/symphony"
  cd "$HOME/symphony"
  git checkout ${SYMPHONY_BRANCH} || echo 'Branch ${SYMPHONY_BRANCH} not found'
  echo "symphony checkout to branch ${SYMPHONY_BRANCH} done"
}

clone_dev_repo() {
  cd "$HOME"
  sudo rm -rf "$HOME/dev-repo"
  git clone "https://${GITHUB_USER}:${GITHUB_TOKEN}@github.com/margo/dev-repo.git"
  cd "$HOME/dev-repo"
  git checkout ${DEV_REPO_BRANCH} || echo 'Branch ${DEV_REPO_BRANCH} not found'
  echo "dev-repo checkout to branch ${DEV_REPO_BRANCH} done"
}

# ----------------------------
# Service Setup Functions
# ----------------------------


setup_harbor() {
  if docker ps --format '{{.Names}}' | grep -q harbor; then
    echo 'Harbor is already running, skipping startup.'
  else
    cd "$HOME/dev-repo/pipeline/harbor"
    #Update harbor.yml with EXPOSED_HARBOR_IP
    sudo sed -i "s|^hostname: .*|hostname: $EXPOSED_HARBOR_IP|" harbor.yml
    echo 'Starting Harbor...'
    sudo chmod +x install.sh prepare common.sh
    sudo bash install.sh
    docker ps
    sleep 10
    docker ps | grep harbor || echo 'Harbor did not start properly'
  fi
}



# ----------------------------
# OCI Application Package Push Functions (NEW - replaces Git push)
# ----------------------------

push_nextcloud_to_oci() {
  echo "📦 Pushing Nextcloud application package to OCI Registry..."
  
  local app_dir="$HOME/dev-repo/poc/tests/artefacts/nextcloud-compose/margo-package"
  local repository="${OCI_ORGANIZATION}/nextcloud-compose-app-package"
  local tag="latest"
  
  cd "$app_dir" || { echo "❌ Nextcloud package dir missing"; return 1; }
  
  echo "$REGISTRY_PASS" | oras login "${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}" \
    -u "$REGISTRY_USER" --password-stdin --plain-http
  
  if [ ! -f "margo.yaml" ]; then
    echo "❌ margo.yaml not found in $app_dir"
    return 1
  fi
  
  local files=("margo.yaml:application/vnd.margo.app.description.v1+yaml")
  
  if [ -d "resources" ] && [ "$(ls -A resources 2>/dev/null)" ]; then
    while IFS= read -r file; do
      if [ -f "$file" ]; then
        files+=("$file:application/octet-stream")
      fi
    done < <(find resources -type f 2>/dev/null)
  fi
  
  echo "Pushing files: ${files[@]}"
  oras push "${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}/${repository}:${tag}" \
    --artifact-type "application/vnd.margo.app.v1+json" \
    --plain-http \
    "${files[@]}"
  
  if [ $? -eq 0 ]; then
    echo "✅ Nextcloud package pushed to OCI Registry"
    echo "📍 Location: ${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}/${repository}:${tag}"
  else
    echo "❌ Failed to push Nextcloud package"
    return 1
  fi
}



push_nginx_to_oci() {
  echo "📦 Pushing Nginx application package to OCI Registry..."
  
  local app_dir="$HOME/dev-repo/poc/tests/artefacts/nginx-helm/margo-package"
  local repository="${OCI_ORGANIZATION}/nginx-helm-app-package"
  local tag="latest"
  
  cd "$app_dir" || { echo "❌ Nginx package dir missing"; return 1; }
  
  echo "$REGISTRY_PASS" | oras login "${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}" \
    -u "$REGISTRY_USER" --password-stdin --plain-http
  
  if [ ! -f "margo.yaml" ]; then
    echo "❌ margo.yaml not found in $app_dir"
    return 1
  fi
  
  local files=("margo.yaml:application/vnd.margo.app.description.v1+yaml")
  
  if [ -d "resources" ] && [ "$(ls -A resources 2>/dev/null)" ]; then
    while IFS= read -r file; do
      if [ -f "$file" ]; then
        files+=("$file:application/octet-stream")
      fi
    done < <(find resources -type f 2>/dev/null)
  fi
  
  echo "Pushing files: ${files[@]}"
  oras push "${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}/${repository}:${tag}" \
    --artifact-type "application/vnd.margo.app.v1+json" \
    --plain-http \
    "${files[@]}"
  
  if [ $? -eq 0 ]; then
    echo "✅ Nginx package pushed to OCI Registry"
    echo "📍 Location: ${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}/${repository}:${tag}"
  else
    echo "❌ Failed to push Nginx package"
    return 1
  fi
}



push_otel_to_oci() {
  echo "📦 Pushing OTEL application package to OCI Registry..."
  
  local app_dir="$HOME/dev-repo/poc/tests/artefacts/open-telemetry-demo-helm/margo-package"
  local repository="${OCI_ORGANIZATION}/otel-demo-app-package"
  local tag="latest"
  
  cd "$app_dir" || { echo "❌ OTEL package dir missing"; return 1; }
  
  echo "$REGISTRY_PASS" | oras login "${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}" \
    -u "$REGISTRY_USER" --password-stdin --plain-http
  
  if [ ! -f "margo.yaml" ]; then
    echo "❌ margo.yaml not found in $app_dir"
    return 1
  fi
  
  local files=("margo.yaml:application/vnd.margo.app.description.v1+yaml")
  
  if [ -d "resources" ] && [ "$(ls -A resources 2>/dev/null)" ]; then
    while IFS= read -r file; do
      if [ -f "$file" ]; then
        files+=("$file:application/octet-stream")
      fi
    done < <(find resources -type f 2>/dev/null)
  fi
  
  echo "Pushing files: ${files[@]}"
  oras push "${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}/${repository}:${tag}" \
    --artifact-type "application/vnd.margo.app.v1+json" \
    --plain-http \
    "${files[@]}"
  
  if [ $? -eq 0 ]; then
    echo "✅ OTEL package pushed to OCI Registry"
    echo "📍 Location: ${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}/${repository}:${tag}"
  else
    echo "❌ Failed to push OTEL package"
    return 1
  fi
}


push_custom_otel_to_oci() {
  echo "📦 Pushing Custom OTEL application package to OCI Registry..."
  
  local app_dir="$HOME/dev-repo/poc/tests/artefacts/custom-otel-helm-app/margo-package"
  local repository="${OCI_ORGANIZATION}/custom-otel-helm-app-package"
  local tag="latest"
  
  cd "$app_dir" || { echo "❌ Custom OTEL package dir missing"; return 1; }
  
  # Login to Harbor OCI Registry
  echo "$REGISTRY_PASS" | oras login "${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}" \
    -u "$REGISTRY_USER" --password-stdin --plain-http
  
  # Check if margo.yaml exists
  if [ ! -f "margo.yaml" ]; then
    echo "❌ margo.yaml not found in $app_dir"
    return 1
  fi
  
  # Build file list for ORAS - start with margo.yaml
  local files=("margo.yaml:application/vnd.margo.app.description.v1+yaml")
  
  # Add resource files if directory exists and has files
  if [ -d "resources" ] && [ "$(ls -A resources 2>/dev/null)" ]; then
    while IFS= read -r file; do
      if [ -f "$file" ]; then
        files+=("$file:application/octet-stream")
      fi
    done < <(find resources -type f 2>/dev/null)
  fi
  
  # Push to OCI Registry with Margo-specific artifact type
  echo "Pushing files: ${files[@]}"
  oras push "${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}/${repository}:${tag}" \
    --artifact-type "application/vnd.margo.app.v1+json" \
    --plain-http \
    "${files[@]}"
  
  if [ $? -eq 0 ]; then
    echo "✅ Custom OTEL package pushed to OCI Registry"
    echo "📍 Location: ${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}/${repository}:${tag}"
  else
    echo "❌ Failed to push Custom OTEL package"
    return 1
  fi
}



# ----------------------------
# Build Functions
# ----------------------------

build_maestro_cli() {
  CLI_DIR="$HOME/symphony/cli";
  if [ -d "$CLI_DIR" ]; then 
    cd "$CLI_DIR"; 
    go mod tidy; 
    go build -o maestro; 
  fi
}


enable_tls_in_symphony_api() {
  cd $HOME
  echo "Enabling tls in symphony API server (will generate certs and seed their settings in symphony-api-margo.json)..."
  collect_certs_info 
  generate_server_certs
  # replace value of "tls": false, to "tls": true
  sed -i "s|\"tls\": false|\"tls\": true|" "$HOME/symphony/api/symphony-api-margo.json"
  echo "TLS Config is setup and seeded in symphony-api-margo.json"
}

install_jaeger() {
  echo "🔄 Refreshing Jaeger Helm repo..."
  helm repo remove jaegertracing || true
  helm repo add jaegertracing https://jaegertracing.github.io/helm-charts
  helm repo update

  echo "🚀 Installing Jaeger with OTLP and NodePort configuration..."
  helm install $JAEGER_RELEASE jaegertracing/jaeger \
    --namespace $NAMESPACE_OBSERVABILITY \
    --set agent.enabled=false \
    --set collector.enabled=true \
    --set collector.otlp.enabled=true \
    --set collector.service.type=NodePort \
    --set collector.service.nodePort=30417 \
    --set collector.service.additionalPorts[0].name=otlp-grpc \
    --set collector.service.additionalPorts[0].port=4317 \
    --set collector.service.additionalPorts[0].protocol=TCP \
    --set query.enabled=true \
    --set query.service.type=NodePort \
    --set query.service.nodePort=32500

  echo "⏳ Waiting for Jaeger pods to initialize..."
  sleep 10

  echo "🛠 Patching Jaeger Collector Service for OTLP gRPC..."
  sudo kubectl patch svc ${JAEGER_RELEASE}-collector \
    -n $NAMESPACE_OBSERVABILITY \
    --type='json' \
    -p='[
      {
        "op": "add",
        "path": "/spec/ports/-",
        "value": {
          "name": "otlp-grpc",
          "port": 4317,
          "protocol": "TCP",
          "targetPort": 4317,
          "nodePort": 30417
        }
      }
    ]'

  echo "🛠 Patching Jaeger Collector Service for OTLP HTTP..."
  sudo kubectl patch svc ${JAEGER_RELEASE}-collector \
    -n $NAMESPACE_OBSERVABILITY \
    --type='json' \
    -p='[
      {
        "op": "add",
        "path": "/spec/ports/-",
        "value": {
          "name": "otlp-http",
          "port": 4318,
          "protocol": "TCP",
          "targetPort": 4318,
          "nodePort": 30418
        }
      }
    ]'

  echo "✅ Jaeger setup complete!"
  echo "🌐 Query UI: NodePort 32500"
  echo "📡 OTLP gRPC: Port 4317"
  echo "📡 OTLP HTTP: Port 4318"
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

install_prometheus() {
  cd "$HOME/dev-repo/pipeline/observability"
  echo "📡 Setting up Prometheus to expose metrics for OTEL Collector..."

  cat <<EOF > prometheus-values.yaml
server:
  image:
    repository: prom/prometheus
    tag: latest
  service:
    type: NodePort
    nodePort: 30900
  persistentVolume:
    enabled: false
  serverFiles:
    prometheus.yml:
      global:
        scrape_interval: 5s
      scrape_configs:
        - job_name: 'otel-collector'
          static_configs:
            - targets: ['${DEVICE_NODE_IP}:30999']
EOF

  helm repo remove prometheus-community || true
  helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
  helm repo update

  helm install $PROM_RELEASE prometheus-community/prometheus \
    --namespace $NAMESPACE_OBSERVABILITY \
    -f prometheus-values.yaml

  echo "✅ Prometheus setup complete!"
  echo "📊 Prometheus UI: NodePort 30900"
  echo "📡 Metrics exposed at ${DEVICE_NODE_IP}:30999"

  patch_prometheus_configmap
}

patch_prometheus_configmap() {
  cd "$HOME/dev-repo/pipeline/observability"
  echo "🛠 Applying Prometheus ConfigMap with DEVICE_NODE_IP..."

  CM_SOURCE="collector-scrape-cm-change.txt"
  CM_TARGET="collector-scrape-cm-change.yaml"

  if [ ! -f "$CM_SOURCE" ]; then
    echo "❌ Source ConfigMap file '$CM_SOURCE' not found."
    exit 1
  fi

  sed "s|__DEVICE_NODE_IP__|${DEVICE_NODE_IP}|g" "$CM_SOURCE" > "$CM_TARGET"

  echo "📄 Applying ConfigMap with force replace..."
  
  # Method 1: Force replace (recommended) 
  sudo kubectl replace -f "$CM_TARGET" --force --namespace "$NAMESPACE_OBSERVABILITY" || {
    echo "⚠️ Force replace failed, trying server-side apply..."
    # Method 2: Server-side apply (handles conflicts better) 
    sudo kubectl apply -f "$CM_TARGET" --server-side --force-conflicts --namespace "$NAMESPACE_OBSERVABILITY" || {
      echo "⚠️ Server-side apply failed, trying delete and recreate..."
      # Method 3: Delete and recreate as last resort
      sudo kubectl delete configmap prometheus-server -n "$NAMESPACE_OBSERVABILITY" --ignore-not-found=true
      sleep 3
      sudo kubectl apply -f "$CM_TARGET" --namespace "$NAMESPACE_OBSERVABILITY"
    }
  }

  echo "🔄 Restarting Prometheus pod to apply new config..."
  sudo kubectl rollout restart deployment prometheus-server -n "$NAMESPACE_OBSERVABILITY" || \
  sudo kubectl delete pod -l app=prometheus,component=server -n "$NAMESPACE_OBSERVABILITY" || \
  echo "⚠️ Pod restart may be needed manually."

  rm -f "$CM_TARGET"
  echo "✅ ConfigMap applied and temporary file removed."
}


install_loki() {
  echo "📦 Installing Loki for log aggregation..."

  cat <<EOF > loki-values.yaml
deploymentMode: SingleBinary
chunksCache:
  enabled: false
loki:
  auth_enabled: false
  commonConfig:
    replication_factor: 1
  limits_config:
    allow_structured_metadata: false
  storage:
    type: filesystem
  schemaConfig:
    configs:
      - from: 2020-10-24
        store: boltdb-shipper
        object_store: filesystem
        schema: v11
        index:
          prefix: index_
          period: 24h
  storage_config:
    boltdb_shipper:
      active_index_directory: /tmp/loki/index
      cache_location: /tmp/loki/cache
    filesystem:
      directory: /tmp/loki/chunks
singleBinary:
  replicas: 1
read:
  replicas: 0
write:
  replicas: 0
backend:
  replicas: 0
EOF

  helm install $LOKI_RELEASE grafana/loki -f loki-values.yaml --namespace $NAMESPACE_OBSERVABILITY

  echo "🔧 Patching Loki service to expose via NodePort 32100..."
 sudo kubectl patch svc loki -n $NAMESPACE_OBSERVABILITY \
    --type='json' \
    -p='[
      {
        "op": "replace",
        "path": "/spec/type",
        "value": "NodePort"
      },
      {
        "op": "add",
        "path": "/spec/ports/0/nodePort",
        "value": 32100
      }
    ]'

  echo "✅ Loki installed and exposed at NodePort 32100"
}

install_grafana() {
  echo "📊 Installing Grafana..."
  helm repo remove grafana || true
  helm repo add grafana https://grafana.github.io/helm-charts
  helm repo update

  helm install $GRAFANA_RELEASE grafana/grafana \
    --namespace $NAMESPACE_OBSERVABILITY \
    --set service.type=NodePort \
    --set service.nodePort=32000 \
    --set adminPassword='admin' \
    --set persistence.enabled=false

  echo "✅ Grafana installed!"
  echo "🌐 Grafana UI available at NodePort 32000"
  echo "🔐 Login with username: admin and password: admin"
}

observability_stack_install(){
echo "Observability stack installation started"

# Check if collector-scrape-cm-change.txt file exists
if [ ! -f "$HOME/dev-repo/pipeline/observability/collector-scrape-cm-change.txt" ]; then
    echo "Error: collector-scrape-cm-change.txt file not found in $HOME/dev-repo/pipeline/observability"
    echo "Please ensure the file exists before proceeding."
    exit 1
fi

echo "collector-scrape-cm-change.txt file found, proceeding..."
  create_observability_namespace
  install_jaeger
  install_prometheus
  install_grafana
  install_loki
echo "Observability stack installation completed"
}



observability_stack_uninstall(){
    echo "Observability stack uninstall started"
    cd "$HOME/dev-repo/pipeline/observability" || { echo '❌ observability dir missing'; exit 1; }

    # Uninstall helm releases only if they exist
    for release in $PROM_RELEASE $JAEGER_RELEASE $GRAFANA_RELEASE $LOKI_RELEASE; do
        if helm status $release -n "$NAMESPACE_OBSERVABILITY" >/dev/null 2>&1; then
            echo "🗑️ Uninstalling $release..."
            helm uninstall $release --namespace "$NAMESPACE_OBSERVABILITY"
        else
            echo "⏭️ $release not found, skipping..."
        fi
    done

    
    # Wait for pods to be completely terminated
    echo "Waiting for pods to be terminated..."
    
    # Wait for specific pods to be deleted
    kubectl wait --for=delete pods -l app.kubernetes.io/instance=jaeger --timeout=300s || true
    kubectl wait --for=delete pods -l app.kubernetes.io/instance=grafana --timeout=300s || true
    kubectl wait --for=delete pods -l app.kubernetes.io/instance=loki --timeout=300s || true
    kubectl wait --for=delete pods -l app.kubernetes.io/instance=prometheus --timeout=300s || true
    
    rm -f prometheus-values.yaml loki-values.yaml collector-scrape-cm-change.yaml
    echo "Observability stack uninstall completed"
}

add_container_registry_mirror_to_k3s() {
  echo "Configuring container registry mirror for k3s..."

  # ---------------------------------------------------
  # Load registry settings from environment variables
  # ---------------------------------------------------
  registry_url="${REGISTRY_URL:-http://${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}}"
  registry_user="${REGISTRY_USER:-admin}"
  registry_password="${REGISTRY_PASS:-Harbor12345}"

										 
												   
								  
											
			
	
										 
														
  echo "Using registry mirror: $registry_url"
  echo "Using registry credentials: $registry_user / ******"
  # ---------------------------------------------------
											
			
	

  # Create k3s directory if needed
  # ---------------------------------------------------
  sudo mkdir -p /var/lib/rancher/k3s
  sudo mkdir -p /etc/rancher/k3s

  # Backup existing registries if present
  if [ -f /var/lib/rancher/k3s/registries.yml ]; then
    sudo cp /var/lib/rancher/k3s/registries.yml /var/lib/rancher/k3s/registries.yml.backup.$(date +%s)
    echo "✅ Backed up /var/lib/rancher/k3s/registries.yml"
  fi

																	  
														  
		
											
			 
					   

		
											
		 
								
									
		
							  
   

  # ---------------------------------------------------
  # Write the registry config
  # ---------------------------------------------------
  cat <<EOF | sudo tee /var/lib/rancher/k3s/registries.yml >/dev/null
mirrors:
  "${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}":
    endpoint:
      - "${registry_url}"

configs:
  "${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}":
    auth:
      username: "${registry_user}"
      password: "${registry_password}"
    tls:
      insecure_skip_verify: true
EOF

																
													 
		
											
			 
					   

		
											
		 
								
									
   

  sudo cp /var/lib/rancher/k3s/registries.yml /var/lib/rancher/k3s/registries.yaml
  sudo cp /var/lib/rancher/k3s/registries.yml /etc/rancher/k3s/registries.yml
  sudo cp /var/lib/rancher/k3s/registries.yml /etc/rancher/k3s/registries.yaml
											
			 
					   

  echo "✅ Created k3s registry mirror configuration"
 # ---------------------------------------------------
		 
								
									
   

  # ---------------------------------------------------
						
								
  echo "Restarting k3s..."
  if sudo systemctl restart k3s; then
    echo "✅ k3s restarted successfully"
  else
    echo "❌ Failed to restart k3s"
    return 1
  fi

  # Wait for k3s active
  echo "Waiting for k3s to come up..."
  for i in {1..30}; do
    if sudo systemctl is-active --quiet k3s; then
      echo "✅ k3s is running"
      break
		  
										 
			   
    fi
										 
    sleep 2
		
  done

  echo "Checking cluster..."
  if sudo k3s kubectl get nodes >/dev/null 2>&1; then
    echo "✅ k3s cluster is responding"
  else
    echo "⚠️ k3s cluster not ready yet"
	  
	  
									
			
  fi

  echo "✅ Registry mirror configuration completed."
}


# ----------------------------
# Uninstall Functions (Reverse Chronological Order)
# ----------------------------
uninstall_prerequisites() {
  echo "Running complete uninstallation in reverse chronological order..."
   
  # Step 1: Remove Symphony binaries and builds
  cleanup_symphony_builds
    
  # Step 2: Remove cloned repositories
  remove_cloned_repositories
  
  # Step 3: Uninstall Rust
  uninstall_rust
  
  # Step 4: Uninstall Docker and Docker Compose
  uninstall_docker_compose
  
  # Step 5: Uninstall Go
  uninstall_go

  # Step 6: Stop harbor service
  stop_harbor_service
  
  # Step 7: Remove basic utilities and cleanup
  cleanup_basic_utilities
 
  echo "Complete uninstallation finished"
}


cleanup_symphony_builds() {
  echo "1. Cleaning up Symphony builds..."
  
  # Remove built binaries
  [ -f "$HOME/symphony/api/symphony-api" ] && rm -f "$HOME/symphony/api/symphony-api" && echo "✅ Removed symphony-api binary"
  [ -f "$HOME/symphony/cli/maestro" ] && rm -f "$HOME/symphony/cli/maestro" && echo "✅ Removed maestro CLI binary"
  
  # Clean Rust build artifacts
  RUST_DIR="$HOME/symphony/api/pkg/apis/v1alpha1/providers/target/rust"
  if [ -d "$RUST_DIR/target" ]; then
    rm -rf "$RUST_DIR/target" && echo "✅ Removed Rust build artifacts"
  fi

  # remove the generated server cerificates as well
  rm -rf $CERT_DIR
  
  # Clean Go build cache
  if command -v go >/dev/null 2>&1; then
    go clean -cache -modcache 2>/dev/null && echo "✅ Cleaned Go build cache"
  fi
}


remove_cloned_repositories() {
  echo "2. Removing cloned repositories..."
  
  # Remove dev-repo
  [ -d "$HOME/dev-repo" ] && sudo rm -rf "$HOME/dev-repo" && echo "✅ Removed dev-repo"
  
  # Remove symphony repo
  [ -d "$HOME/symphony" ] && sudo rm -rf "$HOME/symphony" && echo "✅ Removed symphony repository"
}

uninstall_rust() {
  echo "3. Uninstalling Rust..."
  
  if [ -d "$HOME/.cargo" ]; then
    # Remove Rust installation
    if command -v rustup >/dev/null 2>&1; then
      rustup self uninstall -y && echo "✅ Uninstalled Rust via rustup"
    else
      rm -rf "$HOME/.cargo" "$HOME/.rustup" && echo "✅ Removed Rust directories manually"
    fi
    
    # Remove from PATH in shell profiles
    sed -i '/\.cargo\/env/d' "$HOME/.bashrc" "$HOME/.profile" 2>/dev/null
    echo "✅ Removed Rust from shell profiles"
  else
    echo "ℹ️ Rust was not installed"
  fi
}

uninstall_docker_compose() {
  echo "4. Uninstalling Docker and Docker Compose..."
  
  # Stop Docker daemon
  systemctl stop docker 2>/dev/null && echo "✅ Stopped Docker daemon"
  systemctl disable docker 2>/dev/null && echo "✅ Disabled Docker daemon"
  
  # Remove Docker Compose
  [ -f "/usr/local/bin/docker-compose" ] && rm -f "/usr/local/bin/docker-compose" && echo "✅ Removed Docker Compose"
  
  # Remove Docker (optional - uncomment if you want complete removal)
  # apt-get remove -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
  # rm -rf /var/lib/docker /etc/docker
  # groupdel docker 2>/dev/null
  # echo "✅ Completely removed Docker"
  
  echo "⚠️ Docker engine left installed (remove manually if needed)"
}

uninstall_go() {
  echo "5. Uninstalling Go..."
  
  # Remove Go installation
  [ -d "/usr/local/go" ] && rm -rf "/usr/local/go" && echo "✅ Removed Go from /usr/local/go"
  
  # Remove Go from PATH in shell profiles
  sed -i '/\/usr\/local\/go\/bin/d' "$HOME/.bashrc" "$HOME/.profile" 2>/dev/null
  
  # Remove GOPATH and other Go environment variables
  sed -i '/GOPATH\|GOROOT\|GOPRIVATE/d' "$HOME/.bashrc" "$HOME/.profile" 2>/dev/null
  
  # Clear Go environment for current session
  unset GOPATH GOROOT GOPRIVATE
  
  echo "✅ Removed Go installation and environment variables"
}

stop_harbor_service() {
  echo "6. Stopping and removing Harbor service..."

  # Stop Harbor container
  if docker ps --format '{{.Names}}' | grep -q harbor; then
    cd "$HOME/dev-repo/pipeline/harbor"
    docker compose down --remove-orphans --volumes 2>/dev/null && echo "✅ Stopped Harbor containers"
    sleep 10
  fi

  # Remove Harbor compose directory
  [ -d "$HOME/dev-repo/pipeline/harbor" ] && rm -rf "$HOME/dev-repo/pipeline/harbor" && echo "✅ Removed Harbor compose directory"

  # Remove Harbor images
  # docker images | grep harbor | awk '{print $3}' | xargs -r docker rmi -f && echo "✅ Removed Harbor images"
}

cleanup_basic_utilities() {
  echo "7. Final cleanup of basic utilities..."
  
  # Remove temporary files
  rm -f /tmp/go.tar.gz /tmp/resp.json /tmp/headers.txt get-docker.sh 2>/dev/null && echo "✅ Removed temporary files"
  
  # Clear exported variables
  unset EXPOSED_HARBOR_IP EXPOSED_HARBOR_PORT EXPOSED_SYMPHONY_IP EXPOSED_SYMPHONY_PORT
  
  # Note: Not removing curl as it might be needed by system
  echo "⚠️ Basic utilities (curl) left installed as they may be system dependencies"
  
  echo "✅ Environment cleanup completed"
  echo ""
  echo "🔄 Please restart your shell or run 'source ~/.bashrc' to apply PATH changes"
}


build_custom_otel_container_images() {
  echo "Building/Downloading Custom Otel images..."

  cd "$HOME/dev-repo/poc/tests/artefacts/custom-otel-helm-app/code/app"
  docker build . -t "${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}/library/custom-otel-app:latest"
  echo "Ensuring Harbor registry login..."
  docker login "${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}" -u admin -p Harbor12345
  
  # Docker push them to the harbor registry
  echo "Pushing otel images to Harbor..."
  docker push "${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}/library/custom-otel-app:latest"
  
  OTEL_APP_CONTAINER_URL="${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}/library/custom-otel-app"
  deploy_file="$HOME/dev-repo/poc/tests/artefacts/custom-otel-helm-app/code/helm/values.yaml"
  tag="latest"
  
  echo "Preparing Helm chart..."
  cd "$HOME/dev-repo/poc/tests/artefacts/custom-otel-helm-app/code"
  
  # Read existing chart version 
  CHART_FILE="$HOME/dev-repo/poc/tests/artefacts/custom-otel-helm-app/code/helm/Chart.yaml"
  CHART_VERSION=$(grep "^version:" "$CHART_FILE" | awk '{print $2}')
  
  echo "Using existing chart version: $CHART_VERSION"
  
  # Replace placeholders in values.yaml
  echo "Replacing placeholders in values.yaml..."
  sed -i "s|{{REPOSITORY}}|$OTEL_APP_CONTAINER_URL|g" "$deploy_file" 2>/dev/null || true
  sed -i "s|{{TAG}}|$tag|g" "$deploy_file" 2>/dev/null || true
  
  # Package and push chart with existing version
  echo "Packaging Helm chart version $CHART_VERSION..."
  helm package helm/
  
  echo "Pushing chart to Harbor..."
  helm push "custom-otel-helm-${CHART_VERSION}.tgz" "oci://${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}/library" --plain-http
  
  # Update margo.yaml in package directory with placeholders
  HELM_REPOSITORY="oci://${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}/library/custom-otel-helm"
  HELM_REVISION="$CHART_VERSION"
  helm_deploy_file="$HOME/dev-repo/poc/tests/artefacts/custom-otel-helm-app/margo-package/margo.yaml"

  echo "Updating margo.yaml with chart version $CHART_VERSION..."
  
  # Only replace placeholders if they exist, don't modify existing values
  sed -i "s|{{HELM_REPOSITORY}}|$HELM_REPOSITORY|g" "$helm_deploy_file" 2>/dev/null || true
  sed -i "s|{{HELM_REVISION}}|$HELM_REVISION|g" "$helm_deploy_file" 2>/dev/null || true
  
  echo "✅ Custom otel chart version $CHART_VERSION successfully pushed to Harbor"
  echo "📦 Chart: oci://${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}/library/custom-otel-helm:$CHART_VERSION"
  echo "🔄 Updated margo.yaml to reference version $CHART_VERSION"
  
}


# Alternative simpler version without jq dependency
add_insecure_registry_to_daemon() {
  echo "Adding insecure registry to Docker daemon (simple version)..."
  
  local registry_url="${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}"
  local daemon_config="/etc/docker/daemon.json"
  
  # Create Docker directory if it doesn't exist
  mkdir -p /etc/docker
  
  # Backup existing config if it exists
  [ -f "$daemon_config" ] && cp "$daemon_config" "$daemon_config.backup.$(date +%s)"
  
  # Create or update daemon.json
  tee "$daemon_config" > /dev/null <<EOF
{
  "insecure-registries": ["$registry_url"]
}
EOF
  
  echo "✅ Configured insecure registry: $registry_url"
  echo "Current daemon.json:"
  cat "$daemon_config"
  
  # Restart Docker daemon
  echo "Restarting Docker daemon..."
  systemctl restart docker
  
  # Wait for Docker to be ready
  for i in {1..30}; do
    if systemctl is-active --quiet docker; then
      echo "✅ Docker daemon restarted successfully"
      return 0
    fi
    echo "Waiting for Docker... ($i/30)"
    sleep 10
  done
  
  echo "❌ Docker daemon failed to restart properly"
  return 1
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
# Main Orchestration Functions
# ----------------------------
install_prerequisites() {
  echo "Running all pre-req setup tasks..."
  install_basic_utilities
  install_go
  install_docker_compose
  add_insecure_registry_to_daemon
  setup_k3s
  install_redis
  install_oras  # NEW: Install ORAS for OCI operations
  
  clone_symphony_repo
  clone_dev_repo
  add_container_registry_mirror_to_k3s
  
   
  setup_harbor
  build_custom_otel_container_images
  
  # NEW: Push application packages to OCI Registry instead of Git
  echo "📦 Pushing application packages to OCI Registry..."
  push_nextcloud_to_oci
  push_nginx_to_oci
  push_otel_to_oci
  push_custom_otel_to_oci
  
  echo "✅ Setup completed - Application packages now in OCI Registry!"
  echo "✅ Workload Fleet Manager pre-requisites installation finished."
}


start_symphony() {
  echo "Starting Symphony API server on..."
  export PATH="$PATH:/usr/local/go/bin";
  # Build phase
  build_maestro_cli   
  # verify_symphony_api
  enable_tls_in_symphony_api
  start_symphony_api_container
}

start_symphony_api_container(){

    cd "$HOME/symphony/api"
	  echo "Building Symphony API container..."																			   
    
    # Check for required environment variables
    if [ -z "$GITHUB_USER" ] || [ -z "$GITHUB_TOKEN" ]; then
        echo "Error: GITHUB_USER and GITHUB_TOKEN environment variables must be set"
        echo "Current values:"
        echo "  GITHUB_USER: ${GITHUB_USER:-'(not set)'}"
        echo "  GITHUB_TOKEN: ${GITHUB_TOKEN:-'(not set)'}"
        return 1
    fi
    
    echo "Using GitHub credentials for user: $GITHUB_USER"

    # Stop and remove existing container if present
    echo "Stopping and removing existing symphony-api-container if present..."
    docker stop symphony-api-container 2>/dev/null || true
    docker rm symphony-api-container 2>/dev/null || true
	  pkill -f "symphony-api" 2>/dev/null || true										   
    
    # Check if image already exists
    if docker image inspect margo-symphony-api:latest >/dev/null 2>&1; then
        echo "✅ Image margo-symphony-api:latest already exists, skipping build"
    else
        echo "🔨 Building Symphony API container..."
        
        # Create credential files
        echo "$GITHUB_USER" > github_username.txt
        echo "$GITHUB_TOKEN" > github_token.txt

        # Build with secrets
        docker build \
          --secret id=github_username,src=github_username.txt \
          --secret id=github_token,src=github_token.txt \
          -t margo-symphony-api:latest \
          .. -f Dockerfile

        # Clean up credential files
        rm github_username.txt github_token.txt
        
        if [ $? -ne 0 ]; then
            echo "❌ Failed to build Symphony API container"
            return 1
        fi
        
	   
        echo "✅ Symphony API container built successfully with tag: margo-symphony-api:latest"
    fi
    
    # Run the container
    echo "🚀 Starting Symphony API container..."

    docker run -dit --name symphony-api-container \
        --network host \
        -p 8082:8082 \
        -e LOG_LEVEL=Debug \
        -v "$HOME/symphony/api/certificates:/certificates" \
        -v "$HOME/symphony/api":/configs \
        -e CONFIG=symphony-api-margo.json \
        margo-symphony-api:latest
        
    if [ $? -eq 0 ]; then
        echo "✅ Symphony API container started successfully"
        echo "📡 Container is running on port 8082"
        echo "🏷️  Container name: symphony-api-container"
    else
        echo "❌ Failed to start Symphony API container"
        return 1
    fi
 
}


stop_symphony() {
  echo "Stopping and removing Symphony API container..."
  
  # Stop the container if running
  if docker ps --format '{{.Names}}' | grep -q "symphony-api-container"; then
    docker stop symphony-api-container && echo '✅ Symphony API container stopped'
  fi
  
  # Remove the container if it exists
  if docker ps -a --format '{{.Names}}' | grep -q "symphony-api-container"; then
    docker rm symphony-api-container && echo '✅ Symphony API container removed'
  else
    echo 'ℹ️ Symphony API container not found'
  fi
  
  # Prompt user to delete Redis data
  echo ""
  echo "⚠️  Warning: Deleting Redis data will require device re-onboarding"
  read -p "Do you want to delete Redis data? (y/n): " delete_redis
  
  if [[ "$delete_redis" =~ ^[Yy]$ ]]; then
    echo "Flushing Redis data..."
    if redis-cli flushall; then
      echo '✅ Redis data deleted successfully'
      echo 'ℹ️ Device re-onboarding will be required'
    else
      echo '❌ Failed to delete Redis data'
    fi
  else
    echo 'ℹ️ Redis data preserved'
  fi
}



# Collect certificate information
collect_certs_info() {
    echo "Collecting certificate information..."
    CN="${EXPOSED_SYMPHONY_IP:-localhost}"
    C="IN"
    ST="GGN"
    L="Some ABC Location"
    O="Margo"
    EMAIL="admin@example.com"
    DAYS="365"
    SAN_DOMAINS="${EXPOSED_SYMPHONY_IP:-localhost}"
    SAN_IPS="${EXPOSED_SYMPHONY_IP:-localhost}"
       
    echo "Using certificate defaults with CN: $CN"
}


# Generate OpenSSL config
generate_config_for_certs() {
    local config_file="$1"
    local cert_type="$2"
    
    # Create base config
    cat > "$config_file" << EOF
[req]
default_bits = 2048
prompt = no
distinguished_name = dn
$([ "$cert_type" = "server" ] && echo "req_extensions = v3_req")

[dn]
C=$C
ST=$ST
L=$L
O=$O
CN=$CN
emailAddress=$EMAIL

[v3_req]
basicConstraints = CA:FALSE
keyUsage = keyEncipherment, dataEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = $CN
EOF

    # Initialize counters
    local dns_count=2
    local ip_count=1

    # Add SAN domains if provided
    if [ -n "$SAN_DOMAINS" ]; then
        echo "Adding SAN domains..."
        IFS=',' read -ra DOMAINS <<< "$SAN_DOMAINS"
        for domain in "${DOMAINS[@]}"; do
            echo "DNS.$dns_count = ${domain// /}" >> "$config_file"
            ((dns_count++))
        done
    fi

    # Add SAN IPs if provided
  if [ -n "$SAN_IPS" ]; then
    echo "Adding SAN IPs..."
    IFS=',' read -ra IPS <<< "$SAN_IPS"
    for ip in "${IPS[@]}"; do
        echo "IP.$ip_count = ${ip// /}" >> "$config_file"
        ((ip_count++))
    done
  fi

    # Debug output
    echo "Generated OpenSSL config at $config_file:"
    cat "$config_file"
}


# Generate CA certificate
generate_ca() {
    info "Generating CA certificate..."
    local ca_key="$CERT_DIR/ca-key.pem"
    local ca_cert="$CERT_DIR/ca-cert.pem"
    local ca_config="$CERT_DIR/ca.conf"
    
    generate_config_for_certs "$ca_config" "ca"
    
    openssl genrsa -out "$ca_key" 2048
    openssl req -new -x509 -key "$ca_key" -out "$ca_cert" -days "$DAYS" -config "$ca_config"
    chmod 600 "$ca_key"
    
    success "CA generated: $ca_cert"
}

# Generate server certificate
generate_server_certs() {
    echo "Generating server certificate..."
    if ! mkdir -p "$CERT_DIR"; then
        echo "Error: Failed to create directory $CERT_DIR"
        return 1
    fi
    
    if [[ ! -w "$CERT_DIR" ]]; then
        echo "Error: Cannot write to $CERT_DIR"
        return 1
    fi
    local server_key="$CERT_DIR/server-key.pem"
    local server_csr="$CERT_DIR/server.csr"
    local server_cert="$CERT_DIR/server-cert.pem"
    local server_config="$CERT_DIR/server.conf"
    generate_ca
    generate_config_for_certs "$server_config" "server"
    
    openssl genrsa -out "$server_key" 2048
    openssl req -new -key "$server_key" -out "$server_csr" -config "$server_config"
    
    if [[ -f "$CERT_DIR/ca-cert.pem" ]]; then
        openssl x509 -req -in "$server_csr" -CA "$CERT_DIR/ca-cert.pem" -CAkey "$CERT_DIR/ca-key.pem" \
            -CAcreateserial -out "$server_cert" -days "$DAYS" -extensions v3_req -extfile "$server_config"
        success "Server certificate signed by CA: $server_cert"
    else
        openssl x509 -req -in "$server_csr" -signkey "$server_key" -out "$server_cert" -days "$DAYS" \
            -extensions v3_req -extfile "$server_config"
        success "Self-signed server certificate: $server_cert"
    fi
    
    rm -f "$server_csr"
    chmod 600 "$server_key"
}



# Update the show_menu function to include uninstall option														   
show_menu() {
  echo "Choose an option:"
  echo "1) PreRequisites: Setup"
  echo "2) PreRequisites: Cleanup"
  echo "3) Symphony: Start"
  echo "4) Symphony: Stop"
  echo "5) ObeservabiliyStack: Start"
  echo "6) ObeservabiliyStack: Stop"
  read -p "Enter choice [1-6]: " choice
  case $choice in
    1) install_prerequisites ;;
    2) uninstall_prerequisites ;;
    3) start_symphony ;;
    4) stop_symphony ;;
    5) observability_stack_install ;;
    6) observability_stack_uninstall ;;
    *) echo "⚠️ Invalid choice"; exit 1 ;;
  esac
}

# ----------------------------
# Main Script Execution
# ----------------------------
# Update the main script execution section
if [[ -z "$1" ]]; then
  show_menu
fi