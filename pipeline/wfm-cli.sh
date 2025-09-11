#!/bin/bash
set -e

# ----------------------------
# Environment & Validation
# ----------------------------

#--- harbor settings (can be overridden via env)
EXPOSED_HARBOR_IP="${EXPOSED_HARBOR_IP:-127.0.0.1}"
EXPOSED_HARBOR_PORT="${EXPOSED_HARBOR_PORT:-8081}"

#--- symphony settings (can be overridden via env)
EXPOSED_SYMPHONY_IP="${EXPOSED_SYMPHONY_IP:-127.0.0.1}"
EXPOSED_SYMPHONY_PORT="${EXPOSED_SYMPHONY_PORT:-8082}"

#--  device node IP (can be overridden via env) for prometheus to scrape metrics 
DEVICE_NODE_IP="${DEVICE_NODE_IP:-127.0.0.1}"

#--- gogs settings (can be overridden via env)
EXPOSED_GOGS_IP="${EXPOSED_GOGS_IP:-127.0.0.1}"
EXPOSED_GOGS_PORT="${EXPOSED_GOGS_PORT:-8084}"

# ----------------------------
# Utility Functions
# ----------------------------
MAESTRO_CLI_PATH="$HOME/symphony/cli"
check_maestro_cli() {
  if [ ! -f "${MAESTRO_CLI_PATH}/maestro" ]; then
    echo "❌ maestro CLI not found in ${MAESTRO_CLI_PATH} directory"
    echo "Please ensure maestro CLI is built and available there"
    return 1
  fi
  return 0
}

validate_choice() {
  local choice="$1"
  local max_choice="$2"
  if [[ ! "$choice" =~ ^[0-9]+$ ]] || [ "$choice" -lt 1 ] || [ "$choice" -gt "$max_choice" ]; then
    echo "❌ Invalid choice. Please enter a number between 1 and $max_choice"
    return 1
  fi
  return 0
}

# ----------------------------
# List Functions
# ----------------------------
list_app_packages() {
  echo "📦 Listing all app packages from WFM..."
  if check_maestro_cli; then
    ${MAESTRO_CLI_PATH}/maestro wfm list app-pkg || echo "❌ Failed to list app-pkg"
  fi
  echo ""
  read -p "Press Enter to continue..."
}

list_devices() {
  echo "🖥️  Listing all devices from WFM..."
  if check_maestro_cli; then
    ${MAESTRO_CLI_PATH}/maestro wfm list devices || echo "❌ Failed to list devices"
  fi
  echo ""
  read -p "Press Enter to continue..."
}

list_deployments() {
  echo "🚀 Listing all deployments from WFM..."
  if check_maestro_cli; then
    ${MAESTRO_CLI_PATH}/maestro wfm list deployments || echo "❌ Failed to list deployments"
  fi
  echo ""
  read -p "Press Enter to continue..."
}

list_all() {
  echo "📋 Listing all resources from WFM..."
  echo "=================================="
  
  echo "📦 App Packages:"
  echo "----------------"
  if check_maestro_cli; then
    ${MAESTRO_CLI_PATH}/maestro wfm list app-pkg || echo "❌ Failed to list app-pkg"
  fi
  
  echo ""
  echo "🖥️  Devices:"
  echo "----------"
  if check_maestro_cli; then
    ${MAESTRO_CLI_PATH}/maestro wfm list devices || echo "❌ Failed to list devices"
  fi
  
  echo ""
  echo "🚀 Deployments:"
  echo "---------------"
  if check_maestro_cli; then
    ${MAESTRO_CLI_PATH}/maestro wfm list deployments || echo "❌ Failed to list deployments"
  fi
  
  echo ""
  read -p "Press Enter to continue..."
}

