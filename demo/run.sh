#!/usr/bin/env bash
# Bring up the local cross-cloud federation demo on Apple `container`
# (default) or Docker (DEMO_RUNTIME=docker), then run bootstrap.sh.
#
# run.sh starts the two SPIRE servers (everything else depends on them) and
# hands bootstrap.sh the runtime-specific run-commands for the GCP agent and
# forge-serve. bootstrap.sh launches those at the right moments — the agent
# needs a join token first, and forge-serve must not start until the GCP
# bundle endpoint is serving (its initial bundle fetch is fatal on failure).
set -euo pipefail

RT="${DEMO_RUNTIME:-container}"   # container | docker
NET=forge-demo
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GEN="$ROOT/demo/generated"
CERTS="$ROOT/demo/certs"
SPIRE_SERVER_IMG=ghcr.io/spiffe/spire-server:1.11.2
SPIRE_AGENT_IMG=ghcr.io/spiffe/spire-agent:1.11.2

cd "$ROOT"

echo "==> rendering configs + certs"
go run ./demo/gen "$GEN"
./demo/gen-certs.sh "$CERTS"

echo "==> building forge linux binary"
CGO_ENABLED=0 GOOS=linux GOARCH="$(go env GOARCH)" \
  go build -o demo/generated/forge ./cmd/forge

# RUN = runtime's detached-run command; EXEC = runtime's exec command.
if [ "$RT" = "docker" ]; then
  RUN="docker run"
  export EXEC="docker exec"
  docker network rm "$NET" >/dev/null 2>&1 || true
  docker network create "$NET" >/dev/null
else
  RUN="container run"
  export EXEC="container exec"
  container network rm "$NET" >/dev/null 2>&1 || true
  container network create "$NET"
fi
echo "==> network $NET ready ($RT runtime)"

# The SPIRE images' ENTRYPOINT is ["/opt/spire/bin/spire-<role>", "run"], so the
# command we append must NOT repeat "run" — just the flags.
run_srv() { # name conf cert key
  $RUN -d --name "$1" --network "$NET" \
    -v "$GEN/$2:/etc/spire/server.conf:ro" \
    -v "$CERTS/$3:/etc/spire/certs/server.crt:ro" \
    -v "$CERTS/$4:/etc/spire/certs/server.key:ro" \
    -v "$CERTS/ca.crt:/etc/spire/certs/ca.crt:ro" \
    -e SSL_CERT_FILE=/etc/spire/certs/ca.crt \
    "$SPIRE_SERVER_IMG" -config /etc/spire/server.conf
}

echo "==> starting SPIRE servers"
run_srv spire-gcp-server server-gcp.conf spire-gcp-server.crt spire-gcp-server.key
run_srv spire-aws-server server-aws.conf spire-aws-server.crt spire-aws-server.key

# Apple `container` has no built-in name DNS, so resolve the servers' assigned
# IPs and hand the agent + forge-serve a generated /etc/hosts. Docker provides
# name DNS on a user-defined network, so the hosts mount is container-only.
HOSTS_MOUNT=""
if [ "$RT" = "container" ]; then
  # inspect emits "ipv4Address" : "192.168.65.2\/24" — capture the dotted quad,
  # stopping before the escaped slash.
  ip_of() { container inspect "$1" 2>/dev/null | sed -n 's/.*"ipv4Address"[^0-9]*\([0-9][0-9.]*\).*/\1/p' | head -1; }
  gcp_ip() { ip_of spire-gcp-server; }
  aws_ip() { ip_of spire-aws-server; }
  echo "==> resolving server IPs"
  for _ in $(seq 1 15); do
    GCP_IP="$(gcp_ip)"; AWS_IP="$(aws_ip)"
    [ -n "$GCP_IP" ] && [ -n "$AWS_IP" ] && break
    sleep 1
  done
  [ -n "$GCP_IP" ] && [ -n "$AWS_IP" ] || { echo "FAIL: could not resolve server IPs"; exit 1; }
  printf '127.0.0.1 localhost\n%s spire-gcp-server\n%s spire-aws-server\n' "$GCP_IP" "$AWS_IP" > "$GEN/hosts"
  echo "  spire-gcp-server=$GCP_IP  spire-aws-server=$AWS_IP"
  HOSTS_MOUNT="-v $GEN/hosts:/etc/hosts:ro"
fi

# Run-commands bootstrap.sh fires later (word-split on use, like $EXEC). Paths
# here contain no spaces. bootstrap appends the trailing spire-<role> flags.
export RUN_AGENT="$RUN -d --name spire-gcp-agent --network $NET $HOSTS_MOUNT \
  -v $GEN/agent-gcp.conf:/etc/spire/agent.conf:ro \
  -v $CERTS/ca.crt:/etc/spire/certs/ca.crt:ro \
  -e SSL_CERT_FILE=/etc/spire/certs/ca.crt $SPIRE_AGENT_IMG"

export RUN_FORGE="$RUN -d --name forge-serve --network $NET -p 8080:8080 $HOSTS_MOUNT \
  -v $GEN/forge:/usr/local/bin/forge:ro \
  -v $CERTS/ca.crt:/etc/spire/certs/ca.crt:ro \
  -e SSL_CERT_FILE=/etc/spire/certs/ca.crt \
  -e FORGE_LOCAL_TRUST_DOMAIN=forge.aws.local \
  -e FORGE_REMOTE_TRUST_DOMAIN=forge.gcp.local \
  -e FORGE_BUNDLE_ENDPOINT_URL=https://spire-gcp-server:8443 \
  -e FORGE_LISTEN_ADDR=:8080 \
  --entrypoint /usr/local/bin/forge alpine serve"

echo "==> running bootstrap"
bash demo/bootstrap.sh
