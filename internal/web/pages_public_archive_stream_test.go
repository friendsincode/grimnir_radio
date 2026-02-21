package web

import (
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestArchiveStreamContentTypeByExtension(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Station{}, &models.MediaItem{}, &models.Mount{}, &models.LandingPage{}, &migration.Job{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create(&models.User{ID: "u1", Email: "test@example.com", Password: "x"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	station := models.Station{ID: "s1", Name: "S1", Active: true, Public: true, Approved: true}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	tempDir := t.TempDir()
	h, err := NewHandler(db, []byte("test"), tempDir, nil, "", "", WebRTCConfig{}, 0, events.NewBus(), nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	r := chi.NewRouter()
	h.Routes(r)

	cases := []struct {
		path string
		ct   string
	}{
		{path: "a.mp3", ct: "audio/mpeg"},
		{path: "b.flac", ct: "audio/flac"},
		{path: "c.wav", ct: "audio/wav"},
		{path: "d.ogg", ct: "audio/ogg"},
		{path: "e.m4a", ct: "audio/mp4"},
	}

	for i, tc := range cases {
		id := "m" + string(rune('a'+i))
		abs := filepath.Join(tempDir, tc.path)
		if err := os.WriteFile(abs, []byte("test"), 0644); err != nil {
			t.Fatalf("write media file: %v", err)
		}
		media := models.MediaItem{
			ID:            id,
			StationID:     station.ID,
			Path:          tc.path,
			Title:         "T",
			ShowInArchive: true,
			AllowDownload: true,
		}
		if err := db.Create(&media).Error; err != nil {
			t.Fatalf("create media: %v", err)
		}

		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/archive/"+id+"/stream", nil)
		r.ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Fatalf("expected 200, got %d for %s", rr.Code, tc.path)
		}
		if got := rr.Header().Get("Content-Type"); got != tc.ct {
			t.Fatalf("expected content-type %s, got %s for %s", tc.ct, got, tc.path)
		}
	}
}

func TestArchiveStreamDownloadWithShortPathAddsFallbackExtension(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Station{}, &models.MediaItem{}, &models.Mount{}, &models.LandingPage{}, &migration.Job{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create(&models.User{ID: "u1", Email: "test@example.com", Password: "x"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	station := models.Station{ID: "s1", Name: "S1", Active: true, Public: true, Approved: true}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "x"), []byte("test"), 0644); err != nil {
		t.Fatalf("write media file: %v", err)
	}
	media := models.MediaItem{
		ID:            "m1",
		StationID:     station.ID,
		Path:          "x",
		Title:         "Title",
		ShowInArchive: true,
		AllowDownload: true,
	}
	if err := db.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	h, err := NewHandler(db, []byte("test"), tempDir, nil, "", "", WebRTCConfig{}, 0, events.NewBus(), nil, zerolog.Nop())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	r := chi.NewRouter()
	h.Routes(r)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/archive/m1/stream?download=1", nil)
	r.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	cd := rr.Header().Get("Content-Disposition")
	if !strings.Contains(cd, ".bin") {
		t.Fatalf("expected fallback .bin extension in content-disposition, got %q", cd)
	}
}
