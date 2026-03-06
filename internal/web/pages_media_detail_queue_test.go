package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newMediaDetailTestHandler(t *testing.T) (*Handler, *gorm.DB, models.User, models.Station) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{},
		&models.Station{},
		&models.StationUser{},
		&models.MediaItem{},
		&models.Mount{},
		&models.Tag{},
		&models.MediaTagLink{},
		&models.PlayHistory{},
		&models.Playlist{},
		&models.PlaylistItem{},
		&models.ScheduleEntry{},
		&models.ClockHour{},
		&models.ClockSlot{},
		&models.LandingPage{},
		&migration.Job{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	user := models.User{ID: "u1", Email: "user@example.com", Password: "x"}
	station := models.Station{ID: "s1", Name: "Station One", Active: true}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.StationUser{
		ID:        "su1",
		UserID:    user.ID,
		StationID: station.ID,
		Role:      models.StationRoleManager,
	}).Error; err != nil {
		t.Fatalf("create station user: %v", err)
	}

	h, err := NewHandler(db, []byte("test"), t.TempDir(), nil, WebRTCConfig{}, HarborConfig{}, 0, events.NewBus(), nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	return h, db, user, station
}

func mediaDetailRequest(user models.User, station models.Station, mediaID string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/dashboard/media/"+mediaID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", mediaID)

	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, ctxKeyUser, &user)
	ctx = context.WithValue(ctx, ctxKeyStation, &station)
	return req.WithContext(ctx)
}

func TestMediaDetailRendersQueueControlsWithRealQueueAPI(t *testing.T) {
	h, db, user, station := newMediaDetailTestHandler(t)

	if err := db.Create(&models.MediaItem{
		ID:        "media-1",
		StationID: station.ID,
		Title:     "Track One",
		Artist:    "Artist One",
		Path:      "track-one.mp3",
	}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}
	if err := db.Create(&models.Mount{
		ID:        "mount-main",
		StationID: station.ID,
		Name:      "Main",
		Format:    "mp3",
		URL:       "/live/main",
	}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	rr := httptest.NewRecorder()
	h.MediaDetail(rr, mediaDetailRequest(user, station, "media-1"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	for _, want := range []string{
		`id="queueMountSelect"`,
		`<option value="mount-main">Main</option>`,
		`id="addToQueueBtn"`,
		`fetch('/api/v1/playout/queue'`,
		`station_id: 's1'`,
		`media_id: 'media-1'`,
		`Queued on ${mountName}${pos}`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected media detail page to contain %q", want)
		}
	}
}

func TestMediaDetailDisablesQueueControlsWithoutMounts(t *testing.T) {
	h, db, user, station := newMediaDetailTestHandler(t)

	if err := db.Create(&models.MediaItem{
		ID:        "media-2",
		StationID: station.ID,
		Title:     "Track Two",
		Path:      "track-two.mp3",
	}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	rr := httptest.NewRecorder()
	h.MediaDetail(rr, mediaDetailRequest(user, station, "media-2"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	for _, want := range []string{
		`id="queueMountSelect" class="form-select form-select-sm" disabled`,
		`id="addToQueueBtn" title="Add track to play queue" disabled`,
		`No mounts configured for this station.`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected media detail page to contain %q", want)
		}
	}
}
