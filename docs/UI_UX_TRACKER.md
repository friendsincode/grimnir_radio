# Grimnir Radio UI/UX Implementation Tracker

## Overview

This document tracks the implementation status of the web UI for Grimnir Radio. The UI is built with:
- Go `html/template` for server-side rendering
- Bootstrap 5 (CDN with embedded fallback)
- HTMX for dynamic interactions
- Alpine.js for small client behaviors
- FullCalendar for scheduling (CDN)

---

## Implementation Status Legend

- [ ] Not started
- [~] Partial (handler exists, template missing or incomplete)
- [x] Complete

---

## 1. Core Infrastructure

### Templates & Layouts
- [x] Template loading system (isolated per-page template sets)
- [x] `layouts/base.html` - Base HTML structure
- [x] `layouts/public.html` - Public pages layout (header, footer)
- [x] `layouts/dashboard.html` - Dashboard layout (sidebar, header)
- [x] Theme system (4 themes)
- [x] Static file serving with embed.FS

### Authentication & Middleware
- [x] JWT cookie-based auth
- [x] AuthMiddleware (optional auth context)
- [x] RequireAuth middleware
- [x] RequireRole middleware
- [x] RequireStation middleware
- [x] RequireSetup middleware

### Themes
- [x] `daw-dark.css` - DAW-inspired dark theme
- [x] `clean-light.css` - Clean light theme
- [x] `broadcast.css` - Broadcast-focused theme
- [x] `classic.css` - Classic radio software theme
- [x] Theme switching JS

---

## 2. Public Pages

| Page | Route | Handler | Template | Status |
|------|-------|---------|----------|--------|
| Setup Wizard | `/setup` | `SetupPage`, `SetupSubmit` | `pages/setup.html` | [x] |
| Landing | `/` | `Landing` | `pages/public/landing.html` | [x] |
| Login | `/login` | `LoginPage`, `LoginSubmit` | `pages/public/login.html` | [x] |
| Logout | `/logout` | `Logout` | N/A | [x] |
| Listen | `/listen` | `Listen` | `pages/public/listen.html` | [~] Needs audio player |
| Archive | `/archive` | `Archive` | `pages/public/archive.html` | [x] |
| Archive Detail | `/archive/{id}` | `ArchiveDetail` | `pages/public/archive-detail.html` | [x] |
| Public Schedule | `/schedule` | `PublicSchedule` | `pages/public/schedule.html` | [x] |
| Station Info | `/station/{id}` | `StationInfo` | `pages/public/station.html` | [x] |

---

## 3. Dashboard - Core

| Page | Route | Handler | Template | Status |
|------|-------|---------|----------|--------|
| Dashboard Home | `/dashboard` | `DashboardHome` | `pages/dashboard/home.html` | [x] |
| Station Select | `/dashboard/stations/select` | `StationSelect`, `StationSelectSubmit` | `pages/dashboard/station-select.html` | [x] |
| User Profile | `/dashboard/profile` | `ProfilePage`, `ProfileUpdate`, `ProfileUpdatePassword` | `pages/dashboard/profile.html` | [x] |

---

## 4. Dashboard - Stations (Manager+)

| Page | Route | Handler | Template | Status |
|------|-------|---------|----------|--------|
| List Stations | `/dashboard/stations` | `StationsList` | `pages/dashboard/stations/list.html` | [x] |
| New Station | `/dashboard/stations/new` | `StationNew` | `pages/dashboard/stations/form.html` | [x] |
| Create Station | `POST /dashboard/stations` | `StationCreate` | N/A | [x] |
| Edit Station | `/dashboard/stations/{id}` | `StationEdit` | `pages/dashboard/stations/form.html` | [x] |
| Update Station | `PUT /dashboard/stations/{id}` | `StationUpdate` | N/A | [x] |
| Delete Station | `DELETE /dashboard/stations/{id}` | `StationDelete` | N/A | [x] |

### Mounts (Admin only)

| Page | Route | Handler | Template | Status |
|------|-------|---------|----------|--------|
| List Mounts | `/dashboard/stations/{id}/mounts` | `MountsList` | `pages/dashboard/stations/mounts.html` | [x] |
| New Mount | `/dashboard/stations/{id}/mounts/new` | `MountNew` | `pages/dashboard/stations/mount-form.html` | [x] |
| Create Mount | `POST ...` | `MountCreate` | N/A | [x] |
| Edit Mount | `/dashboard/stations/{id}/mounts/{id}` | `MountEdit` | `pages/dashboard/stations/mount-form.html` | [x] |
| Update Mount | `PUT ...` | `MountUpdate` | N/A | [x] |
| Delete Mount | `DELETE ...` | `MountDelete` | N/A | [x] |

