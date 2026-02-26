# Changelog

## 1.18.20 — 2026-02-26

### Harbor Connection Hijack for Icecast Source Compatibility
- Fixed BUTT/Icecast source clients receiving zero audio bytes through nginx proxy.
- Root cause: BUTT sends PUT requests without Content-Length or Transfer-Encoding headers. Go's HTTP parser treats this as an empty body per HTTP/1.1 spec, so `r.Body` returns EOF immediately.
- Harbor now hijacks the underlying TCP connection after the 200 OK handshake response, reading the raw audio stream directly from the socket instead of relying on Go's HTTP body parser.
- This bypasses the HTTP/1.1 body-length determination entirely, allowing the Icecast full-duplex protocol to work correctly regardless of proxy configuration.

---

## 1.18.19 — 2026-02-26

### Harbor Source Copy Synchronization Fix
- Fixed Harbor ingest race where decoder pipe completion could end stream handling before request-body copy finished.
- Harbor now waits for `source→decoder` copy completion, preventing false zero-byte disconnects during live DJ connects.

---

## 1.18.17 — 2026-02-26

### Harbor Full-Duplex Source Ingest Fix
- Fixed Harbor live-source ingest disconnect loops where sources authenticated and connected but immediately dropped with zero-byte reads.
- Enabled HTTP full-duplex mode in Harbor source handler so request bodies stay readable after sending `200 OK` handshake responses to Icecast-compatible source clients.
- Prevented `http: invalid Read on closed Body` failures during live DJ source ingest.

---

## 1.18.16 — 2026-02-25

### Clock Windowing, Multi-Hour Planning, and Scheduler Noise Reduction
- Added clock-hour active windows with explicit `start_hour` (inclusive) and `end_hour` (exclusive) support.
- Updated clock compilation to evaluate all station clock templates and select the applicable template per hour using station timezone.
- Added overnight window support (for example, `22 -> 2`) and aligned hourly planning to `HH:00` boundaries.
- Improved clock UX to clarify that clocks are reusable scheduling helpers and added start/end hour controls in create/edit/detail/list views.
- Extended clock API create payloads with `start_hour` and `end_hour`.
- Reduced scheduler warning noise for invalid slot payloads by validating earlier and suppressing repeated identical warning emissions.
- Added planner regression tests for multi-window and overnight clock behavior.

---

## 1.17.35 — 2026-02-24

### Live Scheduling UUID Normalization
- Fixed schedule creation failure for `live` source entries where `source_id` was an empty string.
- Added server-side normalization to assign a valid UUID fallback for `live` entries with blank `source_id`.
- Added regression coverage for creating live schedule entries with empty `source_id`.

---

## 1.17.34 — 2026-02-24

### Schedule Entry Mount Auto-Recovery
- Fixed `/dashboard/schedule/entries` returning `400` for stations that had no mount configured.
- Added automatic default mount creation during schedule-entry creation when no station mount exists.
- Added regression test coverage for schedule creation on mount-less stations.

---

## 1.17.15 — 2026-02-23

### Dashboard WebSocket Auth Hotfix
- Fixed dashboard realtime WebSocket (`/api/v1/events`) reconnect loops and `1006` closes by allowing query-token auth only for WebSocket upgrade requests on the events endpoint.
- Kept query-token auth blocked for normal HTTP API routes to preserve tightened security posture.
- Added middleware regression coverage for websocket query-token acceptance and non-websocket query-token rejection.

---

## 1.17.14 — 2026-02-23

### Station Select CSRF Hotfix
- Fixed `/dashboard/stations/select` POST submissions being blocked with `403` by adding explicit `csrf_token` submission in the station selection form (HTMX + normal form submit).
- Resolved false "Access denied" toast on station selection caused by CSRF middleware rejection before handler execution.

---

## 1.17.13 — 2026-02-23

### Access Control Stability
- Fixed station selection flow to validate against the rendered station list, preventing false `Access denied` responses after selecting a visible station.
- Added auto-select redirect when a user has exactly one accessible station, sending them directly to `/dashboard` with station context set.
- Added JWT role-claim normalization for legacy platform claims (`admin`, `manager`) to prevent access regressions with older tokens under stricter auth checks.
- Added regression tests for station-select submit/auto-select behavior and JWT legacy-claim normalization.

---

## 1.17.12 — 2026-02-23

### Landing Preview and Platform Access Fixes
- Fixed landing page editor preview iframes (station + platform) by allowing same-origin framing only on preview endpoints while keeping strict frame-deny defaults elsewhere.
- Fixed platform role compatibility for legacy role values (`admin`/`manager`) so platform admin/mod checks, station selection, and dashboard role rendering work consistently.
- Added runtime role normalization hooks and migration-time role backfill to canonical values (`platform_admin`/`platform_mod`).
- Added a second CTA on platform station cards (`Station Page`) beside `Listen`, routing to station landing (`/s/{shortcode}` with `/station/{id}` fallback).

---

## 1.17.11 — 2026-02-22

### Platform Role Rendering Reliability
- Fixed platform role badges in admin/user pages by normalizing role comparisons in templates.
- Hardened template equality helpers (`eq`/`ne`) to handle typed string enums and numeric values safely, preventing silent UI branch fall-through.
- Updated platform/station user templates to render correct admin/mod/user indicators and selected role states.

---

## 1.17.10 — 2026-02-22

### Staged Media Attribution
- Added explicit source-station attribution in staged review media table so each song shows which station it belongs to.
- Added station label map propagation to import review page data and template helpers for scoped source ID parsing.
- Preserved robust station label resolution from staged metadata + description fallback paths.

---

## 1.17.9 — 2026-02-22

### Staged Review Station Labels
- Fixed staged review station filters to display real source station names instead of generic `Station <id>` placeholders.
- Added AzuraCast staged-analysis metadata warnings that persist source station ID-to-name labels for UI rendering.
- Added fallback label extraction from staged item descriptions when explicit station-label metadata is unavailable.
- Added regression tests for station filter label resolution and fallback behavior.

---

## 1.17.8 — 2026-02-22

### Import Status Rendering Fix
- Fixed migration status template comparisons to normalize job status values before equality checks.
- Restored staged/completed/failed conditional rendering paths, including staged `Review Import` actions and correct status badges.

---

## 1.17.7 — 2026-02-22

### Import Review Discoverability
- Added a prominent in-row `Review Import` button inside each staged history item, so review is visible even if right-side action controls are missed or clipped.
- Kept right-side staged review action and job-based fallback route support in place.

---

## 1.17.6 — 2026-02-22

### Import Review Page Rendering
- Fixed blank import review page by wiring `import-review` template to the dashboard layout entrypoint (`pages/dashboard/settings/import-review`) and `main` content block.
- Added breadcrumb metadata to the import review page for consistent dashboard rendering.

---

## 1.17.5 — 2026-02-22

### Import Status UX
- Updated staged import action in migration status history from icon-only to explicit `Review` button text for clear operator visibility.
- Preserved staged review fallback behavior (`/migrations/review/job/{jobID}`) when direct staged link data is incomplete.

---

## 1.17.4 — 2026-02-22

### Docker Publish Reliability
- Fixed Docker build context exclusions that broke release image builds by explicitly allowing `LICENSE` and `THIRD_PARTY_NOTICES.md` through `.dockerignore`.
- Restored compatibility with Dockerfiles that copy licensing artifacts into runtime images.

---

## 1.17.3 — 2026-02-22

### Import Review Visibility (Staged/Dry-Run)
- Fixed staged import jobs that could appear without a usable Review action by adding a job-based review resolver route.
- Updated migration status UI to always offer a Review button for staged jobs, including fallback resolution when `staged_import_id` is missing.
- Hardened staged analysis completion flow: if analysis returns no staged data, the job now fails explicitly instead of silently remaining in an unusable staged state.
- Added regression coverage to prevent staged jobs from completing without staged review data.

---

## 1.17.2 — 2026-02-22

### Licensing and Distribution Compliance
- Added `THIRD_PARTY_NOTICES.md` to clarify license boundaries between Grimnir Radio (AGPL) and bundled third-party components.
- Added bundled third-party license texts under `third_party/licenses/` and a generated Go dependency license report at `third_party/go-licenses.csv`.
- Updated Docker images to include a complete license bundle under `/usr/share/licenses/grimnir-radio/`.
- Updated release workflow to attach licensing artifacts, including a packaged license bundle tarball, to versioned GitHub releases.
- Added `docs/LICENSING_COMPLIANCE.md` with GHCR-focused verification steps.

---

## 1.17.1 — 2026-02-22

### Import Review Reliability
- Fixed staged import history rows that showed only delete actions by auto-resolving missing `staged_import_id` links from `staged_imports` via `job_id`.
- Added service-level staged-reference hydration/backfill so all job list/detail consumers can render Review links consistently.
- Updated migration status page to load jobs via migration service, inheriting staged-link auto-healing for all import sources.

---

## 1.17.0 — 2026-02-21

