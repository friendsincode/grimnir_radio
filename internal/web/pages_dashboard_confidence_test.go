package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestDashboardPlayoutConfidenceRendersQueueHealthAndActions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{},
		&models.Station{},
		&models.StationUser{},
		&models.Mount{},
		&models.MediaItem{},
		&models.PlayoutQueueItem{},
		&models.ExecutorState{},
		&models.MountPlayoutState{},
		&models.AuditLog{},
		&models.LandingPage{},
		&migration.Job{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	user := models.User{ID: "u1", Email: "manager@example.com", Password: "x"}
	station := models.Station{ID: "s1", Name: "Station One", Active: true}
	mount := models.Mount{ID: "m1", StationID: station.ID, Name: "Main", Format: "mp3", URL: "/live/main"}
	media := models.MediaItem{ID: "track-1", StationID: station.ID, Title: "Track One", Artist: "Artist One", Path: "track1.mp3"}

	for _, record := range []any{&user, &station, &mount, &media} {
		if err := db.Create(record).Error; err != nil {
			t.Fatalf("seed record: %v", err)
		}
	}
	if err := db.Create(&models.StationUser{
		ID:        "su1",
		UserID:    user.ID,
		StationID: station.ID,
		Role:      models.StationRoleManager,
	}).Error; err != nil {
		t.Fatalf("create station user: %v", err)
	}

	now := time.Date(2026, 3, 6, 22, 0, 0, 0, time.UTC)
	if err := db.Create(&models.ExecutorState{
		ID:              "exec-1",
		StationID:       station.ID,
		MountID:         mount.ID,
		State:           models.ExecutorStatePlaying,
		CurrentPriority: models.PriorityAutomation,
		BufferDepthMS:   1400,
		UnderrunCount:   2,
		LastHeartbeat:   now,
	}).Error; err != nil {
		t.Fatalf("create executor state: %v", err)
	}
	if err := db.Create(&models.MountPlayoutState{
		MountID:    mount.ID,
		StationID:  station.ID,
		EntryID:    "entry-1",
		MediaID:    media.ID,
		SourceType: "playlist",
		SourceID:   "playlist-1",
		Position:   1,
		TotalItems: 3,
		StartedAt:  now.Add(-2 * time.Minute),
		EndsAt:     now.Add(3 * time.Minute),
		UpdatedAt:  now,
	}).Error; err != nil {
		t.Fatalf("create mount playout state: %v", err)
	}
	if err := db.Create(&models.PlayoutQueueItem{
		ID:        "q1",
		StationID: station.ID,
		MountID:   mount.ID,
		MediaID:   media.ID,
		Position:  1,
		CreatedAt: now.Add(-1 * time.Minute),
	}).Error; err != nil {
		t.Fatalf("create queue item: %v", err)
	}
	stationID := station.ID
	if err := db.Create(&models.AuditLog{
		ID:        "audit-1",
		Timestamp: now.Add(-30 * time.Second),
		StationID: &stationID,
		UserEmail: user.Email,
		Action:    models.AuditActionPlayoutQueueAdd,
		Details: map[string]any{
			"mount_id": mount.ID,
			"media_id": media.ID,
			"position": 1,
		},
		CreatedAt: now.Add(-30 * time.Second),
	}).Error; err != nil {
		t.Fatalf("create audit log: %v", err)
	}

	h, err := NewHandler(db, []byte("test"), t.TempDir(), nil, WebRTCConfig{}, HarborConfig{}, 0, events.NewBus(), nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/dashboard/playout/confidence", nil)
	rctx := chi.NewRouteContext()
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.DashboardPlayoutConfidence(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	for _, want := range []string{
		"Operator Confidence",
		"playing",
		"1400 ms",
		"Underruns: <strong>2</strong>",
		"Track One",
		"Artist One",
		"playout.queue.add",
		"manager@example.com",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q", want)
		}
	}
}
