/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package notifications

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newNotifService(t *testing.T, cfg Config) (*Service, *gorm.DB) {
	t.Helper()
	db := dbtest.Open(t, &models.User{}, &models.Notification{}, &models.NotificationPreference{})
	return NewService(db, events.NewBus(), cfg, zerolog.Nop()), db
}

func bg() context.Context { return context.Background() }

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("GRIMNIR_SMTP_HOST", "smtp.example.com")
	t.Setenv("GRIMNIR_SMTP_PORT", "2525")
	t.Setenv("GRIMNIR_REMINDER_CHECK_INTERVAL", "5m")
	cfg := ConfigFromEnv()
	if cfg.SMTPHost != "smtp.example.com" || cfg.SMTPPort != 2525 {
		t.Fatalf("smtp config = %+v", cfg)
	}
	if cfg.ReminderCheckInterval != 5*time.Minute {
		t.Fatalf("interval = %v, want 5m", cfg.ReminderCheckInterval)
	}

	// Invalid interval falls back to the 1m default (guards NewTicker(0) panic).
	t.Setenv("GRIMNIR_REMINDER_CHECK_INTERVAL", "not-a-duration")
	if got := ConfigFromEnv().ReminderCheckInterval; got != time.Minute {
		t.Fatalf("fallback interval = %v, want 1m", got)
	}
	// Defaults when unset.
	if ConfigFromEnv().SMTPFrom != "noreply@example.com" {
		t.Fatal("expected default SMTP from")
	}
}

func TestSend_InApp(t *testing.T) {
	svc, db := newNotifService(t, Config{})
	ensureUser(t, db, dbtest.UUID("u1"))
	n := &models.Notification{
		UserID:           dbtest.UUID("u1"),
		ReferenceID:      dbtest.UUID("ref"),
		NotificationType: models.NotificationTypeStorageWarning,
		Channel:          models.NotificationChannelInApp,
		Subject:          "hi",
		Body:             "body",
	}
	if err := svc.Send(bg(), n, nil); err != nil {
		t.Fatalf("send in-app: %v", err)
	}
	var stored models.Notification
	db.First(&stored, "id = ?", n.ID)
	if stored.Status != models.NotificationStatusSent || stored.SentAt == nil {
		t.Fatalf("in-app should be marked sent: %+v", stored)
	}
}

func TestSend_EmailNoSMTP_MarksFailed(t *testing.T) {
	svc, db := newNotifService(t, Config{}) // no SMTPHost
	ensureUser(t, db, dbtest.UUID("u1"))
	n := &models.Notification{
		UserID:           dbtest.UUID("u1"),
		ReferenceID:      dbtest.UUID("ref"),
		NotificationType: models.NotificationTypeStorageWarning,
		Channel:          models.NotificationChannelEmail,
		Subject:          "hi",
	}
	err := svc.Send(bg(), n, &models.User{ID: dbtest.UUID("u1"), Email: "u@x.com"})
	if err == nil {
		t.Fatal("expected error when SMTP is unconfigured")
	}
	var stored models.Notification
	db.First(&stored, "id = ?", n.ID)
	if stored.Status != models.NotificationStatusFailed || stored.Error == "" {
		t.Fatalf("failed email should record status+error: %+v", stored)
	}
}

func TestSend_UnknownChannel(t *testing.T) {
	svc, db := newNotifService(t, Config{})
	ensureUser(t, db, dbtest.UUID("u1"))
	n := &models.Notification{UserID: dbtest.UUID("u1"), ReferenceID: dbtest.UUID("ref"), NotificationType: models.NotificationTypeStorageWarning, Channel: models.NotificationChannel("carrier-pigeon")}
	if err := svc.Send(bg(), n, nil); err == nil {
		t.Fatal("expected error for unknown channel")
	}
}

// ensureUser makes the FK target exist: notifications/preferences reference
// users, and Postgres enforces it (sqlite did not). Idempotent per id.
func ensureUser(t *testing.T, db *gorm.DB, id string) string {
	t.Helper()
	u := models.User{ID: id, Email: id + "@t.local"}
	if err := db.Where("id = ?", id).FirstOrCreate(&u).Error; err != nil {
		t.Fatalf("ensure user %s: %v", id, err)
	}
	return id
}

