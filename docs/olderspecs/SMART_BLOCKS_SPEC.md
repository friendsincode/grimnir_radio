# [Archived] Grimnir Radio Smart Blocks Specification

**Audience:** engineers who will build Grimnir Radio, a Go based radio suite that targets Icecast and Shoutcast without Liquidsoap.  
**Goal:** deliver a minimal viable product for Grimnir Radio that outperforms LibreTime and AzuraCast in scheduling, live handover, and sound consistency.  
**Non goals:** no Liquidsoap, no DSL, no framework heavy frontend. Bootstrap 5 with plain HTML and small JavaScript is acceptable.

## Core pitch

Smart Blocks are rule driven and clock aware. They create a deterministic playout plan with exact cue points and loudness targets. Rules read like sentences in the UI. The engine is written in Go. Playback uses GStreamer. Output targets Icecast or Shoutcast. Formats include MP3, Opus, Vorbis, and optional AAC when legally available on the host system.

## Architecture overview

* Go control plane with HTTP JSON API and WebSocket events
* GStreamer based playout pipelines per mount
* PostgreSQL or MySQL or SQLite backing store
* Object storage for media
* Optional WebRTC ingest for live input
* RBAC for admin, manager, and DJ roles

### Main processes

* Scheduler builds a rolling playout plan from clocks and Smart Blocks
* Analyzer scans media on import and writes loudness and cue points
* Playout engine executes the plan, handles crossfades and live input
* Event bus sends now playing and health events to the UI and hooks

## Smart Blocks design

A Smart Block is a bundle of include and exclude rules, quotas, separation, diversity preferences, and a sequence policy. Given a seed and a target duration, it produces an ordered set of items with cue points and an energy profile.

### Rule types

* Include rules. Example include genre Country or Mood Chill or BPM from 85 to 100 or Year from 2010 to 2020
* Exclude rules. Example exclude Explicit or exclude tag Christmas
* Weights to nudge a tag or field without hard enforcement
* Quotas. Example at least one new release per 15 minutes or exactly two Gold per hour or no more than one Live per 15 minutes
* Separation windows for artist, title, album, and label with time based limits
* Diversity preferences such as avoid adjacent same tempo bucket or alternate language when possible
* Hard inserts and stopsets that act as anchors inside the block
* Duration target and tolerance with a strategy for underfill or overfill
* Fallbacks in defined order when quotas cannot be met

### Determinism and variety

* Seeded selection provides repeatable output for a given seed
* Rolling memory records recent plays per station and per mount
* Beam search with small backtracking prevents dead ends
* Look ahead scoring penalizes choices that would block remaining quotas

### Energy and flow

* Tracks are ranked by tempo, mood, and perceived energy
* Sequence policy selects a curve such as build then release or wave
* Avoid back to back down tempo unless allowed by policy
* Respect intro and outro cue points for clean transitions

### Hard elements and stopsets

* Legal ID at top of hour
* Imaging sweepers and promos at fixed offsets
* News bed or traffic bed where needed
* The scheduler reserves time slices for these and the block fills around them

## Scheduling and clocks

A clock grid describes one hour as a list of slots. Slots can be hard elements or Smart Blocks. The scheduler compiles clocks and rules into a playout plan. The plan covers a rolling window such as 48 hours and is refreshed continuously.

Ghost-originated content can be mapped into program calendars by tagging articles with schedule metadata and embedded audio references; the system translates these into Smart Blocks or hard elements when webhooks fire.

### Example clock intent

* 00:00 legal ID five seconds
* 00:05 stager three seconds
* 00:08 Smart Block Country Hour part one
* 30:00 promo thirty seconds
* 31:00 Smart Block Country Hour part two with optional voice-tracked break
* 58:30 imaging ten seconds
* 58:40 stopset for ads or PSAs with affidavits generated post-air
* 59:55 hard time marker to align to next hour

## Live input handover

* Priority ladder: Emergency followed by Live DJ followed by Scheduled followed by Fallback
* Live input from Icecast source or Shoutcast source or WebRTC
* Remote relay slots and automated fallback routing between redundant streams
* Handover crossfades with RMS matched levels and quick ducking of the outgoing bed

## Audio and DSP standards

* EBU R128 integrated loudness at import and enforced at playout
* ReplayGain compatible values written to the database
* Silence trimming for heads and tails
* Cue points for intro end and outro start
* Transition types per category for consistent station sound

## API surface

* Media: upload signed URL, metadata read and write, waveform and analysis read
* Smart Blocks: create rulesets, dry run, materialize sequence
* Clocks and schedule: CRUD with validation and simulation
* Live: authorize source, connect and disconnect events, force handover
* Playout control: reload pipeline, skip, start, stop
* Station and mount: create, encoder presets, formats, and bit rates
* Analytics: now playing, spin counts, exposure by category, listener trends
* Webhooks: on track start, on DJ connect, and pipeline health change

## Storage model summary

See the SQL files for details. Tables include users, roles, stations, mounts, media, tags, playlists, rulesets, clocks, schedules, plays, encoder presets, analysis jobs, and quotas memory. JSON capable fields store rule definitions and clock shapes. Indexes support fast tag queries and recent play separation checks.

## Security

* JWT based auth with OIDC integration
* RBAC for admin, manager, and DJ
* All writes authenticated, reads limited by role and station ownership

## Frontend

* Plain HTML with Bootstrap 5 and small JavaScript
* Rule builder that reads like sentences
* Timeline preview that shows levels and cue points
* Reroll action that changes the seed and regenerates the block
* No framework lock in

## Telemetry and logs

* Structured logs with request and job IDs
* Metrics for queue depth, schedule health, and pipeline status
* Aircheck recording per show stored as FLAC or Opus

## Compliance and reports

* Spin counts by artist, title, and category
* SoundExchange ready exports where applicable
* Track history for audits

## Should haves

* WebDJ studio in the browser with mic capture, carts, cue bus switching, and library search
* Voice tracking workflow for pre-recorded breaks with auto-leveling and quick publishing
* Program calendar with recurring shows, seasonal overrides, and time zone awareness
* Watch folders, bulk metadata editing, and remote upload tooling for large media libraries
* Listener experience features such as request queue, on-demand episode downloads, and podcast feeds
* Compliance exports beyond spins: show affidavits, royalty splits, SOCAN/PRS-ready reports
* Multi-station tenancy with per-station resource quotas and isolation
* Remote relay ingest, failover routing, and automatic fallback chains for live sources
* Extensible notifications (webhooks, Slack/Matrix, traffic system integrations)
* High-availability options: hot-standby playout nodes, redundant encoders, disaster recovery sync
* Ghost CMS integration for auto-importing published articles with embedded audio and schedule metadata

## Open questions

* Do we standardize on CockroachDB for horizontal scale or allow all three engines
* Which features qualify for the first WebDJ release and what hardware assumptions apply
* How far do we go with hosted multi-tenant isolation in the near term
