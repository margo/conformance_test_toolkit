#!/usr/bin/env bash
# Provisions a multi-component ApplicationDeployment in the WFM so the
# wfm-multi-component-deployment scenario can run without manual operator
# intervention.
#
# WHAT THIS DOES
#   1. Authenticates with the WFM management API (NBI) and gets a JWT token.
#   2. Registers a Margo app package from the specified OCI registry.
#   3. Creates an ApplicationDeployment with two components
#      (one wait:true, one wait:false) assigned to the specified device SPIFFE ID.
#
# USAGE
#   bash provision-multi-component.sh [OPTIONS]
#
# OPTIONS
#   --nbi-url URL         WFM NBI base URL  (default: https://localhost:8082/v1alpha2)
#   --nbi-user USER       NBI username      (default: admin)
#   --nbi-pass PASS       NBI password      (default: empty string — Symphony test-user default)
#   --registry-url URL    OCI registry host (default: harbor.machine:8443)
#   --registry-repo REPO  OCI repository    (default: library/margo-ctt-hello-world)
#   --registry-tag TAG    OCI tag           (default: 1.0.0)
#   --device-id ID        Device SPIFFE ID  (default: spiffe://margo.org/margo/wfm/symphony-1/client/margo-ctt)
#   --compose-image REF   oci:// ref for compose components (default: oci://harbor.machine:8443/library/nextcloud-compose-archive)
#
# SYMPHONY SANDBOX QUICKSTART (no args needed)
#   bash provision-multi-component.sh
#
# VENDOR ADAPTATION
#   Adjust --nbi-url, --nbi-user, --nbi-pass to match your WFM's operator API.
#   Adjust --registry-url and --registry-repo to point at your app package.
#   Adjust --device-id to match the SPIFFE ID your CTT will present.
#   Adjust --compose-image to a real compose OCI image in your registry.
#
# CLEANUP
#   Pass --cleanup to delete the provisioned package and deployment (idempotent).

set -euo pipefail

# ── Defaults ────────────────────────────────────────────────────────────────
NBI_URL="https://localhost:8082/v1alpha2"
NBI_USER="admin"
NBI_PASS=""
REGISTRY_URL="harbor.machine:8443"
REGISTRY_REPO="library/margo-ctt-hello-world"
REGISTRY_TAG="1.0.0"
DEVICE_ID="spiffe://margo.org/margo/wfm/symphony-1/client/margo-ctt"
COMPOSE_IMAGE="oci://harbor.machine:8443/library/nextcloud-compose-archive"
CLEANUP=false
PKG_NAME="ctt-multi-component-app"
DEPLOY_NAME="ctt-multi-component-deployment"

# ── Argument parsing ─────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
  case "$1" in
    --nbi-url)      NBI_URL="$2";       shift 2 ;;
    --nbi-user)     NBI_USER="$2";      shift 2 ;;
    --nbi-pass)     NBI_PASS="$2";      shift 2 ;;
    --registry-url) REGISTRY_URL="$2";  shift 2 ;;
    --registry-repo)REGISTRY_REPO="$2"; shift 2 ;;
    --registry-tag) REGISTRY_TAG="$2";  shift 2 ;;
    --device-id)    DEVICE_ID="$2";     shift 2 ;;
    --compose-image)COMPOSE_IMAGE="$2"; shift 2 ;;
    --cleanup)      CLEANUP=true;       shift   ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

PKGS_URL="$NBI_URL/margo/nbi/v1/app-packages"
DEPS_URL="$NBI_URL/margo/nbi/v1/app-deployments"

# ── Auth ─────────────────────────────────────────────────────────────────────
echo "Authenticating with WFM NBI at $NBI_URL ..."
TOKEN=$(curl -sk -X POST "$NBI_URL/users/auth" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"$NBI_USER\",\"password\":\"$NBI_PASS\"}" \
  | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('accessToken',''))" 2>/dev/null)

if [[ -z "$TOKEN" ]]; then
  echo "ERROR: failed to get auth token from $NBI_URL/users/auth"
  echo "  Check --nbi-url, --nbi-user, --nbi-pass"
  exit 1
fi
echo "  Auth OK"

