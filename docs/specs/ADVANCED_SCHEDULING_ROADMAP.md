# Advanced Scheduling Roadmap

**Parent Phase:** Phase 8 (from main roadmap)
**Status:** ✅ Complete (v1.7.0)

This document breaks down the Advanced Scheduling feature into implementable milestones.

> **Note:** All phases (8A-8H) have been implemented and released in version 1.7.0.

---

## Overview

```
Phase 8A: Foundation        → Data model, basic recurring shows
Phase 8B: Calendar UI       → Visual interface, drag-and-drop
Phase 8C: Validation        → Conflict detection, compliance rules
Phase 8D: Templates         → Save/load templates, versioning
Phase 8E: DJ Self-Service   → Availability, shift swaps, approvals
Phase 8F: Notifications     → Email/SMS reminders, alerts
Phase 8G: Public Schedule   → Widgets, feeds, embeds
Phase 8H: Advanced          → Analytics, syndication, underwriting
```

---

## Phase 8A: Foundation

**Goal:** Core data model and basic recurring show support

**Data Model:**
```sql
-- Show definitions (the recurring pattern)
CREATE TABLE shows (
  id UUID PRIMARY KEY,
  station_id UUID NOT NULL REFERENCES stations(id),
  name VARCHAR(255) NOT NULL,
  description TEXT,
  artwork_path VARCHAR(512),
  host_user_id UUID REFERENCES users(id),
  default_duration_minutes INT NOT NULL DEFAULT 60,
  color VARCHAR(7),  -- hex color for calendar
  rrule TEXT,  -- RFC 5545 recurrence rule (e.g., "FREQ=WEEKLY;BYDAY=MO;BYHOUR=19")
  dtstart TIMESTAMP NOT NULL,  -- first occurrence
  dtend TIMESTAMP,  -- end of recurrence (NULL = forever)
  timezone VARCHAR(64) NOT NULL DEFAULT 'UTC',
  active BOOLEAN NOT NULL DEFAULT true,
  metadata JSONB,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);

-- Materialized show instances (actual scheduled occurrences)
CREATE TABLE show_instances (
  id UUID PRIMARY KEY,
  show_id UUID NOT NULL REFERENCES shows(id),
  station_id UUID NOT NULL REFERENCES stations(id),
  starts_at TIMESTAMP NOT NULL,
  ends_at TIMESTAMP NOT NULL,
  host_user_id UUID REFERENCES users(id),  -- can override show default
  status VARCHAR(32) NOT NULL DEFAULT 'scheduled',  -- scheduled, cancelled, completed
  exception_type VARCHAR(32),  -- NULL, 'cancelled', 'rescheduled', 'substitute'
  exception_note TEXT,
  metadata JSONB,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_show_instances_time ON show_instances(station_id, starts_at, ends_at);
```

**API Endpoints:**
```
POST   /api/v1/shows                    # Create show with recurrence
GET    /api/v1/shows                    # List shows
GET    /api/v1/shows/{id}               # Get show details
PUT    /api/v1/shows/{id}               # Update show
DELETE /api/v1/shows/{id}               # Delete show (and future instances)

GET    /api/v1/show-instances           # List instances (with date range filter)
PUT    /api/v1/show-instances/{id}      # Modify single instance (exception)
DELETE /api/v1/show-instances/{id}      # Cancel single instance
POST   /api/v1/shows/{id}/materialize   # Generate instances for date range
```

**Implementation:**
- [ ] Add `shows` and `show_instances` models
- [ ] RRULE parsing library (use `github.com/teambition/rrule-go` or similar)
- [ ] Show CRUD API handlers
- [ ] Instance materialization (generate instances from RRULE for date range)
- [ ] Exception handling (cancel, reschedule, substitute host)
- [ ] Integration with existing schedule_entries table
- [ ] Migration to add tables

**Acceptance Criteria:**
- Can create a recurring show (e.g., "Every Monday 7-9pm")
- Instances auto-generate for requested date range
- Can cancel or modify individual instances
- Shows appear in existing schedule views

---

## Phase 8B: Calendar UI

**Goal:** Visual calendar interface for schedule management

**Dependencies:** Phase 8A complete

