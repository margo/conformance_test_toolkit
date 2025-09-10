#!/bin/bash
set -e

# ----------------------------
# Environment & Validation
# ----------------------------

#--- Github Settings to pull the code (can be overridden via env)
GITHUB_USER="${GITHUB_USER:-}"  # Set via env or leave empty
GITHUB_TOKEN="${GITHUB_TOKEN:-}"  # Set via env or leave empty

#--- branch details (can be overridden via env)
SYMPHONY_BRANCH="${SYMPHONY_BRANCH:-margo-dev-sprint-6}"
DEV_REPO_BRANCH="${DEV_REPO_BRANCH:-dev-sprint-6}"

#--- harbor settings (can be overridden via env)
EXPOSED_HARBOR_IP="${EXPOSED_HARBOR_IP:-127.0.0.1}"
EXPOSED_HARBOR_PORT="${EXPOSED_HARBOR_PORT:-8081}"

#--- symphony settings (can be overridden via env)
EXPOSED_SYMPHONY_IP="${EXPOSED_SYMPHONY_IP:-127.0.0.1}"
EXPOSED_SYMPHONY_PORT="${EXPOSED_SYMPHONY_PORT:-8082}"

#--- keycloak settings (can be overridden via env)
EXPOSED_KEYCLOAK_IP="${EXPOSED_KEYCLOAK_IP:-127.0.0.1}"
EXPOSED_KEYCLOAK_PORT="${EXPOSED_KEYCLOAK_PORT:-8083}"

#--- gogs settings (can be overridden via env)
EXPOSED_GOGS_IP="${EXPOSED_GOGS_IP:-127.0.0.1}"
EXPOSED_GOGS_PORT="${EXPOSED_GOGS_PORT:-8084}"

