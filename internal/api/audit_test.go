/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/audit"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newAuditAPITest(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.AuditLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	bus := events.NewBus()
	auditSvc := audit.NewService(db, bus, zerolog.Nop())
	return &API{db: db, logger: zerolog.Nop(), auditSvc: auditSvc}, db
}

func TestAuditAPI_List(t *testing.T) {
	a, _ := newAuditAPITest(t)

	// List audit logs (empty)
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	a.handleAuditList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list audit: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if _, ok := resp["audit_logs"]; !ok {
		t.Fatal("expected audit_logs key")
	}
	if _, ok := resp["total"]; !ok {
		t.Fatal("expected total key")
	}
}

func TestAuditAPI_StationList(t *testing.T) {
	a, _ := newAuditAPITest(t)

	t.Run("missing station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		// No chi param for stationID
		rr := httptest.NewRecorder()
		a.handleStationAuditList(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("with station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/s1/audit", nil)
		req = withChiParam(req, "stationID", "s1")
		rr := httptest.NewRecorder()
		a.handleStationAuditList(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200, body=%s", rr.Code, rr.Body.String())
		}
		var resp map[string]any
		json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
		if _, ok := resp["audit_logs"]; !ok {
			t.Fatal("expected audit_logs key")
		}
	})
}

func TestAuditAPI_ParseFilters(t *testing.T) {
	// Test parseAuditFilters as a pure function
	req := httptest.NewRequest("GET", "/?limit=20&offset=5&action=test&since=2025-01-01T00:00:00Z", nil)
	filters := parseAuditFilters(req)
	if filters.Limit != 20 {
		t.Fatalf("expected limit=20, got %d", filters.Limit)
	}
	if filters.Offset != 5 {
		t.Fatalf("expected offset=5, got %d", filters.Offset)
	}
	if filters.Action == nil || *filters.Action != "test" {
		t.Fatalf("expected action=test, got %v", filters.Action)
	}
}

func TestAuditAPI_ToResponse(t *testing.T) {
	// Test toAuditLogResponse as a pure function
	stationID := "st-1"
	userID := "u-1"
	log := models.AuditLog{
		ID:           "log-1",
		Timestamp:    time.Now(),
		UserID:       &userID,
		StationID:    &stationID,
		Action:       "create",
		ResourceType: "show",
		ResourceID:   "show-1",
	}
	resp := toAuditLogResponse(log)
	if resp.Action != "create" {
		t.Fatalf("expected action=create, got %s", resp.Action)
	}
	if resp.ResourceType != "show" {
		t.Fatalf("expected resource_type=show, got %s", resp.ResourceType)
	}
}
