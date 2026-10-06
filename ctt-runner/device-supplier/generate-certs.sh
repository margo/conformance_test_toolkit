#!/bin/bash
# Generates TLS and SVID certificates for the CTT device-supplier mock WFM server.
# Usage: bash generate-certs.sh <cert-dir> <server-host>
#   cert-dir    : output directory (created if absent)
#   server-host : hostname or IP for the mock WFM server TLS cert SAN (default: localhost)

set -euo pipefail

CERT_DIR="${1:-./certs}"
SERVER_HOST="${2:-localhost}"
DAYS=825   # ~2 years, stays valid across demo cycles

mkdir -p "$CERT_DIR"
cd "$CERT_DIR"

echo "🔐 Generating CTT device-supplier certificates in $CERT_DIR (server: $SERVER_HOST)..."

# ── CA ────────────────────────────────────────────────────────────────────────
openssl genrsa -out ca-key.pem 2048 2>/dev/null
openssl req -new -x509 -days $DAYS \
    -key ca-key.pem -out ca-cert.pem \
    -subj "/C=IN/ST=GGN/L=Sector48/O=Margo/OU=WFM/CN=Mock-WFM-CA" 2>/dev/null

# svid-ca.pem is the trust bundle the mock WFM uses to validate device mTLS client
# certs. Starts as the local Mock-WFM-CA only; sandbox integration appends the
# real MIS CA (CN=margo.org) so both CTT self-test certs and real device SVIDs
# are trusted. Do not overwrite svid-ca.pem in place during regeneration if it
# already contains a bundle — replace only the first block.
cp ca-cert.pem svid-ca.pem

# ── Server cert (signed by CA, with SAN for the mock WFM host) ────────────────
# Detect whether server_host is an IP or a hostname
if [[ "$SERVER_HOST" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    SAN_LINE="subjectAltName=DNS:localhost,IP:127.0.0.1,IP:$SERVER_HOST"
else
    SAN_LINE="subjectAltName=DNS:localhost,DNS:$SERVER_HOST,IP:127.0.0.1"
fi

openssl genrsa -out server-key.pem 2048 2>/dev/null
openssl req -new \
    -key server-key.pem \
    -out server.csr \
    -subj "/C=IN/ST=GGN/L=Sector48/O=Margo/OU=WFM/CN=$SERVER_HOST" 2>/dev/null
openssl x509 -req -days $DAYS \
    -in server.csr -CA ca-cert.pem -CAkey ca-key.pem -CAcreateserial \
    -out server-cert.pem \
    -extfile <(echo "$SAN_LINE") 2>/dev/null
rm -f server.csr

# ── Device cert — EC P-256, self-signed (RFC 9421 signing identity) ───────────
openssl ecparam -name prime256v1 -genkey -noout -out device-key.pem 2>/dev/null
openssl req -new -x509 -days $DAYS \
    -key device-key.pem -out device-cert.pem \
    -subj "/C=IN/ST=GGN/L=Sector48/O=AcmeCorp/OU=Devices/CN=device-001" 2>/dev/null

# ── Device cert — EC P-384 (for MI-012 algorithm test) ───────────────────────
openssl ecparam -name secp384r1 -genkey -noout -out device-ec384-key.pem 2>/dev/null
openssl req -new -x509 -days $DAYS \
    -key device-ec384-key.pem -out device-ec384-cert.pem \
    -subj "/C=IN/ST=GGN/L=Sector48/O=AcmeCorp/OU=Devices/CN=device-ec384" 2>/dev/null

# ── Device cert — RSA 2048 (for MI-014 rsa-v1_5-sha256 test) ─────────────────
openssl genrsa -out device-rsa2048-key.pem 2048 2>/dev/null
openssl req -new -x509 -days $DAYS \
    -key device-rsa2048-key.pem -out device-rsa2048-cert.pem \
    -subj "/C=IN/ST=GGN/L=Sector48/O=AcmeCorp/OU=Devices/CN=device-rsa2048" 2>/dev/null

# ── Device cert — RSA 1024 weak key (for negative-test scenario) ─────────────
openssl genrsa -out device-weakkey-key.pem 1024 2>/dev/null
openssl req -new -x509 -days $DAYS \
    -key device-weakkey-key.pem -out device-weakkey-cert.pem \
    -subj "/C=IN/ST=GGN/L=Sector48/O=AcmeCorp/OU=Devices/CN=device-weak" 2>/dev/null

# ── SVID cert (X.509-SVID with SPIFFE URI SAN, signed by CA) ─────────────────
# The CTT test runner presents this cert for mTLS connections to the WFM's MIAF
# port. SPIFFE ID follows the WFM-client format required by MIAF rc.2+:
#   spiffe://<trust-domain>/margo/wfm/<wfm-id>/client/<client-id>
# (a device-agent is identified as a WFM-client, not as a bare device URI)
openssl genrsa -out svid-key.pem 2048 2>/dev/null
openssl req -new \
    -key svid-key.pem \
    -out svid.csr \
    -subj "/CN=ctt-device-001/O=Margo CTT" 2>/dev/null
openssl x509 -req -days $DAYS \
    -in svid.csr -CA ca-cert.pem -CAkey ca-key.pem -CAcreateserial \
    -out svid-cert.pem \
    -extfile <(printf "subjectAltName=URI:spiffe://margo.org/margo/wfm/ctt-mock-wfm/client/ctt-device-001\nbasicConstraints=CA:FALSE\nextendedKeyUsage=clientAuth") 2>/dev/null
rm -f svid.csr

# ── Untrusted server cert (self-signed, different CA — for MI-018 TLS test) ──
openssl genrsa -out untrusted-server-key.pem 2048 2>/dev/null
openssl req -new -x509 -days $DAYS \
    -key untrusted-server-key.pem -out untrusted-server-cert.pem \
    -subj "/C=US/ST=State/L=City/O=NotMargo/OU=WFM/CN=localhost" \
    -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" 2>/dev/null

echo ""
echo "✅ Certificates generated:"
echo "   CA:               $CERT_DIR/ca-cert.pem"
echo "   Server (WFM TLS): $CERT_DIR/server-cert.pem  (SAN: $SERVER_HOST)"
echo "   Device (P-256):   $CERT_DIR/device-cert.pem"
echo "   SVID:             $CERT_DIR/svid-cert.pem  (spiffe://margo.org/margo/wfm/ctt-mock-wfm/client/ctt-device-001)"
echo "   Untrusted:        $CERT_DIR/untrusted-server-cert.pem"
echo ""
echo "  ➜  Share ca-cert.pem with the real device-agent so it trusts this mock WFM."
