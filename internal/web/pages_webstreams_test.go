package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/migration"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

type stubWebstreamService struct {
	created     *models.Webstream
	failoverID  string
	resetID     string
	createErr   error
	failoverErr error
	resetErr    error
}

func (s *stubWebstreamService) CreateWebstream(_ context.Context, ws *models.Webstream) error {
	if s.createErr != nil {
		return s.createErr
	}
	copy := *ws
	s.created = &copy
	return nil
}

func (s *stubWebstreamService) TriggerFailover(_ context.Context, id string) error {
	s.failoverID = id
	return s.failoverErr
}

func (s *stubWebstreamService) ResetToPrimary(_ context.Context, id string) error {
	s.resetID = id
	return s.resetErr
}

func newWebstreamPageTestHandler(t *testing.T) (*Handler, *gorm.DB, models.User, models.Station) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{},
		&models.Station{},
		&models.StationUser{},
		&models.Webstream{},
		&models.LandingPage{},
		&migration.Job{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	user := models.User{ID: "user-1", Email: "manager@example.com", Password: "x"}
	station := models.Station{ID: "station-1", Name: "Station One", Active: true}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.StationUser{
		ID:        "su-1",
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

func withWebstreamRouteID(req *http.Request, id string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func withWebstreamContext(req *http.Request, user *models.User, station *models.Station) *http.Request {
	ctx := req.Context()
	if user != nil {
		ctx = context.WithValue(ctx, ctxKeyUser, user)
	}
	if station != nil {
		ctx = context.WithValue(ctx, ctxKeyStation, station)
	}
	return req.WithContext(ctx)
}

func TestWebstreamPagesRender(t *testing.T) {
	h, db, user, station := newWebstreamPageTestHandler(t)
	now := time.Date(2026, 3, 6, 22, 30, 0, 0, time.UTC)
	if err := db.Create(&models.Webstream{
		ID:                   "ws-render",
		StationID:            station.ID,
		Name:                 "Relay Render",
		Description:          "Fallback chain",
		URLs:                 []string{"https://a.example/stream", "https://b.example/stream"},
		CurrentURL:           "https://a.example/stream",
		CurrentIndex:         0,
		HealthCheckEnabled:   true,
		HealthStatus:         "healthy",
		FailoverEnabled:      true,
		LastHealthCheck:      &now,
		BufferSizeMS:         5000,
		ReconnectDelayMS:     1000,
		MaxReconnectAttempts: 5,
		CreatedAt:            now,
		UpdatedAt:            now,
	}).Error; err != nil {
		t.Fatalf("seed webstream: %v", err)
	}

	t.Run("list renders rows", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/dashboard/webstreams", nil)
		req = withWebstreamContext(req, &user, &station)
		rr := httptest.NewRecorder()

		h.WebstreamList(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		body := rr.Body.String()
		for _, want := range []string{"Webstreams", "Relay Render", "+1 fallback", "Healthy"} {
			if !strings.Contains(body, want) {
				t.Fatalf("expected body to contain %q", want)
			}
		}
	})

	t.Run("new renders default form values", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/dashboard/webstreams/new", nil)
		req = withWebstreamContext(req, &user, &station)
		rr := httptest.NewRecorder()

		h.WebstreamNew(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		body := rr.Body.String()
		for _, want := range []string{"New Webstream", "name=\"buffer_size_ms\"", "value=\"5000\"", "Enable automatic failover"} {
			if !strings.Contains(body, want) {
				t.Fatalf("expected body to contain %q", want)
			}
		}
	})

	t.Run("detail renders model-backed timestamps", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/dashboard/webstreams/ws-render", nil)
		req = withWebstreamContext(withWebstreamRouteID(req, "ws-render"), &user, &station)
		rr := httptest.NewRecorder()

		h.WebstreamDetail(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		body := rr.Body.String()
		for _, want := range []string{"Relay Render", "Last Checked", "Reset to Primary", "https://a.example/stream"} {
			if !strings.Contains(body, want) {
				t.Fatalf("expected body to contain %q", want)
			}
		}
	})

	t.Run("edit renders populated form", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/dashboard/webstreams/ws-render/edit", nil)
		req = withWebstreamContext(withWebstreamRouteID(req, "ws-render"), &user, &station)
		rr := httptest.NewRecorder()

		h.WebstreamEdit(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		body := rr.Body.String()
		for _, want := range []string{"Edit Webstream", "Relay Render", "https://a.example/stream", "hx-put=\"/dashboard/webstreams/ws-render\""} {
			if !strings.Contains(body, want) {
				t.Fatalf("expected body to contain %q", want)
			}
		}
	})
}

func TestWebstreamCreateUsesServiceAndParsesURLs(t *testing.T) {
	h, _, _, station := newWebstreamPageTestHandler(t)
	stub := &stubWebstreamService{}
	h.webstreamSvc = stub

	form := url.Values{
		"name":                      {"Relay 1"},
		"description":               {"backup chain"},
		"urls":                      {" https://a.example/stream\nhttps://b.example/stream , https://c.example/stream "},
		"health_check_enabled":      {"on"},
		"health_check_method":       {"HEAD"},
		"health_check_interval_sec": {"20"},
		"health_check_timeout_sec":  {"4"},
		"failover_enabled":          {"on"},
		"auto_recover_enabled":      {"on"},
		"buffer_size_ms":            {"7000"},
		"reconnect_delay_ms":        {"900"},
		"max_reconnect_attempts":    {"8"},
		"active":                    {"on"},
	}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/webstreams", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req = withWebstreamContext(req, nil, &station)
	rr := httptest.NewRecorder()

	h.WebstreamCreate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("HX-Redirect"); got == "" || !strings.HasPrefix(got, "/dashboard/webstreams/") {
		t.Fatalf("unexpected HX-Redirect %q", got)
	}
	if stub.created == nil {
		t.Fatal("expected service create to be called")
	}
	if len(stub.created.URLs) != 3 || stub.created.URLs[1] != "https://b.example/stream" {
		t.Fatalf("unexpected parsed urls: %+v", stub.created.URLs)
	}
	if stub.created.HealthCheckInterval != 20*time.Second || stub.created.HealthCheckTimeout != 4*time.Second {
		t.Fatalf("unexpected health check settings: %+v", stub.created)
	}
}

func TestWebstreamUpdateDeleteAndFallbackActions(t *testing.T) {
	h, db, _, station := newWebstreamPageTestHandler(t)
	ws := models.Webstream{
		ID:                 "ws-1",
		StationID:          station.ID,
		Name:               "Relay",
		URLs:               []string{"https://a.example/stream", "https://b.example/stream"},
		CurrentURL:         "https://a.example/stream",
		CurrentIndex:       0,
		FailoverEnabled:    true,
		AutoRecoverEnabled: true,
	}
	if err := db.Create(&ws).Error; err != nil {
		t.Fatalf("seed webstream: %v", err)
	}

	t.Run("update persists form fields and redirects", func(t *testing.T) {
		form := url.Values{
			"name":                      {"Relay Updated"},
			"description":               {"new description"},
			"urls":                      {"https://x.example/stream, https://y.example/stream"},
			"health_check_interval_sec": {"45"},
			"health_check_timeout_sec":  {"6"},
			"health_check_method":       {"GET"},
			"buffer_size_ms":            {"6500"},
			"reconnect_delay_ms":        {"1100"},
			"max_reconnect_attempts":    {"4"},
		}
		req := httptest.NewRequest(http.MethodPost, "/dashboard/webstreams/ws-1", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req = withWebstreamContext(withWebstreamRouteID(req, ws.ID), nil, &station)
		rr := httptest.NewRecorder()

		h.WebstreamUpdate(rr, req)
		if rr.Code != http.StatusSeeOther {
			t.Fatalf("expected 303, got %d body=%s", rr.Code, rr.Body.String())
		}

		var updated models.Webstream
		if err := db.First(&updated, "id = ?", ws.ID).Error; err != nil {
			t.Fatalf("reload webstream: %v", err)
		}
		if updated.Name != "Relay Updated" || len(updated.URLs) != 2 || updated.URLs[0] != "https://x.example/stream" {
			t.Fatalf("unexpected updated webstream: %+v", updated)
		}
	})

	t.Run("fallback failover rotates current url and returns hx success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/dashboard/webstreams/ws-1/failover", nil)
		req.Header.Set("HX-Request", "true")
		req = withWebstreamContext(withWebstreamRouteID(req, ws.ID), nil, &station)
		rr := httptest.NewRecorder()

		h.WebstreamFailover(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "Failover triggered") {
			t.Fatalf("unexpected failover body: %s", rr.Body.String())
		}

		var updated models.Webstream
		if err := db.First(&updated, "id = ?", ws.ID).Error; err != nil {
			t.Fatalf("reload webstream: %v", err)
		}
		if updated.CurrentIndex != 1 || updated.CurrentURL != "https://y.example/stream" {
			t.Fatalf("unexpected failover state: %+v", updated)
		}
	})

	t.Run("fallback reset returns to primary and returns hx success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/dashboard/webstreams/ws-1/reset", nil)
		req.Header.Set("HX-Request", "true")
		req = withWebstreamContext(withWebstreamRouteID(req, ws.ID), nil, &station)
		rr := httptest.NewRecorder()

		h.WebstreamReset(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "Reset to primary URL") {
			t.Fatalf("unexpected reset body: %s", rr.Body.String())
		}

		var updated models.Webstream
		if err := db.First(&updated, "id = ?", ws.ID).Error; err != nil {
			t.Fatalf("reload webstream: %v", err)
		}
		if updated.CurrentIndex != 0 || updated.CurrentURL != "https://x.example/stream" {
			t.Fatalf("unexpected reset state: %+v", updated)
		}
	})

	t.Run("delete removes row and sets hx redirect", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/dashboard/webstreams/ws-1/delete", nil)
		req.Header.Set("HX-Request", "true")
		req = withWebstreamContext(withWebstreamRouteID(req, ws.ID), nil, &station)
		rr := httptest.NewRecorder()

		h.WebstreamDelete(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		if rr.Header().Get("HX-Redirect") != "/dashboard/webstreams" {
			t.Fatalf("unexpected HX-Redirect %q", rr.Header().Get("HX-Redirect"))
		}

		var count int64
		db.Model(&models.Webstream{}).Where("id = ?", ws.ID).Count(&count)
		if count != 0 {
			t.Fatalf("expected deleted webstream, remaining=%d", count)
		}
	})
}

