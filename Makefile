GO ?= go
PKG ?= ./...
BIN ?= grimnirradio
GOFLAGS ?=
RACE ?= 1
PROTO_DIR ?= proto
PROTO_OUT ?= proto

.PHONY: help fmt fmt-check vet lint tidy test test-e2e test-frontend build build-mediascan verify ci proto proto-clean \
        dev-db dev-redis dev-stack run-control run-media

help:
	@echo "Common targets:"
	@echo "  make verify      # tidy, fmt, vet, (lint), test"
	@echo "  make fmt         # gofmt -s -w"
	@echo "  make vet         # go vet ./..."
	@echo "  make lint        # golangci-lint run (if installed)"
	@echo "  make test        # go test (-race)"
	@echo "  make build       # build cmd/$(BIN)"
	@echo ""
	@echo "Frontend testing targets:"
	@echo "  make test-e2e    # Run E2E browser tests (go-rod)"
	@echo "  make test-routes # Quick route verification (no browser)"
	@echo ""
	@echo "Development targets:"
	@echo "  make dev-db      # Start PostgreSQL container"
	@echo "  make dev-redis   # Start Redis container"
	@echo "  make dev-stack   # Start full dev stack (docker-compose)"
	@echo "  make run-control # Run control plane locally"
	@echo "  make run-media   # Run media engine locally"

fmt:
	@files=$$(git ls-files '*.go'); \
	gofmt -s -w $$files

fmt-check:
	@files=$$(git ls-files '*.go'); \
	unformatted=$$(gofmt -l $$files); \
	if [ -n "$$unformatted" ]; then \
		echo "Unformatted Go files:"; echo "$$unformatted"; \
		exit 1; \
	fi

vet:
	@$(GO) vet $(GOFLAGS) $(PKG)

lint:
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "golangci-lint not found; skipping lint"

tidy:
	@$(GO) mod tidy

test:
	@$(GO) test $(GOFLAGS) $(if $(filter 1,$(RACE)),-race,) $(PKG)

build:
	@$(GO) build $(GOFLAGS) -o ./grimnirradio ./cmd/grimnirradio
	@$(GO) build $(GOFLAGS) -o ./mediaengine ./cmd/mediaengine

build-mediascan:
	@$(GO) build $(GOFLAGS) -o ./bin/mediascan ./cmd/mediascan

verify: tidy fmt vet lint test

ci: verify fmt-check

proto:
	@echo "Generating protobuf code..."
	@PATH="$$PATH:$$HOME/go/bin" protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		$(PROTO_DIR)/mediaengine/v1/*.proto

proto-clean:
	@find $(PROTO_OUT) -name '*.pb.go' -delete
	@echo "Cleaned generated protobuf files"

# Development targets
dev-db:
	@docker run -d --name grimnir-postgres \
		-e POSTGRES_USER=grimnir \
		-e POSTGRES_PASSWORD=grimnir_secret \
		-e POSTGRES_DB=grimnir \
		-p 5432:5432 \
		postgres:15-alpine || docker start grimnir-postgres
	@echo "PostgreSQL running on localhost:5432"

dev-redis:
	@docker run -d --name grimnir-redis \
		-p 6379:6379 \
		redis:7-alpine || docker start grimnir-redis
	@echo "Redis running on localhost:6379"

dev-stack:
	@docker compose up -d postgres redis
	@echo "Dev stack running (postgres + redis)"

run-control:
	@if [ -f .env ]; then set -a; . ./.env; set +a; fi; \
	GRIMNIR_DB_DSN="host=localhost port=5432 user=grimnir password=$${POSTGRES_PASSWORD:-grimnir_secret} dbname=grimnir sslmode=disable" \
	GRIMNIR_REDIS_ADDR="localhost:6379" \
	GRIMNIR_MEDIA_ENGINE_GRPC_ADDR="localhost:9091" \
	GRIMNIR_JWT_SIGNING_KEY="$${GRIMNIR_JWT_SIGNING_KEY:-dev-secret-key}" \
	$(GO) run ./cmd/grimnirradio serve

run-media:
	$(GO) run ./cmd/mediaengine

# Frontend testing targets
test-e2e:
	@echo "Running E2E frontend tests..."
	@E2E_HEADLESS=true $(GO) test $(GOFLAGS) -v ./test/e2e/...

test-frontend: test-e2e

# Test all routes are working (quick HTTP check without browser)
test-routes:
	@echo "Running route tests..."
	@$(GO) test $(GOFLAGS) -v -run TestTemplateRendering ./test/e2e/...
