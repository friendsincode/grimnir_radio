-- Migration 003: Persist smart-block regeneration counter
-- Created: 2026-06-02
-- Phase: expand
--
-- Description: Adds the smart_block_generations table that backs
-- Director.sbGeneration. The counter increments on each smart-block sequence
-- exhaustion (handleTrackEnded) and is read on every startSmartBlockEntry so
-- the shuffle seed survives a director restart. Two control planes serving
-- the same schedule rely on this counter staying in sync to produce
-- byte-identical gRPC commands. See docs/audits/2026-06-06-executor-determinism.md
-- finding C9.
--
-- Expand-only: a new table; nothing dropped, renamed, or retyped.

-- ============================================================================
-- SQL BELOW
-- ============================================================================

CREATE TABLE IF NOT EXISTS smart_block_generations (
    entry_id          VARCHAR(64) NOT NULL,
    occurrence_start  TIMESTAMPTZ NOT NULL,
    generation        INTEGER     NOT NULL DEFAULT 0,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (entry_id, occurrence_start)
);

CREATE INDEX IF NOT EXISTS idx_smart_block_generations_updated_at
    ON smart_block_generations (updated_at);
