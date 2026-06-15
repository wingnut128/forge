#!/usr/bin/env bash
# Bootstrap cross-cloud SPIRE federation, mint a JWT-SVID on the GCP side,
# and validate it through forge serve (AWS side).
# Requires: EXEC env var = container-exec command (e.g. "container exec" or "docker exec").
set -euo pipefail

EXEC="${EXEC:?set EXEC to the container exec command}"
GCP_SRV=spire-gcp-server
AWS_SRV=spire-aws-server
GCP_AGENT=spire-gcp-agent
GCP_TD=forge.gcp.local
AWS_TD=forge.aws.local
WORKLOAD_ID="spiffe://${GCP_TD}/workload/demo"
FORGE_URL="http://localhost:8080"

srv() { $EXEC "$1" /opt/spire/bin/spire-server "${@:2}"; }

echo "==> waiting for SPIRE servers to be healthy"
for s in "$GCP_SRV" "$AWS_SRV"; do
  for i in $(seq 1 30); do
    if srv "$s" healthcheck >/dev/null 2>&1; then echo "  $s healthy"; break; fi
    sleep 2
    [ "$i" = 30 ] && { echo "FAIL: $s never became healthy"; exit 1; }
  done
done

echo "==> exchanging trust bundles (federation)"
# GCP bundle -> AWS server (federates_with GCP)
srv "$GCP_SRV" bundle show -format spiffe > /tmp/gcp.bundle
$EXEC -i "$AWS_SRV" /opt/spire/bin/spire-server bundle set \
  -format spiffe -id "spiffe://${GCP_TD}" < /tmp/gcp.bundle
# AWS bundle -> GCP server (federates_with AWS)
srv "$AWS_SRV" bundle show -format spiffe > /tmp/aws.bundle
$EXEC -i "$GCP_SRV" /opt/spire/bin/spire-server bundle set \
  -format spiffe -id "spiffe://${AWS_TD}" < /tmp/aws.bundle

echo "==> creating join token + registering GCP agent"
JOIN_TOKEN="$(srv "$GCP_SRV" token generate -spiffeID "spiffe://${GCP_TD}/agent" \
  | sed -n 's/^Token: //p')"
[ -n "$JOIN_TOKEN" ] || { echo "FAIL: empty join token"; exit 1; }

# Start the agent with the join token (agent container already running, idle).
$EXEC "$GCP_AGENT" sh -c \
  "/opt/spire/bin/spire-agent run -config /etc/spire/agent.conf -joinToken ${JOIN_TOKEN} &
   sleep 5"

echo "==> registering demo workload (federated with AWS)"
srv "$GCP_SRV" entry create \
  -parentID "spiffe://${GCP_TD}/agent" \
  -spiffeID "${WORKLOAD_ID}" \
  -selector "unix:uid:0" \
  -federatesWith "${AWS_TD}"
sleep 5

echo "==> minting JWT-SVID on GCP (audience = AWS trust domain)"
TOKEN="$($EXEC "$GCP_AGENT" /opt/spire/bin/spire-agent api fetch jwt \
  -audience "${AWS_TD}" -spiffeID "${WORKLOAD_ID}" \
  -socketPath /tmp/agent.sock | sed -n '2p' | tr -d '[:space:]')"
[ -n "$TOKEN" ] || { echo "FAIL: empty JWT-SVID"; exit 1; }

echo "==> validating the SVID through forge serve (AWS role)"
bash demo/validate.sh "$FORGE_URL" "$TOKEN" "$GCP_TD"