# ----------------------------
# Utility Functions
# ----------------------------
validate_passwordless_sudo() {
  local username="${1:-$(whoami)}"
  local exit_code=0
  
  echo "Validating passwordless for user: $username"
  echo "==============================================="
  
  # Test 1: Basic test
  echo -n "Test 1 - Basic access: "
  if -n true 2>/dev/null; then
      echo "✓ PASS"
  else
      echo "✗ FAIL"
      exit_code=1
  fi
  
  # Test 2: Specific command test
  echo -n "Test 2 - Command execution: "
  if -n whoami >/dev/null 2>&1; then
      echo "✓ PASS"
  else
      echo "✗ FAIL"
      exit_code=1
  fi
  
  # Test 3: File access test
  echo -n "Test 3 - File access: "
  if -n test -r /etc/shadow 2>/dev/null; then
      echo "✓ PASS"
  else
      echo "✗ FAIL"
      exit_code=1
  fi
  
  # Test 4: Configuration verification
  echo -n "Test 4 - Config verification: "
  if grep -q "$username.*NOPASSWD\|%.*NOPASSWD" /etc/sudoers /etc/sudoers.d/* 2>/dev/null; then
      echo "✓ PASS"
  else
      echo "✗ FAIL"
      exit_code=1
  fi
  
  echo "==============================================="
  if [ $exit_code -eq 0 ]; then
      echo "✓ All tests passed - Passwordless is properly configured"
  else
      echo "✗ Some tests failed - Passwordless may not be fully configured"
  fi
  
  return $exit_code
}

# ----------------------------
# Installation Functions
# ----------------------------
install_basic_utilities() {
  apt update && apt install curl -y
}

install_go() {
  if which go >/dev/null 2>&1; then
    echo 'Go already installed, skipping installation';
    go version;
  else
    echo 'Go not found, installing...';
    rm -rf /usr/local/go /usr/bin/go
    wget "https://go.dev/dl/go1.23.2.linux-amd64.tar.gz" -O go.tar.gz;
    tar -C /usr/local -xzf go.tar.gz;
    rm go.tar.gz
    export PATH="$PATH:/usr/local/go/bin";
    which go;
    go version;
  fi
}

install_docker_compose() {
  if ! command -v docker >/dev/null 2>&1; then
    echo 'Docker not found. Installing Docker...';
    apt-get remove -y docker docker-engine docker.io containerd runc || true;
    curl -fsSL "https://get.docker.com" -o get-docker.sh; sh get-docker.sh;
    usermod -aG docker $USER;
  else
    echo 'Docker already installed.';
  fi;

  if ! command -v docker-compose >/dev/null 2>&1; then
    echo 'Docker Compose not found. Installing Docker Compose...';
    curl -L "https://github.com/docker/compose/releases/download/v2.24.6/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose;
    chmod +x /usr/local/bin/docker-compose;
  else
    echo 'Docker Compose already installed.';
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

install_rust() {
  echo 'Installing Rust...';
  curl --proto "=https" --tlsv1.2 -sSf "https://sh.rustup.rs" | sh -s -- -y;
  source $HOME/.cargo/env
}

# ----------------------------
# Repository Functions
# ----------------------------
clone_symphony_repo() {
  echo 'Cloning symphony...'
  rm -rf "$HOME/symphony"
  git clone "https://${GITHUB_USER}:${GITHUB_TOKEN}@github.com/margo/symphony.git" "$HOME/symphony"
  cd $HOME/symphony
  git checkout ${SYMPHONY_BRANCH} || echo 'Branch ${SYMPHONY_BRANCH} not found'
  echo 'symphony checkout to branch ${SYMPHONY_BRANCH} done'
}

clone_dev_repo() {
  rm -rf "dev-repo";
  git clone "https://${GITHUB_USER}:${GITHUB_TOKEN}@github.com/margo/dev-repo.git";
  cd dev-repo;
  git checkout ${DEV_REPO_BRANCH}
}

# ----------------------------
# Service Setup Functions
# ----------------------------
setup_keycloak() {
  if docker ps --format '{{.Names}}' | grep -q keycloak; then
    echo 'Keycloak is already running, skipping startup.'
   else
    echo 'Starting Keycloak...'
    cd $HOME/dev-repo/pipeline/keycloak
    chmod +x init.sh
    docker compose up -d
    sleep 90
    docker ps | grep keycloak || echo 'Keycloak did not start properly'
  fi
}

update_keycloak_config() {
  echo "Updating keycloak URL in symphony-api-margo.json..."
  sed -i "s|\"keycloakURL\": *\"http://[^\"]*\"|\"keycloakURL\": \"http://"$EXPOSED_KEYCLOAK_IP":$EXPOSED_KEYCLOAK_PORT\"|" ~/symphony/api/symphony-api-margo.json
}

setup_gogs_directories() {
  GOGS_BASE_DIR="$HOME/dev-repo/pipeline/gogs"
  DATA_DIR="$GOGS_BASE_DIR/data"
  LOGS_DIR="$GOGS_BASE_DIR/logs"
  APP_INI_PATH="$GOGS_BASE_DIR/app.ini"
  GOGS_IP="$EXPOSED_GOGS_IP"
  GOGS_PORT=$EXPOSED_GOGS_PORT
  
  rm -rf "$DATA_DIR" "$LOGS_DIR"
  mkdir -p "$GOGS_BASE_DIR" "$DATA_DIR" "$LOGS_DIR"
  chown -R 1000:1000 "$DATA_DIR" "$LOGS_DIR"
  chmod -R 755 "$DATA_DIR" "$LOGS_DIR"
  # Fix line endings + permissions for entrypoint
  # chmod +x "$GOGS_BASE_DIR/entrypoint.sh"
  # Update template with remote GOGS_IP details
  # Use printf to build the replacement strings safely
  DOMAIN_LINE=$(printf "DOMAIN              = %s" "$GOGS_IP")
  HTTP_PORT_LINE=$(printf "HTTP_PORT        = %s" "$GOGS_PORT")
  EXTERNAL_URL_LINE=$(printf "EXTERNAL_URL  = http://%s:%s/" "$GOGS_IP" "$GOGS_PORT")

  sed -i 's/\r$//' "$APP_INI_PATH"
  sed -i "s/^DOMAIN.*/$DOMAIN_LINE/" "$APP_INI_PATH"
  sed -i "s/^HTTP_PORT.*/$HTTP_PORT_LINE/" "$APP_INI_PATH"
  sed -i "s|^EXTERNAL_URL.*|$EXTERNAL_URL_LINE|" "$APP_INI_PATH"
  sudo chown 1000:1000 "$APP_INI_PATH"

  echo 'Final runtime app.ini:'
  grep -E 'DOMAIN|HTTP_PORT|EXTERNAL_URL' "$APP_INI_PATH"
}

