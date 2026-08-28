VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build build-playground start deps-playground lint lint-fix

build:
	go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o bin/host cmd/main.go

build-playground: deps-playground
	go generate -tags hostplayground ./playground
	go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -tags hostplayground -o ./bin/host cmd/main.go

start:
	./bin/host

# Download playground static assets
deps-playground:
	cd playground && go generate .

lint:
	@echo "🔍 Running golangci-lint..."
	@golangci-lint run ./...

lint-fix:
	@echo "🔧 Running golangci-lint with auto-fix..."
	@golangci-lint run --fix ./...
