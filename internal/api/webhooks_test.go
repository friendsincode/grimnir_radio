package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/webhooks"
)

func newWebhookAPITest(t *testing.T) (*WebhookAPI, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.StationUser{}, &models.WebhookTarget{}, &models.WebhookLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	base := &API{db: db, bus: events.NewBus()}
	return NewWebhookAPI(base, webhooks.NewService(db, events.NewBus(), zerolog.Nop())), db
}

func webhookClaims(userID, stationID string, roles ...string) *auth.Claims {
	return &auth.Claims{UserID: userID, StationID: stationID, Roles: roles}
}

func withWebhookRouteParam(req *http.Request, id string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestWebhookAPI_CRUDAndLogs(t *testing.T) {
	api, db := newWebhookAPITest(t)

	for _, su := range []models.StationUser{
		{ID: "su-manager", UserID: "u-manager", StationID: "s1", Role: models.StationRoleManager},
		{ID: "su-dj", UserID: "u-dj", StationID: "s1", Role: models.StationRoleDJ},
	} {
		if err := db.Create(&su).Error; err != nil {
			t.Fatalf("seed station user: %v", err)
		}
	}

	t.Run("create requires station manager access", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"station_id": "s1",
			"url":        "https://example.com/hook",
			"events":     "show_start,show_end",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", bytes.NewReader(body))
		req = req.WithContext(auth.WithClaims(req.Context(), webhookClaims("u-dj", "s1", string(models.RoleDJ))))
		rr := httptest.NewRecorder()

		api.handleCreate(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d body=%s", rr.Code, rr.Body.String())
		}
	})

	var created struct {
		Webhook models.WebhookTarget `json:"webhook"`
		Secret  string               `json:"secret"`
	}

	t.Run("create succeeds for manager", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"station_id": "s1",
			"url":        "https://example.com/hook",
			"events":     "show_start,show_end",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", bytes.NewReader(body))
		req = req.WithContext(auth.WithClaims(req.Context(), webhookClaims("u-manager", "s1", string(models.RoleManager))))
		rr := httptest.NewRecorder()

		api.handleCreate(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
		}
		if err := json.NewDecoder(rr.Body).Decode(&created); err != nil {
			t.Fatalf("decode create response: %v", err)
		}
		if created.Webhook.ID == "" || created.Secret == "" {
			t.Fatalf("expected created webhook id and secret, got %+v", created)
		}
	})

	t.Run("list returns created webhook", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks?station_id=s1", nil)
		req = req.WithContext(auth.WithClaims(req.Context(), webhookClaims("u-manager", "s1", string(models.RoleManager))))
		rr := httptest.NewRecorder()

		api.handleList(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}

		var payload struct {
			Webhooks []models.WebhookTarget `json:"webhooks"`
		}
		if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
			t.Fatalf("decode list response: %v", err)
		}
		if len(payload.Webhooks) != 1 || payload.Webhooks[0].ID != created.Webhook.ID {
			t.Fatalf("unexpected list payload: %+v", payload)
		}
	})

	t.Run("get returns webhook", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/"+created.Webhook.ID, nil)
		req = withWebhookRouteParam(req, created.Webhook.ID)
		req = req.WithContext(auth.WithClaims(req.Context(), webhookClaims("u-manager", "s1", string(models.RoleManager))))
		rr := httptest.NewRecorder()

		api.handleGet(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("update persists changes", func(t *testing.T) {
		url := "https://example.com/new-hook"
		active := false
		body, _ := json.Marshal(map[string]any{
			"url":    url,
			"active": active,
		})
		req := httptest.NewRequest(http.MethodPut, "/api/v1/webhooks/"+created.Webhook.ID, bytes.NewReader(body))
		req = withWebhookRouteParam(req, created.Webhook.ID)
		req = req.WithContext(auth.WithClaims(req.Context(), webhookClaims("u-manager", "s1", string(models.RoleManager))))
		rr := httptest.NewRecorder()

		api.handleUpdate(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}

		var updated models.WebhookTarget
		if err := db.First(&updated, "id = ?", created.Webhook.ID).Error; err != nil {
			t.Fatalf("reload webhook: %v", err)
		}
		if updated.URL != url || updated.Active != active {
			t.Fatalf("unexpected webhook after update: %+v", updated)
		}
	})

	t.Run("logs returns webhook logs", func(t *testing.T) {
		if err := db.Create(&models.WebhookLog{
			ID:         "log-1",
			TargetID:   created.Webhook.ID,
			Event:      "show_start",
			Payload:    "{}",
			StatusCode: 200,
		}).Error; err != nil {
			t.Fatalf("seed webhook log: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/"+created.Webhook.ID+"/logs", nil)
		req = withWebhookRouteParam(req, created.Webhook.ID)
		req = req.WithContext(auth.WithClaims(req.Context(), webhookClaims("u-manager", "s1", string(models.RoleManager))))
		rr := httptest.NewRecorder()

		api.handleLogs(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}

		var payload struct {
			Logs []models.WebhookLog `json:"logs"`
		}
		if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
			t.Fatalf("decode logs response: %v", err)
		}
		if len(payload.Logs) != 1 || payload.Logs[0].ID != "log-1" {
			t.Fatalf("unexpected logs payload: %+v", payload)
		}
	})

	t.Run("delete removes webhook and logs", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/webhooks/"+created.Webhook.ID, nil)
		req = withWebhookRouteParam(req, created.Webhook.ID)
		req = req.WithContext(auth.WithClaims(req.Context(), webhookClaims("u-manager", "s1", string(models.RoleManager))))
		rr := httptest.NewRecorder()

		api.handleDelete(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}

		var count int64
		db.Model(&models.WebhookTarget{}).Where("id = ?", created.Webhook.ID).Count(&count)
		if count != 0 {
			t.Fatalf("expected webhook deleted, remaining count=%d", count)
		}
		db.Model(&models.WebhookLog{}).Where("target_id = ?", created.Webhook.ID).Count(&count)
		if count != 0 {
			t.Fatalf("expected logs deleted, remaining count=%d", count)
		}
	})
}

