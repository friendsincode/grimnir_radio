package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newSchedulePermissionTestHandler(t *testing.T) (*Handler, models.User, models.User, models.Station) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Station{}, &models.StationUser{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	station := models.Station{ID: "s1", Name: "Station One", Active: true}
	manager := models.User{ID: "u-manager", Email: "manager@example.com", Password: "x"}
	dj := models.User{ID: "u-dj", Email: "dj@example.com", Password: "x"}
	for _, record := range []any{&station, &manager, &dj} {
		if err := db.Create(record).Error; err != nil {
			t.Fatalf("seed record: %v", err)
		}
	}

	for _, record := range []models.StationUser{
		{ID: "su-manager", UserID: manager.ID, StationID: station.ID, Role: models.StationRoleManager},
		{ID: "su-dj", UserID: dj.ID, StationID: station.ID, Role: models.StationRoleDJ},
	} {
		if err := db.Create(&record).Error; err != nil {
			t.Fatalf("seed station user %s: %v", record.ID, err)
		}
	}

	return &Handler{db: db, logger: zerolog.Nop()}, manager, dj, station
}

func scheduleMutationTestRouter(h *Handler, user *models.User, station *models.Station) http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), ctxKeyUser, user)
			ctx = context.WithValue(ctx, ctxKeyStation, station)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})

	r.Route("/dashboard/schedule", func(r chi.Router) {
		r.With(h.RequireRole("manager")).Post("/entries", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
		r.With(h.RequireRole("manager")).Put("/entries/{id}", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
		r.With(h.RequireRole("manager")).Delete("/entries/{id}", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
		r.With(h.RequireRole("manager")).Post("/refresh", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
	})

	return r
}

func TestScheduleMutationRoutesRequireManagerRole(t *testing.T) {
	h, manager, dj, station := newSchedulePermissionTestHandler(t)

	cases := []struct {
		name       string
		user       *models.User
		method     string
		path       string
		wantStatus int
	}{
		{name: "dj create denied", user: &dj, method: http.MethodPost, path: "/dashboard/schedule/entries", wantStatus: http.StatusForbidden},
		{name: "dj update denied", user: &dj, method: http.MethodPut, path: "/dashboard/schedule/entries/e1", wantStatus: http.StatusForbidden},
		{name: "dj delete denied", user: &dj, method: http.MethodDelete, path: "/dashboard/schedule/entries/e1", wantStatus: http.StatusForbidden},
		{name: "dj refresh denied", user: &dj, method: http.MethodPost, path: "/dashboard/schedule/refresh", wantStatus: http.StatusForbidden},
		{name: "manager create allowed", user: &manager, method: http.MethodPost, path: "/dashboard/schedule/entries", wantStatus: http.StatusNoContent},
		{name: "manager update allowed", user: &manager, method: http.MethodPut, path: "/dashboard/schedule/entries/e1", wantStatus: http.StatusNoContent},
		{name: "manager delete allowed", user: &manager, method: http.MethodDelete, path: "/dashboard/schedule/entries/e1", wantStatus: http.StatusNoContent},
		{name: "manager refresh allowed", user: &manager, method: http.MethodPost, path: "/dashboard/schedule/refresh", wantStatus: http.StatusNoContent},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := scheduleMutationTestRouter(h, tc.user, &station)
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rr := httptest.NewRecorder()

			router.ServeHTTP(rr, req)

			if rr.Code != tc.wantStatus {
				t.Fatalf("expected %d, got %d body=%s", tc.wantStatus, rr.Code, rr.Body.String())
			}
		})
	}
}
