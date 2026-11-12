#!/usr/bin/env bash
# Entrypoint of the CTT runner container.
#
#   1. Creates the CTT registry's TLS certificate and login (once, kept in /ctt-data)
#   2. Starts the CTT's own OCI registry (Zot) on port 5443
#   3. Trusts it and logs in to it, so ORAS and the CTT scripts can use it
#   4. Pushes the CTT reference Application Package to it (if not there yet)
#   5. Keeps running as long as Zot runs
#
# The CTT itself is started inside the running container:
#   docker compose exec ctt-runner bash ctt-runner/ctt-start.sh
set -euo pipefail

DATA=/ctt-data/zot
CERTS="$DATA/certs"
REGISTRY="127.0.0.1:5443"                       # not "localhost": ORAS uses plain HTTP for localhost
HOST="${CTT_REGISTRY_HOST:-localhost}"          # address clients outside the container use
USERNAME="${CTT_REGISTRY_USER:-ctt}"
PASSWORD="${CTT_REGISTRY_PASSWORD:-margo-ctt}"
PACKAGE="$REGISTRY/library/margo-ctt-hello-world:1.0.0"

log() { echo "[ctt-entrypoint] $*"; }

mkdir -p "$CERTS" "$DATA/storage"

# ── 1. TLS certificate and login ──────────────────────────────────────────────
# The CA is created once. The server certificate is created again when
# CTT_REGISTRY_HOST changes, so it always names the address clients use.
if [[ "$HOST" =~ ^[0-9.]+$ ]]; then HOST_SAN="IP:$HOST"; else HOST_SAN="DNS:$HOST"; fi
SAN="DNS:localhost,IP:127.0.0.1,$HOST_SAN"

if [[ ! -f "$CERTS/ca.crt" ]]; then
    log "Creating the registry CA ($CERTS/ca.crt)"
    openssl req -x509 -newkey rsa:4096 -nodes -sha256 -days 3650 \
        -subj "/CN=Margo CTT Registry CA" \
        -keyout "$CERTS/ca.key" -out "$CERTS/ca.crt" 2>/dev/null
fi

if [[ ! -f "$CERTS/server.crt" ]] || [[ "$(cat "$CERTS/server.san" 2>/dev/null)" != "$SAN" ]]; then
    log "Creating the registry server certificate for: $SAN"
    openssl req -newkey rsa:4096 -nodes -subj "/CN=$HOST" \
        -keyout "$CERTS/server.key" -out "$CERTS/server.csr" 2>/dev/null
    printf 'subjectAltName=%s\nbasicConstraints=CA:FALSE\nkeyUsage=digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth\n' \
        "$SAN" > "$CERTS/server.ext"
    openssl x509 -req -in "$CERTS/server.csr" -CA "$CERTS/ca.crt" -CAkey "$CERTS/ca.key" \
        -CAcreateserial -days 365 -sha256 -extfile "$CERTS/server.ext" -out "$CERTS/server.crt" 2>/dev/null
    cat "$CERTS/server.crt" "$CERTS/ca.crt" > "$CERTS/server-chain.crt"
    echo "$SAN" > "$CERTS/server.san"
    rm -f "$CERTS/server.csr" "$CERTS/server.ext"
fi

htpasswd -bnB "$USERNAME" "$PASSWORD" > "$DATA/htpasswd"

# The container trusts its own registry (ORAS, Go and curl use the system store),
# plus every CA you put in /ctt-data/trusted-ca/*.crt (e.g. the CA of a vendor's
# registry), because a container does not see the host's trust store.
mkdir -p /ctt-data/trusted-ca
cp "$CERTS/ca.crt" /usr/local/share/ca-certificates/ctt-registry-ca.crt
for ca in /ctt-data/trusted-ca/*.crt; do
    [[ -f "$ca" ]] && cp "$ca" "/usr/local/share/ca-certificates/trusted-$(basename "$ca")" && log "Trusting CA: $ca"
done
update-ca-certificates >/dev/null 2>&1

# ── 2. Start Zot ──────────────────────────────────────────────────────────────
zot verify /etc/zot/config.json >/dev/null
zot serve /etc/zot/config.json &
ZOT_PID=$!
trap 'kill "$ZOT_PID" 2>/dev/null' TERM INT

for _ in $(seq 1 30); do
    code=$(curl -s -o /dev/null -w '%{http_code}' "https://$REGISTRY/v2/" || true)
    [[ "$code" == "401" ]] && break
    sleep 1
done
[[ "$code" == "401" ]] || { log "Zot did not start (last HTTP code: $code)"; exit 1; }
log "Zot is running on port 5443"

# ── 3. Log in (credentials go to ~/.docker/config.json, read by the CTT) ─────
echo "$PASSWORD" | oras login "$REGISTRY" -u "$USERNAME" --password-stdin >/dev/null
log "Logged in to $REGISTRY as $USERNAME"

# ── 4. CTT reference Application Package ─────────────────────────────────────
if oras manifest fetch "$PACKAGE" >/dev/null 2>&1; then
    log "Reference package already present: $PACKAGE"
else
    bash /ctt/ctt-runner/wfm-supplier/utils/fixtures/margo-ctt-package/push.sh "$PACKAGE" >/dev/null
    log "Pushed reference package: $PACKAGE"
fi

log "Ready. Registry CA for clients outside the container: $CERTS/ca.crt"
log "Start the CTT with: docker compose exec ctt-runner bash ctt-runner/ctt-start.sh"

# ── 5. Run as long as Zot runs ───────────────────────────────────────────────
wait "$ZOT_PID"
