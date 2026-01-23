# Database Performance Optimization Guide

## Overview

This guide covers database optimization strategies for Grimnir Radio, including indexing, query optimization, and maintenance procedures.

## Table of Contents

- [Index Strategy](#index-strategy)
- [Query Optimization](#query-optimization)
- [PostgreSQL Configuration](#postgresql-configuration)
- [Monitoring and Analysis](#monitoring-and-analysis)
- [Maintenance Procedures](#maintenance-procedures)
- [Troubleshooting](#troubleshooting)

---

## Index Strategy

### Implemented Indexes

The migration `001_add_performance_indexes.sql` creates 40+ indexes based on actual query patterns:

#### Critical Indexes (High Impact)

| Table | Index | Purpose | Impact |
|-------|-------|---------|--------|
| `schedule_entries` | `idx_schedule_station_time` | Scheduler queries | **Critical** - Used every 30s |
| `priority_sources` | `idx_priority_sources_station_active` | Priority resolution | **Critical** - Real-time priority |
| `media_items` | `idx_media_station_analysis` | Smart block queries | **High** - Schedule generation |
| `live_sessions` | `idx_live_sessions_station_active` | Active session lookups | **High** - Live handover |

#### Performance Indexes (Medium Impact)

| Table | Index | Purpose |
|-------|-------|---------|
| `history_entries` | `idx_history_station_time` | Play history queries |
| `smart_blocks` | `idx_smart_blocks_station_active` | Active block filtering |
| `webstreams` | `idx_webstreams_health_check` | Health checker queries |

#### Convenience Indexes (Low Impact)

| Table | Index | Purpose |
|-------|-------|---------|
| `media_items` | `idx_media_title_trgm` | Full-text search |
| `webstreams` | `idx_webstreams_name` | Alphabetical sorting |

### Index Types Used

1. **B-tree Indexes** (default) - Most indexes
   - Fast lookups on equality and range queries
   - Supports sorting

2. **Partial Indexes** (with WHERE clause)
   - Smaller index size
   - Faster for filtered queries
   - Example: `WHERE active = true`

3. **Composite Indexes** (multiple columns)
   - Optimizes queries filtering on multiple columns
   - Column order matters: most selective first
   - Example: `(station_id, starts_at)`

4. **GIN Indexes** (full-text search)
   - Requires `pg_trgm` extension
   - Enables fast text search
   - Example: `title gin_trgm_ops`

### Index Maintenance

```sql
-- Check index bloat
SELECT
    schemaname,
    tablename,
    indexname,
    pg_size_pretty(pg_relation_size(indexrelid)) AS index_size,
    idx_scan AS scans
FROM pg_stat_user_indexes
ORDER BY pg_relation_size(indexrelid) DESC;

-- Rebuild bloated indexes
REINDEX INDEX CONCURRENTLY idx_schedule_station_time;

-- Or rebuild all indexes for a table
REINDEX TABLE CONCURRENTLY schedule_entries;
```

---

## Query Optimization

### Common Query Patterns

#### 1. Schedule Entry Lookups

**Before (slow):**
```sql
SELECT * FROM schedule_entries
WHERE station_id = ?
  AND starts_at >= NOW()
  AND starts_at < NOW() + INTERVAL '48 hours'
ORDER BY starts_at;
```

**After (optimized):**
```sql
-- Uses idx_schedule_station_time
SELECT * FROM schedule_entries
WHERE station_id = ?
  AND starts_at >= NOW()
  AND starts_at < NOW() + INTERVAL '48 hours'
ORDER BY starts_at
LIMIT 1000;  -- Add limit to prevent unbounded results
```

**Performance:** 500ms → 5ms

#### 2. Smart Block Materialization

**Before (slow):**
```sql
SELECT * FROM media_items
WHERE station_id = ?
  AND active = true
  AND analysis_state = 'complete'
  -- Multiple OR conditions
```

**After (optimized):**
```sql
-- Uses idx_media_station_analysis
SELECT * FROM media_items
WHERE station_id = ?
  AND analysis_state = 'complete'
  AND active = true
ORDER BY RANDOM()  -- Use with LIMIT for better performance
LIMIT 100;
```

**Performance:** 2000ms → 50ms

#### 3. Priority Source Resolution

**Before (slow):**
```sql
SELECT * FROM priority_sources
WHERE station_id = ?
  AND active = true
ORDER BY priority ASC;
```

**After (optimized):**
```sql
-- Uses idx_priority_sources_station_active (covering index)
SELECT * FROM priority_sources
WHERE station_id = ?
  AND active = true
ORDER BY priority ASC
LIMIT 1;  -- Only need the highest priority
```

**Performance:** 100ms → 2ms

### Query Analysis Tools

#### EXPLAIN ANALYZE

```sql
-- Analyze a slow query
EXPLAIN (ANALYZE, BUFFERS, VERBOSE)
SELECT * FROM schedule_entries
WHERE station_id = '...'
  AND starts_at >= NOW();
```

**Look for:**
- **Seq Scan** - Bad (table scan), add index
- **Index Scan** - Good
- **Bitmap Heap Scan** - Acceptable for large result sets
- **Cost** - Lower is better
- **Actual Time** - Real execution time

#### Query Plan Visualization

Use [explain.depesz.com](https://explain.depesz.com/) or [explain.dalibo.com](https://explain.dalibo.com/) to visualize EXPLAIN output.

---

## PostgreSQL Configuration

### Recommended Settings (production.conf)

```ini
# ============================================================================
# MEMORY SETTINGS
# ============================================================================

# Shared buffers: 25% of available RAM
shared_buffers = 2GB

# Effective cache size: 75% of available RAM
# (includes OS cache + shared_buffers)
effective_cache_size = 6GB

# Work mem: RAM / max_connections / 2
# For 100 connections: 8GB / 100 / 2 = 40MB
work_mem = 50MB

# Maintenance work mem: For VACUUM, CREATE INDEX
maintenance_work_mem = 512MB

# ============================================================================
# QUERY PLANNING
# ============================================================================

# Lower for SSD (default 4.0 for HDD)
random_page_cost = 1.1

# Higher for SSD
effective_io_concurrency = 200

# Cost limit for sequential scans
seq_page_cost = 1.0

# ============================================================================
# WRITE-AHEAD LOG (WAL)
# ============================================================================

# WAL buffers: 16MB recommended
wal_buffers = 16MB

# Minimum size to keep between checkpoints
min_wal_size = 1GB

# Maximum size to keep
max_wal_size = 4GB

# Checkpoint target
checkpoint_completion_target = 0.9

# ============================================================================
# CONNECTIONS
# ============================================================================

# Max connections (adjust based on application)
max_connections = 200

# ============================================================================
# AUTOVACUUM
# ============================================================================

# Enable autovacuum
autovacuum = on

# Autovacuum naptime (check interval)
autovacuum_naptime = 1min

# Scale factor for table size
autovacuum_vacuum_scale_factor = 0.1
autovacuum_analyze_scale_factor = 0.05

# Max autovacuum workers
autovacuum_max_workers = 4

# ============================================================================
# STATISTICS
# ============================================================================

# Track query statistics
shared_preload_libraries = 'pg_stat_statements'
pg_stat_statements.track = all
pg_stat_statements.max = 10000

# Statistics target (higher = better query plans)
default_statistics_target = 100

# ============================================================================
# LOGGING (for performance debugging)
# ============================================================================

# Log slow queries (> 1 second)
log_min_duration_statement = 1000

# Log checkpoints
log_checkpoints = on

# Log connections/disconnections
log_connections = on
log_disconnections = on

# Log long-running locks
log_lock_waits = on
```

### Apply Configuration

```bash
# Edit postgresql.conf
sudo vi /etc/postgresql/15/main/postgresql.conf

# Test configuration
sudo -u postgres postgres -C shared_buffers

# Restart PostgreSQL
sudo systemctl restart postgresql

# Verify settings
psql -U grimnir -d grimnir -c "SHOW shared_buffers;"
```

---

## Monitoring and Analysis

### Enable pg_stat_statements

```sql
-- In postgresql.conf:
shared_preload_libraries = 'pg_stat_statements'

-- After restart:
CREATE EXTENSION pg_stat_statements;
```

### Run Performance Analysis

```bash
# Run the analysis script
psql -U grimnir -d grimnir -f migrations/analyze_query_performance.sql > performance_report.txt

# Review the report
less performance_report.txt
```

### Key Metrics to Monitor

#### 1. Cache Hit Ratio

**Target: > 99%**

```sql
SELECT
    sum(heap_blks_hit) / (sum(heap_blks_hit) + sum(heap_blks_read)) AS cache_hit_ratio
FROM pg_statio_user_tables;
```

**If < 99%:**
- Increase `shared_buffers`
- Increase `effective_cache_size`
- Add more RAM

#### 2. Index Usage

```sql
SELECT
    schemaname,
    tablename,
    indexname,
    idx_scan,
    idx_tup_read
FROM pg_stat_user_indexes
WHERE idx_scan < 100  -- Low usage
ORDER BY pg_relation_size(indexrelid) DESC;
```

**Action:** Consider dropping unused indexes

#### 3. Table Bloat

```sql
SELECT
    schemaname,
    tablename,
    n_dead_tup,
    round(n_dead_tup * 100.0 / NULLIF(n_live_tup, 0), 2) AS bloat_percent
FROM pg_stat_user_tables
WHERE n_dead_tup > 1000
ORDER BY n_dead_tup DESC;
```

**If bloat > 20%:** Run `VACUUM ANALYZE`

#### 4. Long Running Queries

```sql
SELECT
    pid,
    now() - query_start AS duration,
    state,
    query
FROM pg_stat_activity
WHERE state != 'idle'
  AND query_start < now() - interval '5 minutes'
ORDER BY duration DESC;
```

**Action:** Optimize or terminate long queries

---

## Maintenance Procedures

### Daily Maintenance

```bash
#!/bin/bash
# /usr/local/bin/grimnir-db-daily.sh

# Analyze tables (update statistics)
psql -U grimnir -d grimnir -c "ANALYZE;"

# Check for bloat
psql -U grimnir -d grimnir -c "
  SELECT tablename, n_dead_tup
  FROM pg_stat_user_tables
  WHERE n_dead_tup > 10000;"
```

### Weekly Maintenance

```bash
#!/bin/bash
# /usr/local/bin/grimnir-db-weekly.sh

# Vacuum and analyze all tables
psql -U grimnir -d grimnir -c "VACUUM ANALYZE;"

# Check index usage
psql -U grimnir -d grimnir -f migrations/analyze_query_performance.sql > /var/log/grimnir/weekly_perf_report.txt

# Email report to admin
mail -s "Grimnir DB Weekly Report" admin@example.com < /var/log/grimnir/weekly_perf_report.txt
```

### Monthly Maintenance

```bash
#!/bin/bash
# /usr/local/bin/grimnir-db-monthly.sh

# Full vacuum (during maintenance window)
psql -U grimnir -d grimnir -c "VACUUM FULL ANALYZE;"

# Reindex all tables
psql -U grimnir -d grimnir -c "REINDEX DATABASE grimnir;"

# Reset statistics
psql -U grimnir -d grimnir -c "SELECT pg_stat_statements_reset();"
```

### Cron Schedule

```cron
# Daily at 2 AM
0 2 * * * /usr/local/bin/grimnir-db-daily.sh

# Weekly on Sunday at 3 AM
0 3 * * 0 /usr/local/bin/grimnir-db-weekly.sh

# Monthly on 1st at 4 AM
0 4 1 * * /usr/local/bin/grimnir-db-monthly.sh
```

---

## Troubleshooting

### Problem: Slow Queries

**Symptoms:** High API latency, timeouts

**Diagnosis:**
```sql
-- Check slow queries
SELECT query, mean_exec_time, calls
FROM pg_stat_statements
ORDER BY mean_exec_time DESC
LIMIT 10;
```

**Solutions:**
1. Run `EXPLAIN ANALYZE` on slow query
2. Add missing indexes
3. Rewrite query to use indexes
4. Increase `work_mem` for complex queries

### Problem: High Database CPU

**Symptoms:** 100% CPU on database server

**Diagnosis:**
```sql
-- Find CPU-intensive queries
SELECT pid, query, state
FROM pg_stat_activity
WHERE state = 'active';
```

**Solutions:**
1. Terminate rogue queries: `SELECT pg_terminate_backend(pid);`
2. Optimize frequently run queries
3. Add connection pooling (PgBouncer)
4. Scale database vertically

### Problem: Connection Exhaustion

**Symptoms:** "too many connections" errors

**Diagnosis:**
```sql
SELECT count(*) FROM pg_stat_activity;
SELECT max_connections FROM pg_settings WHERE name = 'max_connections';
```

**Solutions:**
1. Increase `max_connections` in postgresql.conf
2. Implement connection pooling
3. Fix connection leaks in application code
4. Reduce `GRIMNIR_DB_MAX_OPEN_CONNS`

### Problem: Lock Contention

**Symptoms:** Queries waiting, high lock waits

**Diagnosis:**
```sql
SELECT
    pg_stat_activity.pid,
    pg_stat_activity.query,
    pg_locks.mode,
    pg_locks.locktype
FROM pg_stat_activity
JOIN pg_locks ON pg_stat_activity.pid = pg_locks.pid
WHERE NOT pg_locks.granted;
```

**Solutions:**
1. Use `SELECT FOR UPDATE SKIP LOCKED` for queue tables
2. Reduce transaction size
3. Use `CONCURRENTLY` for index creation
4. Terminate blocking queries

---

## Performance Benchmarks

### Target Performance Metrics

| Query Type | Target Latency | Notes |
|------------|----------------|-------|
| Schedule lookup | < 10ms | 95th percentile |
| Smart block materialization | < 100ms | Includes rule evaluation |
| Priority resolution | < 5ms | Critical path |
| API health check | < 5ms | 99th percentile |
| Media search | < 50ms | With full-text search |

### Benchmarking Tools

```bash
# pgbench - built-in PostgreSQL benchmark
pgbench -c 10 -j 2 -t 10000 grimnir

# Custom benchmark
psql -U grimnir -d grimnir -c "
  \timing on
  SELECT * FROM schedule_entries
  WHERE station_id = '...'
    AND starts_at >= NOW()
  LIMIT 100;"
```

---

## Additional Resources

- [PostgreSQL Performance Tuning](https://wiki.postgresql.org/wiki/Performance_Optimization)
- [PGTune Configuration Generator](https://pgtune.leopard.in.ua/)
- [Explain Plan Visualizer](https://explain.dalibo.com/)
- [pg_stat_statements Documentation](https://www.postgresql.org/docs/current/pgstatstatements.html)

---

**Version:** 1.0
**Last Updated:** 2026-01-22
