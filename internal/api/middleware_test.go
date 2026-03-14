/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// okHandler is a trivial handler that writes 200.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func newMiddlewareAPITest(t *testing.T) *API {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.StationUser{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return &API{db: db, logger: zerolog.Nop()}
}

func TestRequireRoles_NoAuth(t *testing.T) {
	a := newMiddlewareAPITest(t)
	h := a.requireRoles(models.RoleAdmin)(okHandler)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rr.Code)
	}
}

func TestRequireRoles_PlatformAdmin_Passes(t *testing.T) {
	a := newMiddlewareAPITest(t)
	h := a.requireRoles(models.RoleAdmin)(okHandler)

	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
		UserID: "u1",
		Roles:  []string{string(models.PlatformRoleAdmin)},
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
}

func TestRequireRoles_MatchingRole_Passes(t *testing.T) {
	a := newMiddlewareAPITest(t)
	h := a.requireRoles(models.RoleAdmin)(okHandler)

	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
		UserID: "u1",
		Roles:  []string{string(models.RoleAdmin)},
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
}

func TestRequireRoles_Forbidden(t *testing.T) {
	a := newMiddlewareAPITest(t)
	h := a.requireRoles(models.RoleAdmin)(okHandler)

	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
		UserID: "u1",
		Roles:  []string{string(models.RoleDJ)},
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", rr.Code)
	}
}

func TestRequirePlatformAdmin_NoAuth(t *testing.T) {
	a := newMiddlewareAPITest(t)
	h := a.requirePlatformAdmin()(okHandler)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rr.Code)
	}
}

func TestRequirePlatformAdmin_Authorized(t *testing.T) {
	a := newMiddlewareAPITest(t)
	h := a.requirePlatformAdmin()(okHandler)

	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
		UserID: "u1",
		Roles:  []string{string(models.PlatformRoleAdmin)},
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
}

func TestRequirePlatformAdmin_Forbidden(t *testing.T) {
	a := newMiddlewareAPITest(t)
	h := a.requirePlatformAdmin()(okHandler)

	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
		UserID: "u1",
		Roles:  []string{string(models.RoleAdmin)},
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", rr.Code)
	}
}

func TestRequireRolesOrPlatformAdmin_NoAuth(t *testing.T) {
	a := newMiddlewareAPITest(t)
	h := a.requireRolesOrPlatformAdmin(models.RoleAdmin)(okHandler)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rr.Code)
	}
}

func TestRequireRolesOrPlatformAdmin_PlatformAdmin(t *testing.T) {
	a := newMiddlewareAPITest(t)
	h := a.requireRolesOrPlatformAdmin(models.RoleDJ)(okHandler)

	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
		UserID: "u1",
		Roles:  []string{string(models.PlatformRoleAdmin)},
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
}

func TestRequireRolesOrPlatformAdmin_MatchingRole(t *testing.T) {
	a := newMiddlewareAPITest(t)
	h := a.requireRolesOrPlatformAdmin(models.RoleManager)(okHandler)

	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
		UserID: "u1",
		Roles:  []string{string(models.RoleManager)},
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
}

func TestRequireRolesOrPlatformAdmin_Forbidden(t *testing.T) {
	a := newMiddlewareAPITest(t)
	h := a.requireRolesOrPlatformAdmin(models.RoleAdmin)(okHandler)

	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
		UserID: "u1",
		Roles:  []string{string(models.RoleDJ)},
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", rr.Code)
	}
}

func TestClaimsHasPlatformAdmin(t *testing.T) {
	if claimsHasPlatformAdmin(nil) {
		t.Fatal("nil claims should return false")
	}
	if claimsHasPlatformAdmin(&auth.Claims{}) {
		t.Fatal("empty claims should return false")
	}
	if !claimsHasPlatformAdmin(&auth.Claims{Roles: []string{string(models.PlatformRoleAdmin)}}) {
		t.Fatal("platform admin claims should return true")
	}
}
