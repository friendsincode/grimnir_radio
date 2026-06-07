-- Migration 007: station_streams table for the custom JS listener player
-- Created: 2026-06-06
-- Phase: expand
--
-- Description: Adds the station_streams table consumed by the custom JS
-- player (Track B-3, docs/superpowers/plans/2026-06-06-custom-js-player.md).
-- One row per listener-facing audio URL a station advertises (HQ, LQ,
-- future HLS). The browser player fetches the ordered list at page load
-- from GET /api/v1/stations/<id>/streams & walks HQ -> LQ on failure.
--
-- Distinct from the existing mounts table: mounts describe the encoder
-- pipeline output (format, bitrate, channels, sample_rate, preset) the
-- media engine produces. station_streams describes the public URL a
-- listener's browser dials plus the human label ("HQ" / "LQ") shown in
-- the UI. A future CDN-fronted URL or HLS endpoint can be advertised
-- without touching the mount that produced the bytes.
--
-- The Go-side AutoMigrate registration in internal/db/migrate.go
-- creates the same table on every startup; this SQL file is the
-- canonical record for operators running migrations out-of-band.
--
-- Expand-only: additive table + two read indexes. Nothing dropped,
-- renamed, or retyped. Safe to deploy alongside v(N-1) of the control
-- plane (it simply ignores the new table).

-- ============================================================================
-- SQL BELOW
-- ============================================================================

CREATE TABLE IF NOT EXISTS station_streams (
    id            uuid PRIMARY KEY,
    station_id    uuid NOT NULL,
    url           varchar(512) NOT NULL,
    format        varchar(16)  NOT NULL,
    bitrate_kbps  integer      NOT NULL DEFAULT 0,
    label         varchar(32)  NOT NULL,
    priority      integer      NOT NULL DEFAULT 0,
    created_at    timestamptz  NOT NULL DEFAULT now(),
    updated_at    timestamptz  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_station_streams_station_priority
    ON station_streams (station_id, priority ASC);

CREATE INDEX IF NOT EXISTS idx_station_streams_priority
    ON station_streams (priority);
