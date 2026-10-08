#!/usr/bin/env bash
# ctt-mis.sh
#
# Self-contained MIS-role script for CTT identity setup.
# Generates all SPIFFE identity material needed to run CTT conformance tests
# without a real SPIRE server or sandbox clone:
#
#   WFM SVID      → certs presented by the mock WFM during mTLS handshakes
#   Device SVID   → certs presented by the mock device-agent (CTT runner)
#   trustDetails  → trust domain string, for reference
#   trust-bundle  → SPIFFE JWKS trust bundle (for /.well-known/spiffe/bundle.json)
#
# Usage:
#   bash ctt-runner/ctt-mis.sh
#
# Outputs land in OUTPUT_DIR (below). Override any variable by setting it in
# the environment before running:
#   WFM_ID=my-wfm bash ctt-runner/ctt-mis.sh
#
# ── Configurable variables ─────────────────────────────────────────────────
TRUST_DOMAIN="${TRUST_DOMAIN:-margo.org}"
WFM_ID="${WFM_ID:-ctt-mock-wfm}"
WFM_CLIENT_ID="${WFM_CLIENT_ID:-ctt-device-001}"
OUTPUT_DIR="${OUTPUT_DIR:-${HOME}/conformance-identities}"
CERT_DAYS="${CERT_DAYS:-825}"          # ~2 years
# When set to the CTT conformance directory (ctt-runner/), auto-copy outputs
# to the paths CTT expects under wfm-supplier/utils/fixtures/miaf/real/.
CTT_DIR="${CTT_DIR:-}"
# ──────────────────────────────────────────────────────────────────────────

set -euo pipefail

# Derived SPIFFE IDs (read-only — change via variables above)
WFM_SPIFFE_ID="spiffe://${TRUST_DOMAIN}/margo/wfm/${WFM_ID}"
DEVICE_SPIFFE_ID="spiffe://${TRUST_DOMAIN}/margo/wfm/${WFM_ID}/client/${WFM_CLIENT_ID}"

# ── preflight ──────────────────────────────────────────────────────────────
if ! command -v openssl &>/dev/null; then
  echo "[ERROR] openssl is required but not found." >&2; exit 1
fi
if ! command -v python3 &>/dev/null; then
  echo "[ERROR] python3 is required (for trust-bundle JWKS generation)." >&2; exit 1
fi

mkdir -p "${OUTPUT_DIR}"
WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT

echo ""
echo "════════════════════════════════════════════════════════════════"
echo "  CTT Self-Contained MIS Role"
echo "════════════════════════════════════════════════════════════════"
echo "  Trust domain    : ${TRUST_DOMAIN}"
echo "  WFM SPIFFE ID   : ${WFM_SPIFFE_ID}"
echo "  Device SPIFFE ID: ${DEVICE_SPIFFE_ID}"
echo "  Output dir      : ${OUTPUT_DIR}"
echo "  Cert validity   : ${CERT_DAYS} days"
echo "════════════════════════════════════════════════════════════════"
echo ""

# ── Step 1: CA (trust anchor) ──────────────────────────────────────────────
echo ">>> Step 1/4: Generating CA (trust anchor)"
openssl ecparam -name prime256v1 -genkey -noout \
    -out "${WORK}/ca-key.pem" 2>/dev/null
openssl req -new -x509 -days "${CERT_DAYS}" \
    -key "${WORK}/ca-key.pem" \
    -out "${WORK}/ca-cert.pem" \
    -subj "/O=Margo CTT/CN=CTT-MIS-CA/C=US" 2>/dev/null
echo "    CA cert: ${OUTPUT_DIR}/ca-cert.pem"

# ── Step 2: WFM SVID ───────────────────────────────────────────────────────
echo ""
echo ">>> Step 2/4: Generating WFM SVID"
openssl ecparam -name prime256v1 -genkey -noout \
    -out "${WORK}/wfm-svid-key.pem" 2>/dev/null
openssl req -new \
    -key "${WORK}/wfm-svid-key.pem" \
    -out "${WORK}/wfm-svid.csr" \
    -subj "/O=Margo CTT/CN=${WFM_ID}" 2>/dev/null
openssl x509 -req -days "${CERT_DAYS}" \
    -in "${WORK}/wfm-svid.csr" \
    -CA "${WORK}/ca-cert.pem" -CAkey "${WORK}/ca-key.pem" -CAcreateserial \
    -out "${WORK}/wfm-svid-cert.pem" \
    -extfile <(printf \
        "subjectAltName=URI:%s\nbasicConstraints=CA:FALSE\nextendedKeyUsage=serverAuth,clientAuth" \
        "${WFM_SPIFFE_ID}") 2>/dev/null
echo "    WFM SVID : ${OUTPUT_DIR}/wfm-svid-cert.pem"
echo "    WFM key  : ${OUTPUT_DIR}/wfm-svid-key.pem"
echo "    SPIFFE ID: ${WFM_SPIFFE_ID}"