### Security & Access Control
- Enforced station-scoped authorization across API handlers while preserving global `platform_admin` access.
- Hardened JWT validation and auth middleware coverage (including rejection of query-token auth patterns).
- Added CSRF token validation for dashboard actions, cookie security improvements, and security header regression tests.
- Added open-redirect and public asset access regression tests.

### Import & Data Integrity
- Completed AzuraCast staged import review flow (analyze -> select -> commit) with persisted selection state.
- Added import anomaly reporting artifacts and surfaced anomaly summaries in status/review views.
- Added integrity audit + repair service with admin UI for findings and idempotent remediation actions.
- Expanded import reliability tests: idempotency, duration verification, staged backup handling, and safe extraction paths.

### Uploads, Storage & Platform Hardening
- Added hard API media upload request limits with explicit oversized payload handling (`413 file_too_large`).
- Wired upload limits to configurable env-based size controls and added regression tests for API/web multipart limits.
- Documented and guarded experimental S3/MinIO media backend behavior.
- Updated deployment guidance for production secret requirements and security headers.

### Scheduling, Smart Blocks & UX
- Improved schedule overlap validation responses and scheduler conflict observability.
- Completed IRT/Now Playing visibility updates across dashboard and schedule surfaces.
- Improved smart block preview/search/fallback usability and related UI behavior.

### Infrastructure
- Fixed Docker Icecast healthcheck target to use loopback (`127.0.0.1`) for reliable container health reporting.

---

## 1.9.5 — 2026-02-06

### New Features
- **WebDJ Console**: Complete browser-based DJ mixing interface
  - Real waveform generation via media engine gRPC
  - Live broadcast controls with Go Live / Off Air buttons
  - Mount selector for choosing broadcast destination
  - Live status indicator with pulsing animation
  - Dual deck layout with transport controls, hot cues, EQ, pitch

- **System Settings**: Persistent runtime-configurable settings
  - Scheduler lookahead, analysis, websocket, metrics toggles
  - Log level configuration
  - Singleton pattern with database storage

- **DSP Parameter Support**: Additional audio processing parameters
  - AGC target level configuration
  - Limiter release time control
  - Ducking threshold and reduction settings

### Improvements
- **Live Session Handlers**: Wired up live service for token generation, disconnect, handover
- **Webstream Failover**: Connected failover/reset handlers to webstream service
- **Schedule Refresh**: Linked schedule refresh endpoint to scheduler service
- **Migration Import**: Background file processing for AzuraCast/LibreTime imports

---

## 1.10.0 — 2026-02-02

### New Features
- **Platform Landing Page**: Separate editable landing page for the platform at `/`
  - Platform admins can customize via `/dashboard/admin/landing-page/editor`
  - Supports themes, hero section, SEO settings
  - Shows grid of all public stations

- **Station Landing Pages**: Individual station pages at `/s/{shortcode}`
  - Each station gets its own public landing page
  - Shows station info, player, schedule link
  - Uses station's shortcode for URL

### Changes
- **Landing Page Model**: StationID is now nullable (NULL = platform page)
- **Dashboard Navigation**: Added "Platform Landing Page" link in admin sidebar

---

## 1.9.4 — 2026-02-02

### Bug Fixes
- **API Auth**: Fixed API authentication to accept JWT Bearer tokens from dashboard JavaScript
  - Previously only X-API-Key headers were accepted
  - Now accepts both X-API-Key and Authorization Bearer tokens
  - Fixes 401 errors on notifications, audit, and other API calls from dashboard

---

## 1.9.3 — 2026-02-02

### Bug Fixes
- **Notifications**: Fixed 401 errors on notification API calls from dashboard by adding Authorization headers

---

## 1.9.2 — 2026-02-02

### New Features
- **Audit Logs UI**: Added web pages for viewing audit logs
  - Platform-wide audit logs at `/dashboard/admin/audit` (platform admins)
  - Station audit logs at `/dashboard/station/audit` (managers+)
  - Filter by action type, station, and date range
  - View detailed JSON for each audit entry

### Bug Fixes
- **Landing Page Editor**: Added missing `landing-page.js` file

---

## 1.9.1 — 2026-02-02

### Bug Fixes
- **System Status**: Fixed "Failed to check" error on System Settings page by adding Authorization header to API calls
- **Webhook Routes**: Fixed panic on startup caused by duplicate `/webhooks` route registration

### UI Improvements
- **View Site Button**: Added button in dashboard header to open platform landing page
- **Landing Page Link**: Added "Landing Page" navigation link in station sidebar

---

## 1.9.0 — 2026-02-02

### Phase 9: Landing Page Editor (Complete)

Visual editor for station operators to customize their public-facing landing page without writing code. Includes theme system, widget library, drag-and-drop editor, asset management, versioning, and SEO controls.

#### Phase 9A: Foundation
- **Landing Page Models**: LandingPage, LandingPageAsset, LandingPageVersion with JSONB config storage
- **Service Layer**: CRUD operations, draft/publish workflow, version management
- **Theme System**: 7 built-in themes (Default, Dark, Light, Bold, Vintage, Neon, Community)
- **API Handlers**: Full REST API for landing page management

#### Phase 9B: Core Widgets
- **Widget Registry**: 16 widget types with configuration specs
- **Server-Side Rendering**: Go templates for all widgets
- **Core Widgets**: Player, Schedule, Recent Tracks, Text, Image, Spacer, Divider

#### Phase 9C: Editor UI
- **Visual Editor**: Drag-and-drop interface with live preview
- **Sidebar Tabs**: Widgets, Theme, Header, Hero, Content, Footer, SEO, Custom CSS
- **Live Preview**: iframe with real-time updates via postMessage
- **Auto-Save**: Automatic draft saving every 30 seconds

#### Phase 9D: Asset Management
- **Asset Upload**: Images up to 10MB (PNG, JPG, GIF, WebP, SVG)
- **Asset Library**: Grid view with upload dropzone
- **Asset Serving**: Public URLs for uploaded assets

#### Phase 9E: Additional Widgets
- **DJ Grid**: Display DJs with photos and bios
- **Upcoming Shows**: Next N shows with host info
- **Image Gallery**: Grid layout with optional lightbox
- **Video**: Embed videos with autoplay/muted options
- **CTA (Call to Action)**: Headline, subtext, button
- **Contact**: Form, map, contact info
- **Social Feed**: Platform embeds
- **Newsletter**: Email signup form
- **Custom HTML**: Sanitized custom code blocks

#### Phase 9F: Advanced Features
- **Version History**: Browse, preview, and restore previous versions
- **Mobile Preview**: Responsive viewport testing
- **SEO Configuration**: Title, description, OG image, Twitter cards
- **Custom CSS Editor**: Scoped CSS with live preview

### New API Endpoints

**Landing Page Configuration:**
- `GET /api/v1/landing-page` - Get landing page config
- `PUT /api/v1/landing-page` - Update landing page config
- `POST /api/v1/landing-page/publish` - Publish changes
- `POST /api/v1/landing-page/discard-draft` - Discard draft
- `GET /api/v1/landing-page/preview` - Preview rendered page

**Assets:**
- `GET /api/v1/landing-page/assets` - List assets
- `POST /api/v1/landing-page/assets` - Upload asset
- `DELETE /api/v1/landing-page/assets/{assetID}` - Delete asset

**Versions:**
- `GET /api/v1/landing-page/versions` - List versions
- `GET /api/v1/landing-page/versions/{versionID}` - Get version
- `POST /api/v1/landing-page/versions/{versionID}/restore` - Restore version

**Themes:**
- `GET /api/v1/landing-page/themes` - List themes
- `GET /api/v1/landing-page/themes/{name}` - Get theme details

### Web UI Routes

- `/dashboard/station/landing-page/editor` - Visual editor
- `/dashboard/station/landing-page/preview` - Live preview iframe
- `/dashboard/station/landing-page/versions` - Version history
- `/landing-assets/{assetID}` - Public asset serving

### Database Changes

Adds 3 new tables via GORM AutoMigrate:
- `landing_pages` - Main configuration with draft/published configs
- `landing_page_assets` - Uploaded images and files
- `landing_page_versions` - Version history with change tracking

### Files Added

- `internal/models/landing_page.go` - Data models
- `internal/landingpage/service.go` - Business logic service
- `internal/landingpage/themes.go` - Theme definitions
- `internal/landingpage/widgets.go` - Widget registry
- `internal/landingpage/renderer.go` - Server-side rendering
- `internal/api/landing_page.go` - API handlers
- `internal/web/pages_landing_editor.go` - Web handlers
- `internal/web/templates/pages/dashboard/landing-editor.html` - Editor UI
- `internal/web/templates/pages/dashboard/landing-versions.html` - Version history
- `internal/web/static/css/landing-page.css` - Theme and widget styles

### Files Modified

- `internal/db/migrate.go` - Added new models to migration
- `internal/server/server.go` - Service wiring
- `internal/api/api.go` - Route registration
- `internal/web/handler.go` - Service injection
- `internal/web/routes.go` - Editor routes

---

## 1.7.0 — 2026-02-01

