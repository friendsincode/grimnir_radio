package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	webstreamsvc "github.com/friendsincode/grimnir_radio/internal/webstream"
)

func newWebstreamAPITest(t *testing.T) (*API, *gorm.DB, *events.Bus, *webstreamsvc.Service) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.Webstream{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	bus := events.NewBus()
	svc := webstreamsvc.NewService(db, bus, zerolog.Nop())
	t.Cleanup(func() {
		if err := svc.Shutdown(); err != nil {
			t.Fatalf("shutdown webstream service: %v", err)
		}
	})

	return &API{db: db, bus: bus, webstreamSvc: svc, logger: zerolog.Nop()}, db, bus, svc
}

func withAPIRouteID(req *http.Request, id string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestWebstreamAPI_CRUDAndFailover(t *testing.T) {
	api, db, bus, _ := newWebstreamAPITest(t)

	createAudit := bus.Subscribe(events.EventAuditWebstreamCreate)
	updateAudit := bus.Subscribe(events.EventAuditWebstreamUpdate)
	deleteAudit := bus.Subscribe(events.EventAuditWebstreamDelete)
	failoverEvent := bus.Subscribe(events.EventWebstreamFailover)
	defer bus.Unsubscribe(events.EventAuditWebstreamCreate, createAudit)
	defer bus.Unsubscribe(events.EventAuditWebstreamUpdate, updateAudit)
	defer bus.Unsubscribe(events.EventAuditWebstreamDelete, deleteAudit)
	defer bus.Unsubscribe(events.EventWebstreamFailover, failoverEvent)

	body, _ := json.Marshal(createWebstreamRequest{
		StationID:            "station-1",
		Name:                 "News Relay",
		Description:          "primary relay",
		URLs:                 []string{"https://a.example/stream", "https://b.example/stream"},
		FailoverEnabled:      true,
		AutoRecoverEnabled:   true,
		BufferSizeMS:         6000,
		ReconnectDelayMS:     1200,
		MaxReconnectAttempts: 9,
		PassthroughMetadata:  true,
		OverrideMetadata:     true,
		CustomMetadata:       map[string]any{"title": "Relay"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webstreams", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	api.handleCreateWebstream(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}

	var created webstreamResponse
	if err := json.NewDecoder(rr.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.ID == "" || created.CurrentIndex != 0 || created.CurrentURL != "https://a.example/stream" {
		t.Fatalf("unexpected created webstream: %+v", created)
	}
	select {
	case payload := <-createAudit:
		if payload["resource_id"] != created.ID {
			t.Fatalf("unexpected create audit payload: %+v", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected create audit event")
	}

	t.Run("get and list return created webstream", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/webstreams/"+created.ID, nil)
		req = withAPIRouteID(req, created.ID)
		rr := httptest.NewRecorder()

		api.handleGetWebstream(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}

		req = httptest.NewRequest(http.MethodGet, "/api/v1/webstreams?station_id=station-1", nil)
		rr = httptest.NewRecorder()
		api.handleListWebstreams(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var payload struct {
			Webstreams []webstreamResponse `json:"webstreams"`
		}
		if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
			t.Fatalf("decode list response: %v", err)
		}
		if len(payload.Webstreams) != 1 || payload.Webstreams[0].ID != created.ID {
			t.Fatalf("unexpected list payload: %+v", payload)
		}
	})

	t.Run("update persists duration-backed fields", func(t *testing.T) {
		name := "Emergency Relay"
		interval := 45
		timeout := 7
		body, _ := json.Marshal(updateWebstreamRequest{
			Name:                   &name,
			HealthCheckIntervalSec: &interval,
			HealthCheckTimeoutSec:  &timeout,
		})
		req := httptest.NewRequest(http.MethodPut, "/api/v1/webstreams/"+created.ID, bytes.NewReader(body))
		req = withAPIRouteID(req, created.ID)
		rr := httptest.NewRecorder()

		api.handleUpdateWebstream(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}

		var ws models.Webstream
		if err := db.First(&ws, "id = ?", created.ID).Error; err != nil {
			t.Fatalf("reload webstream: %v", err)
		}
		if ws.Name != name || ws.HealthCheckInterval != 45*time.Second || ws.HealthCheckTimeout != 7*time.Second {
			t.Fatalf("unexpected updated webstream: %+v", ws)
		}
		select {
		case payload := <-updateAudit:
			if payload["resource_id"] != created.ID {
				t.Fatalf("unexpected update audit payload: %+v", payload)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("expected update audit event")
		}
	})

	t.Run("manual failover advances current url and publishes event", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webstreams/"+created.ID+"/failover", nil)
		req = withAPIRouteID(req, created.ID)
		rr := httptest.NewRecorder()

		api.handleTriggerWebstreamFailover(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var got webstreamResponse
		if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
			t.Fatalf("decode failover response: %v", err)
		}
		if got.CurrentIndex != 1 || got.CurrentURL != "https://b.example/stream" {
			t.Fatalf("unexpected failover state: %+v", got)
		}
		select {
		case payload := <-failoverEvent:
			if payload["current_index"] != 1 {
				t.Fatalf("unexpected failover payload: %+v", payload)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("expected failover event")
		}
	})

	t.Run("reset returns to primary url", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webstreams/"+created.ID+"/reset", nil)
		req = withAPIRouteID(req, created.ID)
		rr := httptest.NewRecorder()

		api.handleResetWebstreamToPrimary(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var got webstreamResponse
		if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
			t.Fatalf("decode reset response: %v", err)
		}
		if got.CurrentIndex != 0 || got.CurrentURL != "https://a.example/stream" {
			t.Fatalf("unexpected reset state: %+v", got)
		}
	})

	t.Run("delete removes row and emits audit event", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/webstreams/"+created.ID, nil)
		req = withAPIRouteID(req, created.ID)
		rr := httptest.NewRecorder()

		api.handleDeleteWebstream(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var count int64
		db.Model(&models.Webstream{}).Where("id = ?", created.ID).Count(&count)
		if count != 0 {
			t.Fatalf("expected deleted webstream, remaining=%d", count)
		}
		select {
		case payload := <-deleteAudit:
			if payload["resource_id"] != created.ID {
				t.Fatalf("unexpected delete audit payload: %+v", payload)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("expected delete audit event")
		}
	})
}

func TestWebstreamAPI_ErrorBranches(t *testing.T) {
	api, _, _, _ := newWebstreamAPITest(t)

	t.Run("create rejects missing required fields", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webstreams", bytes.NewBufferString(`{"station_id":"","name":"","urls":[]}`))
		rr := httptest.NewRecorder()
		api.handleCreateWebstream(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("get rejects missing id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/webstreams/", nil)
		rr := httptest.NewRecorder()
		api.handleGetWebstream(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("failover rejects missing id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webstreams//failover", nil)
		rr := httptest.NewRecorder()
		api.handleTriggerWebstreamFailover(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
		}
	})
}