start_gogs() {
  echo 'Starting Gogs container...'
  GOGS_BASE_DIR="$HOME/dev-repo/pipeline/gogs/"
  cd "$GOGS_BASE_DIR"
  docker-compose down
  docker-compose build --no-cache gogs
  docker-compose -f docker-compose.yml up -d
}

wait_for_gogs() {
  GOGS_IP=$EXPOSED_GOGS_IP
  GOGS_PORT=$EXPOSED_GOGS_PORT
  for i in {1..32}; do
    if curl -s http://$GOGS_IP:$GOGS_PORT | grep -q "Gogs"; then
      echo "Gogs is up!";
      break;
    fi;
    sleep 2;
  done
}

create_gogs_admin() {
  GOGS_IP=$EXPOSED_GOGS_IP
  GOGS_PORT=$EXPOSED_GOGS_PORT
  GOGS_CONTAINER=$(docker ps --filter "name=gogs" --format "{{.Names}}" | head -n 1)
  if [ -z "$GOGS_CONTAINER" ]; then
    echo "Gogs container not found! Exiting."
    exit 1
  fi

  docker exec -u git "$GOGS_CONTAINER" /app/gogs/gogs admin create-user \
    --name gogsadmin \
    --password admin123 \
    --email you@example.com \
    --admin || echo "User might already exist, skipping..."
}

create_gogs_token() {
  GOGS_IP=$EXPOSED_GOGS_IP
  GOGS_PORT=$EXPOSED_GOGS_PORT
  TOKEN_NAME="autogen-$(date +%s)"
  TOKEN_RESP=$(curl -s -X POST -u 'gogsadmin:admin123' -H 'Content-Type: application/json' -d "{\"name\": \"$TOKEN_NAME\"}" "http://$GOGS_IP:$GOGS_PORT/api/v1/users/gogsadmin/tokens")
  echo "TOKEN RESP $TOKEN_RESP"
  TOKEN=$(echo ${TOKEN_RESP} | jq -r '.sha1')
  export GOGS_TOKEN=$TOKEN
}

create_gogs_repositories() {
  # Create nextcloud repo
  GOGS_IP=$EXPOSED_GOGS_IP
  GOGS_PORT=$EXPOSED_GOGS_PORT
  echo "GOGS TOKEN: $GOGS_TOKEN"
  curl -s -o /tmp/resp.json -w '\nHTTP %{http_code}\n' \
    -H "Authorization: token $GOGS_TOKEN" \
    -H 'Content-Type: application/json' \
    -d '{"name":"nextcloud","private":false}' \
    "http://$GOGS_IP:$GOGS_PORT/api/v1/user/repos"
  cat /tmp/resp.json

  GOGS_IP=$GOG
  GOGS_PORT=8084
  curl -s -o /tmp/resp.json -w '\nHTTP %{http_code}\n' \
    -H "Authorization: token $GOGS_TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"name":"nginx","private":false}' \
    "http://$GOGS_IP:$GOGS_PORT/api/v1/user/repos"
  cat /tmp/resp.json

  # Create nextcloud repo
  GOGS_IP=$EXPOSED_GOGS_IP
  GOGS_PORT=$EXPOSED_GOGS_PORT
  curl -s -o /tmp/resp.json -w '\nHTTP %{http_code}\n' \
    -H "Authorization: token $GOGS_TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"name":"otel","private":false}' \
    http://$GOGS_IP:$GOGS_PORT/api/v1/user/repos
  cat /tmp/resp.json

  # Create nextcloud repo
  GOGS_IP=$EXPOSED_GOGS_IP
  GOGS_PORT=$EXPOSED_GOGS_PORT
  curl -s -o /tmp/resp.json -w '\nHTTP %{http_code}\n' \
    -H "Authorization: token $GOGS_TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"name":"custom-otel","private":false}' \
    http://$GOGS_IP:$GOGS_PORT/api/v1/user/repos
  cat /tmp/resp.json
}

