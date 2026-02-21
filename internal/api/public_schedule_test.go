package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestPublicSchedule_ArtworkURLResolvableOrEmpty(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.Station{}, &models.Show{}, &models.ShowInstance{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now()
	station := models.Station{ID: "s1", Name: "Station", Public: true}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	showRoot := models.Show{
		ID:                     "show-root",
		StationID:              station.ID,
		Name:                   "Root Art",
		ArtworkPath:            "/assets/root.jpg",
		DefaultDurationMinutes: 60,
		DTStart:                now,
		Timezone:               "UTC",
	}
	showRelative := models.Show{
		ID:                     "show-rel",
		StationID:              station.ID,
		Name:                   "Relative Art",
		ArtworkPath:            "cover.jpg",
		DefaultDurationMinutes: 60,
		DTStart:                now,
		Timezone:               "UTC",
	}
	if err := db.Create(&showRoot).Error; err != nil {
		t.Fatalf("create showRoot: %v", err)
	}
	if err := db.Create(&showRelative).Error; err != nil {
		t.Fatalf("create showRelative: %v", err)
	}
	inst1 := models.ShowInstance{
		ID:        "inst1",
		ShowID:    showRoot.ID,
		StationID: station.ID,
		StartsAt:  now.Add(1 * time.Hour),
		EndsAt:    now.Add(2 * time.Hour),
		Status:    models.ShowInstanceScheduled,
	}
	inst2 := models.ShowInstance{
		ID:        "inst2",
		ShowID:    showRelative.ID,
		StationID: station.ID,
		StartsAt:  now.Add(3 * time.Hour),
		EndsAt:    now.Add(4 * time.Hour),
		Status:    models.ShowInstanceScheduled,
	}
	if err := db.Create(&inst1).Error; err != nil {
		t.Fatalf("create inst1: %v", err)
	}
	if err := db.Create(&inst2).Error; err != nil {
		t.Fatalf("create inst2: %v", err)
	}

	a := &API{db: db}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/public/schedule?station_id=s1", nil)
	a.handlePublicSchedule(rr, req)
	if rr.Code != 200 {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Schedule []PublicShowInstance `json:"schedule"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Schedule) != 2 {
		t.Fatalf("expected 2 schedule items, got %d", len(resp.Schedule))
	}
	if resp.Schedule[0].Show.ArtworkURL != "/assets/root.jpg" {
		t.Fatalf("expected root-relative artwork URL, got %q", resp.Schedule[0].Show.ArtworkURL)
	}
	if resp.Schedule[1].Show.ArtworkURL != "" {
		t.Fatalf("expected unresolved relative artwork path to be empty, got %q", resp.Schedule[1].Show.ArtworkURL)
	}
}

func TestPublicNowPlaying_ArtworkURLResolvableOrEmpty(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.Station{}, &models.Show{}, &models.ShowInstance{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now()
	station := models.Station{ID: "s2", Name: "Station 2", Public: true}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	show := models.Show{
		ID:                     "show-current",
		StationID:              station.ID,
		Name:                   "Current Show",
		ArtworkPath:            "/assets/current.jpg",
		DefaultDurationMinutes: 60,
		DTStart:                now.Add(-1 * time.Hour),
		Timezone:               "UTC",
	}
	if err := db.Create(&show).Error; err != nil {
		t.Fatalf("create show: %v", err)
	}
	current := models.ShowInstance{
		ID:        "inst-current",
		ShowID:    show.ID,
		StationID: station.ID,
		StartsAt:  now.Add(-30 * time.Minute),
		EndsAt:    now.Add(30 * time.Minute),
		Status:    models.ShowInstanceScheduled,
	}
	if err := db.Create(&current).Error; err != nil {
		t.Fatalf("create current instance: %v", err)
	}

	a := &API{db: db}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/public/schedule/now?station_id=s2", nil)
	a.handlePublicNowPlaying(rr, req)
	if rr.Code != 200 {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Current *PublicShowInstance `json:"current"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Current == nil {
		t.Fatalf("expected current show in response")
	}
	if resp.Current.Show.ArtworkURL != "/assets/current.jpg" {
		t.Fatalf("expected current artwork URL to be root-relative and reachable, got %q", resp.Current.Show.ArtworkURL)
	}
}
