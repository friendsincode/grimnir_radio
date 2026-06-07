-- Migration 008: listener_events table for anonymous JS-player telemetry
-- Created: 2026-06-06
-- Phase: expand
--
-- Description: Adds the listener_events table written to by the custom JS
-- player (Track B-3, docs/superpowers/plans/2026-06-06-custom-js-player.md,
-- Chunk 5). The player POSTs reconnect / degrade / upgrade / exhausted /
-- play / stop transitions to /api/v1/listener-events. Operators read
-- aggregate counts on the B-4 dashboards (Chunk 9, deferred).
--
-- No PII. The handler reads the request socket IP for rate limiting only;
-- it is NOT stored on a row. Columns are event-type counts + station_id +
-- stream_label; duration_ms is reconnect-recovery time in ms (nullable).
--
-- The Go-side AutoMigrate registration in internal/db/migrate.go creates
-- the same table on every startup; this SQL file is the canonical record
-- for operators running migrations out-of-band.
--
-- Expand-only: additive table + two read indexes. Nothing dropped,
-- renamed, or retyped. Safe to deploy alongside v(N-1) of the control
-- plane (it simply ignores the new table).

-- ============================================================================
-- SQL BELOW
-- ============================================================================

CREATE TABLE IF NOT EXISTS listener_events (
    id           uuid PRIMARY KEY,
    timestamp    timestamptz  NOT NULL,
    event_type   varchar(16)  NOT NULL,
    station_id   uuid         NOT NULL,
    stream_label varchar(32)  NOT NULL,
    duration_ms  integer,
    created_at   timestamptz  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_listener_events_station_timestamp
    ON listener_events (station_id, timestamp DESC);

CREATE INDEX IF NOT EXISTS idx_listener_events_event_type_timestamp
    ON listener_events (event_type, timestamp DESC);
