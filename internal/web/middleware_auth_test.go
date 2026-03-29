/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// ---------------------------------------------------------------------------
// RequireAuth
// ---------------------------------------------------------------------------

func TestRequireAuth_RedirectsUnauthenticated(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dashboard/media", nil)

	called := false
	h.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if loc != "/login?redirect=/dashboard/media" {
		t.Fatalf("expected /login?redirect=..., got %q", loc)
	}
	if called {
		t.Fatalf("expected next handler NOT to be called")
	}
}

func TestRequireAuth_PassesAuthenticatedUser(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	user := &models.User{ID: "u1", Email: "admin@example.com"}
	req := httptest.NewRequest(http.MethodGet, "/dashboard/media", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, user))
	rr := httptest.NewRecorder()

	called := false
	h.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !called {
		t.Fatalf("expected next handler to be called")
	}
}

func TestRequireAuth_HTMXUnauthorized(t *testing.T) {
	// HTMX requests get 401 + HX-Redirect header instead of a 303 redirect
	h := &Handler{logger: zerolog.Nop()}
	req := httptest.NewRequest(http.MethodGet, "/dashboard/media", nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()

	h.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for HTMX unauthenticated, got %d", rr.Code)
	}
	if got := rr.Header().Get("HX-Redirect"); got != "/login" {
		t.Fatalf("expected HX-Redirect=/login, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// RequireRole
// ---------------------------------------------------------------------------

func TestRequireRole_WrongRoleReturns403(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	db.AutoMigrate(&models.User{}, &models.StationUser{})

	h := &Handler{db: db, logger: zerolog.Nop()}

	// Regular user with no elevated role
	user := &models.User{ID: "u1", Email: "user@example.com", PlatformRole: models.PlatformRoleUser}
	db.Create(user)

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, user))
	rr := httptest.NewRecorder()

	h.RequireRole("platform_admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestRequireRole_PlatformAdminBypassesRoleCheck(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	db.AutoMigrate(&models.User{}, &models.StationUser{})

	h := &Handler{db: db, logger: zerolog.Nop()}

	admin := &models.User{ID: "u1", Email: "admin@example.com", PlatformRole: models.PlatformRoleAdmin}
	db.Create(admin)

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, admin))
	rr := httptest.NewRecorder()

	called := false
	h.RequireRole("platform_admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !called {
		t.Fatalf("expected next handler to be called for platform admin")
	}
}

func TestRequireRole_UnauthenticatedRedirects(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := httptest.NewRecorder()

	h.RequireRole("manager")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect for unauthenticated, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// RequireStation
// ---------------------------------------------------------------------------

func TestRequireStation_NoStationRedirects(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	db.AutoMigrate(&models.User{}, &models.StationUser{})
	h := &Handler{db: db, logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodGet, "/dashboard/media", nil)
	rr := httptest.NewRecorder()

	h.RequireStation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/dashboard/stations/select" {
		t.Fatalf("expected redirect to /dashboard/stations/select, got %q", loc)
	}
}

func TestRequireStation_ValidStationPassesThrough(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	db.AutoMigrate(&models.User{}, &models.Station{}, &models.StationUser{})
	h := &Handler{db: db, logger: zerolog.Nop()}

	station := &models.Station{ID: "s1", Name: "My Station", Active: true}
	db.Create(station)

	// User is a platform admin, so they have access without a station_user record
	user := &models.User{ID: "u1", Email: "admin@example.com", PlatformRole: models.PlatformRoleAdmin}
	db.Create(user)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/media", nil)
	ctx := context.WithValue(req.Context(), ctxKeyStation, station)
	ctx = context.WithValue(ctx, ctxKeyUser, user)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	called := false
	h.RequireStation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !called {
		t.Fatalf("expected next handler to be called")
	}
}

