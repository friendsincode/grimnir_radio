/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// station role permissions
// ---------------------------------------------------------------------------

// The permission matrix is what the API authorizes against, so each role's
// grants are pinned explicitly rather than derived from the code under test.
func TestDefaultPermissionsForRole(t *testing.T) {
	t.Run("owner and admin get everything", func(t *testing.T) {
		for _, role := range []StationRole{StationRoleOwner, StationRoleAdmin} {
			p := DefaultPermissionsForRole(role)
			for name, granted := range map[string]bool{
				"CanUploadMedia":       p.CanUploadMedia,
				"CanDeleteMedia":       p.CanDeleteMedia,
				"CanEditMetadata":      p.CanEditMetadata,
				"CanManagePlaylists":   p.CanManagePlaylists,
				"CanManageSmartBlocks": p.CanManageSmartBlocks,
				"CanManageSchedule":    p.CanManageSchedule,
				"CanManageClocks":      p.CanManageClocks,
				"CanGoLive":            p.CanGoLive,
				"CanKickDJ":            p.CanKickDJ,
				"CanRecord":            p.CanRecord,
				"CanManageRecordings":  p.CanManageRecordings,
				"CanManageUsers":       p.CanManageUsers,
				"CanManageSettings":    p.CanManageSettings,
				"CanViewAnalytics":     p.CanViewAnalytics,
				"CanManageMounts":      p.CanManageMounts,
			} {
				if !granted {
					t.Fatalf("%s: %s not granted", role, name)
				}
			}
		}
	})

	// A manager runs the schedule but cannot change who has access, alter
	// station settings, reconfigure mounts, or kick a live DJ off air.
	t.Run("manager stops short of user and station control", func(t *testing.T) {
		p := DefaultPermissionsForRole(StationRoleManager)
		for name, granted := range map[string]bool{
			"CanUploadMedia":       p.CanUploadMedia,
			"CanDeleteMedia":       p.CanDeleteMedia,
			"CanManagePlaylists":   p.CanManagePlaylists,
			"CanManageSmartBlocks": p.CanManageSmartBlocks,
			"CanManageSchedule":    p.CanManageSchedule,
			"CanManageClocks":      p.CanManageClocks,
			"CanGoLive":            p.CanGoLive,
			"CanRecord":            p.CanRecord,
			"CanManageRecordings":  p.CanManageRecordings,
			"CanViewAnalytics":     p.CanViewAnalytics,
		} {
			if !granted {
				t.Fatalf("manager should have %s", name)
			}
		}
		for name, granted := range map[string]bool{
			"CanKickDJ":         p.CanKickDJ,
			"CanManageUsers":    p.CanManageUsers,
			"CanManageSettings": p.CanManageSettings,
			"CanManageMounts":   p.CanManageMounts,
		} {
			if granted {
				t.Fatalf("manager should not have %s", name)
			}
		}
	})

	// A DJ can add and tag their own material and go on air, and nothing else.
	t.Run("dj is upload, tag, live and record only", func(t *testing.T) {
		p := DefaultPermissionsForRole(StationRoleDJ)
		for name, granted := range map[string]bool{
			"CanUploadMedia":  p.CanUploadMedia,
			"CanEditMetadata": p.CanEditMetadata,
			"CanGoLive":       p.CanGoLive,
			"CanRecord":       p.CanRecord,
		} {
			if !granted {
				t.Fatalf("dj should have %s", name)
			}
		}
		for name, granted := range map[string]bool{
			"CanDeleteMedia":       p.CanDeleteMedia,
			"CanManagePlaylists":   p.CanManagePlaylists,
			"CanManageSmartBlocks": p.CanManageSmartBlocks,
			"CanManageSchedule":    p.CanManageSchedule,
			"CanManageClocks":      p.CanManageClocks,
			"CanKickDJ":            p.CanKickDJ,
			"CanManageRecordings":  p.CanManageRecordings,
			"CanManageUsers":       p.CanManageUsers,
			"CanManageSettings":    p.CanManageSettings,
			"CanViewAnalytics":     p.CanViewAnalytics,
			"CanManageMounts":      p.CanManageMounts,
		} {
			if granted {
				t.Fatalf("dj should not have %s", name)
			}
		}
	})

	// Viewer and any unrecognized role both fall through to no permissions,
	// so a typo in a role string cannot escalate.
	t.Run("viewer and unknown roles get nothing", func(t *testing.T) {
		for _, role := range []StationRole{StationRoleViewer, StationRole("superuser"), StationRole(""), StationRole("ADMIN")} {
			if DefaultPermissionsForRole(role) != (StationPermissions{}) {
				t.Fatalf("role %q granted permissions", role)
			}
		}
	})
}

