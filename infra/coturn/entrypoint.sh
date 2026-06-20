#!/bin/sh
# Phaze coturn entrypoint. Reads:
#   TURN_SECRET    HMAC secret used for time-limited credentials
#                  (must match PHAZE_TURN_SECRET on the nexus app)
#   TURN_REALM     e.g. turn.phazechat.world (just labels, not DNS)
#   PUBLIC_IP      public IPv4 Fly hands out (export via fly ips list)
#                  Allowed to be blank — coturn will autodiscover.
set -eu

: "${TURN_SECRET:?TURN_SECRET env var required}"
TURN_REALM="${TURN_REALM:-phaze.local}"
EXTERNAL_IP="${PUBLIC_IP:-}"

CFG=$(mktemp)
cat > "$CFG" <<EOF
listening-port=3478
tls-listening-port=5349
fingerprint
lt-cred-mech
use-auth-secret
static-auth-secret=${TURN_SECRET}
realm=${TURN_REALM}
min-port=49160
max-port=49161
no-multicast-peers
no-cli
no-tlsv1
no-tlsv1_1
EOF

if [ -n "$EXTERNAL_IP" ]; then
  echo "external-ip=${EXTERNAL_IP}" >> "$CFG"
fi

exec turnserver -c "$CFG" -v
