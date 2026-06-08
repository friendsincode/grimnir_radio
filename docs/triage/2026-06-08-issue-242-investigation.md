# Issue #242 investigation: media delete fails on prod after v1.40.7 "fix"

**Filed:** 2026-06-08 by reallibertymedia-01 (S.M.)
**Title:** "In a Perfect World or Other stations - will not delete tracks in media Library"
**Related:** #223 (closed v1.40.7), #228 (closed no-comms 2026-06-08)
**Fix shipped:** v2.0.0-rc.7

## Issue body summary

S.M. reports two failure modes on the media library page:

1. **Top "Delete" bulk action** returns a visible error (he sent screenshots).
2. **Right-side per-row delete** does nothing visible; "screen flash" only.

He notes prior recommendations have not resolved it. Same reporter, same
symptom, third time in 6 weeks. Stations affected this round: "In a Perfect
World" and others (not the same stations as #223 or #228).

## Deployed prod version

```
$ ssh -J <ssh-user>@<edge-vps> <ssh-user>@<v1-prod-host> \
    'docker inspect grimnir-radio --format "{{.Config.Image}}"'
ghcr.io/friendsincode/grimnir_radio:latest
```

Image label `org.opencontainers.image.version=1.40.8`,
revision `307a67edf8ef0917419bb0a4e8fbd59a8f19bb65`. So prod **is on v1.40.8**
(includes the v1.40.7 FK-cleanup fix for #223). Bucket A ruled out.

## Root cause: bucket B

The v1.40.7 fix introduced `adminDeleteMediaReferences` to clean up FK referrers
before deleting `media_items`. Of the 5 referrers it handles, two use the wrong
pattern. Prod log from 18:21 UTC on 2026-06-08 (S.M.'s exact delete attempt):

```
ERR bulk media action failed error="clear MountPlayoutState.MediaID:
ERROR: invalid input syntax for type uuid: \"\" (SQLSTATE 22P02)" action=delete
```

The bug is at `internal/web/pages_admin.go:1582-1593`:

```go
// MountPlayoutState: clear MediaID
tx.Model(&models.MountPlayoutState{}).
    Where("media_id IN ?", mediaIDs).
    Update("media_id", "").Error  // <-- "" rejected by Postgres uuid column

// PlayHistory: clear MediaID (keep historical row)
tx.Model(&models.PlayHistory{}).
    Where("media_id IN ?", mediaIDs).
    Update("media_id", "").Error  // <-- same bug
```

Both `MountPlayoutState.media_id` and `PlayHistory.media_id` are declared in
GORM as `string \`gorm:"type:uuid;index"\``. Confirmed in prod via
`information_schema.columns`: both are Postgres `uuid` (nullable). Postgres
rejects empty string for uuid columns with SQLSTATE 22P02; the transaction
rolls back; the handler returns 500; HTMX shows it as a flash for the row
delete & a visible error for the bulk action.

The third FK-clear sibling, `UnderwritingObligation` at line 1596-1600, already
uses `nil` correctly because that column was declared as `*string` (truly
nullable in Go).

### Why the existing regression test passed

`TestMediaBulk_Delete_RemovesPlaylistReferences` (added in v1.40.7) uses
SQLite via `newCascadeTestDB`. SQLite is permissive about empty strings in
columns declared as uuid - it stores them happily. Postgres rejects them. So
the test caught the FK violation in #223 but missed the uuid-binding bug
sitting one line below. This is exactly the "test in a different DB engine
than prod" failure mode.

## Code paths inspected

- `internal/web/pages_media.go:1099-1141` - `MediaDelete` (per-row trash icon).
  Calls `adminDeleteMediaReferences` inside a transaction. Same bug path.
- `internal/web/pages_media.go:1216-1265` - `MediaBulk` case `"delete"` (top
  bulk action). Calls `adminDeleteMediaReferences` inside a transaction.
- `internal/web/pages_media.go:1796-1870` - `MediaPurgeDuplicates`. Also calls
  it. Same bug path.
- `internal/web/pages_admin.go:1425` - `AdminMediaBulk` delete. Same bug path.
- `internal/web/pages_admin.go:1565-1611` - `adminDeleteMediaReferences`. The
  one helper, the one bug, four entry points.
- FK constraints in prod (`pg_constraint` query): only
  `playlist_items.media_id`, `underwriting_obligations.media_id`, and
  `media_tag_links.media_id` are declared FKs. So the helper does NOT need to
  clear `media_tag_links` (it's never inserted - empty in prod) and DOES need
  to handle the rest as it does. The FK list is shorter than the GORM
  `MediaID` field list because most relationships are app-level only, not
  enforced by the DB.

## Fix shipped (v2.0.0-rc.7)

`internal/web/pages_admin.go`: change both `Update("media_id", "")` calls to
`Update("media_id", nil)`. GORM binds Go `nil` as SQL `NULL` via the driver,
which Postgres accepts for nullable uuid columns. Both columns are already
nullable in prod (verified via `information_schema.columns.is_nullable=YES`).
No migration needed.

## Regression test

`TestAdminDeleteMediaReferences_WritesNullNotEmptyString` in
`internal/web/pages_admin_test.go`. Wires a GORM `Update` callback that
captures bound parameters for any statement touching `media_id`; asserts that
`adminDeleteMediaReferences` never binds the empty string `""`. This catches
the bug regardless of underlying driver, so SQLite-vs-Postgres won't hide it
again.

## Recommended action

Tag v2.0.0-rc.7, push, comment on #242 with the fix citation, ask the operator
to redeploy. v1.x release line also affected; if anyone is running it instead
of v2.x, a backport patch v1.40.9 would mirror the same one-line change.