# ----------------------------
# Package Management Functions
# ----------------------------
get_package_upload_request_file_path() {
  local choice="$1"
  case $choice in
    1) 
      GIT_URL="http://${EXPOSED_GOGS_IP}:${EXPOSED_GOGS_PORT}/gogsadmin/custom-otel"
      sed -i "s|\"url\": *\"http://[^\"]*\"|\"url\": \"$GIT_URL\"|" "$HOME/symphony/cli/templates/margo/custom-otel-helm/package.yaml" && echo "$HOME/symphony/cli/templates/margo/custom-otel-helm/package.yaml" ;;
    2)
      GIT_URL="http://${EXPOSED_GOGS_IP}:${EXPOSED_GOGS_PORT}/gogsadmin/nginx"
      sed -i "s|\"url\": *\"http://[^\"]*\"|\"url\": \"$GIT_URL\"|" "$HOME/symphony/cli/templates/margo/nginx-helm/package.yaml" && echo "$HOME/symphony/cli/templates/margo/nginx-helm/package.yaml" ;;
    3)
      GIT_URL="http://${EXPOSED_GOGS_IP}:${EXPOSED_GOGS_PORT}/gogsadmin/nextcloud"
      sed -i "s|\"url\": *\"http://[^\"]*\"|\"url\": \"$GIT_URL\"|" "$HOME/symphony/cli/templates/margo/nextcloud-compose/package.yaml" && echo "$HOME/symphony/cli/templates/margo/nextcloud-compose/package.yaml" ;;
    *) 
      echo "" ;;
  esac
}

get_package_name() {
  local choice="$1"
  case $choice in
    1) echo "Custom OTEL Helm App" ;;
    2) echo "Nginx Helm App" ;;
    3) echo "Nextcloud Compose App" ;;
    *) echo "Unknown Package" ;;
  esac
}

upload_app_package() {
  echo "📦 Upload App Package"
  echo "===================="
  echo "Select one of the packages:"
  echo "1) Custom OTEL Helm App"
  echo "2) Nginx Helm App"
  echo "3) Nextcloud Compose App"
  echo "4) Exit"
  echo ""
  
  read -p "Enter choice [1-4]: " app_package_choice
  
  if [ "$app_package_choice" = "4" ]; then
    echo "Returning to main menu..."
    return 0
  fi
  
  if ! validate_choice "$app_package_choice" 3; then
    return 1
  fi
  
  local package_file=$(get_package_upload_request_file_path "$app_package_choice")
  local package_name=$(get_package_name "$app_package_choice")
  
  if [ ! -f "$package_file" ]; then
    echo "❌ Package file not found: $package_file"
    return 1
  fi
  
  echo "📤 Uploading $package_name to WFM..."
  if check_maestro_cli; then
    if ${MAESTRO_CLI_PATH}/maestro wfm apply -f "$package_file"; then
      echo "✅ $package_name uploaded successfully!"
      echo ""
    else
      echo "❌ Failed to upload $package_name"
    fi
  fi
  
  echo ""
  read -p "Press Enter to continue..."
}

delete_app_package() {
  echo "🗑️  Delete App Package"
  echo "===================="
  
  echo "📦 Current packages:"
  if check_maestro_cli; then
    ${MAESTRO_CLI_PATH}/maestro wfm list app-pkg
  fi
  
  echo ""
  read -p "Enter the package name/ID to delete: " package_id
  
  if [ -z "$package_id" ]; then
    echo "❌ Package name/ID is required"
    return 1
  fi
  
  read -p "Are you sure you want to delete app-pkg '$package_id'? (y/N): " confirm
  if [[ "$confirm" =~ ^[Yy]$ ]]; then
    echo "🗑️  Deleting package '$package_id'..."
    if check_maestro_cli; then
      if ${MAESTRO_CLI_PATH}/maestro wfm delete app-pkg "$package_id"; then
        echo "✅ Package '$package_id' deleted successfully!"
      else
        echo "❌ Failed to delete app-pkg '$package_id'"
      fi
    fi
  else
    echo "Deletion cancelled"
  fi
  
  echo ""
  read -p "Press Enter to continue..."
}

# ----------------------------
# Instance Management Functions
# ----------------------------
# Add this helper function to get deployment file paths
get_instance_file_path() {
  local package_name="$1"
  case $package_name in
    "custom-otel-helm-app"|"custom-otel") 
      echo "$HOME/symphony/cli/templates/margo/custom-otel-helm/deployment.yaml" ;;
    "nginx-helm-app"|"nginx") 
      echo "$HOME/symphony/cli/templates/margo/nginx-helm/deployment.yaml" ;;
    "nextcloud-compose-app"|"nextcloud") 
      echo "$HOME/symphony/cli/templates/margo/nextcloud-compose/deployment.yaml" ;;
    *) 
      echo "" ;;
  esac
}

