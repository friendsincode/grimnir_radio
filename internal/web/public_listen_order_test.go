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
	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestListenOrdersStationsBySortOrderThenName(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Station{}, &models.Mount{}, &models.LandingPage{}, &migration.Job{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Avoid setup redirect (public routes still run setup check middleware).
	if err := db.Create(&models.User{ID: "u1", Email: "test@example.com", Password: "x"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Three public stations with mixed sort/name.
	sA := models.Station{ID: "sA", Name: "Zulu", Active: true, Public: true, Approved: true, SortOrder: 2}
	sB := models.Station{ID: "sB", Name: "Alpha", Active: true, Public: true, Approved: true, SortOrder: 1}
	sC := models.Station{ID: "sC", Name: "Bravo", Active: true, Public: true, Approved: true, SortOrder: 1}
	if err := db.Create(&sA).Error; err != nil {
		t.Fatalf("create sA: %v", err)
	}
	if err := db.Create(&sB).Error; err != nil {
		t.Fatalf("create sB: %v", err)
	}
	if err := db.Create(&sC).Error; err != nil {
		t.Fatalf("create sC: %v", err)
	}

	// Each station needs a mount so template renders mount list.
	_ = db.Create(&models.Mount{ID: "mA", StationID: sA.ID, Name: "ma", Format: "mp3", Bitrate: 128}).Error
	_ = db.Create(&models.Mount{ID: "mB", StationID: sB.ID, Name: "mb", Format: "mp3", Bitrate: 128}).Error
	_ = db.Create(&models.Mount{ID: "mC", StationID: sC.ID, Name: "mc", Format: "mp3", Bitrate: 128}).Error

	h, err := NewHandler(
		db,
		[]byte("test"),
		"/tmp",
		nil,
		"",
		"",
		WebRTCConfig{},
		0,
		events.NewBus(),
		nil,
		zerolog.Nop(),
	)
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	r := chi.NewRouter()
	h.Routes(r)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/listen", nil)
	r.ServeHTTP(rr, req)

	body := rr.Body.String()
	// We only care that station card headers appear in order.
	idxB := strings.Index(body, "<h5 class=\"mb-0\">"+sB.Name+"</h5>")
	idxC := strings.Index(body, "<h5 class=\"mb-0\">"+sC.Name+"</h5>")
	idxA := strings.Index(body, "<h5 class=\"mb-0\">"+sA.Name+"</h5>")
	if idxB == -1 || idxC == -1 || idxA == -1 {
		t.Fatalf("expected station names in response; idxB=%d idxC=%d idxA=%d", idxB, idxC, idxA)
	}
	if !(idxB < idxC && idxC < idxA) {
		t.Fatalf("unexpected order: Alpha=%d Bravo=%d Zulu=%d", idxB, idxC, idxA)
	}
}
