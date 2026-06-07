-- Migration 009: playout_queue_decisions table for leader/follower queue parity
-- Created: 2026-06-06
-- Phase: expand
--
-- Description: Fixes issue #240. Under HA lockstep two control planes both
-- call popNextQueuedMedia after a track ends; only the leader wins the
-- SELECT ... FOR UPDATE row lock on playout_queue_items, & the follower
-- silently falls back to a random pick. Listeners on the follower's media
-- engine never hear the user-queued track.
--
-- Resolution (option 1 from #240): the leader pops as before & writes a
-- short-lived decision row here; the follower reads the most-recent row for
-- the (station_id, mount_id) within the 15-second TTL & plays the same
-- media. Rows are swept by a background goroutine in the playout director.
--
-- The Go-side AutoMigrate registration in internal/db/migrate.go creates
-- the same table on every startup; this SQL file is the canonical record
-- for operators running migrations out-of-band.
--
-- Expand-only: additive table + read index. Nothing dropped, renamed, or
-- retyped. Safe to deploy alongside v(N-1) of the control plane (it simply
-- ignores the new table).

-- ============================================================================
-- SQL BELOW
-- ============================================================================

CREATE TABLE IF NOT EXISTS playout_queue_decisions (
    id                   uuid PRIMARY KEY,
    station_id           uuid         NOT NULL,
    mount_id             uuid         NOT NULL,
    media_id             uuid         NOT NULL,
    source_queue_item_id uuid         NOT NULL,
    decided_at           timestamptz  NOT NULL,
    expires_at           timestamptz  NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_playout_queue_decisions_station_decided
    ON playout_queue_decisions (station_id, mount_id, decided_at DESC);

CREATE INDEX IF NOT EXISTS idx_playout_queue_decisions_expires_at
    ON playout_queue_decisions (expires_at);
