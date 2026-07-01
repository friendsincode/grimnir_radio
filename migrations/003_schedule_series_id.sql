-- Schedule series identity
-- Migration: 003_schedule_series_id.sql
-- Created: 2026-07-01
-- Description: Adds a stable series id to schedule_entries so "edit all
--   occurrences" can span every segment of a recurring show. Before this,
--   "this and all following" edits split a series into standalone recurring
--   rows with no link back, so nothing tied the segments together and a later
--   "all" edit only touched one segment.
--
-- The control plane applies this automatically at boot: GORM AutoMigrate adds
-- the column and backfillScheduleSeriesID (internal/db/migrate.go) fills it.
-- This file mirrors that for operators who manage schema by hand. Additive
-- only; no column is dropped or renamed.

-- ============================================================================
-- SCHEDULE_ENTRIES TABLE
-- ============================================================================

ALTER TABLE schedule_entries ADD COLUMN IF NOT EXISTS series_id uuid;

CREATE INDEX IF NOT EXISTS idx_schedule_entries_series_id
ON schedule_entries(series_id);

-- Backfill (conservative: each recurring root is its own series, overrides
-- inherit their parent's series; historically-split segments are NOT merged).
UPDATE schedule_entries SET series_id = id
WHERE series_id IS NULL AND is_instance = false;

UPDATE schedule_entries SET series_id = recurrence_parent_id
WHERE series_id IS NULL AND recurrence_parent_id IS NOT NULL;

UPDATE schedule_entries SET series_id = id
WHERE series_id IS NULL;
