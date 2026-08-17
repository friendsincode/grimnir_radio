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

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// migrateExtras adds the tables the event handlers and reminder scan touch,
// beyond the three the base helper migrates.
func migrateExtras(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.AutoMigrate(&models.StationUser{}, &models.Show{}, &models.ShowInstance{}); err != nil {
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

	db.Create(&models.User{ID: "u1", Email: "dj@example.com"})
	db.Create(&models.StationUser{UserID: "u1", StationID: "st1", Role: models.StationRoleManager})
	db.Create(&models.NotificationPreference{
		UserID: "u1", NotificationType: models.NotificationTypeScheduleChange,
		Channel: models.NotificationChannelInApp, Enabled: true,
	})

	svc.handleScheduleChange(bg(), events.Payload{"station_id": "st1"})

	if got := countNotifications(t, db, "u1"); got != 1 {
		t.Fatalf("expected 1 schedule-change notification, got %d", got)
	}
}

func TestHandleScheduleChange_NoStation_NoOp(t *testing.T) {
	svc, db := newNotifService(t, Config{})
	migrateExtras(t, db)
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

	db.Create(&models.User{ID: "mgr", Email: "mgr@example.com"})
	db.Create(&models.StationUser{UserID: "mgr", StationID: "st1", Role: models.StationRoleOwner})
	db.Create(&models.NotificationPreference{
		UserID: "mgr", NotificationType: models.NotificationTypeScheduleChange,
		Channel: models.NotificationChannelInApp, Enabled: true,
	})

	svc.handleDJConnect(bg(), events.Payload{"station_id": "st1", "dj_name": "DJ Shadow"})

	var notif models.Notification
	if err := db.Where("user_id = ?", "mgr").First(&notif).Error; err != nil {
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

	db.Create(&models.User{ID: "mgr", Email: "mgr@example.com"})
	db.Create(&models.StationUser{UserID: "mgr", StationID: "st1", Role: models.StationRoleAdmin})
	db.Create(&models.NotificationPreference{
		UserID: "mgr", NotificationType: models.NotificationTypeScheduleChange,
		Channel: models.NotificationChannelInApp, Enabled: true,
	})

	svc.handleDJDisconnect(bg(), events.Payload{"station_id": "st1", "dj_name": "DJ Shadow"})

	var notif models.Notification
	if err := db.Where("user_id = ?", "mgr").First(&notif).Error; err != nil {
		t.Fatalf("expected a DJ-disconnect notification: %v", err)
	}
	if notif.Subject != "DJ Disconnected" || !strings.Contains(notif.Body, "DJ Shadow") {
		t.Fatalf("unexpected notification: subject=%q body=%q", notif.Subject, notif.Body)
	}
}

func TestProcessReminders_SendsOnceWithinWindow(t *testing.T) {
	svc, db := newNotifService(t, Config{ReminderCheckInterval: 5 * time.Minute})
	migrateExtras(t, db)

	db.Create(&models.User{ID: "host", Email: "host@example.com"})
	db.Create(&models.Show{ID: "sh1", Name: "Morning Drive"})
	hostID := "host"
	db.Create(&models.ShowInstance{
		ID: "si1", ShowID: "sh1", StationID: "st1",
		StartsAt: time.Now().Add(30 * time.Minute), EndsAt: time.Now().Add(90 * time.Minute),
		HostUserID: &hostID, Status: models.ShowInstanceScheduled,
	})
	db.Create(&models.NotificationPreference{
		UserID: "host", NotificationType: models.NotificationTypeShowReminder,
		Channel: models.NotificationChannelInApp, Enabled: true,
		Config: map[string]any{"reminder_minutes": float64(30)},
	})

	svc.processReminders(bg())
	if got := countNotifications(t, db, "host"); got != 1 {
		t.Fatalf("expected 1 reminder, got %d", got)
	}

	// Idempotent: a second scan in the same window must not double-send.
	svc.processReminders(bg())
	if got := countNotifications(t, db, "host"); got != 1 {
		t.Fatalf("reminder sent twice, got %d", got)
	}
}