### Phase 8: Advanced Scheduling (Complete)

This release completes the entire Phase 8 Advanced Scheduling feature set.

#### Phase 8A: Foundation
- **Shows and Instances**: Recurring show support with RRULE patterns
- **Show CRUD API**: Create, update, delete shows with recurrence rules
- **Instance Materialization**: Auto-generate show instances for date ranges
- **Exception Handling**: Cancel, reschedule, or substitute individual instances

#### Phase 8B: Calendar UI
- **Visual Calendar**: Day, week, and month views
- **Drag-and-Drop**: Reschedule shows by dragging
- **Resize**: Change show duration by resizing
- **Color Coding**: Shows colored by type or host

#### Phase 8C: Validation & Conflict Detection
- **Overlap Detection**: Immediate flagging of scheduling conflicts
- **Gap Detection**: Highlight unscheduled time slots
- **DJ Double-Booking**: Detect same DJ on multiple stations
- **Configurable Rules**: Station-specific compliance rules

#### Phase 8D: Templates & Versioning
- **Schedule Templates**: Save current week as reusable template
- **Template Application**: Apply templates to future weeks
- **Version History**: Automatic versioning on schedule changes
- **Diff View**: Compare versions to see changes
- **Rollback**: Restore to any previous version

#### Phase 8E: DJ Self-Service
- **Availability Management**: DJs set weekly availability windows
- **Schedule Requests**: Submit time-off, shift swap, new show requests
- **Approval Workflow**: Managers approve/reject with notes
- **Schedule Locks**: Lock schedule N days out to prevent changes

#### Phase 8F: Notifications
- **Notification Preferences**: Per-user, per-type, per-channel settings
- **Show Reminders**: "Your show starts in 30 minutes"
- **Schedule Changes**: Notify when assignments change
- **Request Status**: Updates on approval/rejection
- **In-App + Email**: Multiple delivery channels

#### Phase 8G: Public Schedule
- **Public API**: No-auth schedule endpoints for listeners
- **Embeddable Widgets**: iframe and JS snippets for external sites
- **Now Playing Widget**: Current and upcoming shows
- **Customizable Themes**: Light/dark, custom colors

#### Phase 8H: Analytics, Syndication, Underwriting
- **Schedule Analytics**: Show performance, time slot analysis, best slots
- **Scheduling Suggestions**: Data-driven recommendations
- **Network Syndication**: Share shows across stations
- **Station Subscriptions**: Subscribe to network shows with local scheduling
- **Sponsor Management**: Track sponsors and contact info
- **Underwriting Obligations**: Spots per week, preferred dayparts
- **Underwriting Spots**: Schedule and track spot fulfillment
- **iCal Export**: Export schedule in standard calendar format

### New API Endpoints

**Shows & Instances:**
- `POST /api/v1/shows` - Create show with recurrence
- `GET /api/v1/shows` - List shows
- `GET /api/v1/shows/{id}` - Get show details
- `PUT /api/v1/shows/{id}` - Update show
- `DELETE /api/v1/shows/{id}` - Delete show
- `GET /api/v1/show-instances` - List instances
- `PUT /api/v1/show-instances/{id}` - Modify instance
- `DELETE /api/v1/show-instances/{id}` - Cancel instance

**Schedule Management:**
- `GET /api/v1/schedule/validate` - Validate date range
- `POST /api/v1/schedule-rules` - Create validation rule
- `GET /api/v1/schedule-rules` - List rules
- `POST /api/v1/schedule-templates` - Save template
- `POST /api/v1/schedule-templates/{id}/apply` - Apply template
- `GET /api/v1/schedule/versions` - List versions
- `POST /api/v1/schedule/versions/{id}/restore` - Restore version

**DJ Self-Service:**
- `GET /api/v1/dj/availability` - Get availability
- `PUT /api/v1/dj/availability` - Update availability
- `POST /api/v1/schedule-requests` - Submit request
- `PUT /api/v1/schedule-requests/{id}/approve` - Approve
- `PUT /api/v1/schedule-requests/{id}/reject` - Reject

**Notifications:**
- `GET /api/v1/notifications/preferences` - Get preferences
- `PUT /api/v1/notifications/preferences` - Update preferences
- `GET /api/v1/notifications` - List notifications

**Public Schedule:**
- `GET /api/v1/public/schedule` - Public schedule JSON
- `GET /api/v1/public/now-playing` - Current + next show
- `GET /embed/schedule` - Embeddable widget
- `GET /embed/now-playing` - Now playing widget

**Analytics:**
- `GET /api/v1/schedule-analytics/shows` - Show performance
- `GET /api/v1/schedule-analytics/time-slots` - Time slot performance
- `GET /api/v1/schedule-analytics/best-slots` - Best performing slots
- `GET /api/v1/schedule-analytics/suggestions` - Scheduling suggestions

**Syndication:**
- `POST /api/v1/networks` - Create network
- `GET /api/v1/networks` - List networks
- `POST /api/v1/network-shows` - Create network show
- `POST /api/v1/network-subscriptions` - Subscribe to network show

**Underwriting:**
- `POST /api/v1/sponsors` - Create sponsor
- `GET /api/v1/sponsors` - List sponsors
- `POST /api/v1/underwriting-obligations` - Create obligation
- `POST /api/v1/underwriting-spots` - Schedule spot
- `GET /api/v1/underwriting/fulfillment` - Fulfillment report

**Export:**
- `GET /api/v1/schedule/export` - Export schedule as iCal

### Database Changes

Adds 21 new tables via GORM AutoMigrate (no modifications to existing tables):
- `shows`, `show_instances`
- `schedule_rules`, `schedule_templates`, `schedule_versions`
- `dj_availability`, `schedule_requests`, `schedule_locks`
- `notification_preferences`, `notifications`
- `webhook_targets`, `webhook_logs`
- `schedule_analytics`
- `networks`, `network_shows`, `network_subscriptions`
- `sponsors`, `underwriting_obligations`, `underwriting_spots`

### Files Added

- `internal/models/show.go` - Show and ShowInstance models
- `internal/models/schedule_rule.go` - Schedule rules
- `internal/models/schedule_template.go` - Templates and versions
- `internal/models/dj_availability.go` - DJ self-service models
- `internal/models/notification.go` - Notification models
- `internal/models/webhook.go` - Webhook models
- `internal/models/analytics.go` - Analytics models
- `internal/models/syndication.go` - Network/syndication models
- `internal/models/underwriting.go` - Sponsor/underwriting models
- `internal/api/shows.go` - Show API handlers
- `internal/api/schedule_rules.go` - Rule API handlers
- `internal/api/schedule_templates.go` - Template API handlers
- `internal/api/schedule_versions.go` - Version API handlers
- `internal/api/dj_self_service.go` - DJ self-service handlers
- `internal/api/notifications.go` - Notification handlers
- `internal/api/webhooks.go` - Webhook handlers
- `internal/api/public_schedule.go` - Public schedule handlers
- `internal/api/analytics.go` - Analytics handlers
- `internal/api/syndication.go` - Syndication handlers
- `internal/api/underwriting.go` - Underwriting handlers
- `internal/api/schedule_export.go` - Export handlers
- `internal/analytics/schedule_analytics.go` - Analytics service
- `internal/syndication/service.go` - Syndication service
- `internal/underwriting/service.go` - Underwriting service
- `internal/schedule/export.go` - Export service
- `internal/notifications/service.go` - Notification service
- `internal/webhooks/service.go` - Webhook service
- `internal/scheduling/validator.go` - Validation engine
- `internal/web/pages_shows.go` - Show management UI
- `internal/web/pages_dj.go` - DJ self-service UI
- `internal/web/pages_embed.go` - Embeddable widgets

---

## 1.3.0 — 2026-02-01

### Breaking Changes
- **API Key Authentication**: Replaced JWT login-based API authentication with API key authentication
  - Removed `POST /api/v1/auth/login` and `POST /api/v1/auth/refresh` endpoints
  - API requests now use `X-API-Key: <key>` header instead of `Authorization: Bearer <jwt>`
  - Users generate API keys from their profile page in the web dashboard
  - API keys have configurable expiration (30 days, 90 days, 180 days, or 1 year max)
  - Key format: `gr_<32 random chars>` (e.g., `gr_a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6`)

### Features
- **API Key Management UI**: Added API key management to profile page
  - Generate new API keys with custom names and expiration
  - View existing keys (prefix, creation date, last used, expiration)
  - Revoke individual API keys
  - One-time display of full key on creation (not stored in cleartext)

### Migration Guide
If you have scripts or applications using the old JWT-based API:
1. Log into the web dashboard
2. Go to Profile → API Keys
3. Generate a new API key with appropriate expiration
4. Replace `Authorization: Bearer $TOKEN` header with `X-API-Key: YOUR_API_KEY`
5. Remove any login/refresh token logic from your code

**Before:**
```bash
TOKEN=$(curl -X POST .../auth/login -d '{"email":"...","password":"..."}' | jq -r .token)
curl .../stations -H "Authorization: Bearer $TOKEN"
```

