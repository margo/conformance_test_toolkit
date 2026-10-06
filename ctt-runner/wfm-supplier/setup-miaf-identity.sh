#!/usr/bin/env bash
# setup-miaf-identity.sh
#
# One-stop script to provision the CTT WFM-Supplier MIAF identity before
# running conformance scenarios.  Three modes, pick the one that fits your
# environment:
#
#   --mode self-signed   Generate a self-signed CA + SVID inside CTT.
#                        No MIS or sandbox required.  Use when:
#                          • You are testing CTT itself against the built-in
#                            mock WFM server (no real WFM involved).
#                          • Your WFM is configured to trust a CA you supply.
#                        Output files are trusted only by components that
#                        explicitly import the generated ca-cert.pem.
#
#   --mode sandbox       Mint an SVID from the Margo reference sandbox MIS
#                        (SPIRE + Symphony).  Use when:
#                          • Your WFM is the Margo reference Symphony instance.
#                          • You have SSH / local access to the sandbox VM.
#                        If the sandbox repo is not already cloned, pass
#                        --sandbox-repo-url <git-url> to fetch only the needed
#                        SVID-minting scripts (sparse clone — no full checkout).
#
#   --mode external      Validate and install SVID material you obtained from
#                        your WFM vendor's SPIFFE administrator.  Use when:
#                          • You are testing a vendor WFM with its own MIS/PKI.
#                        Pass --cert, --key, and --ca to name the files.
#
# Usage:
#   bash setup-miaf-identity.sh --mode self-signed [options]
#   bash setup-miaf-identity.sh --mode sandbox     [options]
#   bash setup-miaf-identity.sh --mode external    --cert F --key F --ca F
#   bash setup-miaf-identity.sh                    # interactive mode
#
# Common options:
#   --out-dir DIR       Where to write the three output files
#                       (default: utils/fixtures/miaf/real/ next to this script)
#   --trust-domain TD   SPIFFE trust domain (default: margo.org)
#   --wfm-id ID         WFM instance name in the SPIFFE path (default: symphony-1)
#   --client-id ID      CTT client name in the SPIFFE path (default: margo-ctt)
#   --days N            Cert validity for self-signed mode (default: 90)
#
# sandbox-mode-only options:
#   --sandbox-dir DIR       Path to the cloned sandbox repo (default: ~/test/sandbox)
#   --sandbox-repo-url URL  Git URL to sparse-clone if sandbox-dir/scripts/lib/mis
#                           is absent.  Required when sandbox is not pre-cloned.
#   --mis-base-url URL      MIS HTTPS URL (default: https://127.0.0.1:9443)
#   --allowlist-path PATH   Path to authorized-clients.json inside the WFM
#                           (default: ~/symphony/api/mis/authorized-clients.json)
#
# external-mode-only options:
#   --cert PATH    Path to the X.509-SVID cert PEM
#   --key  PATH    Path to the SVID private key PEM
#   --ca   PATH    Path to the MIS trust-bundle CA PEM
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEFAULT_OUT_DIR="${SCRIPT_DIR}/utils/fixtures/miaf/real"
PROVISION_SCRIPT="$(cd "${SCRIPT_DIR}/../../ctt-creator/common/scripts" && pwd)/provision-mis-identity.sh"

# ── defaults ──────────────────────────────────────────────────────────────────
MODE=""
OUT_DIR="${DEFAULT_OUT_DIR}"
TRUST_DOMAIN="margo.org"
WFM_ID="symphony-1"
CLIENT_ID="margo-ctt"
DAYS=90

SANDBOX_DIR="${SANDBOX_DIR:-${HOME}/test/sandbox}"
SANDBOX_REPO_URL=""
MIS_BASE_URL="https://127.0.0.1:9443"
ALLOWLIST_PATH="${HOME}/symphony/api/mis/authorized-clients.json"

EXT_CERT=""
EXT_KEY=""
EXT_CA=""

# ── arg parsing ───────────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
  case "$1" in
    --mode)              MODE="$2"; shift 2 ;;
    --out-dir)           OUT_DIR="$2"; shift 2 ;;
    --trust-domain)      TRUST_DOMAIN="$2"; shift 2 ;;
    --wfm-id)            WFM_ID="$2"; shift 2 ;;
    --client-id)         CLIENT_ID="$2"; shift 2 ;;
    --days)              DAYS="$2"; shift 2 ;;
    --sandbox-dir)       SANDBOX_DIR="$2"; shift 2 ;;
    --sandbox-repo-url)  SANDBOX_REPO_URL="$2"; shift 2 ;;
    --mis-base-url)      MIS_BASE_URL="$2"; shift 2 ;;
    --allowlist-path)    ALLOWLIST_PATH="$2"; shift 2 ;;
    --cert)              EXT_CERT="$2"; shift 2 ;;
    --key)               EXT_KEY="$2"; shift 2 ;;
    --ca)                EXT_CA="$2"; shift 2 ;;
    -h|--help)
      grep '^#' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) echo "[ERROR] Unknown argument: $1" >&2; exit 2 ;;
  esac
