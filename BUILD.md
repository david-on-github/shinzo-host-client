# Build from source

## Prerequisites

- [Go 1.25+](https://go.dev/dl/)
- Make

The binary is self-contained: Lens transforms run on wazero (pure Go), so there are no runtime libraries to install.

## Steps

```shell
git clone git@github.com:shinzonetwork/shinzo-host-client.git
cd shinzo-host-client
make build
```

The binary lands at `./bin/host`.

## Useful commands

| Command | What it does |
| --- | --- |
| `make build` | Build the binary into `./bin/host`. |
| `make build-playground` | Download playground assets and build with the embedded GraphQL Playground UI. |
| `make start` | Run the compiled `./bin/host` binary. |
| `make deps-playground` | Download playground static assets (required before `build-playground`). |
| `go run cmd/main.go` | Run without building. |
| `go test -v ./pkg/...` | Run the test suite. |

## Build tags

| Tag | Effect |
| --- | --- |
| `hostplayground` | Embeds the GraphQL Playground UI (served on port `9182`). |

## Docker

`docker-compose.yml` pulls the published image from GHCR. To build locally instead:

```shell
docker build -t shinzo-host-client .
```

To include the Playground UI in the image, pass `--build-arg TAGS=hostplayground`.

## Running standalone (no Docker)

The binary is self-contained. Once `make build` succeeds:

```shell
./bin/host                       # that's it: joins testnet, state in ~/.shinzo/host, passphrase generated
./bin/host --data-dir /var/lib/shinzo-host --network testnet
./bin/host health                # exit 0 when the local node is healthy (probes, systemd)
./bin/host id                    # peer ID + connection string to hand out
./bin/host version
```

No config file is needed; the compiled-in defaults join `testnet`. To customise, pass `--config` (or `CONFIG_PATH`) pointing at your own copy of `config/config.yaml`. Every flag mirrors an environment variable, so the README's table applies here exactly as it does under Docker. Nothing else needs to be installed.

A minimal systemd unit:

```ini
[Unit]
Description=Shinzo host
After=network-online.target

[Service]
User=shinzo
Environment=SHINZO_DATA_DIR=/var/lib/shinzo-host
EnvironmentFile=-/etc/shinzo-host.env     # optional: SHINZO_NETWORK=..., ALLOWED_ORIGINS=...
ExecStart=/usr/local/bin/shinzo-host run
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

## Ports

> [!NOTE]
> The playground port is set to the DefraDB GraphQL port `+1`. For example, if the DefraDB GraphQL port is set to `9181`, then the playground port is automatically set to `9182`. Similarly, if the DefraDB GraphQL port is set to `443`, then they playground port is set to `444`.

| Port | Service |
| --- | --- |
| `9181` | DefraDB GraphQL + REST API |
| `9182` | GraphQL Playground UI (if enabled) |
| `9171` | libp2p P2P networking |
| `8080` | Health, metrics, registration, and a reverse proxy to the DefraDB API under `/api/v0/` |

`docker-compose.yml` publishes the HTTP server on host port `80` (container `8080`) and P2P on `9171`; `9181`/`9182` stay internal to the container. The HTTP port is the only one a browser client or dashboard needs. CORS origins and optional TLS for it are set under `host.http` in `config.yaml`; put your own reverse proxy in front if you'd rather terminate TLS there.
