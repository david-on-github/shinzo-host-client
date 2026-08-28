# Build stage. The binary is self-contained: lens transforms run on wazero
# (pure Go), so no WASM runtime libraries are needed at build or run time.
FROM golang:1.26 AS builder

ARG TAGS=hostplayground

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN set -ex && \
    if echo "$TAGS" | grep -q hostplayground; then (cd playground && go generate .); fi && \
    CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -tags="${TAGS}" -o bin/host cmd/main.go

# Runtime stage
FROM ubuntu:24.04

# Set by the release workflow; harmless defaults for local builds.
ARG VERSION=dev
ARG VCS_REF
LABEL org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.revision=$VCS_REF \
      org.opencontainers.image.source=https://github.com/shinzonetwork/shinzo-host-client

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates tzdata wget \
    && apt-get clean && rm -rf /var/lib/apt/lists/*

RUN groupadd -g 1001 shinzo && useradd -u 1001 -g shinzo -m -s /bin/bash shinzo

WORKDIR /app
COPY --from=builder /app/bin/host /app/host
COPY --from=builder /app/config/config.yaml /app/config.yaml
COPY --from=builder /app/playground/dist /app/playground/dist
RUN mkdir -p data && chown -R shinzo:shinzo /app

# All node state (database, keys, lens registry) lives here. Declared so that
# even a bare `docker run` gets a volume instead of writing into the container layer.
VOLUME ["/app/data"]

USER shinzo

# Embedded DefraDB logging (corelog); the app logger honours LOG_LEVEL too.
ENV LOG_LEVEL=error
ENV LOG_SOURCE=false
ENV LOG_STACKTRACE=false

# 8080: HTTP (health, metrics, registration, /api/v0/ proxy); 9171: libp2p
EXPOSE 8080 9171

HEALTHCHECK --interval=15s --timeout=30s --start-period=120s --retries=10 \
    CMD wget -qO- http://localhost:8080/health >/dev/null || exit 1

CMD ["./host"]
