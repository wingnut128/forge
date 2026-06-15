#!/usr/bin/env bash
# Generate a throwaway demo CA and https_web serving certs for the SPIRE
# bundle endpoints. Output: demo/certs/{ca.crt,ca.key,<host>.crt,<host>.key}.
set -euo pipefail

CERT_DIR="${1:-demo/certs}"
mkdir -p "$CERT_DIR"
cd "$CERT_DIR"

HOSTS=(spire-gcp-server spire-aws-server)

if [ ! -f ca.crt ]; then
  openssl genrsa -out ca.key 2048
  openssl req -x509 -new -nodes -key ca.key -sha256 -days 7 \
    -subj "/CN=forge-demo-ca" -out ca.crt
fi

for host in "${HOSTS[@]}"; do
  [ -f "${host}.crt" ] && continue
  openssl genrsa -out "${host}.key" 2048
  openssl req -new -key "${host}.key" -subj "/CN=${host}" -out "${host}.csr"
  cat > "${host}.ext" <<EXT
subjectAltName = DNS:${host}
extendedKeyUsage = serverAuth
EXT
  openssl x509 -req -in "${host}.csr" -CA ca.crt -CAkey ca.key \
    -CAcreateserial -out "${host}.crt" -days 7 -sha256 \
    -extfile "${host}.ext"
  rm -f "${host}.csr" "${host}.ext"
  echo "issued ${host}.crt"
done

echo "demo CA + serving certs ready in $CERT_DIR"
