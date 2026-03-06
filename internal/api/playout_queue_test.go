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
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestPlayoutQueueCreateListAndRoleChecks(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.StationUser{}, &models.Mount{}, &models.MediaItem{}, &models.PlayoutQueueItem{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if err := db.Create(&models.StationUser{
		ID:        "su-manager",
		UserID:    "u-manager",
		StationID: "s1",
		Role:      models.StationRoleManager,
	}).Error; err != nil {
		t.Fatalf("seed manager membership: %v", err)
	}
	if err := db.Create(&models.StationUser{
		ID:        "su-dj",
		UserID:    "u-dj",
		StationID: "s1",
		Role:      models.StationRoleDJ,
	}).Error; err != nil {
		t.Fatalf("seed dj membership: %v", err)
	}
	if err := db.Create(&models.Mount{
		ID:        "m1",
		StationID: "s1",
		Name:      "Main",
		Format:    "mp3",
		URL:       "/live/main",
	}).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}
	if err := db.Create(&models.MediaItem{
		ID:        "track-1",
		StationID: "s1",
		Title:     "Track One",
		Artist:    "Artist One",
		Duration:  90 * time.Second,
		Path:      "/tmp/track1.mp3",
	}).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	a := &API{db: db}

	// DJ should be forbidden from mutating queue.
	djReqBody := map[string]any{
		"station_id": "s1",
		"mount_id":   "m1",
		"media_id":   "track-1",
	}
	djBody, _ := json.Marshal(djReqBody)
	djReq := httptest.NewRequest("POST", "/api/v1/playout/queue", bytes.NewReader(djBody))
	djReq = djReq.WithContext(auth.WithClaims(djReq.Context(), &auth.Claims{
		UserID:    "u-dj",
		StationID: "s1",
		Roles:     []string{string(models.RoleDJ)},
	}))
	djRes := httptest.NewRecorder()
	a.handlePlayoutQueueCreate(djRes, djReq)
	if djRes.Code != 403 {
		t.Fatalf("expected 403 for DJ mutate, got %d body=%s", djRes.Code, djRes.Body.String())
	}

	// Manager can create queue item.
	mgrBody, _ := json.Marshal(djReqBody)
	mgrReq := httptest.NewRequest("POST", "/api/v1/playout/queue", bytes.NewReader(mgrBody))
	mgrReq = mgrReq.WithContext(auth.WithClaims(mgrReq.Context(), &auth.Claims{
		UserID:    "u-manager",
		StationID: "s1",
		Roles:     []string{string(models.RoleManager)},
	}))
	mgrRes := httptest.NewRecorder()
	a.handlePlayoutQueueCreate(mgrRes, mgrReq)
	if mgrRes.Code != 201 {
		t.Fatalf("expected 201 for manager create, got %d body=%s", mgrRes.Code, mgrRes.Body.String())
	}

	// DJ can read queue.
	listReq := httptest.NewRequest("GET", "/api/v1/playout/queue?station_id=s1&mount_id=m1", nil)
	listReq = listReq.WithContext(auth.WithClaims(listReq.Context(), &auth.Claims{
		UserID:    "u-dj",
		StationID: "s1",
		Roles:     []string{string(models.RoleDJ)},
	}))
	listRes := httptest.NewRecorder()
	a.handlePlayoutQueueList(listRes, listReq)
	if listRes.Code != 200 {
		t.Fatalf("expected 200 for DJ list, got %d body=%s", listRes.Code, listRes.Body.String())
	}

	var payload struct {
		Count int                        `json:"count"`
		Items []playoutQueueItemResponse `json:"items"`
	}
	if err := json.NewDecoder(listRes.Body).Decode(&payload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if payload.Count != 1 || len(payload.Items) != 1 {
		t.Fatalf("unexpected queue payload: %+v", payload)
	}
	if payload.Items[0].Position != 1 || payload.Items[0].MediaID != "track-1" {
		t.Fatalf("unexpected queue item: %+v", payload.Items[0])
	}
}

func TestPlayoutQueueReorderAndDelete(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.StationUser{}, &models.PlayoutQueueItem{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if err := db.Create(&models.StationUser{
		ID:        "su-manager",
		UserID:    "u-manager",
		StationID: "s1",
		Role:      models.StationRoleManager,
	}).Error; err != nil {
		t.Fatalf("seed manager membership: %v", err)
	}

	item1 := models.PlayoutQueueItem{ID: "q1", StationID: "s1", MountID: "m1", MediaID: "track-1", Position: 1}
	item2 := models.PlayoutQueueItem{ID: "q2", StationID: "s1", MountID: "m1", MediaID: "track-2", Position: 2}
	if err := db.Create(&item1).Error; err != nil {
		t.Fatalf("seed item1: %v", err)
	}
	if err := db.Create(&item2).Error; err != nil {
		t.Fatalf("seed item2: %v", err)
	}

	a := &API{db: db}

	reorderBody, _ := json.Marshal(map[string]any{"position": 1})
	reorderReq := httptest.NewRequest("PATCH", "/api/v1/playout/queue/q2", bytes.NewReader(reorderBody))
	reorderReq = withQueueRouteParam(reorderReq, "queueID", "q2")
	reorderReq = reorderReq.WithContext(auth.WithClaims(reorderReq.Context(), &auth.Claims{
		UserID:    "u-manager",
		StationID: "s1",
		Roles:     []string{string(models.RoleManager)},
	}))
	reorderRes := httptest.NewRecorder()
	a.handlePlayoutQueueReorder(reorderRes, reorderReq)
	if reorderRes.Code != 200 {
		t.Fatalf("expected 200 reorder, got %d body=%s", reorderRes.Code, reorderRes.Body.String())
	}

	var ordered []models.PlayoutQueueItem
	if err := db.Order("position ASC").Find(&ordered).Error; err != nil {
		t.Fatalf("load ordered queue: %v", err)
	}
	if len(ordered) != 2 || ordered[0].ID != "q2" || ordered[0].Position != 1 || ordered[1].ID != "q1" || ordered[1].Position != 2 {
		t.Fatalf("unexpected order after reorder: %+v", ordered)
	}

	deleteReq := httptest.NewRequest("DELETE", "/api/v1/playout/queue/q2", nil)
	deleteReq = withQueueRouteParam(deleteReq, "queueID", "q2")
	deleteReq = deleteReq.WithContext(auth.WithClaims(deleteReq.Context(), &auth.Claims{
		UserID:    "u-manager",
		StationID: "s1",
		Roles:     []string{string(models.RoleManager)},
	}))
	deleteRes := httptest.NewRecorder()
	a.handlePlayoutQueueDelete(deleteRes, deleteReq)
	if deleteRes.Code != 200 {
		t.Fatalf("expected 200 delete, got %d body=%s", deleteRes.Code, deleteRes.Body.String())
	}

	var remaining []models.PlayoutQueueItem
	if err := db.Order("position ASC").Find(&remaining).Error; err != nil {
		t.Fatalf("load remaining queue: %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != "q1" || remaining[0].Position != 1 {
		t.Fatalf("unexpected remaining queue: %+v", remaining)
	}
}

func TestPlayoutQueueCreate_DefaultMountAndCrossStationDenied(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.StationUser{}, &models.Mount{}, &models.MediaItem{}, &models.PlayoutQueueItem{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if err := db.Create(&models.StationUser{
		ID:        "su-manager",
		UserID:    "u-manager",
		StationID: "s1",
		Role:      models.StationRoleManager,
	}).Error; err != nil {
		t.Fatalf("seed manager membership: %v", err)
	}
	if err := db.Create(&models.Mount{
		ID:        "m-a",
		StationID: "s1",
		Name:      "Alpha",
		Format:    "mp3",
		URL:       "/live/alpha",
	}).Error; err != nil {
		t.Fatalf("seed mount alpha: %v", err)
	}
	if err := db.Create(&models.Mount{
		ID:        "m-z",
		StationID: "s1",
		Name:      "Zulu",
		Format:    "mp3",
		URL:       "/live/zulu",
	}).Error; err != nil {
		t.Fatalf("seed mount zulu: %v", err)
	}
	if err := db.Create(&models.MediaItem{
		ID:        "track-1",
		StationID: "s1",
		Title:     "Track One",
		Duration:  30 * time.Second,
		Path:      "/tmp/track1.mp3",
	}).Error; err != nil {
		t.Fatalf("seed media in s1: %v", err)
	}
	if err := db.Create(&models.MediaItem{
		ID:        "track-2",
		StationID: "s2",
		Title:     "Track Two",
		Duration:  30 * time.Second,
		Path:      "/tmp/track2.mp3",
	}).Error; err != nil {
		t.Fatalf("seed media in s2: %v", err)
	}

	a := &API{db: db}

	t.Run("defaults to first mount when mount_id omitted", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"station_id": "s1",
			"media_id":   "track-1",
		})
		req := httptest.NewRequest("POST", "/api/v1/playout/queue", bytes.NewReader(body))
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
			UserID:    "u-manager",
			StationID: "s1",
			Roles:     []string{string(models.RoleManager)},
		}))
		rr := httptest.NewRecorder()
		a.handlePlayoutQueueCreate(rr, req)
		if rr.Code != 201 {
			t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
		}

		var resp playoutQueueItemResponse
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("decode create response: %v", err)
		}
		if resp.MountID != "m-a" {
			t.Fatalf("expected default mount m-a, got %s", resp.MountID)
		}
	})

	t.Run("denies cross-station media enqueue", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"station_id": "s1",
			"mount_id":   "m-a",
			"media_id":   "track-2",
		})
		req := httptest.NewRequest("POST", "/api/v1/playout/queue", bytes.NewReader(body))
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
			UserID:    "u-manager",
			StationID: "s1",
			Roles:     []string{string(models.RoleManager)},
		}))
		rr := httptest.NewRecorder()
		a.handlePlayoutQueueCreate(rr, req)
		if rr.Code != 400 {
			t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
		}
	})
}