---

## 5. Dashboard - Media Library

| Page | Route | Handler | Template | Status |
|------|-------|---------|----------|--------|
| Media List | `/dashboard/media` | `MediaList` | `pages/dashboard/media/list.html` | [x] |
| Media Upload | `/dashboard/media/upload` | `MediaUploadPage`, `MediaUpload` | `pages/dashboard/media/upload.html` | [x] |
| Media Detail | `/dashboard/media/{id}` | `MediaDetail` | `pages/dashboard/media/detail.html` | [x] |
| Media Edit | `/dashboard/media/{id}/edit` | `MediaEdit` | `pages/dashboard/media/edit.html` | [x] |
| Media Update | `PUT /dashboard/media/{id}` | `MediaUpdate` | N/A | [x] |
| Media Delete | `DELETE /dashboard/media/{id}` | `MediaDelete` | N/A | [x] |
| Media Waveform | `/dashboard/media/{id}/waveform` | `MediaWaveform` | N/A (JSON/image) | [~] |
| Media Table Partial | `/dashboard/media/table` | `MediaTablePartial` | `partials/media-table.html` | [x] |
| Media Grid Partial | `/dashboard/media/grid` | `MediaGridPartial` | **MISSING** | [ ] |

---

## 6. Dashboard - Playlists

| Page | Route | Handler | Template | Status |
|------|-------|---------|----------|--------|
| Playlist List | `/dashboard/playlists` | `PlaylistList` | `pages/dashboard/playlists/list.html` | [x] |
| Playlist New | `/dashboard/playlists/new` | `PlaylistNew` | `pages/dashboard/playlists/form.html` | [x] |
| Playlist Create | `POST ...` | `PlaylistCreate` | N/A | [x] |
| Playlist Detail | `/dashboard/playlists/{id}` | `PlaylistDetail` | `pages/dashboard/playlists/detail.html` | [x] |
| Playlist Edit | `/dashboard/playlists/{id}/edit` | `PlaylistEdit` | `pages/dashboard/playlists/form.html` | [x] |
| Playlist Update | `PUT ...` | `PlaylistUpdate` | N/A | [x] |
| Playlist Delete | `DELETE ...` | `PlaylistDelete` | N/A | [x] |
| Add Item | `POST .../{id}/items` | `PlaylistAddItem` | N/A | [x] |
| Remove Item | `DELETE .../{id}/items/{itemID}` | `PlaylistRemoveItem` | N/A | [x] |
| Reorder Items | `POST .../{id}/items/reorder` | `PlaylistReorderItems` | N/A | [x] |

---

## 7. Dashboard - Smart Blocks (Manager+)

| Page | Route | Handler | Template | Status |
|------|-------|---------|----------|--------|
| Smart Block List | `/dashboard/smart-blocks` | `SmartBlockList` | `pages/dashboard/smartblocks/list.html` | [x] |
| Smart Block New | `/dashboard/smart-blocks/new` | `SmartBlockNew` | `pages/dashboard/smartblocks/form.html` | [x] |
| Smart Block Create | `POST ...` | `SmartBlockCreate` | N/A | [x] |
| Smart Block Detail | `/dashboard/smart-blocks/{id}` | `SmartBlockDetail` | `pages/dashboard/smartblocks/detail.html` | [x] |
| Smart Block Edit | `/dashboard/smart-blocks/{id}/edit` | `SmartBlockEdit` | `pages/dashboard/smartblocks/form.html` | [x] |
| Smart Block Update | `PUT ...` | `SmartBlockUpdate` | N/A | [x] |
| Smart Block Delete | `DELETE ...` | `SmartBlockDelete` | N/A | [x] |
| Smart Block Preview | `POST .../{id}/preview` | `SmartBlockPreview` | `partials/smartblock-preview.html` | [x] |

---

## 8. Dashboard - Clock Templates (Manager+)

