<!--
  This README covers local setup, Docker, and deployment only.
  Do not add: architecture explanations, API reference, configuration 
  deep-dives, or troubleshooting guides. Those belong in the Shinzo 
  documentation site. If you're tempted to add a section, link to the docs 
  instead.
-->

# shinzo-host-client

![Build Status](https://img.shields.io/github/actions/workflow/status/shinzonetwork/shinzo-host-client/go-test.yml)
![License](https://img.shields.io/github/license/shinzonetwork/shinzo-host-client)
![Docker](https://img.shields.io/docker/v/shinzonetwork/shinzo-host-client)

A Host node for the Shinzo network. It pulls primitive blockchain data from Indexers, runs Lens WASM transforms, and serves the resulting Views to subscriber nodes via an embedded DefraDB instance.

## Getting started

Make sure `~/config.yaml` exists on the host machine, then:

```shell
docker compose up
```

Set the passphrase that encrypts your node's keys before starting — either export it or copy `.env.sample` to `.env` and fill it in:

```shell
export SHINZO_KEY_PASSPHRASE=<your-passphrase>
```

Further instructions, as well as hardware recommendations, can be found at [docs.shinzo.network](https://docs.shinzo.network/hosts/overview).

> [!TIP]
> See [BUILD.md](./BUILD.md) for full build-from-source instructions.

## Local development

Copy the example override and edit it; `docker compose up` picks it up automatically and merges it over `docker-compose.yml`:

```shell
cp docker-compose.override.example.yml docker-compose.override.yml
```

The override is where local-only tweaks live (build from source, debug logging, memory limits). `docker-compose.yml` stays the reference deployment and is what operators run unchanged.

## Configuration

The image ships with working defaults for the `testnet` network; nothing needs to be mounted. The few things an operator picks are environment variables:

| Variable | Purpose |
| --- | --- |
| `SHINZO_KEY_PASSPHRASE` | Encrypts the node's identity keys on disk. Required. |
| `SHINZO_NETWORK` | `testnet` (default) or `custom`. Selects the built-in hub + bootstrap peers. |
| `BOOTSTRAP_PEERS` | Comma-separated extra peers: node URLs, IPs, or multiaddrs. |
| `ALLOWED_ORIGINS` | Extra browser origins allowed to call this node (CORS), added to the network's own apps (`https://*.shinzo.network`). |
| `LOG_LEVEL` | `debug`, `info`, `warn`, `error`. |
| `LOG_DIR` | Also write log files here. Unset = stdout only (the container/systemd default). |
| `SHINZO_KEY_PASSPHRASE_FILE` | Read the passphrase from a file (Docker/Kubernetes secrets) instead of the env var. |
| `BOOTSTRAP_FROM_HUB` | `true` to also discover indexers from the hub registry (capped by `max_indexer_peers`). Off by default. |
| `SOURCE_CHAIN_ID` | EVM chain the host consumes (default `1`, Ethereum mainnet). |
| `SHINZO_DATA_DIR` | Where all node state lives (default `./data`; `/app/data` in the image). |
| `CONFIG_PATH` | Use a specific config file instead of the built-in lookup. |

Everything else lives in `config/config.yaml` (tuning knobs with sane defaults). To change those, mount your own copy as `/app/config.local.yaml` — see the override example.

See [docs.shinzo.network](https://docs.shinzo.network/hosts/overview) for the full configuration reference.

## Deployment

See the [Shinzo documentation site](https://docs.shinzo.network) for production deployment instructions, hardware requirements, and network topology guidance.

## Contributing

Open an issue before submitting a PR. See [CONTRIBUTING.md](./CONTRIBUTING.md) for guidelines.

## License

[MIT](./LICENSE)
