# Import Idempotency Contract

Date: 2026-02-21

## Goal

Repeated imports of the same source entities should be deterministic and avoid duplicate target records.

## Source Identity

- Primary key for imported entities is `import_source + import_source_id`.
- For AzuraCast multi-station data, `import_source_id` uses scoped form: `stationID::sourceID`.
- Legacy unscoped source IDs are still recognized for compatibility.

## Behavior

- If an entity already exists for the same station and source identity, importer skips creating a duplicate.
- Skip counters report `*_already_imported` where applicable.
- For entities without full provenance fields, importer uses stable best-effort matching (for example, clock template by station + name).

## Current Coverage

- Enforced in staged AzuraCast commit path for:
  - Smart blocks
  - Shows
  - Webstreams
- Media already had source/hash-based duplicate handling.

## Testing

- `internal/migration/idempotency_test.go` validates scoped and legacy source ID matching behavior.