**Features:**
- Day view (hourly grid)
- Week view (7-day grid)
- Month view (overview)
- Drag-and-drop to reschedule
- Resize to change duration
- Click to create new show/instance
- Color-coded by show or host
- Filter by station, show type, host

**Implementation:**
- [ ] Calendar component (recommend: FullCalendar.js or similar)
- [ ] HTMX integration for server-driven updates
- [ ] API endpoint for calendar data feed
- [ ] Drag-and-drop handlers (PATCH instance times)
- [ ] Create show modal/form
- [ ] Instance detail popover
- [ ] View switching (day/week/month)
- [ ] Filter controls

**API Endpoints:**
```
GET /api/v1/calendar/events?start=...&end=...&station_id=...  # Calendar feed
```

**Acceptance Criteria:**
- Can view schedule in day/week/month views
- Can drag show to new time slot
- Can resize show duration
- Can click to create new show
- Changes persist immediately

---

## Phase 8C: Validation & Conflict Detection

**Goal:** Prevent scheduling errors before they happen

**Dependencies:** Phase 8A complete (8B helpful but not required)

**Data Model:**
```sql
-- Compliance rules
CREATE TABLE schedule_rules (
  id UUID PRIMARY KEY,
  station_id UUID NOT NULL REFERENCES stations(id),
  name VARCHAR(255) NOT NULL,
  rule_type VARCHAR(64) NOT NULL,  -- 'station_id_interval', 'content_restriction', 'required_break', etc.
  config JSONB NOT NULL,  -- rule-specific configuration
  severity VARCHAR(32) NOT NULL DEFAULT 'warning',  -- 'error', 'warning', 'info'
  active BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMP NOT NULL
);
```

**Rule Types:**
- `overlap` - Two shows at same time (built-in, always error)
- `gap` - Unscheduled time > N minutes (configurable threshold)
- `dj_double_booking` - Same DJ on multiple stations
- `station_id_interval` - Station ID required every N minutes
- `content_restriction` - Certain tags/content only in specific dayparts
- `min_duration` - Show must be at least N minutes
- `max_duration` - Show cannot exceed N minutes
- `required_break` - Must have break at specific times

**API Endpoints:**
```
GET    /api/v1/schedule/validate?start=...&end=...  # Validate date range
POST   /api/v1/schedule-rules                       # Create rule
GET    /api/v1/schedule-rules                       # List rules
PUT    /api/v1/schedule-rules/{id}                  # Update rule
DELETE /api/v1/schedule-rules/{id}                  # Delete rule
```

**Implementation:**
- [ ] Validation engine (runs rules against schedule)
- [ ] Real-time validation on calendar UI (show warnings inline)
- [ ] Validation API endpoint
- [ ] Rule CRUD
- [ ] Built-in rules (overlap, gap)
- [ ] Configurable rules (station ID, content restrictions)
- [ ] Validation report generation

**Acceptance Criteria:**
- Overlap immediately flagged when creating/moving shows
- Gaps highlighted in calendar view
- DJ double-booking detected across stations
- Compliance rules configurable per station
- Can run validation report for date range

---

## Phase 8D: Templates & Versioning

**Goal:** Save, reuse, and track schedule changes

**Dependencies:** Phase 8A complete

**Data Model:**
```sql
-- Schedule templates
CREATE TABLE schedule_templates (
  id UUID PRIMARY KEY,
  station_id UUID NOT NULL REFERENCES stations(id),
  name VARCHAR(255) NOT NULL,
  description TEXT,
  template_data JSONB NOT NULL,  -- serialized week of shows
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);

-- Schedule versions (for history/rollback)
CREATE TABLE schedule_versions (
  id UUID PRIMARY KEY,
  station_id UUID NOT NULL REFERENCES stations(id),
  version_number INT NOT NULL,
  snapshot_data JSONB NOT NULL,  -- full schedule state
  change_summary TEXT,
  changed_by UUID REFERENCES users(id),
  created_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_schedule_versions_station ON schedule_versions(station_id, version_number DESC);
```

