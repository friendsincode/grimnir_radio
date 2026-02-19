package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestScheduleCreateEntryRejectsOverlap(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.Station{}, &models.Mount{}, &models.ScheduleEntry{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	station := models.Station{ID: "s1", Name: "S1"}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.Mount{ID: "m1", StationID: station.ID, Name: "main", Format: "mp3", Bitrate: 128}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	existing := models.ScheduleEntry{
		ID:         "e1",
		StationID:  station.ID,
		MountID:    "m1",
		StartsAt:   time.Date(2026, 2, 19, 10, 0, 0, 0, time.UTC),
		EndsAt:     time.Date(2026, 2, 19, 11, 0, 0, 0, time.UTC),
		SourceType: "playlist",
		SourceID:   "p1",
	}
	if err := db.Create(&existing).Error; err != nil {
		t.Fatalf("create entry: %v", err)
	}

	h := &Handler{db: db, logger: zerolog.Nop()}

	reqBody, _ := json.Marshal(map[string]any{
		"mount_id":    "m1",
		"starts_at":   time.Date(2026, 2, 19, 10, 30, 0, 0, time.UTC),
		"ends_at":     time.Date(2026, 2, 19, 11, 30, 0, 0, time.UTC),
		"source_type": "playlist",
		"source_id":   "p2",
		"metadata":    map[string]any{},
	})
	req := httptest.NewRequest(http.MethodPost, "/dashboard/schedule/entries", bytes.NewReader(reqBody))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyStation, &station))
	rr := httptest.NewRecorder()

	h.ScheduleCreateEntry(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected %d, got %d body=%s", http.StatusConflict, rr.Code, rr.Body.String())
	}
}

func TestScheduleCreateEntryAllowsBoundaryTouch(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.Station{}, &models.Mount{}, &models.ScheduleEntry{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	station := models.Station{ID: "s1", Name: "S1"}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.Mount{ID: "m1", StationID: station.ID, Name: "main", Format: "mp3", Bitrate: 128}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	existing := models.ScheduleEntry{
		ID:         "e1",
		StationID:  station.ID,
		MountID:    "m1",
		StartsAt:   time.Date(2026, 2, 19, 10, 0, 0, 0, time.UTC),
		EndsAt:     time.Date(2026, 2, 19, 11, 0, 0, 0, time.UTC),
		SourceType: "playlist",
		SourceID:   "p1",
	}
	if err := db.Create(&existing).Error; err != nil {
		t.Fatalf("create entry: %v", err)
	}

	h := &Handler{db: db, logger: zerolog.Nop()}

	reqBody, _ := json.Marshal(map[string]any{
		"mount_id":    "m1",
		"starts_at":   time.Date(2026, 2, 19, 11, 0, 0, 0, time.UTC),
		"ends_at":     time.Date(2026, 2, 19, 12, 0, 0, 0, time.UTC),
		"source_type": "playlist",
		"source_id":   "p2",
		"metadata":    map[string]any{},
	})
	req := httptest.NewRequest(http.MethodPost, "/dashboard/schedule/entries", bytes.NewReader(reqBody))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyStation, &station))
	rr := httptest.NewRecorder()

	h.ScheduleCreateEntry(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}
}

func TestScheduleDeleteEntryVirtualInstanceDeletesOverride(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.Station{}, &models.Mount{}, &models.ScheduleEntry{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	station := models.Station{ID: "s1", Name: "S1"}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	parentID := "parent-entry"
	start := time.Date(2026, 2, 20, 9, 0, 0, 0, time.UTC)
	override := models.ScheduleEntry{
		ID:                 "override-entry",
		StationID:          station.ID,
		MountID:            "m1",
		StartsAt:           start,
		EndsAt:             start.Add(30 * time.Minute),
		SourceType:         "playlist",
		SourceID:           "p1",
		IsInstance:         true,
		RecurrenceParentID: &parentID,
	}
	if err := db.Create(&override).Error; err != nil {
		t.Fatalf("create override: %v", err)
	}

	virtualID := recurrenceInstanceKey(parentID, start)
	h := &Handler{db: db, logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodDelete, "/dashboard/schedule/entries/"+virtualID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", virtualID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.ScheduleDeleteEntry(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected %d, got %d body=%s", http.StatusNoContent, rr.Code, rr.Body.String())
	}

	var count int64
	if err := db.Model(&models.ScheduleEntry{}).Where("id = ?", override.ID).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected override to be deleted, count=%d", count)
	}
}
