/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package audit

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newAuditService(t *testing.T) (*Service, *gorm.DB, *events.Bus) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// One connection only: each sqlite :memory: connection is its own empty
	// database, & this test runs the service loop on a second goroutine.
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.AuditLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	bus := events.NewBus()
	return NewService(db, bus, zerolog.Nop()), db, bus
}

func TestLog_FillsDefaultsAndPersists(t *testing.T) {
	s, db, _ := newAuditService(t)

	entry := &models.AuditLog{Action: models.AuditActionPriorityEmergency}
	if err := s.Log(context.Background(), entry); err != nil {
		t.Fatalf("Log: %v", err)
	}
	if entry.ID == "" || entry.Timestamp.IsZero() || entry.Details == nil {
		t.Errorf("defaults not filled: %+v", entry)
	}

	var count int64
	db.Model(&models.AuditLog{}).Count(&count)
	if count != 1 {
		t.Fatalf("rows = %d", count)
	}
}

func TestQuery_Filters(t *testing.T) {
	s, _, _ := newAuditService(t)
	ctx := context.Background()

	userA := "user-a"
	stationX := "station-x"
	base := time.Now()

	seed := []*models.AuditLog{
		{Action: models.AuditActionPriorityEmergency, UserID: &userA, StationID: &stationX, Timestamp: base.Add(-2 * time.Hour)},
		{Action: models.AuditActionPriorityOverride, UserID: &userA, Timestamp: base.Add(-time.Hour)},
		{Action: models.AuditActionPriorityEmergency, StationID: &stationX, Timestamp: base},
	}
	for _, e := range seed {
		if err := s.Log(ctx, e); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	action := models.AuditActionPriorityEmergency
	logs, total, err := s.Query(ctx, QueryFilters{Action: &action})
	if err != nil {
		t.Fatalf("query action: %v", err)
	}
	if total != 2 || len(logs) != 2 {
		t.Errorf("action filter: total=%d len=%d", total, len(logs))
	}

	logs, total, _ = s.Query(ctx, QueryFilters{UserID: &userA})
	if total != 2 {
		t.Errorf("user filter total=%d", total)
	}

	logs, total, _ = s.Query(ctx, QueryFilters{StationID: &stationX})
	if total != 2 {
		t.Errorf("station filter total=%d", total)
	}

	since := base.Add(-90 * time.Minute)
	logs, total, _ = s.Query(ctx, QueryFilters{StartTime: &since})
	if total != 2 {
		t.Errorf("start-time filter total=%d", total)
	}
	until := base.Add(-90 * time.Minute)
	logs, total, _ = s.Query(ctx, QueryFilters{EndTime: &until})
	if total != 1 {
		t.Errorf("end-time filter total=%d", total)
	}

	// Pagination.
	logs, total, _ = s.Query(ctx, QueryFilters{Limit: 1, Offset: 1})
	if total != 3 || len(logs) != 1 {
		t.Errorf("pagination: total=%d len=%d", total, len(logs))
	}
}

func TestStart_BusEventBecomesAuditRow(t *testing.T) {
	s, db, bus := newAuditService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go s.Start(ctx)
	time.Sleep(50 * time.Millisecond) // let subscriptions attach

	bus.Publish(events.EventPriorityEmergency, events.Payload{
		"user_id":       "u-1",
		"user_email":    "op@example.com",
		"station_id":    "s-1",
		"resource_type": "priority",
		"resource_id":   "p-9",
		"ip_address":    "10.0.0.9",
		"user_agent":    "test-agent",
		"reason":        "fire drill",
	})

	var row models.AuditLog
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := db.First(&row, "action = ?", models.AuditActionPriorityEmergency).Error; err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if row.ID == "" {
		t.Fatal("bus event never became an audit row")
	}
	if row.UserID == nil || *row.UserID != "u-1" || row.UserEmail != "op@example.com" {
		t.Errorf("user fields: %+v", row)
	}
	if row.StationID == nil || *row.StationID != "s-1" {
		t.Errorf("station: %+v", row.StationID)
	}
	if row.ResourceType != "priority" || row.ResourceID != "p-9" {
		t.Errorf("resource: %+v", row)
	}
	if row.IPAddress != "10.0.0.9" || row.UserAgent != "test-agent" {
		t.Errorf("request context: %+v", row)
	}
	// Non-standard keys land in Details; extracted keys must not duplicate there.
	if row.Details["reason"] != "fire drill" {
		t.Errorf("details: %v", row.Details)
	}
	if _, dup := row.Details["user_id"]; dup {
		t.Error("extracted key duplicated into details")
	}

	// A couple more switch arms: API-key create & webstream failover.
	bus.Publish(events.EventAuditAPIKeyCreate, events.Payload{"user_id": "u-2"})
	bus.Publish(events.EventWebstreamFailover, events.Payload{"station_id": "s-1"})
	deadline = time.Now().Add(2 * time.Second)
	var total int64
	for time.Now().Before(deadline) {
		db.Model(&models.AuditLog{}).Count(&total)
		if total >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if total < 3 {
		t.Errorf("rows = %d, want 3 (apikey + failover arms)", total)
	}
}
