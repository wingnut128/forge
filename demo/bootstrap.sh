#!/usr/bin/env bash
# Bootstrap cross-cloud SPIRE federation, mint a JWT-SVID on the GCP side,
# and validate it through forge serve (AWS side).
#
# Requires (exported by run.sh):
#   EXEC       - container-exec command   ("container exec" | "docker exec")
#   RUN_AGENT  - run-command that launches the GCP agent (image included);
#                bootstrap appends the spire-agent flags
#   RUN_FORGE  - run-command that launches forge-serve (fully formed)
set -euo pipefail

EXEC="${EXEC:?set EXEC to the container exec command}"
RUN_AGENT="${RUN_AGENT:?set RUN_AGENT to the agent run command}"
RUN_FORGE="${RUN_FORGE:?set RUN_FORGE to the forge-serve run command}"

GCP_SRV=spire-gcp-server
AWS_SRV=spire-aws-server
GCP_AGENT=spire-gcp-agent
GCP_TD=forge.gcp.local
AWS_TD=forge.aws.local
AGENT_ID="spiffe://${GCP_TD}/agent"
WORKLOAD_ID="spiffe://${GCP_TD}/workload/demo"
FORGE_URL="http://localhost:8080"

srv() { $EXEC "$1" /opt/spire/bin/spire-server "${@:2}"; }

echo "==> waiting for SPIRE servers to be healthy"
for s in "$GCP_SRV" "$AWS_SRV"; do
  ok=""
  for _ in $(seq 1 30); do
    if srv "$s" healthcheck >/dev/null 2>&1; then echo "  $s healthy"; ok=1; break; fi
    sleep 2
  done
  [ -n "$ok" ] || { echo "FAIL: $s never became healthy"; srv "$s" healthcheck || true; exit 1; }
done

echo "==> exchanging trust bundles (federation)"
# GCP bundle -> AWS server (which federates_with GCP)
srv "$GCP_SRV" bundle show -format spiffe > /tmp/gcp.bundle
$EXEC -i "$AWS_SRV" /opt/spire/bin/spire-server bundle set \
  -format spiffe -id "spiffe://${GCP_TD}" < /tmp/gcp.bundle
# AWS bundle -> GCP server (which federates_with AWS)
srv "$AWS_SRV" bundle show -format spiffe > /tmp/aws.bundle
$EXEC -i "$GCP_SRV" /opt/spire/bin/spire-server bundle set \
  -format spiffe -id "spiffe://${AWS_TD}" < /tmp/aws.bundle

echo "==> starting forge serve (AWS role) — GCP bundle endpoint is up now"
$RUN_FORGE
for _ in $(seq 1 15); do
  curl -fsS "${FORGE_URL}/healthz" >/dev/null 2>&1 && { echo "  forge serve healthy"; break; }
  sleep 2
done

echo "==> generating agent join token"
JOIN_TOKEN="$(srv "$GCP_SRV" token generate -spiffeID "${AGENT_ID}" \
  | sed -n 's/^Token: *//p' | tr -d '[:space:]')"
[ -n "$JOIN_TOKEN" ] || { echo "FAIL: empty join token"; exit 1; }
echo "  token: ${JOIN_TOKEN}"

echo "==> launching GCP agent with join token"
$RUN_AGENT -config /etc/spire/agent.conf -joinToken "${JOIN_TOKEN}"
# Wait for the agent to attest and expose its Workload API socket.
for _ in $(seq 1 15); do
  $EXEC "$GCP_AGENT" /opt/spire/bin/spire-agent healthcheck -socketPath /tmp/agent.sock >/dev/null 2>&1 \
    && { echo "  agent healthy"; break; }
  sleep 2
done

echo "==> registering demo workload (federated with AWS)"
srv "$GCP_SRV" entry create \
  -parentID "${AGENT_ID}" \
  -spiffeID "${WORKLOAD_ID}" \
  -selector "unix:uid:0" \
  -federatesWith "${AWS_TD}"

echo "==> minting JWT-SVID on GCP (audience = AWS trust domain)"
# Poll: the agent needs a sync cycle to receive the new entry before it can
# issue the SVID ("no identity issued" until then). The JWT-SVID starts "eyJ".
TOKEN=""
for _ in $(seq 1 20); do
  # `|| true` on both stages: until the entry syncs the fetch errors and grep
  # finds nothing — without it, set -e/pipefail would abort instead of retry.
  out="$($EXEC "$GCP_AGENT" /opt/spire/bin/spire-agent api fetch jwt \
    -audience "${AWS_TD}" -spiffeID "${WORKLOAD_ID}" \
    -socketPath /tmp/agent.sock 2>/dev/null || true)"
  TOKEN="$(printf '%s' "$out" | grep -oE 'eyJ[A-Za-z0-9._-]+' | head -1 || true)"
  [ -n "$TOKEN" ] && break
  sleep 2
done
[ -n "$TOKEN" ] || { echo "FAIL: empty JWT-SVID (agent never issued the identity)"; exit 1; }

echo "==> validating the SVID through forge serve (AWS role)"
bash demo/validate.sh "$FORGE_URL" "$TOKEN" "$GCP_TD"
