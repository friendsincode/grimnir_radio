-- Query Performance Analysis Script
-- Use this script to identify slow queries and missing indexes

-- Enable pg_stat_statements extension (if not already enabled)
-- Requires: shared_preload_libraries = 'pg_stat_statements' in postgresql.conf
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

-- ============================================================================
-- TOP 20 SLOWEST QUERIES BY AVERAGE EXECUTION TIME
-- ============================================================================

SELECT
    query,
    calls,
    total_exec_time,
    mean_exec_time,
    max_exec_time,
    stddev_exec_time,
    rows
FROM pg_stat_statements
WHERE query NOT LIKE '%pg_stat_statements%'
ORDER BY mean_exec_time DESC
LIMIT 20;

-- ============================================================================
-- TOP 20 QUERIES BY TOTAL TIME (most impactful to optimize)
-- ============================================================================

SELECT
    query,
    calls,
    total_exec_time,
    mean_exec_time,
    (total_exec_time / sum(total_exec_time) OVER ()) * 100 AS percent_total
FROM pg_stat_statements
WHERE query NOT LIKE '%pg_stat_statements%'
ORDER BY total_exec_time DESC
LIMIT 20;

-- ============================================================================
-- INDEX USAGE STATISTICS
-- ============================================================================

-- Indexes ordered by usage (most scanned first)
SELECT
    schemaname,
    tablename,
    indexname,
    idx_scan AS index_scans,
    idx_tup_read AS tuples_read,
    idx_tup_fetch AS tuples_fetched,
    pg_size_pretty(pg_relation_size(indexrelid)) AS index_size
FROM pg_stat_user_indexes
ORDER BY idx_scan DESC
LIMIT 30;

-- ============================================================================
-- UNUSED INDEXES (candidates for removal)
-- ============================================================================

SELECT
    schemaname,
    tablename,
    indexname,
    pg_size_pretty(pg_relation_size(indexrelid)) AS index_size
FROM pg_stat_user_indexes
WHERE idx_scan = 0
  AND indexrelname NOT LIKE '%pkey'  -- Exclude primary keys
  AND schemaname = 'public'
ORDER BY pg_relation_size(indexrelid) DESC;

-- ============================================================================
-- MISSING INDEXES (tables with sequential scans)
-- ============================================================================

SELECT
    schemaname,
    tablename,
    seq_scan AS sequential_scans,
    seq_tup_read AS rows_read,
    idx_scan AS index_scans,
    n_live_tup AS estimated_rows,
    pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) AS total_size
FROM pg_stat_user_tables
WHERE seq_scan > idx_scan  -- More sequential scans than index scans
  AND n_live_tup > 1000    -- Ignore small tables
  AND schemaname = 'public'
ORDER BY seq_tup_read DESC
LIMIT 20;

-- ============================================================================
-- TABLE BLOAT ANALYSIS
-- ============================================================================

SELECT
    schemaname,
    tablename,
    n_dead_tup AS dead_tuples,
    n_live_tup AS live_tuples,
    round(n_dead_tup * 100.0 / NULLIF(n_live_tup + n_dead_tup, 0), 2) AS dead_tuple_percent,
    last_vacuum,
    last_autovacuum
FROM pg_stat_user_tables
WHERE n_dead_tup > 1000
  AND schemaname = 'public'
ORDER BY n_dead_tup DESC;

-- ============================================================================
-- CACHE HIT RATIO (should be > 99%)
-- ============================================================================

SELECT
    'index hit rate' AS name,
    (sum(idx_blks_hit)) / nullif(sum(idx_blks_hit + idx_blks_read), 0) AS ratio
FROM pg_statio_user_indexes
UNION ALL
SELECT
    'table hit rate' AS name,
    sum(heap_blks_hit) / nullif(sum(heap_blks_hit) + sum(heap_blks_read), 0) AS ratio
FROM pg_statio_user_tables;

-- ============================================================================
-- TABLE AND INDEX SIZES
-- ============================================================================

SELECT
    schemaname,
    tablename,
    pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) AS total_size,
    pg_size_pretty(pg_relation_size(schemaname||'.'||tablename)) AS table_size,
    pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename) - pg_relation_size(schemaname||'.'||tablename)) AS indexes_size,
    pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename) - pg_relation_size(schemaname||'.'||tablename)) AS toast_size
FROM pg_tables
WHERE schemaname = 'public'
ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC
LIMIT 20;

-- ============================================================================
-- LONG RUNNING QUERIES (currently executing)
-- ============================================================================

SELECT
    pid,
    now() - query_start AS duration,
    query,
    state
FROM pg_stat_activity
WHERE state != 'idle'
  AND query NOT LIKE '%pg_stat_activity%'
ORDER BY duration DESC;

-- ============================================================================
-- DATABASE CONNECTION STATISTICS
-- ============================================================================

SELECT
    datname AS database,
    numbackends AS connections,
    xact_commit AS commits,
    xact_rollback AS rollbacks,
    blks_read AS disk_reads,
    blks_hit AS cache_hits,
    tup_returned AS rows_returned,
    tup_fetched AS rows_fetched,
    tup_inserted AS rows_inserted,
    tup_updated AS rows_updated,
    tup_deleted AS rows_deleted
FROM pg_stat_database
WHERE datname = current_database();

-- ============================================================================
-- RECOMMENDATIONS
-- ============================================================================

-- 1. If cache hit ratio < 99%:
--    - Increase shared_buffers in postgresql.conf
--    - Increase effective_cache_size
--
-- 2. If many sequential scans on large tables:
--    - Add indexes on frequently filtered columns
--    - Check if statistics are up to date: ANALYZE;
--
-- 3. If high dead tuple percentage:
--    - Run VACUUM ANALYZE manually
--    - Adjust autovacuum settings
--
-- 4. If slow queries persist:
--    - Use EXPLAIN ANALYZE on specific queries
--    - Check for missing indexes on WHERE/JOIN columns
--    - Consider query rewriting
--
-- 5. Reset statistics after analysis:
--    SELECT pg_stat_statements_reset();
