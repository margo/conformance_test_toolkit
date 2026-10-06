#!/usr/bin/env bash
# provision-mis-identity.sh
#
# Demo/self-test convenience ONLY — mints the CTT's own WFM-Client X.509-SVID
# from a real MIS, registers it in the target WFM's accepted-client allowlist,
# and fetches that WFM's trust bundle — the three one-time setup steps MIAF
# requires before any conformance scenario can run (see
# CONFORMANCE_FLOWS_AND_MIAF_MIGRATION.md Part 5.7/7.1).
#
# This script is NEVER used when testing a real vendor's WFM: a real vendor
# mints and registers our identity themselves, using their own MIS/PKI and
# their own WFM's admin tooling — this suite never assumes access to either.
# This script exists only for demoing/self-testing against a `margo/sandbox`
# reference deployment (Symphony + MIS), where WE are also the operator of
# the system under test. It shells out to that sandbox checkout's own scripts
# by path — it does not import or depend on any sandbox code.
#
# Usage:
#   ./provision-mis-identity.sh [--sandbox-dir DIR] [--trust-domain DOMAIN]
#       [--wfm-id ID] [--client-id ID] [--mis-base-url URL]
#       [--allowlist-path PATH] [--out-dir DIR]
#
# All flags have defaults matching this project's own demo VM; override only
# what differs in your environment.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFORMANCE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

SANDBOX_DIR="${SANDBOX_DIR:-${HOME}/test/sandbox}"
TRUST_DOMAIN="${TRUST_DOMAIN:-margo.org}"
WFM_ID="${WFM_ID:-symphony-1}"
CLIENT_ID="${CLIENT_ID:-margo-ctt}"
MIS_BASE_URL="${MIS_BASE_URL:-https://mis.margo.org:9443}"
ALLOWLIST_PATH="${ALLOWLIST_PATH:-${HOME}/symphony/api/mis/authorized-clients.json}"
OUT_DIR="${OUT_DIR:-${CONFORMANCE_DIR}/wfm-supplier/fixtures/miaf/real}"
SANDBOX_REPO_URL="${SANDBOX_REPO_URL:-}"   # set to auto-clone the sandbox if svid-gen.sh is absent

while [[ $# -gt 0 ]]; do
  case "$1" in
    --sandbox-dir)        SANDBOX_DIR="$2"; shift 2 ;;
    --trust-domain)       TRUST_DOMAIN="$2"; shift 2 ;;
    --wfm-id)             WFM_ID="$2"; shift 2 ;;
    --client-id)          CLIENT_ID="$2"; shift 2 ;;
    --mis-base-url)       MIS_BASE_URL="$2"; shift 2 ;;
    --allowlist-path)     ALLOWLIST_PATH="$2"; shift 2 ;;
    --out-dir)            OUT_DIR="$2"; shift 2 ;;
    --sandbox-repo-url)   SANDBOX_REPO_URL="$2"; shift 2 ;;
    -h|--help)
      grep '^#' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) echo "[ERROR] Unknown argument: $1" >&2; exit 2 ;;
  esac
done

SPIFFE_ID="spiffe://${TRUST_DOMAIN}/margo/wfm/${WFM_ID}/client/${CLIENT_ID}"
SVID_GEN="${SANDBOX_DIR}/scripts/lib/mis/svid-gen.sh"

echo "════════════════════════════════════════════════════════════════"
echo " Provisioning CTT identity via MIS"
echo "════════════════════════════════════════════════════════════════"
echo " SPIFFE ID       : ${SPIFFE_ID}"
echo " MIS base URL    : ${MIS_BASE_URL}"
echo " Allowlist file  : ${ALLOWLIST_PATH}"
echo " Output dir      : ${OUT_DIR}"
echo "════════════════════════════════════════════════════════════════"

