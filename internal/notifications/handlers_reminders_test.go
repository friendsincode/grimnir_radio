/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package notifications

import (
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// migrateExtras adds the tables the event handlers and reminder scan touch,
// beyond the three the base helper migrates.
func migrateExtras(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.AutoMigrate(&models.Station{}, &models.StationUser{}, &models.Show{}, &models.ShowInstance{}); err != nil {
		t.Fatalf("migrate extras: %v", err)
	}
}

func countNotifications(t *testing.T, db *gorm.DB, userID string) int64 {
	t.Helper()
	var n int64
	db.Model(&models.Notification{}).Where("user_id = ?", userID).Count(&n)
	return n
}

func TestHandleScheduleChange_NotifiesStationUsers(t *testing.T) {
	svc, db := newNotifService(t, Config{})
	migrateExtras(t, db)
	db.Create(&models.Station{ID: dbtest.UUID("st1"), OwnerID: dbtest.UUID("owner"), Name: "st1"})

	db.Create(&models.User{ID: dbtest.UUID("u1"), Email: "dj@example.com"})
	db.Create(&models.StationUser{ID: dbtest.UUID("su"), UserID: dbtest.UUID("u1"), StationID: dbtest.UUID("st1"), Role: models.StationRoleManager})
	db.Create(&models.NotificationPreference{
		ID:     dbtest.UUID("pref"),
		UserID: dbtest.UUID("u1"), NotificationType: models.NotificationTypeScheduleChange,
		Channel: models.NotificationChannelInApp, Enabled: true,
	})

	svc.handleScheduleChange(bg(), events.Payload{"station_id": dbtest.UUID("st1")})

	if got := countNotifications(t, db, dbtest.UUID("u1")); got != 1 {
		t.Fatalf("expected 1 schedule-change notification, got %d", got)
	}
}

func TestHandleScheduleChange_NoStation_NoOp(t *testing.T) {
	svc, db := newNotifService(t, Config{})
	migrateExtras(t, db)
	db.Create(&models.Station{ID: dbtest.UUID("st1"), OwnerID: dbtest.UUID("owner"), Name: "st1"})
	svc.handleScheduleChange(bg(), events.Payload{}) // no station_id
	var total int64
	db.Model(&models.Notification{}).Count(&total)
	if total != 0 {
		t.Fatalf("expected no notifications for empty station, got %d", total)
	}
}

func TestHandleDJConnect_NotifiesManagers(t *testing.T) {
	svc, db := newNotifService(t, Config{})
	migrateExtras(t, db)
	db.Create(&models.Station{ID: dbtest.UUID("st1"), OwnerID: dbtest.UUID("owner"), Name: "st1"})

	db.Create(&models.User{ID: dbtest.UUID("mgr"), Email: "mgr@example.com"})
	db.Create(&models.StationUser{ID: dbtest.UUID("su"), UserID: dbtest.UUID("mgr"), StationID: dbtest.UUID("st1"), Role: models.StationRoleOwner})
	db.Create(&models.NotificationPreference{
		ID:     dbtest.UUID("pref"),
		UserID: dbtest.UUID("mgr"), NotificationType: models.NotificationTypeScheduleChange,
		Channel: models.NotificationChannelInApp, Enabled: true,
	})

	svc.handleDJConnect(bg(), events.Payload{"station_id": dbtest.UUID("st1"), "dj_name": "DJ Shadow"})

	var notif models.Notification
	if err := db.Where("user_id = ?", dbtest.UUID("mgr")).First(&notif).Error; err != nil {
		t.Fatalf("expected a DJ-connect notification: %v", err)
	}
	if !strings.Contains(notif.Body, "DJ Shadow") || notif.Subject != "DJ Connected" {
		t.Fatalf("unexpected notification: subject=%q body=%q", notif.Subject, notif.Body)
	}
	if notif.Metadata["dj_name"] != "DJ Shadow" {
		t.Fatalf("metadata dj_name = %v, want DJ Shadow", notif.Metadata["dj_name"])
	}
}

func TestHandleDJDisconnect_NotifiesManagers(t *testing.T) {
	svc, db := newNotifService(t, Config{})
	migrateExtras(t, db)
	db.Create(&models.Station{ID: dbtest.UUID("st1"), OwnerID: dbtest.UUID("owner"), Name: "st1"})

	db.Create(&models.User{ID: dbtest.UUID("mgr"), Email: "mgr@example.com"})
	db.Create(&models.StationUser{ID: dbtest.UUID("su"), UserID: dbtest.UUID("mgr"), StationID: dbtest.UUID("st1"), Role: models.StationRoleAdmin})
	db.Create(&models.NotificationPreference{
		ID:     dbtest.UUID("pref"),
		UserID: dbtest.UUID("mgr"), NotificationType: models.NotificationTypeScheduleChange,
		Channel: models.NotificationChannelInApp, Enabled: true,
	})

	svc.handleDJDisconnect(bg(), events.Payload{"station_id": dbtest.UUID("st1"), "dj_name": "DJ Shadow"})

	var notif models.Notification
	if err := db.Where("user_id = ?", dbtest.UUID("mgr")).First(&notif).Error; err != nil {
		t.Fatalf("expected a DJ-disconnect notification: %v", err)
	}
	if notif.Subject != "DJ Disconnected" || !strings.Contains(notif.Body, "DJ Shadow") {
		t.Fatalf("unexpected notification: subject=%q body=%q", notif.Subject, notif.Body)
	}
}

func TestProcessReminders_SendsOnceWithinWindow(t *testing.T) {
	svc, db := newNotifService(t, Config{ReminderCheckInterval: 5 * time.Minute})
	migrateExtras(t, db)
	db.Create(&models.Station{ID: dbtest.UUID("st1"), OwnerID: dbtest.UUID("owner"), Name: "st1"})

	db.Create(&models.User{ID: dbtest.UUID("host"), Email: "host@example.com"})
	db.Create(&models.Show{ID: dbtest.UUID("sh1"), StationID: dbtest.UUID("st1"), Name: "Morning Drive"})
	hostID := dbtest.UUID("host")
	db.Create(&models.ShowInstance{
		ID: dbtest.UUID("si1"), ShowID: dbtest.UUID("sh1"), StationID: dbtest.UUID("st1"),
		StartsAt: time.Now().Add(30 * time.Minute), EndsAt: time.Now().Add(90 * time.Minute),
		HostUserID: &hostID, Status: models.ShowInstanceScheduled,
	})
	db.Create(&models.NotificationPreference{
		ID:     dbtest.UUID("pref"),
		UserID: dbtest.UUID("host"), NotificationType: models.NotificationTypeShowReminder,
		Channel: models.NotificationChannelInApp, Enabled: true,
		Config: map[string]any{"reminder_minutes": float64(30)},
	})

	svc.processReminders(bg())
	if got := countNotifications(t, db, dbtest.UUID("host")); got != 1 {
		t.Fatalf("expected 1 reminder, got %d", got)
	}

	// Idempotent: a second scan in the same window must not double-send.
	svc.processReminders(bg())
	if got := countNotifications(t, db, dbtest.UUID("host")); got != 1 {
		t.Fatalf("reminder sent twice, got %d", got)
	}
}

// seedReminderFixture wires a host, show, upcoming instance and reminder pref so
// each window-boundary case only varies the two knobs that matter.
func seedReminderFixture(t *testing.T, db *gorm.DB, startIn time.Duration, reminderMinutes int, enabled bool) {
	t.Helper()
	db.Create(&models.Station{ID: dbtest.UUID("st1"), OwnerID: dbtest.UUID("owner"), Name: "st1"})
	db.Create(&models.User{ID: dbtest.UUID("host"), Email: "host@example.com"})
	db.Create(&models.Show{ID: dbtest.UUID("sh1"), StationID: dbtest.UUID("st1"), Name: "Morning Drive"})
	hostID := dbtest.UUID("host")
	db.Create(&models.ShowInstance{
		ID: dbtest.UUID("si1"), ShowID: dbtest.UUID("sh1"), StationID: dbtest.UUID("st1"),
		StartsAt: time.Now().Add(startIn), EndsAt: time.Now().Add(startIn + time.Hour),
		HostUserID: &hostID, Status: models.ShowInstanceScheduled,
	})
	db.Create(&models.NotificationPreference{
		ID:     dbtest.UUID("pref"),
		UserID: dbtest.UUID("host"), NotificationType: models.NotificationTypeShowReminder,
		Channel: models.NotificationChannelInApp, Enabled: true,
		Config: map[string]any{"reminder_minutes": float64(reminderMinutes)},
	})
	// Enabled carries a gorm `default:true`, so a false zero-value is dropped on
	// Create and the DB default wins. Force the disabled state with an explicit
	// column write.
	if !enabled {
		db.Model(&models.NotificationPreference{}).
			Where("id = ?", dbtest.UUID("pref")).Update("enabled", false)
	}
}

// TestProcessReminders_TooEarly_NoSend guards #86: the reminder fires only once
// now has reached start-reminderMinutes. With a 5-minute lead on a show 30
// minutes out, the reminder time is still 25 minutes away, so nothing sends.
func TestProcessReminders_TooEarly_NoSend(t *testing.T) {
	svc, db := newNotifService(t, Config{ReminderCheckInterval: 5 * time.Minute})
	migrateExtras(t, db)
	seedReminderFixture(t, db, 30*time.Minute, 5, true)

	svc.processReminders(bg())
	if got := countNotifications(t, db, dbtest.UUID("host")); got != 0 {
		t.Fatalf("reminder sent before its window opened, got %d", got)
	}
}

// TestProcessReminders_TooLate_NoSend guards #86: once now is past
// reminderTime + ReminderCheckInterval the window has closed. A 90-minute lead
// on a show 30 minutes out puts the reminder time an hour in the past, so the
// scan must skip it rather than fire a stale reminder.
func TestProcessReminders_TooLate_NoSend(t *testing.T) {
	svc, db := newNotifService(t, Config{ReminderCheckInterval: 5 * time.Minute})
	migrateExtras(t, db)
	seedReminderFixture(t, db, 30*time.Minute, 90, true)

	svc.processReminders(bg())
	if got := countNotifications(t, db, dbtest.UUID("host")); got != 0 {
		t.Fatalf("stale reminder fired after its window closed, got %d", got)
	}
}

// TestProcessReminders_DisabledPref_NoSend guards #86: a disabled reminder
// preference must gate the scan out entirely, even squarely inside the window.
func TestProcessReminders_DisabledPref_NoSend(t *testing.T) {
	svc, db := newNotifService(t, Config{ReminderCheckInterval: 5 * time.Minute})
	migrateExtras(t, db)
	seedReminderFixture(t, db, 30*time.Minute, 30, false)

	svc.processReminders(bg())
	if got := countNotifications(t, db, dbtest.UUID("host")); got != 0 {
		t.Fatalf("reminder sent despite disabled preference, got %d", got)
	}
}
