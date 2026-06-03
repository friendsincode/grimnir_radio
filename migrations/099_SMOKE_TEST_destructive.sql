-- Smoke test: deliberately destructive, now annotated.
-- This PR should pass migration-lint in CI with the annotation present.
-- migration-contract: smoke-test fixture for PR #235; will be deleted before close.
ALTER TABLE foo DROP COLUMN bar;