if [[ ! -f "${SVID_GEN}" ]]; then
  if [[ -n "${SANDBOX_REPO_URL}" ]]; then
    echo "[INFO] svid-gen.sh not found at ${SVID_GEN}"
    echo "[INFO] Attempting sparse clone of sandbox scripts from: ${SANDBOX_REPO_URL}"
    if ! command -v git &>/dev/null; then
      echo "[ERROR] git is required for auto-clone but was not found." >&2
      echo "[ERROR] Install git or manually clone the sandbox repo to ${SANDBOX_DIR}" >&2
      exit 1
    fi
    mkdir -p "${SANDBOX_DIR}"
    git -C "${SANDBOX_DIR}" init -q 2>/dev/null || true
    git -C "${SANDBOX_DIR}" remote add origin "${SANDBOX_REPO_URL}" 2>/dev/null || \
      git -C "${SANDBOX_DIR}" remote set-url origin "${SANDBOX_REPO_URL}"
    git -C "${SANDBOX_DIR}" config core.sparseCheckout true
    mkdir -p "${SANDBOX_DIR}/.git/info"
    echo "scripts/lib/mis/" > "${SANDBOX_DIR}/.git/info/sparse-checkout"
    git -C "${SANDBOX_DIR}" fetch --depth=1 origin HEAD
    git -C "${SANDBOX_DIR}" checkout FETCH_HEAD -- scripts/lib/mis/ 2>/dev/null || \
      git -C "${SANDBOX_DIR}" reset --hard FETCH_HEAD
    echo "[INFO] Sandbox scripts fetched to ${SANDBOX_DIR}/scripts/lib/mis/"
  fi
  if [[ ! -f "${SVID_GEN}" ]]; then
    echo "[ERROR] svid-gen.sh not found at ${SVID_GEN}" >&2
    echo "[ERROR] Options:" >&2
    echo "[ERROR]   1. Clone the sandbox repo: git clone <sandbox-repo-url> ${SANDBOX_DIR}" >&2
    echo "[ERROR]   2. Pass --sandbox-repo-url <url> to auto-clone the needed scripts" >&2
    echo "[ERROR]   3. Pass --sandbox-dir <path> if sandbox is cloned elsewhere" >&2
    exit 1
  fi
fi

mkdir -p "${OUT_DIR}"

echo ""
echo ">>> STEP 1: Minting SVID from MIS"
WORKDIR="$(mktemp -d)"
( cd "${WORKDIR}" && sudo bash "${SVID_GEN}" --automated --principal wfm-client --spiffe-id "${SPIFFE_ID}" )
MINTED_DIR="${WORKDIR}/x509svid-wfmclient"
if [[ ! -f "${MINTED_DIR}/payload-cert.pem" ]]; then
  echo "[ERROR] Minting appeared to succeed but ${MINTED_DIR}/payload-cert.pem is missing." >&2
  exit 1
fi
sudo chmod 644 "${MINTED_DIR}/payload-cert.pem" "${MINTED_DIR}/payload-key.pem"
sudo chown "$(id -u):$(id -g)" "${MINTED_DIR}/payload-cert.pem" "${MINTED_DIR}/payload-key.pem"
cp "${MINTED_DIR}/payload-cert.pem" "${OUT_DIR}/client-svid-cert.pem"
cp "${MINTED_DIR}/payload-key.pem"  "${OUT_DIR}/client-svid-key.pem"
# docker cp (run as root inside svid-gen.sh) leaves root-owned files here.
sudo rm -rf "${WORKDIR}"
echo "✓ Minted and copied to ${OUT_DIR}/client-svid-{cert,key}.pem"

echo ""
echo ">>> STEP 2: Registering SPIFFE ID in the WFM's accepted-client allowlist"
if [[ ! -f "${ALLOWLIST_PATH}" ]]; then
  echo "[]" | sudo tee "${ALLOWLIST_PATH}" > /dev/null
fi
CURRENT_JSON="$(sudo cat "${ALLOWLIST_PATH}")"
UPDATED_JSON="$(echo "${CURRENT_JSON}" | jq --arg id "${SPIFFE_ID}" '. + [$id] | unique')"
echo "${UPDATED_JSON}" | sudo tee "${ALLOWLIST_PATH}" > /dev/null
echo "✓ ${ALLOWLIST_PATH} now contains:"
echo "${UPDATED_JSON}" | jq -r '.[] | "    " + .'

echo ""
echo ">>> STEP 3: Fetching the WFM's trust bundle from MIS"
node "${CONFORMANCE_DIR}/wfm-supplier/run_wfm_scenarios.js" --fetch-trust-bundle \
  "${MIS_BASE_URL}" "${OUT_DIR}/trust-bundle-ca.pem"

echo ""
echo "════════════════════════════════════════════════════════════════"
echo " Done. To use this identity in a run_wfm_scenarios.js run:"
echo ""
echo "   cp ${OUT_DIR}/client-svid-cert.pem <cert-dir>/svid-cert.pem"
echo "   cp ${OUT_DIR}/client-svid-key.pem  <cert-dir>/svid-key.pem"
echo "   cp ${OUT_DIR}/trust-bundle-ca.pem  <cert-dir>/svid-ca.pem"
echo "════════════════════════════════════════════════════════════════"