push_nextcloud_files() {
  cd "$HOME/dev-repo/poc/tests/artefacts/nextcloud-compose" || { echo '❌ Nextcloud dir missing'; exit 1; }
  [ ! -d .git ] && git init && \
    git config user.name 'gogsadmin' && \
    git config user.email 'nitin.parihar@capgemini.com'
  git remote remove origin 2>/dev/null || true
  git remote add origin "http://gogsadmin:${GOGS_TOKEN}@${EXPOSED_GOGS_IP}:EXPOSED_GOGS_PORT/gogsadmin/nextcloud.git"
  git add margo.yaml resources/ 2>/dev/null || true
  if ! git diff --cached --quiet; then
    git commit -m 'Initial commit with Nextcloud files'
  fi
  git branch -M main
  git push -u origin main --force
}

push_nginx_files() {
  cd "$HOME/dev-repo/poc/tests/artefacts/nginx-helm" || { echo '❌ nginx-helm dir missing'; exit 1; }
  [ ! -d .git ] && git init && \
    git config user.name 'gogsadmin' && \
    git config user.email 'nitin.parihar@capgemini.com'
  git remote remove origin 2>/dev/null || true
  git remote add origin "http://gogsadmin:${GOGS_TOKEN}@${EXPOSED_GOGS_IP}:EXPOSED_GOGS_PORT/gogsadmin/nginx.git"
  git add margo.yaml resources/ 2>/dev/null || true
  if ! git diff --cached --quiet; then
    git commit -m 'Initial commit with nginx-helm files'
  fi
  git branch -M main
  git push -u origin main --force
}

push_otel_files() {
  cd "$HOME/dev-repo/poc/tests/artefacts/open-telemetry-demo-helm" || { echo '❌ OTEL dir missing'; exit 1; }
  [ ! -d .git ] && git init && \
    git config user.name 'gogsadmin' && \
    git config user.email 'nitin.parihar@capgemini.com'
  git remote remove origin 2>/dev/null || true
  git remote add origin "http://gogsadmin:${GOGS_TOKEN}@${EXPOSED_GOGS_IP}:EXPOSED_GOGS_PORT/gogsadmin/otel.git"
  git add margo.yaml resources/ 2>/dev/null || true
  if ! git diff --cached --quiet; then
    git commit -m 'Initial commit with OTEL files'
  fi
  git branch -M main
  git push -u origin main --force
}

push_custom_otel_files() {
  cd "$HOME/dev-repo/poc/tests/artefacts/custom-otel-helm-app" || { echo '❌ Custom OTEL dir missing'; exit 1; }
  [ ! -d .git ] && git init && \
    git config user.name 'gogsadmin' && \
    git config user.email 'nitin.parihar@capgemini.com'
  git remote remove origin 2>/dev/null || true
  git remote add origin "http://gogsadmin:${GOGS_TOKEN}@${EXPOSED_GOGS_IP}:EXPOSED_GOGS_PORT/gogsadmin/custom-otel.git"
  git add margo.yaml resources/ 2>/dev/null || true
  if ! git diff --cached --quiet; then
    git commit -m 'Initial commit with Custom OTEL files'
  fi
  git branch -M main
  git push -u origin main --force
}

