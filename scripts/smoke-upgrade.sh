#!/usr/bin/env bash
# Upgrade-path smoke test for the host: an operator running the PREVIOUS
# release (its binary, its config file, its ./.defra layout, its env var
# names) starts the NEW binary on the same directory and keeps their identity.
#
#  1. anvil + a generator (new binary) produce blocks with USDC Transfer logs.
#  2. The OLD host runs the way it shipped: ./config/config.yaml + ./.defra,
#     keyring secret in the config, peers as ip:port. It attests some blocks.
#  3. kill -9 the old host. Start the NEW host in the same directory with the
#     OLD config file untouched and the OLD env var name (DEFRA_KEYRING_SECRET).
#  4. Assert: same peer ID, healthy, attestations continue, state stayed in
#     ./.defra (nothing was created under ~/.shinzo).
#
# Usage: scripts/smoke-upgrade.sh <old-host-binary> <new-host-binary> <generator-binary>
#   run from the NEW host repo root; the old binary's repo must be at <old-binary>/../..
set -euo pipefail

OLD_BIN="${1:?usage: smoke-upgrade.sh <old-host-binary> <new-host-binary> <generator-binary>}"
NEW_BIN="${2:?}"; GEN_BIN="${3:?}"
REPO_ROOT="$(pwd)"
ANVIL_PORT="${ANVIL_PORT:-8545}"
RPC="http://127.0.0.1:$ANVIL_PORT"
WORKDIR="$(mktemp -d)"
GEN_LOG="$WORKDIR/generator.log"; OLD_LOG="$WORKDIR/host-old.log"; NEW_LOG="$WORKDIR/host-new.log"

GEN_HTTP=8084; GEN_DEFRA=9185; GEN_P2P=9175
HOST_HTTP=8082; HOST_DEFRA=9183; HOST_P2P=9173

USDC=0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48
KEY0=0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80
ADDR1=0x70997970C51812dc3A010C7d01b50e0d17dc79C8

