.PHONY: build build-embed build-frontend web-build build-go test race

# Version: uses git tag if available (e.g. v0.1.0-alpha), falls back to "dev".
# Override on the command line: make build VERSION=v0.1.0-alpha
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -X main.version=$(VERSION)

# Build the full binary with embedded frontend (uses pnpm)
build: build-frontend
	go build -tags embed_frontend -ldflags="$(LDFLAGS)" -o bin/huginn .

# Build the full binary with embedded frontend (uses npm — CI / npm-only environments)
build-embed: web-build
	go build -tags embed_frontend -ldflags="$(LDFLAGS)" -o huginn .
	@echo "Built huginn $(VERSION) with embedded frontend"

# Build only the Go binary (no frontend required — uses placeholder)
build-go:
	go build -ldflags="$(LDFLAGS)" -o bin/huginn .

# Build the Vue frontend using pnpm (output goes to internal/server/dist via vite.config.ts)
build-frontend:
	cd web && pnpm install && pnpm build

# Build the Vue frontend using npm (output goes to internal/server/dist via vite.config.ts)
web-build:
	cd web && npm run build

test:
	go test ./...

race:
	go test -race ./...