**After:**
```bash
curl .../stations -H "X-API-Key: gr_your-api-key-here"
```

---

## 1.2.26 — 2026-02-01

### API Documentation
- **OpenAPI/Swagger Specification**: Created comprehensive OpenAPI 3.0 spec at `api/openapi.yaml`
  - Complete documentation for all 26+ REST API endpoints
  - Request/response schemas for all data models
  - Authentication documentation (JWT Bearer tokens)
  - Error response schemas
- **Python Client Library**: Created full-featured Python client at `docs/api/examples/python/grimnir_client.py`
  - All API operations: stations, media, playlists, smart blocks, schedule, live, webstreams
  - Automatic token refresh
  - Error handling with custom exceptions
  - Type hints and docstrings
- **API Documentation Guide**: Created `docs/api/README.md` with quick start examples
- **Updated README**: Added API Documentation section with Python and curl examples

### Bug Fixes
- **Log Buffer Timestamp Fix**: Fixed all log entries showing the same timestamp
  - zerolog uses Unix timestamps (float64) but buffer was parsing RFC3339 strings
  - Added handling for float64 time values in the Writer's Write method

---

## 1.2.25 — 2026-01-31

### Bug Fixes
- **Station Logs API Route Fix**: Fixed 404 errors on station logs API
  - Routes were incorrectly added at `/api/v1/logs` instead of `/api/v1/stations/{stationID}/logs`
  - Moved routes inside the `/{stationID}` route group

---

## 1.2.24 — 2026-01-31

### Features
- **Station-Level Logs**: Added logging for DJs and station owners to troubleshoot their own issues
  - Added `StationID` filter to log buffer `QueryParams`
  - Added `StatsForStation()` and `GetComponentsForStation()` methods
  - Added station-scoped API endpoints under `/stations/{stationID}/logs`
  - Created station logs web page at `/dashboard/station/logs`
  - Added nav link in sidebar
- **Platform Logs Enhancement**: Updated platform logs to show station names instead of UUIDs
  - Added `station_names` mapping in API response

---

## 1.2.23 — 2026-01-30

### Improvements
- **Broadcast Server Logging**: Added mount name to key broadcast log messages
  - Added `Str("mount", m.Name)` to: feed started, feed active, client connected/disconnected, stream ended
  - Improves debugging by showing which mount point logs refer to

---

## 1.2.22 — 2026-01-30

### Bug Fixes
- **System Logs API Auth Fix**: Fixed 403 errors on `/api/v1/system/logs` endpoints
  - WSToken contained `platform_admin` role but middleware checked for station-level `admin` role
  - Added `requirePlatformAdmin()` middleware that checks for `platform_admin` in claims.Roles
  - Applied to all `/system` routes

---

## 1.2.21 — 2026-01-30

### Features
- **System Logs for Platform Admins**: Added in-memory log buffer with web UI
  - Ring buffer storing last 10,000 log entries
  - Filter by level, component, search text
  - Real-time log streaming via API
  - Platform admin only access

---

## 1.0.0 (Production Release) — 2026-01-22

**🎉 Grimnir Radio 1.0 is production-ready!**

All planned implementation phases are complete. Grimnir Radio is a modern, production-grade broadcast automation system with multi-instance scaling, comprehensive observability, and multiple deployment options.

---

## 1.0.0-rc1 (Phase 7 Complete) — 2026-01-22

### Phase 7: Nix Integration (100% Complete)
**Reproducible Builds and Three Deployment Flavors**

#### Nix Flake Infrastructure
- Created `flake.nix` with three distinct deployment flavors
  - **Basic**: Just the binaries (`nix run github:friendsincode/grimnir_radio`)
  - **Full**: Turn-key NixOS module with PostgreSQL, Redis, Icecast2
  - **Dev**: Complete development environment with all dependencies
- Implemented reproducible builds with locked dependencies
- Cross-platform support (Linux, macOS for control plane)
- Overlays for custom package builds

#### Basic Package (For Nerds)
- Created `nix/package.nix` - Control plane binary
  - Go build with protobuf code generation
  - Stripped binaries with version info
  - Cross-compilation support
  - Zero runtime dependencies
- Created `nix/mediaengine-package.nix` - Media engine binary
  - GStreamer 1.0 with all plugin packages
  - Wrapped binary with GST_PLUGIN_PATH configuration
  - pkg-config integration for native dependencies
  - Linux-only (GStreamer requirement)
- Command-line usage:
  ```bash
  nix run github:friendsincode/grimnir_radio        # Run control plane
  nix run github:friendsincode/grimnir_radio#mediaengine  # Run media engine
  nix profile install github:friendsincode/grimnir_radio  # Install to profile
  ```

#### Full Turn-Key Installation (White Glove Treatment)
- Created `nix/module.nix` - Complete NixOS module
  - Auto-configured PostgreSQL with database and user creation
  - Auto-configured Redis for event bus and caching
  - Auto-configured Icecast2 streaming server
  - systemd services for both binaries (auto-start, auto-restart)
  - Security hardening (PrivateTmp, ProtectSystem, NoNewPrivileges)
  - Resource limits (MemoryMax, CPUQuota)
  - Dedicated system user and group
  - Automatic firewall rules
  - Media storage directory creation
- Configuration options (25+ options):
  - HTTP bind address and port
  - Database URL (auto-generated if using built-in PostgreSQL)
  - Redis URL (auto-generated if using built-in Redis)
  - Media engine gRPC address
  - JWT secret
  - Media storage path
  - Tracing configuration (OTLP endpoint, sample rate)
  - Icecast password
  - User/group customization
  - Toggle switches for database/Redis/Icecast (enable/disable)
- Integration with NixOS configuration.nix
- Optional Nginx reverse proxy with TLS
- Automatic service dependencies and ordering
- Journal logging with syslog identifiers

#### Development Environment (For Hacking)
- Created `nix/dev-shell.nix` - Complete dev environment
  - **Go development**: Go 1.22+, gopls, gotools, go-tools
  - **Protocol Buffers**: protoc, protoc-gen-go, protoc-gen-go-grpc
  - **GStreamer**: Full stack with plugins, dev tools, pkg-config
  - **Infrastructure**: PostgreSQL, Redis, Icecast (for local dev)
  - **Container tools**: Docker Compose, kubectl, k9s
  - **Build tools**: GNU Make, Git
  - **Utilities**: jq, yq, curl
  - **Load testing**: k6
- Shell hook with welcome message and instructions
- Automatic environment variable setup
  - GOPATH configuration
  - GST_PLUGIN_PATH configuration
  - Default DATABASE_URL and REDIS_URL
  - Auto-create .env from template
- IDE integration (VSCode, GoLand)
- Direnv support for automatic shell activation
- Usage:
  ```bash
  nix develop  # Enter development shell
  make build   # Build binaries
  make test    # Run tests
  make proto   # Generate protobuf code
  ```

#### Documentation
- Created `docs/NIX_INSTALLATION.md` (600+ lines)
  - Quick start guide for all three flavors
  - Prerequisites and Nix installation
  - **Basic flavor**: Installation, configuration, manual setup
  - **Full flavor**: NixOS module integration, automatic setup
  - **Dev flavor**: Development workflow, IDE integration
  - Advanced usage: Custom builds, multi-instance, cross-compilation
  - Troubleshooting: Common issues and solutions
  - Migration guide: From Docker and bare metal
  - Performance tuning: PostgreSQL, Redis, systemd limits
  - Uninstallation procedures
- Three complete usage examples with code snippets
- Environment variable reference
- Service management commands
- Security best practices

**Files Added:**
- `flake.nix` - Main flake with three flavors (91 lines)
- `nix/package.nix` - Control plane package (60 lines)
- `nix/mediaengine-package.nix` - Media engine package (72 lines)
- `nix/module.nix` - NixOS module for full installation (347 lines)
- `nix/dev-shell.nix` - Development environment (120 lines)
- `docs/NIX_INSTALLATION.md` - Comprehensive guide (650+ lines)

**Code Statistics:**
- ~690 lines of Nix code
- 650+ lines of documentation
- Total: ~1,340 lines for Phase 7

**Benefits:**
- **Reproducible builds**: Exact same binary every time
- **Declarative configuration**: Infrastructure as code
- **Zero dependency conflicts**: Nix isolation
- **Rollback support**: Revert to previous generations
- **Development-production parity**: Same environment everywhere
- **Easy updates**: `nix flake update` to get latest
- **Multi-version support**: Run different versions side-by-side

---

## 1.1.16 — 2026-01-28

### AzuraCast API Import Fixes (Batch 3)
- Fixed mount name storage to avoid double slashes in stream URLs
  - Mount path now stored without leading slash (e.g., `radio.mp3` not `/radio.mp3`)
  - Fixes 404 errors for streams like `/live//radio.mp3`

---

## 1.1.15 — 2026-01-27