done

# ── helpers ───────────────────────────────────────────────────────────────────
info()    { echo "  [INFO]  $*"; }
success() { echo "  [OK]    $*"; }
warn()    { echo "  [WARN]  $*" >&2; }
err()     { echo "  [ERROR] $*" >&2; exit 1; }

banner() {
  echo ""
  echo "════════════════════════════════════════════════════════════════"
  echo "  CTT WFM Supplier — MIAF Identity Setup"
  echo "  Mode: ${MODE}"
  echo "  Output: ${OUT_DIR}"
  echo "════════════════════════════════════════════════════════════════"
  echo ""
}

check_existing() {
  local cert="${OUT_DIR}/client-svid-cert.pem"
  if [[ -f "${cert}" ]]; then
    local expiry
    expiry=$(openssl x509 -in "${cert}" -noout -enddate 2>/dev/null | sed 's/notAfter=//' || echo "unknown")
    local spiffe
    spiffe=$(openssl x509 -in "${cert}" -text -noout 2>/dev/null \
             | grep -oP 'URI:spiffe://[^\s,]+' | head -1 | sed 's/URI://' || echo "")
    info "Existing identity found:"
    [[ -n "$spiffe" ]] && info "  SPIFFE ID : ${spiffe}"
    info "  Expires   : ${expiry}"
    echo ""
    read -p "  Overwrite it? [y/N] " overwrite < /dev/tty
    [[ "${overwrite,,}" != "y" ]] && info "Keeping existing identity." && exit 0
    echo ""
  fi
}

verify_chain() {
  local ca="${OUT_DIR}/trust-bundle-ca.pem"
  local cert="${OUT_DIR}/client-svid-cert.pem"
  if openssl verify -CAfile "${ca}" "${cert}" >/dev/null 2>&1; then
    success "Chain verified: cert is signed by the trust bundle CA."
  else
    warn "Chain verification failed — cert may not be signed by the CA you fetched."
    warn "If testing a vendor WFM: make sure --cert and --ca came from the same MIS."
  fi
}

print_next_steps() {
  echo ""
  echo "────────────────────────────────────────────────────────────────"
  echo "  Identity is ready.  What to do next:"
  echo ""
  echo "  1. Run the conformance suite:"
  echo "       bash ctt-runner/ctt-start.sh"
  echo "       → WFM Supplier → Run scenario group tests → core"
  echo ""
  echo "  2. The runner picks up the identity automatically from:"
  echo "       ${OUT_DIR}/"
  echo ""
  echo "  3. Re-run this script when the SVID expires (check with:"
  echo "       openssl x509 -in ${OUT_DIR}/client-svid-cert.pem -noout -enddate)"
  echo "────────────────────────────────────────────────────────────────"
  echo ""
}

# ── interactive mode: pick a mode ────────────────────────────────────────────
if [[ -z "${MODE}" ]]; then
  echo ""
  echo "┌─────────────────────────────────────────────────────────────────────┐"
  echo "│  CTT WFM Supplier — MIAF Identity Setup                            │"
  echo "│                                                                     │"
  echo "│  Select how you want to get the CTT's X.509-SVID:                  │"
  echo "│                                                                     │"
  echo "│  1) self-signed   Generate a local CA + SVID (no MIS needed)       │"
  echo "│                   Use for: CTT self-tests, mock WFM, or when your  │"
  echo "│                   WFM trusts a CA you supply                       │"
  echo "│                                                                     │"
  echo "│  2) sandbox       Mint from the Margo reference sandbox MIS        │"
  echo "│                   Use for: testing against the Symphony reference   │"
  echo "│                   instance (Capgemini sandbox)                      │"
  echo "│                                                                     │"
  echo "│  3) external      Install a cert+key+CA you got from your WFM      │"
  echo "│                   vendor's SPIFFE administrator                     │"
  echo "│                   Use for: testing a vendor's own WFM              │"
  echo "└─────────────────────────────────────────────────────────────────────┘"
  echo ""
  read -p "  Select mode (1/2/3): " mode_choice < /dev/tty
  case "${mode_choice}" in
    1) MODE="self-signed" ;;
    2) MODE="sandbox" ;;
    3) MODE="external" ;;
    *) err "Invalid choice." ;;
  esac
  echo ""
