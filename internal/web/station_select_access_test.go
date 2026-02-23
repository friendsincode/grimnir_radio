package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestStationSelectSubmit_PlatformAdminCanSelectRenderedStation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Station{}, &models.StationUser{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	admin := models.User{ID: "u-admin", Email: "admin@example.com", PlatformRole: models.PlatformRole("admin")}
	station := models.Station{ID: "s-1", Name: "Only", Active: true}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	h := &Handler{db: db}

	form := url.Values{}
	form.Set("station_id", station.ID)
	req := httptest.NewRequest(http.MethodPost, "/dashboard/stations/select", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))

	rr := httptest.NewRecorder()
	h.StationSelectSubmit(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("HX-Redirect"); got != "/dashboard" {
		t.Fatalf("HX-Redirect=%q, want /dashboard", got)
	}
	if setCookie := rr.Header().Get("Set-Cookie"); !strings.Contains(setCookie, "grimnir_station=s-1") {
		t.Fatalf("expected station cookie, got %q", setCookie)
	}
}

func TestStationSelect_AutoSelectsSingleStation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Station{}, &models.StationUser{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	admin := models.User{ID: "u-admin", Email: "admin@example.com", PlatformRole: models.PlatformRole("admin")}
	station := models.Station{ID: "s-1", Name: "Only", Active: true}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	h := &Handler{db: db}
	req := httptest.NewRequest(http.MethodGet, "/dashboard/stations/select", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, &admin))
	rr := httptest.NewRecorder()

	h.StationSelect(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Location"); got != "/dashboard" {
		t.Fatalf("Location=%q, want /dashboard", got)
	}
	if setCookie := rr.Header().Get("Set-Cookie"); !strings.Contains(setCookie, "grimnir_station=s-1") {
		t.Fatalf("expected station cookie, got %q", setCookie)
	}
}