func seedNotif(t *testing.T, db *gorm.DB, userID string, channel models.NotificationChannel, status models.NotificationStatus, created time.Time) {
	t.Helper()
	ensureUser(t, db, userID)
	n := &models.Notification{
		ID:     uuid.NewString(),
		UserID: userID, ReferenceID: dbtest.UUID("ref"), NotificationType: models.NotificationTypeStorageWarning,
		Channel: channel, Status: status, CreatedAt: created,
	}
	if err := db.Create(n).Error; err != nil {
		t.Fatalf("seed notif: %v", err)
	}
}

func TestGetUserNotificationsAndCounts(t *testing.T) {
	svc, db := newNotifService(t, Config{})
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seedNotif(t, db, dbtest.UUID("u1"), models.NotificationChannelInApp, models.NotificationStatusSent, base)
	seedNotif(t, db, dbtest.UUID("u1"), models.NotificationChannelInApp, models.NotificationStatusSent, base.Add(time.Minute))
	seedNotif(t, db, dbtest.UUID("u1"), models.NotificationChannelInApp, models.NotificationStatusRead, base.Add(2*time.Minute))
	seedNotif(t, db, dbtest.UUID("u2"), models.NotificationChannelInApp, models.NotificationStatusSent, base)

	all, total, err := svc.GetUserNotifications(bg(), dbtest.UUID("u1"), false, 0, 0)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if total != 3 || len(all) != 3 {
		t.Fatalf("all for u1: total=%d len=%d, want 3", total, len(all))
	}
	// Newest first.
	if all[0].CreatedAt.Before(all[1].CreatedAt) {
		t.Fatal("expected descending order")
	}

	unread, total, _ := svc.GetUserNotifications(bg(), dbtest.UUID("u1"), true, 0, 0)
	if total != 2 || len(unread) != 2 {
		t.Fatalf("unread for u1 = %d, want 2", total)
	}

	if c, _ := svc.GetUnreadCount(bg(), dbtest.UUID("u1")); c != 2 {
		t.Fatalf("unread count = %d, want 2", c)
	}
}

func TestMarkAsReadAndAll(t *testing.T) {
	svc, db := newNotifService(t, Config{})
	ensureUser(t, db, dbtest.UUID("u1"))
	n := &models.Notification{ID: dbtest.UUID("n1"), UserID: dbtest.UUID("u1"), ReferenceID: dbtest.UUID("ref"), NotificationType: models.NotificationTypeStorageWarning, Channel: models.NotificationChannelInApp, Status: models.NotificationStatusSent}
	db.Create(n)

	if err := svc.MarkAsRead(bg(), dbtest.UUID("n1"), dbtest.UUID("u1")); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	var reloaded models.Notification
	db.First(&reloaded, "id = ?", dbtest.UUID("n1"))
	if reloaded.Status != models.NotificationStatusRead {
		t.Fatalf("status = %s, want read", reloaded.Status)
	}

	// Not-found (wrong user) is an error, not a silent success.
	if err := svc.MarkAsRead(bg(), dbtest.UUID("n1"), dbtest.UUID("other")); err == nil {
		t.Fatal("expected not-found error for wrong user")
	}

	// MarkAllAsRead clears the rest.
	seedNotif(t, db, dbtest.UUID("u1"), models.NotificationChannelInApp, models.NotificationStatusSent, time.Now())
	if err := svc.MarkAllAsRead(bg(), dbtest.UUID("u1")); err != nil {
		t.Fatalf("mark all: %v", err)
	}
	if c, _ := svc.GetUnreadCount(bg(), dbtest.UUID("u1")); c != 0 {
		t.Fatalf("unread after mark-all = %d, want 0", c)
	}
}

func seedInAppPref(t *testing.T, db *gorm.DB, userID string, nt models.NotificationType) {
	t.Helper()
	ensureUser(t, db, userID)
	p := &models.NotificationPreference{ID: uuid.NewString(), UserID: userID, NotificationType: nt, Channel: models.NotificationChannelInApp, Enabled: true}
	if err := db.Create(p).Error; err != nil {
		t.Fatalf("seed pref: %v", err)
	}
}

