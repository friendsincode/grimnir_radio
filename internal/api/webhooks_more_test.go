/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func withManagerWebhookClaims(req *http.Request, stationID string) *http.Request {
	return req.WithContext(auth.WithClaims(req.Context(), webhookClaims("u-mgr", stationID, string(models.RoleManager))))
}

func TestWebhookAPI_List_MissingStationID(t *testing.T) {
	api, _ := newWebhookAPITest(t)

	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), webhookClaims("u-mgr", "s1", string(models.RoleManager))))
	rr := httptest.NewRecorder()
	api.handleList(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("list missing station_id: got %d, want 400", rr.Code)
	}
}

func TestWebhookAPI_Create_MissingFields(t *testing.T) {
	api, db := newWebhookAPITest(t)

	db.Create(&models.StationUser{ID: "su-m", UserID: "u-mgr", StationID: "s1", Role: models.StationRoleManager}) //nolint:errcheck

	t.Run("missing station_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"url": "https://example.com/hook"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		req = withManagerWebhookClaims(req, "s1")
		rr := httptest.NewRecorder()
		api.handleCreate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("missing url", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"station_id": "s1"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		req = withManagerWebhookClaims(req, "s1")
		rr := httptest.NewRecorder()
		api.handleCreate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
		req = withManagerWebhookClaims(req, "s1")
		rr := httptest.NewRecorder()
		api.handleCreate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})
}

func TestWebhookAPI_Get_NotFound(t *testing.T) {
	api, db := newWebhookAPITest(t)

	db.Create(&models.StationUser{ID: "su-m2", UserID: "u-mgr", StationID: "s1", Role: models.StationRoleManager}) //nolint:errcheck

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	req = withWebhookRouteParam(req, "nonexistent-id")
	req = withManagerWebhookClaims(req, "s1")
	rr := httptest.NewRecorder()
	api.handleGet(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("get not found: got %d, want 404", rr.Code)
	}
}

func TestWebhookAPI_Update_NotFound(t *testing.T) {
	api, _ := newWebhookAPITest(t)

	body, _ := json.Marshal(map[string]any{"url": "https://example.com/new"})
	req := httptest.NewRequest("PUT", "/nonexistent", bytes.NewReader(body))
	req = withWebhookRouteParam(req, "nonexistent-id")
	req = withManagerWebhookClaims(req, "s1")
	rr := httptest.NewRecorder()
	api.handleUpdate(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("update not found: got %d, want 404", rr.Code)
	}
}

func TestWebhookAPI_Update_InvalidJSON(t *testing.T) {
	api, db := newWebhookAPITest(t)

	db.Create(&models.StationUser{ID: "su-m3", UserID: "u-mgr", StationID: "s1", Role: models.StationRoleManager}) //nolint:errcheck

	// Create a webhook first
	wh := models.NewWebhookTarget("s1", "https://example.com", "show_start")
	db.Create(wh) //nolint:errcheck

	req := httptest.NewRequest("PUT", "/"+wh.ID, bytes.NewReader([]byte("{")))
	req = withWebhookRouteParam(req, wh.ID)
	req = withManagerWebhookClaims(req, "s1")
	rr := httptest.NewRecorder()
	api.handleUpdate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("update invalid json: got %d, want 400", rr.Code)
	}
}

func TestWebhookAPI_Delete_NotFound(t *testing.T) {
	api, _ := newWebhookAPITest(t)

	req := httptest.NewRequest("DELETE", "/nonexistent", nil)
	req = withWebhookRouteParam(req, "nonexistent-id")
	req = withManagerWebhookClaims(req, "s1")
	rr := httptest.NewRecorder()
	api.handleDelete(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("delete not found: got %d, want 404", rr.Code)
	}
}

func TestWebhookAPI_Test_NotFound(t *testing.T) {
	api, _ := newWebhookAPITest(t)

	req := httptest.NewRequest("POST", "/nonexistent/test", nil)
	req = withWebhookRouteParam(req, "nonexistent-id")
	req = withManagerWebhookClaims(req, "s1")
	rr := httptest.NewRecorder()
	api.handleTest(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("test not found: got %d, want 404", rr.Code)
	}
}

func TestWebhookAPI_Logs_NotFound(t *testing.T) {
	api, _ := newWebhookAPITest(t)

	req := httptest.NewRequest("GET", "/nonexistent/logs", nil)
	req = withWebhookRouteParam(req, "nonexistent-id")
	req = withManagerWebhookClaims(req, "s1")
	rr := httptest.NewRecorder()
	api.handleLogs(rr, req)
	// Returns 404 when webhook not found
	if rr.Code != http.StatusNotFound && rr.Code != http.StatusForbidden {
		t.Fatalf("logs not found: got %d", rr.Code)
	}
}

func TestWebhookAPI_Update_NoOpEmpty(t *testing.T) {
	api, db := newWebhookAPITest(t)

	db.Create(&models.StationUser{ID: "su-m4", UserID: "u-mgr", StationID: "s1", Role: models.StationRoleManager}) //nolint:errcheck

	wh := models.NewWebhookTarget("s1", "https://example.com", "show_start")
	db.Create(wh) //nolint:errcheck

	// Empty body — no updates
	body, _ := json.Marshal(map[string]any{})
	req := httptest.NewRequest("PUT", "/"+wh.ID, bytes.NewReader(body))
	req = withWebhookRouteParam(req, wh.ID)
	req = withManagerWebhookClaims(req, "s1")
	rr := httptest.NewRecorder()
	api.handleUpdate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("update no-op: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
}
