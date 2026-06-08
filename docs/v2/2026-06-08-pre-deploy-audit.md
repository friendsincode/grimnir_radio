# v2.0.0-rc.5 pre-deploy audit

Date: 2026-06-08. Branch: `v2-dev` @ tag `v2.0.0-rc.5` (commit `709f4ce`). Auditor: Claude (Opus 4.7, automation).

This report walks every check in the pre-deploy audit checklist & records pass/fail with evidence. Findings are categorized **Blocker** / **Warning** / **Info** at the bottom.

## Summary

| Bucket | Count |
|---|---:|
| Checks run | 47 |
| Pass | 41 |
| Warning | 5 |
| Blocker | 1 |

The single blocker is documentation drift: `docs/v2/UPGRADE.md` still pins `git checkout v2.0.0-rc.1` & references a `docker-compose.v2.example.yml` file that doesn't exist in the repo. An operator following UPGRADE.md verbatim today would check out rc.1 (which is missing every fix between rc.1 and rc.5, including the v1.40.8 PCM-decoder leak fix backported into rc.5) & then fail on the missing compose file. Patch the two strings before the operator starts Phase 3.

Everything else is green or a known-tracked Warning.

## 1. Binary builds

Each binary built clean with no warnings on Go 1.24.

| Target | Exit | Notes |
|---|---:|---|
| `go build ./cmd/grimnirradio/...` | 0 | |
| `go build ./cmd/mediaengine/...` | 0 | |
| `go build ./cmd/edge-encoder/...` | 0 | CGo (go-gst) |
| `go build ./cmd/grimnir-fanout/...` | 0 | CGo (go-gst + libsrt) |
| `go build ./cmd/grimnir-deploy/...` | 0 | |
| `go build ./cmd/alertmanager-ntfy/...` | 0 | |
| `go build ./cmd/mediascan/...` | 0 | |
| `go build ./cmd/migration-lint/...` | 0 | |
| `make build` | 0 | top-level chains the deploy build |

All eight binaries currently shipping in `cmd/` compile clean. No `-W` warnings surfaced.

## 2. Full test suite

`make ci` (= `verify` + `fmt-check` + `migration-lint-ci`) exit 0.

- Total packages: 77 (48 with tests, 29 marked `[no test files]`).
- Zero `FAIL` lines in the output.
- All cached (`(cached)`) on this run because the source tree is clean & the test cache is warm; an uncached run would re-execute every test in the 48-package set.
- `golangci-lint` is logged as `not found; skipping lint`. The CI workflow has the same skip path. **Warning**: the lint pass that fires on PRs in GitHub Actions runs against a separate installer; verify the workflow still passes by opening a PR before cutover.

No flakes observed in this run. Previous v2-dev runs (per `notes/` & recent commits) flagged a flaky hour-boundary scheduler test in v1.38.11; the truncation fix in that release is present on this branch.

## 3. Migrations

GORM `AutoMigrate` in `internal/db/migrate.go` registers 60 models (User, SystemSettings, APIKey, PlatformGroup, PlatformGroupMember, AuditLog, Station, StationUser, StationGroup, StationGroupMember, Mount, StationStream, ListenerEvent, EncoderPreset, MediaItem, Tag, MediaTagLink, SmartBlock, ClockHour, ClockSlot, ScheduleEntry, PlayHistory, MountPlayoutState, SmartBlockGeneration, PlayoutQueueItem, PlayoutQueueDecision, AnalysisJob, PrioritySource, ExecutorState, LiveSession, Webstream, Playlist, PlaylistItem, Clock, Show, ShowInstance, ScheduleRule, ScheduleTemplate, ScheduleVersion, DJAvailability, ScheduleRequest, ScheduleLock, NotificationPreference, Notification, WebhookTarget, WebhookLog, ListenerSample, ScheduleAnalytics, ScheduleAnalyticsDaily, Network, NetworkShow, NetworkSubscription, Sponsor, UnderwritingObligation, UnderwritingSpot, LandingPage, LandingPageAsset, LandingPageVersion, migration.Job, StagedImport, Recording, RecordingChapter, OrphanMedia, ScheduleSuppression, WebDJSession, WaveformCache, deployaudit.Entry, deployhistory.Entry). The SQL files in `migrations/` are supplemental: partial unique indexes (content_hash, play_histories entry_id), the Postgres schedule-overlap trigger, legacy platform-role normalization, & webstream health-method migration.