# ── Step 3: Device/Client SVID ─────────────────────────────────────────────
echo ""
echo ">>> Step 3/4: Generating Device (WFM-client) SVID"
openssl ecparam -name prime256v1 -genkey -noout \
    -out "${WORK}/device-svid-key.pem" 2>/dev/null
openssl req -new \
    -key "${WORK}/device-svid-key.pem" \
    -out "${WORK}/device-svid.csr" \
    -subj "/O=Margo CTT/CN=${WFM_CLIENT_ID}" 2>/dev/null
openssl x509 -req -days "${CERT_DAYS}" \
    -in "${WORK}/device-svid.csr" \
    -CA "${WORK}/ca-cert.pem" -CAkey "${WORK}/ca-key.pem" -CAcreateserial \
    -out "${WORK}/device-svid-cert.pem" \
    -extfile <(printf \
        "subjectAltName=URI:%s\nbasicConstraints=CA:FALSE\nextendedKeyUsage=clientAuth" \
        "${DEVICE_SPIFFE_ID}") 2>/dev/null
echo "    Device SVID: ${OUTPUT_DIR}/device-svid-cert.pem"
echo "    Device key : ${OUTPUT_DIR}/device-svid-key.pem"
echo "    SPIFFE ID  : ${DEVICE_SPIFFE_ID}"

# ── Step 4: Trust bundle (SPIFFE JWKS) ────────────────────────────────────
echo ""
echo ">>> Step 4/4: Generating trust bundle (SPIFFE JWKS)"
python3 - "${WORK}/ca-cert.pem" "${WORK}/trust-bundle.json" <<'PYEOF'
import base64, json, subprocess, sys

cert_pem_path = sys.argv[1]
out_path      = sys.argv[2]

# Convert PEM cert to DER for x5c field
cert_der = subprocess.run(
    ['openssl', 'x509', '-in', cert_pem_path, '-outform', 'DER'],
    capture_output=True, check=True
).stdout
x5c = base64.b64encode(cert_der).decode('ascii')

# Extract EC public key DER from the cert
pubkey_pem = subprocess.run(
    ['openssl', 'x509', '-in', cert_pem_path, '-pubkey', '-noout'],
    capture_output=True, check=True
).stdout
pubkey_der = subprocess.run(
    ['openssl', 'ec', '-pubin', '-outform', 'DER'],
    input=pubkey_pem, capture_output=True, check=True
).stdout

# P-256 DER: last 65 bytes are uncompressed point: 04 || x[32] || y[32]
point = pubkey_der[-65:]
if point[0] != 4:
    sys.exit('ERROR: unexpected EC public key format — expected uncompressed P-256 point')

def b64url(b):
    return base64.urlsafe_b64encode(b).rstrip(b'=').decode('ascii')

bundle = {
    "keys": [
        {
            "kty":  "EC",
            "use":  "x509-svid",
            "crv":  "P-256",
            "x":    b64url(point[1:33]),
            "y":    b64url(point[33:65]),
            "x5c":  [x5c]
        }
    ]
}

with open(out_path, 'w') as f:
    json.dump(bundle, f, indent=2)
print(f'    Bundle  : {out_path}')
PYEOF

# ── Copy outputs ───────────────────────────────────────────────────────────
cp "${WORK}/ca-cert.pem"          "${OUTPUT_DIR}/ca-cert.pem"
cp "${WORK}/wfm-svid-cert.pem"    "${OUTPUT_DIR}/wfm-svid-cert.pem"
cp "${WORK}/wfm-svid-key.pem"     "${OUTPUT_DIR}/wfm-svid-key.pem"
cp "${WORK}/device-svid-cert.pem" "${OUTPUT_DIR}/device-svid-cert.pem"
cp "${WORK}/device-svid-key.pem"  "${OUTPUT_DIR}/device-svid-key.pem"
cp "${WORK}/trust-bundle.json"    "${OUTPUT_DIR}/trust-bundle.json"

# ── CTT auto-install (when called from ctt-start.sh) ─────────────────────
if [[ -n "${CTT_DIR}" ]]; then
    CTT_MIAF_REAL="${CTT_DIR}/wfm-supplier/utils/fixtures/miaf/real"
    mkdir -p "${CTT_MIAF_REAL}"
    cp "${WORK}/device-svid-cert.pem" "${CTT_MIAF_REAL}/client-svid-cert.pem"
    cp "${WORK}/device-svid-key.pem"  "${CTT_MIAF_REAL}/client-svid-key.pem"
    cp "${WORK}/ca-cert.pem"          "${CTT_MIAF_REAL}/trust-bundle-ca.pem"
    cp "${WORK}/trust-bundle.json"    "${CTT_MIAF_REAL}/trust-bundle.json"
    echo ""
    echo "  ✓ CTT fixtures installed → ${CTT_MIAF_REAL}/"