func TestWebstreamServiceBranchesOnPages(t *testing.T) {
	h, db, _, station := newWebstreamPageTestHandler(t)
	ws := models.Webstream{ID: "ws-2", StationID: station.ID, Name: "Relay", URLs: []string{"https://a.example/stream"}, CurrentURL: "https://a.example/stream"}
	if err := db.Create(&ws).Error; err != nil {
		t.Fatalf("seed webstream: %v", err)
	}

	t.Run("create surfaces service errors", func(t *testing.T) {
		h.webstreamSvc = &stubWebstreamService{createErr: errors.New("boom")}
		form := url.Values{"name": {"Relay"}, "urls": {"https://a.example/stream"}}
		req := httptest.NewRequest(http.MethodPost, "/dashboard/webstreams", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req = withWebstreamContext(req, nil, &station)
		rr := httptest.NewRecorder()

		h.WebstreamCreate(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("failover surfaces service hx error", func(t *testing.T) {
		h.webstreamSvc = &stubWebstreamService{failoverErr: errors.New("manual failure")}
		req := httptest.NewRequest(http.MethodPost, "/dashboard/webstreams/ws-2/failover", nil)
		req.Header.Set("HX-Request", "true")
		req = withWebstreamContext(withWebstreamRouteID(req, ws.ID), nil, &station)
		rr := httptest.NewRecorder()

		h.WebstreamFailover(rr, req)
		if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "manual failure") {
			t.Fatalf("unexpected failover response: code=%d body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("reset uses service when available", func(t *testing.T) {
		stub := &stubWebstreamService{}
		h.webstreamSvc = stub
		req := httptest.NewRequest(http.MethodPost, "/dashboard/webstreams/ws-2/reset", nil)
		req = withWebstreamContext(withWebstreamRouteID(req, ws.ID), nil, &station)
		rr := httptest.NewRecorder()

		h.WebstreamReset(rr, req)
		if rr.Code != http.StatusSeeOther {
			t.Fatalf("expected 303, got %d body=%s", rr.Code, rr.Body.String())
		}
		if stub.resetID != ws.ID {
			t.Fatalf("expected reset id %q, got %q", ws.ID, stub.resetID)
		}
	})

	t.Run("reset service error returns 500", func(t *testing.T) {
		h.webstreamSvc = &stubWebstreamService{resetErr: errors.New("reset failed")}
		req := httptest.NewRequest(http.MethodPost, "/dashboard/webstreams/ws-2/reset", nil)
		req = withWebstreamContext(withWebstreamRouteID(req, ws.ID), nil, &station)
		rr := httptest.NewRecorder()

		h.WebstreamReset(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 on reset error, got %d body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("reset service error htmx returns danger alert", func(t *testing.T) {
		h.webstreamSvc = &stubWebstreamService{resetErr: errors.New("reset failed htmx")}
		req := httptest.NewRequest(http.MethodPost, "/dashboard/webstreams/ws-2/reset", nil)
		req.Header.Set("HX-Request", "true")
		req = withWebstreamContext(withWebstreamRouteID(req, ws.ID), nil, &station)
		rr := httptest.NewRecorder()

		h.WebstreamReset(rr, req)
		if !strings.Contains(rr.Body.String(), "reset failed htmx") {
			t.Fatalf("expected error message in HTMX response, got: %s", rr.Body.String())
		}
	})
}
