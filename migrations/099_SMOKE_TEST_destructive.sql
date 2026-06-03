-- Smoke test: deliberately destructive, no annotation.
-- This PR should be blocked by migration-lint in CI.
-- Will be deleted after the smoke test confirms enforcement works.
ALTER TABLE foo DROP COLUMN bar;
