# Import Re-evaluation (LibreTime + AzuraCast)

Date: 2026-02-21
Scope: `internal/migration/*`, staged review UI/API flow, import correctness/data quality behavior.

## Priority Gap Report

1. P1: AzuraCast staged review is API-only; backup staged analysis is explicitly unsupported.
Impact: staged workflow parity gap and inconsistent operator UX.
Evidence: `internal/migration/azuracast.go` (`"staged AzuraCast import currently requires API mode"`).

2. P1: Staged duplicate detection is hash-only in shared analyzer path.
Impact: misses likely duplicates when source hash is absent/inconsistent; can inflate media.
Evidence: `internal/migration/staged_analyzer.go` duplicate query keyed by `content_hash`.

3. P1: Deterministic re-import/idempotency behavior is not explicitly enforced/tested end-to-end.
Impact: repeated imports can produce source-dependent outcomes and harder rollback/redo predictability.
Evidence: broad create-first importer flow without a documented idempotency contract.

4. P2: Duration quality is source-derived during import; post-import verification/reconciliation is not guaranteed as a required phase.
Impact: imported scheduling/planning can inherit bad source durations.
Evidence: source duration parse/use in `internal/migration/libretime.go` and `internal/migration/azuracast.go` with no mandatory post-import duration verification gate.

5. P2: Staged station-level selection is flagged as not yet available.
Impact: multi-station review ergonomics are weaker and can increase manual deselection burden.
Evidence: warning emitted in `internal/migration/azuracast.go` ("Station-level selection is not yet available").

6. P2: Import anomaly observability is mostly logs/progress; no consolidated anomaly report artifact per job.
Impact: operators need manual log parsing for partial failures/data integrity drift.

## Test Matrix (Minimum)

### Sources
- AzuraCast API (single station, multi-station)
- AzuraCast backup (single station, multi-station)
- LibreTime API
- LibreTime DB

### Data edge cases
- Missing/zero/invalid durations
- Missing content hash
- Conflicting metadata with same file hash
- Duplicate media across same station and cross-station
- Show recurrence with exceptions (irregular instances)
- Empty/partial playlists, missing linked media
- Missing mounts or station mapping ambiguities

### Workflow states
- `analyzing -> staged -> running -> completed`
- `analyzing -> failed`
- staged reject path
- staged selection update persistence
- redo/rollback and re-import determinism checks

## Implementation Plan

1. Milestone A (P1 correctness)
- Add metadata fallback duplicate heuristics (artist/title/album normalization) when hash unavailable.
- Add deterministic idempotency rules and tests (source key + station scoping + rerun behavior).

2. Milestone B (P1/P2 staged parity + quality)
- Add AzuraCast backup staged analysis.
- Add required post-import duration verification/reconcile pass and anomaly counters.

3. Milestone C (P2 UX + observability)
- Add station-level staged selection for multi-station imports.
- Add per-job anomaly report endpoint + UI surfacing.

## Follow-up Issues

- #67 AzuraCast backup staged analysis parity
- #68 Duplicate detection fallback when hash missing
- #69 Deterministic re-import/idempotency contract + tests
- #70 Post-import duration verification/reconcile phase
- #71 Station-level selection in staged review
- #72 Import anomaly report artifact + UI