**API Endpoints:**
```
POST   /api/v1/schedule-templates                   # Save current week as template
GET    /api/v1/schedule-templates                   # List templates
GET    /api/v1/schedule-templates/{id}              # Get template
DELETE /api/v1/schedule-templates/{id}              # Delete template
POST   /api/v1/schedule-templates/{id}/apply        # Apply template to date range

GET    /api/v1/schedule/versions                    # List versions
GET    /api/v1/schedule/versions/{id}               # Get version details
POST   /api/v1/schedule/versions/{id}/restore       # Restore to version
GET    /api/v1/schedule/versions/{id}/diff          # Diff between versions
```

**Implementation:**
- [ ] Template serialization (week → JSON)
- [ ] Template application (JSON → instances for target week)
- [ ] Auto-versioning on schedule changes
- [ ] Version diff calculation
- [ ] Rollback functionality
- [ ] UI for template management
- [ ] UI for version history

**Acceptance Criteria:**
- Can save current week as named template
- Can apply template to future week
- Every schedule change creates version
- Can view diff between versions
- Can rollback to previous version

---

## Phase 8E: DJ Self-Service

**Goal:** Let DJs manage their own availability and request changes

**Dependencies:** Phase 8A, 8C (for validation)

**Data Model:**
```sql
-- DJ availability windows
CREATE TABLE dj_availability (
  id UUID PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id),
  station_id UUID REFERENCES stations(id),  -- NULL = all stations
  day_of_week INT,  -- 0-6 (NULL = specific date)
  specific_date DATE,  -- for one-off availability
  start_time TIME NOT NULL,
  end_time TIME NOT NULL,
  available BOOLEAN NOT NULL DEFAULT true,  -- true = available, false = unavailable
  note TEXT,
  created_at TIMESTAMP NOT NULL
);

-- Schedule change requests
CREATE TABLE schedule_requests (
  id UUID PRIMARY KEY,
  station_id UUID NOT NULL REFERENCES stations(id),
  request_type VARCHAR(32) NOT NULL,  -- 'new_show', 'swap', 'cancel', 'time_off'
  requester_id UUID NOT NULL REFERENCES users(id),
  target_instance_id UUID REFERENCES show_instances(id),
  swap_with_user_id UUID REFERENCES users(id),
  proposed_data JSONB,  -- new times, etc.
  status VARCHAR(32) NOT NULL DEFAULT 'pending',  -- pending, approved, rejected
  reviewed_by UUID REFERENCES users(id),
  reviewed_at TIMESTAMP,
  review_note TEXT,
  created_at TIMESTAMP NOT NULL
);

-- Schedule locks
CREATE TABLE schedule_locks (
  id UUID PRIMARY KEY,
  station_id UUID NOT NULL REFERENCES stations(id),
  lock_before_days INT NOT NULL DEFAULT 7,  -- lock schedule this many days out
  min_role VARCHAR(32) NOT NULL DEFAULT 'manager',  -- minimum role to bypass lock
  created_at TIMESTAMP NOT NULL
);
```

**API Endpoints:**
```
# Availability
GET    /api/v1/dj/availability                      # Get my availability
PUT    /api/v1/dj/availability                      # Update my availability
GET    /api/v1/users/{id}/availability              # Get DJ's availability (manager+)

# Requests
POST   /api/v1/schedule-requests                    # Submit request
GET    /api/v1/schedule-requests                    # List requests (filtered by role)
GET    /api/v1/schedule-requests/{id}               # Get request details
PUT    /api/v1/schedule-requests/{id}/approve       # Approve request (manager+)
PUT    /api/v1/schedule-requests/{id}/reject        # Reject request (manager+)

# Locks
GET    /api/v1/schedule-locks                       # Get lock settings
PUT    /api/v1/schedule-locks                       # Update lock settings (admin)
```

**Implementation:**
- [ ] Availability model and CRUD
- [ ] Availability UI for DJs
- [ ] Request submission system
- [ ] Approval workflow UI for managers
- [ ] Schedule lock enforcement
- [ ] Notifications on request status change
- [ ] Calendar integration (show DJ availability)

**Acceptance Criteria:**
- DJ can set weekly availability
- DJ can request time off
- DJ can request shift swap
- Manager sees pending requests queue
- Manager can approve/reject with notes
- Locked dates prevent DJ edits

