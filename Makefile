GO ?= go
PKG ?= ./...
BIN ?= grimnirradio
GOFLAGS ?=
RACE ?= 1

.PHONY: help fmt fmt-check vet lint tidy test build verify ci

help:
	@echo "Common targets:"
	@echo "  make verify   # tidy, fmt, vet, (lint), test"
	@echo "  make fmt      # gofmt -s -w"
	@echo "  make vet      # go vet ./..."
	@echo "  make lint     # golangci-lint run (if installed)"
	@echo "  make test     # go test (-race)"
	@echo "  make build    # build cmd/$(BIN)"

fmt:
	@gofmt -s -w .

fmt-check:
	@unformatted=$$(gofmt -l .); \
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
	@$(GO) build $(GOFLAGS) -o ./$(BIN) ./cmd/$(BIN)

verify: tidy fmt vet lint test

ci: verify fmt-check

