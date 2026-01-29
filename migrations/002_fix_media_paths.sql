-- Migration: Fix duplicated media paths
-- Issue: Media paths were stored with absolute prefixes like "/var/lib/grimnir/media/..."
-- instead of relative paths like "station_id/ab/cd/file.audio".
-- When the playout system joins these with GRIMNIR_MEDIA_ROOT, it creates invalid paths.
--
-- Root cause: storage_fs.go Store() was returning fullPath instead of relativePath.
-- This has been fixed in the code, but existing data needs correction.
--
-- Run this migration after deploying the fix to correct existing paths.

-- Preview what will be updated (uncomment to check before running):
-- SELECT id, path FROM media_items WHERE path LIKE '/var/lib/grimnir/media/%' LIMIT 20;
-- SELECT id, path FROM media_items WHERE path LIKE '/media/%' LIMIT 20;

-- Fix paths starting with /var/lib/grimnir/media/ (most common case)
-- Strips the prefix, leaving just the relative path
UPDATE media_items
SET path = SUBSTRING(path FROM 25)
WHERE path LIKE '/var/lib/grimnir/media/%';

-- Fix paths starting with /media/ (legacy importer issue)
UPDATE media_items
SET path = SUBSTRING(path FROM 8)
WHERE path LIKE '/media/%';

-- Verify no absolute paths remain
-- SELECT COUNT(*) FROM media_items WHERE path LIKE '/%';
-- SELECT id, path FROM media_items WHERE path LIKE '/%' LIMIT 10;
