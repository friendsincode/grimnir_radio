-- Migration 005: audit_log table for grimnir-deploy
-- Created: 2026-06-06
-- Phase: expand
--
-- Description: Adds the audit_log table written by every grimnir-deploy
-- subcommand per Section 8.3 of the HA design. Schema is verbatim from the
-- design doc. Rows are written on START and on COMPLETE/FAILED of every
-- subcommand; the (subcommand, phase) tuple lets dashboards filter for
-- in-progress operations vs completed history.
--
-- This table is distinct from the existing audit_logs table (plural, used by
-- the control plane for app-level audit events like priority changes).
-- The two stores share a Postgres instance for backup convenience but no
-- code in common.
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

CREATE TABLE IF NOT EXISTS audit_log (
    id              uuid PRIMARY KEY,
    ts              timestamptz NOT NULL DEFAULT now(),
    operator        text NOT NULL,
    source_ip       text NOT NULL,
    subcommand      text NOT NULL,
    args_json       jsonb NOT NULL,
    phase           text NOT NULL,
    outcome         text,
    duration_ms     bigint,
    notes           text
);

CREATE INDEX IF NOT EXISTS idx_audit_log_ts ON audit_log (ts DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_subcommand_ts ON audit_log (subcommand, ts DESC);