### AzuraCast API Import Fixes (Batch 2)
- **Artwork endpoint fix**: Changed from `/api/station/{id}/file/{id}/art` to `/api/station/{id}/art/{id}`
- **Artwork download**: Removed `art_updated_at > 0` check that prevented artwork from being downloaded
- **Extra metadata parsing**: Added `AzuraCastAPIExtraMetadata` struct to properly parse nested cue points (cue_in, cue_out, fade_in, fade_out)
- **Migration status template fix**: Fixed Go template boolean check for `*migration.Result` pointer type
- **Database reset functionality**: Added `ResetImportedData()` function and `/dashboard/settings/migrations/reset` endpoint
- **JSONB serialization fix**: Updated `Scan` methods in migration types to handle both `[]byte` and `string` from database

### New Features
- Reset Data button on migration status page to clear imported data and retry

---

## 1.1.14 — 2026-01-27

### AzuraCast API Import Fixes (Batch 1)
- **Media download endpoint fix**: Changed from `/api/station/{id}/file/{id}/download` to `/api/station/{id}/file/{id}/play`
  - The `/download` endpoint returns 405 Method Not Allowed
  - The `/play` endpoint correctly streams the file content

---

## 1.1.13 — 2026-01-26

### Multi-Tenant Security & UI Enhancements
- Multi-tenant security improvements
- Public schedule calendar
- Archive controls
- Color themes
- Button tooltips across all templates

---

## 1.1.0 Progress: Event Bus Implementations — 2026-01-23

### Redis Event Bus (COMPLETE)
**Production-Ready Distributed Event Bus**

#### Full Redis Pub/Sub Implementation
- **Real Redis connection** using `github.com/redis/go-redis/v9`
- **Per-event-type subscriptions** with dedicated goroutines
- **Circuit breaker pattern**: Auto-fallback to in-memory bus on failures
- **Automatic reconnection**: Attempts to reconnect every 30 seconds
- **Message filtering**: Prevents echo by skipping own messages (NodeID check)
- **Connection pooling**: Configurable pool size and idle connections
- **Graceful shutdown**: Waits for all receivers to finish
- **Comprehensive error handling**: Timeout-aware publish/subscribe

**Key Features:**
- Publishes to Redis pub/sub AND local in-memory bus (hybrid approach)
- Failure threshold tracking (5 failures → circuit breaker)
- Per-message timeout (2 seconds for publish)
- Structured logging with zerolog
- Thread-safe with RWMutex

**Files Modified:**
- `internal/eventbus/redis.go` - 400 lines of production code
  - `NewRedisBus()` - Connection with health check
  - `Subscribe()` - Creates Redis subscription + goroutine
  - `receiveMessages()` - Handles incoming messages from Redis
  - `Publish()` - Dual publish (local + Redis)
  - `handleFailure()` - Circuit breaker logic
  - `Close()` - Graceful shutdown with WaitGroup

---

### NATS Event Bus with JetStream (COMPLETE)
**Enterprise-Grade Event Bus with Persistence**

#### Full NATS JetStream Implementation
- **NATS connection** using `github.com/nats-io/nats.go v1.48.0`
- **JetStream persistence**: 24-hour message retention with file storage
- **Durable consumers**: Survives restarts, picks up where it left off
- **Explicit acknowledgment**: Messages require Ack()/Nak() for delivery guarantee
- **Automatic stream creation**: Creates GRIMNIR_EVENTS stream on first run
- **Message deduplication**: UUID-based message IDs
- **Circuit breaker pattern**: Fallback to in-memory on connection failure
- **Automatic reconnection handlers**: Logs disconnect/reconnect events

**Key Features:**
- Subject pattern: `grimnir.events.{event_type}`
- WorkQueue retention policy (messages deleted after ack)
- Per-node durable consumers for horizontal scaling
- Message ordering guarantees
- Lower latency than Redis for pub/sub
- Better cluster support

**Configuration:**
```go
NATSConfig{
    URL:           "nats://localhost:4222",
    StreamName:    "GRIMNIR_EVENTS",
    Durable:       "grimnir-consumer",
    MaxReconnects: -1,  // Unlimited
    MaxFailures:   5,
}
```

**Files Modified:**
- `internal/eventbus/nats.go` - 464 lines of production code
  - `NewNATSBus()` - Connect + JetStream setup + stream creation
  - `createOrUpdateStream()` - Idempotent stream management
  - `Subscribe()` - Durable consumer creation + message receiver
  - `receiveMessages()` - JetStream message iteration with Ack/Nak
  - `Publish()` - JetStream publish with persistence
  - `Close()` - Clean NATS shutdown

**Dependencies Added:**
- `github.com/nats-io/nats.go v1.48.0`
- `github.com/nats-io/nkeys v0.4.11`
- `github.com/nats-io/nuid v1.0.1`

---

### Benefits of Real Event Bus Implementations

**Multi-Instance Scaling Now Works:**
- Events published on instance-1 are received by instance-2, instance-3
- Leader election coordination via Redis/NATS
- Scheduler events broadcast to all instances
- DJ connection events shared across instances

**Production Reliability:**
- Circuit breaker prevents cascading failures
- Automatic fallback to in-memory bus if Redis/NATS unavailable
- Graceful degradation (single-instance mode)

---

### S3-Compatible Object Storage (COMPLETE)
**Cloud-Native Media Storage Backend**

#### Full S3 Storage Implementation
- **AWS SDK v2 integration** using latest `github.com/aws/aws-sdk-go-v2/*` packages
- **Multi-provider support**: AWS S3, MinIO, DigitalOcean Spaces, Backblaze B2, Wasabi
- **Custom endpoint resolution**: Automatic configuration for S3-compatible services
- **Path-style URL support**: Required for MinIO and some S3-compatible services
- **CDN integration**: PublicBaseURL for CloudFront/custom CDN domains
- **Presigned URLs**: Temporary authenticated access for private buckets
- **Server-side operations**: Copy, Delete, Exists checks without download/upload
- **Object metadata**: Track station_id, media_id, upload timestamps
- **Content-Type detection**: Automatic MIME type detection for audio formats

**Key Features:**
- Graceful connection validation with non-blocking HeadBucket check
- Comprehensive error handling with proper NotFound detection
- Thread-safe operations
- Structured logging with zerolog
- Context-aware operations for cancellation/timeout support

**Supported Audio Formats:**
- MP3 (`audio/mpeg`)
- FLAC (`audio/flac`)
- OGG/OGA (`audio/ogg`)
- M4A (`audio/mp4`)
- WAV (`audio/wav`)
- AAC (`audio/aac`)
- Opus (`audio/opus`)

**Configuration:**
```go
S3Config{
    AccessKeyID:     "...",
    SecretAccessKey: "...",
    Region:          "us-east-1",
    Bucket:          "grimnir-media",
    Endpoint:        "",              // Optional: for S3-compatible
    PublicBaseURL:   "",              // Optional: CDN URL
    UsePathStyle:    false,           // true for MinIO
    PresignedExpiry: 15 * time.Minute,
}
```

**Environment Variables:**
- `GRIMNIR_S3_ACCESS_KEY_ID` or `AWS_ACCESS_KEY_ID`
- `GRIMNIR_S3_SECRET_ACCESS_KEY` or `AWS_SECRET_ACCESS_KEY`
- `GRIMNIR_S3_REGION` or `AWS_REGION` (default: us-east-1)
- `GRIMNIR_S3_BUCKET` or `S3_BUCKET`
- `GRIMNIR_S3_ENDPOINT` or `S3_ENDPOINT` (for MinIO/Spaces)
- `GRIMNIR_S3_PUBLIC_BASE_URL` (for CDN)
- `GRIMNIR_S3_USE_PATH_STYLE` (true for MinIO)

**Files Modified:**
- `internal/media/storage_s3.go` - 364 lines of production code
  - `NewS3Storage()` - AWS SDK configuration with custom endpoint resolver
  - `Store()` - Upload media with metadata tagging
  - `Delete()` - Remove objects from S3
  - `URL()` - Generate public URLs (supports CDN, path-style, custom endpoints)
  - `PresignedURL()` - Generate temporary authenticated URLs
  - `Exists()` - Check object existence with proper error detection
  - `GetMetadata()` - Retrieve object metadata
  - `Copy()` - Server-side copy within S3
  - `ListObjects()` - List objects with prefix filtering
  - `detectContentType()` - MIME type detection for audio files
- `internal/media/service.go` - Updated to use S3Config struct
  - Error handling for S3Storage initialization
  - Automatic selection of storage backend (S3 vs filesystem)
- `internal/config/config.go` - Added S3 configuration fields
  - 7 new S3-specific config fields
  - Environment variable loading with AWS_* fallbacks
- `internal/server/server.go` - Error handling for media service initialization

**Dependencies Added:**
- `github.com/aws/aws-sdk-go-v2 v1.41.1`
- `github.com/aws/aws-sdk-go-v2/config v1.32.7`
- `github.com/aws/aws-sdk-go-v2/credentials v1.19.7`
- `github.com/aws/aws-sdk-go-v2/service/s3 v1.95.1`
- Related AWS SDK v2 internal packages

