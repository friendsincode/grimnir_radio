-- Migration 006: deploy_history table for grimnir-deploy
-- Created: 2026-06-06
-- Phase: expand
--
-- Description: Adds the deploy_history table per Section 6 of the HA design.
-- One row per grimnir-deploy invocation; outcome column captures success,
-- rolled_back_mid_roll, rollback, soak_failed, or failed. Used by the
-- --rollback flag to find the previous successful tag and to detect
-- contract-migration crossings.
--
-- This table is distinct from audit_log (Migration 005): audit_log records
-- every subcommand start/complete/fail event for auditing; deploy_history
-- records the higher-level "we deployed tag X to region Y" lifecycle that
-- rollback eligibility queries against.
--
-- The equivalent Go-side AutoMigrate registration in internal/db/migrate.go
-- runs on every startup; this SQL file is the canonical record for operators
-- running migrations out-of-band.
--
-- Expand-only: additive table + two read indexes. Nothing dropped, renamed,
-- or retyped.

-- ============================================================================
-- SQL BELOW
-- ============================================================================

CREATE TABLE IF NOT EXISTS deploy_history (
    id              uuid PRIMARY KEY,
    region          text NOT NULL,
    tag             text NOT NULL,
    previous_tag    text,
    started_at      timestamptz NOT NULL DEFAULT now(),
    completed_at    timestamptz,
    operator        text NOT NULL,
    outcome         text,
    reason          text,
    soak_outcome    text,
    failure_log     text
);

CREATE INDEX IF NOT EXISTS idx_deploy_history_region_started ON deploy_history (region, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_deploy_history_outcome ON deploy_history (outcome, started_at DESC);