func TestPlayoutQueueChangeEventsEmitted(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.StationUser{}, &models.Mount{}, &models.MediaItem{}, &models.PlayoutQueueItem{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create(&models.StationUser{
		ID:        "su-manager",
		UserID:    "u-manager",
		StationID: "s1",
		Role:      models.StationRoleManager,
	}).Error; err != nil {
		t.Fatalf("seed station user: %v", err)
	}
	if err := db.Create(&models.Mount{ID: "m1", StationID: "s1", Name: "Main", Format: "mp3", URL: "/live/main"}).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}
	if err := db.Create(&models.MediaItem{ID: "track-1", StationID: "s1", Title: "One", Path: "/tmp/1.mp3", Duration: 10 * time.Second}).Error; err != nil {
		t.Fatalf("seed media1: %v", err)
	}
	if err := db.Create(&models.MediaItem{ID: "track-2", StationID: "s1", Title: "Two", Path: "/tmp/2.mp3", Duration: 10 * time.Second}).Error; err != nil {
		t.Fatalf("seed media2: %v", err)
	}

	bus := events.NewBus()
	a := &API{db: db, bus: bus}
	sub := bus.Subscribe(events.EventPlayoutQueueChange)
	defer bus.Unsubscribe(events.EventPlayoutQueueChange, sub)

	managerClaims := &auth.Claims{
		UserID:    "u-manager",
		StationID: "s1",
		Roles:     []string{string(models.RoleManager)},
	}

	// Create item 1.
	create1Body, _ := json.Marshal(map[string]any{"station_id": "s1", "mount_id": "m1", "media_id": "track-1"})
	create1Req := httptest.NewRequest("POST", "/api/v1/playout/queue", bytes.NewReader(create1Body))
	create1Req = create1Req.WithContext(auth.WithClaims(create1Req.Context(), managerClaims))
	create1Res := httptest.NewRecorder()
	a.handlePlayoutQueueCreate(create1Res, create1Req)
	if create1Res.Code != 201 {
		t.Fatalf("create1 status=%d body=%s", create1Res.Code, create1Res.Body.String())
	}
	expectQueueEvent(t, sub, "created", "s1", "m1")

	// Create item 2.
	create2Body, _ := json.Marshal(map[string]any{"station_id": "s1", "mount_id": "m1", "media_id": "track-2"})
	create2Req := httptest.NewRequest("POST", "/api/v1/playout/queue", bytes.NewReader(create2Body))
	create2Req = create2Req.WithContext(auth.WithClaims(create2Req.Context(), managerClaims))
	create2Res := httptest.NewRecorder()
	a.handlePlayoutQueueCreate(create2Res, create2Req)
	if create2Res.Code != 201 {
		t.Fatalf("create2 status=%d body=%s", create2Res.Code, create2Res.Body.String())
	}
	expectQueueEvent(t, sub, "created", "s1", "m1")

	var second playoutQueueItemResponse
	if err := json.NewDecoder(create2Res.Body).Decode(&second); err != nil {
		t.Fatalf("decode create2: %v", err)
	}

	// Reorder item 2 to position 1.
	reorderBody, _ := json.Marshal(map[string]any{"position": 1})
	reorderReq := httptest.NewRequest("PATCH", "/api/v1/playout/queue/"+second.ID, bytes.NewReader(reorderBody))
	reorderReq = withQueueRouteParam(reorderReq, "queueID", second.ID)
	reorderReq = reorderReq.WithContext(auth.WithClaims(reorderReq.Context(), managerClaims))
	reorderRes := httptest.NewRecorder()
	a.handlePlayoutQueueReorder(reorderRes, reorderReq)
	if reorderRes.Code != 200 {
		t.Fatalf("reorder status=%d body=%s", reorderRes.Code, reorderRes.Body.String())
	}
	expectQueueEvent(t, sub, "reordered", "s1", "m1")

	// Delete reordered item.
	deleteReq := httptest.NewRequest("DELETE", "/api/v1/playout/queue/"+second.ID, nil)
	deleteReq = withQueueRouteParam(deleteReq, "queueID", second.ID)
	deleteReq = deleteReq.WithContext(auth.WithClaims(deleteReq.Context(), managerClaims))
	deleteRes := httptest.NewRecorder()
	a.handlePlayoutQueueDelete(deleteRes, deleteReq)
	if deleteRes.Code != 200 {
		t.Fatalf("delete status=%d body=%s", deleteRes.Code, deleteRes.Body.String())
	}
	expectQueueEvent(t, sub, "deleted", "s1", "m1")
}