fi

# ── trustDetails.txt ───────────────────────────────────────────────────────
cat > "${OUTPUT_DIR}/trustDetails.txt" <<EOF
trustDomain=${TRUST_DOMAIN}
wfmSpiffeId=${WFM_SPIFFE_ID}
deviceSpiffeId=${DEVICE_SPIFFE_ID}
caFile=${OUTPUT_DIR}/ca-cert.pem
trustBundleFile=${OUTPUT_DIR}/trust-bundle.json
EOF

# ── Summary ────────────────────────────────────────────────────────────────
echo ""
echo "════════════════════════════════════════════════════════════════"
echo "  Done. Files written to: ${OUTPUT_DIR}/"
echo ""
echo "  ca-cert.pem           CA / trust anchor (PEM)"
echo "  wfm-svid-cert.pem     WFM server identity"
echo "  wfm-svid-key.pem      WFM server key"
echo "  device-svid-cert.pem  Device/WFM-client identity"
echo "  device-svid-key.pem   Device/WFM-client key"
echo "  trust-bundle.json     SPIFFE JWKS trust bundle"
echo "  trustDetails.txt      Trust domain + SPIFFE IDs"
echo ""
echo "  ── For Device Supplier testing (real device → CTT mock WFM) ─"
echo "  Note: CTT's mock WFM reads its identity from device-supplier/certs/"
echo "  (folder named after the persona under test, not CTT's role)."
echo "  1. Install CTT mock WFM identity:"
echo "     cp ${OUTPUT_DIR}/wfm-svid-cert.pem  ctt-runner/device-supplier/certs/server-cert.pem"
echo "     cp ${OUTPUT_DIR}/wfm-svid-key.pem   ctt-runner/device-supplier/certs/server-key.pem"
echo "     cp ${OUTPUT_DIR}/ca-cert.pem         ctt-runner/device-supplier/certs/svid-ca.pem"
echo "  2. Give the real device-agent:"
echo "     - ca-cert.pem                        (so device trusts CTT mock WFM server cert)"
echo "     - device-svid-cert.pem + device-svid-key.pem  (device's own mTLS client identity)"
echo ""
echo "  ── For WFM Supplier testing (CTT mock device → real/mock WFM) ─"
echo "  1. Install CTT's device identity (auto-done if run via ctt-start.sh menu):"
echo "     cp ${OUTPUT_DIR}/device-svid-cert.pem  ctt-runner/wfm-supplier/utils/fixtures/miaf/real/client-svid-cert.pem"
echo "     cp ${OUTPUT_DIR}/device-svid-key.pem   ctt-runner/wfm-supplier/utils/fixtures/miaf/real/client-svid-key.pem"
echo "  2. trust-bundle-ca.pem — CA that CTT uses to verify the WFM's server cert:"
echo "     a) Mock WFM / self-contained:"
echo "        cp ${OUTPUT_DIR}/ca-cert.pem  ctt-runner/wfm-supplier/utils/fixtures/miaf/real/trust-bundle-ca.pem"
echo "     b) Real WFM (e.g. Symphony) — fetch Symphony's MIS CA instead:"
echo '        curl -sk https://mis.margo.org:9443/.well-known/spiffe/bundle.json \'
echo '          | python3 -c "import sys,json,base64; [print('"'"'-----BEGIN CERTIFICATE-----\n'"'"'+base64.encodebytes(base64.b64decode(k['"'"'x5c'"'"'][0])).decode()+'"'"'-----END CERTIFICATE-----'"'"') for k in json.load(sys.stdin)['"'"'keys'"'"']]" \'
echo '          > ctt-runner/wfm-supplier/utils/fixtures/miaf/real/trust-bundle-ca.pem'
echo "  3. Import ca-cert.pem into your WFM's trusted-CA store"
echo "     (so the WFM trusts CTT's device cert during mTLS)."
echo "════════════════════════════════════════════════════════════════"
echo ""

# ── Quick verify ───────────────────────────────────────────────────────────
echo "  Verifying chains..."
if openssl verify -CAfile "${OUTPUT_DIR}/ca-cert.pem" \
        "${OUTPUT_DIR}/wfm-svid-cert.pem" >/dev/null 2>&1; then
    echo "  ✓ wfm-svid-cert.pem   → valid"
else
    echo "  ✗ wfm-svid-cert.pem   → CHAIN ERROR"
fi
if openssl verify -CAfile "${OUTPUT_DIR}/ca-cert.pem" \
        "${OUTPUT_DIR}/device-svid-cert.pem" >/dev/null 2>&1; then
    echo "  ✓ device-svid-cert.pem → valid"
else
    echo "  ✗ device-svid-cert.pem → CHAIN ERROR"
fi
echo ""