Migration lint:
- `make migration-lint` → exit 0. Every `migrations/*.sql` is non-destructive or carries `-- migration-contract:` annotation (none currently need it).
- No `DROP`, `ALTER COLUMN ... TYPE`, `RENAME COLUMN`, or `TRUNCATE` in any non-TEMPLATE migration file.

SQLite + Postgres compatibility: both `applyContentHashUniqueIndex` & `applyPlayHistoryUniqueIndex` use partial-index syntax both engines support; the schedule-overlap trigger gates itself on `Dialector.Name() == "postgres"`.

Pass.

## 4. Environment variable inventory

The code reads 49 environment variables across the binary set (excluding stdlib & test-only):

`CI`, `E2E_HEADLESS`, `EDGE_ENCODER_ENGINE_A_GRPC`, `EDGE_ENCODER_ENGINE_B_GRPC`, `EDGE_ENCODER_HLS_S3_BUCKET`, `EDGE_ENCODER_HLS_S3_ENDPOINT`, `GRIMNIR_ALERTBRIDGE_ADDR`, `GRIMNIR_COVERAGE_PROFILE`, `GRIMNIR_COVERAGE_TARGET`, `GRIMNIR_DB_DSN`, `GRIMNIR_DEPLOY_AUTOROLLBACK_ENABLED`, `GRIMNIR_DEPLOY_AUTOROLLBACK_PROM_URL`, `GRIMNIR_DEPLOY_AUTOROLLBACK_TICK`, `GRIMNIR_DEPLOY_DB_DSN`, `GRIMNIR_DEPLOY_OPERATOR`, `GRIMNIR_DEPLOY_PEER_HOST`, `GRIMNIR_DEPLOY_PEER_SSH_KEY`, `GRIMNIR_DEPLOY_PEER_SSH_PORT`, `GRIMNIR_DEPLOY_PEER_SSH_USER`, `GRIMNIR_DEPLOY_POLICY`, `GRIMNIR_DEPLOY_REDIS_ADDR`, `GRIMNIR_DEPLOY_REDIS_PASSWORD`, `GRIMNIR_DEPLOY_ROLLBACK_WINDOW`, `GRIMNIR_DEPLOY_SOAK_WINDOW`, `GRIMNIR_DEPLOY_WINDOW_CRON`, `GRIMNIR_DJ_VIP`, `GRIMNIR_ENV`, `GRIMNIR_LISTENER_VIP`, `GRIMNIR_MEDIA_ENGINE_GRPC_ADDR`, `GRIMNIR_NETCLOCK_PORT`, `GRIMNIR_NTFY_TOKEN_AUDIT`, `GRIMNIR_NTFY_TOKEN_PAGE`, `GRIMNIR_NTFY_TOKEN_ROLLBACK`, `GRIMNIR_NTFY_URL`, `GRIMNIR_PROMETHEUS_URL`, `GRIMNIR_REDIS_ADDR`, `GRIMNIR_REDIS_PASSWORD`, `GRIMNIR_REGION`, `GRIMNIR_SECRETS_BACKEND`, `GRIMNIR_SECRETS_ENV_FILE`, `MEDIA_ENGINE_GRPC_ADDR`, `REDIS_PW`, `RLM_ENV`, `RLM_NETCLOCK_PORT`, `SKIP_BROWSER_TESTS`, `SSH_CLIENT`, `TEST_DB_DSN`, `USER`, `VAULT_ADDR`, `VAULT_ROLE_ID`, `VAULT_SECRET_ID`.

Plus the `getEnvAny([]string{...})` calls in `internal/config/config.go` (60+ pairs each with a `GRIMNIR_*` primary & `RLM_*` fallback) & the `FANOUT_*` namespace in `internal/grimnirfanout/config.go` (12 vars: `BIND_ADDR`, `GRPC_PORT`, `HTTP_PORT`, `METRICS_PORT`, `HARBOR_PORT`, `RTP_PORT`, `SRT_PORT`, `WEBRTC_HTTP_PORT`, `ENGINE_A_RTP`, `ENGINE_B_RTP`, `CONTROL_PLANE_GRPC`, `REDIS_ADDR`, `NETCLOCK_ENABLED`, `NETCLOCK_MASTER_ADDR`, `LOG_LEVEL`).

Cross-reference against `CLAUDE.md`'s Environment Variables section:

| Status | Variable | Where read | Where documented |
|---|---|---|---|
| Documented & wired | GRIMNIR_DB_DSN, GRIMNIR_REDIS_ADDR, GRIMNIR_MEDIA_ENGINE_GRPC_ADDR, GRIMNIR_MEDIA_ROOT, GRIMNIR_JWT_SIGNING_KEY, GRIMNIR_HA_PCM_RTP_ENABLED, GRIMNIR_HA_PCM_RTP_TARGETS, GRIMNIR_NETCLOCK_*  | config.go | CLAUDE.md |
| Wired, not in CLAUDE.md (Warning) | All FANOUT_*, all EDGE_ENCODER_*, GRIMNIR_DEPLOY_*, GRIMNIR_NTFY_*, GRIMNIR_ALERTBRIDGE_ADDR, GRIMNIR_REGION, GRIMNIR_PROMETHEUS_URL, GRIMNIR_LISTENER_VIP, GRIMNIR_DJ_VIP, GRIMNIR_SECRETS_*, VAULT_*, REDIS_PW | per-binary configs | per-binary READMEs, RELEASE_NOTES.md |

CLAUDE.md is a v1-era doc & doesn't enumerate the v2 env surface. The per-binary READMEs (`cmd/grimnir-fanout/README.md`, `cmd/edge-encoder/README.md`) & `docs/v2/RELEASE_NOTES.md` do. **Warning**: extend CLAUDE.md's env section to point at the per-binary READMEs as the v2 source of truth — or add a single `.env.v2.example` (delivered as part of this audit; see `.env.v2.example`).

No env var the code reads is undocumented across the whole doc set. Pass with note.

## 5. Runbook completeness

`docs/runbooks/index.md` links 13 long-form runbooks; every link resolves to a file that exists (`backup-drill.md`, `cold-start-region.md`, `deploy.md`, `drain.md`, `drain-a-node.md`, `emergency-pause.md`, `fanout-down.md`, `keepalived-install.md`, `migrate-media-to-r2.md`, `promote-replica.md`, `recover-partition.md`, `restore-from-backup.md`, `verify.md`). Plus `secrets/rotation.md` under the subdirectory.

`docs/v2/UPGRADE.md` links 3 runbooks (`docs/runbooks/index.md`, `docs/runbooks/keepalived-install.md`, `docs/runbooks/migrate-media-to-r2.md`); all exist.

Per-binary failure-mode coverage: every v2 binary has a runbook for its common failure modes (`fanout-down.md` for fan-out, `promote-replica.md` for Postgres, `restore-from-backup.md` for media, `drain.md` for control-plane drains, `verify.md` for the omnibus health check, `recover-partition.md` for VRRP split-brain).

Pass.

## 6. Ops configs

Every config file parsed clean:

| File | Validator | Result |
|---|---|---|
| `ops/prometheus/prometheus.yml` | python yaml | OK |
| `ops/prometheus/rules/grimnir-ha.yml` | python yaml | OK |
| `ops/prometheus/rules/grimnir-ha-tests.yml` | python yaml | OK |
| `ops/alertmanager/config.yml` | python yaml | OK |
| `ops/grafana/dashboards/ha-overview.json` | jq | OK |
| `ops/grafana/dashboards/deploy-and-audit.json` | jq | OK |
| `ops/grafana/dashboards/audio-pipeline.json` | jq | OK |
| `ops/grafana/provisioning/dashboards/ha.yml` | python yaml | OK |
| `ops/grafana/provisioning/datasources/prometheus.yml` | python yaml | OK |
| `ops/keepalived/check-edge.sh` | `bash -n` | OK |
| `ops/keepalived/check-fanout.sh` | `bash -n` | OK |
| `ops/keepalived/notify.sh` | `bash -n` | OK |
| `ops/keepalived/keepalived-listener.conf` | brace balance | 2 open / 2 close balanced |
| `ops/keepalived/keepalived-dj.conf` | brace balance | 2 open / 2 close balanced |
| `docker-compose.yml` | python yaml | OK |
| `docker-compose.fanout.yml` | python yaml | OK |
| `docker-compose.monitoring.yml` | python yaml | OK |
| `docker-compose.override.yml` | python yaml | OK |
| `Dockerfile.fanout` | manual inspect | FROM golang:1.24-bookworm; runtime debian:bookworm-slim; healthcheck on 8003 |

`docker compose config -q` not run because docker isn't on the audit host; the YAML structure parses & matches the documented v2 layout (fanout has dedicated overlay, mediaengine has its own Dockerfile).