fi

mkdir -p "${OUT_DIR}"

# ══════════════════════════════════════════════════════════════════════════════
# MODE 1 — SELF-SIGNED
# ══════════════════════════════════════════════════════════════════════════════
if [[ "${MODE}" == "self-signed" ]]; then
  banner
  check_existing

  if ! command -v openssl &>/dev/null; then
    err "openssl is required but not found."
  fi

  SPIFFE_ID="spiffe://${TRUST_DOMAIN}/margo/wfm/${WFM_ID}/client/${CLIENT_ID}"
  info "Generating self-signed CA + SVID"
  info "  SPIFFE ID : ${SPIFFE_ID}"
  info "  Valid for : ${DAYS} days"
  echo ""

  WORK="$(mktemp -d)"
  trap 'rm -rf "${WORK}"' EXIT

  # CA key + cert (EC P-256 — spec allows EC, and no minimum size constraint applies to CAs)
  openssl ecparam -name prime256v1 -genkey -noout -out "${WORK}/ca-key.pem" 2>/dev/null
  openssl req -new -x509 -days "${DAYS}" \
      -key "${WORK}/ca-key.pem" \
      -out "${WORK}/ca-cert.pem" \
      -subj "/O=Margo CTT/CN=CTT-Self-Signed-CA" 2>/dev/null

  # SVID key + cert (EC P-256, SPIFFE URI SAN)
  openssl ecparam -name prime256v1 -genkey -noout -out "${WORK}/svid-key.pem" 2>/dev/null
  openssl req -new \
      -key "${WORK}/svid-key.pem" \
      -out "${WORK}/svid.csr" \
      -subj "/O=Margo CTT/CN=${CLIENT_ID}" 2>/dev/null
  openssl x509 -req -days "${DAYS}" \
      -in "${WORK}/svid.csr" \
      -CA "${WORK}/ca-cert.pem" -CAkey "${WORK}/ca-key.pem" -CAcreateserial \
      -out "${WORK}/svid-cert.pem" \
      -extfile <(printf "subjectAltName=URI:%s\nbasicConstraints=CA:FALSE\nextendedKeyUsage=clientAuth" \
                        "${SPIFFE_ID}") 2>/dev/null

  cp "${WORK}/svid-cert.pem" "${OUT_DIR}/client-svid-cert.pem"
  cp "${WORK}/svid-key.pem"  "${OUT_DIR}/client-svid-key.pem"
  cp "${WORK}/ca-cert.pem"   "${OUT_DIR}/trust-bundle-ca.pem"

  success "Files written to ${OUT_DIR}/"
  echo ""
  echo "  client-svid-cert.pem  (SPIFFE ID: ${SPIFFE_ID})"
  echo "  client-svid-key.pem"
  echo "  trust-bundle-ca.pem   ← give this to your WFM so it trusts the CTT"
  echo ""
  warn "Self-signed mode: you MUST import trust-bundle-ca.pem into your WFM's"
  warn "trust store before running tests, or mTLS will fail (unknown CA)."
  print_next_steps
  exit 0
fi

