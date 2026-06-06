-- Migration 004: PlayHistory idempotency for HA lockstep
-- Created: 2026-06-02
-- Phase: expand
--
-- Description: Adds entry_id + position columns to play_histories (via GORM
-- AutoMigrate) and a partial unique index over (entry_id, position, started_at)
-- so two grimnir control planes writing the same logical play to the same DB
-- produce exactly one row. The second writer hits ON CONFLICT DO NOTHING.
--
-- See internal/playout/director.go::recordPlayHistory and issue #239. The
-- equivalent Go-side helper in internal/db/migrate.go::applyPlayHistoryUniqueIndex
-- runs on every startup; this SQL file is the canonical record for operators
-- running migrations out-of-band.
--
-- The WHERE entry_id <> '' clause means existing rows that pre-date the
-- entry_id column (where the GORM default is empty string) do not violate the
-- constraint. New writes from director.go always populate entry_id.
--
-- Expand-only: additive index. Nothing dropped, renamed, or retyped.

-- ============================================================================
-- SQL BELOW
-- ============================================================================

CREATE UNIQUE INDEX IF NOT EXISTS uq_play_history_entry_position_started
ON play_histories (entry_id, position, started_at)
WHERE entry_id <> '';
