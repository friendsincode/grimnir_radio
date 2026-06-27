# Breaking changes & requirements: v1.40.x to 2.0.0

The running system is backward-compatible. v2 didn't remove a single v1 env var, endpoint, table, or binary, & the expand-only migrations mean v1.40.9 code still reads a 2.0-migrated database. So a single-node upgrade is mostly a pull-and-restart.

What breaks is how you build the binaries & what the HA topology demands of the substrate. Read this before you upgrade, then follow [`docs/UPGRADING.md`](../UPGRADING.md).

## Build requirements changed (source builds only)

The media engine & the edge encoder no longer shell out to `gst-launch-1.0`. They use go-gst CGo bindings so the pipeline can switch inputs at runtime & bind a shared clock. The edge encoder moved first in `v2.0.0-alpha.3`; the media engine's spawning layer followed in `v2.0.0-alpha.4`. The fan-out (`v2.0.0-alpha.7`) uses the same bindings.

That makes CGo mandatory for those three binaries. If you build from source, you now need:

- `CGO_ENABLED=1` & a C toolchain (`gcc` or `clang`)
- `libgstreamer1.0-dev` plus the plugin packs: `gstreamer1.0-plugins-base`, `-good`, `-bad`, `-ugly`, & `gstreamer1.0-libav`
- Go 1.24.0 (see `go.mod`)

A static `CGO_ENABLED=0 go build ./...` that worked on v1.40.9 fails on `cmd/mediaengine`, `cmd/edge-encoder`, & `cmd/grimnir-fanout`. The control plane (`cmd/grimnirradio`) & the operator CLI (`cmd/grimnir-deploy`) still build without CGo.

If you deploy the published Docker images, this doesn't touch you. `ghcr.io/friendsincode/grimnir_radio`, `grimnir_mediaengine`, & `grimnir_fanout` already bundle GStreamer. There's no published `edge-encoder` image yet, so the HA edge tier is the one piece you build yourself; the build steps are in `cmd/edge-encoder/README.md`.

## Postgres 16+ for HA

The single-node stack runs on the bundled `postgres:15-alpine` & needs no change. HA is different: it leans on Postgres physical replication, so both nodes must reach an external Postgres 16 or later on TCP 5432. The verification step in `docs/v2/UPGRADE.md` Phase 0b fails fast if the server reports 15.x.

This is a requirement of the HA path, not of the 2.0 release. Stay single-node & Postgres 15 keeps working.

## The `:latest` tag now points at the 2.0 line

The compose files reference `:latest`. A `./grimnir pull` on a box that last pulled v1.40.x will jump it straight to 2.0.0. That's usually fine because the upgrade is additive, but it isn't a decision you want made for you at an unplanned moment. Pin the version tag in `docker-compose.override.yml` instead:

```yaml
services:
  grimnir:
    image: ghcr.io/friendsincode/grimnir_radio:v2.0.0
  mediaengine:
    image: ghcr.io/friendsincode/grimnir_mediaengine:v2.0.0
```

Then a `pull` only moves you when you change the tag.

## New listening ports

v2 binds ports that v1 never did. They only matter if you run the new binaries on a host that already uses these numbers, or if a firewall has to open them. The control-plane DJAuth gRPC is the one that lands on an existing node: it binds `0.0.0.0:9095` by default. Set `GRIMNIR_GRPC_PORT=0` to disable it if you don't run a fan-out.

| Port | Binary | Purpose |
|---|---|---|
| 9095 | control plane | DJAuth gRPC (set port `0` to disable) |
| 9091 | media engine | media-engine gRPC (control) |
| 9094 | media engine | NetClock master (HA) |
| 5008 | media engine | live-input PCM-over-RTP `udpsrc` (HA) |
| 8001 | edge encoder | HTTP/ICY listener endpoint |
| 9092 | edge encoder | `GetStatus` gRPC |
| 5004 / 5005 | edge encoder | RTP-L16 ingest from engine A & B |
| 8003 | fan-out | HTTP control + health |
| 9093 | fan-out | gRPC control |
| 8000 / 5006 / 1935 / 8004 | fan-out | DJ ingress: Harbor / RTP / SRT / WebRTC |

Full per-binary tables: `cmd/edge-encoder/README.md`, `cmd/grimnir-fanout/README.md`, `CLAUDE.md`.

## Deprecated env var aliases

The `GRIMNIR_*` prefix is canonical. The old `RLM_*` aliases still parse, & so do six legacy bare names: `ENVIRONMENT`, `LEADER_ELECTION_ENABLED`, `JWT_SIGNING_KEY`, `TRACING_ENABLED`, `OTLP_ENDPOINT`, & `TRACING_SAMPLE_RATE`. Each one logs a deprecation warning at startup. Nothing breaks today; rename them to the `GRIMNIR_*` form so a future release can drop the aliases without surprising you.

## Substrate the HA path expects

These are HA-only. Single-node needs none of them.

| Dependency | Minimum | Used for |
|---|---|---|
| Postgres | 16+, reachable on 5432 from both nodes | shared state + physical replication |
| Redis | reachable on 6379 from both nodes | leader election, event bus, NetClock lease, fan-out session replication |
| MinIO (self-hosted, S3-compatible) on its own VM | two buckets per region | shared media + pgbackrest backups |
| ntfy host | three topics per region | Tier-1/2/3 alerting |
| Edge VPS | nginx terminating TLS | the cutover is one `upstream` rewrite + reload |
| Two nodes | 4 vCPU, 8 GB RAM, 80 GB disk, Ubuntu 24.04 | the HA binary set per node |

Capacity note from production: a 4 vCPU node saturates around 40 stations when each station decodes & triple-encodes (MP3 @128, MP3 @64, Opus). The HA edge-encoder tier exists partly to move encoding off the control-plane node; size accordingly before you pack a node that dense.

## Rollback is safe

Because the migrations are expand-only, you roll back by pointing the image tag (or the nginx upstream, in HA) back at v1.40.9 & restarting. The 2.0 schema additions are invisible to v1 code. Keep the old media volume & a database backup until you've soaked 2.0 for a week; after that, the old volumes are yours to delete.
