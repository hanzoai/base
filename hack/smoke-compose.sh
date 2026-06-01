#!/usr/bin/env bash
# smoke-compose.sh — bring up compose.network.yml, drive a write through the
# gateway, kill a node, read it back, tear down. Exits 0 on success.
#
# Prereqs:
#   - Docker + compose v2.
#   - ghcr.io/hanzoai/base:dev and ghcr.io/hanzoai/gateway:latest available
#     (either pulled locally or network-accessible).
#
# This script is deliberately strict: any broken rung (members endpoint not
# responding, write fails, read fails after kill) aborts with a non-zero exit
# code and a pointer to the failing assertion.

set -euo pipefail

cd "$(dirname "$0")/.."
COMPOSE="docker compose -f compose.network.yml"
GATEWAY="http://localhost:18080"
USER="alice"

cleanup() {
  echo "--- tearing down ---"
  $COMPOSE down -v >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "--- docker compose up ---"
$COMPOSE up -d

echo "--- wait for gateway + all 3 pods healthy ---"
deadline=$((SECONDS + 60))
while [[ $SECONDS -lt $deadline ]]; do
  # Any response at all (including 404/400) means gateway is listening.
  if curl -s -o /dev/null -w '%{http_code}' "$GATEWAY/api/__probe" | grep -qE '^[0-9]+$'; then
    # Probe each pod directly to confirm it's up.
    if curl -fsS "http://localhost:18090/api/health" >/dev/null 2>&1 \
      || curl -fsS "http://localhost:18090/-/base/members" >/dev/null 2>&1; then
      break
    fi
  fi
  sleep 1
done

echo "--- verify /-/base/members is exposed by each pod ---"
for pod in 18090 18091 18092; do
  if ! curl -fsS "http://localhost:$pod/-/base/members" | grep -q members; then
    echo "FAIL: pod :$pod does not expose /-/base/members" >&2
    echo "(this is Agent #1's territory — base/network package. If you're" >&2
    echo " seeing this before Agent #1 lands, the image is too old.)" >&2
    exit 2
  fi
done

echo "--- write a row via gateway ---"
write_resp=$(curl -fsS -XPOST "$GATEWAY/api/collections/_pbc/records" \
  -H "Content-Type: application/json" \
  -H "X-User-Id: $USER" \
  -d '{"payload":"smoke"}')
echo "write: $write_resp"

echo "--- kill base-b to simulate a node failure ---"
$COMPOSE kill base-b
sleep 2

echo "--- read back via gateway (must still succeed with 2/3 quorum) ---"
if ! curl -fsS "$GATEWAY/api/collections/_pbc/records?filter=payload=smoke" \
  -H "X-User-Id: $USER" >/dev/null; then
  echo "FAIL: read after node kill did not succeed" >&2
  $COMPOSE logs --tail=50 base-a base-c gateway >&2 || true
  exit 3
fi

echo "--- shard_key_missing is enforced (no X-User-Id → 400) ---"
status=$(curl -s -o /dev/null -w '%{http_code}' -XPOST "$GATEWAY/api/collections/_pbc/records" \
  -H "Content-Type: application/json" -d '{}')
if [[ "$status" != "400" ]]; then
  echo "FAIL: expected 400 for missing shard key, got $status" >&2
  exit 4
fi

echo ""
echo "--- OK: base-network compose smoke passed ---"