abs() { case "$1" in /*) echo "$1";; *) echo "$REPO_ROOT/$1";; esac; }
OLD_BIN="$(abs "$OLD_BIN")"; NEW_BIN="$(abs "$NEW_BIN")"; GEN_BIN="$(abs "$GEN_BIN")"
OLD_REPO="$(dirname "$(dirname "$OLD_BIN")")"

for tool in anvil cast forge curl python3; do command -v "$tool" >/dev/null || { echo "FAIL(preflight): '$tool' not on PATH"; exit 1; }; done
for b in "$OLD_BIN" "$NEW_BIN" "$GEN_BIN"; do [ -x "$b" ] || { echo "FAIL(preflight): not executable: $b"; exit 1; }; done
[ -f "$OLD_REPO/config/config.yaml" ] || { echo "FAIL(preflight): old repo config not found at $OLD_REPO/config/config.yaml"; exit 1; }

cleanup() { kill "${HOST_PID:-}" "${GEN_PID:-}" "${TX_PID:-}" "${ANVIL_PID:-}" 2>/dev/null || true; }
trap cleanup EXIT

# ---- chain -------------------------------------------------------------------------
anvil --block-time 1 --port "$ANVIL_PORT" >"$WORKDIR/anvil.log" 2>&1 &
ANVIL_PID=$!
for _ in $(seq 1 30); do curl -sf -X POST -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' "$RPC" >/dev/null 2>&1 && break; sleep 1; done
BYTECODE=$(cd "$REPO_ROOT/scripts/smoke/token" && forge build --silent && forge inspect Token deployedBytecode)
cast rpc --rpc-url "$RPC" anvil_setCode "$USDC" "$BYTECODE" >/dev/null
( while true; do cast send "$USDC" "transfer(address,uint256)" "$ADDR1" 1 --private-key "$KEY0" --rpc-url "$RPC" >/dev/null 2>&1 || true; sleep 0.3; done ) &
TX_PID=$!

# ---- generator (new binary, shifted ports) --------------------------------------------
GEN_DIR="$WORKDIR/generator"; mkdir -p "$GEN_DIR"
cp -r "$(dirname "$(dirname "$GEN_BIN")")/config" "$GEN_DIR/config"
( cd "$GEN_DIR" && exec env GETH_RPC_URL="$RPC" GETH_WS_URL="ws://127.0.0.1:$ANVIL_PORT" GETH_API_KEY= GETH_API_KEY_TYPE= \
    SCHEMA_AUTH_MODE=none SHINZO_KEY_PASSPHRASE=smoke-gen DEFRADB_KEYRING_SECRET=smoke-gen SHINZO_DATA_DIR="$GEN_DIR/data" LOG_LEVEL=info \
    DEFRADB_URL="http://localhost:$GEN_DEFRA" DEFRADB_P2P_LISTEN_ADDR="/ip4/0.0.0.0/tcp/$GEN_P2P" INDEXER_HEALTH_SERVER_PORT="$GEN_HTTP" \
    "$GEN_BIN" >>"$GEN_LOG" 2>&1 ) &
GEN_PID=$!
for _ in $(seq 1 60); do
  [ "$(grep -c 'Committed block' "$GEN_LOG" 2>/dev/null || true)" -ge 3 ] && curl -sf -m 3 "http://127.0.0.1:$GEN_HTTP/health" >/dev/null && break
  kill -0 "$GEN_PID" 2>/dev/null || { echo "FAIL(generator): exited early"; tail -20 "$GEN_LOG"; exit 1; }; sleep 2
done
GEN_PEER=$(curl -sf -H 'Accept: application/json' "http://127.0.0.1:$GEN_HTTP/registration" | python3 -c 'import json,sys; print(json.load(sys.stdin)["p2p"]["self"]["id"])')
echo "OK(generator): indexing; peer $GEN_PEER"

# ---- the operator's old installation ----------------------------------------------------
OLD_DIR="$WORKDIR/old"; mkdir -p "$OLD_DIR/config"
python3 - "$OLD_REPO/config/config.yaml" "$OLD_DIR/config/config.yaml" "$HOST_DEFRA" "$HOST_P2P" "$HOST_HTTP" "$GEN_P2P" "$GEN_PEER" <<'PY'
import re, sys
src, dst, defra, p2p, http, genp2p, genpeer = sys.argv[1:]
s = open(src).read()
# exactly what shipped, minus anything that would reach the real network
s = re.sub(r'bootstrap_peers:\n(?:[ \t]*(?:#.*|- .*)\n)+', f"bootstrap_peers:\n      - '/ip4/127.0.0.1/tcp/{genp2p}/p2p/{genpeer}'\n", s, count=1)
s = s.replace('url: "localhost:9181"', f'url: "localhost:{defra}"', 1)
s = s.replace('listen_addr: "/ip4/0.0.0.0/tcp/9171"', f'listen_addr: "/ip4/0.0.0.0/tcp/{p2p}"', 1)
s = s.replace('health_server_port: 8080', f'health_server_port: {http}', 1)
s = re.sub(r'^  hub_base_url: .*$', '  hub_base_url: ""', s, flags=re.M)
s = re.sub(r'^\s*indexer_url: .*$', '    indexer_url: ""', s, flags=re.M)
assert './.defra' in s, "old config no longer uses ./.defra — is the old binary really old?"
open(dst, 'w').write(s)
PY

peer_id() { curl -sf -m 3 -H 'Accept: application/json' "http://127.0.0.1:$HOST_HTTP/health" 2>/dev/null | python3 -c 'import json,sys; print((json.load(sys.stdin).get("p2p") or {}).get("self",{}).get("id",""))' 2>/dev/null || true; }
await_attest() { # $1=log $2=min $3=label
  local deadline=$((SECONDS + 180)) n
  while [ "$SECONDS" -lt "$deadline" ]; do
    kill -0 "$HOST_PID" 2>/dev/null || { echo "FAIL($3): host exited early"; tail -30 "$1"; exit 1; }
    n=$(grep -c "Created attestation" "$1" 2>/dev/null || true)
    [ "${n:-0}" -ge "$2" ] && [ -n "$(peer_id)" ] && { echo "OK($3): attestations=$n"; return 0; }
    sleep 3
  done
  echo "FAIL($3): timeout"; tail -30 "$1"; exit 1
}

echo "starting OLD host ($(basename "$OLD_REPO") @ $(git -C "$OLD_REPO" rev-parse --short HEAD 2>/dev/null || echo '?')) on ./.defra ..."
( cd "$OLD_DIR" && exec env HOME="$WORKDIR/home-old" LOG_LEVEL=info "$OLD_BIN" >>"$OLD_LOG" 2>&1 ) &
HOST_PID=$!
await_attest "$OLD_LOG" 2 "old-host"
OLD_PEER=$(peer_id)
[ -d "$OLD_DIR/.defra" ] || { echo "FAIL(old-host): expected state in ./.defra"; ls -la "$OLD_DIR"; exit 1; }
echo "OK(old-host): peer $OLD_PEER, state in ./.defra"
kill -9 "$HOST_PID"; sleep 2

# ---- upgrade: new binary, same directory, old config file, old env var name ---------------
echo "starting NEW host in the same directory ..."
( cd "$OLD_DIR" && exec env HOME="$WORKDIR/home-new" DEFRA_KEYRING_SECRET=pingpong SHINZO_NETWORK=custom LOG_LEVEL=info "$NEW_BIN" run >>"$NEW_LOG" 2>&1 ) &
HOST_PID=$!
await_attest "$NEW_LOG" 2 "new-host"
NEW_PEER=$(peer_id)
[ "$OLD_PEER" = "$NEW_PEER" ] || { echo "FAIL(upgrade): peer id changed ($OLD_PEER -> $NEW_PEER) — identity was not carried over"; exit 1; }
grep -q 'config: ./config/config.yaml' "$NEW_LOG" || { echo "FAIL(upgrade): new binary did not pick up the old config file"; grep -m1 'is running' "$NEW_LOG"; exit 1; }
grep -q "Data         : $OLD_DIR/.defra\|Data         : ./.defra" "$NEW_LOG" || { echo "FAIL(upgrade): new binary did not use ./.defra"; grep 'Data' "$NEW_LOG"; exit 1; }
[ -e "$WORKDIR/home-new/.shinzo" ] && { echo "FAIL(upgrade): new binary created ~/.shinzo despite the old layout"; exit 1; }
grep -q 'Passphrase   : generated' "$NEW_LOG" && { echo "FAIL(upgrade): a new passphrase was generated — old secret ignored"; exit 1; }
echo "PASS(upgrade): same identity $NEW_PEER, old config + ./.defra + DEFRA_KEYRING_SECRET all honoured, attestations continue"