# ══════════════════════════════════════════════════════════════════════════════
# MODE 2 — SANDBOX (Margo reference MIS)
# ══════════════════════════════════════════════════════════════════════════════
if [[ "${MODE}" == "sandbox" ]]; then
  banner
  check_existing

  if [[ ! -f "${PROVISION_SCRIPT}" ]]; then
    err "provision-mis-identity.sh not found at ${PROVISION_SCRIPT}"
  fi

  echo "  Sandbox MIS defaults (press Enter to accept):"
  echo ""
  read -p "  MIS base URL   [${MIS_BASE_URL}]: " input < /dev/tty
  MIS_BASE_URL="${input:-${MIS_BASE_URL}}"

  read -p "  Trust domain   [${TRUST_DOMAIN}]: " input < /dev/tty
  TRUST_DOMAIN="${input:-${TRUST_DOMAIN}}"

  read -p "  WFM ID         [${WFM_ID}]: " input < /dev/tty
  WFM_ID="${input:-${WFM_ID}}"

  read -p "  Client ID      [${CLIENT_ID}]: " input < /dev/tty
  CLIENT_ID="${input:-${CLIENT_ID}}"

  if [[ -z "${SANDBOX_REPO_URL}" ]]; then
    local_svid_gen="${SANDBOX_DIR}/scripts/lib/mis/svid-gen.sh"
    if [[ ! -f "${local_svid_gen}" ]]; then
      echo ""
      warn "Sandbox scripts not found at ${SANDBOX_DIR}/scripts/lib/mis/"
      echo ""
      echo "  To auto-fetch just the needed SVID scripts (no full checkout),"
      echo "  provide the sandbox git URL.  Leave blank to skip and get"
      echo "  instructions for manual setup instead."
      echo ""
      read -p "  Sandbox git URL (or Enter to skip): " input < /dev/tty
      SANDBOX_REPO_URL="${input:-}"
    fi
  fi

  echo ""
  info "Running provision-mis-identity.sh..."
  echo ""

  provision_args=(
    "--mis-base-url"   "${MIS_BASE_URL}"
    "--trust-domain"   "${TRUST_DOMAIN}"
    "--wfm-id"         "${WFM_ID}"
    "--client-id"      "${CLIENT_ID}"
    "--sandbox-dir"    "${SANDBOX_DIR}"
    "--out-dir"        "${OUT_DIR}"
  )
  [[ -n "${SANDBOX_REPO_URL}" ]] && provision_args+=("--sandbox-repo-url" "${SANDBOX_REPO_URL}")

  if bash "${PROVISION_SCRIPT}" "${provision_args[@]}"; then
    echo ""
    success "Identity provisioned from sandbox MIS."
    verify_chain
    print_next_steps
  else
    echo ""
    err "Provision script failed.  Check MIS reachability at ${MIS_BASE_URL} and re-run."
  fi
  exit 0
fi

# ══════════════════════════════════════════════════════════════════════════════
# MODE 3 — EXTERNAL (vendor-provided SVID)
# ══════════════════════════════════════════════════════════════════════════════
if [[ "${MODE}" == "external" ]]; then
  banner

  if [[ -z "${EXT_CERT}" || -z "${EXT_KEY}" || -z "${EXT_CA}" ]]; then
    echo "  Provide the three files your WFM's SPIFFE administrator issued for CTT."
    echo "  The SVID cert must have a SPIFFE URI SAN matching:"
    echo "    spiffe://<trust-domain>/margo/wfm/<wfm-id>/client/<client-id>"
    echo ""
    read -p "  Path to SVID cert PEM  : " EXT_CERT < /dev/tty
    read -p "  Path to SVID key PEM   : " EXT_KEY  < /dev/tty
    read -p "  Path to trust bundle CA: " EXT_CA   < /dev/tty
  fi

  [[ ! -f "${EXT_CERT}" ]] && err "Cert not found: ${EXT_CERT}"
  [[ ! -f "${EXT_KEY}"  ]] && err "Key not found:  ${EXT_KEY}"
  [[ ! -f "${EXT_CA}"   ]] && err "CA not found:   ${EXT_CA}"

  # Validate: cert must parse as X.509
  openssl x509 -in "${EXT_CERT}" -noout 2>/dev/null \
    || err "${EXT_CERT} does not appear to be a valid PEM certificate."

  # Validate: SPIFFE URI SAN must be present
  spiffe=$(openssl x509 -in "${EXT_CERT}" -text -noout 2>/dev/null \
           | grep -oP 'URI:spiffe://[^\s,]+' | head -1 | sed 's/URI://' || echo "")
  if [[ -z "${spiffe}" ]]; then
    err "No SPIFFE URI SAN found in ${EXT_CERT}. The cert must be an X.509-SVID."
  fi

  # Validate: SPIFFE ID path must match WFM-client format
  if ! echo "${spiffe}" | grep -qP 'spiffe://[^/]+/margo/wfm/[^/]+/client/[^/]+'; then
    warn "SPIFFE ID '${spiffe}' does not match the required WFM-client path format:"
    warn "  spiffe://<trust-domain>/margo/wfm/<wfm-id>/client/<client-id>"
    warn "The WFM may reject this SVID at mTLS time."
  fi

  check_existing

  cp "${EXT_CERT}" "${OUT_DIR}/client-svid-cert.pem"
  cp "${EXT_KEY}"  "${OUT_DIR}/client-svid-key.pem"
  cp "${EXT_CA}"   "${OUT_DIR}/trust-bundle-ca.pem"

  echo ""
  success "Files installed to ${OUT_DIR}/"
  info "  SPIFFE ID : ${spiffe}"
  openssl x509 -in "${OUT_DIR}/client-svid-cert.pem" -noout -enddate 2>/dev/null \
    | sed 's/notAfter=/  Expires   : /'
  echo ""
  verify_chain
  print_next_steps
  exit 0
fi

err "Unknown mode '${MODE}'. Use --mode self-signed|sandbox|external or run without --mode for interactive."