func TestPlayoutQueueList_ClaimsStationFallbackAndMountValidation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.StationUser{}, &models.Mount{}, &models.PlayoutQueueItem{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create(&models.StationUser{
		ID:        "su-dj",
		UserID:    "u-dj",
		StationID: "s1",
		Role:      models.StationRoleDJ,
	}).Error; err != nil {
		t.Fatalf("seed station user: %v", err)
	}
	if err := db.Create(&models.Mount{ID: "m1", StationID: "s1", Name: "Main", Format: "mp3", URL: "/live/main"}).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}
	if err := db.Create(&models.PlayoutQueueItem{ID: "q1", StationID: "s1", MountID: "m1", MediaID: "track-1", Position: 1}).Error; err != nil {
		t.Fatalf("seed queue: %v", err)
	}

	a := &API{db: db}

	t.Run("uses claims station when station_id query omitted", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/playout/queue?mount_id=m1", nil)
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
			UserID:    "u-dj",
			StationID: "s1",
			Roles:     []string{string(models.RoleDJ)},
		}))
		rr := httptest.NewRecorder()
		a.handlePlayoutQueueList(rr, req)
		if rr.Code != 200 {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("rejects mount that does not belong to station", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/playout/queue?station_id=s1&mount_id=not-a-station-mount", nil)
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
			UserID:    "u-dj",
			StationID: "s1",
			Roles:     []string{string(models.RoleDJ)},
		}))
		rr := httptest.NewRecorder()
		a.handlePlayoutQueueList(rr, req)
		if rr.Code != 400 {
			t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
		}
	})
}

func expectQueueEvent(t *testing.T, sub events.Subscriber, wantAction, wantStation, wantMount string) {
	t.Helper()
	select {
	case payload := <-sub:
		if got, _ := payload["action"].(string); got != wantAction {
			t.Fatalf("event action=%q want %q payload=%v", got, wantAction, payload)
		}
		if got, _ := payload["station_id"].(string); got != wantStation {
			t.Fatalf("event station_id=%q want %q payload=%v", got, wantStation, payload)
		}
		if got, _ := payload["mount_id"].(string); got != wantMount {
			t.Fatalf("event mount_id=%q want %q payload=%v", got, wantMount, payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for queue event")
	}
}

func withQueueRouteParam(req *http.Request, key, val string) *http.Request {
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add(key, val)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx)
	return req.WithContext(ctx)
}
