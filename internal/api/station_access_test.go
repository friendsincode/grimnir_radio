package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestRequireStationAccess_PlatformAdminAllowed(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	a := &API{db: db}

	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
		UserID: "u1",
		Roles:  []string{string(models.PlatformRoleAdmin)},
	}))
	rr := httptest.NewRecorder()

	if ok := a.requireStationAccess(rr, req, "station-a"); !ok {
		t.Fatalf("expected platform admin access, status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRequireStationAccess_NonAdminDeniedWithoutMembership(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.StationUser{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	a := &API{db: db}

	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
		UserID: "u1",
		Roles:  []string{string(models.RoleManager)},
	}))
	rr := httptest.NewRecorder()

	if ok := a.requireStationAccess(rr, req, "station-a"); ok {
		t.Fatalf("expected access denied")
	}
	if rr.Code != 403 {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestRequireStationAccess_NonAdminAllowedWithMembership(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.StationUser{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create(&models.StationUser{
		ID:        "su1",
		UserID:    "u1",
		StationID: "station-a",
		Role:      models.StationRoleManager,
	}).Error; err != nil {
		t.Fatalf("seed station user: %v", err)
	}

	a := &API{db: db}
	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
		UserID: "u1",
		Roles:  []string{string(models.RoleManager)},
	}))
	rr := httptest.NewRecorder()

	if ok := a.requireStationAccess(rr, req, "station-a"); !ok {
		t.Fatalf("expected access allowed, status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleMountsList_StationScopedAuthorization(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.StationUser{}, &models.Mount{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create(&models.Mount{
		ID:        "m1",
		StationID: "station-a",
		Name:      "Main",
		URL:       "/stream",
		Format:    "mp3",
	}).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}
	if err := db.Create(&models.StationUser{
		ID:        "su1",
		UserID:    "u-member",
		StationID: "station-a",
		Role:      models.StationRoleManager,
	}).Error; err != nil {
		t.Fatalf("seed station user: %v", err)
	}

	a := &API{db: db}

	t.Run("platform admin allowed any station", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/stations/station-a/mounts", nil)
		routeCtx := chi.NewRouteContext()
		routeCtx.URLParams.Add("stationID", "station-a")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
			UserID: "u-admin",
			Roles:  []string{string(models.PlatformRoleAdmin)},
		}))
		rr := httptest.NewRecorder()

		a.handleMountsList(rr, req)
		if rr.Code != 200 {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var mounts []models.Mount
		if err := json.NewDecoder(rr.Body).Decode(&mounts); err != nil {
			t.Fatalf("decode mounts: %v", err)
		}
		if len(mounts) != 1 || mounts[0].ID != "m1" {
			t.Fatalf("unexpected mounts payload: %+v", mounts)
		}
	})

	t.Run("non-admin denied outside assigned station", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/stations/station-a/mounts", nil)
		routeCtx := chi.NewRouteContext()
		routeCtx.URLParams.Add("stationID", "station-a")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
			UserID: "u-denied",
			Roles:  []string{string(models.RoleManager)},
		}))
		rr := httptest.NewRecorder()

		a.handleMountsList(rr, req)
		if rr.Code != 403 {
			t.Fatalf("expected 403, got %d body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("non-admin allowed with membership", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/stations/station-a/mounts", nil)
		routeCtx := chi.NewRouteContext()
		routeCtx.URLParams.Add("stationID", "station-a")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
			UserID: "u-member",
			Roles:  []string{string(models.RoleManager)},
		}))
		rr := httptest.NewRecorder()

		a.handleMountsList(rr, req)
		if rr.Code != 200 {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
	})
}
