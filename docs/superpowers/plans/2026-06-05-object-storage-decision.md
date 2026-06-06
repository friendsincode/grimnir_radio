# Object Storage Decision + Rollout Plan (Track A step 2)

> **Status:** Operational decision document. Code-side prerequisites are already in `internal/media/storage_s3.go` and configurable via env vars; this doc captures the choice + the rollout steps.

**Goal:** Pick an S3-compatible object store for the HA media path, provision it, configure both grimnir control planes + edge encoders + (future) fan-out instances to use it.

**Driver:** Section 4 of `docs/superpowers/plans/2026-06-01-ha-zero-loss-failover-design.md` ("Object storage") — when two grimnir nodes serve listeners in parallel, they need the same media library accessible from both. Current single-instance prod uses local filesystem (`media-data` Docker volume); HA requires shared storage.

## Decision: Cloudflare R2

**Recommendation: Cloudflare R2.**

R2 vs MinIO trade-off table:

| Criterion | Cloudflare R2 | Self-hosted MinIO |
|---|---|---|
| Storage cost | $0.015/GB/month | Hardware + power + ops + RAID/erasure overhead |
| Egress to listeners | **$0** (R2 has no egress fees) | Free if listeners reach via same LAN/WAN; bandwidth-bottlenecked at the host |
| Egress to engines (intra-region pulls) | $0 | $0 |
| Operational surface | One API token + bucket; nothing to run | 2-4 nodes per region for erasure-coded durability; patch/upgrade/monitor |
| Durability claim | 11 nines | Depends on cluster size + erasure config; typically 3-9 nines |
| Failure-mode blast radius | If R2 is down, all regions affected | Per-region MinIO cluster: regional failures isolated |
| Vendor lock-in | Real but low (S3-compatible; any other S3 provider drop-in) | None |
| Setup time for HA | ~30 minutes | Days (provisioning + cluster bootstrap + verification) |

**Why R2 wins for this project:**
1. **Zero egress fees** — listeners pulling HLS segments from R2 cost nothing; with MinIO every bit of listener traffic competes for the engine host's outbound bandwidth.
2. **Solo-operator math** — running a 2-4 node MinIO cluster is real ops work. R2 is "create bucket, paste token, done."
3. **Setup time** — the v2 HA stack has been waiting for storage; R2 unblocks it in an afternoon.

**Counter-case for MinIO** (when it would be the right call):
- If listener egress volume + R2 storage costs exceed what a self-hosted MinIO cluster would cost in hardware amortized over 3 years. Per the Section 4 traffic estimate (25 daily listeners pulling 128kbps for ~2 hours each = ~280MB/day = ~8GB/month per listener = ~200GB/month total) and R2 at $0.015/GB storage with free egress, hosting the entire HA media library on R2 costs **$3/month** in storage with zero egress charges. MinIO can't beat that without already-paid hardware.
- If the project becomes hostile to vendor relationships. Switching is mechanical; same S3 API.

Decision locked: **R2 for phase 1.** Re-evaluate if monthly storage exceeds 1TB ($15/month) or operational independence becomes a hard requirement.

## Code state (no work needed)

`internal/media/storage_s3.go` already implements an S3-compatible backend via `github.com/aws/aws-sdk-go-v2/service/s3`. It accepts:

| Env var | Default | Purpose |
|---|---|---|
| `GRIMNIR_MEDIA_BACKEND` | `filesystem` | Set to `s3` to enable |
| `GRIMNIR_MEDIA_S3_BUCKET` | empty | Bucket name |
| `GRIMNIR_MEDIA_S3_REGION` | `us-east-1` | R2 uses `auto`; AWS S3 uses regions |
| `GRIMNIR_MEDIA_S3_ENDPOINT` | empty | R2: `https://<account-id>.r2.cloudflarestorage.com` |
| `GRIMNIR_MEDIA_S3_ACCESS_KEY` | empty | From AWS env / IAM if blank |
| `GRIMNIR_MEDIA_S3_SECRET_KEY` | empty | From AWS env / IAM if blank |
| `GRIMNIR_MEDIA_S3_USE_PATH_STYLE` | `false` | R2 ignores; set `false` |

Same env-var pattern is reused by the edge encoder for its HLS bucket (`EDGE_ENCODER_HLS_S3_*` per Track A step 4 / v2.0.0-alpha.3).

The `internal/media` local-disk read-through cache (`/var/lib/mediaengine/cache`) is already wired; no changes needed.

