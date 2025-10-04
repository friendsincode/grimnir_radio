# Grimnir Radio — Sales Spec

## One-Liner
Modern, deterministic radio automation and playout that delivers consistent sound, easy live handover, and reliable scheduling without Liquidsoap complexity.

## Target Customers
- Independent and community radio stations upgrading from manual or legacy tools
- Campus and low-power FM/online stations that need modern scheduling
- Content networks syndicating shows across multiple stations/mounts
- Streaming-only stations that care about sound consistency and uptime

## Problems We Solve
- Inconsistent loudness and messy transitions harm listener experience
- Fragile live handovers derail shows and remote talent workflows
- Unpredictable scheduling and DJ tools slow programming teams
- Complex, monolithic solutions are hard to run, extend, or debug

## Key Differentiators
- Deterministic Smart Blocks: clock-aware, rule-driven programming with predictable results
- Seamless Live Handover: priority ladder (Emergency > Live DJ > Scheduled > Fallback) with crossfades
- Consistent Sound: loudness analysis at ingest and enforcement at playout
- Simple Control Plane: Go-based monolith with clean HTTP+WebSocket API
- Cloud or On-Prem: SQLite for small, Postgres/MySQL for production

## Core Capabilities
- Scheduling: 48h rolling window with clocks, stopsets, legal IDs, promos
- Smart Blocks: rules, quotas, separation windows, energy curves, dry-runs
- Playout: GStreamer pipelines with cue-aware crossfades and normalization
- Live: WebRTC/Icecast/Shoutcast ingest, remote relay with failover
- Webstreams: schedule and relay remote HTTP/ICY streams with health checks and graceful fallback, including backup URL chains (e.g., alternate Icecast sessions) when the primary talent fails to connect
- Media: analysis (loudness, intro/outro cues), waveform previews
- Telemetry: structured logs, metrics, and health endpoints

## Packaging & Deployment
- Single binary: `cmd/grimnirradio` (Go 1.22+)
- Databases: Postgres (preferred), MySQL, SQLite (dev/single node)
- Object storage: S3-compatible or local filesystem for media
- Linux first; runs on modern x86_64 and ARM64

## Migration Path
- Supported sources: AzuraCast and LibreTime backups
- Zero/low-downtime approach: per-mount cutover using relays/webstreams; validate on staging mount before switch
- What migrates: stations, mounts, media metadata, playlists, basic schedules, and DJ accounts (where available)
- Guided import: wizard or CLI to point at backup archive, preview mappings, and run a dry-run before applying
- Fallback: if a construct cannot map 1:1 (e.g., complex playlist rules), importer flags it for manual review

### Same-Server Takeover (Stupid-Easy Mode)
- Detect existing AzuraCast/LibreTime install on the host (containers/services, media and config paths)
- One-click (or single CLI) migration: stop source services, import DB, move media and configs, auto-map stations/mounts
- Preserve ports and endpoints where possible; verify with a staging mount, then flip traffic
- Rollback plan: re-enable source services instantly if needed

### Cross-Server Migration (API-Driven)
- Connect to remote AzuraCast/LibreTime using API/credentials
- Enumerate and import stations, mounts, users, media metadata, playlists, and schedules
- Sync media via rsync/S3/HTTP; incremental until cutover
- Finalize with a per-mount switch and webstream relay fallback

## Success Metrics / ROI
- >= 99.9% playout uptime for a single node
- Loudness drift < ±1 LU across entire hour
- < 5 minutes to schedule an hour with Smart Blocks
- < 3 seconds mean live handover time without underruns

## Competitive Positioning
- Versus LibreTime/AzuraCast: deterministic scheduling, easier live handover, simpler extension points
- Versus custom Liquidsoap stacks: less glue code, clearer APIs, simpler ops

## Objections & Responses
- “We already run XYZ”: integrate via webhooks and relay inputs; migrate incrementally by mount
- “Is Go OK for audio?”: GStreamer pipelines keep DSP battle-tested; Go covers control, orchestration, and IO
- “Will our DJs learn it?”: rules read like sentences; WebDJ path enables gradual adoption

## Security & Compliance Highlights
- JWT auth with OIDC integration
- Role-based access (admin/manager/DJ)
- Airchecks, spin counts, and report-friendly exports

## Buying Path / Next Steps
- Pilot: 2 weeks with one station and one mount
- Deliverables: schedule coverage, live handover demo, audio consistency report
- Follow-on: multi-station rollout, WebDJ enablement, reporting integrations
