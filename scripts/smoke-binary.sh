#!/usr/bin/env bash
# Functional smoke test for the host binary. See scripts/smoke/README.md.
#
#  Phase 1: anvil (blocks with ERC-20 Transfer logs at the USDC address)
#           -> generator binary indexes them
#           -> host binary bootstraps to the generator BY NODE URL, replicates
#              over P2P, creates attestations, and serves the production USDC
#              Lens view (WASM transform executed on wazero, no runtime installed).
#  Phase 2: kill -9 the host, restart on the same data dir + passphrase:
#           identity, peers, attestations and the view must all come back.
#  Phase 3: kill -9 the GENERATOR and restart it: the host must reconnect on
#           its own (auto-reconnect) and keep attesting new blocks.
#  Phase 4: a SECOND host bootstraps from the first host's URL (not the
#           generator): primitives must reach it through host-to-host
#           replication, and it must attest and serve the Lens view too.
#
# Usage: scripts/smoke-binary.sh <host-binary> <generator-binary>
#   run from the host repo root; needs anvil, cast, forge, curl, python3.
#   ANVIL_PORT (default 8545) if something else owns 8545.
set -euo pipefail

HOST_BIN="${1:?usage: smoke-binary.sh <host-binary> <generator-binary>}"
GEN_BIN="${2:?usage: smoke-binary.sh <host-binary> <generator-binary>}"
REPO_ROOT="$(pwd)"
ANVIL_PORT="${ANVIL_PORT:-8545}"
RPC="http://127.0.0.1:$ANVIL_PORT"
WORKDIR="$(mktemp -d)"
GEN_LOG="$WORKDIR/generator.log"
HOST_LOG="$WORKDIR/host.log"

# Both nodes on shifted ports so the test never collides with a running container.
GEN_HTTP=8084
GEN_DEFRA=9185
GEN_P2P=9175
HOST_HTTP=8082
HOST_DEFRA=9183
HOST_P2P=9173
HOST2_HTTP=8086
HOST2_DEFRA=9187
HOST2_P2P=9177

USDC=0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48
KEY0=0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80
ADDR1=0x70997970C51812dc3A010C7d01b50e0d17dc79C8

