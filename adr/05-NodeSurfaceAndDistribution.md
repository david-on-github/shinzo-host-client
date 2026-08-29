# ADR-05: Node surface, configuration, and distribution

## Status
Proposed (implemented on branch `refactor/drop-nginx-sidecar`; applies equally to `shinzo-generator-client`)

## Context

The host and generator clients are distributed software: anyone can run a node. The repositories, however, had grown to describe *one particular deployment* — Shinzo's own — rather than the software:

- An **nginx sidecar** (`nginx.conf`, a second container) did CORS, merged DefraDB's port with the health server's, and in production terminated TLS with a self-signed cert for `api.shinzo.network`. Three drifting copies of that config existed (repo, prod compose, a heredoc in a setup script).
- **Deployment scripts** for GCP (`host-prod-setup.sh`, `gcp-startup-host*.sh`, `docker-compose-prod.yml`) lived beside the code. A third party running a host had no use for them and would have been misled by them (they install nginx and configure a certificate for a domain they don't own).
- The single **`config.yaml`** was simultaneously the shipped default, the network definition (bootstrap peer IPs, hub URL), and one developer's local settings (`keyring_secret: pingpong`, `host.docker.internal`). Local edits produced `.bak` files and "LOCAL DEV:" comments in a file that ships to users.
- **Connection strings** were libp2p multiaddrs with peer IDs baked in (`/ip4/1.2.3.4/tcp/9171/p2p/12D3KooW…`), which users had to copy by hand.
- Users had to reason about four ports (8080, 9181, 9182, 9171) and bind-mounted directories.

## Decision

### 1. The node owns its public HTTP surface

The health server on `:8080` is the node's only HTTP port. It serves health, metrics, registration, and **reverse-proxies the embedded DefraDB API under `/api/v0/`**, so a browser client needs one origin. CORS and optional TLS are handled in the binary and configured under `host.http`:

```yaml
host:
  http:
    allowed_origins: []        # empty = no CORS headers (also ALLOWED_ORIGINS env)
    tls:
      cert_file: ""            # both set = serve HTTPS directly
      key_file: ""
```

nginx is gone. Operators who want a reverse proxy in front (TLS termination, rate limiting) add their own; the node does not ship one.

The network preset contributes `https://*.shinzo.network` so every node is readable by the network's own web apps (explorer, studio, …) with no configuration; `allowed_origins` and `ALLOWED_ORIGINS` are additive. CORS here is a policy knob, not a security boundary — nodes hold no cookies or sessions, and anything that needs authorisation uses signed requests — so a scheme-pinned subdomain wildcard costs nothing and avoids a client release for every new Shinzo app.

*Rationale.* CORS is part of the API contract ("which origins may call this?"), so it belongs with the API. A sidecar that duplicates the route table in a second language is exactly what drifts. DefraDB's `:9181` and the Playground's `:9182` still exist inside the container but are no longer published.

### 2. Deployment artifacts leave the repository

Everything describing *how Shinzo deploys* is removed: nginx config, prod compose, GCP/cert setup scripts. The repo keeps the `Dockerfile`, a single-service reference `docker-compose.yml`, and an example config. Shinzo's own infra belongs in an infra repo that consumes the published image.

### 3. One image, one compose file, one override

- `docker-compose.yml` is the **reference deployment**: published image, ports `80:8080` and `9171:9171`, one named volume `data:/app/data`, environment variables for the handful of operator inputs. No config mount, no healthcheck (the image carries a `HEALTHCHECK` on `GET /health`, which returns 503 when the node is unhealthy).
- `docker-compose.override.example.yml` is copied to the gitignored `docker-compose.override.yml`; Compose merges it automatically. Build-from-source, debug logging, memory limits, port remaps and config bind-mounts live there. The generator's override remaps to `18080`/`19171` so both nodes can share one machine.
- Host port **80** is published so a node's address is a bare URL (`http://1.2.3.4`, `https://node.example`). Inside the container the server stays on 8080 because the image runs as a non-root user. When TLS lands, `443` joins it.

### 4. All node state under one directory

DefraDB store, keyring, and lens registry all live under `./data` (`/app/data` in the image). One named volume with an explicit, stable name (`shinzo-host-data` — not the Compose default `<directory>_data`, which silently changes if the checkout is renamed), one `SHINZO_DATA_DIR` variable to relocate it, one `.gitignore` entry. The directory name is deliberately not `defra` or `lens`: it is *the node's state*, and what is inside is an implementation detail.

### 5. Three kinds of configuration, three homes

| Kind | Example | Where it lives |
| --- | --- | --- |
| Network constants | hub hostname, bootstrap peers | Code: `config/networks.go`, selected by `network:` / `SHINZO_NETWORK` |
| Operator inputs | keyring secret, allowed origins, extra peers, log level | Environment variables (see README) |
| Tuning knobs | cache sizes, worker counts, filters | `config/config.yaml`, baked into the image; override by mounting `/app/config.local.yaml` or setting `CONFIG_PATH` |
| Developer settings | `pingpong`, `host.docker.internal` | Gitignored `config/config.local.yaml`, found first by `findConfigFile()` |

Network presets are code because they are properties of the network, not of any deployment — the same reason geth compiles in mainnet bootnodes. `network: custom` disables presets for private networks.

### 6. Hub discovery exists, is bounded, and is opt-in

Which indexers a host listens to is a trust and cost decision, not a lookup: a host subscribes to the primitive collections (`AddP2PCollections`), and DefraDB pubsub then delivers every update from every connected peer. Registration on the hub is KYC-gated today but intended to become trustless (ADR-03), and nothing below the attestation threshold protects a host from a spammy or broken indexer's stream. The default is therefore the **static** choice — the network preset's seeds plus the operator's `bootstrap_peers` — exactly as before, only with the seeds in code and node URLs allowed.

`bootstrap_from_hub: true` (or `BOOTSTRAP_FROM_HUB=true`) opts in to discovery. When enabled, the host queries ShinzoHub's indexer registry (`GET /shinzonetwork/indexer/v1/indexers?source_chain_id=<ours>`) for indexers registered for its source chain (`shinzo.source_chain_id`, default 1 = Ethereum mainnet). It does **not** peer with all of them. Every indexer peer pushes a full copy of every block — that is the attestation model from ADR-01/02 — so ingestion cost scales linearly with indexer peer count. Selection (`pkg/host/peer_select.go`):

1. every explicitly configured peer (network preset + `bootstrap_peers`) is always kept — operator intent wins;
2. hub-discovered indexers fill the remainder, minus this node and unroutable addresses, in a stable pseudo-random order keyed by our peer ID so different hosts spread across different indexers;
3. stop at `max_indexer_peers` (default `minimum_attestations + 1`, so one indexer can drop without stalling attestations).

A hub outage degrades to the static lists. Flipping the default to on is a one-line change once indexer policing exists. **Confirmed by the host smoke test (phase 4):** a second host given only the first host's URL ends up connected to the generator as well — DefraDB *does* exchange peers — so `max_indexer_peers` bounds who a host *dials*, not who it ends up connected to. The real controls remain signature verification against registered keys and DefraDB-level peer allow-listing. Also worth knowing: `shinzo.minimum_attestations` is not enforced anywhere in the host today (it only sizes the default peer cap); the attestation threshold from ADR-01/02 is still unimplemented. The IP multiaddrs in `networks.go` remain a stopgap until DNS names exist for the seed indexers (see Future work).

### 7. One address per node

`bootstrap_peers` entries may be plain node URLs:

```yaml
bootstrap_peers:
  - https://node.example
```

The host fetches `<url>/registration`, reads the peer ID and P2P port the node advertises, and dials `/dns4/node.example/tcp/<port>/p2p/<id>` itself. The URL a user hands out for the API is therefore also the URL peers use; the libp2p multiaddr becomes something the node advertises, never something a human types. Raw multiaddrs and IPs keep working.

*Known limitation.* The P2P port comes from what the node advertises, which is its *listen* port. Behind a port remap or NAT (`-p 19171:9171`, a home router) the published port differs and the resolver dials the wrong one. Until nodes can advertise a public address (libp2p announce addresses — see Future work), run the P2P port unmapped (`19171:19171` with `listen_addr` on 19171), which is what the generator's dev override does.

### 8. The image follows the conventions of other self-contained services

The same things Postgres, Grafana, Gitea and Kubo do, because operators and tooling expect them: non-root user; `VOLUME ["/app/data"]` so even a bare `docker run` persists state; `EXPOSE 8080 9171` documenting exactly what the reference deployment publishes; a `HEALTHCHECK`; OCI `source`/`version`/`revision` labels; logs to stdout only (files are opt-in via `LOG_DIR`); graceful shutdown on SIGTERM within Docker's default 10 s stop timeout; secrets accepted from a file (`SHINZO_KEY_PASSPHRASE_FILE`); a `.env.sample`; SBOM + provenance attestations on every published image; dependabot on base images.

The reference compose stays minimal on purpose: `init`, `security_opt`, `stop_grace_period` and per-service log rotation were considered and left out — the binary handles its own signals and spawns nothing, runs non-root, stops within the default grace period, and log rotation belongs in the Docker daemon config (`/etc/docker/daemon.json`), where operators set it once. `.dockerignore` excludes node state and machine-local config so a 10 GB `data/` never ends up in a build context.

Deliberately not done: `read_only: true` root filesystem (Badger and the WASM runtimes' temp-file behaviour need verifying first) and image signing (cosign) — both worthwhile follow-ups.

### 9. The binary runs standalone

The same binary runs outside Docker with `CONFIG_PATH`, `SHINZO_DATA_DIR`, and the same environment variables; `BUILD.md` documents this plus a systemd unit. It has no external dependencies: Lens transforms run on wazero (pure Go), and the binary links only libc. The Dockerfiles used to download wasmtime and wasmer and ship them in the runtime image — leftovers from an earlier Lens engine, verified dead with `ldd`/`otool` and removed. Binaries are built with `-trimpath -ldflags="-s -w"`, and the host prints a three-line banner on startup (API URL, peer ID, registration URL) so a first run explains itself.

## Consequences

**Positive**
- A new operator runs `docker compose up` (or `docker run … -p 80:8080 -p 9171:9171 -v shinzo:/app/data`) and has a node on testnet. No files to edit, no nginx, one URL to share.
- Two ports instead of four; one volume; one config file with one job.
- The repos describe the software. Deployment concerns have a separate home.
- Hub-derived peers mean rotating a seed indexer needs no release.

**Negative / things to migrate**
- Browser apps that hit `localhost:9181` must move to `http://localhost/api/v0/graphql`.
- Existing Shinzo prod hosts run the old topology (nginx on the box, dashboard probing `:8080`). Their migration is the infra repo's concern, but must happen before they pull a new image.
- The `HEAD`-based `wget --spider` healthcheck was replaced; anything scripted against `/metrics` for liveness should use `/health`.
- Two nodes on one machine now need the override file (they previously coexisted via the generator's hardcoded `18080` remap).
- The keyring passphrase is now `SHINZO_KEY_PASSPHRASE` in both repos (it was `DEFRA_KEYRING_SECRET` on the host and `DEFRADB_KEYRING_SECRET` on the generator — one concept, two names, and both leaked the storage engine into the operator surface). The old names still work as aliases.

## Future work

1. **Automatic TLS.** With a DNS name, `autocert` can obtain Let's Encrypt certificates in ~15 lines, making `tls.cert_file` an override rather than a requirement. Requires a second listener on `:80` for HTTP-01 challenges and redirects.
2. **Shinzo-issued node DNS.** The hub already knows every node's identity and advertised address; a small service can publish `<name>.nodes.shinzo.network` on registration. Combined with (1), a home operator gets a stable HTTPS address without owning a domain.
3. **Network parameters served by the hub.** Everything in `networks.go` except the hub hostname (seed peers, allowed origins) is release-coupled. A hub endpoint (or signed well-known JSON) fetched at startup, with `networks.go` as the offline fallback, would make those hub changes instead of client releases.
4. **DNS seeds instead of IPs.** Replace the literal multiaddrs in `networks.go` with node URLs (`https://ind1.testnet.shinzo.network`) or a libp2p `/dnsaddr/` TXT record, so the static fallback is also release-free.
5. ~~Auto-generated keyring secret~~ Done: with no `SHINZO_KEY_PASSPHRASE`, the node generates a 256-bit passphrase into `<data dir>/passphrase` (0600) on first run and reuses it; the banner says where it is. The default config is compiled into the binary, the data dir defaults to `~/.shinzo/host` (XDG-aware; `/app/data` in the image), and `shinzo-host run|health|id|version` with `--config/--data-dir/--network/--passphrase` flags mirror the environment variables. `health` replaces `wget` in the image's HEALTHCHECK.
6. ~~One image per version.~~ Done: the `-ethereum-mainnet` suffix turned out to be a label only (the `CHAIN`/`NETWORK` build args it produced were never declared in the Dockerfile). `release.yml` now triggers on `v*` and publishes multi-arch `vX.Y.Z` + `latest`. The compose files pin the last suffixed tag until the next release.
7. **Advertise a public P2P address.** Add `p2p.announce_addr` (libp2p announce addresses) so `/registration` reports the address peers should dial, not the listen address — fixes URL resolution behind port remaps and NAT.
8. **Tunnelled P2P for NAT'd operators** via libp2p's WebSocket transport, so Cloudflare Tunnel / Tailscale Funnel can carry both HTTP and P2P.
9. **Generator parity** for network presets and hub-derived peers, if generators ever peer with each other.

## Verification

Tested end to end on 2026-08-28 with both images built from this branch (native arm64), the local `dev-setup` hub, and `scripts/smoke-test.sh` against each node: config lookup, hub-derived and URL-resolved bootstrap peers, bidirectional P2P peering, DefraDB proxy, CORS, image healthchecks, named-volume state, and the standalone binary. Both repos now carry this as CI (`smoke-test.yml`, PR-triggered, never publishes): an *image* job (which also proves the zero-config first run generates a passphrase and keeps identity across a restart), an *upgrade* job (the PR's base commit is built and run the way it shipped — `./.defra`, its config file, `DEFRA_KEYRING_SECRET` — then the new binary starts on the same directory and must keep the same peer ID), and a *binary* job. The host's binary job covers URL bootstrap, replication, attestations, the Lens view, host `kill -9` recovery, generator `kill -9` + host reconnection, and a second host bootstrapping from the first host's URL (host-to-host replication). Compared to the earlier description, this replaces the sentence: an *image* job (build, start with no network dependencies, HTTP surface, graceful stop) and a *binary* job. The generator's binary job indexes tx-bearing blocks from anvil and recovers from `kill -9`. The host's binary job (`scripts/smoke-binary.sh`) runs a bare generator and a bare host against anvil with `Token.sol` placed at the USDC address, and asserts node-URL bootstrap, P2P replication, attestations, the production USDC Lens view returning transformed rows (the WASM transform executing on wazero with nothing installed), and `kill -9` recovery with an unchanged peer ID. Dockerfiles use BuildKit cache mounts so CI image builds reuse Go's module and build caches.

Testing found and fixed four defects that unit tests had not caught:

1. `cmd/main.go`'s `config.local.yaml` / `CONFIG_PATH` lookup had never been written (a failed script step), so the container loaded the baked-in defaults.
2. `DEFRA_KEYRING_SECRET` was only read into `defradb.DefaultConfig` and ignored whenever a YAML config loaded — a pre-existing bug the README contradicted. `LoadConfig` now applies it; the test that asserted the old behaviour was flipped.
3. Both nodes rewrite `localhost` in the DefraDB URL to the container's LAN IP at startup and drop the scheme. The health server was given the stale pre-rewrite value (host) or the schemeless one (generator), so the `/api/v0/` proxy and `checkDefraDB` could not reach DefraDB. The health server now normalises the scheme and the host prefers `defraNode.APIURL`.
4. The URL resolver rejected a 503 `/registration` response, but 503 means "no recent block", and the body still carries the node's identity. It now accepts it.

## Alternatives considered

- **Keep nginx, fix the config drift.** Rejected: still a second container and a second route table for every operator, for features the binary provides in ~100 lines.
- **Explorer proxies to nodes server-side (no CORS anywhere).** Valid and simpler for the node, but it makes nodes unreachable from browsers by design. Kept CORS in the node, off by default, so both models work.
- **Network presets in an embedded YAML.** Cosmetic difference from Go literals; can be revisited if non-Go contributors need to edit them.

## References

- `pkg/server/cors.go`, `pkg/server/health.go` — CORS, proxy, TLS
- `config/networks.go` — network presets
- `pkg/host/peer_url.go` — node-URL bootstrap resolution
- `pkg/shinzohub/indexers.go` — hub-derived peers
- `docker-compose.override.example.yml`, `BUILD.md` — local dev and standalone
