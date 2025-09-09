#!/bin/bash

echo "Waiting 90 seconds for Keycloak to start..."
sleep 90

# Authenticate and disable SSL enforcement
/opt/keycloak/bin/kcadm.sh config credentials \
  --server http://localhost:8080 \
  --realm master \
  --user admin \
  --password admin

/opt/keycloak/bin/kcadm.sh update realms/master -s sslRequired=NONE

# Start Keycloak normally
exec /opt/keycloak/bin/kc.sh start-dev