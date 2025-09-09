#!/bin/bash
set -e

if ! command -v sshpass >/dev/null 2>&1; then
  echo "sshpass not found. Installing..."
  sudo apt-get update
  sudo apt-get install -y sshpass
fi

# Load environment variables
if [ -f .env ]; then
  source .env
else
  echo ".env file not found!"
  exit 1
fi

# ----------------------------
# Helper function
# ----------------------------
vm_ssh() {
  local USER=$1
  local HOST=$2
  local PASS=$3
  local CMD=$4
  sshpass -p "$PASS" ssh -o StrictHostKeyChecking=no "$USER@$HOST" "$CMD"
}

# ----------------------------
# Symphony Start/Stop
# ----------------------------
run_start() {
  echo "Running all VM1 setup tasks and starting Symphony API..."

  #Install go
  #vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" \
  #"sudo apt update
  # wget https://go.dev/dl/go1.21.1.linux-amd64.tar.gz -O /tmp/go1.21.1.linux-amd64.tar.gz
  # sudo rm -rf /usr/local/go
  # sudo tar -C /usr/local -xzf /tmp/go1.21.1.linux-amd64.tar.gz
  # export PATH=$PATH:/usr/local/go/bin
  # go version
  # Install Go
# vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" \
#  "which go
#  dpkg -l | grep golang
#  sudo apt remove --purge -y golang-go golang
#  sudo apt autoremove -y
#  ls -ld /usr/local/go
#  sudo rm -rf /usr/local/go
#  sudo rm -rf /usr/bin/go
#  sudo rm -rf /usr/local/go/bin/go
#  rm -rf ~/go
#  source ~/.profile
#  sudo apt update
#  sudo apt install -y golang-go
# "
vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" \
  "if which go >/dev/null 2>&1; then
      echo 'Go already installed, skipping installation';
      go version;
   else
      echo 'Go not found, installing...';
      dpkg -l | grep golang;
      sudo apt remove --purge -y golang-go golang;
      sudo apt autoremove -y;
      sudo rm -rf /usr/local/go /usr/bin/go /usr/local/go/bin/go
      sudo rm -rf /home/*/go
      wget https://go.dev/dl/go1.23.2.linux-amd64.tar.gz -O /tmp/go.tar.gz;
      sudo tar -C /usr/local -xzf /tmp/go.tar.gz;
      export PATH=\$PATH:/usr/local/go/bin;
      which go;
      go version;
   fi"


# Docker & Compose install
  vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" \
  "if ! command -v docker >/dev/null 2>&1; then \
    echo 'Docker not found. Installing Docker...'; \
    sudo apt-get remove -y docker docker-engine docker.io containerd runc || true; \
    curl -fsSL https://get.docker.com -o get-docker.sh; sudo sh get-docker.sh; \
    sudo usermod -aG docker \$USER; \
  else \
    echo 'Docker already installed.'; \
  fi; \
  \
  if ! command -v docker-compose >/dev/null 2>&1; then \
    echo 'Docker Compose not found. Installing Docker Compose...'; \
    sudo curl -L \"https://github.com/docker/compose/releases/download/v2.24.6/docker-compose-\$(uname -s)-\$(uname -m)\" -o /usr/local/bin/docker-compose; \
    sudo chmod +x /usr/local/bin/docker-compose; \
  else \
    echo 'Docker Compose already installed.'; \
  fi
  # Start and enable Docker daemon
  sudo systemctl start docker
  sudo systemctl enable docker

  # Wait for Docker daemon to be active (max 30s)
  for i in \$(seq 1 30); do
    if sudo systemctl is-active --quiet docker; then
      echo 'Docker daemon is running.'
      break
    else
      echo 'Waiting for Docker daemon to start... (\$i/30)'
      sleep 1
    fi
  done  
  "


  # Install Rust
  vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" \
  "echo 'Installing Rust...'; \
   curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y; \
   source \$HOME/.cargo/env"

  # Clone Symphony repo
 # vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" \
 # echo 'Cloning symphony...'
 # git clone https://${GITHUB_USER}:${GITHUB_TOKEN}@github.com/margo/symphony.git \$HOME/symphony
 # cd \$HOME/symphony
 # git checkout ${SYMPHONY_BRANCH}
 # echo 'symphony checkout to branch ${SYMPHONY_BRANCH} done'
 # "
   vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" "
    echo 'Cloning symphony...'
    sudo rm -rf \$HOME/symphony
    git clone https://${GITHUB_USER}:${GITHUB_TOKEN}@github.com/margo/symphony.git \$HOME/symphony
    cd \$HOME/symphony
    git checkout ${SYMPHONY_BRANCH} || echo 'Branch ${SYMPHONY_BRANCH} not found'
    echo 'symphony checkout to branch ${SYMPHONY_BRANCH} done'
   "

  # Clone dev-repo
  vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" \
  "#sudo rm -rf dev-repo; \
   #git clone https://github.com/margo/dev-repo.git; \
   #cd dev-repo; git checkout dev-sprint-6
   sudo rm -rf dev-repo; \
   git clone https://${GITHUB_USER}:${GITHUB_TOKEN}@github.com/margo/dev-repo.git; \
   cd dev-repo; \
   git checkout ${DEV_REPO_BRANCH}
   "

  # Update keycloak URL in symphony-api-margo.json
  vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" '
  echo "Updating keycloak URL in symphony-api-margo.json..."
  sed -i "s|\"keycloakURL\": *\"http://[^\"]*\"|\"keycloakURL\": \"http://'"$VM1_HOST"':8083\"|" ~/symphony/api/symphony-api-margo.json
'

  # Keycloak setup
  vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" "
  if docker ps --format '{{.Names}}' | grep -q keycloak; then
    echo 'Keycloak is already running, skipping startup.'
   else
    echo 'Starting Keycloak...'
    mkdir -p ~/dev-repo/poc/keycloak
    cp ~/dev-repo/pipeline/keycloak/compose.yml ~/dev-repo/poc/keycloak/
    cd ~/dev-repo/poc/keycloak
    docker compose -f compose.yml up -d
    sleep 20
    docker ps | grep keycloak || echo 'Keycloak did not start properly'
  fi
 "

# Gogs setup on VM1
vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" "
  BASE_DIR=~/dev-repo/poc/gogs
  DATA_DIR=\$BASE_DIR/gogs-data

  sudo rm -rf \$DATA_DIR/*
  mkdir -p \$BASE_DIR
  mkdir -p \$DATA_DIR/gogs/log \$DATA_DIR/gogs/data
  sudo chown -R 1000:1000 \$DATA_DIR
  sudo chmod -R 755 \$DATA_DIR

  # Copy project files
  for file in docker-compose.yml Dockerfile install_gogs.sh app.ini entrypoint.sh; do
    cp ~/dev-repo/pipeline/gogs/\$file \$BASE_DIR/
  done

  # Fix line endings + permissions for entrypoint
  sed -i 's/\r//' \$BASE_DIR/entrypoint.sh
  chmod +x \$BASE_DIR/entrypoint.sh

  #APP_INI=\$BASE_DIR/app.ini
  #RUNTIME_APP_INI=\$DATA_DIR/gogs/conf/app.ini
  #rm -f $RUNTIME_APP_INI

  # Update template with remote VM1_HOST
  #sed -i 's/\r$//' $APP_INI
  #sed -i \"s/^DOMAIN.*/DOMAIN           = $VM1_HOST/\" \$APP_INI
  #sed -i \"s/^HTTP_PORT.*/HTTP_PORT        = 8084/\" \$APP_INI
  #sed -i \"s|^EXTERNAL_URL.*|EXTERNAL_URL     = http://$VM1_HOST:8084/|\" \$APP_INI
  #Copy into runtime config
  #mkdir -p \$(dirname \$RUNTIME_APP_INI)
  #cp \$APP_INI \$RUNTIME_APP_INI

  #dos2unix \$RUNTIME_APP_INI
  #sudo chown 1000:1000 \$RUNTIME_APP_INI

  #echo 'Final runtime app.ini:'
  #grep -E 'DOMAIN|HTTP_PORT|EXTERNAL_URL' \$RUNTIME_APP_INI

"

#Start Gogs
vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" "
  echo 'Starting Gogs container...'
  cd ~/dev-repo/poc/gogs
  docker-compose down
  docker-compose build --no-cache gogs
  docker-compose -f docker-compose.yml up -d
 "


# Wait for Gogs to be ready on VM1
vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" \
"for i in {1..32}; do \
  if curl -s http://$VM1_HOST:8084 | grep -q \"Gogs\"; then \
    echo \"Gogs is up!\"; \
    break; \
  fi; \
  sleep 2; \
done"

# Create Gogs Admin User
vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" \
'
GOGS_CONTAINER=$(docker ps --filter "name=custom-gogs" --format "{{.Names}}" | head -n 1)
if [ -z "$GOGS_CONTAINER" ]; then
  echo "Gogs container not found! Exiting."
  exit 1
fi

docker exec -u git "$GOGS_CONTAINER" /app/gogs/gogs admin create-user \
  --name gogsadmin \
  --password admin123 \
  --email you@example.com \
  --admin || echo "User might already exist, skipping..."
'
# Create Gogs token on VM1 and export locally
TOKEN_NAME="autogen-$(date +%s)"
TOKEN=$(vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" \
"curl -s -X POST -u 'gogsadmin:admin123' -H 'Content-Type: application/json' -d '{\"name\": \"$TOKEN_NAME\"}' http://$VM1_HOST:8084/api/v1/users/gogsadmin/tokens | jq -r '.sha1'")
export GOGS_TOKEN=$TOKEN

# Create nextcloud repo on VM1 using token
#vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" \
#"curl -s -o /tmp/resp.json -w '\nHTTP %{http_code}\n' -H 'Authorization: token $GOGS_TOKEN' -H 'Content-Type: application/json' -d '{\"name\":\"nextcloud-repo\",\"private\":false}' http://$VM1_HOST:8084/api/v1/user/repos; cat /tmp/resp.json"
vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" \
"JSON=\$(printf '{\"name\":\"nextcloud\",\"private\":false}') && \
 curl -s -o /tmp/resp.json -w '\nHTTP %{http_code}\n' \
 -H 'Authorization: token $GOGS_TOKEN' \
 -H 'Content-Type: application/json' \
 -d \"\$JSON\" \
 http://$VM1_HOST:8084/api/v1/user/repos; cat /tmp/resp.json"


# Create ngix repo on VM1 using token
#vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" \
#"curl -s -o /tmp/resp.json -w '\nHTTP %{http_code}\n' -H 'Authorization: token $GOGS_TOKEN' -H 'Content-Type: application/json' -d '{\"name\":\"nginx-repo\",\"private\":false}' http://$VM1_HOST:8084/api/v1/user/repos; cat /tmp/resp.json"
vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" \
"JSON=\$(printf '{\"name\":\"nginx\",\"private\":false}') && \
 curl -s -o /tmp/resp.json -w '\nHTTP %{http_code}\n' \
 -H 'Authorization: token $GOGS_TOKEN' \
 -H 'Content-Type: application/json' \
 -d \"\$JSON\" \
 http://$VM1_HOST:8084/api/v1/user/repos; cat /tmp/resp.json"

# Create OTEL repo on VM1 using token
#vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" \
#"curl -s -o /tmp/resp.json -w '\nHTTP %{http_code}\n' -H 'Authorization: token $GOGS_TOKEN' -H 'Content-Type: application/json' -d '{\"name\":\"OTEL-repo\",\"private\":false}' http://$VM1_HOST:8084/api/v1/user/repos; cat /tmp/resp.json"
#vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" \
#"JSON=\$(printf '{\"name\":\"OTEL\",\"private\":false}') && \
# curl -s -o /tmp/resp.json -w '\nHTTP %{http_code}\n' \
# -H 'Authorization: token $GOGS_TOKEN' \
# -H 'Content-Type: application/json' \
# -d \"\$JSON\" \
# http://$VM1_HOST:8084/api/v1/user/repos; cat /tmp/resp.json"

# Push files into Nextcloud repo
vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" "
cd ~/dev-repo/poc/tests/artefacts/nextcloud-compose || { echo '❌ Nextcloud dir missing'; exit 1; }

[ ! -d .git ] && git init && \
  git config user.name 'gogsadmin' && \
  git config user.email 'nitin.parihar@capgemini.com'

git remote remove origin 2>/dev/null || true
git remote add origin http://gogsadmin:${GOGS_TOKEN}@${VM1_HOST}:8084/gogsadmin/nextcloud.git

git add margo.yaml resources/ 2>/dev/null || true

if ! git diff --cached --quiet; then
  git commit -m 'Initial commit with Nextcloud files'
fi

git branch -M main
git push -u origin main --force
"


# Push files into Nginx repo
vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" "
cd ~/dev-repo/poc/tests/artefacts/nginx-helm || { echo '❌ Nginx dir missing'; exit 1; }

[ ! -d .git ] && git init && \
  git config user.name 'gogsadmin' && \
  git config user.email 'nitin.parihar@capgemini.com'

git remote remove origin 2>/dev/null || true
git remote add origin http://gogsadmin:${GOGS_TOKEN}@${VM1_HOST}:8084/gogsadmin/nginx.git

git add margo.yaml resources/ 2>/dev/null || true

if ! git diff --cached --quiet; then
  git commit -m 'Initial commit with Nginx files'
fi

git branch -M main
git push -u origin main --force
"


# Push files into OTEL repo
vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" "
cd ~/dev-repo/poc/tests/artefacts/open-telemetry-demo-helm || { echo '❌ OTEL dir missing'; exit 1; }

[ ! -d .git ] && git init && \
  git config user.name 'gogsadmin' && \
  git config user.email 'nitin.parihar@capgemini.com'

git remote remove origin 2>/dev/null || true
git remote add origin http://gogsadmin:${GOGS_TOKEN}@${VM1_HOST}/gogsadmin/otel.git

git add margo.yaml resources/ 2>/dev/null || true

if ! git diff --cached --quiet; then
  git commit -m 'Initial commit with OTEL files'
fi

git branch -M main
git push -u origin main --force
"



  # Inspect repo
  vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" "ls -laR \$HOME/symphony"

  # Build Rust
  vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" \
  "source \$HOME/.cargo/env; \
   RUST_DIR=\$HOME/symphony/api/pkg/apis/v1alpha1/providers/target/rust; \
   if [ -d \"\$RUST_DIR\" ]; then cd \"\$RUST_DIR\"; cargo build --release; fi"

  # Build Go API
  vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" \
  "git config --global url.\"https://${GITHUB_TOKEN}@github.com/\".insteadOf \"https://github.com/\"; \
   go env -w GOPRIVATE=github.com/margo/*; \
   GO_DIR=\$HOME/symphony/api; \
   if [ -d \"\$GO_DIR\" ]; then \
   export LD_LIBRARY_PATH=\$HOME/symphony/api/pkg/apis/v1alpha1/providers/target/rust/target/release; \
   cd \"\$GO_DIR\"; go build -o symphony-api; \
   fi"

  # Build Maestro CLI
  vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" \
  "CLI_DIR=\$HOME/symphony/cli; \
   if [ -d \"\$CLI_DIR\" ]; then cd \"\$CLI_DIR\"; go mod tidy; go build -o maestro; fi"

  # Verify symphony-api
  vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" \
  "file \$HOME/symphony/api/symphony-api; ls -l \$HOME/symphony/api/symphony-api"

  # Start Symphony API
  vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" "
  cd \$HOME/symphony/api || exit 1
  echo 'Starting Symphony API...'
  nohup ./symphony-api -c ./symphony-api-margo.json -l Debug > \$HOME/symphony-api.log 2>&1 &
  sleep 5
  echo '--- Symphony API logs ---'
  tail -n 50 \$HOME/symphony-api.log
"

 echo "VM1 setup completed and Symphony API started"
}

run_stop() {
  echo "Stopping Symphony API on $VM1_HOST..."
  vm_ssh "$VM1_USER" "$VM1_HOST" "$VM1_PASS" \
  "PID=\$(ps -ef | grep '[s]ymphony-api-margo.json' | awk '{print \$2}'); \
   if [ -z \"\$PID\" ]; then \
     echo '❌ Symphony API is not running'; \
   else \
     kill -9 \$PID && echo '✅ Symphony API stopped'; \
   fi"
}

show_menu() {
  echo "Choose an option:"
  echo "1) Symphony-Install"
  echo "2) Symphony-Stop"
  read -p "Enter choice [1-2]: " choice
  case $choice in
    1) run_start ;;
    2) run_stop ;;
    *) echo "⚠️ Invalid choice"; exit 1 ;;
  esac
}

if [[ -z "$1" ]]; then
  show_menu
else
  case "$1" in
    Symphony-Install) run_start ;;
    Symphony-Stop)  run_stop ;;
    *) echo "Usage: $0 {symphony-start|symphony-stop}"; exit 1 ;;
  esac
fi

