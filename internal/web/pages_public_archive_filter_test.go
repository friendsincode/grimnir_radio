package web

import (
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

// setupArchiveFilterTest creates a test DB with station, media, playlists, and smart blocks.
func setupArchiveFilterTest(t *testing.T) (*chi.Mux, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{}, &models.Station{}, &models.MediaItem{},
		&models.Mount{}, &models.LandingPage{}, &migration.Job{},
		&models.Playlist{}, &models.PlaylistItem{}, &models.SmartBlock{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	db.Create(&models.User{ID: "u1", Email: "test@example.com", Password: "x"})

	station := models.Station{ID: "s1", Name: "Station One", Active: true, Public: true, Approved: true}
	db.Create(&station)

	station2 := models.Station{ID: "s2", Name: "Station Two", Active: true, Public: true, Approved: true}
	db.Create(&station2)

	// Media items
	db.Create(&models.MediaItem{ID: "m1", StationID: "s1", Title: "Alpha Song", Artist: "ArtistA", ShowInArchive: true})
	db.Create(&models.MediaItem{ID: "m2", StationID: "s1", Title: "Beta Song", Artist: "ArtistB", ShowInArchive: true})
	db.Create(&models.MediaItem{ID: "m3", StationID: "s1", Title: "Gamma Song", Artist: "ArtistC", Genre: "Rock", ShowInArchive: true})
	db.Create(&models.MediaItem{ID: "m4", StationID: "s2", Title: "Delta Song", Artist: "ArtistD", ShowInArchive: true})

	// Playlist with m1 and m2
	db.Create(&models.Playlist{ID: "pl1", StationID: "s1", Name: "Dork Table"})
	db.Create(&models.PlaylistItem{ID: "pi1", PlaylistID: "pl1", MediaID: "m1", Position: 0})
	db.Create(&models.PlaylistItem{ID: "pi2", PlaylistID: "pl1", MediaID: "m2", Position: 1})

	// Playlist on station 2
	db.Create(&models.Playlist{ID: "pl2", StationID: "s2", Name: "Station Two Playlist"})
	db.Create(&models.PlaylistItem{ID: "pi3", PlaylistID: "pl2", MediaID: "m4", Position: 0})

	// Smart block with artist filter
	db.Create(&models.SmartBlock{
		ID:        "sb1",
		StationID: "s1",
		Name:      "Rock Block",
		Rules:     map[string]any{"genre": "Rock"},
		Sequence:  map[string]any{"mode": "random"},
	})

	// Smart block with text_search
	db.Create(&models.SmartBlock{
		ID:        "sb2",
		StationID: "s1",
		Name:      "Alpha Search",
		Rules:     map[string]any{"text_search": "Alpha"},
		Sequence:  map[string]any{"mode": "random"},
	})

	tempDir := t.TempDir()
	h, err := NewHandler(db, []byte("test"), tempDir, nil, WebRTCConfig{}, HarborConfig{}, 0, events.NewBus(), nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	r := chi.NewRouter()
	h.Routes(r)
	return r, db
}

func TestArchiveShowDropdownRendered(t *testing.T) {
	router, _ := setupArchiveFilterTest(t)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/archive", nil)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()

	// Should contain the Show dropdown
	if !strings.Contains(body, "All Shows") {
		t.Error("expected 'All Shows' option in dropdown")
	}
	// Should contain playlist names
	if !strings.Contains(body, "Dork Table") {
		t.Error("expected 'Dork Table' playlist in dropdown")
	}
	// Should contain smart block names
	if !strings.Contains(body, "Rock Block") {
		t.Error("expected 'Rock Block' smart block in dropdown")
	}
}

func TestArchiveFilterByPlaylist(t *testing.T) {
	router, _ := setupArchiveFilterTest(t)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/archive?show=playlist:pl1", nil)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()

	// Should include media in the playlist
	if !strings.Contains(body, "Alpha Song") {
		t.Error("expected 'Alpha Song' (in playlist pl1)")
	}
	if !strings.Contains(body, "Beta Song") {
		t.Error("expected 'Beta Song' (in playlist pl1)")
	}
	// Should NOT include media outside the playlist
	if strings.Contains(body, "Gamma Song") {
		t.Error("unexpected 'Gamma Song' (not in playlist pl1)")
	}
	if strings.Contains(body, "Delta Song") {
		t.Error("unexpected 'Delta Song' (not in playlist pl1)")
	}
}

func TestArchiveFilterBySmartBlockGenre(t *testing.T) {
	router, _ := setupArchiveFilterTest(t)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/archive?show=smartblock:sb1", nil)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()

	// Should include Rock genre media
	if !strings.Contains(body, "Gamma Song") {
		t.Error("expected 'Gamma Song' (genre=Rock)")
	}
	// Should NOT include non-Rock media
	if strings.Contains(body, "Alpha Song") {
		t.Error("unexpected 'Alpha Song' (genre is not Rock)")
	}
}

func TestArchiveFilterBySmartBlockTextSearch(t *testing.T) {
	router, _ := setupArchiveFilterTest(t)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/archive?show=smartblock:sb2", nil)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()

	if !strings.Contains(body, "Alpha Song") {
		t.Error("expected 'Alpha Song' (matches text_search=Alpha)")
	}
	if strings.Contains(body, "Beta Song") {
		t.Error("unexpected 'Beta Song' (does not match text_search=Alpha)")
	}
}

func TestArchiveStationFilterNarrowsShowDropdown(t *testing.T) {
	router, _ := setupArchiveFilterTest(t)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/archive?station=s2", nil)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()

	// Should contain station 2's playlist
	if !strings.Contains(body, "Station Two Playlist") {
		t.Error("expected 'Station Two Playlist' in dropdown for station s2")
	}
	// Should NOT contain station 1's playlists/smart blocks
	if strings.Contains(body, "Dork Table") {
		t.Error("unexpected 'Dork Table' in dropdown when filtered to station s2")
	}
	if strings.Contains(body, "Rock Block") {
		t.Error("unexpected 'Rock Block' in dropdown when filtered to station s2")
	}
}

func TestArchiveShowFilterPreservedInPagination(t *testing.T) {
	router, _ := setupArchiveFilterTest(t)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/archive?show=playlist:pl1", nil)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()

	// The selected option should be marked as selected
	if !strings.Contains(body, `selected`) {
		t.Error("expected 'selected' attribute on the chosen show option")
	}
}

func TestArchiveClearButtonShownWithShowFilter(t *testing.T) {
	router, _ := setupArchiveFilterTest(t)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/archive?show=playlist:pl1", nil)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()

	if !strings.Contains(body, "Clear") {
		t.Error("expected 'Clear' button when show filter is active")
	}
}

func TestArchiveNoClearButtonWithoutFilters(t *testing.T) {
	router, _ := setupArchiveFilterTest(t)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/archive", nil)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()

	if strings.Contains(body, "x-circle") {
		t.Error("unexpected 'Clear' button when no filters are active")
	}
}
