# Object Storage (S3/MinIO) Experimental Status

S3/MinIO support is currently **experimental**.

## Current behavior
- Upload/storage writes can be backed by S3-compatible storage when `GRIMNIR_S3_BUCKET` is set.
- Several runtime paths still assume local filesystem media paths.

## Known limitations
- Analyzer path resolution still reads local media paths.
  - `internal/analyzer/service.go`
- Playout/director runtime path resolution still reads local media paths.
  - `internal/playout/director.go`
- Public archive stream serving still uses local filesystem serving.
  - `internal/web/pages_public.go`

## Operational guardrails
- Startup logs emit a warning when S3 is enabled.
- Do not treat S3 mode as full end-to-end parity for production playout/analyze workloads yet.

## Follow-up work for full support
1. Refactor analyzer to consume storage URLs/streams instead of local file paths.
2. Refactor playout pathing to resolve through a storage abstraction.
3. Refactor public archive streaming to read through storage backend abstraction.
4. Add integration tests covering filesystem and S3 modes for upload, analyze, playout, and archive stream paths.
