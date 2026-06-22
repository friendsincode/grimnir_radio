# GitHub Release body: v2.0.0

Paste the block below into the GitHub Releases page for the `v2.0.0` tag. The links resolve once the tag exists.

---

## Grimnir Radio 2.0.0

First stable release of the high-availability architecture. 2.0.0 keeps both v1 binaries unchanged & adds two runtime binaries plus operator tooling, so a deployment can lose a node without dropping a listener. The last v1 release was `v1.40.9`; v2 runs against the same database, & the v1 single-node layout still works on the 2.0 images.

### Highlights

- **Zero-loss failover.** The edge encoder buffers a sample-aligned mix from both media engines; losing one is inaudible. Keepalived floats listener + DJ VIPs & sessions reconnect in under 5s.
- **Two new binaries.** `edge-encoder` (RTP-L16 ingest, HTTP/ICY + HLS out) & `grimnir-fanout` (one DJ in over Harbor/RTP/SRT/WebRTC, PCM-over-RTP out to every engine). Both use go-gst CGo bindings.
- **`grimnir-deploy` CLI.** One entry point for every mutating cluster operation, with `--dry-run` & an `audit_log` row per action.
- **S3/R2 media backend.** `GRIMNIR_MEDIA_BACKEND=s3` serves media from Cloudflare R2, AWS S3, or MinIO. Local disk stays as a read-through cache.
- **Custom JS player.** Vanilla ES module, no build step, automatic HQ-to-LQ fallback & recovery on the public `/listen` page & the embed widget.
- **HA observability.** Prometheus metrics per binary, a split-brain VRRP detector, replication-lag probe, & three-tier ntfy alerting with auto-rollback.

### Breaking changes

Nothing in the running system was removed; the migrations are expand-only, so v1.40.9 code still reads a 2.0 database. What changed is the build & the HA substrate:

- Source builds of `mediaengine`, `edge-encoder`, & `fanout` now require `CGO_ENABLED=1`, `libgstreamer1.0-dev`, & the GStreamer plugin packs. The published Docker images already bundle this.
- HA requires an external Postgres 16+. Single-node keeps the bundled Postgres 15.
- `:latest` now tracks the 2.0 line. Pin `:v2.0.0` if you don't want an unplanned jump.
- `RLM_*` env aliases & six legacy bare names still parse but log a deprecation warning. Prefer the `GRIMNIR_*` form.

Full list: [`docs/v2/BREAKING_CHANGES.md`](https://github.com/friendsincode/grimnir_radio/blob/v2.0.0/docs/v2/BREAKING_CHANGES.md).

### Upgrade

Single-node, from the deploy directory:

```bash
docker exec grimnir-postgres pg_dump -U grimnir grimnir > grimnir-pre-2.0.sql   # back up first
# pin :v2.0.0 in docker-compose.override.yml, then:
./grimnir pull && ./grimnir up -d
```

Migrations apply on startup. Roll back by pointing the image tags at `:v1.40.9` & restarting; no database downgrade needed.

- Upgrade guide (single-node + HA): [`docs/UPGRADING.md`](https://github.com/friendsincode/grimnir_radio/blob/v2.0.0/docs/UPGRADING.md)
- Full HA cutover runbook: [`docs/v2/UPGRADE.md`](https://github.com/friendsincode/grimnir_radio/blob/v2.0.0/docs/v2/UPGRADE.md)
- Release notes: [`docs/v2/RELEASE_NOTES.md`](https://github.com/friendsincode/grimnir_radio/blob/v2.0.0/docs/v2/RELEASE_NOTES.md)

### Known gaps

In-browser WebDJ UI & EAS integration are planned for v2.1+. The Vault secrets backend is contract-tested but not production-soaked; `.env` is the default.