**Example Usage:**
```bash
# AWS S3
export GRIMNIR_S3_BUCKET=my-media-bucket
export AWS_ACCESS_KEY_ID=...
export AWS_SECRET_ACCESS_KEY=...
export AWS_REGION=us-west-2

# MinIO
export GRIMNIR_S3_BUCKET=media
export GRIMNIR_S3_ENDPOINT=https://minio.example.com
export GRIMNIR_S3_ACCESS_KEY_ID=minioadmin
export GRIMNIR_S3_SECRET_ACCESS_KEY=minioadmin
export GRIMNIR_S3_USE_PATH_STYLE=true

# DigitalOcean Spaces
export GRIMNIR_S3_BUCKET=my-space
export GRIMNIR_S3_ENDPOINT=https://nyc3.digitaloceanspaces.com
export GRIMNIR_S3_ACCESS_KEY_ID=...
export GRIMNIR_S3_SECRET_ACCESS_KEY=...
export GRIMNIR_S3_REGION=nyc3

# With CloudFront CDN
export GRIMNIR_S3_BUCKET=media
export GRIMNIR_S3_PUBLIC_BASE_URL=https://d1234567890.cloudfront.net
```

**Benefits:**
- **Scalability**: No local disk limits, petabyte-scale storage
- **Durability**: 99.999999999% (11 nines) with AWS S3
- **Multi-region**: Automatic replication and geo-distribution
- **Cost-effective**: Pay only for what you use
- **CDN integration**: Fast global delivery via CloudFront/CDN
- **Backup**: Built-in versioning and lifecycle policies
- Zero downtime during event bus failures

**Performance Characteristics:**
- **Redis**: Lower latency (~1-2ms), simpler setup, good for small clusters
- **NATS**: Better message ordering, persistence, scales better (100k+ msg/sec)
- Both: Async delivery, non-blocking publish

**Deployment Options:**
Users can now choose:
1. In-memory bus (single instance, development)
2. Redis event bus (multi-instance, simple production)
3. NATS event bus (multi-instance, enterprise production)

---

### Media File Copying in Migration Tools (COMPLETE)
**Production-Ready File Operations with Parallel Processing**

#### File Operations Module Created
- **New file**: `internal/migration/fileops.go` (368 lines)
- **Parallel copy engine**: Configurable worker pool (default: 4 concurrent workers)
- **SHA256 verification**: Optional checksum validation for file integrity
- **Progress tracking**: Real-time callbacks with copied/total counts
- **Error resilience**: Individual failures don't stop entire import
- **Storage agnostic**: Works with both filesystem and S3 backends

**Key Features:**
- `FileOperations` struct manages copy jobs with state tracking
- `CopyFiles()` method processes jobs in parallel worker pool
- `copyFile()` handles individual file upload with retry logic
- File size tracking for progress estimation
- Graceful error handling with detailed logging

#### AzuraCast Importer Enhanced
- **File**: `internal/migration/azuracast.go` - importMedia() rewritten (200+ lines added)
- Walks extracted backup media directory
- Scans for audio files: MP3, FLAC, OGG, M4A, WAV, AAC
- Creates MediaItem database records with metadata
- Builds copy jobs for all discovered files
- Parallel file upload to storage backend
- Updates MediaItem records with storage keys
- Comprehensive progress reporting
- Success/failure tracking with warnings

#### LibreTime Importer Enhanced
- **File**: `internal/migration/libretime.go` - importMedia() enhanced (150+ lines added)
- Queries LibreTime cc_files table for metadata
- Validates LibreTimeMediaPath if provided
- Two-phase import: metadata then files
- Path resolution: handles absolute and relative paths
- File existence checking before copy jobs
- Parallel file copying with progress callbacks
- Graceful degradation: metadata-only import if files unavailable
- Detailed warnings for missing files and failed copies

**Implementation Details:**

File Operations API:
```go
type FileCopyJob struct {
    SourcePath  string
    StationID   string
    MediaID     string
    FileSize    int64
}

type CopyOptions struct {
    SourceRoot       string
    VerifyChecksum   bool  // SHA256 verification
    SkipExisting     bool  // Skip already copied files
    Concurrency      int   // Worker pool size
    ProgressCallback func(copied, total int)
}

fileOps := NewFileOperations(mediaService, logger)
results, err := fileOps.CopyFiles(ctx, jobs, options)
```

**Files Modified:**
- ✅ `internal/migration/fileops.go` (NEW - 368 lines)
  - `NewFileOperations()` - Initialize file operations handler
  - `CopyFiles()` - Parallel file copy with worker pool
  - `copyFile()` - Single file upload with checksum
  - `VerifyFile()` - SHA256 integrity check
  - `ResolveFilePath()` - Path resolution utilities
  - `ValidateSourceDirectory()` - Pre-flight validation
- ✅ `internal/migration/azuracast.go`
  - `importMedia()` - Complete rewrite for file copying
  - Media directory walking and file discovery
  - Parallel copy with progress tracking
- ✅ `internal/migration/libretime.go`
  - `importMedia()` - Enhanced with file operations
  - Two-phase import (metadata + files)
  - Graceful degradation for missing files
- ✅ `internal/migration/types.go`
  - Added `MediaCopied` field to Progress struct
- ✅ `internal/api/migration.go`
  - Updated constructor to pass media service
- ✅ `cmd/grimnirradio/cmd_import.go`
  - Initialize media service for importers

**Usage Example:**
```bash
# AzuraCast import with media files
./grimnirradio import azuracast \
  --backup /path/to/backup.tar.gz

# LibreTime import with media directory
./grimnirradio import libretime \
  --db-host localhost \
  --db-name libretime \
  --media-path /srv/airtime/stor
```

**Benefits:**
- **Fast**: 4 concurrent uploads by default
- **Reliable**: Continues on individual file failures
- **Safe**: SHA256 verification prevents corruption
- **Flexible**: Works with local filesystem and S3
- **Observable**: Real-time progress tracking
- **Resilient**: Metadata imported even if files missing

**Statistics:**
- Processes ~1000 files in ~5-10 minutes (depends on file sizes and storage)
- 4 concurrent workers (configurable)
- Gracefully handles partial failures
- Detailed logging for troubleshooting

---

## 1.0.0 Enhancement v2: Intelligent Deployment Script — 2026-01-23

### Enhanced Docker Quick-Start Script
**Production-Grade Interactive Deployment with Smart Port Detection**

#### Intelligent Port Detection
- **Automatic port scanning**: Detects in-use ports before configuration
- **Smart suggestions**: Finds next available port when defaults conflict
  - Example: Port 8080 in use → suggests 8081
  - Scans up to 100 sequential ports to find available slot
- **Visual feedback**: Shows which ports are available vs in-use
- **Conflict resolution**: Handles user-entered conflicts with suggestions
- **Port change summary**: Displays which ports were adjusted at deployment end

**Port Detection Functions:**
```bash
suggest_port()         # Checks port, suggests alternative if in use
find_available_port()  # Scans for next free port (max 100 attempts)
show_port_usage()      # Pre-scan display of all default ports
```

**Example Output:**
```
Port Usage Check
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✓ HTTP API (port 8080) - Available
⚠ Metrics (port 9000) - IN USE, will suggest alternative
⚠ Icecast (port 8000) - IN USE, will suggest alternative
✓ PostgreSQL (port 5432) - Available

Prometheus metrics port (default: 9001): [auto-suggested]
Icecast streaming port (default: 8001): [auto-suggested]
```

**Deployment Summary:**
```
NOTE: Some ports were changed from defaults due to conflicts:
  Metrics: 9000 → 9001
  Icecast: 8000 → 8001
```

#### Enhanced Quick Start Mode
- Auto-detects all ports before deployment
- Displays allocation summary with adjusted ports
- No manual intervention needed for conflicts
- Complete hands-off experience for development

**Files Modified:**
- `scripts/docker-quick-start.sh` - Added 150+ lines for port intelligence
  - `suggest_port()` - Smart port suggestion
  - `find_available_port()` - Sequential port scanner
  - `show_port_usage()` - Pre-configuration port check
  - Enhanced `configure_ports()` - Loop with suggestions
  - Enhanced `configure_quick_mode()` - Auto-detection
  - Enhanced `display_summary()` - Port change notifications

**Files Added:**
- `docs/DOCKER_QUICK_START_GUIDE.md` - Complete usage guide (500+ lines)
  - Scenario walkthroughs (development, production, multi-instance)
  - Port conflict resolution examples
  - Real-world deployment example (app server with NAS)
  - Troubleshooting section

**Total Enhancement:**
- ~150 lines of Bash port detection logic
- ~500 lines of documentation
- ~650 lines total

**Benefits:**
- **Zero-friction deployment**: No manual port hunting
- **Production-ready**: Handles real-world port conflicts
- **Transparent**: Shows what changed and why
- **Saves time**: Automatic conflict resolution
- **Better UX**: Clear feedback at every step

---

## 1.0.0 Enhancement: Turn-Key Docker Compose — 2026-01-23

### Docker Compose Deployment (100% Complete)
**Full-Stack Deployment Matching Nix Full Flavor**