get_oci_repository_path() {
  local package_name="$1"
  case $package_name in
    "custom-otel-helm-app"|"custom-otel")
      CONTAINER_URL="oci://${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}/library/custom-otel-app";;
      sed -i "s|\"url\": *\"http://[^\"]*\"|\"url\": \"$CONTAINER_URL\"|" "$HOME/symphony/cli/templates/margo/custom-otel-helm/instance.yaml";;
      echo "$HOME/symphony/cli/templates/margo/custom-otel-helm/instance.yaml" ;; 
    "nginx-helm-app"|"nginx")
      echo "oci://ghcr.io/nginx/charts/nginx-ingress";; 
    "nextcloud-compose-app"|"nextcloud") 
      echo "https://raw.githubusercontent.com/docker/awesome-compose/refs/heads/master/nextcloud-redis-mariadb/compose.yaml";; # this is packageLocation and not oci, need to fix this
    *)
      echo "" ;;
  esac
}

# Updated deploy_instance function
deploy_instance() {
  echo "🚀 Deploy Instance"
  echo "=================="
  
  echo "📦 Available packages:"
  if check_maestro_cli; then
    ${MAESTRO_CLI_PATH}/maestro wfm list app-pkg
  fi
  
  echo ""
  read -p "Enter the package name/ID to deploy: " package_id
  
  if [ -z "$package_id" ]; then
    echo "❌ Package name/ID is required"
    return 1
  fi
  
  echo ""
  echo "🖥️  Available devices:"
  ${MAESTRO_CLI_PATH}/maestro wfm list devices
  
  echo ""
  read -p "Enter the device ID for deployment: " device_id
  
  if [ -z "$device_id" ]; then
    echo "❌ Device ID is required"
    return 1
  fi

  # Get app package details and extract metadata.name
  echo "📋 Getting package details..."
  app_package_details=$(${MAESTRO_CLI_PATH}/maestro wfm get app-pkg ${package_id} -o json 2>/dev/null)
  
  if [ $? -ne 0 ] || [ -z "$app_package_details" ]; then
    echo "❌ Failed to get package details for '$package_id'"
    return 1
  fi
  
  # Parse JSON to extract metadata.name (using jq if available, otherwise grep/sed)
  if command -v jq >/dev/null 2>&1; then
    package_name=$(echo "$app_package_details" | jq -r '.metadata.name // empty')
  else
    # Fallback parsing without jq
    package_name=$(echo "$app_package_details" | grep -o '"name"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')
  fi
  
  if [ -z "$package_name" ]; then
    echo "❌ Could not extract package name from package details"
    return 1
  fi
  
  echo "📦 Package name: $package_name"
  
  # Get deployment file path based on package name
  deploy_file=$(get_instance_file_path "$package_name")
  repository=$(get_oci_repository_path "$package_name")
  
  if [ -z "$deploy_file" ] || [ ! -f "$deploy_file" ]; then
    echo "❌ Deployment file not found for package '$package_name'"
    echo "Expected file: $deploy_file"
    return 1
  fi
  
  echo "📄 Using deployment file: $deploy_file"

  
  # Update deployment file with device and package info if needed
  sed -i "s|{{DEVICE_ID}}|$device_id|g" "$deploy_file" 2>/dev/null || true
  sed -i "s|{{PACKAGE_ID}}|$package_id|g" "$deploy_file" 2>/dev/null || true
  sed -i "s|{{REPOSITORY}}|$repository|g" "$deploy_file" 2>/dev/null || true

  echo "🚀 Deploying '$package_id' to device '$device_id'..."
  if check_maestro_cli; then
    if ${MAESTRO_CLI_PATH}/maestro wfm deploy -f "$deploy_file" -d "$device_id" -p "$package_id"; then
      echo "✅ Instance deployment request sent successfully!"
      
      echo ""
      echo "📋 Updated deployments:"
      ${MAESTRO_CLI_PATH}/maestro wfm list deployments
    else
      echo "❌ Failed to deploy instance"
    fi
  fi
  
  echo ""
  read -p "Press Enter to continue..."
}


