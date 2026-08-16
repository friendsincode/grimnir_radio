/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package notifications

import (
	"context"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within deadline")
}

func (s *Service) isRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func TestStart_DispatchesEventsAndStopsOnCancel(t *testing.T) {
	// Long reminder interval so the ticker doesn't fire during the test.
	svc, db := newNotifService(t, Config{ReminderCheckInterval: time.Hour})
	// Start runs concurrently with the test goroutine. A ":memory:" sqlite DB is
	// per-connection, so pin the pool to one connection or the second goroutine
	// gets a fresh, table-less database.
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	migrateExtras(t, db)

	db.Create(&models.User{ID: "mgr", Email: "mgr@example.com"})
	db.Create(&models.StationUser{UserID: "mgr", StationID: "st1", Role: models.StationRoleOwner})
	db.Create(&models.NotificationPreference{
		UserID: "mgr", NotificationType: models.NotificationTypeScheduleChange,
		Channel: models.NotificationChannelInApp, Enabled: true,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { svc.Start(ctx); close(done) }()

	// Start flips running=true before subscribing; wait for it, then let the
	// subscribe settle before publishing onto the (buffered) bus.
	waitFor(t, svc.isRunning)
	time.Sleep(20 * time.Millisecond)
	svc.bus.Publish(events.EventDJConnect, events.Payload{"station_id": "st1", "dj_name": "DJ X"})

	waitFor(t, func() bool { return countNotifications(t, db, "mgr") == 1 })

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after cancel")
	}
	if svc.isRunning() {
		t.Fatal("service still marked running after Start returned")
	}
}
