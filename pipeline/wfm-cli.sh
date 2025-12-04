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


#--- OCI Registry settings (can be overridden via env)
REGISTRY_URL="${REGISTRY_URL:-http://${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}}"
REGISTRY_USER="${REGISTRY_USER:-admin}"
REGISTRY_PASS="${REGISTRY_PASS:-Harbor12345}"
OCI_ORGANIZATION="${OCI_ORGANIZATION:-library}"


# ----------------------------
# Utility Functions
# ----------------------------
MAESTRO_CLI_PATH="$HOME/symphony/cli"  

install_basic_utilities() {
  apt install jq -y
}

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
    ${MAESTRO_CLI_PATH}/maestro wfm list deployment || echo "❌ Failed to list deployment"
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
    ${MAESTRO_CLI_PATH}/maestro wfm list deployment || echo "❌ Failed to list deployment"
  fi
  
  echo ""
  read -p "Press Enter to continue..."
}

# ----------------------------
# Package Management Functions
# ----------------------------
get_package_upload_request_file_path() {
  local choice="$1"
  
  # Use host:port only (strip http://)
  REGISTRY_HOST="${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}"
  
  case $choice in
    1) 
      original_pkg_file="$HOME/symphony/cli/templates/margo/custom-otel-helm/package.yaml"
      pkg_file="$HOME/symphony/cli/templates/margo/custom-otel-helm/package.yaml.copy"
      cp -f ${original_pkg_file} ${pkg_file} 
      sed -i "s|{{REGISTRY_URL}}|${REGISTRY_HOST}|g" "$pkg_file" 2>/dev/null || true
      sed -i "s|{{REPOSITORY}}|${OCI_ORGANIZATION}/custom-otel-helm-app-package|g" "$pkg_file" 2>/dev/null || true
      sed -i "s|{{TAG}}|latest|g" "$pkg_file" 2>/dev/null || true
      sed -i "s|{{REGISTRY_USER}}|${REGISTRY_USER}|g" "$pkg_file" 2>/dev/null || true
      sed -i "s|{{REGISTRY_PASS}}|${REGISTRY_PASS}|g" "$pkg_file" 2>/dev/null || true
      echo $pkg_file ;;
      
    2)
      original_pkg_file="$HOME/symphony/cli/templates/margo/nextcloud-compose/package.yaml"
      pkg_file="$HOME/symphony/cli/templates/margo/nextcloud-compose/package.yaml.copy"
      cp -f ${original_pkg_file} ${pkg_file}
      sed -i "s|{{REGISTRY_URL}}|${REGISTRY_HOST}|g" "$pkg_file" 2>/dev/null || true
      sed -i "s|{{REPOSITORY}}|${OCI_ORGANIZATION}/nextcloud-compose-app-package|g" "$pkg_file" 2>/dev/null || true
      sed -i "s|{{TAG}}|latest|g" "$pkg_file" 2>/dev/null || true
      sed -i "s|{{REGISTRY_USER}}|${REGISTRY_USER}|g" "$pkg_file" 2>/dev/null || true
      sed -i "s|{{REGISTRY_PASS}}|${REGISTRY_PASS}|g" "$pkg_file" 2>/dev/null || true
      echo $pkg_file ;;
      
    *) 
      echo "" ;;
  esac
}

get_package_name() {
  local choice="$1"
  case $choice in
    1) echo "Custom OTEL Helm App" ;;
    2) echo "Nextcloud Compose App" ;;
    *) echo "Unknown Package" ;;
  esac
}

upload_app_package() {
  echo "📦 Upload App Package"
  echo "===================="
  echo "Select one of the packages:"
  echo "1) Custom OTEL Helm App"
  echo "2) Nextcloud Compose App"
  echo "3) Exit"
  echo ""
  
  read -p "Enter choice [1-3]: " app_package_choice
  
  if [ "$app_package_choice" = "3" ]; then
    echo "Returning to main menu..."
    return 0
  fi
  
  if ! validate_choice "$app_package_choice" 2; then
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
get_instance_file_path() {
  local package_name="$1"
  local file_path=""
  
  # Validate HOME directory
  if [ -z "$HOME" ]; then
    echo "❌ HOME environment variable not set" >&2
    return 1
  fi
  
  case $package_name in
    "custom-otel-helm-app"|"custom-otel"|"otel-demo-pkg")
      original_file_path="$HOME/symphony/cli/templates/margo/custom-otel-helm/instance.yaml"
      file_path="$HOME/symphony/cli/templates/margo/custom-otel-helm/instance.yaml.copy"
      cp -f ${original_file_path} ${file_path} ;;
    "nextcloud-compose-app"|"nextcloud"|"nextcloud-pkg")
      original_file_path="$HOME/symphony/cli/templates/margo/nextcloud-compose/instance.yaml"
      file_path="$HOME/symphony/cli/templates/margo/nextcloud-compose/instance.yaml.copy"
      cp -f ${original_file_path} ${file_path} ;;
    *)
      return 1 ;;
  esac
  
  # Verify file exists before returning
  if [ -f "$file_path" ]; then
    echo "$file_path"
  else
    echo "❌ Deployment file not found: $file_path" >&2
    return 1
  fi
}

get_oci_repository_path() {
  local package_name="$1"
  local container_url=""
  
  case $package_name in
    "custom-otel-helm-app"|"custom-otel"|"otel-demo-pkg")
      container_url="oci://${EXPOSED_HARBOR_IP}:${EXPOSED_HARBOR_PORT}/library/custom-otel-helm" ;;
    "nextcloud-compose-app"|"nextcloud"|"nextcloud-pkg")
      container_url="https://raw.githubusercontent.com/docker/awesome-compose/refs/heads/master/nextcloud-redis-mariadb/compose.yaml" ;;
    *)
      container_url="" ;;
  esac
  
  echo "$container_url"
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

  # Get app package details and extract metadata.name
  echo "📋 Getting package details..."
  app_packages=$(${MAESTRO_CLI_PATH}/maestro wfm list app-pkg -o json 2>/dev/null)
  
  if [ $? -ne 0 ] || [ -z "$app_packages" ]; then
    echo "❌ Failed to get package list"
    return 1
  fi
  
  # Parse JSON to find the package and extract metadata.name
  if command -v jq >/dev/null 2>&1; then
    echo "🔍 Searching for package: $package_id"
    
    # Search by both ID and name in the nested structure
    package_name=$(echo "$app_packages" | jq -r --arg pkg_id "$package_id" '
      .Data[0].items[] | 
      select(.metadata.id == $pkg_id or .metadata.name == $pkg_id) | 
      .metadata.name
    ')
    
    if [ -z "$package_name" ] || [ "$package_name" = "null" ]; then
      echo "❌ Package '$package_id' not found in the package list"
      echo "Available packages:"
      echo "$app_packages" | jq -r '.Data[0].items[] | "  - Name: \(.metadata.name), ID: \(.metadata.id)"'
      return 1
    fi
  else
    echo "❌ jq command is required but not installed. Please install it and retry."
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
    if ${MAESTRO_CLI_PATH}/maestro wfm apply -f "$deploy_file"; then
      echo "✅ Instance deployment request sent successfully!"
      
      echo ""
      echo "📋 Updated deployments:"
      ${MAESTRO_CLI_PATH}/maestro wfm list deployment
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
    ${MAESTRO_CLI_PATH}/maestro wfm list deployment
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
        ${MAESTRO_CLI_PATH}/maestro wfm list deployment
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
  echo "3) 🚀 List Deployment"
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
  install_basic_utilities
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
