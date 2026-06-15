#!/usr/bin/env bash
# Bring up the local cross-cloud federation demo on Apple `container`
# (default) or Docker (DEMO_RUNTIME=docker), then run bootstrap.sh.
set -euo pipefail

RT="${DEMO_RUNTIME:-container}"   # container | docker
NET=forge-demo
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GEN="$ROOT/demo/generated"
CERTS="$ROOT/demo/certs"
SPIRE_SERVER_IMG=ghcr.io/spiffe/spire-server:1.11.2
SPIRE_AGENT_IMG=ghcr.io/spiffe/spire-agent:1.11.2

cd "$ROOT"

if [ "$RT" = "docker" ]; then
  echo "==> using Docker Compose fallback"
  go run ./demo/gen "$GEN"
  ./demo/gen-certs.sh "$CERTS"
  CGO_ENABLED=0 GOOS=linux GOARCH="$(go env GOARCH)" \
    go build -o demo/generated/forge ./cmd/forge
  docker compose -f demo/docker-compose.yml up -d
  EXEC="docker exec" bash demo/bootstrap.sh
  exit $?
fi

echo "==> rendering configs + certs"
go run ./demo/gen "$GEN"
./demo/gen-certs.sh "$CERTS"

echo "==> building forge linux binary"
CGO_ENABLED=0 GOOS=linux GOARCH="$(go env GOARCH)" \
  go build -o demo/generated/forge ./cmd/forge

echo "==> (re)creating network $NET"
container network rm "$NET" >/dev/null 2>&1 || true
container network create "$NET"

run_srv() { # name conf cert key
  container run -d --name "$1" --network "$NET" \
    -v "$GEN/$2:/etc/spire/server.conf:ro" \
    -v "$CERTS/$3:/etc/spire/certs/server.crt:ro" \
    -v "$CERTS/$4:/etc/spire/certs/server.key:ro" \
    -v "$CERTS/ca.crt:/etc/spire/certs/ca.crt:ro" \
    -e SSL_CERT_FILE=/etc/spire/certs/ca.crt \
    "$SPIRE_SERVER_IMG" run -config /etc/spire/server.conf
}
run_agent() { # name conf
  container run -d --name "$1" --network "$NET" \
    -v "$GEN/$2:/etc/spire/agent.conf:ro" \
    -v "$CERTS/ca.crt:/etc/spire/certs/ca.crt:ro" \
    -e SSL_CERT_FILE=/etc/spire/certs/ca.crt \
    --entrypoint sleep "$SPIRE_AGENT_IMG" infinity
}

echo "==> starting SPIRE servers + agents"
run_srv spire-gcp-server server-gcp.conf spire-gcp-server.crt spire-gcp-server.key
run_srv spire-aws-server server-aws.conf spire-aws-server.crt spire-aws-server.key
run_agent spire-gcp-agent agent-gcp.conf
run_agent spire-aws-agent agent-aws.conf

echo "==> starting forge serve (AWS role)"
container run -d --name forge-serve --network "$NET" -p 8080:8080 \
  -v "$GEN/forge:/usr/local/bin/forge:ro" \
  -v "$CERTS/ca.crt:/etc/spire/certs/ca.crt:ro" \
  -e SSL_CERT_FILE=/etc/spire/certs/ca.crt \
  -e FORGE_LOCAL_TRUST_DOMAIN=forge.aws.local \
  -e FORGE_REMOTE_TRUST_DOMAIN=forge.gcp.local \
  -e FORGE_BUNDLE_ENDPOINT_URL=https://spire-gcp-server:8443 \
  -e FORGE_LISTEN_ADDR=:8080 \
  alpine /usr/local/bin/forge serve

echo "==> running bootstrap"
EXEC="container exec" bash demo/bootstrap.sh