deploy_instance() {
  echo "🚀 Deploy Instance"
  echo "=================="
  
  echo "📦 Available packages:"
  if check_maestro_cli; then
    ${MAESTRO_CLI_PATH}/maestro wfm list app-pkg
  fi
  
  echo ""
  read -p "Enter the package name/ID to deploy: " package_id
  
  if [ -z "$package_id" ]; then
    echo "❌ Package name/ID is required"
    return 1
  fi
  
  echo ""
  echo "🖥️  Available devices:"
  ${MAESTRO_CLI_PATH}/maestro wfm list devices
  
  echo ""
  read -p "Enter the device ID for deployment: " device_id
  
  if [ -z "$device_id" ]; then
    echo "❌ Device ID is required"
    return 1
  fi


  // TODO: please finish this code
  app_package_details=$("${MAESTRO_CLI_PATH}/maestro wfm get app-pkg ${package_id} -o json")
  app_package_details ... json parse it and extract ... .metadata.name
  based on the package name, select one of the package path

  echo "🚀 Deploying '$package_id' to device '$device_id'..."
  if check_maestro_cli; then
    if ${MAESTRO_CLI_PATH}/maestro wfm deploy -f $deploy_file -d "$device_id" -p "$package_id"; then
      echo "✅ Instance deployment request sent successfully!"
      
      echo ""
      echo "📋 Updated deployments:"
      ${MAESTRO_CLI_PATH}/maestro wfm list deployments
    else
      echo "❌ Failed to deploy instance"
    fi
  fi
  
  echo ""
  read -p "Press Enter to continue..."
}

delete_instance() {
  echo "🗑️  Delete Instance"
  echo "=================="
  
  echo "🚀 Current deployments:"
  if check_maestro_cli; then
    ${MAESTRO_CLI_PATH}/maestro wfm list deployments
  fi
  
  echo ""
  read -p "Enter the deployment/instance ID to delete: " instance_id
  
  if [ -z "$instance_id" ]; then
    echo "❌ Instance ID is required"
    return 1
  fi
  
  read -p "Are you sure you want to delete instance '$instance_id'? (y/N): " confirm
  if [[ "$confirm" =~ ^[Yy]$ ]]; then
    echo "🗑️  Deleting instance '$instance_id'..."
    if check_maestro_cli; then
      if ${MAESTRO_CLI_PATH}/maestro wfm delete deployment "$instance_id"; then
        echo "✅ Instance '$instance_id' deleted successfully!"
        
        echo ""
        echo "📋 Updated deployments:"
        ${MAESTRO_CLI_PATH}/maestro wfm list deployments
      else
        echo "❌ Failed to delete instance '$instance_id'"
      fi
    fi
  else
    echo "Deletion cancelled"
  fi
  
  echo ""
  read -p "Press Enter to continue..."
}

# ----------------------------
# Menu Functions
# ----------------------------
show_menu() {
  clear
  echo "🎛️  WFM CLI Interactive Interface"
  echo "================================="
  echo "Choose an option:"
  echo "1) 📦 list app-pkg"
  echo "2) 🖥️  List Devices"
  echo "3) 🚀 List Deployments"
  echo "4) 📋 List All"
  echo "5) 📤 Upload App-Package"
  echo "6) 🗑️  Delete App-Package"
  echo "7) 🚀 Deploy Instance"
  echo "8) 🗑️  Delete Instance"
  echo "9) 🚪 Exit"
  echo ""
  
  read -p "Enter choice [1-9]: " choice
  case $choice in
    1) list_app_packages ;;
    2) list_devices ;;
    3) list_deployments ;;
    4) list_all ;;
    5) upload_app_package ;;
    6) delete_app_package ;;
    7) deploy_instance ;;
    8) delete_instance ;;
    9) echo "👋 Goodbye!"; exit 0 ;;
    *) echo "⚠️ Invalid choice"; sleep 2 ;;
  esac
}

# ----------------------------
# Main Script Execution
# ----------------------------
main_loop() {
  while true; do
    show_menu
  done
}

if [[ -z "$1" ]]; then
  main_loop
else
  case "$1" in
    list-packages) list_app_packages ;;
    list-devices) list_devices ;;
    list-deployments) list_deployments ;;
    list-all) list_all ;;
    upload) upload_app_package ;;
    delete-package) delete_app_package ;;
    deploy) deploy_instance ;;
    delete-instance) delete_instance ;;
    *) echo "Usage: $0 {list-packages|list-devices|list-deployments|list-all|upload|delete-package|deploy|delete-instance}"; exit 1 ;;
  esac
fi