func TestRequireStation_UnauthorizedStationRedirects(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	db.AutoMigrate(&models.User{}, &models.Station{}, &models.StationUser{})
	h := &Handler{db: db, logger: zerolog.Nop()}

	station := &models.Station{ID: "s1", Name: "Other Station", Active: true}
	db.Create(station)

	// Regular user with no station_user record — should be denied
	user := &models.User{ID: "u2", Email: "other@example.com", PlatformRole: models.PlatformRoleUser}
	db.Create(user)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/media", nil)
	ctx := context.WithValue(req.Context(), ctxKeyStation, station)
	ctx = context.WithValue(ctx, ctxKeyUser, user)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.RequireStation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect for unauthorized station, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/dashboard/stations/select" {
		t.Fatalf("expected redirect to station select, got %q", loc)
	}
}

func TestRequireStation_HTMXNoStationReturns400(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	h := &Handler{db: db, logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodGet, "/dashboard/media", nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()

	h.RequireStation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for HTMX no-station, got %d", rr.Code)
	}
	if got := rr.Header().Get("HX-Redirect"); got != "/dashboard/stations/select" {
		t.Fatalf("expected HX-Redirect to station select, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// RequirePlatformAdmin
// ---------------------------------------------------------------------------

func TestRequirePlatformAdmin_NonAdminReturns403(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	user := &models.User{ID: "u1", PlatformRole: models.PlatformRoleUser}
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, user))
	rr := httptest.NewRecorder()

	h.RequirePlatformAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestRequirePlatformAdmin_AdminPassesThrough(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	admin := &models.User{ID: "u1", PlatformRole: models.PlatformRoleAdmin}
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUser, admin))
	rr := httptest.NewRecorder()

	called := false
	h.RequirePlatformAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !called {
		t.Fatalf("expected next handler to be called")
	}
}

func TestRequirePlatformAdmin_UnauthenticatedRedirects(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := httptest.NewRecorder()

	h.RequirePlatformAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// HasStationAccess
// ---------------------------------------------------------------------------

func TestHasStationAccess_PlatformAdminAlwaysHasAccess(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	db.AutoMigrate(&models.StationUser{})
	h := &Handler{db: db, logger: zerolog.Nop()}

	admin := &models.User{ID: "u1", PlatformRole: models.PlatformRoleAdmin}
	if !h.HasStationAccess(admin, "any-station-id") {
		t.Fatalf("platform admin should always have station access")
	}
}

func TestHasStationAccess_NilUserReturnsFalse(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	db.AutoMigrate(&models.StationUser{})
	h := &Handler{db: db, logger: zerolog.Nop()}

	if h.HasStationAccess(nil, "s1") {
		t.Fatalf("nil user should not have station access")
	}
}

func TestHasStationAccess_StationUserHasAccess(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	db.AutoMigrate(&models.Station{}, &models.User{}, &models.StationUser{})
	h := &Handler{db: db, logger: zerolog.Nop()}

	user := &models.User{ID: "u1", PlatformRole: models.PlatformRoleUser}
	db.Create(&models.Station{ID: "s1", Name: "Test"})
	db.Create(user)
	db.Create(&models.StationUser{
		ID:        "su1",
		UserID:    "u1",
		StationID: "s1",
		Role:      models.StationRoleDJ,
	})

	if !h.HasStationAccess(user, "s1") {
		t.Fatalf("user with station_user record should have access")
	}
}

func TestHasStationAccess_NoRecordReturnsFalse(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	db.AutoMigrate(&models.StationUser{})
	h := &Handler{db: db, logger: zerolog.Nop()}

	user := &models.User{ID: "u1", PlatformRole: models.PlatformRoleUser}
	if h.HasStationAccess(user, "s-other") {
		t.Fatalf("user without station_user record should not have access")
	}
}

// ---------------------------------------------------------------------------
// isSameOriginRequest
// ---------------------------------------------------------------------------

func TestIsSameOriginRequest_SameOriginPasses(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://example.com/dashboard", nil)
	req.Header.Set("Origin", "http://example.com")
	if !isSameOriginRequest(req) {
		t.Fatalf("expected same-origin request to pass")
	}
}

func TestIsSameOriginRequest_DifferentHostFails(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://example.com/dashboard", nil)
	req.Header.Set("Origin", "http://evil.com")
	if isSameOriginRequest(req) {
		t.Fatalf("expected cross-origin request to fail")
	}
}

func TestIsSameOriginRequest_NoOriginNoRefererFails(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://example.com/dashboard", nil)
	if isSameOriginRequest(req) {
		t.Fatalf("expected missing origin/referer to fail")
	}
}

func TestIsSameOriginRequest_RefererSameOriginPasses(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://example.com/dashboard", nil)
	req.Header.Set("Referer", "http://example.com/dashboard/other")
	if !isSameOriginRequest(req) {
		t.Fatalf("expected referer same-origin to pass")
	}
}

func TestIsSameOriginRequest_RefererDifferentHostFails(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://example.com/dashboard", nil)
	req.Header.Set("Referer", "http://attacker.com/page")
	if isSameOriginRequest(req) {
		t.Fatalf("expected referer cross-origin to fail")
	}
}

// ---------------------------------------------------------------------------
// requestScheme
// ---------------------------------------------------------------------------

func TestRequestScheme_XForwardedProtoHTTPS(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	if got := requestScheme(req); got != "https" {
		t.Fatalf("expected 'https', got %q", got)
	}
}

func TestRequestScheme_XForwardedProtoHTTP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-Proto", "http")
	if got := requestScheme(req); got != "http" {
		t.Fatalf("expected 'http', got %q", got)
	}
}

