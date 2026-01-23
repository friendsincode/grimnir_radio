-- Performance Optimization: Add indexes for common query patterns
-- Migration: 001_add_performance_indexes.sql
-- Created: 2026-01-22
-- Description: Adds indexes to improve query performance based on actual usage patterns

-- ============================================================================
-- STATIONS TABLE
-- ============================================================================

-- Index for active station lookups
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_stations_active
ON stations(active)
WHERE active = true;

-- ============================================================================
-- MEDIA_ITEMS TABLE
-- ============================================================================

-- Composite index for station + analysis state (smart block queries)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_media_station_analysis
ON media_items(station_id, analysis_state)
WHERE analysis_state = 'complete';

-- Index for station + active media
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_media_station_active
ON media_items(station_id, active)
WHERE active = true;

-- Index for artist separation queries
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_media_artist
ON media_items(artist);

-- Index for album queries
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_media_album
ON media_items(album);

-- Full-text search index for media metadata
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_media_title_trgm
ON media_items USING gin(title gin_trgm_ops);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_media_artist_trgm
ON media_items USING gin(artist gin_trgm_ops);

-- ============================================================================
-- SCHEDULE_ENTRIES TABLE
-- ============================================================================

-- Composite index for station + time range queries (most critical for scheduler)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_schedule_station_time
ON schedule_entries(station_id, starts_at);

-- Index for finding entries ending soon
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_schedule_station_ends
ON schedule_entries(station_id, ends_at);

-- Index for mount-specific queries
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_schedule_mount_time
ON schedule_entries(mount_id, starts_at);

-- Index for source type filtering
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_schedule_source_type
ON schedule_entries(source_type, station_id);

-- ============================================================================
-- SMART_BLOCKS TABLE
-- ============================================================================

-- Index for station smart blocks
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_smart_blocks_station
ON smart_blocks(station_id);

-- Index for active smart blocks
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_smart_blocks_station_active
ON smart_blocks(station_id, active)
WHERE active = true;

-- ============================================================================
-- SMART_BLOCK_RULES TABLE
-- ============================================================================

-- Composite index for rule lookups
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_smart_block_rules_block
ON smart_block_rules(smart_block_id, criteria);

-- ============================================================================
-- CLOCK_HOURS TABLE
-- ============================================================================

-- Index for station clock hours
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_clock_hours_station
ON clock_hours(station_id);

-- Index for hour-of-day lookups
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_clock_hours_station_hour
ON clock_hours(station_id, hour_of_day);

-- ============================================================================
-- CLOCK_SLOTS TABLE
-- ============================================================================

-- Index for clock hour slots (for Preload queries)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_clock_slots_clock_hour
ON clock_slots(clock_hour_id, position);

-- ============================================================================
-- MOUNTS TABLE
-- ============================================================================

-- Index for station mounts
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_mounts_station
ON mounts(station_id);

-- Index for active mounts
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_mounts_station_active
ON mounts(station_id, active)
WHERE active = true;

-- ============================================================================
-- LIVE_SESSIONS TABLE
-- ============================================================================

-- Composite index for active sessions by station
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_live_sessions_station_active
ON live_sessions(station_id, active)
WHERE active = true;

-- Index for chronological session listing
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_live_sessions_connected_at
ON live_sessions(connected_at DESC);

-- Index for user sessions
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_live_sessions_user
ON live_sessions(user_id, active);

-- Index for token lookups
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_live_sessions_token
ON live_sessions(token)
WHERE token_used = false;

-- ============================================================================
-- PRIORITY_SOURCES TABLE
-- ============================================================================

-- Composite index for active priority sources (critical for priority resolution)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_priority_sources_station_active
ON priority_sources(station_id, priority, active)
WHERE active = true;

-- Index for source ID lookups
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_priority_sources_source
ON priority_sources(source_id, station_id);

-- ============================================================================
-- EXECUTOR_STATES TABLE
-- ============================================================================

-- Index for station executor lookups
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_executor_states_station
ON executor_states(station_id);

-- ============================================================================
-- WEBSTREAMS TABLE
-- ============================================================================

-- Index for station webstreams
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_webstreams_station
ON webstreams(station_id);

-- Index for health check enabled webstreams
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_webstreams_health_check
ON webstreams(health_check_enabled)
WHERE health_check_enabled = true;

-- Index for name sorting
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_webstreams_name
ON webstreams(name);

-- ============================================================================
-- HISTORY_ENTRIES TABLE
-- ============================================================================

-- Composite index for station history (for "last played" queries)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_history_station_time
ON history_entries(station_id, started_at DESC);

-- Index for media play history
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_history_media
ON history_entries(media_id, started_at DESC);

-- ============================================================================
-- USERS TABLE
-- ============================================================================

-- Index for email lookups (login)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_users_email
ON users(LOWER(email));

-- Index for username lookups
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_users_username
ON users(LOWER(username));

-- ============================================================================
-- MEDIA_TAGS TABLE (if exists)
-- ============================================================================

-- Index for tag filtering
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_media_tags_tag
ON media_tags(tag_id, media_id);

-- Index for media tag lookups
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_media_tags_media
ON media_tags(media_id);

-- ============================================================================
-- STATISTICS AND ANALYSIS
-- ============================================================================

-- Update table statistics after creating indexes
ANALYZE stations;
ANALYZE media_items;
ANALYZE schedule_entries;
ANALYZE smart_blocks;
ANALYZE smart_block_rules;
ANALYZE clock_hours;
ANALYZE clock_slots;
ANALYZE mounts;
ANALYZE live_sessions;
ANALYZE priority_sources;
ANALYZE executor_states;
ANALYZE webstreams;
ANALYZE history_entries;
ANALYZE users;

-- ============================================================================
-- NOTES
-- ============================================================================

-- Using CONCURRENTLY to avoid locking tables during index creation
-- This allows the application to continue running during migration
--
-- Partial indexes (WHERE clauses) are used where possible to reduce index size
-- and improve performance for filtered queries
--
-- Full-text search indexes (gin_trgm_ops) require pg_trgm extension:
-- CREATE EXTENSION IF NOT EXISTS pg_trgm;
--
-- To check index usage:
-- SELECT schemaname, tablename, indexname, idx_scan, idx_tup_read, idx_tup_fetch
-- FROM pg_stat_user_indexes
-- ORDER BY idx_scan DESC;
--
-- To check index sizes:
-- SELECT schemaname, tablename, indexname,
--        pg_size_pretty(pg_relation_size(indexrelid)) AS index_size
-- FROM pg_stat_user_indexes
-- ORDER BY pg_relation_size(indexrelid) DESC;