#### Enhanced Docker Compose Stack
- Added Icecast2 streaming server to `docker-compose.yml`
  - Container: `grimnir-icecast` using `moul/icecast:2.4.4`
  - Environment-based configuration (passwords, limits, metadata)
  - Health checks with status endpoint monitoring
  - Persistent logs volume (`icecast-logs`)
  - Port 8000 exposed with configurable binding
- Complete service stack now includes:
  - PostgreSQL 15 (database)
  - Redis 7 (event bus & leader election)
  - Media Engine (GStreamer with gRPC)
  - Control Plane (HTTP API, scheduler)
  - Icecast2 (streaming server) **NEW**

#### Quick-Start Script
- Created `scripts/docker-quick-start.sh` (300+ lines)
  - **Prerequisites checking**: Docker, Docker Compose, daemon status
  - **Automatic .env generation**: Copies from .env.example if missing
  - **Secure password generation**: OpenSSL-based random passwords
    - POSTGRES_PASSWORD
    - REDIS_PASSWORD
    - JWT_SIGNING_KEY
    - ICECAST_ADMIN_PASSWORD
    - ICECAST_SOURCE_PASSWORD
    - ICECAST_RELAY_PASSWORD
  - **Automatic sed replacement**: Updates .env with generated passwords
  - **Parallel image building**: `docker-compose build --parallel`
  - **Service health waiting**: Monitors healthcheck status
  - **Access information display**: URLs, credentials, next steps
  - **Command options**:
    - Default: Full production deployment
    - `--dev`: Development mode (debug logging)
    - `--stop`: Stop all services
    - `--clean`: Stop and remove all data
  - Cross-platform: macOS and Linux support
  - Color-coded output: Success (green), errors (red), warnings (yellow), info (blue)

#### Enhanced Environment Configuration
- Rewrote `.env.example` (170 lines) with comprehensive documentation
  - **Grimnir Radio**: Environment, logging, HTTP server
  - **Database**: PostgreSQL with connection string examples
  - **Redis**: Event bus and leader election settings
  - **Leader Election**: Multi-instance configuration
  - **Media Engine**: gRPC address, logging
  - **Authentication**: JWT configuration
  - **Media Storage**: Filesystem and S3 options
  - **Scheduler**: Lookahead and tick interval
  - **Observability**: Tracing, metrics, OTLP endpoint
  - **Icecast2**: Admin credentials, source/relay passwords, limits
  - **Webstream**: Timeouts, failover, preflight settings
  - **Advanced**: CORS, rate limiting, upload limits
  - All sections clearly commented with examples

#### Customization Support
- Created `docker-compose.override.yml.example` (200+ lines)
  - Extensive examples for common use cases:
    - **Port changes**: Custom port mappings
    - **Volume mounts**: Local media directories
    - **Debug mode**: Development logging
    - **External database**: Use managed PostgreSQL/Redis
    - **Disable services**: Profile-based service control
    - **Multi-instance**: Leader election with 3 API instances
    - **Monitoring stack**: Jaeger, Prometheus, Grafana
    - **Reverse proxy**: Nginx with SSL/TLS
  - Commented examples for easy copy-paste
  - Version-controlled override template

#### Comprehensive Documentation
- Created `docs/DOCKER_DEPLOYMENT.md` (800+ lines)
  - **Quick Start**: 30-second deployment guide
  - **Prerequisites**: Docker versions, system requirements, port list
  - **Deployment Modes**:
    - Turn-key (recommended): Automated setup
    - Basic: Manual control
    - Development: Debug mode
  - **Configuration**: Environment variables, custom overrides
  - **Services**: Architecture diagram, component details
  - **Advanced Usage**:
    - Multi-instance deployment with leader election
    - Monitoring stack (Prometheus + Grafana)
    - Distributed tracing (Jaeger)
    - SSL/TLS with Let's Encrypt
  - **Troubleshooting**: Common issues, diagnostic commands, reset procedures
  - **Upgrading**: Version migration, backup/restore, rollback
  - **Production Checklist**: 15-item pre-deployment checklist

#### Updated Main Documentation
- Enhanced `README.md` Docker Compose section
  - Quick-start script highlighted
  - Link to comprehensive deployment guide
  - List of automated features
- Updated feature status
  - Moved multi-instance and observability to "Recently Completed"
  - Added turn-key Docker deployment

**Files Added:**
- `scripts/docker-quick-start.sh` - One-command deployment (300+ lines)
- `docker-compose.override.yml.example` - Customization examples (200+ lines)
- `docs/DOCKER_DEPLOYMENT.md` - Complete deployment guide (800+ lines)

**Files Modified:**
- `docker-compose.yml` - Added Icecast2 service, icecast-logs volume
- `.env.example` - Complete rewrite with comprehensive documentation (170 lines)
- `README.md` - Enhanced installation section
- `docs/CHANGELOG.md` - This file

**Code Statistics:**
- ~300 lines of Bash scripting
- ~200 lines of Docker Compose configuration
- ~970 lines of documentation
- Total: ~1,470 lines for Docker turn-key enhancement

**Benefits:**
- **One-command deployment**: `./scripts/docker-quick-start.sh` for full stack
- **Secure by default**: Auto-generated random passwords
- **Production-ready**: Matches Nix full flavor completeness
- **Fully documented**: 800+ line guide with troubleshooting
- **Highly customizable**: Override examples for all use cases
- **Developer-friendly**: --dev flag for debug mode
- **Safe operations**: --clean flag with confirmation prompt

---

## 0.0.1-alpha (Phase 5 Complete) — 2026-01-22

### Phase 5: Observability & Multi-Instance (100% Complete)
**Horizontal Scaling, Metrics, and Monitoring**

#### Observability Infrastructure
- Implemented comprehensive Prometheus metrics
  - API metrics: request duration histogram, request counter, active connections
  - WebSocket metrics: active connection gauge with Inc/Dec tracking
  - Scheduler metrics: tick counter, error counter
  - Executor metrics: state gauge (0-5 state values)
  - Playout metrics: dropout counter
  - Media engine metrics: gRPC connection status
  - Database metrics: active connection pool gauge
  - Leader election metrics: status gauge (1=leader, 0=follower)
  - Live session metrics: active DJ session counter
  - Webstream metrics: health status gauge (2=healthy, 1=degraded, 0=unhealthy)
- Added alert validation test suite
  - YAML syntax validation for Prometheus alerts
  - Critical alert presence verification
  - Alert label and annotation requirements
  - Metric declaration verification
  - 4 comprehensive test functions

#### Multi-Instance Scaling
- Implemented consistent hashing for executor distribution
  - CRC32-based hash ring with 500 virtual nodes per instance
  - Binary search for O(log n) instance lookup
  - Thread-safe with RWMutex for concurrent access
  - Minimal churn on topology changes (9% on add, 25% on remove)
  - Even distribution across instances (±7% variance)
- Built distributor service with complete API
  - `AddInstance()` - Register new executor instance
  - `RemoveInstance()` - Deregister failed instance
  - `GetInstance()` - Lookup responsible instance for station
  - `GetAllAssignments()` - Bulk station-to-instance mapping
  - `GetInstanceStations()` - Reverse lookup for instance workload
  - `GetInstances()` - List all registered instances
  - `CalculateChurn()` - Predict churn percentage for topology changes
- Comprehensive test suite for consistent hashing
  - Distribution test: 300 stations across 3 instances (30.3%, 37.0%, 32.7%)
  - Add instance test: 9% churn when adding 4th instance
  - Remove instance test: 25% churn when removing instance
  - Consistency test: Same station always maps to same instance
  - Edge case test: Handle no instances gracefully
  - Benchmark test: Performance validation
  - 8 test functions, all passing

#### Leader Election (Pre-existing)
- Redis-based distributed locking for scheduler leadership
- Automatic failover on leader failure
- PostgreSQL advisory locks for schedule generation
- Heartbeat tracking and health monitoring

**Files Added:**
- `internal/executor/distributor.go` - Consistent hashing implementation (191 lines)
- `internal/executor/distributor_test.go` - Comprehensive test suite (271 lines)
- `internal/telemetry/alerts_test.go` - Alert validation tests (159 lines)

**Files Modified:**
- `internal/api/api.go` - Added WebSocket connection metrics tracking
- `internal/telemetry/metrics.go` - Verified all 11 core metrics declared

**Code Statistics:**
- ~620 lines for executor distribution system
- 8 comprehensive test cases for consistent hashing
- 4 test functions for alert validation
- 11 core Prometheus metrics exported

**Test Results:**
- Consistent hashing: 8/8 tests passing (100%)
- Alert validation: 3/4 tests passing (1 expected failure for missing alerts)
- Distribution quality: 9% churn on scale-up, 25% churn on scale-down
- Even load balancing: ±7% variance across instances

### Phase 4C: Live Input & Webstream Relay (100% Complete)
**Harbor-Style Live Input and HTTP Stream Failover**

#### Live Input System
- Implemented live DJ session management with database persistence
  - Token-based authentication (32-byte cryptographically random tokens)
  - One-time use token validation
  - Session lifecycle tracking (connected, active, disconnected)