func TestWebhookAPI_TestEndpointAndStationAccess(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.StationUser{}, &models.WebhookTarget{}, &models.WebhookLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create(&models.StationUser{
		ID: "su-manager", UserID: "u-manager", StationID: "s1", Role: models.StationRoleManager,
	}).Error; err != nil {
		t.Fatalf("seed manager: %v", err)
	}
	if err := db.Create(&models.StationUser{
		ID: "su-other", UserID: "u-other", StationID: "s2", Role: models.StationRoleManager,
	}).Error; err != nil {
		t.Fatalf("seed other manager: %v", err)
	}

	var hitCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	webhook := models.NewWebhookTarget("s1", srv.URL, "show_start")
	if err := db.Create(webhook).Error; err != nil {
		t.Fatalf("seed webhook: %v", err)
	}

	base := &API{db: db, bus: events.NewBus()}
	api := NewWebhookAPI(base, webhooks.NewService(db, events.NewBus(), zerolog.Nop()))

	t.Run("test endpoint sends webhook", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/"+webhook.ID+"/test", nil)
		req = withWebhookRouteParam(req, webhook.ID)
		req = req.WithContext(auth.WithClaims(req.Context(), webhookClaims("u-manager", "s1", string(models.RoleManager))))
		rr := httptest.NewRecorder()

		api.handleTest(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		if hitCount != 1 {
			t.Fatalf("expected test webhook to be sent once, got %d", hitCount)
		}
	})

	t.Run("station access helper denies other station", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks?station_id=s1", nil)
		req = req.WithContext(auth.WithClaims(req.Context(), webhookClaims("u-other", "s2", string(models.RoleManager))))
		if api.hasStationAccess(req, "s1", "manager") {
			t.Fatal("expected station access to be denied")
		}
	})
}
