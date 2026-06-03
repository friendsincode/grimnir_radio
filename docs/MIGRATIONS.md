# Database Migrations

This project uses **expand/contract** migration discipline so v(N) and v(N+1)
of the control plane can run side-by-side during a rolling update without one
of them tripping over the other's schema assumptions. The rule is enforced by
`cmd/migration-lint/` in `make ci`.

## The rule

Every schema change goes through up to three releases:

1. **Expand**: ADD columns / tables / indexes only. Old code keeps working.
2. **Dual-write + backfill**: app writes both old and new shape; backfill
   populates new shape from existing rows.
3. **Contract**: a later release (after every region is on the dual-write
   code) drops the old shape. Requires `-- migration-contract: <reason>` in
   the SQL file.

For pure expand changes (most additive work), one release is enough; the
discipline only forces extra ceremony for destructive operations.

## Operations the lint flags

| Operation | Why it's destructive |
|---|---|
| `DROP COLUMN` | Old code reading the column gets a runtime error |
| `DROP TABLE` | Old code reading the table gets a runtime error |
| `DROP INDEX` | Performance regression for old code; sometimes deliberate, hence annotation |
| `RENAME COLUMN` | Old code can't find the new name |
| `RENAME TABLE` | Old code can't find the new name |
| `ALTER COLUMN ... TYPE` | Type-narrowing breaks old writes; widening is usually safe but flagged for review |
| `ALTER COLUMN ... SET NOT NULL` | Old code inserting `NULL` fails |
| `TRUNCATE` | Data loss is always destructive |

If you genuinely need one of these and the multi-release sequence is correctly
sequenced, add an annotation:

```sql
-- migration-contract: column "foo" was deprecated in v1.41 (expand release added
-- "bar"); v1.42 wrote both columns; this release drops "foo" because every
-- region is now confirmed on v1.42+ per deploy_history.
ALTER TABLE example DROP COLUMN foo;
```

The annotation must name the original expand release and explain why dropping
now is safe.

## Worked examples

### Example 1: Add a column (one release)

Pure expand. No discipline overhead.

```sql
-- 042_add_listener_country.sql
ALTER TABLE listener_events ADD COLUMN country_code text;
CREATE INDEX idx_listener_events_country
    ON listener_events(country_code)
    WHERE country_code IS NOT NULL;
```

App code starts populating `country_code` in the same release. Old rows have
`NULL`; readers handle that.

### Example 2: Rename a column (three releases)

Renaming `listener_events.country` → `listener_events.country_code`.

**Release N (expand):**

```sql
-- 050_add_country_code_column.sql
ALTER TABLE listener_events ADD COLUMN country_code text;
-- copy existing values asynchronously via the next release; do not backfill
-- in this migration because CONCURRENT large UPDATEs are themselves disruptive.
```

App code: still reads/writes `country`.

**Release N+1 (dual-write + backfill):**

App code: writes to both `country` and `country_code`; reads prefer
`country_code` and fall back to `country`. A background backfill job populates
`country_code` from `country` for old rows.

No migration file needed; this is pure app-layer work.

**Release N+2 (contract):**

```sql
-- 060_drop_legacy_country_column.sql
-- migration-contract: `country` was the legacy column; `country_code` was added
-- in v1.41 (file 050) and v1.42 wrote both. Every region is now on v1.43+ per
-- deploy_history; dropping the legacy column is safe.
ALTER TABLE listener_events DROP COLUMN country;
```

### Example 3: Narrow a column type (three releases)

Narrowing `media_items.duration_ms` from `bigint` → `integer` (because
durations never exceed 2.1 billion ms and the int saves storage).

**Release N (expand):**

```sql
-- 070_add_duration_ms_int.sql
ALTER TABLE media_items ADD COLUMN duration_ms_int integer;
```

**Release N+1 (dual-write + backfill):**

App writes both columns; backfill copies `duration_ms::integer` into
`duration_ms_int` for old rows.

**Release N+2 (contract):**

```sql
-- 080_swap_to_int_duration.sql
-- migration-contract: duration_ms was bigint, narrowed to integer because
-- durations never exceed 2.1B ms. duration_ms_int has been populated since
-- v1.41 (file 070); every region is on v1.43+ per deploy_history.
ALTER TABLE media_items DROP COLUMN duration_ms;
ALTER TABLE media_items RENAME COLUMN duration_ms_int TO duration_ms;
```

### Example 4: Add a NOT NULL constraint (three releases)

The simplest "destructive" case is widening a column constraint.

**Release N (expand):**

```sql
-- 090_add_station_active_default.sql
ALTER TABLE stations ADD COLUMN active boolean DEFAULT true;
UPDATE stations SET active = true WHERE active IS NULL;
```

**Release N+1 (dual-write):** app code always writes `active`; backfill ran in
release N.

**Release N+2 (contract):**

```sql
-- 100_station_active_not_null.sql
-- migration-contract: `active` was added in v1.41 (file 090) with default
-- true and all existing rows backfilled. v1.42 enforced non-NULL writes in
-- app code. Every region is on v1.43+; the constraint is safe.
ALTER TABLE stations ALTER COLUMN active SET NOT NULL;
```

## Tooling

- New migration: copy `migrations/TEMPLATE.sql` and rename to the next
  sequential number. Fill in the phase comment block at the top so reviewers
  can see at a glance whether this is expand, dual-write, or contract.
- Run lint locally: `make migration-lint`.
- CI runs `make migration-lint-ci` which lints only PR-changed files.

## When the discipline does NOT apply

Single-instance deployments without HA (the always-supported fallback shape)
don't need expand/contract because there's only one instance — a brief
downtime during a destructive migration is acceptable. However, the lint
applies uniformly because the codebase ships one migration set for all
deployment shapes. If a single-instance-only deployment wants to take a
destructive shortcut, the annotation `-- migration-contract: single-instance
deployment, no rolling update applies` is acceptable and self-documenting.

## Edge cases

- **`CREATE INDEX CONCURRENTLY`**: safe; pure expand.
- **`DROP INDEX CONCURRENTLY IF EXISTS`** followed by a recreate in the same
  file: still flagged. Annotate with the reason
  ("rebuilding the index with new sort order; CONCURRENTLY so no listener
  impact; readers tolerate the brief window of no index").
- **Renaming a constraint**: not currently flagged because constraints don't
  directly break old code. Audit case-by-case.
- **`pg_repack` or similar online-migration tools**: out of scope; if used,
  add an annotation explaining why the apparent destructiveness is illusory.
