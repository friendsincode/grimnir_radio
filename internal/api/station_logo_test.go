package api

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestHandleStationLogo_VisibilityRestrictions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.Station{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	logoBytes := []byte{0x89, 0x50, 0x4e, 0x47}
	seeds := []models.Station{
		{ID: "s-public", Name: "Public", Active: true, Public: true, Approved: true, Logo: logoBytes, LogoMime: "image/png"},
		{ID: "s-private", Name: "Private", Active: true, Public: false, Approved: true, Logo: logoBytes, LogoMime: "image/png"},
		{ID: "s-inactive", Name: "Inactive", Active: true, Public: true, Approved: true, Logo: logoBytes, LogoMime: "image/png"},
		{ID: "s-unapproved", Name: "Unapproved", Active: true, Public: true, Approved: true, Logo: logoBytes, LogoMime: "image/png"},
	}
	for _, s := range seeds {
		if err := db.Create(&s).Error; err != nil {
			t.Fatalf("seed station %s: %v", s.ID, err)
		}
	}
	if err := db.Model(&models.Station{}).Where("id = ?", "s-inactive").Update("active", false).Error; err != nil {
		t.Fatalf("set inactive: %v", err)
	}
	if err := db.Model(&models.Station{}).Where("id = ?", "s-unapproved").Update("approved", false).Error; err != nil {
		t.Fatalf("set unapproved: %v", err)
	}

	a := &API{db: db}
	tests := []struct {
		name       string
		stationID  string
		wantStatus int
	}{
		{name: "public approved active", stationID: "s-public", wantStatus: 200},
		{name: "private", stationID: "s-private", wantStatus: 404},
		{name: "inactive", stationID: "s-inactive", wantStatus: 404},
		{name: "unapproved", stationID: "s-unapproved", wantStatus: 404},
		{name: "missing", stationID: "does-not-exist", wantStatus: 404},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/v1/stations/"+tc.stationID+"/logo", nil)
			routeCtx := chi.NewRouteContext()
			routeCtx.URLParams.Add("stationID", tc.stationID)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
			rr := httptest.NewRecorder()

			a.handleStationLogo(rr, req)

			if rr.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", rr.Code, tc.wantStatus, rr.Body.String())
			}
			if tc.wantStatus == 200 {
				if rr.Header().Get("Content-Type") != "image/png" {
					t.Fatalf("expected image/png content type, got %q", rr.Header().Get("Content-Type"))
				}
				if rr.Body.Len() == 0 {
					t.Fatalf("expected logo payload")
				}
			}
		})
	}
}