| Page | Route | Handler | Template | Status |
|------|-------|---------|----------|--------|
| Clock List | `/dashboard/clocks` | `ClockList` | `pages/dashboard/clocks/list.html` | [x] |
| Clock New | `/dashboard/clocks/new` | `ClockNew` | `pages/dashboard/clocks/form.html` | [x] |
| Clock Create | `POST ...` | `ClockCreate` | N/A | [x] |
| Clock Detail | `/dashboard/clocks/{id}` | `ClockDetail` | `pages/dashboard/clocks/detail.html` | [x] |
| Clock Edit | `/dashboard/clocks/{id}/edit` | `ClockEdit` | `pages/dashboard/clocks/form.html` | [x] |
| Clock Update | `PUT ...` | `ClockUpdate` | N/A | [x] |
| Clock Delete | `DELETE ...` | `ClockDelete` | N/A | [x] |
| Clock Simulate | `POST .../{id}/simulate` | `ClockSimulate` | `partials/clock-simulation.html` | [x] |

---

## 9. Dashboard - Schedule

| Page | Route | Handler | Template | Status |
|------|-------|---------|----------|--------|
| Schedule Calendar | `/dashboard/schedule` | `ScheduleCalendar` | `pages/dashboard/schedule/calendar.html` | [x] |
| Schedule Events JSON | `/dashboard/schedule/events` | `ScheduleEvents` | N/A (JSON) | [x] |
| Create Entry | `POST .../entries` | `ScheduleCreateEntry` | N/A | [x] |
| Update Entry | `PUT .../entries/{id}` | `ScheduleUpdateEntry` | N/A | [x] |
| Delete Entry | `DELETE .../entries/{id}` | `ScheduleDeleteEntry` | N/A | [x] |
| Refresh Schedule | `POST .../refresh` | `ScheduleRefresh` | N/A | [x] |

---

## 10. Dashboard - Live DJ

| Page | Route | Handler | Template | Status |
|------|-------|---------|----------|--------|
| Live Dashboard | `/dashboard/live` | `LiveDashboard` | `pages/dashboard/live/dashboard.html` | [x] |
| Live Sessions | `/dashboard/live/sessions` | `LiveSessions` | `partials/live-sessions.html` | [x] |
| Generate Token | `POST .../tokens` | `LiveGenerateToken` | `partials/live-token.html` | [x] |
| Connect | `POST .../connect` | `LiveConnect` | N/A | [x] |
| Disconnect | `DELETE .../sessions/{id}` | `LiveDisconnect` | N/A | [x] |
| Handover | `POST .../handover` | `LiveHandover` | N/A | [x] |
| Release Handover | `DELETE .../handover` | `LiveReleaseHandover` | N/A | [x] |

---

## 11. Dashboard - Webstreams (Manager+)

| Page | Route | Handler | Template | Status |
|------|-------|---------|----------|--------|
| Webstream List | `/dashboard/webstreams` | `WebstreamList` | `pages/dashboard/webstreams/list.html` | [x] |
| Webstream New | `/dashboard/webstreams/new` | `WebstreamNew` | `pages/dashboard/webstreams/form.html` | [x] |
| Webstream Create | `POST ...` | `WebstreamCreate` | N/A | [x] |
| Webstream Detail | `/dashboard/webstreams/{id}` | `WebstreamDetail` | `pages/dashboard/webstreams/detail.html` | [x] |
| Webstream Edit | `/dashboard/webstreams/{id}/edit` | `WebstreamEdit` | `pages/dashboard/webstreams/form.html` | [x] |
| Webstream Update | `PUT ...` | `WebstreamUpdate` | N/A | [x] |
| Webstream Delete | `DELETE ...` | `WebstreamDelete` | N/A | [x] |
| Failover | `POST .../{id}/failover` | `WebstreamFailover` | N/A | [x] |
| Reset | `POST .../{id}/reset` | `WebstreamReset` | N/A | [x] |

---

## 12. Dashboard - Playout Controls

| Action | Route | Handler | Status |
|--------|-------|---------|--------|
| Skip Track | `POST /dashboard/playout/skip` | `PlayoutSkip` | [x] |
| Stop Playout | `POST /dashboard/playout/stop` | `PlayoutStop` | [x] |
| Reload Queue | `POST /dashboard/playout/reload` | `PlayoutReload` | [x] |

---

## 13. Dashboard - Analytics

