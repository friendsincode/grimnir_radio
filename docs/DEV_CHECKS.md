# Dev Checks

This repo relies on automated checks to prevent regressions.

## Local Commands

```bash
go mod tidy
git diff --exit-code

gofmt -l .
go vet ./...
CI=1 go test ./...
```

## Make Targets

```bash
make fmt-check
make vet
CI=1 make test
```

