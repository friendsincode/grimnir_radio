# 2026-06-08 — UUID empty-string audit (post-#242)

Audit kicked off the day after the rc.7 fix for issue #242 to find every other
site in the codebase that bound `""` to a Postgres uuid column. Postgres
rejects "" on uuid with SQLSTATE 22P02 ("invalid input syntax for type
uuid"); SQLite (the test driver) accepts it, so the bug survives every test
& detonates on the first prod write. Same family of bug as #223, #228, #242.

## Grep methodology

Three syntactic patterns. Each ran across the full tree, then filtered:

```
grep -rn 'Update("\w*_id"\s*,\s*"")'    --include="*.go"   # direct Update
grep -rn '"\w*_id"\s*:\s*""'            --include="*.go"   # map literal
grep -rn 'updates\["\w*_id"\]\s*=\s*""' --include="*.go"   # map assign
```

Static-pattern grep can't catch every case; the actually-shipped bug at
`updates["host_user_id"] = *req.HostUserID` evaluates to `""` only at runtime.
For that pattern I cross-referenced every `updates["*_id"] =` assignment
against the model's GORM tag (`type:uuid`) & the request struct's field type
(`*string` lets the client pass `""`).

## Hits & verdicts

| File:Line | Pattern | Column | Type | Verdict |
|---|---|---|---|---|
| `internal/web/pages_admin.go:1583,1592` | `Update("media_id", "")` | `media_id` | uuid | **fixed in rc.7** (#242) |
| `internal/api/api.go:1265` | `updates["mount_id"] = req.MountID` | `mount_id` | uuid | safe — guarded by `if req.MountID != ""` at line 1263 |
| `internal/api/shows.go:271` | `updates["host_user_id"] = *req.HostUserID` | `shows.host_user_id` | uuid | **buggy** — no `""` guard, fixed in rc.8 |
| `internal/api/shows.go:639` | `updates["host_user_id"] = *req.HostUserID` | `show_instances.host_user_id` | uuid | **buggy** — no `""` guard, fixed in rc.8 |
| `internal/web/pages_shows.go:361` | `updates["host_user_id"] = *input.HostUserID` | `shows.host_user_id` | uuid | **buggy** — no `""` guard, fixed in rc.8 |
| `internal/web/pages_shows.go:561` | `updates["host_user_id"] = *input.HostUserID` | `show_instances.host_user_id` | uuid | **buggy** — no `""` guard, fixed in rc.8 |
| `internal/web/pages_admin.go:916` | `Update("station_id", req.Value)` | `station_id` | uuid | safe — verified via `db.First(&station, ...)` before the Update |
| `internal/api/webstream_test.go:235` | `"station_id":""` | n/a | n/a | safe — JSON request body in a `_test.go` (decoded into a string, never passed to GORM) |
| `internal/api/api_handlers_test.go:639` | `"mount_id":""` | n/a | n/a | safe — same as above |
| `internal/web/pages_schedule_endpoints_test.go:745,837,1182,1216` | `"source_id":""` | n/a | n/a | safe — JSON request bodies |
| `internal/web/pages_schedule_p0_test.go:242` | `"source_id":""` | n/a | n/a | safe — JSON request body |
| 11 single-column `.Update("...", "")` callsites in non-test code | various | non-uuid (`status`, `password`, `rules`, `theme`, `custom_css`, ...) | n/a | safe — none target uuid columns |

Totals: 11 candidate sites grep'd. 6 already fixed in rc.7 (#242). 4 fresh
buggy sites fixed in rc.8. The remaining 9 are safe.

## Fixes shipped (rc.8)

Each site now normalizes `""` to `nil` before writing the map:

```go
if input.HostUserID != nil {
    if *input.HostUserID == "" {
        updates["host_user_id"] = nil
    } else {
        updates["host_user_id"] = *input.HostUserID
    }
}
```

Files touched:
- `internal/api/shows.go` (lines 271, 639)
- `internal/web/pages_shows.go` (lines 361, 561, plus the virtual-instance
  cast at line 588 that would panic on `nil` if not handled)

## Regression tests

Four per-site tests, all driver-agnostic. Each wires a GORM `After("gorm:update")`
callback that inspects `tx.Statement.Vars` for any `""` bound against
`host_user_id`. Mirrors the rc.7 pattern from
`TestAdminDeleteMediaReferences_WritesNullNotEmptyString`.

- `TestShowsUpdate_EmptyHostUserID_BindsNull` — `internal/api/shows_test.go`
- `TestInstancesUpdate_EmptyHostUserID_BindsNull` — `internal/api/shows_test.go`
- `TestShowUpdate_EmptyHostUserID_BindsNull` — `internal/web/pages_shows_coverage_test.go`
- `TestShowInstanceUpdate_EmptyHostUserID_BindsNull` — `internal/web/pages_shows_coverage_test.go`

Each one fails on the buggy code & passes on the fix; verified red-then-green
during the rc.8 work.

## Static lint added: `cmd/uuid-trap-lint`

Cheap insurance against future regressions. A Go tool that walks every
non-test `.go` file via `git ls-files`, matches three patterns, & exits 1 on
any hit:

1. `Update("foo_id", "")` — single-column direct bind
2. `"foo_id": ""` — map-literal value
3. `updates["foo_id"] = ""` — map-index assignment

Wired into `make ci` so every PR runs it. Lives at `cmd/uuid-trap-lint/`
alongside `cmd/migration-lint/`, mirrors its layout.

The lint is syntactic, so the rc.8 fixes (which use runtime guards rather
than literal `""`) don't trip it. It catches the literal-string case that
would have caught #242 at PR time.

## Commits & tag

- `<sha-fix>` — feat(audit): clear-host accepts "" without 500ing on Postgres (rc.8)
- `<sha-lint>` — feat(ci): add uuid-trap-lint to catch *_id="" in PRs
- Tag: `v2.0.0-rc.8`

## Why not a reflection meta-test instead of the lint

Considered walking every model in the AutoMigrate registry & asserting no
GORM hook sets a uuid field to "". Two reasons against:

1. The actual #242 / rc.8 traps are in *handler* code, not models. Reflection
   on models won't see them.
2. A grep-style lint is two patterns away from also catching `Save(&model)`
   where the struct was constructed from a request — much wider coverage
   surface, near-zero false-positive risk on non-test code.

The lint is the more durable guard for this bug family.