func TestStationUser_GetEffectivePermissions(t *testing.T) {
	dj := &StationUser{Role: StationRoleDJ}
	if perms := dj.GetEffectivePermissions(); perms != DefaultPermissionsForRole(StationRoleDJ) {
		t.Fatalf("dj effective permissions = %+v", perms)
	}

	owner := &StationUser{Role: StationRoleOwner}
	if !owner.GetEffectivePermissions().CanManageUsers {
		t.Fatal("owner cannot manage users")
	}
}

func TestStationPermissions_ValueScan(t *testing.T) {
	orig := DefaultPermissionsForRole(StationRoleManager)

	v, err := orig.Value()
	if err != nil {
		t.Fatalf("value: %v", err)
	}
	raw, ok := v.([]byte)
	if !ok {
		t.Fatalf("Value returned %T, want []byte", v)
	}

	var back StationPermissions
	if err := back.Scan(raw); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if back != orig {
		t.Fatalf("round trip = %+v, want %+v", back, orig)
	}

	// A NULL column and an empty blob both mean "no custom permissions",
	// not an error and not a partially-populated struct.
	var fromNil StationPermissions
	if err := fromNil.Scan(nil); err != nil {
		t.Fatalf("scan nil: %v", err)
	}
	if fromNil != (StationPermissions{}) {
		t.Fatalf("nil scan = %+v, want zero", fromNil)
	}

	var fromEmpty StationPermissions
	if err := fromEmpty.Scan([]byte{}); err != nil {
		t.Fatalf("scan empty: %v", err)
	}
	if fromEmpty != (StationPermissions{}) {
		t.Fatalf("empty scan = %+v, want zero", fromEmpty)
	}

	// A non-blob value is a schema mismatch and must not be silently ignored.
	var bad StationPermissions
	if err := bad.Scan(42); err == nil {
		t.Fatal("scanning an int was accepted")
	}

	var malformed StationPermissions
	if err := malformed.Scan([]byte("{not json")); err == nil {
		t.Fatal("scanning malformed JSON was accepted")
	}
}

// ---------------------------------------------------------------------------
// platform roles
// ---------------------------------------------------------------------------

func TestUser_RoleAndPlatformChecks(t *testing.T) {
	for _, tc := range []struct {
		platform PlatformRole
		wantRole RoleName
		admin    bool
		mod      bool
	}{
		{PlatformRoleAdmin, RoleAdmin, true, true},
		{PlatformRole("admin"), RoleAdmin, true, true},
		{PlatformRoleMod, RoleManager, false, true},
		{PlatformRole("moderator"), RoleManager, false, true},
		{PlatformRole("manager"), RoleManager, false, true},
		{PlatformRoleUser, RoleDJ, false, false},
		{PlatformRole(""), RoleDJ, false, false},
		{PlatformRole("nonsense"), RoleDJ, false, false},
	} {
		u := &User{PlatformRole: tc.platform}
		if got := u.Role(); got != tc.wantRole {
			t.Fatalf("%q Role() = %q, want %q", tc.platform, got, tc.wantRole)
		}
		if got := u.IsPlatformAdmin(); got != tc.admin {
			t.Fatalf("%q IsPlatformAdmin() = %v, want %v", tc.platform, got, tc.admin)
		}
		if got := u.IsPlatformMod(); got != tc.mod {
			t.Fatalf("%q IsPlatformMod() = %v, want %v", tc.platform, got, tc.mod)
		}
	}
}

// Legacy role strings are normalized on the way into and out of storage, so a
// row written before the platform_ prefix still authorizes correctly.
func TestUser_BeforeSaveAfterFindNormalize(t *testing.T) {
	for _, legacy := range []PlatformRole{"admin", "ADMIN", " admin ", "mod", "moderator", "manager", "user", ""} {
		u := &User{PlatformRole: legacy}
		if err := u.BeforeSave(nil); err != nil {
			t.Fatalf("before save %q: %v", legacy, err)
		}
		normalized := u.PlatformRole
		if strings.EqualFold(string(legacy), "admin") && normalized != PlatformRoleAdmin {
			t.Fatalf("%q normalized to %q, want %q", legacy, normalized, PlatformRoleAdmin)
		}

		v := &User{PlatformRole: legacy}
		if err := v.AfterFind(nil); err != nil {
			t.Fatalf("after find %q: %v", legacy, err)
		}
		if v.PlatformRole != normalized {
			t.Fatalf("%q: AfterFind gave %q but BeforeSave gave %q", legacy, v.PlatformRole, normalized)
		}
	}
}

