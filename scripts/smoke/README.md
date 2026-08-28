# Host functional smoke fixtures

`smoke-binary.sh` runs the bare host binary against a bare generator binary and a
local anvil chain — no Docker, no hub, no network:

1. anvil produces blocks; `Token.sol` is placed at the mainnet USDC address with
   `anvil_setCode` and called in a loop so every block carries ERC-20 `Transfer` logs.
2. The generator indexes those blocks; the host bootstraps to it by **node URL**
   (`http://127.0.0.1:8080`), replicates the primitives over P2P and creates attestations.
3. `lens/` is the production USDC-transfer Lens (`views.json.tmpl` + wasm). The host
   loads it from its lens registry, so querying the view proves the WASM transform
   executes on wazero with no external runtime installed.
4. The host is killed with SIGKILL and restarted on the same data dir with the same
   passphrase; identity, store, peers and the view must all come back.