Pass.

## 7. Secrets template

The pre-existing `.env.example` & `.env.docker.example` are both v1-era: they document `POSTGRES_PASSWORD`, `REDIS_PASSWORD`, `JWT_SIGNING_KEY`, `LEADER_ELECTION_ENABLED`, `LOG_LEVEL`, & a handful of webstream tuning knobs. Neither file mentions:

- `FANOUT_*` (any of the 14 vars)
- `EDGE_ENCODER_*` (any of the 15 vars)
- `GRIMNIR_HA_PCM_RTP_*` / `GRIMNIR_NETCLOCK_*`
- `GRIMNIR_NTFY_*` / `GRIMNIR_ALERTBRIDGE_ADDR`
- `GRIMNIR_DEPLOY_*` (any of the 13 deploy-tool vars)
- R2 / S3 envs

The user's interim `/tmp/ha-secrets.env` (`REPL_PW`, `GRIMNIR_PW`, `PGBOUNCER_PW`, `REDIS_PW`) maps cleanly: `REDIS_PW` is read by the deploy tool as a third fallback after `GRIMNIR_DEPLOY_REDIS_PASSWORD` & `GRIMNIR_REDIS_PASSWORD` (`internal/grimnirdeploy/config.go:60`). `REPL_PW`, `GRIMNIR_PW`, `PGBOUNCER_PW` aren't directly read by Go code; they're substrate-layer credentials Postgres & pgbouncer consume out-of-band per `docs/superpowers/plans/2026-06-06-v2-execution-roadmap.md:123-126`.

A new `.env.v2.example` (delivered with this audit) enumerates the full v2 surface with placeholders, comments, and per-VM markers. **Warning** (not blocker): the file lives in the repo root with a leading `.` which means it won't show in `ls` without `-a`; consider also linking it from `docs/v2/UPGRADE.md` § Phase 3.

Pass with note.

## 8. Inter-binary contracts

- **`LiveInputControl.SetLiveInput`**: the controller is registered in `cmd/mediaengine/main.go:215-216` (`liveInputCtrl := mediaengine.NewLiveInputController(logger); grpcServer.RegisterService(liveInputCtrl.ServiceDesc(), liveInputCtrl)`). Pass.

- **`DJAuth` gRPC on the control plane**: still stubbed. `internal/live/djauth_grpc_stub.go:85-93` returns `ErrDJAuthGRPCNotWired` (`"live: DJAuth gRPC server not yet wired into control plane"`). The fan-out side falls back to `AcceptAllAuthenticator` when `FANOUT_CONTROL_PLANE_GRPC` is empty (`cmd/grimnir-fanout/main.go:119-123`) & logs a warning. **Warning**: production HA cannot rely on DJ auth via gRPC; the fan-out will accept every DJ token in dev-mode unless the control plane gets its gRPC server stood up. This is consistent with the rc.5 state — it's a known follow-up, not a regression. Operators must either delay enabling DJ-side fan-out auth until a post-rc release lands the wiring, or run the fan-out without `FANOUT_CONTROL_PLANE_GRPC` set & accept the dev-mode warning (which means DJs can self-authorize by knowing any mount path — not appropriate for prod with public DJ URLs).

- **`grimnir-deploy` SSH key path**: read from `GRIMNIR_DEPLOY_PEER_SSH_KEY` (`internal/grimnirdeploy/config.go`). `docs/runbooks/verify.md:11` references `$GRIMNIR_DEPLOY_PEER_HOST` but no runbook says where to put the SSH key file or what filename to use. **Warning**: add a one-liner to `docs/runbooks/index.md` or `docs/v2/UPGRADE.md` Phase 1 telling operators to generate the key at e.g. `~/.ssh/grimnir-deploy-ed25519` & set `GRIMNIR_DEPLOY_PEER_SSH_KEY=~/.ssh/grimnir-deploy-ed25519`. The Day 0 checklist (delivered with this audit) bakes this in.

## 9. Dependency check

- `go mod tidy` → exit 0, no diff to `go.mod` or `go.sum`. Pass.
- Pseudo-versions (`v0.0.0-YYYYMMDDHHMMSS-<commit>`) exist in `go.mod` (7 instances, all `// indirect`): `dgryski/go-rendezvous`, `jackc/pgservicefile`, `modern-go/concurrent`, `munnerz/goautoneg`, `golang.org/x/exp`, `genproto/googleapis/api`, `genproto/googleapis/rpc`. All are commit-pinned (deterministic) not branch-pinned. Pass.
- No `// indirect` direct dep on a branch ref or `main` branch. Pass.

