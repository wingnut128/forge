#!/usr/bin/env bash
# POST a token to forge serve /validate and assert valid:true with the
# expected remote trust domain. Usage: validate.sh <forge-serve-url> <token> <expected-td>
set -euo pipefail

URL="$1"
TOKEN="$2"
EXPECT_TD="$3"

resp="$(curl -sS -X POST "$URL/validate" \
  -H 'Content-Type: application/json' \
  -d "{\"token\":\"${TOKEN}\"}")"

echo "validate response: $resp"

echo "$resp" | jq -e '.valid == true' >/dev/null || { echo "FAIL: token not valid"; exit 1; }
echo "$resp" | jq -e --arg td "$EXPECT_TD" '.trust_domain == $td' >/dev/null || {
  echo "FAIL: trust domain mismatch (want ${EXPECT_TD})"; exit 1; }

echo "PASS: cross-cloud SVID validated (remote td=${EXPECT_TD})"
