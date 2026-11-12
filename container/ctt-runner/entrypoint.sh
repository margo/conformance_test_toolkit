#!/usr/bin/env bash
set -euo pipefail

CERTS=/ctt-data/zot/certs
REGISTRY=127.0.0.1:5443
PACKAGE=$REGISTRY/library/margo-ctt-hello-world:1.0.0
HOST=${CTT_REGISTRY_HOST:-localhost}
if [[ $HOST =~ ^[0-9.]+$ ]]; then SAN="DNS:localhost,IP:127.0.0.1,IP:$HOST"; else SAN="DNS:localhost,IP:127.0.0.1,DNS:$HOST"; fi

log() { echo "[ctt-entrypoint] $*"; }

mkdir -p "$CERTS" /ctt-data/zot/storage /ctt-data/input /ctt-data/trusted-ca

if [[ ! -f $CERTS/ca.crt ]]; then
    openssl req -x509 -newkey rsa:4096 -nodes -days 3650 -subj "/CN=Margo CTT Registry CA" \
        -keyout "$CERTS/ca.key" -out "$CERTS/ca.crt" 2>/dev/null
fi

if [[ "$(cat "$CERTS/server.san" 2>/dev/null)" != "$SAN" ]]; then
    openssl req -newkey rsa:4096 -nodes -subj "/CN=$HOST" -keyout "$CERTS/server.key" -out "$CERTS/server.csr" 2>/dev/null
    openssl x509 -req -in "$CERTS/server.csr" -CA "$CERTS/ca.crt" -CAkey "$CERTS/ca.key" -CAcreateserial -days 365 \
        -extfile <(echo "subjectAltName=$SAN") -out "$CERTS/server.crt" 2>/dev/null
    cat "$CERTS/server.crt" "$CERTS/ca.crt" > "$CERTS/server-chain.crt"
    echo "$SAN" > "$CERTS/server.san"
    log "Registry certificate created for $SAN"
fi

htpasswd -bnB ctt margo-ctt > /ctt-data/zot/htpasswd

cp "$CERTS/ca.crt" /usr/local/share/ca-certificates/ctt-registry-ca.crt
cp /ctt-data/trusted-ca/*.crt /usr/local/share/ca-certificates/ 2>/dev/null || true
update-ca-certificates >/dev/null 2>&1

zot serve /etc/zot/config.json &
ZOT_PID=$!
trap 'kill $ZOT_PID' TERM INT

until curl -s -o /dev/null "https://$REGISTRY/v2/"; do kill -0 $ZOT_PID; sleep 1; done
echo margo-ctt | oras login $REGISTRY -u ctt --password-stdin >/dev/null
oras manifest fetch $PACKAGE >/dev/null 2>&1 \
    || bash /ctt/ctt-runner/wfm-supplier/utils/fixtures/margo-ctt-package/push.sh $PACKAGE >/dev/null

log "Ready: registry $REGISTRY, CA for other hosts in $CERTS/ca.crt"
wait $ZOT_PID