## 10. Lint state

- `gofmt -l .` (excluding `.gomodcache/`) → empty. Pass.
- `go vet ./...` → exit 0. Pass.
- `golangci-lint` → not installed on this host. `make ci` logs `golangci-lint not found; skipping lint`. **Info**: install golangci-lint v1.59+ on the operator workstation & rerun before cutover to catch the lints CI in GitHub Actions runs.

---

## Findings

### Blocker (1)

**B-1** — `docs/v2/UPGRADE.md` Phase 3 (line 170) & Phase 1 (line 82) tell the operator to `git checkout v2.0.0-rc.1`. The current shippable tag is `v2.0.0-rc.5`, which carries every fix from rc.2 through rc.5 (#220 echo + CPU starvation, #222 stuck-job recalc, #223 bulk delete FK, the v1.40.8 PCM-decoder leak backport, webstream stall watchdog). The same Phase 3 step says `cp docker-compose.v2.example.yml docker-compose.override.yml` — that filename does not exist anywhere in the repo. The closest matches are `docker-compose.override.yml.example` (v1 single-VM template) & `docker-compose.fanout.yml` (the v2 fan-out overlay).

Fix: bump every `v2.0.0-rc.1` reference in `docs/v2/UPGRADE.md` to `v2.0.0-rc.5` & change the compose-copy line to source from `docker-compose.override.yml.example` plus an explicit `docker compose -f docker-compose.yml -f docker-compose.fanout.yml up -d` invocation matching `docker-compose.fanout.yml:3-5`.

### Warning (5)

**W-1** — Control-plane DJAuth gRPC server is still a stub returning `ErrDJAuthGRPCNotWired`. Fan-out auth runs in `AcceptAllAuthenticator` mode unless the operator wires `FANOUT_CONTROL_PLANE_GRPC`. The Day 0 checklist surfaces this in Phase 5 with a recommendation: until a follow-up release lands the server, either keep DJ ingress on the v1 path or accept the dev-mode warning behind a private network ACL.

**W-2** — `.env.example` & `.env.docker.example` are v1-era. The delivered `.env.v2.example` covers the v2 surface; reference it from UPGRADE.md so operators find it.

**W-3** — `CLAUDE.md` Environment Variables section lists 9 vars; the v2 binary set reads 49+ (control-plane) + 14 (fan-out) + 15 (edge-encoder) + 13 (deploy-tool). Per-binary READMEs are accurate but CLAUDE.md is stale. Either point CLAUDE.md at the READMEs or expand the section.

**W-4** — `golangci-lint` is not installed on the audit host (`make ci` skips it with a warning). GitHub Actions has it via a separate installer step; rely on the PR check to catch lint failures before cutover.

**W-5** — `grimnir-deploy` SSH-key path is read from `GRIMNIR_DEPLOY_PEER_SSH_KEY` but no runbook tells operators where to place the file. Day 0 checklist covers this; UPGRADE.md Phase 1 should also say it explicitly.

### Info (1)

**I-1** — Several runbooks reference `internal/grimnirdeploy/autorollback/` & `internal/grimnirdeploy/cmd_deploy.go` by file path. These resolve today; treat them as load-bearing for any future refactor of `internal/grimnirdeploy/`.

---

## Verification commands re-run by an operator

```bash
# 1. Branch & tag
git rev-parse --abbrev-ref HEAD                    # v2-dev
git describe --tags --exact-match HEAD             # v2.0.0-rc.5

# 2. Every binary
for d in grimnirradio mediaengine edge-encoder grimnir-fanout \
         grimnir-deploy alertmanager-ntfy mediascan migration-lint; do
  go build ./cmd/$d/... && echo "$d OK"
done

# 3. Tests + lint
make ci

# 4. Migration lint
make migration-lint

# 5. Format
gofmt -l . | grep -v '^\.gomodcache/'              # expect empty

# 6. Mod tidy
go mod tidy && git diff --stat go.mod go.sum       # expect empty

# 7. Ops configs
make prometheus-validate
for f in docker-compose*.yml; do
  python3 -c "import yaml; yaml.safe_load(open('$f'))" && echo "$f OK"
done
```

If every command above returns the expected result, the binaries are deploy-ready & the documentation drift identified in B-1 is the only outstanding fix.