---

## Phase 8F: Notifications

**Goal:** Keep everyone informed about schedule changes

**Dependencies:** Phase 8A, 8E (for DJ associations)

**Data Model:**
```sql
-- Notification preferences
CREATE TABLE notification_preferences (
  id UUID PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id),
  notification_type VARCHAR(64) NOT NULL,  -- 'show_reminder', 'schedule_change', 'request_status'
  channel VARCHAR(32) NOT NULL,  -- 'email', 'sms', 'push'
  enabled BOOLEAN NOT NULL DEFAULT true,
  config JSONB,  -- e.g., reminder_minutes: 30
  created_at TIMESTAMP NOT NULL
);

-- Notification log
CREATE TABLE notifications (
  id UUID PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id),
  notification_type VARCHAR(64) NOT NULL,
  channel VARCHAR(32) NOT NULL,
  subject VARCHAR(255),
  body TEXT NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'pending',  -- pending, sent, failed
  sent_at TIMESTAMP,
  error TEXT,
  metadata JSONB,
  created_at TIMESTAMP NOT NULL
);
```

**Notification Types:**
- `show_reminder` - "Your show starts in 30 minutes"
- `schedule_change` - "Your Monday show has been moved to Tuesday"
- `request_status` - "Your shift swap request was approved"
- `new_assignment` - "You've been assigned to cover Jazz Hour"
- `schedule_published` - "Next week's schedule is now available"

**Implementation:**
- [ ] Notification preference UI
- [ ] Email sending (SMTP or service like SendGrid)
- [ ] SMS sending (Twilio or similar) - optional
- [ ] Reminder scheduler (background job)
- [ ] Schedule change detector
- [ ] Notification templates
- [ ] Notification history view

**API Endpoints:**
```
GET    /api/v1/notifications/preferences            # Get my preferences
PUT    /api/v1/notifications/preferences            # Update preferences
GET    /api/v1/notifications                        # Get my notification history
POST   /api/v1/notifications/test                   # Send test notification
```

**Acceptance Criteria:**
- DJ gets reminder before their show
- DJ notified when their schedule changes
- Request status changes trigger notifications
- Users can configure notification preferences
- Can opt out of notifications

---

## Phase 8G: Public Schedule

**Goal:** Let listeners see what's on and what's coming

**Dependencies:** Phase 8A

**API Endpoints (Public - No Auth):**
```
GET /api/v1/public/schedule?station_id=...&start=...&end=...  # JSON schedule
GET /api/v1/public/schedule.ics?station_id=...                 # iCal feed
GET /api/v1/public/schedule.rss?station_id=...                 # RSS feed
GET /api/v1/public/now-playing?station_id=...                  # Current + next show
```

**Features:**
- Public schedule JSON API
- iCal feed (subscribe in calendar apps)
- RSS feed of upcoming shows
- Embeddable widget (iframe + JS snippet)
- "Now playing" + "Up next" endpoint
- Social media integration hooks (webhooks for "show starting")

**Implementation:**
- [ ] Public schedule API (no auth required)
- [ ] iCal generation (RFC 5545)
- [ ] RSS feed generation
- [ ] Embeddable widget HTML/JS
- [ ] Widget customization options (colors, size)
- [ ] Webhook for show transitions
- [ ] Social media auto-post integration (optional)

**Widget Example:**
```html
<iframe src="https://station.com/embed/schedule?theme=dark" width="300" height="400"></iframe>
<!-- or -->
<script src="https://station.com/embed/schedule.js" data-station="xyz" data-theme="dark"></script>
```

**Acceptance Criteria:**
- Listeners can view schedule without login
- Can subscribe to schedule in Google Calendar
- Widget embeds on external sites
- "Now playing" shows current and next show

---

## Phase 8H: Advanced Features

**Goal:** Analytics, syndication, and underwriting

**Dependencies:** All previous phases

### Analytics Integration

```sql
-- Schedule analytics (aggregated from listener data)
CREATE TABLE schedule_analytics (
  id UUID PRIMARY KEY,
  station_id UUID NOT NULL REFERENCES stations(id),
  show_id UUID REFERENCES shows(id),
  date DATE NOT NULL,
  hour INT NOT NULL,  -- 0-23
  avg_listeners INT,
  peak_listeners INT,
  tune_ins INT,
  tune_outs INT,
  created_at TIMESTAMP NOT NULL
);
```