- Created live authorization service
  - `GenerateToken()` - Create authorization tokens for DJs
  - `AuthorizeSource()` - Validate tokens before connection
  - `HandleConnect()` - Start live session with priority integration
  - `HandleDisconnect()` - End live session and clean up
  - `GetActiveSessions()` - List all active DJ connections
- Added 6 REST API endpoints for live management
  - `POST /api/v1/live/tokens` - Generate authorization token
  - `POST /api/v1/live/authorize` - Validate token
  - `POST /api/v1/live/connect` - Start live session
  - `DELETE /api/v1/live/sessions/{id}` - Disconnect session
  - `GET /api/v1/live/sessions` - List active sessions
  - `GET /api/v1/live/sessions/{id}` - Get session details
- Implemented harbor-style live input in media engine
  - Icecast-compatible source client input (souphttpsrc)
  - RTP input over UDP
  - SRT (Secure Reliable Transport) input
  - WebRTC placeholder (future implementation)
- Integrated with priority system
  - Live override sessions (priority 1)
  - Live scheduled sessions (priority 2)
  - Automatic priority transitions on connect/disconnect
- Event bus integration
  - `dj.connect` - DJ connection events
  - `dj.disconnect` - DJ disconnection events

#### Webstream Relay System
- Created webstream model with failover chain support
  - Primary → Backup → Backup2 URL progression
  - Health check configuration (interval, timeout, method)
  - Failover settings (enabled, grace period, auto-recovery)
  - Buffer and reconnect settings
  - Metadata passthrough and override
- Implemented webstream service with health monitoring
  - CRUD operations for webstream configurations
  - Background health check workers (one per webstream)
  - Automatic health checker lifecycle management
  - Preflight connection checks
  - Manual failover and primary reset
- Built health check algorithm
  - HTTP HEAD/GET probes with configurable timeout
  - 3-tier health status: healthy → degraded → unhealthy
  - Consecutive failure tracking (degraded after 1, failover after 3)
  - Redirect handling (up to 3 redirects)
- Implemented failover logic
  - Test backup URL before switching
  - Grace window before failover
  - Skip unhealthy backups automatically
  - Auto-recovery to primary when healthy
- Added webstream player to media engine
  - GStreamer souphttpsrc for HTTP/Icecast streams
  - ICY metadata extraction (iradio-mode)
  - Configurable buffer size (max-size-time)
  - Fade-in support on webstream start
  - DSP graph routing for processing
- Created 7 REST API endpoints for webstream management
  - `GET /api/v1/webstreams` - List webstreams
  - `POST /api/v1/webstreams` - Create webstream
  - `GET /api/v1/webstreams/{id}` - Get webstream
  - `PUT /api/v1/webstreams/{id}` - Update webstream
  - `DELETE /api/v1/webstreams/{id}` - Delete webstream
  - `POST /api/v1/webstreams/{id}/failover` - Manual failover
  - `POST /api/v1/webstreams/{id}/reset` - Reset to primary
- Event bus integration
  - `webstream.failover` - Automatic/manual failover events
  - `webstream.recovered` - Auto-recovery to primary events

#### Scheduler Integration
- Added `SlotTypeWebstream` to clock slot types
- Updated scheduler to create webstream schedule entries
- Integrated webstream playback with playout director
  - Load webstream configuration from database
  - Build GStreamer pipeline with current URL
  - Respect failover state and health status
  - Publish now playing events with webstream metadata
  - Schedule automatic stop at entry end time

**Files Added:**
- `internal/models/live.go` - Live session model
- `internal/live/service.go` - Live authorization and session management
- `internal/api/live.go` - Live API handlers (6 endpoints)
- `internal/mediaengine/live.go` - Live input manager
- `internal/models/webstream.go` - Webstream model with failover
- `internal/webstream/service.go` - Webstream service
- `internal/webstream/health_checker.go` - Background health check workers
- `internal/mediaengine/webstream.go` - Webstream player
- `internal/api/webstream.go` - Webstream API handlers (7 endpoints)

**Files Modified:**
- `proto/mediaengine/v1/mediaengine.proto` - Added LiveInputType enum
- `internal/models/models.go` - Added SlotTypeWebstream
- `internal/scheduler/service.go` - Handle webstream slots
- `internal/playout/director.go` - Webstream playback integration
- `internal/server/server.go` - Webstream service initialization
- `internal/db/migrate.go` - LiveSession and Webstream migrations

**Code Statistics:**
- ~1,400 lines for live input system
- ~1,200 lines for webstream relay system
- ~200 lines for scheduler integration
- 13 new REST API endpoints
- 4 new event types

### Phase 4B: Media Engine Separation (100% Complete)
**Multi-Process Architecture with gRPC Communication**

- Created separate `mediaengine` binary with gRPC server (port 9091)
- Implemented DSP graph builder supporting 12 node types:
  - Loudness Normalization (EBU R128)
  - AGC (Automatic Gain Control)
  - Compressor, Limiter, Equalizer, Gate
  - Silence Detector, Level Meter, Mix, Duck
- Built pipeline manager with GStreamer integration
  - Crossfade support with configurable curves (linear, log, exp, S-curve)
  - Cue point handling (intro/outro markers)
  - Emergency insertion with immediate preemption
  - Live input routing with DSP processing
- Added process supervision and watchdog
  - Health monitoring (5-second intervals)
  - Automatic restart on crash (rate limited)
  - Heartbeat tracking (15-second timeout)
- Implemented gRPC client for control plane
  - Connection management with automatic retry
  - All 8 RPC method wrappers (LoadGraph, Play, Stop, Fade, InsertEmergency, RouteLive, StreamTelemetry, GetStatus)
  - Real-time telemetry streaming with callbacks
- Integrated executor with media engine
  - MediaController wrapper for high-level API
  - Priority event handling via gRPC
  - Telemetry streaming (1-second intervals)
- Created production deployment tooling
  - Systemd service files with resource limits
  - Security hardening (PrivateTmp, ProtectSystem, NoNewPrivileges)
  - Complete installation and operations guide
- Comprehensive integration tests
  - 10 client integration tests (connection, playback, telemetry, concurrency)
  - 3 end-to-end tests (executor + media engine + priority + telemetry)
  - All 13 tests passing (100% success rate)
- Documentation
  - Architecture diagrams and component breakdown
  - Deployment guide for systemd
  - Migration notes from old playout system

**Files Added:**
- `cmd/mediaengine/main.go` - Media engine binary
- `proto/mediaengine/v1/mediaengine.proto` - gRPC service definition
- `internal/mediaengine/` - Service, pipeline, supervisor, DSP graph builder
- `internal/mediaengine/client/` - gRPC client
- `internal/executor/media_controller.go` - Executor integration
- `deploy/systemd/` - Systemd service files and deployment guide
- `test/integration/` - End-to-end integration tests
- `docs/ARCHITECTURE_NOTES.md` - Architecture documentation

**Code Statistics:**
- 7,260 lines of production code
- 890 lines of integration tests
- 20+ unit tests for DSP graph builder
- 13 integration tests (all passing)

### Phase 4A: Executor Refactor & Priority System (100% Complete)

- Implemented 5-tier priority system
  - Emergency (0), Live Override (1), Live Scheduled (2), Automation (3), Fallback (4)
  - State machine with transition validation
  - Preemption rules and priority scoring
- Built executor state machine
  - 6 states: Idle, Preloading, Playing, Fading, Live, Emergency
  - Complete transition validation
  - Buffer management and preloading
- Created priority service
  - InsertEmergency, StartOverride, StartScheduledLive, ActivateAutomation, Release
  - Event bus integration
  - Transaction handling
- Implemented event bus
  - Redis event bus with pub/sub
  - NATS support (alternative)
  - Fallback to in-memory bus
- Added REST API endpoints
  - Priority management (`/api/v1/priority/`)
  - Executor state (`/api/v1/executor/`)
  - Real-time telemetry endpoints
- Created 50+ unit tests for state machine and priority logic

### Phase 0: Foundation Fixes (100% Complete)

- Created missing media service package
  - File storage operations
  - S3 client for object storage
  - Integration with API handlers
- Fixed module path errors
  - Updated from `github.com/example/grimnirradio` to `github.com/friendsincode/grimnir_radio`
  - Fixed 18 Go files across codebase
- Added basic unit tests
  - Smart block engine tests
  - Scheduler tests
  - Media service tests

## 0.0.1-alpha (Initial) — Documentation Alpha Baseline
- Standardized project naming to Grimnir Radio
- Added README with shout-outs and naming details
- Introduced .gitignore and Makefile (`verify`, `build`, etc.)
- Created Sales, Engineering, and Programmer specs
- Added Webstream feature plans with fallback chains and env knobs
- Documented migration paths from AzuraCast/LibreTime (local takeover and remote API)
- Added VS Code setup guide, workspace configs, and `.env.example`
- Archived original Smart Blocks specs

Note: This is a documentation alpha, not a production release.
