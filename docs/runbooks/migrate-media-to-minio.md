# migrate-media-to-minio (operator workflow)

## When to use this

One-time cutover from the local `/srv/data/grimnir_radio/media-data` Docker volume to the self-hosted MinIO object store on its own VM (`192.168.195.24:9000`). Run this before turning on the v2 HA stack; the second control plane needs shared media to serve listeners.

The plan rationale is in `docs/superpowers/plans/2026-06-05-object-storage-decision.md`. MinIO runs on the LAN, so media reads stay internal with no per-GB egress bill.

## Pre-flight

1. **Confirm the MinIO bucket exists & an access key is scoped to it.** On the MinIO VM (`192.168.195.24`): `mc mb local/grimnir-media-<region>` (one per region), then create a service account / access key with read-write on that bucket (`mc admin user svcacct add`, or the MinIO Console). Note:

   - Access Key
   - Secret Key
   - Endpoint URL `http://192.168.195.24:9000`

2. **Confirm rclone is installed on the production host:**

   ```bash
   ssh <ssh-user>@<v1-prod-host>
   rclone version || sudo apt-get install -y rclone
   ```

3. **Confirm the local media root is what you think it is.** Count files & total size now; you'll compare both numbers after the sync.

   ```bash
   find /srv/data/grimnir_radio/media-data -type f | wc -l
   du -sh /srv/data/grimnir_radio/media-data
   ```

## Backup the current filesystem media root

MinIO can be configured with erasure coding & versioning, but the cutover itself is the moment something can go wrong. Rsync the live tree to a safe location first.

```bash
sudo rsync -a --info=progress2 \
  /srv/data/grimnir_radio/media-data/ \
  /srv/data/grimnir_radio/media-data.pre-minio-backup/
```

Keep this backup for at least one week (see the contract below).

## Sync to MinIO

Configure rclone for the MinIO endpoint (no network calls; this just writes `~/.config/rclone/rclone.conf`):

```bash
rclone config create grimnir-minio s3 \
  provider Minio \
  endpoint http://192.168.195.24:9000 \
  access_key_id <key> \
  secret_access_key <secret>
```

Dry-run; verify the file list looks right:

```bash
rclone sync /srv/data/grimnir_radio/media-data grimnir-minio:grimnir-media-<region> \
  --dry-run --progress
```

Real run:

```bash
rclone sync /srv/data/grimnir_radio/media-data grimnir-minio:grimnir-media-<region> \
  --progress --transfers=16 --checkers=32
```

Verify the count + total size match what you saw on disk:

```bash
rclone size grimnir-minio:grimnir-media-<region>
find /srv/data/grimnir_radio/media-data -type f | wc -l
```

If the numbers diverge, do NOT proceed. Re-run `rclone sync` (it's idempotent) or investigate which files failed.

## Switch grimnir to the S3 backend

On `<v1-prod-host>`, edit `/srv/docker/grimnir_radio/docker-compose.override.yml`. Add to the `grimnir` service `environment:` block:

```yaml
environment:
  GRIMNIR_MEDIA_BACKEND: s3
  GRIMNIR_S3_BUCKET: grimnir-media-<region>
  GRIMNIR_S3_REGION: auto
  GRIMNIR_S3_ENDPOINT: http://192.168.195.24:9000
  GRIMNIR_S3_ACCESS_KEY: <key>
  GRIMNIR_S3_SECRET_KEY: <secret>
  GRIMNIR_S3_PATH_STYLE: "true"
```

Keep `GRIMNIR_MEDIA_ROOT` set; the on-disk cache still uses it.

Restart:

```bash
cd /srv/docker/grimnir_radio
./grimnir down
./grimnir up -d
```

Tail the logs to confirm the backend selection:

```bash
./grimnir logs -f grimnir | grep -i "s3\|media backend\|bucket"
```

Expect a "S3 storage initialized" line (or, if the HEAD on the bucket fails, a warning that names the bucket). If the warning fires, the token is wrong or the endpoint URL is wrong; fix that & restart.

## Verify a sample playback

1. Open the public listen page for any station that has scheduled content right now.
2. Open the MinIO Console metrics (or run `mc admin trace local`) & confirm `GET` requests increment when a new track starts.
3. Play the same track again from a different browser session; verify the second play does NOT increment MinIO requests (the on-disk cache served it).

If both happen, the cutover is live.

## Rollback (only if needed)

If something is wrong & you need the on-disk path back immediately:

```bash
cd /srv/docker/grimnir_radio
# Remove the GRIMNIR_MEDIA_BACKEND + GRIMNIR_S3_* lines from override
./grimnir down
./grimnir up -d
```

Listener traffic continues from `media-data` exactly as before; the rclone-synced MinIO content is unaffected.

## After one week of stable operation

MinIO is now the source of truth & the on-disk media root is redundant. Reclaim it:

```bash
# Confirm the database media_items.path values are addressable in MinIO.
# Pick a few rows at random:
docker exec -i grimnir-postgres psql -U grimnir -d grimnir -c \
  "SELECT id, path FROM media_items ORDER BY random() LIMIT 5;"

# For each path, confirm MinIO has it:
rclone ls grimnir-minio:grimnir-media-<region>/<station_id>/ | head

# Once you're satisfied, drop the on-disk copy:
sudo rm -rf /srv/data/grimnir_radio/media-data.pre-minio-backup
sudo rm -rf /srv/data/grimnir_radio/media-data
```

The Docker volume mount still exists in the compose file; the directory just becomes the cache. New plays repopulate it.

## What the S3 backend does NOT do

- It does NOT presign listener-facing URLs. Listeners pull HLS / ICY through nginx on the control-plane host; nginx pulls from the media engine, which pulls from MinIO via the on-disk cache. Public MinIO URLs are not exposed.
- It does NOT replicate across regions. Phase 2 of the design (see `2026-06-05-object-storage-decision.md` § "Not in scope") covers cross-region replication.
- It does NOT enforce MinIO's bucket-level versioning. Turn that on with `mc version enable local/grimnir-media-<region>`; it's off by default.

## Audit trail

This runbook is operator-driven; there is no `grimnir-deploy migrate-media-to-minio` subcommand. Record the rclone sync timestamp, the bucket name, & the version of grimnirradio that was running at cutover in the deploy log so the next operator can trace which release introduced the S3 path.