# ----------------------------
# Symphony Build Functions
# ----------------------------
build_symphony_api() {
  echo 'Building Symphony API...'
  cd ~/symphony/api
  export PATH=$PATH:/usr/local/go/bin
  go mod tidy
  go build -o symphony-api .
  echo 'Symphony API build completed'
}

build_symphony_ui() {
  echo 'Building Symphony UI...'
  cd ~/symphony/ui
  npm install
  npm run build
  echo 'Symphony UI build completed'
}

# ----------------------------
# Build Functions
# ----------------------------
build_rust() {
  source "$HOME/.cargo/env"; 
  RUST_DIR="$HOME/symphony/api/pkg/apis/v1alpha1/providers/target/rust"; 
  if [ -d "$RUST_DIR" ]; then 
    cd "$RUST_DIR"; 
    cargo build --release;
  fi
}

build_go_api() {
  git config --global url."https://${GITHUB_TOKEN}@github.com/".insteadOf "https://github.com/";
  go env -w GOPRIVATE="github.com/margo/*";
  GO_DIR="$HOME/symphony/api";
  if [ -d "$GO_DIR" ]; then
    export LD_LIBRARY_PATH="$HOME/symphony/api/pkg/apis/v1alpha1/providers/target/rust/target/release";
    cd "$GO_DIR";
    go build -o symphony-api;
  fi
}

build_maestro_cli() {
  CLI_DIR="$HOME/symphony/cli";
  if [ -d "$CLI_DIR" ]; then 
    cd "$CLI_DIR"; 
    go mod tidy; 
    go build -o maestro; 
  fi
}

verify_symphony_api() {
  file "$HOME/symphony/api/symphony-api";
  ls -l "$HOME/symphony/api/symphony-api";
}

start_symphony_api() {
  cd "$HOME/symphony/api" || exit 1
  echo 'Starting Symphony API...'
  nohup ./symphony-api -c ./symphony-api-margo.json -l Debug > $HOME/symphony-api.log 2>&1 &
  sleep 5
  echo '--- Symphony API logs ---'
  tail -n 50 $HOME/symphony-api.log
}


# ----------------------------
# Main Orchestration Functions
# ----------------------------
run_install() {
  echo "Running all setup tasks and starting Symphony API..."
  install_basic_utilities
  install_go
  install_docker_compose
  install_rust
  clone_symphony_repo
  clone_dev_repo
  setup_keycloak
  update_keycloak_config
  setup_gogs_directories
  start_gogs
  wait_for_gogs
  create_gogs_admin
  create_gogs_token
  create_gogs_repositories
  push_nextcloud_files
  push_nginx_files
  push_otel_files
  push_custom_otel_files
  
  # Build phase
  build_rust
  build_go_api
  build_maestro_cli
  verify_symphony_api
  start_symphony_api
  
  echo "setup completed and symphony API started"
}

run_uninstall() {
  echo "Stopping Symphony API on..."
  PID=$(ps -ef | grep '[s]ymphony-api-margo.json' | awk '{print $2}'); 
  if [ -z "$PID" ]; then 
    echo '❌ Symphony API is not running'; 
  else 
    kill -9 $PID && echo '✅ Symphony API stopped'; 
  fi
}

show_menu() {
  echo "Choose an option:"
  echo "1) Symphony-Install"
  echo "2) Symphony-Uninstall"
  read -p "Enter choice [1-2]: " choice
  case $choice in
    1) run_install ;;
    2) run_uninstall ;;
    *) echo "⚠️ Invalid choice"; exit 1 ;;
  esac
}

# ----------------------------
# Main Script Execution
# ----------------------------
if [[ -z "$1" ]]; then
  show_menu
else
  case "$1" in
    Symphony-Install) run_install ;;
    Symphony-Uninstall) run_uninstall ;;
    *) echo "Usage: $0 {symphony-install|symphony-uninstall}"; exit 1 ;;
  esac
fi

