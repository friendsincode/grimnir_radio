/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package storage

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func monitorTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Notification{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seedAdmin(t *testing.T, db *gorm.DB, id string) {
	t.Helper()
	if err := db.Create(&models.User{ID: id, Email: id + "@grimnir.fm", Password: "x", PlatformRole: models.PlatformRoleAdmin}).Error; err != nil {
		t.Fatalf("seed admin: %v", err)
	}
}

// fakeUsage returns a usageFn reporting a fixed used-percent.
func fakeUsage(pct float64) func(string) (uint64, uint64, float64, error) {
	return func(string) (uint64, uint64, float64, error) {
		const total = uint64(1000)
		used := uint64(pct * 10)
		return total, total - used, pct, nil
	}
}

func countNotifications(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	db.Model(&models.Notification{}).Count(&n)
	return n
}

func TestNewMonitor_DefaultInterval(t *testing.T) {
	m := NewMonitor(monitorTestDB(t), MonitorConfig{}, zerolog.Nop())
	if m.cfg.CheckInterval != 30*time.Minute {
		t.Fatalf("default interval = %v, want 30m", m.cfg.CheckInterval)
	}
	if m.usageFn == nil {
		t.Fatal("usageFn should default to diskUsage")
	}
}

func TestCheck_NotifiesOncePerSeverity(t *testing.T) {
	db := monitorTestDB(t)
	seedAdmin(t, db, "a1")
	seedAdmin(t, db, "a2")
	m := NewMonitor(db, MonitorConfig{MediaRoot: "/media"}, zerolog.Nop())
	ctx := context.Background()

	// Cross the warning threshold (>=80): one notification per admin.
	m.usageFn = fakeUsage(85)
	m.check(ctx)
	if got := countNotifications(t, db); got != 2 {
		t.Fatalf("after warning: %d notifications, want 2", got)
	}
	if m.lastNotifiedLevel != severityWarning {
		t.Fatalf("level = %q, want warning", m.lastNotifiedLevel)
	}

	// Still in the warning band: no additional notifications (notify-once).
	m.usageFn = fakeUsage(83)
	m.check(ctx)
	if got := countNotifications(t, db); got != 2 {
		t.Fatalf("re-check same band: %d notifications, want still 2", got)
	}
}

func TestCheck_EscalatesAndResets(t *testing.T) {
	db := monitorTestDB(t)
	seedAdmin(t, db, "a1")
	m := NewMonitor(db, MonitorConfig{MediaRoot: "/media"}, zerolog.Nop())
	ctx := context.Background()

	m.usageFn = fakeUsage(85) // warning
	m.check(ctx)
	m.usageFn = fakeUsage(92) // critical -> escalates
	m.check(ctx)
	m.usageFn = fakeUsage(96) // emergency -> escalates
	m.check(ctx)
	if got := countNotifications(t, db); got != 3 {
		t.Fatalf("escalation should notify 3 times, got %d", got)
	}
	if m.lastNotifiedLevel != severityEmergency {
		t.Fatalf("level = %q, want emergency", m.lastNotifiedLevel)
	}

	// Drop below all thresholds: state resets so a later rise re-notifies.
	m.usageFn = fakeUsage(50)
	m.check(ctx)
	if m.lastNotifiedLevel != "" {
		t.Fatalf("level after reset = %q, want empty", m.lastNotifiedLevel)
	}
	m.usageFn = fakeUsage(85)
	m.check(ctx)
	if got := countNotifications(t, db); got != 4 {
		t.Fatalf("re-notify after reset: got %d, want 4", got)
	}
}

func TestCheck_UsageError_NoNotify(t *testing.T) {
	db := monitorTestDB(t)
	seedAdmin(t, db, "a1")
	m := NewMonitor(db, MonitorConfig{MediaRoot: "/media"}, zerolog.Nop())
	m.usageFn = func(string) (uint64, uint64, float64, error) {
		return 0, 0, 0, context.DeadlineExceeded
	}
	m.check(context.Background())
	if got := countNotifications(t, db); got != 0 {
		t.Fatalf("usage error should not notify, got %d", got)
	}
}

func TestNotifyAdmins_NoAdmins(t *testing.T) {
	db := monitorTestDB(t)
	m := NewMonitor(db, MonitorConfig{}, zerolog.Nop())
	// No admins seeded -> returns nil, creates nothing.
	if err := m.notifyAdmins(context.Background(), &defaultThresholds[0], 96, 1000, 40); err != nil {
		t.Fatalf("notifyAdmins: %v", err)
	}
	if got := countNotifications(t, db); got != 0 {
		t.Fatalf("no admins should create no notifications, got %d", got)
	}
}

func TestDiskUsage_RealTempDir(t *testing.T) {
	total, free, pct, err := diskUsage(t.TempDir())
	if err != nil {
		t.Fatalf("diskUsage: %v", err)
	}
	if total == 0 {
		t.Fatal("total should be non-zero on a real filesystem")
	}
	if free > total {
		t.Fatal("free should not exceed total")
	}
	if pct < 0 || pct > 100 {
		t.Fatalf("used pct = %f out of range", pct)
	}
	if _, _, _, err := diskUsage("/nonexistent/path/xyz"); err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}