**Features:**
- Listener count correlation with shows
- Best/worst performing time slots
- Show performance trends
- Scheduling suggestions based on data

### Syndication

```sql
-- Network shows (shared across stations)
CREATE TABLE network_shows (
  id UUID PRIMARY KEY,
  network_id UUID,  -- for grouping
  source_show_id UUID REFERENCES shows(id),
  name VARCHAR(255) NOT NULL,
  feed_url TEXT,  -- for external syndicated content
  delay_minutes INT DEFAULT 0,  -- delayed broadcast
  created_at TIMESTAMP NOT NULL
);

-- Station subscriptions to network shows
CREATE TABLE network_subscriptions (
  id UUID PRIMARY KEY,
  station_id UUID NOT NULL REFERENCES stations(id),
  network_show_id UUID NOT NULL REFERENCES network_shows(id),
  local_time TIME,  -- when to air locally
  active BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMP NOT NULL
);
```

**Features:**
- Define shows that air on multiple stations
- Delayed broadcast scheduling
- External feed subscription

### Underwriting

```sql
-- Sponsors
CREATE TABLE sponsors (
  id UUID PRIMARY KEY,
  station_id UUID NOT NULL REFERENCES stations(id),
  name VARCHAR(255) NOT NULL,
  contact_info JSONB,
  active BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMP NOT NULL
);

-- Underwriting obligations
CREATE TABLE underwriting_obligations (
  id UUID PRIMARY KEY,
  sponsor_id UUID NOT NULL REFERENCES sponsors(id),
  station_id UUID NOT NULL REFERENCES stations(id),
  spots_per_week INT NOT NULL,
  spot_duration_seconds INT NOT NULL DEFAULT 30,
  preferred_dayparts JSONB,  -- e.g., ["morning", "afternoon"]
  start_date DATE NOT NULL,
  end_date DATE,
  created_at TIMESTAMP NOT NULL
);

-- Underwriting spots (scheduled)
CREATE TABLE underwriting_spots (
  id UUID PRIMARY KEY,
  obligation_id UUID NOT NULL REFERENCES underwriting_obligations(id),
  scheduled_at TIMESTAMP NOT NULL,
  aired_at TIMESTAMP,
  status VARCHAR(32) NOT NULL DEFAULT 'scheduled',
  created_at TIMESTAMP NOT NULL
);
```

**Features:**
- Sponsor management
- Obligation tracking (X spots per week)
- Spot scheduling integrated with main schedule
- Fulfillment reporting

### Import/Export

**Features:**
- Import from Google Calendar (OAuth + API)
- Import from iCal file upload
- Export to iCal
- Export to PDF (printable schedule)

---

## Milestone Summary

| Phase | Name | Key Deliverable | Est. Effort |
|-------|------|-----------------|-------------|
| 8A | Foundation | Recurring shows with RRULE | Medium |
| 8B | Calendar UI | Visual drag-and-drop calendar | Large |
| 8C | Validation | Conflict detection engine | Medium |
| 8D | Templates | Save/restore schedule versions | Medium |
| 8E | DJ Self-Service | Availability + request workflow | Large |
| 8F | Notifications | Email/SMS reminders | Medium |
| 8G | Public Schedule | Widgets + feeds | Small |
| 8H | Advanced | Analytics, syndication, underwriting | Large |

**Recommended Order:** 8A → 8B → 8C → 8D → 8G → 8E → 8F → 8H

(8G can be done early since it just needs basic show data)

---

## Technical Notes

**RRULE Library:** Use `github.com/teambition/rrule-go` for Go RRULE parsing

**Calendar UI:** Consider FullCalendar.js (MIT license) with HTMX for server integration

**Timezones:** Store all times as UTC in database, convert for display using user's timezone preference

**Materialization Strategy:** Generate instances on-demand for requested date range (don't pre-generate years ahead). Cache materialized instances for performance.

**Validation Performance:** Run validation asynchronously for large ranges, synchronously for single-instance changes.