## Rollout steps

### Step 1: Provision R2 (Cloudflare dashboard or API)

- Create R2 bucket `grimnir-media-prod` (one bucket; per-station prefixing inside it)
- Create an R2 API token scoped to that bucket only with `Object Read & Write` permission
- Note: `Account ID`, `Access Key`, `Secret Key`, `Endpoint URL`

### Step 2: Migrate existing media

Current prod (`/srv/data/grimnir_radio/media-data` on `<v1-prod-host>`) contains the live media library. Migrate to R2:

```bash
# Install rclone on the prod host
sudo apt-get install -y rclone

# Configure rclone for R2
rclone config create grimnir-r2 s3 \
    provider Other \
    endpoint https://<account-id>.r2.cloudflarestorage.com \
    access_key_id <key> \
    secret_access_key <secret>

# Dry-run first
rclone sync /srv/data/grimnir_radio/media-data grimnir-r2:grimnir-media-prod --dry-run --progress

# Real run
rclone sync /srv/data/grimnir_radio/media-data grimnir-r2:grimnir-media-prod --progress

# Verify count
rclone size grimnir-r2:grimnir-media-prod
ls -R /srv/data/grimnir_radio/media-data | wc -l
```

### Step 3: Update prod env (single-instance staging first)

Add to `/srv/docker/grimnir_radio/.env` on `<v1-prod-host>`:

```
GRIMNIR_MEDIA_BACKEND=s3
GRIMNIR_MEDIA_S3_BUCKET=grimnir-media-prod
GRIMNIR_MEDIA_S3_REGION=auto
GRIMNIR_MEDIA_S3_ENDPOINT=https://<account-id>.r2.cloudflarestorage.com
GRIMNIR_MEDIA_S3_ACCESS_KEY=<key>
GRIMNIR_MEDIA_S3_SECRET_KEY=<secret>
GRIMNIR_MEDIA_S3_USE_PATH_STYLE=false
```

Restart `./grimnir down && ./grimnir up -d`. Verify a track plays from R2 (the local cache will hit on subsequent plays but the first miss confirms R2 is reachable).

### Step 4: Verify dual-backend behavior

The S3 backend keeps the local `/var/lib/mediaengine/cache` read-through cache. Test:

- Play a track → first play pulls from R2 (look at network traffic / R2 dashboard request counter)
- Play the same track again → second play hits the local cache (no R2 traffic)
- Restart the container; play a fresh track → cache populates, R2 hit again

### Step 5: Multi-instance rollout (when HA VMs are ready)

Once issue #237 is resolved (Ubuntu 24.04 VMs):

- Set the same R2 env vars on both HA nodes
- Both nodes serve listeners; each has its own local cache; both read the same R2 bucket
- New media uploaded via the API lands in R2 (the `s3` backend writes through); both nodes see it on next access

### Step 6: Backup (Section 8.4 of design)

Phase 1: rely on R2's 11-nines durability + bucket versioning enabled. Enable in R2 dashboard.

Phase 2 (when pgbackrest + cross-region backup story lands per Section 8.4): cross-region R2 bucket as the disaster-recovery target.

## Acceptance

- R2 bucket created with API token scoped to it
- Existing media synced via `rclone`; count and total size match local
- Prod (single-instance) running on R2 backend; tracks play; local cache populates on miss and serves on hit
- One week of normal listener traffic logged; verify R2 spend matches estimate (~$3-5/month including the egress-free serving)

## Open questions to revisit after one week of operation

- Is the local cache hit rate satisfactory? If most plays end up hitting R2 anyway (e.g., very large library, low repeat rate), the cache sizing or eviction policy may need tuning.
- Are R2 latencies acceptable for live playout (sub-200ms)? On a fast connection R2 should serve a media file in well under a second; if not, the engine's "bounded eager prefetch" (per Section 4 of the design) can be widened.
- Per-station prefixing — current code paths use `<station_id>/<media_id>` keys; verify nothing flat-namespaces this.

## Not in scope

- Multi-region bucket replication (Phase 2)
- HLS-segment-specific bucket (will land separately for the edge encoder per Track A step 4)
- Pre-signed URL serving directly to listeners (interesting future optimization; for now nginx-proxied)
- Glacier-style cold storage for archived shows (future)

---

**Filed 2026-06-05** alongside the rest of the Track A step 6 plan. This step has minimal code work; the value is in the operational decision + rollout sequence. Pick up the rollout steps when ready to run them.