| Page | Route | Handler | Template | Status |
|------|-------|---------|----------|--------|
| Analytics Dashboard | `/dashboard/analytics` | `AnalyticsDashboard` | `pages/dashboard/analytics/dashboard.html` | [x] |
| Now Playing | `/dashboard/analytics/now-playing` | `AnalyticsNowPlaying` | `partials/now-playing.html` | [x] |
| History | `/dashboard/analytics/history` | `AnalyticsHistory` | `pages/dashboard/analytics/history.html` | [x] |
| Spins Report | `/dashboard/analytics/spins` | `AnalyticsSpins` | `pages/dashboard/analytics/spins.html` | [x] |
| Listeners | `/dashboard/analytics/listeners` | `AnalyticsListeners` | `pages/dashboard/analytics/listeners.html` | [x] |

---

## 14. Dashboard - Users (Manager+)

| Page | Route | Handler | Template | Status |
|------|-------|---------|----------|--------|
| User List | `/dashboard/users` | `UserList` | `pages/dashboard/users/list.html` | [x] |
| User New | `/dashboard/users/new` | `UserNew` | `pages/dashboard/users/form.html` | [x] |
| User Create | `POST ...` | `UserCreate` | N/A | [x] |
| User Detail | `/dashboard/users/{id}` | `UserDetail` | `pages/dashboard/users/detail.html` | [x] |
| User Edit | `/dashboard/users/{id}/edit` | `UserEdit` | `pages/dashboard/users/form.html` | [x] |
| User Update | `PUT ...` | `UserUpdate` | N/A | [x] |
| User Delete | `DELETE ...` | `UserDelete` | N/A | [x] |

---

## 15. Dashboard - Settings (Admin only)

| Page | Route | Handler | Template | Status |
|------|-------|---------|----------|--------|
| Settings Page | `/dashboard/settings` | `SettingsPage` | `pages/dashboard/settings/index.html` | [x] |
| Settings Update | `PUT ...` | `SettingsUpdate` | N/A | [x] |
| Migrations Page | `/dashboard/settings/migrations` | `MigrationsPage` | `pages/dashboard/settings/migrations.html` | [x] |
| Import | `POST .../import` | `MigrationsImport` | N/A | [x] |

---

## Summary Statistics

### Templates
- **Existing:** ~60
- **Missing:** ~2 (grid partial, player component)
- **Coverage:** ~97%

### By Feature Area

| Feature | Status | Missing Templates |
|---------|--------|-------------------|
| Setup/Auth | 100% | - |
| Public Pages | 100% | - |
| Stations/Mounts | 100% | - |
| Media Library | 90% | grid partial only |
| Playlists | 100% | - |
| Smart Blocks | 100% | - |
| Clocks | 100% | - |
| Schedule | 100% | - |
| Live DJ | 100% | - |
| Webstreams | 100% | - |
| Analytics | 100% | - |
| Users | 100% | - |
| Settings | 100% | - |

---

## Completed Phases

### Phase 1 - Core Flow [COMPLETE]
- [x] Fix login flow completely
- [x] Dashboard home with real data
- [x] Media library (list, upload, detail, edit)
- [x] User profile/edit own account

### Phase 2 - Content Management [COMPLETE]
- [x] Playlist CRUD
- [x] Smart Block CRUD
- [x] Clock Template CRUD

### Phase 3 - Scheduling & Live [COMPLETE]
- [x] Schedule calendar with FullCalendar
- [x] Live DJ controls
- [x] Playout controls panel

### Phase 4 - Administration [COMPLETE]
- [x] User management CRUD
- [x] Station management
- [x] Mount management
- [x] Settings page
- [x] Migrations import

### Phase 5 - Public & Polish [COMPLETE]
- [x] Public schedule page
- [x] Archive with search/filter
- [x] Station info page
- [x] Analytics dashboards (history, spins, listeners)
- [~] Listen page with player (needs audio player component)

---

## Known Issues

1. **FullCalendar CSS 404** - Resolved in base template
2. **Template isolation** - Fixed, each page now has isolated template set
3. **Form CSRF** - CSRF tokens not implemented yet
4. **WebSocket** - Real-time updates not connected yet
5. **File upload** - Needs multipart handling and progress

---

## Testing

### E2E Browser Tests (go-rod)

Run frontend tests with:
```bash
make test-e2e      # Full browser tests (headless)
make test-routes   # Quick route verification (no browser)
```

Test file: `test/e2e/routes_test.go`

Tests verify:
- All public routes return 200
- All dashboard routes are accessible after login
- Form pages render correctly
- Login flow works end-to-end

---

## Notes

- All handlers exist in `pages_*.go` files
- HTMX partials enable dynamic updates without full page reload
- Theme switching works via localStorage + CSS variables
- Alpine.js used for clock editor, playlist drag-drop, and form state