func TestRequestScheme_DefaultIsHTTP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := requestScheme(req); got != "http" {
		t.Fatalf("expected 'http' as default, got %q", got)
	}
}

func TestRequestScheme_XForwardedProtoCommaSeparated(t *testing.T) {
	// Proxies may chain multiple values; only the first should be used
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https, http")
	if got := requestScheme(req); got != "https" {
		t.Fatalf("expected 'https' (first value), got %q", got)
	}
}

// ---------------------------------------------------------------------------
// RequireRole - HTMX paths
// ---------------------------------------------------------------------------

func TestRequireRole_UnauthenticatedHTMX_SetsRedirectHeader(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	req := httptest.NewRequest(http.MethodPost, "/admin", nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()

	h.RequireRole("manager")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for HTMX unauthenticated, got %d", rr.Code)
	}
	if rr.Header().Get("HX-Redirect") != "/login" {
		t.Fatalf("expected HX-Redirect=/login, got %q", rr.Header().Get("HX-Redirect"))
	}
}

func TestRequireRole_InsufficientRoleHTMX_Returns403(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	user := &models.User{ID: "u1", Email: "user@example.com", PlatformRole: models.PlatformRoleUser}
	req := httptest.NewRequest(http.MethodPost, "/admin", nil)
	req.Header.Set("HX-Request", "true")
	ctx := context.WithValue(req.Context(), ctxKeyUser, user)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.RequireRole("platform_admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for HTMX insufficient role, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// RequireStation - HTMX no-station path
// ---------------------------------------------------------------------------

func TestRequireStation_NoStation_HTMX_SetsRedirectHeader(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	db.AutoMigrate(&models.User{}, &models.StationUser{})
	h := &Handler{db: db, logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodGet, "/dashboard/media", nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()

	h.RequireStation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for HTMX no-station, got %d", rr.Code)
	}
	if rr.Header().Get("HX-Redirect") != "/dashboard/stations/select" {
		t.Fatalf("expected HX-Redirect=/dashboard/stations/select, got %q", rr.Header().Get("HX-Redirect"))
	}
}

func TestRequireStation_UnauthorizedStation_HTMX_Returns403(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	db.AutoMigrate(&models.User{}, &models.Station{}, &models.StationUser{})
	h := &Handler{db: db, logger: zerolog.Nop()}

	station := &models.Station{ID: "s1", Name: "Private Station", Active: true}
	db.Create(station)
	user := &models.User{ID: "u2", Email: "noabc@example.com", PlatformRole: models.PlatformRoleUser}
	db.Create(user)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/media", nil)
	req.Header.Set("HX-Request", "true")
	ctx := context.WithValue(req.Context(), ctxKeyStation, station)
	ctx = context.WithValue(ctx, ctxKeyUser, user)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.RequireStation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for HTMX unauthorized station, got %d", rr.Code)
	}
	if rr.Header().Get("HX-Redirect") != "/dashboard/stations/select" {
		t.Fatalf("expected HX-Redirect=/dashboard/stations/select, got %q", rr.Header().Get("HX-Redirect"))
	}
}
