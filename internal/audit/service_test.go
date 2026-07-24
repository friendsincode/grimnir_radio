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
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.AuditLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	bus := events.NewBus()
	return NewService(db, bus, zerolog.Nop()), db, bus
}

func TestLog_FillsDefaults(t *testing.T) {
	svc, db, _ := newAuditService(t)
	entry := &models.AuditLog{Action: models.AuditActionStationCreate}
	if err := svc.Log(context.Background(), entry); err != nil {
		t.Fatalf("log: %v", err)
	}
	if entry.ID == "" || entry.Timestamp.IsZero() || entry.CreatedAt.IsZero() || entry.Details == nil {
		t.Fatalf("Log did not fill defaults: %+v", entry)
	}
	var n int64
	db.Model(&models.AuditLog{}).Count(&n)
	if n != 1 {
		t.Fatalf("expected 1 row, got %d", n)
	}
}

func TestLogAuditEntry_ExtractsPayload(t *testing.T) {
	svc, db, _ := newAuditService(t)
	svc.logAuditEntry(context.Background(), models.AuditActionAPIKeyCreate, events.Payload{
		"user_id":       "u1",
		"user_email":    "dj@grimnir.fm",
		"station_id":    "st1",
		"resource_type": "apikey",
		"resource_id":   "key1",
		"ip_address":    "203.0.113.1",
		"user_agent":    "curl/8",
		"extra_field":   "kept-in-details",
	})

	var log models.AuditLog
	if err := db.First(&log, "action = ?", models.AuditActionAPIKeyCreate).Error; err != nil {
		t.Fatalf("no audit row: %v", err)
	}
	if log.UserID == nil || *log.UserID != "u1" {
		t.Fatalf("user_id not extracted: %+v", log.UserID)
	}
	if log.StationID == nil || *log.StationID != "st1" {
		t.Fatalf("station_id not extracted: %+v", log.StationID)
	}
	if log.UserEmail != "dj@grimnir.fm" || log.ResourceType != "apikey" || log.ResourceID != "key1" {
		t.Fatalf("scalar fields wrong: %+v", log)
	}
	if log.IPAddress != "203.0.113.1" || log.UserAgent != "curl/8" {
		t.Fatalf("request context wrong: %+v", log)
	}
	// Known keys are stripped from Details; unknown keys are kept.
	if _, ok := log.Details["user_id"]; ok {
		t.Fatal("user_id should not leak into Details")
	}
	if log.Details["extra_field"] != "kept-in-details" {
		t.Fatalf("extra field missing from Details: %+v", log.Details)
	}
}

func TestQuery_FiltersAndPagination(t *testing.T) {
	svc, _, _ := newAuditService(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	u1, st1 := "u1", "st1"

	// 5 entries for u1/st1 with staggered timestamps, 1 for a different user.
	for i := 0; i < 5; i++ {
		svc.Log(ctx, &models.AuditLog{UserID: &u1, StationID: &st1, Action: models.AuditActionPlayoutSkip, Timestamp: base.Add(time.Duration(i) * time.Minute)})
	}
	other := "u2"
	svc.Log(ctx, &models.AuditLog{UserID: &other, Action: models.AuditActionStationCreate, Timestamp: base})

	// Filter by user.
	logs, total, err := svc.Query(ctx, QueryFilters{UserID: &u1})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if total != 5 || len(logs) != 5 {
		t.Fatalf("user filter: total=%d len=%d, want 5/5", total, len(logs))
	}
	// Most-recent-first ordering.
	if !logs[0].Timestamp.After(logs[1].Timestamp) {
		t.Fatal("expected descending timestamp order")
	}

	// Filter by action.
	action := models.AuditActionStationCreate
	_, total, _ = svc.Query(ctx, QueryFilters{Action: &action})
	if total != 1 {
		t.Fatalf("action filter total = %d, want 1", total)
	}

	// Time window filter.
	start := base.Add(2 * time.Minute)
	_, total, _ = svc.Query(ctx, QueryFilters{UserID: &u1, StartTime: &start})
	if total != 3 {
		t.Fatalf("start-time filter total = %d, want 3", total)
	}

	// Pagination: limit + offset.
	page, _, _ := svc.Query(ctx, QueryFilters{UserID: &u1, Limit: 2, Offset: 2})
	if len(page) != 2 {
		t.Fatalf("paged len = %d, want 2", len(page))
	}
}

func TestStart_LogsSubscribedEvent(t *testing.T) {
	svc, db, bus := newAuditService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.Start(ctx)

	// Retry-publish until the subscription is live and a row lands (or timeout).
	deadline := time.Now().Add(2 * time.Second)
	var n int64
	for time.Now().Before(deadline) {
		bus.Publish(events.EventDJConnect, events.Payload{"user_id": "u1", "station_id": "st1"})
		time.Sleep(25 * time.Millisecond)
		db.Model(&models.AuditLog{}).Where("action = ?", models.AuditActionLiveConnect).Count(&n)
		if n > 0 {
			break
		}
	}
	if n == 0 {
		t.Fatal("audit Start did not record the DJ-connect event")
	}
}
