package web

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/landingpage"
	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestStationLandingOrdersStations_CurrentThenPlatformFirstThenRest(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Station{}, &models.Mount{}, &models.LandingPage{}, &migration.Job{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Avoid setup redirect.
	if err := db.Create(&models.User{ID: "u1", Email: "test@example.com", Password: "x"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	platform1 := models.Station{ID: "s1", Name: "Platform One", Shortcode: "platform1", Active: true, Public: true, Approved: true, SortOrder: 1}
	platform2 := models.Station{ID: "s2", Name: "Platform Two", Shortcode: "platform2", Active: true, Public: true, Approved: true, SortOrder: 2}
	current := models.Station{ID: "s3", Name: "Current Station", Shortcode: "current", Active: true, Public: true, Approved: true, SortOrder: 3}

	if err := db.Create(&platform1).Error; err != nil {
		t.Fatalf("create platform1: %v", err)
	}
	if err := db.Create(&platform2).Error; err != nil {
		t.Fatalf("create platform2: %v", err)
	}
	if err := db.Create(&current).Error; err != nil {
		t.Fatalf("create current: %v", err)
	}

	// Each station needs a mount so templates render stream URLs.
	_ = db.Create(&models.Mount{ID: "m1", StationID: platform1.ID, Name: "p1", Format: "mp3", Bitrate: 128}).Error
	_ = db.Create(&models.Mount{ID: "m2", StationID: platform2.ID, Name: "p2", Format: "mp3", Bitrate: 128}).Error
	_ = db.Create(&models.Mount{ID: "m3", StationID: current.ID, Name: "c1", Format: "mp3", Bitrate: 128}).Error

	// Create platform station order in landing page config.
	lpSvc := landingpage.NewService(db, nil, "/tmp", zerolog.Nop())
	platformPage, err := lpSvc.GetOrCreatePlatform(t.Context())
	if err != nil {
		t.Fatalf("get platform landing: %v", err)
	}
	if platformPage.PublishedConfig == nil {
		platformPage.PublishedConfig = map[string]any{}
	}
	content, _ := platformPage.PublishedConfig["content"].(map[string]any)
	if content == nil {
		content = map[string]any{}
	}
	content["stationOrder"] = []string{platform1.ID, platform2.ID, current.ID}
	platformPage.PublishedConfig["content"] = content
	if err := db.Save(platformPage).Error; err != nil {
		t.Fatalf("save platform landing: %v", err)
	}

	h, err := NewHandler(
		db,
		[]byte("test"),
		"/tmp",
		nil,
		WebRTCConfig{},
		HarborConfig{},
		0,
		events.NewBus(),
		nil,
		zerolog.Nop(),
	)
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	h.SetLandingPageService(lpSvc)

	r := chi.NewRouter()
	h.Routes(r)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/stations/"+current.Shortcode, nil)
	r.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	start := strings.Index(body, "window.GRIMNIR_STATION_ORDER = [")
	if start == -1 {
		t.Fatalf("missing GRIMNIR_STATION_ORDER in response")
	}
	end := strings.Index(body[start:], "];")
	if end == -1 {
		t.Fatalf("missing station order array terminator")
	}
	orderSnippet := body[start : start+end]

	idxCurrent := strings.Index(orderSnippet, "\""+current.ID+"\"")
	idxP1 := strings.Index(orderSnippet, "\""+platform1.ID+"\"")
	idxP2 := strings.Index(orderSnippet, "\""+platform2.ID+"\"")
	if idxCurrent == -1 || idxP1 == -1 || idxP2 == -1 {
		t.Fatalf("expected station ids in order snippet; current=%d p1=%d p2=%d", idxCurrent, idxP1, idxP2)
	}
	if !(idxCurrent < idxP1 && idxP1 < idxP2) {
		t.Fatalf("unexpected order: current=%d p1=%d p2=%d\nsnippet=%s", idxCurrent, idxP1, idxP2, orderSnippet)
	}
}