# ── Helper: curl with auth ────────────────────────────────────────────────────
nbi() { curl -sk -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" "$@"; }

# ── Cleanup mode ─────────────────────────────────────────────────────────────
if [[ "$CLEANUP" == "true" ]]; then
  echo "Cleanup: looking for provisioned package and deployment..."
  PKGS=$(nbi "$PKGS_URL")
  PKG_ID=$(echo "$PKGS" | python3 -c "
import sys,json
items=json.load(sys.stdin).get('items') or []
for i in items:
    if i.get('metadata',{}).get('name','') == '$PKG_NAME':
        print(i.get('id',''))
        break
" 2>/dev/null)
  DEPS=$(nbi "$DEPS_URL")
  DEP_IDS=$(echo "$DEPS" | python3 -c "
import sys,json
items=json.load(sys.stdin).get('items') or []
for i in items:
    n = i.get('metadata',{}).get('name','')
    if n.startswith('ctt-multi-component-'):
        print(i.get('id',''))
" 2>/dev/null)
  for DEP_ID in $DEP_IDS; do
    nbi -X DELETE "$DEPS_URL/$DEP_ID" >/dev/null && echo "  Deleted deployment $DEP_ID"
  done
  [[ -n "$PKG_ID" ]] && nbi -X DELETE "$PKGS_URL/$PKG_ID" >/dev/null && echo "  Deleted package $PKG_ID"
  echo "Cleanup done."
  exit 0
fi

# ── Step 1: Register app package ─────────────────────────────────────────────
echo "Checking for existing app package '$PKG_NAME'..."
PKGS=$(nbi "$PKGS_URL")
PKG_ID=$(echo "$PKGS" | python3 -c "
import sys,json
items=json.load(sys.stdin).get('items') or []
for i in items:
    if i.get('metadata',{}).get('name','') == '$PKG_NAME':
        print(i.get('id',''))
        break
" 2>/dev/null)

if [[ -n "$PKG_ID" ]]; then
  echo "  App package already registered: $PKG_ID"
else
  echo "Registering app package from $REGISTRY_URL/$REGISTRY_REPO:$REGISTRY_TAG ..."
  RESP=$(nbi -X POST "$PKGS_URL" -d "{
    \"apiVersion\": \"v1\",
    \"metadata\": {\"name\": \"$PKG_NAME\"},
    \"spec\": {
      \"sourceType\": \"OCI_REPO\",
      \"source\": {
        \"registryUrl\": \"$REGISTRY_URL\",
        \"repository\": \"$REGISTRY_REPO\",
        \"tag\": \"$REGISTRY_TAG\"
      }
    }
  }")
  PKG_ID=$(echo "$RESP" | python3 -c "
import sys,json
d=json.load(sys.stdin)
print(d.get('Package',d).get('id',''))
" 2>/dev/null)
  if [[ -z "$PKG_ID" ]]; then
    echo "ERROR: failed to register app package. Response:"
    echo "$RESP"
    exit 1
  fi
  echo "  Registered package: $PKG_ID"

  # Poll until COMPLETED
  echo "  Waiting for package onboarding to complete..."
  for i in $(seq 1 20); do
    sleep 2
    STATUS=$(nbi "$PKGS_URL/$PKG_ID" | python3 -c "
import sys,json
items=json.load(sys.stdin).get('items') or [{}]
print(items[0].get('recentOperation',{}).get('status',''))
" 2>/dev/null)
    [[ "$STATUS" == "COMPLETED" ]] && echo "  Package onboarded (COMPLETED)" && break
    [[ "$STATUS" == "FAILED" ]]    && echo "ERROR: package onboarding FAILED" && exit 1
    echo "  ... ($STATUS)"
  done
fi

# ── Step 2: Create deployment ─────────────────────────────────────────────────
# Use a timestamp suffix so re-runs after a stuck REMOVING deployment always get a fresh one.
DEPLOY_NAME="ctt-multi-component-$(date -u +%Y%m%d%H%M%S)"

echo "Checking for active deployment for device '$DEVICE_ID'..."
DEPS=$(nbi "$DEPS_URL")
DEP_ID=$(echo "$DEPS" | python3 -c "
import sys,json
items=json.load(sys.stdin).get('items') or []
for i in items:
    state = i.get('status',{}).get('state','')
    dev_id = i.get('spec',{}).get('deviceRef',{}).get('id','')
    # Only reuse an actively deployed deployment (not REMOVING/FAILED)
    if dev_id == '$DEVICE_ID' and state not in ('REMOVING','FAILED',''):
        print(i.get('id',''))
        break
" 2>/dev/null)

if [[ -n "$DEP_ID" ]]; then
  echo "  Active deployment already exists: $DEP_ID"
else
  echo "Creating deployment targeting device: $DEVICE_ID ..."
  RESP=$(nbi -X POST "$DEPS_URL" -d "{
    \"metadata\": {\"name\": \"$DEPLOY_NAME\"},
    \"spec\": {
      \"appPackageRef\": {\"id\": \"$PKG_ID\"},
      \"deviceRef\": {\"id\": \"$DEVICE_ID\"},
      \"deploymentProfile\": {
        \"type\": \"compose\",
        \"components\": [
          {
            \"name\": \"wait-true-component\",
            \"properties\": {
              \"repository\": \"$COMPOSE_IMAGE\",
              \"revision\": \"1.0.0\",
              \"wait\": true
            }
          },
          {
            \"name\": \"wait-false-component\",
            \"properties\": {
              \"repository\": \"$COMPOSE_IMAGE\",
              \"revision\": \"1.0.0\",
              \"wait\": false
            }
          }
        ]
      }
    }
  }")
  DEP_ID=$(echo "$RESP" | python3 -c "
import sys,json
d=json.load(sys.stdin)
print(d.get('id',''))
" 2>/dev/null)
  if [[ -z "$DEP_ID" ]]; then
    echo "ERROR: failed to create deployment. Response:"
    echo "$RESP"
    exit 1
  fi
  echo "  Deployment created: $DEP_ID"
fi

echo ""
echo "Done. Multi-component deployment provisioned:"
echo "  Package ID   : $PKG_ID"
echo "  Deployment ID: $DEP_ID"
echo "  Device       : $DEVICE_ID"
echo ""
echo "You can now run the wfm-multi-component-deployment scenario."
echo "To clean up afterwards: bash provision-multi-component.sh --cleanup"