func TestStation_IsOwnedBy(t *testing.T) {
	s := &Station{OwnerID: "user-1"}
	if !s.IsOwnedBy("user-1") {
		t.Fatal("owner not recognized")
	}
	if s.IsOwnedBy("user-2") {
		t.Fatal("non-owner reported as owner")
	}
	// IsOwnedBy is a plain string comparison, so a station with no owner does
	// match the empty user ID an unauthenticated request would carry. Callers
	// must reject the empty subject before asking; this pins that contract.
	if !(&Station{}).IsOwnedBy("") {
		t.Fatal("behavior changed: empty owner no longer matches an empty user ID, so callers relying on this comparison should be rechecked")
	}
}

// ---------------------------------------------------------------------------
// mount names
// ---------------------------------------------------------------------------

func TestGenerateMountName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Radio One", "radio-one"},
		{"RADIO ONE", "radio-one"},
		{"Radio  One", "radio-one"},
		{"Radio_One!", "radioone"},
		{"Rádio Uno", "rdio-uno"},
		{"  spaced  ", "spaced"},
		{"---", "radio"},
		{"", "radio"},
		{"!!!", "radio"},
		{"station-1", "station-1"},
		{"a--b", "a-b"},
	} {
		if got := GenerateMountName(tc.in); got != tc.want {
			t.Fatalf("GenerateMountName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Whatever the input, the result is safe to put in a URL path.
func TestGenerateMountName_AlwaysURLSafe(t *testing.T) {
	for _, in := range []string{"../../etc/passwd", "a b/c?d=e#f", "日本語ラジオ", "%00null"} {
		got := GenerateMountName(in)
		if got == "" {
			t.Fatalf("GenerateMountName(%q) returned empty", in)
		}
		for _, r := range got {
			isLower := r >= 'a' && r <= 'z'
			isDigit := r >= '0' && r <= '9'
			if !isLower && !isDigit && r != '-' {
				t.Fatalf("GenerateMountName(%q) = %q contains %q", in, got, r)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// play history metadata
// ---------------------------------------------------------------------------

func TestPlayHistory_MetadataString(t *testing.T) {
	p := PlayHistory{
		Artist:   "New Order",
		Title:    "Blue Monday",
		Album:    "Power, Corruption & Lies",
		Label:    "Factory",
		Metadata: map[string]any{"artist": "Metadata Artist", "listeners": 42},
	}

	// The metadata map wins over the struct field when it holds a string.
	if got := p.MetadataString("artist"); got != "Metadata Artist" {
		t.Fatalf("artist = %q", got)
	}
	// A non-string metadata value falls through to the struct field, and
	// "listeners" has no struct fallback.
	if got := p.MetadataString("listeners"); got != "" {
		t.Fatalf("listeners = %q, want empty", got)
	}
	// Keys absent from the map use the struct fields, case-insensitively.
	if got := p.MetadataString("title"); got != "Blue Monday" {
		t.Fatalf("title = %q", got)
	}
	if got := p.MetadataString("Album"); got != "Power, Corruption & Lies" {
		t.Fatalf("album = %q", got)
	}
	if got := p.MetadataString("LABEL"); got != "Factory" {
		t.Fatalf("label = %q", got)
	}
	if got := p.MetadataString("unknown"); got != "" {
		t.Fatalf("unknown = %q, want empty", got)
	}

	// A nil map is the common case for rows written before metadata existed.
	bare := PlayHistory{Artist: "Depeche Mode"}
	if got := bare.MetadataString("artist"); got != "Depeche Mode" {
		t.Fatalf("nil metadata artist = %q", got)
	}
}

// ---------------------------------------------------------------------------
// schedule exceptions and locks
// ---------------------------------------------------------------------------

// A deleted occurrence has to stay deleted across schedule rebuilds, and the
// comparison is by local calendar day so a timezone offset cannot resurrect it.
func TestScheduleEntry_IsExceptedOn(t *testing.T) {
	oslo, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}

	e := &ScheduleEntry{RecurrenceExceptions: []string{"2026-07-16", "2026-08-01"}}

	if !e.IsExceptedOn(time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC), time.UTC) {
		t.Fatal("excepted date not matched")
	}
	if e.IsExceptedOn(time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC), time.UTC) {
		t.Fatal("non-excepted date matched")
	}

	// 2026-07-16 22:30 UTC is 2026-07-17 in Oslo (UTC+2), so it is not the
	// excepted day in that zone.
	instant := time.Date(2026, 7, 16, 22, 30, 0, 0, time.UTC)
	if !e.IsExceptedOn(instant, time.UTC) {
		t.Fatal("should be excepted when compared in UTC")
	}
	if e.IsExceptedOn(instant, oslo) {
		t.Fatal("should not be excepted when compared in Oslo local time")
	}

	// A nil location falls back to UTC rather than panicking.
	if !e.IsExceptedOn(instant, nil) {
		t.Fatal("nil location should behave as UTC")
	}

	// No exceptions at all short-circuits.
	if (&ScheduleEntry{}).IsExceptedOn(time.Now(), time.UTC) {
		t.Fatal("entry with no exceptions reported one")
	}
}

func TestScheduleLock_IsLocked(t *testing.T) {
	lock := &ScheduleLock{LockBeforeDays: 7}

	// Anything inside the 7-day window is locked, including the past.
	if !lock.IsLocked(time.Now().AddDate(0, 0, 3)) {
		t.Fatal("date inside the lock window is not locked")
	}
	if !lock.IsLocked(time.Now().AddDate(0, 0, -1)) {
		t.Fatal("past date is not locked")
	}
	// Beyond the window a DJ can still edit.
	if lock.IsLocked(time.Now().AddDate(0, 0, 30)) {
		t.Fatal("date beyond the lock window is locked")
	}

	// An explicitly locked date stays locked even when it is far out.
	far := time.Now().AddDate(0, 0, 60)
	withDate := &ScheduleLock{LockBeforeDays: 7, LockedDates: []time.Time{far}}
	if !withDate.IsLocked(far) {
		t.Fatal("explicitly locked date is not locked")
	}
	// Matching is by calendar day, so a different clock time on the same day
	// is still locked.
	sameDay := time.Date(far.Year(), far.Month(), far.Day(), 23, 59, 0, 0, far.Location())
	if !withDate.IsLocked(sameDay) {
		t.Fatal("same calendar day at a different time is not locked")
	}
	if withDate.IsLocked(far.AddDate(0, 0, 1)) {
		t.Fatal("the day after an explicitly locked date is locked")
	}
}

// ---------------------------------------------------------------------------
// priority and executor state
// ---------------------------------------------------------------------------

func TestPriorityLevel_String(t *testing.T) {
	for level, want := range map[PriorityLevel]string{
		PriorityEmergency:     "Emergency",
		PriorityLiveOverride:  "Live Override",
		PriorityLiveScheduled: "Live Scheduled",
		PriorityAutomation:    "Automation",
		PriorityFallback:      "Fallback",
		PriorityLevel(999):    "Unknown",
	} {
		if got := level.String(); got != want {
			t.Fatalf("PriorityLevel(%d) = %q, want %q", level, got, want)
		}
	}
}

// The health check is what decides whether the scheduler considers an executor
// alive, so the 10-second boundary matters.
func TestExecutorState_IsHealthy(t *testing.T) {
	if !(&ExecutorState{LastHeartbeat: time.Now()}).IsHealthy() {
		t.Fatal("a just-received heartbeat is unhealthy")
	}
	if !(&ExecutorState{LastHeartbeat: time.Now().Add(-5 * time.Second)}).IsHealthy() {
		t.Fatal("a 5s-old heartbeat is unhealthy")
	}
	if (&ExecutorState{LastHeartbeat: time.Now().Add(-11 * time.Second)}).IsHealthy() {
		t.Fatal("an 11s-old heartbeat is healthy")
	}
	if (&ExecutorState{}).IsHealthy() {
		t.Fatal("an executor that never sent a heartbeat is healthy")
	}
}

func TestExecutorState_IsPlaying(t *testing.T) {
	for _, state := range []ExecutorStateEnum{
		ExecutorStatePlaying, ExecutorStateFading, ExecutorStateLive, ExecutorStateEmergency,
	} {
		if !(&ExecutorState{State: state}).IsPlaying() {
			t.Fatalf("state %q not playing", state)
		}
	}
	for _, state := range []ExecutorStateEnum{ExecutorStateIdle, ExecutorStateEnum("stopped"), ExecutorStateEnum("")} {
		if (&ExecutorState{State: state}).IsPlaying() {
			t.Fatalf("state %q reported as playing", state)
		}
	}
}

// ---------------------------------------------------------------------------
// formatting and defaults
// ---------------------------------------------------------------------------

func TestFormatBytesAndScanResultFormatSize(t *testing.T) {
	for _, tc := range []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{1024 * 1024 * 1024 * 1024, "1.0 TB"},
	} {
		if got := formatBytes(tc.bytes); got != tc.want {
			t.Fatalf("formatBytes(%d) = %q, want %q", tc.bytes, got, tc.want)
		}
	}

	r := &ScanResult{TotalSize: 2 * 1024 * 1024}
	if got := r.FormatSize(); got != "2.0 MB" {
		t.Fatalf("FormatSize = %q, want 2.0 MB", got)
	}
}

func TestDefaultNotificationPreferences(t *testing.T) {
	prefs := DefaultNotificationPreferences("user-1")
	if len(prefs) == 0 {
		t.Fatal("no default preferences")
	}

	seen := map[NotificationType]bool{}
	for _, p := range prefs {
		if p.UserID != "user-1" {
			t.Fatalf("preference %+v has the wrong user", p)
		}
		if p.NotificationType == "" || p.Channel == "" {
			t.Fatalf("preference %+v is missing a type or channel", p)
		}
		seen[p.NotificationType] = true
	}

	// Show reminders are the one default that carries config, and the lead
	// time is what a DJ actually feels if it changes.
	var reminder *NotificationPreference
	for i := range prefs {
		if prefs[i].NotificationType == NotificationTypeShowReminder {
			reminder = &prefs[i]
			break
		}
	}
	if reminder == nil {
		t.Fatal("no show-reminder default")
	}
	if !reminder.Enabled {
		t.Fatal("show reminders default to off")
	}
	if reminder.Config["reminder_minutes"] != 30 {
		t.Fatalf("reminder lead time = %v, want 30", reminder.Config["reminder_minutes"])
	}

	// The defaults must survive the JSON round trip they take into jsonb.
	raw, err := json.Marshal(prefs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back []NotificationPreference
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back) != len(prefs) {
		t.Fatalf("round trip = %d preferences, want %d", len(back), len(prefs))
	}
}

// ---------------------------------------------------------------------------
// GORM table names
// ---------------------------------------------------------------------------

// Every TableName override pins a physical table that migrations and raw SQL
// already reference. Renaming one silently repoints reads and writes at a
// table that does not exist, so the mapping is asserted rather than derived.
func TestTableNames(t *testing.T) {
	for _, tc := range []struct {
		got  string
		want string
	}{
		{AuditLog{}.TableName(), "audit_logs"},
		{DJAvailability{}.TableName(), "dj_availability"},
		{ExecutorState{}.TableName(), "executor_states"},
		{ListenerSample{}.TableName(), "listener_samples"},
		{LiveSession{}.TableName(), "live_sessions"},
		{Network{}.TableName(), "networks"},
		{NetworkShow{}.TableName(), "network_shows"},
		{NetworkSubscription{}.TableName(), "network_subscriptions"},
		{Notification{}.TableName(), "notifications"},
		{NotificationPreference{}.TableName(), "notification_preferences"},
		{OrphanMedia{}.TableName(), "orphan_media"},
		{PlayoutQueueItem{}.TableName(), "playout_queue_items"},
		{PrioritySource{}.TableName(), "priority_sources"},
		{ScheduleAnalytics{}.TableName(), "schedule_analytics"},
		{ScheduleAnalyticsDaily{}.TableName(), "schedule_analytics_daily"},
		{ScheduleLock{}.TableName(), "schedule_locks"},
		{ScheduleRequest{}.TableName(), "schedule_requests"},
		{ScheduleRule{}.TableName(), "schedule_rules"},
		{ScheduleTemplate{}.TableName(), "schedule_templates"},
		{ScheduleVersion{}.TableName(), "schedule_versions"},
		{Show{}.TableName(), "shows"},
		{ShowInstance{}.TableName(), "show_instances"},
		{Sponsor{}.TableName(), "sponsors"},
		{StagedImport{}.TableName(), "staged_imports"},
		{SystemSettings{}.TableName(), "system_settings"},
		{UnderwritingObligation{}.TableName(), "underwriting_obligations"},
		{UnderwritingSpot{}.TableName(), "underwriting_spots"},
		{WaveformCache{}.TableName(), "waveform_cache"},
		{WebDJSession{}.TableName(), "webdj_sessions"},
		{WebhookLog{}.TableName(), "webhook_logs"},
		{WebhookTarget{}.TableName(), "webhook_targets"},
		{Webstream{}.TableName(), "webstreams"},
	} {
		if tc.got != tc.want {
			t.Errorf("table name = %q, want %q", tc.got, tc.want)
		}
	}
}