abs() { case "$1" in /*) echo "$1";; *) echo "$REPO_ROOT/$1";; esac; }
HOST_BIN="$(abs "$HOST_BIN")"; GEN_BIN="$(abs "$GEN_BIN")"

for tool in anvil cast forge curl python3; do
  command -v "$tool" >/dev/null 2>&1 || { echo "FAIL(preflight): '$tool' not on PATH"; exit 1; }
done
[ -x "$HOST_BIN" ] || { echo "FAIL(preflight): host binary not executable: $HOST_BIN"; exit 1; }
[ -x "$GEN_BIN" ]  || { echo "FAIL(preflight): generator binary not executable: $GEN_BIN"; exit 1; }

cleanup() { kill "${HOST_PID:-}" "${GEN_PID:-}" "${TX_PID:-}" "${ANVIL_PID:-}" 2>/dev/null || true; }
trap cleanup EXIT

# ---- chain --------------------------------------------------------------------------------
anvil --block-time 1 --port "$ANVIL_PORT" >"$WORKDIR/anvil.log" 2>&1 &
ANVIL_PID=$!
for _ in $(seq 1 30); do
  curl -sf -X POST -H 'Content-Type: application/json' \
    --data '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' "$RPC" >/dev/null 2>&1 && break
  sleep 1
done

echo "placing Token.sol at the USDC address..."
BYTECODE=$(cd "$REPO_ROOT/scripts/smoke/token" && forge build --silent && forge inspect Token deployedBytecode)
cast rpc --rpc-url "$RPC" anvil_setCode "$USDC" "$BYTECODE" >/dev/null
cast send "$USDC" "transfer(address,uint256)" "$ADDR1" 1 --private-key "$KEY0" --rpc-url "$RPC" >/dev/null 2>&1 \
  || { echo "FAIL(preflight): transfer() at USDC address failed"; tail -20 "$WORKDIR/anvil.log"; exit 1; }
( while true; do
    cast send "$USDC" "transfer(address,uint256)" "$ADDR1" 1 --private-key "$KEY0" --rpc-url "$RPC" >/dev/null 2>&1 || true
    sleep 0.3
  done ) &
TX_PID=$!

# ---- generator ------------------------------------------------------------------------------
GEN_DIR="$WORKDIR/generator"; mkdir -p "$GEN_DIR"
cp -r "$(dirname "$(dirname "$GEN_BIN")")/config" "$GEN_DIR/config" 2>/dev/null \
  || cp -r "$REPO_ROOT/../shinzo-generator-client/config" "$GEN_DIR/config"
start_generator() {
  # exec so $GEN_PID is the binary itself (not a subshell) and kill actually stops it.
  ( cd "$GEN_DIR" && exec env \
    GETH_RPC_URL="$RPC" GETH_WS_URL="ws://127.0.0.1:$ANVIL_PORT" GETH_API_KEY= GETH_API_KEY_TYPE= \
    SCHEMA_AUTH_MODE=none SHINZO_KEY_PASSPHRASE=smoke-gen DEFRADB_KEYRING_SECRET=smoke-gen SHINZO_DATA_DIR="$GEN_DIR/data" LOG_LEVEL=info \
    DEFRADB_URL="http://localhost:$GEN_DEFRA" DEFRADB_P2P_LISTEN_ADDR="/ip4/0.0.0.0/tcp/$GEN_P2P" INDEXER_HEALTH_SERVER_PORT="$GEN_HTTP" \
    "$GEN_BIN" >>"$GEN_LOG" 2>&1 ) &
  GEN_PID=$!
}
start_generator
for _ in $(seq 1 60); do
  commits=$(grep -c "Committed block" "$GEN_LOG" 2>/dev/null || true)
  [ "${commits:-0}" -ge 3 ] && curl -sf -m 3 -H 'Accept: application/json' "http://127.0.0.1:$GEN_HTTP/health" >/dev/null && break
  kill -0 "$GEN_PID" 2>/dev/null || { echo "FAIL(generator): exited early"; tail -30 "$GEN_LOG"; exit 1; }
  sleep 2
done
[ "${commits:-0}" -ge 3 ] || { echo "FAIL(generator): no blocks committed"; tail -30 "$GEN_LOG"; exit 1; }
echo "OK(generator): $commits blocks committed with USDC Transfer logs"

# ---- host config + lens fixture --------------------------------------------------------------
HOST_DATA="$WORKDIR/host-data"; LENS_DIR="$HOST_DATA/lens"; mkdir -p "$LENS_DIR"
cp "$REPO_ROOT/scripts/smoke/lens/erc20_transfer_usdc.wasm" "$LENS_DIR/"
sed "s|__LENS_DIR__|$LENS_DIR|g" "$REPO_ROOT/scripts/smoke/lens/views.json.tmpl" > "$LENS_DIR/views.json"

write_host_config() { # $1=dst $2=defra port $3=p2p port $4=http port $5=bootstrap http port
python3 - "$REPO_ROOT/config/config.yaml" "$1" "$2" "$3" "$4" "$5" <<'PY'
import re, sys
src, dst, defra, p2p, http, gen = sys.argv[1:]
s = open(src).read()
s = re.sub(r'^network: .*$', 'network: custom', s, flags=re.M)
s = s.replace('url: "localhost:9181"', f'url: "localhost:{defra}"', 1)
s = s.replace('listen_addr: "/ip4/0.0.0.0/tcp/9171"', f'listen_addr: "/ip4/0.0.0.0/tcp/{p2p}"', 1)
s = s.replace('health_server_port: 8080', f'health_server_port: {http}', 1)
s = s.replace('bootstrap_peers: []', f"bootstrap_peers: ['http://127.0.0.1:{gen}']", 1)
s = re.sub(r'^  hub_base_url: .*$', '  hub_base_url: ""', s, flags=re.M)
s = re.sub(r'^\s*indexer_url: .*$', '    indexer_url: ""', s, flags=re.M)   # no snapshot bootstrap
s = re.sub(r'^(\s*reconnect_interval_ms:) .*$', r'\1 5000', s, flags=re.M)     # reconnect quickly in the test (default 60s)
open(dst, 'w').write(s)
PY
}
HOST_CFG="$WORKDIR/host.yaml"
write_host_config "$HOST_CFG" "$HOST_DEFRA" "$HOST_P2P" "$HOST_HTTP" "$GEN_HTTP"

start_host() {
  ( cd "$WORKDIR" && exec env \
    CONFIG_PATH="$HOST_CFG" SHINZO_DATA_DIR="$HOST_DATA" SHINZO_KEY_PASSPHRASE=smoke-host LOG_LEVEL=info \
    "$HOST_BIN" >>"$HOST_LOG" 2>&1 ) &
  HOST_PID=$!
}

peers()        { curl -sf -m 3 -H 'Accept: application/json' "http://127.0.0.1:$HOST_HTTP/health" 2>/dev/null \
                 | python3 -c 'import json,sys; print(len((json.load(sys.stdin).get("p2p") or {}).get("peers") or []))' 2>/dev/null || echo 0; }
attestations() { grep -c "Created attestation" "$HOST_LOG" 2>/dev/null || true; }
view_rows()    { curl -sf -m 5 -X POST "http://127.0.0.1:$HOST_HTTP/api/v0/graphql" -H 'Content-Type: application/json' \
                   -d '{"query":"{ Studio_v1_Erc20TransferUSDC(limit: 5) { amount tokenAddress blockNumber } }"}' 2>/dev/null \
                 | python3 -c 'import json,sys; d=json.load(sys.stdin); print(len(d.get("data",{}).get("Studio_v1_Erc20TransferUSDC") or []))' 2>/dev/null || echo 0; }

await_host() { # $1=min attestations, $2=phase, $3=timeout seconds
  local deadline=$((SECONDS + $3)) p a r
  while [ "$SECONDS" -lt "$deadline" ]; do
    kill -0 "$HOST_PID" 2>/dev/null || { echo "FAIL($2): host exited early"; tail -40 "$HOST_LOG"; exit 1; }
    p=$(peers); a=$(attestations); r=$(view_rows)
    if [ "${p:-0}" -ge 1 ] && [ "${a:-0}" -ge "$1" ] && [ "${r:-0}" -ge 1 ]; then
      echo "OK($2): peers=$p attestations=$a lens-view rows=$r"; return 0
    fi
    sleep 3
  done
  echo "FAIL($2): timeout (peers=${p:-0} attestations=${a:-0} view-rows=${r:-0})"; tail -40 "$HOST_LOG"; exit 1
}

# ---- Phase 1: fresh host, URL bootstrap, attestations, lens view -------------------------------
start_host
await_host 3 "phase1" 180
grep -q "Bootstrap peer (from URL)" "$HOST_LOG" || { echo "FAIL(phase1): generator was not resolved from its node URL"; exit 1; }
PEER_ID=$(curl -sf -H 'Accept: application/json' "http://127.0.0.1:$HOST_HTTP/health" | python3 -c 'import json,sys; print(json.load(sys.stdin)["p2p"]["self"]["id"])')
echo "OK(phase1): node URL bootstrap, peer id $PEER_ID"

# ---- Phase 2: crash recovery ----------------------------------------------------------------------
P1=$(attestations)
echo "killing host with SIGKILL (pid $HOST_PID)..."
kill -9 "$HOST_PID"; sleep 2
start_host
await_host $((P1 + 2)) "phase2-recovery" 180
PEER_ID2=$(curl -sf -H 'Accept: application/json' "http://127.0.0.1:$HOST_HTTP/health" | python3 -c 'import json,sys; print(json.load(sys.stdin)["p2p"]["self"]["id"])')
[ "$PEER_ID" = "$PEER_ID2" ] || { echo "FAIL(phase2): peer id changed after restart ($PEER_ID -> $PEER_ID2)"; exit 1; }
# ---- Phase 3: the generator crashes and comes back; the host must reconnect by itself -----------
P2=$(attestations)
echo "killing generator with SIGKILL (pid $GEN_PID)..."
kill -9 "$GEN_PID"; sleep 2
for _ in $(seq 1 10); do [ "$(peers)" = 0 ] && break; sleep 2; done
echo "host sees $(peers) peer(s) while generator is down"
start_generator
for _ in $(seq 1 45); do
  commits=$(grep -c "Committed block" "$GEN_LOG" 2>/dev/null || true)
  curl -sf -m 3 -H 'Accept: application/json' "http://127.0.0.1:$GEN_HTTP/health" >/dev/null && break
  sleep 2
done
await_host $((P2 + 2)) "phase3-peer-restart" 180
# ---- Phase 4: a second host that only knows the FIRST HOST's URL -----------------------------------
HOST2_DATA="$WORKDIR/host2-data"; mkdir -p "$HOST2_DATA/lens"
cp "$REPO_ROOT/scripts/smoke/lens/erc20_transfer_usdc.wasm" "$HOST2_DATA/lens/"
sed "s|__LENS_DIR__|$HOST2_DATA/lens|g" "$REPO_ROOT/scripts/smoke/lens/views.json.tmpl" > "$HOST2_DATA/lens/views.json"
HOST2_CFG="$WORKDIR/host2.yaml"; HOST2_LOG="$WORKDIR/host2.log"
write_host_config "$HOST2_CFG" "$HOST2_DEFRA" "$HOST2_P2P" "$HOST2_HTTP" "$HOST_HTTP"   # bootstrap = host 1, not the generator
( cd "$WORKDIR" && exec env CONFIG_PATH="$HOST2_CFG" SHINZO_DATA_DIR="$HOST2_DATA" SHINZO_KEY_PASSPHRASE=smoke-host2 LOG_LEVEL=info "$HOST_BIN" >>"$HOST2_LOG" 2>&1 ) &
HOST2_PID=$!
trap 'kill "${HOST2_PID:-}" 2>/dev/null || true; cleanup' EXIT
h2_peers() { curl -sf -m 3 -H 'Accept: application/json' "http://127.0.0.1:$HOST2_HTTP/health" 2>/dev/null | python3 -c 'import json,sys; print(len((json.load(sys.stdin).get("p2p") or {}).get("peers") or []))' 2>/dev/null || echo 0; }
h2_rows()  { curl -sf -m 5 -X POST "http://127.0.0.1:$HOST2_HTTP/api/v0/graphql" -H 'Content-Type: application/json' -d '{"query":"{ Studio_v1_Erc20TransferUSDC(limit: 5) { amount } }"}' 2>/dev/null | python3 -c 'import json,sys; d=json.load(sys.stdin); print(len(d.get("data",{}).get("Studio_v1_Erc20TransferUSDC") or []))' 2>/dev/null || echo 0; }
deadline=$((SECONDS + 180))
while [ "$SECONDS" -lt "$deadline" ]; do
  kill -0 "$HOST2_PID" 2>/dev/null || { echo "FAIL(phase4): second host exited early"; tail -30 "$HOST2_LOG"; exit 1; }
  p=$(h2_peers); a=$(grep -c "Created attestation" "$HOST2_LOG" 2>/dev/null || true); r=$(h2_rows)
  if [ "${p:-0}" -ge 1 ] && [ "${a:-0}" -ge 2 ] && [ "${r:-0}" -ge 1 ]; then
    echo "OK(phase4): second host via host-1 URL: peers=$p attestations=$a lens-view rows=$r"; break
  fi
  sleep 3
done
[ "$SECONDS" -lt "$deadline" ] || { echo "FAIL(phase4): timeout (peers=${p:-0} attestations=${a:-0} view-rows=${r:-0})"; tail -30 "$HOST2_LOG"; exit 1; }
grep -q "Bootstrap peer (from URL): /ip4/127.0.0.1/tcp/$HOST_P2P/" "$HOST2_LOG" || { echo "FAIL(phase4): second host did not resolve host 1 from its URL"; grep 'Bootstrap' "$HOST2_LOG"; exit 1; }
echo "PASS: URL bootstrap, P2P replication, attestations, wazero lens view, host crash recovery, peer-restart reconnection, and host-to-host replication all verified"