func countFor(t *testing.T, db *gorm.DB, userID string, nt models.NotificationType) int64 {
	t.Helper()
	var n int64
	db.Model(&models.Notification{}).Where("user_id = ? AND notification_type = ?", userID, nt).Count(&n)
	return n
}

func TestNotifyRequestStatus(t *testing.T) {
	svc, db := newNotifService(t, Config{})
	seedInAppPref(t, db, dbtest.UUID("u1"), models.NotificationTypeRequestStatus)
	req := &models.ScheduleRequest{ID: dbtest.UUID("r1"), RequesterID: dbtest.UUID("u1"), Requester: &models.User{ID: dbtest.UUID("u1"), Email: "dj@x.com"}, ReviewNote: "not this week"}

	for _, status := range []string{"approved", "rejected", "deferred"} {
		if err := svc.NotifyRequestStatus(bg(), req, status); err != nil {
			t.Fatalf("notify %s: %v", status, err)
		}
	}
	if got := countFor(t, db, dbtest.UUID("u1"), models.NotificationTypeRequestStatus); got != 3 {
		t.Fatalf("request-status notifications = %d, want 3", got)
	}

	// No requester is an error.
	if err := svc.NotifyRequestStatus(bg(), &models.ScheduleRequest{ID: dbtest.UUID("r2"), RequesterID: dbtest.UUID("u1")}, "approved"); err == nil {
		t.Fatal("expected error when request has no requester")
	}
}

func TestNotifyNewAssignmentAndCancel(t *testing.T) {
	svc, db := newNotifService(t, Config{})
	host := dbtest.UUID("u1")
	seedInAppPref(t, db, host, models.NotificationTypeNewAssignment)
	seedInAppPref(t, db, host, models.NotificationTypeShowCancelled)

	inst := &models.ShowInstance{
		ID: dbtest.UUID("i1"), HostUserID: &host, Host: &models.User{ID: host, Email: "dj@x.com"},
		Show: &models.Show{Name: "Morning Drive"}, StartsAt: time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC),
	}
	if err := svc.NotifyNewAssignment(bg(), inst); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if got := countFor(t, db, host, models.NotificationTypeNewAssignment); got != 1 {
		t.Fatalf("assignment notifications = %d, want 1", got)
	}

	if err := svc.NotifyShowCancelled(bg(), inst, "snow day"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if got := countFor(t, db, host, models.NotificationTypeShowCancelled); got != 1 {
		t.Fatalf("cancel notifications = %d, want 1", got)
	}

	// Hostless instance is a no-op, not an error.
	if err := svc.NotifyNewAssignment(bg(), &models.ShowInstance{ID: dbtest.UUID("i2")}); err != nil {
		t.Fatalf("hostless assign should be a no-op: %v", err)
	}
	if err := svc.NotifyShowCancelled(bg(), &models.ShowInstance{ID: dbtest.UUID("i3")}, ""); err != nil {
		t.Fatalf("hostless cancel should be a no-op: %v", err)
	}
}

func TestPreferences(t *testing.T) {
	svc, db := newNotifService(t, Config{})
	ensureUser(t, db, dbtest.UUID("u1"))
	if err := svc.CreateDefaultPreferences(bg(), dbtest.UUID("u1")); err != nil {
		t.Fatalf("create defaults: %v", err)
	}
	prefs, err := svc.GetUserPreferences(bg(), dbtest.UUID("u1"))
	if err != nil {
		t.Fatalf("get prefs: %v", err)
	}
	if len(prefs) == 0 {
		t.Fatal("expected default preferences to be created")
	}

	// Toggle one off. (Pass nil config: gorm's map-based Updates can't run the
	// jsonb serializer on sqlite; the config-carrying path is a postgres concern.)
	target := prefs[0]
	if err := svc.UpdatePreference(bg(), target.ID, dbtest.UUID("u1"), false, nil); err != nil {
		t.Fatalf("update pref: %v", err)
	}
	after, _ := svc.GetUserPreferences(bg(), dbtest.UUID("u1"))
	for _, p := range after {
		if p.ID == target.ID && p.Enabled {
			t.Fatal("preference should be disabled after update")
		}
	}

	// Not-found preference errors.
	if err := svc.UpdatePreference(bg(), "missing", dbtest.UUID("u1"), true, nil); err == nil {
		t.Fatal("expected not-found error for missing preference")
	}
}
